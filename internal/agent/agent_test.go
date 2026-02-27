package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/edouard/pureclaw/internal/llm"
	"github.com/edouard/pureclaw/internal/telegram"
	"github.com/edouard/pureclaw/internal/tool"
	"github.com/edouard/pureclaw/internal/workspace"
)

// --- Test doubles ---

type fakeLLM struct {
	responses []*llm.ChatResponse // cycle through responses
	errs      []error
	calls     [][]llm.Message
	callIdx   int
}

func (f *fakeLLM) ChatCompletionWithRetry(ctx context.Context, messages []llm.Message, tools []llm.Tool) (*llm.ChatResponse, error) {
	f.calls = append(f.calls, messages)
	idx := f.callIdx
	if f.callIdx < len(f.responses)-1 || f.callIdx < len(f.errs)-1 {
		f.callIdx++
	}
	var resp *llm.ChatResponse
	if idx < len(f.responses) {
		resp = f.responses[idx]
	}
	var err error
	if idx < len(f.errs) {
		err = f.errs[idx]
	}
	return resp, err
}

type sentMessage struct {
	chatID int64
	text   string
}

type fakeSender struct {
	sent []sentMessage
	err  error
}

func (f *fakeSender) Send(ctx context.Context, chatID int64, text string) error {
	f.sent = append(f.sent, sentMessage{chatID, text})
	return f.err
}

type memoryEntry struct {
	source  string
	content string
}

type fakeMemoryWriter struct {
	entries []memoryEntry
	err     error
}

func (f *fakeMemoryWriter) Write(ctx context.Context, source, content string) error {
	f.entries = append(f.entries, memoryEntry{source, content})
	return f.err
}

type toolExecCall struct {
	name string
	args json.RawMessage
}

type fakeToolExecutor struct {
	results     []tool.ToolResult
	calls       []toolExecCall
	definitions []llm.Tool
	callIdx     int
}

func (f *fakeToolExecutor) Execute(ctx context.Context, name string, args json.RawMessage) tool.ToolResult {
	f.calls = append(f.calls, toolExecCall{name, args})
	if f.callIdx < len(f.results) {
		r := f.results[f.callIdx]
		if f.callIdx < len(f.results)-1 {
			f.callIdx++
		}
		return r
	}
	return tool.ToolResult{Success: true, Output: "ok"}
}

func (f *fakeToolExecutor) Definitions() []llm.Tool {
	return f.definitions
}

// --- Helpers ---

func testWorkspace(t *testing.T) *workspace.Workspace {
	t.Helper()
	return &workspace.Workspace{
		Root:    t.TempDir(),
		SoulMD:  "You are a test agent.",
		AgentMD: "# Test Agent\n\n## Environment\n\n- **OS:** test",
	}
}

func makeResponse(typ, content string) *llm.ChatResponse {
	return &llm.ChatResponse{
		Choices: []llm.Choice{{
			Message: llm.Message{
				Content: `{"type":"` + typ + `","content":"` + content + `"}`,
			},
			FinishReason: "stop",
		}},
	}
}

func newTestAgent(ws *workspace.Workspace, l LLMClient, s Sender) *Agent {
	return New(NewAgentConfig{
		Workspace: ws,
		LLM:       l,
		Sender:    s,
	})
}

func newTestAgentWithTools(ws *workspace.Workspace, l LLMClient, s Sender, te ToolExecutor) *Agent {
	return New(NewAgentConfig{
		Workspace:    ws,
		LLM:          l,
		Sender:       s,
		ToolExecutor: te,
	})
}

func makeToolCallResponse(toolCalls ...llm.ToolCall) *llm.ChatResponse {
	return &llm.ChatResponse{
		Choices: []llm.Choice{{
			Message: llm.Message{
				Role:      "assistant",
				Content:   "",
				ToolCalls: toolCalls,
			},
			FinishReason: "tool_calls",
		}},
	}
}

func tc(id, name, args string) llm.ToolCall {
	return llm.ToolCall{
		ID:   id,
		Type: "function",
		Function: llm.ToolCallFunction{
			Name:      name,
			Arguments: args,
		},
	}
}

func sendAndWait(t *testing.T, ch chan telegram.TelegramMessage, msg telegram.TelegramMessage) {
	t.Helper()
	select {
	case ch <- msg:
	case <-time.After(time.Second):
		t.Fatal("timed out sending message to channel")
	}
	// Give the event loop time to process.
	time.Sleep(50 * time.Millisecond)
}

func testMsg(chatID int64, text string) telegram.TelegramMessage {
	return telegram.TelegramMessage{
		Message: telegram.Message{
			Chat: telegram.Chat{ID: chatID},
			Text: text,
		},
	}
}

// --- Tests ---

func TestRun_MessageType(t *testing.T) {
	ws := testWorkspace(t)
	llmFake := &fakeLLM{responses: []*llm.ChatResponse{makeResponse("message", "hello")}}
	sender := &fakeSender{}
	ag := newTestAgent(ws, llmFake, sender)

	ctx, cancel := context.WithCancel(context.Background())
	messages := make(chan telegram.TelegramMessage, 1)

	done := make(chan error, 1)
	go func() { done <- ag.Run(ctx, messages) }()

	sendAndWait(t, messages, testMsg(42, "hi"))
	cancel()
	<-done

	if len(sender.sent) != 1 {
		t.Fatalf("expected 1 sent message, got %d", len(sender.sent))
	}
	if sender.sent[0].chatID != 42 {
		t.Errorf("expected chatID 42, got %d", sender.sent[0].chatID)
	}
	if sender.sent[0].text != "hello" {
		t.Errorf("expected text %q, got %q", "hello", sender.sent[0].text)
	}
}

func TestRun_ThinkType(t *testing.T) {
	ws := testWorkspace(t)
	llmFake := &fakeLLM{responses: []*llm.ChatResponse{makeResponse("think", "reasoning")}}
	sender := &fakeSender{}
	ag := newTestAgent(ws, llmFake, sender)

	ctx, cancel := context.WithCancel(context.Background())
	messages := make(chan telegram.TelegramMessage, 1)

	done := make(chan error, 1)
	go func() { done <- ag.Run(ctx, messages) }()

	sendAndWait(t, messages, testMsg(42, "hi"))
	cancel()
	<-done

	if len(sender.sent) != 0 {
		t.Fatalf("expected 0 sent messages for think type, got %d", len(sender.sent))
	}
}

func TestRun_NoopType(t *testing.T) {
	ws := testWorkspace(t)
	llmFake := &fakeLLM{responses: []*llm.ChatResponse{makeResponse("noop", "")}}
	sender := &fakeSender{}
	ag := newTestAgent(ws, llmFake, sender)

	ctx, cancel := context.WithCancel(context.Background())
	messages := make(chan telegram.TelegramMessage, 1)

	done := make(chan error, 1)
	go func() { done <- ag.Run(ctx, messages) }()

	sendAndWait(t, messages, testMsg(42, "hi"))
	cancel()
	<-done

	if len(sender.sent) != 0 {
		t.Fatalf("expected 0 sent messages for noop type, got %d", len(sender.sent))
	}
}

func TestRun_ContextCancellation(t *testing.T) {
	ws := testWorkspace(t)
	llmFake := &fakeLLM{responses: []*llm.ChatResponse{makeResponse("message", "ok")}}
	sender := &fakeSender{}
	ag := newTestAgent(ws, llmFake, sender)

	ctx, cancel := context.WithCancel(context.Background())
	messages := make(chan telegram.TelegramMessage, 1)

	done := make(chan error, 1)
	go func() { done <- ag.Run(ctx, messages) }()

	cancel()
	err := <-done
	if err != nil {
		t.Fatalf("expected nil error on context cancellation, got %v", err)
	}
}

func TestRun_LLMError(t *testing.T) {
	ws := testWorkspace(t)
	llmFake := &fakeLLM{
		errs: []error{errors.New("llm failure")},
	}
	sender := &fakeSender{}
	ag := newTestAgent(ws, llmFake, sender)

	ctx, cancel := context.WithCancel(context.Background())
	messages := make(chan telegram.TelegramMessage, 1)

	done := make(chan error, 1)
	go func() { done <- ag.Run(ctx, messages) }()

	sendAndWait(t, messages, testMsg(42, "hi"))
	cancel()
	<-done

	if len(sender.sent) != 0 {
		t.Fatalf("expected no sends on LLM error, got %d", len(sender.sent))
	}
}

func TestRun_ParseError(t *testing.T) {
	ws := testWorkspace(t)
	llmFake := &fakeLLM{
		responses: []*llm.ChatResponse{{
			Choices: []llm.Choice{{
				Message:      llm.Message{Content: "not json"},
				FinishReason: "stop",
			}},
		}},
	}
	sender := &fakeSender{}
	ag := newTestAgent(ws, llmFake, sender)

	ctx, cancel := context.WithCancel(context.Background())
	messages := make(chan telegram.TelegramMessage, 1)

	done := make(chan error, 1)
	go func() { done <- ag.Run(ctx, messages) }()

	sendAndWait(t, messages, testMsg(42, "hi"))
	cancel()
	<-done

	if len(sender.sent) != 0 {
		t.Fatalf("expected no sends on parse error, got %d", len(sender.sent))
	}
}

func TestRun_SendError(t *testing.T) {
	ws := testWorkspace(t)
	llmFake := &fakeLLM{responses: []*llm.ChatResponse{makeResponse("message", "hello")}}
	sender := &fakeSender{err: errors.New("send failure")}
	ag := newTestAgent(ws, llmFake, sender)

	ctx, cancel := context.WithCancel(context.Background())
	messages := make(chan telegram.TelegramMessage, 1)

	done := make(chan error, 1)
	go func() { done <- ag.Run(ctx, messages) }()

	sendAndWait(t, messages, testMsg(42, "hi"))
	cancel()
	<-done

	// Sender was called despite error.
	if len(sender.sent) != 1 {
		t.Fatalf("expected 1 send attempt, got %d", len(sender.sent))
	}
	// History should still be updated even on send error.
	if len(ag.history) != 2 {
		t.Fatalf("expected history length 2, got %d", len(ag.history))
	}
}

func TestRun_SequentialProcessing(t *testing.T) {
	ws := testWorkspace(t)
	llmFake := &fakeLLM{
		responses: []*llm.ChatResponse{
			makeResponse("message", "reply1"),
			makeResponse("message", "reply2"),
		},
	}
	sender := &fakeSender{}
	ag := newTestAgent(ws, llmFake, sender)

	ctx, cancel := context.WithCancel(context.Background())
	messages := make(chan telegram.TelegramMessage, 2)

	done := make(chan error, 1)
	go func() { done <- ag.Run(ctx, messages) }()

	sendAndWait(t, messages, testMsg(42, "msg1"))
	sendAndWait(t, messages, testMsg(42, "msg2"))
	cancel()
	<-done

	if len(sender.sent) != 2 {
		t.Fatalf("expected 2 sent messages, got %d", len(sender.sent))
	}
	if sender.sent[0].text != "reply1" {
		t.Errorf("expected first reply %q, got %q", "reply1", sender.sent[0].text)
	}
	if sender.sent[1].text != "reply2" {
		t.Errorf("expected second reply %q, got %q", "reply2", sender.sent[1].text)
	}
}

func TestRun_EmptyMessage(t *testing.T) {
	ws := testWorkspace(t)
	llmFake := &fakeLLM{responses: []*llm.ChatResponse{makeResponse("message", "hello")}}
	sender := &fakeSender{}
	ag := newTestAgent(ws, llmFake, sender)

	ctx, cancel := context.WithCancel(context.Background())
	messages := make(chan telegram.TelegramMessage, 1)

	done := make(chan error, 1)
	go func() { done <- ag.Run(ctx, messages) }()

	// Send a zero-value message (simulates closed channel).
	sendAndWait(t, messages, telegram.TelegramMessage{})
	cancel()
	<-done

	// Should not have called LLM or sender.
	if len(llmFake.calls) != 0 {
		t.Fatalf("expected 0 LLM calls for empty message, got %d", len(llmFake.calls))
	}
	if len(sender.sent) != 0 {
		t.Fatalf("expected 0 sends for empty message, got %d", len(sender.sent))
	}
}

func TestRun_HistoryIncluded(t *testing.T) {
	ws := testWorkspace(t)
	llmFake := &fakeLLM{
		responses: []*llm.ChatResponse{
			makeResponse("message", "reply1"),
			makeResponse("message", "reply2"),
		},
	}
	sender := &fakeSender{}
	ag := newTestAgent(ws, llmFake, sender)

	ctx, cancel := context.WithCancel(context.Background())
	messages := make(chan telegram.TelegramMessage, 2)

	done := make(chan error, 1)
	go func() { done <- ag.Run(ctx, messages) }()

	sendAndWait(t, messages, testMsg(42, "first"))
	sendAndWait(t, messages, testMsg(42, "second"))
	cancel()
	<-done

	if len(llmFake.calls) != 2 {
		t.Fatalf("expected 2 LLM calls, got %d", len(llmFake.calls))
	}

	// First call: system + user = 2 messages.
	if len(llmFake.calls[0]) != 2 {
		t.Errorf("first call: expected 2 messages, got %d", len(llmFake.calls[0]))
	}

	// Second call: system + history(user+assistant) + user = 4 messages.
	if len(llmFake.calls[1]) != 4 {
		t.Errorf("second call: expected 4 messages, got %d", len(llmFake.calls[1]))
	}

	// Verify history messages in second call.
	if llmFake.calls[1][1].Role != "user" || llmFake.calls[1][1].Content != "first" {
		t.Errorf("second call: expected history user message %q, got %q", "first", llmFake.calls[1][1].Content)
	}
	if llmFake.calls[1][2].Role != "assistant" {
		t.Errorf("second call: expected history assistant message, got role %q", llmFake.calls[1][2].Role)
	}
}

func TestRun_ToolCallsReturned(t *testing.T) {
	ws := testWorkspace(t)
	llmFake := &fakeLLM{
		responses: []*llm.ChatResponse{{
			Choices: []llm.Choice{{
				Message: llm.Message{
					Content: "",
					ToolCalls: []llm.ToolCall{{
						ID:   "call_1",
						Type: "function",
						Function: llm.ToolCallFunction{
							Name:      "some_tool",
							Arguments: "{}",
						},
					}},
				},
				FinishReason: "tool_calls",
			}},
		}},
	}
	sender := &fakeSender{}
	ag := newTestAgent(ws, llmFake, sender)

	ctx, cancel := context.WithCancel(context.Background())
	messages := make(chan telegram.TelegramMessage, 1)

	done := make(chan error, 1)
	go func() { done <- ag.Run(ctx, messages) }()

	sendAndWait(t, messages, testMsg(42, "hi"))
	cancel()
	<-done

	// Should not send anything — no tool executor configured.
	if len(sender.sent) != 0 {
		t.Fatalf("expected 0 sends for tool calls with nil executor, got %d", len(sender.sent))
	}
}

func TestRun_UnknownResponseType(t *testing.T) {
	ws := testWorkspace(t)
	llmFake := &fakeLLM{responses: []*llm.ChatResponse{makeResponse("unknown_type", "data")}}
	sender := &fakeSender{}
	ag := newTestAgent(ws, llmFake, sender)

	ctx, cancel := context.WithCancel(context.Background())
	messages := make(chan telegram.TelegramMessage, 1)

	done := make(chan error, 1)
	go func() { done <- ag.Run(ctx, messages) }()

	sendAndWait(t, messages, testMsg(42, "hi"))
	cancel()
	<-done

	// Should not send anything for unknown type.
	if len(sender.sent) != 0 {
		t.Fatalf("expected 0 sends for unknown type, got %d", len(sender.sent))
	}
}

func TestRun_NoChoices(t *testing.T) {
	ws := testWorkspace(t)
	llmFake := &fakeLLM{
		responses: []*llm.ChatResponse{{Choices: []llm.Choice{}}},
	}
	sender := &fakeSender{}
	ag := newTestAgent(ws, llmFake, sender)

	ctx, cancel := context.WithCancel(context.Background())
	messages := make(chan telegram.TelegramMessage, 1)

	done := make(chan error, 1)
	go func() { done <- ag.Run(ctx, messages) }()

	sendAndWait(t, messages, testMsg(42, "hi"))
	cancel()
	<-done

	if len(sender.sent) != 0 {
		t.Fatalf("expected 0 sends when no choices, got %d", len(sender.sent))
	}
}

// --- Memory integration tests ---

func TestRun_MessageLogsOwnerAndAgent(t *testing.T) {
	ws := testWorkspace(t)
	llmFake := &fakeLLM{responses: []*llm.ChatResponse{makeResponse("message", "hello back")}}
	sender := &fakeSender{}
	mem := &fakeMemoryWriter{}
	ag := New(NewAgentConfig{Workspace: ws, LLM: llmFake, Sender: sender, Memory: mem})

	ctx, cancel := context.WithCancel(context.Background())
	messages := make(chan telegram.TelegramMessage, 1)

	done := make(chan error, 1)
	go func() { done <- ag.Run(ctx, messages) }()

	sendAndWait(t, messages, testMsg(42, "hi"))
	cancel()
	<-done

	if len(mem.entries) != 2 {
		t.Fatalf("expected 2 memory entries, got %d", len(mem.entries))
	}
	if mem.entries[0].source != "owner" || mem.entries[0].content != "hi" {
		t.Errorf("entry[0] = %+v, want {owner, hi}", mem.entries[0])
	}
	if mem.entries[1].source != "agent" || mem.entries[1].content != "hello back" {
		t.Errorf("entry[1] = %+v, want {agent, hello back}", mem.entries[1])
	}
}

func TestRun_ThinkLogsOwnerOnly(t *testing.T) {
	ws := testWorkspace(t)
	llmFake := &fakeLLM{responses: []*llm.ChatResponse{makeResponse("think", "reasoning")}}
	sender := &fakeSender{}
	mem := &fakeMemoryWriter{}
	ag := New(NewAgentConfig{Workspace: ws, LLM: llmFake, Sender: sender, Memory: mem})

	ctx, cancel := context.WithCancel(context.Background())
	messages := make(chan telegram.TelegramMessage, 1)

	done := make(chan error, 1)
	go func() { done <- ag.Run(ctx, messages) }()

	sendAndWait(t, messages, testMsg(42, "hi"))
	cancel()
	<-done

	if len(mem.entries) != 1 {
		t.Fatalf("expected 1 memory entry for think, got %d", len(mem.entries))
	}
	if mem.entries[0].source != "owner" {
		t.Errorf("expected source 'owner', got %q", mem.entries[0].source)
	}
}

func TestRun_NoopLogsOwnerOnly(t *testing.T) {
	ws := testWorkspace(t)
	llmFake := &fakeLLM{responses: []*llm.ChatResponse{makeResponse("noop", "")}}
	sender := &fakeSender{}
	mem := &fakeMemoryWriter{}
	ag := New(NewAgentConfig{Workspace: ws, LLM: llmFake, Sender: sender, Memory: mem})

	ctx, cancel := context.WithCancel(context.Background())
	messages := make(chan telegram.TelegramMessage, 1)

	done := make(chan error, 1)
	go func() { done <- ag.Run(ctx, messages) }()

	sendAndWait(t, messages, testMsg(42, "hi"))
	cancel()
	<-done

	if len(mem.entries) != 1 {
		t.Fatalf("expected 1 memory entry for noop, got %d", len(mem.entries))
	}
	if mem.entries[0].source != "owner" {
		t.Errorf("expected source 'owner', got %q", mem.entries[0].source)
	}
}

func TestRun_SendErrorStillLogsMemory(t *testing.T) {
	ws := testWorkspace(t)
	llmFake := &fakeLLM{responses: []*llm.ChatResponse{makeResponse("message", "hello")}}
	sender := &fakeSender{err: errors.New("send failure")}
	mem := &fakeMemoryWriter{}
	ag := New(NewAgentConfig{Workspace: ws, LLM: llmFake, Sender: sender, Memory: mem})

	ctx, cancel := context.WithCancel(context.Background())
	messages := make(chan telegram.TelegramMessage, 1)

	done := make(chan error, 1)
	go func() { done <- ag.Run(ctx, messages) }()

	sendAndWait(t, messages, testMsg(42, "hi"))
	cancel()
	<-done

	// Memory should log both owner and agent even when send fails.
	if len(mem.entries) != 2 {
		t.Fatalf("expected 2 memory entries despite send error, got %d", len(mem.entries))
	}
	if mem.entries[0].source != "owner" || mem.entries[0].content != "hi" {
		t.Errorf("entry[0] = %+v, want {owner, hi}", mem.entries[0])
	}
	if mem.entries[1].source != "agent" || mem.entries[1].content != "hello" {
		t.Errorf("entry[1] = %+v, want {agent, hello}", mem.entries[1])
	}
}

func TestRun_MemoryWriteError(t *testing.T) {
	ws := testWorkspace(t)
	llmFake := &fakeLLM{responses: []*llm.ChatResponse{makeResponse("message", "hello")}}
	sender := &fakeSender{}
	mem := &fakeMemoryWriter{err: errors.New("disk full")}
	ag := New(NewAgentConfig{Workspace: ws, LLM: llmFake, Sender: sender, Memory: mem})

	ctx, cancel := context.WithCancel(context.Background())
	messages := make(chan telegram.TelegramMessage, 1)

	done := make(chan error, 1)
	go func() { done <- ag.Run(ctx, messages) }()

	sendAndWait(t, messages, testMsg(42, "hi"))
	cancel()
	<-done

	// Memory errors must not crash the agent or prevent sending.
	if len(sender.sent) != 1 {
		t.Fatalf("expected 1 sent message despite memory error, got %d", len(sender.sent))
	}
	// Both Write calls should still have been attempted despite errors.
	if len(mem.entries) != 2 {
		t.Fatalf("expected 2 memory write attempts despite errors, got %d", len(mem.entries))
	}
}

func TestRun_NilMemory(t *testing.T) {
	ws := testWorkspace(t)
	llmFake := &fakeLLM{responses: []*llm.ChatResponse{makeResponse("message", "hello")}}
	sender := &fakeSender{}
	// Explicitly pass nil Memory.
	ag := New(NewAgentConfig{Workspace: ws, LLM: llmFake, Sender: sender, Memory: nil})

	ctx, cancel := context.WithCancel(context.Background())
	messages := make(chan telegram.TelegramMessage, 1)

	done := make(chan error, 1)
	go func() { done <- ag.Run(ctx, messages) }()

	sendAndWait(t, messages, testMsg(42, "hi"))
	cancel()
	<-done

	// No panic, message sent normally.
	if len(sender.sent) != 1 {
		t.Fatalf("expected 1 sent message with nil memory, got %d", len(sender.sent))
	}
}

func TestRun_IntrospectionFailure(t *testing.T) {
	// Workspace without ## Environment and with non-writable root triggers introspection failure.
	ws := &workspace.Workspace{
		Root:    "/nonexistent/path/that/should/not/exist",
		SoulMD:  "You are a test agent.",
		AgentMD: "# Test Agent",
	}
	llmFake := &fakeLLM{responses: []*llm.ChatResponse{makeResponse("message", "hello")}}
	sender := &fakeSender{}
	ag := newTestAgent(ws, llmFake, sender)

	ctx, cancel := context.WithCancel(context.Background())
	messages := make(chan telegram.TelegramMessage, 1)

	done := make(chan error, 1)
	go func() { done <- ag.Run(ctx, messages) }()

	sendAndWait(t, messages, testMsg(42, "hi"))
	cancel()
	<-done

	// Introspection fails but agent continues normally.
	if len(sender.sent) != 1 {
		t.Fatalf("expected 1 sent message despite introspection failure, got %d", len(sender.sent))
	}
}

// --- Tool call loop tests ---

func TestHandleMessage_SingleToolCall(t *testing.T) {
	ws := testWorkspace(t)
	// LLM returns tool call, then final text response.
	llmFake := &fakeLLM{
		responses: []*llm.ChatResponse{
			makeToolCallResponse(tc("call_1", "read_file", `{"path":"/tmp/f"}`)),
			makeResponse("message", "done reading"),
		},
	}
	sender := &fakeSender{}
	executor := &fakeToolExecutor{
		results: []tool.ToolResult{
			{Success: true, Output: "file content"},
		},
		definitions: []llm.Tool{{Type: "function", Function: llm.ToolFunction{Name: "read_file"}}},
	}
	ag := newTestAgentWithTools(ws, llmFake, sender, executor)

	ctx, cancel := context.WithCancel(context.Background())
	messages := make(chan telegram.TelegramMessage, 1)
	done := make(chan error, 1)
	go func() { done <- ag.Run(ctx, messages) }()

	sendAndWait(t, messages, testMsg(42, "read a file"))
	cancel()
	<-done

	// Tool was executed.
	if len(executor.calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(executor.calls))
	}
	if executor.calls[0].name != "read_file" {
		t.Errorf("expected tool name %q, got %q", "read_file", executor.calls[0].name)
	}

	// Final message was sent.
	if len(sender.sent) != 1 {
		t.Fatalf("expected 1 sent message, got %d", len(sender.sent))
	}
	if sender.sent[0].text != "done reading" {
		t.Errorf("expected text %q, got %q", "done reading", sender.sent[0].text)
	}

	// LLM was called twice: once with tools, once with tool result.
	if len(llmFake.calls) != 2 {
		t.Fatalf("expected 2 LLM calls, got %d", len(llmFake.calls))
	}

	// Second LLM call should include assistant tool_calls msg and tool result msg.
	secondCall := llmFake.calls[1]
	// Should have: system + user + assistant(tool_calls) + tool(result) = 4 messages.
	if len(secondCall) != 4 {
		t.Fatalf("second LLM call: expected 4 messages, got %d", len(secondCall))
	}
	if secondCall[2].Role != "assistant" {
		t.Errorf("expected assistant role at idx 2, got %q", secondCall[2].Role)
	}
	if secondCall[3].Role != "tool" {
		t.Errorf("expected tool role at idx 3, got %q", secondCall[3].Role)
	}
	if secondCall[3].ToolCallID != "call_1" {
		t.Errorf("expected tool_call_id %q, got %q", "call_1", secondCall[3].ToolCallID)
	}
}

func TestHandleMessage_MultipleToolCalls(t *testing.T) {
	ws := testWorkspace(t)
	// LLM returns 2 tool calls in one response, then text.
	llmFake := &fakeLLM{
		responses: []*llm.ChatResponse{
			makeToolCallResponse(
				tc("call_1", "read_file", `{"path":"a.txt"}`),
				tc("call_2", "list_dir", `{"path":"/"}`),
			),
			makeResponse("message", "found 2 results"),
		},
	}
	sender := &fakeSender{}
	executor := &fakeToolExecutor{
		results: []tool.ToolResult{
			{Success: true, Output: "content a"},
			{Success: true, Output: "dir listing"},
		},
		definitions: []llm.Tool{},
	}
	ag := newTestAgentWithTools(ws, llmFake, sender, executor)

	ctx, cancel := context.WithCancel(context.Background())
	messages := make(chan telegram.TelegramMessage, 1)
	done := make(chan error, 1)
	go func() { done <- ag.Run(ctx, messages) }()

	sendAndWait(t, messages, testMsg(42, "do stuff"))
	cancel()
	<-done

	// Both tools executed.
	if len(executor.calls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(executor.calls))
	}
	if executor.calls[0].name != "read_file" {
		t.Errorf("expected first tool %q, got %q", "read_file", executor.calls[0].name)
	}
	if executor.calls[1].name != "list_dir" {
		t.Errorf("expected second tool %q, got %q", "list_dir", executor.calls[1].name)
	}

	// Final message sent.
	if len(sender.sent) != 1 {
		t.Fatalf("expected 1 sent message, got %d", len(sender.sent))
	}

	// Second LLM call should include: system + user + assistant(2 tool_calls) + 2 tool results = 5 messages.
	if len(llmFake.calls) != 2 {
		t.Fatalf("expected 2 LLM calls, got %d", len(llmFake.calls))
	}
	secondCall := llmFake.calls[1]
	if len(secondCall) != 5 {
		t.Fatalf("second LLM call: expected 5 messages, got %d", len(secondCall))
	}
}

func TestHandleMessage_ChainedToolCalls(t *testing.T) {
	ws := testWorkspace(t)
	// LLM returns tool call → tool call → message (two rounds).
	llmFake := &fakeLLM{
		responses: []*llm.ChatResponse{
			makeToolCallResponse(tc("call_1", "read_file", `{"path":"config.json"}`)),
			makeToolCallResponse(tc("call_2", "write_file", `{"path":"out.txt","content":"data"}`)),
			makeResponse("message", "done chaining"),
		},
	}
	sender := &fakeSender{}
	executor := &fakeToolExecutor{
		results: []tool.ToolResult{
			{Success: true, Output: "config data"},
			{Success: true, Output: "file written: out.txt"},
		},
		definitions: []llm.Tool{},
	}
	ag := newTestAgentWithTools(ws, llmFake, sender, executor)

	ctx, cancel := context.WithCancel(context.Background())
	messages := make(chan telegram.TelegramMessage, 1)
	done := make(chan error, 1)
	go func() { done <- ag.Run(ctx, messages) }()

	sendAndWait(t, messages, testMsg(42, "chain tools"))
	cancel()
	<-done

	// Two tool calls over two rounds.
	if len(executor.calls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(executor.calls))
	}

	// 3 LLM calls total.
	if len(llmFake.calls) != 3 {
		t.Fatalf("expected 3 LLM calls, got %d", len(llmFake.calls))
	}

	// Final message sent.
	if len(sender.sent) != 1 {
		t.Fatalf("expected 1 sent message, got %d", len(sender.sent))
	}
	if sender.sent[0].text != "done chaining" {
		t.Errorf("expected %q, got %q", "done chaining", sender.sent[0].text)
	}
}

func TestHandleMessage_MaxRoundsExceeded(t *testing.T) {
	ws := testWorkspace(t)
	// LLM always returns tool calls (never a text response).
	toolResp := makeToolCallResponse(tc("call_x", "read_file", `{}`))
	responses := make([]*llm.ChatResponse, maxToolRounds+1)
	for i := range responses {
		responses[i] = toolResp
	}
	llmFake := &fakeLLM{responses: responses}
	sender := &fakeSender{}
	executor := &fakeToolExecutor{
		results:     []tool.ToolResult{{Success: true, Output: "ok"}},
		definitions: []llm.Tool{},
	}
	ag := newTestAgentWithTools(ws, llmFake, sender, executor)

	ctx, cancel := context.WithCancel(context.Background())
	messages := make(chan telegram.TelegramMessage, 1)
	done := make(chan error, 1)
	go func() { done <- ag.Run(ctx, messages) }()

	sendAndWait(t, messages, testMsg(42, "infinite tools"))
	cancel()
	<-done

	// Should have executed exactly maxToolRounds tool calls.
	if len(executor.calls) != maxToolRounds {
		t.Fatalf("expected %d tool calls, got %d", maxToolRounds, len(executor.calls))
	}

	// Should NOT have sent any message (loop exhausted without text response).
	// The last response still has tool calls, so ParseAgentResponse will fail on empty content.
	if len(sender.sent) != 0 {
		t.Fatalf("expected 0 sends after max rounds, got %d", len(sender.sent))
	}
}

func TestHandleMessage_NoToolExecutor_ToolCallsReturned(t *testing.T) {
	ws := testWorkspace(t)
	llmFake := &fakeLLM{
		responses: []*llm.ChatResponse{
			makeToolCallResponse(tc("call_1", "some_tool", "{}")),
		},
	}
	sender := &fakeSender{}
	// No tool executor — nil.
	ag := newTestAgent(ws, llmFake, sender)

	ctx, cancel := context.WithCancel(context.Background())
	messages := make(chan telegram.TelegramMessage, 1)
	done := make(chan error, 1)
	go func() { done <- ag.Run(ctx, messages) }()

	sendAndWait(t, messages, testMsg(42, "hi"))
	cancel()
	<-done

	// Should not send anything — no executor configured.
	if len(sender.sent) != 0 {
		t.Fatalf("expected 0 sends with nil executor, got %d", len(sender.sent))
	}
}

func TestHandleMessage_UnknownTool(t *testing.T) {
	ws := testWorkspace(t)
	// LLM calls unknown tool, then gets error result, then responds with text.
	llmFake := &fakeLLM{
		responses: []*llm.ChatResponse{
			makeToolCallResponse(tc("call_1", "nonexistent_tool", "{}")),
			makeResponse("message", "tool not found"),
		},
	}
	sender := &fakeSender{}
	executor := &fakeToolExecutor{
		results: []tool.ToolResult{
			{Success: false, Error: "unknown tool: nonexistent_tool"},
		},
		definitions: []llm.Tool{},
	}
	ag := newTestAgentWithTools(ws, llmFake, sender, executor)

	ctx, cancel := context.WithCancel(context.Background())
	messages := make(chan telegram.TelegramMessage, 1)
	done := make(chan error, 1)
	go func() { done <- ag.Run(ctx, messages) }()

	sendAndWait(t, messages, testMsg(42, "use unknown tool"))
	cancel()
	<-done

	// Tool executor was called with the unknown tool name.
	if len(executor.calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(executor.calls))
	}
	if executor.calls[0].name != "nonexistent_tool" {
		t.Errorf("expected tool name %q, got %q", "nonexistent_tool", executor.calls[0].name)
	}

	// Error result was sent back to LLM, which then produced a message.
	if len(sender.sent) != 1 {
		t.Fatalf("expected 1 sent message, got %d", len(sender.sent))
	}
	if sender.sent[0].text != "tool not found" {
		t.Errorf("expected %q, got %q", "tool not found", sender.sent[0].text)
	}
}

func TestHandleMessage_ToolsNilExistingBehavior(t *testing.T) {
	ws := testWorkspace(t)
	llmFake := &fakeLLM{responses: []*llm.ChatResponse{makeResponse("message", "normal reply")}}
	sender := &fakeSender{}
	// No executor, LLM returns text — same as before.
	ag := newTestAgent(ws, llmFake, sender)

	ctx, cancel := context.WithCancel(context.Background())
	messages := make(chan telegram.TelegramMessage, 1)
	done := make(chan error, 1)
	go func() { done <- ag.Run(ctx, messages) }()

	sendAndWait(t, messages, testMsg(42, "hello"))
	cancel()
	<-done

	if len(sender.sent) != 1 {
		t.Fatalf("expected 1 sent message, got %d", len(sender.sent))
	}
	if sender.sent[0].text != "normal reply" {
		t.Errorf("expected %q, got %q", "normal reply", sender.sent[0].text)
	}
}

func TestHandleMessage_ToolDefinitionsPassedToLLM(t *testing.T) {
	ws := testWorkspace(t)
	toolDefs := []llm.Tool{
		{Type: "function", Function: llm.ToolFunction{Name: "read_file", Description: "Read a file"}},
		{Type: "function", Function: llm.ToolFunction{Name: "write_file", Description: "Write a file"}},
	}

	var capturedTools []llm.Tool
	llmFake := &fakeLLM{responses: []*llm.ChatResponse{makeResponse("message", "ok")}}
	// Override to capture tools.
	origChatFn := llmFake.ChatCompletionWithRetry
	_ = origChatFn // suppress unused warning; we access the fake directly

	sender := &fakeSender{}
	executor := &fakeToolExecutor{definitions: toolDefs}
	ag := newTestAgentWithTools(ws, llmFake, sender, executor)

	// Intercept the tool definitions by checking what toolDefinitions() returns.
	capturedTools = ag.toolDefinitions()
	if len(capturedTools) != 2 {
		t.Fatalf("expected 2 tool definitions, got %d", len(capturedTools))
	}
	if capturedTools[0].Function.Name != "read_file" {
		t.Errorf("expected first tool %q, got %q", "read_file", capturedTools[0].Function.Name)
	}
	if capturedTools[1].Function.Name != "write_file" {
		t.Errorf("expected second tool %q, got %q", "write_file", capturedTools[1].Function.Name)
	}
}

func TestToolDefinitions_NilExecutor(t *testing.T) {
	ws := testWorkspace(t)
	llmFake := &fakeLLM{responses: []*llm.ChatResponse{makeResponse("message", "ok")}}
	sender := &fakeSender{}
	ag := newTestAgent(ws, llmFake, sender)

	defs := ag.toolDefinitions()
	if defs != nil {
		t.Fatalf("expected nil tool definitions with nil executor, got %v", defs)
	}
}

func TestHandleMessage_LLMErrorDuringToolLoop(t *testing.T) {
	ws := testWorkspace(t)
	// First call returns tool call, second call fails.
	llmFake := &fakeLLM{
		responses: []*llm.ChatResponse{
			makeToolCallResponse(tc("call_1", "read_file", `{}`)),
		},
		errs: []error{
			nil,
			errors.New("llm failure during tool loop"),
		},
	}
	sender := &fakeSender{}
	executor := &fakeToolExecutor{
		results:     []tool.ToolResult{{Success: true, Output: "ok"}},
		definitions: []llm.Tool{},
	}
	ag := newTestAgentWithTools(ws, llmFake, sender, executor)

	ctx, cancel := context.WithCancel(context.Background())
	messages := make(chan telegram.TelegramMessage, 1)
	done := make(chan error, 1)
	go func() { done <- ag.Run(ctx, messages) }()

	sendAndWait(t, messages, testMsg(42, "will fail"))
	cancel()
	<-done

	// Tool was executed.
	if len(executor.calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(executor.calls))
	}
	// But no message sent since LLM failed on second call.
	if len(sender.sent) != 0 {
		t.Fatalf("expected 0 sends after LLM error, got %d", len(sender.sent))
	}
}

func TestHandleMessage_NoChoicesDuringToolLoop(t *testing.T) {
	ws := testWorkspace(t)
	// First call returns tool call, second returns no choices.
	llmFake := &fakeLLM{
		responses: []*llm.ChatResponse{
			makeToolCallResponse(tc("call_1", "read_file", `{}`)),
			{Choices: []llm.Choice{}},
		},
	}
	sender := &fakeSender{}
	executor := &fakeToolExecutor{
		results:     []tool.ToolResult{{Success: true, Output: "ok"}},
		definitions: []llm.Tool{},
	}
	ag := newTestAgentWithTools(ws, llmFake, sender, executor)

	ctx, cancel := context.WithCancel(context.Background())
	messages := make(chan telegram.TelegramMessage, 1)
	done := make(chan error, 1)
	go func() { done <- ag.Run(ctx, messages) }()

	sendAndWait(t, messages, testMsg(42, "will get no choices"))
	cancel()
	<-done

	// Tool was executed but no final message sent.
	if len(executor.calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(executor.calls))
	}
	if len(sender.sent) != 0 {
		t.Fatalf("expected 0 sends when no choices in tool loop, got %d", len(sender.sent))
	}
}

// --- File change tests ---

func TestHandleFileChange_Success(t *testing.T) {
	ws := testWorkspace(t)
	original := ws.AgentMD

	origLoad := agentWorkspaceLoadFn
	agentWorkspaceLoadFn = func(root string) (*workspace.Workspace, error) {
		return &workspace.Workspace{
			Root:    root,
			AgentMD: "updated agent",
			SoulMD:  "updated soul",
		}, nil
	}
	defer func() { agentWorkspaceLoadFn = origLoad }()

	ag := New(NewAgentConfig{Workspace: ws, LLM: &fakeLLM{}, Sender: &fakeSender{}})
	ag.handleFileChange(context.Background())

	if ws.AgentMD == original {
		t.Error("expected workspace to be updated after handleFileChange")
	}
	if ws.AgentMD != "updated agent" {
		t.Errorf("expected AgentMD %q, got %q", "updated agent", ws.AgentMD)
	}
	if ws.SoulMD != "updated soul" {
		t.Errorf("expected SoulMD %q, got %q", "updated soul", ws.SoulMD)
	}
}

func TestHandleFileChange_Error(t *testing.T) {
	ws := testWorkspace(t)
	originalAgent := ws.AgentMD
	originalSoul := ws.SoulMD

	origLoad := agentWorkspaceLoadFn
	agentWorkspaceLoadFn = func(root string) (*workspace.Workspace, error) {
		return nil, errors.New("load failed")
	}
	defer func() { agentWorkspaceLoadFn = origLoad }()

	ag := New(NewAgentConfig{Workspace: ws, LLM: &fakeLLM{}, Sender: &fakeSender{}})
	ag.handleFileChange(context.Background())

	if ws.AgentMD != originalAgent {
		t.Errorf("expected AgentMD preserved %q, got %q", originalAgent, ws.AgentMD)
	}
	if ws.SoulMD != originalSoul {
		t.Errorf("expected SoulMD preserved %q, got %q", originalSoul, ws.SoulMD)
	}
}

func TestRun_FileChangeEvent(t *testing.T) {
	ws := testWorkspace(t)

	origLoad := agentWorkspaceLoadFn
	loadCalled := false
	agentWorkspaceLoadFn = func(root string) (*workspace.Workspace, error) {
		loadCalled = true
		return &workspace.Workspace{
			Root:    root,
			AgentMD: "reloaded",
			SoulMD:  "reloaded soul",
		}, nil
	}
	defer func() { agentWorkspaceLoadFn = origLoad }()

	fileChanges := make(chan struct{}, 1)
	ag := New(NewAgentConfig{
		Workspace:   ws,
		LLM:         &fakeLLM{responses: []*llm.ChatResponse{makeResponse("message", "ok")}},
		Sender:      &fakeSender{},
		FileChanges: fileChanges,
	})

	ctx, cancel := context.WithCancel(context.Background())
	messages := make(chan telegram.TelegramMessage, 1)

	done := make(chan error, 1)
	go func() { done <- ag.Run(ctx, messages) }()

	fileChanges <- struct{}{}
	time.Sleep(50 * time.Millisecond)

	cancel()
	<-done

	if !loadCalled {
		t.Error("expected agentWorkspaceLoadFn to be called on file change event")
	}
	if ws.AgentMD != "reloaded" {
		t.Errorf("expected workspace updated to %q, got %q", "reloaded", ws.AgentMD)
	}
}

func TestRun_NilFileChanges(t *testing.T) {
	ws := testWorkspace(t)
	llmFake := &fakeLLM{responses: []*llm.ChatResponse{makeResponse("message", "hello")}}
	sender := &fakeSender{}
	ag := newTestAgent(ws, llmFake, sender)

	ctx, cancel := context.WithCancel(context.Background())
	messages := make(chan telegram.TelegramMessage, 1)

	done := make(chan error, 1)
	go func() { done <- ag.Run(ctx, messages) }()

	sendAndWait(t, messages, testMsg(42, "hi"))
	cancel()
	<-done

	if len(sender.sent) != 1 {
		t.Fatalf("expected 1 sent message with nil FileChanges, got %d", len(sender.sent))
	}
	if sender.sent[0].text != "hello" {
		t.Errorf("expected text %q, got %q", "hello", sender.sent[0].text)
	}
}
