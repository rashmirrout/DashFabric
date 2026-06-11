# `OutboundPortMap` + `OutboundPortMapRanges` — NAT/SLB port allocation

> **TL;DR:** A pool of source-NAT ports a NIC can use for outbound flows
> (e.g. when many VMs share a single VIP through SLB). The header
> describes the pool; the sibling ranges record carries the actual port
> ranges.

**Topics:**
- `/config/v1/group/<device_id>/outbound_port_map/<map_id>` → `OutboundPortMap`
- `/config/v1/group/<device_id>/outbound_port_map/<map_id>/ranges` → `OutboundPortMapRanges`

**Kinds:** `CONFIG_KIND_OUTBOUND_PORT_MAP`, `CONFIG_KIND_OUTBOUND_PORT_MAP_RANGES`
**Scope:** appliance-global
**Lifecycle owner:** orchestrator
**Subscriber:** HDO actor (process-wide cache)

## Example

```json
// /config/v1/group/dpu-007/outbound_port_map/opm-slb-vip-acme-prod
{
  "map_id": "opm-slb-vip-acme-prod",
  "vip_v4": "20.42.7.5",
  "protocol_mask": ["TCP", "UDP"],
  "range_count": 3,
  "attributes": { "vip_role": "slb-shared-tenant" }
}
```

```json
// /config/v1/group/dpu-007/outbound_port_map/opm-slb-vip-acme-prod/ranges
{
  "map_id": "opm-slb-vip-acme-prod",
  "revision": 11,
  "ranges": [
    { "protocol": "TCP", "port_lo": 1024,  "port_hi": 16383, "assigned_to": "ENI_dpu-007_aabbccddee01" },
    { "protocol": "TCP", "port_lo": 16384, "port_hi": 32767, "assigned_to": "ENI_dpu-007_aabbccddee02" },
    { "protocol": "UDP", "port_lo": 1024,  "port_hi": 65535, "assigned_to": "" }
  ]
}
```

A NIC with `outbound.port_map_id = "opm-slb-vip-acme-prod"` SNATs its
egress flows from this VIP using the assigned range.

## Purpose

NAT/SLB ENIs need a deterministic source-port range so that return
traffic can be hashed back to the right VM. The orchestrator owns the
allocation; FleetManager just programs whatever ranges are assigned.

Splitting header from `ranges` mirrors the route/acl/meter pattern:
the header is stable, the ranges payload is hot (flows in/out of the
assignment).

## `OutboundPortMap` fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `map_id` | `string` | yes | Matches path segment. |
| `vip_v4` | `IpAddress` | yes¹ | IPv4 VIP the ranges apply to. |
| `vip_v6` | `IpAddress` | yes¹ | IPv6 VIP. |
| `protocol_mask` | `repeated Protocol` enum | yes | Which L4 protocols this pool covers (`TCP`, `UDP`, `ICMP`). |
| `range_count` | `uint32` | no | Hint for sibling list size. |
| `attributes` | `map<string,string>` | no | Free-form. |

¹ At least one VIP required.

## `OutboundPortMapRanges` fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `map_id` | `string` | yes | Mirrors parent. |
| `revision` | `uint64` | yes | Mirrors `metadata.revision`. |
| `ranges` | `repeated PortRange` | yes | Allocation rows. |

### `PortRange`

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `protocol` | `Protocol` enum | yes | Must be one of `OutboundPortMap.protocol_mask`. |
| `port_lo` | `uint32` | yes | Inclusive low. |
| `port_hi` | `uint32` | yes | Inclusive high. |
| `assigned_to` | `string` | no | ENI id holding this range. Empty = unassigned/spare. |

### Validation rules

1. `OutboundPortMapRanges.map_id` matches path segment.
2. `port_lo ≤ port_hi`, both in [1, 65535].
3. Ranges with the same `protocol` must not overlap.
4. `assigned_to` (when set) must resolve to an existing ENI id.

## Proto3 sketch

```proto
syntax = "proto3";
package fleetmanager.v1;

import "common.proto";   // IpAddress

enum Protocol {
  PROTOCOL_UNSPECIFIED = 0;
  PROTOCOL_TCP         = 1;
  PROTOCOL_UDP         = 2;
  PROTOCOL_ICMP        = 3;
}

message OutboundPortMap {
  string             map_id        = 1;
  IpAddress          vip_v4        = 2;
  IpAddress          vip_v6        = 3;
  repeated Protocol  protocol_mask = 4;
  uint32             range_count   = 5;
  map<string,string> attributes    = 20;
}

message PortRange {
  Protocol protocol    = 1;
  uint32   port_lo     = 2;
  uint32   port_hi     = 3;
  string   assigned_to = 4;
}

message OutboundPortMapRanges {
  string   map_id              = 1;
  uint64   revision            = 2;
  repeated PortRange ranges    = 3;
}
```

## Relationships

- Referenced by: `NicSpec.outbound.port_map_id` (typically only NAT/SLB ENIs).
- References: ENI ids (logical, by string).
- Sibling: `OutboundPortMapRanges`.

## Change semantics

- **Range reassignment**: cascades to the NIC the range moves to (and
  from). HDO recomposes both affected NicGoalStates.
- **Adding a range**: no-op for existing NICs.
- **Removing a range** currently assigned to a NIC: data-plane disruptive
  for that NIC's existing flows; treat as managed change.
- Map deletion rejected if any NIC still references it.

## See also

- [nic-spec](./nic-spec.md) — primary consumer via `outbound.port_map_id`.
- [README](./README.md) — full kind index.