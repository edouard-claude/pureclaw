package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os/signal"
	"strings"
	"syscall"

	"github.com/edouard/pureclaw/internal/agent"
	"github.com/edouard/pureclaw/internal/config"
	"github.com/edouard/pureclaw/internal/llm"
	"github.com/edouard/pureclaw/internal/memory"
	"github.com/edouard/pureclaw/internal/telegram"
	"github.com/edouard/pureclaw/internal/tool"
	"github.com/edouard/pureclaw/internal/vault"
	"github.com/edouard/pureclaw/internal/workspace"
)

// Replaceable for testing.
var (
	configLoad    = config.Load
	vaultLoadSalt = vault.LoadSalt
	vaultDeriveKey = vault.DeriveKey
	vaultOpenFn   = vault.Open
	workspaceLoad = workspace.Load
	newLLMClient  = func(apiKey, model string) agent.LLMClient { return llm.NewClient(apiKey, model) }
	newTGClient   = telegram.NewClient
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
		go p.Run(ctx, ch)
	}
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

	// 6. Create clients
	llmClient := newLLMClient(mistralKey, cfg.ModelText)
	tgClient := newTGClient(telegramToken)
	poller := newPoller(tgClient, cfg.TelegramAllowedIDs, 30)
	sender := newSender(tgClient)

	// 6a. Create memory (serves both writer and searcher)
	mem := newMemory(cfg.Workspace)

	// 6b. Create tool registry
	registry := tool.NewRegistry()
	registry.Register(tool.NewReadFile())
	registry.Register(tool.NewWriteFile())
	registry.Register(tool.NewListDir())

	// 7. Create agent
	ag := newAgent(agent.NewAgentConfig{
		Workspace:      ws,
		LLM:            llmClient,
		Sender:         sender,
		Memory:         mem,
		MemorySearcher: mem,
		ToolExecutor:   registry,
	})

	// 8. Signal handling
	ctx, stop := signalContext()
	defer stop()

	// 9. Start poller goroutine
	messages := make(chan telegram.TelegramMessage, 1)
	runPollerFn(ctx, poller, messages)

	// 10. Run event loop (blocks until ctx cancelled)
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

	slog.Info("agent stopped", "component", "cmd", "operation", "run")
	fmt.Fprintln(stderr, "Agent stopped.")
	return 0
}
