package llm

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

)

func TestTranscribe_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/audio/transcriptions") {
			t.Errorf("path = %s, want suffix /audio/transcriptions", r.URL.Path)
		}

		json.NewEncoder(w).Encode(TranscriptionResponse{
			Text:  "Bonjour le monde",
			Model: "voxtral-mini-2602",
			Usage: TranscriptionUsage{
				PromptAudioSeconds: 3,
				PromptTokens:       3,
				CompletionTokens:   10,
				TotalTokens:        13,
			},
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)

	text, err := client.Transcribe(context.Background(), []byte("fake-audio-data"), "voice.ogg")
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if text != "Bonjour le monde" {
		t.Errorf("text = %q, want 'Bonjour le monde'", text)
	}
}

func TestTranscribe_ServerError(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer srv.Close()

	client := newTestClient(t, srv)

	// Override retry to not wait.
	origRetry := retryFn
	retryFn = func(_ context.Context, maxAttempts int, _ time.Duration, fn func() error) error {
		var lastErr error
		for range maxAttempts {
			lastErr = fn()
			if lastErr == nil {
				return nil
			}
		}
		return lastErr
	}
	defer func() { retryFn = origRetry }()

	_, err := client.Transcribe(context.Background(), []byte("audio"), "voice.ogg")
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	if !strings.Contains(err.Error(), "status 500") {
		t.Errorf("error = %q, want to contain 'status 500'", err.Error())
	}
	// platform.Retry retries 3 times on retryable errors.
	if callCount != 3 {
		t.Errorf("callCount = %d, want 3 (retries on 500)", callCount)
	}
}

func TestTranscribe_RateLimit(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte("rate limited"))
			return
		}
		json.NewEncoder(w).Encode(TranscriptionResponse{
			Text:  "transcribed text",
			Model: "voxtral-mini-2602",
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)

	// Override retry to not wait (no-delay retry loop).
	origRetry := retryFn
	retryFn = func(_ context.Context, maxAttempts int, _ time.Duration, fn func() error) error {
		var lastErr error
		for range maxAttempts {
			lastErr = fn()
			if lastErr == nil {
				return nil
			}
		}
		return lastErr
	}
	defer func() { retryFn = origRetry }()

	text, err := client.Transcribe(context.Background(), []byte("audio"), "voice.ogg")
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if text != "transcribed text" {
		t.Errorf("text = %q, want 'transcribed text'", text)
	}
	if callCount != 2 {
		t.Errorf("callCount = %d, want 2", callCount)
	}
}

func TestTranscribe_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{not valid json}`))
	}))
	defer srv.Close()

	client := newTestClient(t, srv)

	_, err := client.Transcribe(context.Background(), []byte("audio"), "voice.ogg")
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
	if !strings.Contains(err.Error(), "parse response") {
		t.Errorf("error = %q, want to contain 'parse response'", err.Error())
	}
}

func TestTranscribe_EmptyAudioData(t *testing.T) {
	_, err := NewClient("key", "model").Transcribe(context.Background(), nil, "voice.ogg")
	if err == nil {
		t.Fatal("expected error for empty audio data")
	}
	if !strings.Contains(err.Error(), "empty audio data") {
		t.Errorf("error = %q, want to contain 'empty audio data'", err.Error())
	}
}

func TestTranscribe_EmptySliceAudioData(t *testing.T) {
	_, err := NewClient("key", "model").Transcribe(context.Background(), []byte{}, "voice.ogg")
	if err == nil {
		t.Fatal("expected error for empty audio data")
	}
	if !strings.Contains(err.Error(), "empty audio data") {
		t.Errorf("error = %q, want to contain 'empty audio data'", err.Error())
	}
}

func TestTranscribe_CorrectMultipart(t *testing.T) {
	var gotModel string
	var gotFilename string
	var gotFileContent []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ct := r.Header.Get("Content-Type")
		mediaType, params, err := mime.ParseMediaType(ct)
		if err != nil {
			t.Fatalf("parse content-type: %v", err)
		}
		if mediaType != "multipart/form-data" {
			t.Errorf("media type = %q, want multipart/form-data", mediaType)
		}

		reader := multipart.NewReader(r.Body, params["boundary"])
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("read part: %v", err)
			}

			switch part.FormName() {
			case "model":
				data, _ := io.ReadAll(part)
				gotModel = string(data)
			case "file":
				gotFilename = part.FileName()
				gotFileContent, _ = io.ReadAll(part)
			}
		}

		json.NewEncoder(w).Encode(TranscriptionResponse{Text: "ok"})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)

	audioData := []byte("test-audio-bytes")
	_, err := client.Transcribe(context.Background(), audioData, "voice.ogg")
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}

	if gotModel != "test-model" {
		t.Errorf("model = %q, want test-model", gotModel)
	}
	if gotFilename != "voice.ogg" {
		t.Errorf("filename = %q, want voice.ogg", gotFilename)
	}
	if string(gotFileContent) != "test-audio-bytes" {
		t.Errorf("file content = %q, want test-audio-bytes", string(gotFileContent))
	}
}

func TestTranscribe_CorrectAuthHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		json.NewEncoder(w).Encode(TranscriptionResponse{Text: "ok"})
	}))
	defer srv.Close()

	client := newTestClient(t, srv)

	_, err := client.Transcribe(context.Background(), []byte("audio"), "voice.ogg")
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if gotAuth != "Bearer test-key" {
		t.Errorf("Authorization = %q, want Bearer test-key", gotAuth)
	}
}

func TestTranscribe_ContextCancelled(t *testing.T) {
	origHTTPDo := httpDo
	httpDo = func(c *http.Client, req *http.Request) (*http.Response, error) {
		return c.Do(req)
	}
	defer func() { httpDo = origHTTPDo }()

	client := &Client{
		apiKey:     "test-key",
		baseURL:    "http://localhost/",
		model:      "test-model",
		httpClient: &http.Client{},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.Transcribe(ctx, []byte("audio"), "voice.ogg")
	if err == nil {
		t.Fatal("expected context error")
	}
}

func TestTranscribe_NonRetryableError(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("bad request"))
	}))
	defer srv.Close()

	client := newTestClient(t, srv)

	// Override retry to not wait.
	origRetry := retryFn
	retryFn = func(_ context.Context, maxAttempts int, _ time.Duration, fn func() error) error {
		var lastErr error
		for range maxAttempts {
			lastErr = fn()
			if lastErr == nil {
				return nil
			}
		}
		return lastErr
	}
	defer func() { retryFn = origRetry }()

	_, err := client.Transcribe(context.Background(), []byte("audio"), "voice.ogg")
	if err == nil {
		t.Fatal("expected error for 400 response")
	}
	if !strings.Contains(err.Error(), "status 400") {
		t.Errorf("error = %q, want to contain 'status 400'", err.Error())
	}
	// Non-retryable: should only call once (platform.Retry sees non-retryable error wrapped via fmt.Errorf).
	if callCount != 1 {
		t.Errorf("callCount = %d, want 1 (non-retryable 400 should not retry)", callCount)
	}
}
