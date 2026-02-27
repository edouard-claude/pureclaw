package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"
)

// Client is an HTTP client wrapper for the Telegram Bot API.
type Client struct {
	token      string
	baseURL    string
	httpClient *http.Client
}

// httpDo is a package-level variable for testability.
var httpDo = func(client *http.Client, req *http.Request) (*http.Response, error) {
	return client.Do(req)
}

// NewClient creates a new Telegram Bot API client.
func NewClient(token string) *Client {
	return &Client{
		token:   token,
		baseURL: "https://api.telegram.org/bot" + token + "/",
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// doPost sends a POST request with a JSON body to the given Telegram API method.
func (c *Client) doPost(ctx context.Context, method string, body any) ([]byte, error) {
	slog.Debug("telegram API POST", "component", "telegram", "operation", method)

	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("%s: marshal: %w", method, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+method, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%s: new request: %w", method, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpDo(c.httpClient, req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", method, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("%s: read body: %w", method, err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: unexpected status %d: %s", method, resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

// doGet sends a GET request with query parameters to the given Telegram API method.
func (c *Client) doGet(ctx context.Context, method string, params url.Values) ([]byte, error) {
	slog.Debug("telegram API GET", "component", "telegram", "operation", method)

	u := c.baseURL + method
	if len(params) > 0 {
		u += "?" + params.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("%s: new request: %w", method, err)
	}

	resp, err := httpDo(c.httpClient, req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", method, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("%s: read body: %w", method, err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: unexpected status %d: %s", method, resp.StatusCode, string(respBody))
	}

	return respBody, nil
}
