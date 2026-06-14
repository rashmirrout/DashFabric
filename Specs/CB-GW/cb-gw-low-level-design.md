# CB-Gateway: Low-Level Design (LLD)

> **Status:** Draft v1
> **Module:** CB-Gateway (cb-gw)
> **Scope:** Internal architecture, data structures, algorithms, concurrency model
> **Audience:** CB-GW implementers, code reviewers

## 1. Process Architecture

```
cb-gw process
├── Main goroutine
│   ├── Config load
│   ├── Signal handling (SIGTERM, SIGINT)
│   └── Lifecycle management
├── gRPC listener goroutine
│   ├── Accept connections
│   ├── Demultiplex Subscribe/Publish
│   └─ Stream proxying
├── REST listener goroutine
│   ├── Accept HTTP connections
│   ├── Demultiplex observability endpoints
│   └── Response serialization (JSON)
├── Pod discovery goroutine
│   ├── Poll T2 etcd every 10s
│   ├── Update in-memory replica list
│   └── Trigger health checks on new replicas
├── Health checker goroutine
│   ├── Poll each replica every 10s
│   ├── Update replica status (healthy/unhealthy)
│   └── Emit metrics
├── Metrics exporter goroutine
│   ├── Bind HTTP server on :9090
│   ├─ Expose /metrics endpoint (Prometheus)
│   └── Aggregate stats from all components
└── Graceful shutdown handler
    ├── On SIGTERM: stop accepting new requests
    ├─ Drain in-flight requests (timeout 30s)
    ├─ Close all connections
    └─ Exit cleanly
```

## 2. Data Structures

### 2.1 Gateway

```go
type Gateway struct {
    // Configuration
    config *Config
    
    // Replica management
    replicas     []*Replica
    replicasMu   sync.RWMutex
    replicasMap  map[string]*Replica
    
    // Load balancer
    lb LoadBalancer
    
    // gRPC/REST servers
    grpcServer *grpc.Server
    restServer *http.Server
    
    // Queues
    queues map[string]*Queue
    queuesMu sync.RWMutex
    
    // Rate limiter
    rateLimiter *RateLimiter
    
    // Health checker
    healthChecker *HealthChecker
    
    // Metrics
    metrics *Metrics
    
    // Shutdown signal
    shutdownChan chan struct{}
    shutdownOnce sync.Once
}
```

### 2.2 Replica

```go
type Replica struct {
    Name      string
    Address   string        // "10.0.0.1:5052"
    RestAddr  string        // "10.0.0.1:8081"
    
    // State
    Status    ReplicaStatus  // HEALTHY, UNHEALTHY
    StatusMu  sync.RWMutex
    
    // Connection pooling
    grpcConn   *grpc.ClientConn
    httpClient *http.Client
    
    // Metrics
    activeConnections int  // gRPC Subscribe streams
    activeConnMu      sync.RWMutex
    requestsTotal     uint64
    latencyMs         uint64
    
    // Last health check
    lastHealthCheckTime time.Time
    consecutiveFailures int
}

type ReplicaStatus int

const (
    StatusHealthy ReplicaStatus = iota
    StatusUnhealthy
)
```

### 2.3 Queue

```go
type Queue struct {
    replica *Replica
    
    // Request buffer
    ch    chan *Request
    items int
    itemsMu sync.Mutex
    
    // Metrics
    droppedTotal uint64
    waitTimeMs   uint64
}

type Request struct {
    TraceID    string
    RpcType    RpcType  // SUBSCRIBE, PUBLISH
    Metadata   map[string]string
    Body       []byte
    
    // Rate limiting
    ClientID   string
    
    // Timing
    ReceivedAt time.Time
    DeadlineAt time.Time
}

type RpcType int

const (
    RpcTypeSubscribe RpcType = iota
    RpcTypePublish
)
```

### 2.4 PodDiscoverer & HealthChecker

```go
type PodDiscoverer interface {
    Discover(ctx context.Context) ([]*Replica, error)
}

type T2EtcdDiscoverer struct {
    t2Endpoint string
    client     *etcd.Client
}

func (d *T2EtcdDiscoverer) Discover(ctx context.Context) ([]*Replica, error) {
    // Query /dashfabric/cluster/pods/cb-*
    // Return sorted []*Replica
}

type DockerComposeDiscoverer struct {
    replicasStr string  // "cb-0:5052,cb-1:5052"
}

type HealthChecker struct {
    interval   time.Duration
    timeout    time.Duration
    httpClient *http.Client
    stopChan   chan struct{}
}

func (hc *HealthChecker) Check(replica *Replica) bool {
    // GET /api/v1/health
    // Mark HEALTHY or UNHEALTHY
}
```

### 2.5 LoadBalancer

```go
type LoadBalancer interface {
    SelectReplicaForSubscribe(replicas []*Replica) *Replica
    SelectReplicaForPublish(replicas []*Replica) *Replica
    SelectReplicaForRest(replicas []*Replica) *Replica
}

type LeastConnectionsLB struct{}

func (lb *LeastConnectionsLB) SelectReplicaForSubscribe(replicas []*Replica) *Replica {
    minConns := math.MaxInt
    selected := replicas[0]
    for _, r := range replicas {
        r.activeConnMu.RLock()
        conns := r.activeConnections
        r.activeConnMu.RUnlock()
        if conns < minConns {
            minConns = conns
            selected = r
        }
    }
    return selected
}

type RoundRobinLB struct {
    counter uint64
}

func (lb *RoundRobinLB) SelectReplicaForPublish(replicas []*Replica) *Replica {
    idx := atomic.AddUint64(&lb.counter, 1) % uint64(len(replicas))
    return replicas[idx]
}
```

### 2.6 RateLimiter

```go
type RateLimiter struct {
    perClient map[string]*TokenBucket
    perClientMu sync.RWMutex
}

type TokenBucket struct {
    maxTokens    int64
    tokensPerFill int64
    fillInterval time.Duration
    
    tokens   int64
    tokensMu sync.Mutex
    lastFill time.Time
}

func (tb *TokenBucket) TryConsume(tokens int64) bool {
    tb.tokensMu.Lock()
    defer tb.tokensMu.Unlock()
    
    tb.refill()
    if tb.tokens >= tokens {
        tb.tokens -= tokens
        return true
    }
    return false
}
```

## 3. gRPC Request Handling

### 3.1 Subscribe Handler

```go
func (gw *Gateway) Subscribe(req *pb.SubscribeRequest, stream grpc.ServerStream) error {
    traceID := extractTraceID(stream.Context())
    clientID := extractClientID(stream.Context())
    
    // Select replica using least-connections
    gw.replicasMu.RLock()
    replica := gw.lb.SelectReplicaForSubscribe(gw.replicas)
    gw.replicasMu.RUnlock()
    
    if replica == nil || replica.Status == StatusUnhealthy {
        return status.Error(codes.Unavailable, "no healthy replicas")
    }
    
    // Track active connection
    replica.incrActiveConnections()
    defer replica.decrActiveConnections()
    
    gw.metrics.recordSubscribeStarted(replica.Name)
    
    // Dial replica
    replicaConn, err := grpc.Dial(replica.Address)
    if err != nil {
        return status.Error(codes.Unavailable, "cannot reach replica")
    }
    defer replicaConn.Close()
    
    // Open stream to replica
    replicaClient := pb.NewCBClient(replicaConn)
    replicaStream, err := replicaClient.Subscribe(context.Background(), req)
    if err != nil {
        return status.Error(codes.Internal, "replica subscribe failed")
    }
    
    // Proxy stream bidirectionally
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

### 3.2 Publish Handler

```go
func (gw *Gateway) Publish(ctx context.Context, req *pb.PublishRequest) (*pb.PublishResponse, error) {
    traceID := extractTraceID(ctx)
    clientID := extractClientID(ctx)
    
    // Rate limiting
    if !gw.rateLimiter.AllowPerClient(clientID) {
        return nil, status.Error(codes.ResourceExhausted, "rate limited")
    }
    
    // Select replica (round-robin or least-conn)
    gw.replicasMu.RLock()
    replica := gw.lb.SelectReplicaForPublish(gw.replicas)
    gw.replicasMu.RUnlock()
    
    if replica == nil || replica.Status == StatusUnhealthy {
        return nil, status.Error(codes.Unavailable, "no healthy replicas")
    }
    
    // Queue request
    request := &Request{
        TraceID: traceID,
        RpcType: RpcTypePublish,
        Body: proto.Marshal(req),
        ClientID: clientID,
        ReceivedAt: time.Now(),
        DeadlineAt: time.Now().Add(30 * time.Second),
    }
    
    queue := gw.queues[replica.Name]
    select {
    case queue.ch <- request:
        gw.metrics.recordQueueWait(replica.Name, time.Since(request.ReceivedAt))
    default:
        return nil, status.Error(codes.Unavailable, "service overloaded")
    }
    
    // Forward to replica
    startTime := time.Now()
    replicaConn := replica.grpcConn
    replicaClient := pb.NewCBClient(replicaConn)
    
    resp, err := replicaClient.Publish(ctx, req)
    latencyMs := time.Since(startTime).Milliseconds()
    
    gw.metrics.recordPublish(replica.Name, latencyMs, err == nil)
    
    return resp, err
}
```

## 4. REST Observability Handlers

### 4.1 GET /api/v1/topics

```go
func (gw *Gateway) handleTopics(w http.ResponseWriter, r *http.Request) {
    clientID := extractClientIP(r)
    
    if !gw.rateLimiter.AllowPerClient(clientID) {
        http.Error(w, "rate limited", http.StatusTooManyRequests)
        return
    }
    
    gw.replicasMu.RLock()
    replica := gw.lb.SelectReplicaForRest(gw.replicas)
    gw.replicasMu.RUnlock()
    
    if replica == nil {
        http.Error(w, "no healthy replicas", http.StatusServiceUnavailable)
        return
    }
    
    // Forward to replica REST endpoint
    url := fmt.Sprintf("http://%s/api/v1/topics", replica.RestAddr)
    resp, err := replica.httpClient.Get(url)
    if err != nil {
        http.Error(w, "replica unreachable", http.StatusBadGateway)
        return
    }
    defer resp.Body.Close()
    
    body, _ := io.ReadAll(resp.Body)
    w.WriteHeader(resp.StatusCode)
    w.Write(body)
}
```

## 5. Concurrency Model

### 5.1 Replica Synchronization

- `RWMutex` for Status (multiple readers, single writer from health checker)
- `RWMutex` for activeConnections
- `atomic` for requestsTotal, latencyMs

### 5.2 Queue Synchronization

- Unbuffered channel handles synchronization
- One sender (HTTP handler) queues requests
- Multiple receivers (per-replica workers) process

### 5.3 Rate Limiter Synchronization

- `Mutex` per token bucket
- Held briefly; fast path (<1 µs)

## 6. Configuration Defaults

| Parameter | Default | Purpose |
|-----------|---------|---------|
| `CB_GW_GRPC_PORT` | 5052 | gRPC listen port |
| `CB_GW_REST_PORT` | 8081 | REST listen port |
| `CB_GW_METRICS_PORT` | 9090 | Prometheus metrics port |
| `CB_GW_T2_ENDPOINT` | localhost:2379 | T2 etcd endpoint |
| `CB_GW_POD_DISCOVERY_INTERVAL_SECONDS` | 10 | Replica discovery interval |
| `CB_GW_HEALTH_CHECK_INTERVAL_SECONDS` | 10 | Health check interval |
| `CB_GW_QUEUE_DEPTH_PER_REPLICA` | 1000 | Request queue size |
| `CB_GW_REQUEST_TIMEOUT_SECONDS` | 30 | Request timeout |
| `CB_GW_RATE_LIMIT_PER_CLIENT_PER_MIN` | 1000 | Per-client rate limit |

## 7. Performance Considerations

### 7.1 Latency Budget

**Target: <10ms for gateway overhead (p95)**

```
gRPC Subscribe latency:
├─ Load balancer select: 0.05ms
├─ Replica connection: 1ms (reused)
└─ Stream proxy: <1ms

gRPC Publish latency:
├─ Rate limit check: 0.1ms
├─ Load balancer select: 0.05ms
├─ Queue enqueue: 0.05ms
└─ Forward to replica: 5ms (varies)
Total: ~5ms

REST query latency:
├─ Rate limit check: 0.1ms
├─ Load balancer select: 0.05ms
└─ Forward to replica: 50-100ms (REST is slower than gRPC)
```

### 7.2 Memory Usage

```
Per-replica state: ~100 bytes
Replicas (3): 300 bytes
Per-connection tracking: ~50 bytes per Subscribe stream
Queues (3 × 1000 slots): ~3MB

Total baseline: <5MB (+ queue memory as requests arrive)
```

### 7.3 CPU Usage

- Load balancer select: <1% per 100k req/s
- Rate limiting: <1% per 10k req/s
- Queue operations: <1% per 1M req/s

**Expect <5% CPU for 10k req/s on modern CPU.**
