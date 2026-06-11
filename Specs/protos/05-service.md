# `service.proto` — gRPC `FleetManagerService`

**Source:** [`protos/service.proto`](../../protos/service.proto)
**Imports:** `common.proto`, `models.proto`, `delta.proto`,
`google/protobuf/timestamp.proto`, `google/protobuf/duration.proto`,
`google/protobuf/field_mask.proto`

The full gRPC contract on **port 5051**. Every REST endpoint in
`fleet-manager-rest-api.md` has a 1:1 unary RPC analogue here, plus four
streaming RPCs that REST cannot offer cleanly.

---

## 1. RPC Catalogue

### Unary (REST-parity)

| RPC | Request → Response | Purpose |
|-----|-------------------|---------|
| `RegisterDevice` | `RegisterDeviceRequest` → `RegisterDeviceResponse` | Onboard a new device |
| `GetDevice` | `GetDeviceRequest` → `GetDeviceResponse` | Fetch a single `HostDeviceObject` |
| `ListDevices` | `ListDevicesRequest` → `ListDevicesResponse` | Paginated list with filters |
| `UpdateDevice` | `UpdateDeviceRequest` → `UpdateDeviceResponse` | Partial update via `FieldMask` |
| `DeregisterDevice` | `DeregisterDeviceRequest` → `DeregisterDeviceResponse` | Tear down a device |
| `Heartbeat` | `HeartbeatRequest` → `HeartbeatResponse` | Liveness signal |
| `PushTelemetry` | `PushTelemetryRequest` → `PushTelemetryResponse` | Per-NIC / per-container counters |
| `GetDeviceState` | `GetDeviceStateRequest` → `GetDeviceStateResponse` | Full object-tree snapshot |
| `ListDeviceObjects` | `ListDeviceObjectsRequest` → `ListDeviceObjectsResponse` | Filtered child query |
| `ListPendingDeltas` | `ListPendingDeltasRequest` → `ListPendingDeltasResponse` | Inspect the delta queue |
| `Health` | `HealthRequest` → `HealthResponse` | Readiness probe |

### Unary (gRPC-only)

| RPC | Purpose |
|-----|---------|
| `ApplyGoalState` | Push a new desired-state revision; returns the planned delta sequence |
| `Reconcile` | Force a full state-hash compare; returns drift details and a repair plan |

### Streaming

| RPC | Direction | Purpose |
|-----|-----------|---------|
| `StreamDeviceState` | server → client | Snapshot + diff feed for UI / dashboards |
| `StreamTelemetry` | server → client | Continuous telemetry firehose |
| `StreamEvents` | server → client | Lifecycle / programming events for operators |
| `DeviceChannel` | bidi | Long-lived agent connection: client streams hb/telemetry up, server streams `DeltaCommand` down |

---

## 2. Common Request Pattern

Every request message starts with:

```protobuf
TraceContext trace = 1;
```

Mutating requests additionally carry:

```protobuf
uint64 expected_revision = N;   // 0 disables the check
```

List requests use `PageRequest page` + `PageResponse page` from `common.proto`.

---

## 3. Per-RPC Schema

### 3.1 `RegisterDevice`

**Request — `RegisterDeviceRequest`**

| Field | Type | Origin | Purpose |
|-------|------|--------|---------|
| `trace` | `TraceContext` | caller | W3C propagation |
| `profile` | `DeviceProfile` | caller | Identity bundle (see `02-models.md §3`) |

**Response — `RegisterDeviceResponse`**

| Field | Type | Purpose |
|-------|------|---------|
| `host` | `HostDeviceObject` | Authoritative server record (state=`INITIALIZING`, audit.revision=1) |
| `subscription_topics` | `repeated string` | PubSub topics the device must subscribe to |

**Errors:** `VALIDATION_ERROR`, `DEVICE_EXISTS`, `NOT_PRIMARY`, `INTERNAL`.

---

### 3.2 `GetDevice` / `ListDevices`

**`GetDeviceRequest`**

| Field | Type | Purpose |
|-------|------|---------|
| `trace` | `TraceContext` | — |
| `device_id` | `DeviceId` | — |
| `read_mask` | `FieldMask` | Optional — return only listed fields |

**`GetDeviceResponse`**: `HostDeviceObject host`.

**`ListDevicesRequest`**

| Field | Type | Purpose |
|-------|------|---------|
| `trace` | `TraceContext` | — |
| `page` | `PageRequest` | Cursor pagination |
| `shard_id` | `ShardId` | Optional filter |
| `state` | `ObjectState` | Optional filter (e.g. `READY`) |
| `device_type` | `DeviceType` | Optional filter |
| `label_selector` | `string` | Selector query, e.g. `"rack=R12,az=us-west-2a"` |

**`ListDevicesResponse`**: `repeated HostDeviceObject devices` + `PageResponse page`.

---

### 3.3 `UpdateDevice`

**`UpdateDeviceRequest`**

| Field | Type | Purpose |
|-------|------|---------|
| `trace` | `TraceContext` | — |
| `device_id` | `DeviceId` | Path key |
| `host` | `HostDeviceObject` | New values for the fields named in `update_mask` |
| `update_mask` | `FieldMask` | Which fields to mutate (rejects unknown paths) |
| `expected_revision` | `uint64` | Optimistic concurrency; 0 disables |

**`UpdateDeviceResponse`**

| Field | Type | Purpose |
|-------|------|---------|
| `host` | `HostDeviceObject` | New authoritative record (revision incremented) |
| `changed_fields` | `map<string,string>` | Field-level diff (`"max_acl_rules" -> "100000 -> 150000"`) for audit logs |

**Errors:** `VALIDATION_ERROR`, `DEVICE_NOT_FOUND`, `PRECONDITION` (revision mismatch), `INTERNAL`.

---

### 3.4 `DeregisterDevice`

**`DeregisterDeviceRequest`**

| Field | Type | Purpose |
|-------|------|---------|
| `trace` | `TraceContext` | — |
| `device_id` | `DeviceId` | — |
| `force` | `bool` | When `false` and the device still owns containers/NICs, returns `CONFLICT`. When `true`, drives a graceful drain |
| `drain_timeout` | `Duration` | Max time to wait for child cleanup (default 30s) |

**`DeregisterDeviceResponse`**

| Field | Type | Purpose |
|-------|------|---------|
| `containers_terminated` | `uint32` | Count of children torn down |
| `nics_cleaned_up` | `uint32` | Count of grandchildren torn down |
| `deregistered_at` | `Timestamp` | Server commit time |

---

### 3.5 `Heartbeat`

**`HeartbeatRequest`**: `trace`, `device_id`, `payload: HeartbeatPayload`.

**`HeartbeatResponse`**

| Field | Type | Purpose |
|-------|------|---------|
| `ack_id` | `string` | Server-assigned ack — devices use this to detect partition recovery |
| `server_time` | `Timestamp` | For client-side clock-skew correction |
| `next_interval` | `Duration` | Suggested heartbeat interval; server may throttle on overload |
| `redirect_endpoint` | `string` | If non-empty, device should reconnect to this endpoint (failover or rebalance) |

---

### 3.6 `PushTelemetry`

**`PushTelemetryRequest`**: `trace`, `device_id`, `payload: TelemetryPayload`.

**`PushTelemetryResponse`**

| Field | Type | Purpose |
|-------|------|---------|
| `telemetry_id` | `string` | Server-assigned id for log correlation |
| `accepted_at` | `Timestamp` | Server commit time |

---

### 3.7 State Inspection RPCs

**`GetDeviceStateRequest`**: `trace`, `device_id`, `force_recompute: bool`.
- `force_recompute=true` re-runs `ComputeStateHash_` on the fly instead of
  reading the cached value. More expensive but catches drift.

**`GetDeviceStateResponse`**: `DeviceState state`.

**`ListDeviceObjectsRequest`**: `trace`, `device_id`, `object_type: TargetType`, `page`.
- `object_type=UNSPECIFIED` returns all kinds.

**`ListDeviceObjectsResponse`**: per-kind `repeated` arrays + `page`.

**`ListPendingDeltasRequest`**: `trace`, `device_id`, `status_filter: DeltaStatus`, `page`.
- `status_filter=UNSPECIFIED` returns all non-terminal deltas.

**`ListPendingDeltasResponse`**: `repeated DeltaCommand deltas` + `page`.

---

### 3.8 `ApplyGoalState`

**`ApplyGoalStateRequest`**

| Field | Type | Purpose |
|-------|------|---------|
| `trace` | `TraceContext` | — |
| `goal_state` | `DeviceGoalState` | Full desired-state document (see `03-delta.md §4`) |
| `dry_run` | `bool` | If `true`, only validate + return the plan; do not dispatch |

**`ApplyGoalStateResponse`**

| Field | Type | Purpose |
|-------|------|---------|
| `plan` | `DeltaPlan` | Topologically-sorted commands |
| `dispatched` | `bool` | `true` if queued for dispatch (`false` on `dry_run`) |

---

### 3.9 `Reconcile`

**`ReconcileRequest`**: `trace`, `device_id`, `apply: bool`.
- `apply=true` immediately dispatches any deltas needed to converge.

**`ReconcileResponse`**

| Field | Type | Purpose |
|-------|------|---------|
| `observed_hash` | `uint64` | Hash of the device's reported state |
| `expected_hash` | `uint64` | Hash of FleetManager's stored state |
| `drift_detected` | `bool` | `true` when hashes differ |
| `repair_plan` | `DeltaPlan` | Commands that would converge state (empty when no drift) |

---

### 3.10 `Health`

**`HealthRequest`**: `trace`.

**`HealthResponse`**

| Field | Type | Purpose |
|-------|------|---------|
| `service` | `string` | `"FleetManager"` |
| `version` | `string` | Build version |
| `primary` | `bool` | `true` on the leaseholder, `false` on standbys |
| `shard_id` | `ShardId` | Which shard this replica owns |
| `total_devices` | `uint64` | Devices in this shard |
| `devices_ready` / `devices_degraded` | `uint64` | Aggregate health counts |
| `uptime` | `Duration` | Process uptime |
| `timestamp` | `Timestamp` | Server time |

---

## 4. Streaming RPCs

### 4.1 `StreamDeviceState` (server → client)

**`StreamDeviceStateRequest`**

| Field | Type | Purpose |
|-------|------|---------|
| `trace` | `TraceContext` | — |
| `device_ids` | `repeated DeviceId` | Empty = all devices in this shard |
| `include_initial_snapshot` | `bool` | If `true`, server emits a full `DeviceState` before incremental updates |

**`DeviceStateUpdate`** (one stream message)

| Field | Type | Purpose |
|-------|------|---------|
| `kind` | `UpdateKind` enum | `SNAPSHOT` / `DIFF` / `HEARTBEAT` |
| `device_id` | `DeviceId` | Subject |
| `emitted_at` | `Timestamp` | Server emit time |
| `revision` | `uint64` | Goal-state revision |
| `snapshot` | `DeviceState` | Populated on `SNAPSHOT` |
| `changed_hosts` / `changed_containers` / `changed_nics` | repeated | Only changed objects (DIFF) |
| `removed_object_ids` | `repeated string` | Tombstones (DIFF) |

`HEARTBEAT` carries no payload — keepalive when nothing has changed.

### 4.2 `StreamTelemetry` (server → client)

**`StreamTelemetryRequest`**: `trace`, `device_ids`, `aggregation_window: Duration`
(`0` = pass-through).

**`TelemetryEvent`**: `device_id`, `emitted_at`, `payload: TelemetryPayload`.

### 4.3 `StreamEvents` (server → client)

**`StreamEventsRequest`**: `trace`, `device_ids`, `kind_mask: uint64` (0 = all).

**`FleetEvent`**

| Field | Type | Purpose |
|-------|------|---------|
| `kind` | `EventKind` enum | `DEVICE_REGISTERED`, `DELTA_DISPATCHED`, `RECONCILE_DRIFT`, `FAILOVER`, … |
| `device_id` | `DeviceId` | Subject |
| `emitted_at` | `Timestamp` | — |
| `trace` | `TraceContext` | — |
| `attributes` | `map<string,string>` | Free-form attrs (`old_state`, `new_state`, `command_id`, …) |
| `host_payload` | `HostDeviceObject` | Optional richer body |
| `delta_payload` | `DeltaCommand` | Optional |
| `error_payload` | `ErrorDetail` | Optional |

### 4.4 `DeviceChannel` (bidi)

Long-lived agent connection. Replaces the per-RPC dance for the hot path
between FleetManager and the on-device agent.

**Client → Server (`DeviceChannelClientMsg`)**

```
oneof msg {
  DeviceHello       hello;        // First message — MUST be hello
  HeartbeatPayload  heartbeat;
  TelemetryPayload  telemetry;
  DeltaResult       delta_result; // Per-DeltaCommand outcome
}
TraceContext trace = 100;
```

**`DeviceHello`**

| Field | Type | Purpose |
|-------|------|---------|
| `profile` | `DeviceProfile` | Identity bundle |
| `agent_version` | `string` | For compatibility checks |
| `last_known_revision` | `string` | Resume point — server replays missed deltas from here |

**Server → Client (`DeviceChannelServerMsg`)**

```
oneof msg {
  DeviceWelcome welcome;            // First message — MUST be welcome
  DeltaCommand  delta;              // Programming command
  string        redirect_endpoint;  // Failover / rebalance
  Timestamp     ping;               // Server keepalive
}
TraceContext trace = 100;
```

**`DeviceWelcome`**

| Field | Type | Purpose |
|-------|------|---------|
| `session_id` | `string` | Server-assigned channel id |
| `shard_id` | `ShardId` | Which shard owns this device now |
| `heartbeat_interval` | `Duration` | Suggested cadence |
| `server_revision` | `uint64` | Authoritative goal-state revision at session start |
