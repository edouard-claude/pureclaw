package tool

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/edouard/pureclaw/internal/llm"
)

// ToolResult is the structured response from tool execution.
type ToolResult struct {
	Success bool   `json:"success"`
	Output  string `json:"output"`
	Error   string `json:"error"`
}

// Handler is the function signature for tool execution.
type Handler func(ctx context.Context, args json.RawMessage) ToolResult

// Definition describes a tool: its LLM schema and execution handler.
type Definition struct {
	Name        string
	Description string
	Parameters  any // JSON Schema object for function calling
	Handler     Handler
}

// Registry holds registered tools and dispatches execution.
type Registry struct {
	tools map[string]Definition
	order []string // preserves registration order for deterministic output
}

// NewRegistry creates a new empty tool registry.
func NewRegistry() *Registry {
	slog.Info("registry created", "component", "tool", "operation", "registry")
	return &Registry{tools: make(map[string]Definition)}
}

// Register adds a tool definition to the registry.
// If a tool with the same name already exists, it is replaced without duplicating the order entry.
func (r *Registry) Register(def Definition) {
	if _, exists := r.tools[def.Name]; !exists {
		r.order = append(r.order, def.Name)
	}
	r.tools[def.Name] = def
	slog.Info("tool registered", "component", "tool", "operation", "registry", "tool_name", def.Name)
}

// Execute dispatches a tool call by name and returns the result.
func (r *Registry) Execute(ctx context.Context, name string, args json.RawMessage) ToolResult {
	def, ok := r.tools[name]
	if !ok {
		slog.Warn("unknown tool requested",
			"component", "tool",
			"operation", "execute",
			"tool_name", name,
		)
		return ToolResult{Success: false, Error: "unknown tool: " + name}
	}
	slog.Info("executing tool",
		"component", "tool",
		"operation", "execute",
		"tool_name", name,
	)
	result := def.Handler(ctx, args)
	slog.Info("tool execution completed",
		"component", "tool",
		"operation", "execute",
		"tool_name", name,
		"success", result.Success,
	)
	return result
}

// Definitions returns LLM-compatible tool definitions for function calling.
func (r *Registry) Definitions() []llm.Tool {
	defs := make([]llm.Tool, 0, len(r.order))
	for _, name := range r.order {
		def := r.tools[name]
		defs = append(defs, llm.Tool{
			Type: "function",
			Function: llm.ToolFunction{
				Name:        def.Name,
				Description: def.Description,
				Parameters:  def.Parameters,
			},
		})
	}
	return defs
}
