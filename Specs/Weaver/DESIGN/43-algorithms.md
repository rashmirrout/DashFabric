# Weaver: Algorithms

> **Read Time:** 20 minutes  
> **Previous:** [42-data-structures.md](./42-data-structures.md) | **Next:** [44-concurrency-model.md](./44-concurrency-model.md)

---

## Load Balancing Algorithms

### Round-Robin

```
state: counter = 0

Select(replicas):
  selected = replicas[counter % len(replicas)]
  counter++
  return selected

Complexity: O(1)
Fairness: Perfect (strict alternation)
```

### Least-Connections

```
Select(replicas):
  min_load = infinity
  selected = nil
  
  for each replica in replicas:
    if replica.Load < min_load:
      min_load = replica.Load
      selected = replica
  
  return selected

Complexity: O(n)
Fairness: Fair when connections have equal duration
```

### Consistent Hash

```
Ring of 1000 points (hash space: 0-999)

Setup:
  1. Create hash ring
  2. For each replica, hash(replica_name) → point on ring
  3. Replicas now evenly distributed around ring

Select(replicas, key):
  1. hash(key) → point on ring
  2. Walk clockwise until we find a replica
  3. Return that replica

Complexity: O(log n) (binary search on ring)
Benefit: Sticky (same key always → same replica)
Benefit: When replicas change, only 1/n requests move
```

### Resource-Aware

```
Select(replicas):
  best_score = 0
  selected = nil
  
  for each replica in replicas:
    cpu_usage = replica.metrics.cpu_percent
    memory_usage = replica.metrics.memory_percent
    connections = replica.Load
    
    // Lower is better
    score = (cpu_usage * 0.5) + (memory_usage * 0.3) + (connections * 0.2)
    
    if score < best_score:
      best_score = score
      selected = replica
  
  return selected

Complexity: O(n)
Benefit: Sends more traffic to less-loaded replicas
```

---

## Circuit Breaker Algorithm

### State Machine

```
State: CLOSED
Failures: 0

On Request Failure:
  failures++
  if failures >= failure_threshold (5):
    state ← OPEN
    last_failure_time ← now()

---

State: OPEN
On Request:
  return immediate error (no attempt)
  
Background:
  if now() - last_failure_time > timeout (60s):
    state ← HALF_OPEN
    failures ← 0
    successes ← 0

---

State: HALF_OPEN
On Request Success:
  successes++
  if successes >= success_threshold (2):
    state ← CLOSED
    failures ← 0

On Request Failure:
  state ← OPEN
  last_failure_time ← now()
```

### State Transition Diagram

```
         failures >= 5
    ┌─────────────────┐
    │                 ▼
  CLOSED ────────► OPEN
    ▲                 │
    │                 │
    │         timeout elapsed
    │                 │
    │                 ▼
    │             HALF_OPEN
    │                 │
    │    ┌────────────┴────────────┐
    │    │                         │
    │    ▼ (success)               ▼ (failure)
    └──success_threshold       OPEN
```

---

## Retry with Exponential Backoff

```
function retry_request(max_attempts):
  for attempt = 1 to max_attempts:
    try:
      response = send_request()
      return response
    
    except RetryableError as e:
      if attempt < max_attempts:
        backoff_ms = min(
          base_ms * (multiplier ^ (attempt - 1)),
          max_backoff_ms
        )
        example: 100ms * (2^0) = 100ms
                 100ms * (2^1) = 200ms
                 100ms * (2^2) = 400ms
                 capped at max_backoff_ms
        
        sleep(backoff_ms)
      else:
        raise e

Example with base=100ms, multiplier=2, max=5s:
  Attempt 1: fail → wait 100ms
  Attempt 2: fail → wait 200ms
  Attempt 3: fail → wait 400ms
  Attempt 4: fail → wait 800ms
  Attempt 5: fail → wait 1600ms
  Attempt 6: fail → wait 3200ms
  Attempt 7: fail → wait 5000ms (capped)
  Attempt 8: fail → give up
```

---

## Token Bucket (Rate Limiting)

```
class TokenBucket:
  capacity: max tokens (e.g., 1000)
  refill_rate: tokens/sec (e.g., 1000)
  tokens: current tokens
  last_refill: when we last added tokens

method allow(count):
  // First, refill tokens based on time elapsed
  now = current_time()
  elapsed_sec = (now - last_refill).seconds()
  
  tokens_to_add = elapsed_sec * refill_rate
  tokens = min(tokens + tokens_to_add, capacity)
  
  last_refill = now
  
  // Check if we can fulfill request
  if tokens >= count:
    tokens -= count
    return true
  else:
    return false

Example:
  capacity=1000, refill_rate=100 tokens/sec
  
  T=0: tokens=1000
  Request for 500 → allowed, tokens=500
  Request for 500 → allowed, tokens=0
  Request for 100 → denied (tokens=0)
  
  T=1sec: refill 100 tokens → tokens=100
  Request for 100 → allowed, tokens=0
```

---

## Health Check Algorithm

```
for each replica:
  while running:
    try:
      // Execute health check
      result = health_check(replica)  // HTTP, gRPC, or TCP
      
      if result.success:
        replica.success_count++
        replica.failure_count = 0
        
        if replica.success_count >= healthy_threshold:
          if replica.status != HEALTHY:
            replica.status ← HEALTHY
            log("Replica recovered")
            replica.success_count ← 0
      else:
        replica.failure_count++
        replica.success_count = 0
        
        if replica.failure_count >= unhealthy_threshold:
          if replica.status != UNHEALTHY:
            replica.status ← UNHEALTHY
            log("Replica marked unhealthy")
            replica.failure_count ← 0
    
    except exception as e:
      replica.failure_count++
      log("Health check failed", error=e)
    
    sleep(check_interval)

Thresholds example:
  unhealthy_threshold = 3 failures
  healthy_threshold = 1 success (after recovering)
  
  Timeline:
    T=0: HEALTHY
    T=5: check 1 fails → failure_count=1
    T=10: check 2 fails → failure_count=2
    T=15: check 3 fails → failure_count=3 → UNHEALTHY
    T=20: check 1 fails (still unhealthy)
    T=25: check 1 succeeds → success_count=1 >= 1 → HEALTHY
```

---

## Panic Mode Algorithm

```
periodic_check():
  healthy_count = count replicas with status HEALTHY
  
  if healthy_count == 0:
    // No healthy replicas; enter PANIC_MODE
    gateway.panic_mode = true
    log("PANIC MODE: no healthy replicas")
    metrics.panic_mode.set(1)
  else:
    gateway.panic_mode = false
    metrics.panic_mode.set(0)

on_request_in_panic_mode():
  // In panic mode, try any replica (even if marked unhealthy)
  if gateway.panic_mode:
    selected_replica = select_any_replica(all_replicas)
    // Might succeed if replica temporarily recovered
    // Or might fail if replica truly down
  else:
    selected_replica = normal_load_balancer_select()
```

---

## Multi-Dimensional Rate Limiting

```
function check_rate_limit(request):
  // Check each dimension in order
  
  // 1. Per-tenant limit
  if request.tenant_id:
    bucket = get_tenant_bucket(request.tenant_id)
    if not bucket.allow(1):
      return 429  // Rate limited
  
  // 2. Per-client limit
  if request.client_id:
    bucket = get_client_bucket(request.client_id)
    if not bucket.allow(1):
      return 429
  
  // 3. Per-IP limit
  if request.source_ip:
    bucket = get_ip_bucket(request.source_ip)
    if not bucket.allow(1):
      return 429
  
  // 4. Global limit (always checked)
  if not global_bucket.allow(1):
    return 429
  
  return 200  // Allowed

Example:
  Request from tenant="acme", client="client1", ip="10.0.0.1"
  
  Check tenant bucket: pass
  Check client bucket: pass
  Check IP bucket: pass
  Check global bucket: FAIL! (global limit exceeded)
  → return 429
```

---

## Timeout Hierarchy

```
Request Timeline:
  ┌─────────────────────────────────────┐ Global Timeout (30s)
  │                                     │
  │ ┌──────────────────────────────────┐│ Per-Replica Timeout (25s)
  │ │                                  ││
  │ │ ┌────────────┐ ┌──────────────┐ ││
  │ │ │ Connect    │ │ Request      │ ││
  │ │ │ Timeout(5s)│ │ Timeout(20s) │ ││
  │ │ └────────────┘ └──────────────┘ ││
  │ └──────────────────────────────────┘│
  └─────────────────────────────────────┘

Algorithm:
  1. Start request
  2. Set timeout to min(global_timeout, per_replica_timeout)
  3. Connect with timeout = connect_timeout
  4. If timeout exceeded → fail
  5. Send request with timeout = total_timeout - elapsed
  6. If timeout exceeded → cancel and retry
```

---

**Navigation:**
- [← Previous](./42-data-structures.md)
- [Index](../INDEX.md)
- [Next →](./44-concurrency-model.md)
