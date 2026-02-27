package subagent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// saveRunnerVars saves and restores replaceable vars for runner.go tests.
func saveRunnerVars(t *testing.T) {
	t.Helper()
	origExecCommand := execCommand
	origOsReadFile := osReadFile
	origOsStat := osStat
	t.Cleanup(func() {
		execCommand = origExecCommand
		osReadFile = origOsReadFile
		osStat = origOsStat
	})
}

// fakeCmd returns a command that exits with the given code after a short delay.
// Uses the subprocess test helper pattern (TestHelperProcess).
func fakeCmd(exitCode int, sleepMs int) func(ctx context.Context, name string, args ...string) *exec.Cmd {
	return func(ctx context.Context, name string, args ...string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, os.Args[0],
			"-test.run=TestHelperProcess",
			"--",
			fmt.Sprintf("EXIT_CODE=%d", exitCode),
			fmt.Sprintf("SLEEP_MS=%d", sleepMs),
		)
		cmd.Env = append(os.Environ(), "GO_HELPER_PROCESS=1")
		return cmd
	}
}

// TestHelperProcess is used by fakeCmd to simulate subprocess behavior.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_HELPER_PROCESS") != "1" {
		return
	}
	// Parse args after "--"
	args := os.Args
	var exitCode int
	var sleepMs int
	for _, a := range args {
		if strings.HasPrefix(a, "EXIT_CODE=") {
			fmt.Sscanf(a, "EXIT_CODE=%d", &exitCode)
		}
		if strings.HasPrefix(a, "SLEEP_MS=") {
			fmt.Sscanf(a, "SLEEP_MS=%d", &sleepMs)
		}
	}
	if sleepMs > 0 {
		time.Sleep(time.Duration(sleepMs) * time.Millisecond)
	}
	os.Exit(exitCode)
}

func TestNewRunner(t *testing.T) {
	r := NewRunner()
	if r == nil {
		t.Fatal("NewRunner() returned nil")
	}
	if r.active {
		t.Error("new runner should not be active")
	}
}

func TestLaunchSubAgent_Success(t *testing.T) {
	saveRunnerVars(t)

	wsDir := t.TempDir()
	resultFile := filepath.Join(wsDir, "result.md")
	os.WriteFile(resultFile, []byte("task completed successfully"), 0644)

	execCommand = fakeCmd(0, 10)
	osReadFile = func(path string) ([]byte, error) {
		if filepath.Base(path) == "result.md" {
			return []byte("task completed successfully"), nil
		}
		return nil, os.ErrNotExist
	}

	r := NewRunner()
	resultCh := make(chan SubAgentResult, 1)

	err := r.LaunchSubAgent(context.Background(), RunnerConfig{
		BinaryPath:    os.Args[0],
		WorkspacePath: wsDir,
		TaskID:        "test-task-1",
		Timeout:       5 * time.Second,
		ConfigPath:    "/tmp/config.json",
		VaultPath:     "/tmp/vault.enc",
	}, resultCh)
	if err != nil {
		t.Fatalf("LaunchSubAgent() error = %v", err)
	}

	select {
	case result := <-resultCh:
		if result.TaskID != "test-task-1" {
			t.Errorf("TaskID = %q, want %q", result.TaskID, "test-task-1")
		}
		if result.WorkspacePath != wsDir {
			t.Errorf("WorkspacePath = %q, want %q", result.WorkspacePath, wsDir)
		}
		if result.ResultContent != "task completed successfully" {
			t.Errorf("ResultContent = %q, want %q", result.ResultContent, "task completed successfully")
		}
		if result.Err != nil {
			t.Errorf("Err = %v, want nil", result.Err)
		}
		if result.TimedOut {
			t.Error("TimedOut = true, want false")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for SubAgentResult")
	}
}

func TestLaunchSubAgent_Timeout(t *testing.T) {
	saveRunnerVars(t)

	wsDir := t.TempDir()

	// Process sleeps for 5 seconds, but timeout is 200ms.
	execCommand = fakeCmd(0, 5000)

	r := NewRunner()
	resultCh := make(chan SubAgentResult, 1)

	err := r.LaunchSubAgent(context.Background(), RunnerConfig{
		BinaryPath:    os.Args[0],
		WorkspacePath: wsDir,
		TaskID:        "timeout-task",
		Timeout:       200 * time.Millisecond,
		ConfigPath:    "/tmp/config.json",
		VaultPath:     "/tmp/vault.enc",
	}, resultCh)
	if err != nil {
		t.Fatalf("LaunchSubAgent() error = %v", err)
	}

	select {
	case result := <-resultCh:
		if !result.TimedOut {
			t.Error("TimedOut = false, want true")
		}
		if result.Err == nil {
			t.Error("Err = nil, want timeout error")
		}
		if !strings.Contains(result.Err.Error(), "timed out") {
			t.Errorf("Err = %q, want to contain 'timed out'", result.Err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for SubAgentResult")
	}
}

func TestLaunchSubAgent_AlreadyActive(t *testing.T) {
	saveRunnerVars(t)

	wsDir := t.TempDir()

	// Process sleeps long enough for the second launch attempt.
	execCommand = fakeCmd(0, 2000)

	r := NewRunner()
	resultCh := make(chan SubAgentResult, 2)

	// First launch.
	err := r.LaunchSubAgent(context.Background(), RunnerConfig{
		BinaryPath:    os.Args[0],
		WorkspacePath: wsDir,
		TaskID:        "first-task",
		Timeout:       5 * time.Second,
		ConfigPath:    "/tmp/config.json",
		VaultPath:     "/tmp/vault.enc",
	}, resultCh)
	if err != nil {
		t.Fatalf("first LaunchSubAgent() error = %v", err)
	}

	// Second launch (should fail).
	err = r.LaunchSubAgent(context.Background(), RunnerConfig{
		BinaryPath:    os.Args[0],
		WorkspacePath: wsDir,
		TaskID:        "second-task",
		Timeout:       5 * time.Second,
		ConfigPath:    "/tmp/config.json",
		VaultPath:     "/tmp/vault.enc",
	}, resultCh)
	if err == nil {
		t.Fatal("second LaunchSubAgent() should have returned error")
	}
	if !strings.Contains(err.Error(), "sub-agent already active") {
		t.Errorf("error = %q, want 'sub-agent already active'", err)
	}

	// Wait for first to complete.
	select {
	case <-resultCh:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for first SubAgentResult")
	}
}

func TestLaunchSubAgent_NoResultFile(t *testing.T) {
	saveRunnerVars(t)

	wsDir := t.TempDir()

	execCommand = fakeCmd(0, 10)
	osReadFile = func(path string) ([]byte, error) {
		return nil, os.ErrNotExist
	}

	r := NewRunner()
	resultCh := make(chan SubAgentResult, 1)

	err := r.LaunchSubAgent(context.Background(), RunnerConfig{
		BinaryPath:    os.Args[0],
		WorkspacePath: wsDir,
		TaskID:        "no-result-task",
		Timeout:       5 * time.Second,
		ConfigPath:    "/tmp/config.json",
		VaultPath:     "/tmp/vault.enc",
	}, resultCh)
	if err != nil {
		t.Fatalf("LaunchSubAgent() error = %v", err)
	}

	select {
	case result := <-resultCh:
		if result.ResultContent != "" {
			t.Errorf("ResultContent = %q, want empty", result.ResultContent)
		}
		if result.Err != nil {
			t.Errorf("Err = %v, want nil", result.Err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for SubAgentResult")
	}
}

func TestLaunchSubAgent_SubprocessError(t *testing.T) {
	saveRunnerVars(t)

	wsDir := t.TempDir()

	execCommand = fakeCmd(1, 10)
	osReadFile = func(path string) ([]byte, error) {
		return nil, os.ErrNotExist
	}

	r := NewRunner()
	resultCh := make(chan SubAgentResult, 1)

	err := r.LaunchSubAgent(context.Background(), RunnerConfig{
		BinaryPath:    os.Args[0],
		WorkspacePath: wsDir,
		TaskID:        "error-task",
		Timeout:       5 * time.Second,
		ConfigPath:    "/tmp/config.json",
		VaultPath:     "/tmp/vault.enc",
	}, resultCh)
	if err != nil {
		t.Fatalf("LaunchSubAgent() error = %v", err)
	}

	select {
	case result := <-resultCh:
		if result.Err == nil {
			t.Error("Err = nil, want error")
		}
		if !strings.Contains(result.Err.Error(), "exited with error") {
			t.Errorf("Err = %q, want to contain 'exited with error'", result.Err)
		}
		if result.TimedOut {
			t.Error("TimedOut = true, want false")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for SubAgentResult")
	}
}

func TestLaunchSubAgent_ContextCancelled(t *testing.T) {
	saveRunnerVars(t)

	wsDir := t.TempDir()

	// Process sleeps long, context will be cancelled.
	execCommand = fakeCmd(0, 5000)

	r := NewRunner()
	resultCh := make(chan SubAgentResult, 1)

	ctx, cancel := context.WithCancel(context.Background())

	err := r.LaunchSubAgent(ctx, RunnerConfig{
		BinaryPath:    os.Args[0],
		WorkspacePath: wsDir,
		TaskID:        "cancel-task",
		Timeout:       10 * time.Second,
		ConfigPath:    "/tmp/config.json",
		VaultPath:     "/tmp/vault.enc",
	}, resultCh)
	if err != nil {
		t.Fatalf("LaunchSubAgent() error = %v", err)
	}

	// Cancel after a short delay.
	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case result := <-resultCh:
		// When parent context is cancelled, the subprocess should be killed.
		if result.Err == nil {
			t.Error("Err = nil, want error (context cancelled)")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for SubAgentResult")
	}
}

func TestLaunchSubAgent_InvalidWorkspace(t *testing.T) {
	saveRunnerVars(t)

	r := NewRunner()
	resultCh := make(chan SubAgentResult, 1)

	err := r.LaunchSubAgent(context.Background(), RunnerConfig{
		BinaryPath:    os.Args[0],
		WorkspacePath: "/nonexistent/path/workspace",
		TaskID:        "invalid-ws-task",
		Timeout:       5 * time.Second,
		ConfigPath:    "/tmp/config.json",
		VaultPath:     "/tmp/vault.enc",
	}, resultCh)
	if err == nil {
		t.Fatal("LaunchSubAgent() should have returned error for invalid workspace")
	}
	if !strings.Contains(err.Error(), "workspace path does not exist") {
		t.Errorf("error = %q, want to contain 'workspace path does not exist'", err)
	}

	// Runner should not be active.
	r.mu.Lock()
	active := r.active
	r.mu.Unlock()
	if active {
		t.Error("runner should not be active after invalid workspace error")
	}
}

func TestLaunchSubAgent_ReleasesActive(t *testing.T) {
	saveRunnerVars(t)

	wsDir := t.TempDir()

	execCommand = fakeCmd(0, 10)
	osReadFile = func(path string) ([]byte, error) {
		return nil, os.ErrNotExist
	}

	r := NewRunner()
	resultCh := make(chan SubAgentResult, 2)

	// First launch.
	err := r.LaunchSubAgent(context.Background(), RunnerConfig{
		BinaryPath:    os.Args[0],
		WorkspacePath: wsDir,
		TaskID:        "first",
		Timeout:       5 * time.Second,
		ConfigPath:    "/tmp/config.json",
		VaultPath:     "/tmp/vault.enc",
	}, resultCh)
	if err != nil {
		t.Fatalf("first LaunchSubAgent() error = %v", err)
	}

	// Wait for first to complete.
	select {
	case <-resultCh:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for first result")
	}

	// Second launch should succeed (active flag released).
	err = r.LaunchSubAgent(context.Background(), RunnerConfig{
		BinaryPath:    os.Args[0],
		WorkspacePath: wsDir,
		TaskID:        "second",
		Timeout:       5 * time.Second,
		ConfigPath:    "/tmp/config.json",
		VaultPath:     "/tmp/vault.enc",
	}, resultCh)
	if err != nil {
		t.Fatalf("second LaunchSubAgent() error = %v, want nil", err)
	}

	// Wait for second to complete.
	select {
	case result := <-resultCh:
		if result.TaskID != "second" {
			t.Errorf("TaskID = %q, want %q", result.TaskID, "second")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for second result")
	}
}

func TestLaunchSubAgent_StartError(t *testing.T) {
	saveRunnerVars(t)

	wsDir := t.TempDir()

	// Return a command with a non-existent binary that will fail on Start().
	execCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "/nonexistent-binary-xxx")
	}

	r := NewRunner()
	resultCh := make(chan SubAgentResult, 1)

	err := r.LaunchSubAgent(context.Background(), RunnerConfig{
		BinaryPath:    "/nonexistent-binary-xxx",
		WorkspacePath: wsDir,
		TaskID:        "start-error-task",
		Timeout:       5 * time.Second,
		ConfigPath:    "/tmp/config.json",
		VaultPath:     "/tmp/vault.enc",
	}, resultCh)
	if err == nil {
		t.Fatal("LaunchSubAgent() should have returned error for start failure")
	}
	if !strings.Contains(err.Error(), "start sub-agent") {
		t.Errorf("error = %q, want to contain 'start sub-agent'", err)
	}

	// Runner should not be active.
	r.mu.Lock()
	active := r.active
	r.mu.Unlock()
	if active {
		t.Error("runner should not be active after start error")
	}
}

func TestLaunchSubAgent_StatError(t *testing.T) {
	saveRunnerVars(t)

	osStat = func(name string) (os.FileInfo, error) {
		return nil, errors.New("injected stat error")
	}

	r := NewRunner()
	resultCh := make(chan SubAgentResult, 1)

	err := r.LaunchSubAgent(context.Background(), RunnerConfig{
		BinaryPath:    os.Args[0],
		WorkspacePath: "/some/workspace",
		TaskID:        "stat-error-task",
		Timeout:       5 * time.Second,
		ConfigPath:    "/tmp/config.json",
		VaultPath:     "/tmp/vault.enc",
	}, resultCh)
	if err == nil {
		t.Fatal("LaunchSubAgent() should have returned error for stat failure")
	}
	if !strings.Contains(err.Error(), "invalid workspace path") {
		t.Errorf("error = %q, want to contain 'invalid workspace path'", err)
	}

	// Runner should not be active.
	r.mu.Lock()
	active := r.active
	r.mu.Unlock()
	if active {
		t.Error("runner should not be active after stat error")
	}
}

func TestLaunchSubAgent_ValidatePathError(t *testing.T) {
	saveRunnerVars(t)

	// Create a workspace dir and a symlink inside its parent that points outside.
	parentDir := t.TempDir()
	outsideDir := t.TempDir()
	symlinkPath := filepath.Join(parentDir, "escape-link")
	if err := os.Symlink(outsideDir, symlinkPath); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	r := NewRunner()
	resultCh := make(chan SubAgentResult, 1)

	// symlinkPath exists (passes osStat) but resolves outside parentDir (fails ValidatePath).
	err := r.LaunchSubAgent(context.Background(), RunnerConfig{
		BinaryPath:    os.Args[0],
		WorkspacePath: symlinkPath,
		TaskID:        "validate-path-task",
		Timeout:       5 * time.Second,
		ConfigPath:    "/tmp/config.json",
		VaultPath:     "/tmp/vault.enc",
	}, resultCh)
	if err == nil {
		t.Fatal("LaunchSubAgent() should have returned error for symlink escaping parent")
	}
	if !strings.Contains(err.Error(), "invalid workspace path") {
		t.Errorf("error = %q, want to contain 'invalid workspace path'", err)
	}

	// Runner should not be active.
	r.mu.Lock()
	active := r.active
	r.mu.Unlock()
	if active {
		t.Error("runner should not be active after ValidatePath error")
	}
}

func TestLaunchSubAgent_ReadFileError(t *testing.T) {
	saveRunnerVars(t)

	wsDir := t.TempDir()

	execCommand = fakeCmd(0, 10)
	// Simulate a read error that is NOT os.ErrNotExist (e.g., permission denied).
	osReadFile = func(path string) ([]byte, error) {
		return nil, errors.New("permission denied")
	}

	r := NewRunner()
	resultCh := make(chan SubAgentResult, 1)

	err := r.LaunchSubAgent(context.Background(), RunnerConfig{
		BinaryPath:    os.Args[0],
		WorkspacePath: wsDir,
		TaskID:        "read-error-task",
		Timeout:       5 * time.Second,
		ConfigPath:    "/tmp/config.json",
		VaultPath:     "/tmp/vault.enc",
	}, resultCh)
	if err != nil {
		t.Fatalf("LaunchSubAgent() error = %v", err)
	}

	select {
	case result := <-resultCh:
		// Should succeed (subprocess exited 0) but no result content.
		if result.ResultContent != "" {
			t.Errorf("ResultContent = %q, want empty", result.ResultContent)
		}
		if result.Err != nil {
			t.Errorf("Err = %v, want nil", result.Err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for SubAgentResult")
	}
}
