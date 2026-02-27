package platform

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRetry_successFirstAttempt(t *testing.T) {
	calls := 0
	err := Retry(context.Background(), 3, time.Millisecond, func() error {
		calls++
		return nil
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected 1 call, got %d", calls)
	}
}

func TestRetry_successAfterRetries(t *testing.T) {
	calls := 0
	err := Retry(context.Background(), 3, time.Millisecond, func() error {
		calls++
		if calls < 3 {
			return errors.New("temporary")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if calls != 3 {
		t.Fatalf("expected 3 calls, got %d", calls)
	}
}

func TestRetry_allAttemptsFail(t *testing.T) {
	sentinel := errors.New("persistent error")
	calls := 0
	err := Retry(context.Background(), 3, time.Millisecond, func() error {
		calls++
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}
	if calls != 3 {
		t.Fatalf("expected 3 calls, got %d", calls)
	}
}

func TestRetry_contextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	err := Retry(ctx, 5, 100*time.Millisecond, func() error {
		calls++
		if calls == 1 {
			cancel() // Cancel before retry wait.
		}
		return errors.New("fail")
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected 1 call before cancellation, got %d", calls)
	}
}

func TestRetry_contextTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()

	err := Retry(ctx, 100, 50*time.Millisecond, func() error {
		return errors.New("fail")
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded, got %v", err)
	}
}

func TestRetry_singleAttempt(t *testing.T) {
	sentinel := errors.New("single fail")
	err := Retry(context.Background(), 1, time.Millisecond, func() error {
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}
}

func TestRetry_exponentialBackoff(t *testing.T) {
	baseDelay := 10 * time.Millisecond
	calls := 0
	timestamps := make([]time.Time, 0, 4)

	err := Retry(context.Background(), 4, baseDelay, func() error {
		timestamps = append(timestamps, time.Now())
		calls++
		if calls < 4 {
			return errors.New("fail")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if calls != 4 {
		t.Fatalf("expected 4 calls, got %d", calls)
	}

	// Verify delays are roughly exponential: 10ms, 20ms, 40ms.
	for i := 1; i < len(timestamps); i++ {
		gap := timestamps[i].Sub(timestamps[i-1])
		expectedMin := baseDelay * (1 << (i - 1)) / 2 // Allow 50% tolerance.
		if gap < expectedMin {
			t.Errorf("gap %d: %v < expected min %v", i, gap, expectedMin)
		}
	}
}

func TestRetry_zeroAttempts(t *testing.T) {
	calls := 0
	err := Retry(context.Background(), 0, time.Millisecond, func() error {
		calls++
		return errors.New("should not be called")
	})
	if err != nil {
		t.Fatalf("expected nil error for 0 attempts, got %v", err)
	}
	if calls != 0 {
		t.Fatalf("expected 0 calls, got %d", calls)
	}
}
