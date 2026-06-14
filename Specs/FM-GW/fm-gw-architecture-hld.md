# FM-Gateway: Architecture & High-Level Design (HLD) — PRIMARY-AWARE

> **Status:** Draft v2 (Primary-Aware)
> **Module:** FM-Gateway (fm-gw)
> **Scope:** Load balancer for FM REST API (HTTP) and gRPC streams with primary election awareness
> **Audience:** FM-GW implementers, architects, DevOps teams

## 1. Overview

FM-Gateway (fm-gw) is a lightweight, **primary-aware** load balancer that routes client requests to FM replicas (fm-0, fm-1, fm-2). It serves as the **single entry point** for:
- Device registration (REST POST) → routes to **PRIMARY only**
- Device queries (REST GET) → routes to **PRIMARY only** (consistency)
- Device heartbeat/telemetry (REST POST) → routes to **PRIMARY**
- gRPC Subscribe streams (long-lived) → routes to **ANY replica** (load-balanced, least-connections)
- gRPC Publish (acks) → routes to **PRIMARY or any**

**Key innovation:** Gateway monitors FM's adapter lease (from T2 etcd) to detect which replica is the current PRIMARY. Routes requests intelligently based on both **request type** and **current primary**.

**Design principles:**
- Single binary; runs on all deployment tiers (docker-compose, K8s)
- Minimal code (~1200 LOC); reuses FM's existing infrastructure (T2 etcd)
- gRPC + HTTP support (L4 and L7 load balancing)
- **Primary-aware routing** for production reliability and consistency
- Detects primary failover automatically; buffers requests during election
- Zero custom operational infrastructure (uses T2 etcd for discovery)

## 2. Architecture

### 2.1 Deployment Model

```
┌──────────────────────────────────┐
│  Clients (External)              │
│  (device agents, orchestrators)  │
└────────────┬─────────────────────┘
             │
             ▼
┌──────────────────────────────────────────────────┐
│  FM-Gateway (this project)                       │
│  ├─ HTTP listener :8080                          │
│  ├─ gRPC listener :5051                          │
│  ├─ Pod Discovery (from T2 etcd, every 10s)      │
│  ├─ Primary Election Monitor (from T2 lease, 5s) │
│  ├─ Primary-Aware Router (type + role aware)     │
│  ├─ Load Balancer (least-conn for streams)       │
│  ├─ Request Buffer (per-replica, 1000 depth)     │
│  ├─ Health Checker (replicas, 10s interval)      │
│  ├─ Rate Limiter (per-IP, per-device, per-key)   │
│  └─ Metrics/Traces                               │
└──────────┬──────────────────────────┬────────────┘
           │                          │
      ┌────▼────┐                ┌────▼──────────┐
      │ Pod List │                │  T2 etcd      │
      │(cached)  │                │  ├─ Pods      │
      └────▲────┘                │  └─ Lease     │
           │                      └────────────────┘
           │
    ┌──────┴──────────┬──────────────┬──────────────┐
    │                 │              │              │
    ▼                 ▼              ▼              ▼
┌────────┐       ┌────────┐    ┌────────┐    ┌────────┐
│ fm-0   │       │ fm-1   │    │ fm-2   │    │ fm-3   │ (optional)
│STANDBY │       │PRIMARY │    │STANDBY │    │STANDBY │ (role from lease)
│:8080   │       │:8080   │    │:8080   │    │:8080   │
│:5051   │       │:5051   │    │:5051   │    │:5051   │
└────────┘       └────────┘    └────────┘    └────────┘
```

### 2.2 Key Components

| Component | Purpose | Notes |
|-----------|---------|-------|
| **Pod Discoverer** | Discover all FM replicas from T2 etcd | Every 10s; K8s StatefulSet or docker-compose |
| **Lease Monitor** | Monitor adapter lease for primary detection | Every 5s; reads /dashfabric/cluster/adapter/lease |
| **Primary Tracker** | Maintains current PRIMARY replica pointer | Updates on lease change; enables role-aware routing |
| **HTTP Listener** | Port 8080; handles REST API requests | Demux to primary-aware router |
| **gRPC Listener** | Port 5051; handles Subscribe/Publish streams | Demux to primary-aware router |
| **Request Router** | Routes by request type + current primary | Writes → primary; reads/streams → intelligent routing |
| **Load Balancer** | Selects replica for streams | Least-connections (accounts for active stream count) |
| **Request Buffer** | Per-replica queue; handles backpressure | Max 1000 requests/replica; 503 if full |
| **Health Checker** | Polls `/api/v1/health` on each replica | Marks unhealthy; circuit breaker if >50% down |
| **Rate Limiter** | Per-IP, per-device, per-key limits | Token bucket; configurable thresholds |
| **Metrics** | Prometheus metrics | Primary elections, latency, failover events |
| **Traces** | W3C Trace Context propagation | Enables end-to-end observability |

## 3. High-Level Flow

### 3.1 Pod Discovery & Primary Election (Continuous)

```
Gateway startup:
  1. Poll T2 etcd for pod list (/dashfabric/cluster/pods/*)
  2. Read adapter lease (/dashfabric/cluster/adapter/lease)
  3. Determine: primary = pod holding lease; standbys = others
  4. Store in memory: replicas[], primary_replica*

Every 10 seconds:
  └─ Refresh pod list (detect pod add/remove)

Every 5 seconds:
  └─ Read adapter lease
  ├─ If holder unchanged: continue
  └─ If holder changed: update primary_replica; notify routers
```

### 3.2 REST API Request Flow (Primary-Aware)

```
Client Request (e.g., POST /api/v1/devices)
    │
    ▼
[HTTP Listener :8080]
    ├─ Parse request
    └─ Detect request type + trace context
    │
    ▼
[Rate Limiter]
    ├─ Check per-IP, per-device, per-key limits
    └─ If exceeded: return 429 Too Many Requests
    │
    ▼
[Primary-Aware Router]
    │
    ├─ If POST /devices (REGISTRATION):
    │   ├─ Get current PRIMARY
    │   └─ If no primary: return 503 (Retry-After: 10s)
    │       Reason: Waiting for primary election (max 20s)
    │   └─ Route to PRIMARY
    │
    ├─ If GET /devices (QUERY):
    │   ├─ Get current PRIMARY
    │   └─ If no primary: return 503
    │   └─ Route to PRIMARY (consistency)
    │
    ├─ If POST /heartbeat (HEARTBEAT):
    │   ├─ Get current PRIMARY
    │   └─ If no primary: buffer request (or return 503)
    │   └─ Route to PRIMARY
    │
    └─ [Other requests]: Route to PRIMARY or best replica
    │
    ▼
[Request Buffer]
    ├─ Queue request for selected replica
    └─ If queue full: return 503 Service Unavailable
    │
    ▼
[Forward to FM Replica]
    ├─ HTTP POST/GET to fm-{replica}:8080/{path}
    ├─ Wait for response (timeout 30s)
    ├─ Update metrics (latency, status, replica role)
    │
    ▼
[Return to Client]
    └─ HTTP response (201, 409, 503, etc.)
```

### 3.3 gRPC Stream Flow (Load-Balanced)

```
Client gRPC Stream (e.g., Subscribe(...))
    │
    ▼
[gRPC Listener :5051]
    ├─ Accept connection
    └─ Parse metadata (trace-id)
    │
    ▼
[Primary-Aware Router]
    ├─ Request type: SUBSCRIBE (gRPC stream)
    ├─ Route strategy: Load-balance across ALL healthy replicas
    │   Reason: Streams are read-only; all replicas have identical
    │   state via T1 watch; no consistency requirement
    │
    ▼
[Load Balancer: Least-Connections]
    ├─ Count active connections per replica (including primary)
    └─ Select replica with fewest active streams
    │
    ▼
[Stream Multiplexer]
    ├─ Open gRPC stream to fm-{replica}:5051
    └─ Proxy Subscribe RPC
    │
    ▼
[Bidirectional Proxy]
    ├─ Client → fm: Subscribe requests
    ├─ fm → Client: Events, acks
    │
    ▼
[Keep alive until client closes]
    └─ Close stream; decrement active connection count; update metrics
```

## 4. Load Balancing & Routing Strategy

### 4.1 Request Type Routing (Primary-Aware)

**Gateway knows current PRIMARY (via T2 lease). Routing strategy depends on request type:**

| Request Type | Routing | Target | Reason |
|---|---|---|---|
| **Device Registration** | Primary-only | PRIMARY only | Primary writes to T1; single writer; simpler CAS logic |
| **Device Query** | Primary-only | PRIMARY only | Consistency; primary actively programming (freshest state) |
| **Device Heartbeat** | Primary-only | PRIMARY only | Primary tracks device health; should monitor |
| **gRPC Subscribe** | Load-balanced | ANY healthy replica | Streams are read-only; all have identical state; load-balance for fairness |
| **Health Check** | Any | Any healthy replica | Non-critical; for gateway monitoring |

### 4.2 Primary Unavailability Handling

**Scenario: Primary crashes or becomes UNHEALTHY**

```
T=0s:    Primary (fm-1) crashes

T=1s:    Gateway health check detects UNHEALTHY
         └─ primary_replica still = fm-1 (but Status=UNHEALTHY)

T=1s:    New registration request arrives
         └─ routeToRegistration(): getPrimary() → (nil, false)
         └─ Return 503 Service Unavailable
         └─ Header: "Retry-After: 10" (estimated election time)

T=1-15s: Gateway checks lease every 5s
         └─ Lease holder still "fm-1" (not yet expired)
         └─ primaryUnavailableAt = T+1s (mark unavailability start)

T=15s:   Lease expires (expires_at = T+15s)
         └─ Standbys (fm-0, fm-2) win CAS race
         └─ New primary elected (e.g., fm-0)

T=15.1s: Gateway detects lease holder change
         └─ LeaseChangeEvent: old="fm-1", new="fm-0"
         └─ primary_replica = fm-0
         └─ Metrics: election_latency_ms = 150

T=15.2s: New registration request arrives
         └─ routeToRegistration(): getPrimary() → (fm-0, true)
         └─ Queue request to fm-0
         └─ Forward succeeds

T=20s:   Normal operation resumes

RTO (Recovery Time Objective): 15 seconds (adapter lease TTL)

Buffering Strategy:
  Option A (simpler): Return 503 during unavailability
    └─ Client retries; succeeds after new primary elected
    
  Option B (advanced): Buffer requests during unavailability
    └─ Queue up to 20 seconds; drain when new primary elected
    └─ No request loss; automatic retry
```

### 4.3 Least-Connections for gRPC Streams

**For long-lived gRPC Subscribe streams, use least-connections load balancing:**

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

**Why:** Streams are read-only (no consistency requirement); all replicas have identical state via T1 watch. Load-balancing across all (including primary) distributes streams fairly.

**Benefit:** Prevents primary from being overloaded with 1000+ concurrent Subscribe streams.

## 5. Failover & Primary Election

### 5.1 Adapter Lease Structure (from FM)

```
T2 etcd key: /dashfabric/cluster/adapter/lease

Value:
{
  "holder_pod_name": "fm-1",
  "holder_ordinal": 1,
  "claimed_at_unix_ms": 1718400000000,
  "expires_at_unix_ms": 1718400015000,      // claimed_at + 15s TTL
  "version": 42,                             // for CAS
  "renewal_interval_seconds": 5
}
```

**Gateway behavior:**
- If `now < expires_at`: lease is valid; holder is PRIMARY
- If `now >= expires_at`: lease is expired; no primary; wait for new election
- If holder changes: old primary lost; new primary elected

### 5.2 Primary Election Timeline

See `fm-gw-pod-discovery-and-primary-election.md` §7 for detailed scenarios.

## 6. Configuration

### 6.1 Environment Variables

```bash
# Pod discovery (T2 etcd)
FM_GW_T2_ENDPOINT=http://etcd-t2.svc.cluster.local:2379
FM_GW_POD_DISCOVERY_INTERVAL_SECONDS=10
FM_GW_LEASE_MONITOR_INTERVAL_SECONDS=5

# Primary handling
FM_GW_PRIMARY_WAIT_TIMEOUT_SECONDS=20
FM_GW_BUFFER_REQUESTS_ON_PRIMARY_LOSS=true

# Routing strategy
FM_GW_REGISTRATION_TO_PRIMARY_ONLY=true
FM_GW_QUERY_TO_PRIMARY_ONLY=true
FM_GW_STREAM_LOAD_BALANCE=true

# Listeners
FM_GW_HTTP_PORT=8080
FM_GW_GRPC_PORT=5051

# Buffering
FM_GW_QUEUE_DEPTH_PER_REPLICA=1000
FM_GW_REQUEST_TIMEOUT_SECONDS=30

# Health checking
FM_GW_HEALTH_CHECK_INTERVAL_SECONDS=10
FM_GW_HEALTH_CHECK_TIMEOUT_SECONDS=5

# Rate limiting
FM_GW_RATE_LIMIT_PER_IP_PER_MIN=1000
FM_GW_RATE_LIMIT_PER_DEVICE_PER_MIN=100

# docker-compose (static pod list, no T2 etcd)
FM_GW_REPLICAS=fm-0:8080,fm-1:8080,fm-2:8080
FM_GW_REPLICA_DISCOVERY_MODE=static  # or "t2_etcd"
```

## 7. Observability

### 7.1 Metrics

**Primary Election:**
- `fm_gw_primary_replica{pod}` (gauge: 1=primary, 0=standby)
- `fm_gw_primary_election_count_total` (counter)
- `fm_gw_primary_election_latency_ms` (histogram)
- `fm_gw_lease_holder{pod}` (gauge: current holder)
- `fm_gw_primary_unavailable_seconds_total` (counter)

**Replica Status:**
- `fm_gw_replica_health{replica}` (gauge: 1=healthy, 0=unhealthy)
- `fm_gw_active_connections{replica}` (gauge: for streams)
- `fm_gw_queue_depth{replica}` (gauge)

**Routing:**
- `fm_gw_requests_to_primary_total{method}` (counter)
- `fm_gw_requests_to_standby_total{method}` (counter)
- `fm_gw_request_duration_ms{replica, percentile}` (histogram)

### 7.2 Logs

```
[INFO] Gateway startup: discovering pods from T2 etcd
[INFO] Pod list discovered: fm-0, fm-1, fm-2
[INFO] Primary detected: fm-1 (lease expires in 14.8s)
[WARN] Primary unhealthy: fm-1 (failures: 3/3)
[WARN] Primary lost; waiting for new primary election (max 20s)
[INFO] New primary elected: fm-0 (election_latency_ms=150)
[INFO] Draining buffered requests: queued_count=42
```

### 7.3 Traces

Root span: `gateway.request`
- Attribute: `primary_pod=fm-1`, `primary_role=primary|standby`
- Child span: `routing.decision`
  - Attribute: `request_type=registration`, `routed_to=primary`

## 8. Deployment

**Kubernetes:**
```yaml
Deployment: fm-gw (1-3 replicas)
Service: LoadBalancer (exposes :8080, :5051)
ConfigMap: fm-gw-config.yaml
```

**docker-compose:**
```yaml
services:
  fm-gw:
    image: dashfabric/fm-gw:latest
    ports: ["8080:8080", "5051:5051"]
    environment:
      FM_GW_REPLICAS: "fm-0:8080,fm-1:8080,fm-2:8080"
      FM_GW_REPLICA_DISCOVERY_MODE: "static"
```

## 9. References

- `fm-gw-pod-discovery-and-primary-election.md` — Detailed primary detection, lease monitoring, failover scenarios
- `fm-gw-low-level-design.md` — Data structures, algorithms, concurrency
- `fm-gw-implementation-planner.md` — Implementation phases and tasks
- `fm-pod-lifecycle-design.md` — FM replica lifecycle, adapter lease
- `fm-failover-sequence-design.md` — Primary failover step-by-step
