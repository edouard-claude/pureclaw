package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/edouard/pureclaw/internal/platform"
)

// Replaceable for testing.
var (
	osMkdirAll  = os.MkdirAll
	atomicWrite = platform.AtomicWrite
)

type writeFileArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// NewWriteFile returns the definition for the write_file tool.
func NewWriteFile() Definition {
	return Definition{
		Name:        "write_file",
		Description: "Write content to a file at the given path using atomic write",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Absolute or relative path to the file to write",
				},
				"content": map[string]any{
					"type":        "string",
					"description": "Content to write to the file",
				},
			},
			"required": []string{"path", "content"},
		},
		Handler: handleWriteFile,
	}
}

func handleWriteFile(ctx context.Context, args json.RawMessage) ToolResult {
	var a writeFileArgs
	if err := json.Unmarshal(args, &a); err != nil {
		slog.Warn("invalid arguments",
			"component", "tool",
			"operation", "write_file",
			"error", err,
		)
		return ToolResult{Success: false, Error: fmt.Sprintf("invalid arguments: %v", err)}
	}

	if a.Path == "" {
		slog.Warn("empty path",
			"component", "tool",
			"operation", "write_file",
		)
		return ToolResult{Success: false, Error: "invalid arguments: path is required"}
	}

	slog.Info("writing file",
		"component", "tool",
		"operation", "write_file",
		"path", a.Path,
	)

	if err := osMkdirAll(filepath.Dir(a.Path), 0o755); err != nil {
		slog.Warn("mkdir failed",
			"component", "tool",
			"operation", "write_file",
			"path", a.Path,
			"error", err,
		)
		return ToolResult{Success: false, Error: err.Error()}
	}

	if err := atomicWrite(a.Path, []byte(a.Content), 0o644); err != nil {
		slog.Warn("write failed",
			"component", "tool",
			"operation", "write_file",
			"path", a.Path,
			"error", err,
		)
		return ToolResult{Success: false, Error: err.Error()}
	}

	return ToolResult{Success: true, Output: "file written: " + a.Path}
}
