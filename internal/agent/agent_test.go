package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/edouard/pureclaw/internal/llm"
	"github.com/edouard/pureclaw/internal/telegram"
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

	// Should not send anything — tool calls not yet supported.
	if len(sender.sent) != 0 {
		t.Fatalf("expected 0 sends for tool calls, got %d", len(sender.sent))
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
