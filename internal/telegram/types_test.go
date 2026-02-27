package telegram

import (
	"encoding/json"
	"testing"
)

func TestUpdate_JSONRoundTrip(t *testing.T) {
	raw := `{
		"update_id": 123456789,
		"message": {
			"message_id": 1,
			"from": {"id": 987654321, "is_bot": false, "first_name": "Karim"},
			"chat": {"id": 987654321, "type": "private"},
			"date": 1709827200,
			"text": "Hello agent"
		}
	}`

	var u Update
	if err := json.Unmarshal([]byte(raw), &u); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if u.UpdateID != 123456789 {
		t.Errorf("UpdateID = %d, want 123456789", u.UpdateID)
	}
	if u.Message == nil {
		t.Fatal("Message is nil")
	}
	if u.Message.MessageID != 1 {
		t.Errorf("MessageID = %d, want 1", u.Message.MessageID)
	}
	if u.Message.From == nil {
		t.Fatal("From is nil")
	}
	if u.Message.From.ID != 987654321 {
		t.Errorf("From.ID = %d, want 987654321", u.Message.From.ID)
	}
	if u.Message.From.IsBot {
		t.Error("From.IsBot = true, want false")
	}
	if u.Message.From.FirstName != "Karim" {
		t.Errorf("From.FirstName = %q, want %q", u.Message.From.FirstName, "Karim")
	}
	if u.Message.Chat.ID != 987654321 {
		t.Errorf("Chat.ID = %d, want 987654321", u.Message.Chat.ID)
	}
	if u.Message.Chat.Type != "private" {
		t.Errorf("Chat.Type = %q, want %q", u.Message.Chat.Type, "private")
	}
	if u.Message.Date != 1709827200 {
		t.Errorf("Date = %d, want 1709827200", u.Message.Date)
	}
	if u.Message.Text != "Hello agent" {
		t.Errorf("Text = %q, want %q", u.Message.Text, "Hello agent")
	}
}

func TestUpdate_WithVoice(t *testing.T) {
	raw := `{
		"update_id": 100,
		"message": {
			"message_id": 2,
			"from": {"id": 111, "is_bot": false, "first_name": "Test"},
			"chat": {"id": 111, "type": "private"},
			"date": 1700000000,
			"voice": {"file_id": "abc123", "duration": 5}
		}
	}`

	var u Update
	if err := json.Unmarshal([]byte(raw), &u); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if u.Message.Voice == nil {
		t.Fatal("Voice is nil")
	}
	if u.Message.Voice.FileID != "abc123" {
		t.Errorf("Voice.FileID = %q, want %q", u.Message.Voice.FileID, "abc123")
	}
	if u.Message.Voice.Duration != 5 {
		t.Errorf("Voice.Duration = %d, want 5", u.Message.Voice.Duration)
	}
	if u.Message.Text != "" {
		t.Errorf("Text = %q, want empty", u.Message.Text)
	}
}

func TestUpdate_WithoutMessage(t *testing.T) {
	raw := `{"update_id": 200}`

	var u Update
	if err := json.Unmarshal([]byte(raw), &u); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if u.UpdateID != 200 {
		t.Errorf("UpdateID = %d, want 200", u.UpdateID)
	}
	if u.Message != nil {
		t.Error("Message should be nil")
	}
}

func TestAPIResponse_JSON(t *testing.T) {
	raw := `{"ok": true, "result": [{"update_id": 1}]}`

	var resp apiResponse[[]Update]
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !resp.Ok {
		t.Error("Ok = false, want true")
	}
	if len(resp.Result) != 1 {
		t.Fatalf("Result len = %d, want 1", len(resp.Result))
	}
	if resp.Result[0].UpdateID != 1 {
		t.Errorf("Result[0].UpdateID = %d, want 1", resp.Result[0].UpdateID)
	}
}

func TestAPIResponse_Error(t *testing.T) {
	raw := `{"ok": false, "description": "Unauthorized"}`

	var resp apiResponse[[]Update]
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if resp.Ok {
		t.Error("Ok = true, want false")
	}
	if resp.Description != "Unauthorized" {
		t.Errorf("Description = %q, want %q", resp.Description, "Unauthorized")
	}
}

func TestSendMessageRequest_JSON(t *testing.T) {
	req := sendMessageRequest{
		ChatID:    12345,
		Text:      "hello",
		ParseMode: "Markdown",
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded["chat_id"].(float64) != 12345 {
		t.Errorf("chat_id = %v, want 12345", decoded["chat_id"])
	}
	if decoded["text"].(string) != "hello" {
		t.Errorf("text = %v, want hello", decoded["text"])
	}
	if decoded["parse_mode"].(string) != "Markdown" {
		t.Errorf("parse_mode = %v, want Markdown", decoded["parse_mode"])
	}
}

func TestSendMessageRequest_OmitParseMode(t *testing.T) {
	req := sendMessageRequest{
		ChatID: 12345,
		Text:   "hello",
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if _, exists := decoded["parse_mode"]; exists {
		t.Error("parse_mode should be omitted when empty")
	}
}

func TestTelegramMessage_CarriesMessage(t *testing.T) {
	msg := Message{
		MessageID: 42,
		Text:      "test",
		Chat:      Chat{ID: 100, Type: "private"},
	}

	tm := TelegramMessage{Message: msg}
	if tm.Message.MessageID != 42 {
		t.Errorf("MessageID = %d, want 42", tm.Message.MessageID)
	}
	if tm.Message.Text != "test" {
		t.Errorf("Text = %q, want %q", tm.Message.Text, "test")
	}
}

func TestAPIResponse_MessageResult(t *testing.T) {
	raw := `{"ok": true, "result": {"message_id": 99, "chat": {"id": 123, "type": "private"}, "date": 1700000000}}`

	var resp apiResponse[Message]
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !resp.Ok {
		t.Error("Ok = false, want true")
	}
	if resp.Result.MessageID != 99 {
		t.Errorf("MessageID = %d, want 99", resp.Result.MessageID)
	}
}
