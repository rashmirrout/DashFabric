# Registry Semantics: Acquire/Release/Ready (Exact Contract)

**Status:** AUTHORITATIVE
**Owner:** FM Architecture
**Supersedes:** registry-pattern-design.md §3 (ambiguous sections)
**Date:** 2026-06-22

---

## 1. PURPOSE

Resolves ambiguity in the registry Acquire/Release contract that blocks implementation of GlobalRegistry, VnetRegistry, VnetMappingRegistry, GroupRegistry, and HaRegistry. Defines exact semantics for:

1. Initial value timing
2. Ready channel close conditions
3. Release debounce policy
4. Early cancellation / pending-hydration races
5. Lock order and concurrency model
6. Failure recovery

This document is the implementation contract — **deviations are bugs**.

---

## 2. SUBSCRIPTION TYPE

```go
type Subscription[V any] struct {
    Key       string           // The key acquired
    Initial   V                // See §3 for timing
    Updates   <-chan V         // Buffered channel; closed on Release(grace=0) or T1 watch failure
    Ready     <-chan struct{}  // Closed when first full snapshot landed; see §4
    Err       <-chan error     // Closed when no further errors possible; see §7
    cancel    func()           // Internal: invoked by Release()
}

type RegistryConfig struct {
    GracePeriod        time.Duration  // Default: 30s; per-registry override
    UpdatesBufferSize  int            // Default: 64; tune per workload
    HydrationTimeout   time.Duration  // Default: 10s; how long Ready can be open
    WatchRetryBackoff  BackoffConfig  // For T1 watch reconnect
}
```

---

## 3. ACQUIRE SEMANTICS

### 3.1 Three Cases

```
function Acquire(key) -> Subscription:

  REGISTRY_LOCK.write_lock()

  case_a: key in cache AND watch active
    refcount[key]++
    sub = NewSubscription(key)
    sub.Initial = cache[key]                          # CURRENT cached value
    sub.Ready   = AlreadyClosedChan()                 # Ready immediately
    sub.Updates = subscribers[key].AddSubscriber()    # join fanout
    REGISTRY_LOCK.unlock()
    return sub

  case_b: key in cache BUT debounce timer active (refcount went to 0, grace not expired)
    cancel_debounce_timer(key)                         # cancel pending eviction
    refcount[key]++
    sub = NewSubscription(key)
    sub.Initial = cache[key]
    sub.Ready   = AlreadyClosedChan()
    sub.Updates = subscribers[key].AddSubscriber()
    REGISTRY_LOCK.unlock()
    return sub

  case_c: key not in cache (cold start)
    refcount[key] = 1
    sub = NewSubscription(key)
    sub.Initial = ZeroValue[V]()                       # NOTE: zero, not nil
    sub.Ready   = NewOpenChan()                        # signals hydration
    sub.Updates = subscribers[key].AddSubscriber()
    REGISTRY_LOCK.unlock()
    StartT1Watch(key, async=true)                      # see §4
    return sub
```

**KEY RULE: `Initial` is the value at Acquire-time, NOT first-emit-after-Acquire.**
- If cached → cached value
- If not cached → zero value (caller MUST check `Ready` before using)

### 3.2 Caller Contract

```go
// CORRECT pattern:
sub := registry.Acquire("vnet-123")
defer registry.Release("vnet-123")

select {
case <-sub.Ready:
    value := sub.Initial   // safe to use NOW (or read latest from Updates)
case <-ctx.Done():
    return ctx.Err()
case <-time.After(HydrationTimeout):
    return errors.New("registry hydration timeout")
}

// Subsequent updates:
for {
    select {
    case v := <-sub.Updates:
        process(v)
    case <-ctx.Done():
        return
    }
}
```

**ANTI-PATTERN (will cause bugs):**
```go
sub := registry.Acquire("vnet-123")
process(sub.Initial)   // ← may be zero value! Always wait for Ready first
```

---

## 4. READY CHANNEL

### 4.1 Close Conditions

`Ready` closes EXACTLY ONCE under any of these conditions:

1. **First full snapshot received from T1** (normal case)
2. **Key confirmed absent from T1** (Acquire on non-existent key) — Ready closes, Initial stays zero, Updates remains open for future changes
3. **HydrationTimeout exceeded** — Ready closes, Err emits `RegistryError{Code: HYDRATION_TIMEOUT}`, Initial stays zero
4. **Watch creation failed permanently** — Ready closes, Err emits `RegistryError{Code: WATCH_FAILED}`, Updates closes

### 4.2 Multiple Subscribers, Same Key

All subscribers to the same key share the SAME `Ready` channel. When the first hydration completes, all subscribers see the close simultaneously. This is intentional — guarantees consistent observation.

### 4.3 Re-Acquire After Eviction

If a key was previously hydrated, evicted (after grace), then re-Acquired:
- New `Ready` channel created (the old one already closed)
- Hydration starts fresh from T1
- Cache rebuilt from new snapshot

---

## 5. RELEASE SEMANTICS

### 5.1 Algorithm

```
function Release(key):

  REGISTRY_LOCK.write_lock()

  refcount[key]--

  if refcount[key] > 0:
    REGISTRY_LOCK.unlock()
    return

  # refcount reached 0 — schedule debounce
  if debounce_timer[key] != nil:
    cancel(debounce_timer[key])    # shouldn't happen normally

  debounce_timer[key] = ScheduleAfter(grace_period, func() {
    REGISTRY_LOCK.write_lock()
    if refcount[key] > 0:
      REGISTRY_LOCK.unlock()
      return                       # Acquired again during grace; abort eviction
    StopT1Watch(key)
    delete(cache, key)
    delete(subscribers, key)        # closes all Updates channels
    delete(refcount, key)
    delete(debounce_timer, key)
    REGISTRY_LOCK.unlock()
  })

  REGISTRY_LOCK.unlock()
```

### 5.2 Grace Period

- **Default: 30 seconds** (chosen to absorb rapid Acquire/Release cycles during NIC churn)
- **Configurable per-registry** via `RegistryConfig.GracePeriod`
- **Not configurable per-key** (keeps logic simple, debugging predictable)

### 5.3 Updates Channel Closure

When a key is evicted (after grace), all `Updates` channels for that key are closed. Subscribers MUST handle channel close gracefully:

```go
for {
    select {
    case v, ok := <-sub.Updates:
        if !ok {
            return   // channel closed = key evicted = subscription ended
        }
        process(v)
    }
}
```

---

## 6. EARLY CANCELLATION / PENDING-HYDRATION RACE

### 6.1 Scenario: Acquire then Immediate Release

```
t=0  : Acquire(key)   → refcount=1, watch starting, Ready open
t=1ms: Release(key)   → refcount=0, grace timer scheduled (30s)
t=2ms: T1 watch completes → Ready closes, cache populated
t=30s: Grace expires → cache evicted, watch closed
```

**Outcome:** Subscriber sees Ready close (with valid Initial=zero, Updates has the cached value), then channel closes 30s later. No bug, just wasted work.

### 6.2 Scenario: Release Before Ready

```
t=0  : Acquire(key)   → refcount=1, watch starting, Ready open
t=1ms: Release(key)   → refcount=0, grace timer scheduled
t=10s: T1 watch still pending (slow etcd)
t=30s: Grace expires → BUT watch hasn't completed yet
```

**Resolution:** Eviction proceeds; pending watch is cancelled; cache stays empty; Ready closes with `RegistryError{Code: SUBSCRIPTION_CANCELLED}`.

### 6.3 Scenario: Acquire During Grace

```
t=0  : Acquire(A)     → refcount=1
t=5s : Release(A)     → refcount=0, grace timer @ t=35s
t=10s: Acquire(A)     → cancel grace timer, refcount=1, return cached value
```

**Outcome:** Zero wasted work. Cache stays warm. Critical for NIC churn scenarios.

---

## 7. ERROR CHANNEL

### 7.1 Error Types

```go
type RegistryError struct {
    Code      ErrorCode
    Key       string
    Cause     error
    Retryable bool
}

type ErrorCode int
const (
    HYDRATION_TIMEOUT       ErrorCode = iota  // First snapshot took >timeout
    WATCH_FAILED                              // T1 watch could not establish
    WATCH_DISCONNECTED                        // T1 watch dropped mid-stream (will retry)
    PARSE_ERROR                               // T1 value couldn't be decoded
    SUBSCRIPTION_CANCELLED                    // Released before Ready
    PERMANENT_FAILURE                         // Unrecoverable; Updates closing
)
```

### 7.2 Error Semantics

- **Retryable errors** (WATCH_DISCONNECTED): Logged, registry retries with backoff. Subscriber may continue using last cached value.
- **Non-retryable errors** (HYDRATION_TIMEOUT, WATCH_FAILED, PERMANENT_FAILURE): Updates channel closes, subscriber must Release and re-Acquire (or give up).
- **PARSE_ERROR**: Specific bad value is dropped; cache keeps previous value; metric emitted; subscriber notified once.

### 7.3 Subscriber Pattern

```go
go func() {
    for err := range sub.Err {
        if err.Retryable {
            metrics.RegistryError(registry.Name, err.Code).Inc()
            // continue using last value
        } else {
            log.Error("permanent registry failure", "key", sub.Key, "err", err)
            // Release + re-Acquire OR escalate to actor state machine
        }
    }
}()
```

---

## 8. LOCK ORDER & CONCURRENCY

### 8.1 Per-Registry Locks

Each registry instance holds:
- `REGISTRY_LOCK` (sync.RWMutex) — protects `cache`, `refcount`, `subscribers`, `debounce_timer` maps
- `WATCH_LOCK` (sync.Mutex) — protects T1 watch lifecycle per-key

**LOCK ORDER (mandatory to prevent deadlock):**
1. Acquire REGISTRY_LOCK first
2. Acquire WATCH_LOCK second (only if needed)
3. Never call out to T1 / etcd while holding REGISTRY_LOCK

### 8.2 Cross-Registry Acquires

NicActor acquires from multiple registries. **Acquire order is fixed:**

1. GlobalRegistry (routing_types, meter_policies, defaults)
2. VnetRegistry
3. VnetMappingRegistry
4. GroupRegistry (all groups for this NIC)
5. HaRegistry

**RATIONALE:** Deterministic order prevents cross-registry deadlock if any registry internally references another (none currently do, but the rule is defensive).

### 8.3 Reader Concurrency

`cache` reads use RLock — many concurrent readers OK. Updates use WLock — exclusive.

`subscribers[key]` is itself a fanout structure with its own internal lock; sending to subscribers does NOT require REGISTRY_LOCK.

---

## 9. PER-REGISTRY OVERRIDES

| Registry | GracePeriod | UpdatesBuffer | Notes |
|----------|-------------|---------------|-------|
| GlobalRegistry | 5min | 32 | Long-lived, low churn (defaults, routing types) |
| VnetRegistry | 30s | 64 | Medium churn |
| VnetMappingRegistry | 30s | 128 | High update rate (peering changes) |
| GroupRegistry | 30s | 64 | Medium churn |
| HaRegistry | 60s | 32 | HA scopes change rarely |

---

## 10. METRICS (REQUIRED PER REGISTRY)

```
fm_registry_acquire_total{registry, result="cache_hit|cold|debounce_rescue"}
fm_registry_release_total{registry}
fm_registry_cache_size{registry}
fm_registry_refcount_sum{registry}
fm_registry_hydration_latency_seconds{registry} (histogram)
fm_registry_eviction_total{registry, reason="grace|error|permanent"}
fm_registry_watch_reconnects_total{registry}
fm_registry_errors_total{registry, code}
```

---

## 11. TEST MATRIX (REQUIRED FOR PR APPROVAL)

| Test | Behavior Asserted |
|------|-------------------|
| T-001 Acquire cold key | Ready opens, T1 watch starts, Initial=zero |
| T-002 Acquire cached key | Ready closed immediately, Initial=current value |
| T-003 Refcount tracking | N Acquires, N Releases → debounce starts only after Nth Release |
| T-004 Debounce grace | Release → wait 29s → Acquire → no eviction occurred |
| T-005 Debounce eviction | Release → wait 31s → key evicted, watch closed |
| T-006 Concurrent acquires | 100 goroutines Acquire same key → all share Ready close moment |
| T-007 Updates fanout | 5 subscribers, T1 update → all 5 receive in Updates |
| T-008 Release during hydration | Acquire → Release before Ready → no panic, eviction proceeds |
| T-009 Watch reconnect | Kill T1 connection → registry retries → no subscriber notified of disconnect |
| T-010 Parse error isolation | Bad value → cache unchanged → metric incremented → next good value processed |
| T-011 Hydration timeout | Slow T1 → Ready closes after timeout → Err emits HYDRATION_TIMEOUT |
| T-012 Cross-registry order | NicActor acquires from 5 registries → no deadlock under concurrent NIC arrivals |

---

## 12. MIGRATION FROM EXISTING DESIGN

Current `registry-pattern-design.md` §3 is replaced by this document. Implementation work:

1. Update `Subscription` struct: add `Err` channel, remove `WaitReady()` convenience (callers should select on Ready directly)
2. Implement debounce timer with cancellation
3. Add `RegistryConfig` with per-registry defaults from §9
4. Add metrics from §10
5. Implement all 12 tests in §11 before declaring registry "done"

---

## 13. REFERENCES

- `nicgoalstate-schema-design.md` §3.1 — NicActor's Acquire pattern
- `vm-eni-provisioning-design.md` §5A — Registry pattern phase
- `storage-architecture.md` — T1 watch mechanics
- `fleet-manager-lld.md` §1.1 — Actor state machine integration
