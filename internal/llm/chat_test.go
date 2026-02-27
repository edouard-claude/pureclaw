package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newTestClient(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()
	origHTTPDo := httpDo
	httpDo = func(c *http.Client, req *http.Request) (*http.Response, error) {
		return c.Do(req)
	}
	t.Cleanup(func() { httpDo = origHTTPDo })

	return &Client{
		apiKey:     "test-key",
		baseURL:    srv.URL + "/",
		model:      "test-model",
		httpClient: srv.Client(),
	}
}

func TestChatCompletion_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-key" {
			t.Errorf("Authorization = %q, want Bearer test-key", auth)
		}

		body, _ := io.ReadAll(r.Body)
		var req ChatRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("unmarshal request: %v", err)
		}
		if req.ResponseFormat == nil || req.ResponseFormat.Type != "json_object" {
			t.Error("missing json_object response_format")
		}
		if req.Model != "test-model" {
			t.Errorf("model = %q, want test-model", req.Model)
		}

		json.NewEncoder(w).Encode(ChatResponse{
			ID: "chat-1",
			Choices: []Choice{{
				Message:      Message{Role: "assistant", Content: `{"type":"message","content":"Hello"}`},
				FinishReason: "stop",
			}},
			Usage: Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)

	resp, err := client.ChatCompletion(context.Background(), []Message{
		{Role: "user", Content: "Hi"},
	}, nil)
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
	if resp.ID != "chat-1" {
		t.Errorf("ID = %q, want chat-1", resp.ID)
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("Choices len = %d, want 1", len(resp.Choices))
	}
	if resp.Choices[0].FinishReason != "stop" {
		t.Errorf("FinishReason = %q, want stop", resp.Choices[0].FinishReason)
	}
}

func TestChatCompletion_WithToolCalls(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(ChatResponse{
			ID: "chat-tools",
			Choices: []Choice{{
				Message: Message{
					Role: "assistant",
					ToolCalls: []ToolCall{{
						ID:   "tc-1",
						Type: "function",
						Function: ToolCallFunction{
							Name:      "exec_command",
							Arguments: `{"command":"ls -la"}`,
						},
					}},
				},
				FinishReason: "tool_calls",
			}},
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)

	resp, err := client.ChatCompletion(context.Background(), []Message{
		{Role: "user", Content: "list files"},
	}, []Tool{{
		Type: "function",
		Function: ToolFunction{
			Name:        "exec_command",
			Description: "Execute a command",
			Parameters:  map[string]any{"type": "object"},
		},
	}})
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}

	if !HasToolCalls(&resp.Choices[0]) {
		t.Error("expected HasToolCalls to be true")
	}
	tc := resp.Choices[0].Message.ToolCalls[0]
	if tc.ID != "tc-1" {
		t.Errorf("ToolCall.ID = %q, want tc-1", tc.ID)
	}
	if tc.Function.Name != "exec_command" {
		t.Errorf("Function.Name = %q, want exec_command", tc.Function.Name)
	}
}

func TestChatCompletion_ToolsIncludedInRequest(t *testing.T) {
	var gotReq ChatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &gotReq)

		json.NewEncoder(w).Encode(ChatResponse{
			Choices: []Choice{{
				Message:      Message{Role: "assistant", Content: `{"type":"message","content":"ok"}`},
				FinishReason: "stop",
			}},
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)

	tools := []Tool{{
		Type: "function",
		Function: ToolFunction{
			Name:        "test_func",
			Description: "A test function",
			Parameters:  map[string]any{"type": "object"},
		},
	}}

	_, err := client.ChatCompletion(context.Background(), []Message{
		{Role: "user", Content: "test"},
	}, tools)
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}

	if len(gotReq.Tools) != 1 {
		t.Fatalf("Tools len = %d, want 1", len(gotReq.Tools))
	}
	if gotReq.Tools[0].Function.Name != "test_func" {
		t.Errorf("Tool name = %q, want test_func", gotReq.Tools[0].Function.Name)
	}
	if gotReq.ToolChoice != "auto" {
		t.Errorf("ToolChoice = %q, want auto", gotReq.ToolChoice)
	}
}

func TestChatCompletion_NoToolsNoToolChoice(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &gotBody)

		json.NewEncoder(w).Encode(ChatResponse{
			Choices: []Choice{{
				Message:      Message{Role: "assistant", Content: `{"type":"noop","content":""}`},
				FinishReason: "stop",
			}},
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)

	_, err := client.ChatCompletion(context.Background(), []Message{
		{Role: "user", Content: "test"},
	}, nil)
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}

	if _, ok := gotBody["tools"]; ok {
		t.Error("tools should not be present when nil")
	}
	if _, ok := gotBody["tool_choice"]; ok {
		t.Error("tool_choice should not be present when no tools")
	}
}

func TestChatCompletion_ResponseFormatInRequest(t *testing.T) {
	var gotReq ChatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &gotReq)

		json.NewEncoder(w).Encode(ChatResponse{
			Choices: []Choice{{
				Message:      Message{Role: "assistant", Content: `{"type":"message","content":"hi"}`},
				FinishReason: "stop",
			}},
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)

	_, err := client.ChatCompletion(context.Background(), []Message{
		{Role: "user", Content: "test"},
	}, nil)
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}

	if gotReq.ResponseFormat == nil {
		t.Fatal("ResponseFormat is nil")
	}
	if gotReq.ResponseFormat.Type != "json_object" {
		t.Errorf("ResponseFormat.Type = %q, want json_object", gotReq.ResponseFormat.Type)
	}
}

func TestChatCompletion_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("server error"))
	}))
	defer srv.Close()

	client := newTestClient(t, srv)

	_, err := client.ChatCompletion(context.Background(), []Message{
		{Role: "user", Content: "test"},
	}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "status 500") {
		t.Errorf("error = %q, want to contain 'status 500'", err.Error())
	}
}

func TestChatCompletion_InvalidResponseJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`not valid json`))
	}))
	defer srv.Close()

	client := newTestClient(t, srv)

	_, err := client.ChatCompletion(context.Background(), []Message{
		{Role: "user", Content: "test"},
	}, nil)
	if err == nil {
		t.Fatal("expected error for invalid response JSON")
	}
	if !strings.Contains(err.Error(), "unmarshal response") {
		t.Errorf("error = %q, want to contain 'unmarshal response'", err.Error())
	}
}

func TestChatCompletionWithRetry_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(ChatResponse{
			Choices: []Choice{{
				Message:      Message{Role: "assistant", Content: `{"type":"message","content":"Hello"}`},
				FinishReason: "stop",
			}},
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)

	origRetry := retryFn
	retryFn = func(_ context.Context, _ int, _ time.Duration, fn func() error) error {
		return fn()
	}
	defer func() { retryFn = origRetry }()

	resp, err := client.ChatCompletionWithRetry(context.Background(), []Message{
		{Role: "user", Content: "Hi"},
	}, nil)
	if err != nil {
		t.Fatalf("ChatCompletionWithRetry: %v", err)
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("Choices len = %d, want 1", len(resp.Choices))
	}
}

func TestChatCompletionWithRetry_MalformedRetrySucceeds(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			// First call: return malformed JSON content
			json.NewEncoder(w).Encode(ChatResponse{
				Choices: []Choice{{
					Message:      Message{Role: "assistant", Content: `not valid json`},
					FinishReason: "stop",
				}},
			})
			return
		}
		// Second call: return valid response
		json.NewEncoder(w).Encode(ChatResponse{
			Choices: []Choice{{
				Message:      Message{Role: "assistant", Content: `{"type":"message","content":"OK"}`},
				FinishReason: "stop",
			}},
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)

	// Use a real-ish retry that calls fn multiple times
	origRetry := retryFn
	retryFn = func(_ context.Context, maxAttempts int, _ time.Duration, fn func() error) error {
		var lastErr error
		for range maxAttempts {
			lastErr = fn()
			if lastErr == nil {
				return nil
			}
		}
		return lastErr
	}
	defer func() { retryFn = origRetry }()

	resp, err := client.ChatCompletionWithRetry(context.Background(), []Message{
		{Role: "user", Content: "test"},
	}, nil)
	if err != nil {
		t.Fatalf("ChatCompletionWithRetry: %v", err)
	}
	if callCount != 2 {
		t.Errorf("callCount = %d, want 2", callCount)
	}
	if resp.Choices[0].Message.Content != `{"type":"message","content":"OK"}` {
		t.Errorf("unexpected content: %s", resp.Choices[0].Message.Content)
	}
}

func TestChatCompletionWithRetry_ExhaustsRetries(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(ChatResponse{
			Choices: []Choice{{
				Message:      Message{Role: "assistant", Content: `not valid json`},
				FinishReason: "stop",
			}},
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)

	callCount := 0
	origRetry := retryFn
	retryFn = func(_ context.Context, maxAttempts int, _ time.Duration, fn func() error) error {
		var lastErr error
		for range maxAttempts {
			callCount++
			lastErr = fn()
			if lastErr == nil {
				return nil
			}
		}
		return lastErr
	}
	defer func() { retryFn = origRetry }()

	_, err := client.ChatCompletionWithRetry(context.Background(), []Message{
		{Role: "user", Content: "test"},
	}, nil)
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if callCount != 3 {
		t.Errorf("callCount = %d, want 3", callCount)
	}
	if !strings.Contains(err.Error(), "llm: parse agent response:") {
		t.Errorf("error = %q, want to contain 'llm: parse agent response:'", err.Error())
	}
}

func TestChatCompletionWithRetry_ToolCallsSkipValidation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(ChatResponse{
			Choices: []Choice{{
				Message: Message{
					Role: "assistant",
					ToolCalls: []ToolCall{{
						ID:       "tc-1",
						Type:     "function",
						Function: ToolCallFunction{Name: "test", Arguments: "{}"},
					}},
				},
				FinishReason: "tool_calls",
			}},
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)

	origRetry := retryFn
	retryFn = func(_ context.Context, _ int, _ time.Duration, fn func() error) error {
		return fn()
	}
	defer func() { retryFn = origRetry }()

	resp, err := client.ChatCompletionWithRetry(context.Background(), []Message{
		{Role: "user", Content: "test"},
	}, []Tool{{Type: "function", Function: ToolFunction{Name: "test"}}})
	if err != nil {
		t.Fatalf("ChatCompletionWithRetry: %v", err)
	}
	if !HasToolCalls(&resp.Choices[0]) {
		t.Error("expected HasToolCalls to be true")
	}
}

func TestChatCompletionWithRetry_EmptyChoices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(ChatResponse{
			Choices: []Choice{},
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)

	origRetry := retryFn
	retryFn = func(_ context.Context, _ int, _ time.Duration, fn func() error) error {
		return fn()
	}
	defer func() { retryFn = origRetry }()

	resp, err := client.ChatCompletionWithRetry(context.Background(), []Message{
		{Role: "user", Content: "test"},
	}, nil)
	if err != nil {
		t.Fatalf("ChatCompletionWithRetry: %v", err)
	}
	if len(resp.Choices) != 0 {
		t.Errorf("Choices len = %d, want 0", len(resp.Choices))
	}
}

func TestChatCompletionWithRetry_EmptyContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(ChatResponse{
			Choices: []Choice{{
				Message:      Message{Role: "assistant", Content: ""},
				FinishReason: "stop",
			}},
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)

	origRetry := retryFn
	retryFn = func(_ context.Context, _ int, _ time.Duration, fn func() error) error {
		return fn()
	}
	defer func() { retryFn = origRetry }()

	resp, err := client.ChatCompletionWithRetry(context.Background(), []Message{
		{Role: "user", Content: "test"},
	}, nil)
	if err != nil {
		t.Fatalf("ChatCompletionWithRetry: %v", err)
	}
	if resp.Choices[0].Message.Content != "" {
		t.Errorf("Content = %q, want empty", resp.Choices[0].Message.Content)
	}
}

func TestChatCompletionWithRetry_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte("rate limited"))
	}))
	defer srv.Close()

	client := newTestClient(t, srv)

	origRetry := retryFn
	retryFn = func(_ context.Context, _ int, _ time.Duration, fn func() error) error {
		return fn()
	}
	defer func() { retryFn = origRetry }()

	_, err := client.ChatCompletionWithRetry(context.Background(), []Message{
		{Role: "user", Content: "test"},
	}, nil)
	if err == nil {
		t.Fatal("expected error for 429")
	}
	if !strings.Contains(err.Error(), "status 429") {
		t.Errorf("error = %q, want to contain 'status 429'", err.Error())
	}
}

func TestChatCompletionWithRetry_NonRetryableHTTPError(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("bad request"))
	}))
	defer srv.Close()

	client := newTestClient(t, srv)

	origRetry := retryFn
	retryFn = func(_ context.Context, maxAttempts int, _ time.Duration, fn func() error) error {
		var lastErr error
		for range maxAttempts {
			lastErr = fn()
			if lastErr == nil {
				return nil
			}
		}
		return lastErr
	}
	defer func() { retryFn = origRetry }()

	_, err := client.ChatCompletionWithRetry(context.Background(), []Message{
		{Role: "user", Content: "test"},
	}, nil)
	if err == nil {
		t.Fatal("expected error for 400")
	}
	if callCount != 1 {
		t.Errorf("callCount = %d, want 1 (should not retry non-retryable 400)", callCount)
	}
	if !strings.Contains(err.Error(), "status 400") {
		t.Errorf("error = %q, want to contain 'status 400'", err.Error())
	}
}

func TestChatCompletionWithRetry_UnknownAgentType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(ChatResponse{
			Choices: []Choice{{
				Message:      Message{Role: "assistant", Content: `{"type":"action","content":"do something"}`},
				FinishReason: "stop",
			}},
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)

	callCount := 0
	origRetry := retryFn
	retryFn = func(_ context.Context, maxAttempts int, _ time.Duration, fn func() error) error {
		var lastErr error
		for range maxAttempts {
			callCount++
			lastErr = fn()
			if lastErr == nil {
				return nil
			}
		}
		return lastErr
	}
	defer func() { retryFn = origRetry }()

	_, err := client.ChatCompletionWithRetry(context.Background(), []Message{
		{Role: "user", Content: "test"},
	}, nil)
	if err == nil {
		t.Fatal("expected error for unknown agent type")
	}
	if !strings.Contains(err.Error(), "unknown type") {
		t.Errorf("error = %q, want to contain 'unknown type'", err.Error())
	}
}

func TestHasToolCalls(t *testing.T) {
	tests := []struct {
		name   string
		choice Choice
		want   bool
	}{
		{
			name: "has tool calls",
			choice: Choice{
				FinishReason: "tool_calls",
				Message: Message{
					ToolCalls: []ToolCall{
						{ID: "tc-1", Type: "function", Function: ToolCallFunction{Name: "test"}},
					},
				},
			},
			want: true,
		},
		{
			name: "no tool calls - stop finish reason",
			choice: Choice{
				FinishReason: "stop",
				Message:      Message{Content: "hello"},
			},
			want: false,
		},
		{
			name: "tool_calls finish reason but empty slice",
			choice: Choice{
				FinishReason: "tool_calls",
				Message:      Message{ToolCalls: []ToolCall{}},
			},
			want: false,
		},
		{
			name: "stop finish reason with tool calls present",
			choice: Choice{
				FinishReason: "stop",
				Message: Message{
					ToolCalls: []ToolCall{
						{ID: "tc-1", Type: "function", Function: ToolCallFunction{Name: "test"}},
					},
				},
			},
			want: false,
		},
		{
			name: "length finish reason",
			choice: Choice{
				FinishReason: "length",
				Message:      Message{Content: "truncated"},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HasToolCalls(&tt.choice)
			if got != tt.want {
				t.Errorf("HasToolCalls() = %v, want %v", got, tt.want)
			}
		})
	}
}
