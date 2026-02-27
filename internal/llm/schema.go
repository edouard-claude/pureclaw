package llm

import (
	"encoding/json"
	"fmt"
)

// ChatRequest represents a Mistral chat completion API request.
type ChatRequest struct {
	Model          string          `json:"model"`
	Messages       []Message       `json:"messages"`
	Temperature    *float64        `json:"temperature,omitempty"`
	MaxTokens      *int            `json:"max_tokens,omitempty"`
	ResponseFormat *ResponseFormat `json:"response_format,omitempty"`
	Tools          []Tool          `json:"tools,omitempty"`
	ToolChoice     string          `json:"tool_choice,omitempty"`
}

// Message represents a single message in a chat conversation.
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	Name       string     `json:"name,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// ResponseFormat specifies the desired response format from the API.
type ResponseFormat struct {
	Type string `json:"type"`
}

// Tool represents a tool available for the model to call.
type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

// ToolFunction describes a function that can be called by the model.
type ToolFunction struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters"`
}

// ToolCall represents a tool invocation requested by the model.
type ToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function ToolCallFunction `json:"function"`
}

// ToolCallFunction contains the name and arguments of a tool call.
type ToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ChatResponse represents a Mistral chat completion API response.
type ChatResponse struct {
	ID      string   `json:"id"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

// Choice represents a single completion choice in the response.
type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

// Usage contains token usage statistics for the request.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// AgentResponse is the typed JSON envelope parsed from LLM output content.
type AgentResponse struct {
	Type    string `json:"type"`
	Content string `json:"content"`
}

// ParseAgentResponse parses a JSON content string into an AgentResponse
// and validates that the type field is one of message, think, or noop.
func ParseAgentResponse(content string) (*AgentResponse, error) {
	var resp AgentResponse
	if err := json.Unmarshal([]byte(content), &resp); err != nil {
		return nil, fmt.Errorf("llm: parse agent response: %w", err)
	}
	switch resp.Type {
	case "message", "think", "noop":
		return &resp, nil
	default:
		return nil, fmt.Errorf("llm: parse agent response: unknown type %q", resp.Type)
	}
}
