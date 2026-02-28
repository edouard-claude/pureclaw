package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/edouard/pureclaw/internal/agent"
	"github.com/edouard/pureclaw/internal/llm"
	"github.com/edouard/pureclaw/internal/memory"
	"github.com/edouard/pureclaw/internal/platform"
	"github.com/edouard/pureclaw/internal/tool"
	"github.com/edouard/pureclaw/internal/workspace"
)

// Replaceable for testing.
var (
	subAgentSignalContext = func() (context.Context, context.CancelFunc) {
		return signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	}
	subAgentWorkspaceLoad = workspace.Load
	subAgentNewLLMClient  = func(apiKey, model string) agent.LLMClient { return llm.NewClient(apiKey, model) }
	subAgentNewMemory     = func(root string) *memory.Memory { return memory.New(root) }
	subAgentOsStat        = os.Stat
)

// pathGuardedHandler wraps a tool handler to validate the "path" argument
// against the workspace root, preventing file operations outside the workspace.
func pathGuardedHandler(root string, handler tool.Handler) tool.Handler {
	return func(ctx context.Context, args json.RawMessage) tool.ToolResult {
		var pathArg struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(args, &pathArg); err == nil && pathArg.Path != "" {
			target := pathArg.Path
			if !filepath.IsAbs(target) {
				target = filepath.Join(root, target)
			}
			if err := platform.ValidatePath(root, target); err != nil {
				slog.Warn("path guard blocked operation",
					"component", "cmd", "operation", "path_guard",
					"root", root, "path", pathArg.Path, "error", err)
				return tool.ToolResult{
					Success: false,
					Error:   fmt.Sprintf("path outside workspace: %s", pathArg.Path),
				}
			}
		}
		return handler(ctx, args)
	}
}

// runSubAgentCmd starts pureclaw in sub-agent mode.
// Differences from main agent:
// - No Telegram poller (no Sender, no VoiceDownloader)
// - No heartbeat
// - No file watcher
// - RESTRICTED tool registry (no spawn_agent, no reload_workspace)
// - All file operations path-guarded to workspace root
// - Autonomous LLM-driven loop: reads AGENT.md mission, works, writes result.md
// - Self-enforces timeout via context
func runSubAgentCmd(workspacePath, configPath, vaultPath string, stdin io.Reader, stderr io.Writer) int {
	slog.Info("starting sub-agent mode",
		"component", "cmd", "operation", "run_subagent",
		"workspace", workspacePath)

	// 1. Validate workspace path exists and is a directory.
	wsInfo, err := subAgentOsStat(workspacePath)
	if err != nil {
		slog.Error("workspace path not accessible",
			"component", "cmd", "operation", "run_subagent",
			"path", workspacePath, "error", err)
		fmt.Fprintf(stderr, "Error: workspace path not accessible: %v\n", err)
		return 1
	}
	if !wsInfo.IsDir() {
		slog.Error("workspace path is not a directory",
			"component", "cmd", "operation", "run_subagent",
			"path", workspacePath)
		fmt.Fprintf(stderr, "Error: workspace path is not a directory: %s\n", workspacePath)
		return 1
	}

	// 2. Load config.
	cfg, err := configLoad(configPath)
	if err != nil {
		slog.Error("failed to load config",
			"component", "cmd", "operation", "run_subagent",
			"error", err)
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	// 3. Get vault passphrase from env or interactive prompt.
	passphrase := os.Getenv("PURECLAW_VAULT_PASSPHRASE")
	if passphrase == "" {
		fmt.Fprint(stderr, "Vault passphrase: ")
		scanner := bufio.NewScanner(stdin)
		scanner.Scan()
		passphrase = strings.TrimSpace(scanner.Text())
		if passphrase == "" {
			fmt.Fprintln(stderr, "Error: passphrase cannot be empty")
			return 1
		}
	}

	// 4. Open vault.
	salt, err := vaultLoadSalt(vaultPath)
	if err != nil {
		slog.Error("failed to load vault salt",
			"component", "cmd", "operation", "run_subagent",
			"error", err)
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	key := vaultDeriveKey(passphrase, salt)
	v, err := vaultOpenFn(key, vaultPath)
	if err != nil {
		slog.Error("failed to open vault",
			"component", "cmd", "operation", "run_subagent",
			"error", err)
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	// 5. Get Mistral API key (sub-agent needs LLM access).
	mistralKey, err := v.Get("mistral_api_key")
	if err != nil {
		slog.Error("failed to get mistral API key",
			"component", "cmd", "operation", "run_subagent",
			"error", err)
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	// 6. Load workspace from sub-agent workspace path.
	ws, err := subAgentWorkspaceLoad(workspacePath)
	if err != nil {
		slog.Error("failed to load workspace",
			"component", "cmd", "operation", "run_subagent",
			"error", err)
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	// 7. Create LLM client.
	llmClient := subAgentNewLLMClient(mistralKey, cfg.ModelText)

	// 8. Create memory writer (sub-agent logs to its own memory/ directory).
	mem := subAgentNewMemory(workspacePath)

	// 9. Create RESTRICTED tool registry: NO spawn_agent, NO reload_workspace.
	// Extract vault secret values for exec_command sanitization.
	keys := v.List()
	secrets := make([]string, 0, len(keys))
	for _, k := range keys {
		val, err := v.Get(k)
		if err != nil {
			continue
		}
		if val != "" {
			secrets = append(secrets, val)
		}
	}

	registry := tool.NewRegistry()

	// Apply path guard to all file-operation tools, restricting them to the
	// sub-agent workspace root (FR38, NFR10 — sub-agent isolation).
	readFile := tool.NewReadFile()
	readFile.Handler = pathGuardedHandler(workspacePath, readFile.Handler)
	registry.Register(readFile)

	writeFile := tool.NewWriteFile()
	writeFile.Handler = pathGuardedHandler(workspacePath, writeFile.Handler)
	registry.Register(writeFile)

	listDir := tool.NewListDir()
	listDir.Handler = pathGuardedHandler(workspacePath, listDir.Handler)
	registry.Register(listDir)

	registry.Register(tool.NewExecCommand(secrets))
	// Deliberately NOT registering spawn_agent (depth=1 enforcement, FR38)
	// Deliberately NOT registering reload_workspace (no hot-reload for sub-agents)

	slog.Info("sub-agent tool registry created",
		"component", "cmd", "operation", "run_subagent",
		"tool_count", len(registry.Definitions()))

	// 10. Determine timeout.
	timeout := cfg.SubAgentTimeout.Duration
	if timeout == 0 {
		timeout = 5 * time.Minute // Default
	}

	// 11. Create context with timeout and signal handling.
	ctx, stop := subAgentSignalContext()
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// 12. Create agent with nil Telegram components (autonomous mode).
	ag := newAgent(agent.NewAgentConfig{
		Workspace:    ws,
		LLM:          llmClient,
		Sender:       nil, // No Telegram
		Memory:       mem,
		ToolExecutor: registry,
		// FileChanges: nil — no file watcher
		// HeartbeatTick: nil — no heartbeat
		// Heartbeat: nil
		// Transcriber: nil — no voice
		// VoiceDownloader: nil
	})

	// 13. Run sub-agent in autonomous mode.
	slog.Info("sub-agent started",
		"component", "cmd", "operation", "run_subagent",
		"workspace", workspacePath, "timeout", timeout)

	if err := ag.RunSubAgent(ctx); err != nil {
		slog.Error("sub-agent exited with error",
			"component", "cmd", "operation", "run_subagent",
			"error", err)
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	slog.Info("sub-agent completed",
		"component", "cmd", "operation", "run_subagent",
		"workspace", workspacePath)
	return 0
}
