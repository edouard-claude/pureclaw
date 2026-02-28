package agent

import (
	"fmt"
	"strings"

	"github.com/edouard/pureclaw/internal/llm"
)

const maxHistory = 40 // 20 user+assistant pairs

// systemPrompt combines workspace content with the JSON response format contract.
func (a *Agent) systemPrompt() string {
	var b strings.Builder
	b.WriteString(a.workspace.SystemPrompt())
	b.WriteString("\n\n")
	b.WriteString("## Workspace Files\n\n")
	b.WriteString(fmt.Sprintf("Root: %s\n", a.workspace.Root))
	b.WriteString(fmt.Sprintf("- AGENT.md: %s/AGENT.md\n", a.workspace.Root))
	b.WriteString(fmt.Sprintf("- SOUL.md: %s/SOUL.md\n", a.workspace.Root))
	b.WriteString(fmt.Sprintf("- HEARTBEAT.md: %s/HEARTBEAT.md\n", a.workspace.Root))
	b.WriteString(fmt.Sprintf("- Skills directory: %s/skills/\n", a.workspace.Root))
	b.WriteString("\nYou can use read_file and write_file to read and modify these files. ")
	b.WriteString("After modifying any workspace file, call reload_workspace to apply changes immediately.\n")
	b.WriteString("\n")
	b.WriteString("## Response Format\n\n")
	b.WriteString("When you are NOT calling a tool, you MUST respond with a single valid JSON object and absolutely nothing else.\n")
	b.WriteString("No markdown, no explanation, no text before or after the JSON.\n\n")
	b.WriteString("The JSON object MUST have exactly two fields: \"type\" and \"content\".\n")
	b.WriteString("\"type\" MUST be one of: \"message\", \"think\", or \"noop\".\n\n")
	b.WriteString("Examples:\n")
	b.WriteString(`{"type": "message", "content": "text for user"}` + "\n")
	b.WriteString(`{"type": "think", "content": "internal reasoning"}` + "\n")
	b.WriteString(`{"type": "noop", "content": "nothing to do"}` + "\n\n")
	b.WriteString("## Message Formatting\n\n")
	b.WriteString("Messages are sent via Telegram with parse_mode HTML. Use ONLY Telegram HTML tags:\n")
	b.WriteString("<b>bold</b>, <i>italic</i>, <u>underline</u>, <s>strikethrough</s>, ")
	b.WriteString("<code>inline code</code>, <pre>code block</pre>, ")
	b.WriteString("<a href=\"url\">link</a>, <blockquote>quote</blockquote>\n")
	b.WriteString("NEVER use Markdown syntax (no *, **, `, ```, #). Always use HTML tags.\n")
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
