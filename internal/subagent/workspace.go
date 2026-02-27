package subagent

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/edouard/pureclaw/internal/platform"
	"github.com/edouard/pureclaw/internal/workspace"
)

// Replaceable for testing error paths.
var (
	mkdirAll    = os.MkdirAll
	atomicWrite = platform.AtomicWrite
	osStat      = os.Stat
)

// WorkspaceConfig holds the parameters for creating a sub-agent workspace.
type WorkspaceConfig struct {
	ParentWorkspace  *workspace.Workspace
	TaskID           string
	TaskDescription  string
	AgentsDir        string // Parent's agents/ directory path
	IncludeHeartbeat bool
	IncludeSkills    bool
}

// CreateWorkspace creates an isolated sub-agent workspace at AgentsDir/<TaskID>/.
// Returns the absolute path to the created workspace directory.
func CreateWorkspace(cfg WorkspaceConfig) (string, error) {
	if cfg.TaskID == "" {
		return "", fmt.Errorf("task ID is required")
	}
	if cfg.TaskDescription == "" {
		return "", fmt.Errorf("task description is required")
	}
	if cfg.ParentWorkspace == nil {
		return "", fmt.Errorf("parent workspace is required")
	}
	if cfg.AgentsDir == "" {
		return "", fmt.Errorf("agents directory is required")
	}

	wsPath := filepath.Join(cfg.AgentsDir, cfg.TaskID)

	// Ensure agents directory exists (must exist before ValidatePath can resolve it).
	if err := mkdirAll(cfg.AgentsDir, 0o755); err != nil {
		return "", fmt.Errorf("create agents dir: %w", err)
	}

	// Validate resolved path stays within agents directory (path traversal guard).
	if err := platform.ValidatePath(cfg.AgentsDir, wsPath); err != nil {
		return "", fmt.Errorf("invalid task ID %q: %w", cfg.TaskID, err)
	}

	// Idempotency guard — refuse to overwrite existing workspace.
	if _, err := osStat(wsPath); err == nil {
		return "", fmt.Errorf("workspace already exists: %s", wsPath)
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("check workspace path: %w", err)
	}

	slog.Info("creating sub-agent workspace",
		"component", "subagent", "operation", "create_workspace",
		"task_id", cfg.TaskID, "path", wsPath)

	// Create workspace directory.
	if err := mkdirAll(wsPath, 0o755); err != nil {
		return "", fmt.Errorf("create workspace dir: %w", err)
	}

	// Generate task-specific AGENT.md.
	agentMD := generateAgentMD(cfg.TaskID, cfg.TaskDescription)
	if err := atomicWrite(filepath.Join(wsPath, "AGENT.md"), []byte(agentMD), 0o644); err != nil {
		return "", fmt.Errorf("write AGENT.md: %w", err)
	}

	// Copy SOUL.md from parent (raw copy — personality inherited).
	if err := atomicWrite(filepath.Join(wsPath, "SOUL.md"), []byte(cfg.ParentWorkspace.SoulMD), 0o644); err != nil {
		return "", fmt.Errorf("write SOUL.md: %w", err)
	}

	// Conditionally copy HEARTBEAT.md.
	if cfg.IncludeHeartbeat && cfg.ParentWorkspace.HeartbeatMD != "" {
		if err := atomicWrite(filepath.Join(wsPath, "HEARTBEAT.md"), []byte(cfg.ParentWorkspace.HeartbeatMD), 0o644); err != nil {
			return "", fmt.Errorf("write HEARTBEAT.md: %w", err)
		}
	}

	// Conditionally copy skills/.
	if cfg.IncludeSkills && len(cfg.ParentWorkspace.Skills) > 0 {
		for _, skill := range cfg.ParentWorkspace.Skills {
			skillDir := filepath.Join(wsPath, "skills", skill.Name)
			if err := mkdirAll(skillDir, 0o755); err != nil {
				return "", fmt.Errorf("create skill dir %s: %w", skill.Name, err)
			}
			if err := atomicWrite(filepath.Join(skillDir, "SKILL.md"), []byte(skill.Content), 0o644); err != nil {
				return "", fmt.Errorf("write skill %s: %w", skill.Name, err)
			}
		}
	}

	// Create memory/ directory for sub-agent logging.
	if err := mkdirAll(filepath.Join(wsPath, "memory"), 0o755); err != nil {
		return "", fmt.Errorf("create memory dir: %w", err)
	}

	slog.Info("sub-agent workspace created",
		"component", "subagent", "operation", "create_workspace",
		"task_id", cfg.TaskID, "path", wsPath)

	return wsPath, nil
}

// generateAgentMD creates a task-specific AGENT.md for the sub-agent.
func generateAgentMD(taskID, taskDescription string) string {
	return fmt.Sprintf(`# Sub-Agent: %s

## Mission

%s

## Constraints

- You are a sub-agent with depth=1. You CANNOT spawn further sub-agents.
- You have NO Telegram access. Work autonomously within this workspace.
- Write your final result to result.md in this workspace root.
- All file operations are restricted to this workspace directory.

## Environment

_To be populated by introspection on first run._
`, taskID, taskDescription)
}
