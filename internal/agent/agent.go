package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/edouard/pureclaw/internal/llm"
	"github.com/edouard/pureclaw/internal/memory"
	"github.com/edouard/pureclaw/internal/telegram"
	"github.com/edouard/pureclaw/internal/tool"
	"github.com/edouard/pureclaw/internal/workspace"
)

const maxToolRounds = 10

// Replaceable for testing.
var agentWorkspaceLoadFn = workspace.Load

// LLMClient abstracts the LLM provider for testability.
type LLMClient interface {
	ChatCompletionWithRetry(ctx context.Context, messages []llm.Message, tools []llm.Tool) (*llm.ChatResponse, error)
}

// Sender abstracts the Telegram message sender for testability.
type Sender interface {
	Send(ctx context.Context, chatID int64, text string) error
}

// MemoryWriter abstracts the memory persistence layer for testability.
type MemoryWriter interface {
	Write(ctx context.Context, source, content string) error
}

// MemorySearcher abstracts memory search and temporal reading capabilities.
type MemorySearcher interface {
	Search(ctx context.Context, keyword string, start, end time.Time) ([]memory.SearchResult, error)
	ReadRange(ctx context.Context, start, end time.Time) ([]memory.SearchResult, error)
}

// ToolExecutor abstracts the tool registry for testability.
type ToolExecutor interface {
	Execute(ctx context.Context, name string, args json.RawMessage) tool.ToolResult
	Definitions() []llm.Tool
}

// HeartbeatExecutor abstracts the heartbeat execution for testability.
type HeartbeatExecutor interface {
	Execute(ctx context.Context, heartbeatContent string) error
}

// NewAgentConfig holds all dependencies for Agent construction.
type NewAgentConfig struct {
	Workspace      *workspace.Workspace
	LLM            LLMClient
	Sender         Sender
	Memory         MemoryWriter
	MemorySearcher MemorySearcher
	ToolExecutor   ToolExecutor
	FileChanges    <-chan struct{}
	HeartbeatTick  <-chan time.Time
	Heartbeat      HeartbeatExecutor
}

// Agent orchestrates the event loop: receives messages, calls LLM, sends responses.
type Agent struct {
	workspace      *workspace.Workspace
	llm            LLMClient
	sender         Sender
	memory         MemoryWriter
	memorySearcher MemorySearcher
	toolExecutor   ToolExecutor
	fileChanges    <-chan struct{}
	heartbeatTick  <-chan time.Time
	heartbeat      HeartbeatExecutor
	history        []llm.Message
}

// New creates a new Agent with the given dependencies.
func New(cfg NewAgentConfig) *Agent {
	return &Agent{
		workspace:      cfg.Workspace,
		llm:            cfg.LLM,
		sender:         cfg.Sender,
		memory:         cfg.Memory,
		memorySearcher: cfg.MemorySearcher,
		toolExecutor:   cfg.ToolExecutor,
		fileChanges:    cfg.FileChanges,
		heartbeatTick:  cfg.HeartbeatTick,
		heartbeat:      cfg.Heartbeat,
	}
}

// Run starts the event loop, processing messages sequentially until the context is cancelled.
func (a *Agent) Run(ctx context.Context, messages <-chan telegram.TelegramMessage) error {
	slog.Info("event loop started", "component", "agent", "operation", "run")

	if err := a.runIntrospectionIfNeeded(ctx); err != nil {
		slog.Warn("introspection failed",
			"component", "agent",
			"operation", "introspection",
			"error", err,
		)
	}

	for {
		select {
		case <-ctx.Done():
			slog.Info("event loop stopped", "component", "agent", "operation", "run")
			return nil
		case msg := <-messages:
			a.handleMessage(ctx, msg)
		case <-a.fileChanges:
			a.handleFileChange(ctx)
		case <-a.heartbeatTick:
			a.handleHeartbeat(ctx)
		}
	}
}

// handleMessage processes a single incoming Telegram message through the LLM pipeline.
func (a *Agent) handleMessage(ctx context.Context, msg telegram.TelegramMessage) {
	// Skip zero-value messages (closed channel).
	if msg.Message.Text == "" && msg.Message.Chat.ID == 0 {
		slog.Debug("skipping empty message", "component", "agent", "operation", "handle_message")
		return
	}

	slog.Info("processing message",
		"component", "agent",
		"operation", "handle_message",
		"chat_id", msg.Message.Chat.ID,
	)

	a.logMemory(ctx, "owner", msg.Message.Text)

	msgs := a.buildMessages(msg.Message.Text)
	tools := a.toolDefinitions()

	var resp *llm.ChatResponse
	var err error

	for round := range maxToolRounds {
		resp, err = a.llm.ChatCompletionWithRetry(ctx, msgs, tools)
		if err != nil {
			slog.Error("LLM call failed",
				"component", "agent",
				"operation", "handle_message",
				"error", err,
			)
			return
		}

		if len(resp.Choices) == 0 {
			slog.Error("LLM returned no choices",
				"component", "agent",
				"operation", "handle_message",
			)
			return
		}

		if !llm.HasToolCalls(&resp.Choices[0]) {
			break
		}

		if a.toolExecutor == nil {
			slog.Warn("LLM returned tool calls but no executor configured",
				"component", "agent",
				"operation", "handle_message",
			)
			return
		}

		toolMsgs := a.executeToolCalls(ctx, resp.Choices[0].Message)
		msgs = append(msgs, resp.Choices[0].Message)
		msgs = append(msgs, toolMsgs...)

		slog.Info("tool round completed",
			"component", "agent",
			"operation", "handle_message",
			"round", round+1,
			"tool_calls", len(resp.Choices[0].Message.ToolCalls),
		)
	}

	// Check if loop exhausted without a text response.
	if llm.HasToolCalls(&resp.Choices[0]) {
		slog.Warn("max tool rounds exceeded without final response",
			"component", "agent",
			"operation", "handle_message",
			"max_rounds", maxToolRounds,
		)
		return
	}

	content := resp.Choices[0].Message.Content
	agentResp, err := llm.ParseAgentResponse(content)
	if err != nil {
		slog.Error("failed to parse agent response",
			"component", "agent",
			"operation", "handle_message",
			"error", err,
		)
		return
	}

	switch agentResp.Type {
	case "message":
		if err := a.sender.Send(ctx, msg.Message.Chat.ID, agentResp.Content); err != nil {
			slog.Error("failed to send message",
				"component", "agent",
				"operation", "handle_message",
				"error", err,
			)
		}
		a.logMemory(ctx, "agent", agentResp.Content)
		a.addToHistory(msg.Message.Text, agentResp.Content)
	case "think":
		slog.Debug("think response",
			"component", "agent",
			"operation", "handle_message",
			"content", agentResp.Content,
		)
	case "noop":
		slog.Debug("noop response",
			"component", "agent",
			"operation", "handle_message",
		)
	}
}

// executeToolCalls runs each tool call and returns tool result messages.
func (a *Agent) executeToolCalls(ctx context.Context, assistantMsg llm.Message) []llm.Message {
	var toolMsgs []llm.Message
	for _, tc := range assistantMsg.ToolCalls {
		result := a.toolExecutor.Execute(ctx, tc.Function.Name, json.RawMessage(tc.Function.Arguments))

		resultJSON, _ := json.Marshal(result)

		toolMsgs = append(toolMsgs, llm.Message{
			Role:       "tool",
			Content:    string(resultJSON),
			Name:       tc.Function.Name,
			ToolCallID: tc.ID,
		})

		slog.Info("tool executed",
			"component", "agent",
			"operation", "execute_tool",
			"tool_name", tc.Function.Name,
			"tool_call_id", tc.ID,
			"success", result.Success,
		)
	}
	return toolMsgs
}

// toolDefinitions returns LLM tool definitions if a tool executor is configured.
func (a *Agent) toolDefinitions() []llm.Tool {
	if a.toolExecutor == nil {
		return nil
	}
	return a.toolExecutor.Definitions()
}

// handleFileChange reloads the workspace from disk after a file change is detected.
func (a *Agent) handleFileChange(ctx context.Context) {
	slog.Info("workspace file change detected",
		"component", "agent",
		"operation", "file_change",
	)

	newWS, err := agentWorkspaceLoadFn(a.workspace.Root)
	if err != nil {
		slog.Error("workspace reload failed on file change",
			"component", "agent",
			"operation", "file_change",
			"error", err,
		)
		return
	}

	*a.workspace = *newWS

	slog.Info("workspace hot-reloaded",
		"component", "agent",
		"operation", "file_change",
		"skills", len(a.workspace.Skills),
	)
}

// handleHeartbeat runs one heartbeat cycle using the configured executor.
func (a *Agent) handleHeartbeat(ctx context.Context) {
	if a.heartbeat == nil {
		slog.Warn("heartbeat tick received but no executor configured",
			"component", "agent",
			"operation", "heartbeat",
		)
		return
	}

	heartbeatContent := a.workspace.HeartbeatMD
	if heartbeatContent == "" {
		slog.Warn("heartbeat tick received but HEARTBEAT.md is empty",
			"component", "agent",
			"operation", "heartbeat",
		)
		return
	}

	slog.Info("heartbeat cycle starting",
		"component", "agent",
		"operation", "heartbeat",
	)

	if err := a.heartbeat.Execute(ctx, heartbeatContent); err != nil {
		slog.Error("heartbeat execution failed",
			"component", "agent",
			"operation", "heartbeat",
			"error", err,
		)
	}
}

func (a *Agent) logMemory(ctx context.Context, source, content string) {
	if a.memory == nil {
		return
	}
	if err := a.memory.Write(ctx, source, content); err != nil {
		slog.Error("failed to write memory",
			"component", "agent",
			"operation", "log_memory",
			"source", source,
			"error", err,
		)
	}
}
