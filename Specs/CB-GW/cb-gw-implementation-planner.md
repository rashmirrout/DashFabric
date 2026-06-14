# CB-Gateway: Implementation Planner

> **Status:** Draft v1
> **Module:** CB-Gateway (cb-gw)
> **Scope:** Phased implementation with deliverables, dependencies, and tracker
> **Audience:** Project managers, implementers, DevOps teams

## 1. Implementation Overview

**Goal:** Deliver a production-grade dual-protocol gateway binary that routes gRPC traffic from FM to CB replicas and exposes REST observability API.

**Timeline:** 6 phases over 7-8 weeks (1 engineer, parallel with FM core)

**Deliverables:**
- `cb-gw` binary (Go)
- Kubernetes manifests (Deployment, Service)
- docker-compose service
- Unit + integration tests
- Operator runbook

## 2. Phase Structure

```
Phase 1: Foundation
├─ Project setup
├─ Core data structures
└─ gRPC + REST listener skeletons

Phase 2: Pod Discovery & Health Monitoring
├─ Pod discovery (T2 etcd + docker-compose)
├─ Health checker
└─ Replica state management

Phase 3: gRPC Request Routing
├─ gRPC Subscribe handler
├─ gRPC Publish handler
└─ Load balancing (least-connections)

Phase 4: REST Observability API
├─ REST listener (:8081)
├─ Topic inspection endpoints
└─ Replica state endpoints

Phase 5: Quality of Service
├─ Request buffering
├─ Rate limiting
├─ Metrics + tracing

Phase 6: Production Hardening
├─ Testing (unit, integration, load)
├─ Kubernetes manifests
├─ Documentation + runbook
└─ Performance tuning
```

---

## Phase 1: Foundation (Week 1)

**Goal:** Project setup, core data structures, HTTP + gRPC listener skeletons.

### Phase 1 Subphases

#### 1.1 Project Setup (1 day)

**Tasks:**
- [ ] Create git repository structure:
  ```
  src/CB-GW/cb-gw-go/
  ├── cmd/cb-gw/main.go
  ├── pkg/
  │   ├── gateway/gateway.go
  │   ├── config/config.go
  │   ├── replica/replica.go
  │   └── metrics/metrics.go
  ├── test/
  │   └── e2e_test.go
  ├── go.mod
  ├── Dockerfile
  ├── docker-compose.yaml
  └── README.md
  ```
- [ ] Set up Go module: `go mod init github.com/sonic-net/dashfabric/cb-gw`
- [ ] Add dependencies: `google.golang.org/grpc`, `net/http`, etcd client
- [ ] Create Makefile: `build`, `test`, `run`, `docker-build`
- [ ] GitHub Actions CI: lint, test, docker-build

**Deliverable:** Buildable project with CI/CD pipeline.

**Estimated effort:** 1 engineer × 1 day

#### 1.2 Core Data Structures (1.5 days)

**Tasks:**
- [ ] Implement `Gateway` struct
- [ ] Implement `Replica` struct with health tracking
- [ ] Implement `Queue` struct for request buffering
- [ ] Implement `Config` struct with YAML parsing
- [ ] Implement `Request`, `Response` types
- [ ] Unit tests: struct initialization, defaults

**Deliverable:** Configuration system + core data structures.

**Estimated effort:** 1 engineer × 1.5 days

#### 1.3 gRPC + REST Listener Skeletons (2 days)

**Tasks:**
- [ ] Set up gRPC server (port 5052)
- [ ] Set up REST HTTP server (port 8081)
- [ ] Create gRPC service stubs (Subscribe, Publish placeholders)
- [ ] Create REST endpoints stubs (GET /api/v1/topics, etc.)
- [ ] Graceful shutdown handlers
- [ ] Unit tests: server startup, graceful shutdown

**Deliverable:** gRPC + REST servers listening on correct ports; all endpoints return 501 Not Implemented.

**Estimated effort:** 1 engineer × 2 days

**Phase 1 Total:** 4.5 days (1 week)

---

## Phase 2: Pod Discovery & Health Monitoring (Week 2)

**Goal:** Discover CB replicas, monitor health, manage replica state.

### Phase 2 Subphases

#### 2.1 Pod Discovery (1.5 days)

**Tasks:**
- [ ] Implement `PodDiscoverer` interface
- [ ] Implement `T2EtcdDiscoverer`:
  - Query `/dashfabric/cluster/pods/cb-*`
  - Parse pod entries (address, rest_address, ordinal)
  - Return sorted []*Replica
- [ ] Implement `DockerComposeDiscoverer`:
  - Parse `CB_REPLICAS` env var
  - Return static []*Replica
- [ ] Pod discovery loop (every 10s)
- [ ] Unit tests: discovery logic, parsing, error handling

**Deliverable:** Dynamic replica discovery from T2 etcd or static config.

**Estimated effort:** 1 engineer × 1.5 days

#### 2.2 Health Checker (1.5 days)

**Tasks:**
- [ ] Implement `HealthChecker` struct
- [ ] Health check loop (every 10s):
  - HTTP GET `/api/v1/health` to each replica
  - Track consecutive failures (threshold=3)
  - Mark HEALTHY or UNHEALTHY
- [ ] Replica state machine (HEALTHY ↔ UNHEALTHY)
- [ ] Panic mode detection (>50% unhealthy)
- [ ] Unit tests: health check transitions, panic mode

**Deliverable:** Health monitoring with failure detection.

**Estimated effort:** 1 engineer × 1.5 days

#### 2.3 Replica State Management (1 day)

**Tasks:**
- [ ] Implement replica list synchronization (RWMutex)
- [ ] Implement per-replica connection tracking (activeConnections)
- [ ] Implement per-replica queue creation
- [ ] Unit tests: state transitions, concurrency

**Deliverable:** Thread-safe replica state management.

**Estimated effort:** 1 engineer × 1 day

**Phase 2 Total:** 4 days (1 week)

---

## Phase 3: gRPC Request Routing (Week 3)

**Goal:** Implement gRPC Subscribe/Publish handlers with load balancing.

### Phase 3 Subphases

#### 3.1 Load Balancer (1 day)

**Tasks:**
- [ ] Implement `LoadBalancer` interface
- [ ] Implement `LeastConnectionsLB`:
  - Count active connections per replica
  - Select replica with fewest connections
- [ ] Implement `RoundRobinLB` (for Publish/REST)
- [ ] Unit tests: load distribution, replica selection

**Deliverable:** Load balancer with least-connections and round-robin strategies.

**Estimated effort:** 1 engineer × 1 day

#### 3.2 gRPC Subscribe Handler (1.5 days)

**Tasks:**
- [ ] Implement `Subscribe` RPC handler:
  - Select replica (least-connections)
  - Track active connections
  - Dial replica (:5052)
  - Proxy bidirectional stream
  - Handle stream close gracefully
- [ ] Implement error handling (unavailable, timeout)
- [ ] Emit metrics (active streams, latency)
- [ ] Integration tests: Subscribe flow

**Deliverable:** gRPC Subscribe streams proxied to CB replicas.

**Estimated effort:** 1 engineer × 1.5 days

#### 3.3 gRPC Publish Handler (1.5 days)

**Tasks:**
- [ ] Implement `Publish` RPC handler:
  - Rate limit per client
  - Select replica (round-robin or least-conn)
  - Queue request
  - Forward to replica :5052
  - Handle timeout, errors
- [ ] Emit metrics (latency, success rate)
- [ ] Integration tests: Publish flow

**Deliverable:** gRPC Publish calls routed to CB replicas.

**Estimated effort:** 1 engineer × 1.5 days

**Phase 3 Total:** 4 days (1 week)

---

## Phase 4: REST Observability API (Week 3-4)

**Goal:** Expose REST API for topic inspection, replica state, metrics.

### Phase 4 Subphases

#### 4.1 REST Topic Inspection (1.5 days)

**Tasks:**
- [ ] Implement `GET /api/v1/topics`:
  - List all topics in CB
  - Proxy to selected CB replica :8081
  - Return JSON
- [ ] Implement `GET /api/v1/topics/{topic}`:
  - Get topic entries
  - Support filtering by key
- [ ] Rate limit per client
- [ ] Unit/integration tests

**Deliverable:** Topic inspection endpoints.

**Estimated effort:** 1 engineer × 1.5 days

#### 4.2 REST Replica State (1 day)

**Tasks:**
- [ ] Implement `GET /api/v1/replicas`:
  - List all replicas with health status
  - Active connections per replica
  - Queue depth per replica
- [ ] Implement `GET /api/v1/replicas/{replica}`:
  - Replica detail: address, health, metrics
- [ ] Unit tests

**Deliverable:** Replica state endpoints.

**Estimated effort:** 1 engineer × 1 day

#### 4.3 Metrics Endpoint (0.5 days)

**Tasks:**
- [ ] Implement `GET /api/v1/metrics` or expose on `:9090/metrics` (Prometheus)
- [ ] Scrape-friendly format

**Deliverable:** Prometheus metrics endpoint.

**Estimated effort:** 1 engineer × 0.5 days

**Phase 4 Total:** 3 days (partial week)

---

## Phase 5: Quality of Service (Week 4-5)

**Goal:** Add buffering, rate limiting, metrics, tracing.

### Phase 5 Subphases

#### 5.1 Request Buffering (1 day)

**Tasks:**
- [ ] Implement `Queue` struct (per-replica)
- [ ] Create queues on startup (one per replica)
- [ ] Queue requests before forwarding
- [ ] Return 503 if queue full
- [ ] Unit/integration tests

**Deliverable:** Per-replica request buffering with backpressure.

**Estimated effort:** 1 engineer × 1 day

#### 5.2 Rate Limiting (1 day)

**Tasks:**
- [ ] Implement `RateLimiter` struct (per-client token bucket)
- [ ] Check limits in gRPC/REST handlers
- [ ] Return RESOURCE_EXHAUSTED (gRPC) or 429 (REST) if exceeded
- [ ] Unit tests: token bucket, rate limiting

**Deliverable:** Per-client rate limiting.

**Estimated effort:** 1 engineer × 1 day

#### 5.3 Prometheus Metrics (1 day)

**Tasks:**
- [ ] Implement metrics export:
  - Replica health status
  - Active connections
  - Queue depth
  - gRPC latency histogram
  - REST latency histogram
  - Rate limit violations
- [ ] Expose on `:9090/metrics`
- [ ] Unit tests

**Deliverable:** Prometheus metrics instrumentation.

**Estimated effort:** 1 engineer × 1 day

#### 5.4 OpenTelemetry Tracing (1 day)

**Tasks:**
- [ ] Add W3C Trace Context propagation
- [ ] Create spans for Subscribe, Publish, REST requests
- [ ] Span attributes: replica, client_id, latency
- [ ] Unit tests

**Deliverable:** Distributed tracing integration.

**Estimated effort:** 1 engineer × 1 day

**Phase 5 Total:** 4 days (1 week)

---

## Phase 6: Production Hardening (Weeks 5-6)

**Goal:** Testing, Kubernetes manifests, documentation, performance tuning.

### Phase 6 Subphases

#### 6.1 Unit Testing (1.5 days)

**Tasks:**
- [ ] Unit tests for all components (>80% coverage):
  - Pod discovery logic
  - Health checker state machine
  - Load balancer selection
  - Rate limiter token bucket
  - gRPC handlers error cases
- [ ] Test mocking and fixtures
- [ ] Test coverage report

**Deliverable:** >80% unit test coverage.

**Estimated effort:** 1 engineer × 1.5 days

#### 6.2 Integration Testing (2 days)

**Tasks:**
- [ ] Integration tests (cbsim or mock CB replicas):
  - Subscribe flow (stream proxy)
  - Publish flow (request-response)
  - REST topic queries
  - Pod discovery + health monitoring in concert
  - Replica failure scenarios (mark unhealthy, recover)
  - All replicas unhealthy → panic mode
  - Rate limiting enforcement
  - Buffering + backpressure
- [ ] Load test: 10k gRPC Publish/sec, 1k Subscribe streams

**Deliverable:** Integration tests passing; load test SLAs met.

**Estimated effort:** 1 engineer × 2 days

#### 6.3 Kubernetes Manifests (1 day)

**Tasks:**
- [ ] Create Deployment manifest (1-3 replicas)
- [ ] Create Service manifest (LoadBalancer, :5052, :8081)
- [ ] Create ConfigMap (config.yaml)
- [ ] Create ServiceMonitor (Prometheus scrape config)
- [ ] Test in minikube or small cluster

**Deliverable:** Kubernetes deployment-ready manifests.

**Estimated effort:** 1 engineer × 1 day

#### 6.4 Documentation & Runbook (1 day)

**Tasks:**
- [ ] Complete operator runbook:
  - How to deploy CB-GW
  - Configuration reference
  - Troubleshooting guide (high latency, replica failures, panic mode)
  - Metrics interpretation
  - Example dashboards
- [ ] Code comments and design docs

**Deliverable:** Production-ready documentation.

**Estimated effort:** 1 engineer × 1 day

#### 6.5 Performance Tuning (1 day)

**Tasks:**
- [ ] Profile gRPC latency (target <10ms p95)
- [ ] Profile REST latency (target <100ms p95)
- [ ] Optimize hot paths (load balancer, rate limiter)
- [ ] Memory profiling (target <100MB)
- [ ] CPU profiling (target <10% for 10k req/s)

**Deliverable:** Performance SLAs met.

**Estimated effort:** 1 engineer × 1 day

**Phase 6 Total:** 6.5 days (1+ weeks)

---

## Overall Timeline

| Phase | Duration | Total |
|-------|----------|-------|
| 1 | 4.5 days | 4.5 days |
| 2 | 4 days | 8.5 days |
| 3 | 4 days | 12.5 days |
| 4 | 3 days | 15.5 days |
| 5 | 4 days | 19.5 days |
| 6 | 6.5 days | 26 days |

**Total:** ~26 days (~5-6 weeks for 1 engineer, parallel with FM core weeks 1-8)

---

## Success Criteria

✅ **Functionality:**
- [ ] gRPC Subscribe streams routed to CB replicas
- [ ] gRPC Publish calls routed to CB replicas
- [ ] REST observability API queryable
- [ ] Health checks detect replica failures
- [ ] Rate limiting enforced
- [ ] Pod discovery working (T2 etcd + docker-compose)

✅ **Performance:**
- [ ] gRPC latency: <10ms p95 (overhead only)
- [ ] REST latency: <100ms p95
- [ ] Throughput: 10k gRPC requests/sec
- [ ] Memory: <100MB

✅ **Reliability:**
- [ ] No request loss (buffering + backpressure)
- [ ] Graceful failover (<30s detection)
- [ ] Circuit breaker prevents cascades
- [ ] >80% test coverage

---

## Task Tracker

| Phase | Task | Status |
|-------|------|--------|
| 1.1 | Project setup | ⚪ Pending |
| 1.2 | Core data structures | ⚪ Pending |
| 1.3 | gRPC + REST listener skeletons | ⚪ Pending |
| 2.1 | Pod discovery | ⚪ Pending |
| 2.2 | Health checker | ⚪ Pending |
| 2.3 | Replica state management | ⚪ Pending |
| 3.1 | Load balancer | ⚪ Pending |
| 3.2 | gRPC Subscribe handler | ⚪ Pending |
| 3.3 | gRPC Publish handler | ⚪ Pending |
| 4.1 | REST topic inspection | ⚪ Pending |
| 4.2 | REST replica state | ⚪ Pending |
| 4.3 | Metrics endpoint | ⚪ Pending |
| 5.1 | Request buffering | ⚪ Pending |
| 5.2 | Rate limiting | ⚪ Pending |
| 5.3 | Prometheus metrics | ⚪ Pending |
| 5.4 | OpenTelemetry tracing | ⚪ Pending |
| 6.1 | Unit testing | ⚪ Pending |
| 6.2 | Integration testing | ⚪ Pending |
| 6.3 | Kubernetes manifests | ⚪ Pending |
| 6.4 | Documentation | ⚪ Pending |
| 6.5 | Performance tuning | ⚪ Pending |

---

## References

- `cb-gw-architecture-hld.md` — High-level design
- `cb-gw-low-level-design.md` — Low-level design
- `cb-gw-pod-discovery-and-replica-monitoring.md` — Pod discovery details
- `../CB/01-cb-architecture-hld.md` — CB architecture
