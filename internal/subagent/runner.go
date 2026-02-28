package subagent

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/edouard/pureclaw/internal/platform"
)

// SubAgentResult holds the outcome of a sub-agent execution.
type SubAgentResult struct {
	TaskID        string
	WorkspacePath string
	ResultContent string // Contents of result.md, empty if not found
	Err           error
	TimedOut      bool
}

// RunnerConfig holds parameters for launching a sub-agent subprocess.
type RunnerConfig struct {
	BinaryPath    string        // Path to pureclaw binary
	WorkspacePath string        // Path to sub-agent workspace
	TaskID        string        // Sub-agent task identifier
	Timeout       time.Duration // Maximum execution time
	ConfigPath    string        // Path to parent's config.json
	VaultPath     string        // Path to parent's vault.enc
}

// Runner manages sub-agent subprocess lifecycle.
type Runner struct {
	mu     sync.Mutex
	active bool
	done   chan struct{} // closed when watchSubAgent completes
}

// NewRunner creates a new sub-agent runner.
func NewRunner() *Runner {
	slog.Info("runner created", "component", "subagent", "operation", "new_runner")
	return &Runner{
		done: make(chan struct{}),
	}
}

// IsActive returns whether a sub-agent is currently running.
func (r *Runner) IsActive() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.active
}

// WaitForCompletion blocks until the active sub-agent completes or ctx expires.
// Returns nil immediately if no sub-agent is active.
func (r *Runner) WaitForCompletion(ctx context.Context) error {
	r.mu.Lock()
	if !r.active {
		r.mu.Unlock()
		return nil
	}
	done := r.done
	r.mu.Unlock()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// LaunchSubAgent spawns a sub-agent as a subprocess with timeout enforcement.
// Non-blocking: starts the subprocess and a watcher goroutine that sends
// the result on resultCh when the subprocess completes or times out.
// Returns error immediately if another sub-agent is already active.
func (r *Runner) LaunchSubAgent(ctx context.Context, cfg RunnerConfig, resultCh chan<- SubAgentResult) error {
	r.mu.Lock()
	if r.active {
		r.mu.Unlock()
		return fmt.Errorf("sub-agent already active")
	}
	r.active = true
	r.done = make(chan struct{})
	r.mu.Unlock()

	// Resolve to absolute path so the subprocess can find its workspace
	// regardless of the parent's working directory.
	absPath, err := filepath.Abs(cfg.WorkspacePath)
	if err != nil {
		r.mu.Lock()
		r.active = false
		r.mu.Unlock()
		return fmt.Errorf("resolve workspace path: %w", err)
	}
	cfg.WorkspacePath = absPath

	// Validate workspace exists.
	if _, err := osStat(cfg.WorkspacePath); err != nil {
		r.mu.Lock()
		r.active = false
		r.mu.Unlock()
		if os.IsNotExist(err) {
			return fmt.Errorf("workspace path does not exist: %s", cfg.WorkspacePath)
		}
		return fmt.Errorf("invalid workspace path: %w", err)
	}

	// Validate workspace is within its parent directory (path traversal guard).
	if err := platform.ValidatePath(filepath.Dir(cfg.WorkspacePath), cfg.WorkspacePath); err != nil {
		r.mu.Lock()
		r.active = false
		r.mu.Unlock()
		return fmt.Errorf("invalid workspace path: %w", err)
	}

	// Resolve config and vault to absolute paths for the subprocess.
	absConfig, err := filepath.Abs(cfg.ConfigPath)
	if err != nil {
		r.mu.Lock()
		r.active = false
		r.mu.Unlock()
		return fmt.Errorf("resolve config path: %w", err)
	}
	cfg.ConfigPath = absConfig

	absVault, err := filepath.Abs(cfg.VaultPath)
	if err != nil {
		r.mu.Lock()
		r.active = false
		r.mu.Unlock()
		return fmt.Errorf("resolve vault path: %w", err)
	}
	cfg.VaultPath = absVault

	// Build subprocess command.
	timeoutCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	cmd := execCommand(timeoutCtx, cfg.BinaryPath, "run", "--agent", cfg.WorkspacePath,
		"--config", cfg.ConfigPath, "--vault", cfg.VaultPath)
	cmd.Dir = cfg.WorkspacePath
	cmd.Stdout = os.Stderr // Sub-agent logs to parent's stderr
	cmd.Stderr = os.Stderr

	// Process group for clean kill.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		// Send SIGTERM to process group for graceful shutdown.
		// Go's exec.Cmd sends SIGKILL automatically after WaitDelay if needed.
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	}
	cmd.WaitDelay = 5 * time.Second

	slog.Info("launching sub-agent",
		"component", "subagent", "operation", "launch",
		"task_id", cfg.TaskID, "workspace", cfg.WorkspacePath,
		"timeout", cfg.Timeout)

	if err := cmd.Start(); err != nil {
		cancel()
		r.mu.Lock()
		r.active = false
		r.mu.Unlock()
		return fmt.Errorf("start sub-agent: %w", err)
	}

	// Watcher goroutine — monitors subprocess, sends result.
	go r.watchSubAgent(timeoutCtx, cancel, cmd, cfg, resultCh)

	return nil
}

func (r *Runner) watchSubAgent(timeoutCtx context.Context, cancel context.CancelFunc, cmd *exec.Cmd, cfg RunnerConfig, resultCh chan<- SubAgentResult) {
	defer cancel()

	result := SubAgentResult{
		TaskID:        cfg.TaskID,
		WorkspacePath: cfg.WorkspacePath,
	}

	// Wait for subprocess to complete.
	err := cmd.Wait()
	if err != nil {
		if timeoutCtx.Err() == context.DeadlineExceeded {
			result.TimedOut = true
			result.Err = fmt.Errorf("sub-agent timed out after %s", cfg.Timeout)
			slog.Warn("sub-agent timed out",
				"component", "subagent", "operation", "timeout",
				"task_id", cfg.TaskID, "timeout", cfg.Timeout)
		} else {
			result.Err = fmt.Errorf("sub-agent exited with error: %w", err)
			slog.Error("sub-agent failed",
				"component", "subagent", "operation", "watch",
				"task_id", cfg.TaskID, "error", err)
		}
	} else {
		slog.Info("sub-agent completed successfully",
			"component", "subagent", "operation", "watch",
			"task_id", cfg.TaskID)
	}

	// Read result.md if it exists.
	resultPath := filepath.Join(cfg.WorkspacePath, "result.md")
	data, readErr := osReadFile(resultPath)
	if readErr == nil {
		result.ResultContent = string(data)
		slog.Info("sub-agent result collected",
			"component", "subagent", "operation", "collect_result",
			"task_id", cfg.TaskID, "result_bytes", len(data))
	} else if !os.IsNotExist(readErr) {
		slog.Warn("failed to read sub-agent result",
			"component", "subagent", "operation", "collect_result",
			"task_id", cfg.TaskID, "error", readErr)
	}

	// Release active flag and signal completion BEFORE sending result so callers
	// can immediately launch another sub-agent after receiving the result, and
	// WaitForCompletion unblocks before the result is processed by the event loop.
	r.mu.Lock()
	r.active = false
	done := r.done
	r.mu.Unlock()
	close(done)

	// Send result to event loop. The channel must be buffered (capacity >= 1).
	resultCh <- result
}

// Replaceable vars for testing.
var (
	execCommand = exec.CommandContext
	osReadFile  = os.ReadFile
)
