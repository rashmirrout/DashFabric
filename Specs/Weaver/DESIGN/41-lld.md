# Weaver: Low-Level Design (LLD)

> **Read Time:** 30 minutes  
> **Previous:** [40-hld.md](./40-hld.md) | **Next:** [42-data-structures.md](./42-data-structures.md)

---

This document covers internal implementation details. See related docs for data structures ([42-data-structures.md](./42-data-structures.md)), algorithms ([43-algorithms.md](./43-algorithms.md)), and concurrency ([44-concurrency-model.md](./44-concurrency-model.md)).

---

## Gateway Lifecycle

### Initialization

```
1. Parse CLI flags
2. Load configuration (YAML)
3. Validate configuration
4. Initialize discovery (connect to etcd/Consul/K8s/DNS)
5. Initialize health monitor
6. Initialize load balancer
7. Initialize rate limiter
8. Initialize circuit breaker
9. Start listener (port 5051)
10. Start health check goroutines (1 per replica)
11. Start metrics server (port 9090)
12. Ready to accept requests
```

### Request Processing Pipeline

```
Request In
  ↓
1. Parse Protocol (gRPC frame, HTTP, etc.)
  ↓
2. Extract Metadata (client ID, tenant ID, trace ID)
  ↓
3. Authenticate (Bearer token, JWT, mTLS, API key)
  ↓
4. Authorize (RBAC: can client call this endpoint?)
  ↓
5. Rate Limit Check (have we exceeded quota?)
  ↓
6. Load Balancer Select (which replica?)
  ↓
7. Circuit Breaker Check (is replica healthy?)
  ↓
8. Connect + Forward Request
  ↓
9. Wait for Response (with timeout)
  ↓
10. Handle Errors (retry, circuit breaker state change)
  ↓
11. Collect Metrics (increment counters, record latency)
  ↓
12. Send Trace Span (optional sampling)
  ↓
Response Out
```

### Shutdown

```
1. Signal received (SIGTERM, SIGINT)
2. Drain existing connections (wait drain_timeout)
3. Stop accepting new requests
4. Flush metrics to Prometheus
5. Flush traces to Jaeger
6. Close listeners
7. Exit gracefully
```

---

## Discovery Module

### Discovery Plugin Interface

```go
type DiscoveryPlugin interface {
  // Initialize connection to discovery service
  Init(config Config) error
  
  // Watch for replica changes
  Watch(ctx Context) <-chan []Replica
  
  // Get current replica list (immediate)
  GetReplicas() []Replica
  
  // Cleanup
  Close() error
}
```

### etcd Discovery Implementation

```
1. Connect to etcd cluster (multiple endpoints)
2. List all keys under `/weaver/replicas/`
3. For each key, parse JSON: {name, address, attributes}
4. Start watch on `/weaver/replicas/` prefix
5. On key change, update internal replica list
```

### Consul Discovery Implementation

```
1. Connect to Consul HTTP API
2. Query `/v1/catalog/services` → get service names
3. For each service, query `/v1/catalog/service/{service}`
4. Build replica list from service instances
5. Watch for changes via long-polling (every 10s)
```

---

## Health Check Module

### Health Check Loop

```
Per Replica:
  While true:
    1. Check replica health (HTTP GET, gRPC RPC, TCP connect)
    2. Record result (success/failure)
    3. Update replica status based on thresholds
       - 3 consecutive failures → UNHEALTHY
       - 1 success after failure → HEALTHY
    4. Sleep for interval (5s default)
```

### Status State Machine

```
HEALTHY
  ↓ failure_count++ (when health check fails)
UNHEALTHY when failure_count >= unhealthy_threshold (3)
  ↓ if all replicas UNHEALTHY → PANIC_MODE
PANIC_MODE (sends all requests to any replica)
  ↓ success_count++ (when health check succeeds)
HEALTHY when success_count >= healthy_threshold (1)
```

---

## Load Balancer Module

### Load Balancer Plugin Interface

```go
type LoadBalancerPlugin interface {
  // Select replica for request
  Select(ctx Context, replicas []Replica) (*Replica, error)
  
  // Notify of connection change (for least-connections)
  UpdateLoad(replica *Replica, delta int) error
  
  // Rebalance (called when replica list changes)
  Rebalance(replicas []Replica) error
}
```

### Algorithm Complexity

| Algorithm | Time | Space | Notes |
|-----------|------|-------|-------|
| Round-Robin | O(1) | O(1) | Counter mod replicas.len |
| Least-Connections | O(n) | O(n) | Scan all; pick min |
| Random | O(1) | O(1) | No state |
| Consistent Hash | O(log n) | O(n) | Binary search on ring |
| Weighted | O(1) | O(1) | Weighted round-robin |
| Resource-Aware | O(n) | O(n) | Calculate score for each |

---

## Rate Limiter Module

### Token Bucket Algorithm

```
bucket = TokenBucket(tokens_per_second=1000, max_tokens=1000)

On Request:
  if bucket.tokens >= 1:
    bucket.tokens -= 1
    allow request
  else:
    deny request (429)

Background:
  Every 1ms: bucket.tokens += (tokens_per_second / 1000)
  Cap: bucket.tokens <= max_tokens
```

### Multi-Dimensional Rate Limiting

```
Check order:
  1. Per-tenant limit? (if tenant ID in request)
  2. Per-client limit? (if client ID in request)
  3. Per-IP limit? (if rate limiting by IP)
  4. Global limit? (always checked)

If ANY limit exceeded → return 429
```

---

## Circuit Breaker Module

### State Machine

```
CLOSED (healthy)
  ↓ failures++
  ↓ failures >= failure_threshold (5) → OPEN
OPEN (failing)
  ↓ wait timeout (60s)
HALF_OPEN (testing recovery)
  ↓ success
  ↓ CLOSED
  ↓ failure → OPEN
```

### Implementation

```go
type CircuitBreaker struct {
  State CircuitState  // CLOSED, OPEN, HALF_OPEN
  Failures int
  Successes int
  LastFailure time.Time
}

func (cb *CircuitBreaker) Call(fn func() error) error {
  switch cb.State {
  case CLOSED:
    return callAndRecordResult(fn)
  case OPEN:
    if time.Since(cb.LastFailure) > timeout {
      cb.State = HALF_OPEN
      return callAndRecordResult(fn)
    } else {
      return ErrCircuitOpen
    }
  case HALF_OPEN:
    err := callAndRecordResult(fn)
    if err == nil {
      cb.State = CLOSED
    } else {
      cb.State = OPEN
    }
    return err
  }
}
```

---

## Retry Module

### Exponential Backoff

```
attempt = 1
while attempt <= max_attempts:
  try request
  if success:
    return result
  if failure and retryable:
    wait_time = base * (multiplier ^ (attempt - 1))
    wait_time = min(wait_time, max_wait)
    sleep(wait_time)
    attempt++
  else:
    return error
```

### Retryable Errors

**Retryable:**
- Connection timeout
- Temporary network error
- 503 Service Unavailable
- 504 Gateway Timeout

**Not Retryable:**
- 400 Bad Request (client error)
- 401 Unauthorized (auth error)
- 404 Not Found (not going to retry away)

---

## Metrics Collection

### Metric Recording

```
Per Request:
  1. Record request_count++ (total)
  2. Record request_duration_ms (histogram)
  3. If error: record_error_count++
  4. If rate-limited: record_rate_limited_count++
  5. If circuit breaker open: record_cb_open_count++
```

### Prometheus Endpoint

```
GET /metrics → Return all metrics in Prometheus text format

fm_gw_requests_total{replica="fm-1"} 12345
fm_gw_request_duration_p99{replica="fm-1"} 50.5
fm_gw_request_errors_total{replica="fm-1"} 12
...
```

---

## Tracing Integration

### Trace Sampling

```
On request:
  random_num = rand()
  if random_num < sample_rate (0.1 = 10%):
    create trace span
    send to Jaeger
  else:
    skip trace (save bandwidth)
```

### Trace Context Propagation

```
Incoming Request
  ↓ Extract trace context (from headers)
  ↓ Create child span
  ↓ Add span events (discovery, LB, connect, send, receive)
  ↓ Send to Jaeger
```

---

## Configuration Management

### Config Loading

```
1. Read YAML file (path from CLI flag or env var)
2. Parse YAML → Go struct
3. Validate required fields
4. Apply defaults for optional fields
5. Type-check all values
6. Return validated config
```

### Config Reload (Zero Downtime)

```
1. Load new config file
2. Validate it
3. If validation fails: keep old config, log error
4. If validation succeeds:
   - Update in-memory config
   - Restart affected modules (if necessary)
   - Keep existing connections (no disruption)
```

---

## Error Handling

### Error Categories

**User Errors (4xx):**
- 400 Bad Request (invalid request format)
- 401 Unauthorized (missing auth)
- 403 Forbidden (auth failed)
- 429 Too Many Requests (rate limited)

**Server Errors (5xx):**
- 500 Internal Error (unexpected panic)
- 503 Service Unavailable (no healthy replicas)
- 504 Gateway Timeout (request timeout)

---

## Panic Recovery

### Crash Safety

```
Defer recovery in request handler:
  defer func() {
    if r := recover(); r != nil {
      log error with panic info
      increment panic_count metric
      return 500 error to client
      continue processing other requests
    }
  }()
```

This ensures a panic in one request doesn't crash the entire gateway.

---

## Security

### Authentication

```
On Request:
  1. Extract credential from header (Bearer, API Key, mTLS cert)
  2. Validate credential (check against list or JWT issuer)
  3. Extract user/role information
  4. Proceed with authorization check
```

### Authorization

```
On Request:
  1. Extract permission needed (read, write, admin)
  2. Extract user role
  3. Check if role has permission
  4. If not: return 403 Forbidden
```

---

**Navigation:**
- [← Previous](./40-hld.md)
- [Index](../INDEX.md)
- [Next →](./42-data-structures.md)
