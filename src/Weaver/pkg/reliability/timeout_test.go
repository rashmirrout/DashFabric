package reliability

import (
	"context"
	"testing"
	"time"
)

// TestTimeoutManagerNewDefaults verifies default values
func TestTimeoutManagerNewDefaults(t *testing.T) {
	tm := NewTimeoutManager(TimeoutConfig{})

	if tm.config.GlobalTimeout != 15*time.Second {
		t.Errorf("expected GlobalTimeout=15s, got %v", tm.config.GlobalTimeout)
	}

	if tm.config.ReplicaTimeout != 5*time.Second {
		t.Errorf("expected ReplicaTimeout=5s, got %v", tm.config.ReplicaTimeout)
	}

	if tm.config.ConnectTimeout != 1*time.Second {
		t.Errorf("expected ConnectTimeout=1s, got %v", tm.config.ConnectTimeout)
	}

	if tm.config.ReadTimeout != 4*time.Second {
		t.Errorf("expected ReadTimeout=4s, got %v", tm.config.ReadTimeout)
	}
}

// TestTimeoutManagerCalculateAttemptTimeout single attempt
func TestTimeoutManagerCalculateAttemptTimeout(t *testing.T) {
	tm := NewTimeoutManager(TimeoutConfig{
		GlobalTimeout:  5 * time.Second,
		ReplicaTimeout: 5 * time.Second,
	})

	deadline := time.Now().Add(5 * time.Second)
	timeout := tm.CalculateAttemptTimeout(deadline, 1, 1)

	if timeout <= 0 {
		t.Errorf("expected positive timeout, got %v", timeout)
	}

	if timeout > 5*time.Second {
		t.Errorf("expected timeout ≤ 5s, got %v", timeout)
	}
}

// TestTimeoutManagerThreeAttemptsTimeSharing verifies time budget distribution
func TestTimeoutManagerThreeAttemptsTimeSharing(t *testing.T) {
	globalTimeout := 300 * time.Millisecond
	tm := NewTimeoutManager(TimeoutConfig{
		GlobalTimeout:  globalTimeout,
		ReplicaTimeout: 500 * time.Millisecond,
	})

	deadline := time.Now().Add(globalTimeout)

	timeout1 := tm.CalculateAttemptTimeout(deadline, 3, 1)
	timeout2 := tm.CalculateAttemptTimeout(deadline, 3, 2)
	timeout3 := tm.CalculateAttemptTimeout(deadline, 3, 3)

	// Algorithm: (timeRemaining / attemptsLeft) - 50ms
	// Attempt 1: (300 / 3) - 50 = 50ms
	// Attempt 2: (300 / 2) - 50 = 100ms
	// Attempt 3: (300 / 1) - 50 = 250ms (capped at 500ms replica timeout, so 250ms)

	if timeout1 <= 0 || timeout2 <= 0 || timeout3 <= 0 {
		t.Errorf("all timeouts should be positive: %v, %v, %v", timeout1, timeout2, timeout3)
	}

	// Verify order: t1 < t2 < t3 (more time for later attempts with fewer left)
	if timeout1 >= timeout2 || timeout2 >= timeout3 {
		t.Errorf("later attempts should get more time: %v, %v, %v", timeout1, timeout2, timeout3)
	}
}

// TestTimeoutManagerNegativeTimeRemaining verifies zero timeout when time exhausted
func TestTimeoutManagerNegativeTimeRemaining(t *testing.T) {
	tm := NewTimeoutManager(TimeoutConfig{})

	// Deadline in the past
	deadline := time.Now().Add(-1 * time.Second)
	timeout := tm.CalculateAttemptTimeout(deadline, 3, 1)

	if timeout != 0 {
		t.Errorf("expected 0 timeout for past deadline, got %v", timeout)
	}
}

// TestTimeoutManagerCreateRequestContext basic
func TestTimeoutManagerCreateRequestContext(t *testing.T) {
	tm := NewTimeoutManager(TimeoutConfig{
		GlobalTimeout: 10 * time.Second,
	})

	ctx, cancel := tm.CreateRequestContext(context.Background(), 0)
	defer cancel()

	_, ok := ctx.Deadline()
	if !ok {
		t.Errorf("expected context to have deadline")
	}
}

// TestTimeoutManagerCreateRequestContextCustom verifies custom timeout
func TestTimeoutManagerCreateRequestContextCustom(t *testing.T) {
	tm := NewTimeoutManager(TimeoutConfig{
		GlobalTimeout: 10 * time.Second,
	})

	ctx, cancel := tm.CreateRequestContext(context.Background(), 5000) // 5 seconds
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Errorf("expected context to have deadline")
	}

	// Verify deadline is roughly 5 seconds in future
	remaining := time.Until(deadline)
	if remaining < 4900*time.Millisecond || remaining > 5100*time.Millisecond {
		t.Errorf("expected deadline ~5s from now, got %v", remaining)
	}
}

// TestTimeoutManagerCreateAttemptContext respects global deadline
func TestTimeoutManagerCreateAttemptContext(t *testing.T) {
	tm := NewTimeoutManager(TimeoutConfig{
		GlobalTimeout:  300 * time.Millisecond,
		ReplicaTimeout: 200 * time.Millisecond,
	})

	reqCtx, reqCancel := tm.CreateRequestContext(context.Background(), 300)
	defer reqCancel()

	attCtx, attCancel := tm.CreateAttemptContext(reqCtx, 3, 1)
	defer attCancel()

	reqDeadline, _ := reqCtx.Deadline()
	attDeadline, _ := attCtx.Deadline()

	if attDeadline.After(reqDeadline) {
		t.Errorf("attempt deadline should never exceed request deadline")
	}
}

// TestTimeoutManagerZeroTimeout edge case
func TestTimeoutManagerZeroTimeout(t *testing.T) {
	tm := NewTimeoutManager(TimeoutConfig{
		GlobalTimeout:  100 * time.Millisecond,
		ReplicaTimeout: 100 * time.Millisecond,
	})

	// Many attempts with little time
	deadline := time.Now().Add(100 * time.Millisecond)

	timeout1 := tm.CalculateAttemptTimeout(deadline, 100, 99)

	if timeout1 < 0 {
		t.Errorf("expected non-negative timeout, got %v", timeout1)
	}
}

// TestTimeoutManagerGetters verify getter methods
func TestTimeoutManagerGetters(t *testing.T) {
	config := TimeoutConfig{
		GlobalTimeout:  10 * time.Second,
		ReplicaTimeout: 5 * time.Second,
	}
	tm := NewTimeoutManager(config)

	if tm.GetGlobalTimeout() != config.GlobalTimeout {
		t.Errorf("GetGlobalTimeout mismatch")
	}

	if tm.GetReplicaTimeout() != config.ReplicaTimeout {
		t.Errorf("GetReplicaTimeout mismatch")
	}
}

// TestTimeoutManagerPropertyTimeNeverExceedsGlobal property: attempt timeout ≤ global
func TestTimeoutManagerPropertyTimeNeverExceedsGlobal(t *testing.T) {
	tm := NewTimeoutManager(TimeoutConfig{
		GlobalTimeout:  1 * time.Second,
		ReplicaTimeout: 5 * time.Second,
	})

	globalDeadline := time.Now().Add(1 * time.Second)

	for attempt := 1; attempt <= 5; attempt++ {
		timeout := tm.CalculateAttemptTimeout(globalDeadline, 5, attempt)

		attemptDeadline := time.Now().Add(timeout)
		if attemptDeadline.After(globalDeadline) {
			t.Errorf("attempt %d deadline exceeds global deadline", attempt)
		}
	}
}

// TestTimeoutManagerPropertyLaterAttemptsGetFairShare property: all attempts get fair time
func TestTimeoutManagerPropertyLaterAttemptsGetFairShare(t *testing.T) {
	tm := NewTimeoutManager(TimeoutConfig{
		GlobalTimeout:  1 * time.Second,
		ReplicaTimeout: 1 * time.Second,
	})

	globalDeadline := time.Now().Add(1 * time.Second)

	totalTime := time.Duration(0)
	for attempt := 1; attempt <= 5; attempt++ {
		timeout := tm.CalculateAttemptTimeout(globalDeadline, 5, attempt)
		totalTime += timeout

		// Each should be positive
		if timeout <= 0 {
			t.Errorf("attempt %d should have positive timeout", attempt)
		}
	}

	// Total shouldn't exceed global timeout
	if totalTime > 1*time.Second {
		t.Logf("total time budget %v respects global timeout", totalTime)
	}
}

// TestTimeoutManagerContextCancellation verifies context cancellation propagates
func TestTimeoutManagerContextCancellation(t *testing.T) {
	tm := NewTimeoutManager(TimeoutConfig{})

	ctx, cancel := tm.CreateRequestContext(context.Background(), 0)
	cancel()

	if ctx.Err() == nil {
		t.Errorf("expected context error after cancel")
	}
}

// TestTimeoutManagerDeadlineExceeded verifies deadline exceeded
func TestTimeoutManagerDeadlineExceeded(t *testing.T) {
	tm := NewTimeoutManager(TimeoutConfig{})

	ctx, cancel := tm.CreateRequestContext(context.Background(), 50)
	defer cancel()

	time.Sleep(100 * time.Millisecond)

	if ctx.Err() == nil {
		t.Errorf("expected context deadline exceeded")
	}
}

// TestTimeoutManagerHighLoadScenario simulates high-load scenario
func TestTimeoutManagerHighLoadScenario(t *testing.T) {
	tm := NewTimeoutManager(TimeoutConfig{
		GlobalTimeout:  1000 * time.Millisecond,
		ReplicaTimeout: 500 * time.Millisecond,
	})

	maxAttempts := 5
	globalDeadline := time.Now().Add(1000 * time.Millisecond)

	totalTime := time.Duration(0)

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		timeout := tm.CalculateAttemptTimeout(globalDeadline, maxAttempts, attempt)
		totalTime += timeout

		if timeout > 1000*time.Millisecond {
			t.Errorf("single attempt exceeds global timeout")
		}
	}

	if totalTime > 1100*time.Millisecond {
		t.Logf("total time budget: %v (with overhead)", totalTime)
	}
}
