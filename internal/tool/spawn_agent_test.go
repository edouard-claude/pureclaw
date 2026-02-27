package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/edouard/pureclaw/internal/subagent"
	"github.com/edouard/pureclaw/internal/workspace"
)

// saveSpawnVars saves and restores replaceable vars for spawn_agent tests.
func saveSpawnVars(t *testing.T) {
	t.Helper()
	origCreateWorkspace := createWorkspaceFn
	origLaunchSubAgent := launchSubAgentFn
	t.Cleanup(func() {
		createWorkspaceFn = origCreateWorkspace
		launchSubAgentFn = origLaunchSubAgent
	})
}

func testSpawnDeps() SpawnAgentDeps {
	return SpawnAgentDeps{
		Runner:          subagent.NewRunner(),
		ParentWorkspace: &workspace.Workspace{Root: "/test/workspace", AgentMD: "agent", SoulMD: "soul"},
		ResultCh:        make(chan subagent.SubAgentResult, 1),
		BinaryPath:      "/usr/local/bin/pureclaw",
		ConfigPath:      "/test/config.json",
		VaultPath:       "/test/vault.enc",
		Timeout:         5 * time.Minute,
		AgentsDir:       "/test/workspace/agents",
	}
}

func TestNewSpawnAgent_Definition(t *testing.T) {
	deps := testSpawnDeps()
	def := NewSpawnAgent(deps)

	if def.Name != "spawn_agent" {
		t.Errorf("Name = %q, want %q", def.Name, "spawn_agent")
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

	// Verify parameters schema has required fields.
	params, ok := def.Parameters.(map[string]any)
	if !ok {
		t.Fatal("parameters should be map[string]any")
	}
	props, ok := params["properties"].(map[string]any)
	if !ok {
		t.Fatal("parameters.properties should be map[string]any")
	}
	for _, field := range []string{"task_id", "task_description", "include_heartbeat", "include_skills"} {
		if _, ok := props[field]; !ok {
			t.Errorf("parameters should include %q", field)
		}
	}
	required, ok := params["required"].([]string)
	if !ok {
		t.Fatal("parameters.required should be []string")
	}
	requiredSet := map[string]bool{}
	for _, r := range required {
		requiredSet[r] = true
	}
	if !requiredSet["task_id"] || !requiredSet["task_description"] {
		t.Errorf("required = %v, want task_id and task_description", required)
	}
}

func TestSpawnAgent_Success(t *testing.T) {
	saveSpawnVars(t)

	var capturedWsCfg subagent.WorkspaceConfig
	createWorkspaceFn = func(cfg subagent.WorkspaceConfig) (string, error) {
		capturedWsCfg = cfg
		return "/test/workspace/agents/my-task", nil
	}

	var capturedRunCfg subagent.RunnerConfig
	launchSubAgentFn = func(r *subagent.Runner, ctx context.Context, cfg subagent.RunnerConfig, ch chan<- subagent.SubAgentResult) error {
		capturedRunCfg = cfg
		return nil
	}

	deps := testSpawnDeps()
	def := NewSpawnAgent(deps)

	args := `{"task_id": "my-task", "task_description": "Analyze logs for errors"}`
	result := def.Handler(context.Background(), json.RawMessage(args))

	if !result.Success {
		t.Fatalf("expected success=true, got false, error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "my-task") {
		t.Errorf("output should contain task_id, got %q", result.Output)
	}
	if !strings.Contains(result.Output, "launched") {
		t.Errorf("output should contain 'launched', got %q", result.Output)
	}

	// Verify workspace config.
	if capturedWsCfg.TaskID != "my-task" {
		t.Errorf("WorkspaceConfig.TaskID = %q, want %q", capturedWsCfg.TaskID, "my-task")
	}
	if capturedWsCfg.TaskDescription != "Analyze logs for errors" {
		t.Errorf("WorkspaceConfig.TaskDescription = %q, want %q", capturedWsCfg.TaskDescription, "Analyze logs for errors")
	}
	if capturedWsCfg.AgentsDir != "/test/workspace/agents" {
		t.Errorf("WorkspaceConfig.AgentsDir = %q, want %q", capturedWsCfg.AgentsDir, "/test/workspace/agents")
	}
	if capturedWsCfg.ParentWorkspace == nil {
		t.Error("WorkspaceConfig.ParentWorkspace should not be nil")
	}

	// Verify runner config.
	if capturedRunCfg.BinaryPath != "/usr/local/bin/pureclaw" {
		t.Errorf("RunnerConfig.BinaryPath = %q, want %q", capturedRunCfg.BinaryPath, "/usr/local/bin/pureclaw")
	}
	if capturedRunCfg.WorkspacePath != "/test/workspace/agents/my-task" {
		t.Errorf("RunnerConfig.WorkspacePath = %q, want %q", capturedRunCfg.WorkspacePath, "/test/workspace/agents/my-task")
	}
	if capturedRunCfg.TaskID != "my-task" {
		t.Errorf("RunnerConfig.TaskID = %q, want %q", capturedRunCfg.TaskID, "my-task")
	}
	if capturedRunCfg.Timeout != 5*time.Minute {
		t.Errorf("RunnerConfig.Timeout = %v, want %v", capturedRunCfg.Timeout, 5*time.Minute)
	}
	if capturedRunCfg.ConfigPath != "/test/config.json" {
		t.Errorf("RunnerConfig.ConfigPath = %q, want %q", capturedRunCfg.ConfigPath, "/test/config.json")
	}
	if capturedRunCfg.VaultPath != "/test/vault.enc" {
		t.Errorf("RunnerConfig.VaultPath = %q, want %q", capturedRunCfg.VaultPath, "/test/vault.enc")
	}
}

func TestSpawnAgent_AlreadyActive(t *testing.T) {
	saveSpawnVars(t)

	createWorkspaceFn = func(cfg subagent.WorkspaceConfig) (string, error) {
		return "/test/workspace/agents/" + cfg.TaskID, nil
	}
	launchSubAgentFn = func(r *subagent.Runner, ctx context.Context, cfg subagent.RunnerConfig, ch chan<- subagent.SubAgentResult) error {
		return fmt.Errorf("sub-agent already active")
	}

	deps := testSpawnDeps()
	def := NewSpawnAgent(deps)

	args := `{"task_id": "task-1", "task_description": "some task"}`
	result := def.Handler(context.Background(), json.RawMessage(args))

	if result.Success {
		t.Fatal("expected success=false for already active sub-agent")
	}
	if !strings.Contains(result.Error, "sub-agent launch failed") {
		t.Errorf("error should contain 'sub-agent launch failed', got %q", result.Error)
	}
	if !strings.Contains(result.Error, "already active") {
		t.Errorf("error should contain 'already active', got %q", result.Error)
	}
}

func TestSpawnAgent_WorkspaceCreationFails(t *testing.T) {
	saveSpawnVars(t)

	createWorkspaceFn = func(cfg subagent.WorkspaceConfig) (string, error) {
		return "", fmt.Errorf("workspace already exists: /test/workspace/agents/task-1")
	}

	deps := testSpawnDeps()
	def := NewSpawnAgent(deps)

	args := `{"task_id": "task-1", "task_description": "some task"}`
	result := def.Handler(context.Background(), json.RawMessage(args))

	if result.Success {
		t.Fatal("expected success=false for workspace creation failure")
	}
	if !strings.Contains(result.Error, "workspace creation failed") {
		t.Errorf("error should contain 'workspace creation failed', got %q", result.Error)
	}
}

func TestSpawnAgent_LaunchFails(t *testing.T) {
	saveSpawnVars(t)

	createWorkspaceFn = func(cfg subagent.WorkspaceConfig) (string, error) {
		return "/test/workspace/agents/" + cfg.TaskID, nil
	}
	launchSubAgentFn = func(r *subagent.Runner, ctx context.Context, cfg subagent.RunnerConfig, ch chan<- subagent.SubAgentResult) error {
		return fmt.Errorf("start sub-agent: exec: not found")
	}

	deps := testSpawnDeps()
	def := NewSpawnAgent(deps)

	args := `{"task_id": "task-1", "task_description": "some task"}`
	result := def.Handler(context.Background(), json.RawMessage(args))

	if result.Success {
		t.Fatal("expected success=false for launch failure")
	}
	if !strings.Contains(result.Error, "sub-agent launch failed") {
		t.Errorf("error should contain 'sub-agent launch failed', got %q", result.Error)
	}
}

func TestSpawnAgent_MissingTaskID(t *testing.T) {
	saveSpawnVars(t)

	deps := testSpawnDeps()
	def := NewSpawnAgent(deps)

	args := `{"task_description": "some task"}`
	result := def.Handler(context.Background(), json.RawMessage(args))

	if result.Success {
		t.Fatal("expected success=false for missing task_id")
	}
	if result.Error != "task_id is required" {
		t.Errorf("error = %q, want %q", result.Error, "task_id is required")
	}
}

func TestSpawnAgent_EmptyTaskID(t *testing.T) {
	saveSpawnVars(t)

	deps := testSpawnDeps()
	def := NewSpawnAgent(deps)

	args := `{"task_id": "", "task_description": "some task"}`
	result := def.Handler(context.Background(), json.RawMessage(args))

	if result.Success {
		t.Fatal("expected success=false for empty task_id")
	}
	if result.Error != "task_id is required" {
		t.Errorf("error = %q, want %q", result.Error, "task_id is required")
	}
}

func TestSpawnAgent_MissingTaskDescription(t *testing.T) {
	saveSpawnVars(t)

	deps := testSpawnDeps()
	def := NewSpawnAgent(deps)

	args := `{"task_id": "task-1"}`
	result := def.Handler(context.Background(), json.RawMessage(args))

	if result.Success {
		t.Fatal("expected success=false for missing task_description")
	}
	if result.Error != "task_description is required" {
		t.Errorf("error = %q, want %q", result.Error, "task_description is required")
	}
}

func TestSpawnAgent_EmptyTaskDescription(t *testing.T) {
	saveSpawnVars(t)

	deps := testSpawnDeps()
	def := NewSpawnAgent(deps)

	args := `{"task_id": "task-1", "task_description": ""}`
	result := def.Handler(context.Background(), json.RawMessage(args))

	if result.Success {
		t.Fatal("expected success=false for empty task_description")
	}
	if result.Error != "task_description is required" {
		t.Errorf("error = %q, want %q", result.Error, "task_description is required")
	}
}

func TestSpawnAgent_InvalidJSON(t *testing.T) {
	saveSpawnVars(t)

	deps := testSpawnDeps()
	def := NewSpawnAgent(deps)

	result := def.Handler(context.Background(), json.RawMessage(`not json at all`))

	if result.Success {
		t.Fatal("expected success=false for invalid JSON")
	}
	if !strings.Contains(result.Error, "invalid arguments") {
		t.Errorf("error should contain 'invalid arguments', got %q", result.Error)
	}
}

func TestSpawnAgent_ContextCancelled(t *testing.T) {
	saveSpawnVars(t)

	deps := testSpawnDeps()
	def := NewSpawnAgent(deps)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	args := `{"task_id": "task-1", "task_description": "some task"}`
	result := def.Handler(ctx, json.RawMessage(args))

	if result.Success {
		t.Fatal("expected success=false for cancelled context")
	}
	if !strings.Contains(result.Error, "spawn cancelled") {
		t.Errorf("error should contain 'spawn cancelled', got %q", result.Error)
	}
}

func TestSpawnAgent_WithOptionalFlags(t *testing.T) {
	saveSpawnVars(t)

	var capturedCfg subagent.WorkspaceConfig
	createWorkspaceFn = func(cfg subagent.WorkspaceConfig) (string, error) {
		capturedCfg = cfg
		return "/test/workspace/agents/task-1", nil
	}
	launchSubAgentFn = func(r *subagent.Runner, ctx context.Context, cfg subagent.RunnerConfig, ch chan<- subagent.SubAgentResult) error {
		return nil
	}

	deps := testSpawnDeps()
	def := NewSpawnAgent(deps)

	args := `{"task_id": "task-1", "task_description": "some task", "include_heartbeat": true, "include_skills": true}`
	result := def.Handler(context.Background(), json.RawMessage(args))

	if !result.Success {
		t.Fatalf("expected success=true, got false, error: %s", result.Error)
	}
	if !capturedCfg.IncludeHeartbeat {
		t.Error("expected IncludeHeartbeat=true")
	}
	if !capturedCfg.IncludeSkills {
		t.Error("expected IncludeSkills=true")
	}
}

func TestSpawnAgent_DefaultOptionalFlags(t *testing.T) {
	saveSpawnVars(t)

	var capturedCfg subagent.WorkspaceConfig
	createWorkspaceFn = func(cfg subagent.WorkspaceConfig) (string, error) {
		capturedCfg = cfg
		return "/test/workspace/agents/task-1", nil
	}
	launchSubAgentFn = func(r *subagent.Runner, ctx context.Context, cfg subagent.RunnerConfig, ch chan<- subagent.SubAgentResult) error {
		return nil
	}

	deps := testSpawnDeps()
	def := NewSpawnAgent(deps)

	args := `{"task_id": "task-1", "task_description": "some task"}`
	result := def.Handler(context.Background(), json.RawMessage(args))

	if !result.Success {
		t.Fatalf("expected success=true, got false, error: %s", result.Error)
	}
	if capturedCfg.IncludeHeartbeat {
		t.Error("expected IncludeHeartbeat=false by default")
	}
	if capturedCfg.IncludeSkills {
		t.Error("expected IncludeSkills=false by default")
	}
}
