package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// Replaceable for testing.
var (
	osReadDir  = os.ReadDir
	osReadlink = os.Readlink
)

type listDirArgs struct {
	Path string `json:"path"`
}

// NewListDir returns the definition for the list_dir tool.
func NewListDir() Definition {
	return Definition{
		Name:        "list_dir",
		Description: "List the contents of a directory at the given path",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Absolute or relative path to the directory to list",
				},
			},
			"required": []string{"path"},
		},
		Handler: handleListDir,
	}
}

func handleListDir(ctx context.Context, args json.RawMessage) ToolResult {
	var a listDirArgs
	if err := json.Unmarshal(args, &a); err != nil {
		slog.Warn("invalid arguments",
			"component", "tool",
			"operation", "list_dir",
			"error", err,
		)
		return ToolResult{Success: false, Error: fmt.Sprintf("invalid arguments: %v", err)}
	}

	if a.Path == "" {
		slog.Warn("empty path",
			"component", "tool",
			"operation", "list_dir",
		)
		return ToolResult{Success: false, Error: "invalid arguments: path is required"}
	}

	slog.Info("listing directory",
		"component", "tool",
		"operation", "list_dir",
		"path", a.Path,
	)

	entries, err := osReadDir(a.Path)
	if err != nil {
		slog.Warn("readdir failed",
			"component", "tool",
			"operation", "list_dir",
			"path", a.Path,
			"error", err,
		)
		return ToolResult{Success: false, Error: err.Error()}
	}

	var lines []string
	for _, entry := range entries {
		switch {
		case entry.Type()&os.ModeSymlink != 0:
			target, err := osReadlink(filepath.Join(a.Path, entry.Name()))
			if err != nil {
				lines = append(lines, fmt.Sprintf("%s (symlink)", entry.Name()))
			} else {
				lines = append(lines, fmt.Sprintf("%s -> %s (symlink)", entry.Name(), target))
			}
		case entry.IsDir():
			lines = append(lines, fmt.Sprintf("%s/ (dir)", entry.Name()))
		default:
			lines = append(lines, fmt.Sprintf("%s (file)", entry.Name()))
		}
	}

	return ToolResult{Success: true, Output: strings.Join(lines, "\n")}
}
