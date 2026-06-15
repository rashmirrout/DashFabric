package reliability

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

// TestRetryNewDefaults verifies default values
func TestRetryNewDefaults(t *testing.T) {
	retry := NewRetry(RetryConfig{})

	if retry.config.MaxAttempts != 3 {
		t.Errorf("expected MaxAttempts=3, got %d", retry.config.MaxAttempts)
	}

	if retry.config.InitialBackoff != 100*time.Millisecond {
		t.Errorf("expected InitialBackoff=100ms, got %v", retry.config.InitialBackoff)
	}

	if retry.config.MaxBackoff != 30*time.Second {
		t.Errorf("expected MaxBackoff=30s, got %v", retry.config.MaxBackoff)
	}

	if retry.config.Multiplier != 2.0 {
		t.Errorf("expected Multiplier=2.0, got %v", retry.config.Multiplier)
	}

	if retry.config.JitterFraction != 0.1 {
		t.Errorf("expected JitterFraction=0.1, got %v", retry.config.JitterFraction)
	}
}

// TestRetrySuccessFirstAttempt verifies success on first attempt
func TestRetrySuccessFirstAttempt(t *testing.T) {
	retry := NewRetry(RetryConfig{})

	start := time.Now()
	err := retry.Execute(context.Background(), func() error {
		return nil
	})

	elapsed := time.Since(start)

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if elapsed > 50*time.Millisecond {
		t.Errorf("first attempt should not wait, elapsed %v", elapsed)
	}
}

// TestRetryNonRetryableErrorImmediate verifies non-retryable errors return immediately
func TestRetryNonRetryableErrorImmediate(t *testing.T) {
	retry := NewRetry(RetryConfig{
		MaxAttempts: 3,
	})

	testError := errors.New("invalid input")
	attempts := 0

	start := time.Now()
	err := retry.Execute(context.Background(), func() error {
		attempts++
		return testError
	})
	elapsed := time.Since(start)

	if err != testError {
		t.Errorf("expected error %v, got %v", testError, err)
	}

	if attempts != 1 {
		t.Errorf("expected 1 attempt, got %d", attempts)
	}

	if elapsed > 50*time.Millisecond {
		t.Errorf("non-retryable error should return immediately, elapsed %v", elapsed)
	}
}

// TestRetryRetryableErrorEventually succeeds after retries
func TestRetryRetryableErrorEventually(t *testing.T) {
	retry := NewRetry(RetryConfig{
		MaxAttempts:    4,
		InitialBackoff: 20 * time.Millisecond,
		MaxBackoff:     500 * time.Millisecond,
		Multiplier:     2.0,
		JitterFraction: 0.0,
	})

	attempts := 0
	err := retry.Execute(context.Background(), func() error {
		attempts++
		if attempts < 3 {
			return context.DeadlineExceeded
		}
		return nil
	})

	if err != nil {
		t.Errorf("expected success on 3rd attempt, got %v", err)
	}

	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}

// TestRetryAllAttemptsFail verifies error after all attempts fail
func TestRetryAllAttemptsFail(t *testing.T) {
	testError := errors.New("timeout")
	retry := NewRetry(RetryConfig{
		MaxAttempts:    3,
		InitialBackoff: 10 * time.Millisecond,
		Multiplier:     2.0,
		JitterFraction: 0.0,
	})

	attempts := 0
	err := retry.Execute(context.Background(), func() error {
		attempts++
		return testError
	})

	if err != testError {
		t.Errorf("expected error %v, got %v", testError, err)
	}

	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}

// TestRetryExponentialBackoff verifies exponential growth
func TestRetryExponentialBackoff(t *testing.T) {
	retry := NewRetry(RetryConfig{
		InitialBackoff: 100 * time.Millisecond,
		MaxBackoff:     10 * time.Second,
		Multiplier:     2.0,
		JitterFraction: 0.0,
	})

	expected := []time.Duration{
		100 * time.Millisecond,
		200 * time.Millisecond,
		400 * time.Millisecond,
	}

	for i, exp := range expected {
		backoff := retry.calculateBackoff(i)
		if backoff != exp {
			t.Errorf("attempt %d: expected %v, got %v", i, exp, backoff)
		}
	}
}

// TestRetryBackoffCapped verifies max backoff enforcement
func TestRetryBackoffCapped(t *testing.T) {
	maxBackoff := 500 * time.Millisecond
	retry := NewRetry(RetryConfig{
		InitialBackoff: 100 * time.Millisecond,
		MaxBackoff:     maxBackoff,
		Multiplier:     2.0,
	})

	// Calculate what backoff would be without cap
	backoff := retry.calculateBackoff(10) // 100ms * 2^10 = 102400ms

	if backoff > maxBackoff {
		t.Errorf("backoff %v should be capped at %v", backoff, maxBackoff)
	}

	if backoff != maxBackoff {
		t.Errorf("expected backoff=%v, got %v", maxBackoff, backoff)
	}
}

// TestRetryContextCancellation verifies context cancellation stops retry
func TestRetryContextCancellation(t *testing.T) {
	retry := NewRetry(RetryConfig{
		MaxAttempts:    10,
		InitialBackoff: 100 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())

	attempts := 0
	err := retry.Execute(ctx, func() error {
		attempts++
		if attempts == 2 {
			cancel() // Cancel during backoff
		}
		return context.DeadlineExceeded
	})

	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}

	if attempts > 3 {
		t.Errorf("should stop after cancellation, got %d attempts", attempts)
	}
}

// TestRetryContextDeadlineExceeded verifies deadline exceeded detection
func TestRetryContextDeadlineExceeded(t *testing.T) {
	if !isRetryable(context.DeadlineExceeded) {
		t.Errorf("context.DeadlineExceeded should be retryable")
	}
}

// TestRetryNetErrClosed verifies net.ErrClosed is retryable
func TestRetryNetErrClosed(t *testing.T) {
	if !isRetryable(net.ErrClosed) {
		t.Errorf("net.ErrClosed should be retryable")
	}
}

// TestRetryConnectionRefused verifies connection refused pattern
func TestRetryConnectionRefused(t *testing.T) {
	err := errors.New("connection refused")
	if !isRetryable(err) {
		t.Errorf("'connection refused' should be retryable")
	}
}

// TestRetryTimeoutPattern verifies timeout pattern detection
func TestRetryTimeoutPattern(t *testing.T) {
	err := errors.New("request timeout")
	if !isRetryable(err) {
		t.Errorf("'timeout' pattern should be retryable")
	}
}

// TestRetryUnavailablePattern verifies unavailable pattern
func TestRetryUnavailablePattern(t *testing.T) {
	err := errors.New("service temporarily unavailable")
	if !isRetryable(err) {
		t.Errorf("'temporarily unavailable' should be retryable")
	}
}

// TestRetryNonRetryableErrors verifies non-retryable patterns
func TestRetryNonRetryableErrors(t *testing.T) {
	nonRetryable := []error{
		errors.New("unauthorized"),
		errors.New("invalid token"),
		errors.New("bad request"),
		errors.New("forbidden"),
	}

	for _, err := range nonRetryable {
		if isRetryable(err) {
			t.Errorf("'%v' should not be retryable", err)
		}
	}
}

// TestRetryJitterVariation verifies jitter adds variation
func TestRetryJitterVariation(t *testing.T) {
	retry := NewRetry(RetryConfig{
		JitterFraction: 0.1,
	})

	backoff := 100 * time.Millisecond
	jitters := make([]time.Duration, 100)

	for i := 0; i < len(jitters); i++ {
		jitters[i] = retry.calculateJitter(backoff)
	}

	// Check that jitters vary
	hasPositive := false
	hasNegative := false

	for _, j := range jitters {
		if j > 0 {
			hasPositive = true
		}
		if j < 0 {
			hasNegative = true
		}
	}

	if !hasPositive || !hasNegative {
		t.Errorf("jitter should produce both positive and negative values")
	}

	// Check that jitter is bounded
	maxJitter := time.Duration(float64(backoff)*0.1) + time.Duration(1)
	for _, j := range jitters {
		if j > maxJitter || j < -maxJitter {
			t.Errorf("jitter %v exceeds expected bounds ±%v", j, maxJitter)
		}
	}
}

// TestRetryTiming verifies timing of backoff + jitter
func TestRetryTiming(t *testing.T) {
	r := NewRetry(RetryConfig{
		MaxAttempts:    3,
		InitialBackoff: 30 * time.Millisecond,
		MaxBackoff:     500 * time.Millisecond,
		Multiplier:     2.0,
		JitterFraction: 0.0,
	})

	start := time.Now()
	attempts := 0
	r.Execute(context.Background(), func() error {
		attempts++
		return context.DeadlineExceeded
	})
	elapsed := time.Since(start)

	// Expected: 30ms + 60ms = 90ms (plus some tolerance for system overhead)
	expectedMin := 70 * time.Millisecond
	expectedMax := 150 * time.Millisecond

	if elapsed < expectedMin || elapsed > expectedMax {
		t.Errorf("expected timing between %v and %v, got %v", expectedMin, expectedMax, elapsed)
	}
}

// TestRetryPropertyExponentialGrowth property: backoff grows exponentially
func TestRetryPropertyExponentialGrowth(t *testing.T) {
	retry := NewRetry(RetryConfig{
		InitialBackoff: 100 * time.Millisecond,
		MaxBackoff:     100 * time.Second,
		Multiplier:     2.0,
		JitterFraction: 0.0,
	})

	prev := time.Duration(0)
	for attempt := 0; attempt < 5; attempt++ {
		backoff := retry.calculateBackoff(attempt)

		if attempt > 0 {
			ratio := float64(backoff) / float64(prev)
			expected := 2.0
			tolerance := 0.001

			if ratio < expected-tolerance || ratio > expected+tolerance {
				t.Errorf("attempt %d: expected ratio ~%.1f, got %.1f", attempt, expected, ratio)
			}
		}

		prev = backoff
	}
}

// TestRetryPropertyRetryableDetection property: all patterns consistently detected
func TestRetryPropertyRetryableDetection(t *testing.T) {
	retryablePatterns := []string{
		"connection refused",
		"timeout",
		"reset by peer",
		"temporarily unavailable",
		"broken pipe",
	}

	for _, pattern := range retryablePatterns {
		err := errors.New(pattern)
		if !isRetryable(err) {
			t.Errorf("pattern '%s' should be retryable", pattern)
		}
	}
}
