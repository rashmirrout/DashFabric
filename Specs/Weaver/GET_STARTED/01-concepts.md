# Weaver: Core Concepts

> **Read Time:** 15 minutes  
> **Audience:** Everyone  
> **Previous:** [00-introduction.md](./00-introduction.md) | **Next:** [02-architecture-overview.md](./02-architecture-overview.md)

---

## Overview

This document introduces the core concepts used throughout Weaver documentation. Think of this as a quick glossary that will help you understand the rest of the documentation.

---

## Core Concepts

### **Gateway**
A gateway is a proxy server that sits between clients and backend replicas. The gateway receives requests from clients, decides which replica to send them to, and relays the response back.

**In Weaver:**
- Single-purpose: route requests intelligently to distributed backends
- Configuration-driven: behavior defined entirely in YAML
- Stateless: can be horizontally scaled

**Example:**
```
Client → Weaver Gateway → Replica 1, 2, or 3
```

---

### **Replica**
A replica is an individual instance of a backend service (FM, CB, or custom system). The gateway routes requests to one of the available replicas.

**Characteristics:**
- Identified by: Name (e.g., "fm-1", "cb-2"), IP address, port
- Monitored by: Health checks (is it alive?)
- Selected by: Load balancer (which replica gets this request?)
- Status: HEALTHY, UNHEALTHY, or RECOVERING

**Example:**
```
Pod fm-1: 10.0.1.5:5051 (HEALTHY)
Pod fm-2: 10.0.1.6:5051 (HEALTHY)
Pod fm-3: 10.0.1.7:5051 (UNHEALTHY) ← Will be avoided
```

---

### **Pod Discovery**
Pod discovery is the process of finding all available replicas. Weaver automatically discovers replicas from various sources.

**Discovery Methods:**
- **T2 etcd** — DashFabric's preferred method; replicas registered in etcd
- **Consul** — HashiCorp's service mesh; replicas registered in Consul
- **Kubernetes API** — Kubernetes Service discovery
- **DNS** — Simple DNS-based discovery
- **Static List** — Manual list of replicas in config

**Example Discovery Flow:**
```
┌─────────────────┐
│ T2 etcd         │
│ /dashfabric/... │
└────────┬────────┘
         │ (Poll every 10s)
         ↓
┌──────────────────────────────┐
│ Weaver discovers replicas:   │
│ - fm-1: 10.0.1.5:5051        │
│ - fm-2: 10.0.1.6:5051        │
│ - fm-3: 10.0.1.7:5051        │
└──────────────────────────────┘
```

→ See [32-discovery-methods.md](../REFERENCE/32-discovery-methods.md) for details

---

### **Health Check**
A health check is a periodic ping to a replica to verify it's alive and healthy.

**Health Check Types:**
- **HTTP** — GET request to `/health` endpoint; expect 200 status
- **gRPC** — Call `Health.Check()` service
- **TCP** — Connect to port; if successful, replica is healthy
- **Custom** — User-defined health check logic

**Health Check Flow:**
```
Every 10 seconds:

Weaver → HTTP GET /api/v1/health → Replica
Replica → 200 OK
Weaver records: Replica is HEALTHY

If 3 consecutive failures:
Weaver marks: Replica is UNHEALTHY
Weaver avoids routing traffic there
```

**Importance:**
- Without health checks, Weaver might route to a dead replica (bad)
- With health checks, Weaver quickly detects and avoids dead replicas (good)

→ See [33-health-monitoring.md](../REFERENCE/33-health-monitoring.md) for details

---

### **Load Balancer**
A load balancer is an algorithm that decides which replica should receive the next request.

**Common Strategies:**

| Strategy | Decision Logic | Best For |
|----------|----------------|----------|
| **Least-Connections** | Route to replica with fewest active connections | Most workloads (default) |
| **Round-Robin** | Route to replicas in rotation (1 → 2 → 3 → 1...) | Stateless services |
| **Consistent Hash** | Hash(client_id) → same replica always | Session affinity |
| **Weighted** | Route more traffic to "stronger" replicas | Heterogeneous replicas |
| **Sticky** | Remember client→replica mapping for TTL | Session persistence |
| **Resource-Aware** | Route based on replica CPU/memory | Resource-constrained |
| **Random** | Pick randomly | Testing, load balancing |
| **Custom** | User-defined algorithm via plugin | Special needs |

**Example:**
```
3 replicas with load = [2, 5, 1]

Least-Connections: Route to replica 3 (load=1, least busy)
Round-Robin: Route to replica 2 (next in rotation)
Consistent Hash: Route to replica 1 (always same replica for this client)
```

→ See [31-load-balancing-strategies.md](../REFERENCE/31-load-balancing-strategies.md) for details

---

### **Circuit Breaker**
A circuit breaker is a state machine that protects against cascading failures.

**States:**

```
┌────────────┐
│   CLOSED   │ Normal operation
│ (healthy)  │ ↕ (if failures)
└────────────┘
       │
       ↓ (failures > threshold)
┌────────────┐
│    OPEN    │ Fail-fast; reject requests
│ (circuit   │ ↕ (after timeout)
│  broken)   │
└────────────┘
       │
       ↓ (after 30s timeout)
┌────────────────┐
│  HALF-OPEN     │ Try recovery
│  (testing)     │ ↕ (if request succeeds)
└────────────────┘
       │
       ├─→ (success) CLOSED
       └─→ (failure) OPEN
```

**Why It Matters:**
- **Without:** Weaver keeps sending requests to a dead replica, wasting time and resources
- **With:** Weaver quickly stops sending requests to a dead replica and tries again later

**Example:**
```
Replica fm-1 starts failing:
- Failure 1: CLOSED → continue
- Failure 2: CLOSED → continue
- Failure 3: CLOSED → continue
- Failure 4: CLOSED → continue
- Failure 5: CLOSED → OPEN (threshold reached)
- Now: All requests rejected immediately
- After 30s: Try 1 request (HALF-OPEN)
  - If OK: CLOSED (recovered!)
  - If fails: OPEN (stay broken, try again later)
```

→ See [34-reliability-patterns.md](../REFERENCE/34-reliability-patterns.md) for details

---

### **Retry with Exponential Backoff**
Retry is the automatic re-attempt of a failed request. Exponential backoff is the strategy of increasing wait times between retries.

**Why Retries Matter:**
- Some failures are transient (brief network hiccup)
- Retrying immediately often succeeds
- But retrying too aggressively can amplify failures

**Example Timeline:**
```
Attempt 1: Request → Timeout (10ms backoff)
Attempt 2: Request → Timeout (20ms backoff)
Attempt 3: Request → Timeout (40ms backoff)
Attempt 4: Request → Timeout (80ms backoff)
Attempt 5: Request → Success! (recovered)
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

→ See [34-reliability-patterns.md](../REFERENCE/34-reliability-patterns.md) for details

---

### **Request Timeout**
A timeout is the maximum time Weaver will wait for a response from a replica before giving up.

**Why Timeouts Matter:**
- Without timeouts: A slow replica can hang Weaver forever (bad)
- With timeouts: Slow requests fail fast and can be retried (good)

**Types of Timeouts:**
- **Connect Timeout** — Time to establish connection (e.g., 5s)
- **Per-Replica Timeout** — Time per replica (e.g., 25s)
- **Global Timeout** — Time for entire request (e.g., 30s)

**Example:**
```
Weaver sends request to fm-1
  5s for connection: ✓ OK
  20s for response: ✗ Timeout!
Weaver marks request as failed
Weaver retries on fm-2 (up to max_attempts)
```

→ See [34-reliability-patterns.md](../REFERENCE/34-reliability-patterns.md) for details

---

### **Request Queue**
A request queue buffers incoming requests when replicas are slow or busy.

**Why Queuing Matters:**
- Without queuing: Requests are rejected immediately if all replicas are busy (bad for UX)
- With queuing: Requests wait in a queue for a replica to become available (good for UX, under load)
- **But:** If queue fills up, requests are still rejected with a clear backpressure signal

**Configuration:**
```yaml
queuing:
  enabled: true
  per_replica_depth: 1000  # Max 1000 queued requests per replica
```

---

### **Rate Limiting**
Rate limiting controls the number of requests allowed per unit time.

**Dimensions:**
- **Global** — Total requests per second across all clients
- **Per-Client** — Requests per second for a specific client
- **Per-IP** — Requests per second from a specific IP address
- **Per-Tenant** — Requests per second for a specific tenant (multi-tenant)

**Example:**
```
Global: 100,000 req/s max
Per-client: 10,000 req/s max
Per-IP: 5,000 req/s max

Client A sends 12,000 req/s → Rejected (exceeds per-client limit)
Client B sends 8,000 req/s → Accepted (within limits)
```

→ See [37-rate-limiting.md](../REFERENCE/37-rate-limiting.md) for details

---

### **Observability**
Observability is the ability to understand system behavior through metrics, traces, and logs.

**Three Pillars:**

1. **Metrics** — Numerical measurements (e.g., "100 requests/sec", "5ms latency")
   - Provider: Prometheus
   - Collection interval: Every 30 seconds
   - Queries: Grafana dashboards, alert rules

2. **Traces** — Request flow through the system (e.g., "request from A → Weaver → replica B → response")
   - Provider: Jaeger (via OpenTelemetry)
   - Sampled: 10% of requests (configurable)
   - Visualization: Jaeger UI

3. **Logs** — Detailed events (e.g., "Replica fm-1 marked UNHEALTHY", "Circuit breaker opened")
   - Format: JSON (structured)
   - Levels: DEBUG, INFO, WARN, ERROR
   - Async: Written asynchronously for performance

**Example Metrics:**
```
fm_gw_replica_status{replica="fm-1"} = 1  (healthy)
fm_gw_replica_status{replica="fm-2"} = 1  (healthy)
fm_gw_replica_status{replica="fm-3"} = 0  (unhealthy)

fm_gw_requests_total{replica="fm-1"} = 1000000
fm_gw_request_duration_p99{replica="fm-1"} = 2.5ms
```

→ See [35-observability.md](../REFERENCE/35-observability.md) for details

---

### **Authentication & Authorization**

**Authentication (Who are you?):**
- Bearer token: `Authorization: Bearer <token>`
- API key: `X-API-Key: <key>`
- JWT: JSON Web Token with claims
- mTLS: Mutual TLS with client certificates

**Authorization (What can you do?):**
- RBAC: Role-Based Access Control (e.g., "admin", "viewer", "operator")
- ABAC: Attribute-Based Access Control (e.g., by tenant, region, service)

**Example:**
```
Client sends: Authorization: Bearer eyJhbGc...
Weaver validates: Is token valid?
Weaver checks RBAC: Does this token have "write" role?
Weaver allows/denies: Based on role and resource
```

→ See [36-security.md](../REFERENCE/36-security.md) for details

---

### **Primary vs. Peer-Equivalent**

**Primary Topology (FM):**
- One PRIMARY replica handles writes (registrations)
- Many READ replicas handle reads (queries)
- Gateway must route differently based on request type

**Peer-Equivalent Topology (CB):**
- All replicas are equal
- Any replica can handle any request
- Gateway routes uniformly to all replicas

**Configuration Example:**
```yaml
# FM: Primary-aware
gateway_mode: "primary_aware"
load_balancer: "primary_aware"

# CB: Peer-equivalent
gateway_mode: "peer_equivalent"
load_balancer: "least_connections"
```

→ See [20-fm-primary-aware.md](../SCENARIOS/20-fm-primary-aware.md) and [21-cb-peer-equivalent.md](../SCENARIOS/21-cb-peer-equivalent.md)

---

### **gRPC vs. HTTP/REST**

**gRPC:**
- Binary protocol (efficient)
- Bidirectional streaming (client and server can send messages independently)
- Lower latency, higher throughput
- Used for FM↔Weaver and CB↔Weaver bidirectional subscriptions

**HTTP/REST:**
- Text protocol (human-readable)
- Request-response only (client asks, server responds)
- Simpler for observability clients
- Used for CB observability: topics listing, metrics queries

**In Weaver:**
- **Port 5051:** gRPC listener (for backend clients: FM, CB)
- **Port 8080:** HTTP listener (for observability: UI, API clients)
- **Port 9090:** Metrics listener (Prometheus scraping)

---

## How These Concepts Work Together

```
Weaver receives request from client

1. Pod Discovery finds replicas: [fm-1, fm-2, fm-3]

2. Health Checks verify: fm-1(✓), fm-2(✓), fm-3(✗)

3. Load Balancer selects: fm-1 (least connections)

4. Request Router sends request to fm-1

5. Circuit Breaker checks: Is fm-1 still CLOSED? Yes → send

6. Request Timeout: If takes >25s → fail

7. If fails → Retry with exponential backoff

8. If succeeds → Metrics: +1 request, latency = 2ms

9. If succeeds → Trace: Log request flow in Jaeger

10. Observability: Update Prometheus metrics

11. Return response to client
```

---

## Key Terminology Quick Reference

| Term | Definition |
|------|-----------|
| **Gateway** | Proxy server routing requests to replicas |
| **Replica** | Individual backend instance (fm-1, fm-2, etc.) |
| **Pod Discovery** | Finding available replicas (etcd, Consul, K8s, DNS, static) |
| **Health Check** | Periodic ping to verify replica is alive |
| **Load Balancer** | Algorithm selecting which replica gets next request |
| **Circuit Breaker** | State machine protecting against cascading failures |
| **Retry** | Re-attempt of failed request |
| **Timeout** | Max time to wait for response |
| **Queue** | Buffer for incoming requests |
| **Rate Limiting** | Control requests per unit time |
| **Observability** | Metrics, traces, logs for understanding system |
| **Authentication** | Verify who is making the request |
| **Authorization** | Verify what the requester can do |
| **Primary** | Replica handling writes (FM-like) |
| **Peer** | Equal replicas all handling requests (CB-like) |

---

## Glossary

For a complete glossary of 100+ terms, see [62-glossary.md](../GUIDES/62-glossary.md).

---

## Next Steps

Ready to go deeper?

- **Understand Architecture** → [02-architecture-overview.md](./02-architecture-overview.md)
- **Deploy Weaver** → [10-kubernetes.md](../QUICK_START/10-kubernetes.md)
- **See Real Scenarios** → [20-fm-primary-aware.md](../SCENARIOS/20-fm-primary-aware.md)
- **Explore All Concepts** → [62-glossary.md](../GUIDES/62-glossary.md)

---

**Navigation:**
- [← Previous](./00-introduction.md)
- [Index](../INDEX.md)
- [Next →](./02-architecture-overview.md)
