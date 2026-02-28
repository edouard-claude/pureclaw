package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
)

// Sender sends messages via the Telegram Bot API.
type Sender struct {
	client *Client
}

// NewSender creates a new Sender.
func NewSender(client *Client) *Sender {
	return &Sender{client: client}
}

// Send sends a text message to the specified chat.
func (s *Sender) Send(ctx context.Context, chatID int64, text string) error {
	slog.Debug("sending message", "component", "telegram", "operation", "send", "chat_id", chatID)

	body := sendMessageRequest{
		ChatID:    chatID,
		Text:      text,
		ParseMode: "HTML",
	}

	data, err := s.client.doPost(ctx, "sendMessage", body)
	if err != nil {
		return fmt.Errorf("telegram: send: %w", err)
	}

	var resp apiResponse[Message]
	if err := json.Unmarshal(data, &resp); err != nil {
		return fmt.Errorf("telegram: send: unmarshal: %w", err)
	}

	if !resp.Ok {
		return fmt.Errorf("telegram: send: %s", resp.Description)
	}

	slog.Debug("message sent", "component", "telegram", "operation", "send", "message_id", resp.Result.MessageID)
	return nil
}

// React sets an emoji reaction on a message.
func (s *Sender) React(ctx context.Context, chatID, messageID int64, emoji string) error {
	slog.Debug("setting reaction", "component", "telegram", "operation", "react", "chat_id", chatID, "emoji", emoji)

	body := setMessageReactionRequest{
		ChatID:    chatID,
		MessageID: messageID,
		Reaction:  []reactionType{{Type: "emoji", Emoji: emoji}},
	}

	data, err := s.client.doPost(ctx, "setMessageReaction", body)
	if err != nil {
		return fmt.Errorf("telegram: react: %w", err)
	}

	var resp apiResponse[bool]
	if err := json.Unmarshal(data, &resp); err != nil {
		return fmt.Errorf("telegram: react: unmarshal: %w", err)
	}

	if !resp.Ok {
		return fmt.Errorf("telegram: react: %s", resp.Description)
	}

	return nil
}
