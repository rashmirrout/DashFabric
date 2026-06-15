# Weaver: Low-Level Design (LLD)

> **Status:** Production Design  
> **Version:** 1.0 (Phase 1)  
> **Audience:** Engineers, Contributors, Advanced Operators  
> **Last Updated:** 2026-06-15

---

## Table of Contents

1. [Core Data Structures](#core-data-structures)
2. [Interface Definitions](#interface-definitions)
3. [Component Architecture](#component-architecture)
4. [Request Processing Pipeline](#request-processing-pipeline)
5. [Concurrency Model](#concurrency-model)
6. [Algorithm Details](#algorithm-details)
7. [Error Handling Strategy](#error-handling-strategy)
8. [Plugin System](#plugin-system)

---

## Core Data Structures

### Replica

Represents a backend service replica discovered from registry.

```go
type Replica struct {
    // Identity
    ID          string            // Unique replica identifier
    Name        string            // Human-readable name
    Address     string            // IP or hostname
    Port        int               // Service port
    
    // Metadata
    Metadata    map[string]string // Custom metadata (region, tier, etc.)
    Weight      int               // For weighted LB (default: 1)
    
    // State
    Status      ReplicaStatus     // HEALTHY, UNHEALTHY, DRAINING
    LastChecked time.Time         // When last health check ran
    
    // Metrics
    ActiveConnections int          // Current open connections
    TotalRequests     uint64        // Cumulative requests
    TotalErrors       uint64        // Cumulative errors
    TotalRetries      uint64        // Cumulative retries
    
    // Performance
    AvgLatencyMs      float64       // Moving average latency
    P99LatencyMs      float64       // 99th percentile
    LastErrorTime     time.Time     // When last error occurred
    
    // CircuitBreaker
    CBState           CircuitBreakerState  // CLOSED, OPEN, HALF_OPEN
    CBFailures        int                  // Consecutive failures
    CBSuccesses       int                  // Consecutive successes
    CBStateChangedAt  time.Time            // Last state transition
    
    // Queue
    RequestQueue      chan *Request        // Buffered request queue
    QueueDepth        int                  // Current queue length
    QueuedAt          []time.Time          // Track queue entry times
    
    mu                sync.RWMutex         // Synchronize access
}

type ReplicaStatus string
const (
    StatusHealthy   ReplicaStatus = "HEALTHY"
    StatusUnhealthy ReplicaStatus = "UNHEALTHY"
    StatusDraining  ReplicaStatus = "DRAINING"
)

type CircuitBreakerState string
const (
    CBClosed    CircuitBreakerState = "CLOSED"
    CBOpen      CircuitBreakerState = "OPEN"
    CBHalfOpen  CircuitBreakerState = "HALF_OPEN"
)
```

---

### Request

Represents a client request flowing through the gateway.

```go
type Request struct {
    // Identity
    ID              string            // Unique request identifier
    ClientID        string            // Client identifier (from auth/header)
    TraceID         string            // Distributed trace ID
    
    // Protocol
    Protocol        ProtocolType      // GRPC, HTTP, REST
    Method          string            // gRPC method or HTTP path
    ServiceName     string            // gRPC service name
    
    // Payload
    Headers         map[string]string // HTTP headers or gRPC metadata
    Body            []byte            // Request payload
    
    // Routing
    TargetReplica   *Replica          // Selected replica (set by LB)
    LoadBalancer    string            // LB strategy name used
    
    // Timing
    StartTime       time.Time         // When request arrived at gateway
    DeadlineTime    time.Time         // Global timeout deadline
    ReplicaTimeout  time.Duration     // Per-replica timeout
    ConnectTimeout  time.Duration     // Connection timeout
    
    // State
    Status          RequestStatus     // PENDING, ROUTED, COMPLETED, FAILED
    RetryCount      int               // Attempts so far
    MaxRetries      int               // Max retries allowed
    
    // Response
    Response        *Response         // Populated by replica handler
    Error           error             // Error if request failed
    
    // Context
    Context         context.Context   // For cancellation/deadlines
    Cancel          context.CancelFunc
    
    // Rate limiting
    RateLimitExceeded bool            // True if rate limit hit
    
    mu              sync.RWMutex
}

type ProtocolType string
const (
    ProtocolGRPC ProtocolType = "grpc"
    ProtocolHTTP ProtocolType = "http"
    ProtocolREST ProtocolType = "rest"
)

type RequestStatus string
const (
    StatusPending   RequestStatus = "PENDING"
    StatusRouted    RequestStatus = "ROUTED"
    StatusCompleted RequestStatus = "COMPLETED"
    StatusFailed    RequestStatus = "FAILED"
)
```

---

### Response

Result returned by replica.

```go
type Response struct {
    // Protocol
    Protocol        ProtocolType       // Same as request
    StatusCode      int                // HTTP status or gRPC status code
    
    // Payload
    Headers         map[string]string
    Body            []byte
    
    // Timing
    ReceivedAt      time.Time          // When first byte received
    CompletedAt     time.Time          // When fully received
    LatencyMs       float64            // Total latency (ms)
    
    // Stream (for gRPC)
    IsStream        bool               // True for bidirectional streams
    StreamClosed    bool               // True when stream ends
}
```

---

### ReplicaManager

Thread-safe manager for replica collection.

```go
type ReplicaManager struct {
    // Replicas
    replicas        map[string]*Replica  // Key: replica ID
    replicaList     []*Replica           // Sorted list for iteration
    healthyList     []*Replica           // Filtered healthy list (cache)
    
    // Metrics
    totalReplicas   int                  // Total count
    healthyCount    int                  // Healthy count
    unhealthyCount  int                  // Unhealthy count
    
    // State
    panicMode       bool                 // True if >50% unhealthy
    panicModeAt     time.Time            // When panic mode entered
    
    // Sync
    mu              sync.RWMutex         // Serialize access
    changeNotify    chan struct{}        // Notify on replica changes
    
    // Config
    PanicThreshold  float64              // Panic if unhealthy > this % (0.5 = 50%)
}

// Key methods
func (rm *ReplicaManager) Add(r *Replica) error
func (rm *ReplicaManager) Remove(id string) error
func (rm *ReplicaManager) Update(id string, status ReplicaStatus) error
func (rm *ReplicaManager) GetAll() []*Replica
func (rm *ReplicaManager) GetHealthy() []*Replica
func (rm *ReplicaManager) UpdateMetrics(id string, latency float64, err error) error
func (rm *ReplicaManager) InPanicMode() bool
func (rm *ReplicaManager) OnChange() <-chan struct{}  // Returns change notification channel
```

---

### CircuitBreaker

Per-replica circuit breaker state machine.

```go
type CircuitBreaker struct {
    // State
    state           CircuitBreakerState
    stateChangedAt  time.Time
    
    // Counters
    consecutiveFailures int
    consecutiveSuccesses int
    totalFailures    uint64
    totalSuccesses   uint64
    
    // Config
    FailureThreshold   int           // Failures before OPEN
    SuccessThreshold   int           // Successes before CLOSED (from HALF_OPEN)
    HalfOpenTimeout    time.Duration // Time in HALF_OPEN before CLOSED
    MetricsWindow      time.Duration // Window for counting failures
    
    // Recent events
    events           []CBEvent      // Last N events
    
    mu               sync.RWMutex
}

type CBEvent struct {
    Time      time.Time
    OldState  CircuitBreakerState
    NewState  CircuitBreakerState
    Reason    string
}

// Key methods
func (cb *CircuitBreaker) RecordSuccess()
func (cb *CircuitBreaker) RecordFailure() error  // Returns error if OPEN
func (cb *CircuitBreaker) CanAttempt() bool      // True if not OPEN or HALF_OPEN ready
func (cb *CircuitBreaker) State() CircuitBreakerState
func (cb *CircuitBreaker) Events() []CBEvent
```

---

### RequestRouter

Routes incoming requests to replicas.

```go
type RequestRouter struct {
    // Components
    replicaMgr      *ReplicaManager
    loadBalancers   map[string]LoadBalancer
    rateLimiter     RateLimiter
    circuitBreakers map[string]*CircuitBreaker  // Per-replica
    
    // Configuration
    DefaultLB       string
    GlobalTimeout   time.Duration
    PerReplicaTimeout time.Duration
    ConnectTimeout  time.Duration
    MaxRetries      int
    
    // Metrics
    routedRequests  uint64
    failedRequests  uint64
    retries         uint64
}

// Key methods
func (r *RequestRouter) Route(ctx context.Context, req *Request) (*Response, error) {
    // 1. Check rate limit
    // 2. Select replica via LB
    // 3. Forward request with retries
    // 4. Update metrics
    // 5. Return response
}

func (r *RequestRouter) Retry(ctx context.Context, req *Request) (*Response, error)
func (r *RequestRouter) GetMetrics() RouterMetrics
```

---

### Metrics

Prometheus metrics collection.

```go
type Metrics struct {
    // Replica health
    ReplicaStatus      *prometheus.GaugeVec
    ActiveConnections  *prometheus.GaugeVec
    
    // Requests
    RequestsTotal      *prometheus.CounterVec
    RequestLatency     *prometheus.HistogramVec
    
    // Errors
    ErrorsTotal        *prometheus.CounterVec
    
    // Circuit breaker
    CBState            *prometheus.GaugeVec
    CBTransitions      *prometheus.CounterVec
    
    // Queuing
    QueueDepth         *prometheus.GaugeVec
    QueueTimeouts      *prometheus.CounterVec
    
    // Rate limiting
    RateLimitViolations *prometheus.CounterVec
}

// Key methods
func (m *Metrics) RecordRequest(method string, latency float64, replica string, err error)
func (m *Metrics) UpdateReplicaHealth(replica string, healthy bool)
func (m *Metrics) UpdateQueueDepth(replica string, depth int)
func (m *Metrics) RecordRateLimitViolation(limitType string)
```

---

## Interface Definitions

### PodDiscoverer

Discovers available replicas from registry.

```go
type PodDiscoverer interface {
    // Discover returns list of replicas
    Discover(ctx context.Context) ([]*Replica, error)
    
    // Watch returns channel that notifies when replicas change
    Watch(ctx context.Context) <-chan DiscoveryUpdate
    
    // Close stops the discoverer
    Close() error
}

type DiscoveryUpdate struct {
    Replica *Replica
    Action  DiscoveryAction  // ADDED, REMOVED, UPDATED
}

type DiscoveryAction string
const (
    ActionAdded   DiscoveryAction = "ADDED"
    ActionRemoved DiscoveryAction = "REMOVED"
    ActionUpdated DiscoveryAction = "UPDATED"
)

// Implementations:
// - T2EtcdDiscoverer: Poll T2 etcd for replica keys
// - ConsulDiscoverer: Query Consul service catalog
// - K8sDiscoverer: Query Kubernetes API
// - DNSDiscoverer: Query DNS (A/SRV records)
// - StaticDiscoverer: Fixed replica list
```

---

### HealthChecker

Monitors replica health status.

```go
type HealthChecker interface {
    // Check performs single health check
    Check(ctx context.Context, replica *Replica) (bool, error)
    
    // Start begins periodic health checking
    Start(ctx context.Context) error
    
    // Stop halts health checking
    Stop() error
}

// Implementations:
// - HTTPHealthChecker: GET <replica>/health; expect 200
// - GRPCHealthChecker: Call grpc.health.v1.Health/Check
// - TCPHealthChecker: Try to connect; if succeeds, healthy
// - CustomHealthChecker: User-provided via plugin
```

---

### LoadBalancer

Selects a replica for a request.

```go
type LoadBalancer interface {
    // SelectReplica chooses a replica for request
    SelectReplica(ctx context.Context, req *Request, availableReplicas []*Replica) (*Replica, error)
    
    // RecordRequest updates LB state after request completes
    RecordRequest(replica *Replica, latency float64, err error)
    
    // Name returns LB name
    Name() string
}

// Implementations:
// - LeastConnectionsLB: Select replica with fewest active connections
// - RoundRobinLB: Cycle through replicas sequentially
// - RandomLB: Pick random replica
// - ConsistentHashLB: Hash-based affinity (client_id → same replica)
// - WeightedLB: Proportional selection by weight
// - StickyLB: Affinity with TTL
// - ResourceAwareLB: Select by CPU, memory, queue depth
// - CustomLB: User-provided via plugin
```

---

### RateLimiter

Enforces multi-dimensional rate limits.

```go
type RateLimiter interface {
    // Allow checks if request should be allowed
    Allow(ctx context.Context, key RateLimitKey) bool
    
    // Record updates limiter state after request
    Record(key RateLimitKey, allowed bool)
}

type RateLimitKey struct {
    LimitType string        // "global", "client", "ip", "tenant"
    Value     string        // The key value (client_id, IP, tenant_id, etc.)
    Dimension string        // Optional sub-dimension
}

// Implementations:
// - TokenBucketLimiter: Token bucket algorithm
// - LeakyBucketLimiter: Leaky bucket algorithm
// - SlidingWindowLimiter: Sliding window counter
```

---

### RequestHandler

Processes requests for specific protocol.

```go
type RequestHandler interface {
    // Handle processes incoming request
    Handle(ctx context.Context, req *Request) (*Response, error)
    
    // Protocol returns protocol type
    Protocol() ProtocolType
}

// Implementations:
// - GRPCHandler: Handles gRPC Subscribe/Publish
// - HTTPHandler: Handles HTTP requests
// - RESTHandler: Handles REST API requests
```

---

### Authenticator

Verifies client identity.

```go
type Authenticator interface {
    // Authenticate verifies request credentials
    Authenticate(ctx context.Context, req *Request) (AuthContext, error)
}

type AuthContext struct {
    Authenticated bool
    ClientID      string
    UserID        string
    Roles         []string
    Claims        map[string]interface{}  // Custom claims
}

// Implementations:
// - BearerTokenAuth: Validate Bearer token from header
// - APIKeyAuth: Validate API key from header
// - JWTAuth: Verify JWT signature
// - mTLSAuth: Extract client identity from TLS cert
// - CustomAuth: User-provided via plugin
```

---

### Authorizer

Enforces access control.

```go
type Authorizer interface {
    // Authorize checks if authenticated user can access resource
    Authorize(ctx context.Context, authCtx AuthContext, resource string, action string) error
}

// Implementations:
// - RBACAuthorizer: Role-based access control
// - ABACAuthorizer: Attribute-based access control
// - CustomAuthorizer: User-provided via plugin
```

---

## Component Architecture

### Main Gateway Process

```go
type Gateway struct {
    // Listeners
    grpcListener    *grpc.Server
    httpListener    *http.Server
    
    // Core components
    discoverer      PodDiscoverer
    healthChecker   HealthChecker
    replicaMgr      *ReplicaManager
    router          *RequestRouter
    
    // Handlers
    grpcHandler     RequestHandler
    httpHandler     RequestHandler
    
    // Security
    authenticator   Authenticator
    authorizer      Authorizer
    
    // Observability
    metrics         *Metrics
    logger          *Logger
    tracer          opentelemetry.Tracer
    
    // Config
    config          GatewayConfig
    
    // Shutdown
    shutdownCh      chan os.Signal
    ctx             context.Context
    cancel          context.CancelFunc
}

// Key methods
func (g *Gateway) Start(ctx context.Context) error {
    // 1. Load config
    // 2. Initialize discoverer
    // 3. Start health checker
    // 4. Start listeners
    // 5. Wait for signals
}

func (g *Gateway) Stop(ctx context.Context) error {
    // 1. Stop accepting new connections
    // 2. Drain in-flight requests (grace period)
    // 3. Stop health checker
    // 4. Close discoverer
    // 5. Shutdown servers
}
```

---

### Goroutine Model

```
Main Process
├─ gRPC Listener Goroutine
│  ├─ Per-connection handler goroutine (1 per client)
│  │  └─ Per-stream handler goroutine (bidirectional streams)
│  │
│  └─ Metrics collector goroutine
│
├─ HTTP Listener Goroutine
│  ├─ Per-request handler goroutine (go http.Server default)
│  │
│  └─ Metrics collector goroutine
│
├─ Pod Discovery Goroutine
│  └─ Poll loop (every poll_interval)
│     └─ Update ReplicaManager on changes
│
├─ Health Checking Goroutine
│  └─ Poll loop (every health_interval)
│     └─ Update Replica.Status on each check
│
├─ Config Hot-Reload Watcher
│  └─ File system monitor; reload on change
│
├─ Metrics Flush Goroutine
│  └─ Periodic Prometheus metric export
│
└─ Tracing Export Goroutine
   └─ Batch span exports to collector
```

---

## Request Processing Pipeline

### Detailed Request Flow

```
1. REQUEST ARRIVAL
   Client → gRPC/HTTP → Listener
   ├─ Extract protocol type (gRPC/HTTP/REST)
   ├─ Parse headers/metadata
   └─ Create Request object

2. AUTHENTICATION
   ├─ Authenticator.Authenticate(req)
   ├─ Extract credentials from headers/certs
   ├─ Validate against configured auth provider
   └─ Populate AuthContext (user_id, roles, claims)

3. AUTHORIZATION
   ├─ Authorizer.Authorize(authCtx, resource, action)
   ├─ Check if user has required role/permission
   └─ Return error if denied

4. RATE LIMITING
   ├─ Extract rate limit keys (client_id, source_ip, tenant_id)
   ├─ RateLimiter.Allow() for each dimension
   ├─ If any limit exceeded:
   │  ├─ Set req.RateLimitExceeded = true
   │  ├─ Record violation metric
   │  └─ Return 429 (Too Many Requests)
   └─ Otherwise continue

5. REPLICA SELECTION
   ├─ ReplicaManager.GetHealthy() → list of healthy replicas
   ├─ If empty:
   │  ├─ Check if in panic mode (>50% unhealthy)
   │  ├─ If yes, return error
   │  └─ If no, include unhealthy replicas
   ├─ LoadBalancer.SelectReplica(req, replicas) → selected replica
   ├─ Set req.TargetReplica
   └─ Continue

6. CIRCUIT BREAKER CHECK
   ├─ Get CircuitBreaker for selected replica
   ├─ cb.CanAttempt()?
   │  ├─ No (CB OPEN) → Fail fast; try next replica; goto step 5
   │  └─ Yes → Continue
   └─ If CB in HALF_OPEN, mark as probe request

7. ROUTING (with retries)
   ├─ Setup timeout context (global + per-replica)
   ├─ Setup request queue:
   │  ├─ If queue full:
   │  │  ├─ Check overflow_behavior
   │  │  ├─ reject_with_503 → Return 503
   │  │  └─ drop_oldest → Remove oldest queued
   │  └─ Enqueue request
   ├─ Forward to selected replica
   ├─ Wait for response or timeout
   ├─ Record latency
   ├─ Handle response:
   │  ├─ Success (status 0 for gRPC, 200-299 for HTTP):
   │  │  ├─ cb.RecordSuccess()
   │  │  ├─ lb.RecordRequest(replica, latency, nil)
   │  │  └─ Return response; goto METRICS/TRACING
   │  ├─ Retryable error (503, 504, 429):
   │  │  ├─ cb.RecordFailure()
   │  │  ├─ req.RetryCount++
   │  │  ├─ If req.RetryCount < max_retries:
   │  │  │  ├─ Calculate backoff (exponential)
   │  │  │  ├─ Sleep(backoff)
   │  │  │  └─ Goto step 5 (select new replica)
   │  │  └─ Otherwise: goto ERROR HANDLING
   │  └─ Non-retryable error:
   │     └─ Goto ERROR HANDLING

8. ERROR HANDLING
   ├─ Populate req.Error
   ├─ Update circuit breaker
   ├─ Update metrics (error count, latency)
   ├─ Log error with context
   ├─ Return error to client

9. METRICS & TRACING
   ├─ Calculate total latency
   ├─ Record metrics (success/failure, latency, replica)
   ├─ Create trace span with attributes (replica, latency, client_id)
   ├─ Sample and export trace (if sample_rate hit)
   ├─ Write structured log (async)
   └─ Return response to client
```

---

## Concurrency Model

### Thread Safety

**Replica Manager:**
- Uses RWMutex for concurrent reads; serialized writes
- GetHealthy() fast-path: RLock (doesn't block other readers)
- Add/Remove: Wlock (exclusive; blocks all access)

**Circuit Breaker (per-replica):**
- Each replica has its own CB with RWMutex
- RecordSuccess/RecordFailure: Wlock (state changes)
- CanAttempt: Rlock (state query)

**Request Queue (per-replica):**
- Go channel: buffered queue
- Enqueue: non-blocking send (unless full)
- Dequeue: receiver goroutine pops from channel

**Load Balancer (per strategy):**
- Least-connections: Atomic counter increments (lock-free)
- Round-robin: Atomic counter increments (lock-free)
- Consistent hash: RWMutex for ring updates (rare)
- Weighted: Atomic operations

**Metrics:**
- Prometheus client library is thread-safe
- Counter.Inc(), Gauge.Set() use atomic operations

---

### Goroutine Safety

**gRPC Listener:**
- One goroutine per connection (established by grpc.Server)
- Each stream handled in separate goroutine
- Stream context cancellation propagates on disconnect
- No shared state between streams (request-scoped)

**HTTP Listener:**
- go http.Server spawns per-request goroutine
- Request context propagates through call chain
- Automatic cleanup on request completion

**Discovery Goroutine:**
- Polls registry every poll_interval
- Updates ReplicaManager (protected by mutex)
- Notifies change listeners

**Health Check Goroutine:**
- Polls each replica every health_interval
- Runs checks in parallel (one goroutine per replica)
- Updates Replica.Status (protected by replica.mu)

---

## Algorithm Details

### Consistent Hashing Algorithm

Used by ConsistentHashLB.

```
1. Build hash ring:
   ├─ For each replica R:
   │  └─ For i in 1..VirtualNodes:
   │     └─ Insert hash(R.ID + "#" + i) into ring
   └─ Sort ring keys

2. Select replica for request:
   ├─ key = hash(request.ClientID)
   ├─ Find first ring position >= key (wrapping)
   ├─ Return replica owning that position
   └─ On replica failure:
      ├─ Find next position (next available replica)
      └─ Return next replica

3. Virtual nodes (default: 150):
   ├─ More nodes = better distribution
   ├─ Fewer nodes = faster lookup
   └─ Trade-off: 150 nodes ≈ 3-5% distribution variance

Example (N=3 replicas, VirtualNodes=3):
Ring keys: [
  hash("fm-1#1") → fm-1
  hash("fm-1#2") → fm-1
  hash("fm-1#3") → fm-1
  hash("fm-2#1") → fm-2
  hash("fm-2#2") → fm-2
  hash("fm-2#3") → fm-2
  hash("fm-3#1") → fm-3
  hash("fm-3#2") → fm-3
  hash("fm-3#3") → fm-3
]

Request: client_id="alice"
  hash("alice") = 0x4a2f... (hypothetical)
  Find first key >= 0x4a2f in ring
  → hash("fm-2#3") = 0x5001 (next key)
  → Select fm-2
```

---

### Exponential Backoff Algorithm

Used by Retry logic.

```
backoff(attempt) = min(max_backoff, initial_backoff * (multiplier ^ (attempt - 1)))

Examples (initial=10ms, multiplier=2.0, max=5s):
  Attempt 1: 10ms
  Attempt 2: 20ms (10 * 2^1)
  Attempt 3: 40ms (10 * 2^2)
  Attempt 4: 80ms (10 * 2^3)
  ...
  Attempt 10: 5s (capped at max)

With jitter (recommended):
  backoff_with_jitter = backoff * (1 + random(0, 0.1))
  → Prevents thundering herd on retry

Timeline:
  T=0ms:    Request 1 → 503 error
  T=10ms:   Sleep 10ms
  T=20ms:   Request 2 → 503 error
  T=40ms:   Sleep 20ms
  T=60ms:   Request 3 → 503 error
  T=140ms:  Sleep 80ms (hypothetically up to this point)
  T=220ms:  Request 4 → success
```

---

### Token Bucket Algorithm

Used by RateLimiter (per-client, per-IP, etc).

```
Per-dimension (client, IP, tenant):
  tokens_per_second = 10000  (config)
  bucket_size = tokens_per_second  (1 second worth)

At each request:
  elapsed = now - last_refill
  refill_tokens = elapsed * (tokens_per_second / 1000)  // Per millisecond
  current_tokens = min(bucket_size, current_tokens + refill_tokens)
  last_refill = now

  if current_tokens >= 1:
    current_tokens -= 1
    Allow request
  else:
    Reject request (429)

Example (rate=1000 req/s, bucket_size=1000):
  T=0ms:      tokens=1000
  T=10ms:     +10 tokens → 1000 (capped)
              Request arrives → Allow; tokens=999
  T=50ms:     +40 tokens → 1039 (capped to 1000)
              10 requests arrive in burst → Allow all 10; tokens=990
  T=100ms:    +50 tokens → 1040 (capped to 1000)
  T=200ms:    +100 tokens → 1000 (capped)
              100 requests arrive → Allow all 100; tokens=900
  T=300ms:    +100 tokens → 1000
              1000 requests arrive → Reject 900; Allow 100
```

---

### Circuit Breaker State Machine

```
States and transitions:

CLOSED (operating normally)
  ├─ Failure recorded
  │  ├─ consecutive_failures++
  │  └─ If consecutive_failures >= failure_threshold:
  │     → OPEN
  │     → Record transition event
  │     → Set state_changed_at = now
  └─ Success recorded
     ├─ Reset consecutive_failures = 0
     └─ Remain CLOSED

OPEN (failing; reject all)
  ├─ New request arrives:
  │  ├─ If now - state_changed_at < timeout:
  │  │  └─ Reject immediately (fail-fast)
  │  └─ Else:
  │     ├─ → HALF_OPEN
  │     ├─ Allow 1 probe request
  │     └─ Set state_changed_at = now
  └─ Cannot proceed to CLOSED directly (must test first)

HALF_OPEN (testing recovery)
  ├─ Request sent as probe
  └─ Response received:
     ├─ Success:
     │  ├─ consecutive_successes++
     │  └─ If consecutive_successes >= success_threshold:
     │     ├─ → CLOSED
     │     ├─ Reset counters
     │     └─ Resume normal operation
     └─ Failure:
        ├─ consecutive_failures = 1
        └─ → OPEN
           ├─ Reset consecutive_successes = 0
           └─ Wait timeout before next probe
```

---

## Error Handling Strategy

### Retryable vs Non-Retryable Errors

**Retryable (transient):**
- gRPC: UNAVAILABLE (14), DEADLINE_EXCEEDED (4), RESOURCE_EXHAUSTED (8)
- HTTP: 503 (Service Unavailable), 504 (Gateway Timeout), 429 (Too Many Requests)
- Reason: Temporary; try different replica or later

**Non-Retryable (permanent):**
- gRPC: INVALID_ARGUMENT (3), NOT_FOUND (5), ALREADY_EXISTS (6), PERMISSION_DENIED (7)
- HTTP: 400 (Bad Request), 401 (Unauthorized), 403 (Forbidden), 404 (Not Found)
- Reason: Client error; won't succeed on retry

**Circuit Breaker Triggers (replicas marked unhealthy after repeated failures):**
- Connection refused
- Connection timeout
- Read timeout
- Any non-retryable gRPC/HTTP error from replica

---

### Error Propagation

```
Client Request
  ↓
[Auth fails]
  ├─ Return immediately: 401/UNAUTHENTICATED
  └─ No retry; no CB impact

  ↓
[Rate limit exceeded]
  ├─ Return immediately: 429/RESOURCE_EXHAUSTED
  └─ No retry; count violation metric

  ↓
[Replica selected; CB OPEN]
  ├─ Fail-fast: Skip this replica
  ├─ Try next replica
  └─ If all replicas CB OPEN:
     ├─ Return error: "All replicas circuit breaker open"
     └─ No retry

  ↓
[Route to replica; retryable error]
  ├─ cb.RecordFailure()
  ├─ Increment retry count
  ├─ Sleep (exponential backoff)
  ├─ Select new replica
  └─ Try again

  ↓
[Route to replica; non-retryable error]
  ├─ cb.RecordFailure()
  ├─ Check if CB should open (consecutive_failures threshold)
  └─ Return error to client (no retry)

  ↓
[Timeout (global or per-replica)]
  ├─ Abort request
  ├─ If global timeout:
  │  └─ Return timeout error
  └─ If per-replica timeout:
     ├─ cb.RecordFailure()
     ├─ Try next replica (if retries left)
     └─ Continue

  ↓
[Success]
  ├─ cb.RecordSuccess()
  ├─ Update metrics
  └─ Return response
```

---

## Plugin System

### Plugin Interface

```go
type Plugin interface {
    // Name returns unique plugin name
    Name() string
    
    // Version returns semantic version
    Version() string
    
    // Init initializes plugin with config
    Init(ctx context.Context, config map[string]interface{}) error
    
    // Type returns plugin type
    Type() PluginType
}

type PluginType string
const (
    PluginTypeDiscoverer   PluginType = "discoverer"
    PluginTypeHealthCheck  PluginType = "health_check"
    PluginTypeLoadBalancer PluginType = "load_balancer"
    PluginTypeAuth         PluginType = "auth"
    PluginTypeMetrics      PluginType = "metrics"
)

// Example custom plugin:
type CustomGeoLB struct {
    preferredAZ string
}

func (c *CustomGeoLB) Name() string { return "geo_lb" }
func (c *CustomGeoLB) Version() string { return "1.0.0" }
func (c *CustomGeoLB) Init(ctx context.Context, cfg map[string]interface{}) error {
    c.preferredAZ = cfg["prefer_az"].(string)
    return nil
}
func (c *CustomGeoLB) Type() PluginType { return PluginTypeLoadBalancer }

func (c *CustomGeoLB) SelectReplica(ctx context.Context, req *Request, replicas []*Replica) (*Replica, error) {
    // Select replica in preferred AZ; fall back to others
    for _, r := range replicas {
        if r.Metadata["az"] == c.preferredAZ {
            return r, nil
        }
    }
    // Fall back to first available
    if len(replicas) > 0 {
        return replicas[0], nil
    }
    return nil, errors.New("no replicas available")
}
```

---

### Plugin Loading

```go
// At startup:
pluginPaths := []string{
    "/opt/weaver/plugins/geo_lb.so",
    "/opt/weaver/plugins/custom_auth.so",
}

for _, path := range pluginPaths {
    p, err := plugin.Open(path)
    if err != nil {
        return err
    }
    
    // Lookup plugin factory function
    factory, err := p.Lookup("NewPlugin")
    if err != nil {
        return err
    }
    
    // Create plugin instance
    pluginInstance := factory.(func() Plugin)()
    
    // Register based on type
    switch pluginInstance.Type() {
    case PluginTypeLoadBalancer:
        g.loadBalancers[pluginInstance.Name()] = pluginInstance.(LoadBalancer)
    case PluginTypeDiscoverer:
        g.discoverer = pluginInstance.(PodDiscoverer)
    // ... etc
    }
}
```

---

**End of Low-Level Design**

All data structures, interfaces, algorithms, and concurrency model documented for implementation.
