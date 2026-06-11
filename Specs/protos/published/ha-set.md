# `HaSet` — HA peering configuration

> **TL;DR:** Describes a pair (or set) of DPUs that mirror state to each
> other for high availability — who the peers are, how they talk to each
> other, and the failover policy. ENIs that should be HA-protected
> reference an `ha_scope_id` whose underlying set is an `HaSet`.

**Topic:** `/config/v1/global/<device_id>/ha_set/<ha_set_id>`
**Kind:** `CONFIG_KIND_HA_SET`
**Scope:** appliance-global on each member device
**Lifecycle owner:** orchestrator
**Subscriber:** HDO actor (process-wide cache)

## Example

```json
{
  "ha_set_id": "haset-westus2-pair-7",
  "members": [
    { "device_id": "dpu-westus2-rack17-007", "role": "PRIMARY",   "underlay_ip_v4": "100.64.7.5" },
    { "device_id": "dpu-westus2-rack18-008", "role": "SECONDARY", "underlay_ip_v4": "100.64.8.5" }
  ],
  "cp_data_channel_port": 4790,
  "dp_channel_port": 4791,
  "preempt": false,
  "failover_grace_seconds": 5,
  "attributes": {
    "purpose": "production-pair"
  }
}
```

A NIC inside `/config/v1/global/.../ha_scope/<scope>` referencing this
`ha_set_id` is wired up bidirectionally between the two DPUs.

## Purpose

Per decision **#9**: HA is split into two records: `HaSet` (the pair/set
relationship — appliance-global) and `HaScope` (the per-ENI binding —
NIC-scoped, lives inside `NicSpec.ha_scope_id`). This lets the HDO build
the pair channel once and the NO actor decide per-ENI participation.

## Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `ha_set_id` | `string` | yes | Matches path segment. |
| `members` | `repeated HaMember` | yes | 2..N peer DPUs. |
| `cp_data_channel_port` | `uint32` | yes | UDP port for control-plane state sync. |
| `dp_channel_port` | `uint32` | yes | UDP port for data-plane bulk sync. |
| `preempt` | `bool` | no | If true, primary reclaims role when it recovers. |
| `failover_grace_seconds` | `uint32` | no | Time before secondary promotes itself. |
| `attributes` | `map<string,string>` | no | Free-form. |

### `HaMember`

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `device_id` | `string` | yes | DPU id (must have an `Appliance` record). |
| `role` | `HaRole` enum | yes | `PRIMARY`, `SECONDARY`. |
| `underlay_ip_v4` | `IpAddress` | yes | Peer-reachability address (the sync tunnel src/dst). |

### Validation rules

1. `members` size ≥ 2; exactly one `PRIMARY` initially.
2. Each member `device_id` must resolve to an `Appliance` with `capabilities.supports_ha=true`.
3. `cp_data_channel_port` and `dp_channel_port` must differ.
4. `failover_grace_seconds` ∈ [1, 60].

## Proto3 sketch

```proto
syntax = "proto3";
package fleetmanager.v1;

import "common.proto";   // IpAddress

enum HaRole {
  HA_ROLE_UNSPECIFIED = 0;
  HA_ROLE_PRIMARY     = 1;
  HA_ROLE_SECONDARY   = 2;
}

message HaMember {
  string    device_id      = 1;
  HaRole    role           = 2;
  IpAddress underlay_ip_v4 = 3;
}

message HaSet {
  string ha_set_id                     = 1;
  repeated HaMember members            = 2;
  uint32 cp_data_channel_port          = 3;
  uint32 dp_channel_port               = 4;
  bool   preempt                       = 5;
  uint32 failover_grace_seconds        = 6;
  map<string,string> attributes        = 20;
}
```

## Relationships

- Referenced by: `HaScope` records (and indirectly by `NicSpec.ha_scope_id`).
- References: `Appliance` (must exist for each member).

## Change semantics

- **Member change** is a major event: HDO tears the old sync channel,
  builds the new one, and may bounce ENI HA state.
- `preempt` and `failover_grace_seconds` are runtime-tunable.
- `*_port` changes require coordinated restart with peer.

## See also

- [nic-spec](./nic-spec.md) — references via `ha_scope_id` (resolved through HaScope → HaSet).
- [appliance](./appliance.md) — required member precondition.
- [README](./README.md) — full kind index.