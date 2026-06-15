package gateway

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// HealthMonitor interface for monitoring replica health
type HealthMonitor interface {
	Start(ctx context.Context) error
	Stop()
}

// Replica represents a backend replica that handles requests
type Replica struct {
	Name    string
	Address string
	Healthy bool

	mu                sync.RWMutex
	Metrics           ReplicaMetrics
	ActiveConnections int64
	conn              interface{} // Will be grpc.ClientConn in real implementation
}

// ReplicaMetrics tracks replica performance metrics
type ReplicaMetrics struct {
	RequestCount   int64
	ErrorCount     int64
	TotalLatency   int64 // nanoseconds
	LastCheckTime  time.Time
	LastErrorTime  time.Time
	AvgLatencyMs   int64
	ConsecutiveErrors int32
}

// RequestContext holds context for a single request
type RequestContext struct {
	RequestID    string
	ClientIP     string
	ClientPort   string
	Method       string
	Deadline     time.Time
	Replica      *Replica
	StartTime    time.Time
	Attempt      int
	MaxAttempts  int
	TraceID      string
	SpanID       string
}

// HealthCheckConfig defines health check parameters
type HealthCheckConfig struct {
	Type     string        // "http", "grpc", "tcp", "custom"
	Interval time.Duration // How often to check
	Timeout  time.Duration // Check timeout
	Path     string        // HTTP path for health endpoint
	Port     int           // Override port (0 = use replica port)
	Method   string        // HTTP method (GET, POST)
}

// HealthStatus represents replica health state
type HealthStatus string

const (
	HealthStatusHealthy   HealthStatus = "HEALTHY"
	HealthStatusUnhealthy HealthStatus = "UNHEALTHY"
	HealthStatusUnknown   HealthStatus = "UNKNOWN"
)

// CircuitBreakerState represents CB state
type CircuitBreakerState string

const (
	CircuitBreakerClosed   CircuitBreakerState = "CLOSED"
	CircuitBreakerOpen     CircuitBreakerState = "OPEN"
	CircuitBreakerHalfOpen CircuitBreakerState = "HALF_OPEN"
)

// GatewayState represents overall gateway state
type GatewayState string

const (
	GatewayStateReady    GatewayState = "READY"
	GatewayStateDegraded GatewayState = "DEGRADED"
	GatewayStatePanic    GatewayState = "PANIC"
)

// RecordSuccess increments success metrics
func (r *Replica) RecordSuccess(latency time.Duration) {
	atomic.AddInt64(&r.Metrics.RequestCount, 1)
	atomic.AddInt64(&r.Metrics.TotalLatency, int64(latency.Nanoseconds()))
	atomic.StoreInt32(&r.Metrics.ConsecutiveErrors, 0)

	r.mu.Lock()
	r.Metrics.LastCheckTime = time.Now()
	r.mu.Unlock()
}

// RecordError increments error metrics
func (r *Replica) RecordError() {
	atomic.AddInt64(&r.Metrics.ErrorCount, 1)
	atomic.AddInt32(&r.Metrics.ConsecutiveErrors, 1)

	r.mu.Lock()
	r.Metrics.LastErrorTime = time.Now()
	r.mu.Unlock()
}

// GetAverageLatency returns average latency in milliseconds
func (r *Replica) GetAverageLatency() time.Duration {
	reqCount := atomic.LoadInt64(&r.Metrics.RequestCount)
	if reqCount == 0 {
		return 0
	}

	totalLatency := atomic.LoadInt64(&r.Metrics.TotalLatency)
	avgLatencyNs := totalLatency / reqCount
	return time.Duration(avgLatencyNs)
}

// GetErrorRate returns error rate as percentage (0-100)
func (r *Replica) GetErrorRate() float64 {
	reqCount := atomic.LoadInt64(&r.Metrics.RequestCount)
	if reqCount == 0 {
		return 0
	}

	errCount := atomic.LoadInt64(&r.Metrics.ErrorCount)
	return (float64(errCount) / float64(reqCount)) * 100
}

// ResetMetrics clears all metrics
func (r *Replica) ResetMetrics() {
	atomic.StoreInt64(&r.Metrics.RequestCount, 0)
	atomic.StoreInt64(&r.Metrics.ErrorCount, 0)
	atomic.StoreInt64(&r.Metrics.TotalLatency, 0)
	atomic.StoreInt32(&r.Metrics.ConsecutiveErrors, 0)

	r.mu.Lock()
	r.Metrics.LastCheckTime = time.Time{}
	r.Metrics.LastErrorTime = time.Time{}
	r.mu.Unlock()
}

// Request represents incoming client request
type Request struct {
	Method   string
	Payload  []byte
	Headers  map[string]string
	TimeoutMs int64
	RequestID string
	ClientIP  string
}

// Response represents response from replica
type Response struct {
	StatusCode int
	Payload    []byte
	Headers    map[string]string
	LatencyMs  int64
}

// HealthCheckResult represents result of a health check
type HealthCheckResult struct {
	Healthy        bool
	ResponseTimeMs int64
	Error          string
	CheckedAt      time.Time
}
