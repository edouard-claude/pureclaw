package heartbeat

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/edouard/pureclaw/internal/llm"
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

// Executor evaluates a HEARTBEAT.md checklist via LLM and alerts owners when action is needed.
type Executor struct {
	llm      LLMClient
	sender   Sender
	memory   MemoryWriter
	ownerIDs []int64
}

// NewExecutor creates a new heartbeat Executor with the given dependencies.
func NewExecutor(llm LLMClient, sender Sender, memory MemoryWriter, ownerIDs []int64) *Executor {
	return &Executor{
		llm:      llm,
		sender:   sender,
		memory:   memory,
		ownerIDs: ownerIDs,
	}
}

const systemPromptTemplate = `You are a monitoring agent running a periodic heartbeat check.

Your task: evaluate the following checklist and determine if any action is needed.

== HEARTBEAT CHECKLIST ==
%s
== END CHECKLIST ==

Respond with structured JSON:
- If action is needed: {"type": "message", "content": "describe the situation and what needs attention"}
- If everything is fine: {"type": "noop", "content": "brief status summary"}

Be concise. Only alert when genuine action is required.`

// Execute runs one heartbeat cycle: sends the checklist to the LLM, parses the response,
// and either alerts all owners or stays silent.
func (e *Executor) Execute(ctx context.Context, heartbeatContent string) error {
	if heartbeatContent == "" {
		slog.Warn("heartbeat content is empty, skipping",
			"component", "heartbeat",
			"operation", "execute",
		)
		return nil
	}

	slog.Info("executing heartbeat checklist",
		"component", "heartbeat",
		"operation", "execute",
	)

	messages := []llm.Message{
		{Role: "system", Content: fmt.Sprintf(systemPromptTemplate, heartbeatContent)},
		{Role: "user", Content: "Run the heartbeat checklist now. Evaluate each item and respond."},
	}

	resp, err := e.llm.ChatCompletionWithRetry(ctx, messages, nil)
	if err != nil {
		slog.Error("heartbeat LLM call failed",
			"component", "heartbeat",
			"operation", "execute",
			"error", err,
		)
		return fmt.Errorf("heartbeat: llm call: %w", err)
	}

	if len(resp.Choices) == 0 {
		slog.Error("heartbeat LLM returned no choices",
			"component", "heartbeat",
			"operation", "execute",
		)
		return fmt.Errorf("heartbeat: llm returned no choices")
	}

	agentResp, err := llm.ParseAgentResponse(resp.Choices[0].Message.Content)
	if err != nil {
		slog.Error("heartbeat response parse failed",
			"component", "heartbeat",
			"operation", "execute",
			"error", err,
		)
		return fmt.Errorf("heartbeat: parse response: %w", err)
	}

	switch agentResp.Type {
	case "message":
		e.alertOwners(ctx, agentResp.Content)
		e.logMemory(ctx, fmt.Sprintf("Heartbeat alert: %s", agentResp.Content))
	case "noop":
		slog.Info("heartbeat: all clear",
			"component", "heartbeat",
			"operation", "silent",
		)
		e.logMemory(ctx, "Heartbeat: all clear, no action needed")
	case "think":
		slog.Info("heartbeat: think response (treated as silent)",
			"component", "heartbeat",
			"operation", "silent",
		)
		e.logMemory(ctx, "Heartbeat: internal reasoning, no action needed")
	}

	return nil
}

// alertOwners sends a notification to ALL owner IDs. Sender errors are logged but not fatal.
func (e *Executor) alertOwners(ctx context.Context, content string) {
	for _, id := range e.ownerIDs {
		slog.Info("sending heartbeat alert",
			"component", "heartbeat",
			"operation", "alert",
			"chat_id", id,
		)
		if err := e.sender.Send(ctx, id, content); err != nil {
			slog.Error("heartbeat alert send failed",
				"component", "heartbeat",
				"operation", "alert",
				"chat_id", id,
				"error", err,
			)
		}
	}
}

// logMemory writes the heartbeat result to memory. Errors are logged but not fatal.
func (e *Executor) logMemory(ctx context.Context, content string) {
	if e.memory == nil {
		return
	}
	if err := e.memory.Write(ctx, "heartbeat", content); err != nil {
		slog.Error("heartbeat memory write failed",
			"component", "heartbeat",
			"operation", "execute",
			"error", err,
		)
	}
}
