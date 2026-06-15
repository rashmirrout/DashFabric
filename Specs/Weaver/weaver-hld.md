# Weaver: High-Level Design (HLD)

> **Status:** Draft v1  
> **Module:** Weaver Gateway Platform  
> **Scope:** Architecture, components, data flows, deployment model  
> **Audience:** Architects, Senior Engineers

---

## Table of Contents

1. [Architecture Overview](#1-architecture-overview)
2. [Core Components](#2-core-components)
3. [Request Flow (Happy Path)](#3-request-flow-happy-path)
4. [Failure Handling](#4-failure-handling)
5. [Configuration Model](#5-configuration-model)
6. [Deployment Architecture](#6-deployment-architecture)
7. [Extensibility Points](#7-extensibility-points)

---

## 1. Architecture Overview

### 1.1 Layered Architecture

```
┌──────────────────────────────────────────────────────────┐
│ Layer 7: Configuration & Schema Registry                │
│ (YAML parsing, validation, hot-reload)                  │
├──────────────────────────────────────────────────────────┤
│ Layer 6: Plugin Manager & Registry                      │
│ (Discovery, Health, LB, Handlers, Auth)                 │
├──────────────────────────────────────────────────────────┤
│ Layer 5: Request Pipeline                               │
│ (Authenticate → Authorize → Rate-limit → Route)         │
├──────────────────────────────────────────────────────────┤
│ Layer 4: Reliability Patterns                           │
│ (Circuit Breaker, Retry, Timeout, Queue)                │
├──────────────────────────────────────────────────────────┤
│ Layer 3: Protocol Listeners                             │
│ (gRPC Server, HTTP Server, REST Routes)                 │
├──────────────────────────────────────────────────────────┤
│ Layer 2: Data Plane                                     │
│ (Connection pooling, socket I/O)                        │
├──────────────────────────────────────────────────────────┤
│ Layer 1: Observability                                  │
│ (Metrics, Tracing, Logging, Debug API)                  │
└──────────────────────────────────────────────────────────┘
```

### 1.2 Component Interaction Map

```
                    ┌──────────────────────┐
                    │   Configuration      │
                    │   (YAML file)        │
                    └──────────┬───────────┘
                               │
                    ┌──────────▼───────────┐
                    │ Plugin Manager       │
                    │ Registry             │
                    └──────────┬───────────┘
                               │
        ┌──────────────────────┼──────────────────────┐
        │                      │                      │
        ▼                      ▼                      ▼
    ┌────────┐           ┌────────┐            ┌─────────┐
    │Discovery           │Health  │            │Load     │
    │Plugins │           │Checker │            │Balancer │
    └──┬─────┘           └────┬───┘            └────┬────┘
       │                      │                     │
       └──────────────────────┼─────────────────────┘
                              │
                    ┌─────────▼──────────┐
                    │ Replica Manager    │
                    │ (holds all replicas)
                    └─────────┬──────────┘
                              │
        ┌─────────────────────┼─────────────────────┐
        │                     │                     │
        ▼                     ▼                     ▼
    ┌────────┐           ┌────────┐            ┌────────┐
    │Replica │           │Replica │            │Replica │
    │fm-0    │           │fm-1    │            │fm-2    │
    └────────┘           └────────┘            └────────┘
```

### 1.3 Request Processing Pipeline

```
CLIENT REQUEST
    │
    ▼
┌───────────────────────────┐
│ 1. LISTENER (gRPC/HTTP)   │
│    Accept connection      │
└───────────┬───────────────┘
            │
            ▼
┌───────────────────────────┐
│ 2. EXTRACT CONTEXT        │
│    Parse metadata         │
│    Extract trace context  │
└───────────┬───────────────┘
            │
            ▼
┌───────────────────────────┐
│ 3. AUTHENTICATE           │
│    Verify identity        │
└───────────┬───────────────┘
            │
            ▼
┌───────────────────────────┐
│ 4. AUTHORIZE              │
│    Check permissions      │
└───────────┬───────────────┘
            │
            ▼
┌───────────────────────────┐
│ 5. RATE LIMIT             │
│    Check token bucket     │
└───────────┬───────────────┘
            │
            ▼
┌───────────────────────────┐
│ 6. ROUTE                  │
│    Select replica (LB)    │
└───────────┬───────────────┘
            │
            ▼
┌───────────────────────────┐
│ 7. FORWARD                │
│    To selected replica    │
│    (via connection pool)  │
└───────────┬───────────────┘
            │
            ▼
┌───────────────────────────┐
│ 8. RECEIVE RESPONSE       │
│    From replica           │
└───────────┬───────────────┘
            │
            ▼
┌───────────────────────────┐
│ 9. RECORD METRICS         │
│    Latency, status        │
│    Export trace span      │
└───────────┬───────────────┘
            │
            ▼
┌───────────────────────────┐
│ 10. RETURN RESPONSE       │
│     To client             │
└───────────────────────────┘
```

---

## 2. Core Components

### 2.1 Pod Discoverer (Pluggable)

**Interface:**
```go
type PodDiscoverer interface {
    Discover(ctx context.Context) ([]*Replica, error)
}
```

**Implementations:**

| Type | Use Case | Details |
|------|----------|---------|
| **etcd** | FM, CB | Query `/dashfabric/cluster/pods/*`; poll every 10s |
| **Consul** | Distributed systems | Query Consul service catalog; health integrated |
| **Kubernetes API** | K8s native | Query StatefulSets, DaemonSets in cluster |
| **DNS** | Simple topologies | Resolve DNS A records; SRV record support |
| **Static list** | Development | Parse env var or config file |
| **Custom** | Domain-specific | User-provided plugin |

**Discovery Loop:**
```
Startup:
  1. Call PodDiscoverer.Discover()
  2. Parse response → replicas[]
  3. Store in memory
  4. Set all to HEALTHY initially

Every 10 seconds:
  1. Call PodDiscoverer.Discover()
  2. Compare with cached replicas
  3. Add new replicas; remove deleted ones
  4. Trigger health checks on new replicas
  5. Emit metrics (replica count)
```

---

### 2.2 Health Checker (Pluggable)

**Interface:**
```go
type HealthChecker interface {
    Check(ctx context.Context, replica *Replica) (healthy bool, err error)
}
```

**Implementations:**

| Type | Use Case | Details |
|------|----------|---------|
| **HTTP** | REST endpoints | GET `/api/v1/health`; expect 200 OK |
| **gRPC** | gRPC services | Call `/grpc.health.v1.Health/Check` |
| **TCP** | Simple connectivity | Connect to port; check response |
| **Custom** | Domain-specific | User-defined health logic |

**Health Check Loop:**
```
Every 10 seconds:
  For each replica in replicas[]:
    1. Call HealthChecker.Check(replica)
    2. If success (healthy):
         ├─ Set status = HEALTHY
         └─ Reset consecutive failures
    3. If fail (unhealthy):
         ├─ Increment consecutive failures
         ├─ If consecutive_failures >= 3:
         │   ├─ Set status = UNHEALTHY
         │   └─ Emit alert
         └─ Record latency metric

  4. Check panic mode:
     ├─ Count healthy replicas
     └─ If <= 50%: enter PANIC mode
```

**State Machine:**
```
        ┌──────────────┐
        │    START     │
        └────────┬─────┘
                 │
                 ▼
        ┌──────────────────┐
        │    HEALTHY       │
        └────────┬─────────┘
                 │
         [3 consecutive failures]
                 │
                 ▼
        ┌──────────────────┐
        │   UNHEALTHY      │
        └────────┬─────────┘
                 │
          [1 success]
                 │
                 ▼
        ┌──────────────────┐
        │    HEALTHY       │
        └──────────────────┘
```

---

### 2.3 Load Balancer (Pluggable, 8 Strategies)

**Interface:**
```go
type LoadBalancer interface {
    Select(ctx context.Context, replicas []*Replica) (*Replica, error)
}
```

**Strategy Selection Logic:**
```
Configuration specifies strategy (e.g., "least_connections")
  │
  ▼
LoadBalancer.Select(healthy_replicas)
  │
  ├─ Strategy: LEAST_CONNECTIONS
  │   → Count active connections per replica
  │   → Return replica with minimum connections
  │
  ├─ Strategy: ROUND_ROBIN
  │   → Rotate counter
  │   → Return replicas[counter % len(replicas)]
  │
  ├─ Strategy: CONSISTENT_HASH
  │   → Hash client_id
  │   → Map to replica ring
  │   → Return consistent replica
  │
  ├─ Strategy: WEIGHTED
  │   → Get replica weights
  │   → Weighted random selection
  │   → Return selected replica
  │
  ├─ Strategy: RANDOM
  │   → Math.random()
  │   → Return replicas[random_index]
  │
  ├─ Strategy: STICKY
  │   → Hash client_id + TTL
  │   → Return same replica within TTL
  │
  ├─ Strategy: RESOURCE_AWARE
  │   → Query replica metrics (CPU, memory)
  │   → Return replica with most available resources
  │
  └─ Strategy: CUSTOM
      → Call user-defined plugin
      → Return selected replica
```

---

### 2.4 Replica Manager (Central State)

**Holds:**
```go
type ReplicaManager struct {
    replicas     []*Replica      // Sorted list of all replicas
    replicasMu   sync.RWMutex    // Thread-safe access
    replicasMap  map[string]*Replica  // Fast lookup by name
}

type Replica struct {
    Name                 string               // e.g., "fm-0"
    Address              string               // e.g., "10.0.0.1:5051"
    RestAddr             string               // e.g., "10.0.0.1:8081"
    Status               ReplicaStatus        // HEALTHY, UNHEALTHY
    StatusMu             sync.RWMutex
    ActiveConnections    int                  // gRPC streams
    ActiveConnMu         sync.RWMutex
    GrpcConn             *grpc.ClientConn    // Connection pool
    HttpClient           *http.Client         // HTTP client
    LastHealthCheckTime  time.Time
    ConsecutiveFailures  int
    Metrics              *ReplicaMetrics
}
```

**Operations:**
```
AddReplica(name, address)       // Add new replica
RemoveReplica(name)              // Remove replica
GetHealthyReplicas()             // Return []*Replica (HEALTHY only)
GetReplica(name)                 // Fast lookup
UpdateReplicaStatus(name, status) // Mark healthy/unhealthy
IncrActiveConnections(name)      // Track stream count
DecrActiveConnections(name)      // Track stream count
```

---

### 2.5 Request Router (Rule + Strategy)

**Responsibility:** Decide which replica to route request to.

**Logic:**
```go
func (r *Router) Route(ctx context.Context, req *Request) (*Replica, error) {
    // 1. Get request context
    clientID := ExtractClientID(req)
    requestType := ExtractRequestType(req)
    
    // 2. Check rate limiting
    if !r.rateLimiter.Allow(clientID) {
        return nil, ErrRateLimited
    }
    
    // 3. Get healthy replicas
    healthyReplicas := r.replicaManager.GetHealthyReplicas()
    if len(healthyReplicas) == 0 {
        return nil, ErrNoHealthyReplicas
    }
    
    // 4. Check panic mode
    if r.isPanicMode() {
        return nil, ErrGatewayUnavailable
    }
    
    // 5. Select replica using configured strategy
    selected := r.loadBalancer.Select(ctx, healthyReplicas)
    if selected == nil {
        return nil, ErrSelectionFailed
    }
    
    // 6. Check circuit breaker
    if selected.circuitBreaker.IsOpen() {
        return nil, ErrCircuitBreakerOpen
    }
    
    // 7. Queue request (backpressure)
    if !r.queue.Enqueue(req, selected.Name) {
        return nil, ErrQueueFull
    }
    
    return selected, nil
}
```

---

### 2.6 Reliability Patterns (Built-in)

#### Circuit Breaker
```
State Machine:

    CLOSED (healthy)
         ↑ ↓
    [5 failures] → OPEN (failing, fast-fail)
                       │
                       [30s timeout]
                       │
                       ▼
                   HALF-OPEN (testing)
                       ↓ ↑
    [1 failure] ────→ OPEN
                       ↑
                   [2 successes] → CLOSED
```

#### Retry with Backoff
```
Attempt 1: SEND
           ↓ (fail, retryable error)
           Wait 100ms (exponential: 100 * 2^0)
           
Attempt 2: SEND
           ↓ (fail, retryable error)
           Wait 200ms (exponential: 100 * 2^1)
           
Attempt 3: SEND
           ↓ (success)
           RETURN
```

#### Timeout
```
Request arrives at T=0
  │
  ├─ Global timeout: 30s
  ├─ Per-replica: 25s
  └─ Connect: 5s
  
  │
  ▼
[Processing...]
  │
  ├─ If response received by T=25s: SUCCESS
  ├─ If still processing at T=25s: TIMEOUT
  └─ If response received at T=26s: DISCARD (global expired)
```

---

## 3. Request Flow (Happy Path)

### 3.1 gRPC Subscribe Flow (Bidirectional Stream)

```
FM Adapter                              Weaver                    CB Replica (fm-0)
    │                                    │                           │
    │ ─────1. Subscribe RPC ────────────→│                           │
    │  (open stream to Weaver)           │                           │
    │                                    │─2. Discover replicas─→    │
    │                                    │                           │
    │                                    │─3. Check health────→      │
    │                                    │                           │
    │                                    │─4. Select replica (LC)    │
    │                                    │  (least-connections)      │
    │                                    │                           │
    │                                    │─5. Dial replica───────────→│
    │                                    │  (:5051)                  │
    │                                    │                           │
    │                                    │─6. Open stream──────────→ │
    │                                    │  (Subscribe RPC)          │
    │                                    │                           │
    │ ←──7. Config event (filter-0)──────│←─ Stream response ──────  │
    │  (from Weaver stream proxy)        │                           │
    │                                    │                           │
    │ ─────8. Watermark update ─────────→│─ Forward to replica──────→│
    │                                    │                           │
    │ ←──9. Config event (filter-1)──────│←─ Stream response ──────  │
    │                                    │                           │
    │ [Stream active, proxying...]       │ [Proxying bidirectional] │
    │                                    │                           │
    │ ─────10. Unsubscribe ─────────────→│─ Close stream────────────→│
    │  (close stream)                    │                           │
    │                                    │ [Connection cleanup]      │
    │ ←───11. Stream closed──────────────│←─ Stream closed ─────────│
    │                                    │                           │
```

**Timeline:**
```
T=0ms:    FM Adapter initiates Subscribe stream
T=1ms:    Weaver discovers replicas (etcd query)
T=5ms:    Weaver health-checks replicas
T=6ms:    Weaver selects fm-0 (least-connections)
T=7ms:    Weaver establishes connection to fm-0
T=10ms:   Weaver opens stream to fm-0
T=11ms:   First config event received by FM Adapter
[Stream remains open for hours...]
```

---

### 3.2 gRPC Publish Flow (Request-Response)

```
FM Adapter                         Weaver              CB Replica (cb-0)
    │                               │                       │
    │ ─1. Publish RPC ────────────→ │                       │
    │  (single request)             │                       │
    │                               │─2. Extract context    │
    │                               │─3. Check rate limit   │
    │                               │─4. Get healthy        │
    │                               │   replicas            │
    │                               │─5. Select (RR or LC)  │
    │                               │                       │
    │                               │─6. Queue request      │
    │                               │─7. Forward to cb-0────→
    │                               │  (:5052)              │
    │                               │                       │
    │                               │ [Processing...]       │
    │                               │                       │
    │                               │←7. Response ──────────│
    │                               │  (PublishAck)         │
    │                               │                       │
    │ ←─8. Response returned ───────│                       │
    │  (PublishAck)                 │                       │
    │                               │                       │
    │ ─9. Next Publish RPC ────────→│ ─RR: select cb-1──┐   │
    │                               │                   │   │
    │                               │                   │   │
```

**Latency Breakdown:**
```
Client request arrival: T=0ms

T=1ms:   Weaver extracts context, checks rate limit
T=2ms:   Weaver gets healthy replicas from cache
T=2.1ms: Weaver selects replica (RR: O(1))
T=2.2ms: Weaver queues request
T=2.3ms: Weaver establishes/reuses connection
T=3ms:   Request forwarded to replica

T=8ms:   Replica processes and responds
T=8.1ms: Weaver receives response
T=8.2ms: Weaver records metrics
T=8.3ms: Weaver returns response to client

Total gateway overhead: 3.3ms (8.3 - 5 = gateway latency)
```

---

### 3.3 REST Observability Flow

```
Dashboard                          Weaver              CB Replica
    │                               │                     │
    │ GET /api/v1/topics ──────────→│                     │
    │                               │                     │
    │                               │─1. Rate limit check │
    │                               │─2. Get healthy      │
    │                               │   replicas          │
    │                               │─3. Select (RR)      │
    │                               │                     │
    │                               │─GET /api/v1/topics─→
    │                               │  (HTTP)             │
    │                               │                     │
    │                               │ [Processing...]     │
    │                               │                     │
    │                               │←Response (JSON)─────
    │                               │  [{topic_id, ...}]  │
    │                               │                     │
    │ ←Response (JSON) ─────────────│                     │
    │                               │                     │
```

---

## 4. Failure Handling

### 4.1 Replica Failure Detection

```
Timeline of replica failure:

T=0s:    Replica crashes (fm-0 dies)

T=10s:   Health check attempt 1
         GET /api/v1/health → TIMEOUT
         Increment consecutive_failures = 1
         Status remains HEALTHY

T=20s:   Health check attempt 2
         GET /api/v1/health → TIMEOUT
         Increment consecutive_failures = 2
         Status remains HEALTHY
         
T=30s:   Health check attempt 3
         GET /api/v1/health → TIMEOUT
         Increment consecutive_failures = 3
         SET Status = UNHEALTHY ← MARKED DOWN
         Emit alert: "Replica fm-0 marked unhealthy"

T=31s:   New requests skip fm-0 (load balancer filter)

T=40s+:  If fm-0 recovers
         Health check succeeds
         Reset consecutive_failures = 0
         SET Status = HEALTHY
         Emit info: "Replica fm-0 recovered"
```

**Detection latency:** ~30 seconds (3 × 10s health check interval)

---

### 4.2 Panic Mode (>50% Replicas Down)

```
If 3 replicas (fm-0, fm-1, fm-2):

Scenario 1: 1 replica down
  ├─ Healthy: 2/3 = 67%
  ├─ Unhealthy: 1/3 = 33%
  ├─ Threshold: 50%
  └─ Mode: NORMAL (67% > 50%)

Scenario 2: 2 replicas down
  ├─ Healthy: 1/3 = 33%
  ├─ Unhealthy: 2/3 = 67%
  ├─ Threshold: 50%
  └─ Mode: PANIC (33% <= 50%) ← ALERT TRIGGERED
  
  When in PANIC:
  ├─ New requests return UNAVAILABLE (gRPC)
  ├─ New requests return 503 (HTTP)
  ├─ Emit critical alert
  └─ Operators notified

Scenario 3: Replicas recover
  ├─ Healthy: 2/3 = 67%
  ├─ Mode: NORMAL (67% > 50%) ← EXIT PANIC
  ├─ Emit info: "Exited panic mode"
  └─ Normal routing resumes
```

---

## 5. Configuration Model

### 5.1 Configuration Structure

```yaml
# weaver-config.yaml (Single source of truth)

gateway:
  name: "my-gateway"
  description: "FM Gateway for DashFabric"
  
discovery:
  type: "t2_etcd"
  interval_seconds: 10
  config:
    endpoint: "http://etcd-t2:2379"
    key_pattern: "/dashfabric/cluster/pods/fm-*"
    
health:
  type: "http"
  interval_seconds: 10
  timeout_seconds: 5
  success_threshold: 1
  failure_threshold: 3
  config:
    endpoint: "/api/v1/health"
    
listeners:
  - name: "grpc"
    type: "grpc"
    port: 5051
    tls: false
    
  - name: "http_rest"
    type: "http"
    port: 8080
    routes:
      - path: "/metrics"
        handler: "prometheus_metrics"
      - path: "/debug/*"
        handler: "debug_endpoints"
    
routing:
  strategy: "least_connections"
  timeout_seconds: 30
  retry:
    max_attempts: 3
    backoff_type: "exponential"
    initial_delay_ms: 100
    max_delay_ms: 5000
    
load_balancers:
  - name: "subscribe_lb"
    type: "least_connections"
  - name: "publish_lb"
    type: "round_robin"
    
reliability:
  circuit_breaker:
    enabled: true
    failure_threshold: 5
    success_threshold: 2
    timeout_seconds: 30
    
  timeout:
    global_seconds: 30
    per_replica_seconds: 25
    connect_seconds: 5
    
  bulkhead:
    enabled: true
    max_connections_per_replica: 100
    max_pending_per_replica: 1000
    
buffering:
  enabled: true
  depth: 5000
  overflow_behavior: "reject_with_503"
  
rate_limiting:
  enabled: true
  policies:
    - name: "per_client"
      type: "token_bucket"
      rate: 1000/min
      key: "client_id"
      
security:
  authentication:
    enabled: false
    type: "bearer_token"
    
  tls:
    enabled: false
    
observability:
  metrics:
    enabled: true
    namespace: "fm_gw"
    port: 9090
    
  tracing:
    enabled: true
    exporter: "jaeger"
    
  logging:
    level: "info"
    format: "json"
```

---

## 6. Deployment Architecture

### 6.1 Kubernetes Deployment

```yaml
# deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: fm-gateway
spec:
  replicas: 3
  template:
    spec:
      containers:
      - name: weaver
        image: weaver:v1.0
        ports:
        - containerPort: 5051  # gRPC
        - containerPort: 8080  # HTTP
        - containerPort: 9090  # Metrics
        volumeMounts:
        - name: config
          mountPath: /etc/weaver
      volumes:
      - name: config
        configMap:
          name: fm-gateway-config
          
---
apiVersion: v1
kind: Service
metadata:
  name: fm-gateway
spec:
  type: LoadBalancer
  ports:
  - name: grpc
    port: 5051
    targetPort: 5051
  - name: http
    port: 8080
    targetPort: 8080
```

---

## 7. Extensibility Points

### 7.1 Plugin Loading

```
At startup:

1. Load configuration (YAML)
2. Parse plugin specifications
3. Load plugin binaries from /plugins directory
4. Validate plugins (implement required interfaces)
5. Register plugins in manager
6. Initialize selected plugins

Example:
  load_balancers:
    - name: "my_custom_lb"
      type: "custom"
      plugin_path: "/plugins/my_loadbalancer.so"
      config:
        param1: "value1"
        
→ Weaver loads /plugins/my_loadbalancer.so
→ Instantiates with config
→ Registers as available load balancer
```

---

**Next:** Read [weaver-lld.md](./weaver-lld.md) for low-level design details.
