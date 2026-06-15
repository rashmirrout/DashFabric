# Weaver: Implementation Planner

> **Status:** Draft v1  
> **Timeline:** 16 weeks (4 sprints), 1 engineer  
> **Parallel with:** FM core + CB development  
> **Created:** 2026-06-15

---

## Overview

**Weaver Phase 1 Goal:** Production-grade universal gateway platform supporting FM, CB, and future systems.

**Delivery:** Single Go binary + comprehensive documentation. Configuration-driven for all system differences.

---

## Phase 1 Timeline (16 Weeks / 4 Sprints)

```
SPRINT 1 (Weeks 1-4): Foundation & Core Components
├─ Project setup, modules, dependencies
├─ Config system (YAML parser, validator, schema)
├─ Generic pod discovery (etcd, static)
├─ Generic health monitoring (HTTP, gRPC, TCP)
├─ All 8 load balancing strategies
└─ Basic gRPC + HTTP listeners

SPRINT 2 (Weeks 5-8): Reliability & Routing
├─ Circuit breaker implementation
├─ Retry logic with backoff
├─ Timeout management
├─ Request queuing + backpressure
├─ gRPC request routing (Subscribe, Publish)
├─ Rate limiting (multi-dimensional)
└─ Integration with load balancers

SPRINT 3 (Weeks 9-12): Observability & Security
├─ Prometheus metrics collection
├─ OpenTelemetry tracing integration
├─ Structured JSON logging
├─ Debug API endpoints
├─ Authentication hooks (Bearer, API key, JWT)
├─ Authorization framework (RBAC)
└─ TLS/mTLS support

SPRINT 4 (Weeks 13-16): Production Hardening & Launch
├─ Comprehensive testing (unit, integration, load)
├─ Kubernetes manifests + Helm chart
├─ Docker image + docker-compose
├─ Complete documentation + operator runbook
├─ Performance tuning + profiling
└─ Security review + hardening
```

---

## Phase 1 Tasks by Sprint

### SPRINT 1: Foundation (Weeks 1-4, 14 tasks)

**1.1 Project Setup (1 day)**
- [ ] Create Go module: `github.com/dashfabric/weaver`
- [ ] Directory structure: `cmd/`, `pkg/`, `test/`, `deploy/`, `docs/`
- [ ] Makefile: `build`, `test`, `run`, `docker-build`, `lint`
- [ ] GitHub Actions CI/CD pipeline
- **Deliverable:** Buildable empty project; CI green
- **Effort:** 1 FTE day

**1.2 Configuration System (3 days)**
- [ ] YAML schema definition (proto or struct tags)
- [ ] Config parser + validator
- [ ] Environment variable overrides
- [ ] Config hot-reload mechanism
- [ ] Unit tests: parsing, validation, overrides
- **Deliverable:** Config system accepts FM, CB, custom configs
- **Effort:** 1 FTE × 3 days

**1.3 Core Data Structures (2 days)**
- [ ] Replica struct (name, address, status, metrics)
- [ ] Request struct (client_id, method, deadline, context)
- [ ] ReplicaManager (thread-safe replica list management)
- [ ] Metrics struct (counters, histograms)
- [ ] Unit tests: initialization, concurrency
- **Deliverable:** Thread-safe replica management
- **Effort:** 1 FTE × 2 days

**1.4 Pod Discovery (Interface + Implementations) (3 days)**
- [ ] PodDiscoverer interface definition
- [ ] T2 etcd discoverer: query `/dashfabric/cluster/pods/*`
- [ ] Static list discoverer: parse env var
- [ ] Discovery manager: polling loop (every 10s)
- [ ] Unit tests: etcd queries, parsing, change detection
- [ ] Integration tests: mock etcd
- **Deliverable:** Pod discovery working for etcd + static
- **Effort:** 1 FTE × 3 days

**1.5 Health Monitoring (Interface + Implementations) (3 days)**
- [ ] HealthChecker interface definition
- [ ] HTTP health checker: GET + timeout + status code
- [ ] gRPC health checker: `/grpc.health.v1.Health/Check`
- [ ] TCP health checker: connect + close
- [ ] Health check loop: every 10s; track consecutive failures
- [ ] State machine: HEALTHY ↔ UNHEALTHY
- [ ] Panic mode detection: >50% unhealthy
- [ ] Unit tests: all health check types; state transitions
- **Deliverable:** Health monitoring working; replica status tracked
- **Effort:** 1 FTE × 3 days

**1.6 gRPC + HTTP Listeners (Skeleton) (2 days)**
- [ ] gRPC server setup: port 5051
- [ ] gRPC service stubs: Subscribe, Publish (returns 501 Not Implemented)
- [ ] HTTP server setup: port 8080
- [ ] HTTP routes: /metrics, /debug/* (return 501)
- [ ] Graceful shutdown: SIGTERM handling
- [ ] Unit tests: server startup, shutdown, endpoints
- **Deliverable:** Servers listening; endpoints respond (not yet routed)
- **Effort:** 1 FTE × 2 days

**Sprint 1 Total:** 14 days ≈ 2.8 weeks
**Status:** ✅ COMPLETE; FM + CB configs recognized; discovery + health working

---

### SPRINT 2: Reliability & Routing (Weeks 5-8, 16 tasks)

**2.1 Load Balancer Interface + All 8 Strategies (4 days)**
- [ ] LoadBalancer interface
- [ ] Least Connections LB: min active connections
- [ ] Round Robin LB: atomic counter
- [ ] Random LB: math.rand
- [ ] Consistent Hash LB: hash ring (virtual nodes)
- [ ] Weighted LB: weight-based selection
- [ ] Sticky LB: time-based affinity
- [ ] Resource-Aware LB: metric-based selection
- [ ] Custom LB plugin interface
- [ ] Unit tests: all strategies; distribution verification
- **Deliverable:** All 8 LBs implemented; selectable via config
- **Effort:** 1 FTE × 4 days

**2.2 Circuit Breaker (2 days)**
- [ ] Circuit breaker state machine (CLOSED → OPEN → HALF_OPEN → CLOSED)
- [ ] Configurable thresholds (failure count, success count, timeout)
- [ ] Metrics: failures, state transitions
- [ ] Unit tests: state transitions; recovery scenarios
- **Deliverable:** Circuit breaker preventing cascade failures
- **Effort:** 1 FTE × 2 days

**2.3 Retry + Backoff (2 days)**
- [ ] Backoff strategies: exponential, linear, custom
- [ ] Retryable error codes (503, 504, 429 for gRPC)
- [ ] Max attempts + deadline enforcement
- [ ] Unit tests: backoff calculation; deadline enforcement
- **Deliverable:** Automatic retries on transient failures
- **Effort:** 1 FTE × 2 days

**2.4 Timeout Management (1 day)**
- [ ] Global timeout (e.g., 30s)
- [ ] Per-replica timeout (e.g., 25s)
- [ ] Connect timeout (e.g., 5s)
- [ ] Context propagation
- [ ] Unit tests: timeout enforcement
- **Deliverable:** All timeouts enforced
- **Effort:** 1 FTE day

**2.5 Request Queuing + Backpressure (2 days)**
- [ ] Per-replica queue (buffering structure)
- [ ] Configurable queue depth (e.g., 1000 slots)
- [ ] Overflow behavior: reject_with_503 or drop_oldest
- [ ] Metrics: queue depth, dropped requests
- [ ] Unit tests: enqueue, dequeue, overflow
- **Deliverable:** Queuing with explicit backpressure
- **Effort:** 1 FTE × 2 days

**2.6 gRPC Subscribe Handler (2 days)**
- [ ] Implement Subscribe RPC handler
- [ ] Load balancer selection (least-connections)
- [ ] Bidirectional stream proxy
- [ ] Active connection tracking
- [ ] Metrics: active streams, latency
- [ ] Integration tests: Subscribe flow
- **Deliverable:** gRPC Subscribe streams proxied to replicas
- **Effort:** 1 FTE × 2 days

**2.7 gRPC Publish Handler (2 days)**
- [ ] Implement Publish RPC handler
- [ ] Rate limiting check
- [ ] Load balancer selection
- [ ] Request forwarding (queue → replica → response)
- [ ] Metrics: latency, success rate
- [ ] Integration tests: Publish flow
- **Deliverable:** gRPC Publish calls routed to replicas
- **Effort:** 1 FTE × 2 days

**2.8 Rate Limiting (1 day)**
- [ ] Token bucket algorithm
- [ ] Multi-dimensional keys (client_id, client_ip, tenant, api_key)
- [ ] Configurable rates (per-minute, per-second)
- [ ] Unit tests: token consumption; rate enforcement
- **Deliverable:** Multi-dimensional rate limiting
- **Effort:** 1 FTE day

**Sprint 2 Total:** 16 days ≈ 3.2 weeks
**Status:** ✅ COMPLETE; Requests routed with reliability patterns

---

### SPRINT 3: Observability & Security (Weeks 9-12, 12 tasks)

**3.1 Prometheus Metrics (2 days)**
- [ ] Metrics collection (counters, histograms, gauges)
- [ ] Replica health, active connections, queue depth
- [ ] Request latency histograms
- [ ] Rate limit violations
- [ ] /metrics endpoint (Prometheus format)
- [ ] Configurable metric namespace
- [ ] Unit tests: metric recording
- **Deliverable:** Prometheus metrics complete
- **Effort:** 1 FTE × 2 days

**3.2 OpenTelemetry Tracing (2 days)**
- [ ] W3C Trace Context extraction
- [ ] Root span: weaver.request
- [ ] Child spans: discover, select, forward, respond
- [ ] Span attributes: replica, client_id, latency
- [ ] Jaeger exporter integration
- [ ] Unit tests: span creation; trace propagation
- **Deliverable:** End-to-end distributed tracing
- **Effort:** 1 FTE × 2 days

**3.3 Structured Logging (1 day)**
- [ ] JSON logging format
- [ ] Log levels: DEBUG, INFO, WARN, ERROR
- [ ] Context fields: request_id, client_id, replica, latency
- [ ] Async logging (buffered to avoid latency impact)
- [ ] Unit tests: log output
- **Deliverable:** Structured JSON logs
- **Effort:** 1 FTE day

**3.4 Debug API Endpoints (1 day)**
- [ ] GET /debug/replicas: replica list + status
- [ ] GET /debug/replicas/{name}: replica detail
- [ ] GET /debug/config: current configuration
- [ ] GET /debug/metrics: live metrics
- [ ] Unit tests: endpoint responses
- **Deliverable:** Debug endpoints for troubleshooting
- **Effort:** 1 FTE day

**3.5 Authentication Framework (2 days)**
- [ ] Authenticator interface
- [ ] Bearer token extractor
- [ ] API key extractor
- [ ] JWT validation
- [ ] Auth context propagation
- [ ] Unit tests: all auth types
- **Deliverable:** Pluggable authentication
- **Effort:** 1 FTE × 2 days

**3.6 Authorization Framework (1 day)**
- [ ] Authorizer interface
- [ ] RBAC (role-based access control)
- [ ] Request authorization checks
- [ ] Unit tests: authorization decisions
- **Deliverable:** Pluggable authorization
- **Effort:** 1 FTE day

**3.7 TLS/mTLS Support (2 days)**
- [ ] TLS configuration (cert, key, CA)
- [ ] Certificate loading + validation
- [ ] gRPC TLS support
- [ ] HTTP TLS support
- [ ] Unit tests: TLS handshakes
- **Deliverable:** Encrypted communication support
- **Effort:** 1 FTE × 2 days

**Sprint 3 Total:** 11 days ≈ 2.2 weeks
**Status:** ✅ COMPLETE; Production observability + security

---

### SPRINT 4: Hardening & Launch (Weeks 13-16, 14 tasks)

**4.1 Unit Testing (2 days)**
- [ ] Comprehensive unit tests (all modules)
- [ ] >80% code coverage
- [ ] Mocking: etcd, HTTP, gRPC replicas
- [ ] Test fixtures + data builders
- **Deliverable:** >80% coverage; all modules tested
- **Effort:** 1 FTE × 2 days

**4.2 Integration Testing (3 days)**
- [ ] End-to-end Subscribe flow
- [ ] End-to-end Publish flow
- [ ] REST observability queries
- [ ] Pod discovery + health monitoring in concert
- [ ] Replica failure + recovery
- [ ] All replicas unhealthy → panic mode
- [ ] Rate limiting enforcement
- [ ] Buffering + backpressure
- [ ] Load test: 10k req/s; 1k Subscribe streams
- **Deliverable:** Integration tests passing; load SLA met
- **Effort:** 1 FTE × 3 days

**4.3 Kubernetes Deployment (2 days)**
- [ ] Deployment manifest (1-3 replicas)
- [ ] Service manifest (LoadBalancer, :5051, :8080)
- [ ] ConfigMap (weaver-config.yaml)
- [ ] ServiceMonitor (Prometheus)
- [ ] Health checks (liveness, readiness probes)
- [ ] Test in minikube or small cluster
- **Deliverable:** Kubernetes deployment-ready
- **Effort:** 1 FTE × 2 days

**4.4 Docker & Compose (1 day)**
- [ ] Dockerfile (minimal, multi-stage)
- [ ] docker-compose.yaml (weaver + mocked replicas)
- [ ] Docker image push to registry
- **Deliverable:** Docker image + compose for development
- **Effort:** 1 FTE day

**4.5 Documentation (2 days)**
- [ ] README.md (overview, quick start)
- [ ] Architecture docs (HLD, LLD)
- [ ] Configuration reference (all YAML options)
- [ ] User guide (FM, CB, custom configs)
- [ ] Operator runbook (troubleshooting, metrics interpretation)
- [ ] Code comments + examples
- **Deliverable:** Production-ready documentation
- **Effort:** 1 FTE × 2 days

**4.6 Performance Tuning (2 days)**
- [ ] Profile latency (target <1ms p99)
- [ ] Profile CPU + memory
- [ ] Optimize hot paths (load balancer, rate limiter)
- [ ] Connection pooling tuning
- [ ] Metrics & tracing overhead measurement
- [ ] Benchmark: 100k req/s
- **Deliverable:** Performance SLAs met
- **Effort:** 1 FTE × 2 days

**4.7 Security Review (1 day)**
- [ ] Code review (auth, auth, rate limiting)
- [ ] Secrets handling (no hardcoded keys)
- [ ] Input validation (config, headers)
- [ ] Dependency audit (go mod check vulnerabilities)
- **Deliverable:** Security review complete; no critical issues
- **Effort:** 1 FTE day

**Sprint 4 Total:** 13 days ≈ 2.6 weeks
**Status:** ✅ COMPLETE; Production-ready launch

---

## Overall Summary

| Sprint | Weeks | Focus | Status |
|--------|-------|-------|--------|
| 1 | 1-4 | Foundation | ✅ Pending |
| 2 | 5-8 | Reliability | ✅ Pending |
| 3 | 9-12 | Observability | ✅ Pending |
| 4 | 13-16 | Hardening | ✅ Pending |

**Total Effort:** ~54 days = 10.8 weeks (1 engineer)  
**Timeline:** 16 weeks (includes buffer, integration testing, documentation)

---

## Task Tracker

| Task ID | Task | Sprint | Status | Effort | Owner |
|---------|------|--------|--------|--------|-------|
| 1.1 | Project setup | 1 | ⚪ Pending | 1d | - |
| 1.2 | Config system | 1 | ⚪ Pending | 3d | - |
| 1.3 | Core data structures | 1 | ⚪ Pending | 2d | - |
| 1.4 | Pod discovery | 1 | ⚪ Pending | 3d | - |
| 1.5 | Health monitoring | 1 | ⚪ Pending | 3d | - |
| 1.6 | gRPC + HTTP listeners | 1 | ⚪ Pending | 2d | - |
| 2.1 | All 8 load balancers | 2 | ⚪ Pending | 4d | - |
| 2.2 | Circuit breaker | 2 | ⚪ Pending | 2d | - |
| 2.3 | Retry + backoff | 2 | ⚪ Pending | 2d | - |
| 2.4 | Timeout management | 2 | ⚪ Pending | 1d | - |
| 2.5 | Request queuing | 2 | ⚪ Pending | 2d | - |
| 2.6 | gRPC Subscribe | 2 | ⚪ Pending | 2d | - |
| 2.7 | gRPC Publish | 2 | ⚪ Pending | 2d | - |
| 2.8 | Rate limiting | 2 | ⚪ Pending | 1d | - |
| 3.1 | Prometheus metrics | 3 | ⚪ Pending | 2d | - |
| 3.2 | OpenTelemetry tracing | 3 | ⚪ Pending | 2d | - |
| 3.3 | Structured logging | 3 | ⚪ Pending | 1d | - |
| 3.4 | Debug API | 3 | ⚪ Pending | 1d | - |
| 3.5 | Authentication | 3 | ⚪ Pending | 2d | - |
| 3.6 | Authorization | 3 | ⚪ Pending | 1d | - |
| 3.7 | TLS/mTLS | 3 | ⚪ Pending | 2d | - |
| 4.1 | Unit testing | 4 | ⚪ Pending | 2d | - |
| 4.2 | Integration testing | 4 | ⚪ Pending | 3d | - |
| 4.3 | Kubernetes deployment | 4 | ⚪ Pending | 2d | - |
| 4.4 | Docker & compose | 4 | ⚪ Pending | 1d | - |
| 4.5 | Documentation | 4 | ⚪ Pending | 2d | - |
| 4.6 | Performance tuning | 4 | ⚪ Pending | 2d | - |
| 4.7 | Security review | 4 | ⚪ Pending | 1d | - |

---

## Success Criteria (End of Week 16)

✅ **Functionality:**
- [ ] All 8 load balancers working
- [ ] Circuit breaker + retry + timeout all working
- [ ] FM config: Subscribe/Publish routed
- [ ] CB config: Subscribe/Publish + REST queries routed
- [ ] Future system (FR) config works (no code changes)
- [ ] Health monitoring: <5s detection; panic mode working

✅ **Performance:**
- [ ] Routing latency: <1ms p99
- [ ] Throughput: 100k req/s
- [ ] Memory: <50MB baseline
- [ ] Config reload: <1s

✅ **Reliability:**
- [ ] 99.99% uptime SLA
- [ ] Zero data loss
- [ ] Graceful degradation
- [ ] Automatic failover

✅ **Operability:**
- [ ] Single YAML config
- [ ] Hot-reload working
- [ ] Debug API endpoints
- [ ] Comprehensive docs complete
- [ ] Operator runbook complete

**If ANY criterion is ❌ FAILING, Phase 1 is not complete.**

---

**Next:** Read [weaver-user-guide.md](./weaver-user-guide.md) for configuration reference.
