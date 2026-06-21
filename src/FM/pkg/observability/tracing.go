package observability

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
	"go.opentelemetry.io/otel/trace"
)

// TracingContext holds the OpenTelemetry tracer provider and tracer
type TracingContext struct {
	TracerProvider *sdktrace.TracerProvider
	Tracer         trace.Tracer
}

// NewTracingContext initializes OpenTelemetry with OTLP/Jaeger exporter
func NewTracingContext(jaegerEndpoint string, serviceName string) (*TracingContext, error) {
	// Create OTLP/HTTP exporter (compatible with Jaeger)
	exporter, err := otlptracehttp.New(
		context.Background(),
		otlptracehttp.WithEndpoint(jaegerEndpoint),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTLP exporter: %w", err)
	}

	// Create resource with service name
	res, err := resource.New(context.Background(),
		resource.WithAttributes(
			semconv.ServiceName(serviceName),
			semconv.ServiceVersion("1.0.0"),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// Create tracer provider with OTLP exporter
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()), // 100% sampling for dev
	)

	// Set global tracer provider
	otel.SetTracerProvider(tp)

	// Get tracer from provider
	tracer := tp.Tracer("github.com/dashfabric/fm")

	return &TracingContext{
		TracerProvider: tp,
		Tracer:         tracer,
	}, nil
}

// StartSpan creates a new span in the tracer
func (tc *TracingContext) StartSpan(ctx context.Context, name string) (context.Context, trace.Span) {
	return tc.Tracer.Start(ctx, name)
}

// Shutdown gracefully shuts down the tracer provider
func (tc *TracingContext) Shutdown(ctx context.Context) error {
	return tc.TracerProvider.Shutdown(ctx)
}
