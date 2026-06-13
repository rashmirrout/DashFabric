# `NicSpec` — Per-NIC reference bundle (ENI input)

> **TL;DR:** The "what should this one VM NIC look like?" record. The
> orchestrator writes it; FleetManager reads it, joins it with shared
> groups (route/ACL/meter) and the VNET, and programs the DPU. This is
> the most edited object in the system — per-VM policy edits land here.

**Topic:** `/config/v1/hosts/<device_id>/<container_guid>/<nic_id>/spec`
**Kind:** `CONFIG_KIND_NIC_SPEC`
**Scope:** per-NIC
**Lifecycle owner:** orchestrator
**Subscriber:** NO actor (one per NIC)

## Example

```json
{
  "nic_id": "eth0",
  "nic_type": "VM_NIC",
  "mode": "FULL_DUPLEX",
  "mac_address": "aa:bb:cc:dd:ee:ff",
  "vlan_id": 0,
  "vnet_id": "vnet-tenant-acme-prod",
  "ha_scope_id": "haset-westus2-pair-7",
  "primary_ip_v4": "10.42.0.5",
  "underlay_ip_v4": "100.64.7.5",
  "outbound": {
    "route_group_v4": "rg-acme-prod-default-v4",
    "acl_v4": ["acl-acme-vnic-default", "acl-acme-subnet-prod", ""],
    "meter_policy_id": "mp-acme-tier-gold-egress",
    "port_map_id": ""
  },
  "inbound": {
    "acl_v4": ["acl-acme-vnic-default-in", "acl-acme-subnet-prod-in", "acl-acme-vnet-default-in"],
    "meter_policy_id": "mp-acme-tier-gold-ingress"
  },
  "route_rules": [
    { "match": { "dst_prefix": "169.254.169.254/32" }, "action": { "kind": "REDIRECT_LOCAL_METADATA" } }
  ],
  "qos_id": "qos-tier-gold",
  "attributes": {
    "workload_class": "production",
    "billing_tag": "team-payments"
  }
}
```

Notice: every `*_id` field is a **string reference** to something defined
elsewhere (a VNET, an ACL group, a meter policy). FleetManager resolves
all of them together to build the actual DPU program.

## Purpose

The atomic unit of intent the orchestrator publishes for one virtual NIC on
a VM/container. Per decision **#1** this is a **reference bundle**, not a
denormalized ENI body — it carries the *ids* of shared groups (route,
ACL, meter, HA) and the VNET it lives in. The NO actor composes the full
[NicGoalState](./nic-goal-state.md) by joining these references against
the global, group, and VNET caches.

This is the most edited object in the system: per-VM ACL/route policy
edits all surface here.

## Fields

### Identity

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `nic_id` | `string` | yes | Orchestrator-chosen name unique within the container (e.g. `"eth0"`, `"mgmt0"`). Stable across restarts. |
| `nic_type` | `NicType` enum | yes | `VM_NIC`, `APPLIANCE_NIC`, `SLB_VIP`. Drives actor variant and ENI naming. **Decision #20.** |
| `mode` | `NicMode` enum | yes | `FULL_DUPLEX`, `INBOUND_ONLY`, `OUTBOUND_ONLY`. Validates which side blocks must be present. **Decision #20.** |
| `mac_address` | `MacAddress` | yes | Orchestrator-supplied. Used to derive `eni_id = "ENI_<DPU>_<MAC>"`. **Decision #13.** |
| `vlan_id` | `uint32` | no | 0 or omitted = no VLAN tagging. |

### Identity bindings

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `vnet_id` | `string` | yes | Foreign key into `/config/v1/vnet/<device>/<vnet_id>/`. The NO actor subscribes (via HDO refcount) on first attach. |
| `ha_scope_id` | `string` | no | Foreign key into a per-ENI HaScope record. Omitted if NIC is not HA-paired. |

### Addressing

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `primary_ip_v4` | `IpAddress` | no¹ | Primary IPv4 VIP. |
| `primary_ip_v6` | `IpAddress` | no¹ | Primary IPv6 VIP. |
| `secondary_ips_v4` | `repeated IpAddress` | no | Floating/secondary IPv4. |
| `secondary_ips_v6` | `repeated IpAddress` | no | Floating/secondary IPv6. |
| `underlay_ip_v4` | `IpAddress` | no | Underlay PA address for this ENI (omitted when DPU manages PA allocation). |

¹ At least one of `primary_ip_v4` or `primary_ip_v6` must be present.

### Outbound policy (omit entire block for `INBOUND_ONLY`)

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `outbound.route_group_v4` | `string` (RouteGroupId) | no² | Foreign key into `/config/v1/group/<device>/route_group/<id>/`. Materialized as `EniRoute{ family=v4 }` by FleetManager. **Decision #22.** |
| `outbound.route_group_v6` | `string` (RouteGroupId) | no² | Same for v6. |
| `outbound.acl_v4` | `repeated string` length 3 | no³ | `[stage1_group_id, stage2_group_id, stage3_group_id]` — VNIC → Subnet → VNET stages. Empty string = stage disabled. **Decision #21.** |
| `outbound.acl_v6` | `repeated string` length 3 | no³ | Same for v6. |
| `outbound.meter_policy_id` | `string` | no | Foreign key into `/config/v1/group/<device>/meter_policy/<id>/`. **Decision #24.** |
| `outbound.port_map_id` | `string` | no | Foreign key into `/config/v1/group/<device>/outbound_port_map/<id>/`. For NAT/SLB ENIs. |

² At least one of v4/v6 route groups present when the block is present.
³ Array must be exactly 3 elements if present.

### Inbound policy (omit entire block for `OUTBOUND_ONLY`)

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `inbound.acl_v4` | `repeated string` length 3 | no | Same 3-stage shape as outbound. |
| `inbound.acl_v6` | `repeated string` length 3 | no | Same for v6. |
| `inbound.meter_policy_id` | `string` | no | Inbound rate-limit/billing policy. **Decision #24.** |

### Inline per-ENI rules (not shared)

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `route_rules` | `repeated RouteRule` | no | Per-ENI policy-routing override rules. Each has match (prefix, port, proto) + action (next-hop, encap, drop). Inlined because they're not reusable across ENIs. **Decision #23.** |

### Bindings to globals

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `qos_id` | `string` | no | Foreign key into `/config/v1/global/<device>/qos/<id>/`. Per-ENI QoS profile. |
| `prefix_tag_refs` | `repeated string` | no | Foreign keys into prefix-tag globals; used by ACL/route rules that reference named prefix groups. |

### Validation & telemetry attributes

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `pin_to_pa` | `bool` | no | If true, requires PaValidation entry for this NIC's underlay IP. |
| `attributes` | `map<string,string>` | no | Free-form (`workload_class`, `billing_tag`, …). Surfaced in events and dashboards. |

### Validation rules

1. `nic_type=SLB_VIP` ⇒ `mode=INBOUND_ONLY` (always).
2. `mode=FULL_DUPLEX` ⇒ both `outbound` and `inbound` blocks present.
3. `mode=INBOUND_ONLY` ⇒ `outbound` absent, `inbound` present, all `*_route_group_*` absent.
4. `mode=OUTBOUND_ONLY` ⇒ `inbound` absent, `outbound` present.
5. `mac_address` must be globally unique on the DPU; collision → `VALIDATION_REJECTED{code=MAC_COLLISION}`.
6. All foreign keys are validated at composition time — missing refs put the NO in `WAITING_REFS`, not outright reject.
7. `outbound.acl_v4`/`v6` arrays must have length 3 or be omitted entirely.
8. `route_rules` count ≤ 256 per NIC (sanity cap; HERO test scale uses ≤ 64).

## Proto3 sketch

```proto
syntax = "proto3";
package fleetmanager.v1;

import "common.proto";

enum NicType {
  NIC_TYPE_UNSPECIFIED = 0;
  NIC_TYPE_VM_NIC      = 1;
  NIC_TYPE_APPLIANCE   = 2;
  NIC_TYPE_SLB_VIP     = 3;
}

enum NicMode {
  NIC_MODE_UNSPECIFIED   = 0;
  NIC_MODE_FULL_DUPLEX   = 1;
  NIC_MODE_INBOUND_ONLY  = 2;
  NIC_MODE_OUTBOUND_ONLY = 3;
}

message RouteRuleMatch {
  IpPrefix dst_prefix     = 1;
  IpPrefix src_prefix     = 2;
  uint32   dst_port_lo    = 3;
  uint32   dst_port_hi    = 4;
  uint32   protocol       = 5;   // IANA proto number
}

message RouteRuleAction {
  enum Kind {
    KIND_UNSPECIFIED        = 0;
    KIND_DROP               = 1;
    KIND_NEXT_HOP           = 2;
    KIND_REDIRECT_LOCAL_METADATA = 3;
    KIND_ENCAP_OVERRIDE     = 4;
  }
  Kind   kind          = 1;
  string next_hop_tunnel_id  = 2;   // for KIND_ENCAP_OVERRIDE
  IpAddress next_hop_underlay = 3;
}

message RouteRule {
  RouteRuleMatch  match  = 1;
  RouteRuleAction action = 2;
  uint32          priority = 3;   // lower = higher
}

message NicSpecOutbound {
  string route_group_v4   = 1;
  string route_group_v6   = 2;
  repeated string acl_v4  = 3;   // length 3 [vnic, subnet, vnet]
  repeated string acl_v6  = 4;
  string meter_policy_id  = 5;
  string port_map_id      = 6;
}

message NicSpecInbound {
  repeated string acl_v4  = 1;
  repeated string acl_v6  = 2;
  string meter_policy_id  = 3;
}

message NicSpec {
  // Identity
  string      nic_id        = 1;
  NicType     nic_type      = 2;
  NicMode     mode          = 3;
  MacAddress  mac_address   = 4;
  uint32      vlan_id       = 5;

  // Bindings
  string vnet_id      = 10;
  string ha_scope_id  = 11;

  // Addressing
  IpAddress primary_ip_v4              = 20;
  IpAddress primary_ip_v6              = 21;
  repeated IpAddress secondary_ips_v4  = 22;
  repeated IpAddress secondary_ips_v6  = 23;
  IpAddress underlay_ip_v4             = 24;

  // Policy
  NicSpecOutbound outbound = 30;
  NicSpecInbound  inbound  = 31;

  // Inline per-ENI rules
  repeated RouteRule route_rules = 40;

  // Bindings to globals
  string qos_id                       = 50;
  repeated string prefix_tag_refs     = 51;

  // Validation & metadata
  bool   pin_to_pa                    = 60;
  map<string, string> attributes      = 70;
}
```

## Relationships

- References:
  - `Vnet` (1) — must exist when programming.
  - `RouteGroup` (≤ 2) — outbound v4/v6.
  - `AclGroup` (≤ 6) — 3 stages × 2 directions, per family.
  - `MeterPolicy` (≤ 2) — inbound/outbound.
  - `OutboundPortMap` (≤ 1) — only for NAT/SLB roles.
  - `HaScope` (≤ 1) — when HA-paired.
  - `Qos` (≤ 1).
  - `PrefixTag` (N) — indirectly via ACL/route rule contents.
- Referenced by: `ContainerSpec` (parent) lists `nic_ids[]`.

## Change semantics

- Any field change bumps `metadata.revision`. NO actor recomposes
  NicGoalState, hashes, diffs against last-composed, emits delta plan.
- Changing `vnet_id` is a **migration** — actor unsubscribes old VNET (last
  one out triggers HDO unsub), subscribes new, drains+reprograms ENI.
- Changing `mac_address` requires DELETE+CREATE of the ENI (eni_id changes).
- `nic_type` and `mode` are effectively immutable; reject changes.

## Upstream DASH alignment

This spec is FM's *northbound* shape and does not map 1:1 to a single
upstream `DASH_*_TABLE`. Specifically:

- `nic_id`, `mac_address`, `vnet_id`, `qos_id`, `underlay_ip_v4`,
  `ha_scope_id`, and `pin_to_pa` flow into upstream `DASH_ENI_TABLE`
  (MAC-keyed, per-ENI). This is the only piece written to the DPU as an
  ENI row.
- `primary_ip_v4`/`primary_ip_v6` (and `secondary_ips_*`) **do not** land
  in `DASH_ENI_TABLE` — upstream DASH has no overlay-IP attribute on
  ENI. FM uses these to materialize the *self-entry* row in
  `DASH_VNET_MAPPING_TABLE` at compose time so traffic destined to this
  NIC's overlay address resolves correctly.
- `outbound.route_group_v4/v6` becomes a `DASH_ENI_ROUTE_TABLE` binding
  (per-ENI → RouteGroup pointer); the rules themselves live in
  `DASH_ROUTE_TABLE` under their group.
- `outbound.acl_v4/v6` and `inbound.acl_v4/v6` (3 stages each) fan out
  to `DASH_ACL_OUT_TABLE` and `DASH_ACL_IN_TABLE` — one row per ENI per
  stage per family.
- `route_rules` fan out to `DASH_ROUTE_RULE_TABLE` (per-ENI inbound
  override rules).
- `outbound.meter_policy_id` / `inbound.meter_policy_id` are bound by
  reference; the policy and rules live in `DASH_METER_POLICY` /
  `DASH_METER_RULE`.

In short: one `NicSpec` write triggers fan-out to ~6+ upstream DASH
tables at the southbound — assembly is FM's job, not the DPU's.

## See also

- [container-spec](./container-spec.md) — parent record that lists `nic_ids[]`.
- [vnet](./vnet.md) — every NIC binds to exactly one VNET.
- [route-group](./route-group.md), [acl-group](./acl-group.md), [meter-policy](./meter-policy.md), [outbound-port-map](./outbound-port-map.md) — the reusable policies referenced here.
- [ha-set](./ha-set.md) — when NICs are HA-paired (via `ha_scope_id`).
- [nic-goal-state](./nic-goal-state.md) — the composed/denormalized program derived from this spec.
- [envelope](./envelope.md) — wrapper around every published value.
