# 08 — Reconciliation, Storage & Warm Restart

> Every object in DashFabric runs **three loops**. The fast one reacts to
> upstream intent; the medium one verifies the device is alive; the slow one
> proves the device's actual state matches intent — and corrects drift. This
> document describes those loops, the local storage that backs them, and the
> warm-restart contract.

---

## 1. The Three-Loop Model

```
                          ┌─────────────────────────────┐
                          │     Actor goroutine         │
                          │                             │
       Upstream  ─────►   │  Event Loop (fast)          │
       (etcd)             │     • dispatcher → mailbox  │
                          │     • FSM transitions       │
                          │     • HAL.Apply             │
                          ├─────────────────────────────┤
       Scheduler ─────►   │  Liveness Loop (medium)     │
                          │     • probe device          │
                          │     • declare alive/dead    │
                          ├─────────────────────────────┤
       Scheduler ─────►   │  Reconcile Loop (slow)      │
                          │     • HAL.Get → diff        │
                          │     • HAL.Apply if drift    │
                          │     • emit drift metrics    │
                          └─────────────────────────────┘
                              │
                              ▼
                        BadgerDB WAL
```

### 1.1 Why Three (and not one giant loop)
- **Different latencies, different SLOs.** Intent should propagate in <500 ms;
  reconcile every few minutes; liveness every few seconds.
- **Different blast radius.** A reconcile error must not stall intent
  propagation; we want them on independent timers.
- **Different observability.** Operators ask "is intent flowing?", "is the
  device alive?", "are we drifting?" as separate questions; metrics map 1:1.

### 1.2 Event Loop
- **Trigger:** ConfigBus dispatcher pushes `MsgConfigEvent`.
- **Cadence:** as fast as events arrive (target < 100 ms per event in
  steady-state).
- **Actions:** parse Spec → compile GoalState → if Hash differs from
  last → `HAL.Apply` → record outcome → WAL flush.
- **Failures:** retry with exponential backoff; respect FSM (PROGRAMMING vs
  RECONCILING semantics).

### 1.3 Liveness Loop
- **Trigger:** scheduler tick every 10–30 s (jittered per object).
- **Action:** lightweight probe (gNMI Get on `/system/health` or rely on
  Subscribe heartbeat). Updates the object's `lastSeen` field.
- **On miss:** after K=3 missed probes, declare device dead; HDO transitions
  toward `DRAINING` (configurable: hold for grace period before destroying).
- **Cost:** O(1) RPC per device per tick → negligible.

### 1.4 Reconcile Loop
- **Trigger:** scheduler tick (default 60–300 s per object, jittered).
- **Action:**
  1. `HAL.Get` snapshot of the actual device state for this object's paths.
  2. `hal.ComputeDrift(goal, actual)` → `DriftReport`.
  3. If drift is empty → emit success metric, done.
  4. Else → schedule a `RECONCILING` FSM transition; `HAL.Apply` the
     corrective set; emit drift metric with severity.
- **Cost:** dominated by `HAL.Get`. We optimize by:
  - Selecting only **owned subtree** paths.
  - Sharing one `Get` across siblings when feasible (the per-host worker
    pool batches Gets).
  - Backing off the reconcile interval if recent events on this object are
    recent (object is "warm").

### 1.5 Schedulers
- Each shard runs a small priority-queue scheduler per actor type, with
  jitter ±25 % on intervals to spread DPU load.
- Per-host **fairness lock** prevents the reconcile of all 32 ENIs on the
  same DPU from blasting at once.

---

## 2. Drift Severity Model

Drift falls into three severity buckets that affect alerting and
reconciliation aggressiveness:

| Severity | Examples | Default action |
|---|---|---|
| `INFO` | Counter values, ephemeral state lines | Log only |
| `WARN` | Missing optional metadata, harmless misordering | Auto-correct on next reconcile; log |
| `CRITICAL` | Wrong ACL rule, missing route, wrong VNET mapping | Auto-correct **immediately** (don't wait for next tick); alert if recurring |

The classifier lives in the HAL (it knows which leaves are intent-bearing
vs. observed metadata).

---

## 3. Why Reconcile at All?

The upstream PubSub is authoritative; in theory the event loop is enough.
In practice:

- **Device side-effects.** A firmware bug, an operator mistake, an
  out-of-band CLI command, or a partial-write recovery scenario can leave
  the device misaligned.
- **Lost events.** Watch coalescing or compaction-window loss may skip
  intermediate revisions.
- **ISSU windows.** Brief gaps during pod restarts.
- **Initial program after a long device outage.** When a device returns,
  reconcile catches it up to current intent without any operator action.

The reconcile loop is the safety net that makes DashFabric **self-healing**.

---

## 4. Storage Layout

### 4.1 BadgerDB Layout
Per pod, on the PVC, BadgerDB at `/var/lib/dashfabric/wal/`:

```
/wal/shard-<id>/
    meta/
        version           = "v1.3.0"
        last_applied_rev  = uint64
        shard_id          = string
    objects/
        <objectID>        = ActorSnapshot (proto)
    intents/
        <rev>             = QueuedIntent (proto)   # bounded backlog
    devices/
        <hostID>/state    = DeviceCachedState (proto)  # last Get snapshot
        <hostID>/health   = HealthCounters
    pending_calls/
        <uuid>            = PendingHalCall (proto)
    metrics/
        last_flush_ts     = ts
```

Why BadgerDB:
- Pure Go (no cgo) — easier cross-platform builds and CI.
- MVCC + WAL semantics suitable for our cache + checkpoint pattern.
- LSM-tree compaction handles write-heavy WAL workloads.
- Battle-tested in Dgraph and many other production systems.

### 4.2 Compaction Strategy
- **Snapshot compaction**: every 5 minutes, write a consolidated snapshot
  for each actor; truncate `intents/` entries older than the snapshot.
- **TTL on pending_calls**: 1 hour. Forgets calls that never resolved (the
  reconcile loop will rediscover and re-fix).
- **Vacuuming**: nightly Badger value-log GC, triggered by `WithGcThreshold(0.5)`.

### 4.3 Disk Sizing
- 2,000 hosts × ~25 ENIs avg × ~8 KiB snapshot = **400 MiB** for object
  snapshots.
- Per-device cached state (route blobs, mapping tables): cached **out-of-WAL**
  as content-addressable blobs in
  `/var/lib/dashfabric/blob/<sha256>` to avoid Badger pressure.
- Recommended PVC: **50 GiB NVMe** per pod (headroom for log retention and
  bulk blob cache).

### 4.4 Why Not Just Memory?
- Cold-start time matters. A 2k-device shard with cold WAL takes ~30 s to
  warm; cold without WAL (snapshot bootstrap from etcd) can take several
  minutes.
- WAL-recorded `pending_calls` prevent duplicate gNMI Sets after a crash.
- Operator forensics: post-incident, the WAL is dumpable for offline
  analysis (`dfctl wal dump`).

---

## 5. Warm Restart Contract

The warm-restart contract guarantees:

1. **No programming gap** for existing intent during pod restart (other
   pods in the ShardSet keep actuating; HA covers this).
2. **Local in-memory state** rebuilt within `walReplayBudget` (default 30 s)
   for a 2k-device shard.
3. **No duplicate destructive operations** (delete-then-recreate) caused
   by replay.

### 5.1 Replay Procedure
```
pod starts
  │
  ▼
open BadgerDB; acquire process lock
  │
  ▼
read meta.last_applied_rev
  │
  ▼
for each /wal/objects/<id>:
     instantiate actor in snapshot.fsm_state
     register in shard's actor map
  │
  ▼
for each /wal/pending_calls/<uuid>:
     mark target actor for "verify-and-retry-on-Primary"
  │
  ▼
open etcd watch starting at last_applied_rev + 1
  │
  ▼
drain catch-up backlog → actors advance FSM
  │
  ▼
mark /readyz = 200
```

### 5.2 Pending Call Semantics
- Each actor in `PROGRAMMING` state with a pending call:
  - **Standby**: re-validate goalState in shadow mode; if hash matches,
    no action.
  - **Primary on acquisition**: re-issue the call (idempotent). Device
    returns success and the actor advances.
- Worst case: a single duplicate Set RPC per pending call. DASH idempotency
  makes this safe.

### 5.3 Inconsistency Detection
If a snapshot says `PROGRAMMED` but the device's current state disagrees
(measured by the first post-restart reconcile), we treat it as a regular
drift event — the system self-heals.

---

## 6. Bulk Blob Cache

Routes and mappings are stored as **bulk blobs** in object storage (S3-
compatible), referenced by `(sha256, version)` in etcd values. The local
blob cache:

```
/var/lib/dashfabric/blob/<sha256-prefix>/<sha256>
```

- Content-addressed; immutable.
- LRU eviction with 8 GiB cap per pod.
- On `RouteGroup` NO actor: read the blob ref from etcd, fetch from cache
  or download, parse into in-memory routes, hand to HAL.
- Background prefetcher: on shard ownership change (split/merge), warm
  the cache from object storage in parallel.

This pattern keeps etcd small, lets us version routes immutably, and gives
us a free CDN integration point if needed.

---

## 7. Reconcile Cadence Tuning

| Object class | Default reconcile period | Why |
|---|---|---|
| `HDO` (device-level) | 300 s | Rarely changes, low impact |
| `CO` (container) | 180 s | Some VNET-scope state |
| `NO` (ENI) | 60 s | Bulk of intent; quickest drift detection |
| Bulk routes | event-driven only | Reconciled by hash comparison cheaply |
| Bulk mappings | event-driven only | Same — comparing 8M entries every 60 s is unaffordable |

Tunables live in `/policy/v1/global/reconcile-intervals`. Operators can
override per host class.

### 7.1 Storm Avoidance
- **Per-shard concurrent reconciles** capped (e.g., 100 active at once).
- **Per-host fairness**: at most 1 reconcile in flight per device.
- **Jitter**: ±25 % on every scheduled tick.

---

## 8. Operator Tooling Hooks

The `dfctl` CLI (defined in §09) interacts with reconciliation via:

```
dfctl reconcile now <objectID>       # force immediate reconcile
dfctl reconcile pause <objectID>     # operator override; alert if held > 1h
dfctl drift report --host <HostID>   # summary of all current drift
dfctl wal dump --host <HostID>       # offline forensics
```

All of these go through actor mailboxes — operator commands never touch
device state directly.

---

## 9. Failure Modes Specific to Storage

| Mode | Detection | Response |
|---|---|---|
| BadgerDB corrupt | open returns error | refuse to start; emit alert; operator runs `dfctl wal repair` (rebuilds from etcd) |
| PVC full | write returns ENOSPC | emit critical alert; drain incoming events; do not crash |
| Slow disk | flush latency > 100 ms | metric `dashfabric_wal_flush_seconds`; alert at sustained > 200 ms |
| Lost PVC (node loss) | pod reschedules to new node | fresh pod boots without WAL → snapshot bootstrap from etcd (slower but works) |
| Replay schema mismatch | meta.version newer than binary | refuse to start; require upgrade |

---

## 10. Performance Budgets

| Operation | Budget | Notes |
|---|---|---|
| Single event → device-ACK | p99 ≤ 500 ms | end-to-end |
| Reconcile cycle for one NO | p99 ≤ 5 s | dominated by gNMI Get |
| WAL flush latency | p99 ≤ 50 ms | Badger value-log sync |
| Snapshot write per actor | p99 ≤ 10 ms | proto marshal + put |
| Cold start (WAL replay, 2k devices) | p99 ≤ 30 s | parallel actor spawn |
| Cold start (snapshot bootstrap, 2k devices) | p99 ≤ 5 min | bounded by etcd Get throughput |

---

## 11. Open Questions

| ID | Question | Default |
|---|---|---|
| OQ-801 | Use RocksDB for write throughput? | **No.** Badger is sufficient; cgo creates ops pain. |
| OQ-802 | Should reconcile pull telemetry from the existing Subscribe stream instead of fresh Get? | **Yes, where stream covers the path.** Falls back to Get for unsubscribed paths. |
| OQ-803 | Backup WAL to object storage? | **No.** WAL is a local cache; rebuild from etcd is the disaster recovery path. |
| OQ-804 | Adaptive reconcile cadence? | **Yes** (v2). Slow down for stable objects, speed up for known-flaky devices. |
