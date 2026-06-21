package goalstatemanagement

import (
	"context"

	obs "github.com/dashfabric/fm/pkg/observability"
	"go.opentelemetry.io/otel/trace"
)

// tracingContext is the package-level tracing context
var tracingContext *obs.TracingContext

// SetTracingContext sets the package-level tracing context for GM module
func SetTracingContext(ctx *obs.TracingContext) {
	if ctx != nil {
		tracingContext = ctx
	}
}

// startSpan creates a new span for tracing
func startSpan(ctx context.Context, name string) (context.Context, trace.Span) {
	if tracingContext != nil {
		return tracingContext.StartSpan(ctx, name)
	}
	// Return no-op span if tracing not initialized
	return ctx, trace.SpanFromContext(ctx)
}
