package agent

import (
	"strings"

	"github.com/edouard/pureclaw/internal/llm"
)

const maxHistory = 40 // 20 user+assistant pairs

// systemPrompt combines workspace content with the JSON response format contract.
func (a *Agent) systemPrompt() string {
	var b strings.Builder
	b.WriteString(a.workspace.SystemPrompt())
	b.WriteString("\n\n")
	b.WriteString("## Response Format\n\n")
	b.WriteString("You MUST respond with valid JSON in exactly one of these formats:\n\n")
	b.WriteString(`{"type": "message", "content": "text for user"}` + "\n")
	b.WriteString(`{"type": "think", "content": "internal reasoning"}` + "\n")
	b.WriteString(`{"type": "noop", "content": "nothing to do"}` + "\n\n")
	b.WriteString("Always respond with exactly one JSON object. Never include text outside the JSON.\n")
	return b.String()
}

// buildMessages assembles the full message list for the LLM: system prompt + history + current user message.
func (a *Agent) buildMessages(userText string) []llm.Message {
	msgs := make([]llm.Message, 0, 1+len(a.history)+1)
	msgs = append(msgs, llm.Message{Role: "system", Content: a.systemPrompt()})
	msgs = append(msgs, a.history...)
	msgs = append(msgs, llm.Message{Role: "user", Content: userText})
	return msgs
}

// addToHistory appends a user+assistant exchange and trims to maxHistory.
func (a *Agent) addToHistory(userText, assistantContent string) {
	a.history = append(a.history,
		llm.Message{Role: "user", Content: userText},
		llm.Message{Role: "assistant", Content: assistantContent},
	)
	if len(a.history) > maxHistory {
		a.history = a.history[len(a.history)-maxHistory:]
	}
}
