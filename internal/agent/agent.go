package agent

import (
	"context"
	"log/slog"

	"github.com/edouard/pureclaw/internal/llm"
	"github.com/edouard/pureclaw/internal/telegram"
	"github.com/edouard/pureclaw/internal/workspace"
)

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

// NewAgentConfig holds all dependencies for Agent construction.
type NewAgentConfig struct {
	Workspace *workspace.Workspace
	LLM       LLMClient
	Sender    Sender
	Memory    MemoryWriter
}

// Agent orchestrates the event loop: receives messages, calls LLM, sends responses.
type Agent struct {
	workspace *workspace.Workspace
	llm       LLMClient
	sender    Sender
	memory    MemoryWriter
	history   []llm.Message
}

// New creates a new Agent with the given dependencies.
func New(cfg NewAgentConfig) *Agent {
	return &Agent{
		workspace: cfg.Workspace,
		llm:       cfg.LLM,
		sender:    cfg.Sender,
		memory:    cfg.Memory,
	}
}

// Run starts the event loop, processing messages sequentially until the context is cancelled.
func (a *Agent) Run(ctx context.Context, messages <-chan telegram.TelegramMessage) error {
	slog.Info("event loop started", "component", "agent", "operation", "run")
	for {
		select {
		case <-ctx.Done():
			slog.Info("event loop stopped", "component", "agent", "operation", "run")
			return nil
		case msg := <-messages:
			a.handleMessage(ctx, msg)
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
	resp, err := a.llm.ChatCompletionWithRetry(ctx, msgs, nil)
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

	if llm.HasToolCalls(&resp.Choices[0]) {
		slog.Warn("LLM returned tool calls but tools are not supported yet",
			"component", "agent",
			"operation", "handle_message",
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
	default:
		slog.Warn("unknown response type",
			"component", "agent",
			"operation", "handle_message",
			"type", agentResp.Type,
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
