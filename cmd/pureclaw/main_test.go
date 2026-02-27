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
	code := run([]string{"pureclaw", "version"}, &stdout, io.Discard)

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
	output := strings.TrimSpace(stdout.String())
	if output != Version {
		t.Fatalf("expected %q, got %q", Version, output)
	}
}

func TestRun_noArgs(t *testing.T) {
	code := run([]string{"pureclaw"}, io.Discard, io.Discard)
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
}

func TestRun_unknownCommand(t *testing.T) {
	code := run([]string{"pureclaw", "bogus"}, io.Discard, io.Discard)
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
}

func TestRun_unimplementedCommands(t *testing.T) {
	cmds := []string{"init", "run", "vault"}
	for _, cmd := range cmds {
		t.Run(cmd, func(t *testing.T) {
			code := run([]string{"pureclaw", cmd}, io.Discard, io.Discard)
			if code != 1 {
				t.Fatalf("expected exit code 1 for unimplemented %q, got %d", cmd, code)
			}
		})
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
