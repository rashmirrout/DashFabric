package observability

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// MetricsCollector holds all Prometheus metrics
type MetricsCollector struct {
	// Request metrics
	RequestsTotal     prometheus.CounterVec
	RequestLatency    prometheus.HistogramVec
	ErrorsTotal       prometheus.CounterVec
	RetriesTotal      prometheus.CounterVec
	TimeoutsTotal     prometheus.CounterVec

	// Replica metrics
	ReplicasHealthy   prometheus.GaugeVec
	ReplicaLatency    prometheus.HistogramVec
	ReplicaErrorRate  prometheus.GaugeVec

	// Circuit breaker metrics
	CircuitBreakerState prometheus.GaugeVec
	CircuitBreakerTrips prometheus.CounterVec

	// Rate limiter metrics
	RateLimitExceeded prometheus.CounterVec

	// In-memory counters (for thread-safety)
	mu                sync.RWMutex
	requestCount      int64
	errorCount        int64
	retryCount        int64
	timeoutCount      int64
	cbTripCount       int64
	rateLimitCount    int64
}

// NewMetricsCollector creates a new metrics collector
func NewMetricsCollector(namespace string) *MetricsCollector {
	return &MetricsCollector{
		RequestsTotal: *promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "requests_total",
				Help:      "Total number of requests processed",
			},
			[]string{"method", "replica", "status"},
		),

		RequestLatency: *promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Name:      "request_latency_seconds",
				Help:      "Request latency in seconds",
				Buckets:   []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0},
			},
			[]string{"method", "replica"},
		),

		ErrorsTotal: *promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "errors_total",
				Help:      "Total number of errors",
			},
			[]string{"error_type", "replica"},
		),

		RetriesTotal: *promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "retries_total",
				Help:      "Total number of retries",
			},
			[]string{"attempt", "reason"},
		),

		TimeoutsTotal: *promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "timeouts_total",
				Help:      "Total number of timeouts",
			},
			[]string{"timeout_type"},
		),

		ReplicasHealthy: *promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "replicas_healthy",
				Help:      "Number of healthy replicas",
			},
			[]string{"replica"},
		),

		ReplicaLatency: *promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Name:      "replica_latency_seconds",
				Help:      "Per-replica latency in seconds",
				Buckets:   []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0},
			},
			[]string{"replica"},
		),

		ReplicaErrorRate: *promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "replica_error_rate",
				Help:      "Error rate per replica (0.0-1.0)",
			},
			[]string{"replica"},
		),

		CircuitBreakerState: *promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "circuit_breaker_state",
				Help:      "Circuit breaker state (0=CLOSED, 1=OPEN, 2=HALF_OPEN)",
			},
			[]string{"replica"},
		),

		CircuitBreakerTrips: *promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "circuit_breaker_trips_total",
				Help:      "Total number of circuit breaker trips",
			},
			[]string{"replica", "reason"},
		),

		RateLimitExceeded: *promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "rate_limit_exceeded_total",
				Help:      "Total number of rate limit exceeded",
			},
			[]string{"dimension"},
		),
	}
}

// RecordRequest records a successful request
func (m *MetricsCollector) RecordRequest(method, replica, status string, latency time.Duration) {
	m.RequestsTotal.WithLabelValues(method, replica, status).Inc()
	m.RequestLatency.WithLabelValues(method, replica).Observe(latency.Seconds())

	atomic.AddInt64(&m.requestCount, 1)
}

// RecordError records an error
func (m *MetricsCollector) RecordError(errorType, replica string) {
	m.ErrorsTotal.WithLabelValues(errorType, replica).Inc()

	atomic.AddInt64(&m.errorCount, 1)
}

// RecordRetry records a retry attempt
func (m *MetricsCollector) RecordRetry(attempt int, reason string) {
	m.RetriesTotal.WithLabelValues(
		prometheus.BuildFQName("", "", string(rune('0'+attempt))),
		reason,
	).Inc()

	atomic.AddInt64(&m.retryCount, 1)
}

// RecordTimeout records a timeout
func (m *MetricsCollector) RecordTimeout(timeoutType string) {
	m.TimeoutsTotal.WithLabelValues(timeoutType).Inc()

	atomic.AddInt64(&m.timeoutCount, 1)
}

// UpdateReplicaHealth updates replica health status
func (m *MetricsCollector) UpdateReplicaHealth(replica string, healthy bool) {
	value := 0.0
	if healthy {
		value = 1.0
	}
	m.ReplicasHealthy.WithLabelValues(replica).Set(value)
}

// RecordReplicaLatency records per-replica latency
func (m *MetricsCollector) RecordReplicaLatency(replica string, latency time.Duration) {
	m.ReplicaLatency.WithLabelValues(replica).Observe(latency.Seconds())
}

// UpdateReplicaErrorRate updates error rate for replica
func (m *MetricsCollector) UpdateReplicaErrorRate(replica string, errorRate float64) {
	if errorRate < 0 {
		errorRate = 0
	}
	if errorRate > 1.0 {
		errorRate = 1.0
	}
	m.ReplicaErrorRate.WithLabelValues(replica).Set(errorRate)
}

// RecordCircuitBreakerState records CB state change
func (m *MetricsCollector) RecordCircuitBreakerState(replica string, state int) {
	m.CircuitBreakerState.WithLabelValues(replica).Set(float64(state))
}

// RecordCircuitBreakerTrip records CB trip
func (m *MetricsCollector) RecordCircuitBreakerTrip(replica, reason string) {
	m.CircuitBreakerTrips.WithLabelValues(replica, reason).Inc()

	atomic.AddInt64(&m.cbTripCount, 1)
}

// RecordRateLimitExceeded records rate limit exceeded
func (m *MetricsCollector) RecordRateLimitExceeded(dimension string) {
	m.RateLimitExceeded.WithLabelValues(dimension).Inc()

	atomic.AddInt64(&m.rateLimitCount, 1)
}

// GetCounters returns a snapshot of in-memory counters
func (m *MetricsCollector) GetCounters() map[string]int64 {
	return map[string]int64{
		"requests":           atomic.LoadInt64(&m.requestCount),
		"errors":             atomic.LoadInt64(&m.errorCount),
		"retries":            atomic.LoadInt64(&m.retryCount),
		"timeouts":           atomic.LoadInt64(&m.timeoutCount),
		"circuit_breaker_trips": atomic.LoadInt64(&m.cbTripCount),
		"rate_limit_exceeded": atomic.LoadInt64(&m.rateLimitCount),
	}
}
