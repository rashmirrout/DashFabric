# Weaver: CB Peer-Equivalent Scenario

> **Read Time:** 25 minutes  
> **Scenario:** ControllerBridge (CB) system with peer-equivalent replicas  
> **Audience:** Architects, Engineers, Operators  
> **Previous:** [20-fm-primary-aware.md](./20-fm-primary-aware.md) | **Next:** [22-custom-system.md](./22-custom-system.md)

---

## WHAT: Problem Statement

**CB System Topology:**
- **N PEER-EQUIVALENT replicas** — All replicas are equal; any can handle any request
- **Symmetric routing needed** — Weaver distributes requests uniformly across all healthy replicas
- **Bidirectional streaming** — Clients maintain persistent gRPC connections for publish/subscribe

**Key Difference from FM:**
- FM: PRIMARY (writes only) + READ replicas (reads only) = asymmetric
- CB: All replicas equal = symmetric

**Example Architecture:**

```
┌─────────────────────────────────────────────────┐
│    CB PEER-EQUIVALENT REPLICAS (all equal)      │
│                                                 │
│  cb-1:5051 ✓ PUBLISH ✓ SUBSCRIBE ✓ QUERY      │
│  cb-2:5051 ✓ PUBLISH ✓ SUBSCRIBE ✓ QUERY      │
│  cb-3:5051 ✓ PUBLISH ✓ SUBSCRIBE ✓ QUERY      │
│  cb-4:5051 ✓ PUBLISH ✓ SUBSCRIBE ✓ QUERY      │
│                                                 │
│  (Each replica replicates state from others)   │
│  (Eventual consistency model)                  │
└──────────────┬───────────────────────────────┘
               │ (ANY request → ANY replica)
               │
    ┌──────────┼──────────┐
    │          │          │
    ↓          ↓          ↓
 CLIENT A   CLIENT B   CLIENT C
 SUBSCRIBE  PUBLISH    SUBSCRIBE
  Events    Message     Events
  Stream    Acknowledgment (ack)
```

---

## HOW: Configuration Walkthrough

### **Configuration File: weaver-config-cb.yaml**

```yaml
########################################
# WEAVER CONFIGURATION FOR CB SYSTEM
########################################

gateway:
  name: "cb-gateway"
  mode: "peer_equivalent"  # ← KEY: All replicas equal

########################################
# DISCOVERY: Find CB replicas in etcd
########################################
discovery:
  type: "t2_etcd"
  poll_interval: 10s
  config:
    endpoint: "http://etcd-t2:2379"
    key_pattern: "/dashfabric/cluster/pods/cb-*"
    timeout: 5s

########################################
# HEALTH MONITORING: Check replicas alive
########################################
health:
  enabled: true
  type: "gRPC"          # ← Different from FM (HTTP)
  interval: 10s
  timeout: 5s
  config:
    service: "health.HealthService/Check"
  
  consecutive_failures: 3
  
  panic_mode:
    enabled: true
    threshold_percent: 50

########################################
# LOAD BALANCING: Distribute uniformly across all replicas
########################################
load_balancers:
  # Least-connections: distribute by active connection count
  - name: "default"
    type: "least_connections"
    config:
      # No special config needed; all replicas are equal

########################################
# LISTENERS: Accept gRPC and HTTP requests
########################################
listeners:
  grpc:
    enabled: true
    port: 5051
    max_connections: 10000
    # Bidirectional streaming settings
    stream_idle_timeout: 30m      # Keep streams alive for 30 min
    max_stream_age: 8h
  
  http:
    enabled: true
    port: 8080
    # REST API for observability
    max_connections: 10000
  
  metrics:
    enabled: true
    port: 9090
    path: "/metrics"

########################################
# RELIABILITY: Handle failures gracefully
########################################
reliability:
  circuit_breaker:
    enabled: true
    failure_threshold: 5
    success_threshold: 2
    timeout: 30s
  
  retry:
    enabled: true
    max_attempts: 3
    backoff_strategy: "exponential"
    initial_backoff: 10ms
    max_backoff: 5s
  
  queuing:
    enabled: true
    per_replica_depth: 5000      # Higher for CB (more concurrent streams)
  
  timeout:
    global: 30s
    per_replica: 25s
    connect: 5s

########################################
# RATE LIMITING: Control per-tenant throughput
########################################
rate_limiting:
  enabled: true
  
  global:
    enabled: true
    requests_per_second: 100000
  
  # Per-tenant rate limiting (CB supports multi-tenant)
  per_tenant:
    enabled: true
    requests_per_second: 50000
  
  per_client:
    enabled: true
    requests_per_second: 10000

########################################
# OBSERVABILITY: Metrics, tracing, logging
########################################
observability:
  metrics:
    enabled: true
    namespace: "cb_gw"
    port: 9090
  
  tracing:
    enabled: true
    provider: "jaeger"
    sample_rate: 0.1
    config:
      endpoint: "http://jaeger:6831"
  
  logging:
    enabled: true
    level: "INFO"
    format: "json"
    async: true

########################################
# REST API ENDPOINTS (for observability clients)
########################################
rest_api:
  enabled: true
  port: 8081
  
  endpoints:
    # List all topics in all replicas
    GET /api/v1/topics
    
    # List subscriptions on a topic
    GET /api/v1/topics/{topic}/subscriptions
    
    # Get replica status
    GET /api/v1/replicas
    
    # Get metrics
    GET /api/v1/metrics

########################################
# AUTHENTICATION & AUTHORIZATION
########################################
authentication:
  enabled: true
  method: "jwt"
  config:
    issuer: "https://auth.example.com"
    public_key_url: "https://auth.example.com/.well-known/jwks.json"
    cache_ttl: 1h

authorization:
  enabled: true
  rbac:
    roles:
      publisher:
        permissions: ["publish"]
      subscriber:
        permissions: ["subscribe", "query"]
      admin:
        permissions: ["publish", "subscribe", "query", "admin"]

########################################
# TLS ENCRYPTION
########################################
tls:
  enabled: true
  
  server:
    enabled: true
    cert_path: "/etc/weaver/certs/server.crt"
    key_path: "/etc/weaver/certs/server.key"
    min_version: "1.2"
  
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
# etcd contains CB replica registrations (all equal)
/dashfabric/cluster/pods/cb-1 → {"address": "10.1.1.5", "port": 5051}
/dashfabric/cluster/pods/cb-2 → {"address": "10.1.1.6", "port": 5051}
/dashfabric/cluster/pods/cb-3 → {"address": "10.1.1.7", "port": 5051}
/dashfabric/cluster/pods/cb-4 → {"address": "10.1.1.8", "port": 5051}

# No "role" field (unlike FM which has "primary" and "read")
# All replicas are identical
```

Weaver discovers: `[cb-1, cb-2, cb-3, cb-4]` (all equal)

---

### **Step 2: Health Checks (gRPC)**

Unlike FM (HTTP health checks), CB uses gRPC health checks:

```
T=0s:   gRPC Health.Check() → cb-1 ✓ OK   (HEALTHY)
        gRPC Health.Check() → cb-2 ✓ OK   (HEALTHY)
        gRPC Health.Check() → cb-3 ✓ OK   (HEALTHY)
        gRPC Health.Check() → cb-4 ✓ OK   (HEALTHY)

T=10s:  gRPC Health.Check() → cb-1 ✓ OK   (HEALTHY)
        gRPC Health.Check() → cb-2 ✗ FAIL  (failure #1)
        gRPC Health.Check() → cb-3 ✓ OK   (HEALTHY)
        gRPC Health.Check() → cb-4 ✓ OK   (HEALTHY)

T=30s:  (After 3 consecutive failures) cb-2 marked UNHEALTHY

Available replicas: [cb-1, cb-3, cb-4]
```

---

### **Step 3: Peer-Equivalent Routing**

Client sends requests (all requests go to any replica):

```
REQUEST 1: Publish Event
  Method: CB.EventBroker/Publish
  Weaver selects: Least-connections
    cb-1: 10 active streams
    cb-3: 8 active streams ← SELECTED
    cb-4: 9 active streams
  Sends to: cb-3
  Response: Event published (to all subscribers)

REQUEST 2: Subscribe to Topic
  Method: CB.EventBroker/Subscribe
  Weaver selects: Least-connections
    cb-1: 10 active streams
    cb-4: 9 active streams ← SELECTED
    cb-3: 12 active streams (just increased from REQUEST 1)
  Sends to: cb-4
  Response: Stream opened (receives events as they happen)
  (Stream stays open until client unsubscribes or disconnects)

REQUEST 3: Query Topic
  Method: CB.EventBroker/Query
  Weaver selects: Least-connections
    cb-1: 10 active streams ← SELECTED
    cb-3: 12 active streams
    cb-4: 11 active streams
  Sends to: cb-1
  Response: Query results
```

**Key Difference from FM:**
- FM: Write always to PRIMARY, read distributed to READ replicas
- CB: All requests distributed uniformly to all replicas

---

### **Step 4: Bidirectional Streaming**

CB uses bidirectional gRPC streams (client and server send messages independently):

```
CLIENT opens subscription stream:
  Open: Subscribe(topic="events")
  ├─ Client → Server: {"action": "subscribe", "topic": "events"}
  └─ Server → Client: {"status": "subscribed", "topic": "events"}
  
Events happen in CB cluster:
  ├─ Event 1 arrives
  ├─ Server → Client: {"event_id": "1", "data": {...}}
  ├─ Event 2 arrives
  └─ Server → Client: {"event_id": "2", "data": {...}}
  
Client publishes while subscribed:
  ├─ Client → Server: {"action": "publish", "data": {...}}
  └─ Server → Client: {"ack": true, "event_id": "3"}
  
Stream stays open (no request-response cycles)
  Client can receive events for hours if needed
  
Client unsubscribes:
  ├─ Client → Server: {"action": "unsubscribe"}
  └─ Server → Client: {"status": "unsubscribed"}
```

**Advantage over REST:**
- No polling (client waits for events from server)
- Lower latency (events pushed immediately)
- Single connection (more efficient)

---

### **Step 5: Consistency Model (Eventual)**

CB replicas eventually converge (not immediately consistent like FM):

```
Time T1: Client A publishes event to cb-1
  cb-1: event_id=100 ✓
  
Time T1+1ms: Client B queries topic on cb-2
  cb-2: event_id=99 ✗ (doesn't see event_id=100 yet)
  
Time T1+50ms: CB replicas replicate
  cb-1: event_id=100 ✓
  cb-2: event_id=100 ✓ (replicated from cb-1)
  cb-3: event_id=100 ✓
  cb-4: event_id=100 ✓

Eventual consistency: All replicas converge within 50ms
Strong consistency: Not guaranteed (by design)
```

**Trade-off:**
- ✓ Can scale writes (any replica can publish)
- ✗ Client may see stale data (before replication)
- ✓ Better for high-throughput event streams
- ✗ Not suitable for strong consistency requirements

---

### **Step 6: Load Distribution**

After 1 hour of traffic:

```
METRICS:
cb_gw_requests_total{replica="cb-1"} = 25000  (roughly equal)
cb_gw_requests_total{replica="cb-2"} = 24500  (roughly equal)
cb_gw_requests_total{replica="cb-3"} = 25200  (roughly equal)
cb_gw_requests_total{replica="cb-4"} = 25300  (roughly equal)

cb_gw_active_streams{replica="cb-1"} = 500
cb_gw_active_streams{replica="cb-2"} = 480
cb_gw_active_streams{replica="cb-3"} = 510
cb_gw_active_streams{replica="cb-4"} = 490

Distribution: Very uniform (within ±2%)
Least-connections LB ensures balanced streams
```

---

## WHY: Decision Justification & Trade-Offs

### **Why Peer-Equivalent?**

**Benefit: Horizontal Scalability**
- Add more replicas → more throughput (no PRIMARY bottleneck)
- CB can handle 10x requests by adding replicas
- FM cannot (PRIMARY is bottleneck)

**Cost: Complexity**
- Eventual consistency (clients may see stale data temporarily)
- Requires replication protocol (higher complexity)
- Conflict resolution needed (if same key written to different replicas)

**When to use:**
- High-throughput event streams (100k+ events/sec)
- Pub/Sub systems (eventual consistency acceptable)
- Systems where data is primarily additive (events, logs)

---

### **Why gRPC Health Checks?**

**Benefit: Application-level health**
- gRPC checks the actual CB service health
- HTTP checks only verify the health endpoint
- More accurate than HTTP for CB system

**Cost: Slightly higher latency**
- gRPC call is slower than HTTP GET
- But negligible (1-2ms difference)

---

### **Why Bidirectional Streaming?**

**Benefit: Efficient subscriptions**
- One connection per subscriber (not one per message)
- Server pushes events (not client polling)
- Lower latency, lower bandwidth

**Cost: Connection management**
- Must handle stream disconnections
- Must handle reconnections gracefully
- Firewall/proxy issues (some don't support bidirectional streams)

---

### **Why Least-Connections LB?**

**For Streams:**
- HTTP: Least-connections makes sense (short requests)
- gRPC streams: Least-connections also works
  - But need to count active streams, not just connection count
  - Works well if stream lifetimes vary

**Alternative: Consistent Hash**
```
Hash(client_id) → same replica always
Benefit: Better stream locality (don't reconnect if replica restarts)
Cost: Less balanced load if clients vary in request rate
```

---

## Performance Characteristics

| Metric | FM (Primary-Aware) | CB (Peer-Equivalent) |
|--------|-------------------|---------------------|
| Write throughput | 10k req/s | 100k req/s (scales) |
| Read throughput | 100k req/s | 100k req/s |
| Consistency | Strong | Eventual |
| Replica complexity | Low | High |
| Replication overhead | None | 10-20% |

---

## Common Configurations for CB

### **High-Throughput Event Stream**

```yaml
load_balancers:
  - type: "least_connections"

reliability:
  retry:
    max_attempts: 1          # Fewer retries (CB is resilient)
    initial_backoff: 1ms

listeners:
  grpc:
    stream_idle_timeout: 30m # Long-lived streams
```

### **Multi-Tenant CB**

```yaml
rate_limiting:
  per_tenant:
    enabled: true
    requests_per_second: 50000

authentication:
  method: "jwt"
  # JWT contains tenant_id claim
```

---

## Deployment Checklist for CB

```
Before Production:
□ All CB replicas have same replica count (no PRIMARY)
□ gRPC health check endpoint returns OK for all replicas
□ Replication latency is < 100ms (measure)
□ Rate limits configured per tenant
□ Circuit breaker timeout configured (30s default)
□ Bidirectional streams configured (30m timeout)
□ Monitoring (Prometheus, Jaeger) set up
□ Alerts for circuit breaker state changes
□ Chaos testing: kill a replica, verify clients reconnect
□ Test eventual consistency: publish, then query different replica
```

---

## Comparison: FM vs. CB

| Aspect | FM (Primary-Aware) | CB (Peer-Equivalent) |
|--------|-------------------|---------------------|
| **Topology** | Primary + Read | All Equal |
| **Write routing** | → PRIMARY only | → Any replica |
| **Read routing** | → Distributed | → Any replica |
| **Consistency** | Strong | Eventual |
| **Scale writes** | ✗ No (PRIMARY bottleneck) | ✓ Yes |
| **Replication** | None | Active (peer-to-peer) |
| **Best for** | Metadata, config | Event streams, pub/sub |
| **Example workload** | 1k writes/sec, 100k reads/sec | 100k events/sec |

---

## Next Steps

- **See Custom System Scenario** → [22-custom-system.md](./22-custom-system.md)
- **Configure Weaver** → [30-configuration-reference.md](../REFERENCE/30-configuration-reference.md)
- **Load Balancing Reference** → [31-load-balancing-strategies.md](../REFERENCE/31-load-balancing-strategies.md)
- **Design Document** → [40-hld.md](../DESIGN/40-hld.md)

---

**Navigation:**
- [← Previous](./20-fm-primary-aware.md)
- [Index](../INDEX.md)
- [Next →](./22-custom-system.md)
