package heartbeat

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/edouard/pureclaw/internal/llm"
)

// --- Test doubles ---

type fakeLLM struct {
	resp     *llm.ChatResponse
	err      error
	messages []llm.Message
	tools    []llm.Tool
	called   bool
}

func (f *fakeLLM) ChatCompletionWithRetry(ctx context.Context, messages []llm.Message, tools []llm.Tool) (*llm.ChatResponse, error) {
	f.called = true
	f.messages = messages
	f.tools = tools
	return f.resp, f.err
}

type sentMsg struct {
	chatID int64
	text   string
}

type fakeSender struct {
	sent []sentMsg
	errs map[int64]error // per-chatID errors
}

func (f *fakeSender) Send(ctx context.Context, chatID int64, text string) error {
	f.sent = append(f.sent, sentMsg{chatID, text})
	if f.errs != nil {
		if err, ok := f.errs[chatID]; ok {
			return err
		}
	}
	return nil
}

type memEntry struct {
	source  string
	content string
}

type fakeMemory struct {
	entries []memEntry
	err     error
}

func (f *fakeMemory) Write(ctx context.Context, source, content string) error {
	f.entries = append(f.entries, memEntry{source, content})
	return f.err
}

// --- Helpers ---

func makeResp(typ, content string) *llm.ChatResponse {
	return &llm.ChatResponse{
		Choices: []llm.Choice{{
			Message: llm.Message{
				Content: `{"type":"` + typ + `","content":"` + content + `"}`,
			},
			FinishReason: "stop",
		}},
	}
}

// --- Tests ---

func TestNewExecutor(t *testing.T) {
	l := &fakeLLM{}
	s := &fakeSender{}
	m := &fakeMemory{}
	ids := []int64{111, 222}

	e := NewExecutor(l, s, m, ids)

	if e.llm != l {
		t.Error("expected llm to be set")
	}
	if e.sender != s {
		t.Error("expected sender to be set")
	}
	if e.memory != m {
		t.Error("expected memory to be set")
	}
	if len(e.ownerIDs) != 2 || e.ownerIDs[0] != 111 || e.ownerIDs[1] != 222 {
		t.Errorf("expected ownerIDs [111, 222], got %v", e.ownerIDs)
	}
}

func TestExecute_AlertSent(t *testing.T) {
	l := &fakeLLM{resp: makeResp("message", "disk full")}
	s := &fakeSender{}
	m := &fakeMemory{}
	ids := []int64{42, 99}
	e := NewExecutor(l, s, m, ids)

	err := e.Execute(context.Background(), "Check disk space")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Sender called for each owner ID.
	if len(s.sent) != 2 {
		t.Fatalf("expected 2 sends, got %d", len(s.sent))
	}
	if s.sent[0].chatID != 42 || s.sent[0].text != "disk full" {
		t.Errorf("sent[0] = %+v, want {42, disk full}", s.sent[0])
	}
	if s.sent[1].chatID != 99 || s.sent[1].text != "disk full" {
		t.Errorf("sent[1] = %+v, want {99, disk full}", s.sent[1])
	}

	// Memory logged.
	if len(m.entries) != 1 {
		t.Fatalf("expected 1 memory entry, got %d", len(m.entries))
	}
	if m.entries[0].source != "heartbeat" {
		t.Errorf("expected source 'heartbeat', got %q", m.entries[0].source)
	}
	if !strings.Contains(m.entries[0].content, "disk full") {
		t.Errorf("expected memory content to contain 'disk full', got %q", m.entries[0].content)
	}
}

func TestExecute_Silent(t *testing.T) {
	l := &fakeLLM{resp: makeResp("noop", "all clear")}
	s := &fakeSender{}
	m := &fakeMemory{}
	e := NewExecutor(l, s, m, []int64{42})

	err := e.Execute(context.Background(), "Check stuff")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(s.sent) != 0 {
		t.Fatalf("expected 0 sends for noop, got %d", len(s.sent))
	}

	if len(m.entries) != 1 {
		t.Fatalf("expected 1 memory entry, got %d", len(m.entries))
	}
	if !strings.Contains(m.entries[0].content, "all clear") {
		t.Errorf("expected memory to contain 'all clear', got %q", m.entries[0].content)
	}
}

func TestExecute_ThinkTreatedAsSilent(t *testing.T) {
	l := &fakeLLM{resp: makeResp("think", "reasoning about things")}
	s := &fakeSender{}
	m := &fakeMemory{}
	e := NewExecutor(l, s, m, []int64{42})

	err := e.Execute(context.Background(), "Check stuff")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(s.sent) != 0 {
		t.Fatalf("expected 0 sends for think, got %d", len(s.sent))
	}

	if len(m.entries) != 1 {
		t.Fatalf("expected 1 memory entry, got %d", len(m.entries))
	}
}

func TestExecute_EmptyContent(t *testing.T) {
	l := &fakeLLM{resp: makeResp("message", "should not happen")}
	s := &fakeSender{}
	m := &fakeMemory{}
	e := NewExecutor(l, s, m, []int64{42})

	err := e.Execute(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if l.called {
		t.Error("expected LLM not to be called with empty content")
	}
	if len(s.sent) != 0 {
		t.Errorf("expected 0 sends, got %d", len(s.sent))
	}
	if len(m.entries) != 0 {
		t.Errorf("expected 0 memory entries, got %d", len(m.entries))
	}
}

func TestExecute_LLMError(t *testing.T) {
	l := &fakeLLM{err: errors.New("api timeout")}
	s := &fakeSender{}
	m := &fakeMemory{}
	e := NewExecutor(l, s, m, []int64{42})

	err := e.Execute(context.Background(), "Check stuff")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "api timeout") {
		t.Errorf("expected error to contain 'api timeout', got %q", err.Error())
	}

	if len(s.sent) != 0 {
		t.Errorf("expected 0 sends on LLM error, got %d", len(s.sent))
	}
}

func TestExecute_SenderError(t *testing.T) {
	l := &fakeLLM{resp: makeResp("message", "alert!")}
	s := &fakeSender{
		errs: map[int64]error{
			42: errors.New("send failed"),
		},
	}
	m := &fakeMemory{}
	e := NewExecutor(l, s, m, []int64{42, 99, 77})

	err := e.Execute(context.Background(), "Check stuff")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// All 3 sends attempted despite error on ID 42.
	if len(s.sent) != 3 {
		t.Fatalf("expected 3 send attempts, got %d", len(s.sent))
	}

	// Memory still written.
	if len(m.entries) != 1 {
		t.Fatalf("expected 1 memory entry, got %d", len(m.entries))
	}
}

func TestExecute_MultipleOwnerIDs(t *testing.T) {
	l := &fakeLLM{resp: makeResp("message", "warning")}
	s := &fakeSender{}
	m := &fakeMemory{}
	ids := []int64{10, 20, 30}
	e := NewExecutor(l, s, m, ids)

	err := e.Execute(context.Background(), "Check stuff")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(s.sent) != 3 {
		t.Fatalf("expected 3 sends, got %d", len(s.sent))
	}
	for i, id := range ids {
		if s.sent[i].chatID != id {
			t.Errorf("sent[%d].chatID = %d, want %d", i, s.sent[i].chatID, id)
		}
		if s.sent[i].text != "warning" {
			t.Errorf("sent[%d].text = %q, want %q", i, s.sent[i].text, "warning")
		}
	}
}

func TestExecute_SystemPromptContainsChecklist(t *testing.T) {
	l := &fakeLLM{resp: makeResp("noop", "ok")}
	s := &fakeSender{}
	m := &fakeMemory{}
	e := NewExecutor(l, s, m, []int64{42})

	checklist := "- [ ] Check if server is running\n- [ ] Check disk space > 20%"
	err := e.Execute(context.Background(), checklist)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !l.called {
		t.Fatal("expected LLM to be called")
	}

	// First message should be system prompt containing the checklist.
	if len(l.messages) < 2 {
		t.Fatalf("expected at least 2 messages, got %d", len(l.messages))
	}
	sysMsg := l.messages[0]
	if sysMsg.Role != "system" {
		t.Errorf("expected first message role 'system', got %q", sysMsg.Role)
	}
	if !strings.Contains(sysMsg.Content, "Check if server is running") {
		t.Error("expected system prompt to contain checklist item")
	}
	if !strings.Contains(sysMsg.Content, "Check disk space > 20%") {
		t.Error("expected system prompt to contain second checklist item")
	}
	if !strings.Contains(sysMsg.Content, "HEARTBEAT CHECKLIST") {
		t.Error("expected system prompt to contain 'HEARTBEAT CHECKLIST' header")
	}
}

func TestExecute_NoToolsPassedToLLM(t *testing.T) {
	l := &fakeLLM{resp: makeResp("noop", "ok")}
	s := &fakeSender{}
	m := &fakeMemory{}
	e := NewExecutor(l, s, m, []int64{42})

	err := e.Execute(context.Background(), "Check stuff")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if l.tools != nil {
		t.Errorf("expected nil tools, got %v", l.tools)
	}
}

func TestExecute_NoChoices(t *testing.T) {
	l := &fakeLLM{resp: &llm.ChatResponse{Choices: []llm.Choice{}}}
	s := &fakeSender{}
	m := &fakeMemory{}
	e := NewExecutor(l, s, m, []int64{42})

	err := e.Execute(context.Background(), "Check stuff")
	if err == nil {
		t.Fatal("expected error for no choices, got nil")
	}
	if !strings.Contains(err.Error(), "no choices") {
		t.Errorf("expected error about no choices, got %q", err.Error())
	}
}

func TestExecute_PlainTextFallback(t *testing.T) {
	// ParseAgentResponse wraps non-JSON text as a "message", so it gets sent.
	l := &fakeLLM{resp: &llm.ChatResponse{
		Choices: []llm.Choice{{
			Message:      llm.Message{Content: "not valid json"},
			FinishReason: "stop",
		}},
	}}
	s := &fakeSender{}
	m := &fakeMemory{}
	e := NewExecutor(l, s, m, []int64{42})

	err := e.Execute(context.Background(), "Check stuff")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(s.sent) != 1 {
		t.Fatalf("expected 1 send (plain text fallback), got %d", len(s.sent))
	}
	if s.sent[0].text != "not valid json" {
		t.Errorf("expected fallback text, got %q", s.sent[0].text)
	}
}

func TestExecute_NilMemory(t *testing.T) {
	l := &fakeLLM{resp: makeResp("message", "alert")}
	s := &fakeSender{}
	e := NewExecutor(l, s, nil, []int64{42})

	err := e.Execute(context.Background(), "Check stuff")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Sender still called, no panic from nil memory.
	if len(s.sent) != 1 {
		t.Fatalf("expected 1 send, got %d", len(s.sent))
	}
}

func TestExecute_EmptyOwnerIDs(t *testing.T) {
	l := &fakeLLM{resp: makeResp("message", "alert!")}
	s := &fakeSender{}
	m := &fakeMemory{}
	e := NewExecutor(l, s, m, []int64{})

	err := e.Execute(context.Background(), "Check stuff")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// No sends attempted with empty owner IDs.
	if len(s.sent) != 0 {
		t.Fatalf("expected 0 sends with empty ownerIDs, got %d", len(s.sent))
	}

	// Memory still written.
	if len(m.entries) != 1 {
		t.Fatalf("expected 1 memory entry, got %d", len(m.entries))
	}
}

func TestExecute_MemoryWriteError(t *testing.T) {
	l := &fakeLLM{resp: makeResp("noop", "ok")}
	s := &fakeSender{}
	m := &fakeMemory{err: errors.New("disk full")}
	e := NewExecutor(l, s, m, []int64{42})

	err := e.Execute(context.Background(), "Check stuff")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Memory Write was still attempted.
	if len(m.entries) != 1 {
		t.Fatalf("expected 1 memory write attempt, got %d", len(m.entries))
	}
}
