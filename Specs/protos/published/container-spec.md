# `ContainerSpec` — Per-VM/container metadata

> **TL;DR:** The "what VM or container is this, and which NICs does it
> have?" record. It groups one or more `NicSpec`s under a common owner,
> tenant, and lifecycle. Per-VM properties (power state, pinning) live
> here; per-NIC policy lives in the child `NicSpec`s.

**Topic:** `/config/v1/hosts/<device_id>/<container_guid>/spec`
**Kind:** `CONFIG_KIND_CONTAINER_SPEC`
**Scope:** per-container (per-VM)
**Lifecycle owner:** orchestrator
**Subscriber:** CO actor (one per container)

## Example

```json
{
  "container_guid": "cont-vm-42-abcd1234",
  "tenant_id": "tenant-acme-corp",
  "container_type": "VM",
  "display_name": "acme-prod-web-7",
  "power_state": "RUNNING",
  "nic_ids": ["eth0", "eth1"],
  "placement": {
    "host_numa_node": 0,
    "cpu_pinning": "2,4,6,8",
    "memory_mb": 16384
  },
  "lifecycle": {
    "created_at": "2026-06-11T09:12:00Z",
    "expected_terminate_at": "",
    "drain_on_remove": true
  },
  "attributes": {
    "workload_class": "production",
    "billing_tag": "team-payments",
    "k8s_pod_uid": "ee11cc22-..."
  }
}
```

## Purpose

The CO actor uses `ContainerSpec` to:
1. Know which NIC ids to expect under `/<container_guid>/<nic_id>/spec/`.
2. Gate NIC programming on `power_state` (a `STOPPED` container's NICs
   may still be programmed but with a `disabled` hint).
3. Propagate tenant/billing attributes onto NicGoalState for telemetry.

The CO actor does **not** read `NicSpec` directly — that's the NO actor's
job. The CO just enumerates `nic_ids` and ensures one NO per id exists.

## Fields

### Identity

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `container_guid` | `string` | yes | Matches path segment. Stable for container's life. |
| `tenant_id` | `string` | yes | Tenant ownership; mirrors envelope's `tenant_id`. |
| `container_type` | `ContainerType` enum | yes | `VM`, `CONTAINER`, `BARE_METAL`. |
| `display_name` | `string` | no | Human-friendly label. |

### Lifecycle

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `power_state` | `PowerState` enum | yes | `RUNNING`, `STOPPED`, `SUSPENDED`, `TERMINATED`. |
| `lifecycle.created_at` | `Timestamp` | no | When the container was provisioned. |
| `lifecycle.expected_terminate_at` | `Timestamp` | no | Hint for housekeeping; not enforced. |
| `lifecycle.drain_on_remove` | `bool` | no | If true, on deletion the NIC programs are first set to drain (close to existing flows) before delete. |

### NIC enumeration

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `nic_ids` | `repeated string` | yes | List of `nic_id`s that must exist as children under this container. The CO actor refuses to mark `READY` until every listed NO is `PROGRAMMED`. |

### Placement / sizing (informational)

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `placement.host_numa_node` | `uint32` | no | NUMA node hint. |
| `placement.cpu_pinning` | `string` | no | Comma-separated CPU mask. |
| `placement.memory_mb` | `uint32` | no | Memory allocation hint. |
| `attributes` | `map<string,string>` | no | Free-form. |

### Validation rules

1. `container_guid` matches path segment.
2. `nic_ids` must be non-empty for `power_state=RUNNING`.
3. `nic_ids` are unique strings within the container.
4. `tenant_id` matches the envelope's `tenant_id`.
5. `power_state=TERMINATED` ⇒ orchestrator should DELETE the key shortly
   after; CO actor begins teardown.

## Proto3 sketch

```proto
syntax = "proto3";
package fleetmanager.v1;

import "google/protobuf/timestamp.proto";

enum ContainerType {
  CONTAINER_TYPE_UNSPECIFIED = 0;
  CONTAINER_TYPE_VM          = 1;
  CONTAINER_TYPE_CONTAINER   = 2;
  CONTAINER_TYPE_BARE_METAL  = 3;
}

enum PowerState {
  POWER_STATE_UNSPECIFIED = 0;
  POWER_STATE_RUNNING     = 1;
  POWER_STATE_STOPPED     = 2;
  POWER_STATE_SUSPENDED   = 3;
  POWER_STATE_TERMINATED  = 4;
}

message ContainerPlacement {
  uint32 host_numa_node = 1;
  string cpu_pinning    = 2;
  uint32 memory_mb      = 3;
}

message ContainerLifecycle {
  google.protobuf.Timestamp created_at            = 1;
  google.protobuf.Timestamp expected_terminate_at = 2;
  bool                      drain_on_remove        = 3;
}

message ContainerSpec {
  // Identity
  string         container_guid  = 1;
  string         tenant_id       = 2;
  ContainerType  container_type  = 3;
  string         display_name    = 4;

  // Lifecycle
  PowerState         power_state = 10;
  ContainerLifecycle lifecycle   = 11;

  // NIC enumeration
  repeated string nic_ids = 20;

  // Placement
  ContainerPlacement placement  = 30;
  map<string,string> attributes = 40;
}
```

## Relationships

- Parent of: every `NicSpec` under `/<container_guid>/<nic_id>/spec`.
- Child of: `HostSpec` (same device).
- Referenced by: CO actor at attach time.

## Change semantics

- **`nic_ids` addition**: CO spawns a new NO actor; NO subscribes to the
  child `NicSpec` key (or stays in `WAITING_SPEC` if not yet present).
- **`nic_ids` removal**: CO instructs the NO to drain (per
  `drain_on_remove`) then deletes itself; NO unsubscribes from VNET (HDO
  refcount drops).
- **`power_state` change**: cascades to NO actors which may issue
  enable/disable hints to the device program.
- **`tenant_id` change**: NOT allowed (immutable post-creation; reject).
- **Key DELETE**: CO actor enters `DELETING`; tears down all child NOs.

## See also

- [host-spec](./host-spec.md) — parent record.
- [nic-spec](./nic-spec.md) — child records, one per `nic_id`.
- [README](./README.md) — full kind index.