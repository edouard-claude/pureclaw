package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetFile_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/getFile") {
			t.Errorf("path = %s, want suffix /getFile", r.URL.Path)
		}
		if r.URL.Query().Get("file_id") != "AwACAgI123" {
			t.Errorf("file_id = %q, want AwACAgI123", r.URL.Query().Get("file_id"))
		}

		json.NewEncoder(w).Encode(apiResponse[File]{
			Ok: true,
			Result: File{
				FileID:   "AwACAgI123",
				FilePath: "voice/file_123.oga",
				FileSize: 1024,
			},
		})
	}))
	defer srv.Close()

	client := &Client{
		token:      "test-token",
		baseURL:    srv.URL + "/",
		httpClient: srv.Client(),
	}

	origHTTPDo := httpDo
	httpDo = func(c *http.Client, req *http.Request) (*http.Response, error) {
		return c.Do(req)
	}
	defer func() { httpDo = origHTTPDo }()

	filePath, err := client.GetFile(context.Background(), "AwACAgI123")
	if err != nil {
		t.Fatalf("GetFile: %v", err)
	}
	if filePath != "voice/file_123.oga" {
		t.Errorf("filePath = %q, want voice/file_123.oga", filePath)
	}
}

func TestGetFile_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(apiResponse[File]{
			Ok:          false,
			Description: "file not found",
		})
	}))
	defer srv.Close()

	client := &Client{
		token:      "test-token",
		baseURL:    srv.URL + "/",
		httpClient: srv.Client(),
	}

	origHTTPDo := httpDo
	httpDo = func(c *http.Client, req *http.Request) (*http.Response, error) {
		return c.Do(req)
	}
	defer func() { httpDo = origHTTPDo }()

	_, err := client.GetFile(context.Background(), "bad-id")
	if err == nil {
		t.Fatal("expected error for API error response")
	}
	if !strings.Contains(err.Error(), "file not found") {
		t.Errorf("error = %q, want to contain 'file not found'", err.Error())
	}
}

func TestGetFile_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer srv.Close()

	client := &Client{
		token:      "test-token",
		baseURL:    srv.URL + "/",
		httpClient: srv.Client(),
	}

	origHTTPDo := httpDo
	httpDo = func(c *http.Client, req *http.Request) (*http.Response, error) {
		return c.Do(req)
	}
	defer func() { httpDo = origHTTPDo }()

	_, err := client.GetFile(context.Background(), "test-id")
	if err == nil {
		t.Fatal("expected error for HTTP 500")
	}
	if !strings.Contains(err.Error(), "get file") {
		t.Errorf("error = %q, want to contain 'get file'", err.Error())
	}
}

func TestGetFile_ContextCancelled(t *testing.T) {
	client := &Client{
		token:      "test-token",
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

	_, err := client.GetFile(ctx, "test-id")
	if err == nil {
		t.Fatal("expected context error")
	}
}

func TestDownloadFile_Success(t *testing.T) {
	expectedData := []byte{0x4F, 0x67, 0x67, 0x53, 0x00} // OGG header bytes
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/voice/file_123.oga") {
			t.Errorf("path = %s, want suffix /voice/file_123.oga", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		w.Write(expectedData)
	}))
	defer srv.Close()

	client := &Client{
		token:      "test-token",
		baseURL:    srv.URL + "/",
		httpClient: srv.Client(),
	}

	origHTTPDo := httpDo
	httpDo = func(c *http.Client, req *http.Request) (*http.Response, error) {
		return c.Do(req)
	}
	defer func() { httpDo = origHTTPDo }()

	data, err := client.DownloadFile(context.Background(), "voice/file_123.oga")
	if err != nil {
		t.Fatalf("DownloadFile: %v", err)
	}
	if !bytes.Equal(data, expectedData) {
		t.Errorf("data = %v, want %v", data, expectedData)
	}
}

func TestDownloadFile_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("not found"))
	}))
	defer srv.Close()

	client := &Client{
		token:      "test-token",
		baseURL:    srv.URL + "/",
		httpClient: srv.Client(),
	}

	origHTTPDo := httpDo
	httpDo = func(c *http.Client, req *http.Request) (*http.Response, error) {
		return c.Do(req)
	}
	defer func() { httpDo = origHTTPDo }()

	_, err := client.DownloadFile(context.Background(), "voice/missing.oga")
	if err == nil {
		t.Fatal("expected error for HTTP 404")
	}
	if !strings.Contains(err.Error(), "unexpected status 404") {
		t.Errorf("error = %q, want to contain 'unexpected status 404'", err.Error())
	}
}

func TestDownloadFile_NetworkError(t *testing.T) {
	origHTTPDo := httpDo
	httpDo = func(c *http.Client, req *http.Request) (*http.Response, error) {
		return c.Do(req)
	}
	defer func() { httpDo = origHTTPDo }()

	client := &Client{
		token:      "test-token",
		baseURL:    "http://127.0.0.1:1/",
		httpClient: &http.Client{},
	}

	_, err := client.DownloadFile(context.Background(), "voice/file.oga")
	if err == nil {
		t.Fatal("expected network error")
	}
	if !strings.Contains(err.Error(), "download file") {
		t.Errorf("error = %q, want to contain 'download file'", err.Error())
	}
}

func TestDownloadFile_ContextCancelled(t *testing.T) {
	client := &Client{
		token:      "test-token",
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

	_, err := client.DownloadFile(ctx, "voice/file.oga")
	if err == nil {
		t.Fatal("expected context error")
	}
}

func TestDownloadFile_ReadBodyError(t *testing.T) {
	origHTTPDo := httpDo
	httpDo = func(_ *http.Client, _ *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       errReader{},
		}, nil
	}
	defer func() { httpDo = origHTTPDo }()

	client := &Client{
		token:      "test-token",
		baseURL:    "http://localhost/",
		httpClient: &http.Client{},
	}

	_, err := client.DownloadFile(context.Background(), "voice/file.oga")
	if err == nil {
		t.Fatal("expected read body error")
	}
	if !strings.Contains(err.Error(), "read body") {
		t.Errorf("error = %q, want to contain 'read body'", err.Error())
	}
}

func TestGetFile_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`not valid json`))
	}))
	defer srv.Close()

	client := &Client{
		token:      "test-token",
		baseURL:    srv.URL + "/",
		httpClient: srv.Client(),
	}

	origHTTPDo := httpDo
	httpDo = func(c *http.Client, req *http.Request) (*http.Response, error) {
		return c.Do(req)
	}
	defer func() { httpDo = origHTTPDo }()

	_, err := client.GetFile(context.Background(), "test-id")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "parse response") {
		t.Errorf("error = %q, want to contain 'parse response'", err.Error())
	}
}
