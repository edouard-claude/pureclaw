package tool

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/edouard/pureclaw/internal/workspace"
)

func TestReloadWorkspace_Success(t *testing.T) {
	original := workspaceLoadFn
	workspaceLoadFn = func(root string) (*workspace.Workspace, error) {
		return &workspace.Workspace{
			Root:    root,
			AgentMD: "new agent",
			SoulMD:  "new soul",
		}, nil
	}
	defer func() { workspaceLoadFn = original }()

	ws := &workspace.Workspace{
		Root:    "/test/workspace",
		AgentMD: "old agent",
		SoulMD:  "old soul",
	}
	def := NewReloadWorkspace(ws)
	result := def.Handler(context.Background(), json.RawMessage(`{}`))

	if !result.Success {
		t.Fatalf("expected success=true, got false, error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "workspace reloaded") {
		t.Errorf("expected output to contain 'workspace reloaded', got %q", result.Output)
	}
	if !strings.Contains(result.Output, "/test/workspace") {
		t.Errorf("expected output to contain workspace root, got %q", result.Output)
	}
}

func TestReloadWorkspace_UpdatesFields(t *testing.T) {
	original := workspaceLoadFn
	workspaceLoadFn = func(root string) (*workspace.Workspace, error) {
		return &workspace.Workspace{
			Root:        root,
			AgentMD:     "updated agent",
			SoulMD:      "updated soul",
			HeartbeatMD: "updated heartbeat",
			Skills: []workspace.Skill{
				{Name: "greet", Content: "greeting skill"},
			},
		}, nil
	}
	defer func() { workspaceLoadFn = original }()

	ws := &workspace.Workspace{
		Root:    "/test/workspace",
		AgentMD: "old agent",
		SoulMD:  "old soul",
	}
	def := NewReloadWorkspace(ws)
	def.Handler(context.Background(), json.RawMessage(`{}`))

	if ws.AgentMD != "updated agent" {
		t.Errorf("expected AgentMD %q, got %q", "updated agent", ws.AgentMD)
	}
	if ws.SoulMD != "updated soul" {
		t.Errorf("expected SoulMD %q, got %q", "updated soul", ws.SoulMD)
	}
	if ws.HeartbeatMD != "updated heartbeat" {
		t.Errorf("expected HeartbeatMD %q, got %q", "updated heartbeat", ws.HeartbeatMD)
	}
	if len(ws.Skills) != 1 || ws.Skills[0].Name != "greet" {
		t.Errorf("expected 1 skill named 'greet', got %+v", ws.Skills)
	}
}

func TestReloadWorkspace_PreservesRoot(t *testing.T) {
	original := workspaceLoadFn
	workspaceLoadFn = func(root string) (*workspace.Workspace, error) {
		return &workspace.Workspace{
			Root:    root,
			AgentMD: "new",
			SoulMD:  "new",
		}, nil
	}
	defer func() { workspaceLoadFn = original }()

	ws := &workspace.Workspace{
		Root:    "/original/root",
		AgentMD: "old",
		SoulMD:  "old",
	}
	def := NewReloadWorkspace(ws)
	def.Handler(context.Background(), json.RawMessage(`{}`))

	if ws.Root != "/original/root" {
		t.Errorf("expected Root %q preserved, got %q", "/original/root", ws.Root)
	}
}

func TestReloadWorkspace_PreservesOnError(t *testing.T) {
	original := workspaceLoadFn
	workspaceLoadFn = func(root string) (*workspace.Workspace, error) {
		return nil, errors.New("AGENT.md missing")
	}
	defer func() { workspaceLoadFn = original }()

	ws := &workspace.Workspace{
		Root:    "/test/workspace",
		AgentMD: "original agent",
		SoulMD:  "original soul",
	}
	def := NewReloadWorkspace(ws)
	result := def.Handler(context.Background(), json.RawMessage(`{}`))

	if result.Success {
		t.Fatal("expected success=false for reload error")
	}
	if !strings.Contains(result.Error, "workspace reload failed") {
		t.Errorf("expected error to contain 'workspace reload failed', got %q", result.Error)
	}
	// Verify old state preserved.
	if ws.AgentMD != "original agent" {
		t.Errorf("expected AgentMD preserved as %q, got %q", "original agent", ws.AgentMD)
	}
	if ws.SoulMD != "original soul" {
		t.Errorf("expected SoulMD preserved as %q, got %q", "original soul", ws.SoulMD)
	}
}

func TestReloadWorkspace_WithSkills(t *testing.T) {
	original := workspaceLoadFn
	workspaceLoadFn = func(root string) (*workspace.Workspace, error) {
		return &workspace.Workspace{
			Root:    root,
			AgentMD: "agent",
			SoulMD:  "soul",
			Skills: []workspace.Skill{
				{Name: "weather", Content: "weather skill"},
				{Name: "reminder", Content: "reminder skill"},
			},
		}, nil
	}
	defer func() { workspaceLoadFn = original }()

	ws := &workspace.Workspace{Root: "/test/workspace"}
	def := NewReloadWorkspace(ws)
	result := def.Handler(context.Background(), json.RawMessage(`{}`))

	if !result.Success {
		t.Fatalf("expected success=true, got false, error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "2 skill(s)") {
		t.Errorf("expected output to mention '2 skill(s)', got %q", result.Output)
	}
}

func TestReloadWorkspace_WithoutHeartbeat(t *testing.T) {
	original := workspaceLoadFn
	workspaceLoadFn = func(root string) (*workspace.Workspace, error) {
		return &workspace.Workspace{
			Root:    root,
			AgentMD: "agent",
			SoulMD:  "soul",
		}, nil
	}
	defer func() { workspaceLoadFn = original }()

	ws := &workspace.Workspace{Root: "/test/workspace"}
	def := NewReloadWorkspace(ws)
	result := def.Handler(context.Background(), json.RawMessage(`{}`))

	if !result.Success {
		t.Fatalf("expected success=true, got false, error: %s", result.Error)
	}
	if strings.Contains(result.Output, "HEARTBEAT") {
		t.Errorf("expected output to omit HEARTBEAT.md when empty, got %q", result.Output)
	}
}

func TestReloadWorkspace_InvalidJSONArgs(t *testing.T) {
	original := workspaceLoadFn
	workspaceLoadFn = func(root string) (*workspace.Workspace, error) {
		return &workspace.Workspace{
			Root:    root,
			AgentMD: "agent",
			SoulMD:  "soul",
		}, nil
	}
	defer func() { workspaceLoadFn = original }()

	ws := &workspace.Workspace{Root: "/test/workspace"}
	def := NewReloadWorkspace(ws)

	// Tool takes no arguments — invalid JSON should still succeed because args are ignored.
	result := def.Handler(context.Background(), json.RawMessage(`{"unexpected": "field"}`))
	if !result.Success {
		t.Fatalf("expected success=true with unexpected args, got false, error: %s", result.Error)
	}

	// Completely invalid JSON should also succeed.
	result = def.Handler(context.Background(), json.RawMessage(`not json at all`))
	if !result.Success {
		t.Fatalf("expected success=true with invalid JSON, got false, error: %s", result.Error)
	}
}

func TestReloadWorkspace_CancelledContext(t *testing.T) {
	ws := &workspace.Workspace{Root: "/test/workspace"}
	def := NewReloadWorkspace(ws)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	result := def.Handler(ctx, json.RawMessage(`{}`))
	if result.Success {
		t.Fatal("expected success=false for cancelled context")
	}
	if !strings.Contains(result.Error, "workspace reload cancelled") {
		t.Errorf("expected error to mention cancellation, got %q", result.Error)
	}
}

func TestReloadWorkspace_Definition(t *testing.T) {
	ws := &workspace.Workspace{Root: "/test"}
	def := NewReloadWorkspace(ws)

	if def.Name != "reload_workspace" {
		t.Errorf("expected name %q, got %q", "reload_workspace", def.Name)
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
