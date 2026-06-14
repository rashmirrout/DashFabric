# CB-Gateway (CB-GW) — Complete Design Documentation

> **Status:** Design Complete (Ready for Implementation)
> **Module:** CB-Gateway (cb-gw)
> **Created:** 2026-06-14
> **Audience:** Architects, engineers, operators

## Quick Navigation

| Document | Purpose | Read Time |
|----------|---------|-----------|
| **[cb-gw-architecture-hld.md](./cb-gw-architecture-hld.md)** | High-level architecture, dual protocols (gRPC + REST), replica discovery, load balancing | 20 min |
| **[cb-gw-low-level-design.md](./cb-gw-low-level-design.md)** | Internal data structures, gRPC/REST handlers, concurrency model | 30 min |
| **[cb-gw-pod-discovery-and-replica-monitoring.md](./cb-gw-pod-discovery-and-replica-monitoring.md)** | CB replica discovery (no primary), health monitoring, failover handling | 20 min |
| **[cb-gw-implementation-planner.md](./cb-gw-implementation-planner.md)** | Phased implementation (6 phases, 7-8 weeks), tasks, timeline, tracker | 20 min |

## Overview

**CB-Gateway** is a lightweight dual-protocol gateway that routes gRPC traffic from FM Adapter to CB (ControllerBridge) replicas and exposes REST observability API to dashboards and debug tools.

**Key characteristics:**
- Single binary; runs on all deployment tiers (docker-compose → Kubernetes)
- Minimal codebase (~1500 LOC Go)
- **Dual protocols:** gRPC for FM→CB (high performance), REST for observability clients (dashboards, curl)
- **Replica-aware routing:** Load-balanced across all healthy CB replicas (all replicas are peers; no primary election)
- Pod discovery from T2 etcd every 10s
- gRPC Subscribe/Publish routing + buffering
- REST topic inspection, metrics, debug APIs
- Production-grade observability (metrics, traces, logs)

## Architecture At-a-Glance

```
FM Adapter                           Observability Clients
    ↓                                     ↓
    └─ gRPC (:5052)                REST (:8081)
       Subscribe/Publish               Query Topics
                 ↓                        ↓
         CB-Gateway (:5052 gRPC, :8081 REST)
         ├─ Pod Discovery (from T2 etcd every 10s)
         ├─ Replica Health Monitor (every 10s)
         ├─ gRPC Subscribe/Publish Router (load-balanced)
         ├─ REST Observability API
         ├─ Request Buffering (per-replica queues)
         ├─ Rate Limiting (per-client)
         └─ Observability (metrics, traces, logs)
                 ↓
         ┌─ cb-0 ├─ cb-1 ├─ cb-2 (all peers, no primary)
         └───────────────────────
              CB Replicas
         (symmetric topic brokers)
```

## Design Decisions

### 1. Why Dual Protocols (gRPC + REST)?

**Rationale:**
- **gRPC for FM Adapter** — High performance, long-lived Subscribe streams, binary encoding
- **REST for Observability** — Integrates with dashboards, monitoring tools, curl/Postman-friendly
- Separation of concerns: data path (gRPC) vs observability path (REST)

**Trade-off:** More code to maintain (gRPC + REST servers) vs better UX for operators

### 2. No Primary Election (All Replicas are Peers)

**Rationale:**
- CB topics are eventually-consistent (all replicas converge to same state via topic store)
- No single writer constraint (unlike FM where only PRIMARY writes to T1)
- All CB replicas can accept Subscribe/Publish operations
- Simpler routing logic

**Trade-off:** No strong consistency guarantee (eventual consistency accepted for CB)

### 3. Load Balancing Strategy

**For gRPC Subscribe (long-lived streams):**
- **Least-connections** — Distributes streams evenly across all healthy replicas

**For gRPC Publish (request-response):**
- **Round-robin or least-connections** — Any healthy replica

**For REST queries:**
- **Round-robin** — Stateless, each query can go anywhere

**Rationale:** All replicas are peers; no preference for any replica

### 4. Replica Discovery from T2 etcd

**Rationale:**
- Same as FM-GW; consistent infrastructure
- K8s StatefulSet: `cb-0`, `cb-1`, `cb-2` registered in T2 etcd
- docker-compose: static list via env var `CB_REPLICAS`

### 5. Request Buffering & Backpressure

**Chosen: Per-replica queues (1000 depth)**
- Absorb traffic spikes
- Return 503 when queue full (don't drop silently)
- Enables graceful degradation under load

## Key Components

### gRPC Request Flow (FM Adapter → CB)

1. **FM Adapter connects** to CB-GW (:5052)
2. **Subscribe RPC** — Opens long-lived stream to CB replica
   - Load balancer selects replica (least-connections)
   - Stream proxied to selected CB replica
   - FM receives config events from CB
3. **Publish RPC** — Sends acks to CB replica
   - Load balancer selects replica (round-robin or least-conn)
   - Request queued, forwarded to replica
   - Response returned to FM

### REST Observability Flow (Dashboards → CB)

1. **Dashboard queries** CB-GW REST API (:8081)
   - `GET /api/v1/topics` — List all topics
   - `GET /api/v1/topics/<topic>` — Get topic entries
   - `GET /api/v1/replicas` — Get replica health status
   - `GET /api/v1/metrics` — Prometheus metrics
   - `GET /api/v1/debug/<replica>` — Replica state dump

2. **Load balancer selects replica** (round-robin)
3. **Query proxied** to selected replica
4. **Response returned** to dashboard

## Configuration

**Environment Variables:**
```bash
# Pod discovery (T2 etcd)
CB_GW_T2_ENDPOINT=http://etcd-t2:2379
CB_GW_POD_DISCOVERY_INTERVAL_SECONDS=10

# Listeners
CB_GW_GRPC_PORT=5052
CB_GW_REST_PORT=8081

# Load balancing
CB_GW_LB_ALGORITHM=least_connections  # for gRPC Subscribe

# Buffering
CB_GW_QUEUE_DEPTH_PER_REPLICA=1000
CB_GW_REQUEST_TIMEOUT_SECONDS=30

# Health checking
CB_GW_HEALTH_CHECK_INTERVAL_SECONDS=10
CB_GW_HEALTH_CHECK_TIMEOUT_SECONDS=5

# Rate limiting
CB_GW_RATE_LIMIT_PER_CLIENT_PER_MIN=1000

# docker-compose (static list)
CB_GW_REPLICAS=cb-0:5052,cb-1:5052,cb-2:5052
CB_GW_REPLICA_DISCOVERY_MODE=static  # or "t2_etcd"
```

## Deployment

**Docker-Compose (ultra-small):**
```yaml
services:
  cb-gw:
    image: dashfabric/cb-gw:latest
    ports: ["5052:5052", "8081:8081"]
    environment:
      CB_GW_REPLICAS: "cb-0:5052,cb-1:5052,cb-2:5052"
      CB_GW_REPLICA_DISCOVERY_MODE: "static"
```

**Kubernetes (small/medium/large):**
```yaml
Deployment: cb-gw (1-3 replicas)
Service: LoadBalancer (exposes :5052, :8081)
ConfigMap: cb-gw-config.yaml
```

## Observability

**Metrics (Prometheus):**
- `cb_gw_replica_health{replica}` (gauge: 1=healthy, 0=unhealthy)
- `cb_gw_active_connections{replica}` (gauge: active gRPC Subscribe streams)
- `cb_gw_grpc_request_duration_ms{method, replica}` (histogram)
- `cb_gw_rest_request_duration_ms{endpoint, replica}` (histogram)
- `cb_gw_queue_depth{replica}` (gauge)
- `cb_gw_rate_limit_violations_total{client}` (counter)

**Traces (W3C Trace Context):**
- Root span: `cb-gw.request`
- Children: `replica.select`, `queue.wait`, `grpc.forward`, `rest.query`

**Logs (JSON structured):**
- Replica discovery events
- Health check status changes
- Rate limit violations
- Errors (forward failures, timeouts)

## Success Criteria

✅ **Functionality:**
- gRPC Subscribe/Publish routed to CB replicas
- REST observability API queryable
- Health checks detect replica failures
- Rate limiting enforced

✅ **Performance:**
- gRPC latency: <10ms p95 (overhead only)
- REST query latency: <100ms p95
- Throughput: 10k gRPC requests/sec
- Memory: <100MB

✅ **Reliability:**
- No request loss (buffering + backpressure)
- Graceful failover (<30s detection)
- Circuit breaker prevents cascades
- >80% test coverage

## Next Steps

1. **Review design** (this folder)
2. **Get architecture approval** (arch review)
3. **Start Phase 1** (project setup, foundation)
4. **Track progress** (use `cb-gw-implementation-planner.md` tracker)

## Related Documentation

- `../cb-architecture-hld.md` — CB requirements and overall architecture
- `../CB/02-cb-low-level-design-lld.md` — CB internal design (topic store, translation core)
- `../FM-GW/fm-gw-architecture-hld.md` — FM-Gateway design (analogous pattern)
- `../FM/fm-comprehensive-lld.md` — FM architecture and registry design

## Questions?

Refer to the specific design doc:
- **"How does it work?"** → HLD (§2-4)
- **"How is it implemented?"** → LLD (§2-4)
- **"What about failover?"** → Pod Discovery & Replica Monitoring (§4-6)
- **"What are the tasks?"** → Implementation Planner (§2-6)
- **"What about metrics?"** → HLD (§Observability) or README
