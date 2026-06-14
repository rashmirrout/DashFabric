# CB-Gateway: Pod Discovery & Replica Monitoring

> **Status:** Draft v1
> **Module:** CB-Gateway (cb-gw)
> **Scope:** CB replica discovery, health monitoring (no primary election)
> **Audience:** CB-GW implementers, architects

## 1. Overview

CB replicas are **peer-equivalent** — all replicas maintain identical topic store (eventually consistent). There is no primary/standby role, no leader election, and no lease-based coordination. CB-GW discovers replicas from T2 etcd and monitors health.

**Key difference from FM-GW:** No primary election. All replicas are valid targets for both Subscribe and Publish operations.

## 2. Pod Discovery

### 2.1 T2 etcd Pod List

CB replicas register themselves in T2 etcd under `/dashfabric/cluster/pods/cb-{ordinal}`:

```
/dashfabric/cluster/pods/cb-0 → {name: "cb-0", address: "10.0.0.1:5052", rest_address: "10.0.0.1:8081"}
/dashfabric/cluster/pods/cb-1 → {name: "cb-1", address: "10.0.0.2:5052", rest_address: "10.0.0.2:8081"}
/dashfabric/cluster/pods/cb-2 → {name: "cb-2", address: "10.0.0.3:5052", rest_address: "10.0.0.3:8081"}
```

### 2.2 Pod Discoverer Interface

```go
type PodDiscoverer interface {
    Discover(ctx context.Context) ([]*Replica, error)
}

// T2 etcd implementation
type T2EtcdDiscoverer struct {
    t2Endpoint string
    client     *etcd.Client
}

func (d *T2EtcdDiscoverer) Discover(ctx context.Context) ([]*Replica, error) {
    // Query /dashfabric/cluster/pods/cb-*
    // Parse each pod entry
    // Return sorted []*Replica (by ordinal)
}

// docker-compose implementation (static)
type DockerComposeDiscoverer struct {
    replicasStr string  // "cb-0:5052,cb-1:5052"
}

func (d *DockerComposeDiscoverer) Discover(ctx context.Context) ([]*Replica, error) {
    // Parse CB_REPLICAS env var
    // Return fixed list (no dynamic changes)
}
```

### 2.3 Pod Discovery Loop

```
Gateway startup:
  1. Call podDiscoverer.Discover()
  2. Parse response → replicas[]
  3. All replicas set to HEALTHY (initial state)
  4. Store in memory: replicas = [cb-0, cb-1, cb-2]

Every 10 seconds:
  └─ Call podDiscoverer.Discover()
      └─ If replicas changed (add/remove): update in-memory list
      └─ Mark new replicas HEALTHY
      └─ Remove deleted replicas from load balancing
```

**Expected discovery latency:** <100ms per cycle

## 3. Health Monitoring

### 3.1 Health Check Mechanism

CB exposes `/api/v1/health` HTTP endpoint. Gateway polls this every 10s.

```go
type HealthStatus struct {
    Status     string  // "OK" or error reason
    Uptime     int64   // seconds
    TopicCount int     // number of topics
    Version    string  // software version
}

func (hc *HealthChecker) Check(replica *Replica) bool {
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    
    url := fmt.Sprintf("http://%s/api/v1/health", replica.RestAddr)
    resp, err := hc.httpClient.Get(url)
    if err != nil {
        replica.consecutiveFailures++
        if replica.consecutiveFailures >= 3 {
            replica.setStatus(StatusUnhealthy)
        }
        return false
    }
    defer resp.Body.Close()
    
    if resp.StatusCode == 200 {
        replica.consecutiveFailures = 0
        replica.setStatus(StatusHealthy)
        return true
    }
    
    replica.consecutiveFailures++
    if replica.consecutiveFailures >= 3 {
        replica.setStatus(StatusUnhealthy)
    }
    return false
}
```

### 3.2 Health Check Loop

```
Every 10 seconds:
  For each replica in replicas[]:
    └─ GET /api/v1/health
        └─ If success (200 + parse OK): mark HEALTHY, reset consecutive failures
        └─ If fail: increment consecutive failures
        └─ If consecutive_failures >= 3: mark UNHEALTHY
        
    Emit metrics:
      └─ cb_gw_replica_health{replica} = 1 (healthy) or 0 (unhealthy)
```

**Failure detection timeline:**
- T=0s: Replica crashes
- T=1s: First failed health check (consecutive_failures=1)
- T=11s: Second failed health check (consecutive_failures=2)
- T=21s: Third failed health check (consecutive_failures=3) → Mark UNHEALTHY
- **RTO: ~20 seconds** (3 checks × 10s interval)

### 3.3 Recovery

Once marked UNHEALTHY, replica is skipped by load balancer. When health check succeeds:

```go
if resp.StatusCode == 200 {
    replica.consecutiveFailures = 0
    replica.setStatus(StatusHealthy)
    return true
}
```

Replica is immediately re-introduced to load balancing pool on next request.

## 4. Replica State

### 4.1 Replica Struct

```go
type Replica struct {
    Name         string        // "cb-0"
    Ordinal      int           // 0, 1, 2
    Address      string        // "10.0.0.1:5052" (gRPC)
    RestAddr     string        // "10.0.0.1:8081" (REST)
    
    // State
    Status       ReplicaStatus
    StatusMu     sync.RWMutex
    
    // Connection pooling
    grpcConn     *grpc.ClientConn
    httpClient   *http.Client
    
    // Metrics
    activeConnections int
    activeConnMu      sync.RWMutex
    requestsTotal     uint64
    latencyMs         uint64
    
    // Health tracking
    lastHealthCheckTime time.Time
    consecutiveFailures int
}

func (r *Replica) IsHealthy() bool {
    r.StatusMu.RLock()
    defer r.StatusMu.RUnlock()
    return r.Status == StatusHealthy
}

func (r *Replica) setStatus(status ReplicaStatus) {
    r.StatusMu.Lock()
    defer r.StatusMu.Unlock()
    r.Status = status
}

func (r *Replica) incrActiveConnections() {
    r.activeConnMu.Lock()
    defer r.activeConnMu.Unlock()
    r.activeConnections++
}

func (r *Replica) decrActiveConnections() {
    r.activeConnMu.Lock()
    defer r.activeConnMu.Unlock()
    r.activeConnections--
}
```

### 4.2 Replica Transitions

```
                ┌─────────────┐
                │   HEALTHY   │
                └──────┬──────┘
                       │
                       │ 3 consecutive failures
                       │
                ┌──────▼──────┐
                │ UNHEALTHY   │
                └──────┬──────┘
                       │
                       │ Health check succeeds
                       │
                ┌──────▼──────┐
                │   HEALTHY   │
                └─────────────┘

Load balancer skips UNHEALTHY replicas:
  if replica.IsHealthy() {
      selected = replica
  }
```

## 5. Monitoring & Metrics

### 5.1 Health Metrics

```
cb_gw_replica_health{replica="cb-0"} = 1    # HEALTHY
cb_gw_replica_health{replica="cb-1"} = 0    # UNHEALTHY

cb_gw_active_connections{replica="cb-0"} = 42
cb_gw_queue_depth{replica="cb-1"} = 5

cb_gw_health_check_latency_ms{replica="cb-0"} = 2
```

### 5.2 Structured Logs

```json
{
  "timestamp": "2026-06-14T12:34:56Z",
  "level": "INFO",
  "event": "health_check_passed",
  "replica": "cb-0",
  "latency_ms": 2,
  "topic_count": 15
}

{
  "timestamp": "2026-06-14T12:34:57Z",
  "level": "WARN",
  "event": "health_check_failed",
  "replica": "cb-1",
  "consecutive_failures": 1,
  "error": "connection refused"
}

{
  "timestamp": "2026-06-14T12:35:17Z",
  "level": "WARN",
  "event": "replica_marked_unhealthy",
  "replica": "cb-1",
  "consecutive_failures": 3,
  "rto_seconds": 20
}
```

## 6. Handling Complete Outage

### 6.1 All Replicas Unhealthy

If >50% of replicas are unhealthy, gateway enters PANIC mode:

```go
func (gw *Gateway) checkPanicMode() {
    gw.replicasMu.RLock()
    defer gw.replicasMu.RUnlock()
    
    healthyCount := 0
    for _, r := range gw.replicas {
        if r.IsHealthy() {
            healthyCount++
        }
    }
    
    panicThreshold := len(gw.replicas) / 2
    if healthyCount <= panicThreshold {
        gw.enterPanicMode()
    }
}

func (gw *Gateway) enterPanicMode() {
    // All new requests return UNAVAILABLE/503
    // Log alert: "CB-GW entering PANIC mode: <N>/<M> replicas unhealthy"
}
```

### 6.2 Panic Mode Behavior

**gRPC:**
```go
return status.Error(codes.Unavailable, "gateway in panic mode")
```

**REST:**
```
HTTP 503 Service Unavailable
Retry-After: 10
```

### 6.3 Recovery from Panic Mode

Once >50% of replicas recover:

```go
if healthyCount > panicThreshold {
    gw.exitPanicMode()
}
```

## 7. Configuration

```bash
# Pod discovery
CB_GW_T2_ENDPOINT=http://etcd-t2:2379
CB_GW_POD_DISCOVERY_INTERVAL_SECONDS=10

# Health checking
CB_GW_HEALTH_CHECK_INTERVAL_SECONDS=10
CB_GW_HEALTH_CHECK_TIMEOUT_SECONDS=5

# Panic mode threshold
CB_GW_PANIC_MODE_THRESHOLD_PERCENT=50

# docker-compose
CB_GW_REPLICAS=cb-0:5052,cb-1:5052,cb-2:5052
CB_GW_REPLICA_DISCOVERY_MODE=static
```

## 8. Testing Strategy

### 8.1 Unit Tests

- Pod discoverer (T2 etcd + docker-compose)
- Health check logic (pass/fail transitions)
- Panic mode detection

### 8.2 Integration Tests

- Pod discovery + health checks in concert
- Replica add/remove during runtime
- All replicas unhealthy → PANIC mode → recovery
- Health check latency measured

### 8.3 Chaos Tests

- Kill CB replica mid-operation → verify load balancer skips unhealthy replica
- Slow health check response → verify timeout handling
- Network partition to one replica → verify failover

## 9. References

- `cb-gw-architecture-hld.md` — Architecture and high-level design
- `cb-gw-low-level-design.md` — Data structures and algorithms
- `cb-gw-implementation-planner.md` — Implementation phases
