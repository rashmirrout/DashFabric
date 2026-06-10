# 01 — Architecture Overview

> *Read first.* This document gives the full-stack picture: principles,
> components, layers, and end-to-end flows. Each subsequent document drills into
> one slice.

---

## 1. Problem Statement

A datacenter contains **tens of thousands** of DPU-equipped servers, SmartSwitch
DPUs, and DPU appliances ([DASH-compliant](https://github.com/sonic-net/DASH/tree/main)).
Every DPU hosts up to **32 ENIs** (Elastic Network Interfaces), each ENI
carrying **millions of flows** governed by VNETs, routes, ACLs (NSGs), CA-PA
mappings, metering policies, and tunneling rules.

An upstream control plane (think Azure SDN Controller, OpenStack Neutron, or a
proprietary fabric controller) publishes **declarative intent** describing what
each tenant's ENI should do.

We need a service that:

1. **Discovers** each DPU when it registers.
2. **Subscribes** to that DPU's slice of intent.
3. **Translates** intent into device-specific programming via gNMI (per DASH
   SAI/gNMI schemas).
4. **Reconciles** continuously to detect and repair drift.
5. **Tears down** cleanly when intent is withdrawn at any layer.
6. **Survives** software crashes, host reboots, ISSU, rolling upgrades, partial
   DC outages — all without dropping a flow that is already programmed.
7. **Scales horizontally** to the full DC and across DCs.
8. **Emits** full observability (metrics, traces, structured logs) and ships
   with a powerful operator/diagnostic CLI.

---

## 2. Design Principles

1. **Source of truth lives upstream.** DashFabric never owns authoritative
   state; it is a projection + reconciler. This eliminates the hardest
   distributed-systems problem (consensus on writes).
2. **One actor per object, single writer per object.** Each HDO/CO/NO owns its
   state and mailbox; never shared. No global locks.
3. **Hierarchy mirrors topic structure.** The PubSub key path *is* the object
   tree. Subscription patterns map 1:1 to object lifecycle.
4. **All replicas converge by replaying the same upstream stream.** No
   log-shipping between replicas. Failover is lease handoff, not state copy.
5. **Idempotent everywhere.** Every config apply, every device call, every
   reconcile is idempotent. Replays must be safe.
6. **Reject silently dropped writes.** If a southbound RPC fails, surface the
   error, mark the object as drifted, retry with backoff, never lie upward.
7. **Vendor variability is contained in HAL plugins.** Core logic is
   vendor-agnostic and operates on the DASH gNMI schema.
8. **Trace every event end-to-end.** A single OTel trace ID flows from
   PubSub-event ingress → object FSM transition → gNMI Set RPC → device ACK.
9. **Observability is a first-class feature, not a sidecar.** No
   not-instrumented code paths.
10. **Operator UX matters.** A clean CLI to dump, diff, simulate, and replay
    must exist before GA.

---

## 3. High-Level Architecture

```
                            ┌────────────────────────────────────────┐
                            │   Upstream Intent Producer             │
                            │   (Azure SDN, OpenStack, proprietary)  │
                            └─────────────────┬──────────────────────┘
                                              │ writes hierarchical keys
                                              ▼
                            ┌────────────────────────────────────────┐
                            │   etcd cluster  (Config PubSub)        │
                            │   /config/hosts/<HOST-ID>/...          │
                            │   /config/hosts/<HOST-ID>/<CONT-GUID>  │
                            │   /config/hosts/<HOST-ID>/<CONT>/<NIC> │
                            └──┬───────────────────────────────────┬─┘
                               │ prefix watch                       │
                               ▼                                    ▼
   ┌─────────────────────────────────────────────────────┐    ┌──────────────┐
   │                  DashFabric Control Plane           │    │  Partition   │
   │                                                     │    │  Manager     │
   │   ┌────────────────────────────────────────────┐    │    │  (Operator)  │
   │   │  Northbound Gateway (stateless)            │    │    │              │
   │   │  • mTLS device register / heartbeat        │    │    │ assigns      │
   │   │  • Maps DeviceID → Shard                   │◄───┼────│ shards to    │
   │   └──────────────────┬─────────────────────────┘    │    │ ShardSets    │
   │                      │ HashRing(DeviceID)           │    └──────────────┘
   │                      ▼                              │
   │   ┌────────────────────────────────────────────┐    │
   │   │  ShardSet 0       ShardSet 1   ...    N-1  │    │
   │   │  [P][S][S]        [P][S][S]                │    │
   │   │   │                                        │    │
   │   │   │ each replica:                          │    │
   │   │   │   • etcd watch on its hosts            │    │
   │   │   │   • HDO/CO/NO actor tree per host      │    │
   │   │   │   • BadgerDB WAL on local NVMe         │    │
   │   │   │   • lease in K8s coordination.k8s.io   │    │
   │   │   │     for primary election               │    │
   │   │   │   • only P actuates southbound         │    │
   │   │   └─────────────────┬──────────────────────┤    │
   │   └─────────────────────┼──────────────────────┘    │
   └─────────────────────────┼───────────────────────────┘
                             │ gNMI Set/Get  (only from Primary)
                             ▼
                  ┌──────────────────────────┐
                  │  HAL  (vendor plugins)   │
                  │  • DASH-gNMI codec       │
                  │  • Vendor X codec        │
                  └────────────┬─────────────┘
                               │ gRPC over mTLS
                               ▼
                  ┌──────────────────────────┐
                  │  DPU gNMI endpoint       │
                  │  SONiC-DASH container    │
                  │  → SWSS → SAI → ASIC     │
                  └──────────────────────────┘
                               │
                               ▼ telemetry (gNMI Subscribe stream)
                       back to ShardSet P (+ S as observer)
```

---

## 4. Component Catalog

### 4.1 Northbound Gateway (NBG)
- **Role:** entry point for device registration and heartbeat.
- **Stateless** K8s `Deployment`, fronted by a Service of type `LoadBalancer`
  (or anycast in larger DCs).
- Validates device mTLS cert, extracts DeviceID, looks up the current shard
  assignment from the **Shard Map** (a small etcd-backed table), and returns
  the assigned ShardSet endpoint to the device.
- Devices keep a long-lived gRPC stream to *their* ShardSet, not the NBG.
- See `06-northbound-registration.md`.

### 4.2 ShardSet (Stateful)
A **ShardSet** is the unit of HA and partitioning. Each ShardSet:
- **Owns one shard** = one contiguous range of `hash(DeviceID)` space.
- Runs as **3 pods** in a K8s `StatefulSet`, one pod per failure domain (AZ /
  rack).
- Each pod has a local PVC (NVMe-backed) holding the BadgerDB WAL.
- **All 3 pods** open the etcd watch on their hosts' config keys.
- All 3 pods build and maintain identical HDO/CO/NO FSM trees in memory.
- Pods race for a per-shard lease in `coordination.k8s.io/v1`. The lease holder
  is the **Primary**; the other two are **Hot Standbys**.
- Only the Primary actuates southbound (gNMI Set + Subscribe on devices, sends
  heartbeats to devices, runs the reconciliation actuator).
- Standbys still process events (build state), but their HAL is in "shadow
  mode" — they validate without sending RPCs.
- See `05-high-availability-and-issu.md`.

### 4.3 Partition Manager (PM)
- A K8s controller (operator pattern) that owns the **Shard Map**.
- Initially: `numShards = ⌈DeviceCount / shardCapacity⌉` (e.g., shardCapacity
  = 2,000 devices).
- Scale-out: split a shard into two halves; orchestrates handoff with a
  freeze-and-replay protocol.
- Scale-in: merge two shards; reverse process.
- Replaces a failed ShardSet by spinning up a new StatefulSet binding and
  fencing the old.
- See `04-partitioning-and-shard-management.md`.

### 4.4 Configuration PubSub (etcd)
- 3- or 5-node etcd cluster (separate from the K8s control-plane etcd).
- Hierarchical keys under `/config/hosts/<HOST-ID>/...`.
- Watches are by prefix; the system trivially scopes per host, per container, per
  NIC.
- Leases provide presence semantics (a key with a lease disappears when the
  upstream producer dies; HDO sees a delete and tears down).
- See `03-control-plane-pubsub-and-topics.md`.

### 4.5 Object Tree (per host)
```
HostDeviceObject (HDO)            actor goroutine
  ├── watches:                    /config/hosts/<HOST-ID>
  ├── owns:                       gNMI session to DPU, device-level attributes
  └── children:                   ContainerObject[]

ContainerObject (CO)              actor goroutine per VM
  ├── watches:                    /config/hosts/<HOST-ID>/<CONT-GUID>
  ├── owns:                       per-container artifacts (VNET refs, ACL groups)
  └── children:                   NICObject[]

NICObject (NO)                    actor goroutine per ENI
  ├── watches:                    /config/hosts/<HOST-ID>/<CONT>/<NIC-ID>
  ├── owns:                       ENI gNMI subtree (DASH ENI/ACL/routes/mappings)
  └── children:                   none — leaf
```

Each object:
- Has a **bounded mailbox** (channel) of `(ConfigEvent | ReconcileTick |
  DestroyOrder | ChildReport)`.
- Runs a single goroutine (single writer).
- Carries an FSM (see `02-domain-model-and-object-lifecycle.md`).
- Records every transition to BadgerDB WAL for warm restart.

### 4.6 HAL (Hardware Abstraction Layer)
- **Codec plugins** convert internal `GoalState` to vendor-specific payloads.
- The default `dash-gnmi` codec implements the [SONiC-DASH gNMI/APP_DB
  schema](https://github.com/sonic-net/SONiC/blob/master/doc/dash/dash-sonic-hld.md).
- **Transport plugins** wrap protocols (gNMI v0.10, future SAI-RPC, debug
  loopback).
- **Capability registry** keyed by `(vendor, hwSKU, firmwareVersion)` declares
  supported features and scale limits.
- The HAL exposes a stable internal interface; the rest of the system is
  vendor-agnostic.
- See `07-hal-and-dash-southbound.md`.

### 4.7 Reconciliation Engine
- Each object runs **three loops**:
  1. **Event loop** — react to upstream config events. Lowest latency.
  2. **Liveness loop** — every 10–30 s, probe device aliveness via gNMI
     `Get(SYSTEM/STATE)` or a lightweight subscription heartbeat.
  3. **Reconcile loop** — every 60–300 s (configurable), fetch device state
     for objects owned by this actor, compute diff against GoalState, replay
     deltas.
- The reconciliation cadence is staggered (jittered) to spread DPU load.
- See `08-reconciliation-storage-and-warm-restart.md`.

### 4.8 Observability Stack
- **Traces:** OTel SDK, OTLP export to Tempo/Jaeger. Trace context flows
  PubSub-event → object → HAL → gNMI.
- **Metrics:** Prometheus client lib, scraped by Mimir/VictoriaMetrics. RED
  + USE method, plus DASH-specific KPIs (gNMI RPC latency, ENI program
  latency, reconcile drift counts).
- **Logs:** structured JSON to stderr, shipped via Promtail to Loki.
- **Diagnostic CLI (`dfctl`):** dump, diff, simulate, replay; never mutates
  device state directly — always through the actor mailbox.
- See `09-observability-and-diagnostics.md`.

### 4.9 Security Surface
- **Northbound (devices ↔ DashFabric):** mTLS, SPIFFE-style identity per DPU.
- **Eastbound (DashFabric ↔ etcd / upstream):** mTLS + RBAC per role.
- **Southbound (DashFabric ↔ DPU):** mTLS over gNMI, certs rotated via SPIRE.
- **Operator plane:** K8s RBAC + audit log; OPA/Gatekeeper for admission.
- See `10-deployment-and-security.md`.

---

## 5. End-to-End Walkthrough

### 5.1 Device Registration
```
DPU boots → calls NorthboundGateway.Register(DeviceCSR, capabilities)
         → NBG validates cert, computes shardId = ring.Lookup(DeviceID)
         → NBG reads ShardMap to find ShardSet endpoint
         → returns { shardEndpoint, expectedHeartbeatInterval }
DPU opens persistent gRPC stream to ShardSet (all 3 pods register interest;
   stream is mirrored).
ShardSet Primary instantiates HDO for this DeviceID.
HDO writes its presence to /state/hosts/<HOST-ID>/registered = true (lease).
HDO opens etcd watch on /config/hosts/<HOST-ID>/.
```

### 5.2 ENI Programming
```
Upstream writes:
  /config/hosts/HOST-1                        → device-level attrs
  /config/hosts/HOST-1/VM-abc                 → container-level attrs
  /config/hosts/HOST-1/VM-abc/NIC-primary     → ENI spec
  /config/hosts/HOST-1/VM-abc/NIC-primary/acl → ACL group
  ...

HDO sees the device-level change → applies → spawns CO for VM-abc.
CO sees its spec → applies → spawns NO for NIC-primary.
NO sees its spec → compiles to DASH gNMI payload via dash-gnmi codec:
   • DASH_ENI_TABLE
   • DASH_ACL_GROUP_TABLE + DASH_ACL_RULE_TABLE
   • DASH_ROUTE_GROUP_TABLE + DASH_ROUTE_TABLE
   • DASH_VNET_MAPPING_TABLE
NO sends gNMI SetRequest with all-or-nothing atomic update.
On 200 OK, NO transitions FSM to PROGRAMMED, emits metric & trace span.
```

### 5.3 Drift Detection
```
Reconcile tick on NO:
  HAL.GetActualState() → snapshot of device's current ENI subtree.
  NO computes diff(GoalState, ActualState).
  If diff is non-empty:
     emit metric dashfabric_drift_total{eni="..."}.
     send corrective SetRequest.
     log structured event with diff payload (sanitized).
```

### 5.4 Cascading Delete
```
Upstream deletes /config/hosts/HOST-1/VM-abc/NIC-primary.
NO receives delete event → FSM: PROGRAMMED → DRAINING.
NO sends gNMI Set(Delete) for its ENI subtree.
On ACK → FSM: TERMINATED, reports done to CO, exits goroutine.
CO removes NO from children. If CO has no more children AND upstream key
   /config/hosts/HOST-1/VM-abc is also deleted, CO drains itself similarly.
```

### 5.5 Device Unregistration
```
DPU disconnects (lease on /state/hosts/HOST-1/registered expires).
HDO receives a lease-expired event.
HDO orders all COs to TERMINATE.
HDO deletes its WAL entries (per user spec: re-register = fresh state).
HDO exits.
```

### 5.6 Primary Failover (unplanned)
```
Primary pod dies.
K8s coordination.k8s.io lease expires after `LeaseDuration` (e.g., 5 s).
Next Standby acquires lease → becomes Primary.
New Primary's HDO/CO/NO tree is already populated (it has been consuming
   etcd in shadow mode).
New Primary turns HAL out of shadow mode, replays last unACKed gNMI
   intentions (from WAL).
Steady state restored ≤ LeaseDuration + replay-time (target ≤ 2 s).
```

### 5.7 ISSU (planned upgrade)
```
Operator triggers rolling update on ShardSet StatefulSet (OnDelete strategy).
Target pod traps SIGTERM:
   1. Releases its lease (if Primary).
   2. Flushes BadgerDB.
   3. Closes etcd watch.
   4. Exits 0.
A peer takes the lease immediately (≈ ms because the lease wasn't waited out).
New pod (with new image) starts, mounts the same PVC, replays WAL into memory,
   re-opens etcd watch at the recorded revision, joins ShardSet as Standby.
Process repeats for the next pod. Zero programming gap.
```

---

## 6. Why This Architecture Is Different From the Common Naive One

| Naive design | Why it breaks | DashFabric's answer |
|---|---|---|
| Single global queue feeding worker pool | Head-of-line blocking; one slow device stalls all hosts | Per-object actor + bounded mailbox; isolation by construction |
| Strongly-consistent local DB | Forces consensus on writes; complex; not the right tool | Source of truth lives upstream; we are a projection |
| Primary-only replicates state to standbys | Long failover (warm-up time); split-brain risks | All replicas consume upstream independently → identical state |
| Per-vendor RPC libraries scattered through code | Untestable, brittle, leak-prone | All vendor variability lives behind HAL; core is schema-only |
| Reconciliation == full rebuild | Crushes DPU CPU; ID-thrashing in hardware | Diff-based, idempotent SetRequests with merge semantics |
| Telemetry as logging only | Cannot answer "why is this ENI slow to program" | OTel trace from upstream event to gNMI ACK; one span per FSM transition |
| Shard = consistent hash over devices, fixed | Cannot split hot shards | Range-shard + Partition Manager that splits/merges atomically |
| Devices write to one big control IP | LB blast radius; weird mTLS rotation | NBG only routes; persistent channel goes direct to the device's ShardSet |

---

## 7. Cross-Cutting Concerns

### 7.1 Idempotency
Every operation — config apply, device program, reconcile, delete — must be
idempotent. We achieve this via:
- **Version-stamped GoalState.** Each NO stores `(intentRev, hash(goalState))`
  and skips no-op rev bumps.
- **gNMI Replace** semantics over Update for the leaf where the schema allows
  it; otherwise Update + delete-stale.
- **DASH SAI semantics.** Per DASH SONiC HLD §1.6 #11, ADD/DELETE are
  idempotent; we rely on this.

### 7.2 Ordering
We do **not** require global event ordering. We require:
- **Per-object ordering** — each actor sees its own keys in etcd revision
  order. etcd guarantees this within a single watch.
- **Parent-before-child causality** — a child's actor cannot exist before its
  parent acknowledges it. Enforced by the actor tree.

### 7.3 Backpressure
- Mailbox is bounded (default 1024 events).
- On overflow, the watch is paused with a `coalescing` policy: only the latest
  revision per key is kept. This is acceptable because the source of truth is
  upstream and we will re-fetch the current value.
- A metric (`dashfabric_mailbox_coalesced_total`) tracks this; alert if non-zero
  for sustained period.

### 7.4 Versioning
- All gRPC APIs follow [buf](https://buf.build/) breaking-change rules.
- Internal proto: `v1` is forward and backward compatible within minor releases.
- Etcd schema is versioned per key prefix:
  `/config/v1/hosts/...`. v2 launches under `/config/v2/...` and migrations are
  done by upstream producers.

### 7.5 Multi-tenancy
- Tenant ID is part of every Container/ENI's metadata.
- RBAC on the diagnostic CLI restricts what an operator can see.
- Per-tenant rate limits on event ingress prevent one tenant from starving
  another (token-bucket per `TenantID` at the etcd-watch dispatcher).

---

## 8. What Is Out of Scope (Explicitly)

| Concern | Why excluded | Where it lives |
|---|---|---|
| Generating intent | DashFabric is *consumer*, not author of intent | Upstream SDN (Azure / OpenStack / proprietary) |
| Data-plane flow programming | That's the DPU's job, driven by our control programming | DPU hardware pipeline |
| HA-pair flow sync between DPUs | DASH-specific protocol between cards | DASH HA spec; DashFabric only programs pairing config |
| Tenant billing aggregation | Consume metering counters; do not aggregate or bill | Billing pipeline downstream |
| Underlay routing | SONiC BGP / EVPN | SONiC underlay containers (out of DASH container) |
| Topology discovery | Devices declare themselves via registration; no LLDP crawl | Upstream inventory |

---

## 9. Reading Onward

| Concern | Document |
|---|---|
| How objects behave (FSM, lifecycle, delete propagation) | `02-domain-model-and-object-lifecycle.md` |
| Topic/key naming, message schema | `03-control-plane-pubsub-and-topics.md` |
| Sharding, partition manager, splits | `04-partitioning-and-shard-management.md` |
| HA, failover, ISSU | `05-high-availability-and-issu.md` |
| Device onboarding | `06-northbound-registration.md` |
| Vendor abstraction, DASH gNMI mapping | `07-hal-and-dash-southbound.md` |
| Reconcile, WAL, warm restart | `08-reconciliation-storage-and-warm-restart.md` |
| Observability & diagnostics | `09-observability-and-diagnostics.md` |
| K8s deployment, security | `10-deployment-and-security.md` |
| Failure matrix, runbooks | `11-failure-modes-and-runbooks.md` |
| Phasing, open questions | `12-roadmap-and-open-questions.md` |
