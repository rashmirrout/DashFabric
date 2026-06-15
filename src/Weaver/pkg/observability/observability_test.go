package observability

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// TestMetricsCollectorRecordRequest verifies request recording
func TestMetricsCollectorRecordRequest(t *testing.T) {
	mc := NewMetricsCollector("test_request")

	mc.RecordRequest("GET", "replica-1", "200", 10*time.Millisecond)
	mc.RecordRequest("POST", "replica-2", "201", 50*time.Millisecond)

	counters := mc.GetCounters()
	if counters["requests"] != 2 {
		t.Errorf("expected 2 requests, got %d", counters["requests"])
	}
}

// TestMetricsCollectorRecordError verifies error recording
func TestMetricsCollectorRecordError(t *testing.T) {
	mc := NewMetricsCollector("test_error")

	mc.RecordError("connection_refused", "replica-1")
	mc.RecordError("timeout", "replica-2")
	mc.RecordError("connection_refused", "replica-1")

	counters := mc.GetCounters()
	if counters["errors"] != 3 {
		t.Errorf("expected 3 errors, got %d", counters["errors"])
	}
}

// TestMetricsCollectorRecordRetry verifies retry recording
func TestMetricsCollectorRecordRetry(t *testing.T) {
	mc := NewMetricsCollector("test_retry")

	mc.RecordRetry(1, "connection_refused")
	mc.RecordRetry(2, "timeout")
	mc.RecordRetry(3, "timeout")

	counters := mc.GetCounters()
	if counters["retries"] != 3 {
		t.Errorf("expected 3 retries, got %d", counters["retries"])
	}
}

// TestMetricsCollectorRecordTimeout verifies timeout recording
func TestMetricsCollectorRecordTimeout(t *testing.T) {
	mc := NewMetricsCollector("test_timeout")

	mc.RecordTimeout("global")
	mc.RecordTimeout("replica")
	mc.RecordTimeout("global")

	counters := mc.GetCounters()
	if counters["timeouts"] != 3 {
		t.Errorf("expected 3 timeouts, got %d", counters["timeouts"])
	}
}

// TestMetricsCollectorUpdateReplicaHealth verifies health updates
func TestMetricsCollectorUpdateReplicaHealth(t *testing.T) {
	mc := NewMetricsCollector("test_health")

	mc.UpdateReplicaHealth("replica-1", true)
	mc.UpdateReplicaHealth("replica-2", false)
	mc.UpdateReplicaHealth("replica-1", false)

	// Verify no panics during health updates
}

// TestMetricsCollectorCircuitBreakerState verifies CB state recording
func TestMetricsCollectorCircuitBreakerState(t *testing.T) {
	mc := NewMetricsCollector("test_cbstate")

	mc.RecordCircuitBreakerState("replica-1", 0) // CLOSED
	mc.RecordCircuitBreakerState("replica-1", 1) // OPEN
	mc.RecordCircuitBreakerState("replica-1", 2) // HALF_OPEN

	// Verify no panics
}

// TestMetricsCollectorCircuitBreakerTrip verifies CB trip recording
func TestMetricsCollectorCircuitBreakerTrip(t *testing.T) {
	mc := NewMetricsCollector("test_cbtrip")

	mc.RecordCircuitBreakerTrip("replica-1", "failure_threshold")
	mc.RecordCircuitBreakerTrip("replica-2", "failure_threshold")
	mc.RecordCircuitBreakerTrip("replica-1", "failure_threshold")

	counters := mc.GetCounters()
	if counters["circuit_breaker_trips"] != 3 {
		t.Errorf("expected 3 CB trips, got %d", counters["circuit_breaker_trips"])
	}
}

// TestMetricsCollectorRateLimitExceeded verifies rate limit recording
func TestMetricsCollectorRateLimitExceeded(t *testing.T) {
	mc := NewMetricsCollector("test_ratelimit")

	mc.RecordRateLimitExceeded("global")
	mc.RecordRateLimitExceeded("per_client")
	mc.RecordRateLimitExceeded("global")

	counters := mc.GetCounters()
	if counters["rate_limit_exceeded"] != 3 {
		t.Errorf("expected 3 rate limit exceeded, got %d", counters["rate_limit_exceeded"])
	}
}

// TestMetricsCollectorConcurrentRecording verifies concurrent access
func TestMetricsCollectorConcurrentRecording(t *testing.T) {
	mc := NewMetricsCollector("test_concurrent")

	// Simulate concurrent calls
	for i := 0; i < 100; i++ {
		go func(idx int) {
			if idx%2 == 0 {
				mc.RecordRequest("GET", "replica-1", "200", 10*time.Millisecond)
			} else {
				mc.RecordError("timeout", "replica-1")
			}
		}(i)
	}

	time.Sleep(100 * time.Millisecond) // Wait for goroutines

	counters := mc.GetCounters()
	total := counters["requests"] + counters["errors"]

	if total != 100 {
		t.Errorf("expected 100 total operations, got %d", total)
	}
}

// TestStructuredLoggerCreation verifies logger creation
func TestStructuredLoggerCreation(t *testing.T) {
	logger, err := NewStructuredLogger("test-service-creation", true)
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}

	defer logger.Sync()

	if logger.logger == nil {
		t.Errorf("logger should not be nil")
	}
}

// TestStructuredLoggerRequestStarted verifies request logging
func TestStructuredLoggerRequestStarted(t *testing.T) {
	logger, err := NewStructuredLogger("test-service-reqstart", true)
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}
	defer logger.Sync()

	ctx := context.Background()
	logger.LogRequestStarted(ctx, "req-123", "GET", "192.168.1.1")

	// Verify no panics
}

// TestStructuredLoggerRequestCompleted verifies request completion logging
func TestStructuredLoggerRequestCompleted(t *testing.T) {
	logger, err := NewStructuredLogger("test-service-reqcomplete", true)
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}
	defer logger.Sync()

	ctx := context.Background()
	logger.LogRequestCompleted(ctx, "req-123", 200, 100*time.Millisecond, nil)

	// Verify no panics
}

// TestStructuredLoggerReplicaStatusChanged verifies replica logging
func TestStructuredLoggerReplicaStatusChanged(t *testing.T) {
	logger, err := NewStructuredLogger("test-service-replica", true)
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}
	defer logger.Sync()

	ctx := context.Background()
	logger.LogReplicaStatusChanged(ctx, "replica-1", "HEALTHY", "UNHEALTHY")

	// Verify no panics
}

// TestStructuredLoggerCircuitBreakerStateChanged verifies CB logging
func TestStructuredLoggerCircuitBreakerStateChanged(t *testing.T) {
	logger, err := NewStructuredLogger("test-service-cb", true)
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}
	defer logger.Sync()

	ctx := context.Background()
	logger.LogCircuitBreakerStateChanged(ctx, "replica-1", "CLOSED", "OPEN")

	// Verify no panics
}

// TestStructuredLoggerRetryAttempt verifies retry logging
func TestStructuredLoggerRetryAttempt(t *testing.T) {
	logger, err := NewStructuredLogger("test-service-retry", true)
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}
	defer logger.Sync()

	ctx := context.Background()
	logger.LogRetryAttempt(ctx, "req-123", 1, "connection_refused")

	// Verify no panics
}

// TestStructuredLoggerTimeoutOccurred verifies timeout logging
func TestStructuredLoggerTimeoutOccurred(t *testing.T) {
	logger, err := NewStructuredLogger("test-service-timeout", true)
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}
	defer logger.Sync()

	ctx := context.Background()
	logger.LogTimeoutOccurred(ctx, "req-123", "global", 15*time.Second)

	// Verify no panics
}

// TestStructuredLoggerError verifies error logging
func TestStructuredLoggerError(t *testing.T) {
	logger, err := NewStructuredLogger("test-service-error", true)
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}
	defer logger.Sync()

	ctx := context.Background()
	testErr := fmt.Errorf("replica unavailable")
	logger.LogError(ctx, "req-123", "replica_error", testErr)

	// Verify no panics
}

// TestExtractTraceID verifies trace ID extraction
func TestExtractTraceID(t *testing.T) {
	ctx := context.Background()

	// Without trace ID
	traceID1 := extractTraceID(ctx)
	if traceID1 == "" {
		t.Errorf("trace ID should be generated")
	}

	// With trace ID
	ctx2 := ContextWithTraceID(ctx, "custom-trace-123")
	traceID2 := extractTraceID(ctx2)

	if traceID2 != "custom-trace-123" {
		t.Errorf("expected custom-trace-123, got %s", traceID2)
	}
}
