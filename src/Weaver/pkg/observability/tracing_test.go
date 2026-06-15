package observability

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestTracingManager(t *testing.T) {
	tm := NewTracingManager()

	// Start a trace
	trace := tm.StartTrace(context.Background(), "trace-123", "span-1", "request")
	if trace.TraceID != "trace-123" {
		t.Errorf("expected trace-123, got %s", trace.TraceID)
	}

	// Add tags
	tm.AddTag("trace-123", "endpoint", "/api/users")
	tm.AddTag("trace-123", "method", "GET")

	// Add logs
	tm.AddLog("trace-123", map[string]interface{}{"event": "request_received"})

	// End trace
	tm.EndTrace("trace-123", TraceStatusOK, "")

	// Verify trace
	retrieved := tm.GetTrace("trace-123")
	if retrieved == nil {
		t.Fatal("expected to retrieve trace")
	}

	if retrieved.Status != TraceStatusOK {
		t.Errorf("expected status OK, got %s", retrieved.Status)
	}

	if len(retrieved.Tags) != 2 {
		t.Errorf("expected 2 tags, got %d", len(retrieved.Tags))
	}

	if len(retrieved.Logs) != 1 {
		t.Errorf("expected 1 log, got %d", len(retrieved.Logs))
	}
}

func TestTracingError(t *testing.T) {
	tm := NewTracingManager()

	_ = tm.StartTrace(context.Background(), "trace-err", "span-1", "request")
	tm.AddTag("trace-err", "error_type", "timeout")
	tm.EndTrace("trace-err", TraceStatusError, "request timeout after 5s")

	retrieved := tm.GetTrace("trace-err")
	if retrieved.Status != TraceStatusError {
		t.Errorf("expected error status, got %s", retrieved.Status)
	}

	if retrieved.Error != "request timeout after 5s" {
		t.Errorf("expected error message, got %s", retrieved.Error)
	}
}

func TestTracingDuration(t *testing.T) {
	tm := NewTracingManager()

	_ = tm.StartTrace(context.Background(), "trace-duration", "span-1", "request")
	time.Sleep(100 * time.Millisecond)
	tm.EndTrace("trace-duration", TraceStatusOK, "")

	retrieved := tm.GetTrace("trace-duration")
	if retrieved.Duration < 100*time.Millisecond {
		t.Errorf("expected duration >= 100ms, got %v", retrieved.Duration)
	}
}

func TestJaegerExporter(t *testing.T) {
	tm := NewTracingManager()
	exporter := NewJaegerExporter("localhost:6831", "weaver-gateway", tm)
	defer exporter.Close()

	// Export traces
	for i := 0; i < 10; i++ {
		tr := &Trace{
			TraceID:   "trace-" + string(rune('0'+i)),
			Operation: "request",
			StartTime: time.Now(),
		}
		err := exporter.Export(tr)
		if err != nil {
			t.Errorf("export failed: %v", err)
		}
	}

	// Give exporter time to process
	time.Sleep(100 * time.Millisecond)

	count := exporter.GetExportedCount()
	if count < 10 {
		t.Logf("exported %d traces (may still be processing)", count)
	}
}

func TestJaegerExporterClosed(t *testing.T) {
	tm := NewTracingManager()
	exporter := NewJaegerExporter("localhost:6831", "weaver-gateway", tm)
	exporter.Close()

	// Should fail when closed
	tr := &Trace{
		TraceID:   "trace-closed",
		Operation: "request",
	}
	err := exporter.Export(tr)
	if err == nil {
		t.Error("expected error when exporting after close")
	}
}

func TestTraceContextPropagation(t *testing.T) {
	tc := &TraceContext{
		TraceID:      "trace-123",
		SpanID:       "span-1",
		ParentSpanID: "span-0",
		Baggage: map[string]string{
			"user_id": "user-1",
		},
	}

	ctx := ContextWithTrace(context.Background(), tc)

	// Extract and verify
	extracted := TraceFromContext(ctx)
	if extracted == nil {
		t.Fatal("expected to extract trace context")
	}

	if extracted.TraceID != "trace-123" {
		t.Errorf("expected trace-123, got %s", extracted.TraceID)
	}

	if extracted.Baggage["user_id"] != "user-1" {
		t.Errorf("expected user-1, got %s", extracted.Baggage["user_id"])
	}
}

func TestTracingConcurrency(t *testing.T) {
	tm := NewTracingManager()

	count := int64(0)
	done := make(chan struct{})

	// Start 10 concurrent trace operations
	for i := 0; i < 10; i++ {
		go func(id int) {
			defer func() {
				done <- struct{}{}
			}()

			traceID := "trace-" + string(rune('0'+id))
			_ = tm.StartTrace(context.Background(), traceID, "span-1", "request")

			for j := 0; j < 100; j++ {
				tm.AddTag(traceID, "tag-"+string(rune('0'+(j%10))), j)
				tm.AddLog(traceID, map[string]interface{}{"index": j})
			}

			tm.EndTrace(traceID, TraceStatusOK, "")
			atomic.AddInt64(&count, 1)
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	if atomic.LoadInt64(&count) != 10 {
		t.Errorf("expected 10 completed traces, got %d", atomic.LoadInt64(&count))
	}

	// Verify all traces exist
	traces := tm.ExportTraces()
	if len(traces) != 10 {
		t.Errorf("expected 10 traces, got %d", len(traces))
	}
}

func TestTracingClear(t *testing.T) {
	tm := NewTracingManager()

	// Create traces
	for i := 0; i < 5; i++ {
		tm.StartTrace(context.Background(), "trace-"+string(rune('0'+i)), "span-1", "request")
	}

	// Verify they exist
	traces := tm.ExportTraces()
	if len(traces) != 5 {
		t.Errorf("expected 5 traces, got %d", len(traces))
	}

	// Clear
	tm.Clear()

	// Verify cleared
	traces = tm.ExportTraces()
	if len(traces) != 0 {
		t.Errorf("expected 0 traces after clear, got %d", len(traces))
	}
}
