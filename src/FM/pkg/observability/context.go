package observability

import (
	"context"
	"fmt"
	"math/rand"
)

const (
	traceIDKey = "trace_id"
)

// WithTraceID adds a trace ID to the context
func WithTraceID(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, traceIDKey, traceID)
}

// TraceIDFromContext extracts trace ID from context, or generates one if not present
func TraceIDFromContext(ctx context.Context) string {
	if traceID, ok := ctx.Value(traceIDKey).(string); ok && traceID != "" {
		return traceID
	}
	return generateTraceID()
}

// generateTraceID creates a random hex trace ID (16 chars)
func generateTraceID() string {
	return fmt.Sprintf("%016x", rand.Int63())
}
