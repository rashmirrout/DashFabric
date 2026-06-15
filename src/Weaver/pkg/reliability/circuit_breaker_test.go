package reliability

import (
	"sync"
	"testing"
	"time"
)

// TestCircuitBreakerNewDefaults verifies default values
func TestCircuitBreakerNewDefaults(t *testing.T) {
	cb := NewCircuitBreaker(0, 0, 0)

	if cb.failureThreshold != 5 {
		t.Errorf("expected failureThreshold=5, got %d", cb.failureThreshold)
	}

	if cb.successThreshold != 2 {
		t.Errorf("expected successThreshold=2, got %d", cb.successThreshold)
	}

	if cb.timeout != 30*time.Second {
		t.Errorf("expected timeout=30s, got %v", cb.timeout)
	}

	if cb.GetState() != StateClosed {
		t.Errorf("expected initial state=CLOSED, got %v", cb.GetState())
	}
}

// TestCircuitBreakerClosedAllowsRequests verifies CLOSED state allows requests
func TestCircuitBreakerClosedAllowsRequests(t *testing.T) {
	cb := NewCircuitBreaker(3, 2, 30*time.Second)

	for i := 0; i < 100; i++ {
		if !cb.Allow() {
			t.Errorf("CLOSED state should allow request at iteration %d", i)
		}
	}

	if cb.GetState() != StateClosed {
		t.Errorf("state should remain CLOSED, got %v", cb.GetState())
	}
}

// TestCircuitBreakerClosedToOpen verifies CLOSED->OPEN transition
func TestCircuitBreakerClosedToOpen(t *testing.T) {
	cb := NewCircuitBreaker(3, 2, 30*time.Second)

	// Record 2 failures (not threshold yet)
	cb.RecordFailure()
	cb.RecordFailure()

	if cb.GetState() != StateClosed {
		t.Errorf("state should remain CLOSED after 2 failures, got %v", cb.GetState())
	}

	if !cb.Allow() {
		t.Errorf("CLOSED state should allow requests")
	}

	// Third failure triggers transition
	cb.RecordFailure()

	if cb.GetState() != StateOpen {
		t.Errorf("state should be OPEN after 3 failures, got %v", cb.GetState())
	}

	if cb.Allow() {
		t.Errorf("OPEN state should reject requests")
	}
}

// TestCircuitBreakerOpenToHalfOpen verifies OPEN->HALF_OPEN transition after timeout
func TestCircuitBreakerOpenToHalfOpen(t *testing.T) {
	timeout := 100 * time.Millisecond
	cb := NewCircuitBreaker(1, 1, timeout)

	// Trip the breaker
	cb.RecordFailure()

	if cb.GetState() != StateOpen {
		t.Errorf("expected state=OPEN, got %v", cb.GetState())
	}

	if cb.Allow() {
		t.Errorf("OPEN state should reject")
	}

	// Wait for timeout
	time.Sleep(timeout + 10*time.Millisecond)

	// First Allow() after timeout should return true and transition to HALF_OPEN
	if !cb.Allow() {
		t.Errorf("after timeout, Allow() should return true for probe")
	}

	if cb.GetState() != StateHalfOpen {
		t.Errorf("expected state=HALF_OPEN, got %v", cb.GetState())
	}
}

// TestCircuitBreakerHalfOpenToClosedSuccess verifies HALF_OPEN->CLOSED on success
func TestCircuitBreakerHalfOpenToClosedSuccess(t *testing.T) {
	timeout := 50 * time.Millisecond
	cb := NewCircuitBreaker(1, 2, timeout)

	// Trip and wait for HALF_OPEN
	cb.RecordFailure()
	time.Sleep(timeout + 10*time.Millisecond)
	cb.Allow() // Transition to HALF_OPEN

	// Record successes (threshold = 2)
	cb.RecordSuccess()
	if cb.GetState() != StateHalfOpen {
		t.Errorf("after 1 success, state should remain HALF_OPEN, got %v", cb.GetState())
	}

	cb.RecordSuccess()
	if cb.GetState() != StateClosed {
		t.Errorf("after 2 successes (threshold), state should be CLOSED, got %v", cb.GetState())
	}

	if !cb.Allow() {
		t.Errorf("CLOSED state should allow requests")
	}
}

// TestCircuitBreakerHalfOpenToOpenFailure verifies HALF_OPEN->OPEN on failure
func TestCircuitBreakerHalfOpenToOpenFailure(t *testing.T) {
	timeout := 50 * time.Millisecond
	cb := NewCircuitBreaker(1, 2, timeout)

	// Trip and wait for HALF_OPEN
	cb.RecordFailure()
	time.Sleep(timeout + 10*time.Millisecond)
	cb.Allow()

	if cb.GetState() != StateHalfOpen {
		t.Errorf("expected state=HALF_OPEN, got %v", cb.GetState())
	}

	// Record failure while in HALF_OPEN
	cb.RecordFailure()

	if cb.GetState() != StateOpen {
		t.Errorf("after failure in HALF_OPEN, state should be OPEN, got %v", cb.GetState())
	}

	if cb.Allow() {
		t.Errorf("OPEN state should reject requests")
	}
}

// TestCircuitBreakerSuccessResetsFailureCounter verifies failure counter reset on success
func TestCircuitBreakerSuccessResetsFailureCounter(t *testing.T) {
	cb := NewCircuitBreaker(3, 2, 30*time.Second)

	cb.RecordFailure()
	cb.RecordFailure()
	cb.RecordSuccess() // Reset counter

	if cb.GetState() != StateClosed {
		t.Errorf("success should reset counter, state should remain CLOSED, got %v", cb.GetState())
	}

	// Now need 3 more failures to trip
	cb.RecordFailure()
	cb.RecordFailure()

	if cb.GetState() != StateClosed {
		t.Errorf("should need 3 failures total, state should remain CLOSED, got %v", cb.GetState())
	}

	cb.RecordFailure()
	if cb.GetState() != StateOpen {
		t.Errorf("3rd failure should trip, state should be OPEN, got %v", cb.GetState())
	}
}

// TestCircuitBreakerMetrics verifies metrics tracking
func TestCircuitBreakerMetrics(t *testing.T) {
	cb := NewCircuitBreaker(2, 1, 30*time.Second)

	cb.RecordFailure()
	cb.RecordFailure()
	cb.RecordSuccess()
	cb.RecordSuccess()

	metrics := cb.GetMetrics()

	if metrics["total_failures"].(int64) != 2 {
		t.Errorf("expected total_failures=2, got %v", metrics["total_failures"])
	}

	if metrics["total_successes"].(int64) != 2 {
		t.Errorf("expected total_successes=2, got %v", metrics["total_successes"])
	}

	if metrics["state_changes"].(int64) < 1 {
		t.Errorf("expected at least 1 state change, got %v", metrics["state_changes"])
	}
}

// TestCircuitBreakerReset verifies reset clears state
func TestCircuitBreakerReset(t *testing.T) {
	cb := NewCircuitBreaker(2, 1, 30*time.Second)

	cb.RecordFailure()
	cb.RecordFailure()

	if cb.GetState() != StateOpen {
		t.Errorf("expected state=OPEN before reset, got %v", cb.GetState())
	}

	cb.Reset()

	if cb.GetState() != StateClosed {
		t.Errorf("expected state=CLOSED after reset, got %v", cb.GetState())
	}

	metrics := cb.GetMetrics()
	if metrics["total_failures"].(int64) != 0 {
		t.Errorf("expected metrics cleared after reset, got %v", metrics)
	}
}

// TestCircuitBreakerConcurrentRecordSuccess verifies concurrent success calls
func TestCircuitBreakerConcurrentRecordSuccess(t *testing.T) {
	cb := NewCircuitBreaker(100, 1, 30*time.Second)

	var wg sync.WaitGroup
	numGoroutines := 100

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cb.RecordSuccess()
		}()
	}

	wg.Wait()

	metrics := cb.GetMetrics()
	if metrics["total_successes"].(int64) != int64(numGoroutines) {
		t.Errorf("expected total_successes=%d, got %v", numGoroutines, metrics["total_successes"])
	}

	if cb.GetState() != StateClosed {
		t.Errorf("state should remain CLOSED, got %v", cb.GetState())
	}
}

// TestCircuitBreakerConcurrentRecordFailure verifies concurrent failure calls
func TestCircuitBreakerConcurrentRecordFailure(t *testing.T) {
	threshold := 50
	cb := NewCircuitBreaker(threshold, 1, 30*time.Second)

	var wg sync.WaitGroup
	numGoroutines := 100

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cb.RecordFailure()
		}()
	}

	wg.Wait()

	metrics := cb.GetMetrics()
	if metrics["total_failures"].(int64) != int64(numGoroutines) {
		t.Errorf("expected total_failures=%d, got %v", numGoroutines, metrics["total_failures"])
	}

	if cb.GetState() != StateOpen {
		t.Errorf("state should be OPEN after failures exceed threshold, got %v", cb.GetState())
	}
}

// TestCircuitBreakerConcurrentAllow verifies concurrent Allow calls
func TestCircuitBreakerConcurrentAllow(t *testing.T) {
	cb := NewCircuitBreaker(5, 2, 30*time.Second)

	var wg sync.WaitGroup
	numGoroutines := 100
	allowCount := 0
	var mu sync.Mutex

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if cb.Allow() {
				mu.Lock()
				allowCount++
				mu.Unlock()
			}
		}()
	}

	wg.Wait()

	if allowCount != numGoroutines {
		t.Errorf("expected %d allows in CLOSED state, got %d", numGoroutines, allowCount)
	}
}

// TestCircuitBreakerConcurrentMixed verifies mixed concurrent operations
func TestCircuitBreakerConcurrentMixed(t *testing.T) {
	cb := NewCircuitBreaker(20, 2, 30*time.Second)

	var wg sync.WaitGroup
	numGoroutines := 100

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			switch idx % 3 {
			case 0:
				cb.RecordSuccess()
			case 1:
				cb.RecordFailure()
			case 2:
				cb.Allow()
			}
		}(i)
	}

	wg.Wait()

	// Verify metrics are consistent (no crashes, no deadlocks)
	metrics := cb.GetMetrics()
	totalOps := metrics["total_successes"].(int64) + metrics["total_failures"].(int64)

	if totalOps != int64(numGoroutines*2/3) {
		t.Logf("concurrent operations completed: successes=%d, failures=%d",
			metrics["total_successes"], metrics["total_failures"])
	}
}

// TestCircuitBreakerStateString verifies state string representation
func TestCircuitBreakerStateString(t *testing.T) {
	tests := []struct {
		state    CircuitBreakerState
		expected string
	}{
		{StateClosed, "CLOSED"},
		{StateOpen, "OPEN"},
		{StateHalfOpen, "HALF_OPEN"},
	}

	for _, tt := range tests {
		if tt.state.String() != tt.expected {
			t.Errorf("expected %s, got %s", tt.expected, tt.state.String())
		}
	}
}

// TestCircuitBreakerPropertyFailureThreshold property: F consecutive failures → OPEN
func TestCircuitBreakerPropertyFailureThreshold(t *testing.T) {
	failureThreshold := 5
	cb := NewCircuitBreaker(failureThreshold, 2, 30*time.Second)

	for i := 0; i < failureThreshold-1; i++ {
		cb.RecordFailure()
		if cb.GetState() != StateClosed {
			t.Errorf("state should remain CLOSED after %d failures (threshold=%d)", i+1, failureThreshold)
		}
	}

	cb.RecordFailure() // Final failure

	if cb.GetState() != StateOpen {
		t.Errorf("state should be OPEN after %d failures", failureThreshold)
	}
}

// TestCircuitBreakerPropertySuccessThreshold property: S consecutive successes in HALF_OPEN → CLOSED
func TestCircuitBreakerPropertySuccessThreshold(t *testing.T) {
	successThreshold := 3
	cb := NewCircuitBreaker(1, successThreshold, 50*time.Millisecond)

	// Trip and transition to HALF_OPEN
	cb.RecordFailure()
	time.Sleep(60 * time.Millisecond)
	cb.Allow()

	if cb.GetState() != StateHalfOpen {
		t.Errorf("expected state=HALF_OPEN")
	}

	for i := 0; i < successThreshold-1; i++ {
		cb.RecordSuccess()
		if cb.GetState() != StateHalfOpen {
			t.Errorf("state should remain HALF_OPEN after %d successes (threshold=%d)", i+1, successThreshold)
		}
	}

	cb.RecordSuccess() // Final success

	if cb.GetState() != StateClosed {
		t.Errorf("state should be CLOSED after %d successes", successThreshold)
	}
}
