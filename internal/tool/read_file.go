package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
)

const maxReadFileSize = 10 * 1024 * 1024 // 10 MB — protects memory budget (NFR1)

// Replaceable for testing.
var (
	osReadFile = os.ReadFile
	osStat     = os.Stat
)

type readFileArgs struct {
	Path string `json:"path"`
}

// NewReadFile returns the definition for the read_file tool.
func NewReadFile() Definition {
	return Definition{
		Name:        "read_file",
		Description: "Read the contents of a file at the given path",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Absolute or relative path to the file to read",
				},
			},
			"required": []string{"path"},
		},
		Handler: handleReadFile,
	}
}

func handleReadFile(ctx context.Context, args json.RawMessage) ToolResult {
	var a readFileArgs
	if err := json.Unmarshal(args, &a); err != nil {
		slog.Warn("invalid arguments",
			"component", "tool",
			"operation", "read_file",
			"error", err,
		)
		return ToolResult{Success: false, Error: fmt.Sprintf("invalid arguments: %v", err)}
	}

	if a.Path == "" {
		slog.Warn("empty path",
			"component", "tool",
			"operation", "read_file",
		)
		return ToolResult{Success: false, Error: "invalid arguments: path is required"}
	}

	slog.Info("reading file",
		"component", "tool",
		"operation", "read_file",
		"path", a.Path,
	)

	info, err := osStat(a.Path)
	if err != nil {
		slog.Warn("stat failed",
			"component", "tool",
			"operation", "read_file",
			"path", a.Path,
			"error", err,
		)
		return ToolResult{Success: false, Error: err.Error()}
	}
	if info.Size() > maxReadFileSize {
		slog.Warn("file too large",
			"component", "tool",
			"operation", "read_file",
			"path", a.Path,
			"size", info.Size(),
			"max", maxReadFileSize,
		)
		return ToolResult{Success: false, Error: fmt.Sprintf("file too large: %d bytes (max %d)", info.Size(), maxReadFileSize)}
	}

	data, err := osReadFile(a.Path)
	if err != nil {
		slog.Warn("read failed",
			"component", "tool",
			"operation", "read_file",
			"path", a.Path,
			"error", err,
		)
		return ToolResult{Success: false, Error: err.Error()}
	}

	return ToolResult{Success: true, Output: string(data)}
}
