# Weaver: Architecture & Requirements Specification

> **Status:** Draft v1  
> **Module:** Weaver Gateway Platform  
> **Scope:** Features, requirements, non-functional requirements, use cases  
> **Audience:** Architects, Engineers, Product Managers

---

## Table of Contents

1. [Overview](#1-overview)
2. [Vision & Problem Statement](#2-vision--problem-statement)
3. [Core Features](#3-core-features)
4. [Non-Functional Requirements](#4-non-functional-requirements)
5. [Use Cases & Scenarios](#5-use-cases--scenarios)
6. [Design Principles](#6-design-principles)
7. [Extensibility Model](#7-extensibility-model)
8. [Success Criteria](#8-success-criteria)

---

## 1. Overview

**Weaver** is a universal gateway platform that routes requests across distributed backend systems with intelligence, reliability, and extensibility.

### What Makes Weaver Different?

| Aspect | Typical Gateway | Weaver |
|--------|-----------------|--------|
| **Codebase** | One per system | Single, reusable |
| **Configuration** | Code changes | YAML only |
| **Systems** | FM-Gateway, CB-Gateway, FR-Gateway | All: same binary |
| **Extensibility** | Hard-coded | Plugin architecture |
| **Operators** | Debug per system | Single operational model |
| **Future Systems** | Rewrite gateway | Plug and play |

### The "Weaver" Metaphor

> *Weaves multiple backend systems together seamlessly; connects different topologies with a single unified interface.*

```
┌─────────────────────────────────────────────────┐
│  REQUEST STREAM (Clients, Apps, Services)      │
│                                                 │
│  Registrations  Queries  Subscriptions  Metrics│
└─────────────┬───────────────────────────────────┘
              │
              ▼
    ┌─────────────────────┐
    │  WEAVER GATEWAY     │
    │                     │
    │  Discover replicas  │
    │  Check health       │
    │  Route smartly      │
    │  Handle failures    │
    │  Observe deeply     │
    └─────────┬───────────┘
              │
    ┌─────────┴─────────────┬──────────────┬──────────────┐
    │                       │              │              │
    ▼                       ▼              ▼              ▼
  ┌────────────┐      ┌──────────┐  ┌──────────┐  ┌────────────┐
  │ FM Cluster │      │ CB Cluster   │ FR System    │ Future     │
  │ (Primary   │      │ (Peer        │ (Custom      │ Systems    │
  │  +Replicas)│      │  equivalent) │  topology)   │            │
  └────────────┘      └──────────┘  └──────────┘  └────────────┘
```

---

## 2. Vision & Problem Statement

### Problem: Gateway Code Duplication

Today, each system needs its own gateway:

```
FM-Gateway (1200 LOC):
├─ Pod discovery (etcd)
├─ Health monitoring
├─ Load balancing
├─ Request routing (primary-aware)
├─ Buffering
├─ Rate limiting
├─ Metrics/tracing
└─ Observability

CB-Gateway (1500 LOC):
├─ Pod discovery (etcd) ← DUPLICATE
├─ Health monitoring ← DUPLICATE
├─ Load balancing ← DUPLICATE
├─ Request routing (peer-equivalent)
├─ Buffering ← DUPLICATE
├─ Rate limiting ← DUPLICATE
├─ Metrics/tracing ← DUPLICATE
└─ REST observability

FR-Gateway (1800 LOC):
├─ Pod discovery (Consul) ← DIFFERENT
├─ Health monitoring (gRPC) ← DIFFERENT
├─ Load balancing ← DUPLICATE
├─ Request routing (distributed) ← DIFFERENT
├─ Buffering ← DUPLICATE
├─ Rate limiting ← DUPLICATE
├─ Metrics/tracing ← DUPLICATE
└─ Custom observability
```

**Result:** 60% code duplication; maintenance burden; bug fixes propagated 3× over.

### Vision: Single Unified Gateway

```
Weaver Gateway:
├─ Pod discovery (etcd, Consul, K8s, DNS, static) ← PLUGGABLE
├─ Health monitoring (HTTP, gRPC, TCP, custom) ← PLUGGABLE
├─ Load balancing (LC, RR, hash, weighted, etc.) ← PLUGGABLE
├─ Request routing (rules engine + strategy) ← CONFIG-DRIVEN
├─ Buffering ← BUILT-IN
├─ Rate limiting (multi-dimensional) ← PLUGGABLE
├─ Metrics/tracing (Prometheus, OpenTelemetry, etc.) ← PLUGGABLE
└─ Observability (debug API, logs, traces) ← BUILT-IN

Deploy:  ./weaver --config fm-config.yaml
         ./weaver --config cb-config.yaml
         ./weaver --config fr-config.yaml  (NEW: No code changes!)
```

**Result:** 1 codebase; ~51% reduction; future systems = zero effort.

---

## 3. Core Features

### 3.1 Universal Request Routing

**What it does:**
- Accept gRPC, HTTP, or REST requests from clients
- Discover backend replicas (from etcd, Consul, K8s API, DNS, static list)
- Check replica health (HTTP, gRPC, TCP probes)
- Route requests to healthy replicas using configurable load-balancing strategies
- Forward requests to selected replica
- Return response to client

**Supported request types:**
- Unary RPC (request-response)
- Streaming RPC (bidirectional, client-streaming, server-streaming)
- HTTP/REST requests
- Custom protocol handlers (pluggable)

**Configuration example:**
```yaml
routing:
  strategy: "least_connections"  # How to pick replica
  timeout: 30s                    # Request timeout
  retry:
    max_attempts: 3
    backoff: "exponential"
```

---

### 3.2 Eight Load Balancing Strategies

All implemented, all selectable via config:

#### 1. **Least Connections (LC)**
- Count active connections per replica
- Route to replica with fewest connections
- **Best for:** Long-lived streams (FM Subscribe, CB Subscribe)
- **Example:**
  ```yaml
  load_balancers:
    - name: "stream_lb"
      type: "least_connections"
  ```

#### 2. **Round Robin (RR)**
- Rotate through replicas in order
- **Best for:** Stateless, uniform-load requests (REST queries, Publish calls)
- **Example:**
  ```yaml
  load_balancers:
    - name: "stateless_lb"
      type: "round_robin"
  ```

#### 3. **Random**
- Pick a random healthy replica
- **Best for:** Simple load spreading; no affinity needed
- **Example:**
  ```yaml
  load_balancers:
    - name: "random_lb"
      type: "random"
  ```

#### 4. **Consistent Hash**
- Hash-based affinity (e.g., hash client ID)
- Same client always routes to same replica
- **Best for:** Stateful requests; session affinity
- **Example:**
  ```yaml
  load_balancers:
    - name: "session_lb"
      type: "consistent_hash"
      key: "client_id"  # or "header:X-Session-ID"
      virtual_nodes: 150
  ```

#### 5. **Weighted**
- Route proportional to replica weight (capacity, performance)
- Replica weight from labels or config
- **Best for:** Heterogeneous replica clusters
- **Example:**
  ```yaml
  load_balancers:
    - name: "weighted_lb"
      type: "weighted"
      weight_source: "replica_label:capacity"  # "high", "medium", "low"
  ```

#### 6. **Sticky**
- Route same client always within TTL
- Different from consistent hash (time-based, not hash-based)
- **Best for:** Maintaining request affinity temporarily
- **Example:**
  ```yaml
  load_balancers:
    - name: "sticky_lb"
      type: "sticky"
      key: "client_ip"
      ttl: 300s
  ```

#### 7. **Resource-Aware**
- Route by replica resource availability (CPU, memory, custom metrics)
- Requires replica metrics (from Prometheus, custom exporter)
- **Best for:** Dynamic load based on replica resources
- **Example:**
  ```yaml
  load_balancers:
    - name: "resource_lb"
      type: "resource_aware"
      metric: "available_cpu_percent"
  ```

#### 8. **Custom**
- User-defined load balancing algorithm (via plugin)
- Implement `LoadBalancer` interface; register plugin
- **Best for:** Domain-specific logic (e.g., geolocation-based, ML-based)
- **Example:**
  ```yaml
  load_balancers:
    - name: "ml_lb"
      type: "custom"
      plugin_path: "/plugins/ml_loadbalancer.so"
      config:
        model_path: "/models/routing_model.pkl"
  ```

---

### 3.3 Production-Grade Reliability

#### Circuit Breaker
**Pattern:** Fail-fast on replica failures; auto-recover when healthy.

```yaml
reliability:
  circuit_breaker:
    enabled: true
    failure_threshold: 5      # Mark unhealthy after 5 failures
    success_threshold: 2      # Recover after 2 successes
    timeout: 30s              # Half-open state timeout
```

**States:**
```
CLOSED (healthy) --[5 failures]--> OPEN (failing)
                                       │
                                    [30s timeout]
                                       │
                                       ▼
                                  HALF-OPEN (testing)
                                       │
                       ┌───────────────┴───────────────┐
                       │                               │
                   [2 successes]                  [1 failure]
                       │                               │
                       ▼                               ▼
                    CLOSED                           OPEN
```

#### Retry with Backoff
**Pattern:** Automatically retry transient failures with exponential backoff.

```yaml
reliability:
  retry:
    enabled: true
    max_attempts: 3
    backoff:
      type: "exponential"  # or "linear", "custom"
      initial_delay: 100ms
      max_delay: 5s
      multiplier: 2.0
    retryable_codes: [503, 504, 429]  # Which gRPC codes to retry
```

**Timeline:**
```
Request attempt 1: FAIL (503)
  → Wait 100ms (exponential backoff)
Request attempt 2: FAIL (503)
  → Wait 200ms (exponential backoff: 100 * 2)
Request attempt 3: SUCCESS
```

#### Timeout Management
**Pattern:** Ensure requests don't hang indefinitely.

```yaml
reliability:
  timeout:
    global: 30s              # Max time for any request
    per_replica: 25s         # Per-replica deadline
    connect_timeout: 5s      # Connection establishment timeout
```

#### Bulkhead Isolation
**Pattern:** Limit resource usage per replica (connection pooling).

```yaml
reliability:
  bulkhead:
    enabled: true
    max_connections_per_replica: 100
    max_pending_requests_per_replica: 1000
```

**Benefit:** One replica failure doesn't exhaust all connections.

---

### 3.4 Request Buffering & Backpressure

**Pattern:** Queue requests; return 503 (overload) if queue full.

```yaml
buffering:
  enabled: true
  depth: 5000                    # Max requests in queue
  overflow_behavior: "reject_with_503"  # or "drop_oldest"
```

**Timeline:**
```
Request stream: [Req1, Req2, Req3, Req4, ...]
                   │
                   ▼
            ┌─────────────┐
            │ Queue (5000)│
            └─────────────┘
                   │
         ┌─────────┴─────────┐
         ▼                   ▼
    QUEUE EMPTY          QUEUE FULL
    Forward to replica   Return 503 Overload
```

---

### 3.5 Multi-Dimensional Rate Limiting

**Pattern:** Limit requests per client, per IP, per tenant, etc.

```yaml
rate_limiting:
  enabled: true
  policies:
    # Policy 1: Per client ID
    - name: "per_client"
      type: "token_bucket"
      rate: 1000/min
      key: "client_id"
      
    # Policy 2: Per IP address
    - name: "per_ip"
      type: "token_bucket"
      rate: 5000/min
      key: "client_ip"
      
    # Policy 3: Per tenant (from header)
    - name: "per_tenant"
      type: "sliding_window"
      rate: 10000/min
      key: "header:X-Tenant-ID"
      
    # Policy 4: Per API key (from request)
    - name: "per_api_key"
      type: "token_bucket"
      rate: "header:X-Rate-Limit"  # Dynamic from header
      key: "api_key"
```

**Algorithm:** Token bucket (configurable fill rate).

---

### 3.6 Comprehensive Observability

#### Metrics (Prometheus)
```
# Replica health
weaver_replica_health{replica="fm-0"} = 1      # healthy
weaver_replica_health{replica="fm-1"} = 0      # unhealthy

# Request volume
weaver_requests_total{method="Subscribe", status="OK"} = 50000
weaver_requests_total{method="Publish", status="ERROR"} = 123

# Latency (histogram)
weaver_request_latency_ms{method="Subscribe", replica="fm-0", le="1"} = 1000
weaver_request_latency_ms{method="Subscribe", replica="fm-0", le="10"} = 45000

# Queue depth
weaver_queue_depth{replica="fm-0"} = 234

# Active connections
weaver_active_connections{replica="fm-0"} = 42

# Rate limit violations
weaver_rate_limit_violations_total{policy="per_client"} = 5
```

#### Distributed Tracing (OpenTelemetry)
```
ROOT SPAN: "weaver.request"
├─ Attribute: request_type=Subscribe
├─ Attribute: client_id=fm-adapter-1
│
├─ CHILD SPAN: "discover_replica"
│  └─ Attribute: discovered_count=3
│
├─ CHILD SPAN: "select_replica"
│  ├─ Attribute: strategy=least_connections
│  └─ Attribute: selected_replica=fm-0
│
├─ CHILD SPAN: "forward_request"
│  ├─ Attribute: latency_ms=2.5
│  └─ Attribute: status=OK
│
└─ CHILD SPAN: "return_response"
   └─ Attribute: bytes_sent=512
```

#### Structured Logging (JSON)
```json
{
  "timestamp": "2026-06-15T10:30:45.123Z",
  "level": "INFO",
  "component": "weaver.gateway",
  "event": "request_forwarded",
  "request_id": "req-12345",
  "client_id": "fm-adapter-1",
  "replica": "fm-0",
  "method": "Subscribe",
  "latency_ms": 2.5,
  "status": "OK"
}
```

---

### 3.7 Security Framework

#### Authentication
```yaml
security:
  authentication:
    enabled: true
    type: "bearer_token"  # "api_key", "oauth", "jwt", "mTLS", "custom"
    config:
      token_source: "header:Authorization"
```

#### Authorization
```yaml
security:
  authorization:
    enabled: true
    type: "rbac"  # "abac", "custom"
    config:
      admin_roles: ["admin", "operator"]
```

#### TLS/mTLS
```yaml
security:
  tls:
    enabled: true
    mode: "mutual"  # "server_only" or "mutual"
    cert_path: "/etc/certs/server.crt"
    key_path: "/etc/certs/server.key"
    ca_path: "/etc/certs/ca.crt"
```

---

### 3.8 Configuration-Driven Everything

**Example: FM vs CB with same binary**

**FM Config (fm-weaver.yaml):**
```yaml
gateway:
  name: "fm-gateway"
discovery:
  type: "t2_etcd"
  key_pattern: "/dashfabric/cluster/pods/fm-*"
listeners:
  - name: "grpc"
    type: "grpc"
    port: 5051
load_balancers:
  - name: "default"
    type: "least_connections"
```

**CB Config (cb-weaver.yaml):**
```yaml
gateway:
  name: "cb-gateway"
discovery:
  type: "t2_etcd"
  key_pattern: "/dashfabric/cluster/pods/cb-*"
listeners:
  - name: "grpc"
    type: "grpc"
    port: 5052
  - name: "rest"
    type: "http"
    port: 8081
load_balancers:
  - name: "default"
    type: "least_connections"
```

**Deploy:**
```bash
./weaver --config fm-weaver.yaml  # FM Gateway
./weaver --config cb-weaver.yaml  # CB Gateway
```

---

## 4. Non-Functional Requirements

### 4.1 Performance

| Metric | Target | Rationale |
|--------|--------|-----------|
| Routing latency (p99) | <1ms | Load balancer selection only; no processing |
| Health check latency | <100ms/replica | Every 10s; doesn't block requests |
| Request throughput | 100k req/s | Single gateway instance |
| Memory baseline | <50MB | Plus buffers as needed |
| Config reload | <1s | Zero-restart updates |
| Failover detection | <5s | 3 × 10s health checks |

### 4.2 Reliability

| Requirement | Target | Implementation |
|-------------|--------|-----------------|
| Availability | 99.99% uptime | Circuit breaker, retry, timeout |
| Data loss | Zero | Queue-based buffering; backpressure |
| Request loss | Zero | Durable queue; explicit rejection |
| Recovery time | <30s | Circuit breaker half-open timeout |
| Graceful shutdown | Yes | Drain in-flight requests; close connections |

### 4.3 Scalability

| Dimension | Target | Notes |
|-----------|--------|-------|
| Replicas per backend | 100+ | Configurable discovery |
| Concurrent requests | 100k+ | Connection pooling limits |
| Requests per second | 100k+ | Single instance |
| Gateways per system | 3-5 | Load-balanced service |
| Config size | <10MB | YAML parsing |

### 4.4 Operability

| Aspect | Requirement |
|--------|-------------|
| **Configuration** | Single YAML file; environment variable overrides |
| **Deployment** | Kubernetes, docker-compose, standalone binary |
| **Monitoring** | Prometheus metrics; OpenTelemetry traces |
| **Debugging** | Debug API; detailed JSON logs; request tracing |
| **Documentation** | Complete user guide; operator runbook; examples |
| **Troubleshooting** | Common issues + solutions; health checks; metrics |

### 4.5 Security

| Requirement | Implementation |
|-------------|-----------------|
| **Encryption in transit** | TLS/mTLS (configurable) |
| **Authentication** | Pluggable (Bearer, API key, OAuth, JWT, mTLS, custom) |
| **Authorization** | Pluggable (RBAC, ABAC, custom) |
| **Rate limiting** | Multi-dimensional (client, IP, tenant, API-key) |
| **Audit logging** | Structured JSON logs with auth events |

---

## 5. Use Cases & Scenarios

### 5.1 Use Case 1: FM System (Primary-Aware Routing)

**Scenario:**
- FM system has 1 PRIMARY replica + N READ replicas
- PRIMARY: handles writes (Registrations), reads (Queries), heartbeats
- READ: handles Queries only
- Weaver routes based on request type

**Configuration:**
```yaml
gateway:
  name: "fm-gateway"
  
discovery:
  type: "t2_etcd"
  key_pattern: "/dashfabric/cluster/pods/fm-*"
  
health:
  type: "http"
  endpoint: "/api/v1/health"
  interval: 10s
  
listeners:
  - name: "grpc"
    type: "grpc"
    port: 5051
    
routing:
  strategy: "least_connections"
  
load_balancers:
  - name: "default"
    type: "least_connections"
    
observability:
  metrics:
    namespace: "fm_gw"
```

**Flow:**
```
FM Adapter → Weaver :5051
  │
  ├─ Registration request → Weaver selects PRIMARY → fm-0 (PRIMARY)
  │
  ├─ Query request → Weaver load-balances → fm-0, fm-1, or fm-2
  │
  └─ Subscribe stream → Weaver least-conn → fm-0 (fewest active streams)
```

---

### 5.2 Use Case 2: CB System (Peer-Equivalent Routing)

**Scenario:**
- CB system has N peer-equivalent replicas (all replicas are equal)
- All replicas handle Subscribe, Publish, and topic queries
- Weaver load-balances across all replicas

**Configuration:**
```yaml
gateway:
  name: "cb-gateway"
  
discovery:
  type: "t2_etcd"
  key_pattern: "/dashfabric/cluster/pods/cb-*"
  
health:
  type: "http"
  endpoint: "/api/v1/health"
  interval: 10s
  
listeners:
  - name: "grpc"
    type: "grpc"
    port: 5052
  - name: "rest_api"
    type: "http"
    port: 8081
    routes:
      - path: "/api/v1/*"
        handler: "rest_proxy"
    
routing:
  strategy: "least_connections"
  timeout: 30s
  
load_balancers:
  - name: "subscribe"
    type: "least_connections"
  - name: "publish"
    type: "round_robin"
  - name: "rest"
    type: "round_robin"
    
observability:
  metrics:
    namespace: "cb_gw"
  rest_api:
    enabled: true
    port: 8081
```

**Flow:**
```
FM Adapter → Weaver :5052 (gRPC)
  │
  ├─ Subscribe → Least-connections → cb-0 (fewest active streams)
  ├─ Publish → Round-robin → cb-1
  └─ Subscribe → Least-connections → cb-2

Dashboard → Weaver :8081 (REST)
  │
  ├─ GET /api/v1/topics → Round-robin → cb-0
  ├─ GET /api/v1/replicas → Direct response (gateway cache)
  └─ GET /api/v1/metrics → Prometheus format
```

---

### 5.3 Use Case 3: Future System (Extensible)

**Scenario:**
- New FR (Fabric Router) system with custom topology
- Uses Consul for service discovery (not etcd)
- gRPC health checks (not HTTP)
- Custom load balancing logic (geographic affinity)

**Configuration:**
```yaml
gateway:
  name: "fr-gateway"
  
discovery:
  type: "consul"  # Different discoverer
  config:
    endpoint: "http://consul:8500"
    service_name: "fr"
    
health:
  type: "grpc"  # Different health checker
  config:
    endpoint: "/health.HealthService/Check"
    
listeners:
  - name: "grpc"
    type: "grpc"
    port: 5053
    
routing:
  strategy: "custom"  # Custom plugin
  
load_balancers:
  - name: "geographic"
    type: "custom"
    plugin_path: "/plugins/geographic_lb.so"
    config:
      region_weights:
        us-east: 0.5
        us-west: 0.3
        eu-west: 0.2
    
observability:
  metrics:
    namespace: "fr_gw"
```

**Key:** Same Weaver binary; different config; new discoverer + health checker + load balancer = all pluggable.

---

### 5.4 Use Case 4: Testing & Chaos Engineering

**Scenario:**
- Test gateway behavior under replica failures
- Inject faults for chaos engineering
- Mirror traffic to canary replica

**Configuration:**
```yaml
gateway:
  name: "test-gateway"
  
testing:
  enabled: true
  fault_injection:
    error_rate: 0.05  # 5% of requests fail
    delay_ms: 100     # 100ms latency added
  
  traffic_mirroring:
    enabled: true
    target_replica: "cb-canary"
    mirror_percentage: 10  # 10% of traffic mirrored
```

**Flow:**
```
100 requests → Weaver
├─ 90 requests → cb-0 (production)
│   ├─ 85 succeed (5% fail injected)
│   └─ Latency: 100ms added
│
└─ 10 requests → cb-canary (mirrored)
    └─ Responses discarded (for testing only)
```

---

## 6. Design Principles

1. **Configuration First**
   - All system differences expressed in YAML
   - No code branching for FM vs CB
   - Operators define behavior, not engineers

2. **Pluggable Everything**
   - Discovery backends pluggable
   - Health check types pluggable
   - Load balancing strategies pluggable
   - Protocol handlers pluggable
   - Auth/authz pluggable

3. **Production-Ready by Default**
   - Circuit breakers, retries, timeouts built-in
   - Observability (metrics, traces, logs) built-in
   - Security (TLS, auth, rate-limiting) built-in

4. **Operator-Friendly**
   - Single config file
   - Hot-reload (no restart)
   - Debug endpoints for troubleshooting
   - Comprehensive logging

5. **Future-Proof**
   - Works for FM, CB, FR, and beyond
   - New systems don't require code changes
   - Plugin architecture supports new requirements

---

## 7. Extensibility Model

### Plugin Types (All Extensible)

```
┌─────────────────────────────────────────┐
│  WEAVER EXTENSIBILITY MODEL             │
├─────────────────────────────────────────┤
│                                         │
│  1. DISCOVERER (Find replicas)          │
│     ├─ etcd                             │
│     ├─ Consul                           │
│     ├─ Kubernetes API                   │
│     ├─ DNS                              │
│     ├─ Static list                      │
│     └─ Custom (user-provided)           │
│                                         │
│  2. HEALTH CHECKER (Check health)       │
│     ├─ HTTP                             │
│     ├─ gRPC                             │
│     ├─ TCP                              │
│     └─ Custom                           │
│                                         │
│  3. LOAD BALANCER (Select replica)      │
│     ├─ Least Connections                │
│     ├─ Round Robin                      │
│     ├─ Random                           │
│     ├─ Consistent Hash                  │
│     ├─ Weighted                         │
│     ├─ Sticky                           │
│     ├─ Resource-Aware                   │
│     └─ Custom                           │
│                                         │
│  4. PROTOCOL HANDLER (Forward request)  │
│     ├─ gRPC                             │
│     ├─ HTTP                             │
│     ├─ REST                             │
│     └─ Custom                           │
│                                         │
│  5. AUTHENTICATOR (Who are you?)        │
│     ├─ Bearer Token                     │
│     ├─ API Key                          │
│     ├─ OAuth                            │
│     ├─ JWT                              │
│     ├─ mTLS                             │
│     └─ Custom                           │
│                                         │
│  6. AUTHORIZER (Are you allowed?)       │
│     ├─ RBAC                             │
│     ├─ ABAC                             │
│     └─ Custom                           │
│                                         │
│  7. RATE LIMITER (Throttle requests)    │
│     ├─ Token Bucket                     │
│     ├─ Sliding Window                   │
│     └─ Custom                           │
│                                         │
│  8. METRICS COLLECTOR (Observe)         │
│     ├─ Prometheus                       │
│     ├─ StatsD                           │
│     ├─ OpenTelemetry                    │
│     └─ Custom                           │
│                                         │
│  9. REQUEST TRANSFORMER (Mutate)        │
│     ├─ Header injection                 │
│     ├─ Body rewrite                     │
│     └─ Custom                           │
│                                         │
└─────────────────────────────────────────┘
```

---

## 8. Success Criteria

### Phase 1 (Core Gateway)

✅ **Functionality:**
- [x] Pod discovery (etcd, static)
- [x] Health monitoring (HTTP, gRPC)
- [x] All 8 load balancing strategies
- [x] gRPC + HTTP listeners
- [x] Request buffering + backpressure
- [x] Circuit breaker, retry, timeout
- [x] Rate limiting (multi-dimensional)
- [x] Prometheus metrics
- [x] OpenTelemetry tracing
- [x] JSON structured logging
- [x] FM + CB configs validated
- [x] Future system (FR) config works

✅ **Performance:**
- [x] Routing latency <1ms p99
- [x] 100k req/s throughput
- [x] <50MB memory baseline
- [x] Health check <100ms/replica
- [x] Config reload <1s

✅ **Reliability:**
- [x] 99.99% uptime SLA
- [x] Zero data loss (queue-based)
- [x] Graceful degradation
- [x] Automatic failover (<5s)

✅ **Operability:**
- [x] Single YAML config
- [x] Hot-reload
- [x] Debug API
- [x] Comprehensive docs
- [x] Operator runbook

### Phase 2+ (Advanced Features)

🔄 **Future:**
- [ ] Request mirroring
- [ ] Canary deployment
- [ ] A/B testing infrastructure
- [ ] Multi-region federation
- [ ] Kubernetes CRDs
- [ ] Helm chart
- [ ] Grafana dashboard templates

---

**Next:** Read [weaver-hld.md](./weaver-hld.md) for high-level architecture details.
