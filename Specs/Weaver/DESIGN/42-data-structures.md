# Weaver: Data Structures

> **Read Time:** 20 minutes  
> **Previous:** [41-lld.md](./41-lld.md) | **Next:** [43-algorithms.md](./43-algorithms.md)

---

## Core Data Structures

### Gateway

```go
type Gateway struct {
  config *Config
  listeners map[string]*Listener
  discovery Discovery
  healthMonitor *HealthMonitor
  loadBalancer LoadBalancer
  rateLimiter *RateLimiter
  circuitBreaker *CircuitBreaker
  replicas []*Replica  // current list
  replicasMu sync.RWMutex  // protects replicas
  metrics *MetricsCollector
  tracer trace.Tracer
}
```

### Replica

```go
type Replica struct {
  Name string
  Address string  // "host:port"
  Status ReplicaStatus  // HEALTHY, UNHEALTHY, PANIC_MODE
  Load int64  // active connections (atomic)
  LastHealthCheck time.Time
  FailureCount int
  SuccessCount int
  Attributes map[string]string  // {region: us-east-1, tier: primary}
  Conn *grpc.ClientConn  // connection pool
  ConnMu sync.Mutex  // protects connection
}

type ReplicaStatus string
const (
  ReplicaHealthy = "HEALTHY"
  ReplicaUnhealthy = "UNHEALTHY"
  ReplicaPanicMode = "PANIC_MODE"
)
```

### RequestContext

```go
type RequestContext struct {
  ClientID string
  TenantID string
  TraceID string
  Metadata map[string]string
  SelectedReplica *Replica
  AttemptCount int
  StartTime time.Time
  RequestDeadline time.Time
}
```

### CircuitBreaker

```go
type CircuitBreaker struct {
  state CircuitState
  failures int
  successes int
  lastFailureTime time.Time
  lastStateChangeTime time.Time
  mu sync.Mutex
  config CircuitBreakerConfig
}

type CircuitState string
const (
  StateClosed = "CLOSED"
  StateOpen = "OPEN"
  StateHalfOpen = "HALF_OPEN"
)

type CircuitBreakerConfig struct {
  FailureThreshold int  // e.g., 5
  SuccessThreshold int  // e.g., 2
  Timeout time.Duration  // e.g., 60s
}
```

### TokenBucket (for Rate Limiting)

```go
type TokenBucket struct {
  tokens float64  // current tokens available
  capacity float64  // max tokens
  refillRate float64  // tokens per second
  lastRefillTime time.Time
  mu sync.Mutex
}

func (tb *TokenBucket) Allow(count int) bool {
  tb.mu.Lock()
  defer tb.mu.Unlock()
  
  tb.refill()  // add tokens based on time elapsed
  
  if tb.tokens >= float64(count) {
    tb.tokens -= float64(count)
    return true
  }
  return false
}
```

### HealthMonitor

```go
type HealthMonitor struct {
  replicas []*Replica
  config HealthCheckConfig
  results map[string]HealthCheckResult
  mu sync.RWMutex
}

type HealthCheckConfig struct {
  Type string  // HTTP, gRPC, TCP
  Interval time.Duration
  Timeout time.Duration
  UnhealthyThreshold int
  HealthyThreshold int
  // For HTTP:
  Endpoint string
  // For TCP:
  Port int
}

type HealthCheckResult struct {
  Replica string
  Status ReplicaStatus
  LastCheck time.Time
  FailureCount int
}
```

### LoadBalancer Interface

```go
type LoadBalancer interface {
  // Select replica for incoming request
  Select(ctx *RequestContext, replicas []*Replica) (*Replica, error)
  
  // Notify of load change (for least-connections tracking)
  UpdateLoad(replica *Replica, delta int)
  
  // React to replica list changes
  Rebalance(replicas []*Replica) error
  
  // Get algorithm name
  Name() string
}
```

### RateLimiter

```go
type RateLimiter struct {
  // Global rate limit
  global *TokenBucket
  
  // Per-client limits
  perClient map[string]*TokenBucket
  perClientMu sync.RWMutex
  
  // Per-IP limits
  perIP map[string]*TokenBucket
  perIPMu sync.RWMutex
  
  // Per-tenant limits
  perTenant map[string]*TokenBucket
  perTenantMu sync.RWMutex
  
  config RateLimiterConfig
}

type RateLimiterConfig struct {
  Global RateLimitConfig
  PerClient RateLimitConfig
  PerIP RateLimitConfig
  PerTenant RateLimitConfig
}

type RateLimitConfig struct {
  Enabled bool
  RequestsPerSecond int
}
```

### Listener

```go
type Listener struct {
  name string
  protocol string  // grpc, http
  port int
  config ListenerConfig
  server interface{}  // *grpc.Server or *http.Server
}
```

### Config

```go
type Config struct {
  Gateway GatewayConfig
  Listeners map[string]ListenerConfig
  Discovery DiscoveryConfig
  Health HealthCheckConfig
  LoadBalancers map[string]LoadBalancerConfig
  RateLimiting RateLimitingConfig
  Reliability ReliabilityConfig
  Observability ObservabilityConfig
  Authentication AuthConfig
  Authorization AuthzConfig
  TLS TLSConfig
}

type GatewayConfig struct {
  Name string
  Description string
  LogLevel string  // DEBUG, INFO, WARN, ERROR
}

type ReliabilityConfig struct {
  Timeout TimeoutConfig
  CircuitBreaker CircuitBreakerConfig
  Retry RetryConfig
}

type TimeoutConfig struct {
  Global time.Duration
  PerReplica time.Duration
  Connection time.Duration
}

type RetryConfig struct {
  Enabled bool
  MaxAttempts int
  Backoff BackoffConfig
}

type BackoffConfig struct {
  Base time.Duration
  Multiplier float64
  Max time.Duration
}
```

### Metrics

```go
type MetricsCollector struct {
  // Counters
  requestsTotal prometheus.Counter
  errorsTotal prometheus.Counter
  rateLimitedTotal prometheus.Counter
  cbTransitionsTotal prometheus.Counter
  
  // Gauges
  replicasTotal prometheus.Gauge
  replicasHealthy prometheus.Gauge
  circuitBreakerState prometheus.GaugeVec
  replicaLoad prometheus.GaugeVec
  
  // Histograms
  requestDuration prometheus.Histogram
}
```

---

## Memory Layout (Example)

```
Gateway Memory:
  ├─ Config (1KB)
  ├─ Replicas (3 replicas × 500B = 1.5KB)
  ├─ CircuitBreaker (100B)
  ├─ RateLimiter (global + per-client buckets)
  │   └─ Per-client buckets: 100 clients × 50B = 5KB
  ├─ HealthMonitor (status for 3 replicas = 150B)
  ├─ Connections (3 replicas × 50KB per conn pool = 150KB)
  └─ Metrics (histogram data = varies)

Total: ~200KB baseline + connection pools
Per request: minimal (just context struct ~200B)
```

---

## Comparison with Other Approaches

### Array vs Map for Replicas

**Array (Current):**
- Lookup: O(n) linear scan
- Update: O(1) index
- Memory: Dense
- Good for: <100 replicas

**Map:**
- Lookup: O(1) hash
- Update: O(1)
- Memory: Sparse
- Good for: 1000+ replicas

**Decision:** Use array for <100 replicas (typical); upgrade to map if needed.

### Mutex vs RWMutex for Replicas

**Mutex:**
- Simple, fast for uncontended case
- Fair scheduling
- Good for: moderate contention

**RWMutex:**
- Many readers, few writers
- Allows concurrent reads
- Good for: high read/write ratio (>10:1)

**Decision:** Use RWMutex (many requests read replicas, few discovery updates)

---

**Navigation:**
- [← Previous](./41-lld.md)
- [Index](../INDEX.md)
- [Next →](./43-algorithms.md)
