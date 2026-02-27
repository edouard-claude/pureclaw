package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/edouard/pureclaw/internal/config"
	"github.com/edouard/pureclaw/internal/vault"
)

func TestRunInit_success(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	input := "sk-abc123\n12345:ABC-DEF\n123456789\nmy-passphrase\n30m\n"
	var stdout, stderr bytes.Buffer
	code := runInit(strings.NewReader(input), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, stderr.String())
	}

	// Verify vault.enc exists and contains secrets
	salt, err := vault.LoadSalt(filepath.Join(dir, "vault.enc"))
	if err != nil {
		t.Fatalf("load salt: %v", err)
	}
	key := vault.DeriveKey("my-passphrase", salt)
	v, err := vault.Open(key, filepath.Join(dir, "vault.enc"))
	if err != nil {
		t.Fatalf("open vault: %v", err)
	}
	mistralKey, err := v.Get("mistral_api_key")
	if err != nil {
		t.Fatalf("get mistral_api_key: %v", err)
	}
	if mistralKey != "sk-abc123" {
		t.Fatalf("mistral_api_key = %q, want %q", mistralKey, "sk-abc123")
	}
	telegramToken, err := v.Get("telegram_bot_token")
	if err != nil {
		t.Fatalf("get telegram_bot_token: %v", err)
	}
	if telegramToken != "12345:ABC-DEF" {
		t.Fatalf("telegram_bot_token = %q, want %q", telegramToken, "12345:ABC-DEF")
	}

	// Verify config.json
	cfg, err := config.Load(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Workspace != "./workspace" {
		t.Fatalf("workspace = %q, want %q", cfg.Workspace, "./workspace")
	}
	if cfg.ModelText != "mistral-large-latest" {
		t.Fatalf("model_text = %q, want %q", cfg.ModelText, "mistral-large-latest")
	}
	if cfg.ModelAudio != "voxtral-mini-latest" {
		t.Fatalf("model_audio = %q, want %q", cfg.ModelAudio, "voxtral-mini-latest")
	}
	if len(cfg.TelegramAllowedIDs) != 1 || cfg.TelegramAllowedIDs[0] != 123456789 {
		t.Fatalf("telegram_allowed_ids = %v, want [123456789]", cfg.TelegramAllowedIDs)
	}
	if cfg.HeartbeatInterval.Duration.String() != "30m0s" {
		t.Fatalf("heartbeat_interval = %v, want 30m0s", cfg.HeartbeatInterval.Duration)
	}
	if cfg.SubAgentTimeout.Duration.String() != "5m0s" {
		t.Fatalf("sub_agent_timeout = %v, want 5m0s", cfg.SubAgentTimeout.Duration)
	}

	// Verify workspace files
	workDir := filepath.Join(dir, "workspace")
	for _, sub := range []string{"skills", "memory", "agents"} {
		info, err := os.Stat(filepath.Join(workDir, sub))
		if err != nil {
			t.Fatalf("workspace/%s not found: %v", sub, err)
		}
		if !info.IsDir() {
			t.Fatalf("workspace/%s is not a directory", sub)
		}
	}
	for _, f := range []struct {
		name    string
		content string
	}{
		{"AGENT.md", defaultAgentMD},
		{"SOUL.md", defaultSoulMD},
		{"HEARTBEAT.md", defaultHeartbeatMD},
	} {
		data, err := os.ReadFile(filepath.Join(workDir, f.name))
		if err != nil {
			t.Fatalf("read %s: %v", f.name, err)
		}
		if string(data) != f.content {
			t.Fatalf("%s content mismatch:\ngot:  %q\nwant: %q", f.name, string(data), f.content)
		}
	}

	// Verify output messages
	if !strings.Contains(stderr.String(), "pureclaw is ready!") {
		t.Fatalf("expected success message, got %q", stderr.String())
	}
}

func TestRunInit_defaultHeartbeat(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	// Empty line for heartbeat → should use default 30m
	input := "sk-key\nbot-token\n999\npassphrase\n\n"
	var stderr bytes.Buffer
	code := runInit(strings.NewReader(input), io.Discard, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, stderr.String())
	}

	cfg, err := config.Load(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.HeartbeatInterval.Duration.String() != "30m0s" {
		t.Fatalf("heartbeat = %v, want 30m0s", cfg.HeartbeatInterval.Duration)
	}
}

func TestRunInit_multipleIDs(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	input := "sk-key\nbot-token\n111, 222, 333\npassphrase\n1h\n"
	var stderr bytes.Buffer
	code := runInit(strings.NewReader(input), io.Discard, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, stderr.String())
	}

	cfg, err := config.Load(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if len(cfg.TelegramAllowedIDs) != 3 {
		t.Fatalf("expected 3 IDs, got %d", len(cfg.TelegramAllowedIDs))
	}
	want := []int64{111, 222, 333}
	for i, w := range want {
		if cfg.TelegramAllowedIDs[i] != w {
			t.Fatalf("ID[%d] = %d, want %d", i, cfg.TelegramAllowedIDs[i], w)
		}
	}
}

func TestRunInit_invalidIDs(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	input := "sk-key\nbot-token\nabc\npassphrase\n30m\n"
	var stderr bytes.Buffer
	code := runInit(strings.NewReader(input), io.Discard, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "invalid Telegram ID") {
		t.Fatalf("expected invalid ID error, got %q", stderr.String())
	}
}

func TestRunInit_emptyIDs(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	input := "sk-key\nbot-token\n  ,  , \npassphrase\n30m\n"
	var stderr bytes.Buffer
	code := runInit(strings.NewReader(input), io.Discard, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "at least one") {
		t.Fatalf("expected at least one ID error, got %q", stderr.String())
	}
}

func TestRunInit_invalidDuration(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	input := "sk-key\nbot-token\n123\npassphrase\nnot-a-duration\n"
	var stderr bytes.Buffer
	code := runInit(strings.NewReader(input), io.Discard, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "invalid duration") {
		t.Fatalf("expected invalid duration error, got %q", stderr.String())
	}
}

func TestRunInit_overwriteDecline(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	// Pre-create config.json
	os.WriteFile(filepath.Join(dir, "config.json"), []byte("{}"), 0644)

	input := "N\n"
	var stderr bytes.Buffer
	code := runInit(strings.NewReader(input), io.Discard, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "Aborted") {
		t.Fatalf("expected Aborted message, got %q", stderr.String())
	}
}

func TestRunInit_overwriteEmptyDecline(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	// Pre-create vault.enc
	os.WriteFile(filepath.Join(dir, "vault.enc"), []byte("{}"), 0600)

	input := "\n"
	var stderr bytes.Buffer
	code := runInit(strings.NewReader(input), io.Discard, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "Aborted") {
		t.Fatalf("expected Aborted message, got %q", stderr.String())
	}
}

func TestRunInit_overwriteConfirm(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	// Pre-create files
	os.WriteFile(filepath.Join(dir, "config.json"), []byte("{}"), 0644)
	os.WriteFile(filepath.Join(dir, "vault.enc"), []byte("{}"), 0600)
	os.MkdirAll(filepath.Join(dir, "workspace"), 0755)

	input := "y\nsk-key\nbot-token\n123\npassphrase\n30m\n"
	var stderr bytes.Buffer
	code := runInit(strings.NewReader(input), io.Discard, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "pureclaw is ready!") {
		t.Fatalf("expected success, got %q", stderr.String())
	}
}

func TestRunInit_overwriteConfirmUpperY(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	os.WriteFile(filepath.Join(dir, "config.json"), []byte("{}"), 0644)

	input := "Y\nsk-key\nbot-token\n123\npassphrase\n30m\n"
	var stderr bytes.Buffer
	code := runInit(strings.NewReader(input), io.Discard, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, stderr.String())
	}
}

func TestRunInit_overwriteEOF(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	os.WriteFile(filepath.Join(dir, "config.json"), []byte("{}"), 0644)

	// EOF when asking for overwrite confirmation
	var stderr bytes.Buffer
	code := runInit(strings.NewReader(""), io.Discard, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
}

func TestRunInit_emptyMistralKey(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	// Empty line for mistral key — required
	input := "\n"
	var stderr bytes.Buffer
	code := runInit(strings.NewReader(input), io.Discard, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "required") {
		t.Fatalf("expected required error, got %q", stderr.String())
	}
}

func TestRunInit_emptyTelegramToken(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	input := "sk-key\n\n"
	var stderr bytes.Buffer
	code := runInit(strings.NewReader(input), io.Discard, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
}

func TestRunInit_emptyTelegramIDs(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	input := "sk-key\nbot-token\n\n"
	var stderr bytes.Buffer
	code := runInit(strings.NewReader(input), io.Discard, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
}

func TestRunInit_emptyPassphrase(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	input := "sk-key\nbot-token\n123\n"
	var stderr bytes.Buffer
	code := runInit(strings.NewReader(input), io.Discard, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
}

func TestRunInit_generateSaltError(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	original := generateSalt
	defer func() { generateSalt = original }()
	generateSalt = func() ([]byte, error) {
		return nil, errors.New("salt error")
	}

	input := "sk-key\nbot-token\n123\npass\n30m\n"
	var stderr bytes.Buffer
	code := runInit(strings.NewReader(input), io.Discard, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "salt") {
		t.Fatalf("expected salt error, got %q", stderr.String())
	}
}

func TestRunInit_scaffoldError(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	// Create a file where workspace dir should be, blocking MkdirAll.
	// This also triggers overwrite detection (workspace exists), so prefix input with "y\n".
	os.WriteFile(filepath.Join(dir, "workspace"), []byte("blocker"), 0644)

	input := "y\nsk-key\nbot-token\n123\npassphrase\n30m\n"
	var stderr bytes.Buffer
	code := runInit(strings.NewReader(input), io.Discard, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "create directory") {
		t.Fatalf("expected scaffold error, got %q", stderr.String())
	}
}

func TestParseAllowedIDs(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []int64
		wantErr bool
	}{
		{"single", "123", []int64{123}, false},
		{"multiple", "1,2,3", []int64{1, 2, 3}, false},
		{"spaces", " 1 , 2 , 3 ", []int64{1, 2, 3}, false},
		{"trailing comma", "1,2,", []int64{1, 2}, false},
		{"invalid", "abc", nil, true},
		{"mixed", "1,abc", nil, true},
		{"empty", "", nil, true},
		{"only spaces", "  ,  ", nil, true},
		{"negative", "-123", []int64{-123}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseAllowedIDs(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("got[%d] = %d, want %d", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestDetectExisting(t *testing.T) {
	t.Run("nothing exists", func(t *testing.T) {
		dir := t.TempDir()
		found := detectExisting(
			filepath.Join(dir, "config.json"),
			filepath.Join(dir, "vault.enc"),
			filepath.Join(dir, "workspace"),
		)
		if len(found) != 0 {
			t.Fatalf("expected empty, got %v", found)
		}
	})

	t.Run("all exist", func(t *testing.T) {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "config.json"), []byte("{}"), 0644)
		os.WriteFile(filepath.Join(dir, "vault.enc"), []byte("{}"), 0600)
		os.MkdirAll(filepath.Join(dir, "workspace"), 0755)
		found := detectExisting(
			filepath.Join(dir, "config.json"),
			filepath.Join(dir, "vault.enc"),
			filepath.Join(dir, "workspace"),
		)
		if len(found) != 3 {
			t.Fatalf("expected 3 found, got %v", found)
		}
	})

	t.Run("partial", func(t *testing.T) {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "config.json"), []byte("{}"), 0644)
		found := detectExisting(
			filepath.Join(dir, "config.json"),
			filepath.Join(dir, "vault.enc"),
			filepath.Join(dir, "workspace"),
		)
		if len(found) != 1 {
			t.Fatalf("expected 1 found, got %v", found)
		}
	})
}

func TestReadPrompt(t *testing.T) {
	t.Run("value provided", func(t *testing.T) {
		var w bytes.Buffer
		scanner := bufio.NewScanner(strings.NewReader("hello\n"))
		got, err := readPrompt(scanner, "Enter: ", "", &w)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "hello" {
			t.Fatalf("got %q, want %q", got, "hello")
		}
		if !strings.Contains(w.String(), "Enter:") {
			t.Fatalf("expected prompt, got %q", w.String())
		}
	})

	t.Run("empty with default", func(t *testing.T) {
		var w bytes.Buffer
		scanner := bufio.NewScanner(strings.NewReader("\n"))
		got, err := readPrompt(scanner, "Enter: ", "default", &w)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "default" {
			t.Fatalf("got %q, want %q", got, "default")
		}
	})

	t.Run("empty no default", func(t *testing.T) {
		var w bytes.Buffer
		scanner := bufio.NewScanner(strings.NewReader("\n"))
		_, err := readPrompt(scanner, "Enter: ", "", &w)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("EOF", func(t *testing.T) {
		var w bytes.Buffer
		scanner := bufio.NewScanner(strings.NewReader(""))
		_, err := readPrompt(scanner, "Enter: ", "", &w)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("scanner error", func(t *testing.T) {
		var w bytes.Buffer
		scanner := bufio.NewScanner(errReader{err: os.ErrPermission})
		_, err := readPrompt(scanner, "Enter: ", "", &w)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestScaffoldWorkspace(t *testing.T) {
	dir := t.TempDir()
	wsPath := filepath.Join(dir, "workspace")

	if err := scaffoldWorkspace(wsPath); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, sub := range []string{"skills", "memory", "agents"} {
		info, err := os.Stat(filepath.Join(wsPath, sub))
		if err != nil {
			t.Fatalf("%s not found: %v", sub, err)
		}
		if !info.IsDir() {
			t.Fatalf("%s is not a directory", sub)
		}
	}

	for _, name := range []string{"AGENT.md", "SOUL.md", "HEARTBEAT.md"} {
		if _, err := os.Stat(filepath.Join(wsPath, name)); err != nil {
			t.Fatalf("%s not found: %v", name, err)
		}
	}
}

func TestRunInit_vaultCreateError(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	original := vaultCreate
	defer func() { vaultCreate = original }()
	vaultCreate = func(key, salt []byte, path string) (*vault.Vault, error) {
		return nil, errors.New("create failed")
	}

	input := "sk-key\nbot-token\n123\npassphrase\n30m\n"
	var stderr bytes.Buffer
	code := runInit(strings.NewReader(input), io.Discard, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "create vault") {
		t.Fatalf("expected vault create error, got %q", stderr.String())
	}
}

func TestRunInit_vaultSetMistralError(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	original := vaultCreate
	defer func() { vaultCreate = original }()
	vaultCreate = func(key, salt []byte, path string) (*vault.Vault, error) {
		v, err := vault.Create(key, salt, path)
		if err != nil {
			return nil, err
		}
		// Make dir read-only so v.Set's save fails
		os.Chmod(dir, 0555)
		t.Cleanup(func() { os.Chmod(dir, 0755) })
		return v, nil
	}

	input := "sk-key\nbot-token\n123\npassphrase\n30m\n"
	var stderr bytes.Buffer
	code := runInit(strings.NewReader(input), io.Discard, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "store mistral_api_key") {
		t.Fatalf("expected mistral key error, got %q", stderr.String())
	}
}

func TestRunInit_vaultSetSecondSecretError(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	// Create vault in a writable subdir, then chmod it after pre-setting mistral_api_key.
	// This triggers a write failure on the next v.Set call, verifying the loop error path.
	subdir := filepath.Join(dir, "vaultdir")
	os.MkdirAll(subdir, 0755)

	original := vaultCreate
	defer func() { vaultCreate = original }()
	vaultCreate = func(key, salt []byte, path string) (*vault.Vault, error) {
		// Create vault in subdir instead of CWD
		subpath := filepath.Join(subdir, "vault.enc")
		v, err := vault.Create(key, salt, subpath)
		if err != nil {
			return nil, err
		}
		// Pre-store mistral key, then block writes
		if err := v.Set("mistral_api_key", "pre-set"); err != nil {
			return nil, err
		}
		os.Chmod(subdir, 0555)
		t.Cleanup(func() { os.Chmod(subdir, 0755) })
		return v, nil
	}

	input := "sk-key\nbot-token\n123\npassphrase\n30m\n"
	var stderr bytes.Buffer
	code := runInit(strings.NewReader(input), io.Discard, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "store") {
		t.Fatalf("expected store error, got %q", stderr.String())
	}
}

func TestRunInit_configSaveError(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	original := configSave
	defer func() { configSave = original }()
	configSave = func(cfg *config.Config, path string) error {
		return errors.New("save failed")
	}

	input := "sk-key\nbot-token\n123\npassphrase\n30m\n"
	var stderr bytes.Buffer
	code := runInit(strings.NewReader(input), io.Discard, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "save config") {
		t.Fatalf("expected config save error, got %q", stderr.String())
	}

	// Verify cleanup: vault.enc should have been removed
	if _, err := os.Stat(filepath.Join(dir, "vault.enc")); !os.IsNotExist(err) {
		t.Fatalf("expected vault.enc to be cleaned up after config save failure")
	}
}

func TestRunInit_heartbeatEOF(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	// 4 lines: mistral, telegram, IDs, passphrase. The 5th readPrompt (heartbeat)
	// gets EOF — scanner.Scan() returns false, triggering "unexpected end of input".
	input := "sk-key\nbot-token\n123\npassphrase\n"

	var stderr bytes.Buffer
	code := runInit(strings.NewReader(input), io.Discard, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
}

func TestScaffoldWorkspace_atomicWriteError(t *testing.T) {
	dir := t.TempDir()
	wsPath := filepath.Join(dir, "workspace")
	os.MkdirAll(wsPath, 0755)

	// Create a directory where AGENT.md should go to block AtomicWrite
	os.MkdirAll(filepath.Join(wsPath, "AGENT.md"), 0755)

	err := scaffoldWorkspace(wsPath)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "AGENT.md") {
		t.Fatalf("expected AGENT.md write error, got %q", err.Error())
	}
}

func TestRunInit_configJsonContent(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	input := "sk-key\nbot-token\n111,222\npass\n1h\n"
	code := runInit(strings.NewReader(input), io.Discard, io.Discard)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	// Read raw JSON and verify structure
	data, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	expectedKeys := []string{"workspace", "model_text", "model_audio", "telegram_allowed_ids", "heartbeat_interval", "sub_agent_timeout"}
	for _, k := range expectedKeys {
		if _, ok := raw[k]; !ok {
			t.Fatalf("missing key %q in config.json", k)
		}
	}
}
