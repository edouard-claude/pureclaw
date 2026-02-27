package tool

import (
	"context"
	"encoding/json"
	"testing"
)

func TestNewRegistry(t *testing.T) {
	r := NewRegistry()
	if r == nil {
		t.Fatal("expected non-nil registry")
	}
	if len(r.tools) != 0 {
		t.Errorf("expected 0 tools, got %d", len(r.tools))
	}
	defs := r.Definitions()
	if len(defs) != 0 {
		t.Errorf("expected 0 definitions, got %d", len(defs))
	}
}

func TestRegister(t *testing.T) {
	r := NewRegistry()
	r.Register(Definition{
		Name:        "test_tool",
		Description: "A test tool",
		Parameters:  map[string]any{"type": "object"},
		Handler:     func(ctx context.Context, args json.RawMessage) ToolResult { return ToolResult{} },
	})

	defs := r.Definitions()
	if len(defs) != 1 {
		t.Fatalf("expected 1 definition, got %d", len(defs))
	}
	if defs[0].Function.Name != "test_tool" {
		t.Errorf("expected name %q, got %q", "test_tool", defs[0].Function.Name)
	}
	if defs[0].Function.Description != "A test tool" {
		t.Errorf("expected description %q, got %q", "A test tool", defs[0].Function.Description)
	}
	if defs[0].Type != "function" {
		t.Errorf("expected type %q, got %q", "function", defs[0].Type)
	}
}

func TestRegisterMultiple(t *testing.T) {
	r := NewRegistry()
	names := []string{"tool_a", "tool_b", "tool_c"}
	for _, name := range names {
		r.Register(Definition{
			Name:    name,
			Handler: func(ctx context.Context, args json.RawMessage) ToolResult { return ToolResult{} },
		})
	}

	defs := r.Definitions()
	if len(defs) != 3 {
		t.Fatalf("expected 3 definitions, got %d", len(defs))
	}
	// Verify registration order is preserved.
	for i, name := range names {
		if defs[i].Function.Name != name {
			t.Errorf("definitions[%d]: expected name %q, got %q", i, name, defs[i].Function.Name)
		}
	}
}

func TestExecute_Success(t *testing.T) {
	r := NewRegistry()
	r.Register(Definition{
		Name: "echo",
		Handler: func(ctx context.Context, args json.RawMessage) ToolResult {
			return ToolResult{Success: true, Output: "echoed"}
		},
	})

	result := r.Execute(context.Background(), "echo", json.RawMessage(`{}`))
	if !result.Success {
		t.Errorf("expected success=true, got false")
	}
	if result.Output != "echoed" {
		t.Errorf("expected output %q, got %q", "echoed", result.Output)
	}
}

func TestExecute_UnknownTool(t *testing.T) {
	r := NewRegistry()
	result := r.Execute(context.Background(), "nonexistent", json.RawMessage(`{}`))
	if result.Success {
		t.Errorf("expected success=false for unknown tool")
	}
	if result.Error != "unknown tool: nonexistent" {
		t.Errorf("expected error %q, got %q", "unknown tool: nonexistent", result.Error)
	}
}

func TestDefinitions_WithSchema(t *testing.T) {
	r := NewRegistry()
	params := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
		},
	}
	r.Register(Definition{
		Name:        "greet",
		Description: "Greet someone",
		Parameters:  params,
		Handler:     func(ctx context.Context, args json.RawMessage) ToolResult { return ToolResult{} },
	})

	defs := r.Definitions()
	if len(defs) != 1 {
		t.Fatalf("expected 1 definition, got %d", len(defs))
	}

	// Verify parameters are passed through.
	p, ok := defs[0].Function.Parameters.(map[string]any)
	if !ok {
		t.Fatalf("expected Parameters to be map[string]any")
	}
	if p["type"] != "object" {
		t.Errorf("expected Parameters.type = %q, got %q", "object", p["type"])
	}
}

func TestRegister_DuplicateNameReplaces(t *testing.T) {
	r := NewRegistry()
	r.Register(Definition{
		Name:        "tool_a",
		Description: "original",
		Handler:     func(ctx context.Context, args json.RawMessage) ToolResult { return ToolResult{Output: "v1"} },
	})
	r.Register(Definition{
		Name:        "tool_a",
		Description: "replaced",
		Handler:     func(ctx context.Context, args json.RawMessage) ToolResult { return ToolResult{Output: "v2"} },
	})

	// Should have exactly 1 definition, not 2.
	defs := r.Definitions()
	if len(defs) != 1 {
		t.Fatalf("expected 1 definition after duplicate registration, got %d", len(defs))
	}
	if defs[0].Function.Description != "replaced" {
		t.Errorf("expected description %q, got %q", "replaced", defs[0].Function.Description)
	}

	// Execute should use the replacement handler.
	result := r.Execute(context.Background(), "tool_a", json.RawMessage(`{}`))
	if result.Output != "v2" {
		t.Errorf("expected output %q from replacement, got %q", "v2", result.Output)
	}
}

func TestDefinitions_Empty(t *testing.T) {
	r := NewRegistry()
	defs := r.Definitions()
	if defs == nil {
		t.Fatal("expected non-nil slice")
	}
	if len(defs) != 0 {
		t.Errorf("expected empty slice, got %d elements", len(defs))
	}
}
