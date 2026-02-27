package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"time"
)

// TranscriptionResponse is the JSON response from the Voxtral transcription API.
type TranscriptionResponse struct {
	Text  string             `json:"text"`
	Model string             `json:"model"`
	Usage TranscriptionUsage `json:"usage"`
}

// TranscriptionUsage tracks token and audio usage for a transcription request.
type TranscriptionUsage struct {
	PromptAudioSeconds int `json:"prompt_audio_seconds"`
	PromptTokens       int `json:"prompt_tokens"`
	CompletionTokens   int `json:"completion_tokens"`
	TotalTokens        int `json:"total_tokens"`
}

// Transcribe sends audio data to the Voxtral transcription API and returns the transcribed text.
// It uses multipart/form-data encoding and retries on retryable errors (429, 5xx).
func (c *Client) Transcribe(ctx context.Context, audioData []byte, filename string) (string, error) {
	slog.Debug("transcription request", "component", "llm", "operation", "transcribe", "model", c.model, "audio_size", len(audioData))

	if len(audioData) == 0 {
		return "", fmt.Errorf("llm: transcribe: empty audio data")
	}

	var result string
	var nonRetryErr error
	err := retryFn(ctx, 3, time.Second, func() error {
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)

		if err := w.WriteField("model", c.model); err != nil {
			return fmt.Errorf("llm: transcribe: write model field: %w", err)
		}

		fw, err := w.CreateFormFile("file", filename)
		if err != nil {
			return fmt.Errorf("llm: transcribe: create form file: %w", err)
		}
		if _, err := fw.Write(audioData); err != nil {
			return fmt.Errorf("llm: transcribe: write audio data: %w", err)
		}
		w.Close()

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"audio/transcriptions", &buf)
		if err != nil {
			return fmt.Errorf("llm: transcribe: create request: %w", err)
		}
		req.Header.Set("Content-Type", w.FormDataContentType())
		req.Header.Set("Authorization", "Bearer "+c.apiKey)

		resp, err := httpDo(c.httpClient, req)
		if err != nil {
			return fmt.Errorf("llm: transcribe: %w", err)
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("llm: transcribe: read body: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			httpErr := &httpError{
				StatusCode: resp.StatusCode,
				Endpoint:   "audio/transcriptions",
				Body:       string(body),
			}
			if httpErr.IsRetryable() {
				return httpErr
			}
			// Non-retryable: stop retry loop by returning nil, store error externally.
			nonRetryErr = httpErr
			return nil
		}

		var transcription TranscriptionResponse
		if err := json.Unmarshal(body, &transcription); err != nil {
			return fmt.Errorf("llm: transcribe: parse response: %w", err)
		}

		result = transcription.Text

		slog.Info("transcription completed",
			"component", "llm",
			"operation", "transcribe",
			"text_length", len(result),
			"prompt_tokens", transcription.Usage.PromptTokens,
			"total_tokens", transcription.Usage.TotalTokens,
		)
		return nil
	})

	if nonRetryErr != nil {
		return "", nonRetryErr
	}
	return result, err
}
