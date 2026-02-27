package platform

import (
	"context"
	"log/slog"
	"time"
)

// Retry calls fn up to maxAttempts times with exponential backoff.
// Backoff: baseDelay * 2^attempt. Respects context cancellation.
// Returns nil immediately if maxAttempts <= 0 (fn is never called).
func Retry(ctx context.Context, maxAttempts int, baseDelay time.Duration, fn func() error) error {
	var lastErr error
	for attempt := range maxAttempts {
		lastErr = fn()
		if lastErr == nil {
			return nil
		}

		slog.Warn("retry attempt failed",
			"component", "platform",
			"operation", "retry",
			"attempt", attempt+1,
			"max_attempts", maxAttempts,
			"error", lastErr,
		)

		// Don't wait after the last attempt.
		if attempt == maxAttempts-1 {
			break
		}

		delay := baseDelay * (1 << attempt)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return lastErr
}
