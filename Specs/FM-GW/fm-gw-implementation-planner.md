# FM-Gateway: Implementation Planner

> **Status:** Draft v1
> **Module:** FM-Gateway (fm-gw)
> **Scope:** Phased implementation with deliverables, dependencies, and tracker
> **Audience:** Project managers, implementers, DevOps teams

## 1. Implementation Overview

**Goal:** Deliver a production-grade gateway binary that routes HTTP + gRPC traffic to FM replicas with load balancing, buffering, and observability.

**Timeline:** 6 phases over 6-8 weeks (1 engineer)

**Deliverables:**
- `fm-gw` binary (Go)
- Kubernetes manifests (Deployment, Service)
- docker-compose service (for ultra-small tier)
- Helm chart (optional; Phase 6)
- Unit + integration tests
- Operator runbook

## 2. Phase Structure

```
Phase 1: Foundation
├─ Project setup
├─ Core data structures
└─ HTTP listener skeleton

Phase 2: Request Routing
├─ Load balancer (consistent hash)
├─ Request forwarding (HTTP)
├─ Error handling

Phase 3: gRPC Streaming
├─ gRPC listener
├─ Subscribe/Publish proxying
├─ Stream lifecycle

Phase 4: Quality of Service
├─ Request buffering
├─ Health checking
├─ Rate limiting

Phase 5: Observability
├─ Metrics (Prometheus)
├─ Tracing (W3C)
├─ Logging

Phase 6: Production Hardening
├─ Testing (unit, integration, load)
├─ Helm chart
├─ Documentation
└─ Performance tuning
```

---

## Phase 1: Foundation (Week 1)

**Goal:** Project setup, core data structures, skeleton HTTP listener.

### Phase 1 Subphases

#### 1.1 Project Setup (1 day)

**Tasks:**
- [ ] Create git repository structure:
  ```
  fm-gw/
  ├── cmd/fm-gw/main.go
  ├── pkg/
  │   ├── gateway/gateway.go
  │   ├── config/config.go
  │   ├── replica/replica.go
  │   └── metrics/metrics.go
  ├── test/
  │   └── e2e_test.go
  ├── go.mod
  ├── go.sum
  ├── Dockerfile
  ├── docker-compose.yaml
  └── README.md
  ```
- [ ] Set up Go module: `go mod init github.com/sonic-net/dashfabric/fm-gw`
- [ ] Add dependencies: `net/http`, `google.golang.org/grpc`, `etcd/client`
- [ ] Create Makefile: `build`, `test`, `run`, `docker-build`
- [ ] GitHub Actions CI: lint, test, docker-build

**Deliverable:** Buildable project with CI/CD pipeline.

**Estimated effort:** 1 engineer × 1 day

#### 1.2 Core Data Structures (2 days)

**Tasks:**
- [ ] Implement `Gateway` struct (from LLD §2.1)
- [ ] Implement `Replica` struct (from LLD §2.2)
- [ ] Implement `Config` struct with YAML parsing
  ```go
  type Config struct {
      HTTPPort int
      GrpcPort int
      MetricsPort int
      Replicas ReplicaConfig
      LoadBalancer LBConfig
      Buffering BufferingConfig
      HealthCheck HealthCheckConfig
      RateLimiting RateLimitConfig
  }
  ```
- [ ] Unit tests: config parsing, defaults
- [ ] Environment variable override support

**Deliverable:** Configuration system that reads from YAML and env vars.

**Estimated effort:** 1 engineer × 2 days

#### 1.3 HTTP Listener Skeleton (2 days)

**Tasks:**
- [ ] Implement HTTP server startup (port 8080)
- [ ] Create request router (mux) with basic endpoints:
  ```
  GET    /metrics
  GET    /api/v1/health
  POST   /api/v1/devices          (forward to replica)
  GET    /api/v1/devices          (forward to replica)
  GET    /api/v1/devices/{id}     (forward to replica)
  ```
- [ ] Implement HTTP handler skeleton (no forwarding yet)
  ```go
  func (gw *Gateway) handleRequest(w http.ResponseWriter, r *http.Request) {
      // Parse request
      // TODO: rate limit
      // TODO: select replica
      // TODO: forward
      http.Error(w, "not implemented", http.StatusNotImplemented)
  }
  ```
- [ ] Graceful shutdown handler (SIGTERM)
- [ ] Unit tests: server startup, graceful shutdown

**Deliverable:** HTTP server that listens on :8080 and responds with 501 Not Implemented.

**Estimated effort:** 1 engineer × 2 days

**Phase 1 Total:** 5 days (1 week)

---

## Phase 2: Request Routing (Week 2)

**Goal:** Implement load balancer and HTTP request forwarding.

### Phase 2 Subphases

#### 2.1 Pod Discovery & Primary Election (2 days)

**Tasks:**
- [ ] Implement `PodDiscoverer` interface:
  ```go
  type PodDiscoverer interface {
      Discover(ctx context.Context) ([]*Replica, error)
  }
  ```
- [ ] Implement `T2EtcdDiscoverer`:
  - Query T2 etcd `/dashfabric/cluster/pods/{pod_name}` keys
  - Parse pod address, port, ordinal from response
  - Return []*Replica sorted by ordinal (deterministic)
- [ ] Implement `DockerComposeDiscoverer`:
  - Parse env var `FM_REPLICAS=fm-0:8080,fm-1:8080,fm-2:8080`
  - Return fixed []*Replica (no watching)
- [ ] Implement `LeaseMonitor` interface:
  ```go
  type LeaseMonitor interface {
      Get(ctx context.Context) (holder string, expiresAt int64, err error)
  }
  ```
- [ ] Implement `T2LeaseMonitor`:
  - Read `/dashfabric/cluster/adapter/lease` from T2 etcd
  - Parse JSON: `{holder: "fm-1", expires_at: T+15000, version: 42}`
  - Return holder and expiresAt
- [ ] Implement `DiscoveryManager`:
  - Manages pod list and current primary
  - Runs pod discovery loop (every 10s)
  - Runs lease monitoring loop (every 5s)
  - Provides `GetPrimary()` and `UpdatePrimary()` methods
- [ ] Add Replica.Role (PRIMARY/STANDBY) with getter/setter
- [ ] Unit tests: pod discovery, lease parsing, primary update

**Deliverable:** Pod discovery and primary election detection infrastructure.

**Estimated effort:** 1 engineer × 2 days

#### 2.2 Load Balancer (1 day)

**Tasks:**
- [ ] Implement `LoadBalancer` interface (from LLD §2.5):
  ```go
  type LoadBalancer interface {
      SelectReplica(deviceID string, replicas []*Replica) *Replica
      SelectReplicaForStream(replicas []*Replica) *Replica
  }
  ```
- [ ] Implement `ConsistentHashLB`:
  - Use FNV hash on device_id
  - Deterministic; same device always routes to same replica
- [ ] Implement `RoundRobinLB` (fallback):
  - Simple round-robin
  - For requests without device_id
- [ ] Unit tests: consistent hashing, replica distribution

**Deliverable:** Load balancer that selects replicas based on device_id.

**Estimated effort:** 1 engineer × 1 day

#### 2.3 HTTP Request Forwarding & Primary-Aware Routing (2 days)

**Tasks:**
- [ ] Implement `RequestType` enum (Registration, Query, Heartbeat, Unknown)
- [ ] Implement `parseRequestType(r *http.Request)` helper:
  - POST /api/v1/devices → Registration (PRIMARY only)
  - GET /api/v1/devices → Query (PRIMARY only)
  - POST /api/v1/heartbeat → Heartbeat (PRIMARY only)
  - Other → Unknown (load-balanced)
- [ ] Implement primary-aware request router:
  - For registration/query/heartbeat: call `gw.discoveryManager.GetPrimary()`
  - If primary unavailable: return 503 with "Retry-After: 10" header
  - Track primary unavailability timestamp; timeout after 20s max
  - For unknown requests: use load balancer with consistent hash
- [ ] Implement `forwardRequest(replica, req)` (from LLD §3.2):
  - Create HTTP request to replica
  - Forward headers (including X-Trace-ID, X-Shard-ID if set)
  - Handle response
  - Record latency and replica role (primary vs standby)
- [ ] Implement timeout handling (30s default, configurable)
- [ ] Implement error responses:
  - 502 Bad Gateway (replica unreachable)
  - 504 Gateway Timeout (replica slow)
  - 503 Service Unavailable (primary unavailable during election)
- [ ] Update HTTP handler to use primary-aware router
- [ ] Integration tests: forward to primary, failover on primary unavailable

**Deliverable:** HTTP requests routed to primary (for writes) or load-balanced (for reads); primary unavailability handled gracefully.

**Estimated effort:** 1 engineer × 2 days

**Phase 2 Total:** 5 days (1+ weeks)

---

## Phase 3: gRPC Streaming (Week 3)

**Goal:** Implement gRPC Subscribe/Publish stream proxying.

### Phase 3 Subphases

#### 3.1 gRPC Listener (1 day)

**Tasks:**
- [ ] Set up gRPC server (port 5051)
- [ ] Generate gRPC service stubs from `cb_fm_protos/`:
  ```bash
  protoc --go_out=. --go-grpc_out=. cb_fm_protos/service/cb_service.proto
  ```
- [ ] Implement `CB_SubscribeServer` interface stub
- [ ] Implement `CB_PublishServer` interface stub (if bidirectional needed)
- [ ] Graceful shutdown for gRPC server
- [ ] Unit tests: gRPC server startup

**Deliverable:** gRPC server listening on :5051; can accept Subscribe/Publish calls.

**Estimated effort:** 1 engineer × 1 day

#### 3.2 Subscribe Stream Proxying (2 days)

**Tasks:**
- [ ] Implement `Subscribe` RPC handler (from LLD §3.3):
  ```go
  func (gw *Gateway) Subscribe(req *pb.SubscribeRequest, stream pb.CB_SubscribeServer) error {
      // Select replica using least-connections LB
      // (can select from ANY healthy replica, including primary)
      // Dial replica
      // Open stream to replica
      // Proxy stream (client → replica → client)
  }
  ```
- [ ] Implement least-connections load balancer (from LLD §2.6):
  - Count active connections per replica
  - Select replica with fewest active connections
  - Provides fairness for long-lived streams
- [ ] Track active connections (increment on stream start, decrement on stream close)
- [ ] Implement bidirectional proxying:
  - Client receives → forward to replica
  - Replica sends → forward to client
  - Handle both directions concurrently (goroutines)
- [ ] Handle stream close gracefully (client close, replica close, error)
- [ ] Error handling (replica unreachable, stream error)
- [ ] Metrics: active connections, stream count, stream latency
- [ ] Integration tests: full Subscribe flow to primary and standbys

**Deliverable:** gRPC Subscribe streams proxied to FM replicas with load balancing.

**Estimated effort:** 1 engineer × 2 days

#### 3.3 Publish Stream Proxying (1 day)

**Tasks:**
- [ ] Implement `Publish` RPC handler (if needed):
  - FM publishes acks
  - Route to replica (usually same replica as Subscribe; or primary)
- [ ] Handle bidirectional flow (acks from FM)
- [ ] Metrics: publish latency, success/error
- [ ] Integration tests

**Deliverable:** gRPC Publish streams proxied.

**Estimated effort:** 1 engineer × 1 day

**Phase 3 Total:** 4 days (~1 week)

---

## Phase 4: Quality of Service (Week 3-4)

**Goal:** Buffering, health checks, rate limiting.

### Phase 4 Subphases

#### 4.1 Request Buffering (1.5 days)

**Tasks:**
- [ ] Implement `Queue` struct (from LLD §2.3):
  - Per-replica buffered channel (size 1000)
  - Metrics: queue depth, dropped count
- [ ] Create queues on gateway startup (one per replica)
- [ ] Update HTTP handler to enqueue requests:
  ```go
  select {
  case queue.ch <- request:
      // Queued
  default:
      // Queue full; backpressure
      return 503
  }
  ```
- [ ] Process requests from queue (in separate goroutine)
- [ ] Backpressure test: verify 503 when queue full
- [ ] Metrics: queue depth, queue wait time

**Deliverable:** Requests buffered per-replica; backpressure when full.

**Estimated effort:** 1 engineer × 1.5 days

#### 4.2 Health Checking (1.5 days)

**Tasks:**
- [ ] Implement `HealthChecker` struct (from LLD §2.6):
  - Poll `/api/v1/health` every 10s
  - Timeout 5s
  - Mark replica HEALTHY/UNHEALTHY based on consecutive passes/failures
- [ ] Background health check loop:
  ```go
  func (hc *HealthChecker) run() {
      ticker := time.NewTicker(hc.interval)
      for range ticker.C {
          for _, replica := range gw.replicas {
              go hc.Check(replica)
          }
      }
  }
  ```
- [ ] Update request handler to skip UNHEALTHY replicas
- [ ] Circuit breaker: if >50% replicas unhealthy, panic mode (or 503)
- [ ] Metrics: replica health status
- [ ] Integration tests: health check success/failure

**Deliverable:** Health checks running; replicas marked healthy/unhealthy; circuit breaker.

**Estimated effort:** 1 engineer × 1.5 days

#### 4.3 Rate Limiting (1.5 days)

**Tasks:**
- [ ] Implement `RateLimiter` struct (from LLD §2.4):
  - Token bucket per-IP (1000 req/min)
  - Token bucket per-device (100 reg/min)
  - Token bucket per-idempotency-key (10 identical/min)
- [ ] Implement token bucket refill logic
- [ ] Update HTTP handler to check rate limit before forwarding:
  ```go
  if !gw.rateLimiter.AllowPerIP(clientIP) {
      return 429 Too Many Requests
  }
  ```
- [ ] Extract device_id and idempotency_key from request
- [ ] Rate limit response format (include Retry-After)
- [ ] Metrics: rate limit violations
- [ ] Unit tests: token bucket, rate limit logic

**Deliverable:** Rate limiting enforced on HTTP requests.

**Estimated effort:** 1 engineer × 1.5 days

**Phase 4 Total:** 4.5 days (~1 week)

---

## Phase 5: Observability (Week 4)

**Goal:** Metrics, tracing, logging.

### Phase 5 Subphases

#### 5.1 Prometheus Metrics (1.5 days)

**Tasks:**
- [ ] Add Prometheus client library: `go get github.com/prometheus/client_golang`
- [ ] Implement `Metrics` struct:
  ```go
  type Metrics struct {
      requestsTotal prometheus.Counter
      requestDuration prometheus.Histogram
      activeConnections prometheus.Gauge
      queueDepth prometheus.GaugeVec
      rateLimitViolations prometheus.Counter
      replicaHealthStatus prometheus.GaugeVec
  }
  ```
- [ ] Register metrics on startup
- [ ] Record metrics in handlers (after each request)
- [ ] Expose `/metrics` endpoint (Prometheus format)
- [ ] Unit tests: metric recording

**Deliverable:** Prometheus metrics exposed on :9090/metrics.

**Estimated effort:** 1 engineer × 1.5 days

#### 5.2 Distributed Tracing (1.5 days)

**Tasks:**
- [ ] Add tracing library: `go get go.opentelemetry.io/otel`
- [ ] Implement trace context propagation (W3C):
  - Extract X-Trace-ID from request headers
  - Generate trace ID if missing
  - Pass trace ID to replicas (X-Trace-ID header)
- [ ] Create spans for major operations (from LLD §9.3):
  - gateway.request (root)
  - rate_limiting
  - load_balancer.select
  - queue.wait
  - replica.request
- [ ] Implement span attributes (trace_id, replica, device_id)
- [ ] Export traces to stdout or OTLP endpoint (configurable)
- [ ] Unit tests: trace context propagation

**Deliverable:** Distributed traces enabled; context propagation working.

**Estimated effort:** 1 engineer × 1.5 days

#### 5.3 Structured Logging (1 day)

**Tasks:**
- [ ] Add logging library: `go get github.com/sirupsen/logrus`
- [ ] Create logger at startup:
  ```go
  log := logrus.New()
  log.SetFormatter(&logrus.JSONFormatter{})
  log.SetLevel(logrus.InfoLevel)
  ```
- [ ] Add log statements at key points:
  - Gateway startup/shutdown
  - Replica health changes
  - Rate limit violations
  - Backpressure events (queue full)
  - Errors (forward failures, timeouts)
- [ ] Structured fields: trace_id, replica, device_id, latency_ms
- [ ] Unit tests: log output format

**Deliverable:** JSON structured logs with trace context.

**Estimated effort:** 1 engineer × 1 day

**Phase 5 Total:** 4 days (1 week)

---

## Phase 6: Production Hardening (Week 5-6)

**Goal:** Testing, documentation, deployment readiness.

### Phase 6 Subphases

#### 6.1 Unit Testing (2 days)

**Tasks:**
- [ ] Test coverage goal: >80%
- [ ] Tests for each package:
  - `config/`: config parsing, env var override, defaults
  - `replica/`: replica discovery, health status updates
  - `loadbalancer/`: consistent hash, least-connections, round-robin
  - `ratelimiter/`: token bucket, rate limit enforcement
  - `gateway/`: request forwarding, error handling, metrics recording
- [ ] Mocking: mock FM replicas (fake HTTP server)
- [ ] Run tests in CI/CD: `make test`

**Deliverable:** >80% code coverage; all tests passing.

**Estimated effort:** 1 engineer × 2 days

#### 6.2 Integration Testing (2 days)

**Tasks:**
- [ ] End-to-end tests:
  - Start fm-gw + 3 FM replicas (mock or real)
  - Send HTTP request; verify forwarded to correct replica
  - Send gRPC Subscribe stream; verify proxied to replica
  - Test failover: kill one replica; verify requests routed to others
  - Test circuit breaker: kill all replicas; verify 503 response
- [ ] Load test: 1000 req/s for 60s; measure latency, throughput
- [ ] Chaos test: random replica kills/restarts; verify stability
- [ ] Test with real FM replicas (if available)

**Deliverable:** Integration tests in CI/CD; load test results documented.

**Estimated effort:** 1 engineer × 2 days

#### 6.3 Deployment & Helm Chart (1.5 days)

**Tasks:**
- [ ] Update Dockerfile:
  - Multi-stage build (golang:1.22 → alpine)
  - Expose ports 8080, 5051, 9090
- [ ] Create Kubernetes manifests:
  - Deployment: fm-gw (1-3 replicas)
  - Service: LoadBalancer
  - ConfigMap: fm-gw-config.yaml
  - Probe: liveness + readiness
- [ ] Create Helm chart (optional; can skip for MVP):
  ```
  helm/fm-gw/
  ├── Chart.yaml
  ├── values.yaml
  ├── templates/
  │   ├── deployment.yaml
  │   ├── service.yaml
  │   └── configmap.yaml
  ```
- [ ] Test K8s deployment: `kubectl apply -f k8s/`
- [ ] Test docker-compose: `docker-compose up`

**Deliverable:** Production-ready Dockerfile, K8s manifests, helm chart.

**Estimated effort:** 1 engineer × 1.5 days

#### 6.4 Documentation & Runbook (1.5 days)

**Tasks:**
- [ ] Update README.md:
  - Build instructions
  - Configuration guide
  - Deployment (docker-compose, K8s, Helm)
- [ ] Create OPERATOR_RUNBOOK.md:
  - Startup/shutdown
  - Debugging (logs, metrics, traces)
  - Common issues and fixes
  - Scaling (add/remove replicas)
  - Monitoring (Prometheus dashboards, alerting rules)
- [ ] Create ARCHITECTURE.md (summary of HLD/LLD)
- [ ] Add code comments (non-obvious logic only)

**Deliverable:** Comprehensive documentation for operators and maintainers.

**Estimated effort:** 1 engineer × 1.5 days

#### 6.5 Performance Tuning & Security (1 day)

**Tasks:**
- [ ] Profiling: CPU + memory under load
  - Target: <10% CPU for 10k req/s
  - Target: <100MB memory
- [ ] Optimization: if needed
  - Connection pooling improvements
  - Replica connection reuse (gRPC)
  - Buffer pool (reduce allocations)
- [ ] Security review:
  - Input validation (request headers, body)
  - DoS mitigation (rate limiting, timeout)
  - TLS support (future; optional)
  - No secrets in logs

**Deliverable:** Performance benchmarks documented; security review passed.

**Estimated effort:** 1 engineer × 1 day

**Phase 6 Total:** 8 days (~1.5 weeks)

---

## Implementation Timeline

```
Week 1:   Phase 1 (Foundation)              [######]
Week 2:   Phase 2 (Routing)                 [######]
Week 3:   Phase 3 (gRPC) + Phase 4 start   [#####]
Week 4:   Phase 4 (QoS) + Phase 5 (Obs)    [#######]
Week 5-6: Phase 6 (Hardening)              [#########]

Total: 6-8 weeks, 1 engineer
```

---

## Tracker (Progress)

### Phase 1: Foundation

| Subphase | Task | Status | Owner | ETA | Notes |
|----------|------|--------|-------|-----|-------|
| 1.1 | Project setup | ⬜ pending | — | — | Create git repo, Go module, CI/CD |
| 1.1 | Makefile & CI | ⬜ pending | — | — | build, test, docker-build targets |
| 1.2 | Gateway struct | ⬜ pending | — | — | From LLD §2.1 |
| 1.2 | Replica struct | ⬜ pending | — | — | From LLD §2.2 |
| 1.2 | Config struct | ⬜ pending | — | — | YAML parsing, env override |
| 1.2 | Unit tests (config) | ⬜ pending | — | — | Test parsing, defaults |
| 1.3 | HTTP listener | ⬜ pending | — | — | Listen on :8080 |
| 1.3 | Request router | ⬜ pending | — | — | Basic endpoints (health, devices) |
| 1.3 | Handler skeleton | ⬜ pending | — | — | Return 501 for now |
| 1.3 | Graceful shutdown | ⬜ pending | — | — | SIGTERM handler |
| 1.3 | Unit tests (http) | ⬜ pending | — | — | Server startup, shutdown |

**Phase 1 Status: ⬜ Not Started**

### Phase 2: Request Routing

| Subphase | Task | Status | Owner | ETA | Notes |
|----------|------|--------|-------|-----|-------|
| 2.1 | T2EtcdDiscoverer | ⬜ pending | — | — | Read pod list from T2 etcd |
| 2.1 | DockerComposeDiscoverer | ⬜ pending | — | — | Parse env var |
| 2.1 | Replica refresh loop | ⬜ pending | — | — | Every 10s |
| 2.1 | Unit tests (discovery) | ⬜ pending | — | — | Parsing, error cases |
| 2.2 | ConsistentHashLB | ⬜ pending | — | — | FNV hash |
| 2.2 | RoundRobinLB | ⬜ pending | — | — | Fallback |
| 2.2 | Unit tests (LB) | ⬜ pending | — | — | Hash distribution |
| 2.3 | forwardRequest | ⬜ pending | — | — | HTTP forward |
| 2.3 | Timeout handling | ⬜ pending | — | — | 30s default |
| 2.3 | Error responses | ⬜ pending | — | — | 502, 503, 504 |
| 2.3 | Integration tests | ⬜ pending | — | — | Forward to real FM |

**Phase 2 Status: ⬜ Not Started**

### Phase 3: gRPC Streaming

| Subphase | Task | Status | Owner | ETA | Notes |
|----------|------|--------|-------|-----|-------|
| 3.1 | gRPC server setup | ⬜ pending | — | — | Listen on :5051 |
| 3.1 | Proto generation | ⬜ pending | — | — | protoc from cb_fm_protos |
| 3.1 | Service stubs | ⬜ pending | — | — | Subscribe, Publish |
| 3.2 | Subscribe handler | ⬜ pending | — | — | Proxy stream |
| 3.2 | Bidirectional proxy | ⬜ pending | — | — | Client ↔ Replica |
| 3.2 | Active conn tracking | ⬜ pending | — | — | For least-conn LB |
| 3.2 | Integration tests | ⬜ pending | — | — | Full flow |
| 3.3 | Publish handler | ⬜ pending | — | — | If needed |
| 3.3 | Integration tests | ⬜ pending | — | — | Publish flow |

**Phase 3 Status: ⬜ Not Started**

### Phase 4: Quality of Service

| Subphase | Task | Status | Owner | ETA | Notes |
|----------|------|--------|-------|-----|-------|
| 4.1 | Queue struct | ⬜ pending | — | — | Per-replica channel |
| 4.1 | Queue enqueue | ⬜ pending | — | — | In HTTP handler |
| 4.1 | Backpressure | ⬜ pending | — | — | 503 when full |
| 4.1 | Metrics | ⬜ pending | — | — | Queue depth, dropped |
| 4.2 | HealthChecker | ⬜ pending | — | — | Poll /health |
| 4.2 | Health loop | ⬜ pending | — | — | Every 10s |
| 4.2 | Replica status | ⬜ pending | — | — | HEALTHY/UNHEALTHY |
| 4.2 | Circuit breaker | ⬜ pending | — | — | Panic if >50% down |
| 4.2 | Integration tests | ⬜ pending | — | — | Health check success/fail |
| 4.3 | RateLimiter struct | ⬜ pending | — | — | Token bucket |
| 4.3 | Rate limit check | ⬜ pending | — | — | Per-IP, per-device, per-key |
| 4.3 | Unit tests | ⬜ pending | — | — | Token bucket logic |

**Phase 4 Status: ⬜ Not Started**

### Phase 5: Observability

| Subphase | Task | Status | Owner | ETA | Notes |
|----------|------|--------|-------|-----|-------|
| 5.1 | Prometheus metrics | ⬜ pending | — | — | requests, latency, queue depth |
| 5.1 | Metrics recording | ⬜ pending | — | — | After each request |
| 5.1 | /metrics endpoint | ⬜ pending | — | — | On :9090 |
| 5.2 | Trace context | ⬜ pending | — | — | W3C propagation |
| 5.2 | Span creation | ⬜ pending | — | — | Root + children |
| 5.2 | Trace export | ⬜ pending | — | — | To stdout or OTLP |
| 5.3 | JSON logging | ⬜ pending | — | — | Structured logs |
| 5.3 | Log statements | ⬜ pending | — | — | At key points |

**Phase 5 Status: ⬜ Not Started**

### Phase 6: Hardening

| Subphase | Task | Status | Owner | ETA | Notes |
|----------|------|--------|-------|-----|-------|
| 6.1 | Unit tests | ⬜ pending | — | — | >80% coverage |
| 6.1 | Mocking | ⬜ pending | — | — | Mock FM replicas |
| 6.2 | E2E tests | ⬜ pending | — | — | Full flow |
| 6.2 | Load test | ⬜ pending | — | — | 1000 req/s × 60s |
| 6.2 | Chaos test | ⬜ pending | — | — | Kill replicas randomly |
| 6.3 | Dockerfile | ⬜ pending | — | — | Multi-stage, alpine |
| 6.3 | K8s manifests | ⬜ pending | — | — | Deployment, Service |
| 6.3 | Helm chart | ⬜ pending | — | — | Optional |
| 6.4 | Documentation | ⬜ pending | — | — | README, RUNBOOK |
| 6.5 | Performance tune | ⬜ pending | — | — | <10% CPU for 10k req/s |
| 6.5 | Security review | ⬜ pending | — | — | Input validation, DoS, TLS |

**Phase 6 Status: ⬜ Not Started**

---

## Key Metrics & Success Criteria

**Functionality:**
- ✅ HTTP requests routed to FM replicas
- ✅ gRPC Subscribe streams proxied
- ✅ Health checks detect replica failures
- ✅ Rate limiting enforced
- ✅ Metrics and traces available

**Performance:**
- ✅ Gateway latency: <10ms p95 (overhead only)
- ✅ Throughput: 10k req/s
- ✅ Memory: <100MB
- ✅ CPU: <10% for 10k req/s

**Reliability:**
- ✅ No request loss (buffering + backpressure)
- ✅ Graceful failover (<10s recovery)
- ✅ Circuit breaker prevents cascade failures
- ✅ >80% test coverage

---

## References

- `fm-gw-architecture-hld.md` — Architecture overview
- `fm-gw-low-level-design.md` — Implementation details
- `fm-api-gateway-design.md` — Gateway requirements
