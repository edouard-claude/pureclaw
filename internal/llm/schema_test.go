package llm

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestChatRequest_JSONMarshal(t *testing.T) {
	temp := 0.7
	maxTok := 4096
	req := ChatRequest{
		Model: "mistral-large-latest",
		Messages: []Message{
			{Role: "system", Content: "You are helpful."},
			{Role: "user", Content: "Hello"},
		},
		Temperature:    &temp,
		MaxTokens:      &maxTok,
		ResponseFormat: &ResponseFormat{Type: "json_object"},
		Tools: []Tool{
			{
				Type: "function",
				Function: ToolFunction{
					Name:        "exec_command",
					Description: "Execute a shell command",
					Parameters: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"command": map[string]any{"type": "string"},
						},
						"required": []string{"command"},
					},
				},
			},
		},
		ToolChoice: "auto",
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal to map: %v", err)
	}

	// Verify snake_case JSON keys
	snakeKeys := []string{"model", "messages", "temperature", "max_tokens", "response_format", "tools", "tool_choice"}
	for _, key := range snakeKeys {
		if _, ok := m[key]; !ok {
			t.Errorf("missing JSON key %q", key)
		}
	}

	// Roundtrip
	var req2 ChatRequest
	if err := json.Unmarshal(data, &req2); err != nil {
		t.Fatalf("roundtrip unmarshal: %v", err)
	}
	if req2.Model != req.Model {
		t.Errorf("Model = %q, want %q", req2.Model, req.Model)
	}
	if len(req2.Messages) != 2 {
		t.Errorf("Messages len = %d, want 2", len(req2.Messages))
	}
	if req2.ResponseFormat.Type != "json_object" {
		t.Errorf("ResponseFormat.Type = %q, want json_object", req2.ResponseFormat.Type)
	}
}

func TestChatRequest_OmitEmpty(t *testing.T) {
	req := ChatRequest{
		Model:    "test-model",
		Messages: []Message{{Role: "user", Content: "hi"}},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal to map: %v", err)
	}

	omitKeys := []string{"temperature", "max_tokens", "response_format", "tools", "tool_choice"}
	for _, key := range omitKeys {
		if _, ok := m[key]; ok {
			t.Errorf("expected key %q to be omitted", key)
		}
	}
}

func TestMessage_JSONRoundtrip(t *testing.T) {
	tests := []struct {
		name string
		msg  Message
	}{
		{
			name: "user message",
			msg:  Message{Role: "user", Content: "Hello"},
		},
		{
			name: "assistant with tool calls",
			msg: Message{
				Role: "assistant",
				ToolCalls: []ToolCall{
					{
						ID:   "abc123",
						Type: "function",
						Function: ToolCallFunction{
							Name:      "exec_command",
							Arguments: `{"command":"ls"}`,
						},
					},
				},
			},
		},
		{
			name: "tool result",
			msg: Message{
				Role:       "tool",
				Name:       "exec_command",
				Content:    `{"success":true}`,
				ToolCallID: "abc123",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.msg)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}

			var msg2 Message
			if err := json.Unmarshal(data, &msg2); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if msg2.Role != tt.msg.Role {
				t.Errorf("Role = %q, want %q", msg2.Role, tt.msg.Role)
			}
			if msg2.Content != tt.msg.Content {
				t.Errorf("Content = %q, want %q", msg2.Content, tt.msg.Content)
			}
		})
	}
}

func TestMessage_OmitEmpty(t *testing.T) {
	msg := Message{Role: "user", Content: "hi"}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal to map: %v", err)
	}

	omitKeys := []string{"name", "tool_calls", "tool_call_id"}
	for _, key := range omitKeys {
		if _, ok := m[key]; ok {
			t.Errorf("expected key %q to be omitted", key)
		}
	}
}

func TestMessage_ToolCallIDSnakeCase(t *testing.T) {
	msg := Message{Role: "tool", ToolCallID: "xyz"}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal to map: %v", err)
	}

	if _, ok := m["tool_call_id"]; !ok {
		t.Error("expected snake_case key tool_call_id")
	}
}

func TestChatResponse_JSONUnmarshal(t *testing.T) {
	raw := `{
		"id": "chat-abc123",
		"choices": [{
			"index": 0,
			"message": {
				"role": "assistant",
				"content": "{\"type\":\"message\",\"content\":\"Hello\"}"
			},
			"finish_reason": "stop"
		}],
		"usage": {
			"prompt_tokens": 95,
			"completion_tokens": 32,
			"total_tokens": 127
		}
	}`

	var resp ChatResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if resp.ID != "chat-abc123" {
		t.Errorf("ID = %q, want chat-abc123", resp.ID)
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("Choices len = %d, want 1", len(resp.Choices))
	}
	if resp.Choices[0].FinishReason != "stop" {
		t.Errorf("FinishReason = %q, want stop", resp.Choices[0].FinishReason)
	}
	if resp.Usage.TotalTokens != 127 {
		t.Errorf("TotalTokens = %d, want 127", resp.Usage.TotalTokens)
	}
}

func TestChatResponse_WithToolCalls(t *testing.T) {
	raw := `{
		"id": "chat-xyz",
		"choices": [{
			"index": 0,
			"message": {
				"role": "assistant",
				"content": "",
				"tool_calls": [{
					"id": "D681PevKs",
					"type": "function",
					"function": {
						"name": "exec_command",
						"arguments": "{\"command\":\"df -h\"}"
					}
				}]
			},
			"finish_reason": "tool_calls"
		}],
		"usage": {"prompt_tokens": 50, "completion_tokens": 20, "total_tokens": 70}
	}`

	var resp ChatResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	choice := resp.Choices[0]
	if choice.FinishReason != "tool_calls" {
		t.Errorf("FinishReason = %q, want tool_calls", choice.FinishReason)
	}
	if len(choice.Message.ToolCalls) != 1 {
		t.Fatalf("ToolCalls len = %d, want 1", len(choice.Message.ToolCalls))
	}
	tc := choice.Message.ToolCalls[0]
	if tc.ID != "D681PevKs" {
		t.Errorf("ToolCall.ID = %q, want D681PevKs", tc.ID)
	}
	if tc.Function.Name != "exec_command" {
		t.Errorf("Function.Name = %q, want exec_command", tc.Function.Name)
	}
	if tc.Function.Arguments != `{"command":"df -h"}` {
		t.Errorf("Function.Arguments = %q, want {\"command\":\"df -h\"}", tc.Function.Arguments)
	}
}

func TestToolCall_JSONSnakeCase(t *testing.T) {
	tc := ToolCall{
		ID:   "abc",
		Type: "function",
		Function: ToolCallFunction{
			Name:      "test",
			Arguments: `{}`,
		},
	}

	data, err := json.Marshal(tc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal to map: %v", err)
	}

	if _, ok := m["id"]; !ok {
		t.Error("expected key id")
	}
	if _, ok := m["type"]; !ok {
		t.Error("expected key type")
	}
	if _, ok := m["function"]; !ok {
		t.Error("expected key function")
	}
}

func TestParseAgentResponse(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    *AgentResponse
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid message",
			content: `{"type":"message","content":"Hello!"}`,
			want:    &AgentResponse{Type: "message", Content: "Hello!"},
		},
		{
			name:    "valid think",
			content: `{"type":"think","content":"Analyzing..."}`,
			want:    &AgentResponse{Type: "think", Content: "Analyzing..."},
		},
		{
			name:    "valid noop",
			content: `{"type":"noop","content":""}`,
			want:    &AgentResponse{Type: "noop", Content: ""},
		},
		{
			name:    "unknown type",
			content: `{"type":"unknown","content":"test"}`,
			wantErr: true,
			errMsg:  "unknown type",
		},
		{
			name:    "malformed JSON",
			content: `not json at all`,
			wantErr: true,
			errMsg:  "parse agent response",
		},
		{
			name:    "empty type",
			content: `{"type":"","content":"test"}`,
			wantErr: true,
			errMsg:  "unknown type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseAgentResponse(tt.content)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errMsg != "" {
					if !strings.Contains(err.Error(), tt.errMsg) {
						t.Errorf("error = %q, want to contain %q", err.Error(), tt.errMsg)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Type != tt.want.Type {
				t.Errorf("Type = %q, want %q", got.Type, tt.want.Type)
			}
			if got.Content != tt.want.Content {
				t.Errorf("Content = %q, want %q", got.Content, tt.want.Content)
			}
		})
	}
}

func TestTool_JSONMarshal(t *testing.T) {
	tool := Tool{
		Type: "function",
		Function: ToolFunction{
			Name:        "exec_command",
			Description: "Execute a shell command",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"command": map[string]any{"type": "string"},
				},
				"required": []string{"command"},
			},
		},
	}

	data, err := json.Marshal(tool)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var tool2 Tool
	if err := json.Unmarshal(data, &tool2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if tool2.Type != "function" {
		t.Errorf("Type = %q, want function", tool2.Type)
	}
	if tool2.Function.Name != "exec_command" {
		t.Errorf("Function.Name = %q, want exec_command", tool2.Function.Name)
	}
}

func TestUsage_JSONRoundtrip(t *testing.T) {
	u := Usage{PromptTokens: 95, CompletionTokens: 32, TotalTokens: 127}
	data, err := json.Marshal(u)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal to map: %v", err)
	}

	snakeKeys := []string{"prompt_tokens", "completion_tokens", "total_tokens"}
	for _, key := range snakeKeys {
		if _, ok := m[key]; !ok {
			t.Errorf("missing snake_case key %q", key)
		}
	}

	var u2 Usage
	if err := json.Unmarshal(data, &u2); err != nil {
		t.Fatalf("roundtrip unmarshal: %v", err)
	}
	if u2.PromptTokens != 95 {
		t.Errorf("PromptTokens = %d, want 95", u2.PromptTokens)
	}
}

func TestChoice_JSONSnakeCase(t *testing.T) {
	c := Choice{Index: 0, FinishReason: "stop", Message: Message{Role: "assistant"}}
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal to map: %v", err)
	}

	if _, ok := m["finish_reason"]; !ok {
		t.Error("expected snake_case key finish_reason")
	}
}

func TestResponseFormat_JSONMarshal(t *testing.T) {
	rf := ResponseFormat{Type: "json_object"}
	data, err := json.Marshal(rf)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var rf2 ResponseFormat
	if err := json.Unmarshal(data, &rf2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if rf2.Type != "json_object" {
		t.Errorf("Type = %q, want json_object", rf2.Type)
	}
}

