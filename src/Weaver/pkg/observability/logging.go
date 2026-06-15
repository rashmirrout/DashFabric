package observability

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"go.uber.org/zap"
)

// StructuredLogger wraps zap logger with structured event logging
type StructuredLogger struct {
	logger *zap.Logger
	sugar  *zap.SugaredLogger
}

// LogEvent represents a structured log event
type LogEvent struct {
	Timestamp   time.Time              `json:"timestamp"`
	EventType   string                 `json:"event_type"`
	TraceID     string                 `json:"trace_id"`
	RequestID   string                 `json:"request_id"`
	Level       string                 `json:"level"`
	Message     string                 `json:"message"`
	Context     map[string]interface{} `json:"context,omitempty"`
	Error       string                 `json:"error,omitempty"`
	Duration    float64                `json:"duration_seconds,omitempty"`
}

// NewStructuredLogger creates a new structured logger
func NewStructuredLogger(serviceName string, development bool) (*StructuredLogger, error) {
	var config zap.Config

	if development {
		config = zap.NewDevelopmentConfig()
	} else {
		config = zap.NewProductionConfig()
	}

	// Configure for JSON output
	config.Encoding = "json"
	config.EncoderConfig.TimeKey = "timestamp"
	config.EncoderConfig.StacktraceKey = "stack"

	logger, err := config.Build()
	if err != nil {
		return nil, err
	}

	logger = logger.With(zap.String("service", serviceName))

	return &StructuredLogger{
		logger: logger,
		sugar:  logger.Sugar(),
	}, nil
}

// LogRequestStarted logs when a request starts
func (sl *StructuredLogger) LogRequestStarted(ctx context.Context, requestID, method, clientIP string) {
	traceID := extractTraceID(ctx)

	event := LogEvent{
		Timestamp: time.Now(),
		EventType: "request_started",
		TraceID:   traceID,
		RequestID: requestID,
		Level:     "info",
		Message:   fmt.Sprintf("Request started: %s from %s", method, clientIP),
		Context: map[string]interface{}{
			"method":    method,
			"client_ip": clientIP,
		},
	}

	sl.logEvent(event)
}

// LogRequestCompleted logs when a request completes
func (sl *StructuredLogger) LogRequestCompleted(ctx context.Context, requestID string, status int, latency time.Duration, err error) {
	traceID := extractTraceID(ctx)

	level := "info"
	if status >= 400 {
		level = "warn"
	}
	if err != nil || status >= 500 {
		level = "error"
	}

	eventType := "request_completed"
	message := fmt.Sprintf("Request completed: status=%d, latency=%v", status, latency)

	event := LogEvent{
		Timestamp: time.Now(),
		EventType: eventType,
		TraceID:   traceID,
		RequestID: requestID,
		Level:     level,
		Message:   message,
		Duration:  latency.Seconds(),
		Context: map[string]interface{}{
			"status": status,
		},
	}

	if err != nil {
		event.Error = err.Error()
	}

	sl.logEvent(event)
}

// LogReplicaStatusChanged logs replica state changes
func (sl *StructuredLogger) LogReplicaStatusChanged(ctx context.Context, replica, oldStatus, newStatus string) {
	traceID := extractTraceID(ctx)

	event := LogEvent{
		Timestamp: time.Now(),
		EventType: "replica_status_changed",
		TraceID:   traceID,
		Level:     "warn",
		Message:   fmt.Sprintf("Replica %s status changed: %s → %s", replica, oldStatus, newStatus),
		Context: map[string]interface{}{
			"replica":    replica,
			"old_status": oldStatus,
			"new_status": newStatus,
		},
	}

	sl.logEvent(event)
}

// LogCircuitBreakerStateChanged logs CB state transitions
func (sl *StructuredLogger) LogCircuitBreakerStateChanged(ctx context.Context, replica, oldState, newState string) {
	traceID := extractTraceID(ctx)

	level := "warn"
	if newState == "OPEN" {
		level = "error"
	}

	event := LogEvent{
		Timestamp: time.Now(),
		EventType: "circuit_breaker_state_changed",
		TraceID:   traceID,
		Level:     level,
		Message:   fmt.Sprintf("Circuit breaker for %s: %s → %s", replica, oldState, newState),
		Context: map[string]interface{}{
			"replica":    replica,
			"old_state":  oldState,
			"new_state":  newState,
		},
	}

	sl.logEvent(event)
}

// LogRetryAttempt logs retry attempts
func (sl *StructuredLogger) LogRetryAttempt(ctx context.Context, requestID string, attempt int, reason string) {
	traceID := extractTraceID(ctx)

	event := LogEvent{
		Timestamp: time.Now(),
		EventType: "retry_attempt",
		TraceID:   traceID,
		RequestID: requestID,
		Level:     "info",
		Message:   fmt.Sprintf("Retry attempt %d: %s", attempt, reason),
		Context: map[string]interface{}{
			"attempt": attempt,
			"reason":  reason,
		},
	}

	sl.logEvent(event)
}

// LogTimeoutOccurred logs timeout events
func (sl *StructuredLogger) LogTimeoutOccurred(ctx context.Context, requestID, timeoutType string, duration time.Duration) {
	traceID := extractTraceID(ctx)

	event := LogEvent{
		Timestamp: time.Now(),
		EventType: "timeout_occurred",
		TraceID:   traceID,
		RequestID: requestID,
		Level:     "error",
		Message:   fmt.Sprintf("Timeout occurred: %s after %v", timeoutType, duration),
		Duration:  duration.Seconds(),
		Context: map[string]interface{}{
			"timeout_type": timeoutType,
		},
	}

	sl.logEvent(event)
}

// LogError logs errors
func (sl *StructuredLogger) LogError(ctx context.Context, requestID, errorType string, err error) {
	traceID := extractTraceID(ctx)

	event := LogEvent{
		Timestamp: time.Now(),
		EventType: "error",
		TraceID:   traceID,
		RequestID: requestID,
		Level:     "error",
		Message:   fmt.Sprintf("Error: %s", errorType),
		Error:     err.Error(),
	}

	sl.logEvent(event)
}

// logEvent writes a structured log event
func (sl *StructuredLogger) logEvent(event LogEvent) {
	// Also output as JSON for log aggregation systems
	data, err := json.Marshal(event)
	if err != nil {
		sl.sugar.Errorw("failed to marshal log event", "error", err)
		return
	}

	fmt.Fprintln(os.Stdout, string(data))
}

// extractTraceID extracts trace ID from context (or generates one)
func extractTraceID(ctx context.Context) string {
	if traceID, ok := ctx.Value("trace_id").(string); ok && traceID != "" {
		return traceID
	}

	// Generate a simple trace ID if not in context
	return fmt.Sprintf("trace-%d", time.Now().UnixNano())
}

// Sync flushes logs
func (sl *StructuredLogger) Sync() error {
	return sl.logger.Sync()
}

// ContextWithTraceID adds a trace ID to context
func ContextWithTraceID(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, "trace_id", traceID)
}
