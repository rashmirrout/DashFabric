# CB-Gateway: Architecture & High-Level Design (HLD)

> **Status:** Draft v1
> **Module:** CB-Gateway (cb-gw)
> **Scope:** Dual-protocol load balancer for CB (gRPC for FM, REST for observability)
> **Audience:** CB-GW implementers, architects, DevOps teams

## 1. Overview

CB-Gateway (cb-gw) is a lightweight, **replica-aware** dual-protocol gateway that routes requests to CB (ControllerBridge) replicas:
- **gRPC endpoint (:5052)** — Routes FM Adapter Subscribe/Publish calls to CB replicas (load-balanced)
- **REST endpoint (:8081)** — Exposes observability API for dashboards and debug tools

**Key innovation:** All CB replicas are peer-equivalent (no primary election). Load balancing is based on replica health and connection count, not role.

**Design principles:**
- Single binary; runs on all deployment tiers (docker-compose, K8s)
- Minimal code (~1500 LOC); reuses CB's existing infrastructure (T2 etcd)
- gRPC + REST support (dual protocol)
- Replica-aware routing (all replicas serve both Subscribe and Publish)
- Zero custom operational infrastructure (uses T2 etcd for discovery)
- Detects replica failures automatically; buffers requests during transient outages

## 2. Architecture

### 2.1 Deployment Model

```
┌──────────────────────────────────┐
│  FM Adapter                      │
│  (publishes configs)             │
└────────────┬─────────────────────┘
             │ gRPC
             │ Subscribe/Publish
             ▼
┌──────────────────────────────────────────────────┐
│  CB-Gateway (this project)                       │
│  ├─ gRPC listener :5052                          │
│  ├─ REST listener :8081                          │
│  ├─ Pod Discovery (from T2 etcd, every 10s)      │
│  ├─ Replica Monitor (health checks, 10s)         │
│  ├─ Load Balancer (least-conn for gRPC)          │
│  ├─ Request Router (Subscribe/Publish)           │
│  ├─ REST Observability API                       │
│  ├─ Request Buffer (per-replica, 1000 depth)     │
│  ├─ Rate Limiter (per-client)                    │
│  └─ Metrics/Traces                               │
└──────────┬──────────────────────────────┬────────┘
           │                              │
      ┌────▼────┐                    ┌────▼──────────┐
      │ Pod List │                    │  T2 etcd      │
      │(cached)  │                    │  └─ Pods      │
      └────▲────┘                    └────────────────┘
           │
    ┌──────┴──────────┬──────────────┬──────────────┐
    │                 │              │              │
    ▼                 ▼              ▼              ▼
┌────────┐       ┌────────┐    ┌────────┐    ┌────────┐
│ cb-0   │       │ cb-1   │    │ cb-2   │    │ cb-3   │ (optional)
│ Topic  │       │ Topic  │    │ Topic  │    │ Topic  │
│ Broker │       │ Broker │    │ Broker │    │ Broker │
│:5052   │       │:5052   │    │:5052   │    │:5052   │
│:8081   │       │:8081   │    │:8081   │    │:8081   │
└────────┘       └────────┘    └────────┘    └────────┘
```

### 2.2 Key Components

| Component | Purpose | Notes |
|-----------|---------|-------|
| **Pod Discoverer** | Discover all CB replicas from T2 etcd | Every 10s; K8s StatefulSet or docker-compose |
| **Health Monitor** | Detect unhealthy CB replicas | Every 10s; HTTP GET /api/v1/health |
| **gRPC Listener** | Port 5052; handles FM Adapter Subscribe/Publish | Implements CB FM-side gRPC contract |
| **REST Listener** | Port 8081; observability API | Topic inspection, metrics, debug endpoints |
| **Load Balancer** | Selects replica for gRPC/REST | Least-connections for Subscribe; round-robin for Publish/REST |
| **Request Router** | Routes by RPC type (Subscribe vs Publish) | Subscribe → long-lived stream; Publish → request-response |
| **Request Buffer** | Per-replica queue; handles backpressure | Max 1000 requests/replica; 503 if full |
| **Rate Limiter** | Per-client request throttling | Token bucket; per-client limit |
| **Metrics** | Prometheus metrics | Replica health, latency, queue depth, throughput |
| **Traces** | W3C Trace Context propagation | End-to-end request tracing |

## 3. High-Level Flow

### 3.1 Pod Discovery & Health Monitoring (Continuous)

```
Gateway startup:
  1. Poll T2 etcd for pod list (/dashfabric/cluster/pods/cb-*)
  2. Determine: all replicas = peers (no primary/standby role)
  3. Store in memory: replicas[]

Every 10 seconds:
  ├─ Refresh pod list (detect pod add/remove)
  └─ Health check each replica
      └─ GET /api/v1/health → mark HEALTHY or UNHEALTHY
```

### 3.2 gRPC Subscribe Flow (Long-Lived Stream)

```
FM Adapter opens Subscribe stream
    │
    ▼
[gRPC Listener :5052]
    ├─ Accept connection
    └─ Parse gRPC metadata
    │
    ▼
[Load Balancer: Least-Connections]
    ├─ Count active connections per replica
    └─ Select replica with fewest active streams
    │
    ▼
[Stream Multiplexer]
    ├─ Dial replica :5052
    ├─ Open gRPC stream to CB replica
    └─ Proxy Subscribe RPC
    │
    ▼
[Bidirectional Proxy]
    ├─ FM → CB: Filter/watermark updates
    ├─ CB → FM: Config events (topics)
    │
    ▼
[Keep alive until FM closes]
    └─ Close stream; decrement active connection count; update metrics
```

### 3.3 gRPC Publish Flow (Request-Response)

```
FM Adapter sends Publish RPC (e.g., publish acks)
    │
    ▼
[gRPC Listener :5052]
    ├─ Receive Publish request
    └─ Parse gRPC metadata
    │
    ▼
[Rate Limiter]
    ├─ Check per-client limit
    └─ If exceeded: return RESOURCE_EXHAUSTED
    │
    ▼
[Load Balancer]
    ├─ Select replica (round-robin or least-conn)
    │
    ▼
[Request Buffer]
    ├─ Queue request for selected replica
    └─ If queue full: return UNAVAILABLE
    │
    ▼
[Forward to CB Replica]
    ├─ gRPC call to cb-N:5052
    ├─ Wait for response (timeout 30s)
    ├─ Update metrics (latency, status)
    │
    ▼
[Return to FM]
    └─ gRPC response (OK or error)
```

### 3.4 REST Observability Flow

```
Dashboard queries /api/v1/topics
    │
    ▼
[REST Listener :8081]
    ├─ Parse request
    └─ Rate limit per-client
    │
    ▼
[Load Balancer]
    ├─ Select replica (round-robin)
    │
    ▼
[REST Proxy]
    ├─ Forward request to cb-N:8081
    ├─ Wait for response (timeout 10s)
    │
    ▼
[Return to Dashboard]
    └─ JSON response (topics, entries, metrics)
```

## 4. Load Balancing & Routing Strategy

### 4.1 Replica Selection (All Peers, No Primary)

**CB replicas are peer-equivalent** — all replicas have identical topic store (eventually consistent).

| Request Type | Load Balancing | Target | Reason |
|---|---|---|---|
| **gRPC Subscribe** | Least-connections | ANY healthy replica | Distributes long-lived streams evenly |
| **gRPC Publish** | Round-robin or least-conn | ANY healthy replica | All replicas accept writes |
| **REST Query** | Round-robin | ANY healthy replica | Stateless; no replica preference |

### 4.2 Least-Connections for gRPC Subscribe

```go
func selectReplicaForStream(replicas []*Replica) *Replica {
    var minConns int = math.MaxInt
    var selected *Replica = replicas[0]
    
    for _, r := range replicas {
        activeConns := r.getActiveConnections()
        if activeConns < minConns {
            minConns = activeConns
            selected = r
        }
    }
    return selected
}
```

**Why:** Subscribe streams are long-lived (hours). Distributing by connection count prevents any one replica from being overloaded.

## 5. Replica Unavailability Handling

### 5.1 Health Check Failure

```
T=0s:    CB replica crashes

T=1s:    Gateway health check detects UNHEALTHY
         └─ Mark replica as StatusUnhealthy

T=1s:    New gRPC request arrives
         └─ Load balancer skips unhealthy replica
         └─ Routes to next healthy replica

T=2-10s: Gateway checks every 10s
         └─ Replica still UNHEALTHY

T=10s+:  If replica recovers
         └─ Next health check detects HEALTHY
         └─ Replica re-enters load balancing pool
```

**RTO (Recovery Time Objective):** 10 seconds (health check interval)

### 5.2 All Replicas Unhealthy

```
If >50% of replicas unhealthy:
  └─ Enter PANIC mode
  └─ Return UNAVAILABLE (gRPC) or 503 (REST)
  └─ Header: "Retry-After: 10"
```

## 6. Configuration

### 6.1 Environment Variables

```bash
# Pod discovery (T2 etcd)
CB_GW_T2_ENDPOINT=http://etcd-t2:2379
CB_GW_POD_DISCOVERY_INTERVAL_SECONDS=10

# Listeners
CB_GW_GRPC_PORT=5052
CB_GW_REST_PORT=8081

# Load balancing
CB_GW_LB_ALGORITHM=least_connections

# Buffering
CB_GW_QUEUE_DEPTH_PER_REPLICA=1000
CB_GW_REQUEST_TIMEOUT_SECONDS=30

# Health checking
CB_GW_HEALTH_CHECK_INTERVAL_SECONDS=10
CB_GW_HEALTH_CHECK_TIMEOUT_SECONDS=5

# Rate limiting
CB_GW_RATE_LIMIT_PER_CLIENT_PER_MIN=1000

# docker-compose (static pod list, no T2 etcd)
CB_GW_REPLICAS=cb-0:5052,cb-1:5052,cb-2:5052
CB_GW_REPLICA_DISCOVERY_MODE=static  # or "t2_etcd"
```

## 7. Observability

### 7.1 Metrics

**Replica Status:**
- `cb_gw_replica_health{replica}` (gauge: 1=healthy, 0=unhealthy)
- `cb_gw_active_connections{replica}` (gauge: active gRPC streams)
- `cb_gw_queue_depth{replica}` (gauge)

**Request Metrics:**
- `cb_gw_grpc_requests_total{method, status, replica}` (counter: Subscribe, Publish)
- `cb_gw_grpc_request_duration_ms{method, replica, percentile}` (histogram)
- `cb_gw_rest_requests_total{endpoint, status, replica}` (counter)
- `cb_gw_rest_request_duration_ms{endpoint, replica, percentile}` (histogram)

**Rate Limiting:**
- `cb_gw_rate_limit_violations_total{client}` (counter)

### 7.2 Logs

```
[INFO] Gateway startup: discovering replicas from T2 etcd
[INFO] Replica list discovered: cb-0, cb-1, cb-2
[WARN] Replica unhealthy: cb-1 (failures: 3/3)
[INFO] Replica recovered: cb-1
[ERROR] All replicas unhealthy; entering PANIC mode
```

### 7.3 Traces

Root span: `cb-gw.request`
- Attribute: `request_type=subscribe|publish`, `replica=cb-0`
- Child span: `load_balancer.select`
  - Attribute: `selected_replica=cb-1`, `active_connections=10`
- Child span: `replica.request`
  - Attribute: `latency_ms=5`, `status=OK`

## 8. Deployment

**Kubernetes:**
```yaml
Deployment: cb-gw (1-3 replicas)
Service: LoadBalancer (exposes :5052, :8081)
ConfigMap: cb-gw-config.yaml
```

**docker-compose:**
```yaml
services:
  cb-gw:
    image: dashfabric/cb-gw:latest
    ports: ["5052:5052", "8081:8081"]
    environment:
      CB_GW_REPLICAS: "cb-0:5052,cb-1:5052,cb-2:5052"
      CB_GW_REPLICA_DISCOVERY_MODE: "static"
```

## 9. References

- `cb-gw-low-level-design.md` — Data structures, algorithms, concurrency
- `cb-gw-pod-discovery-and-replica-monitoring.md` — Detailed replica discovery and health monitoring
- `cb-gw-implementation-planner.md` — Implementation phases and tasks
- `../CB/01-cb-architecture-hld.md` — CB architecture and topic broker design
- `../FM-GW/fm-gw-architecture-hld.md` — FM-Gateway design (analogous pattern)
