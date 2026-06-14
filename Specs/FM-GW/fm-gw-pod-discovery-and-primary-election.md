# FM-Gateway: Pod Discovery & Primary Election Design

> **Status:** Draft v1
> **Module:** FM-Gateway (fm-gw)
> **Focus:** Discovering FM pods, monitoring adapter lease, detecting primary, handling failover
> **Audience:** FM-GW implementers, architects

## 1. Overview

**Goal:** Gateway continuously discovers FM replicas AND detects which replica is the current primary (holds adapter lease). This enables:
- **Primary-aware routing:** Different request types go to primary vs standbys
- **Failover detection:** Automatic transition when primary changes
- **Observability:** Track primary elections, RTO, latency
- **Reliability:** Buffer requests during primary unavailability; don't lose traffic

**Key insight:** Primary election happens in FM via T2 etcd adapter lease. Gateway monitors the same source (T2) to stay in sync.

## 2. Architecture

### 2.1 Discovery Sources (Both from T2 etcd)

```
T2 etcd (fm-cluster-state):
├─ /dashfabric/cluster/pods/fm-0
│  └─ { address: "10.0.0.1:8080", ordinal: 0, status: "running" }
├─ /dashfabric/cluster/pods/fm-1
│  └─ { address: "10.0.0.1:8081", ordinal: 1, status: "running" }
├─ /dashfabric/cluster/pods/fm-2
│  └─ { address: "10.0.0.1:8082", ordinal: 2, status: "running" }
│
└─ /dashfabric/cluster/adapter/lease
   └─ { holder: "fm-1", expires_at: T+15s, version: 42, claimed_at: T }
```

**Gateway reads both:**
1. **Pod list:** Discover all replicas (addresses, ordinals)
2. **Adapter lease:** Detect which pod holds the lease (primary)

### 2.2 Discovery Components

```go
type DiscoveryManager struct {
    // Pod discovery
    podDiscoverer PodDiscoverer          // interface
    podList      []*Replica              // cached pods
    podsMu       sync.RWMutex
    
    // Primary election monitoring
    leaseMonitor LeaseMonitor            // interface
    currentLease *AdapterLease           // cached lease
    leaseChange  chan *LeaseChangeEvent  // notifies on change
    leaseMu      sync.RWMutex
    
    // Derived state
    primaryReplica *Replica    // pointer to current primary
    primaryChanges chan *Replica  // notifies on primary change
    
    // Metrics
    metrics *DiscoveryMetrics
}

type AdapterLease struct {
    Holder      string    // "fm-0", "fm-1", or "fm-2"
    ExpiresAt   int64     // Unix ms
    Version     int64     // for CAS
    ClaimedAt   int64     // Unix ms
    TTL         int64     // seconds
}

type LeaseChangeEvent struct {
    OldHolder   string    // previous primary
    NewHolder   string    // new primary
    Reason      string    // "elected", "expired", "lost"
    Timestamp   time.Time
}
```

## 3. Pod Discovery

### 3.1 Pod List Retrieval (Every 10 seconds)

**Algorithm:**

```
func (dm *DiscoveryManager) discoverPods() {
    // 1. Query T2 etcd for all pod records
    pods := dm.podDiscoverer.List(ctx)
    
    // 2. Sort by ordinal (deterministic order)
    sort.Slice(pods, func(i, j int) bool {
        return pods[i].Ordinal < pods[j].Ordinal
    })
    
    // 3. Update gateway's replica list (atomic)
    dm.podsMu.Lock()
    oldPods := dm.podList
    dm.podList = pods
    dm.podsMu.Unlock()
    
    // 4. Detect additions/removals
    added, removed := diff(oldPods, pods)
    if len(added) > 0 || len(removed) > 0 {
        log.Infof("Pod list changed: added=%v, removed=%v", added, removed)
        dm.metrics.recordPodListChange(len(added), len(removed))
    }
    
    // 5. Return to gateway
    return pods
}
```

**Expected output:**
```
Pod List (sorted by ordinal):
├─ Replica{ Name: "fm-0", Address: "10.0.0.1:8080", Ordinal: 0 }
├─ Replica{ Name: "fm-1", Address: "10.0.0.1:8081", Ordinal: 1 }
└─ Replica{ Name: "fm-2", Address: "10.0.0.1:8082", Ordinal: 2 }
```

### 3.2 Pod Naming Convention

**For Kubernetes (K8s StatefulSet):**
- Pod names: `fm-0`, `fm-1`, `fm-2` (StatefulSet ordinal suffix)
- Service: `fm-0.fm.svc.cluster.local` (DNS A record)
- Ordinal extracted from name: `"fm-1"` → ordinal=1

**For docker-compose:**
- Service names: `fm-0`, `fm-1`, `fm-2` (from service config)
- DNS resolution via docker DNS: `fm-0` → `10.0.0.1` (internally)
- Ordinal from env var: `FM_POD_ORDINAL=0`

**For bare metal/VMs:**
- Hardcoded in config: `replicas: [10.0.0.1:8080, 10.0.0.2:8080, 10.0.0.3:8080]`
- Ordinal: 0, 1, 2 (order in config)

### 3.3 Pod Discoverer Interface

```go
type PodDiscoverer interface {
    // List all healthy pods
    List(ctx context.Context) ([]*PodInfo, error)
    
    // Watch for pod changes (optional; for streaming updates)
    Watch(ctx context.Context) <-chan PodChange
}

type PodInfo struct {
    Name      string  // "fm-0", "fm-1", "fm-2"
    Ordinal   int     // 0, 1, 2
    Address   string  // "10.0.0.1:8080"
    GrpcAddr  string  // "10.0.0.1:5051"
}

// Implementation for K8s
type K8sPodDiscoverer struct {
    etcdClient *clientv3.Client
    namespace  string    // "dashfabric"
    statefulset string   // "fm"
}

func (d *K8sPodDiscoverer) List(ctx context.Context) ([]*PodInfo, error) {
    // Query etcd for /dashfabric/cluster/pods/*
    // Parse pod info from records
    // Return sorted list
}

// Implementation for docker-compose (static)
type DockerComposePodDiscoverer struct {
    replicas []string  // env var FM_REPLICAS="fm-0:8080,fm-1:8080,fm-2:8080"
}

func (d *DockerComposePodDiscoverer) List(ctx context.Context) ([]*PodInfo, error) {
    // Parse comma-separated list
    // Return static list (no changes expected)
}
```

## 4. Adapter Lease Monitoring

### 4.1 Lease Retrieval (Every 5 seconds OR continuous watch)

**Algorithm (Polling):**

```
func (dm *DiscoveryManager) monitorLease() {
    ticker := time.NewTicker(5 * time.Second)
    defer ticker.Stop()
    
    for range ticker.C {
        // 1. Read current lease from T2 etcd
        lease, err := dm.leaseMonitor.Get(ctx)
        if err != nil {
            log.Warnf("Failed to read lease: %v", err)
            dm.metrics.recordLeaseReadError()
            continue
        }
        
        // 2. Check if lease changed
        dm.leaseMu.Lock()
        oldLease := dm.currentLease
        dm.currentLease = lease
        dm.leaseMu.Unlock()
        
        // 3. Detect primary change
        if oldLease == nil || oldLease.Holder != lease.Holder {
            log.Infof("Primary changed: %s → %s", 
                if oldLease != nil { oldLease.Holder } else { "none" },
                lease.Holder)
            
            // 4. Find new primary in replica list
            newPrimary := dm.findReplica(lease.Holder)
            dm.updatePrimary(newPrimary, "elected")
            
            // 5. Notify gateway
            dm.primaryChanges <- newPrimary
        }
        
        // 6. Emit metrics
        dm.metrics.recordLease(lease)
    }
}
```

**Expected output:**
```
Adapter Lease {
  Holder: "fm-1"
  ExpiresAt: 1718400015000 (ms)
  TTL: 14.5s
  Version: 42
  ClaimedAt: 1718400000000
}
```

### 4.2 Lease Structure (from fm-pod-lifecycle-design.md)

```
T2 etcd key: /dashfabric/cluster/adapter/lease

Value:
{
  "holder_pod_name": "fm-1",
  "holder_ordinal": 1,
  "claimed_at_unix_ms": 1718400000000,
  "expires_at_unix_ms": 1718400015000,      // claimed_at + TTL (15s)
  "version": 42,                             // for CAS
  "renewal_interval_seconds": 5
}
```

### 4.3 Lease State Machine (Gateway Perspective)

```
[UNKNOWN] (on startup)
    ↓
    Read lease from T2
    ↓
[LEASE_FOUND]
    ├─ holder: "fm-1"
    ├─ expires_at: T+15s
    └─ Status: PRIMARY DETECTED
    
    (every 5s, update lease)
    
    If lease valid (now < expires_at):
        └─ Primary still: "fm-1"
    
    If lease expired (now > expires_at):
        └─ Status: PRIMARY LOST
        └─ Wait for new primary election (max 15s)
        ├─ If new primary elected: go to LEASE_FOUND
        └─ If timeout: go to NO_PRIMARY
    
    If lease holder changes "fm-1" → "fm-0":
        └─ Status: PRIMARY CHANGED
        └─ Update routing; accept new primary
        
    If no lease found (rare):
        └─ Status: NO_PRIMARY
        └─ Return 503 to requests
        └─ Wait for primary election
```

### 4.4 LeaseMonitor Interface

```go
type LeaseMonitor interface {
    // Get current lease (may be expired)
    Get(ctx context.Context) (*AdapterLease, error)
    
    // Watch for lease changes (streaming; optional)
    Watch(ctx context.Context) <-chan *AdapterLease
}

// Implementation
type T2EtcdLeaseMonitor struct {
    etcdClient *clientv3.Client
    leaseKey   string  // "/dashfabric/cluster/adapter/lease"
    cacheTTL   time.Duration  // cache for up to 10s
}

func (m *T2EtcdLeaseMonitor) Get(ctx context.Context) (*AdapterLease, error) {
    resp, err := m.etcdClient.Get(ctx, m.leaseKey)
    if err != nil {
        return nil, err
    }
    if len(resp.Kvs) == 0 {
        return nil, nil  // no lease
    }
    
    lease := &AdapterLease{}
    json.Unmarshal(resp.Kvs[0].Value, lease)
    
    // Compute TTL
    lease.TTL = (lease.ExpiresAt - time.Now().UnixMilli()) / 1000
    
    return lease, nil
}
```

## 5. Primary Detection Logic

### 5.1 Deriving Primary Replica

```go
func (dm *DiscoveryManager) updatePrimary(newPrimary *Replica, reason string) {
    dm.podsMu.RLock()
    
    // Update all replicas' role
    for _, r := range dm.podList {
        r.Role = STANDBY
        r.IsPrimary = false
    }
    
    // Mark new primary
    if newPrimary != nil {
        newPrimary.Role = PRIMARY
        newPrimary.IsPrimary = true
    }
    
    dm.podsMu.RUnlock()
    
    dm.primaryReplica = newPrimary
    
    // Metrics
    dm.metrics.recordPrimaryChange(newPrimary, reason)
    
    log.Infof("Primary updated: %v (reason: %s)", newPrimary, reason)
}

func (dm *DiscoveryManager) findReplica(name string) *Replica {
    dm.podsMu.RLock()
    defer dm.podsMu.RUnlock()
    
    for _, r := range dm.podList {
        if r.Name == name {
            return r
        }
    }
    return nil  // replica not found
}
```

### 5.2 Primary State in Gateway

```go
type Gateway struct {
    // ... existing fields ...
    
    // Primary management
    primary *Replica            // current primary
    primaryMu sync.RWMutex      // protects primary pointer
    primaryChanges chan *Replica // notifies on primary change
    
    // Primary unavailability handling
    primaryUnavailableAt time.Time // when primary was lost
    primaryWaitTimeout time.Duration // max 20s wait for new primary
}

func (gw *Gateway) getPrimary() (*Replica, bool) {
    gw.primaryMu.RLock()
    defer gw.primaryMu.RUnlock()
    
    if gw.primary == nil {
        return nil, false
    }
    if gw.primary.Status == UNHEALTHY {
        return nil, false
    }
    return gw.primary, true
}

func (gw *Gateway) onPrimaryChange(newPrimary *Replica) {
    gw.primaryMu.Lock()
    gw.primary = newPrimary
    gw.primaryUnavailableAt = time.Time{}  // clear timestamp
    gw.primaryMu.Unlock()
    
    log.Infof("Gateway: Primary updated to %s", newPrimary.Name)
    
    // Drain buffered requests from old primary's queue
    // Route new requests to new primary
}
```

## 6. Routing Strategy (Primary-Aware)

### 6.1 Request Router

```go
func (gw *Gateway) routeRequest(req *Request) (*Replica, error) {
    requestType := parseRequestType(req.Method, req.Path)
    
    switch requestType {
    
    case REGISTRATION:  // POST /api/v1/devices
        return gw.routeToRegistration(req)
        
    case DEVICE_QUERY:  // GET /api/v1/devices
        return gw.routeToQuery(req)
        
    case HEARTBEAT:     // POST /api/v1/devices/{id}/heartbeat
        return gw.routeToHeartbeat(req)
        
    case SUBSCRIBE:     // gRPC Subscribe (long stream)
        return gw.routeToSubscribe(req)
        
    default:
        return gw.routeToPrimary(req)
    }
}

// Route registration to primary only
func (gw *Gateway) routeToRegistration(req *Request) (*Replica, error) {
    primary, ok := gw.getPrimary()
    if !ok {
        return nil, ErrNoPrimary
    }
    return primary, nil
}

// Route query to primary (consistency)
// Alternative: route to any healthy replica (same data via T1 watch)
func (gw *Gateway) routeToQuery(req *Request) (*Replica, error) {
    primary, ok := gw.getPrimary()
    if !ok {
        return nil, ErrNoPrimary
    }
    return primary, nil
}

// Route heartbeat to primary only
func (gw *Gateway) routeToHeartbeat(req *Request) (*Replica, error) {
    primary, ok := gw.getPrimary()
    if !ok {
        return nil, ErrNoPrimary
    }
    return primary, nil
}

// Route Subscribe (gRPC stream) to any healthy replica
// Use least-connections load balancer
func (gw *Gateway) routeToSubscribe(req *Request) (*Replica, error) {
    gw.replicasMu.RLock()
    healthy := filterHealthy(gw.replicas)
    gw.replicasMu.RUnlock()
    
    if len(healthy) == 0 {
        return nil, ErrNoHealthyReplicas
    }
    
    // Least-connections (includes primary + standbys)
    return gw.lb.SelectReplicaForStream(healthy), nil
}

// Fallback: route to primary
func (gw *Gateway) routeToPrimary(req *Request) (*Replica, error) {
    primary, ok := gw.getPrimary()
    if !ok {
        return nil, ErrNoPrimary
    }
    return primary, nil
}
```

### 6.2 Request Type Detection

```go
type RequestType int

const (
    REGISTRATION RequestType = iota
    DEVICE_QUERY
    HEARTBEAT
    TELEMETRY
    SUBSCRIBE  // gRPC
    PUBLISH    // gRPC
    HEALTH
)

func parseRequestType(method, path string) RequestType {
    switch {
    case method == "POST" && path == "/api/v1/devices":
        return REGISTRATION
    case method == "GET" && strings.HasPrefix(path, "/api/v1/devices"):
        return DEVICE_QUERY
    case method == "POST" && strings.Contains(path, "/heartbeat"):
        return HEARTBEAT
    case method == "POST" && strings.Contains(path, "/telemetry"):
        return TELEMETRY
    case method == "GET" && path == "/api/v1/health":
        return HEALTH
    default:
        return DEVICE_QUERY  // default to read-only
    }
}
```

## 7. Failover Scenarios

### 7.1 Primary Crash → New Primary Election

**Timeline:**

```
T=0s:   Primary (fm-1) crashes; process dies
        └─ All connections to fm-1 close

T=1s:   Gateway health check detects fm-1 UNHEALTHY
        └─ Replica[fm-1].Status = UNHEALTHY
        └─ getPrimary() returns (nil, false)

T=1s:   New request arrives → routeToRegistration()
        └─ getPrimary() returns no primary
        └─ Return 503 Service Unavailable
        └─ Include: "Retry-After: 10" (estimated wait)
        └─ Metrics: fm_gw_primary_unavailable_total++

T=2s:   Gateway reads T2 lease
        └─ Lease still valid (holder: fm-1, expires_at: T+15s)
        └─ No primary change detected yet

T=10s:  New request arrives
        └─ Still no primary; return 503
        └─ Client retries with exponential backoff

T=15s:  T2 lease expires (now > expires_at)
        └─ Standbys (fm-0, fm-2) see expired lease
        └─ CAS race: one wins
        └─ Example: fm-0 wins; holds new lease

T=15.1s: Gateway reads T2 lease
        └─ Lease holder changed: fm-1 → fm-0
        └─ LeaseChangeEvent: { OldHolder: "fm-1", NewHolder: "fm-0" }
        └─ updatePrimary(fm-0, "elected")
        └─ Replica[fm-0].IsPrimary = true
        └─ Log: "Primary changed: fm-1 → fm-0"
        └─ Metrics: fm_gw_primary_election_latency_ms = 15100

T=15.2s: New request arrives
        └─ routeToRegistration() calls getPrimary()
        └─ getPrimary() returns fm-0 (PRIMARY, HEALTHY)
        └─ Request queued to fm-0
        └─ Forward succeeds

T=20s:  Normal operation resumes
        └─ fm-0 actively programming DPUs
        └─ All 3 replicas healthy (fm-1 restarted by K8s)

Key metrics:
  - Primary loss detected in: 1s
  - New primary elected in: 15s
  - Total RTO: 15 seconds
  - Requests during outage: 503 Service Unavailable
  - No traffic loss; all requests retried successfully
```

### 7.2 Error Handling in Router

```go
func (gw *Gateway) handleRequest(w http.ResponseWriter, r *http.Request) {
    replica, err := gw.routeRequest(&request)
    
    switch err {
    case ErrNoPrimary:
        // Primary unavailable; check if we're in grace period
        if time.Since(gw.primaryUnavailableAt) < gw.primaryWaitTimeout {
            // Still waiting for new primary
            w.Header().Set("Retry-After", "10")
            http.Error(w, "Primary unavailable; primary election in progress", 
                http.StatusServiceUnavailable)
            gw.metrics.recordRequestToUnavailablePrimary()
            return
        }
        
        // Timeout exceeded; likely system failure
        http.Error(w, "Primary unavailable; no new primary elected", 
            http.StatusServiceUnavailable)
        gw.metrics.recordPrimaryElectionTimeout()
        return
        
    case ErrNoHealthyReplicas:
        http.Error(w, "No healthy replicas", 
            http.StatusServiceUnavailable)
        return
        
    case nil:
        // Routing succeeded; proceed to forward
        response := gw.forwardRequest(replica, request)
        w.WriteHeader(response.Status)
        w.Write(response.Body)
        
    default:
        http.Error(w, err.Error(), 
            http.StatusInternalServerError)
    }
}
```

## 8. Observability

### 8.1 Metrics

```
Primary Election:
  - fm_gw_primary_replica{pod}               // gauge: 1 if primary, 0 if standby
  - fm_gw_primary_election_count_total        // counter: primary changes
  - fm_gw_primary_election_latency_ms         // histogram: RTO per election
  - fm_gw_lease_holder{pod}                   // gauge: current holder

Pod Discovery:
  - fm_gw_replica_count                       // gauge: total replicas
  - fm_gw_replica_health_status{pod}          // gauge: 1=healthy, 0=unhealthy
  - fm_gw_pod_list_changes_total              // counter: pod add/remove

Primary Unavailability:
  - fm_gw_primary_unavailable_seconds_total   // counter: downtime by pod
  - fm_gw_requests_during_primary_loss_total  // counter: 503s due to loss
  - fm_gw_primary_election_timeout_total      // counter: elections that timeout

Request Routing:
  - fm_gw_requests_to_primary_total{method}   // counter: routed to primary
  - fm_gw_requests_to_standby_total{method}   // counter: routed to standbys
```

### 8.2 Logs

```
[INFO] Gateway startup: discovering pods and primary
[INFO] Pod list discovered: 3 replicas (fm-0, fm-1, fm-2)
[INFO] Primary detected: fm-1 (lease expires in 14.8s)

[During normal operation]
[DEBUG] Lease valid: holder=fm-1, expires_in=14.5s

[On primary crash]
[WARN] Primary health check failed: fm-1 (failures: 1/3)
[WARN] Primary health check failed: fm-1 (failures: 2/3)
[ERROR] Primary unhealthy: fm-1 (failures: 3/3)
[WARN] Primary lost; waiting for new primary election (max 20s)

[During primary election grace period]
[INFO] Request received; primary unavailable; queueing
[INFO] Request queued: device_id=dpu-1234 (estimated wait 10s)

[On new primary elected]
[INFO] Lease updated: holder changed fm-1 → fm-0
[INFO] New primary elected: fm-0 (election_latency_ms=150)
[INFO] Draining buffered requests: queued_count=42

[Recovery]
[INFO] Primary recovered: fm-1 (marked healthy after 2 consecutive checks)
[INFO] All 3 replicas healthy; normal operation
```

### 8.3 Traces

```
Span: discovery.update (root)
  ├─ Span: pod.discovery
  │   └─ Attribute: pod_count=3
  └─ Span: lease.monitor
      └─ Attribute: lease_holder=fm-1
      └─ Attribute: lease_ttl_seconds=14.5

Span: gateway.request (per request)
  ├─ Attribute: primary_pod=fm-1
  ├─ Span: routing.decision
  │   └─ Attribute: request_type=registration
  │   └─ Attribute: routed_to=primary
  └─ Span: replica.request
      └─ Attribute: replica=fm-1
```

## 9. Configuration

### 9.1 Environment Variables

```bash
# Pod discovery (K8s)
FM_GW_T2_ENDPOINT=http://etcd-t2.svc.cluster.local:2379
FM_GW_POD_DISCOVERY_INTERVAL_SECONDS=10
FM_GW_POD_DISCOVERY_TIMEOUT_SECONDS=5

# Lease monitoring (primary election)
FM_GW_LEASE_MONITOR_INTERVAL_SECONDS=5
FM_GW_LEASE_MONITOR_TIMEOUT_SECONDS=5
FM_GW_PRIMARY_WAIT_TIMEOUT_SECONDS=20  # max wait for new primary

# Routing strategy
FM_GW_REGISTRATION_TO_PRIMARY_ONLY=true
FM_GW_QUERY_TO_PRIMARY_ONLY=true
FM_GW_STREAM_LOAD_BALANCE=true
FM_GW_HEARTBEAT_TO_PRIMARY_ONLY=true

# docker-compose (static pod list)
FM_GW_REPLICAS=fm-0:8080,fm-1:8080,fm-2:8080
FM_GW_REPLICA_DISCOVERY_MODE=static  # or "t2_etcd"
```

### 9.2 Config File

```yaml
discovery:
  t2_endpoint: "http://etcd-t2.svc.cluster.local:2379"
  pod_discovery:
    interval_seconds: 10
    timeout_seconds: 5
    mode: "t2_etcd"  # or "static"
  
  lease_monitoring:
    interval_seconds: 5
    timeout_seconds: 5
    primary_wait_timeout_seconds: 20
    
routing:
  registration_to_primary: true
  query_to_primary: true
  stream_load_balance: true
  heartbeat_to_primary: true
  
failover:
  buffer_requests_during_loss: true
  buffer_timeout_seconds: 20
  drain_old_primary_queue: true
```

## 10. Testing Strategy

### 10.1 Unit Tests

```
- Test pod list parsing (K8s, docker-compose, static)
- Test lease parsing and expiry calculation
- Test primary detection logic
- Test request type detection
- Test routing logic (registration, query, stream)
- Test primary unavailability handling
- Test replica health state machine
```

### 10.2 Integration Tests

```
- Discover pod list from T2 etcd
- Monitor lease from T2 etcd
- Detect primary change (when lease holder changes)
- Failover scenario: primary crash → new primary elected → requests flow
- Concurrent requests during primary unavailability (buffer and retry)
- Pod add/remove (scaling) and rescan
- Lease expiry and re-election
```

### 10.3 Chaos Tests

```
- Kill primary pod; verify failover latency < 15s
- Kill standby pod; verify requests reroute
- Slow primary (high latency); verify buffer/timeout
- Partition primary (network cut); verify failover
- Kill all replicas; verify 503 responses
- Restart primary; verify re-election, no duplicates
```

## 11. References

- `fm-gw-architecture-hld.md` — Architecture overview
- `fm-gw-low-level-design.md` — Data structures, algorithms
- `fm-pod-lifecycle-design.md` — FM replica lifecycle, adapter lease
- `fm-failover-sequence-design.md` — Primary failover details
