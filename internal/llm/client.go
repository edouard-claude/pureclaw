package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// httpDo is a package-level variable for testability.
var httpDo = func(client *http.Client, req *http.Request) (*http.Response, error) {
	return client.Do(req)
}

// Client is an HTTP client wrapper for the Mistral API.
type Client struct {
	apiKey     string
	baseURL    string
	model      string
	httpClient *http.Client
}

// httpError represents an HTTP error response from the Mistral API.
type httpError struct {
	StatusCode int
	Endpoint   string
	Body       string
}

func (e *httpError) Error() string {
	return fmt.Sprintf("llm: %s: status %d: %s", e.Endpoint, e.StatusCode, e.Body)
}

// IsRetryable returns true for 429 (rate limit) and 5xx (server error) status codes.
func (e *httpError) IsRetryable() bool {
	return e.StatusCode == http.StatusTooManyRequests || e.StatusCode >= 500
}

// NewClient creates a new Mistral API client with HTTPS base URL and 10s timeout.
func NewClient(apiKey, model string) *Client {
	return &Client{
		apiKey:  apiKey,
		baseURL: "https://api.mistral.ai/v1/",
		model:   model,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// doPost sends a POST request with a JSON body to the given Mistral API endpoint.
func (c *Client) doPost(ctx context.Context, endpoint string, body any) ([]byte, error) {
	slog.Debug("mistral API POST", "component", "llm", "operation", endpoint)

	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("llm: %s: marshal: %w", endpoint, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("llm: %s: request: %w", endpoint, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := httpDo(c.httpClient, req)
	if err != nil {
		return nil, fmt.Errorf("llm: %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("llm: %s: read body: %w", endpoint, err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &httpError{StatusCode: resp.StatusCode, Endpoint: endpoint, Body: string(respBody)}
	}

	return respBody, nil
}
