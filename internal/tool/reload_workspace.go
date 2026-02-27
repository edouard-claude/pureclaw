package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/edouard/pureclaw/internal/workspace"
)

// Replaceable for testing.
var workspaceLoadFn = workspace.Load

// NewReloadWorkspace creates a tool that reloads workspace files from disk.
// ws is shared with the agent — mutating it updates the agent's view.
func NewReloadWorkspace(ws *workspace.Workspace) Definition {
	return Definition{
		Name:        "reload_workspace",
		Description: "Reload workspace files (AGENT.md, SOUL.md, HEARTBEAT.md, skills/) from disk into memory. Call this after modifying workspace files with write_file to apply changes immediately.",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
		Handler: makeReloadHandler(ws),
	}
}

func makeReloadHandler(ws *workspace.Workspace) Handler {
	// args is intentionally ignored — this tool takes no parameters.
	return func(ctx context.Context, args json.RawMessage) ToolResult {
		slog.Info("reloading workspace",
			"component", "tool",
			"operation", "reload_workspace",
			"root", ws.Root,
		)

		// Fail fast if context is already cancelled.
		if err := ctx.Err(); err != nil {
			return ToolResult{Success: false, Error: fmt.Sprintf("workspace reload cancelled: %v", err)}
		}

		newWS, err := workspaceLoadFn(ws.Root)
		if err != nil {
			slog.Error("workspace reload failed",
				"component", "tool",
				"operation", "reload_workspace",
				"error", err,
			)
			return ToolResult{Success: false, Error: fmt.Sprintf("workspace reload failed: %v", err)}
		}

		// Dereference-assign: copies all fields from newWS into the struct ws points to.
		*ws = *newWS

		summary := fmt.Sprintf("workspace reloaded from %s: AGENT.md, SOUL.md", ws.Root)
		if ws.HeartbeatMD != "" {
			summary += ", HEARTBEAT.md"
		}
		if len(ws.Skills) > 0 {
			summary += fmt.Sprintf(", %d skill(s)", len(ws.Skills))
		}

		slog.Info("workspace reloaded",
			"component", "tool",
			"operation", "reload_workspace",
			"skills", len(ws.Skills),
		)
		return ToolResult{Success: true, Output: summary}
	}
}
