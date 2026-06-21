package layer1_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	res "github.com/dashfabric/fm/pkg/resilience"
)

func TestRetryPolicySuccess(t *testing.T) {
	policy := res.DefaultRetryPolicy()
	attempts := 0

	err := policy.Attempt(context.Background(), func() error {
		attempts++
		if attempts < 2 {
			return fmt.Errorf("temporary error")
		}
		return nil
	})

	if err != nil {
		t.Fatalf("Expected success after retry, got error: %v", err)
	}

	if attempts != 2 {
		t.Errorf("Expected 2 attempts, got %d", attempts)
	}

	t.Logf("✓ Retry succeeded after %d attempts", attempts)
}

func TestRetryPolicyMaxRetriesExceeded(t *testing.T) {
	policy := res.DefaultRetryPolicy()
	attempts := 0

	err := policy.Attempt(context.Background(), func() error {
		attempts++
		return fmt.Errorf("persistent error")
	})

	if err == nil {
		t.Fatal("Expected error when max retries exceeded")
	}

	if attempts != policy.MaxRetries+1 {
		t.Errorf("Expected %d attempts, got %d", policy.MaxRetries+1, attempts)
	}

	t.Logf("✓ Max retries enforced: %d attempts", attempts)
}

func TestRetryPolicyContextCancellation(t *testing.T) {
	policy := res.DefaultRetryPolicy()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := policy.Attempt(ctx, func() error {
		return fmt.Errorf("should not reach here")
	})

	if err == nil {
		t.Fatal("Expected error on context cancellation")
	}

	if !strings.Contains(fmt.Sprint(err), "retry cancelled") {
		t.Errorf("Expected cancellation error, got: %v", err)
	}

	t.Logf("✓ Context cancellation handled correctly")
}

func TestCircuitBreakerClosed(t *testing.T) {
	cb := res.NewCircuitBreaker(3, 2, 100*time.Millisecond)

	// Successful calls should work
	for i := 0; i < 5; i++ {
		err := cb.Call(func() error { return nil })
		if err != nil {
			t.Fatalf("Circuit breaker should allow calls when closed: %v", err)
		}
	}

	if cb.State() != res.StateClosed {
		t.Errorf("Expected circuit to remain closed, got state: %v", cb.State())
	}

	t.Logf("✓ Circuit breaker closed state working correctly")
}

func TestCircuitBreakerOpenOnThreshold(t *testing.T) {
	cb := res.NewCircuitBreaker(2, 1, 100*time.Millisecond)

	// Fail enough times to open circuit
	for i := 0; i < 3; i++ {
		_ = cb.Call(func() error { return fmt.Errorf("error") })
	}

	if cb.State() != res.StateOpen {
		t.Errorf("Expected circuit to be open after threshold, got state: %v", cb.State())
	}

	// Verify circuit rejects calls
	err := cb.Call(func() error { return nil })
	if err == nil {
		t.Fatal("Expected circuit breaker to reject calls when open")
	}

	t.Logf("✓ Circuit breaker opened at threshold")
}

func TestCircuitBreakerHalfOpenToClosedTransition(t *testing.T) {
	cb := res.NewCircuitBreaker(1, 1, 50*time.Millisecond)

	// Open circuit
	_ = cb.Call(func() error { return fmt.Errorf("error") })
	if cb.State() != res.StateOpen {
		t.Fatal("Circuit should be open")
	}

	// Wait for timeout
	time.Sleep(100 * time.Millisecond)

	// Should transition to half-open on next call and close on success
	err := cb.Call(func() error { return nil })
	if err != nil {
		t.Errorf("Half-open circuit should allow test call: %v", err)
	}

	if cb.State() != res.StateClosed {
		t.Errorf("Expected circuit to close after successful half-open call, got state: %v", cb.State())
	}

	t.Logf("✓ Circuit breaker half-open to closed transition working")
}

func TestCircuitBreakerReset(t *testing.T) {
	cb := res.NewCircuitBreaker(1, 1, 100*time.Millisecond)

	// Open circuit
	_ = cb.Call(func() error { return fmt.Errorf("error") })

	if cb.State() != res.StateOpen {
		t.Fatal("Circuit should be open")
	}

	// Reset
	cb.Reset()

	if cb.State() != res.StateClosed {
		t.Errorf("Expected circuit to be closed after reset, got state: %v", cb.State())
	}

	// Should accept calls
	err := cb.Call(func() error { return nil })
	if err != nil {
		t.Errorf("Circuit should accept calls after reset: %v", err)
	}

	t.Logf("✓ Circuit breaker reset working correctly")
}

func TestTimeoutManager(t *testing.T) {
	tm := res.NewTimeoutManager(10 * time.Second)
	tm.SetStageTimeout("stage1", 2*time.Second)

	ctx := context.Background()

	// Test stage with custom timeout
	stageCtx, cancel := tm.WithTimeout(ctx, "stage1")
	defer cancel()

	done := make(chan bool, 1)
	go func() {
		<-stageCtx.Done()
		done <- true
	}()

	// Should timeout within 3 seconds (with some margin)
	select {
	case <-done:
		t.Logf("✓ Timeout manager enforced 2-second timeout for stage1")
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout manager did not enforce timeout")
	}
}

func TestErrorClassification(t *testing.T) {
	tests := []struct {
		err    error
		expect res.ErrorType
	}{
		{fmt.Errorf("connection refused"), res.ErrorTypeTransient},
		{fmt.Errorf("context deadline exceeded"), res.ErrorTypeTimeout},
		{fmt.Errorf("circuit breaker open: service unavailable"), res.ErrorTypeCircuitOpen},
		{fmt.Errorf("unknown error"), res.ErrorTypePermanent},
	}

	for _, tc := range tests {
		result := res.ClassifyError(tc.err)
		if result != tc.expect {
			t.Errorf("ClassifyError(%v): expected %v, got %v", tc.err, tc.expect, result)
		}
	}

	t.Logf("✓ Error classification working correctly")
}

func TestIsRetryable(t *testing.T) {
	retryable := []error{
		fmt.Errorf("connection refused"),
		fmt.Errorf("network unreachable"),
		fmt.Errorf("temporary failure"),
	}

	for _, err := range retryable {
		if !res.IsRetryable(err) {
			t.Errorf("Expected %v to be retryable", err)
		}
	}

	nonRetryable := fmt.Errorf("permanent error")
	if res.IsRetryable(nonRetryable) {
		t.Errorf("Expected %v to not be retryable", nonRetryable)
	}

	t.Logf("✓ Retry classification working correctly")
}

func TestRetryPolicyWithBackoff(t *testing.T) {
	policy := res.RetryPolicy{
		MaxRetries:        2,
		InitialBackoff:    50 * time.Millisecond,
		MaxBackoff:        200 * time.Millisecond,
		BackoffMultiplier: 2.0,
		JitterFraction:    0.1,
	}

	start := time.Now()
	attempts := 0

	policy.Attempt(context.Background(), func() error {
		attempts++
		if attempts < 3 {
			return fmt.Errorf("retry")
		}
		return nil
	})

	elapsed := time.Since(start)

	// Should have at least some backoff (100ms min, accounting for jitter variations)
	if elapsed < 100*time.Millisecond {
		t.Errorf("Expected at least 100ms backoff, got %v", elapsed)
	}

	t.Logf("✓ Retry backoff timing verified: %v elapsed", elapsed)
}
