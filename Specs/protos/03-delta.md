# `delta.proto` — Programming Pipeline

**Source:** [`protos/delta.proto`](../../protos/delta.proto)
**Imports:** `common.proto`, `models.proto`, `google/protobuf/any.proto`,
`google/protobuf/timestamp.proto`, `google/protobuf/duration.proto`

The *verbs* of FleetManager. Whereas `models.proto` describes what the world
**is**, `delta.proto` describes how it **changes**: the diff between desired
and current state, expressed as ordered, idempotent commands ready for
dispatch to the southbound HAL.

---

## 1. Pipeline Overview

```
        DeviceGoalState (desired)            DeviceState (current)
                  \                                /
                   \                              /
                    +--> ComputeDeltas_ --------+
                                |
                                v
                       [DeltaCommand ...]   (unordered)
                                |
                                v
                       ResolveDependencies_   (Kahn topological sort)
                                |
                                v
                            DeltaPlan
                                |
                                v
                       SouthboundDriver.Apply
                                |
                                v
                          DeltaResult
```

Every step is described in detail in `fleet-manager-lld.md §6`. This document
covers only the wire schema.

---

## 2. Enumerations

### `OperationType` — what kind of mutation

| Value | Meaning |
|-------|---------|
| `OPERATION_TYPE_CREATE` (1) | New object — `desired` populated, `current` empty |
| `OPERATION_TYPE_UPDATE` (2) | Spec changed — both `desired` and `current` populated |
| `OPERATION_TYPE_DELETE` (3) | Object removed — `current` populated, `desired` empty |
| `OPERATION_TYPE_NOOP` (4) | Reconcile found device already converged. Kept in audit trail without re-programming hardware |

### `TargetType` — what object is being mutated

| Value | Domain |
|-------|--------|
| `TARGET_TYPE_HOST` (1) | FleetManager-native (`HostDeviceObject`) |
| `TARGET_TYPE_CONTAINER` (2) | FleetManager-native (`ContainerObject`) |
| `TARGET_TYPE_NIC` (3) | FleetManager-native (`NICObject`) |
| `TARGET_TYPE_ENI` (10) | DASH-native — forwarded straight through |
| `TARGET_TYPE_VNET` (11) | DASH-native |
| `TARGET_TYPE_ROUTE` (12) | DASH-native |
| `TARGET_TYPE_ACL_GROUP` (13) | DASH-native |
| `TARGET_TYPE_ACL_RULE` (14) | DASH-native |
| `TARGET_TYPE_METER` (15) | DASH-native |
| `TARGET_TYPE_TUNNEL` (16) | DASH-native |

For DASH-native target types, the body is a `DashObject` from `dash.proto`,
wrapped in `google.protobuf.Any` so non-DASH callers (e.g. a REST client
listing pending deltas) don't need to depend on the upstream proto tree.

### `DeltaStatus` — lifecycle of one command

| Value | Meaning |
|-------|---------|
| `DELTA_STATUS_PENDING` (1) | Queued, not yet sent |
| `DELTA_STATUS_DISPATCHED` (2) | Sent to southbound, awaiting ACK |
| `DELTA_STATUS_SUCCESS` (3) | Terminal success |
| `DELTA_STATUS_FAILED` (4) | Terminal failure; will not be retried automatically |
| `DELTA_STATUS_RETRYING` (5) | Transient failure, exponential backoff |
| `DELTA_STATUS_CANCELLED` (6) | Superseded by a newer goal-state revision |

---

## 3. `DeltaCommand` — single programming operation

The atomic unit of work dispatched to a southbound driver.

### Identity & tracing

| Field | Type | Purpose |
|-------|------|---------|
| `command_id` | `string` | Stable UUID v4. Used as **idempotency key** on retries — driver MUST de-dupe |
| `trace` | `TraceContext` | End-to-end W3C trace for distributed debugging |
| `goal_revision` | `uint64` | Goal-state revision this command belongs to (monotonic per device) |

### What & where

| Field | Type | Purpose |
|-------|------|---------|
| `operation` | `OperationType` | CREATE / UPDATE / DELETE / NOOP |
| `target_type` | `TargetType` | Object class |
| `target_id` | `string` | Object identity inside the device tree (`"eth0"`, `"eni-123"`) |
| `device_id` | `DeviceId` | Owning device |

### Payloads

| Field | Type | When populated |
|-------|------|----------------|
| `desired` | `google.protobuf.Any` | CREATE + UPDATE — wraps a `fleetmanager.v1` model OR an upstream `dash.*` message |
| `current` | `google.protobuf.Any` | UPDATE + DELETE — driver can compute minimal patch |

### Dependencies

| Field | Type | Purpose |
|-------|------|---------|
| `depends_on` | `repeated string` | Command ids resolved during topological sort. Driver MUST NOT dispatch until all listed commands reach `SUCCESS` |

### Retry policy

| Field | Type | Purpose |
|-------|------|---------|
| `max_retries` | `uint32` | Upper bound on retry attempts |
| `retry_backoff` | `Duration` | Initial backoff (doubled on each retry, capped at 30s) |
| `timeout` | `Duration` | Per-attempt deadline |

### Bookkeeping (server-populated; do not set on input)

| Field | Type | Purpose |
|-------|------|---------|
| `status` | `DeltaStatus` | Current lifecycle state |
| `retry_count` | `uint32` | Attempts so far |
| `created_at`, `dispatched_at`, `completed_at` | `Timestamp` | Lifecycle markers |
| `last_error` | `ErrorDetail` | Most recent failure (or empty on success) |

---

## 4. `DeviceGoalState` — full desired-state document

Produced by the upstream control plane, consumed by FleetManager.
Every publication bumps `revision`; FleetManager processes revisions
**strictly in order** and discards out-of-order writes.

| Field | Type | Purpose |
|-------|------|---------|
| `device_id` | `DeviceId` | Target |
| `revision` | `uint64` | Monotonic per device. FleetManager rejects `<= current_revision` |
| `generated_at` | `Timestamp` | Upstream emit time |
| `trace` | `TraceContext` | Source trace, propagated to all derived deltas |
| `host` | `HostDeviceObject` | Desired host state |
| `containers` | `repeated ContainerObject` | Desired children |
| `nics` | `repeated NICObject` | Desired grandchildren |
| `dash_objects` | `repeated google.protobuf.Any` | Desired DASH data-plane objects (Eni, Vnet, Route, …), wrapped via `DashObject` |
| `plan` | `repeated DeltaCommand` | **Optional** pre-computed delta plan. If absent, FleetManager computes one from `(current, desired)` |

`plan` exists for callers who want bit-for-bit reproducible programming
(e.g. canary rollouts of a known-good plan). When omitted, FleetManager's
`StateCompilationEngine` derives it.

---

## 5. `DeltaResult` — per-command outcome

Reported by the southbound driver back to the actor.

| Field | Type | Purpose |
|-------|------|---------|
| `command_id` | `string` | Correlates with `DeltaCommand.command_id` |
| `status` | `DeltaStatus` | Terminal status (`SUCCESS` / `FAILED`) |
| `error` | `ErrorDetail` | Populated on `FAILED` |
| `apply_latency` | `Duration` | Driver→device round-trip latency |
| `diagnostics` | `map<string,string>` | Driver-specific debug info (SAI status code, gNMI path, …) |

---

## 6. `DeltaPlan` — the topologically-sorted dispatch order

Output of `ResolveDependencies_`. Returned to callers of `ApplyGoalState`
(synchronous response) and `Reconcile`.

| Field | Type | Purpose |
|-------|------|---------|
| `device_id` | `DeviceId` | Target |
| `goal_revision` | `uint64` | Revision being applied |
| `commands` | `repeated DeltaCommand` | Topologically sorted in dispatch order |
| `wave_offsets` | `repeated uint32` | Wave boundaries: `commands[wave_offsets[i] .. wave_offsets[i+1])` form wave `i`. Within a wave the driver MAY dispatch in parallel |
| `plan_hash` | `uint64` | FNV-1a over canonical bytes — used to dedupe identical plans |
| `computed_at` | `Timestamp` | When the plan was generated |
| `compile_time` | `Duration` | How long compilation took (observability) |

### Why "waves"?

A naive topological sort yields a fully serial schedule. In practice, many
deltas have **no mutual dependency** (e.g. creating ten unrelated NICs).
The planner groups commands by depth in the dependency graph; commands at
the same depth are a "wave" and may run concurrently. The driver enforces
the wave barrier — wave `i+1` does not start until every command in wave
`i` reaches `SUCCESS`.
