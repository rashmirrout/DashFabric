# FM-Gateway: Low-Level Design (LLD)

> **Status:** Draft v1
> **Module:** FM-Gateway (fm-gw)
> **Scope:** Internal architecture, data structures, algorithms, concurrency model
> **Audience:** FM-GW implementers, code reviewers

## 1. Process Architecture

```
fm-gw process
├── Main goroutine
│   ├── Config load
│   ├── Signal handling (SIGTERM, SIGINT)
│   └── Lifecycle management
├── HTTP listener goroutine
│   ├── Accept connections
│   ├── Demultiplex to router
│   └── Write responses
├── gRPC listener goroutine
│   ├── Accept connections
│   ├── Demultiplex to Subscribe/Publish handlers
│   └── Proxy streams
├── Replica discovery goroutine
│   ├── Poll T2 etcd every FM_GW_REPLICA_DISCOVERY_INTERVAL_SECONDS
│   ├── Update in-memory replica list
│   └── Trigger health checks on new replicas
├── Health checker goroutine
│   ├── Poll each replica every FM_GW_HEALTH_CHECK_INTERVAL_SECONDS
│   ├── Update replica status (healthy/unhealthy)
│   └── Emit metrics
├── Metrics exporter goroutine
│   ├── Bind HTTP server on :9090
│   ├── Expose /metrics endpoint (Prometheus format)
│   └── Aggregate stats from all components
└── Graceful shutdown handler
    ├── On SIGTERM: stop accepting new requests
    ├── Drain in-flight requests (timeout 30s)
    ├── Close all connections
    └── Exit cleanly
```

## 2. Data Structures

### 2.1 Gateway

```go
type Gateway struct {
    // Configuration
    config *Config
    
    // Pod discovery + primary election
    discoveryManager *DiscoveryManager
    
    // Load balancer
    lb LoadBalancer  // interface: SelectReplica(deviceID, replicas)
    
    // HTTP/gRPC listeners
    httpServer *http.Server
    grpcServer *grpc.Server
    
    // Queues
    queues map[string]*Queue  // replica_name -> Queue
    queuesMu sync.RWMutex
    
    // Rate limiter
    rateLimiter *RateLimiter
    
    // Health checker
    healthChecker *HealthChecker
    
    // Metrics
    metrics *Metrics
    
    // Primary unavailability tracking
    primaryUnavailableAt *time.Time
    primaryWaitTimeout   time.Duration  // max 20s
    primaryMu            sync.Mutex
    
    // Shutdown signal
    shutdownChan chan struct{}
    shutdownOnce sync.Once
}
```

### 2.2 Replica

```go
type Replica struct {
    Name      string        // "fm-0", "fm-1", "fm-2"
    Ordinal   int           // 0, 1, 2 (for deterministic ordering)
    Address   string        // "10.0.0.1:8080"
    GrpcAddr  string        // "10.0.0.1:5051"
    
    // Primary/Standby role (from adapter lease)
    Role      ReplicaRole   // PRIMARY or STANDBY
    RoleMu    sync.RWMutex
    
    // State
    Status    ReplicaStatus // HEALTHY, DEGRADED, UNHEALTHY
    StatusMu  sync.RWMutex
    
    // Connection pooling
    httpClient *http.Client  // reused for HTTP requests
    grpcConn   *grpc.ClientConn  // reused for gRPC streams
    
    // Metrics
    activeConnections int  // gRPC streams
    activeConnMu   sync.RWMutex
    requestsTotal  uint64  // atomic
    latencyMs      uint64  // atomic; for moving average
    
    // Last health check
    lastHealthCheckTime time.Time
    consecutiveFailures int
}

type ReplicaRole int

const (
    RoleStandby ReplicaRole = iota
    RolePrimary
)

func (r *Replica) IsPrimary() bool {
    r.RoleMu.RLock()
    defer r.RoleMu.RUnlock()
    return r.Role == RolePrimary
}

func (r *Replica) SetRole(role ReplicaRole) {
    r.RoleMu.Lock()
    defer r.RoleMu.Unlock()
    r.Role = role
}

type ReplicaStatus int

const (
    StatusHealthy ReplicaStatus = iota
    StatusDegraded
    StatusUnhealthy
)
```

### 2.3 Queue

```go
type Queue struct {
    replica *Replica
    
    // Request buffer
    ch    chan *Request  // buffered channel; size = FM_GW_QUEUE_DEPTH_PER_REPLICA
    items int            // current queue depth
    itemsMu sync.Mutex
    
    // Priorities (if implemented)
    critical  chan *Request  // high priority
    high      chan *Request  // device registration
    default_  chan *Request  // heartbeat
    low       chan *Request  // metrics
    
    // Metrics
    droppedTotal uint64  // atomic
    waitTimeMs   uint64  // atomic
}

type Request struct {
    TraceID    string
    Method     string  // GET, POST, PUT, DELETE
    Path       string  // /api/v1/devices
    Headers    http.Header
    Body       []byte
    
    // For consistent hash
    DeviceID   string  // extracted from body or query params
    
    // Rate limiting
    ClientIP   string
    IdempotencyKey string  // for dedup
    
    // Timing
    ReceivedAt time.Time
    DeadlineAt time.Time  // ReceivedAt + timeout
}

type Response struct {
    Status     int
    Headers    http.Header
    Body       []byte
    LatencyMs  int64
}
```

### 2.4 RateLimiter

```go
type RateLimiter struct {
    perIP    map[string]*TokenBucket  // IP -> bucket
    perIPMu  sync.RWMutex
    
    perDevice map[string]*TokenBucket  // device_id -> bucket
    perDeviceMu sync.RWMutex
    
    perKey   map[string]*TokenBucket  // idempotency_key -> bucket
    perKeyMu sync.RWMutex
}

type TokenBucket struct {
    maxTokens    int64      // e.g., 1000
    tokensPerFill int64     // e.g., 1000
    fillInterval time.Duration  // e.g., 60s
    
    tokens   int64        // current tokens
    tokensMu sync.Mutex
    
    lastFill time.Time
}

func (tb *TokenBucket) TryConsume(tokens int64) bool {
    tb.tokensMu.Lock()
    defer tb.tokensMu.Unlock()
    
    tb.refill()  // add tokens based on time elapsed
    
    if tb.tokens >= tokens {
        tb.tokens -= tokens
        return true
    }
    return false
}

func (tb *TokenBucket) refill() {
    now := time.Now()
    elapsed := now.Sub(tb.lastFill)
    tokensToAdd := int64(elapsed.Seconds() / tb.fillInterval.Seconds() * float64(tb.tokensPerFill))
    tb.tokens = min(tb.maxTokens, tb.tokens+tokensToAdd)
    tb.lastFill = now
}
```

### 2.5 DiscoveryManager (Pod Discovery + Primary Election)

```go
type DiscoveryManager struct {
    replicas      []*Replica
    replicasMu    sync.RWMutex
    replicasMap   map[string]*Replica  // name -> *Replica
    
    primaryReplica *Replica
    primaryMu      sync.RWMutex
    
    podDiscoverer   PodDiscoverer
    leaseMonitor    LeaseMonitor
    
    stopChan chan struct{}
}

// PodDiscoverer discovers FM replicas from T2 etcd or static config
type PodDiscoverer interface {
    // Discover returns sorted []*Replica (by ordinal)
    Discover(ctx context.Context) ([]*Replica, error)
}

// T2 etcd implementation
type T2EtcdDiscoverer struct {
    t2Endpoint string  // "http://etcd-t2:2379"
    namespace  string  // "dashfabric"
    statefulSetName string  // "fm"
    
    client *etcd.Client
}

func (d *T2EtcdDiscoverer) Discover(ctx context.Context) ([]*Replica, error) {
    // Query /dashfabric/cluster/pods/fm-{0,1,2,...}
    // Parse each pod's address, port, ordinal
    // Return sorted by ordinal: [fm-0, fm-1, fm-2]
}

// docker-compose implementation (static)
type DockerComposeDiscoverer struct {
    replicasStr string  // "fm-0:8080,fm-1:8080,fm-2:8080"
}

func (d *DockerComposeDiscoverer) Discover(ctx context.Context) ([]*Replica, error) {
    // Parse FM_REPLICAS env var
    // Return fixed list (no dynamic changes)
}

// LeaseMonitor monitors adapter lease for primary detection
type LeaseMonitor interface {
    // Get returns current lease holder and TTL remaining
    Get(ctx context.Context) (holder string, expiresAt int64, err error)
}

// T2 etcd implementation
type T2LeaseMonitor struct {
    t2Endpoint string
    leaseKey   string  // "/dashfabric/cluster/adapter/lease"
    
    client *etcd.Client
}

func (m *T2LeaseMonitor) Get(ctx context.Context) (holder string, expiresAt int64, err error) {
    // Read lease from T2 etcd
    // Parse JSON: {holder: "fm-1", expires_at: T+15000, version: 42}
    // Return holder and expiresAt
}

// UpdatePrimary is called when lease holder changes
func (dm *DiscoveryManager) UpdatePrimary(newPrimaryName string) {
    dm.primaryMu.Lock()
    defer dm.primaryMu.Unlock()
    
    dm.replicasMu.RLock()
    defer dm.replicasMu.RUnlock()
    
    // Set all replicas to STANDBY
    for _, r := range dm.replicas {
        r.SetRole(RoleStandby)
    }
    
    // Set new primary
    if primary, ok := dm.replicasMap[newPrimaryName]; ok {
        primary.SetRole(RolePrimary)
        dm.primaryReplica = primary
    } else {
        dm.primaryReplica = nil
    }
}

// GetPrimary returns current primary or (nil, false) if unavailable
func (dm *DiscoveryManager) GetPrimary() (*Replica, bool) {
    dm.primaryMu.RLock()
    defer dm.primaryMu.RUnlock()
    
    if dm.primaryReplica != nil && dm.primaryReplica.Status == StatusHealthy {
        return dm.primaryReplica, true
    }
    return nil, false
}

// Run starts the discovery and lease monitoring loops
func (dm *DiscoveryManager) Run(ctx context.Context) {
    // Pod discovery every 10s
    go dm.runPodDiscovery(ctx)
    
    // Lease monitoring every 5s
    go dm.runLeaseMonitoring(ctx)
}

func (dm *DiscoveryManager) runPodDiscovery(ctx context.Context) {
    ticker := time.NewTicker(10 * time.Second)
    defer ticker.Stop()
    
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            replicas, err := dm.podDiscoverer.Discover(ctx)
            if err != nil {
                // Log error; keep existing replicas
                continue
            }
            
            dm.replicasMu.Lock()
            dm.replicas = replicas
            dm.replicasMap = make(map[string]*Replica)
            for _, r := range replicas {
                dm.replicasMap[r.Name] = r
            }
            dm.replicasMu.Unlock()
        }
    }
}

func (dm *DiscoveryManager) runLeaseMonitoring(ctx context.Context) {
    ticker := time.NewTicker(5 * time.Second)
    defer ticker.Stop()
    
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            holder, _, err := dm.leaseMonitor.Get(ctx)
            if err != nil {
                // Log error; keep existing primary
                continue
            }
            
            dm.primaryMu.RLock()
            oldPrimary := dm.primaryReplica
            dm.primaryMu.RUnlock()
            
            if oldPrimary == nil || oldPrimary.Name != holder {
                dm.UpdatePrimary(holder)
                // Log: "Primary changed from {old} to {new}"
            }
        }
    }
}
```

### 2.6 LoadBalancer Interface

```go
type LoadBalancer interface {
    SelectReplica(deviceID string, replicas []*Replica) *Replica
    SelectReplicaForStream(replicas []*Replica) *Replica
}

// Consistent hash implementation
type ConsistentHashLB struct {
    hash hash.Hash32
}

func (lb *ConsistentHashLB) SelectReplica(deviceID string, replicas []*Replica) *Replica {
    h := fnv.New32a()
    h.Write([]byte(deviceID))
    hashValue := h.Sum32()
    idx := hashValue % uint32(len(replicas))
    return replicas[idx]
}

// Least connections implementation
type LeastConnectionsLB struct{}

func (lb *LeastConnectionsLB) SelectReplica(deviceID string, replicas []*Replica) *Replica {
    // Not used; just round-robin
    return lb.SelectReplicaForStream(replicas)
}

func (lb *LeastConnectionsLB) SelectReplicaForStream(replicas []*Replica) *Replica {
    minConnections := math.MaxInt
    selected := replicas[0]
    for _, r := range replicas {
        r.activeConnMu.RLock()
        conns := r.activeConnections
        r.activeConnMu.RUnlock()
        if conns < minConnections {
            minConnections = conns
            selected = r
        }
    }
    return selected
}
```

### 2.6 HealthChecker

```go
type HealthChecker struct {
    interval   time.Duration
    timeout    time.Duration
    unhealthyThreshold int
    healthyThreshold   int
    
    httpClient *http.Client
    
    stopChan chan struct{}
}

type HealthStatus struct {
    Status  string  // "OK"
    Mode    string  // "primary", "standby"
    Uptime  int64   // seconds
    DpuCount int     // count
    T1Latency int64  // ms
}

func (hc *HealthChecker) Check(replica *Replica) bool {
    ctx, cancel := context.WithTimeout(context.Background(), hc.timeout)
    defer cancel()
    
    url := fmt.Sprintf("http://%s/api/v1/health", replica.Address)
    req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
    
    resp, err := hc.httpClient.Do(req)
    if err != nil {
        replica.updateStatus(StatusUnhealthy)
        return false
    }
    defer resp.Body.Close()
    
    if resp.StatusCode == 200 {
        var status HealthStatus
        json.NewDecoder(resp.Body).Decode(&status)
        replica.updateStatus(StatusHealthy)
        return true
    }
    
    replica.updateStatus(StatusUnhealthy)
    return false
}
```

## 3. Request Handling Flow

### 3.1 HTTP Request Handler (Primary-Aware)

```go
type RequestType int

const (
    RequestTypeUnknown RequestType = iota
    RequestTypeRegistration  // POST /api/v1/devices
    RequestTypeQuery         // GET /api/v1/devices
    RequestTypeHeartbeat     // POST /api/v1/heartbeat
)

func (gw *Gateway) handleHTTPRequest(w http.ResponseWriter, r *http.Request) {
    traceID := r.Header.Get("X-Trace-ID")
    if traceID == "" {
        traceID = generateTraceID()
    }
    
    span, ctx := tracer.StartSpan(context.Background(), "gateway.request", 
        attribute.String("trace_id", traceID),
        attribute.String("method", r.Method),
        attribute.String("path", r.Path),
    )
    defer span.End()
    
    // Step 1: Rate limiting
    clientIP := extractClientIP(r)
    if !gw.rateLimiter.AllowPerIP(clientIP) {
        http.Error(w, "rate limited", http.StatusTooManyRequests)
        gw.metrics.recordRateLimitViolation("per_ip")
        return
    }
    
    // Step 2: Parse request type
    reqType := gw.parseRequestType(r)
    
    // Step 3: Primary-aware routing
    var replica *Replica
    
    switch reqType {
    case RequestTypeRegistration, RequestTypeQuery, RequestTypeHeartbeat:
        // These must go to PRIMARY only
        var ok bool
        replica, ok = gw.discoveryManager.GetPrimary()
        if !ok {
            // Primary unavailable
            gw.primaryMu.Lock()
            if gw.primaryUnavailableAt == nil {
                now := time.Now()
                gw.primaryUnavailableAt = &now
            }
            elapsed := time.Since(*gw.primaryUnavailableAt)
            gw.primaryMu.Unlock()
            
            if elapsed > gw.primaryWaitTimeout {
                // Waited too long; give up
                http.Error(w, "primary election timeout", http.StatusServiceUnavailable)
                return
            }
            
            // Return 503 with Retry-After
            w.Header().Set("Retry-After", "10")
            http.Error(w, "primary unavailable; primary election in progress", http.StatusServiceUnavailable)
            return
        }
        
        // Clear unavailable timestamp
        gw.primaryMu.Lock()
        gw.primaryUnavailableAt = nil
        gw.primaryMu.Unlock()
        
    default:
        // For unknown types: select via load balancer
        gw.discoveryManager.replicasMu.RLock()
        replicas := gw.discoveryManager.replicas
        gw.discoveryManager.replicasMu.RUnlock()
        
        if len(replicas) == 0 {
            http.Error(w, "no replicas available", http.StatusServiceUnavailable)
            return
        }
        
        deviceID := extractDeviceID(r)
        replica = gw.lb.SelectReplica(deviceID, replicas)
    }
    
    if replica == nil || replica.Status == StatusUnhealthy {
        http.Error(w, "replica unhealthy", http.StatusServiceUnavailable)
        return
    }
    
    // Step 4: Queue request
    deviceID := extractDeviceID(r)
    request := &Request{
        TraceID: traceID,
        Method: r.Method,
        Path: r.Path,
        Headers: r.Header,
        Body: readBody(r),
        DeviceID: deviceID,
        ClientIP: clientIP,
        ReceivedAt: time.Now(),
        DeadlineAt: time.Now().Add(30 * time.Second),
    }
    
    queue := gw.queues[replica.Name]
    select {
    case queue.ch <- request:
        // Queued successfully
        gw.metrics.recordQueueWait(replica.Name, time.Since(request.ReceivedAt))
    default:
        // Queue full; backpressure
        http.Error(w, "service overloaded", http.StatusServiceUnavailable)
        gw.metrics.recordQueueDropped(replica.Name)
        return
    }
    
    // Step 5: Forward request to replica (blocking)
    span2, _ := tracer.StartSpan(ctx, "replica.request", 
        attribute.String("replica", replica.Name),
        attribute.String("replica_role", roleString(replica)))
    
    response := gw.forwardRequest(replica, request)
    
    span2.End()
    
    // Step 6: Return response
    w.WriteHeader(response.Status)
    for k, v := range response.Headers {
        w.Header()[k] = v
    }
    w.Write(response.Body)
    
    gw.metrics.recordRequest(r.Method, r.Path, response.Status, replica.Name, response.LatencyMs)
}

func (gw *Gateway) parseRequestType(r *http.Request) RequestType {
    if r.Method == "POST" && r.URL.Path == "/api/v1/devices" {
        return RequestTypeRegistration
    }
    if r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/api/v1/devices") {
        return RequestTypeQuery
    }
    if r.Method == "POST" && r.URL.Path == "/api/v1/heartbeat" {
        return RequestTypeHeartbeat
    }
    return RequestTypeUnknown
}
```

### 3.2 Request Forward (HTTP)

```go
func (gw *Gateway) forwardRequest(replica *Replica, req *Request) *Response {
    startTime := time.Now()
    
    // Create HTTP request
    url := fmt.Sprintf("http://%s%s", replica.Address, req.Path)
    httpReq, _ := http.NewRequest(req.Method, url, bytes.NewReader(req.Body))
    
    // Copy headers
    for k, v := range req.Headers {
        httpReq.Header[k] = v
    }
    httpReq.Header.Set("X-Trace-ID", req.TraceID)
    
    // Execute with timeout
    ctx, cancel := context.WithDeadline(context.Background(), req.DeadlineAt)
    defer cancel()
    
    httpReq = httpReq.WithContext(ctx)
    
    httpResp, err := replica.httpClient.Do(httpReq)
    latencyMs := time.Since(startTime).Milliseconds()
    
    if err != nil {
        gw.metrics.recordForwardError(replica.Name, err)
        return &Response{
            Status: http.StatusBadGateway,
            Body: []byte("replica unreachable"),
        }
    }
    
    defer httpResp.Body.Close()
    body, _ := io.ReadAll(httpResp.Body)
    
    return &Response{
        Status: httpResp.StatusCode,
        Headers: httpResp.Header,
        Body: body,
        LatencyMs: latencyMs,
    }
}
```

### 3.3 gRPC Stream Handler (Load-Balanced)

```go
func (gw *Gateway) Subscribe(req *pb.SubscribeRequest, stream pb.CB_SubscribeServer) error {
    traceID := extractTraceIDFromMetadata(stream.Context())
    
    // Select replica using least-connections (load-balance across ALL healthy replicas)
    gw.discoveryManager.replicasMu.RLock()
    replica := gw.lb.SelectReplicaForStream(gw.discoveryManager.replicas)
    gw.discoveryManager.replicasMu.RUnlock()
    
    if replica == nil || replica.Status == StatusUnhealthy {
        return status.Error(codes.Unavailable, "no healthy replicas")
    }
    
    // Track active connection
    replica.incrActiveConnections()
    defer replica.decrActiveConnections()
    
    gw.metrics.recordGrpcStreamStarted(replica.Name)
    
    // Dial replica (reuse connection from pool)
    replicaConn, err := grpc.Dial(replica.GrpcAddr, grpc.WithInsecure())
    if err != nil {
        gw.metrics.recordForwardError(replica.Name, err)
        return status.Error(codes.Unavailable, "cannot reach replica")
    }
    defer replicaConn.Close()
    
    // Open stream to replica
    replicaClient := pb.NewCBClient(replicaConn)
    replicaStream, err := replicaClient.Subscribe(context.Background(), req)
    if err != nil {
        return status.Error(codes.Internal, "replica subscribe failed")
    }
    
    // Proxy stream (bidirectional)
    // Client → replica
    go func() {
        for {
            req, err := stream.Recv()
            if err == io.EOF {
                replicaStream.CloseSend()
                return
            }
            replicaStream.Send(req)
        }
    }()
    
    // Replica → client
    for {
        resp, err := replicaStream.Recv()
        if err == io.EOF {
            return nil
        }
        if err != nil {
            return err
        }
        stream.Send(resp)
    }
}
```

## 4. Concurrency Model

### 4.1 Replica Synchronization

**Replica state is protected by:**
- `sync.RWMutex` for Status (multiple readers, single writer from health checker)
- `sync.RWMutex` for activeConnections (multiple readers, writers for stream create/destroy)
- `sync.atomic` for requestsTotal, latencyMs (wait-free)

**Replicas slice is protected by:**
- `sync.RWMutex` for adding/removing replicas (rare; every 10s)
- Read lock held during request handling (fast)

### 4.2 Queue Synchronization

**Per-replica queue:**
- Unbuffered channel (or buffered; both work)
- Sender: HTTP handler (caller puts request in queue)
- Receiver: Request processor goroutine (pulls from queue, forwards to replica)

**No explicit locking needed; channels handle synchronization.**

### 4.3 Rate Limiter Synchronization

**Token bucket per-IP:**
- `sync.Mutex` protects tokens and lastFill
- Held briefly (<1 µs); fast path

## 5. Error Handling

### 5.1 Primary Unavailable

```go
// When primary is unavailable, return 503 with Retry-After
// This occurs when:
// - Adapter lease holder died
// - Lease expired but new primary not yet elected
// - Gateway hasn't discovered new primary yet

if primary, ok := gw.discoveryManager.GetPrimary(); !ok {
    gw.primaryMu.Lock()
    if gw.primaryUnavailableAt == nil {
        now := time.Now()
        gw.primaryUnavailableAt = &now
    }
    elapsed := time.Since(*gw.primaryUnavailableAt)
    gw.primaryMu.Unlock()
    
    if elapsed > 20*time.Second {
        // Waited 20s for election; give up
        return &Response{
            Status: http.StatusServiceUnavailable,
            Headers: http.Header{
                "Retry-After": []string{"10"},
            },
            Body: []byte("primary election timeout"),
        }
    }
    
    return &Response{
        Status: http.StatusServiceUnavailable,
        Headers: http.Header{
            "Retry-After": []string{"10"},
        },
        Body: []byte("primary unavailable; election in progress"),
    }
}
```

### 5.2 No Primary Elected (during startup)

```go
// At gateway startup, before any primary lease is detected:
// - getPrimary() returns (nil, false)
// - Return 503 Service Unavailable
// - Retry-After: 5s (waiting for first lease read)

return &Response{
    Status: http.StatusServiceUnavailable,
    Headers: http.Header{
        "Retry-After": []string{"5"},
    },
    Body: []byte("primary not yet elected; cluster starting up"),
}
```

### 5.3 Replica Unhealthy (health check failure)

```go
if replica.Status == StatusUnhealthy {
    // Return 503; don't queue
    return &Response{
        Status: http.StatusServiceUnavailable,
        Body: []byte("replica unhealthy; please retry"),
    }
}
```

### 5.2 Queue Full

```go
select {
case queue.ch <- request:
    // OK
default:
    // Queue full
    return &Response{
        Status: http.StatusServiceUnavailable,
        Headers: http.Header{
            "Retry-After": []string{"5"},
        },
        Body: []byte("service overloaded"),
    }
}
```

### 5.3 Request Timeout

```go
ctx, cancel := context.WithDeadline(context.Background(), req.DeadlineAt)
defer cancel()

httpReq = httpReq.WithContext(ctx)
resp, err := httpClient.Do(httpReq)

if err == context.DeadlineExceeded {
    return &Response{
        Status: http.StatusGatewayTimeout,
        Body: []byte("replica timeout"),
    }
}
```

## 6. Configuration Defaults

| Parameter | Default | Type | Purpose |
|-----------|---------|------|---------|
| `FM_GW_HTTP_PORT` | 8080 | int | HTTP listen port |
| `FM_GW_GRPC_PORT` | 5051 | int | gRPC listen port |
| `FM_GW_METRICS_PORT` | 9090 | int | Prometheus metrics port |
| `FM_GW_T2_ENDPOINT` | localhost:2379 | string | T2 etcd endpoint |
| `FM_GW_REPLICA_NAMESPACE` | dashfabric | string | K8s namespace |
| `FM_GW_REPLICA_STATEFULSET` | fm | string | StatefulSet name |
| `FM_GW_REPLICA_DISCOVERY_INTERVAL_SECONDS` | 10 | int | Replica discovery interval |
| `FM_GW_LB_ALGORITHM` | consistent_hash | string | Load balancer algorithm |
| `FM_GW_QUEUE_DEPTH_PER_REPLICA` | 1000 | int | Queue size per replica |
| `FM_GW_REQUEST_TIMEOUT_SECONDS` | 30 | int | Request timeout |
| `FM_GW_HEALTH_CHECK_INTERVAL_SECONDS` | 10 | int | Health check interval |
| `FM_GW_HEALTH_CHECK_TIMEOUT_SECONDS` | 5 | int | Health check timeout |
| `FM_GW_RATE_LIMIT_PER_IP_PER_MIN` | 1000 | int | IP-based rate limit |

## 7. Performance Considerations

### 7.1 Latency Budget

**Target: <10ms for gateway overhead (95th percentile)**

```
Request path latency breakdown:
├─ HTTP parse + extract: 1ms
├─ Rate limit check: 0.1ms
├─ Load balancer select: 0.05ms
├─ Queue enqueue: 0.05ms
├─ Channel send/receive: 0.1ms
├─ Replica forward (HTTP): 5ms (varies; network-dependent)
└─ Response write: 0.7ms
Total: ~7ms (excluding replica forward time)
```

### 7.2 Memory Usage

**Assumptions:**
- 1000 requests/sec
- 10ms average queue time
- 10 requests per queue (1000 requests/sec × 10ms)

```
Memory per request: ~1KB (headers, trace ID, device ID, body pointer)
Total queue memory: 10 requests × 3 replicas × 1KB = 30KB

Metrics: ~1MB (counters, histograms)
Trace buffers: ~10MB (if tracing enabled; sample rate = 0.1)

Total memory footprint: ~50-100MB
```

### 7.3 CPU Usage

**Single-core baseline:**
- HTTP parsing: negligible
- Rate limiting: <1% per 10k req/s
- Load balancer select: <1% per 100k req/s
- Queue operations: <1% per 1M req/s

**Expect <10% CPU for 10k req/s on modern CPU.**

## 8. References

- `fm-gw-architecture-hld.md` — High-level design
- `fm-gw-implementation-planner.md` — Implementation phases
- `fm-api-gateway-design.md` — Gateway requirements
