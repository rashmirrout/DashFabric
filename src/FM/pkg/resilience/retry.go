package resilience

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"time"
)

// RetryPolicy defines retry behavior with exponential backoff
type RetryPolicy struct {
	MaxRetries      int
	InitialBackoff  time.Duration
	MaxBackoff      time.Duration
	BackoffMultiplier float64
	JitterFraction    float64
}

// DefaultRetryPolicy returns a sensible default retry policy
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxRetries:       3,
		InitialBackoff:   100 * time.Millisecond,
		MaxBackoff:       10 * time.Second,
		BackoffMultiplier: 2.0,
		JitterFraction:   0.1,
	}
}

// Attempt executes function with retry logic and exponential backoff
func (rp *RetryPolicy) Attempt(ctx context.Context, fn func() error) error {
	var lastErr error

	for attempt := 0; attempt <= rp.MaxRetries; attempt++ {
		select {
		case <-ctx.Done():
			return fmt.Errorf("retry cancelled: %w", ctx.Err())
		default:
		}

		err := fn()
		if err == nil {
			return nil
		}

		lastErr = err
		if attempt < rp.MaxRetries {
			backoff := rp.calculateBackoff(attempt)
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return fmt.Errorf("retry cancelled: %w", ctx.Err())
			}
		}
	}

	return fmt.Errorf("max retries exceeded (last error: %w)", lastErr)
}

// calculateBackoff computes exponential backoff with jitter
func (rp *RetryPolicy) calculateBackoff(attempt int) time.Duration {
	backoff := time.Duration(float64(rp.InitialBackoff) * math.Pow(rp.BackoffMultiplier, float64(attempt)))

	if backoff > rp.MaxBackoff {
		backoff = rp.MaxBackoff
	}

	// Add jitter
	jitter := time.Duration(float64(backoff) * rp.JitterFraction * (rand.Float64()*2 - 1))
	return backoff + jitter
}

// AttemptAsync executes function asynchronously with retry logic
func (rp *RetryPolicy) AttemptAsync(ctx context.Context, fn func() error) <-chan error {
	errChan := make(chan error, 1)

	go func() {
		errChan <- rp.Attempt(ctx, fn)
	}()

	return errChan
}
