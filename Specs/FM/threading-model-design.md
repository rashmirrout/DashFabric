# Threading Model & Concurrency Architecture

**Status:** AUTHORITATIVE
**Owner:** FM Architecture
**Supersedes:** fleet-manager-hld.md §6.2 (thread pool list, no sizing)
**Date:** 2026-06-22

---

## 1. PURPOSE

Quantifies the threading model for FM: pool sizes derived from queuing analysis (not guesses), backpressure with explicit policies, memory budgets, synchronization primitives, lock order, panic recovery, and shutdown sequencing. Resolves `ARCHITECTURE_REVIEW.md` GAP 1.1.

**Implementation contract:** Every numeric value in this document comes with its derivation. When tuning, update the derivation — do not change numbers in isolation.

---

## 2. WORKLOAD ASSUMPTIONS (BASELINE FOR SIZING)

All pool sizes derive from these assumptions. **If reality diverges by >2×, resize.**

| Parameter | Value | Source / Rationale |
|-----------|-------|--------------------|
| Devices per FM pod (shard) | 500 | Sharding target: 10k devices across 20 pods |
| ENIs per device (avg) | 16 | DASH-typical (VM density) |
| ENIs per device (P99) | 64 | Bursty container hosts |
| ENIs per FM pod | 8,000 (avg), 32,000 (peak) | 500 × 16 avg, 500 × 64 peak |
| Steady-state NIC events/sec | 50 | Routine churn: 1% of NICs/min |
| Burst NIC events/sec | 2,000 | Rolling upgrade scenario (4k NICs in 2s) |
| Compose time (P50) | 5 ms | Hot path, all registries cached |
| Compose time (P99) | 50 ms | Cold registry, large prefix-tag |
| Program time per wave (P50) | 8 ms | gNMI/SAI typical |
| Program time per wave (P99) | 80 ms | Device under load |
| Registry value size (P50) | 1 KB | VnetSpec, GroupSpec |
| Registry value size (P99) | 64 KB | Large prefix-tag bundle |
| Total registry footprint | ≤ 4 GiB | T3 cache + in-mem registries combined |

---

## 3. POOL INVENTORY (CANONICAL LIST)

This is the single source of truth for thread pools. Any code spawning goroutines outside these pools is a bug.

| ID | Name | Type | Pool Size | Owner |
|----|------|------|-----------|-------|
| P1 | Actor Executor | Goroutine-per-actor (bounded total) | 4k–32k goroutines (autoscale) | Actor framework |
| P2 | Southbound RPC | Bounded worker pool | 16/device, 8k total cap | SouthboundDriver |
| P3 | Registry Watchers | One goroutine per active key | ≤ 50k goroutines (= max keys) | Registry layer |
| P4 | Adapter Stream Processors | One goroutine per CB endpoint | ≤ 8 (matches CB count) | Adapter pod |
| P5 | T1/T2 Client Workers | gRPC connection pool | 32 connections | Storage layer |
| P6 | T3 RocksDB | RocksDB internal threads | 8 (config: `max_background_jobs`) | T3 layer |
| P7 | Reconciliation Sweeper | Bounded scheduler | 4 goroutines | Reconciliation engine |
| P8 | HTTP/gRPC Server | Standard server pool | 256 max in-flight | API gateway |
| P9 | Metrics Exporter | Single goroutine | 1 | Observability |
| P10 | Health/Lease Renewer | Single goroutine | 1 per role | HA layer |

---

## 4. POOL P1 — ACTOR EXECUTOR

### 4.1 Why Goroutine-Per-Actor

Go's M:N scheduler handles tens of thousands of goroutines cheaply (~2KB stack base). Actors are mostly IO-bound (registry waits, RPC waits). Goroutine-per-actor:
- Simplifies code (no manual event loops)
- Natural backpressure via mailbox channels
- Aligns with Go idioms (no exotic scheduler needed)

**Constraint:** A goroutine MUST belong to an actor. No fire-and-forget `go func()` calls in business logic.

### 4.2 Sizing (Little's Law)

```
N (goroutines) = λ (arrival rate)  ×  W (service time per request)
```

**ENIs as actors:**
- ENI actor exists for the lifetime of the NIC
- At 8k ENIs avg → 8k goroutines at baseline
- At 32k peak → 32k goroutines
- Plus HDO (500) + CO (≤1.5k container actors) + assemblers ≈ 2k

**Steady state:** ~10k goroutines × 2KB stack ≈ 20 MiB (negligible)
**Peak:** ~35k goroutines × 2KB stack ≈ 70 MiB

### 4.3 Mailbox Sizing (Per Actor)

Each actor has a buffered channel as mailbox. Sized to absorb registry update bursts:

| Actor Class | Mailbox Capacity | Rationale |
|-------------|------------------|-----------|
| NicActor | 32 | 6 registry inputs × ~5 updates burst |
| ContainerActor | 16 | Lower fan-in |
| HostDeviceActor | 64 | Aggregates all child events + reconciliation |
| Assembler (VnetMappingRegistry inner) | 128 | High update rate on peering |

**Backpressure on mailbox full:**
- Producer (registry fanout) **blocks with 100ms timeout**, then drops oldest event
- Drop emits metric `fm_actor_mailbox_dropped_total{actor_class}` — alert if non-zero
- Rationale: registry updates are idempotent (next snapshot is full state); losing intermediate updates is safe

### 4.4 Hard Caps & Admission

- **Hard cap on NicActor count per pod:** 50,000 (refuses new NICs with 503; rebalance shard)
- **Soft cap (warning):** 40,000 (logs + metric)
- Admission control: API gateway checks `fm_active_nic_actors` gauge before accepting new ENI

---

## 5. POOL P2 — SOUTHBOUND RPC

### 5.1 Per-Device Concurrency

Each HDO holds one SouthboundDriver instance. Within a wave, up to **16 concurrent RPCs** (from `southbound-driver-interface-redesign.md` §4 `max_parallel_in_wave`).

**Per-pod cap:** 500 devices × 16 = 8,000 max concurrent southbound RPCs.

### 5.2 RPC Resource Budget

- File descriptors: each gNMI/SAI connection = 1 FD; 500 devices × 1 = 500 FDs (well under default 65k limit)
- Memory per in-flight RPC: ~16 KB request + ~16 KB response ≈ 32 KB
- Peak memory: 8,000 × 32 KB = 256 MiB

### 5.3 Backpressure

If wave concurrency limit reached:
- Additional commands queue inside driver (bounded: 256 per device)
- If driver queue full → return `DRV_RESOURCE_EXHAUSTED` to NicActor
- NicActor parks in `THROTTLED` substate, retries after exponential backoff

---

## 6. POOL P3 — REGISTRY WATCHERS

### 6.1 One Goroutine Per Watched Key

Each Acquired key spins up one watcher goroutine that pumps T1 events into the registry's fanout.

**Maximum watched keys per pod:**
- Globals: ~100 keys
- Groups: ~10,000 keys (1 per group × 500 devices avg sharing)
- VNETs: ~2,000 keys (1 per VNET, refcounted across NICs)
- VnetMappings: ~2,000 keys
- HAScopes: ~500 keys
- **Total: ~15,000 watcher goroutines** (well under the 50k Go ceiling)

### 6.2 Watcher Lifecycle

- Created on first Acquire of a key
- Lives until grace period after refcount=0 (default 30s; see `registry-semantics-exact.md` §5)
- On disconnect: retries with exponential backoff (1s → 30s cap, 10% jitter)

### 6.3 Memory Budget

15,000 watchers × ~4 KB (channel buffers + state) ≈ 60 MiB

---

## 7. POOL P4 — ADAPTER STREAM PROCESSORS

### 7.1 One Goroutine Per CB Endpoint

From `adapter-protocol-design.md` §10, supports multiple CB endpoints. Each gets a dedicated processor goroutine.

**Expected CB count:** 1–8 (one per vendor plugin + simulator). Pool size matches.

### 7.2 Per-Processor Queue

Internal queue between CB stream receive and T1 write:
- Capacity: **2,000 events** (≈ 1s burst at 2k events/sec)
- Backpressure: if full, CB stream backpressures naturally (gRPC flow control)
- No drop — at-least-once delivery must hold

---

## 8. POOL P7 — RECONCILIATION SWEEPER

### 8.1 Bounded Workers

Reconciliation reads device hashes; each read is an RPC. To avoid overwhelming devices:

- 4 sweeper goroutines per pod
- Each iterates a fair-share of devices (round-robin)
- Per-device interval: 60s (configurable in `reconciliation-design.md`)
- 500 devices ÷ 4 workers = 125 devices/worker; at 60s interval = 1 RPC/(60/125) ≈ 0.5s spacing per worker

### 8.2 Backoff on Disagreement

If reconciliation detects drift, it ENQUEUES to the NicActor mailbox (does not block sweeper). Multiple drifts of same ENI within 5min coalesce (mailbox dedup).

---

## 9. BACKPRESSURE STRATEGY (END-TO-END)

### 9.1 Layered Flow

```
CB → Adapter → T1 → Registry Watchers → Actor Mailboxes → NicActor → SouthboundDriver → Device
 |       |       |          |                  |                |              |
 [gRPC]  [Q:2k] [etcd]    [fanout]          [Q:32-128]       [bounded         [Q:256/device]
                                                              compose]
```

### 9.2 Per-Hop Policies

| Hop | Capacity | Overflow Policy | Backpressure Mechanism |
|-----|----------|----------------|------------------------|
| CB → Adapter | 2,000 events | Block (gRPC flow control) | Server-side window |
| Adapter → T1 | T1 throughput | Pause processing, no ack | CAS retry loop |
| T1 → Registry watchers | etcd stream | etcd handles flow | Standard etcd watch |
| Registry → Actor mailbox | 32–128 | **Drop oldest** (idempotent updates) | Metric: `mailbox_dropped_total` |
| Actor → SouthboundDriver | 256/device | Return RESOURCE_EXHAUSTED | NicActor enters THROTTLED |
| SouthboundDriver → Device | 16 concurrent | Wave-internal queue | Driver enforces concurrency |

### 9.3 Why "Drop Oldest" Is Safe

Registry updates are full snapshots of the watched key. Losing intermediate snapshots is fine because the next snapshot is the complete current state. (Unlike event logs, which would require retention.)

**Exception:** Mapping-assembler chunks are NOT full snapshots. They use a per-key mailbox without drop — assembler internal state requires ordered chunk delivery. Sized for worst case: 128 chunks.

### 9.4 Global Admission Control

API gateway exposes a "system health" check:
- Reject new ENI creation (503) if any of:
  - Active NicActors > 45k
  - `mailbox_dropped_total` increasing > 10/sec
  - Adapter lag > 30s
  - Southbound error rate > 5%

These rules live in `error-handling-design.md` §10.

---

## 10. SYNCHRONIZATION PRIMITIVES (CANONICAL LIST)

### 10.1 Inventory

| ID | Lock | Scope | Type | Held While |
|----|------|-------|------|-----------|
| L1 | `REGISTRY_LOCK` (per-registry) | Per registry instance | `sync.RWMutex` | Cache map mutations |
| L2 | `WATCH_LOCK` (per-key, per-registry) | Per watched key | `sync.Mutex` | T1 watch lifecycle ops |
| L3 | `ACTOR_MAILBOX` | Per actor | Buffered channel | (not a lock; channel ops) |
| L4 | `HDO_STATE` | Per HDO actor | `sync.RWMutex` | HDO state machine transitions |
| L5 | `NIC_STATE` | Per NicActor | `sync.RWMutex` | NIC state machine transitions |
| L6 | `DRIVER_CONN` | Per driver instance | `sync.Mutex` | Connection state changes |
| L7 | `ADAPTER_LEASE` | Per adapter pod | etcd lease (external) | Leader lifecycle |
| L8 | `T2_TXN` | Per adapter pod | etcd transaction | T1 CAS + T2 watermark |

### 10.2 LOCK ORDER (MANDATORY)

To prevent deadlock, locks are acquired in **strictly ascending ID order**.

```
L1 (REGISTRY_LOCK)
  ↓
L2 (WATCH_LOCK)
  ↓
L4 (HDO_STATE)
  ↓
L5 (NIC_STATE)
  ↓
L6 (DRIVER_CONN)
```

**Rules:**
- Never hold L1 while making outbound RPCs (T1, southbound) — release before IO
- L7 (adapter lease) is independent (held by leader-elect goroutine only)
- L8 (T2 txn) is short-lived; not held across goroutine boundaries

### 10.3 Channel Operations vs. Locks

Channels are not in the lock order; they have their own internal synchronization. But:
- Never send to a channel while holding any lock from §10.1 (potential block while holding lock = serial chain to deadlock)
- Always use `select` with `ctx.Done()` to avoid stuck sends

---

## 11. PANIC RECOVERY

### 11.1 Per-Goroutine Recovery

Every pool's worker goroutine wraps its main loop with `defer recover()`:

```go
func actorLoop(actor Actor) {
    defer func() {
        if r := recover(); r != nil {
            metrics.PanicRecovered(actor.Class()).Inc()
            log.Error("actor panicked", "actor", actor.ID(), "panic", r, "stack", debug.Stack())
            actor.transitionTo(QUARANTINED)
        }
    }()
    actor.run()
}
```

### 11.2 Quarantine vs. Kill

- **Actor panic** → actor → QUARANTINED state, ENI flagged for operator review (no auto-recover; panics indicate bugs)
- **Pool worker panic** → goroutine respawned with backoff (1s → 30s cap), pool continues
- **Critical-path panic** (e.g., leader-elect, T1 client) → pod restart (via supervisor)

### 11.3 Repeated Panics

If same actor panics 3 times within 10 min → permanent QUARANTINED + alert. Prevents thrashing.

---

## 12. STARTUP / SHUTDOWN ORDERING

### 12.1 Startup (Reverse Topological)

```
1. T3 (RocksDB local cache)      — local, no deps
2. T2 client                      — needed for lease/coordination
3. T1 client                      — needed for state
4. Metrics + health endpoints     — observability online before work starts
5. Adapter (campaign for lease)   — if leader, begin CB subscription
6. Registry layer                 — opens T1 watches lazily on first Acquire
7. API gateway                    — accept device registrations
8. (Per device registration: HDO spawned)
9. (Per ENI announcement: NicActor spawned)
10. Reconciliation sweeper        — last (depends on everything else being healthy)
```

### 12.2 Shutdown (Forward Topological)

Inverse of startup:
```
1. API gateway: stop accepting new registrations (503)
2. Reconciliation sweeper: stop
3. Drain NicActors: wait up to 30s for in-flight PROGRAMMING to complete
4. Drain HDO actors: deregister devices, release registry refs
5. Registry layer: close all T1 watches
6. Adapter: stop CB streams, resign lease
7. T1/T2 clients: close gRPC connections
8. T3: flush and close RocksDB
9. Process exit
```

**Hard timeout:** 60s total shutdown budget. Exceeded → SIGKILL self.

### 12.3 Drain vs. Crash

- **Graceful drain (SIGTERM):** Above sequence; in-flight programming completes
- **Crash (panic, OOM):** Restarts; HA layer promotes peer pod; affected actors recover via reconciliation

---

## 13. MEMORY BUDGET (PER POD)

| Component | Steady (MiB) | Peak (MiB) | Notes |
|-----------|--------------|------------|-------|
| Goroutine stacks (P1 + P3) | 80 | 130 | 35k goroutines × 2-4 KB |
| Registry cache (in-mem) | 1,200 | 4,000 | 4 GiB hard cap |
| Actor mailboxes | 100 | 200 | Pre-allocated channel buffers |
| Southbound RPC buffers | 100 | 256 | 8k concurrent × 32 KB |
| T3 RocksDB block cache | 1,024 | 1,024 | Fixed config |
| T1/T2 client buffers | 64 | 128 | gRPC pools |
| Metrics + misc | 64 | 128 | Prometheus client |
| **TOTAL** | **~2.6 GiB** | **~6 GiB** | Pod limit: 8 GiB |

If total exceeds 7 GiB → GC pressure → admission throttle (see §9.4).

---

## 14. CONTEXT CANCELLATION DISCIPLINE

Every blocking operation MUST accept `context.Context`. Specifically:

- Registry `Acquire` returns immediately, but `<-sub.Ready` MUST be selected with ctx
- Southbound RPCs MUST use ctx-bounded timeouts (default 5s per command)
- Mailbox sends/receives MUST use `select { case ch <- v: case <-ctx.Done(): }`
- Lock acquisitions (long-held only) should be cancellable

**Anti-pattern (causes shutdown hangs):**
```go
v := <-channel              // BAD: ignores ctx
ch <- v                     // BAD: ignores ctx
mutex.Lock()                // OK only if hold duration is bounded (μs)
```

---

## 15. METRICS (REQUIRED)

```
fm_pool_goroutines{pool}                              gauge
fm_actor_count{class="HDO|CO|NIC|ASSEMBLER"}          gauge
fm_actor_mailbox_depth{class}                          gauge (histogram quantiles)
fm_actor_mailbox_dropped_total{class}                  counter (ALERT: > 0)
fm_actor_panic_recovered_total{class}                  counter (ALERT: > 0)
fm_pool_saturation{pool}                               gauge (in-use / capacity)
fm_lock_wait_duration_ms{lock_id}                      histogram
fm_lock_held_duration_ms{lock_id}                      histogram
fm_gc_heap_inuse_bytes                                 gauge
fm_admission_rejected_total{reason}                    counter
fm_shutdown_duration_ms{phase}                         histogram
```

---

## 16. TEST MATRIX

| Test | Behavior Asserted |
|------|-------------------|
| THR-001 | 50k goroutines steady state, no stack overflow |
| THR-002 | Mailbox overflow → drop oldest, metric increments |
| THR-003 | Lock order enforced: deadlock detector finds no violations |
| THR-004 | Panic in NicActor → quarantine, pool unaffected |
| THR-005 | 3 panics in 10min → permanent quarantine |
| THR-006 | Shutdown completes in <60s under load (8k active NICs) |
| THR-007 | Context cancellation propagates through all blocking ops |
| THR-008 | Memory cap 8 GiB never exceeded over 24h soak |
| THR-009 | Admission rejects new ENIs when at 45k+ |
| THR-010 | Registry watcher count = sum(Acquired keys) (no leaks) |

---

## 17. REFERENCES

- `registry-semantics-exact.md` §8 — Registry lock model (L1, L2)
- `southbound-driver-interface-redesign.md` §4 — Wave concurrency (P2 sizing)
- `adapter-protocol-design.md` §4 — Leader lease (L7)
- `device-lifecycle-design.md` §3 — HDO states drive actor lifecycle
- `error-handling-design.md` §10 — Admission control rules
- `MASTER_DESIGN_INDEX.md` — Canonical pool/lock/state name registry
