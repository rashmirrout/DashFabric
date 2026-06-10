# 02 — Domain Model & Object Lifecycle

> The DashFabric core is an **actor model**. Each managed concept on a DPU is a
> living, single-writer, single-mailbox actor with a deterministic finite-state
> machine, an etcd watch, and a HAL handle.

This document defines the object types, their FSMs, the rules of hierarchy
(creation, destruction, reparenting), the message protocol between actors, and
the invariants the runtime preserves.

---

## 1. Object Taxonomy

```
HostDeviceObject  (HDO)        1:1 with a DPU registration
└── ContainerObject (CO)       1:N children — one per VM / workload slot
    └── NICObject  (NO)        1:N children — one per ENI / vPort
```

### 1.1 HostDeviceObject (HDO)
Represents one registered, physical (or virtual) DPU as observed by
DashFabric.

**Identity:** `HostID` = the device-issued unique GUID asserted in its mTLS
cert SAN.

**Owns:**
- The persistent gNMI session to the DPU.
- Device-level attributes (DEVICE_METADATA, BGP underlay reference, region/AZ
  hints, capability matrix, firmware version).
- The lifetime of all its child Containers.
- A heartbeat watchdog: detects DPU disappearance independently of the
  Northbound Gateway.

**Subscribes to:** `/config/v1/hosts/<HostID>` *and*
`/config/v1/hosts/<HostID>/`  (prefix).

### 1.2 ContainerObject (CO)
Represents a single VM, container, pod, or arbitrary tenant workload slot
hosted on a DPU.

**Identity:** `(HostID, ContainerGUID)`. ContainerGUID is assigned by upstream.

**Owns:**
- Per-container metadata (TenantID, region, billing tags).
- VNET membership references that are reused across this VM's NICs.
- An optional per-VM ACL pre-stage (some DPUs expose subnet-level ACLs).
- Lifetime of all child NICs.

**Subscribes to:** `/config/v1/hosts/<HostID>/<ContainerGUID>` and prefix.

**Note:** The "Container" name is chosen for generality. In DASH terminology
this most often corresponds to a **VM with one or more ENIs**, but the model
deliberately allows other workload taxonomies.

### 1.3 NICObject (NO)
Represents a single **ENI** (DASH "Elastic Network Interface" /
"Virtual Port"). This is where the bulk of DASH programming lives.

**Identity:** `(HostID, ContainerGUID, NicID)`. `NicID` is upstream-assigned
(usually a tenant-meaningful name like `nic-primary`, `nic-storage`, etc.).

**Owns:**
- The full DASH ENI subtree:
  - `DASH_ENI_TABLE` entry
  - `DASH_VNET` association
  - `DASH_ACL_GROUP_TABLE` + `DASH_ACL_RULE_TABLE` (inbound + outbound, all 3
    stages: VNIC, Subnet, VNET — see DASH HLD §1.7)
  - `DASH_ROUTE_GROUP_TABLE` + `DASH_ROUTE_TABLE` (outbound LPM routes)
  - `DASH_VNET_MAPPING_TABLE` (CA-PA mappings) — *typically* shared per VNET
    but owned by NO in our model when the upstream scopes them per-ENI
  - `DASH_METER_POLICY` + `DASH_METER_RULE` bindings
  - `DASH_TUNNEL` definitions if used (overlay encap variants)
  - DASH inbound routing rules (`DASH_INBOUND_ROUTING_RULE`)
  - HA pairing references (`DASH_HA` peer card ID, ENI active/passive role)
- The compiled `GoalState` blob and its content-hash.
- Drift counters and last reconcile time.

**Subscribes to:** `/config/v1/hosts/<HostID>/<ContainerGUID>/<NicID>` and
prefix.

**No further children** — NO is a leaf.

---

## 2. The Actor Contract

Every actor in DashFabric conforms to the same internal contract:

```go
// Pseudocode — see 07 for the real interface
type Actor interface {
    ID() ObjectID
    Mailbox() chan<- Message
    Run(ctx context.Context) error      // single goroutine, single writer
    Snapshot() ActorSnapshot            // for WAL & diagnostics
    Restore(s ActorSnapshot) error      // for warm restart
}
```

Rules:
1. **Single writer.** The only goroutine that mutates an actor's state is its
   own `Run` loop. All inputs come through the mailbox.
2. **Bounded mailbox.** Default 1024 messages. Overflow triggers coalescing
   (latest-wins per key) and a metric.
3. **No blocking I/O on the actor goroutine.** Long ops (gNMI Set, BadgerDB
   sync) are dispatched to a worker pool; the actor receives a future-completed
   `Result` message.
4. **No shared mutable state with siblings.** Cross-object data is exchanged
   by message-passing only.
5. **Idempotent message handling.** Every handler must tolerate replay of any
   message it has already processed.
6. **WAL before action.** Every state transition is written to BadgerDB
   *before* the corresponding side effect is dispatched. Required for warm
   restart determinism.

---

## 3. Message Protocol

Actors exchange a small typed envelope:

```go
type Message struct {
    TraceCtx     trace.SpanContext   // OTel; propagates from upstream event
    Kind         MessageKind
    Payload      any                 // strongly typed per Kind
    DeadlineNs   int64
}

type MessageKind int
const (
    MsgConfigEvent      MessageKind = iota  // from etcd watch
    MsgReconcileTick                        // from scheduler
    MsgLivenessProbe                        // from scheduler
    MsgChildReport                          // from a child actor
    MsgParentOrder                          // from parent — usually DESTROY
    MsgHALResult                            // gNMI RPC outcome
    MsgWALCompacted                         // bookkeeping
    MsgAdminCommand                         // operator CLI
)
```

`MsgChildReport` and `MsgParentOrder` form the **upward** and **downward**
control plane between actors, see §6.

---

## 4. Finite-State Machine

Every actor type uses **the same canonical FSM** with a few type-specific
sub-states for clarity. Keeping the FSM uniform makes operator tooling and
tests simpler.

```
                  ┌──────────────┐
                  │   CREATED    │     allocator returned; no IO yet
                  └──────┬───────┘
                         │ MsgConfigEvent: SPEC arrived OR Restore() from WAL
                         ▼
                  ┌──────────────┐
                  │  CONFIGURING │     compile GoalState; precondition checks
                  └──────┬───────┘
                         │ valid spec & parent in PROGRAMMED
                         ▼
                  ┌──────────────┐
                  │  PROGRAMMING │     HAL Set RPC in flight
                  └──────┬───────┘
              ACK ok ┌───┴──────┐  non-retryable error
                     ▼          ▼
              ┌────────────┐  ┌──────────────┐
              │ PROGRAMMED │  │  FAILED      │   alerts + manual unblock
              └─────┬──────┘  └──────────────┘
                    │
                ┌───┴──── DRIFT detected by reconcile
                │
                ▼
          ┌──────────────┐
          │ RECONCILING  │   diff-based Set RPC
          └──────┬───────┘
                 │ ACK ok
                 ▼ back to PROGRAMMED
                 │
   MsgConfigEvent: SPEC withdrawn  OR  MsgParentOrder: DESTROY
                 │
                 ▼
          ┌──────────────┐
          │  DRAINING    │   order children to DESTROY; HAL Delete RPC
          └──────┬───────┘
                 │ all children TERMINATED + HAL ack
                 ▼
          ┌──────────────┐
          │ TERMINATED   │   send MsgChildReport(DONE) to parent; exit goroutine
          └──────────────┘
```

### 4.1 Substate detail: PROGRAMMING vs RECONCILING
Both call HAL; the difference is **semantics** and **metric labels**:
- `PROGRAMMING` is intent-driven (event from upstream).
- `RECONCILING` is drift-driven (timer + diff).

This keeps the dashboards clean (you can see "are we drifting?" separately from
"how fast is intent propagating?").

### 4.2 FAILED → recovery
- An object lands in `FAILED` only on **non-retryable** errors (e.g., DASH
  schema violation rejected by gNMI). Retryable errors stay in `PROGRAMMING`
  with exponential backoff.
- `FAILED` is sticky and emits an alert. Operators clear it with `dfctl
  object reset <id>` which transitions back to `CONFIGURING`.

### 4.3 Crash / restart determinism
- WAL stores FSM transitions: `(t, objectID, from, to, payloadHash)`.
- On restart, the actor restores the last recorded state.
- If we crashed *during* `PROGRAMMING`, we resume in `PROGRAMMING` and replay
  the HAL call (idempotent).
- See `08-reconciliation-storage-and-warm-restart.md`.

---

## 5. Hierarchy Invariants

These are **strict invariants** preserved by the runtime. Any code path
that violates them is a bug.

1. **A child cannot exist without a live parent.**
2. **A child is created only on parent's order** (after parent reaches
   `PROGRAMMED`). The parent issues `MsgParentOrder(CREATE, spec)` to a
   newly-spawned actor.
3. **A parent cannot finish DRAINING until all children are TERMINATED.**
4. **A child sends `MsgChildReport(DONE)` to parent on TERMINATED.**
5. **Sibling order is irrelevant.** Two NOs under the same CO are independent.
6. **Re-registration of the parent device wipes the entire subtree** (per user
   requirement; see §7 below).

These invariants give us:
- Trivial garbage collection: when an actor's `Run` exits, all its goroutines,
  mailboxes, and watch handles GC naturally.
- Trivial blast-radius reasoning: failure of one NO cannot stall its siblings.

---

## 6. Creation, Destruction, and Reparenting Flows

### 6.1 Creation

```
1. Parent receives MsgConfigEvent for path /config/v1/hosts/H/C/N (new key).
2. Parent decides: this is a new child.
3. Parent calls runtime.SpawnChild(NICObject, NO_ID(H,C,N), initialSpec).
4. Runtime:
     a. Allocates goroutine + mailbox.
     b. Writes "actor CREATED" record to WAL.
     c. Opens etcd watch on the child's prefix.
     d. Sends MsgParentOrder(CREATE, spec) into child mailbox.
5. Child runs its FSM: CREATED → CONFIGURING → PROGRAMMING → PROGRAMMED.
6. Child notifies parent via MsgChildReport(PROGRAMMED).
```

The parent **does not block** waiting for the child. The parent simply records
the child in its `children` map and moves on. The asynchrony is essential for
parallelism: a CO with 8 NICs spawns them concurrently.

### 6.2 Destruction — initiated by upstream withdrawal

```
1. NO receives MsgConfigEvent(DELETE) on /config/v1/hosts/H/C/N.
2. NO FSM: PROGRAMMED → DRAINING.
3. NO has no children → directly calls HAL.Delete(eniSubtree).
4. On HAL ACK → FSM: TERMINATED.
5. NO sends MsgChildReport(DONE) to CO.
6. CO removes the NO from its child map.
7. CO writes WAL "child terminated".
8. If CO's own key is also gone and children map is empty → CO drains itself.
```

### 6.3 Destruction — cascading from a parent

```
1. HDO receives MsgConfigEvent(DELETE) on /config/v1/hosts/H.
   (Or device unregisters; see §7.)
2. HDO FSM: PROGRAMMED → DRAINING.
3. HDO sends MsgParentOrder(DESTROY) to each child CO.
4. Each CO recursively sends DESTROY to its NOs and itself drains after all
   children TERMINATED.
5. When all COs TERMINATED, HDO calls HAL.Delete(device-level resources),
   closes gNMI session, transitions to TERMINATED.
6. HDO clears its WAL (re-registration semantics).
```

### 6.4 Out-of-order events

A NIC key may arrive *before* its parent CO's key (race in upstream writes,
network reordering, or watch resumption from a stale revision).

**Rule:** the runtime buffers child events until the parent reaches
`PROGRAMMED`. Specifically:
- The shard's **etcd watch dispatcher** routes events by deepest existing
  ancestor.
- If no parent exists yet, the event is parked in a **bootstrap buffer**
  keyed by parent prefix.
- When a parent is spawned and reports `PROGRAMMED`, the dispatcher flushes
  the buffer to it; the parent then spawns the children.

A bounded bootstrap buffer (default 1 MiB per device) keeps memory finite.
Overflow → drop oldest with metric + warning; the next reconcile cycle will
re-fetch by full snapshot of the prefix.

### 6.5 Reparenting

Reparenting (moving a NIC to a different VM/Container) is **not modeled as
mutation**; it is **delete + create**. This matches DASH's idempotency
model where the SAI/gNMI controllers expect full reprogramming on
identity changes.

---

## 7. Registration and Re-registration Semantics

Per user requirement:

> "A device can unregister itself, in such case the whole Device host Object
> is destroyed which destroy the hierarchy which withdraw all the config. A
> re-register is a completely new flow with no past data persistance."

**Specified behavior:**

1. **Unregister trigger**: either explicit gRPC `Unregister(DeviceID)` from
   the DPU, OR expiration of the device's etcd presence lease, OR Northbound
   Gateway declares dead after `K * heartbeatInterval` missed beats
   (default K=3).
2. On unregister:
   - HDO transitions to `DRAINING` with reason = `UNREGISTER`.
   - Cascading destruction proceeds as in §6.3.
   - HDO additionally **deletes its WAL subtree** in BadgerDB.
   - HDO records `/state/v1/hosts/<HostID>/last-seen` with timestamp, but
     does **not** retain any GoalState.
3. On a subsequent register from the same DeviceID:
   - NBG treats it as a brand new device.
   - A fresh HDO is spawned.
   - No state inherited; full re-derivation from current upstream intent.

**Implication for upstream:** the upstream must be prepared to re-publish the
device's intent on registration. We give upstream a hook:
`/event/v1/registrations/<HostID>/registered` (lease-bound key) that the
upstream can watch as the signal to (re-)publish.

---

## 8. Concurrency and Parallelism

| Concern | Approach |
|---|---|
| Number of goroutines | One per actor + worker pool. For a 2,000-host shard with ~25 ENIs avg, ≈ 60k actors per shard. Go runtime handles this easily (M:N scheduler). |
| Memory per actor | Target ≤ 8 KiB resident (FSM state + mailbox + hash). 60k × 8 KiB ≈ 480 MiB per shard. |
| HAL worker pool | Per-host pool (size = `min(8, ENI-count)`) to bound gNMI concurrency to the device. |
| Mailbox throughput | Each actor consumes ≥ 10k msg/sec for trivial messages; gNMI-bound actors consume at gNMI RPS. |
| Lock-free | All mailboxes are bounded channels; no mutexes inside the FSM. |

---

## 9. State Snapshot Format

Each actor's snapshot has a stable schema:

```protobuf
message ActorSnapshot {
  string  object_id          = 1;
  string  object_kind        = 2;     // HDO|CO|NO
  int64   intent_revision    = 3;     // etcd ModRevision of latest applied spec
  string  fsm_state          = 4;
  bytes   goal_state_blob    = 5;     // serialized DASH-schema payload
  string  goal_state_hash    = 6;     // sha256
  int64   last_reconcile_ts  = 7;
  int64   drift_count        = 8;
  int64   schema_version     = 9;     // for forward compat
  repeated string children_ids = 10;  // for parents only
}
```

Snapshots are written to BadgerDB under
`/wal/shard-<id>/objects/<object_id>` with a periodic compaction.

---

## 10. Worked Example: A Tenant Creates a VM with Two NICs

```
T0   Upstream writes:
       /config/v1/hosts/HOST-1/VM-abc                            (CO spec)
       /config/v1/hosts/HOST-1/VM-abc/NIC-primary                (NO spec)
       /config/v1/hosts/HOST-1/VM-abc/NIC-storage                (NO spec)

T0+ε HDO is already PROGRAMMED (device-level config).
     etcd watch dispatcher sees three new keys.
     CO key has no actor → HDO spawns CO(VM-abc).
     NIC keys arrive → buffered in bootstrap buffer until CO is PROGRAMMED.

T1   CO compiles its GoalState (tenant tags, VNET references).
     CO calls HAL.Apply(coGoalState) — typically a no-op or shallow merge
     (the heavy lifting is per-ENI). Returns ACK ⇒ CO PROGRAMMED.

T1+ε CO drains bootstrap buffer → spawns NO(NIC-primary) and NO(NIC-storage).

T2   Each NO compiles its DASH-gNMI payload in parallel.
     Two gNMI Set RPCs to HOST-1's DPU, dispatched through HAL worker pool
     bounded at 8 concurrent.
     DPU returns ACK ⇒ both NOs PROGRAMMED.

T3   COs send MsgChildReport(PROGRAMMED) to parent.
     HDO updates aggregate "tenant deployed" counter.
     Metric `dashfabric_eni_program_latency_seconds` records (T2 - T0).
     OTel trace shows the full T0→T2 span tree.
```

If the gNMI Set for NIC-storage fails with `INVALID_ARGUMENT` (schema bug),
that NO goes to `FAILED`, alert fires, NIC-primary is unaffected. The
operator can `dfctl object dump NO(HOST-1,VM-abc,NIC-storage)` to inspect.

---

## 11. Invariants Checklist (For Implementers)

- [ ] No actor mutates another actor's state directly.
- [ ] Every state transition is WAL-logged before its side effect.
- [ ] Every mailbox is bounded.
- [ ] Every child reports DONE to parent before exiting.
- [ ] Every actor exits its `Run` exactly once.
- [ ] Every HAL call carries the originating OTel trace context.
- [ ] Re-entrant config events with the same `intent_revision` are no-ops.
- [ ] Out-of-order events are buffered, never silently dropped.
- [ ] FAILED is sticky and observable.
- [ ] On unregister, WAL is purged for that HostID.

---

## 12. Open Questions

| ID | Question | Default decision |
|---|---|---|
| OQ-201 | Should COs be allowed to share VNET-level resources across NOs to dedupe HAL calls? | Yes; CO acts as a per-VM cache for VNET-scope artifacts (`DASH_VNET_MAPPING_TABLE` rows). |
| OQ-202 | Should NICs explicitly model ACL groups as separate sub-actors? | No in v1. ACLs are part of NO. May refactor if ACL rule scale per ENI grows beyond a single actor's comfortable in-memory size. |
| OQ-203 | How do we handle a device that registers but is in firmware-update mode and rejects all gNMI? | New FSM substate `DEVICE_QUARANTINED`; HDO holds with periodic re-probe; metric + alert. |
| OQ-204 | Do we support live migration of an ENI between DPUs? | Out of scope of DashFabric core — the upstream models this as DELETE on the old DPU + CREATE on the new DPU. Coordination is upstream's responsibility. |
