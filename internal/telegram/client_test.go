package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestNewClient(t *testing.T) {
	c := NewClient("123:ABC")

	if c.token != "123:ABC" {
		t.Errorf("token = %q, want %q", c.token, "123:ABC")
	}
	if c.baseURL != "https://api.telegram.org/bot123:ABC/" {
		t.Errorf("baseURL = %q, want %q", c.baseURL, "https://api.telegram.org/bot123:ABC/")
	}
	if c.httpClient == nil {
		t.Fatal("httpClient is nil")
	}
}

func TestClient_doPost_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/sendMessage") {
			t.Errorf("path = %s, want suffix /sendMessage", r.URL.Path)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}

		body, _ := io.ReadAll(r.Body)
		var req sendMessageRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("unmarshal request body: %v", err)
		}
		if req.ChatID != 42 {
			t.Errorf("ChatID = %d, want 42", req.ChatID)
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(apiResponse[Message]{
			Ok:     true,
			Result: Message{MessageID: 1},
		})
	}))
	defer srv.Close()

	client := &Client{
		baseURL:    srv.URL + "/",
		httpClient: srv.Client(),
	}

	origHTTPDo := httpDo
	httpDo = func(c *http.Client, req *http.Request) (*http.Response, error) {
		return c.Do(req)
	}
	defer func() { httpDo = origHTTPDo }()

	data, err := client.doPost(context.Background(), "sendMessage", sendMessageRequest{ChatID: 42, Text: "hi"})
	if err != nil {
		t.Fatalf("doPost: %v", err)
	}

	var resp apiResponse[Message]
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !resp.Ok {
		t.Error("Ok = false, want true")
	}
}

func TestClient_doPost_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer srv.Close()

	client := &Client{
		baseURL:    srv.URL + "/",
		httpClient: srv.Client(),
	}

	origHTTPDo := httpDo
	httpDo = func(c *http.Client, req *http.Request) (*http.Response, error) {
		return c.Do(req)
	}
	defer func() { httpDo = origHTTPDo }()

	_, err := client.doPost(context.Background(), "sendMessage", sendMessageRequest{})
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	if !strings.Contains(err.Error(), "unexpected status 500") {
		t.Errorf("error = %q, want to contain 'unexpected status 500'", err.Error())
	}
}

func TestClient_doPost_NetworkError(t *testing.T) {
	client := &Client{
		baseURL:    "http://127.0.0.1:1/",
		httpClient: &http.Client{},
	}

	origHTTPDo := httpDo
	httpDo = func(c *http.Client, req *http.Request) (*http.Response, error) {
		return c.Do(req)
	}
	defer func() { httpDo = origHTTPDo }()

	_, err := client.doPost(context.Background(), "sendMessage", sendMessageRequest{})
	if err == nil {
		t.Fatal("expected network error")
	}
	if !strings.Contains(err.Error(), "sendMessage:") {
		t.Errorf("error = %q, want to contain 'sendMessage:'", err.Error())
	}
}

func TestClient_doPost_InvalidBody(t *testing.T) {
	client := &Client{
		baseURL:    "http://localhost/",
		httpClient: &http.Client{},
	}

	origHTTPDo := httpDo
	httpDo = func(c *http.Client, req *http.Request) (*http.Response, error) {
		return c.Do(req)
	}
	defer func() { httpDo = origHTTPDo }()

	// json.Marshal cannot marshal a channel
	_, err := client.doPost(context.Background(), "test", make(chan int))
	if err == nil {
		t.Fatal("expected marshal error")
	}
	if !strings.Contains(err.Error(), "marshal") {
		t.Errorf("error = %q, want to contain 'marshal'", err.Error())
	}
}

func TestClient_doPost_CancelledContext(t *testing.T) {
	client := &Client{
		baseURL:    "http://localhost/",
		httpClient: &http.Client{},
	}

	origHTTPDo := httpDo
	httpDo = func(c *http.Client, req *http.Request) (*http.Response, error) {
		return c.Do(req)
	}
	defer func() { httpDo = origHTTPDo }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.doPost(ctx, "test", sendMessageRequest{})
	if err == nil {
		t.Fatal("expected context error")
	}
}

func TestClient_doGet_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/getUpdates") {
			t.Errorf("path = %s, want suffix /getUpdates", r.URL.Path)
		}
		if r.URL.Query().Get("timeout") != "30" {
			t.Errorf("timeout = %q, want 30", r.URL.Query().Get("timeout"))
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(apiResponse[[]Update]{
			Ok:     true,
			Result: []Update{{UpdateID: 1}},
		})
	}))
	defer srv.Close()

	client := &Client{
		baseURL:    srv.URL + "/",
		httpClient: srv.Client(),
	}

	origHTTPDo := httpDo
	httpDo = func(c *http.Client, req *http.Request) (*http.Response, error) {
		return c.Do(req)
	}
	defer func() { httpDo = origHTTPDo }()

	params := url.Values{}
	params.Set("timeout", "30")

	data, err := client.doGet(context.Background(), "getUpdates", params)
	if err != nil {
		t.Fatalf("doGet: %v", err)
	}

	var resp apiResponse[[]Update]
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !resp.Ok {
		t.Error("Ok = false, want true")
	}
	if len(resp.Result) != 1 {
		t.Fatalf("Result len = %d, want 1", len(resp.Result))
	}
}

func TestClient_doGet_NoParams(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "" {
			t.Errorf("query = %q, want empty", r.URL.RawQuery)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true,"result":[]}`))
	}))
	defer srv.Close()

	client := &Client{
		baseURL:    srv.URL + "/",
		httpClient: srv.Client(),
	}

	origHTTPDo := httpDo
	httpDo = func(c *http.Client, req *http.Request) (*http.Response, error) {
		return c.Do(req)
	}
	defer func() { httpDo = origHTTPDo }()

	_, err := client.doGet(context.Background(), "getMe", nil)
	if err != nil {
		t.Fatalf("doGet: %v", err)
	}
}

func TestClient_doGet_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("bad request"))
	}))
	defer srv.Close()

	client := &Client{
		baseURL:    srv.URL + "/",
		httpClient: srv.Client(),
	}

	origHTTPDo := httpDo
	httpDo = func(c *http.Client, req *http.Request) (*http.Response, error) {
		return c.Do(req)
	}
	defer func() { httpDo = origHTTPDo }()

	_, err := client.doGet(context.Background(), "getUpdates", nil)
	if err == nil {
		t.Fatal("expected error for 400 response")
	}
	if !strings.Contains(err.Error(), "unexpected status 400") {
		t.Errorf("error = %q, want to contain 'unexpected status 400'", err.Error())
	}
}

func TestClient_doGet_NetworkError(t *testing.T) {
	client := &Client{
		baseURL:    "http://127.0.0.1:1/",
		httpClient: &http.Client{},
	}

	origHTTPDo := httpDo
	httpDo = func(c *http.Client, req *http.Request) (*http.Response, error) {
		return c.Do(req)
	}
	defer func() { httpDo = origHTTPDo }()

	_, err := client.doGet(context.Background(), "getUpdates", nil)
	if err == nil {
		t.Fatal("expected network error")
	}
	if !strings.Contains(err.Error(), "getUpdates:") {
		t.Errorf("error = %q, want to contain 'getUpdates:'", err.Error())
	}
}

func TestClient_doGet_CancelledContext(t *testing.T) {
	client := &Client{
		baseURL:    "http://localhost/",
		httpClient: &http.Client{},
	}

	origHTTPDo := httpDo
	httpDo = func(c *http.Client, req *http.Request) (*http.Response, error) {
		return c.Do(req)
	}
	defer func() { httpDo = origHTTPDo }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.doGet(ctx, "test", nil)
	if err == nil {
		t.Fatal("expected context error")
	}
}

func TestHTTPDo_Default(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	// Save and restore the original httpDo but use it directly (don't override)
	origHTTPDo := httpDo
	defer func() { httpDo = origHTTPDo }()

	// Reset to the default implementation
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

func TestClient_doPost_InvalidURL(t *testing.T) {
	client := &Client{
		baseURL:    "http://invalid\x00url/",
		httpClient: &http.Client{},
	}

	origHTTPDo := httpDo
	httpDo = func(c *http.Client, req *http.Request) (*http.Response, error) {
		return c.Do(req)
	}
	defer func() { httpDo = origHTTPDo }()

	_, err := client.doPost(context.Background(), "test", sendMessageRequest{})
	if err == nil {
		t.Fatal("expected error for invalid URL")
	}
	if !strings.Contains(err.Error(), "new request") {
		t.Errorf("error = %q, want to contain 'new request'", err.Error())
	}
}

func TestClient_doGet_InvalidURL(t *testing.T) {
	client := &Client{
		baseURL:    "http://invalid\x00url/",
		httpClient: &http.Client{},
	}

	origHTTPDo := httpDo
	httpDo = func(c *http.Client, req *http.Request) (*http.Response, error) {
		return c.Do(req)
	}
	defer func() { httpDo = origHTTPDo }()

	_, err := client.doGet(context.Background(), "test", nil)
	if err == nil {
		t.Fatal("expected error for invalid URL")
	}
	if !strings.Contains(err.Error(), "new request") {
		t.Errorf("error = %q, want to contain 'new request'", err.Error())
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
		baseURL:    "http://localhost/",
		httpClient: &http.Client{},
	}

	_, err := client.doPost(context.Background(), "test", sendMessageRequest{})
	if err == nil {
		t.Fatal("expected read body error")
	}
	if !strings.Contains(err.Error(), "read body") {
		t.Errorf("error = %q, want to contain 'read body'", err.Error())
	}
}

func TestClient_doGet_ReadBodyError(t *testing.T) {
	origHTTPDo := httpDo
	httpDo = func(_ *http.Client, _ *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       errReader{},
		}, nil
	}
	defer func() { httpDo = origHTTPDo }()

	client := &Client{
		baseURL:    "http://localhost/",
		httpClient: &http.Client{},
	}

	_, err := client.doGet(context.Background(), "test", nil)
	if err == nil {
		t.Fatal("expected read body error")
	}
	if !strings.Contains(err.Error(), "read body") {
		t.Errorf("error = %q, want to contain 'read body'", err.Error())
	}
}
