package telegram

// Update represents a Telegram Bot API Update object.
type Update struct {
	UpdateID int64    `json:"update_id"`
	Message  *Message `json:"message,omitempty"`
}

// Message represents a Telegram message.
type Message struct {
	MessageID int64  `json:"message_id"`
	From      *User  `json:"from,omitempty"`
	Chat      Chat   `json:"chat"`
	Date      int64  `json:"date"`
	Text      string `json:"text,omitempty"`
	Voice     *Voice `json:"voice,omitempty"`
}

// User represents a Telegram user.
type User struct {
	ID        int64  `json:"id"`
	IsBot     bool   `json:"is_bot"`
	FirstName string `json:"first_name"`
}

// Chat represents a Telegram chat.
type Chat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}

// Voice represents a Telegram voice message.
type Voice struct {
	FileID   string `json:"file_id"`
	Duration int    `json:"duration"`
}

// apiResponse is a generic wrapper for Telegram Bot API responses.
type apiResponse[T any] struct {
	Ok          bool   `json:"ok"`
	Result      T      `json:"result"`
	Description string `json:"description,omitempty"`
}

// sendMessageRequest is the JSON body for the sendMessage API call.
type sendMessageRequest struct {
	ChatID    int64  `json:"chat_id"`
	Text      string `json:"text"`
	ParseMode string `json:"parse_mode,omitempty"`
}

// TelegramMessage carries a validated message to the event loop.
type TelegramMessage struct {
	Message Message
}
