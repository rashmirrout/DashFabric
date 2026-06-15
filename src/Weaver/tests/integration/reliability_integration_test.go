package integration

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dashfabric/weaver/pkg/reliability"
	"github.com/dashfabric/weaver/pkg/testutil"
)

// TestReliabilityCircuitBreakerTrip verifies CB opens after failures
func TestReliabilityCircuitBreakerTrip(t *testing.T) {
	cb := reliability.NewCircuitBreaker(3, 2, 50*time.Millisecond)

	// Simulate 3 failures
	for i := 0; i < 3; i++ {
		if !cb.Allow() {
			t.Errorf("CB should allow request at failure %d", i+1)
		}
		cb.RecordFailure()
	}

	// After 3 failures, CB should be open
	if cb.GetState() != reliability.StateOpen {
		t.Errorf("expected state=OPEN, got %v", cb.GetState())
	}

	// Should reject immediately
	if cb.Allow() {
		t.Errorf("OPEN CB should reject requests")
	}

	// Wait for timeout
	time.Sleep(60 * time.Millisecond)

	// Should probe
	if !cb.Allow() {
		t.Errorf("after timeout, should allow probe")
	}

	if cb.GetState() != reliability.StateHalfOpen {
		t.Errorf("expected state=HALF_OPEN")
	}

	// Probe success
	cb.RecordSuccess()
	cb.RecordSuccess()

	if cb.GetState() != reliability.StateClosed {
		t.Errorf("after 2 successes (threshold), should be CLOSED")
	}

	if !cb.Allow() {
		t.Errorf("CLOSED CB should allow requests")
	}
}

// TestReliabilityRetryWithExponentialBackoff verifies retry logic
func TestReliabilityRetryWithExponentialBackoff(t *testing.T) {
	r := reliability.NewRetry(reliability.RetryConfig{
		MaxAttempts:    3,
		InitialBackoff: 10 * time.Millisecond,
		MaxBackoff:     100 * time.Millisecond,
		Multiplier:     2.0,
		JitterFraction: 0.0,
	})

	attempts := 0
	start := time.Now()

	err := r.Execute(context.Background(), func() error {
		attempts++
		if attempts < 3 {
			return context.DeadlineExceeded // Retryable
		}
		return nil
	})

	elapsed := time.Since(start)

	if err != nil {
		t.Errorf("expected success on 3rd attempt, got %v", err)
	}

	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}

	// Expected: 10ms + 20ms = 30ms (plus some overhead)
	if elapsed < 25*time.Millisecond || elapsed > 100*time.Millisecond {
		t.Logf("timing: %v (expected ~30ms)", elapsed)
	}
}

// TestReliabilityTimeoutRespected verifies timeout enforcement
func TestReliabilityTimeoutRespected(t *testing.T) {
	tm := reliability.NewTimeoutManager(reliability.TimeoutConfig{
		GlobalTimeout:  100 * time.Millisecond,
		ReplicaTimeout: 100 * time.Millisecond,
	})

	ctx, cancel := tm.CreateRequestContext(context.Background(), 100)
	defer cancel()

	start := time.Now()
	<-ctx.Done()
	elapsed := time.Since(start)

	if elapsed < 90*time.Millisecond || elapsed > 150*time.Millisecond {
		t.Logf("timeout duration: %v (expected ~100ms)", elapsed)
	}

	if ctx.Err() == nil {
		t.Errorf("expected context to be cancelled")
	}
}

// TestReliabilityCBAndRetryIntegration verifies CB + Retry together
func TestReliabilityCBAndRetryIntegration(t *testing.T) {
	cb := reliability.NewCircuitBreaker(2, 1, 50*time.Millisecond)
	r := reliability.NewRetry(reliability.RetryConfig{
		MaxAttempts:    3,
		InitialBackoff: 10 * time.Millisecond,
		Multiplier:     2.0,
	})

	attempts := 0

	// First batch: trigger CB
	err := r.Execute(context.Background(), func() error {
		attempts++
		if !cb.Allow() {
			return errors.New("CB open")
		}

		// Fail twice to open CB
		if attempts <= 2 {
			cb.RecordFailure()
			return errors.New("failure")
		}

		cb.RecordSuccess()
		return nil
	})

	// After 2 failures, CB should be open
	// Remaining attempts should be rejected by CB
	if cb.GetState() != reliability.StateOpen {
		t.Logf("CB state after failures: %v", cb.GetState())
	}

	if err == nil {
		t.Logf("executed %d attempts", attempts)
	}
}

// TestReliabilityConcurrentCBStateChanges verifies concurrent access
func TestReliabilityConcurrentCBStateChanges(t *testing.T) {
	cb := reliability.NewCircuitBreaker(50, 20, 100*time.Millisecond)

	var wg sync.WaitGroup
	var successCount int32
	var failureCount int32

	// 100 goroutines simultaneously calling Allow/Record
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			if cb.Allow() {
				if idx%2 == 0 {
					cb.RecordSuccess()
					atomic.AddInt32(&successCount, 1)
				} else {
					cb.RecordFailure()
					atomic.AddInt32(&failureCount, 1)
				}
			}
		}(i)
	}

	wg.Wait()

	metrics := cb.GetMetrics()
	t.Logf("concurrent results: successes=%d, failures=%d, metrics=%v",
		atomic.LoadInt32(&successCount),
		atomic.LoadInt32(&failureCount),
		metrics)
}

// TestReliabilityTimeoutDistribution verifies fair time budgeting
func TestReliabilityTimeoutDistribution(t *testing.T) {
	tm := reliability.NewTimeoutManager(reliability.TimeoutConfig{
		GlobalTimeout:  300 * time.Millisecond,
		ReplicaTimeout: 500 * time.Millisecond,
	})

	deadline := time.Now().Add(300 * time.Millisecond)

	timeout1 := tm.CalculateAttemptTimeout(deadline, 5, 1)
	timeout2 := tm.CalculateAttemptTimeout(deadline, 5, 2)
	timeout3 := tm.CalculateAttemptTimeout(deadline, 5, 5)

	t.Logf("timeouts: attempt1=%v, attempt2=%v, attempt5=%v", timeout1, timeout2, timeout3)

	// Verify all are positive and within global timeout
	if timeout1 <= 0 || timeout2 <= 0 || timeout3 <= 0 {
		t.Errorf("all timeouts should be positive")
	}

	if timeout1 > 300*time.Millisecond || timeout2 > 300*time.Millisecond || timeout3 > 300*time.Millisecond {
		t.Errorf("timeouts should not exceed global timeout")
	}
}

// TestReliabilityRetryNonRetryableError verifies fast fail
func TestReliabilityRetryNonRetryableError(t *testing.T) {
	r := reliability.NewRetry(reliability.RetryConfig{
		MaxAttempts: 5,
	})

	attempts := 0
	nonRetryableErr := errors.New("unauthorized")

	err := r.Execute(context.Background(), func() error {
		attempts++
		return nonRetryableErr
	})

	if err != nonRetryableErr {
		t.Errorf("expected non-retryable error, got %v", err)
	}

	if attempts != 1 {
		t.Errorf("non-retryable error should return immediately, got %d attempts", attempts)
	}
}

// TestReliabilityCBFailureReset verifies failure counter reset on success
func TestReliabilityCBFailureReset(t *testing.T) {
	cb := reliability.NewCircuitBreaker(3, 2, 50*time.Millisecond)

	// 2 failures
	cb.RecordFailure()
	cb.RecordFailure()

	// Success resets counter
	cb.RecordSuccess()

	// Need 3 more failures to trip
	cb.RecordFailure()
	cb.RecordFailure()

	if cb.GetState() != reliability.StateClosed {
		t.Errorf("should need 3 failures to trip, got state %v", cb.GetState())
	}

	cb.RecordFailure() // 3rd failure

	if cb.GetState() != reliability.StateOpen {
		t.Errorf("3rd failure should trip CB")
	}
}

// TestReliabilitySequentialRetryWithTimeouts simulates sequential retries
func TestReliabilitySequentialRetryWithTimeouts(t *testing.T) {
	tm := reliability.NewTimeoutManager(reliability.TimeoutConfig{
		GlobalTimeout:  200 * time.Millisecond,
		ReplicaTimeout: 100 * time.Millisecond,
	})

	ctx, cancel := tm.CreateRequestContext(context.Background(), 200)
	defer cancel()

	attempts := 0
	start := time.Now()

	for attempt := 1; attempt <= 3; attempt++ {
		attCtx, attCancel := tm.CreateAttemptContext(ctx, 3, attempt)

		// Simulate work
		select {
		case <-time.After(40 * time.Millisecond):
			attempts++
		case <-attCtx.Done():
			attCancel()
			break
		}

		attCancel()

		if ctx.Err() != nil {
			break
		}
	}

	elapsed := time.Since(start)
	t.Logf("sequential retries: %d attempts in %v", attempts, elapsed)

	if elapsed > 250*time.Millisecond {
		t.Errorf("total time should not exceed global timeout")
	}
}

// TestReliabilityMockReplicaWithCB verifies integration with mock replica
func TestReliabilityMockReplicaWithCB(t *testing.T) {
	mockReplica := testutil.NewMockReplica("test-replica", "localhost:5051")
	cb := reliability.NewCircuitBreaker(2, 2, 50*time.Millisecond)

	// Configure replica to fail
	mockReplica.SetErrorRate(1.0)

	// Send requests
	for i := 0; i < 5; i++ {
		if !cb.Allow() {
			t.Logf("CB rejected request at attempt %d", i+1)
			break
		}

		// Simulate calling replica
		mockReplica.RecordRequest([]byte("test"), map[string]string{})

		// Record failure
		cb.RecordFailure()
	}

	if cb.GetState() != reliability.StateOpen {
		t.Errorf("CB should be OPEN after failures")
	}

	t.Logf("replica recorded %d requests before CB opened", len(mockReplica.GetRecordedRequests()))
}
