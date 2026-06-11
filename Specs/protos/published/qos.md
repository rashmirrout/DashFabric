# `Qos` — QoS profile

> **TL;DR:** A bandwidth/queue/DSCP profile a NIC can reference by id.
> Defines how the DPU rate-limits and prioritizes traffic for that ENI.

**Topic:** `/config/v1/global/<device_id>/qos/<qos_id>`
**Kind:** `CONFIG_KIND_QOS`
**Scope:** appliance-global
**Lifecycle owner:** orchestrator
**Subscriber:** HDO actor (process-wide cache)

## Example

```json
{
  "qos_id": "qos-tier-gold",
  "bw_gbps": 25,
  "burst_size_mb": 64,
  "queue_count": 8,
  "dscp_remap": [
    { "from_dscp": 46, "to_dscp": 46, "priority": 0 },
    { "from_dscp": 0,  "to_dscp": 0,  "priority": 7 }
  ],
  "attributes": {
    "tier": "gold"
  }
}
```

## Purpose

QoS is split out so the same profile can be reused across hundreds of
ENIs without each `NicSpec` carrying the same bandwidth/queue config.
`NicSpec.qos_id` resolves here at composition time.

## Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `qos_id` | `string` | yes | Matches path segment. |
| `bw_gbps` | `uint32` | yes | Sustained rate cap in Gbps. |
| `burst_size_mb` | `uint32` | no | Token-bucket burst size. |
| `queue_count` | `uint32` | no | Number of priority queues to allocate. |
| `dscp_remap` | `repeated DscpRemapEntry` | no | DSCP rewrite + priority mapping table. |
| `attributes` | `map<string,string>` | no | Free-form: tier, billing class. |

### `DscpRemapEntry`

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `from_dscp` | `uint32` | yes | Inbound DSCP (0–63). |
| `to_dscp` | `uint32` | yes | Rewritten DSCP. |
| `priority` | `uint32` | yes | Queue priority (lower = higher). |

### Validation rules

1. `bw_gbps > 0`, `≤ device line rate` (verified against Appliance capabilities).
2. `dscp_remap[*].from_dscp` and `to_dscp` in [0,63].
3. `dscp_remap[*].priority < queue_count`.

## Proto3 sketch

```proto
syntax = "proto3";
package fleetmanager.v1;

message DscpRemapEntry {
  uint32 from_dscp = 1;
  uint32 to_dscp   = 2;
  uint32 priority  = 3;
}

message Qos {
  string qos_id           = 1;
  uint32 bw_gbps          = 2;
  uint32 burst_size_mb    = 3;
  uint32 queue_count      = 4;
  repeated DscpRemapEntry dscp_remap = 5;
  map<string,string> attributes      = 20;
}
```

## Relationships

- Referenced by: `NicSpec.qos_id` (1:N).
- References: none.

## Change semantics

- Edits cascade to every NicGoalState that references this `qos_id`.
- Lowering `bw_gbps` is data-plane disruptive for hot flows; treat as policy change with notice.

## See also

- [nic-spec](./nic-spec.md) — primary consumer via `qos_id`.
- [appliance](./appliance.md) — for line-rate validation.
- [README](./README.md) — full kind index.