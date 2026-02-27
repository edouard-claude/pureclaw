package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/edouard/pureclaw/internal/agent"
	"github.com/edouard/pureclaw/internal/config"
	"github.com/edouard/pureclaw/internal/heartbeat"
	"github.com/edouard/pureclaw/internal/subagent"
	"github.com/edouard/pureclaw/internal/llm"
	"github.com/edouard/pureclaw/internal/memory"
	"github.com/edouard/pureclaw/internal/telegram"
	"github.com/edouard/pureclaw/internal/tool"
	"github.com/edouard/pureclaw/internal/vault"
	"github.com/edouard/pureclaw/internal/watcher"
	"github.com/edouard/pureclaw/internal/workspace"
)

// Replaceable for testing.
var (
	configLoad    = config.Load
	vaultLoadSalt = vault.LoadSalt
	vaultDeriveKey = vault.DeriveKey
	vaultOpenFn   = vault.Open
	workspaceLoad = workspace.Load
	newLLMClient   = func(apiKey, model string) agent.LLMClient { return llm.NewClient(apiKey, model) }
	newAudioClient = func(apiKey, model string) agent.Transcriber { return llm.NewClient(apiKey, model) }
	newTGClient    = telegram.NewClient
	newPoller     = func(client *telegram.Client, allowedIDs []int64, timeout int) *telegram.Poller {
		return telegram.NewPoller(client, allowedIDs, timeout)
	}
	newSender = func(client *telegram.Client) agent.Sender { return telegram.NewSender(client) }
	newMemory = func(root string) *memory.Memory { return memory.New(root) }
	newAgent  = agent.New
	signalContext = func() (context.Context, context.CancelFunc) {
		return signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	}
	runPollerFn = func(ctx context.Context, p *telegram.Poller, ch chan<- telegram.TelegramMessage) {
		p.Run(ctx, ch)
	}
	osExecutable = os.Executable
)

func runAgent(stdin io.Reader, stdout, stderr io.Writer) int {
	// 1. Load config
	cfg, err := configLoad(defaultConfigPath)
	if err != nil {
		slog.Error("failed to load config",
			"component", "cmd",
			"operation", "run",
			"error", err,
		)
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	// 2. Prompt for vault passphrase
	fmt.Fprint(stderr, "Vault passphrase: ")
	scanner := bufio.NewScanner(stdin)
	scanner.Scan()
	passphrase := strings.TrimSpace(scanner.Text())
	if passphrase == "" {
		fmt.Fprintln(stderr, "Error: passphrase cannot be empty")
		return 1
	}

	// 3. Open vault
	salt, err := vaultLoadSalt(defaultVaultPath)
	if err != nil {
		slog.Error("failed to load vault salt",
			"component", "cmd",
			"operation", "run",
			"error", err,
		)
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	key := vaultDeriveKey(passphrase, salt)
	v, err := vaultOpenFn(key, defaultVaultPath)
	if err != nil {
		slog.Error("failed to open vault",
			"component", "cmd",
			"operation", "run",
			"error", err,
		)
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	// 4. Get secrets
	mistralKey, err := v.Get("mistral_api_key")
	if err != nil {
		slog.Error("failed to get mistral API key",
			"component", "cmd",
			"operation", "run",
			"error", err,
		)
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	telegramToken, err := v.Get("telegram_bot_token")
	if err != nil {
		slog.Error("failed to get telegram bot token",
			"component", "cmd",
			"operation", "run",
			"error", err,
		)
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	// 5. Load workspace
	ws, err := workspaceLoad(cfg.Workspace)
	if err != nil {
		slog.Error("failed to load workspace",
			"component", "cmd",
			"operation", "run",
			"error", err,
		)
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	// 6. Create file watcher for workspace hot-reload
	fileChanges := make(chan struct{}, 1)
	w := watcher.New(cfg.Workspace, 2*time.Second)

	// 6a. Create clients
	llmClient := newLLMClient(mistralKey, cfg.ModelText)
	audioClient := newAudioClient(mistralKey, cfg.ModelAudio)
	tgClient := newTGClient(telegramToken)
	poller := newPoller(tgClient, cfg.TelegramAllowedIDs, 30)
	sender := newSender(tgClient)

	// 6b. Create memory (serves both writer and searcher)
	mem := newMemory(cfg.Workspace)

	// 6c. Extract vault secret values for exec_command sanitization (NFR9)
	keys := v.List()
	secrets := make([]string, 0, len(keys))
	for _, k := range keys {
		val, err := v.Get(k)
		if err != nil {
			slog.Warn("failed to read vault secret for sanitization",
				"component", "cmd",
				"operation", "run",
				"key", k,
				"error", err,
			)
			continue
		}
		if val != "" {
			secrets = append(secrets, val)
		}
	}

	// 6d. Create tool registry
	registry := tool.NewRegistry()
	registry.Register(tool.NewReadFile())
	registry.Register(tool.NewWriteFile())
	registry.Register(tool.NewListDir())
	registry.Register(tool.NewExecCommand(secrets))
	registry.Register(tool.NewReloadWorkspace(ws))

	// 6e. Create heartbeat executor and ticker
	var heartbeatTick <-chan time.Time
	var hb agent.HeartbeatExecutor
	if cfg.HeartbeatInterval.Duration > 0 {
		hb = heartbeat.NewExecutor(llmClient, sender, mem, cfg.TelegramAllowedIDs)
		heartbeatTicker := time.NewTicker(cfg.HeartbeatInterval.Duration)
		defer heartbeatTicker.Stop()
		heartbeatTick = heartbeatTicker.C
		slog.Info("heartbeat enabled",
			"component", "cmd",
			"operation", "run",
			"interval", cfg.HeartbeatInterval.Duration,
		)
	}

	// 6f. Create sub-agent result channel and runner for event loop integration.
	subAgentResults := make(chan subagent.SubAgentResult, 1)
	runner := subagent.NewRunner()

	// 6g. Determine binary path for sub-agent subprocess launch.
	binaryPath, err := osExecutable()
	if err != nil {
		slog.Error("failed to determine binary path",
			"component", "cmd",
			"operation", "run",
			"error", err,
		)
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	// 6h. Register spawn_agent tool.
	agentsDir := filepath.Join(cfg.Workspace, "agents")
	registry.Register(tool.NewSpawnAgent(tool.SpawnAgentDeps{
		Runner:          runner,
		ParentWorkspace: ws,
		ResultCh:        subAgentResults,
		BinaryPath:      binaryPath,
		ConfigPath:      defaultConfigPath,
		VaultPath:       defaultVaultPath,
		Timeout:         cfg.SubAgentTimeout.Duration,
		AgentsDir:       agentsDir,
	}))

	// 7. Create agent
	ag := newAgent(agent.NewAgentConfig{
		Workspace:       ws,
		LLM:             llmClient,
		Sender:          sender,
		Memory:          mem,
		MemorySearcher:  mem,
		ToolExecutor:    registry,
		FileChanges:     fileChanges,
		HeartbeatTick:   heartbeatTick,
		Heartbeat:       hb,
		Transcriber:     audioClient,
		VoiceDownloader: tgClient,
		SubAgentResults: subAgentResults,
		OwnerIDs:        cfg.TelegramAllowedIDs,
	})

	// 8. Signal handling
	ctx, stop := signalContext()
	defer stop()

	// 9. Start watcher goroutine with WaitGroup tracking
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		w.Run(ctx, fileChanges)
	}()

	// 10. Start poller goroutine with WaitGroup tracking
	messages := make(chan telegram.TelegramMessage, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		runPollerFn(ctx, poller, messages)
	}()

	// 11. Run event loop (blocks until ctx cancelled)
	slog.Info("agent started",
		"component", "cmd",
		"operation", "run",
		"workspace", cfg.Workspace,
	)
	fmt.Fprintln(stderr, "Agent started. Press Ctrl+C to stop.")
	if err := ag.Run(ctx, messages); err != nil {
		slog.Error("agent exited with error",
			"component", "cmd",
			"operation", "run",
			"error", err,
		)
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	// === SHUTDOWN SEQUENCE ===
	shutdownStart := time.Now()
	slog.Info("shutdown initiated", "component", "pureclaw", "operation", "shutdown")

	// Create shutdown deadline context (30 seconds per NFR18).
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	// 1. Write final memory entry.
	if err := mem.Write(shutdownCtx, "agent", "Shutdown initiated (signal received)"); err != nil {
		slog.Warn("failed to write shutdown memory entry",
			"component", "pureclaw", "operation", "shutdown", "error", err)
	}

	// 2. Wait for active sub-agent to complete.
	if runner.IsActive() {
		slog.Info("waiting for active sub-agent",
			"component", "pureclaw", "operation", "shutdown")
		if err := runner.WaitForCompletion(shutdownCtx); err != nil {
			slog.Warn("sub-agent wait timed out during shutdown",
				"component", "pureclaw", "operation", "shutdown", "error", err)
		}
	}

	// 3. Wait for background goroutines (poller + watcher) to exit.
	goroutineDone := make(chan struct{})
	go func() {
		wg.Wait()
		close(goroutineDone)
	}()

	select {
	case <-goroutineDone:
		slog.Info("all goroutines stopped", "component", "pureclaw", "operation", "shutdown")
	case <-shutdownCtx.Done():
		slog.Warn("goroutine shutdown timed out", "component", "pureclaw", "operation", "shutdown")
	}

	slog.Info("shutdown complete",
		"component", "pureclaw", "operation", "shutdown",
		"duration", time.Since(shutdownStart))
	fmt.Fprintln(stderr, "Agent stopped.")
	return 0
}
