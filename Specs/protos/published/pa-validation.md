# `PaValidation` — Allowed underlay PA list for a VNET

> **TL;DR:** The whitelist of underlay (PA) IP addresses that may
> legitimately send VXLAN-decap'd traffic *into* this VNET. Protects
> against spoofed encap from untrusted underlays.

**Topic:** `/config/v1/vnet/<device_id>/<vnet_id>/pa_validation`
**Kind:** `CONFIG_KIND_PA_VALIDATION`
**Scope:** per-VNET, per device
**Lifecycle owner:** orchestrator
**Subscriber:** HDO actor (refcounted with the parent VNET)

## Example

```json
{
  "vnet_id": "vnet-tenant-acme-prod",
  "allowed_pa_v4": [
    "100.64.0.0/12",
    "172.20.0.0/16"
  ],
  "allowed_pa_v6": [
    "fd00:fab::/48"
  ],
  "default_action": "DROP",
  "attributes": {
    "policy_source": "fleet-ipam"
  }
}
```

## Purpose

When `Vnet.pa_validation_required = true`, the DPU drops decap'd packets
whose outer source is not in this list. This stops a misconfigured or
malicious neighbor from injecting traffic into the wrong tenant VNET.

PaValidation lives as a **sibling** of the VNET spec rather than as a
field on it so it can update at a different cadence (PA pools change as
the fleet grows; the VNET spec rarely changes).

## Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `vnet_id` | `string` | yes | Mirrors path segment; self-containment. |
| `allowed_pa_v4` | `repeated IpPrefix` | no¹ | Allowed IPv4 underlay prefixes. |
| `allowed_pa_v6` | `repeated IpPrefix` | no¹ | Allowed IPv6 underlay prefixes. |
| `default_action` | `PaDefaultAction` enum | yes | `DROP` or `ALLOW` for non-matching packets. Production should be `DROP`. |
| `attributes` | `map<string,string>` | no | Free-form: policy source, last update reason. |

¹ At least one of v4/v6 lists must be non-empty when `Vnet.pa_validation_required=true`.

### Validation rules

1. `vnet_id` matches path segment.
2. `default_action=ALLOW` warns if `Vnet.pa_validation_required=true` (effectively disables protection).
3. Total prefix count ≤ 4,096 (sanity cap).

## Proto3 sketch

```proto
syntax = "proto3";
package fleetmanager.v1;

import "common.proto";   // IpPrefix

enum PaDefaultAction {
  PA_DEFAULT_ACTION_UNSPECIFIED = 0;
  PA_DEFAULT_ACTION_DROP        = 1;
  PA_DEFAULT_ACTION_ALLOW       = 2;
}

message PaValidation {
  string             vnet_id        = 1;
  repeated IpPrefix  allowed_pa_v4  = 2;
  repeated IpPrefix  allowed_pa_v6  = 3;
  PaDefaultAction    default_action = 4;
  map<string,string> attributes     = 20;
}
```

## Relationships

- Sibling of: `Vnet` (same `<vnet_id>` subtree).
- Referenced by: `Vnet.pa_validation_required` (presence-required link, not by id).
- References: none.

## Change semantics

- Adding allowed prefixes is non-disruptive.
- Removing allowed prefixes is disruptive: legitimate decap'd traffic
  from those PAs starts dropping. Orchestrator should coordinate with
  the PA migration timeline.
- `default_action=DROP → ALLOW` is a security regression; logged loudly.

## See also

- [vnet](./vnet.md) — parent VNET that gates this validation via `pa_validation_required`.
- [vnet-mapping](./vnet-mapping.md) — sibling that holds address mappings.
- [README](./README.md) — full kind index.