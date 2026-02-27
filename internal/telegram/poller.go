package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"time"

	"github.com/edouard/pureclaw/internal/platform"
)

// retryFn is a package-level variable wrapping platform.Retry for testability.
var retryFn = platform.Retry

// retryDelay is the delay after all retries are exhausted before starting a new cycle.
var retryDelay = 5 * time.Second

// Poller receives updates from the Telegram Bot API using long polling.
type Poller struct {
	client     *Client
	allowedIDs map[int64]bool
	offset     int64
	timeout    int
}

// NewPoller creates a new Poller with a whitelist of allowed user IDs.
func NewPoller(client *Client, allowedIDs []int64, timeout int) *Poller {
	allowed := make(map[int64]bool, len(allowedIDs))
	for _, id := range allowedIDs {
		allowed[id] = true
	}
	return &Poller{
		client:     client,
		allowedIDs: allowed,
		timeout:    timeout,
	}
}

// Poll performs a single getUpdates call and returns the updates.
func (p *Poller) Poll(ctx context.Context) ([]Update, error) {
	params := url.Values{}
	if p.offset > 0 {
		params.Set("offset", strconv.FormatInt(p.offset, 10))
	}
	params.Set("timeout", strconv.Itoa(p.timeout))
	params.Set("allowed_updates", `["message"]`)

	// Use a longer timeout for the HTTP request to accommodate long polling.
	pollCtx, cancel := context.WithTimeout(ctx, time.Duration(p.timeout)*time.Second+5*time.Second)
	defer cancel()

	data, err := p.client.doGet(pollCtx, "getUpdates", params)
	if err != nil {
		return nil, fmt.Errorf("telegram: poll: %w", err)
	}

	var resp apiResponse[[]Update]
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("telegram: poll: unmarshal: %w", err)
	}

	if !resp.Ok {
		return nil, fmt.Errorf("telegram: poll: %s", resp.Description)
	}

	return resp.Result, nil
}

// Run starts the long polling loop, filtering messages by whitelist
// and sending valid messages on the out channel.
func (p *Poller) Run(ctx context.Context, out chan<- TelegramMessage) {
	slog.Info("poller started", "component", "telegram", "operation", "poll_start")

	for {
		var updates []Update
		err := retryFn(ctx, 3, 2*time.Second, func() error {
			var pollErr error
			updates, pollErr = p.Poll(ctx)
			return pollErr
		})
		if err != nil {
			if ctx.Err() != nil {
				slog.Info("poller stopped", "component", "telegram", "operation", "poll_stop")
				return
			}
			slog.Error("poll failed after retries", "component", "telegram", "operation", "poll", "error", err)
			select {
			case <-time.After(retryDelay):
			case <-ctx.Done():
				slog.Info("poller stopped", "component", "telegram", "operation", "poll_stop")
				return
			}
			continue
		}

		for _, u := range updates {
			if u.UpdateID >= p.offset {
				p.offset = u.UpdateID + 1
			}
			if u.Message == nil {
				continue
			}
			if !p.isAllowed(u.Message.From) {
				slog.Warn("rejected unauthorized message",
					"component", "telegram",
					"operation", "whitelist",
					"user_id", p.getUserID(u.Message.From),
				)
				continue
			}
			select {
			case out <- TelegramMessage{Message: *u.Message}:
			case <-ctx.Done():
				slog.Info("poller stopped", "component", "telegram", "operation", "poll_stop")
				return
			}
		}
	}
}

// isAllowed checks if the user is in the whitelist.
func (p *Poller) isAllowed(user *User) bool {
	if user == nil {
		return false
	}
	return p.allowedIDs[user.ID]
}

// getUserID safely extracts the user ID for logging.
func (p *Poller) getUserID(user *User) int64 {
	if user == nil {
		return 0
	}
	return user.ID
}
