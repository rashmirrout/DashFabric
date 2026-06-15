package observability

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Trace represents a distributed trace
type Trace struct {
	TraceID      string
	SpanID       string
	ParentSpanID string
	Operation    string
	StartTime    time.Time
	EndTime      time.Time
	Duration     time.Duration
	Tags         map[string]interface{}
	Logs         []TraceLog
	Status       TraceStatus
	Error        string
}

// TraceStatus represents trace completion status
type TraceStatus string

const (
	TraceStatusOK    TraceStatus = "ok"
	TraceStatusError TraceStatus = "error"
	TraceStatusUnknown TraceStatus = "unknown"
)

// TraceLog represents a log entry within a trace
type TraceLog struct {
	Timestamp time.Time
	Fields    map[string]interface{}
}

// TracingManager manages distributed traces
type TracingManager struct {
	traces map[string]*Trace
	mu     sync.RWMutex
}

// NewTracingManager creates a new tracing manager
func NewTracingManager() *TracingManager {
	return &TracingManager{
		traces: make(map[string]*Trace),
	}
}

// StartTrace begins a new trace
func (tm *TracingManager) StartTrace(ctx context.Context, traceID, spanID, operation string) *Trace {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	trace := &Trace{
		TraceID:   traceID,
		SpanID:    spanID,
		Operation: operation,
		StartTime: time.Now(),
		Tags:      make(map[string]interface{}),
		Logs:      make([]TraceLog, 0),
		Status:    TraceStatusUnknown,
	}

	tm.traces[traceID] = trace
	return trace
}

// EndTrace completes a trace
func (tm *TracingManager) EndTrace(traceID string, status TraceStatus, errMsg string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	trace, ok := tm.traces[traceID]
	if !ok {
		return
	}

	trace.EndTime = time.Now()
	trace.Duration = trace.EndTime.Sub(trace.StartTime)
	trace.Status = status
	trace.Error = errMsg
}

// AddTag adds a tag to a trace
func (tm *TracingManager) AddTag(traceID string, key string, value interface{}) {
	tm.mu.RLock()
	trace, ok := tm.traces[traceID]
	tm.mu.RUnlock()

	if ok && trace != nil {
		trace.Tags[key] = value
	}
}

// AddLog adds a log entry to a trace
func (tm *TracingManager) AddLog(traceID string, fields map[string]interface{}) {
	tm.mu.RLock()
	trace, ok := tm.traces[traceID]
	tm.mu.RUnlock()

	if ok && trace != nil {
		trace.Logs = append(trace.Logs, TraceLog{
			Timestamp: time.Now(),
			Fields:    fields,
		})
	}
}

// GetTrace retrieves a trace by ID
func (tm *TracingManager) GetTrace(traceID string) *Trace {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	return tm.traces[traceID]
}

// ExportTraces exports all traces (for testing/debugging)
func (tm *TracingManager) ExportTraces() []*Trace {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	traces := make([]*Trace, 0, len(tm.traces))
	for _, trace := range tm.traces {
		traces = append(traces, trace)
	}
	return traces
}

// Clear clears all traces
func (tm *TracingManager) Clear() {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tm.traces = make(map[string]*Trace)
}

// JaegerExporter exports traces to Jaeger
type JaegerExporter struct {
	endpoint      string
	serviceName   string
	tracingMgr    *TracingManager
	buffer        chan *Trace
	stopCh        chan struct{}
	wg            sync.WaitGroup
	exportedCount int64
	stopped       bool
	mu            sync.RWMutex
}

// NewJaegerExporter creates a new Jaeger exporter
func NewJaegerExporter(endpoint, serviceName string, tracingMgr *TracingManager) *JaegerExporter {
	exporter := &JaegerExporter{
		endpoint:    endpoint,
		serviceName: serviceName,
		tracingMgr:  tracingMgr,
		buffer:      make(chan *Trace, 1000),
		stopCh:      make(chan struct{}),
	}

	// Start background export goroutine
	exporter.wg.Add(1)
	go exporter.exportWorker()

	return exporter
}

// Export exports a trace
func (je *JaegerExporter) Export(trace *Trace) error {
	if trace == nil {
		return fmt.Errorf("trace cannot be nil")
	}

	je.mu.RLock()
	stopped := je.stopped
	je.mu.RUnlock()

	if stopped {
		return fmt.Errorf("exporter stopped")
	}

	select {
	case je.buffer <- trace:
		return nil
	case <-je.stopCh:
		return fmt.Errorf("exporter stopped")
	default:
		return fmt.Errorf("buffer full")
	}
}

// exportWorker processes traces from the buffer
func (je *JaegerExporter) exportWorker() {
	defer je.wg.Done()

	for {
		select {
		case trace := <-je.buffer:
			if trace != nil {
				je.sendToJaeger(trace)
				je.mu.Lock()
				je.exportedCount++
				je.mu.Unlock()
			}

		case <-je.stopCh:
			return
		}
	}
}

// sendToJaeger sends a trace to Jaeger (stub implementation)
func (je *JaegerExporter) sendToJaeger(trace *Trace) error {
	// TODO: Implement actual Jaeger HTTP/gRPC endpoint sending
	// For now, just log the trace locally
	return nil
}

// GetExportedCount returns the number of traces exported
func (je *JaegerExporter) GetExportedCount() int64 {
	je.mu.RLock()
	defer je.mu.RUnlock()

	return je.exportedCount
}

// Close closes the exporter and waits for pending exports
func (je *JaegerExporter) Close() error {
	je.mu.Lock()
	je.stopped = true
	je.mu.Unlock()

	close(je.stopCh)

	// Flush remaining traces
	timeout := time.After(5 * time.Second)
	done := false
	for !done {
		select {
		case <-je.buffer:
			// drain any remaining traces
			continue
		case <-timeout:
			done = true
		default:
			done = true
		}
	}

	je.wg.Wait()
	return nil
}

// TraceContext holds trace context for propagation
type TraceContext struct {
	TraceID      string
	SpanID       string
	ParentSpanID string
	Baggage      map[string]string
}

// ContextWithTrace embeds TraceContext in a context
func ContextWithTrace(ctx context.Context, tc *TraceContext) context.Context {
	return context.WithValue(ctx, "trace_context", tc)
}

// TraceFromContext extracts TraceContext from a context
func TraceFromContext(ctx context.Context) *TraceContext {
	tc, ok := ctx.Value("trace_context").(*TraceContext)
	if ok {
		return tc
	}
	return nil
}
