package llm

import (
	"encoding/json"
	"strings"
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
// Supports "text", "json_object", and "json_schema" types.
type ResponseFormat struct {
	Type       string      `json:"type"`
	JSONSchema *JSONSchema `json:"json_schema,omitempty"`
}

// JSONSchema defines a strict JSON schema for structured output.
type JSONSchema struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Schema      json.RawMessage `json:"schema"`
	Strict      bool            `json:"strict,omitempty"`
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

// ParseAgentResponse parses an LLM content string into an AgentResponse.
// It uses a 3-step strategy to handle models that don't always return strict JSON:
//  1. Direct JSON parse (ideal case with json_schema enforcement)
//  2. Extract embedded JSON from surrounding text (model added preamble/suffix)
//  3. Fallback: wrap raw text as a "message" type response
func ParseAgentResponse(content string) (*AgentResponse, error) {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return &AgentResponse{Type: "noop", Content: ""}, nil
	}

	// Step 1: try direct JSON parse.
	if resp, ok := tryParseAgent(trimmed); ok {
		return resp, nil
	}

	// Step 2: try to extract a JSON object from the text.
	if start := strings.Index(trimmed, "{"); start >= 0 {
		if end := strings.LastIndex(trimmed, "}"); end > start {
			if resp, ok := tryParseAgent(trimmed[start : end+1]); ok {
				return resp, nil
			}
		}
	}

	// Step 3: fallback — treat raw text as a message.
	return &AgentResponse{Type: "message", Content: trimmed}, nil
}

// tryParseAgent attempts to unmarshal s as an AgentResponse and validates the type.
func tryParseAgent(s string) (*AgentResponse, bool) {
	var resp AgentResponse
	if err := json.Unmarshal([]byte(s), &resp); err != nil {
		return nil, false
	}
	switch resp.Type {
	case "message", "think", "noop":
		return &resp, true
	default:
		return nil, false
	}
}
