# Weaver: FM Primary-Aware Scenario

> **Read Time:** 30 minutes  
> **Scenario:** Fabric Management (FM) system with primary replica for writes  
> **Audience:** Architects, Engineers, Operators  
> **Previous:** [13-verify-deployment.md](../QUICK_START/13-verify-deployment.md) | **Next:** [21-cb-peer-equivalent.md](./21-cb-peer-equivalent.md)

---

## WHAT: Problem Statement

**FM System Topology:**
- **1 PRIMARY replica** — Handles all write operations (registrations, updates)
- **N READ replicas** — Handle read operations (queries, subscriptions)
- **Asymmetric routing needed** — Weaver must route differently based on operation type

**Example Architecture:**

```
┌──────────────────────────────────┐
│        FM PRIMARY (write)        │
│  fm-1:5051 ✓ WRITE CAPABLE      │
├──────────────────────────────────┤
│  Single point for consistency    │
└──────────────────────────────────┘
         ▲
         │ (Registrations, updates)
         │
    ┌────┴─────┐
    │           │
┌───▼────┐ ┌───▼────┐ ┌────────┐
│ CLIENT  │ │ CLIENT  │ │ CLIENT  │
│ A       │ │ B       │ │ C       │
└─┬───────┘ └─┬───────┘ └────┬───┘
  │ Register  │ Query      │ Subscribe
  │           │            │
  └───────┬───┴────────────┬┘
          │                │
          ↓                ↓
    WEAVER GATEWAY
          │
    ┌─────┼─────┐
    │     │     │
    ↓     ↓     ↓
┌────────────────────────────────────┐
│  FM READ REPLICAS (read)           │
│  fm-2:5051 ✓ READ ONLY             │
│  fm-3:5051 ✓ READ ONLY             │
│  fm-4:5051 ✓ READ ONLY             │
│  (Replicate from fm-1)             │
├────────────────────────────────────┤
│  Distribute queries across replicas│
│  (load balance for throughput)     │
└────────────────────────────────────┘
```

---

## HOW: Configuration Walkthrough

### **Configuration File: weaver-config-fm.yaml**

```yaml
########################################
# WEAVER CONFIGURATION FOR FM SYSTEM
########################################

gateway:
  name: "fm-gateway"
  mode: "primary_aware"  # ← KEY: Enables primary-aware routing

########################################
# DISCOVERY: Find FM replicas in etcd
########################################
discovery:
  type: "t2_etcd"
  poll_interval: 10s
  config:
    endpoint: "http://etcd-t2:2379"
    key_pattern: "/dashfabric/cluster/pods/fm-*"
    timeout: 5s

########################################
# HEALTH MONITORING: Check replicas alive
########################################
health:
  enabled: true
  type: "http"
  interval: 10s
  timeout: 5s
  config:
    endpoint: "/api/v1/health"
    expected_status: 200
  
  # Mark unhealthy after 3 consecutive failures
  consecutive_failures: 3
  
  # Panic mode: if >50% of replicas down, don't reject everything
  panic_mode:
    enabled: true
    threshold_percent: 50

########################################
# LOAD BALANCING: Route to primary for writes, 
# distribute reads across all healthy replicas
########################################
load_balancers:
  # Primary-aware load balancer for write/read routing
  - name: "default"
    type: "primary_aware"
    config:
      # Primary replica is determined by metadata tag "role=primary"
      primary_selector: "role=primary"

########################################
# LISTENERS: Accept gRPC and HTTP requests
########################################
listeners:
  grpc:
    enabled: true
    port: 5051
    max_connections: 10000
    read_timeout: 30s
    write_timeout: 30s
  
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

########################################
# RELIABILITY: Handle failures gracefully
########################################
reliability:
  # Circuit Breaker: Stop sending to failing replicas
  circuit_breaker:
    enabled: true
    failure_threshold: 5         # Open if 5 failures
    success_threshold: 2         # Close if 2 successes in HALF_OPEN
    timeout: 30s                 # Try recovery after 30s
    
  # Retry: Automatically retry transient failures
  retry:
    enabled: true
    max_attempts: 3              # Retry up to 3 times
    backoff_strategy: "exponential"
    initial_backoff: 10ms        # First retry after 10ms
    max_backoff: 5s              # Never wait longer than 5s
  
  # Request Queuing: Buffer requests when replicas busy
  queuing:
    enabled: true
    per_replica_depth: 1000      # Max 1000 queued per replica
  
  # Timeouts: Don't wait forever
  timeout:
    global: 30s                  # Max time for entire request
    per_replica: 25s             # Max time per replica
    connect: 5s                  # Max time to establish connection

########################################
# RATE LIMITING: Control request rate
########################################
rate_limiting:
  enabled: true
  
  # Global rate limit
  global:
    enabled: true
    requests_per_second: 100000
  
  # Per-client rate limiting
  per_client:
    enabled: true
    requests_per_second: 10000
  
  # Per-IP rate limiting
  per_ip:
    enabled: true
    requests_per_second: 5000

########################################
# OBSERVABILITY: Metrics, tracing, logging
########################################
observability:
  # Prometheus metrics
  metrics:
    enabled: true
    namespace: "fm_gw"
    port: 9090
  
  # OpenTelemetry tracing
  tracing:
    enabled: true
    provider: "jaeger"
    sample_rate: 0.1             # Trace 10% of requests
    config:
      endpoint: "http://jaeger:6831"
  
  # Structured JSON logging
  logging:
    enabled: true
    level: "INFO"
    format: "json"
    async: true

########################################
# AUTHENTICATION & AUTHORIZATION
########################################
authentication:
  enabled: true
  method: "bearer_token"
  # Bearer token validation
  config:
    header_name: "Authorization"
    scheme: "Bearer"

authorization:
  enabled: true
  rbac:
    roles:
      admin:
        permissions: ["read", "write", "register"]
      operator:
        permissions: ["read", "subscribe"]
      viewer:
        permissions: ["read"]

########################################
# TLS ENCRYPTION
########################################
tls:
  enabled: true
  
  # Client -> Weaver
  server:
    enabled: true
    cert_path: "/etc/weaver/certs/server.crt"
    key_path: "/etc/weaver/certs/server.key"
    min_version: "1.2"
  
  # Weaver -> Replica
  client:
    enabled: true
    ca_path: "/etc/weaver/certs/ca.crt"
    skip_verify: false
```

---

## Step-by-Step Configuration Walkthrough

### **Step 1: Discovery**

Weaver polls etcd every 10 seconds:

```bash
# etcd contains FM replica registrations
/dashfabric/cluster/pods/fm-1 → {"role": "primary", "address": "10.0.1.5", "port": 5051}
/dashfabric/cluster/pods/fm-2 → {"role": "read",    "address": "10.0.1.6", "port": 5051}
/dashfabric/cluster/pods/fm-3 → {"role": "read",    "address": "10.0.1.7", "port": 5051}
/dashfabric/cluster/pods/fm-4 → {"role": "read",    "address": "10.0.1.8", "port": 5051}
```

Weaver discovers: `[fm-1 (primary), fm-2 (read), fm-3 (read), fm-4 (read)]`

---

### **Step 2: Health Checks**

Weaver periodically checks each replica:

```
T=0s:   GET /api/v1/health → fm-1 ✓ 200 OK   (HEALTHY)
        GET /api/v1/health → fm-2 ✓ 200 OK   (HEALTHY)
        GET /api/v1/health → fm-3 ✓ 200 OK   (HEALTHY)
        GET /api/v1/health → fm-4 ✓ 200 OK   (HEALTHY)

T=10s:  GET /api/v1/health → fm-1 ✓ 200 OK   (HEALTHY)
        GET /api/v1/health → fm-2 ✗ 500 ERR   (failure #1, still HEALTHY)
        GET /api/v1/health → fm-3 ✓ 200 OK   (HEALTHY)
        GET /api/v1/health → fm-4 ✓ 200 OK   (HEALTHY)

T=20s:  GET /api/v1/health → fm-1 ✓ 200 OK   (HEALTHY)
        GET /api/v1/health → fm-2 ✗ 500 ERR   (failure #2, still HEALTHY)
        GET /api/v1/health → fm-3 ✓ 200 OK   (HEALTHY)
        GET /api/v1/health → fm-4 ✓ 200 OK   (HEALTHY)

T=30s:  GET /api/v1/health → fm-1 ✓ 200 OK   (HEALTHY)
        GET /api/v1/health → fm-2 ✗ 500 ERR   (failure #3 → UNHEALTHY!)
        GET /api/v1/health → fm-3 ✓ 200 OK   (HEALTHY)
        GET /api/v1/health → fm-4 ✓ 200 OK   (HEALTHY)

Available replicas for routing: [fm-1, fm-3, fm-4]
```

---

### **Step 3: Primary-Aware Routing**

Client sends requests:

```
REQUEST 1: Register replica (WRITE)
  Method: FM.Broker/Register
  → Weaver identifies: WRITE operation
  → Load Balancer selects: PRIMARY (fm-1 only)
  → Sends to: fm-1
  → Response: Success (registered)

REQUEST 2: Query service (READ)
  Method: FM.Broker/Query
  → Weaver identifies: READ operation
  → Load Balancer selects: Least-connections from [fm-1, fm-3, fm-4]
    fm-1: 5 active connections
    fm-3: 2 active connections ← SELECTED (least connections)
    fm-4: 3 active connections
  → Sends to: fm-3
  → Response: Success

REQUEST 3: Subscribe (READ, bidirectional stream)
  Method: FM.Broker/Subscribe
  → Weaver identifies: READ operation
  → Load Balancer selects: Least-connections
    fm-1: 5 active connections
    fm-4: 2 active connections ← SELECTED
    fm-3: 6 active connections
  → Sends to: fm-4
  → Response: Subscription opened (streaming)
```

**Key Difference from CB (Peer-Equivalent):**
- CB routes ALL requests to ANY replica (uniform distribution)
- FM routes WRITES only to PRIMARY, READS to all healthy READ replicas (asymmetric)

---

### **Step 4: Circuit Breaker**

If fm-1 (PRIMARY) fails:

```
Failure Timeline:
  Failure #1: Register request fails (still try PRIMARY)
  Failure #2: Register request fails (still try PRIMARY)
  Failure #3: Register request fails (still try PRIMARY)
  Failure #4: Register request fails (still try PRIMARY)
  Failure #5: Circuit Breaker OPENS → fm-1 marked UNHEALTHY
  
New Behavior:
  Query request → Routes to fm-3 or fm-4 (still read available)
  Register request → FAILS IMMEDIATELY with "service unavailable"
           (Client knows PRIMARY is down; can retry later)
  
After 30s timeout:
  Weaver tries 1 request to fm-1 (HALF_OPEN state)
    → If succeeds: CLOSED (recovered!)
    → If fails: OPEN (still broken)
```

---

### **Step 5: Retry with Exponential Backoff**

Register request hits temporary network error:

```
Attempt 1: Register → Timeout (network hiccup)
  Wait 10ms
Attempt 2: Register → Timeout (still hiccupping)
  Wait 20ms
Attempt 3: Register → Success! (network recovered)
  
Total time: 30ms (if network recovers)
Without retry: Request would fail immediately
With retry: Request succeeds
```

---

### **Step 6: Observability**

After handling 10,000 requests:

```
METRICS:
fm_gw_requests_total{replica="fm-1"} = 1500  (writes only)
fm_gw_requests_total{replica="fm-3"} = 4200  (reads, balanced)
fm_gw_requests_total{replica="fm-4"} = 4300  (reads, balanced)

fm_gw_request_duration_p99{replica="fm-1"} = 2.5ms
fm_gw_request_duration_p99{replica="fm-3"} = 1.8ms
fm_gw_request_duration_p99{replica="fm-4"} = 1.7ms

fm_gw_circuit_breaker_state{replica="fm-1"} = 0 (CLOSED)
fm_gw_circuit_breaker_state{replica="fm-3"} = 0 (CLOSED)

TRACES:
[Trace ID: abc123def456]
├─ Pod discovery (1ms)
├─ Load balancing (0.2ms)
├─ Circuit breaker check (0.05ms)
├─ Connect to fm-3 (5ms)
├─ Send request (0.5ms)
├─ Wait for response (1.2ms)
└─ Total: 8ms

LOGS:
{
  "timestamp": "2026-06-15T10:30:45Z",
  "level": "DEBUG",
  "event": "request_routed",
  "operation": "Register",
  "routing_decision": "write→fm-1",
  "replica": "fm-1",
  "duration_ms": 2.1
}
```

---

## WHY: Decision Justification & Trade-Offs

### **Why Primary-Aware Routing?**

**Benefit: Consistency**
- Writes always go to one source (PRIMARY)
- Eliminates write conflicts and split-brain scenarios
- Simpler consistency model than eventual consistency

**Cost: Limited write throughput**
- PRIMARY is bottleneck (can't scale writes)
- Must have backup PRIMARY failover plan

**When to use:**
- Systems that prioritize consistency over throughput
- Metadata services, configurations (read-heavy, few writes)
- FM: ~1000 registrations/sec, ~100k queries/sec (read-heavy)

---

### **Why Circuit Breaker?**

**Benefit: Fail-fast**
- Don't waste time sending to dead replicas
- Reduce tail latency
- Protect replicas from overload during recovery

**Cost: Temporary unavailability**
- If PRIMARY circuit opens, writes are rejected
- Clients must retry and handle gracefully

**When to use:**
- High-throughput systems (prevents cascading failures)
- Systems with replicas that can fail independently

---

### **Why Exponential Backoff?**

**Benefit: Better recovery**
- Initial backoff short (10ms) for quick recovery
- Max backoff long (5s) to not hammer dead replicas
- Exponential growth (10ms → 20ms → 40ms...) balances latency and load

**Cost: Slightly longer latency on transient errors**
- But protects system stability

---

### **Why Rate Limiting?**

**Benefit: Fairness**
- Prevents one client from hogging gateway resources
- Ensures all clients get their share of throughput

**Cost: Some requests rejected**
- Clients must handle 429 (rate limited) responses gracefully

**Dimensions:**
- Global: Total 100k req/s (gateway capacity)
- Per-client: 10k req/s per client (fairness)
- Per-IP: 5k req/s per IP (DDoS protection)

---

## Performance Characteristics

| Metric | Typical | Notes |
|--------|---------|-------|
| Routing latency (p99) | <1ms | Pure memory operation |
| Primary write throughput | 10k req/s | Limited by PRIMARY replica |
| Read throughput | 100k req/s | Distributed across read replicas |
| Failover time | 5-30s | 3 health checks × 10s interval |
| Memory per pod | 50MB | Plus request buffers |

---

## Common Configurations for FM

### **High Read, Few Writes (Typical FM)**

```yaml
load_balancers:
  - type: "primary_aware"

health:
  interval: 10s
  consecutive_failures: 3

reliability:
  circuit_breaker:
    timeout: 30s
  retry:
    max_attempts: 3
```

### **High Write Requirements**

```yaml
# Can't improve write throughput with Weaver alone;
# Must scale PRIMARY replica (vertical scaling)
# or implement PRIMARY failover to another replica
```

### **Development/Testing**

```yaml
health:
  interval: 5s                    # Faster detection
  consecutive_failures: 2         # Lower threshold

reliability:
  retry:
    initial_backoff: 50ms         # Faster retries
    max_backoff: 1s
```

---

## Deployment Checklist for FM

```
Before Production:
□ PRIMARY replica is tagged with role=primary in etcd
□ READ replicas are tagged with role=read in etcd
□ Health check endpoint returns 200 for all replicas
□ Rate limits configured for your expected traffic
□ Circuit breaker timeout configured (30s default)
□ Retry strategy configured (3 attempts)
□ Monitoring (Prometheus, Jaeger) is set up
□ Alerts configured for circuit breaker state changes
□ Configuration is tested with chaos (kill a replica)
□ Graceful shutdown procedure documented
```

---

## Next Steps

- **See CB Scenario** → [21-cb-peer-equivalent.md](./21-cb-peer-equivalent.md)
- **Configure Weaver** → [30-configuration-reference.md](../REFERENCE/30-configuration-reference.md)
- **Load Balancing Reference** → [31-load-balancing-strategies.md](../REFERENCE/31-load-balancing-strategies.md)
- **Troubleshooting** → [54-troubleshooting.md](../OPERATIONS/54-troubleshooting.md)

---

**Navigation:**
- [← Previous](../QUICK_START/13-verify-deployment.md)
- [Index](../INDEX.md)
- [Next →](./21-cb-peer-equivalent.md)
