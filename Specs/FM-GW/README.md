# FM-Gateway (FM-GW) — Complete Design Documentation

> **Status:** Design Complete (Ready for Implementation)
> **Module:** FM-Gateway (fm-gw)
> **Created:** 2026-06-14
> **Audience:** Architects, engineers, operators

## Quick Navigation

| Document | Purpose | Read Time |
|----------|---------|-----------|
| **[fm-gw-architecture-hld.md](./fm-gw-architecture-hld.md)** | High-level architecture, pod discovery, primary election, request routing, components | 20 min |
| **[fm-gw-low-level-design.md](./fm-gw-low-level-design.md)** | Internal data structures, algorithms, concurrency, code patterns, primary-aware routing | 30 min |
| **[fm-gw-pod-discovery-and-primary-election.md](./fm-gw-pod-discovery-and-primary-election.md)** | Detailed pod discovery (K8s/docker-compose), adapter lease monitoring, primary detection logic, failover scenarios | 25 min |
| **[fm-gw-implementation-planner.md](./fm-gw-implementation-planner.md)** | Phased implementation (6 phases, 7-8 weeks), tasks, timeline, tracker | 20 min |

## Overview

**FM-Gateway** is a lightweight, **primary-aware** load balancer that routes HTTP + gRPC traffic from clients to FM (Fleet Manager) replicas. It's the **single entry point** for device registration, status queries, and gRPC streams.

**Key characteristics:**
- Single binary; runs on all deployment tiers (docker-compose → Kubernetes)
- Minimal codebase (~1200 LOC Go)
- **Primary-aware routing:** intelligently routes requests based on request type AND current primary replica
- Pod discovery from T2 etcd + adapter lease monitoring for primary election
- gRPC + HTTP support (L4/L7 load balancing)
- Production-grade observability (metrics, traces, logs)
- Graceful failover (<20s RTO)

## Architecture At-a-Glance

```
Clients (Device Agents, Orchestrators)
         ↓
    FM-Gateway (:8080 HTTP, :5051 gRPC)
    ├─ Pod Discovery (from T2 etcd every 10s)
    ├─ Primary Election Monitor (adapter lease every 5s)
    ├─ Primary-Aware Router (by request type)
    │   ├─ Registrations → PRIMARY only
    │   ├─ Queries → PRIMARY only (consistency)
    │   ├─ Streams → ANY replica (least-connections)
    ├─ Request Buffering (per-replica queues)
    ├─ Health Checking (replica status)
    ├─ Rate Limiting (per-IP, per-device)
    └─ Observability (metrics, traces)
         ↓
    ┌─ fm-0 (Standby/Primary) ├─ fm-1 (Primary/Standby) ├─ fm-2 (Standby) ├─ fm-3 (optional)
    └────────────────────────────────────────────────────────────
         FM Replicas (role determined by adapter lease)
```

## Design Decisions

### 1. Why Primary-Aware Routing?

**Rationale:**
- Device registrations must go to PRIMARY (single writer for CAS semantics)
- Consistency: queries to standby might see stale data (still loading from T1)
- Fairness: gRPC streams load-balanced across all replicas (read-only, identical state)
- RTO: Failover detected within 5s (lease monitor); new primary elected within 15s (lease TTL)

**Trade-off:** More complex gateway logic vs. stronger consistency guarantees

### 2. Pod Discovery from T2 etcd

**Rationale:**
- FM already uses T2 etcd for adapter lease and cluster coordination
- Two discoverers: T2EtcdDiscoverer (K8s) and DockerComposeDiscoverer (static)
- Single binary works everywhere
- No dependency on K8s API; works in docker-compose

### 3. Consistent Hash vs. Round-Robin (for non-primary writes)

**For HTTP (REST):**
- **Chosen: Consistent hash on device_id** (for non-registration requests)
- Benefit: Same device always routes to same replica (warm cache on retry)
- Trade-off: Slightly less even load distribution

**For gRPC (streams):**
- **Chosen: Least-connections**
- Benefit: Long-lived streams distributed evenly
- Trade-off: More complex state tracking

### 4. Request Buffering Strategy

**Chosen: Per-replica queues (1000 depth) with backpressure**
- Absorb traffic spikes
- Return 503 when queue full (don't drop silently)
- Enables graceful degradation under load

## Primary Election & Failover

**Pod Discovery:** Every 10s, read T2 etcd `/dashfabric/cluster/pods/*` to discover FM replicas
- All 3 replicas expected: fm-0, fm-1, fm-2
- Gateway maintains sorted replica list [fm-0, fm-1, fm-2]

**Primary Detection:** Every 5s, read T2 etcd `/dashfabric/cluster/adapter/lease` 
- Adapter lease holder is PRIMARY
- Format: `{holder: "fm-1", expires_at: T+15s, version: 42}`
- TTL: 15 seconds (renewed every 5s by primary)

**Failover Timeline:**
```
T=0s:    Primary pod crashes
T=1s:    Gateway health check detects UNHEALTHY
T=5s:    Gateway lease monitor reads; lease still valid
T=15s:   Adapter lease expires
T=15.05s: Standby wins CAS race; new primary elected
T=15.1s: Gateway detects new primary from lease change
T=15.2s: New requests flow to new primary
```

**RTO (Recovery Time Objective):** 15 seconds (adapter lease TTL)

## Key Components

### HTTP Request Flow (Primary-Aware)

1. **Client sends request** (e.g., POST /api/v1/devices for registration)
2. **Gateway rate-limits** (per-IP, per-device)
3. **Router detects request type:**
   - Registration, Query, Heartbeat → PRIMARY only
   - Other → Load-balanced
4. **If primary unavailable:** Return 503 Service Unavailable with "Retry-After: 10" header
5. **Queue enqueues request** (backpressure if full → 503)
6. **Forward to replica** (HTTP, 30s timeout)
7. **Return response** (200, 201, 409, 503, etc.)

### gRPC Stream Flow (Load-Balanced)

1. **Client opens Subscribe stream**
2. **Load balancer selects replica** (least-connections across ALL healthy replicas)
3. **Gateway proxies stream** (bidirectional)
4. **Replica sends events** → **Gateway forwards to client**
5. **Stream closes** (gracefully; decrement active connection count)

## Observability

**Metrics (Prometheus):**
- `fm_gw_primary_replica{pod}` (gauge: 1=primary, 0=standby)
- `fm_gw_primary_election_count_total` (counter)
- `fm_gw_lease_holder{pod}` (current lease holder)
- `fm_gw_requests_total{method, status, replica, replica_role}`
- `fm_gw_request_duration_ms{percentile}`
- `fm_gw_replica_health_status{replica}`
- `fm_gw_active_connections{replica}`
- `fm_gw_queue_depth{replica}`

**Traces (W3C Trace Context):**
- Root span: `gateway.request`
- Children: `rate_limiting`, `router.primary_check`, `load_balancer.select`, `queue.wait`, `replica.request`
- Attribute: `replica_pod`, `replica_role` (primary/standby)

**Logs (JSON structured):**
- Pod discovery events
- Primary election changes ("New primary elected: fm-0")
- Replica health changes
- Primary unavailability windows
- Rate limit violations
- Errors (forward failures, timeouts)

## Configuration

**Environment Variables:**
```bash
# Pod discovery (T2 etcd)
FM_GW_T2_ENDPOINT=http://etcd-t2:2379
FM_GW_POD_DISCOVERY_INTERVAL_SECONDS=10
FM_GW_LEASE_MONITOR_INTERVAL_SECONDS=5

# Primary handling
FM_GW_PRIMARY_WAIT_TIMEOUT_SECONDS=20

# Routing strategy
FM_GW_REGISTRATION_TO_PRIMARY_ONLY=true
FM_GW_QUERY_TO_PRIMARY_ONLY=true

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

## Deployment

**Docker-Compose (ultra-small):**
```yaml
services:
  fm-gw:
    image: dashfabric/fm-gw:latest
    ports: ["8080:8080", "5051:5051"]
    environment:
      FM_GW_REPLICAS: "fm-0:8080,fm-1:8080,fm-2:8080"
      FM_GW_REPLICA_DISCOVERY_MODE: "static"
```

**Kubernetes (small/medium/large):**
```yaml
Deployment: fm-gw (1-3 replicas)
Service: LoadBalancer (exposes :8080, :5051)
ConfigMap: fm-gw-config.yaml
```

## Success Criteria

✅ **Functionality:**
- HTTP requests routed to FM replicas (primary-aware for registrations)
- gRPC Subscribe streams proxied with load balancing
- Health checks detect replica failures
- Rate limiting enforced
- Primary failover detected within 15s

✅ **Performance:**
- Gateway latency: <10ms p95 (overhead only)
- Throughput: 10k req/s
- Memory: <100MB
- CPU: <10% for 10k req/s

✅ **Reliability:**
- No request loss (buffering + backpressure)
- Graceful failover (RTO 15s)
- Circuit breaker prevents cascades
- >80% test coverage

## Next Steps

1. **Review design** (this folder)
2. **Get architecture approval** (arch review)
3. **Start Phase 1** (project setup, foundation)
4. **Track progress** (use `fm-gw-implementation-planner.md` tracker)

## Related Documentation

- `../fm-api-gateway-design.md` — Gateway requirements (parent doc)
- `../fm-pod-lifecycle-design.md` — FM replica lifecycle
- `../fm-device-registration-api-design.md` — API semantics
- `../fm-device-registration-flow-design.md` — Registration across 3 replicas
- `../fm-failover-sequence-design.md` — Primary failover step-by-step
- `../storage-architecture.md` — T1/T2 etcd details
- `../deployment-tiers.md` — Customer tiers and configurations

## Questions?

Refer to the specific design doc:
- **"How does it work?"** → HLD (§2-4)
- **"How does primary election work?"** → Pod Discovery & Primary Election (§4-7)
- **"How is it implemented?"** → LLD (§2-4)
- **"What about failover?"** → Pod Discovery & Primary Election (§7) or HLD (§5)
- **"What are the tasks?"** → Implementation Planner (§2-6)
- **"What about metrics?"** → HLD (§7) or README (§Observability)
