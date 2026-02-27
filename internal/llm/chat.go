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

// ChatCompletion sends a chat completion request to the Mistral API
// with response_format set to json_object.
func (c *Client) ChatCompletion(ctx context.Context, messages []Message, tools []Tool) (*ChatResponse, error) {
	slog.Debug("chat completion request", "component", "llm", "operation", "chat_completion", "model", c.model)

	req := ChatRequest{
		Model:          c.model,
		Messages:       messages,
		ResponseFormat: &ResponseFormat{Type: "json_object"},
	}

	if len(tools) > 0 {
		req.Tools = tools
		req.ToolChoice = "auto"
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

// ChatCompletionWithRetry wraps ChatCompletion with retry on JSON parse failure.
// It retries up to 3 times with exponential backoff starting at 1s.
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

		// Validate: if text response (not tool call), parse as AgentResponse
		if len(resp.Choices) > 0 && resp.Choices[0].FinishReason != "tool_calls" {
			content := resp.Choices[0].Message.Content
			if content != "" {
				if _, parseErr := ParseAgentResponse(content); parseErr != nil {
					slog.Warn("malformed LLM response, retrying",
						"component", "llm",
						"operation", "chat_completion",
						"error", parseErr,
					)
					return parseErr
				}
			}
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
