package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewPoller(t *testing.T) {
	client := NewClient("test-token")
	p := NewPoller(client, []int64{111, 222, 333}, 30)

	if p.client != client {
		t.Error("client mismatch")
	}
	if p.timeout != 30 {
		t.Errorf("timeout = %d, want 30", p.timeout)
	}
	if len(p.allowedIDs) != 3 {
		t.Fatalf("allowedIDs len = %d, want 3", len(p.allowedIDs))
	}
	for _, id := range []int64{111, 222, 333} {
		if !p.allowedIDs[id] {
			t.Errorf("allowedIDs[%d] = false, want true", id)
		}
	}
	if p.offset != 0 {
		t.Errorf("offset = %d, want 0", p.offset)
	}
}

func TestPoller_Poll_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/getUpdates") {
			t.Errorf("path = %s, want suffix /getUpdates", r.URL.Path)
		}
		if r.URL.Query().Get("timeout") != "30" {
			t.Errorf("timeout = %q, want 30", r.URL.Query().Get("timeout"))
		}

		json.NewEncoder(w).Encode(apiResponse[[]Update]{
			Ok: true,
			Result: []Update{
				{
					UpdateID: 100,
					Message: &Message{
						MessageID: 1,
						From:      &User{ID: 111, FirstName: "Test"},
						Chat:      Chat{ID: 111, Type: "private"},
						Date:      1700000000,
						Text:      "hello",
					},
				},
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
	p := NewPoller(client, []int64{111}, 30)

	updates, err := p.Poll(context.Background())
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}

	if len(updates) != 1 {
		t.Fatalf("updates len = %d, want 1", len(updates))
	}
	if updates[0].UpdateID != 100 {
		t.Errorf("UpdateID = %d, want 100", updates[0].UpdateID)
	}
	if updates[0].Message.Text != "hello" {
		t.Errorf("Text = %q, want %q", updates[0].Message.Text, "hello")
	}
}

func TestPoller_Poll_EmptyUpdates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(apiResponse[[]Update]{
			Ok:     true,
			Result: []Update{},
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
	p := NewPoller(client, []int64{111}, 30)

	updates, err := p.Poll(context.Background())
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if len(updates) != 0 {
		t.Errorf("updates len = %d, want 0", len(updates))
	}
}

func TestPoller_Poll_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(apiResponse[[]Update]{
			Ok:          false,
			Description: "Unauthorized",
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
	p := NewPoller(client, []int64{111}, 30)

	_, err := p.Poll(context.Background())
	if err == nil {
		t.Fatal("expected error for API error response")
	}
	if !strings.Contains(err.Error(), "Unauthorized") {
		t.Errorf("error = %q, want to contain 'Unauthorized'", err.Error())
	}
}

func TestPoller_Poll_InvalidJSON(t *testing.T) {
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
	p := NewPoller(client, []int64{111}, 30)

	_, err := p.Poll(context.Background())
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "unmarshal") {
		t.Errorf("error = %q, want to contain 'unmarshal'", err.Error())
	}
}

func TestPoller_Poll_OffsetSent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("offset") != "101" {
			t.Errorf("offset = %q, want 101", r.URL.Query().Get("offset"))
		}
		json.NewEncoder(w).Encode(apiResponse[[]Update]{Ok: true, Result: []Update{}})
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
	p := NewPoller(client, []int64{111}, 30)
	p.offset = 101

	_, err := p.Poll(context.Background())
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
}

func TestPoller_Run_WhitelistRejection(t *testing.T) {
	var callCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := callCount.Add(1)
		if count == 1 {
			json.NewEncoder(w).Encode(apiResponse[[]Update]{
				Ok: true,
				Result: []Update{
					{
						UpdateID: 100,
						Message: &Message{
							MessageID: 1,
							From:      &User{ID: 999, FirstName: "Hacker"},
							Chat:      Chat{ID: 999, Type: "private"},
							Text:      "try to hack",
						},
					},
					{
						UpdateID: 101,
						Message: &Message{
							MessageID: 2,
							From:      &User{ID: 111, FirstName: "Owner"},
							Chat:      Chat{ID: 111, Type: "private"},
							Text:      "legit message",
						},
					},
				},
			})
		} else {
			json.NewEncoder(w).Encode(apiResponse[[]Update]{Ok: true, Result: []Update{}})
		}
	}))
	defer srv.Close()

	origHTTPDo := httpDo
	httpDo = func(c *http.Client, req *http.Request) (*http.Response, error) {
		return c.Do(req)
	}
	defer func() { httpDo = origHTTPDo }()

	origRetry := retryFn
	retryFn = func(_ context.Context, _ int, _ time.Duration, fn func() error) error {
		return fn()
	}
	defer func() { retryFn = origRetry }()

	client := &Client{
		baseURL:    srv.URL + "/",
		httpClient: srv.Client(),
	}
	p := NewPoller(client, []int64{111}, 1)

	out := make(chan TelegramMessage, 10)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		p.Run(ctx, out)
		close(done)
	}()

	select {
	case msg := <-out:
		if msg.Message.From.ID != 111 {
			t.Errorf("received message from user %d, want 111", msg.Message.From.ID)
		}
		if msg.Message.Text != "legit message" {
			t.Errorf("text = %q, want %q", msg.Message.Text, "legit message")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for message")
	}

	// Ensure only one message was sent (unauthorized was filtered)
	cancel()
	<-done
	if len(out) != 0 {
		t.Errorf("unexpected extra messages in channel: %d", len(out))
	}
}

func TestPoller_Run_OffsetAdvancement(t *testing.T) {
	var callCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := callCount.Add(1)
		if count == 1 {
			json.NewEncoder(w).Encode(apiResponse[[]Update]{
				Ok: true,
				Result: []Update{
					{
						UpdateID: 200,
						Message: &Message{
							MessageID: 1,
							From:      &User{ID: 111, FirstName: "Test"},
							Chat:      Chat{ID: 111, Type: "private"},
							Text:      "first",
						},
					},
					{
						UpdateID: 201,
						Message: &Message{
							MessageID: 2,
							From:      &User{ID: 111, FirstName: "Test"},
							Chat:      Chat{ID: 111, Type: "private"},
							Text:      "second",
						},
					},
				},
			})
		} else {
			// Verify offset was advanced
			offset := r.URL.Query().Get("offset")
			if offset != "202" {
				t.Errorf("offset = %q, want 202", offset)
			}
			json.NewEncoder(w).Encode(apiResponse[[]Update]{Ok: true, Result: []Update{}})
		}
	}))
	defer srv.Close()

	origHTTPDo := httpDo
	httpDo = func(c *http.Client, req *http.Request) (*http.Response, error) {
		return c.Do(req)
	}
	defer func() { httpDo = origHTTPDo }()

	origRetry := retryFn
	retryFn = func(_ context.Context, _ int, _ time.Duration, fn func() error) error {
		return fn()
	}
	defer func() { retryFn = origRetry }()

	client := &Client{
		baseURL:    srv.URL + "/",
		httpClient: srv.Client(),
	}
	p := NewPoller(client, []int64{111}, 1)

	out := make(chan TelegramMessage, 10)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		p.Run(ctx, out)
		close(done)
	}()

	// Receive both messages
	for i := range 2 {
		select {
		case <-out:
		case <-time.After(3 * time.Second):
			t.Fatalf("timeout waiting for message %d", i+1)
		}
	}

	// Wait for the second poll to happen (verifying offset)
	time.Sleep(200 * time.Millisecond)
	cancel()
	<-done
}

func TestPoller_Run_NilMessageSkipped(t *testing.T) {
	var callCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := callCount.Add(1)
		if count == 1 {
			json.NewEncoder(w).Encode(apiResponse[[]Update]{
				Ok: true,
				Result: []Update{
					{UpdateID: 300, Message: nil},
					{
						UpdateID: 301,
						Message: &Message{
							MessageID: 1,
							From:      &User{ID: 111, FirstName: "Test"},
							Chat:      Chat{ID: 111, Type: "private"},
							Text:      "after nil",
						},
					},
				},
			})
		} else {
			json.NewEncoder(w).Encode(apiResponse[[]Update]{Ok: true, Result: []Update{}})
		}
	}))
	defer srv.Close()

	origHTTPDo := httpDo
	httpDo = func(c *http.Client, req *http.Request) (*http.Response, error) {
		return c.Do(req)
	}
	defer func() { httpDo = origHTTPDo }()

	origRetry := retryFn
	retryFn = func(_ context.Context, _ int, _ time.Duration, fn func() error) error {
		return fn()
	}
	defer func() { retryFn = origRetry }()

	client := &Client{
		baseURL:    srv.URL + "/",
		httpClient: srv.Client(),
	}
	p := NewPoller(client, []int64{111}, 1)

	out := make(chan TelegramMessage, 10)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		p.Run(ctx, out)
		close(done)
	}()

	select {
	case msg := <-out:
		if msg.Message.Text != "after nil" {
			t.Errorf("text = %q, want %q", msg.Message.Text, "after nil")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for message")
	}

	cancel()
	<-done
}

func TestPoller_Run_NetworkErrorRetry(t *testing.T) {
	var callCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := callCount.Add(1)
		if count <= 2 {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("error"))
			return
		}
		json.NewEncoder(w).Encode(apiResponse[[]Update]{
			Ok: true,
			Result: []Update{
				{
					UpdateID: 400,
					Message: &Message{
						MessageID: 1,
						From:      &User{ID: 111, FirstName: "Test"},
						Chat:      Chat{ID: 111, Type: "private"},
						Text:      "recovered",
					},
				},
			},
		})
	}))
	defer srv.Close()

	origHTTPDo := httpDo
	httpDo = func(c *http.Client, req *http.Request) (*http.Response, error) {
		return c.Do(req)
	}
	defer func() { httpDo = origHTTPDo }()

	// Use a retry that actually retries (calls fn multiple times)
	origRetry := retryFn
	retryFn = func(ctx context.Context, maxAttempts int, _ time.Duration, fn func() error) error {
		var lastErr error
		for range maxAttempts {
			lastErr = fn()
			if lastErr == nil {
				return nil
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
		}
		return lastErr
	}
	defer func() { retryFn = origRetry }()

	client := &Client{
		baseURL:    srv.URL + "/",
		httpClient: srv.Client(),
	}
	p := NewPoller(client, []int64{111}, 1)

	out := make(chan TelegramMessage, 10)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		p.Run(ctx, out)
		close(done)
	}()

	select {
	case msg := <-out:
		if msg.Message.Text != "recovered" {
			t.Errorf("text = %q, want %q", msg.Message.Text, "recovered")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for message after retry")
	}

	cancel()
	<-done
}

func TestPoller_Run_ContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(apiResponse[[]Update]{Ok: true, Result: []Update{}})
	}))
	defer srv.Close()

	origHTTPDo := httpDo
	httpDo = func(c *http.Client, req *http.Request) (*http.Response, error) {
		return c.Do(req)
	}
	defer func() { httpDo = origHTTPDo }()

	origRetry := retryFn
	retryFn = func(_ context.Context, _ int, _ time.Duration, fn func() error) error {
		return fn()
	}
	defer func() { retryFn = origRetry }()

	client := &Client{
		baseURL:    srv.URL + "/",
		httpClient: srv.Client(),
	}
	p := NewPoller(client, []int64{111}, 1)

	out := make(chan TelegramMessage, 10)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		p.Run(ctx, out)
		close(done)
	}()

	// Give it a moment to start polling, then cancel
	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// Run exited cleanly
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not exit after context cancellation")
	}
}

func TestPoller_Run_RetryExhaustedContinuesLoop(t *testing.T) {
	var callCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := callCount.Add(1)
		if count <= 3 {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("error"))
			return
		}
		json.NewEncoder(w).Encode(apiResponse[[]Update]{
			Ok: true,
			Result: []Update{
				{
					UpdateID: 500,
					Message: &Message{
						MessageID: 1,
						From:      &User{ID: 111, FirstName: "Test"},
						Chat:      Chat{ID: 111, Type: "private"},
						Text:      "finally",
					},
				},
			},
		})
	}))
	defer srv.Close()

	origHTTPDo := httpDo
	httpDo = func(c *http.Client, req *http.Request) (*http.Response, error) {
		return c.Do(req)
	}
	defer func() { httpDo = origHTTPDo }()

	// Retry that only tries once (will fail for first 3 calls)
	origRetry := retryFn
	retryFn = func(_ context.Context, _ int, _ time.Duration, fn func() error) error {
		return fn()
	}
	defer func() { retryFn = origRetry }()

	origDelay := retryDelay
	retryDelay = 10 * time.Millisecond
	defer func() { retryDelay = origDelay }()

	client := &Client{
		baseURL:    srv.URL + "/",
		httpClient: srv.Client(),
	}
	p := NewPoller(client, []int64{111}, 1)

	out := make(chan TelegramMessage, 10)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		p.Run(ctx, out)
		close(done)
	}()

	select {
	case msg := <-out:
		if msg.Message.Text != "finally" {
			t.Errorf("text = %q, want %q", msg.Message.Text, "finally")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout: poller should have continued after retry failure")
	}

	cancel()
	<-done
}

func TestPoller_isAllowed(t *testing.T) {
	p := NewPoller(NewClient("test"), []int64{111, 222}, 30)

	tests := []struct {
		name string
		user *User
		want bool
	}{
		{"allowed user", &User{ID: 111}, true},
		{"another allowed", &User{ID: 222}, true},
		{"rejected user", &User{ID: 999}, false},
		{"nil user", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := p.isAllowed(tt.user); got != tt.want {
				t.Errorf("isAllowed() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPoller_getUserID(t *testing.T) {
	p := NewPoller(NewClient("test"), nil, 30)

	if got := p.getUserID(nil); got != 0 {
		t.Errorf("getUserID(nil) = %d, want 0", got)
	}
	if got := p.getUserID(&User{ID: 42}); got != 42 {
		t.Errorf("getUserID({42}) = %d, want 42", got)
	}
}

func TestPoller_Run_NilFromRejected(t *testing.T) {
	var callCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := callCount.Add(1)
		if count == 1 {
			json.NewEncoder(w).Encode(apiResponse[[]Update]{
				Ok: true,
				Result: []Update{
					{
						UpdateID: 600,
						Message: &Message{
							MessageID: 1,
							From:      nil,
							Chat:      Chat{ID: 999, Type: "private"},
							Text:      "no from field",
						},
					},
				},
			})
		} else {
			json.NewEncoder(w).Encode(apiResponse[[]Update]{Ok: true, Result: []Update{}})
		}
	}))
	defer srv.Close()

	origHTTPDo := httpDo
	httpDo = func(c *http.Client, req *http.Request) (*http.Response, error) {
		return c.Do(req)
	}
	defer func() { httpDo = origHTTPDo }()

	origRetry := retryFn
	retryFn = func(_ context.Context, _ int, _ time.Duration, fn func() error) error {
		return fn()
	}
	defer func() { retryFn = origRetry }()

	client := &Client{
		baseURL:    srv.URL + "/",
		httpClient: srv.Client(),
	}
	p := NewPoller(client, []int64{111}, 1)

	out := make(chan TelegramMessage, 10)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		p.Run(ctx, out)
		close(done)
	}()

	select {
	case msg := <-out:
		t.Fatalf("should not receive message with nil From, got: %+v", msg)
	case <-ctx.Done():
		// Expected: no messages passed through
	}

	// Wait for Run to finish before defers restore package-level vars.
	<-done
}

func TestPoller_Run_ChannelFullContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return a message that will be allowed
		json.NewEncoder(w).Encode(apiResponse[[]Update]{
			Ok: true,
			Result: []Update{
				{
					UpdateID: 700,
					Message: &Message{
						MessageID: 1,
						From:      &User{ID: 111, FirstName: "Test"},
						Chat:      Chat{ID: 111, Type: "private"},
						Text:      "blocked",
					},
				},
			},
		})
	}))
	defer srv.Close()

	origHTTPDo := httpDo
	httpDo = func(c *http.Client, req *http.Request) (*http.Response, error) {
		return c.Do(req)
	}
	defer func() { httpDo = origHTTPDo }()

	origRetry := retryFn
	retryFn = func(_ context.Context, _ int, _ time.Duration, fn func() error) error {
		return fn()
	}
	defer func() { retryFn = origRetry }()

	client := &Client{
		baseURL:    srv.URL + "/",
		httpClient: srv.Client(),
	}
	p := NewPoller(client, []int64{111}, 1)

	// Use unbuffered channel that we never read from — forces the select to block
	out := make(chan TelegramMessage)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		p.Run(ctx, out)
		close(done)
	}()

	// Give time for the poller to get a message and block on channel send
	time.Sleep(200 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// Run exited via ctx.Done() in the select
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not exit when channel full and context cancelled")
	}
}

func TestPoller_Run_ContextCancelDuringRetryBackoff(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("error"))
	}))
	defer srv.Close()

	origHTTPDo := httpDo
	httpDo = func(c *http.Client, req *http.Request) (*http.Response, error) {
		return c.Do(req)
	}
	defer func() { httpDo = origHTTPDo }()

	// Retry that only tries once (always fails)
	origRetry := retryFn
	retryFn = func(_ context.Context, _ int, _ time.Duration, fn func() error) error {
		return fn()
	}
	defer func() { retryFn = origRetry }()

	// Use a long retry delay so context cancel happens during the wait
	origDelay := retryDelay
	retryDelay = 10 * time.Second
	defer func() { retryDelay = origDelay }()

	client := &Client{
		baseURL:    srv.URL + "/",
		httpClient: srv.Client(),
	}
	p := NewPoller(client, []int64{111}, 1)

	out := make(chan TelegramMessage, 10)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		p.Run(ctx, out)
		close(done)
	}()

	// Wait for the poller to enter the retry backoff, then cancel
	time.Sleep(200 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// Run exited via ctx.Done() during retry backoff
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not exit when context cancelled during retry backoff")
	}
}

func TestPoller_Poll_NetworkError(t *testing.T) {
	origHTTPDo := httpDo
	httpDo = func(c *http.Client, req *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("connection refused")
	}
	defer func() { httpDo = origHTTPDo }()

	client := &Client{
		baseURL:    "http://localhost:1/",
		httpClient: &http.Client{},
	}
	p := NewPoller(client, []int64{111}, 1)

	_, err := p.Poll(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "telegram: poll:") {
		t.Errorf("error = %q, want to contain 'telegram: poll:'", err.Error())
	}
}
