package agent

import (
	"strings"
	"testing"

	"github.com/edouard/pureclaw/internal/workspace"
)

func TestSystemPrompt_Format(t *testing.T) {
	ws := &workspace.Workspace{
		Root:    t.TempDir(),
		SoulMD:  "You are a soul.",
		AgentMD: "# Agent Config",
	}
	ag := New(NewAgentConfig{Workspace: ws})

	prompt := ag.systemPrompt()

	// Should contain workspace system prompt content.
	if !strings.Contains(prompt, "You are a soul.") {
		t.Error("expected system prompt to contain soul content")
	}
	if !strings.Contains(prompt, "# Agent Config") {
		t.Error("expected system prompt to contain agent config")
	}

	// Should contain response format instructions.
	if !strings.Contains(prompt, "## Response Format") {
		t.Error("expected system prompt to contain response format header")
	}
	if !strings.Contains(prompt, `{"type": "message"`) {
		t.Error("expected system prompt to contain message type example")
	}
	if !strings.Contains(prompt, `{"type": "think"`) {
		t.Error("expected system prompt to contain think type example")
	}
	if !strings.Contains(prompt, `{"type": "noop"`) {
		t.Error("expected system prompt to contain noop type example")
	}
	if !strings.Contains(prompt, "exactly one JSON object") {
		t.Error("expected system prompt to contain JSON instruction")
	}
}

func TestBuildMessages_Empty(t *testing.T) {
	ws := &workspace.Workspace{
		Root:    t.TempDir(),
		SoulMD:  "Soul",
		AgentMD: "Agent",
	}
	ag := New(NewAgentConfig{Workspace: ws})

	msgs := ag.buildMessages("hello")

	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages (system + user), got %d", len(msgs))
	}
	if msgs[0].Role != "system" {
		t.Errorf("expected first message role %q, got %q", "system", msgs[0].Role)
	}
	if msgs[1].Role != "user" {
		t.Errorf("expected second message role %q, got %q", "user", msgs[1].Role)
	}
	if msgs[1].Content != "hello" {
		t.Errorf("expected user content %q, got %q", "hello", msgs[1].Content)
	}
}

func TestBuildMessages_WithHistory(t *testing.T) {
	ws := &workspace.Workspace{
		Root:    t.TempDir(),
		SoulMD:  "Soul",
		AgentMD: "Agent",
	}
	ag := New(NewAgentConfig{Workspace: ws})

	ag.addToHistory("q1", "a1")
	ag.addToHistory("q2", "a2")

	msgs := ag.buildMessages("q3")

	// system + 4 history + user = 6
	if len(msgs) != 6 {
		t.Fatalf("expected 6 messages, got %d", len(msgs))
	}
	if msgs[0].Role != "system" {
		t.Errorf("expected system role, got %q", msgs[0].Role)
	}
	if msgs[1].Content != "q1" {
		t.Errorf("expected history[0] content %q, got %q", "q1", msgs[1].Content)
	}
	if msgs[2].Content != "a1" {
		t.Errorf("expected history[1] content %q, got %q", "a1", msgs[2].Content)
	}
	if msgs[5].Content != "q3" {
		t.Errorf("expected user content %q, got %q", "q3", msgs[5].Content)
	}
}

func TestAddToHistory_Basic(t *testing.T) {
	ws := &workspace.Workspace{Root: t.TempDir(), SoulMD: "S", AgentMD: "A"}
	ag := New(NewAgentConfig{Workspace: ws})

	ag.addToHistory("question", "answer")

	if len(ag.history) != 2 {
		t.Fatalf("expected 2 history entries, got %d", len(ag.history))
	}
	if ag.history[0].Role != "user" || ag.history[0].Content != "question" {
		t.Errorf("expected user message, got %+v", ag.history[0])
	}
	if ag.history[1].Role != "assistant" || ag.history[1].Content != "answer" {
		t.Errorf("expected assistant message, got %+v", ag.history[1])
	}
}

func TestAddToHistory_Trim(t *testing.T) {
	ws := &workspace.Workspace{Root: t.TempDir(), SoulMD: "S", AgentMD: "A"}
	ag := New(NewAgentConfig{Workspace: ws})

	// Add 21 exchanges (42 messages), should trim to maxHistory (40).
	for i := 0; i < 21; i++ {
		ag.addToHistory("q", "a")
	}

	if len(ag.history) != maxHistory {
		t.Fatalf("expected history trimmed to %d, got %d", maxHistory, len(ag.history))
	}
}

func TestAddToHistory_ExactMax(t *testing.T) {
	ws := &workspace.Workspace{Root: t.TempDir(), SoulMD: "S", AgentMD: "A"}
	ag := New(NewAgentConfig{Workspace: ws})

	// Add exactly 20 exchanges (40 messages) — no trim needed.
	for i := 0; i < 20; i++ {
		ag.addToHistory("q", "a")
	}

	if len(ag.history) != maxHistory {
		t.Fatalf("expected history length %d, got %d", maxHistory, len(ag.history))
	}
}
