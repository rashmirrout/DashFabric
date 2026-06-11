# `Tunnel` — Underlay encap definition

> **TL;DR:** Defines *how to wrap a packet* before sending it on the
> physical network — encap type (VXLAN/Geneve/GRE), source PA, optional
> next-hops. Every VNET points at exactly one Tunnel.

**Topic:** `/config/v1/global/<device_id>/tunnel/<tunnel_id>`
**Kind:** `CONFIG_KIND_TUNNEL`
**Scope:** appliance-global (per device)
**Lifecycle owner:** orchestrator
**Subscriber:** HDO actor (process-wide cache)

## Example

```json
{
  "tunnel_id": "tun-vxlan-default-westus2",
  "encap_type": "VXLAN",
  "src_underlay_ip_v4": "100.64.7.5",
  "dst_underlay_ips_v4": [],
  "dst_group": "TOR_ANYCAST",
  "src_mac": "aa:bb:cc:00:00:01",
  "udp_dst_port": 4789,
  "attributes": {
    "purpose": "default-tenant-encap"
  }
}
```

## Purpose

`Vnet.tunnel_id` resolves here. The DPU uses the tunnel record to build
the outer header for traffic leaving an ENI in that VNET. A Tunnel is
appliance-scoped because `src_underlay_ip_v4` is device-specific (each
DPU has its own loopback).

## Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `tunnel_id` | `string` | yes | Identifier matching path segment. |
| `encap_type` | `EncapType` enum | yes | `VXLAN`, `GENEVE`, `NVGRE`. |
| `src_underlay_ip_v4` | `IpAddress` | yes¹ | Outer source IP. Usually matches `Appliance.loopback_ip_v4`. |
| `src_underlay_ip_v6` | `IpAddress` | yes¹ | IPv6 variant. |
| `dst_underlay_ips_v4` | `repeated IpAddress` | no | Explicit destination list. Empty when destination is derived per-flow from `VnetMapping.underlay_ip`. |
| `dst_group` | `string` | no | Logical destination group (e.g. `TOR_ANYCAST`) when destinations are advertised by underlay routing. |
| `src_mac` | `MacAddress` | no | Source MAC for the outer L2 frame on the underlay link. |
| `udp_dst_port` | `uint32` | no | VXLAN/Geneve UDP destination port. Defaults: VXLAN=4789, Geneve=6081. |
| `attributes` | `map<string,string>` | no | Free-form. |

¹ At least one src underlay IP required.

### Validation rules

1. `encap_type=VXLAN` requires `udp_dst_port=4789` if set.
2. `dst_underlay_ips_v4` and `dst_group` are mutually exclusive when both set explicitly.
3. `src_underlay_ip_v4` SHOULD equal `Appliance.loopback_ip_v4`; mismatch logs a warning, not a reject.

## Proto3 sketch

```proto
syntax = "proto3";
package fleetmanager.v1;

import "common.proto";   // IpAddress, MacAddress

enum EncapType {
  ENCAP_TYPE_UNSPECIFIED = 0;
  ENCAP_TYPE_VXLAN       = 1;
  ENCAP_TYPE_GENEVE      = 2;
  ENCAP_TYPE_NVGRE       = 3;
}

message Tunnel {
  string     tunnel_id          = 1;
  EncapType  encap_type         = 2;
  IpAddress  src_underlay_ip_v4 = 3;
  IpAddress  src_underlay_ip_v6 = 4;
  repeated IpAddress dst_underlay_ips_v4 = 5;
  string     dst_group          = 6;
  MacAddress src_mac            = 7;
  uint32     udp_dst_port       = 8;
  map<string,string> attributes = 20;
}
```

## Relationships

- Referenced by: `Vnet.tunnel_id` (1:N).
- References: none directly; pairs with `Appliance.loopback_ip_*`.

## Change semantics

- `src_underlay_ip_*` change requires reprogramming every ENI in every
  VNET that uses this tunnel. HDO fanouts the change.
- `udp_dst_port` change is data-plane disruptive; treat as maintenance.
- `attributes` updates are no-op for the data plane.

## See also

- [vnet](./vnet.md) — primary consumer.
- [appliance](./appliance.md) — owns the local loopback addresses.
- [README](./README.md) — full kind index.