package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/edouard/pureclaw/internal/subagent"
	"github.com/edouard/pureclaw/internal/workspace"
)

// SpawnAgentDeps holds all dependencies needed by the spawn_agent tool.
type SpawnAgentDeps struct {
	Runner          *subagent.Runner
	ParentWorkspace *workspace.Workspace
	ResultCh        chan<- subagent.SubAgentResult
	BinaryPath      string
	ConfigPath      string
	VaultPath       string
	Timeout         time.Duration
	AgentsDir       string // Parent's agents/ directory path
}

// Replaceable for testing.
var (
	createWorkspaceFn  = subagent.CreateWorkspace
	launchSubAgentFn = func(r *subagent.Runner, ctx context.Context, cfg subagent.RunnerConfig, ch chan<- subagent.SubAgentResult) error {
		return r.LaunchSubAgent(ctx, cfg, ch)
	}
)

// NewSpawnAgent creates a spawn_agent tool that delegates complex tasks to sub-agents.
func NewSpawnAgent(deps SpawnAgentDeps) Definition {
	return Definition{
		Name:        "spawn_agent",
		Description: "Spawn an isolated sub-agent to handle a complex or long-running task autonomously. The sub-agent runs in its own workspace as a subprocess with a timeout. Use this when a task requires multiple steps, extensive file analysis, or would take too long for a single interaction. Returns immediately — results arrive asynchronously.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"task_id": map[string]any{
					"type":        "string",
					"description": "Unique identifier for the task (used as workspace folder name, e.g. 'log-analysis-2026-03')",
				},
				"task_description": map[string]any{
					"type":        "string",
					"description": "Detailed description of the task for the sub-agent to execute. Be specific about what to do, what files to examine, and what result to produce.",
				},
				"include_heartbeat": map[string]any{
					"type":        "boolean",
					"description": "Whether to copy HEARTBEAT.md to the sub-agent workspace (default: false)",
				},
				"include_skills": map[string]any{
					"type":        "boolean",
					"description": "Whether to copy skills/ to the sub-agent workspace (default: false)",
				},
			},
			"required": []string{"task_id", "task_description"},
		},
		Handler: makeSpawnHandler(deps),
	}
}

type spawnArgs struct {
	TaskID           string `json:"task_id"`
	TaskDescription  string `json:"task_description"`
	IncludeHeartbeat bool   `json:"include_heartbeat"`
	IncludeSkills    bool   `json:"include_skills"`
}

func makeSpawnHandler(deps SpawnAgentDeps) Handler {
	return func(ctx context.Context, args json.RawMessage) ToolResult {
		slog.Info("spawn_agent called",
			"component", "tool", "operation", "spawn_agent")

		if err := ctx.Err(); err != nil {
			slog.Warn("spawn_agent cancelled",
				"component", "tool", "operation", "spawn_agent", "error", err)
			return ToolResult{Success: false, Error: fmt.Sprintf("spawn cancelled: %v", err)}
		}

		var a spawnArgs
		if err := json.Unmarshal(args, &a); err != nil {
			return ToolResult{Success: false, Error: fmt.Sprintf("invalid arguments: %v", err)}
		}
		if a.TaskID == "" {
			return ToolResult{Success: false, Error: "task_id is required"}
		}
		if a.TaskDescription == "" {
			return ToolResult{Success: false, Error: "task_description is required"}
		}

		// 1. Create isolated workspace.
		wsCfg := subagent.WorkspaceConfig{
			ParentWorkspace:  deps.ParentWorkspace,
			TaskID:           a.TaskID,
			TaskDescription:  a.TaskDescription,
			AgentsDir:        deps.AgentsDir,
			IncludeHeartbeat: a.IncludeHeartbeat,
			IncludeSkills:    a.IncludeSkills,
		}
		wsPath, err := createWorkspaceFn(wsCfg)
		if err != nil {
			slog.Error("workspace creation failed",
				"component", "tool", "operation", "spawn_agent",
				"task_id", a.TaskID, "error", err)
			return ToolResult{Success: false, Error: fmt.Sprintf("workspace creation failed: %v", err)}
		}

		// 2. Launch subprocess.
		runCfg := subagent.RunnerConfig{
			BinaryPath:    deps.BinaryPath,
			WorkspacePath: wsPath,
			TaskID:        a.TaskID,
			Timeout:       deps.Timeout,
			ConfigPath:    deps.ConfigPath,
			VaultPath:     deps.VaultPath,
		}
		if err := launchSubAgentFn(deps.Runner, ctx, runCfg, deps.ResultCh); err != nil {
			slog.Error("sub-agent launch failed",
				"component", "tool", "operation", "spawn_agent",
				"task_id", a.TaskID, "error", err)
			return ToolResult{Success: false, Error: fmt.Sprintf("sub-agent launch failed: %v", err)}
		}

		slog.Info("sub-agent spawned",
			"component", "tool", "operation", "spawn_agent",
			"task_id", a.TaskID, "workspace", wsPath,
			"timeout", deps.Timeout)

		return ToolResult{
			Success: true,
			Output:  fmt.Sprintf("Sub-agent '%s' launched. It will work autonomously and results will be reported when complete (timeout: %s).", a.TaskID, deps.Timeout),
		}
	}
}
