package main

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestRun_version(t *testing.T) {
	var stdout bytes.Buffer
	code := run([]string{"pureclaw", "version"}, strings.NewReader(""), &stdout, io.Discard)

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
	output := strings.TrimSpace(stdout.String())
	if output != Version {
		t.Fatalf("expected %q, got %q", Version, output)
	}
}

func TestRun_noArgs(t *testing.T) {
	code := run([]string{"pureclaw"}, strings.NewReader(""), io.Discard, io.Discard)
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
}

func TestRun_unknownCommand(t *testing.T) {
	code := run([]string{"pureclaw", "bogus"}, strings.NewReader(""), io.Discard, io.Discard)
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
}

func TestRun_runDelegation(t *testing.T) {
	// "run" with no config.json returns 1 (config load error).
	dir := t.TempDir()
	chdir(t, dir)
	code := run([]string{"pureclaw", "run"}, strings.NewReader(""), io.Discard, io.Discard)
	if code != 1 {
		t.Fatalf("expected exit code 1 (no config), got %d", code)
	}
}

func TestRun_initDelegation(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	input := "sk-key\nbot-token\n123\npassphrase\n30m\n"
	var stderr bytes.Buffer
	code := run([]string{"pureclaw", "init"}, strings.NewReader(input), io.Discard, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "pureclaw is ready!") {
		t.Fatalf("expected success message, got %q", stderr.String())
	}
}

func TestRun_vaultNoSubcommand(t *testing.T) {
	var stderr bytes.Buffer
	code := run([]string{"pureclaw", "vault"}, strings.NewReader(""), io.Discard, &stderr)
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
	if !strings.Contains(stderr.String(), "Usage:") {
		t.Fatalf("expected vault usage, got %q", stderr.String())
	}
}

func TestRun_vaultSubcommandDelegation(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	var stdout, stderr bytes.Buffer
	input := "test-pass\ntest-value\n"
	code := run([]string{"pureclaw", "vault", "set", "my_key"}, strings.NewReader(input), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "Secret stored: my_key") {
		t.Fatalf("expected confirmation, got %q", stderr.String())
	}
}

func TestParseAgentFlags_WithAgent(t *testing.T) {
	agentPath, configPath, vaultPath, err := parseAgentFlags([]string{"--agent", "/path/to/workspace"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if agentPath != "/path/to/workspace" {
		t.Errorf("agentPath = %q, want %q", agentPath, "/path/to/workspace")
	}
	if configPath != defaultConfigPath {
		t.Errorf("configPath = %q, want %q (default)", configPath, defaultConfigPath)
	}
	if vaultPath != defaultVaultPath {
		t.Errorf("vaultPath = %q, want %q (default)", vaultPath, defaultVaultPath)
	}
}

func TestParseAgentFlags_WithAllFlags(t *testing.T) {
	agentPath, configPath, vaultPath, err := parseAgentFlags([]string{
		"--agent", "/ws", "--config", "/cfg.json", "--vault", "/v.enc",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if agentPath != "/ws" {
		t.Errorf("agentPath = %q, want %q", agentPath, "/ws")
	}
	if configPath != "/cfg.json" {
		t.Errorf("configPath = %q, want %q", configPath, "/cfg.json")
	}
	if vaultPath != "/v.enc" {
		t.Errorf("vaultPath = %q, want %q", vaultPath, "/v.enc")
	}
}

func TestParseAgentFlags_NoFlags(t *testing.T) {
	agentPath, _, _, err := parseAgentFlags(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if agentPath != "" {
		t.Errorf("agentPath = %q, want empty", agentPath)
	}
}

func TestParseAgentFlags_AgentMissingPath(t *testing.T) {
	_, _, _, err := parseAgentFlags([]string{"--agent"})
	if err == nil {
		t.Fatal("expected error for --agent without path")
	}
	if !strings.Contains(err.Error(), "--agent requires") {
		t.Errorf("error = %q, want to contain '--agent requires'", err)
	}
}

func TestParseAgentFlags_ConfigMissingPath(t *testing.T) {
	_, _, _, err := parseAgentFlags([]string{"--agent", "/ws", "--config"})
	if err == nil {
		t.Fatal("expected error for --config without path")
	}
	if !strings.Contains(err.Error(), "--config requires") {
		t.Errorf("error = %q, want to contain '--config requires'", err)
	}
}

func TestParseAgentFlags_VaultMissingPath(t *testing.T) {
	_, _, _, err := parseAgentFlags([]string{"--agent", "/ws", "--vault"})
	if err == nil {
		t.Fatal("expected error for --vault without path")
	}
	if !strings.Contains(err.Error(), "--vault requires") {
		t.Errorf("error = %q, want to contain '--vault requires'", err)
	}
}

func TestRun_RunWithAgentFlag(t *testing.T) {
	// "run --agent /nonexistent" should fail because workspace doesn't exist.
	var stderr bytes.Buffer
	code := run([]string{"pureclaw", "run", "--agent", "/nonexistent/workspace"}, strings.NewReader(""), io.Discard, &stderr)
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
}

func TestRun_RunAgentFlagMissingWorkspace(t *testing.T) {
	var stderr bytes.Buffer
	code := run([]string{"pureclaw", "run", "--agent"}, strings.NewReader(""), io.Discard, &stderr)
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
	if !strings.Contains(stderr.String(), "--agent requires") {
		t.Errorf("expected '--agent requires' error, got %q", stderr.String())
	}
}

// TestMain_subprocess tests the main() function which calls os.Exit.
// Uses the subprocess pattern: re-exec the test binary with a flag.
func TestMain_subprocess(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantCode int
		wantOut  string
	}{
		{"version", []string{"version"}, 0, Version},
		{"no args", nil, 1, ""},
		{"unknown", []string{"bogus"}, 1, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], append([]string{"-test.run=TestHelperMain", "--"}, tt.args...)...)
			cmd.Env = append(os.Environ(), "PURECLAW_TEST_MAIN=1")
			out, err := cmd.CombinedOutput()

			exitCode := 0
			if err != nil {
				if exitErr, ok := err.(*exec.ExitError); ok {
					exitCode = exitErr.ExitCode()
				} else {
					t.Fatalf("unexpected error: %v", err)
				}
			}
			if exitCode != tt.wantCode {
				t.Fatalf("exit code = %d, want %d; output: %s", exitCode, tt.wantCode, out)
			}
			if tt.wantOut != "" && !strings.Contains(string(out), tt.wantOut) {
				t.Fatalf("output %q does not contain %q", out, tt.wantOut)
			}
		})
	}
}

// TestHelperMain is called by TestMain_subprocess in a subprocess.
func TestHelperMain(t *testing.T) {
	if os.Getenv("PURECLAW_TEST_MAIN") != "1" {
		return
	}
	// Replace os.Args with the args after "--"
	args := os.Args
	for i, a := range args {
		if a == "--" {
			os.Args = append([]string{"pureclaw"}, args[i+1:]...)
			break
		}
	}
	main()
}
