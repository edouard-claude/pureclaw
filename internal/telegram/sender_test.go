package telegram

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewSender(t *testing.T) {
	client := NewClient("test-token")
	s := NewSender(client)

	if s.client != client {
		t.Error("client mismatch")
	}
}

func TestSender_Send_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/sendMessage") {
			t.Errorf("path = %s, want suffix /sendMessage", r.URL.Path)
		}

		body, _ := io.ReadAll(r.Body)
		var req sendMessageRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("unmarshal request: %v", err)
		}
		if req.ChatID != 12345 {
			t.Errorf("ChatID = %d, want 12345", req.ChatID)
		}
		if req.Text != "Hello!" {
			t.Errorf("Text = %q, want %q", req.Text, "Hello!")
		}

		json.NewEncoder(w).Encode(apiResponse[Message]{
			Ok: true,
			Result: Message{
				MessageID: 42,
				Chat:      Chat{ID: 12345, Type: "private"},
			},
		})
	}))
	defer srv.Close()

	origHTTPDo := httpDo
	httpDo = func(c *http.Client, req *http.Request) (*http.Response, error) {
		return c.Do(req)
	}
	defer func() { httpDo = origHTTPDo }()

	client := &Client{
		baseURL:    srv.URL + "/",
		httpClient: srv.Client(),
	}
	s := NewSender(client)

	err := s.Send(context.Background(), 12345, "Hello!")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
}

func TestSender_Send_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(apiResponse[Message]{
			Ok:          false,
			Description: "Bad Request: chat not found",
		})
	}))
	defer srv.Close()

	origHTTPDo := httpDo
	httpDo = func(c *http.Client, req *http.Request) (*http.Response, error) {
		return c.Do(req)
	}
	defer func() { httpDo = origHTTPDo }()

	client := &Client{
		baseURL:    srv.URL + "/",
		httpClient: srv.Client(),
	}
	s := NewSender(client)

	err := s.Send(context.Background(), 99999, "test")
	if err == nil {
		t.Fatal("expected error for API error response")
	}
	if !strings.Contains(err.Error(), "chat not found") {
		t.Errorf("error = %q, want to contain 'chat not found'", err.Error())
	}
}

func TestSender_Send_NetworkError(t *testing.T) {
	client := &Client{
		baseURL:    "http://127.0.0.1:1/",
		httpClient: &http.Client{},
	}

	origHTTPDo := httpDo
	httpDo = func(c *http.Client, req *http.Request) (*http.Response, error) {
		return c.Do(req)
	}
	defer func() { httpDo = origHTTPDo }()

	s := NewSender(client)

	err := s.Send(context.Background(), 12345, "test")
	if err == nil {
		t.Fatal("expected network error")
	}
	if !strings.Contains(err.Error(), "telegram: send:") {
		t.Errorf("error = %q, want to contain 'telegram: send:'", err.Error())
	}
}

func TestSender_Send_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer srv.Close()

	origHTTPDo := httpDo
	httpDo = func(c *http.Client, req *http.Request) (*http.Response, error) {
		return c.Do(req)
	}
	defer func() { httpDo = origHTTPDo }()

	client := &Client{
		baseURL:    srv.URL + "/",
		httpClient: srv.Client(),
	}
	s := NewSender(client)

	err := s.Send(context.Background(), 12345, "test")
	if err == nil {
		t.Fatal("expected unmarshal error")
	}
	if !strings.Contains(err.Error(), "unmarshal") {
		t.Errorf("error = %q, want to contain 'unmarshal'", err.Error())
	}
}

func TestSender_Send_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer srv.Close()

	origHTTPDo := httpDo
	httpDo = func(c *http.Client, req *http.Request) (*http.Response, error) {
		return c.Do(req)
	}
	defer func() { httpDo = origHTTPDo }()

	client := &Client{
		baseURL:    srv.URL + "/",
		httpClient: srv.Client(),
	}
	s := NewSender(client)

	err := s.Send(context.Background(), 12345, "test")
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	if !strings.Contains(err.Error(), "telegram: send:") {
		t.Errorf("error = %q, want to contain 'telegram: send:'", err.Error())
	}
}
