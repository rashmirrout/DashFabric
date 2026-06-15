# Weaver: Complete User Guide & Configuration Reference

> **Status:** Production Documentation  
> **Version:** 1.0 (Phase 1)  
> **Audience:** Operators, DevOps, Power Users, Integrators  
> **Last Updated:** 2026-06-15

---

## Table of Contents

1. [Configuration Overview](#configuration-overview)
2. [Complete YAML Schema](#complete-yaml-schema)
3. [Load Balancing Strategies](#load-balancing-strategies)
4. [Pod Discovery Methods](#pod-discovery-methods)
5. [Health Monitoring](#health-monitoring)
6. [Reliability Patterns](#reliability-patterns)
7. [Authentication & Authorization](#authentication--authorization)
8. [Rate Limiting](#rate-limiting)
9. [Observability Configuration](#observability-configuration)
10. [Use Case Walkthroughs](#use-case-walkthroughs)
11. [Troubleshooting](#troubleshooting)
12. [Metrics Reference](#metrics-reference)

---

## Configuration Overview

Weaver is configured entirely through YAML. All behavior—discovery, routing, reliability, observability—is driven by configuration; no code changes are needed to support new systems.

**Configuration Structure:**
```
weaver-config.yaml
├── gateway          # Metadata, ports, modes
├── discovery        # Pod discovery configuration
├── health           # Health check configuration
├── listeners        # gRPC, HTTP, REST endpoints
├── routing          # Default routing strategy
├── load_balancers   # Strategy definitions
├── reliability      # Circuit breaker, retry, timeout
├── rate_limiting    # Multi-dimensional rate limits
├── observability    # Metrics, tracing, logging
├── authentication   # Auth provider configurations
├── authorization    # RBAC/ABAC policies
└── plugins          # Custom plugin loading
```

---

## Complete YAML Schema

### Gateway Section
```yaml
gateway:
  name: "my-gateway"                    # Identifier for this gateway instance
  description: "FM cluster gateway"     # Optional description
  mode: "primary_aware"                 # or "peer_equivalent", "load_balanced"
  
  listeners:
    grpc:
      enabled: true
      port: 5051
      max_connections: 10000
      keepalive_interval: 30s
      keepalive_timeout: 10s
      
    http:
      enabled: true
      port: 8080
      max_connections: 10000
      read_timeout: 30s
      write_timeout: 30s
      
    metrics:
      enabled: true
      port: 9090
      path: "/metrics"
      
  shutdown:
    grace_period: 30s                  # Graceful shutdown timeout
    drain_connections: true             # Drain in-flight requests before exit
```

### Discovery Section
```yaml
discovery:
  type: "t2_etcd"                       # or "consul", "kubernetes", "dns", "static"
  poll_interval: 10s                    # How often to refresh replica list
  
  # For T2 etcd
  config:
    endpoint: "http://etcd-t2:2379"    # etcd endpoint
    key_pattern: "/dashfabric/cluster/pods/fm-*"  # Glob pattern
    timeout: 5s
    
  # For Consul
  # config:
  #   endpoint: "http://consul:8500"
  #   service_name: "fm-service"
  #   datacenter: "us-west-1"
  
  # For Kubernetes
  # config:
  #   namespace: "default"
  #   label_selector: "app=fm,tier=replica"
  #   field_selector: "status.phase=Running"
  
  # For static list
  # config:
  #   replicas:
  #     - name: "fm-replica-1"
  #       address: "10.0.1.10:5050"
  #     - name: "fm-replica-2"
  #       address: "10.0.1.11:5050"
  
  # For DNS
  # config:
  #   domain: "fm.cluster.local"
  #   port: 5050
```

### Health Monitoring Section
```yaml
health:
  enabled: true
  interval: 10s                        # Check interval
  timeout: 5s                          # Per-replica timeout
  
  # HTTP health check
  type: "http"
  config:
    endpoint: "/api/v1/health"         # Path on replica
    expected_status: 200               # Expected HTTP status code
    method: "GET"
    
  # Alternative: gRPC health check
  # type: "grpc"
  # config:
  #   service: "grpc.health.v1.Health"
  #   method: "Check"
  
  # Alternative: TCP connection check
  # type: "tcp"
  # config:
  #   timeout: 2s
  
  # State transitions
  consecutive_failures: 3              # Failures before marking unhealthy
  consecutive_successes: 1             # Successes before marking healthy
  
  # Panic mode (circuit breaker for entire gateway)
  panic_mode:
    enabled: true
    threshold_percent: 50              # If >50% replicas down, enter panic mode
    notify_url: "https://alerts.example.com/webhook"  # Optional webhook
    log_level: "ERROR"
```

### Load Balancers Section
```yaml
load_balancers:
  - name: "default"
    type: "least_connections"
    # See Load Balancing Strategies section for all strategy configs
    
  - name: "hash_based"
    type: "consistent_hash"
    config:
      hash_key: "client_id"           # or "source_ip", "user_id", custom
      virtual_nodes: 150               # Replicas per hash ring node
      
  - name: "weighted"
    type: "weighted"
    config:
      weights:
        "fm-replica-1": 2              # 2x traffic vs others
        "fm-replica-2": 1
        "fm-replica-3": 1
```

### Routing Section
```yaml
routing:
  default_strategy: "least_connections"  # Fall back strategy
  timeout: 30s                           # Global request timeout
  
  rules:
    # Route /api/* to "api_servers" pool
    - path_prefix: "/api"
      pool: "api_servers"
      strategy: "round_robin"
      timeout: 15s
      
    # Route /health/* to any replica
    - path_prefix: "/health"
      pool: "*"
      strategy: "random"
      timeout: 5s
```

### Reliability Section
```yaml
reliability:
  # Circuit breaker configuration
  circuit_breaker:
    enabled: true
    failure_threshold: 5              # Failures before opening
    success_threshold: 2              # Successes before closing
    timeout: 30s                      # Time in HALF_OPEN before retry
    metrics_window: 60s               # Window for counting failures
    
  # Retry configuration
  retry:
    enabled: true
    max_attempts: 3
    backoff_strategy: "exponential"   # or "linear", "constant"
    initial_backoff: 10ms
    max_backoff: 5s
    backoff_multiplier: 2.0
    retryable_status_codes: [503, 504, 429]  # gRPC: UNAVAILABLE, DEADLINE_EXCEEDED
    
  # Timeout configuration
  timeout:
    global: 30s                       # Overall request timeout
    per_replica: 25s                  # Individual replica timeout
    connect: 5s                       # Connection establishment
    
  # Request queuing
  queuing:
    enabled: true
    per_replica_depth: 1000           # Max queued requests per replica
    overflow_behavior: "reject_with_503"  # or "drop_oldest"
    max_wait_time: 30s                # Reject if >30s in queue
```

### Rate Limiting Section
```yaml
rate_limiting:
  enabled: true
  
  # Global rate limit
  global:
    requests_per_second: 100000       # Aggregate across all clients
    
  # Per-client rate limits
  per_client:
    enabled: true
    requests_per_second: 10000        # Per unique client_id
    requests_per_minute: 100000
    
  # Per-IP rate limits
  per_ip:
    enabled: true
    requests_per_second: 5000         # Per source IP
    requests_per_minute: 50000
    
  # Per-tenant rate limits
  per_tenant:
    enabled: true
    requests_per_second: 10000
    extractor_key: "x-tenant-id"      # HTTP header or gRPC metadata key
    
  # Per-API-key rate limits
  per_api_key:
    enabled: true
    requests_per_second: 1000
    extractor_key: "x-api-key"
    
  # Rate limiter algorithm
  algorithm: "token_bucket"           # or "leaky_bucket", "sliding_window"
  
  # Exceeding limit behavior
  exceed_behavior: "reject_with_429"  # or "queue", "sample"
```

### Observability Section
```yaml
observability:
  # Prometheus metrics
  metrics:
    enabled: true
    namespace: "fm_gw"                # Prefix for all metrics
    subsystem: "gateway"
    port: 9090
    path: "/metrics"
    interval: 30s                     # Collection interval
    
    # Which metrics to collect
    replica_health: true              # replica_status gauge
    active_connections: true          # active_connections counter
    request_latency: true             # request_latency_ms histogram
    request_throughput: true          # requests_total counter
    error_rate: true                  # errors_total counter
    queue_depth: true                 # queue_depth gauge
    circuit_breaker_state: true       # circuit_breaker_state gauge
    rate_limit_violations: true       # rate_limit_violations counter
    
  # OpenTelemetry tracing
  tracing:
    enabled: true
    provider: "jaeger"                # or "otlp", "zipkin"
    service_name: "fm-gateway"
    sample_rate: 0.1                  # 10% sampling
    
    # Jaeger exporter
    config:
      endpoint: "http://jaeger:6831"
      port: 6831
      
    # Which spans to collect
    root_span_name: "gateway.request"
    child_spans:
      - "discovery.select_replica"
      - "lb.choose"
      - "reliability.route"
      - "replica.forward"
      - "replica.response"
      
    # Span attributes
    include_client_id: true
    include_request_size: true
    include_response_size: true
    include_error_details: true
    
  # Structured JSON logging
  logging:
    enabled: true
    level: "INFO"                     # DEBUG, INFO, WARN, ERROR
    format: "json"                    # or "text", "logfmt"
    
    # Async buffering (prevents latency spike)
    async: true
    buffer_size: 10000
    flush_interval: 100ms
    
    # Fields to include
    include_timestamp: true
    include_request_id: true
    include_client_id: true
    include_replica: true
    include_latency: true
    include_status_code: true
    include_error_stack: true
    
    # Log sampling (reduce volume)
    sample_rate: 1.0                  # 1.0 = 100% (all logs)
    error_sample_rate: 1.0            # Always log errors
    success_sample_rate: 0.1          # 10% of successful requests
    
  # Debug API endpoints
  debug_api:
    enabled: true
    port: 8080
    
    endpoints:
      /debug/replicas: "GET"          # List all replicas
      /debug/replicas/{name}: "GET"   # Detail for one replica
      /debug/config: "GET"            # Current configuration
      /debug/metrics: "GET"           # Live metrics snapshot
      /debug/logs: "GET"              # Recent logs
      /debug/traces: "GET"            # Trace queries
```

### Authentication Section
```yaml
authentication:
  enabled: true
  
  # Bearer token authentication
  bearer:
    enabled: true
    header_name: "authorization"      # HTTP header or gRPC metadata
    prefix: "Bearer"
    validate_endpoint: "http://auth:8080/validate"
    cache_ttl: 60s                    # Cache validation result
    
  # API key authentication
  api_key:
    enabled: true
    header_name: "x-api-key"
    validate_endpoint: "http://auth:8080/validate-key"
    cache_ttl: 60s
    
  # JWT authentication
  jwt:
    enabled: true
    header_name: "authorization"
    prefix: "Bearer"
    jwks_url: "https://auth.example.com/.well-known/jwks.json"
    cache_ttl: 3600s
    audience: "fm-gateway"
    issuer: "https://auth.example.com"
    
  # OAuth2 authentication
  oauth2:
    enabled: false
    provider: "google"                # or "github", "azure", custom
    client_id: "${OAUTH_CLIENT_ID}"   # Environment variable
    client_secret: "${OAUTH_CLIENT_SECRET}"
    
  # mTLS authentication
  mtls:
    enabled: true
    ca_cert_path: "/etc/weaver/ca.crt"
    require_client_cert: true
    
  # Fallback behavior
  fallback_behavior: "deny"           # "allow" or "deny" if auth fails
```

### Authorization Section
```yaml
authorization:
  enabled: true
  
  # RBAC (Role-Based Access Control)
  rbac:
    enabled: true
    
    roles:
      - name: "admin"
        permissions:
          - "*"
          
      - name: "subscriber"
        permissions:
          - "subscribe"
          - "publish"
          
      - name: "observer"
        permissions:
          - "get_metrics"
          - "get_logs"
    
    # Role assignment
    role_mapping:
      endpoint: "http://auth:8080/user-roles"
      cache_ttl: 300s
      
  # Policy enforcement
  policies:
    - resource: "/subscribe"
      methods: ["POST"]
      allowed_roles: ["subscriber", "admin"]
      
    - resource: "/metrics"
      methods: ["GET"]
      allowed_roles: ["observer", "admin"]
```

### TLS/mTLS Section
```yaml
tls:
  enabled: true
  
  # Server certificate
  server:
    cert_path: "/etc/weaver/server.crt"
    key_path: "/etc/weaver/server.key"
    
  # Client certificate verification
  client:
    enabled: true
    ca_cert_path: "/etc/weaver/ca.crt"
    required: false                    # Require client cert for mTLS
    
  # Minimum TLS version
  min_version: "1.2"                   # "1.2", "1.3"
  
  # Cipher suites
  cipher_suites:
    - "TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384"
    - "TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384"
```

---

## Load Balancing Strategies

### 1. Least Connections (LC)

**Best for:** Long-lived connections (gRPC streams, WebSockets).

**Algorithm:** Route to replica with fewest active connections.

```yaml
load_balancers:
  - name: "least_conn"
    type: "least_connections"
    config:
      check_interval: 100ms           # How often to sample connection counts
```

**Example:** 3 replicas with 100, 50, 75 connections → next request → 50-conn replica.

---

### 2. Round Robin (RR)

**Best for:** Stateless services; simple load distribution.

**Algorithm:** Cycle through replicas sequentially.

```yaml
load_balancers:
  - name: "round_robin"
    type: "round_robin"
    config:
      reset_interval: 1h              # Reset counter periodically
```

**Example:** Replicas [A, B, C] → requests → A, B, C, A, B, C, ...

---

### 3. Random

**Best for:** Unpredictable workload; chaos testing.

**Algorithm:** Pick random replica on each request.

```yaml
load_balancers:
  - name: "random"
    type: "random"
    config:
      seed: 12345                     # Reproducible randomness (optional)
```

**Example:** Each request picks uniformly from healthy replicas.

---

### 4. Consistent Hash

**Best for:** Session affinity; cache locality; consistent ordering.

**Algorithm:** Hash client_id → position on hash ring → closest replica.

```yaml
load_balancers:
  - name: "hash"
    type: "consistent_hash"
    config:
      hash_key: "client_id"           # or "source_ip", "user_id", custom field
      virtual_nodes: 150              # Replicas per hash ring node (more = better distribution)
      hash_function: "md5"            # or "sha1", "crc32"
```

**Example:** client_id="alice" always → same replica (unless replica fails, then next on ring).

**Use case:** FM: all Subscribe calls from one client → same FM replica (state locality).

---

### 5. Weighted

**Best for:** Heterogeneous replicas (different capacities, CPU, memory).

**Algorithm:** Assign weight to each replica; select proportionally.

```yaml
load_balancers:
  - name: "weighted"
    type: "weighted"
    config:
      weights:
        "fm-replica-1": 3             # Powerful machine
        "fm-replica-2": 2
        "fm-replica-3": 1             # Modest machine
      weight_source: "config"         # or "replica_metric"
```

**Example:** Requests: 50% → replica-1, 33% → replica-2, 17% → replica-3.

---

### 6. Sticky (Session Affinity)

**Best for:** Requests with shared state; session pinning.

**Algorithm:** Route same client to same replica for TTL; after TTL, rebalance.

```yaml
load_balancers:
  - name: "sticky"
    type: "sticky"
    config:
      ttl: 300s                       # How long to stick to same replica
      key: "client_id"                # or "source_ip", "session_id"
      hash_on_failure: true           # Use consistent hash if replica down
```

**Example:** client_id="bob" → replica-2 for 5 min; then re-route based on LC.

---

### 7. Resource-Aware

**Best for:** Dynamic load based on replica resource availability.

**Algorithm:** Select replica with lowest resource usage (CPU, memory, queue depth).

```yaml
load_balancers:
  - name: "resource_aware"
    type: "resource_aware"
    config:
      metrics:
        - "cpu_percent"               # From metrics endpoint
        - "memory_percent"
        - "queue_depth"
      weights:
        "cpu_percent": 0.5            # Weight factors
        "memory_percent": 0.3
        "queue_depth": 0.2
      update_interval: 5s             # Refresh metrics this often
```

**Example:** Replica A: 80% CPU; Replica B: 30% CPU → prefer B.

---

### 8. Custom

**Best for:** Domain-specific logic (e.g., geography, availability zone, priority).

**Algorithm:** User-provided plugin.

```yaml
load_balancers:
  - name: "custom_geo"
    type: "custom"
    config:
      plugin_path: "/opt/weaver/plugins/geo_lb.so"
      plugin_config:
        prefer_az: "us-west-1a"       # Custom config passed to plugin
```

**Example:** Plugin implements: select replica in preferred AZ; fall back to others if full.

---

## Pod Discovery Methods

### T2 etcd Discovery

```yaml
discovery:
  type: "t2_etcd"
  poll_interval: 10s
  
  config:
    endpoint: "http://etcd-t2:2379"
    key_pattern: "/dashfabric/cluster/pods/fm-*"
    timeout: 5s
    
    # Optional: filter by metadata
    metadata_filters:
      - key: "status"
        value: "running"
```

**How it works:** Polls etcd every 10s for keys matching pattern. Parses replica metadata (address, port, status).

**Example etcd entry:**
```json
{
  "key": "/dashfabric/cluster/pods/fm-1",
  "value": {
    "replica_id": "fm-1",
    "address": "10.0.1.10",
    "port": 5050,
    "status": "running",
    "region": "us-west"
  }
}
```

---

### Consul Discovery

```yaml
discovery:
  type: "consul"
  poll_interval: 10s
  
  config:
    endpoint: "http://consul:8500"
    service_name: "fm-service"
    datacenter: "us-west-1"
    filter: "Status == passing"       # Only healthy services
```

**How it works:** Query Consul service catalog; filter by status.

---

### Kubernetes Discovery

```yaml
discovery:
  type: "kubernetes"
  poll_interval: 10s
  
  config:
    namespace: "production"
    label_selector: "app=fm,tier=replica"
    field_selector: "status.phase=Running"
    port: 5050
```

**How it works:** Query K8s API for pods matching label/field selectors; extract pod IPs and port.

---

### DNS Discovery

```yaml
discovery:
  type: "dns"
  poll_interval: 30s                  # DNS caching, so slower polling OK
  
  config:
    domain: "fm-replicas.cluster.local"  # SRV record or A record
    port: 5050
    srv_lookup: true                  # Use SRV records for port discovery
```

**How it works:** Query DNS (A or SRV records); return all resolved IPs as replicas.

---

### Static List Discovery

```yaml
discovery:
  type: "static"
  poll_interval: 0s                   # No polling; fixed list
  
  config:
    replicas:
      - name: "fm-replica-1"
        address: "10.0.1.10:5050"
        weight: 2
      - name: "fm-replica-2"
        address: "10.0.1.11:5050"
        weight: 1
```

**How it works:** Use hardcoded replica list (good for testing, small deployments).

---

## Health Monitoring

### HTTP Health Check

```yaml
health:
  type: "http"
  interval: 10s
  timeout: 5s
  
  config:
    endpoint: "/api/v1/health"       # Path on replica
    expected_status: 200
    method: "GET"
    expected_body: '{"status":"ok"}'  # Optional: match response body
    
  consecutive_failures: 3            # Mark down after 3 failures
  consecutive_successes: 1           # Mark up after 1 success
```

**Flow:**
```
T=0s:   GET /api/v1/health → 200 OK (HEALTHY)
T=10s:  GET /api/v1/health → 500 (failure 1/3)
T=20s:  GET /api/v1/health → 500 (failure 2/3)
T=30s:  GET /api/v1/health → 500 (failure 3/3) → UNHEALTHY
T=40s:  GET /api/v1/health → 200 OK (success 1/1) → HEALTHY
```

---

### gRPC Health Check

```yaml
health:
  type: "grpc"
  interval: 10s
  timeout: 5s
  
  config:
    service: "grpc.health.v1.Health"
    method: "Check"
    expected_status: "SERVING"
```

**Standard:** Follows [gRPC health check protocol](https://github.com/grpc/grpc/blob/master/doc/health-checking.md).

---

### TCP Health Check

```yaml
health:
  type: "tcp"
  interval: 10s
  timeout: 5s
  
  config:
    connect_timeout: 2s               # How long to wait for connection
```

**Flow:** Try to establish TCP connection; if succeeds, HEALTHY; if fails, UNHEALTHY.

---

## Reliability Patterns

### Circuit Breaker

**State Machine:**

```
CLOSED (healthy) → threshold failures → OPEN (fail-fast)
   ↑                                        ↓
   └─── success after timeout ← HALF_OPEN (retry)
```

**Configuration:**

```yaml
reliability:
  circuit_breaker:
    enabled: true
    failure_threshold: 5              # Failures before OPEN
    success_threshold: 2              # Successes before CLOSED (from HALF_OPEN)
    timeout: 30s                      # Time in HALF_OPEN before retrying
    metrics_window: 60s               # Count failures in 60s window
```

**Behavior:**

- **CLOSED:** Normal operation; count failures.
- **OPEN (failure_threshold reached):** Reject new requests immediately (fail-fast).
- **HALF_OPEN (after timeout):** Allow 1 probe request to test recovery.
- **CLOSED (probe succeeds):** Resume normal operation.

---

### Retry with Backoff

**Configuration:**

```yaml
reliability:
  retry:
    enabled: true
    max_attempts: 3
    backoff_strategy: "exponential"
    initial_backoff: 10ms
    max_backoff: 5s
    backoff_multiplier: 2.0
    retryable_status_codes: [503, 504, 429]
```

**Timeline:**

```
T=0ms:  Attempt 1 → 503 (UNAVAILABLE)
T=10ms: Wait 10ms
T=20ms: Attempt 2 → 503
T=40ms: Wait 20ms (10ms × 2)
T=60ms: Attempt 3 → 503
T=120ms: Wait max 5s (capped)
        Give up; return error
```

---

### Timeout Management

**Configuration:**

```yaml
reliability:
  timeout:
    global: 30s               # Entire request start→finish
    per_replica: 25s          # Individual replica response
    connect: 5s               # TCP/gRPC connection establishment
```

**Timeline:**

```
T=0s:    Request starts (global timeout = 30s)
T=2s:    Connecting to replica (connect timeout = 5s)
T=2.5s:  Connected
T=5s:    First byte from replica
T=27s:   Replica timeout (25s) → abort replica
T=27.5s: Try next replica
T=30s:   Global timeout → return error
```

---

### Request Queuing & Backpressure

**Configuration:**

```yaml
reliability:
  queuing:
    enabled: true
    per_replica_depth: 1000
    overflow_behavior: "reject_with_503"
    max_wait_time: 30s
```

**Behavior:**

```
Request arrives
  ├─ Replica queue full?
  │   └─ Yes → Overflow behavior:
  │       ├─ reject_with_503 → Return 503 (Unavailable)
  │       └─ drop_oldest → Evict oldest queued request
  │
  └─ No → Enqueue
        └─ Wait for replica capacity
            └─ max_wait_time exceeded?
                └─ Yes → Reject with 503
```

---

## Authentication & Authorization

### Bearer Token

**Configuration:**

```yaml
authentication:
  bearer:
    enabled: true
    header_name: "authorization"
    prefix: "Bearer"
    validate_endpoint: "http://auth-service:8080/validate"
    cache_ttl: 60s
```

**Flow:**

```
Request: GET /api/metrics
Header:  Authorization: Bearer eyJhbGc...

Weaver:
  1. Extract "Bearer eyJhbGc..."
  2. Call auth-service:/validate?token=eyJhbGc...
  3. Response: {valid: true, user_id: "alice", roles: ["admin"]}
  4. Cache result for 60s
  5. Allow request
```

---

### API Key

**Configuration:**

```yaml
authentication:
  api_key:
    enabled: true
    header_name: "x-api-key"
    validate_endpoint: "http://auth-service:8080/validate-key"
    cache_ttl: 60s
```

**Flow:**

```
Request: GET /api/metrics
Header:  x-api-key: sk-1234567890

Weaver:
  1. Extract "sk-1234567890"
  2. Call auth-service:/validate-key?key=sk-1234567890
  3. Response: {valid: true, api_key_id: "key-42", quota: 100000}
  4. Allow request
```

---

### JWT

**Configuration:**

```yaml
authentication:
  jwt:
    enabled: true
    header_name: "authorization"
    prefix: "Bearer"
    jwks_url: "https://auth.example.com/.well-known/jwks.json"
    audience: "fm-gateway"
    issuer: "https://auth.example.com"
```

**Flow:**

```
Request: GET /api/metrics
Header:  Authorization: Bearer eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9...

Weaver:
  1. Extract JWT
  2. Download public keys from JWKS endpoint
  3. Verify signature using public key
  4. Check iss="https://auth.example.com"
  5. Check aud="fm-gateway"
  6. Check exp > now
  7. Allow request; extract claims
```

---

### mTLS

**Configuration:**

```yaml
authentication:
  mtls:
    enabled: true
    ca_cert_path: "/etc/weaver/ca.crt"
    require_client_cert: true

tls:
  client:
    enabled: true
    ca_cert_path: "/etc/weaver/ca.crt"
    required: true
```

**Flow:**

```
TLS Handshake:
  1. Client presents cert signed by CA
  2. Weaver verifies cert chain against ca.crt
  3. Weaver extracts CN (Common Name) as client identity
  4. Weaver checks authorization based on CN
  5. Allow TLS connection
```

---

## Rate Limiting

### Per-Client Rate Limiting

**Configuration:**

```yaml
rate_limiting:
  per_client:
    enabled: true
    requests_per_second: 10000
    requests_per_minute: 100000
    extractor_key: "client_id"        # Extract from gRPC metadata or HTTP header
```

**Example:** client_id="fm-adapter-1" → limited to 10k req/s; once exceeded, return 429.

---

### Per-IP Rate Limiting

**Configuration:**

```yaml
rate_limiting:
  per_ip:
    enabled: true
    requests_per_second: 5000
    requests_per_minute: 50000
```

**Example:** source_ip="10.0.1.5" → limited to 5k req/s; once exceeded, return 429.

---

### Per-Tenant Rate Limiting

**Configuration:**

```yaml
rate_limiting:
  per_tenant:
    enabled: true
    requests_per_second: 10000
    extractor_key: "x-tenant-id"      # HTTP header or gRPC metadata key
```

**Example:** x-tenant-id="acme-corp" → limited to 10k req/s.

---

### Multi-Dimensional Composition

```yaml
rate_limiting:
  # Apply all limits; reject if ANY limit exceeded

  global:
    requests_per_second: 100000       # Absolute cap
    
  per_client:
    enabled: true
    requests_per_second: 10000        # Per client within global cap
    
  per_ip:
    enabled: true
    requests_per_second: 5000         # Per IP within per-client cap
    
  exceed_behavior: "reject_with_429"
```

**Example:**
```
Global limit: 100k req/s
Per-client limit: 10k req/s
Per-IP limit: 5k req/s

Client "alice" from IP "10.0.1.1" sends 6k req/s:
  ✗ Per-IP limit: 5k (exceeded)
  → Return 429 (Too Many Requests)
```

---

## Observability Configuration

### Prometheus Metrics

**Configuration:**

```yaml
observability:
  metrics:
    enabled: true
    namespace: "fm_gw"
    port: 9090
    path: "/metrics"
```

**Available Metrics:**

```
fm_gw_replica_status{replica="fm-1"}        # 1 = healthy, 0 = unhealthy
fm_gw_active_connections{replica="fm-1"}    # Current active streams
fm_gw_request_latency_ms_bucket              # Latency histogram
fm_gw_requests_total{method="subscribe"}     # Request count by method
fm_gw_errors_total{replica="fm-1"}           # Error count per replica
fm_gw_queue_depth{replica="fm-1"}            # Queued requests per replica
fm_gw_circuit_breaker_state{replica="fm-1"}  # CB state (0=closed, 1=open, 2=half-open)
fm_gw_rate_limit_violations_total            # Rate limit rejections
```

---

### OpenTelemetry Tracing

**Configuration:**

```yaml
observability:
  tracing:
    enabled: true
    provider: "jaeger"
    service_name: "fm-gateway"
    sample_rate: 0.1                  # 10% of traces
    
    config:
      endpoint: "http://jaeger:6831"
```

**Span Structure:**

```
gateway.request [root]
  ├─ discovery.select_replica
  ├─ lb.choose
  ├─ reliability.route
  ├─ replica.forward [spans over network]
  │   └─ [replica handles request]
  └─ replica.response
```

**Trace Example (Jaeger UI):**

```
Service: fm-gateway
Operation: gateway.request
Trace ID: abc123def456
Spans:
  ├─ gateway.request (12.5ms)
  │  ├─ discovery.select_replica (0.1ms) [cached]
  │  ├─ lb.choose (0.3ms)
  │  │  ├─ attribute: selected_replica = "fm-1"
  │  │  └─ attribute: active_connections = 42
  │  ├─ reliability.route (2.1ms)
  │  ├─ replica.forward (10.0ms)
  │  │  ├─ attribute: replica = "fm-1"
  │  │  ├─ attribute: request_type = "Subscribe"
  │  │  └─ error: grpc_status = 0
  │  └─ replica.response (0.0ms)
  └─ Total: 12.5ms
```

---

### Structured JSON Logging

**Configuration:**

```yaml
observability:
  logging:
    enabled: true
    level: "INFO"
    format: "json"
    async: true
    buffer_size: 10000
    flush_interval: 100ms
```

**Log Example:**

```json
{
  "timestamp": "2026-06-15T10:30:45.123Z",
  "level": "INFO",
  "service": "fm-gateway",
  "request_id": "req-abc123",
  "event": "request_routed",
  "client_id": "fm-adapter-1",
  "method": "Subscribe",
  "selected_replica": "fm-1",
  "latency_ms": 12.5,
  "status_code": 0,
  "active_connections": 42,
  "queue_depth": 5
}
```

---

## Use Case Walkthroughs

### Use Case 1: FM (Primary-Aware Routing)

**Scenario:** FM system with 1 primary + 2 read replicas. Writes → PRIMARY only; Reads → load-balanced.

**Configuration:**

```yaml
gateway:
  name: "fm-gateway"
  mode: "primary_aware"
  
discovery:
  type: "t2_etcd"
  config:
    endpoint: "http://etcd-t2:2379"
    key_pattern: "/dashfabric/cluster/pods/fm-*"

health:
  type: "http"
  interval: 10s
  config:
    endpoint: "/api/v1/health"

load_balancers:
  - name: "write_lb"
    type: "random"                    # Writes: any replica, but... (see routing rules)
  - name: "read_lb"
    type: "least_connections"

routing:
  rules:
    - operation: "Publish"            # FM write operation
      load_balancer: "write_lb"
      # Special rule: if replica not primary, skip and try next
      
    - operation: "Subscribe"          # FM read operation
      load_balancer: "read_lb"        # Any replica OK
      
reliability:
  circuit_breaker:
    enabled: true
    failure_threshold: 3
    timeout: 30s
    
  retry:
    enabled: true
    max_attempts: 3
    backoff_strategy: "exponential"
    
rate_limiting:
  per_client:
    enabled: true
    requests_per_second: 10000

observability:
  metrics:
    enabled: true
    namespace: "fm_gw"
  tracing:
    enabled: true
    service_name: "fm-gateway"
```

**Expected Behavior:**

```
Request: FM Adapter calls Subscribe("topic1")
  1. Discover replicas: [fm-1 (primary), fm-2 (replica), fm-3 (replica)]
  2. Health check: All healthy
  3. Select replica via least_connections LB: fm-2 (lowest connections)
  4. Route to fm-2
  5. fm-2 responds with topic updates
  6. Success; log latency, update metrics

Request: FM Adapter calls Publish("topic1", {...})
  1. Discover replicas: [fm-1, fm-2, fm-3]
  2. Health check: All healthy
  3. Routing rule: Publish → must go to primary
  4. Select replica: fm-1 (primary)
  5. Route to fm-1
  6. fm-1 updates topic state, replicates to fm-2, fm-3
  7. Success
```

---

### Use Case 2: CB (Peer-Equivalent Routing)

**Scenario:** CB system with 3 peer replicas (all equivalent). All requests → any healthy replica.

**Configuration:**

```yaml
gateway:
  name: "cb-gateway"
  mode: "peer_equivalent"
  
discovery:
  type: "t2_etcd"
  config:
    endpoint: "http://etcd-t2:2379"
    key_pattern: "/dashfabric/cluster/pods/cb-*"

health:
  type: "http"
  interval: 10s
  config:
    endpoint: "/api/v1/health"

load_balancers:
  - name: "default"
    type: "least_connections"

routing:
  default_strategy: "least_connections"
  timeout: 30s

reliability:
  circuit_breaker:
    enabled: true
    failure_threshold: 5
    timeout: 30s
    
  queuing:
    enabled: true
    per_replica_depth: 1000

observability:
  metrics:
    enabled: true
    namespace: "cb_gw"
  tracing:
    enabled: true
    service_name: "cb-gateway"
```

**Expected Behavior:**

```
Request 1: Subscribe("config-topics") from FM Adapter
  1. Discover: [cb-1, cb-2, cb-3] (all peer-equivalent)
  2. Select via least_connections: cb-1 (5 active streams)
  3. Route to cb-1
  4. Receive config stream from cb-1

Request 2: Publish("ack-topic", {...}) from FM Adapter
  1. Discover: [cb-1, cb-2, cb-3]
  2. Select via least_connections: cb-3 (2 active streams)
  3. Route to cb-3
  4. cb-3 stores ack in topic
  5. Eventual consistency: cb-1, cb-2 learn about ack

Multiple Subscribers:
  FM Adapter 1 → cb-1 (least conn at time of request)
  FM Adapter 2 → cb-2
  FM Adapter 3 → cb-3
  (Load distributed; no single replica overloaded)
```

---

### Use Case 3: Custom System (Future-Proof)

**Scenario:** New FR (Fabric Router) system with geographic affinity.

**Configuration:**

```yaml
gateway:
  name: "fr-gateway"
  mode: "load_balanced"
  
discovery:
  type: "consul"
  config:
    endpoint: "http://consul:8500"
    service_name: "fr-service"
    filter: "Status == passing"

health:
  type: "grpc"
  config:
    service: "grpc.health.v1.Health"

load_balancers:
  - name: "geo_aware"
    type: "custom"
    config:
      plugin_path: "/opt/weaver/plugins/geo_lb.so"
      plugin_config:
        prefer_region: "us-west"

routing:
  default_strategy: "geo_aware"

observability:
  metrics:
    enabled: true
    namespace: "fr_gw"
```

**Expected Behavior:**

```
Request: FR Client (in us-west region)
  1. Discover: [fr-1 (us-west-1a), fr-2 (us-east-1a), fr-3 (us-west-1b)]
  2. Load balancer plugin: prefer_region=us-west
  3. Select from [fr-1, fr-3] (us-west)
  4. Further selection: least_connections → fr-3
  5. Route to fr-3
  6. Low latency due to geographic affinity
```

---

### Use Case 4: Testing & Chaos Engineering

**Scenario:** Inject faults to test resilience.

**Configuration:**

```yaml
chaos:
  enabled: true
  
  replica_failure:
    enabled: true
    replica: "fm-1"
    duration: 60s                     # Simulate down for 60s
    inject_at: 15s
    
  latency_injection:
    enabled: true
    latency_ms: 500
    percentage: 10                    # 10% of requests to fm-2
    
  error_injection:
    enabled: true
    error_rate: 5                     # 5% of requests return error
    error_codes: [503, 504]

reliability:
  circuit_breaker:
    enabled: true
    failure_threshold: 2              # Lower threshold for faster detection
    timeout: 10s
    
  retry:
    max_attempts: 3
    backoff_strategy: "exponential"
```

**Expected Behavior:**

```
T=0s:    All replicas healthy
T=15s:   fm-1 injection starts (simulate down)
         Client requests to fm-1 fail
T=16s:   Circuit breaker detects 2 failures → OPEN
T=17s:   Requests fail-fast (avoid wasting time)
T=25s:   Circuit breaker enters HALF_OPEN → probe fm-1
T=30s:   fm-1 recovery begins (injection ends)
T=31s:   Probe succeeds → CLOSED
T=32s+:  Normal operation resumed
         Latency spike captured in metrics
         Errors logged and traced
```

**Verification:**

```bash
# Check metrics during chaos
curl http://localhost:9090/metrics | grep "fm_gw_circuit_breaker_state"
# Expected: fm_gw_circuit_breaker_state{replica="fm-1"} 1.0 (OPEN)

# Check logs
curl http://localhost:8080/debug/logs | jq '.[] | select(.replica == "fm-1")'
# Expected: Errors logged for fm-1

# Check trace
curl http://jaeger:6831/api/traces?service=fm-gateway | jq '.data[] | select(.tags.replica == "fm-1")'
# Expected: Traces show failures and retries
```

---

### Use Case 5: Multi-Tenant Isolation

**Scenario:** Single Weaver instance serving multiple tenants with strict rate limits.

**Configuration:**

```yaml
gateway:
  name: "multi-tenant-gateway"

discovery:
  type: "kubernetes"
  config:
    namespace: "production"
    label_selector: "app=shared-service"

load_balancers:
  - name: "least_conn"
    type: "least_connections"

rate_limiting:
  per_tenant:
    enabled: true
    requests_per_second: 1000         # Per tenant
    extractor_key: "x-tenant-id"
    exceed_behavior: "reject_with_429"
    
  per_client:
    enabled: true
    requests_per_second: 100          # Per client within tenant limit
    extractor_key: "x-client-id"

authentication:
  bearer:
    enabled: true
    validate_endpoint: "http://auth:8080/validate"
    cache_ttl: 60s

authorization:
  rbac:
    enabled: true
    role_mapping:
      endpoint: "http://auth:8080/user-roles"
```

**Expected Behavior:**

```
Request: 
  Header: x-tenant-id: acme-corp
  Header: x-client-id: acme-client-1
  Authorization: Bearer token123

Weaver:
  1. Authenticate: token123 → valid, user=alice
  2. Extract tenant: acme-corp
  3. Check per-tenant limit: acme-corp at 500/1000 req/s → OK
  4. Check per-client limit: acme-client-1 at 50/100 req/s → OK
  5. Extract role: alice → ["client-admin"]
  6. Check authorization: ["client-admin"] → can call /api/*
  7. Route to least-loaded replica
  8. Success

Tenant acme-corp now at 501/1000 req/s; client-1 at 51/100 req/s

New request from acme-client-2:
  Tenant at 1000/1000 limit (exceeded)
  → 429 Too Many Requests
  → Reject without routing
```

---

### Use Case 6: Failover & Disaster Recovery

**Scenario:** Primary replica down; gateway automatically fails over.

**Configuration:**

```yaml
health:
  interval: 10s
  consecutive_failures: 3
  
reliability:
  circuit_breaker:
    enabled: true
    failure_threshold: 5
    timeout: 30s
    
  retry:
    max_attempts: 3
    backoff_strategy: "exponential"

load_balancers:
  - name: "least_conn_with_fallback"
    type: "least_connections"
    config:
      fallback_strategy: "next_available"  # Try next replica if selected one fails
```

**Failure Timeline:**

```
T=0s:    All replicas healthy [fm-1, fm-2, fm-3]
T=5s:    fm-1 (primary) crashes (network partition)
T=10s:   Health check: fm-1 health endpoint unreachable (failure 1/3)
T=20s:   Health check: fm-1 unreachable (failure 2/3)
T=30s:   Health check: fm-1 unreachable (failure 3/3) → UNHEALTHY
         Replica state: [fm-1 (down), fm-2 (up), fm-3 (up)]

Request during outage (T=25s):
  1. LB selects fm-1 (not yet marked unhealthy)
  2. Route to fm-1 → timeout (5s)
  3. Retry attempt 2: Use fallback_strategy → fm-2
  4. Route to fm-2 → SUCCESS
  5. Total latency: ~7s (timeout + retry)

Request after T=30s (fm-1 marked unhealthy):
  1. LB excludes fm-1, selects from [fm-2, fm-3]
  2. Least connections: fm-2 (current connections = 10)
  3. Route to fm-2 → SUCCESS
  4. Latency: ~5ms (no timeout)

fm-1 Recovery (T=60s network partition heals):
  1. fm-1 service comes online
  2. Health check: fm-1 responds → success (1/1) → HEALTHY
  3. Replica state: [fm-1 (up), fm-2 (up), fm-3 (up)]
  4. LB now includes fm-1 in rotation
```

---

## Troubleshooting

### Issue: Replica marked UNHEALTHY but appears to be running

**Diagnosis:**

```bash
# Check health endpoint manually
curl http://<replica>:5050/api/v1/health

# Check gateway debug API
curl http://localhost:8080/debug/replicas/fm-1 | jq .

# Check logs for health check failures
curl http://localhost:8080/debug/logs | jq '.[] | select(.event == "health_check_failed")'
```

**Common Causes:**

1. **Health endpoint not responding** → Check replica is listening
2. **Firewall/network issue** → Test connectivity: `nc -zv <replica> 5050`
3. **Wrong endpoint path** → Verify config matches replica's actual health endpoint
4. **High latency** → Increase health check timeout in config

**Fix:**

```yaml
health:
  timeout: 10s                         # Increase from 5s
  interval: 15s                        # Slower polling if network flaky
```

---

### Issue: All requests getting 503 errors

**Diagnosis:**

```bash
# Check if any replicas are healthy
curl http://localhost:8080/debug/replicas | jq '.replicas[] | {name, status}'

# Check metrics
curl http://localhost:9090/metrics | grep "replica_status"

# Check queue depth
curl http://localhost:9090/metrics | grep "queue_depth"

# Check circuit breaker state
curl http://localhost:9090/metrics | grep "circuit_breaker_state"
```

**Common Causes:**

1. **All replicas unhealthy** → Check replica health and logs
2. **Queue overflow** → Requests being rejected due to backpressure
3. **Rate limit exceeded** → Global or per-client limit hit
4. **Circuit breaker OPEN** → Multiple replica failures

**Fix (depending on cause):**

```yaml
# Option 1: Increase queue depth
reliability:
  queuing:
    per_replica_depth: 5000            # Increase from 1000

# Option 2: Increase rate limit
rate_limiting:
  global:
    requests_per_second: 200000        # Increase capacity

# Option 3: Adjust circuit breaker thresholds
reliability:
  circuit_breaker:
    failure_threshold: 10              # More lenient (from 5)
    timeout: 60s                       # Longer recovery attempt
```

---

### Issue: High latency (p99 > 100ms)

**Diagnosis:**

```bash
# Check request latency histogram
curl http://localhost:9090/metrics | grep "request_latency_ms"

# Check replica latencies individually
curl http://localhost:8080/debug/replicas | jq '.replicas[] | {name, avg_latency_ms, p99_latency_ms}'

# Check traces for bottlenecks
curl http://jaeger:6831/api/traces?service=fm-gateway&limit=100 | jq '.data[] | {duration_ms, spans}'
```

**Common Causes:**

1. **Replica overloaded** → High active connection count
2. **Network latency** → Check replica connectivity
3. **Load imbalance** → LB not distributing evenly
4. **Queueing** → Requests waiting in queue

**Fix:**

```yaml
# Option 1: Use better LB strategy
load_balancers:
  - name: "resource_aware"
    type: "resource_aware"            # Pick least loaded
    config:
      metrics: ["cpu_percent", "queue_depth"]

# Option 2: Increase replica count
discovery:
  config:
    # Add more replicas via orchestration
    
# Option 3: Reduce timeout to fail faster
reliability:
  timeout:
    per_replica: 10s                  # Fail faster; retry
    connect: 2s
```

---

### Issue: Circuit breaker stuck in OPEN state

**Diagnosis:**

```bash
# Check CB state
curl http://localhost:9090/metrics | grep "circuit_breaker_state{replica=\"fm-1\"}"

# Check when it transitioned to OPEN
curl http://localhost:8080/debug/logs | jq '.[] | select(.event == "circuit_breaker_opened")'

# Check replica health
curl http://localhost:8080/debug/replicas/fm-1 | jq '.health_status'
```

**Common Causes:**

1. **Replica still unhealthy** → Health checks failing
2. **Circuit breaker timeout too long** → Stuck in HALF_OPEN → never probes
3. **Probe request failing** → Doesn't transition to CLOSED

**Fix:**

```yaml
reliability:
  circuit_breaker:
    timeout: 10s                      # Shorter timeout; probe more often
    success_threshold: 1              # Single success closes CB
    failure_threshold: 10             # More lenient (allow more failures before open)
```

---

### Issue: Memory usage increasing over time

**Diagnosis:**

```bash
# Check Weaver process memory
ps aux | grep weaver | grep -v grep

# Check queue depths (may be accumulating)
curl http://localhost:9090/metrics | grep "queue_depth"

# Check goroutine count
curl http://localhost:8080/debug/logs | jq '.[] | .goroutine_count' | tail -1
```

**Common Causes:**

1. **Request queue growing unbounded** → Backlog accumulating
2. **Goroutine leak** → Handlers not cleaning up
3. **Metrics/traces accumulating** → Too much sampled data

**Fix:**

```yaml
# Option 1: Reduce queue depth; reject overflow
reliability:
  queuing:
    per_replica_depth: 100            # Smaller queue
    overflow_behavior: "reject_with_503"

# Option 2: Increase sampling threshold
observability:
  tracing:
    sample_rate: 0.01                # 1% sampling (from 10%)
    
  logging:
    sample_rate: 0.5                 # 50% sampling
```

---

## Metrics Reference

### Replica Health

| Metric | Type | Labels | Meaning |
|--------|------|--------|---------|
| `fm_gw_replica_status` | Gauge | `replica` | 1 = healthy, 0 = unhealthy |
| `fm_gw_replica_active_connections` | Gauge | `replica` | Current active gRPC streams |
| `fm_gw_replica_health_checks_total` | Counter | `replica`, `result` | Total health checks (pass/fail) |
| `fm_gw_replica_health_check_duration_ms` | Histogram | `replica` | Health check latency |

**Example Query (Prometheus):**

```promql
# Unhealthy replicas
fm_gw_replica_status{job="fm-gateway"} == 0

# Replica with most connections
topk(1, fm_gw_replica_active_connections)

# Health check failure rate
rate(fm_gw_replica_health_checks_total{result="fail"}[5m])
```

---

### Request Metrics

| Metric | Type | Labels | Meaning |
|--------|------|--------|---------|
| `fm_gw_requests_total` | Counter | `method`, `status` | Total requests by method and status |
| `fm_gw_request_latency_ms` | Histogram | `method`, `replica` | Request latency distribution |
| `fm_gw_request_size_bytes` | Histogram | `method` | Request payload size |
| `fm_gw_response_size_bytes` | Histogram | `method` | Response payload size |

**Example Query:**

```promql
# Request latency p99
histogram_quantile(0.99, fm_gw_request_latency_ms)

# Requests per second
rate(fm_gw_requests_total[1m])

# Error rate
rate(fm_gw_requests_total{status="error"}[1m]) / rate(fm_gw_requests_total[1m])
```

---

### Reliability Metrics

| Metric | Type | Labels | Meaning |
|--------|------|--------|---------|
| `fm_gw_circuit_breaker_state` | Gauge | `replica` | CB state (0=CLOSED, 1=OPEN, 2=HALF_OPEN) |
| `fm_gw_circuit_breaker_transitions_total` | Counter | `replica`, `from`, `to` | CB state changes |
| `fm_gw_retries_total` | Counter | `replica`, `reason` | Retry attempts |
| `fm_gw_request_queue_depth` | Gauge | `replica` | Queued requests per replica |
| `fm_gw_queue_timeouts_total` | Counter | `replica` | Requests rejected due to queue timeout |

**Example Query:**

```promql
# Circuit breakers currently open
count(fm_gw_circuit_breaker_state == 1)

# Retry rate
rate(fm_gw_retries_total[5m])

# Queue saturation
fm_gw_request_queue_depth / 1000  # Assuming queue_depth: 1000
```

---

### Rate Limiting Metrics

| Metric | Type | Labels | Meaning |
|--------|------|--------|---------|
| `fm_gw_rate_limit_violations_total` | Counter | `limit_type` (global, client, ip, tenant) | Rejected requests |
| `fm_gw_rate_limit_tokens_consumed_total` | Counter | `limit_type` | Tokens used |
| `fm_gw_rate_limit_tokens_available` | Gauge | `limit_type` | Tokens remaining (current bucket) |

**Example Query:**

```promql
# Clients getting rate limited
rate(fm_gw_rate_limit_violations_total{limit_type="per_client"}[5m])

# Which tenant hit limit most?
topk(5, rate(fm_gw_rate_limit_violations_total{limit_type="per_tenant"}[5m]))
```

---

### Observability Metrics

| Metric | Type | Labels | Meaning |
|--------|------|--------|---------|
| `fm_gw_traces_sampled_total` | Counter | `service` | Traces exported |
| `fm_gw_logs_buffered` | Gauge | — | Buffered logs waiting flush |
| `fm_gw_metrics_collection_duration_ms` | Histogram | — | Time to collect all metrics |

---

## Dashboard Examples

### Grafana: Replica Health Dashboard

```
Row 1: Replica Status
  ├─ Panel: Healthy Replicas (fm_gw_replica_status == 1)
  ├─ Panel: Unhealthy Replicas (fm_gw_replica_status == 0)
  └─ Panel: Health Check Failure Rate

Row 2: Request Performance
  ├─ Panel: Request Latency (p50, p99, max)
  ├─ Panel: Request Rate (req/s)
  └─ Panel: Error Rate (%)

Row 3: Reliability
  ├─ Panel: Circuit Breaker States
  ├─ Panel: Retry Rate
  └─ Panel: Queue Depth

Row 4: Rate Limiting
  ├─ Panel: Rate Limit Violations
  └─ Panel: Top Violating Clients
```

---

**End of User Guide**

All configuration options documented. All scenarios covered. Ready for production deployment.
