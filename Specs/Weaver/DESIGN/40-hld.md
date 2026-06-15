# Weaver: High-Level Design (HLD)

> **Read Time:** 25 minutes  
> **Previous:** [../OPERATIONS/57-version-matrix.md](../OPERATIONS/57-version-matrix.md) | **Next:** [41-lld.md](./41-lld.md)

---

## Architecture Overview

Weaver is a universal application gateway for DashFabric. It routes requests from clients to backend replicas, with built-in load balancing, reliability patterns, and observability.

---

## 7-Layer Architecture

```
Layer 7: Protocol Handling
  ↓ (gRPC/HTTP/Custom)
Layer 6: Observability
  ↓ (Metrics, Traces, Logs)
Layer 5: Reliability
  ↓ (Circuit Breaker, Retry, Timeout)
Layer 4: Rate Limiting
  ↓ (Token Bucket Algorithm)
Layer 3: Request Routing
  ↓ (Replica Selection)
Layer 2: Load Balancing
  ↓ (8 Strategies)
Layer 1: Pod Discovery
  ↓ (etcd, Consul, K8s, DNS, Static)
Layer 0: Backend Replicas
```

---

## Layer 0: Backend Replicas

**What:** Actual backend services (etcd nodes, Consul servers, etc.)

**Properties:**
- Multiple replicas (3+ in production)
- Each replica at `host:port`
- Health state: HEALTHY or UNHEALTHY
- Optional attributes: region, tier, CPU, memory

**Example:**
```
fm-primary-1: 10.0.1.10:5051 (HEALTHY)
fm-primary-2: 10.0.1.11:5051 (HEALTHY)
fm-primary-3: 10.0.1.12:5051 (HEALTHY)
```

---

## Layer 1: Pod Discovery

**Purpose:** Discover which replicas exist and where they are

**Discovery Methods:**
1. **etcd** (T2 DashFabric) - Read `/weaver/replicas` key
2. **Consul** - Query `catalog/services` API
3. **Kubernetes** - Watch Pod resources
4. **DNS** - Resolve SRV records
5. **Static** - Read from ConfigMap

**Data Flow:**
```
Discovery Service (etcd/Consul/K8s)
  ↓
Weaver Discovery Module (polls every 30s)
  ↓
In-memory Replica List
  ↓
Next Layer (Health Monitoring)
```

**Example Discovery Output:**
```json
{
  "replicas": [
    {"name": "fm-primary-1", "address": "10.0.1.10:5051"},
    {"name": "fm-primary-2", "address": "10.0.1.11:5051"},
    {"name": "fm-primary-3", "address": "10.0.1.12:5051"}
  ]
}
```

---

## Layer 2: Health Monitoring

**Purpose:** Determine which replicas are healthy

**Health Check Methods:**
1. **HTTP** - GET /health; expect 200
2. **gRPC** - Call grpc.health.v1.Health/Check
3. **TCP** - Connect and close

**State Machine:**
```
HEALTHY
  ↓ (3 consecutive failures)
UNHEALTHY
  ↓ (60s elapsed)
PANIC MODE (all replicas unhealthy)
  ↓ (1 success)
HEALTHY
```

**Data Flow:**
```
Health Check Goroutines (1 per replica)
  ↓ (every 5s)
Health Status Cache
  ↓
Next Layer (Load Balancing)
```

---

## Layer 3: Load Balancing

**Purpose:** Select which replica to send request to

**Algorithms:**
1. **Round-Robin** - Cycle through replicas
2. **Least-Connections** - Pick replica with fewest active connections
3. **Random** - Pick random replica
4. **Consistent Hash** - Hash on request property (for sticky sessions)
5. **Weighted** - Send more traffic to some replicas
6. **Sticky** - Same client → same replica
7. **Resource-Aware** - Consider CPU/memory of replicas
8. **Custom** - User-defined algorithm

**Selection Process:**
```
1. Get list of HEALTHY replicas
2. Apply load balancer algorithm
3. Return selected replica: {name, address}
```

**Example:**
```
Input: Request from client
Output: Selected replica = fm-primary-1 (10.0.1.10:5051)
```

---

## Layer 4: Request Routing

**Purpose:** Connect to selected replica and forward request

**Steps:**
1. Look up replica address from Layer 3
2. Create gRPC connection (or reuse pooled connection)
3. Forward request to replica
4. Wait for response
5. Return response to client

**Connection Management:**
- Connection pool per replica (reuse connections)
- Keep-alive pings (every 30s)
- Graceful close on error

---

## Layer 5: Rate Limiting

**Purpose:** Limit request rate to prevent overload

**Algorithm:** Token Bucket

**Dimensions:**
1. **Global** - Total requests per second (gateway-wide)
2. **Per-Client** - Max requests per client
3. **Per-IP** - Max requests from IP address
4. **Per-Tenant** - Max requests per tenant

**Example:**
```yaml
rate_limiting:
  global:
    requests_per_second: 100000
  per_client:
    requests_per_second: 10000
  per_tenant:
    requests_per_second: 50000
```

**Behavior:**
- Request comes in → Check rate limit
- ✅ Within limit → Allow request (forward to layer 4)
- ❌ Over limit → Return 429 (Too Many Requests)

---

## Layer 6: Reliability

**Purpose:** Handle failures gracefully

**Components:**

### 6.1 Circuit Breaker
```
CLOSED (healthy) → 5 failures → OPEN (failing)
                                  ↓ 60s
                            HALF_OPEN (testing)
                                  ↓ success/failure
                     CLOSED (recovered) / OPEN (still failing)
```

### 6.2 Retry with Exponential Backoff
```
1st attempt: fails
  ↓ wait 100ms
2nd attempt: fails
  ↓ wait 200ms
3rd attempt: fails
  ↓ wait 400ms
3rd attempt: give up, return error
```

### 6.3 Timeout
```
Connect timeout: 5s (max time to connect to replica)
Request timeout: 30s (max time to receive response)
Global timeout: 30s (max time for entire request)
```

**Example Response Path:**
```
Request → Try replica 1 → Circuit Breaker OPEN?
  → Retry #1 → Fail → Wait 100ms
  → Retry #2 → Fail → Wait 200ms
  → Retry #3 → Fail → Return error
```

---

## Layer 7: Observability

**Purpose:** Understand what's happening

**Components:**

### 7.1 Metrics (Prometheus)
```
fm_gw_requests_total         # Total requests
fm_gw_request_duration_p99   # Latency (99th percentile)
fm_gw_request_errors_total   # Failed requests
fm_gw_circuit_breaker_state  # Circuit breaker state
```

### 7.2 Tracing (Jaeger)
```
Request timeline:
  0ms: Discovery lookup
  2ms: Load balancer selection
  5ms: Connection establish
  8ms: Request sent
  25ms: Response received
  25ms: Total
```

### 7.3 Logging
```json
{
  "timestamp": "2026-06-15T10:30:45Z",
  "level": "INFO",
  "message": "Request forwarded",
  "replica": "fm-primary-1",
  "duration_ms": 25,
  "status": "success"
}
```

---

## Layer 8: Protocol Handling

**Purpose:** Support different protocols

**Supported:**
- gRPC (primary)
- HTTP (for health checks)
- Custom (via plugins)

**Flow:**
```
Client → Protocol Parser → Layer 1-7 → Replica Protocol
```

---

## Data Structures

### Gateway State

```go
type Gateway struct {
  Name string
  Listeners map[string]Listener
  Discovery Discovery
  HealthMonitor HealthMonitor
  LoadBalancer LoadBalancer
  RateLimiter RateLimiter
  CircuitBreaker CircuitBreaker
}
```

### Replica State

```go
type Replica struct {
  Name string
  Address string  // host:port
  Status ReplicaStatus  // HEALTHY, UNHEALTHY
  Load int  // active connections
  Attributes map[string]string  // region, tier, etc.
}
```

### Request State

```go
type RequestContext struct {
  ClientID string
  TenantID string
  ReplicaSelected *Replica
  AttemptCount int
  StartTime time.Time
}
```

---

## Concurrency Model

### Goroutines

```
Main Goroutine
  → Discovery Goroutine (watches for replica changes)
  → Health Check Goroutines (1 per replica)
  → Listener Goroutine (accepts incoming connections)
      → Request Handler Goroutine (per incoming request)
  → Metrics Goroutine (collects metrics)
  → Tracer Goroutine (sends traces)
```

### Synchronization

**Replica List Updates:**
```
Discovery Goroutine updates replica list
  → Lock mutex
  → Update internal list
  → Unlock mutex
  → Notify Load Balancer
```

**Thread Safety:**
- All shared state protected by mutex
- No locks in request path (copy-on-write for replica list)
- Atomic operations for counters

---

## Request Flow

```
1. Client connects (incoming TCP connection)
2. Weaver accepts connection (Layer 7: Protocol)
3. Weaver receives request
4. Rate limiter checks (Layer 5)
   → 429 if over limit
5. Load balancer selects replica (Layer 3)
6. Health check verifies replica healthy (Layer 2)
7. Circuit breaker checks (Layer 6)
   → Fail if OPEN
8. Connect to replica + send request (Layer 4)
9. Wait for response (Layer 4)
10. Handle failures: retry with backoff (Layer 6)
11. Collect metrics (Layer 7)
12. Return response to client
```

---

## Scaling Properties

### Horizontal Scaling

```
1 Weaver → Single point of failure
2 Weaver → High availability
3+ Weaver → Load balanced across multiple nodes
```

**Load Balancing Multiple Weaver:**
- Use external load balancer (nginx, HAProxy, cloud LB)
- Route requests round-robin or consistent hash

### Vertical Scaling

**Per Weaver:**
- 1 Weaver can handle: 100k+ requests/sec (depends on hardware)
- CPU bottleneck at: 80% utilization
- Memory bottleneck at: active connection limit

**Tuning:**
- Increase thread pool size → more parallelism
- Increase buffer sizes → fewer context switches
- Decrease health check frequency → less overhead

---

## Failure Modes

| Failure | Impact | Recovery |
|---------|--------|----------|
| Single replica down | Requests rerouted to others | Automatic (health check detects) |
| Multiple replicas down | Increased latency/errors | Circuit breaker protects backend |
| Network partition | All replicas unreachable | Panic mode; return errors |
| Weaver down | All requests fail | Clients must retry |
| etcd/Consul down | Can't discover new replicas | Uses cached list (stale) |

---

**Navigation:**
- [← Previous](../OPERATIONS/57-version-matrix.md)
- [Index](../INDEX.md)
- [Next →](./41-lld.md)
