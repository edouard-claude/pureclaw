package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/edouard/pureclaw/internal/platform"
)

// retryFn is a package-level variable for testability.
var retryFn = platform.Retry

// agentResponseSchema is the JSON Schema for structured agent responses,
// enforced via Mistral's json_schema response_format with strict: true.
var agentResponseSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"type": {"type": "string", "enum": ["message", "think", "noop"]},
		"content": {"type": "string"}
	},
	"required": ["type", "content"],
	"additionalProperties": false
}`)

// ChatCompletion sends a chat completion request to the Mistral API.
// When tools are provided, response_format is omitted (Mistral rejects structured output + tools).
// When no tools are provided, response_format uses json_schema with strict enforcement.
func (c *Client) ChatCompletion(ctx context.Context, messages []Message, tools []Tool) (*ChatResponse, error) {
	slog.Debug("chat completion request", "component", "llm", "operation", "chat_completion", "model", c.model)

	req := ChatRequest{
		Model:    c.model,
		Messages: messages,
	}

	if len(tools) > 0 {
		req.Tools = tools
		req.ToolChoice = "auto"
	} else {
		req.ResponseFormat = &ResponseFormat{
			Type: "json_schema",
			JSONSchema: &JSONSchema{
				Name:        "agent_response",
				Description: "Agent response with type and content fields",
				Schema:      agentResponseSchema,
				Strict:      true,
			},
		}
	}

	data, err := c.doPost(ctx, "chat/completions", req)
	if err != nil {
		return nil, err
	}

	var resp ChatResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("llm: chat/completions: unmarshal response: %w", err)
	}

	return &resp, nil
}

// ChatCompletionWithRetry wraps ChatCompletion with retry on transient HTTP errors.
// It retries up to 3 times with exponential backoff starting at 1s.
// Note: ParseAgentResponse handles non-JSON text gracefully via fallback,
// so JSON parse errors are NOT retried (they would produce the same result).
func (c *Client) ChatCompletionWithRetry(ctx context.Context, messages []Message, tools []Tool) (*ChatResponse, error) {
	var chatResp *ChatResponse
	var nonRetryErr error
	err := retryFn(ctx, 3, 1*time.Second, func() error {
		resp, err := c.ChatCompletion(ctx, messages, tools)
		if err != nil {
			var he *httpError
			if errors.As(err, &he) && !he.IsRetryable() {
				nonRetryErr = err
				return nil
			}
			return err
		}

		chatResp = resp
		return nil
	})
	if nonRetryErr != nil {
		return nil, nonRetryErr
	}
	if err != nil {
		return nil, err
	}
	return chatResp, nil
}

// HasToolCalls checks if a choice contains tool call requests.
func HasToolCalls(choice *Choice) bool {
	return choice.FinishReason == "tool_calls" && len(choice.Message.ToolCalls) > 0
}
