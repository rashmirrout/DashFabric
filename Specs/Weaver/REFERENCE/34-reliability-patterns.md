# Weaver: Reliability Patterns Reference

> **Read Time:** 25 minutes  
> **Previous:** [33-health-monitoring.md](./33-health-monitoring.md) | **Next:** [35-observability.md](./35-observability.md)

---

## Circuit Breaker State Machine

```
CLOSED (normal)
  ↕ (if failures > threshold)
OPEN (fail-fast, reject requests)
  ↕ (after timeout)
HALF_OPEN (testing recovery)
  ├→ (if success) CLOSED
  └→ (if fails) OPEN
```

**Configuration:**
```yaml
circuit_breaker:
  enabled: true
  failure_threshold: 5
  success_threshold: 2
  timeout: 30s
```

---

## Retry with Exponential Backoff

```
Attempt 1: Send → Fail
Wait: 10ms
Attempt 2: Send → Fail
Wait: 20ms
Attempt 3: Send → Fail
Wait: 40ms
Attempt 4: Send → Success
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

---

## Timeout Management

```yaml
timeout:
  global: 30s              # Total request time
  per_replica: 25s         # Per attempt
  connect: 5s              # Connection establish
```

---

## Request Queuing

```yaml
queuing:
  enabled: true
  per_replica_depth: 1000  # Max queue size per replica
```

When queue full → request rejected with backpressure signal

---

**Navigation:**
- [← Previous](./33-health-monitoring.md)
- [Index](../INDEX.md)
- [Next →](./35-observability.md)
