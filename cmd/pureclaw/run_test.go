package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/edouard/pureclaw/internal/agent"
	"github.com/edouard/pureclaw/internal/config"
	"github.com/edouard/pureclaw/internal/llm"
	"github.com/edouard/pureclaw/internal/memory"
	"github.com/edouard/pureclaw/internal/telegram"
	"github.com/edouard/pureclaw/internal/vault"
)

// saveRunVars saves the current run.go package-level vars and returns a restore function.
func saveRunVars(t *testing.T) {
	t.Helper()
	origConfigLoad := configLoad
	origVaultLoadSalt := vaultLoadSalt
	origVaultDeriveKey := vaultDeriveKey
	origVaultOpenFn := vaultOpenFn
	origWorkspaceLoad := workspaceLoad
	origNewLLMClient := newLLMClient
	origNewAudioClient := newAudioClient
	origNewTGClient := newTGClient
	origNewPoller := newPoller
	origNewSender := newSender
	origNewMemory := newMemory
	origNewAgent := newAgent
	origSignalContext := signalContext
	origRunPollerFn := runPollerFn
	origOsExecutable := osExecutable
	t.Cleanup(func() {
		configLoad = origConfigLoad
		vaultLoadSalt = origVaultLoadSalt
		vaultDeriveKey = origVaultDeriveKey
		vaultOpenFn = origVaultOpenFn
		workspaceLoad = origWorkspaceLoad
		newLLMClient = origNewLLMClient
		newAudioClient = origNewAudioClient
		newTGClient = origNewTGClient
		newPoller = origNewPoller
		newSender = origNewSender
		newMemory = origNewMemory
		newAgent = origNewAgent
		signalContext = origSignalContext
		runPollerFn = origRunPollerFn
		osExecutable = origOsExecutable
	})
}

// stubLLM implements agent.LLMClient for testing run.go.
type stubLLM struct{}

func (s *stubLLM) ChatCompletionWithRetry(ctx context.Context, messages []llm.Message, tools []llm.Tool) (*llm.ChatResponse, error) {
	return &llm.ChatResponse{
		Choices: []llm.Choice{{
			Message:      llm.Message{Content: `{"type":"noop","content":""}`},
			FinishReason: "stop",
		}},
	}, nil
}

// stubSender implements agent.Sender for testing run.go.
type stubSender struct{}

func (s *stubSender) Send(ctx context.Context, chatID int64, text string) error {
	return nil
}

// fakeVault creates a real test vault in dir.
func fakeVault(t *testing.T, dir string) {
	t.Helper()
	createTestVault(t, dir, "test-pass", map[string]string{
		"mistral_api_key":    "sk-test",
		"telegram_bot_token": "bot-test",
	})
}

// setupHappyPath configures all run.go vars for a successful execution.
func setupHappyPath(t *testing.T, dir string) {
	t.Helper()
	saveRunVars(t)

	// Create real config and vault files.
	cfg := &config.Config{
		Workspace:          dir + "/workspace",
		ModelText:           "test-model",
		TelegramAllowedIDs: []int64{123},
	}
	if err := config.Save(cfg, dir+"/config.json"); err != nil {
		t.Fatalf("save config: %v", err)
	}
	fakeVault(t, dir)

	// Create workspace files.
	wsDir := dir + "/workspace"
	os.MkdirAll(wsDir, 0755)
	os.WriteFile(wsDir+"/AGENT.md", []byte("# Agent"), 0644)
	os.WriteFile(wsDir+"/SOUL.md", []byte("# Soul"), 0644)

	// Replace clients with stubs that don't make network calls.
	newLLMClient = func(apiKey, model string) agent.LLMClient { return &stubLLM{} }
	newAudioClient = func(apiKey, model string) agent.Transcriber { return llm.NewClient(apiKey, model) }
	newSender = func(client *telegram.Client) agent.Sender { return &stubSender{} }
	newMemory = func(root string) *memory.Memory { return memory.New(root) }
}

func TestRunAgent_ConfigLoadError(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	saveRunVars(t)

	configLoad = func(path string) (*config.Config, error) {
		return nil, errors.New("config not found")
	}

	var stderr bytes.Buffer
	code := runAgent(strings.NewReader(""), io.Discard, &stderr)
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
	if !strings.Contains(stderr.String(), "config not found") {
		t.Errorf("expected error in stderr, got %q", stderr.String())
	}
}

func TestRunAgent_EmptyPassphrase(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	saveRunVars(t)

	configLoad = func(path string) (*config.Config, error) {
		return &config.Config{}, nil
	}

	var stderr bytes.Buffer
	code := runAgent(strings.NewReader("\n"), io.Discard, &stderr)
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
	if !strings.Contains(stderr.String(), "passphrase cannot be empty") {
		t.Errorf("expected passphrase error, got %q", stderr.String())
	}
}

func TestRunAgent_VaultLoadSaltError(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	saveRunVars(t)

	configLoad = func(path string) (*config.Config, error) {
		return &config.Config{}, nil
	}
	vaultLoadSalt = func(path string) ([]byte, error) {
		return nil, errors.New("salt not found")
	}

	var stderr bytes.Buffer
	code := runAgent(strings.NewReader("mypass\n"), io.Discard, &stderr)
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
	if !strings.Contains(stderr.String(), "salt not found") {
		t.Errorf("expected salt error, got %q", stderr.String())
	}
}

func TestRunAgent_VaultOpenError(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	saveRunVars(t)

	configLoad = func(path string) (*config.Config, error) {
		return &config.Config{}, nil
	}
	vaultLoadSalt = func(path string) ([]byte, error) {
		return []byte("salt"), nil
	}
	vaultOpenFn = func(derivedKey []byte, path string) (*vault.Vault, error) {
		return nil, errors.New("bad vault")
	}

	var stderr bytes.Buffer
	code := runAgent(strings.NewReader("mypass\n"), io.Discard, &stderr)
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
	if !strings.Contains(stderr.String(), "bad vault") {
		t.Errorf("expected vault error, got %q", stderr.String())
	}
}

func TestRunAgent_MistralKeyMissing(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	saveRunVars(t)

	// Create vault with only telegram_bot_token, no mistral_api_key.
	createTestVault(t, dir, "test-pass", map[string]string{
		"telegram_bot_token": "bot-test",
	})

	configLoad = func(path string) (*config.Config, error) {
		return &config.Config{}, nil
	}

	var stderr bytes.Buffer
	code := runAgent(strings.NewReader("test-pass\n"), io.Discard, &stderr)
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
	if !strings.Contains(stderr.String(), "Error:") {
		t.Errorf("expected error about missing key, got %q", stderr.String())
	}
}

func TestRunAgent_TelegramTokenMissing(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	saveRunVars(t)

	// Create vault with only mistral_api_key, no telegram_bot_token.
	createTestVault(t, dir, "test-pass", map[string]string{
		"mistral_api_key": "sk-test",
	})

	configLoad = func(path string) (*config.Config, error) {
		return &config.Config{}, nil
	}

	var stderr bytes.Buffer
	code := runAgent(strings.NewReader("test-pass\n"), io.Discard, &stderr)
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
	if !strings.Contains(stderr.String(), "Error:") {
		t.Errorf("expected error about missing token, got %q", stderr.String())
	}
}

func TestRunAgent_WorkspaceLoadError(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	saveRunVars(t)

	fakeVault(t, dir)

	configLoad = func(path string) (*config.Config, error) {
		return &config.Config{Workspace: dir + "/nonexistent"}, nil
	}

	var stderr bytes.Buffer
	code := runAgent(strings.NewReader("test-pass\n"), io.Discard, &stderr)
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
	if !strings.Contains(stderr.String(), "Error:") {
		t.Errorf("expected workspace error, got %q", stderr.String())
	}
}

func TestRunAgent_FullStartup(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	setupHappyPath(t, dir)

	// Replace signal context with one that cancels after a short delay.
	signalContext = func() (context.Context, context.CancelFunc) {
		return context.WithTimeout(context.Background(), 100*time.Millisecond)
	}

	// Replace poller with a blocking mock that respects ctx.Done().
	runPollerFn = func(ctx context.Context, p *telegram.Poller, ch chan<- telegram.TelegramMessage) {
		<-ctx.Done()
	}

	var stderr bytes.Buffer
	code := runAgent(strings.NewReader("test-pass\n"), io.Discard, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr: %s", code, stderr.String())
	}

	output := stderr.String()
	if !strings.Contains(output, "Agent started") {
		t.Errorf("expected 'Agent started' in output, got %q", output)
	}
	if !strings.Contains(output, "Agent stopped") {
		t.Errorf("expected 'Agent stopped' in output, got %q", output)
	}
}

func TestRunAgent_GracefulShutdown(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	setupHappyPath(t, dir)

	// Use a cancellable context to simulate SIGTERM.
	ctx, cancel := context.WithCancel(context.Background())
	signalContext = func() (context.Context, context.CancelFunc) {
		return ctx, cancel
	}

	// Poller blocks on ctx.Done() like the real poller.
	runPollerFn = func(ctx context.Context, p *telegram.Poller, ch chan<- telegram.TelegramMessage) {
		<-ctx.Done()
	}

	done := make(chan int, 1)
	go func() {
		var stderr bytes.Buffer
		done <- runAgent(strings.NewReader("test-pass\n"), io.Discard, &stderr)
	}()

	// Give agent time to start, then send "SIGTERM".
	time.Sleep(200 * time.Millisecond)
	cancel()

	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d", code)
		}
	case <-time.After(35 * time.Second):
		t.Fatal("runAgent did not complete within 35 seconds — shutdown may be hanging")
	}
}

func TestRunAgent_ShutdownWritesMemory(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	setupHappyPath(t, dir)

	signalContext = func() (context.Context, context.CancelFunc) {
		return context.WithTimeout(context.Background(), 100*time.Millisecond)
	}

	runPollerFn = func(ctx context.Context, p *telegram.Poller, ch chan<- telegram.TelegramMessage) {
		<-ctx.Done()
	}

	var stderr bytes.Buffer
	code := runAgent(strings.NewReader("test-pass\n"), io.Discard, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr: %s", code, stderr.String())
	}

	// Verify shutdown memory entry was written.
	wsDir := dir + "/workspace"
	memDir := filepath.Join(wsDir, "memory")

	var found bool
	filepath.WalkDir(memDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		if strings.Contains(string(data), "Shutdown initiated (signal received)") {
			found = true
		}
		return nil
	})

	if !found {
		t.Error("expected shutdown memory entry 'Shutdown initiated (signal received)' not found in memory files")
	}
}

func TestRunAgent_ShutdownNoSubAgent(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	setupHappyPath(t, dir)

	signalContext = func() (context.Context, context.CancelFunc) {
		return context.WithTimeout(context.Background(), 100*time.Millisecond)
	}

	runPollerFn = func(ctx context.Context, p *telegram.Poller, ch chan<- telegram.TelegramMessage) {
		<-ctx.Done()
	}

	start := time.Now()
	var stderr bytes.Buffer
	code := runAgent(strings.NewReader("test-pass\n"), io.Discard, &stderr)
	elapsed := time.Since(start)

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr: %s", code, stderr.String())
	}

	// Without an active sub-agent, shutdown should be fast (well under 30s).
	if elapsed > 10*time.Second {
		t.Errorf("shutdown took %v — expected fast exit without active sub-agent", elapsed)
	}
}

func TestRunAgent_ShutdownGracefulCancel(t *testing.T) {
	// Tests the graceful shutdown path when context is cancelled (simulates SIGTERM).
	// No sub-agent is active — the runner.IsActive() == false path is exercised.
	// The active sub-agent shutdown path (runner.WaitForCompletion) is covered by
	// unit tests in internal/subagent/runner_test.go (TestRunner_WaitForCompletion_*).
	dir := t.TempDir()
	chdir(t, dir)
	setupHappyPath(t, dir)

	ctx, cancel := context.WithCancel(context.Background())
	signalContext = func() (context.Context, context.CancelFunc) {
		return ctx, cancel
	}

	runPollerFn = func(ctx context.Context, p *telegram.Poller, ch chan<- telegram.TelegramMessage) {
		<-ctx.Done()
	}

	done := make(chan int, 1)
	go func() {
		var stderr bytes.Buffer
		done <- runAgent(strings.NewReader("test-pass\n"), io.Discard, &stderr)
	}()

	// Give agent time to start, then cancel to trigger shutdown.
	time.Sleep(200 * time.Millisecond)
	cancel()

	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d", code)
		}
	case <-time.After(35 * time.Second):
		t.Fatal("runAgent did not complete within 35 seconds")
	}
}
