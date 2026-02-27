package main

import (
	"bufio"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/edouard/pureclaw/internal/config"
	"github.com/edouard/pureclaw/internal/platform"
	"github.com/edouard/pureclaw/internal/vault"
)

const (
	defaultConfigPath    = "config.json"
	defaultWorkspacePath = "./workspace"
)

// Replaceable for testing error paths.
var (
	vaultCreate = vault.Create
	configSave  = config.Save
)

const defaultAgentMD = `# Agent Identity

## Name

pureclaw

## Role

Personal AI assistant running on local hardware.

## Environment

_This section will be populated automatically on first run._
`

const defaultSoulMD = `# Soul

You are a helpful, concise AI assistant. You respond in the same language as the user.

You prioritize clarity and directness. You avoid unnecessary qualifiers or hedging.

When you don't know something, you say so honestly rather than guessing.
`

const defaultHeartbeatMD = `# Heartbeat Checklist

Check these conditions periodically:

- [ ] System disk usage is below 90%
- [ ] Memory usage is below 80%
- [ ] No unusual error patterns in recent logs
`

// readPrompt prints prompt to w, reads one line from scanner, and returns
// the trimmed value. If empty and defaultVal is set, returns defaultVal.
// If empty and no default, returns an error.
func readPrompt(scanner *bufio.Scanner, prompt string, defaultVal string, w io.Writer) (string, error) {
	fmt.Fprint(w, prompt)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return "", fmt.Errorf("init: reading input: %w", err)
		}
		return "", fmt.Errorf("init: reading input: unexpected end of input")
	}
	val := strings.TrimSpace(scanner.Text())
	if val == "" {
		if defaultVal != "" {
			return defaultVal, nil
		}
		return "", fmt.Errorf("init: required value not provided")
	}
	return val, nil
}

// parseAllowedIDs parses comma-separated integer IDs.
func parseAllowedIDs(input string) ([]int64, error) {
	parts := strings.Split(input, ",")
	ids := make([]int64, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		id, err := strconv.ParseInt(p, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("init: invalid Telegram ID %q: %w", p, err)
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("init: at least one Telegram user ID is required")
	}
	return ids, nil
}

// detectExisting checks if any pureclaw instance files exist.
func detectExisting(configPath, vaultPath, workspacePath string) []string {
	var found []string
	if _, err := os.Stat(configPath); err == nil {
		found = append(found, configPath)
	}
	if _, err := os.Stat(vaultPath); err == nil {
		found = append(found, vaultPath)
	}
	if _, err := os.Stat(workspacePath); err == nil {
		found = append(found, workspacePath+"/")
	}
	return found
}

// scaffoldWorkspace creates workspace directories and default files.
func scaffoldWorkspace(workspacePath string) error {
	dirs := []string{
		workspacePath,
		filepath.Join(workspacePath, "skills"),
		filepath.Join(workspacePath, "memory"),
		filepath.Join(workspacePath, "agents"),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("init: create directory %s: %w", dir, err)
		}
	}

	files := []struct {
		name    string
		content string
	}{
		{"AGENT.md", defaultAgentMD},
		{"SOUL.md", defaultSoulMD},
		{"HEARTBEAT.md", defaultHeartbeatMD},
	}
	for _, f := range files {
		path := filepath.Join(workspacePath, f.name)
		if err := platform.AtomicWrite(path, []byte(f.content), 0644); err != nil {
			return fmt.Errorf("init: write %s: %w", f.name, err)
		}
	}
	return nil
}

// runInit implements the interactive init wizard.
func runInit(stdin io.Reader, stdout, stderr io.Writer) int {
	slog.Info("wizard started", "component", "init", "operation", "start")

	scanner := bufio.NewScanner(stdin)

	// Overwrite detection
	existing := detectExisting(defaultConfigPath, defaultVaultPath, defaultWorkspacePath)
	if len(existing) > 0 {
		slog.Warn("existing instance detected", "component", "init", "operation", "overwrite_check", "files", strings.Join(existing, ", "))
		fmt.Fprintln(stderr, "Warning: existing pureclaw instance detected.")
		fmt.Fprintf(stderr, "  Found: %s\n", strings.Join(existing, ", "))
		fmt.Fprint(stderr, "Overwrite? This will destroy existing data. (y/N): ")
		if !scanner.Scan() {
			fmt.Fprintln(stderr, "Error: unexpected end of input")
			return 1
		}
		answer := strings.TrimSpace(scanner.Text())
		if answer != "y" && answer != "Y" {
			slog.Info("overwrite declined", "component", "init", "operation", "overwrite_check")
			fmt.Fprintln(stderr, "Aborted.")
			return 1
		}
		slog.Info("overwrite confirmed", "component", "init", "operation", "overwrite_check")
	}

	// Prompt for inputs
	mistralKey, err := readPrompt(scanner, "Mistral API key: ", "", stderr)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	telegramToken, err := readPrompt(scanner, "Telegram bot token: ", "", stderr)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	idsInput, err := readPrompt(scanner, "Allowed Telegram user IDs (comma-separated): ", "", stderr)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	parsedIDs, err := parseAllowedIDs(idsInput)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	passphrase, err := readPrompt(scanner, "Vault passphrase: ", "", stderr)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	heartbeatStr, err := readPrompt(scanner, "Heartbeat interval (default 30m): ", "30m", stderr)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	heartbeatDur, err := time.ParseDuration(heartbeatStr)
	if err != nil {
		fmt.Fprintf(stderr, "Error: invalid duration %q: %v\n", heartbeatStr, err)
		return 1
	}

	fmt.Fprintln(stderr, "")
	fmt.Fprintln(stderr, "Creating pureclaw instance...")

	// Track created files for cleanup on partial failure.
	var createdFiles []string
	cleanup := func() {
		for _, f := range createdFiles {
			os.Remove(f)
		}
	}

	// Create vault
	salt, err := generateSalt()
	if err != nil {
		fmt.Fprintf(stderr, "Error: init: generate salt: %v\n", err)
		return 1
	}
	key := vault.DeriveKey(passphrase, salt)
	v, err := vaultCreate(key, salt, defaultVaultPath)
	if err != nil {
		fmt.Fprintf(stderr, "Error: init: create vault: %v\n", err)
		return 1
	}
	createdFiles = append(createdFiles, defaultVaultPath)
	for _, secret := range []struct{ key, value string }{
		{"mistral_api_key", mistralKey},
		{"telegram_bot_token", telegramToken},
	} {
		if err := v.Set(secret.key, secret.value); err != nil {
			cleanup()
			fmt.Fprintf(stderr, "Error: init: store %s: %v\n", secret.key, err)
			return 1
		}
	}
	slog.Info("vault created", "component", "init", "operation", "vault_create")
	fmt.Fprintln(stderr, "  ✓ Vault created with secrets")

	// Create config
	cfg := &config.Config{
		Workspace:          defaultWorkspacePath,
		ModelText:          "mistral-large-latest",
		ModelAudio:         "voxtral-mini-latest",
		TelegramAllowedIDs: parsedIDs,
		HeartbeatInterval:  config.Duration{Duration: heartbeatDur},
		SubAgentTimeout:    config.Duration{Duration: 5 * time.Minute},
	}
	if err := configSave(cfg, defaultConfigPath); err != nil {
		cleanup()
		fmt.Fprintf(stderr, "Error: init: save config: %v\n", err)
		return 1
	}
	createdFiles = append(createdFiles, defaultConfigPath)
	slog.Info("config saved", "component", "init", "operation", "config_save")
	fmt.Fprintln(stderr, "  ✓ Configuration saved")

	// Scaffold workspace
	if err := scaffoldWorkspace(defaultWorkspacePath); err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	slog.Info("workspace scaffolded", "component", "init", "operation", "scaffold", "path", defaultWorkspacePath)
	fmt.Fprintln(stderr, "  ✓ Workspace scaffolded")
	fmt.Fprintf(stderr, "    - %s/AGENT.md\n", defaultWorkspacePath)
	fmt.Fprintf(stderr, "    - %s/SOUL.md\n", defaultWorkspacePath)
	fmt.Fprintf(stderr, "    - %s/HEARTBEAT.md\n", defaultWorkspacePath)
	fmt.Fprintf(stderr, "    - %s/skills/\n", defaultWorkspacePath)
	fmt.Fprintf(stderr, "    - %s/memory/\n", defaultWorkspacePath)
	fmt.Fprintf(stderr, "    - %s/agents/\n", defaultWorkspacePath)

	fmt.Fprintln(stderr, "")
	fmt.Fprintln(stderr, "pureclaw is ready! Run 'pureclaw run' to start.")
	return 0
}
