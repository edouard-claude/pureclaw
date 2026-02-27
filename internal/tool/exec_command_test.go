package tool

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestExecCommand_Success(t *testing.T) {
	original := execCommandFn
	execCommandFn = func(ctx context.Context, command string) ([]byte, error) {
		return []byte("hello\n"), nil
	}
	defer func() { execCommandFn = original }()

	args, _ := json.Marshal(execCommandArgs{Command: "echo hello"})
	def := NewExecCommand(nil)
	result := def.Handler(context.Background(), args)

	if !result.Success {
		t.Fatalf("expected success=true, got false, error: %s", result.Error)
	}
	if result.Output != "hello\n" {
		t.Errorf("expected output %q, got %q", "hello\n", result.Output)
	}
}

func TestExecCommand_SecretSanitization(t *testing.T) {
	original := execCommandFn
	execCommandFn = func(ctx context.Context, command string) ([]byte, error) {
		return []byte("token: sk-abc123-secret\n"), nil
	}
	defer func() { execCommandFn = original }()

	secrets := []string{"sk-abc123-secret"}
	args, _ := json.Marshal(execCommandArgs{Command: "cat config"})
	def := NewExecCommand(secrets)
	result := def.Handler(context.Background(), args)

	if !result.Success {
		t.Fatalf("expected success=true, got false, error: %s", result.Error)
	}
	if strings.Contains(result.Output, "sk-abc123-secret") {
		t.Error("expected secret to be redacted from output")
	}
	if !strings.Contains(result.Output, "[REDACTED]") {
		t.Errorf("expected [REDACTED] in output, got %q", result.Output)
	}
}

func TestExecCommand_MultipleSecrets(t *testing.T) {
	original := execCommandFn
	execCommandFn = func(ctx context.Context, command string) ([]byte, error) {
		return []byte("key1=secret1 key2=secret2\n"), nil
	}
	defer func() { execCommandFn = original }()

	secrets := []string{"secret1", "secret2"}
	args, _ := json.Marshal(execCommandArgs{Command: "env"})
	def := NewExecCommand(secrets)
	result := def.Handler(context.Background(), args)

	if !result.Success {
		t.Fatalf("expected success=true, got false, error: %s", result.Error)
	}
	if strings.Contains(result.Output, "secret1") || strings.Contains(result.Output, "secret2") {
		t.Errorf("expected all secrets redacted, got %q", result.Output)
	}
	if count := strings.Count(result.Output, "[REDACTED]"); count != 2 {
		t.Errorf("expected 2 [REDACTED] occurrences, got %d", count)
	}
}

func TestExecCommand_SecretInError(t *testing.T) {
	original := execCommandFn
	execCommandFn = func(ctx context.Context, command string) ([]byte, error) {
		return []byte("partial output with mysecret"), errors.New("failed: mysecret leaked")
	}
	defer func() { execCommandFn = original }()

	secrets := []string{"mysecret"}
	args, _ := json.Marshal(execCommandArgs{Command: "failing-cmd"})
	def := NewExecCommand(secrets)
	result := def.Handler(context.Background(), args)

	if result.Success {
		t.Fatal("expected success=false for command error")
	}
	if strings.Contains(result.Error, "mysecret") {
		t.Errorf("expected secret redacted from error, got %q", result.Error)
	}
	if strings.Contains(result.Output, "mysecret") {
		t.Errorf("expected secret redacted from output, got %q", result.Output)
	}
	if !strings.Contains(result.Error, "[REDACTED]") {
		t.Errorf("expected [REDACTED] in error, got %q", result.Error)
	}
}

func TestExecCommand_NonZeroExit(t *testing.T) {
	original := execCommandFn
	execCommandFn = func(ctx context.Context, command string) ([]byte, error) {
		return []byte("some output\n"), &exec.ExitError{}
	}
	defer func() { execCommandFn = original }()

	args, _ := json.Marshal(execCommandArgs{Command: "false"})
	def := NewExecCommand(nil)
	result := def.Handler(context.Background(), args)

	if result.Success {
		t.Fatal("expected success=false for non-zero exit code")
	}
	if result.Output != "some output\n" {
		t.Errorf("expected output %q, got %q", "some output\n", result.Output)
	}
	if result.Error == "" {
		t.Error("expected non-empty error for non-zero exit code")
	}
}

func TestExecCommand_NonZeroExitWithSecrets(t *testing.T) {
	original := execCommandFn
	execCommandFn = func(ctx context.Context, command string) ([]byte, error) {
		return []byte("output with mysecret\n"), &exec.ExitError{}
	}
	defer func() { execCommandFn = original }()

	secrets := []string{"mysecret"}
	args, _ := json.Marshal(execCommandArgs{Command: "failing-cmd"})
	def := NewExecCommand(secrets)
	result := def.Handler(context.Background(), args)

	if result.Success {
		t.Fatal("expected success=false for non-zero exit code")
	}
	if strings.Contains(result.Output, "mysecret") {
		t.Errorf("expected secret redacted from output, got %q", result.Output)
	}
	if !strings.Contains(result.Output, "[REDACTED]") {
		t.Errorf("expected [REDACTED] in output, got %q", result.Output)
	}
}

func TestExecCommand_Timeout(t *testing.T) {
	original := execCommandFn
	execCommandFn = func(ctx context.Context, command string) ([]byte, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	defer func() { execCommandFn = original }()

	// Use a context with a deadline already in the past so the child context
	// created by WithTimeout inherits DeadlineExceeded (not Canceled).
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	args, _ := json.Marshal(execCommandArgs{Command: "sleep 100"})
	def := NewExecCommand(nil)
	result := def.Handler(ctx, args)

	if result.Success {
		t.Fatal("expected success=false for timeout")
	}
	if !strings.Contains(result.Error, "timed out") {
		t.Errorf("expected timeout error, got %q", result.Error)
	}
}

func TestExecCommand_EmptyCommand(t *testing.T) {
	args, _ := json.Marshal(execCommandArgs{Command: ""})
	def := NewExecCommand(nil)
	result := def.Handler(context.Background(), args)

	if result.Success {
		t.Fatal("expected success=false for empty command")
	}
	if !strings.Contains(result.Error, "command is required") {
		t.Errorf("expected error to contain 'command is required', got %q", result.Error)
	}
}

func TestExecCommand_InvalidArgs(t *testing.T) {
	def := NewExecCommand(nil)
	result := def.Handler(context.Background(), json.RawMessage(`{invalid`))

	if result.Success {
		t.Fatal("expected success=false for invalid args")
	}
	if !strings.Contains(result.Error, "invalid arguments") {
		t.Errorf("expected error to contain 'invalid arguments', got %q", result.Error)
	}
}

func TestExecCommand_EmptySecrets(t *testing.T) {
	original := execCommandFn
	execCommandFn = func(ctx context.Context, command string) ([]byte, error) {
		return []byte("output with no secrets\n"), nil
	}
	defer func() { execCommandFn = original }()

	args, _ := json.Marshal(execCommandArgs{Command: "echo test"})
	def := NewExecCommand([]string{})
	result := def.Handler(context.Background(), args)

	if !result.Success {
		t.Fatalf("expected success=true, got false, error: %s", result.Error)
	}
	if result.Output != "output with no secrets\n" {
		t.Errorf("expected output unchanged, got %q", result.Output)
	}
}

func TestExecCommand_OutputTruncation(t *testing.T) {
	original := execCommandFn
	bigOutput := strings.Repeat("x", maxExecOutputSize+100)
	execCommandFn = func(ctx context.Context, command string) ([]byte, error) {
		return []byte(bigOutput), nil
	}
	defer func() { execCommandFn = original }()

	args, _ := json.Marshal(execCommandArgs{Command: "generate-big-output"})
	def := NewExecCommand(nil)
	result := def.Handler(context.Background(), args)

	if !result.Success {
		t.Fatalf("expected success=true, got false, error: %s", result.Error)
	}
	if !strings.HasSuffix(result.Output, "\n[output truncated at 1MB]") {
		t.Error("expected truncation suffix in output")
	}
	// The output should be maxExecOutputSize + the suffix length.
	expectedLen := maxExecOutputSize + len("\n[output truncated at 1MB]")
	if len(result.Output) != expectedLen {
		t.Errorf("expected output length %d, got %d", expectedLen, len(result.Output))
	}
}

func TestExecCommand_Definition(t *testing.T) {
	def := NewExecCommand(nil)

	if def.Name != "exec_command" {
		t.Errorf("expected name %q, got %q", "exec_command", def.Name)
	}
	if def.Description == "" {
		t.Error("expected non-empty description")
	}
	if def.Parameters == nil {
		t.Error("expected non-nil parameters")
	}
	if def.Handler == nil {
		t.Error("expected non-nil handler")
	}
}

func TestSanitize(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		secrets []string
		want    string
	}{
		{
			name:    "single secret",
			input:   "the password is hunter2",
			secrets: []string{"hunter2"},
			want:    "the password is [REDACTED]",
		},
		{
			name:    "multiple occurrences",
			input:   "abc123 and abc123 again",
			secrets: []string{"abc123"},
			want:    "[REDACTED] and [REDACTED] again",
		},
		{
			name:    "multiple secrets",
			input:   "key=aaa val=bbb",
			secrets: []string{"aaa", "bbb"},
			want:    "key=[REDACTED] val=[REDACTED]",
		},
		{
			name:    "no match",
			input:   "nothing to redact",
			secrets: []string{"xyz"},
			want:    "nothing to redact",
		},
		{
			name:    "empty string in secrets skipped",
			input:   "output text",
			secrets: []string{"", "text"},
			want:    "output [REDACTED]",
		},
		{
			name:    "substring secret replaced longest first",
			input:   "token is abcdef123",
			secrets: []string{"abc", "abcdef123"},
			want:    "token is [REDACTED]",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitize(tc.input, tc.secrets)
			if got != tc.want {
				t.Errorf("sanitize(%q, %v) = %q, want %q", tc.input, tc.secrets, got, tc.want)
			}
		})
	}
}

func TestSanitize_EmptySecrets(t *testing.T) {
	got := sanitize("hello world", nil)
	if got != "hello world" {
		t.Errorf("expected unchanged output, got %q", got)
	}
}

func TestSanitize_EmptyOutput(t *testing.T) {
	got := sanitize("", []string{"secret"})
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}
