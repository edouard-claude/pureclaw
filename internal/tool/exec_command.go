package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"sort"
	"strings"
	"time"
)

const (
	defaultExecTimeout = 30 * time.Second
	maxExecOutputSize  = 1 << 20 // 1 MB
)

// Replaceable for testing.
var execCommandFn = func(ctx context.Context, command string) ([]byte, error) {
	return exec.CommandContext(ctx, "sh", "-c", command).CombinedOutput()
}

type execCommandArgs struct {
	Command string `json:"command"`
}

// sanitize replaces all secret values with [REDACTED] in the output string.
// Secrets are sorted by length (longest first) to prevent partial redaction
// when one secret is a substring of another.
func sanitize(output string, secrets []string) string {
	sorted := make([]string, 0, len(secrets))
	for _, s := range secrets {
		if s != "" {
			sorted = append(sorted, s)
		}
	}
	sort.Slice(sorted, func(i, j int) bool {
		return len(sorted[i]) > len(sorted[j])
	})
	for _, s := range sorted {
		output = strings.ReplaceAll(output, s, "[REDACTED]")
	}
	return output
}

// NewExecCommand creates an exec_command tool that sanitizes secrets from output.
// secrets is a list of vault secret values to redact from command output.
func NewExecCommand(secrets []string) Definition {
	return Definition{
		Name:        "exec_command",
		Description: "Execute a shell command on the host system. Returns stdout/stderr with secrets redacted.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{
					"type":        "string",
					"description": "The command to execute (passed to sh -c)",
				},
			},
			"required": []string{"command"},
		},
		Handler: makeExecHandler(secrets),
	}
}

func makeExecHandler(secrets []string) Handler {
	return func(ctx context.Context, args json.RawMessage) ToolResult {
		var a execCommandArgs
		if err := json.Unmarshal(args, &a); err != nil {
			slog.Warn("invalid arguments",
				"component", "tool",
				"operation", "exec_command",
				"error", err,
			)
			return ToolResult{Success: false, Error: fmt.Sprintf("invalid arguments: %v", err)}
		}

		if a.Command == "" {
			slog.Warn("empty command",
				"component", "tool",
				"operation", "exec_command",
			)
			return ToolResult{Success: false, Error: "command is required"}
		}

		slog.Info("executing command",
			"component", "tool",
			"operation", "exec_command",
		)

		childCtx, cancel := context.WithTimeout(ctx, defaultExecTimeout)
		defer cancel()

		output, err := execCommandFn(childCtx, a.Command)

		// Truncate output if too large.
		out := string(output)
		if len(out) > maxExecOutputSize {
			out = out[:maxExecOutputSize] + "\n[output truncated at 1MB]"
		}

		if err != nil {
			// Check for timeout (context deadline exceeded).
			if childCtx.Err() == context.DeadlineExceeded {
				slog.Warn("command timed out",
					"component", "tool",
					"operation", "exec_command",
				)
				return ToolResult{Success: false, Error: "command timed out after 30s"}
			}

			slog.Warn("command failed",
				"component", "tool",
				"operation", "exec_command",
				"error", err,
			)
			return ToolResult{
				Success: false,
				Output:  sanitize(out, secrets),
				Error:   sanitize(err.Error(), secrets),
			}
		}

		return ToolResult{Success: true, Output: sanitize(out, secrets)}
	}
}
