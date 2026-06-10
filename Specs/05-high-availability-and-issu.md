# 05 — High Availability, Replication & ISSU

> **Tenet:** DashFabric never loses programming continuity due to its own
> failure. ShardSet failover is sub-second; in-service software upgrades
> cause zero programming gap.

This document describes how 3-replica ShardSets achieve this without log
shipping, distributed consensus, or vendor-specific clustering.

---

## 1. The Core Insight

Most stateful services need replication because **the service itself is the
source of truth** for some state. Replicating Postgres, etcd, or Kafka means
solving distributed consensus on every write.

**DashFabric is different.** The source of truth lives **upstream** in the
config PubSub (etcd). DashFabric is a *projection* — a derived,
deterministic function of the upstream stream.

Therefore: any pod that consumes the same upstream stream in the same order
will converge to the same in-memory state. We do not need to ship logs
between replicas; we just need every replica to be a consumer.

This dramatically simplifies HA:
- **No leader-only writes to replicate.** Standbys consume the same stream.
- **No catch-up on failover.** Standbys are already current.
- **No split-brain risk on programming.** The Lease ensures only one
  Primary actuates; the others are observers.

---

## 2. ShardSet Topology

```
ShardSet S, e.g. shard-0007  (range [L, H))

Pod shard-0007-0           Pod shard-0007-1           Pod shard-0007-2
─────────────              ─────────────              ─────────────
etcd watch  ◄────── same revisions ──────►  etcd watch  ◄──── same ───►  etcd watch
   │                            │                            │
   ▼                            ▼                            ▼
HDO/CO/NO actor tree     HDO/CO/NO actor tree     HDO/CO/NO actor tree
(identical state)        (identical state)        (identical state)
   │                            │                            │
   ▼                            ▼                            ▼
HAL  (active)             HAL  (shadow)             HAL  (shadow)
   │
   ▼
Devices

         ▲                       ▲                            ▲
         └───────────── Race for Lease  /shardmap/v1/leases/shard-0007 ─────────┘
                                                                  └─ K8s coordination.k8s.io
```

- All 3 pods watch the same etcd prefixes.
- All 3 pods build the same actor tree.
- All 3 pods write to **their own** local BadgerDB WAL.
- Only the Lease holder actuates southbound; the others' HAL operates in
  **shadow mode** (validates payloads, increments shadow metrics, no RPCs).

---

## 3. Primary Election

### 3.1 Lease
We use Kubernetes' `coordination.k8s.io/v1` `Lease` resource (the same
primitive used by `kube-controller-manager` and many operators).

```yaml
apiVersion: coordination.k8s.io/v1
kind: Lease
metadata:
  name: shard-0007-primary
  namespace: dashfabric-system
spec:
  holderIdentity: shard-0007-1
  leaseDurationSeconds: 5
  renewTime: 2026-06-11T02:00:00Z
```

### 3.2 Timing Knobs
| Knob | Default | Notes |
|---|---|---|
| `leaseDurationSeconds` | 5 s | Failover budget |
| `renewIntervalSeconds` | 1.5 s | ~1/3 of duration; standard |
| `retryPeriodSeconds` | 0.5 s | How often candidates probe |
| `gracefulHandoffWindow` | 2 s | Primary releases lease on SIGTERM, waits before exit |

**Worst-case unplanned failover ≈ leaseDuration + replay time ≈ 5 s + ε.**
For 0-blackout planned failover (upgrade), we use **graceful handoff**: the
outgoing Primary writes `holderIdentity = ""` and a fresh candidate captures
it within ~50 ms.

### 3.3 Anti-Split-Brain
The Lease object is a single etcd row in the K8s API; the K8s API server
guarantees at-most-one holder at any moment. The HAL guards with a
**lease-fencing token** (Lease `resourceVersion`) attached to every gNMI
SetRequest:
```
gNMI SetRequest metadata:
   x-dfabric-shard:      shard-0007
   x-dfabric-fence-token: 42        # monotonic resourceVersion of the lease
```
DPUs that receive a SetRequest with a stale fence token (lower than the
last seen) reject it. This protects against the rare case where an
ex-Primary has not yet realized it lost the lease.

> **Note:** Fence-token enforcement on the device side is an *enhancement*
> to the gNMI server in the SONiC-DASH container. We propose this
> upstream; in absence of device support, the control-plane-side check
> (Lease still held before each RPC) is the fallback.

---

## 4. Standby Lifecycle

```
Pod start
  │
  ▼
Open ConfigBus (etcd) connections.
Restore WAL (BadgerDB) into in-memory actor tree.
  │
  ▼
Open etcd watches at lastRev recorded in WAL.
Process the catch-up backlog → actors converge.
  │
  ▼
Begin Lease participation (candidacy).
  │
  ▼  loop:
       if not Lease holder:
            HAL in shadow mode; consume events; update FSM; WAL.
       if just became Lease holder:
            HAL switches to active mode.
            Reissue any in-flight gNMI calls that the WAL marks UNCONFIRMED.
            Begin reconcile and liveness loops on devices.
       if just lost Lease (rare; only via fault):
            HAL switches back to shadow mode; cancel pending RPCs.
       sleep(retryPeriod)
```

### 4.1 Shadow Mode in Detail
- Mailbox handlers run normally.
- HAL.Apply(goalState) → builds the gNMI message, validates it locally, but
  **does not call** the transport layer. Instead it returns
  `ShadowResult{wouldSend: bytes, validatedOK: bool}`.
- The FSM still transitions through `PROGRAMMING` → `PROGRAMMED` based on
  shadow-validated success, so the FSM is identical on all replicas.
- Metric `dashfabric_shadow_apply_total{result}` tracks shadow throughput.

### 4.2 Catch-up Window
On boot, a standby may be hours behind (after a long downtime). It:
1. Reads `lastAppliedRev` from WAL.
2. Compares to etcd's `currentRev`.
3. If gap > compactRev → switches to **snapshot bootstrap** (List + replay).
4. Otherwise replays from lastAppliedRev.

While catching up, the pod's `/readyz` returns 503; Lease participation is
disabled. Once caught up (gap < 1 s for 3 consecutive seconds), `/readyz`
returns 200 and the pod joins Lease candidacy.

---

## 5. Failover Flows

### 5.1 Planned: Graceful Handoff (e.g. for ISSU)
```
1. Operator triggers pod restart (e.g. via OnDelete strategy).
2. K8s sends SIGTERM to Primary pod shard-0007-1.
3. preStop hook (5 s grace) runs:
     a. Pause new HAL dispatches.
     b. Wait for in-flight gNMI calls to complete (cap at 2 s).
     c. Release Lease (write holderIdentity = "", resourceVersion bump).
     d. Flush BadgerDB.
4. shard-0007-0 (a Standby) sees the released Lease via watch within ~50 ms.
5. shard-0007-0 acquires Lease; HAL goes active; resumes dispatching.
6. shard-0007-1 exits 0; K8s starts new container with new image.

Programming gap = ~50 ms ≈ 0.
```

### 5.2 Unplanned: Pod Crash
```
1. Primary pod crashes; SIGKILL.
2. Lease not renewed.
3. After leaseDuration (5 s), etcd marks Lease expired.
4. Standby pods see expiry and race; one wins.
5. New Primary's actor tree is already current.
6. WAL may show some UNCONFIRMED gNMI calls → reissue (idempotent).

Programming gap = ≤ 5 s + ε (target ≤ 2 s with tuned timings).
```

### 5.3 Unplanned: Node Crash
```
Similar to pod crash; additionally K8s reschedules the pod.
Other 2 pods are unaffected (different nodes via topologySpread).
```

### 5.4 Unplanned: AZ Failure
```
If pods are spread across 3 AZs, losing 1 AZ loses 1 pod.
Quorum (2 of 3) preserved; if the lost pod was Primary, the same
flow as 5.2 applies; Lease moves to a surviving pod in another AZ.
```

### 5.5 Unplanned: etcd Cluster Outage
```
All pods lose ConfigBus connectivity.
Lease cannot be renewed → Lease expires → all pods lose Primary status.
HAL is disabled; existing device programming continues to operate
   (the data plane is independent).
Reconcile loop is suspended (cannot reach intent).
Metric dashfabric_configbus_down_seconds rises; high-severity alert.
When etcd returns, pods resume Lease participation and catch up.

Programming gap = 0 for existing state; new intent is delayed by outage
duration. This is the correct behavior — we fail static, not fail open.
```

### 5.6 Unplanned: Device Disconnects
The device, not DashFabric, is down. Covered in `11-failure-modes`.

---

## 6. In-Service Software Upgrade (ISSU)

### 6.1 Goals
- **Zero programming gap** for existing intent.
- **Bounded delay** (≤ 5 s per pod) for fresh intent.
- **Online** — no maintenance window.
- **Schema-compatible** — old and new versions coexist for ≥ 1 minor release.

### 6.2 Procedure (Rolling Update with OnDelete Strategy)

For each shard's 3 pods, serially:

```
1. Operator (or CD pipeline) calls:
     kubectl delete pod shard-0007-2          # Standby first
2. K8s scheduler creates new pod with new image, mounts same PVC.
3. New pod boots, restores WAL, joins ShardSet as Standby.
4. /readyz=200 → Lease candidacy enabled.
5. Wait for `readinessGate` to confirm catch-up.
6. Repeat for shard-0007-0 (other Standby).
7. Repeat for shard-0007-1 (Primary) — graceful handoff first.
```

### 6.3 Forward/Backward Compatibility
- WAL schema is **versioned**. Old binary refuses to load WAL from a newer
  schema (fails fast); always upgrade old → new.
- Proto schemas are buf-linted; field additions OK, removals
  reserved for 2 releases.
- gNMI schema versions detected via gNMI `Capabilities` RPC; HAL codec
  picks the right one per device.

### 6.4 Canary
A subset of ShardSets (e.g., 1 of 10) is upgraded first and observed for
4 hours. Metrics auto-rollback gates:
- `dashfabric_hal_error_rate{shard}` > 2× baseline
- `dashfabric_program_latency_p99{shard}` > 2× baseline
- Any new `FAILED` actors not present pre-upgrade

If gates trip, the rolling update is **paused** and operator-resumed.

---

## 7. Crash-Recovery Semantics

### 7.1 WAL Replay
On any pod restart, the recovery sequence is:

```
1. Open BadgerDB; lock.
2. Read each /wal/shard-<id>/objects/<objectID> snapshot.
3. Spawn an actor in the recorded FSM state.
4. For actors in PROGRAMMING or RECONCILING with UNCONFIRMED HAL call:
     mark for re-issue on becoming Primary.
5. Replay any pending /wal/shard-<id>/intents/<rev> entries
   (events received but not yet handled at crash time).
6. Move to live etcd consumption.
```

### 7.2 Pending HAL Calls
The HAL records, before each RPC, a `pending_call_<uuid>` entry. On
completion (ACK or error), the entry is removed. On restart, any pending
entries are re-tried under the same call ID. Idempotency at the device
(DASH semantics) ensures correctness.

### 7.3 Data Loss Boundaries
- **Volatile state lost** between WAL flushes: at most `walFlushInterval`
  (default 100 ms) of unconfirmed events.
- **Recovery action:** the dispatcher will re-deliver any events with
  `ModRevision > lastAppliedRev` from etcd's history (within compaction
  window).

---

## 8. Multi-Region & Disaster Recovery

### 8.1 No Cross-Region State
DashFabric does **not** replicate state across regions. Each region is
independent:
- Its own etcd cluster.
- Its own ShardSets.
- Its own PM.

### 8.2 Region Loss
If a region is destroyed, its devices are dead anyway (they're physical
servers in that region). When a region comes back, devices re-register and
the system rebuilds from upstream intent.

### 8.3 Cross-Region Operator Plane
A global operator tool (`dfctl`) talks to per-region API endpoints; there
is no global state to coordinate.

---

## 9. SLO Targets

| Metric | Target | Rationale |
|---|---|---|
| Unplanned failover time | ≤ 2 s p99 | Tight enough that operators don't notice |
| Planned failover time | 0 ms (graceful handoff) | Zero ISSU impact |
| Pod restart RTO (WAL restore) | ≤ 30 s for 2k devices | Cold-start budget |
| Pod restart RTO (snapshot bootstrap from etcd) | ≤ 5 min for 2k devices | Worst-case after long downtime |
| Probability of double-actuation | 0 (with fence tokens) ; ≤ 0.01 % (without) | Lease + fence enforcement |
| etcd outage tolerance (no program degradation) | ≥ 1 hour | Steady state runs from local FSM |

---

## 10. Implementation Notes

### 10.1 Library Choices
- `client-go` for `coordination.k8s.io/v1` Lease (battle-tested).
- `etcd-io/etcd/client/v3` for ConfigBus.
- `dgraph-io/badger/v4` for WAL/cache.
- Custom Lease wrapper provides:
  - `OnAcquired(func)`, `OnLost(func)` callbacks.
  - Fence-token observation.
  - Crash-safe renewal goroutine.

### 10.2 Observability Hooks
- Span `lease.transition` on every acquire/lose.
- Metric `dashfabric_primary_pod{shard}` = pod hostname (gauge with label).
- Metric `dashfabric_failover_seconds` histogram.
- Audit log entry on every Primary change (with reason).

---

## 11. Testing Strategy

| Test | What it verifies |
|---|---|
| Chaos: kill -9 Primary pod every 60 s for 1 h | Failover holds; no FAILED actors; no double-RPCs |
| Chaos: kill etcd for 5 min | Fail-static; recovery is graceful; no spurious deletes |
| Chaos: 10× event burst into one shard | Coalescing keeps mailboxes bounded; no OOM |
| ISSU: rolling upgrade of 100-shard region | 0 programming gap measured; canary gates exercised |
| Recovery: cold start of fresh pod with full WAL | RTO within budget |
| Recovery: fresh pod with empty WAL (snapshot bootstrap) | RTO within budget for worst case |
| Fence: stale Primary re-issues RPC after Lease loss | Device rejects; no double-actuation |

---

## 12. Open Questions

| ID | Question | Default |
|---|---|---|
| OQ-501 | 3-replica per shard sufficient? Or 5? | **3**. We don't need byzantine tolerance; 3 covers single-AZ loss. |
| OQ-502 | Use Raft (e.g. dragonboat) inside shard for stronger consistency? | **No**. Source of truth lives upstream; consensus inside shard adds latency for no benefit. |
| OQ-503 | Should fence tokens be standardized into DASH gNMI schema? | **Yes** — file as an upstream proposal to sonic-net/DASH. |
| OQ-504 | Can we run 2 replicas (Primary + Standby) instead of 3 for cost? | **Permitted via config flag**, with explicit operator acknowledgment that AZ loss may degrade. Default remains 3. |
