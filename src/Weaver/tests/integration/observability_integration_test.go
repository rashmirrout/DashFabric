package integration

import (
	"context"
	"testing"
	"time"

	"github.com/dashfabric/weaver/pkg/observability"
	"github.com/dashfabric/weaver/pkg/reliability"
)

// TestObservabilityMetricsCollection verifies metrics are recorded correctly
func TestObservabilityMetricsCollection(t *testing.T) {
	mc := observability.NewMetricsCollector("test_obs_integration")

	// Simulate request flow with metrics
	mc.RecordRequest("GET", "replica-1", "200", 10*time.Millisecond)
	mc.RecordRequest("GET", "replica-2", "200", 15*time.Millisecond)
	mc.RecordRequest("POST", "replica-1", "201", 50*time.Millisecond)

	// Record some errors
	mc.RecordError("timeout", "replica-1")
	mc.RecordError("connection_refused", "replica-2")

	// Record retries
	mc.RecordRetry(1, "connection_refused")
	mc.RecordRetry(2, "timeout")

	// Record timeouts
	mc.RecordTimeout("global")
	mc.RecordTimeout("replica")

	// Record CB trips
	mc.RecordCircuitBreakerTrip("replica-1", "failure_threshold")

	counters := mc.GetCounters()

	if counters["requests"] != 3 {
		t.Errorf("expected 3 requests, got %d", counters["requests"])
	}

	if counters["errors"] != 2 {
		t.Errorf("expected 2 errors, got %d", counters["errors"])
	}

	if counters["retries"] != 2 {
		t.Errorf("expected 2 retries, got %d", counters["retries"])
	}

	if counters["timeouts"] != 2 {
		t.Errorf("expected 2 timeouts, got %d", counters["timeouts"])
	}

	if counters["circuit_breaker_trips"] != 1 {
		t.Errorf("expected 1 CB trip, got %d", counters["circuit_breaker_trips"])
	}
}

// TestObservabilityLoggingWithReliability verifies logging integrates with reliability
func TestObservabilityLoggingWithReliability(t *testing.T) {
	logger, err := observability.NewStructuredLogger("test_obs_reliability", true)
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}
	defer logger.Sync()

	ctx := context.Background()

	// Simulate request lifecycle with logging
	logger.LogRequestStarted(ctx, "req-123", "GET", "10.0.0.1")

	// Simulate CB opening
	logger.LogCircuitBreakerStateChanged(ctx, "replica-1", "CLOSED", "OPEN")

	// Simulate retries
	logger.LogRetryAttempt(ctx, "req-123", 1, "connection_refused")
	logger.LogRetryAttempt(ctx, "req-123", 2, "timeout")

	// Simulate timeout
	logger.LogTimeoutOccurred(ctx, "req-123", "global", 15*time.Second)

	// Log completion
	logger.LogRequestCompleted(ctx, "req-123", 500, 150*time.Millisecond,
		observableError("request timeout after retries"))

	// Verify no panics - structured logging should work with reliability patterns
}

// TestObservabilityWithCircuitBreaker verifies metrics during CB transitions
func TestObservabilityWithCircuitBreaker(t *testing.T) {
	mc := observability.NewMetricsCollector("test_obs_cb")
	cb := reliability.NewCircuitBreaker(3, 2, 50*time.Millisecond)

	// Simulate CB failure scenario
	for i := 0; i < 3; i++ {
		if cb.Allow() {
			mc.RecordRequest("GET", "replica-1", "200", 10*time.Millisecond)
			cb.RecordFailure()
			mc.RecordError("timeout", "replica-1")
		}
	}

	// Verify CB is now open
	if cb.GetState() != reliability.StateOpen {
		t.Errorf("expected CB state=OPEN")
	}

	// Record CB trip
	mc.RecordCircuitBreakerTrip("replica-1", "failure_threshold")

	counters := mc.GetCounters()

	if counters["errors"] != 3 {
		t.Errorf("expected 3 errors recorded, got %d", counters["errors"])
	}

	if counters["circuit_breaker_trips"] != 1 {
		t.Errorf("expected 1 CB trip recorded, got %d", counters["circuit_breaker_trips"])
	}
}

// TestObservabilityWithRetry verifies metrics during retries
func TestObservabilityWithRetry(t *testing.T) {
	mc := observability.NewMetricsCollector("test_obs_retry")
	r := reliability.NewRetry(reliability.RetryConfig{
		MaxAttempts:    3,
		InitialBackoff: 10 * time.Millisecond,
		Multiplier:     2.0,
	})

	logger, err := observability.NewStructuredLogger("test_obs_retry_log", true)
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}
	defer logger.Sync()

	ctx := context.Background()

	attempts := 0
	err = r.Execute(ctx, func() error {
		attempts++
		mc.RecordRetry(attempts, "connection_refused")
		logger.LogRetryAttempt(ctx, "req-456", attempts, "connection_refused")

		if attempts < 3 {
			return context.DeadlineExceeded
		}
		return nil
	})

	if err != nil {
		t.Errorf("expected success after retries, got %v", err)
	}

	counters := mc.GetCounters()
	if counters["retries"] != 3 {
		t.Errorf("expected 3 retry attempts recorded, got %d", counters["retries"])
	}
}

// TestObservabilityWithTimeout verifies metrics during timeout scenarios
func TestObservabilityWithTimeout(t *testing.T) {
	mc := observability.NewMetricsCollector("test_obs_timeout")
	logger, err := observability.NewStructuredLogger("test_obs_timeout_log", true)
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}
	defer logger.Sync()

	tm := reliability.NewTimeoutManager(reliability.TimeoutConfig{
		GlobalTimeout: 100 * time.Millisecond,
	})

	ctx := context.Background()
	ctx, cancel := tm.CreateRequestContext(ctx, 100)
	defer cancel()

	// Wait for timeout
	<-ctx.Done()

	// Record timeout metric and log
	mc.RecordTimeout("global")
	logger.LogTimeoutOccurred(ctx, "req-789", "global", 100*time.Millisecond)

	counters := mc.GetCounters()
	if counters["timeouts"] != 1 {
		t.Errorf("expected 1 timeout recorded, got %d", counters["timeouts"])
	}
}

// TestObservabilityRateLimitMetrics verifies rate limit recording
func TestObservabilityRateLimitMetrics(t *testing.T) {
	mc := observability.NewMetricsCollector("test_obs_ratelimit")

	// Simulate rate limit exceeded for different dimensions
	for i := 0; i < 5; i++ {
		mc.RecordRateLimitExceeded("global")
	}

	for i := 0; i < 3; i++ {
		mc.RecordRateLimitExceeded("per_client")
	}

	for i := 0; i < 2; i++ {
		mc.RecordRateLimitExceeded("per_ip")
	}

	counters := mc.GetCounters()
	if counters["rate_limit_exceeded"] != 10 {
		t.Errorf("expected 10 rate limit exceeded events, got %d", counters["rate_limit_exceeded"])
	}
}

// TestObservabilityConcurrentMetricsLogging verifies concurrent access
func TestObservabilityConcurrentMetricsLogging(t *testing.T) {
	mc := observability.NewMetricsCollector("test_obs_concurrent")
	logger, err := observability.NewStructuredLogger("test_obs_concurrent_log", true)
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}
	defer logger.Sync()

	ctx := context.Background()

	// Spawn 50 goroutines recording metrics and logs
	for i := 0; i < 50; i++ {
		go func(idx int) {
			if idx%3 == 0 {
				mc.RecordRequest("GET", "replica-1", "200", 10*time.Millisecond)
			} else if idx%3 == 1 {
				mc.RecordError("timeout", "replica-1")
				logger.LogError(ctx, "req-concurrent", "timeout", observableError("request timeout"))
			} else {
				mc.RecordRetry(1, "connection_refused")
				logger.LogRetryAttempt(ctx, "req-concurrent", 1, "connection_refused")
			}
		}(i)
	}

	time.Sleep(100 * time.Millisecond)

	counters := mc.GetCounters()
	total := counters["requests"] + counters["errors"] + counters["retries"]

	if total != 50 {
		t.Errorf("expected 50 total operations, got %d", total)
	}
}

// Helper function for test errors
func observableError(msg string) error {
	return &testError{message: msg}
}

type testError struct {
	message string
}

func (e *testError) Error() string {
	return e.message
}
