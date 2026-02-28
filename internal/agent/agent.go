package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/edouard/pureclaw/internal/llm"
	"github.com/edouard/pureclaw/internal/memory"
	"github.com/edouard/pureclaw/internal/platform"
	"github.com/edouard/pureclaw/internal/subagent"
	"github.com/edouard/pureclaw/internal/telegram"
	"github.com/edouard/pureclaw/internal/tool"
	"github.com/edouard/pureclaw/internal/workspace"
)

const maxToolRounds = 10

// Replaceable for testing.
var agentWorkspaceLoadFn = workspace.Load

// LLMClient abstracts the LLM provider for testability.
type LLMClient interface {
	ChatCompletionWithRetry(ctx context.Context, messages []llm.Message, tools []llm.Tool) (*llm.ChatResponse, error)
}

// Sender abstracts the Telegram message sender for testability.
type Sender interface {
	Send(ctx context.Context, chatID int64, text string) error
	React(ctx context.Context, chatID, messageID int64, emoji string) error
}

// MemoryWriter abstracts the memory persistence layer for testability.
type MemoryWriter interface {
	Write(ctx context.Context, source, content string) error
}

// MemorySearcher abstracts memory search and temporal reading capabilities.
type MemorySearcher interface {
	Search(ctx context.Context, keyword string, start, end time.Time) ([]memory.SearchResult, error)
	ReadRange(ctx context.Context, start, end time.Time) ([]memory.SearchResult, error)
}

// ToolExecutor abstracts the tool registry for testability.
type ToolExecutor interface {
	Execute(ctx context.Context, name string, args json.RawMessage) tool.ToolResult
	Definitions() []llm.Tool
}

// HeartbeatExecutor abstracts the heartbeat execution for testability.
type HeartbeatExecutor interface {
	Execute(ctx context.Context, heartbeatContent string) error
}

// Transcriber abstracts the audio transcription provider for testability.
type Transcriber interface {
	Transcribe(ctx context.Context, audioData []byte, filename string) (string, error)
}

// VoiceDownloader abstracts the Telegram voice file download for testability.
type VoiceDownloader interface {
	GetFile(ctx context.Context, fileID string) (string, error)
	DownloadFile(ctx context.Context, filePath string) ([]byte, error)
}

// NewAgentConfig holds all dependencies for Agent construction.
type NewAgentConfig struct {
	Workspace       *workspace.Workspace
	LLM             LLMClient
	Sender          Sender
	Memory          MemoryWriter
	MemorySearcher  MemorySearcher
	ToolExecutor    ToolExecutor
	FileChanges     <-chan struct{}
	HeartbeatTick   <-chan time.Time
	Heartbeat       HeartbeatExecutor
	Transcriber     Transcriber
	VoiceDownloader VoiceDownloader
	SubAgentResults <-chan subagent.SubAgentResult
	OwnerIDs        []int64 // Telegram chat IDs for unsolicited messages (sub-agent results)
}

// Agent orchestrates the event loop: receives messages, calls LLM, sends responses.
type Agent struct {
	workspace       *workspace.Workspace
	llm             LLMClient
	sender          Sender
	memory          MemoryWriter
	memorySearcher  MemorySearcher
	toolExecutor    ToolExecutor
	fileChanges     <-chan struct{}
	heartbeatTick   <-chan time.Time
	heartbeat       HeartbeatExecutor
	transcriber     Transcriber
	voiceDownloader VoiceDownloader
	subAgentResults <-chan subagent.SubAgentResult
	ownerIDs        []int64 // Telegram chat IDs for unsolicited messages
	history         []llm.Message
}

// New creates a new Agent with the given dependencies.
func New(cfg NewAgentConfig) *Agent {
	return &Agent{
		workspace:       cfg.Workspace,
		llm:             cfg.LLM,
		sender:          cfg.Sender,
		memory:          cfg.Memory,
		memorySearcher:  cfg.MemorySearcher,
		toolExecutor:    cfg.ToolExecutor,
		fileChanges:     cfg.FileChanges,
		heartbeatTick:   cfg.HeartbeatTick,
		heartbeat:       cfg.Heartbeat,
		transcriber:     cfg.Transcriber,
		voiceDownloader: cfg.VoiceDownloader,
		subAgentResults: cfg.SubAgentResults,
		ownerIDs:        cfg.OwnerIDs,
	}
}

// Run starts the event loop, processing messages sequentially until the context is cancelled.
func (a *Agent) Run(ctx context.Context, messages <-chan telegram.TelegramMessage) error {
	slog.Info("event loop started", "component", "agent", "operation", "run")

	if err := a.runIntrospectionIfNeeded(ctx); err != nil {
		slog.Warn("introspection failed",
			"component", "agent",
			"operation", "introspection",
			"error", err,
		)
	}

	for {
		select {
		case <-ctx.Done():
			slog.Info("event loop stopped", "component", "agent", "operation", "run")
			return nil
		case msg := <-messages:
			a.handleMessage(ctx, msg)
		case <-a.fileChanges:
			a.handleFileChange(ctx)
		case <-a.heartbeatTick:
			a.handleHeartbeat(ctx)
		case result := <-a.subAgentResults:
			a.handleSubAgentResult(ctx, result)
		}
	}
}

// handleMessage processes a single incoming Telegram message through the LLM pipeline.
func (a *Agent) handleMessage(ctx context.Context, msg telegram.TelegramMessage) {
	// Skip zero-value messages (closed channel).
	if msg.Message.Text == "" && msg.Message.Voice == nil && msg.Message.Chat.ID == 0 {
		slog.Debug("skipping empty message", "component", "agent", "operation", "handle_message")
		return
	}

	slog.Info("processing message",
		"component", "agent",
		"operation", "handle_message",
		"chat_id", msg.Message.Chat.ID,
	)

	// Acknowledge receipt with a reaction emoji.
	if a.sender != nil {
		if err := a.sender.React(ctx, msg.Message.Chat.ID, msg.Message.MessageID, "\U0001F440"); err != nil {
			slog.Debug("failed to set reaction", "component", "agent", "operation", "react", "error", err)
		}
	}

	// Determine user text — either from text or voice transcription.
	userText := msg.Message.Text
	if msg.Message.Voice != nil {
		transcribed, err := a.transcribeVoice(ctx, msg.Message.Voice.FileID)
		if err != nil {
			slog.Error("voice transcription failed",
				"component", "agent",
				"operation", "transcribe_voice",
				"error", err,
			)
			a.sender.Send(ctx, msg.Message.Chat.ID,
				fmt.Sprintf("Failed to transcribe voice message: %v", err))
			return
		}
		userText = transcribed
		slog.Info("voice message transcribed",
			"component", "agent",
			"operation", "transcribe_voice",
			"duration", msg.Message.Voice.Duration,
		)
	}

	// Skip if still no text after voice transcription.
	if userText == "" {
		return
	}

	if msg.Message.Voice != nil {
		a.logMemory(ctx, "voice-transcription", userText)
	} else {
		a.logMemory(ctx, "owner", userText)
	}

	msgs := a.buildMessages(userText)
	tools := a.toolDefinitions()

	var resp *llm.ChatResponse
	var err error

	for round := range maxToolRounds {
		resp, err = a.llm.ChatCompletionWithRetry(ctx, msgs, tools)
		if err != nil {
			slog.Error("LLM call failed",
				"component", "agent",
				"operation", "handle_message",
				"error", err,
			)
			return
		}

		if len(resp.Choices) == 0 {
			slog.Error("LLM returned no choices",
				"component", "agent",
				"operation", "handle_message",
			)
			return
		}

		if !llm.HasToolCalls(&resp.Choices[0]) {
			break
		}

		if a.toolExecutor == nil {
			slog.Warn("LLM returned tool calls but no executor configured",
				"component", "agent",
				"operation", "handle_message",
			)
			return
		}

		toolMsgs := a.executeToolCalls(ctx, resp.Choices[0].Message)
		assistantMsg := resp.Choices[0].Message
		normalizeToolCallTypes(&assistantMsg)
		msgs = append(msgs, assistantMsg)
		msgs = append(msgs, toolMsgs...)

		slog.Info("tool round completed",
			"component", "agent",
			"operation", "handle_message",
			"round", round+1,
			"tool_calls", len(resp.Choices[0].Message.ToolCalls),
		)
	}

	// Check if loop exhausted without a text response.
	if llm.HasToolCalls(&resp.Choices[0]) {
		slog.Warn("max tool rounds exceeded without final response",
			"component", "agent",
			"operation", "handle_message",
			"max_rounds", maxToolRounds,
		)
		return
	}

	content := resp.Choices[0].Message.Content
	agentResp, err := llm.ParseAgentResponse(content)
	if err != nil {
		slog.Error("failed to parse agent response",
			"component", "agent",
			"operation", "handle_message",
			"error", err,
		)
		return
	}

	switch agentResp.Type {
	case "message":
		if err := a.sender.Send(ctx, msg.Message.Chat.ID, agentResp.Content); err != nil {
			slog.Error("failed to send message",
				"component", "agent",
				"operation", "handle_message",
				"error", err,
			)
		}
		a.logMemory(ctx, "agent", agentResp.Content)
		a.addToHistory(userText, agentResp.Content)
	case "think":
		slog.Debug("think response",
			"component", "agent",
			"operation", "handle_message",
			"content", agentResp.Content,
		)
	case "noop":
		slog.Debug("noop response",
			"component", "agent",
			"operation", "handle_message",
		)
	}
}

// executeToolCalls runs each tool call and returns tool result messages.
func (a *Agent) executeToolCalls(ctx context.Context, assistantMsg llm.Message) []llm.Message {
	var toolMsgs []llm.Message
	for _, tc := range assistantMsg.ToolCalls {
		result := a.toolExecutor.Execute(ctx, tc.Function.Name, json.RawMessage(tc.Function.Arguments))

		resultJSON, _ := json.Marshal(result)

		toolMsgs = append(toolMsgs, llm.Message{
			Role:       "tool",
			Content:    string(resultJSON),
			Name:       tc.Function.Name,
			ToolCallID: tc.ID,
		})

		slog.Info("tool executed",
			"component", "agent",
			"operation", "execute_tool",
			"tool_name", tc.Function.Name,
			"tool_call_id", tc.ID,
			"success", result.Success,
		)
	}
	return toolMsgs
}

// toolDefinitions returns LLM tool definitions if a tool executor is configured.
func (a *Agent) toolDefinitions() []llm.Tool {
	if a.toolExecutor == nil {
		return nil
	}
	return a.toolExecutor.Definitions()
}

// handleFileChange reloads the workspace from disk after a file change is detected.
func (a *Agent) handleFileChange(ctx context.Context) {
	slog.Info("workspace file change detected",
		"component", "agent",
		"operation", "file_change",
	)

	newWS, err := agentWorkspaceLoadFn(a.workspace.Root)
	if err != nil {
		slog.Error("workspace reload failed on file change",
			"component", "agent",
			"operation", "file_change",
			"error", err,
		)
		return
	}

	*a.workspace = *newWS

	slog.Info("workspace hot-reloaded",
		"component", "agent",
		"operation", "file_change",
		"skills", len(a.workspace.Skills),
	)
}

// handleHeartbeat runs one heartbeat cycle using the configured executor.
func (a *Agent) handleHeartbeat(ctx context.Context) {
	if a.heartbeat == nil {
		slog.Warn("heartbeat tick received but no executor configured",
			"component", "agent",
			"operation", "heartbeat",
		)
		return
	}

	heartbeatContent := a.workspace.HeartbeatMD
	if heartbeatContent == "" {
		slog.Warn("heartbeat tick received but HEARTBEAT.md is empty",
			"component", "agent",
			"operation", "heartbeat",
		)
		return
	}

	slog.Info("heartbeat cycle starting",
		"component", "agent",
		"operation", "heartbeat",
	)

	if err := a.heartbeat.Execute(ctx, heartbeatContent); err != nil {
		slog.Error("heartbeat execution failed",
			"component", "agent",
			"operation", "heartbeat",
			"error", err,
		)
	}
}

// transcribeVoice downloads a voice file from Telegram and transcribes it via the Voxtral API.
func (a *Agent) transcribeVoice(ctx context.Context, fileID string) (string, error) {
	if a.voiceDownloader == nil || a.transcriber == nil {
		return "", fmt.Errorf("voice transcription not configured")
	}

	filePath, err := a.voiceDownloader.GetFile(ctx, fileID)
	if err != nil {
		return "", fmt.Errorf("get voice file path: %w", err)
	}

	audioData, err := a.voiceDownloader.DownloadFile(ctx, filePath)
	if err != nil {
		return "", fmt.Errorf("download voice file: %w", err)
	}

	text, err := a.transcriber.Transcribe(ctx, audioData, "voice.ogg")
	if err != nil {
		return "", fmt.Errorf("transcribe audio: %w", err)
	}

	return text, nil
}

// handleSubAgentResult processes the result of a completed sub-agent.
// Sends result summary to owner via Telegram and logs to memory.
func (a *Agent) handleSubAgentResult(ctx context.Context, result subagent.SubAgentResult) {
	slog.Info("sub-agent completed",
		"component", "agent", "operation", "handle_sub_agent_result",
		"task_id", result.TaskID, "timed_out", result.TimedOut,
		"has_result", result.ResultContent != "")

	var memoryEntry string
	var telegramMsg string

	switch {
	case result.TimedOut && result.ResultContent != "":
		memoryEntry = fmt.Sprintf("Sub-agent '%s' timed out but partial result collected (%d bytes).", result.TaskID, len(result.ResultContent))
		content := truncateForTelegram(result.ResultContent)
		telegramMsg = fmt.Sprintf("[Sub-agent '%s' timed out — partial result]\n\n%s", result.TaskID, content)
	case result.TimedOut:
		memoryEntry = fmt.Sprintf("Sub-agent '%s' timed out. No result collected.", result.TaskID)
		telegramMsg = fmt.Sprintf("[Sub-agent '%s' timed out — no result produced]", result.TaskID)
	case result.Err != nil:
		memoryEntry = fmt.Sprintf("Sub-agent '%s' failed: %s", result.TaskID, result.Err)
		telegramMsg = fmt.Sprintf("[Sub-agent '%s' failed: %s]", result.TaskID, result.Err)
	default:
		memoryEntry = fmt.Sprintf("Sub-agent '%s' completed successfully.", result.TaskID)
		if result.ResultContent != "" {
			content := truncateForTelegram(result.ResultContent)
			telegramMsg = fmt.Sprintf("[Sub-agent '%s' completed]\n\n%s", result.TaskID, content)
		} else {
			telegramMsg = fmt.Sprintf("[Sub-agent '%s' completed — no output produced]", result.TaskID)
		}
	}

	a.logMemory(ctx, "sub-agent-result", memoryEntry)

	// Send to Telegram if sender is available (not in sub-agent mode).
	if a.sender != nil {
		for _, id := range a.ownerIDs {
			if err := a.sender.Send(ctx, id, telegramMsg); err != nil {
				slog.Error("failed to send sub-agent result to Telegram",
					"component", "agent", "operation", "handle_sub_agent_result",
					"task_id", result.TaskID, "chat_id", id, "error", err)
			}
		}
	}
}

// truncateForTelegram limits text to a reasonable Telegram message size.
// Uses rune count to avoid splitting multi-byte UTF-8 characters.
func truncateForTelegram(text string) string {
	const maxRunes = 3500 // Telegram max is 4096, leave room for prefix
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return string(runes[:maxRunes]) + "\n\n[...truncated]"
}

// RunSubAgent runs the agent in autonomous sub-agent mode.
// It reads the mission from AGENT.md, processes it through the LLM pipeline,
// and writes the result to result.md in the workspace root.
func (a *Agent) RunSubAgent(ctx context.Context) error {
	slog.Info("sub-agent autonomous mode started",
		"component", "agent", "operation", "run_subagent")

	mission := a.workspace.AgentMD
	if mission == "" {
		return fmt.Errorf("no AGENT.md mission found in workspace")
	}

	// Build messages with mission as user message.
	msgs := a.buildMessages(mission)
	tools := a.toolDefinitions()

	var lastContent string
	exhausted := true

	for round := range maxToolRounds {
		resp, err := a.llm.ChatCompletionWithRetry(ctx, msgs, tools)
		if err != nil {
			return fmt.Errorf("LLM call failed (round %d): %w", round+1, err)
		}

		if len(resp.Choices) == 0 {
			return fmt.Errorf("LLM returned no choices (round %d)", round+1)
		}

		if !llm.HasToolCalls(&resp.Choices[0]) {
			lastContent = resp.Choices[0].Message.Content
			exhausted = false
			break
		}

		if a.toolExecutor == nil {
			return fmt.Errorf("LLM returned tool calls but no executor configured")
		}

		toolMsgs := a.executeToolCalls(ctx, resp.Choices[0].Message)
		assistantMsg := resp.Choices[0].Message
		normalizeToolCallTypes(&assistantMsg)
		msgs = append(msgs, assistantMsg)
		msgs = append(msgs, toolMsgs...)

		slog.Info("sub-agent tool round completed",
			"component", "agent", "operation", "run_subagent",
			"round", round+1,
			"tool_calls", len(resp.Choices[0].Message.ToolCalls))
	}

	// Check if tool rounds were exhausted without a final text response.
	if exhausted {
		slog.Warn("sub-agent exhausted tool rounds without producing a result",
			"component", "agent", "operation", "run_subagent",
			"max_rounds", maxToolRounds)
		return fmt.Errorf("sub-agent exhausted %d tool rounds without producing a result", maxToolRounds)
	}

	// Parse the LLM response to extract content.
	if lastContent != "" {
		agentResp, err := llm.ParseAgentResponse(lastContent)
		if err != nil {
			slog.Warn("failed to parse sub-agent response, using raw content",
				"component", "agent", "operation", "run_subagent",
				"error", err)
			// Keep lastContent as-is (raw LLM output as fallback).
		} else {
			lastContent = agentResp.Content
		}
	}

	// Write result.md via AtomicWrite.
	resultPath := filepath.Join(a.workspace.Root, "result.md")
	if lastContent != "" {
		if err := platform.AtomicWrite(resultPath, []byte(lastContent), 0644); err != nil {
			return fmt.Errorf("write result.md: %w", err)
		}
		slog.Info("sub-agent result written",
			"component", "agent", "operation", "run_subagent",
			"path", resultPath, "bytes", len(lastContent))
	} else {
		slog.Warn("sub-agent completed without generating a result",
			"component", "agent", "operation", "run_subagent")
	}

	a.logMemory(ctx, "sub-agent", "Mission completed")
	slog.Info("sub-agent autonomous mode completed",
		"component", "agent", "operation", "run_subagent")
	return nil
}

// normalizeToolCallTypes ensures all tool calls have Type set to "function".
// Some LLM providers omit the type field in responses; Mistral rejects empty type on re-send.
func normalizeToolCallTypes(msg *llm.Message) {
	for i := range msg.ToolCalls {
		if msg.ToolCalls[i].Type == "" {
			msg.ToolCalls[i].Type = "function"
		}
	}
}

func (a *Agent) logMemory(ctx context.Context, source, content string) {
	if a.memory == nil {
		return
	}
	if err := a.memory.Write(ctx, source, content); err != nil {
		slog.Error("failed to write memory",
			"component", "agent",
			"operation", "log_memory",
			"source", source,
			"error", err,
		)
	}
}
