package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
)

// File represents a Telegram file object from the getFile API.
type File struct {
	FileID   string `json:"file_id"`
	FilePath string `json:"file_path"`
	FileSize int64  `json:"file_size,omitempty"`
}

// GetFile retrieves the file path for a given file ID from Telegram servers.
func (c *Client) GetFile(ctx context.Context, fileID string) (string, error) {
	slog.Debug("telegram API getFile", "component", "telegram", "operation", "get_file", "file_id", fileID)

	params := url.Values{"file_id": {fileID}}
	data, err := c.doGet(ctx, "getFile", params)
	if err != nil {
		return "", fmt.Errorf("get file: %w", err)
	}

	var resp apiResponse[File]
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", fmt.Errorf("get file: parse response: %w", err)
	}
	if !resp.Ok {
		return "", fmt.Errorf("get file: API error: %s", resp.Description)
	}

	slog.Debug("file path resolved", "component", "telegram", "operation", "get_file", "file_path", resp.Result.FilePath)
	return resp.Result.FilePath, nil
}

// DownloadFile downloads the raw file bytes from Telegram servers.
func (c *Client) DownloadFile(ctx context.Context, filePath string) ([]byte, error) {
	slog.Debug("telegram API download file", "component", "telegram", "operation", "download_file", "file_path", filePath)

	// Download URL uses /file/bot<token>/<file_path> — different from API base URL.
	// Derive from baseURL to keep testability (no hardcoded domain).
	fileURL := strings.Replace(c.baseURL, "/bot", "/file/bot", 1) + filePath

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
	if err != nil {
		return nil, fmt.Errorf("download file: create request: %w", err)
	}

	resp, err := httpDo(c.httpClient, req)
	if err != nil {
		return nil, fmt.Errorf("download file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download file: unexpected status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("download file: read body: %w", err)
	}

	slog.Debug("file downloaded", "component", "telegram", "operation", "download_file", "size", len(data))
	return data, nil
}
