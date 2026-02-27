package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewClient(t *testing.T) {
	c := NewClient("test-api-key", "mistral-large-latest")

	if c.apiKey != "test-api-key" {
		t.Errorf("apiKey = %q, want test-api-key", c.apiKey)
	}
	if c.baseURL != "https://api.mistral.ai/v1/" {
		t.Errorf("baseURL = %q, want https://api.mistral.ai/v1/", c.baseURL)
	}
	if c.model != "mistral-large-latest" {
		t.Errorf("model = %q, want mistral-large-latest", c.model)
	}
	if c.httpClient == nil {
		t.Fatal("httpClient is nil")
	}
	if c.httpClient.Timeout.Seconds() != 10 {
		t.Errorf("Timeout = %v, want 10s", c.httpClient.Timeout)
	}
}

func TestClient_doPost_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-key" {
			t.Errorf("Authorization = %q, want Bearer test-key", auth)
		}
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			t.Errorf("path = %s, want suffix /chat/completions", r.URL.Path)
		}

		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("unmarshal request: %v", err)
		}
		if req["model"] != "test-model" {
			t.Errorf("model = %v, want test-model", req["model"])
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"chat-1","choices":[],"usage":{}}`))
	}))
	defer srv.Close()

	origHTTPDo := httpDo
	httpDo = func(c *http.Client, req *http.Request) (*http.Response, error) {
		return c.Do(req)
	}
	defer func() { httpDo = origHTTPDo }()

	client := &Client{
		apiKey:     "test-key",
		baseURL:    srv.URL + "/",
		model:      "test-model",
		httpClient: srv.Client(),
	}

	data, err := client.doPost(context.Background(), "chat/completions", map[string]string{"model": "test-model"})
	if err != nil {
		t.Fatalf("doPost: %v", err)
	}

	var resp ChatResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.ID != "chat-1" {
		t.Errorf("ID = %q, want chat-1", resp.ID)
	}
}

func TestClient_doPost_AuthHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	origHTTPDo := httpDo
	httpDo = func(c *http.Client, req *http.Request) (*http.Response, error) {
		return c.Do(req)
	}
	defer func() { httpDo = origHTTPDo }()

	client := &Client{
		apiKey:     "my-secret-key",
		baseURL:    srv.URL + "/",
		model:      "test",
		httpClient: srv.Client(),
	}

	_, err := client.doPost(context.Background(), "test", map[string]string{})
	if err != nil {
		t.Fatalf("doPost: %v", err)
	}
	if gotAuth != "Bearer my-secret-key" {
		t.Errorf("Authorization = %q, want Bearer my-secret-key", gotAuth)
	}
}

func TestClient_doPost_HTTPErrors(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		wantMsg    string
	}{
		{"bad request 400", http.StatusBadRequest, "bad request", "status 400"},
		{"rate limit 429", http.StatusTooManyRequests, "rate limited", "status 429"},
		{"server error 500", http.StatusInternalServerError, "internal error", "status 500"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.body))
			}))
			defer srv.Close()

			origHTTPDo := httpDo
			httpDo = func(c *http.Client, req *http.Request) (*http.Response, error) {
				return c.Do(req)
			}
			defer func() { httpDo = origHTTPDo }()

			client := &Client{
				apiKey:     "test-key",
				baseURL:    srv.URL + "/",
				model:      "test",
				httpClient: srv.Client(),
			}

			_, err := client.doPost(context.Background(), "chat/completions", map[string]string{})
			if err == nil {
				t.Fatalf("expected error for status %d", tt.statusCode)
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("error = %q, want to contain %q", err.Error(), tt.wantMsg)
			}
		})
	}
}

func TestClient_doPost_NetworkError(t *testing.T) {
	origHTTPDo := httpDo
	httpDo = func(c *http.Client, req *http.Request) (*http.Response, error) {
		return nil, errors.New("connection refused")
	}
	defer func() { httpDo = origHTTPDo }()

	client := &Client{
		apiKey:     "test-key",
		baseURL:    "http://127.0.0.1:1/",
		model:      "test",
		httpClient: &http.Client{},
	}

	_, err := client.doPost(context.Background(), "chat/completions", map[string]string{})
	if err == nil {
		t.Fatal("expected network error")
	}
	if !strings.Contains(err.Error(), "llm: chat/completions:") {
		t.Errorf("error = %q, want to contain 'llm: chat/completions:'", err.Error())
	}
}

func TestClient_doPost_MarshalError(t *testing.T) {
	client := &Client{
		apiKey:     "test-key",
		baseURL:    "http://localhost/",
		model:      "test",
		httpClient: &http.Client{},
	}

	_, err := client.doPost(context.Background(), "test", make(chan int))
	if err == nil {
		t.Fatal("expected marshal error")
	}
	if !strings.Contains(err.Error(), "marshal") {
		t.Errorf("error = %q, want to contain 'marshal'", err.Error())
	}
}

func TestClient_doPost_InvalidURL(t *testing.T) {
	client := &Client{
		apiKey:     "test-key",
		baseURL:    "http://invalid\x00url/",
		model:      "test",
		httpClient: &http.Client{},
	}

	origHTTPDo := httpDo
	httpDo = func(c *http.Client, req *http.Request) (*http.Response, error) {
		return c.Do(req)
	}
	defer func() { httpDo = origHTTPDo }()

	_, err := client.doPost(context.Background(), "test", map[string]string{})
	if err == nil {
		t.Fatal("expected error for invalid URL")
	}
	if !strings.Contains(err.Error(), "request") {
		t.Errorf("error = %q, want to contain 'request'", err.Error())
	}
}

func TestClient_doPost_CancelledContext(t *testing.T) {
	origHTTPDo := httpDo
	httpDo = func(c *http.Client, req *http.Request) (*http.Response, error) {
		return c.Do(req)
	}
	defer func() { httpDo = origHTTPDo }()

	client := &Client{
		apiKey:     "test-key",
		baseURL:    "http://localhost/",
		model:      "test",
		httpClient: &http.Client{},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.doPost(ctx, "test", map[string]string{})
	if err == nil {
		t.Fatal("expected context error")
	}
}

// errReader is a reader that always returns an error.
type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("read error") }
func (errReader) Close() error             { return nil }

func TestClient_doPost_ReadBodyError(t *testing.T) {
	origHTTPDo := httpDo
	httpDo = func(_ *http.Client, _ *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       errReader{},
		}, nil
	}
	defer func() { httpDo = origHTTPDo }()

	client := &Client{
		apiKey:     "test-key",
		baseURL:    "http://localhost/",
		model:      "test",
		httpClient: &http.Client{},
	}

	_, err := client.doPost(context.Background(), "test", map[string]string{})
	if err == nil {
		t.Fatal("expected read body error")
	}
	if !strings.Contains(err.Error(), "read body") {
		t.Errorf("error = %q, want to contain 'read body'", err.Error())
	}
}

func TestHTTPError_IsRetryable(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		retryable  bool
	}{
		{"400 not retryable", 400, false},
		{"401 not retryable", 401, false},
		{"403 not retryable", 403, false},
		{"404 not retryable", 404, false},
		{"429 retryable", 429, true},
		{"500 retryable", 500, true},
		{"502 retryable", 502, true},
		{"503 retryable", 503, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			he := &httpError{StatusCode: tt.statusCode, Endpoint: "test", Body: "error"}
			if he.IsRetryable() != tt.retryable {
				t.Errorf("IsRetryable() = %v, want %v", he.IsRetryable(), tt.retryable)
			}
		})
	}
}

func TestHTTPError_Error(t *testing.T) {
	he := &httpError{StatusCode: 500, Endpoint: "chat/completions", Body: "server error"}
	want := "llm: chat/completions: status 500: server error"
	if he.Error() != want {
		t.Errorf("Error() = %q, want %q", he.Error(), want)
	}
}

func TestHTTPDo_Default(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	origHTTPDo := httpDo
	defer func() { httpDo = origHTTPDo }()

	httpDo = func(client *http.Client, req *http.Request) (*http.Response, error) {
		return client.Do(req)
	}

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	resp, err := httpDo(srv.Client(), req)
	if err != nil {
		t.Fatalf("httpDo: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}
