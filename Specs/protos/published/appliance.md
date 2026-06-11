# `Appliance` — DPU appliance config

> **TL;DR:** The "what is this DPU?" record — its identity, hardware
> capabilities, and fleet-wide knobs the orchestrator wants in effect on
> the box. One per device.

**Topic:** `/config/v1/global/<device_id>/appliance`
**Kind:** `CONFIG_KIND_APPLIANCE`
**Scope:** appliance-global (per device)
**Lifecycle owner:** orchestrator
**Subscriber:** HDO actor (one per device)

## Example

```json
{
  "device_id": "dpu-westus2-rack17-007",
  "hostname": "dpu007.westus2.example.net",
  "loopback_ip_v4": "100.64.7.5",
  "loopback_ip_v6": "fd00:fab::7:5",
  "vip_v4": "100.64.7.5",
  "asn": 65007,
  "site_id": "westus2-az1-rack17",
  "appliance_role": "DPU_TIER1",
  "capabilities": {
    "max_enis": 64,
    "max_acl_rules_per_eni": 4096,
    "max_routes_per_group": 8192,
    "supports_ha": true,
    "supports_pa_validation": true
  },
  "sw_version_min": "dash-2026.04",
  "attributes": {
    "vendor": "exampleinc",
    "fw_version": "1.7.3"
  }
}
```

## Purpose

Every DPU's HDO actor is bootstrapped from this single record. It sets
the device's underlay identity (loopback, ASN), declares what the
hardware can do (so the HDO refuses programs that would exceed limits),
and pins the minimum software version the orchestrator expects.

This object is **rare-change** — typically updated only at provisioning,
firmware upgrade, or capability re-discovery.

## Fields

### Identity

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `device_id` | `string` | yes | Globally unique DPU identifier; matches the etcd path segment. |
| `hostname` | `string` | yes | DNS hostname for the DPU. |
| `site_id` | `string` | yes | Logical site / AZ / rack identifier used for placement and sharding hints. |
| `appliance_role` | `string` | yes | Role enum (e.g. `DPU_TIER1`, `DPU_TIER2`, `APPLIANCE_SLB`). Drives which actor variants are allowed on the box. |

### Underlay addressing

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `loopback_ip_v4` | `IpAddress` | yes¹ | DPU's underlay loopback. Used as tunnel source by default. |
| `loopback_ip_v6` | `IpAddress` | yes¹ | IPv6 loopback. |
| `vip_v4` | `IpAddress` | no | Anycast/management VIP for the appliance. |
| `asn` | `uint32` | yes | BGP ASN advertised by this appliance to ToRs. |

¹ At least one of v4/v6 must be present.

### Capabilities (read-only from orchestrator's POV)

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `capabilities.max_enis` | `uint32` | yes | HW cap on simultaneously programmed ENIs. |
| `capabilities.max_acl_rules_per_eni` | `uint32` | yes | Per-ENI ACL rule budget. |
| `capabilities.max_routes_per_group` | `uint32` | yes | Per-RouteGroup route count cap. |
| `capabilities.supports_ha` | `bool` | yes | If false, all NICs on this device must have `ha_scope_id` absent. |
| `capabilities.supports_pa_validation` | `bool` | yes | If false, VNETs with `pa_validation_required=true` are rejected on this device. |

### Software gates

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `sw_version_min` | `string` | yes | Minimum DASH agent version FleetManager expects to talk to. HAL refuses to send programs if the device reports lower. |
| `attributes` | `map<string,string>` | no | Free-form: vendor, firmware version, asset tag, rack position. |

### Validation rules

1. `device_id` in payload must match the path segment.
2. At least one loopback must be set.
3. All `capabilities.*` values must be > 0.
4. `appliance_role=APPLIANCE_SLB` requires `capabilities.supports_pa_validation=true`.

## Proto3 sketch

```proto
syntax = "proto3";
package fleetmanager.v1;

import "common.proto";   // IpAddress

message ApplianceCapabilities {
  uint32 max_enis              = 1;
  uint32 max_acl_rules_per_eni = 2;
  uint32 max_routes_per_group  = 3;
  bool   supports_ha           = 4;
  bool   supports_pa_validation = 5;
}

message Appliance {
  // Identity
  string device_id      = 1;
  string hostname       = 2;
  string site_id        = 3;
  string appliance_role = 4;

  // Underlay
  IpAddress loopback_ip_v4 = 10;
  IpAddress loopback_ip_v6 = 11;
  IpAddress vip_v4         = 12;
  uint32    asn            = 13;

  // Capabilities
  ApplianceCapabilities capabilities = 20;

  // Software gates
  string sw_version_min          = 30;
  map<string,string> attributes  = 40;
}
```

## Relationships

- Referenced by: every other object scoped to `<device_id>`. The HDO
  actor uses `capabilities` to admission-control all downstream programs.
- References: none directly. Drives behavior of `HostSpec` and below.

## Change semantics

- Bumping `capabilities` may **unblock** previously rejected configs;
  HDO replays its `REJECTED` set on capability increase.
- Lowering `capabilities` is a **dangerous** edit — HDO must scan
  programmed ENIs for over-budget configs and emit `WARNING` errors
  rather than silently failing. Orchestrator should drain before
  lowering.
- `device_id`, `loopback_ip_*`, `asn` should be considered immutable
  during a device's life; changes require a full re-bootstrap.

## See also

- [host-spec](./host-spec.md) — sibling record about the host side of the device.
- [README](./README.md) — full kind index.
- [envelope](./envelope.md) — wrapper around every published value.