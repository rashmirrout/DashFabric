# Weaver: Architecture Overview

> **Read Time:** 20 minutes  
> **Audience:** Architects, Engineers, Leads  
> **Previous:** [01-concepts.md](./01-concepts.md) | **Next:** [QUICK_START/](../QUICK_START/)

---

## System Architecture at 10,000 Feet

Weaver is organized as **7 layers**, each with a specific responsibility:

```
┌──────────────────────────────────────────────────────────┐
│  Layer 7: PROTOCOL HANDLING (gRPC, HTTP/REST)           │
├──────────────────────────────────────────────────────────┤
│  Layer 6: OBSERVABILITY (Metrics, Tracing, Logging)      │
├──────────────────────────────────────────────────────────┤
│  Layer 5: RELIABILITY (Circuit Breaker, Retry, Timeout)  │
├──────────────────────────────────────────────────────────┤
│  Layer 4: REQUEST ROUTING (Route by rules + strategy)    │
├──────────────────────────────────────────────────────────┤
│  Layer 3: LOAD BALANCING (Select replica: 8 strategies)  │
├──────────────────────────────────────────────────────────┤
│  Layer 2: HEALTH MONITORING (Continuous replica checks)  │
├──────────────────────────────────────────────────────────┤
│  Layer 1: POD DISCOVERY (Find replicas in etcd/Consul)   │
└──────────────────────────────────────────────────────────┘
                          ↓
           ┌──────────────┼──────────────┐
           ↓              ↓              ↓
      FM Cluster    CB Cluster    Future Systems
```

---

## Layer 1: Pod Discovery

**Responsibility:** Find all available backend replicas.

**How It Works:**
1. Weaver connects to a discovery source (etcd, Consul, K8s API, DNS, or static config)
2. Queries for replicas matching a pattern (e.g., `/dashfabric/cluster/pods/fm-*`)
3. Polls every 10 seconds for changes
4. Maintains a live list of: `{replica_name, IP, port, metadata}`

**Discovery Methods (Pluggable):**
- **T2 etcd** — DashFabric standard; replicas registered in etcd with keys like `/dashfabric/cluster/pods/fm-1`
- **Consul** — HashiCorp's service discovery; replicas registered as services
- **Kubernetes API** — Native K8s Service discovery via `endpoints` resource
- **DNS** — Simple DNS-based discovery of replicas
- **Static List** — Manual list of replica IPs in config file

**Example Discovery:**
```
Weaver polls etcd: GET /dashfabric/cluster/pods/fm-*
Returns:
  - fm-1: 10.0.1.5:5051
  - fm-2: 10.0.1.6:5051
  - fm-3: 10.0.1.7:5051

(Repeat every 10 seconds)
```

→ For complete details, see [32-discovery-methods.md](../REFERENCE/32-discovery-methods.md)

---

## Layer 2: Health Monitoring

**Responsibility:** Continuously check if each replica is alive and healthy.

**How It Works:**
1. For each discovered replica, Weaver sends periodic health checks
2. Checks can be HTTP GET, gRPC call, or TCP connection
3. If check succeeds → replica marked HEALTHY
4. If 3 consecutive checks fail → replica marked UNHEALTHY
5. Unhealthy replicas are avoided by the load balancer

**Health Check Timeline:**
```
T=0s:   fm-1 health check → 200 OK (HEALTHY)
T=10s:  fm-1 health check → 200 OK (HEALTHY)
T=20s:  fm-1 health check → 500 ERROR (failure #1, still HEALTHY)
T=30s:  fm-1 health check → 500 ERROR (failure #2, still HEALTHY)
T=40s:  fm-1 health check → 500 ERROR (failure #3, now UNHEALTHY)
T=50s:  fm-1 marked UNHEALTHY; load balancer avoids
T=60s:  fm-1 health check → 200 OK (1 success, marked RECOVERING)
T=70s:  fm-1 health check → 200 OK (2 successes, marked HEALTHY)
```

**Configuration:**
```yaml
health:
  type: "http"
  interval: 10s
  timeout: 5s
  config:
    endpoint: "/api/v1/health"
  consecutive_failures: 3
```

→ For complete details, see [33-health-monitoring.md](../REFERENCE/33-health-monitoring.md)

---

## Layer 3: Load Balancing

**Responsibility:** Select which replica should receive the next request.

**8 Load Balancing Strategies:**

1. **Least-Connections** (default) — Route to replica with fewest active connections
2. **Round-Robin** — Route to replicas in rotation
3. **Random** — Pick randomly
4. **Consistent Hash** — Same client always routes to same replica (session affinity)
5. **Weighted** — Route by replica weight/capacity
6. **Sticky** — Remember client→replica mapping for TTL
7. **Resource-Aware** — Route by replica resource metrics (CPU, memory)
8. **Custom** — User-defined algorithm via plugin

**Example Selection:**
```
3 replicas: fm-1 (load=5), fm-2 (load=2), fm-3 (load=8)

Least-Connections: Select fm-2 (load=2, least busy)
Round-Robin: Select next in rotation
Consistent Hash: Hash(client_id) → always same replica
Weighted: Select based on replica capacity
```

**Configuration:**
```yaml
load_balancers:
  - name: "default"
    type: "least_connections"
    # OR: "round_robin", "random", "consistent_hash", etc.
```

→ For complete details, see [31-load-balancing-strategies.md](../REFERENCE/31-load-balancing-strategies.md)

---

## Layer 4: Request Routing

**Responsibility:** Execute the routing decision and send request to selected replica.

**How It Works:**
1. Load Balancer selects a replica
2. Request Router opens/reuses connection to that replica
3. Sends request (gRPC message or HTTP request)
4. Waits for response
5. Returns response to client

**Routing Logic:**
```
Request from client
    ↓
Is replica healthy? 
  Yes → Send request
  No → Try next replica
    ↓
Connect to replica
    ↓
Send request (gRPC or HTTP)
    ↓
Wait for response (with timeout)
    ↓
Return to client
```

---

## Layer 5: Reliability

**Responsibility:** Protect against cascading failures through circuit breakers, retries, and timeouts.

### **Circuit Breaker**

State machine with 3 states:

```
CLOSED (healthy)
  ├─ Normal operation
  └─ If failures > threshold → OPEN

OPEN (circuit broken)
  ├─ Reject requests immediately
  └─ After timeout → HALF_OPEN

HALF_OPEN (testing recovery)
  ├─ Allow 1 request
  ├─ If success → CLOSED (recovered)
  └─ If fails → OPEN (still broken)
```

**Configuration:**
```yaml
circuit_breaker:
  enabled: true
  failure_threshold: 5      # Open if 5 failures
  success_threshold: 2      # Close if 2 successes in HALF_OPEN
  timeout: 30s              # Try recovery after 30s
```

### **Retry with Exponential Backoff**

Automatically retry transient failures with increasing wait times:

```
Attempt 1: Send → Timeout
  Wait 10ms ↓
Attempt 2: Send → Timeout
  Wait 20ms ↓
Attempt 3: Send → Timeout
  Wait 40ms ↓
Attempt 4: Send → Timeout
  Wait 80ms ↓
Attempt 5: Send → Success!
```

**Configuration:**
```yaml
retry:
  enabled: true
  max_attempts: 3
  backoff_strategy: "exponential"
  initial_backoff: 10ms
  max_backoff: 5s
```

### **Timeout Management**

Control how long Weaver waits for responses:

```yaml
timeout:
  global: 30s              # Max time for entire request
  per_replica: 25s         # Max time per replica
  connect: 5s              # Max time to establish connection
```

### **Request Queuing**

Buffer incoming requests when replicas are busy:

```yaml
queuing:
  enabled: true
  per_replica_depth: 1000  # Max 1000 queued per replica
```

**Queue Behavior:**
```
Replica fm-1 has 1000 queued requests
New request arrives
  → Queue is full
  → Request rejected with "backpressure" signal
  → Client can retry later or try different replica
```

→ For complete details, see [34-reliability-patterns.md](../REFERENCE/34-reliability-patterns.md)

---

## Layer 6: Observability

**Responsibility:** Provide visibility into gateway behavior through metrics, traces, and logs.

### **Metrics (Prometheus)**

Numerical measurements scraped by Prometheus:

```
fm_gw_requests_total{replica="fm-1"} = 1,000,000
fm_gw_request_duration_p99{replica="fm-1"} = 2.5ms
fm_gw_replica_status{replica="fm-1"} = 1 (healthy)
fm_gw_circuit_breaker_state{replica="fm-1"} = 0 (CLOSED)
fm_gw_queue_depth{replica="fm-1"} = 5
```

### **Traces (OpenTelemetry + Jaeger)**

Request flow through the system:

```
Request from client
  ├─ Trace ID: abc123def456
  ├─ Span: Pod discovery (1ms)
  ├─ Span: Load balancing (0.1ms)
  ├─ Span: Connect to replica (5ms)
  ├─ Span: Send request (0.5ms)
  ├─ Span: Wait for response (2ms)
  └─ Span: Return to client (0.1ms)
  
Total: 8.7ms (visualized in Jaeger UI)
```

**Sampling:** By default, 10% of requests are traced (configurable)

### **Logging (Structured JSON)**

Detailed events for debugging:

```json
{
  "timestamp": "2026-06-15T10:30:45Z",
  "level": "WARN",
  "event": "replica_marked_unhealthy",
  "replica": "fm-1",
  "consecutive_failures": 3,
  "health_check_type": "http"
}
```

→ For complete details, see [35-observability.md](../REFERENCE/35-observability.md)

---

## Layer 7: Protocol Handling

**Responsibility:** Handle different protocols: gRPC, HTTP, REST.

### **gRPC Support**

- Bidirectional streaming for real-time subscriptions
- Binary protocol (efficient)
- Lower latency, higher throughput
- Used by FM and CB for bidirectional communication

**Example gRPC Flows:**
```
Client Subscribe request (gRPC)
  → Weaver routes to replica
  → Replica streams updates back
  → Client receives updates in real-time
  → (Connection stays open)
```

### **HTTP/REST Support**

- Request-response only
- Human-readable
- Used for observability and REST clients

**Example HTTP Endpoints:**
```
GET /debug/replicas
  Returns: List of replicas + status
  
GET /metrics
  Returns: Prometheus metrics
  
POST /api/v1/operation
  Performs operation
```

---

## Configuration-Driven Architecture

**Key Design Principle:** All differentiation between FM, CB, and future systems is expressed in configuration (YAML), not code.

**FM Configuration:**
```yaml
gateway_mode: "primary_aware"
load_balancer: "primary_aware"  # Route writes to PRIMARY only
discovery_type: "t2_etcd"
key_pattern: "/dashfabric/cluster/pods/fm-*"
```

**CB Configuration:**
```yaml
gateway_mode: "peer_equivalent"
load_balancer: "least_connections"  # Route uniformly
discovery_type: "t2_etcd"
key_pattern: "/dashfabric/cluster/pods/cb-*"
```

**Same binary, different configs → Different behavior!**

→ For complete configuration reference, see [30-configuration-reference.md](../REFERENCE/30-configuration-reference.md)

---

## Extensibility Model

All major components are pluggable:

| Component | Built-in Options | Extensible? |
|-----------|-----------------|-------------|
| **Pod Discovery** | etcd, Consul, K8s, DNS, static | ✅ Yes |
| **Health Checker** | HTTP, gRPC, TCP, custom | ✅ Yes |
| **Load Balancer** | 8 strategies + custom | ✅ Yes |
| **Authenticator** | Bearer, API key, JWT, mTLS | ✅ Yes |
| **Rate Limiter** | Token bucket + custom | ✅ Yes |
| **Metrics Provider** | Prometheus + custom | ✅ Yes |

**To extend Weaver:**
1. Implement the plugin interface
2. Configure Weaver to use your plugin
3. No code changes to Weaver needed

→ For plugin development, see [71-plugin-development.md](../CONTRIBUTE/71-plugin-development.md)

---

## Data Flow: End-to-End Example

**Scenario:** FM client sends Subscribe request

```
1. CLIENT sends gRPC Subscribe request to Weaver
   Weaver receives request on port 5051

2. DISCOVERY: Is fm-1 still in etcd? Yes. Still fm-2, fm-3? Yes.
   Replicas: [fm-1 (healthy), fm-2 (healthy), fm-3 (unhealthy)]

3. HEALTH CHECK: fm-3 marked unhealthy 5 seconds ago. Skip.
   Available: [fm-1, fm-2]

4. LOAD BALANCER: Least-connections → fm-1 has 5 active, fm-2 has 2
   Select: fm-2

5. REQUEST ROUTER: Connect to fm-2:5051 (connection pooling reuses existing)
   Send: gRPC Subscribe message

6. CIRCUIT BREAKER: fm-2 is CLOSED (healthy). Proceed.

7. RESPONSE: fm-2 responds with subscription updates
   Weaver relays to client
   (Connection stays open for streaming)

8. METRICS: Log to Prometheus
   fm_gw_requests_total{replica="fm-2"}++
   fm_gw_request_duration_p99{replica="fm-2"} = 1.2ms

9. TRACE: Send span to Jaeger (if in 10% sample)
   Trace ID: abc123, Span: routing took 1.2ms

10. LOG: (If DEBUG level enabled)
    {
      "timestamp": "2026-06-15T10:30:45Z",
      "level": "DEBUG",
      "event": "request_routed",
      "replica": "fm-2",
      "duration_ms": 1.2
    }
```

---

## Performance Characteristics

| Metric | Target | How Achieved |
|--------|--------|-------------|
| Routing latency (p99) | <1ms | Direct memory lookups (no DB queries) |
| Throughput | 100k req/s | Single-threaded load balancer + connection pooling |
| Health check latency | <100ms | Parallel health checks, <1s interval |
| Memory footprint | <50MB | Efficient data structures, minimal allocations |
| Config reload | <1s | Zero-restart hot reload |
| Failover detection | <5s | 3 failed health checks × 10s interval |

---

## Deployment Architecture (Example: Kubernetes)

```
┌──────────────────────────────────────────────────────┐
│ KUBERNETES CLUSTER                                   │
├──────────────────────────────────────────────────────┤
│                                                      │
│  Namespace: weaver                                   │
│  ├─ Deployment: weaver (3 replicas)                 │
│  │  ├─ Pod 1: weaver:1.0                            │
│  │  ├─ Pod 2: weaver:1.0                            │
│  │  └─ Pod 3: weaver:1.0                            │
│  │                                                   │
│  ├─ Service: weaver (ClusterIP)                     │
│  │  ├─ Port 5051 (gRPC)                             │
│  │  ├─ Port 8080 (HTTP)                             │
│  │  └─ Port 9090 (Metrics)                          │
│  │                                                   │
│  ├─ ConfigMap: weaver-config                        │
│  │  └─ weaver-config.yaml (mounted at /etc/weaver) │
│  │                                                   │
│  └─ ServiceMonitor (for Prometheus)                 │
│                                                      │
├──────────────────────────────────────────────────────┤
│ Clients: FM, CB, other services                     │
│ Connect to: weaver:5051 (gRPC) or weaver:8080 (HTTP)
└──────────────────────────────────────────────────────┘
```

→ For complete Kubernetes deployment, see [50-kubernetes-deployment.md](../OPERATIONS/50-kubernetes-deployment.md)

---

## Security Architecture

```
CLIENT REQUEST
    ↓
AUTHENTICATION
  ├─ Bearer token?
  ├─ API key?
  ├─ JWT?
  └─ mTLS certificate?
    ↓
AUTHORIZATION (RBAC)
  ├─ Does this token have "write" role?
  ├─ Or "read" role?
  └─ Or "admin" role?
    ↓
RATE LIMITING
  ├─ Global: <100k req/s?
  ├─ Per-client: <10k req/s?
  └─ Per-IP: <5k req/s?
    ↓
TLS ENCRYPTION
  ├─ Client → Weaver: TLS
  └─ Weaver → Replica: TLS (optional)
    ↓
REQUEST PROCEEDS
```

→ For complete security details, see [36-security.md](../REFERENCE/36-security.md)

---

## Summary

Weaver is a **7-layer gateway** that routes requests intelligently through:

1. **Pod Discovery** — Find replicas
2. **Health Monitoring** — Check if they're alive
3. **Load Balancing** — Choose which one
4. **Request Routing** — Send the request
5. **Reliability** — Handle failures
6. **Observability** — Understand what happened
7. **Protocol Handling** — Support gRPC and HTTP

All **pluggable** and **configuration-driven** for maximum flexibility.

---

## Next Steps

Choose your path:

- **Deploy Weaver** → [10-kubernetes.md](../QUICK_START/10-kubernetes.md)
- **See Real Scenarios** → [20-fm-primary-aware.md](../SCENARIOS/20-fm-primary-aware.md)
- **Configure Weaver** → [30-configuration-reference.md](../REFERENCE/30-configuration-reference.md)
- **Understand Design** → [40-hld.md](../DESIGN/40-hld.md)

---

**Navigation:**
- [← Previous](./01-concepts.md)
- [Index](../INDEX.md)
- [Next →](../QUICK_START/10-kubernetes.md)
