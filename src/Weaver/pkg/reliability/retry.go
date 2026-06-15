package reliability

import (
	"context"
	"errors"
	"math"
	"math/rand"
	"net"
	"strings"
	"time"
)

type RetryConfig struct {
	MaxAttempts    int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	Multiplier     float64
	JitterFraction float64
}

type Retry struct {
	config RetryConfig
}

func NewRetry(config RetryConfig) *Retry {
	if config.MaxAttempts == 0 {
		config.MaxAttempts = 3
	}
	if config.InitialBackoff == 0 {
		config.InitialBackoff = 100 * time.Millisecond
	}
	if config.MaxBackoff == 0 {
		config.MaxBackoff = 30 * time.Second
	}
	if config.Multiplier == 0 {
		config.Multiplier = 2.0
	}
	if config.JitterFraction == 0 {
		config.JitterFraction = 0.1
	}

	return &Retry{config: config}
}

// Execute runs function with retry logic
func (r *Retry) Execute(ctx context.Context, fn func() error) error {
	var lastErr error

	for attempt := 0; attempt < r.config.MaxAttempts; attempt++ {
		err := fn()
		if err == nil {
			return nil
		}

		lastErr = err

		if !isRetryable(err) {
			return err
		}

		if attempt == r.config.MaxAttempts-1 {
			break
		}

		backoff := r.calculateBackoff(attempt)
		jitter := r.calculateJitter(backoff)
		totalWait := backoff + jitter

		select {
		case <-time.After(totalWait):
			// Time elapsed, proceed to next attempt

		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return lastErr
}

// calculateBackoff computes exponential backoff
func (r *Retry) calculateBackoff(attempt int) time.Duration {
	multiplied := float64(r.config.InitialBackoff) *
		math.Pow(r.config.Multiplier, float64(attempt))

	backoff := time.Duration(multiplied)

	if backoff > r.config.MaxBackoff {
		backoff = r.config.MaxBackoff
	}

	return backoff
}

// calculateJitter adds ±variation to backoff
func (r *Retry) calculateJitter(backoff time.Duration) time.Duration {
	jitterAmount := float64(backoff) * r.config.JitterFraction
	randomJitter := (rand.Float64() - 0.5) * 2 * jitterAmount
	return time.Duration(randomJitter)
}

// isRetryable determines if error should trigger retry
func isRetryable(err error) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	if errors.Is(err, net.ErrClosed) {
		return true
	}

	errStr := err.Error()
	retryablePatterns := []string{
		"connection refused",
		"timeout",
		"temporarily unavailable",
		"reset by peer",
		"broken pipe",
		"unavailable",
	}

	for _, pattern := range retryablePatterns {
		if strings.Contains(strings.ToLower(errStr), pattern) {
			return true
		}
	}

	return false
}
