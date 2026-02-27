package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
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
	origNewTGClient := newTGClient
	origNewPoller := newPoller
	origNewSender := newSender
	origNewMemory := newMemory
	origNewAgent := newAgent
	origSignalContext := signalContext
	origRunPollerFn := runPollerFn
	t.Cleanup(func() {
		configLoad = origConfigLoad
		vaultLoadSalt = origVaultLoadSalt
		vaultDeriveKey = origVaultDeriveKey
		vaultOpenFn = origVaultOpenFn
		workspaceLoad = origWorkspaceLoad
		newLLMClient = origNewLLMClient
		newTGClient = origNewTGClient
		newPoller = origNewPoller
		newSender = origNewSender
		newMemory = origNewMemory
		newAgent = origNewAgent
		signalContext = origSignalContext
		runPollerFn = origRunPollerFn
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

	// Replace poller goroutine with a no-op — avoid real HTTP calls.
	runPollerFn = func(ctx context.Context, p *telegram.Poller, ch chan<- telegram.TelegramMessage) {
		// Don't start real poller — context will cancel and agent will exit.
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
