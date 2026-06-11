# `models.proto` — Hierarchical Object Model

**Source:** [`protos/models.proto`](../../protos/models.proto)
**Imports:** `common.proto`, `google/protobuf/timestamp.proto`

The *nouns* of FleetManager. Every persistent object follows the same
**Spec / Status** pattern:

```
Identity   : stable id + Uuid surrogate
Lifecycle  : ObjectState + transition timestamps
Audit      : AuditMetadata (created_at, updated_at, revision)
Spec       : caller-supplied desired-state fields
Status     : server-populated observed-state fields
```

Hierarchy: **`HostDeviceObject` -> `ContainerObject` -> `NICObject`.** A
`DeviceProfile` is the immutable identity bundle the caller submits during
registration; `HostDeviceObject` is the live, server-managed record that
wraps it.

---

## 1. Enumerations

### `DeviceType` — hardware/role classification

| Value | Meaning | Southbound HAL |
|-------|---------|----------------|
| `DEVICE_TYPE_UNSPECIFIED` (0) | Default | — |
| `DEVICE_TYPE_HOST_LINUX` (1) | Pure-Linux host, no DPU offload | netlink / eBPF |
| `DEVICE_TYPE_HOST_DPU_ATTACHED` (2) | Linux host with attached DPU/SmartNIC | DASH gNMI |
| `DEVICE_TYPE_APPLIANCE_DASH_STANDALONE` (3) | Single-ASIC DASH appliance | SAI / Thrift |
| `DEVICE_TYPE_APPLIANCE_DASH_CHASSIS` (4) | Multi-line-card DASH chassis | SAI / Thrift |

Drives **driver selection** in `SouthboundDriverFactory` and
**capability validation** at registration.

### `ObjectState` — lifecycle state shared by all hierarchical objects

| Value | Meaning | Outgoing transitions |
|-------|---------|----------------------|
| `OBJECT_STATE_INITIALIZING` (1) | Object created, programming in flight | -> `READY` (success) / `DEGRADED` (partial fail) |
| `OBJECT_STATE_READY` (2) | Steady state, last reconcile succeeded | -> `RECONFIGURING` / `POLICY_UPDATE` / `DRAINING` |
| `OBJECT_STATE_RECONFIGURING` (3) | Spec changed, recompiling deltas | -> `READY` / `DEGRADED` |
| `OBJECT_STATE_POLICY_UPDATE` (4) | Policy-only edit, no data plane churn | -> `READY` |
| `OBJECT_STATE_DEGRADED` (5) | Partial programming failure, retrying | -> `READY` / `DESTROYING` |
| `OBJECT_STATE_DRAINING` (6) | Children being torn down before delete | -> `DESTROYING` |
| `OBJECT_STATE_DESTROYING` (7) | Final cleanup in progress | -> `TERMINATED` |
| `OBJECT_STATE_TERMINATED` (8) | Soft-deleted, retained for audit window | (terminal) |

State machines per object type are documented in `fleet-manager-lld.md §4`.

### `HealthState` — coarse health classification

| Value | Meaning |
|-------|---------|
| `HEALTH_STATE_HEALTHY` (1) | All checks passing |
| `HEALTH_STATE_DEGRADED` (2) | Reduced capability, still serving |
| `HEALTH_STATE_UNHEALTHY` (3) | Not serving |

Independent of `ObjectState`. A device can be `READY` + `DEGRADED`
(e.g. one of several NICs failed) — the lifecycle is fine, but health
deserves an operator's attention.

---

## 2. `HardwareCapabilities`

Declared at registration. Used by **admission control** (refuse
over-subscription) and **shard placement** (advisory).

| Field | Type | Purpose |
|-------|------|---------|
| `max_flow_table_entries` | `uint64` | DASH connection table cap |
| `max_routes_per_eni` | `uint64` | DASH ENI route table cap |
| `max_acl_rules` | `uint64` | DASH ACL group cap |
| `max_nics` | `uint32` | Max simultaneous NICObjects |
| `max_containers` | `uint32` | Max simultaneous ContainerObjects |
| `max_cps` | `uint64` | Connections-per-second envelope |
| `cpu_cores` | `uint32` | Compute envelope (advisory) |
| `memory_gb` | `uint32` | Memory envelope (advisory) |
| `dpu_vendor` | `string` | `"Nvidia"`, `"Intel"`, `"Pensando"`, … — selects driver instance |
| `dpu_model` | `string` | Model designator (e.g. `"BlueField-3"`) |
| `firmware_version` | `string` | Surfaced in audit / dashboards |
| `driver_version` | `string` | Surfaced in audit / dashboards |
| `vendor_extensions` | `map<string,string>` | Open k/v escape hatch — vendor-specific knobs without bumping schema |

---

## 3. `DeviceProfile` — registration payload (immutable identity bundle)

This is the **only** message the caller controls during `RegisterDevice`.

| Field | Type | Purpose |
|-------|------|---------|
| `device_id` | `DeviceId` | Stable primary key |
| `device_type` | `DeviceType` | Drives southbound HAL selection |
| `host_ip` | `IpAddress` | Where to reach the device's agent / management plane |
| `hardware_capabilities` | `HardwareCapabilities` | Resource envelope |
| `labels` | `map<string,string>` | Selector queries: `rack=R12`, `az=us-west-2a` |
| `annotations` | `map<string,string>` | Operator-visible tags for dashboards / audit |
| `subscription_prefix` | `string` | PubSub topic prefix; defaults to `/config/hosts/{device_id}` |

**`labels` vs `annotations` (Kubernetes convention)**: labels are queryable
(`ListDevices?label_selector=rack=R12`); annotations are read-only metadata.

---

## 4. `HostDeviceObject` — live, server-managed record

Returned by `RegisterDevice`, `GetDevice`, `ListDevices`. Wraps the original
`DeviceProfile` and adds server-owned status.

### Identity

| Field | Type | Origin | Purpose |
|-------|------|--------|---------|
| `uuid` | `Uuid` | server | Internal surrogate key |
| `device_id` | `DeviceId` | from profile | Echoed back |
| `shard_id` | `ShardId` | server-computed | `xxhash64(device_id) % shard_count` |

### Lifecycle / Spec

| Field | Type | Origin | Purpose |
|-------|------|--------|---------|
| `state` | `ObjectState` | server | `INITIALIZING` on first call |
| `health` | `HealthState` | server | `UNSPECIFIED` until first heartbeat |
| `profile` | `DeviceProfile` | embedded copy | Audit + future re-validation |

### Aggregate counters (server-maintained, do NOT set on PUT)

| Field | Type | Updated by |
|-------|------|------------|
| `container_count` | `uint32` | Container create/delete |
| `nic_count` | `uint32` | NIC create/delete |
| `allocated_memory_bytes` | `uint64` | Container resource accounting |
| `allocated_cpu_cores` | `uint32` | Container resource accounting |
| `active_flows` | `uint64` | Heartbeat / telemetry rollup |

### Reconciliation

| Field | Type | Purpose |
|-------|------|---------|
| `last_heartbeat` | `Timestamp` | Drives the liveness watchdog (default 30s threshold) |
| `last_reconcile` | `Timestamp` | Last full state hash compare |
| `state_hash` | `uint64` | FNV-1a over canonical serialization; drives drift detection |

### Audit

| Field | Type | Purpose |
|-------|------|---------|
| `audit` | `AuditMetadata` | Created/updated timestamps, revision counter, last mutator |

---

## 5. `ContainerObject` — tenant workload on a host

| Field | Type | Origin | Purpose |
|-------|------|--------|---------|
| `uuid` | `Uuid` | server | Surrogate key |
| `container_guid` | `string` | caller | Caller-supplied id, unique within a host |
| `host_uuid` | `Uuid` | parent ref | Points at `HostDeviceObject.uuid` |
| `state` | `ObjectState` | server | Same lifecycle as host |
| `health` | `HealthState` | server | Independent of state |
| `tenant_id` | `string` | spec | Drives policy selection |
| `vpc_id` | `string` | spec | Determines VPC binding |
| `requested_memory_bytes` | `uint64` | spec | Advisory; enforced by host runtime |
| `requested_cpu_cores` | `uint32` | spec | Advisory; enforced by host runtime |
| `nic_uuids` | `repeated Uuid` | server | Children (NICs that belong to this container) |
| `audit` | `AuditMetadata` | server | Standard audit fields |

---

## 6. `NICObject` — virtual NIC bound to a container, mapped to a DASH ENI

| Field | Type | Origin | Purpose |
|-------|------|--------|---------|
| `uuid` | `Uuid` | server | Surrogate key |
| `nic_id` | `string` | caller | Caller-supplied name, e.g. `"eth0"` |
| `container_uuid` | `Uuid` | parent ref | Points at `ContainerObject.uuid` |
| `host_uuid` | `Uuid` | grandparent ref | Cached for fast topic routing |
| `state`, `health` | enums | server | Lifecycle + health |
| `eni_id` | `string` | spec | DASH ENI binding — the data plane object this NIC programs |
| `vpc_id` | `string` | spec | VPC binding |
| `primary_ip` | `IpAddress` | spec | Primary VIP |
| `secondary_ips` | `repeated IpAddress` | spec | Floating IPs |
| `mac_address` | `MacAddress` | spec | L2 binding |
| `vlan_id` | `uint32` | spec | L2 binding |
| `packets_in/out`, `bytes_in/out`, `drops` | `uint64` | counters | Refreshed via `Telemetry`; not authoritative for billing |
| `audit` | `AuditMetadata` | server | Standard audit fields |

---

## 7. `DeviceState` — full snapshot of one device's object tree

Returned by `GetDeviceState`, embedded in `StreamDeviceState` snapshots.

| Field | Type | Purpose |
|-------|------|---------|
| `host` | `HostDeviceObject` | Root |
| `containers` | `repeated ContainerObject` | All children of `host` |
| `nics` | `repeated NICObject` | All grandchildren |
| `total_objects` | `uint32` | Aggregate count across the tree |
| `ready_objects` | `uint32` | Count in `OBJECT_STATE_READY` |
| `degraded_objects` | `uint32` | Count in `DEGRADED` |
| `snapshot_at` | `Timestamp` | Server time at snapshot |
| `state_hash` | `uint64` | FNV-1a over canonical serialization; used for drift detection |

---

## 8. Telemetry payloads

### `HeartbeatPayload` — coarse device-level liveness

| Field | Type | Purpose |
|-------|------|---------|
| `timestamp` | `Timestamp` | Device-side clock (server clamps for skew) |
| `health` | `HealthState` | Self-reported health |
| `cpu_usage_percent` | `uint32` | 0–100 |
| `memory_usage_percent` | `uint32` | 0–100 |
| `active_connections` | `uint64` | Current flow count |
| `pps_in`, `pps_out` | `uint64` | Aggregate device throughput |
| `extensions` | `map<string,string>` | Vendor extensions (firmware temps, fan RPM, …) |

### `ContainerStats` (one element of `TelemetryPayload.container_stats`)

| Field | Type |
|-------|------|
| `container_guid` | `string` |
| `cpu_usage_ns` | `uint64` |
| `memory_usage_bytes` | `uint64` |
| `io_read_bytes`, `io_write_bytes` | `uint64` |

### `NicStats` (one element of `TelemetryPayload.nic_stats`)

| Field | Type |
|-------|------|
| `nic_id` | `string` |
| `packets_in/out`, `bytes_in/out`, `drops`, `errors` | `uint64` |

### `TelemetryPayload`

| Field | Type | Purpose |
|-------|------|---------|
| `timestamp` | `Timestamp` | Sample time |
| `container_stats` | `repeated ContainerStats` | Per-container counters |
| `nic_stats` | `repeated NicStats` | Per-NIC counters |
