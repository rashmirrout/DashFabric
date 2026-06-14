# DASH Compliance: Missing Objects & NicConfig Gaps (Phase 2 Design)

> **Status:** Detailed specification for 5 missing objects + NicConfig enhancement
> **Purpose:** Design decisions BEFORE Phase 2 implementation, ensuring DASH compliance
> **Date:** 2026-06-14
> **Audience:** FM architects; prep work for Phase 2 implementation

---

## Overview: What's Deferred & Why It Matters

**5 objects missing from Phase 1:**
1. **Tunnel** — Referenced in route.tunnel_id, but no proto definition
2. **OutboundPortMap** — SNAT pool referenced in route.snat_pool_id
3. **PrefixTag** — Named IP lists (CP expands; low priority)
4. **RoutingType** — Vendor action catalog (CP owns; low priority)
5. **PaValidation** — Anti-spoofing whitelist (optional security feature)

**NicConfig gaps (workarounds exist for Phase 1):**
- Multi-stage ACL binding (3 stages × 2 directions = 6 slots per family)
- Meter policy binding (ENI-level meters + per-route override)
- Tunnel override (ENI-level tunnel + per-mapping override)
- HA membership (ha_set_id + ha_scope)
- Per-ENI route overrides (route_rules[])

**Strategy:** Phase 1 uses per-route/per-mapping bindings as workaround. Phase 2 adds ENI-level bindings for operational convenience.

---

## Missing Object 1: Tunnel Proto

### Context

**DASH spec (Chapter 09):** Tunnel is a device-global encap profile. Routes reference tunnels by ID. VNETs declare default tunnel.

**Current state in FM:**
- route.proto has tunnel_id field ✅
- vnet.proto has default_tunnel_id field ✅
- TunnelRegistry manages tunnel state (in-memory) ✅
- BUT: No formal proto schema for `/config/tunnels/<tunnel_id>` topic

### Design Decision: Tunnel Proto

**Proto definition:**

```protobuf
// cb_fm_protos/topics/tunnel.proto
//
// Topic: /dashfabric/v1/config/tunnels/<tunnel_id>    (compacted)
// DASH lineage: DASH_TUNNEL object
//
// Device-global tunnel encap profiles. Referenced by routes + VNETs.

syntax = "proto3";
package dashfabric.cb.v1;
option go_package = "github.com/dashfabric/cb-fm-protos/topics;cbtopics";

import "common/annotations.proto";
import "common/dash_types.proto";

message Tunnel {
  option (dash_table) = "DASH_TUNNEL";
  option (proto_major) = 1;

  string tunnel_id = 1 [(field) = UPSTREAM, canonical = true,
                        (dash_attr) = "SAI_TUNNEL_ATTR_TUNNEL_ID"];

  EncapType encap_type = 2 [(field) = UPSTREAM, canonical = true,
                            (dash_attr) = "SAI_TUNNEL_ATTR_TYPE"];

  // Source underlay IP (local DPU's PA).
  IpAddress src_underlay_ip = 3 [(field) = UPSTREAM, canonical = true,
                                 (dash_attr) = "SAI_TUNNEL_ATTR_SRC_IP"];

  // Destination underlay IPs (remote PA addresses).
  // Used for tunnels with fixed remote (e.g., internet gateway).
  // If empty, destination is derived from mapping table (VNET action).
  repeated IpAddress dst_underlay_ips = 4 [(field) = UPSTREAM, canonical = true,
                                           (dash_attr) = "SAI_TUNNEL_ATTR_DST_IPS"];

  // Destination group (for ECMP, load spreading).
  // Vendor-specific; e.g., "anycast-internet-gw-west".
  string dst_group = 5 [(field) = UPSTREAM];

  // UDP destination port (4789 for VXLAN, 6081 for GENEVE).
  uint32 udp_dst_port = 6 [(field) = UPSTREAM,
                           (dash_attr) = "SAI_TUNNEL_ATTR_DST_PORT"];

  // Optional: underlay VLAN tag (if tunnel carries tagged traffic).
  uint32 underlay_vlan = 7 [(field) = UPSTREAM];

  // Admin state.
  AdminState admin_state = 20 [(field) = UPSTREAM];

  // Envelope.
  string tenant = 50 [(field) = ENVELOPE];
  string region = 51 [(field) = ENVELOPE];
  string fabric_id = 52 [(field) = ENVELOPE];
}

enum EncapType {
  ENCAP_TYPE_UNSPECIFIED = 0;
  VXLAN  = 1;
  GENEVE = 2;
  NVGRE  = 3;
}
```

### When to Create tunnel.proto

**Trigger:** When tunnel provisioning flow stabilizes (after Phase 1 core logic works).

**Implementation order:**
1. Phase 1: TunnelRegistry works with in-memory state; no proto schema needed
2. Phase 1.5: Tunnel provisioning end-to-end (CP → FM → dataplane) works; tunnel state is stable
3. Phase 2: Formalize tunnel.proto once schema is validated in production

### DASH Compliance

- ✅ Device-global scope (one Tunnel object per device, referenced by many routes)
- ✅ EncapType enum matches DASH (VXLAN, GENEVE, NVGRE)
- ✅ src/dst IP support matches DASH routing pipeline
- ✅ AdminState for lifecycle management
- ✅ No VNET-specific tunneling (tunnels are shared infrastructure)

---

## Missing Object 2: OutboundPortMap (SNAT Pool)

### Context

**DASH spec (Chapter 08 + 06):** OutboundPortMap (NAT pool) holds port ranges for SNAT. Per-ENI binding. Per-route reference also possible.

**Current state in FM:**
- route.snat_pool_id field ✅ (routes can reference SNAT pool)
- SnatPoolRegistry manages pool state (in-memory) ✅
- BUT: No formal proto schema for `/config/snat-pools/<pool_id>` topic
- NicConfig lacks outbound_port_map_id field ❌ (ENI-level binding)

### Design Decision: OutboundPortMap Proto

**Proto definition:**

```protobuf
// cb_fm_protos/topics/outbound-port-map.proto
//
// Topic: /dashfabric/v1/config/snat-pools/<snat_pool_id>    (compacted)
// DASH lineage: DASH_OUTBOUND_PORT_MAP
//
// SNAT pool: port allocation for outbound NAT (per-tenant).

syntax = "proto3";
package dashfabric.cb.v1;
option go_package = "github.com/dashfabric/cb-fm-protos/topics;cbtopics";

import "common/annotations.proto";
import "common/dash_types.proto";

message OutboundPortMap {
  option (dash_table) = "DASH_OUTBOUND_PORT_MAP";
  option (proto_major) = 1;

  string snat_pool_id = 1 [(field) = UPSTREAM, canonical = true];

  // Primary SNAT IP (the address traffic is NATted to).
  IpAddress snat_ip = 2 [(field) = UPSTREAM, canonical = true];

  // Port range for SNAT.
  PortRange port_range = 3 [(field) = UPSTREAM, canonical = true];

  // Optional: backup SNAT IP (for HA scenarios).
  IpAddress backup_snat_ip = 4 [(field) = UPSTREAM];

  // Admin state.
  AdminState admin_state = 20 [(field) = UPSTREAM];

  // Envelope.
  string tenant = 50 [(field) = ENVELOPE];
  string region = 51 [(field) = ENVELOPE];
  string fabric_id = 52 [(field) = ENVELOPE];
}

message PortRange {
  uint32 lo = 1 [(field) = UPSTREAM, canonical = true];  // Inclusive
  uint32 hi = 2 [(field) = UPSTREAM, canonical = true];  // Inclusive
}
```

### When to Create outbound-port-map.proto

**Trigger:** When SNAT pool provisioning and port allocation strategy is finalized.

**Implementation order:**
1. Phase 1: SnatPoolRegistry works with in-memory state; per-route SNAT references work
2. Phase 1.5: SNAT pool provisioning end-to-end (CP → FM → dataplane) works
3. Phase 2: Formalize outbound-port-map.proto + add ENI-level binding

### DASH Compliance

- ✅ Group-scope object (shared by many ENIs)
- ✅ Per-pool IP + port range captures SNAT semantics
- ✅ Backup IP for HA failover
- ✅ AdminState for lifecycle management

---

## Missing Object 3: PrefixTag Proto (Optional, Low Priority)

### Context

**DASH spec (Chapter 07):** PrefixTag is a named list of IP prefixes (e.g., `tag-azure-storage`). Referenced in ACL + meter rules for matching.

**Current state in FM:**
- ACL rules don't expose tag_refs in acl.proto (CP expands tags before sending)
- Meter rules don't expose tag_refs in meter.proto (CP expands before sending)
- FM receives fully-expanded rules with literal prefixes
- CP owns tag versioning + expansion

### Design Decision: DEFER to Phase 2 (or Skip Entirely)

**Rationale:**
1. CP expands tags during composition; FM never sees tag IDs directly
2. FM receives rules with literal prefix lists; no tag lookup needed
3. Tag management is CP concern; FM is a dataplane orchestrator

**If needed in future (low probability):**

```protobuf
message PrefixTag {
  string tag_id = 1;
  repeated IpPrefix prefixes = 2;
}
```

**Recommendation:** Skip for Phase 1 + Phase 2. Only add if operational request requires it.

---

## Missing Object 4: RoutingType Proto (Optional, Low Priority)

### Context

**DASH spec (Chapter 06):** RoutingType is a fleet-wide catalog of action templates. Routes reference RoutingType by name. Enables extensible routing actions.

**Current state in FM:**
- route.proto has action field (enum RouteAction with fixed values) ✅
- FM validates action exists in RouteAction enum
- CP owns RoutingType versioning

### Design Decision: DEFER to Phase 2 (or Implement as Comment)

**Rationale:**
1. RouteAction enum is FM's view of available actions
2. CP manages RoutingType catalog; FM doesn't compose it
3. Multivendor support (why RoutingType exists) not a Phase 1 priority

**If needed in future (for multivendor support):**

```protobuf
message RoutingType {
  string routing_type_id = 1;  // e.g., "privatelink-v1", "service-tunnel-v2"
  repeated RoutingTypeEntry items = 2;
}

message RoutingTypeEntry {
  string action_name = 1;      // e.g., "privatelink"
  RouteAction action_type = 2; // Enum value
  EncapType encap_type = 3;    // VXLAN, GENEVE, etc.
  // ... vendor-specific extras
}
```

**Recommendation:** Skip for Phase 1. FM's enum-based action validation is sufficient. Add proto in Phase 2 if multivendor becomes real requirement.

---

## Missing Object 5: PaValidation Proto (Optional, Security Feature)

### Context

**DASH spec (Chapter 04):** PaValidation is an anti-spoofing whitelist. Per-VNET; lists which underlay PAs are allowed to decap into this VNET.

**Current state in FM:**
- Not protobuffed
- Decap validation happens in dataplane (DPU HAL), not FM
- FM doesn't validate PaValidation; just passes it to HAL

### Design Decision: DEFER to Phase 2 (Optional Security Feature)

**Rationale:**
1. Anti-spoofing is a dataplane / HAL concern, not FM orchestration
2. FM doesn't need to understand PA whitelist; just programs rules
3. Low operational priority for Phase 1

**If needed in future (security-conscious clouds):**

```protobuf
message PaValidation {
  string vnet_id = 1;
  repeated IpAddress allowed_underlay_pas = 2;
  AdminState admin_state = 3;
}
```

**Recommendation:** Skip for Phase 1. Add in Phase 2 if cloud security posture requires it.

---

## NicConfig Enhancements (Phase 2)

### Problem: Current NicConfig Gaps

```protobuf
// Current (Phase 1):
message NicConfig {
  string eni_id = 1;
  string mac = 2;
  string vnet_id = 4;
  string acl_group_in = 10;   // Stage 1 only!
  string acl_group_out = 11;  // Stage 1 only!
  string route_group_id = 20;
  // Missing: meter, tunnel, HA, port map, route overrides
}

// Phase 1 Workaround:
// - Multi-stage ACLs: assume stage 1 only; stage 2+3 handled externally
// - Meter: use route.meter_id per-route
// - Tunnel override: use route.tunnel_id + vnet.default_tunnel_id
// - SNAT pool: use route.snat_pool_id per-route
// - HA: CP manages; FM doesn't express
// - Route overrides: use shared RouteGroup only
```

### Enhanced NicConfig (Phase 2)

**Proto enhancement:**

```protobuf
message NicConfig {
  // Existing fields (Phase 1)
  string eni_id = 1 [(field) = SYNTHETIC, canonical = true];
  string mac = 2 [(field) = UPSTREAM, canonical = true];
  string dpu_id = 3 [(field) = UPSTREAM, canonical = true];
  string vnet_id = 4 [(field) = UPSTREAM, canonical = true];
  string route_group_id = 20 [(field) = UPSTREAM, canonical = true];

  // NEW (Phase 2): Multi-stage ACL bindings
  // Outbound: 3 stages (VNIC, SUBNET, VNET)
  repeated string acl_group_ids_v4_out = 30 [(field) = UPSTREAM];  // [stage1, stage2, stage3]
  repeated string acl_group_ids_v6_out = 31 [(field) = UPSTREAM];
  // Inbound: 3 stages
  repeated string acl_group_ids_v4_in = 32 [(field) = UPSTREAM];
  repeated string acl_group_ids_v6_in = 33 [(field) = UPSTREAM];

  // NEW (Phase 2): ENI-level meter policies
  string meter_policy_id_out = 40 [(field) = UPSTREAM];  // Outbound metering
  string meter_policy_id_in = 41 [(field) = UPSTREAM];   // Inbound metering

  // NEW (Phase 2): ENI-level tunnel override
  string tunnel_id = 42 [(field) = UPSTREAM];  // Override VNET default

  // NEW (Phase 2): SNAT pool binding
  string outbound_port_map_id = 43 [(field) = UPSTREAM];

  // NEW (Phase 2): HA membership
  string ha_set_id = 44 [(field) = UPSTREAM];
  HaScope ha_scope = 45 [(field) = UPSTREAM];  // DPU-level or ENI-level HA

  // NEW (Phase 2): Per-ENI route overrides (small list)
  repeated RouteRule route_rules = 46 [(field) = UPSTREAM];

  // NEW (Phase 2): QoS binding
  string qos_id = 47 [(field) = UPSTREAM];

  // Admin state
  AdminState admin_state = 50 [(field) = UPSTREAM];

  // Envelope
  string tenant = 60 [(field) = ENVELOPE];
  string region = 61 [(field) = ENVELOPE];
  string fabric_id = 62 [(field) = ENVELOPE];
}

// Per-ENI route override
message RouteRule {
  IpPrefix match_prefix = 1 [(field) = UPSTREAM, canonical = true];
  RouteAction action = 2 [(field) = UPSTREAM, canonical = true];
  uint32 priority = 3 [(field) = UPSTREAM];
  // Optional: tunnel_id, snat_pool_id for override
}
```

### Rationale for Each Enhancement

| Field | Why needed | Phase 1 workaround | Phase 2 benefit |
|-------|-----------|---|---|
| acl_group_ids_v4_out[] | Multi-stage ACL (3 stages × 2 directions) | Assume stage 1 only | Expose full ACL pipeline to CP; FM understands all stages |
| meter_policy_id_out/in | ENI-level meter policy | Use route.meter_id per-route | Simpler for CP to bind one policy per ENI + allow per-route override |
| tunnel_id | ENI-level tunnel override | Use route.tunnel_id + vnet.default | Explicit override capability |
| outbound_port_map_id | ENI-level SNAT pool | Use route.snat_pool_id per-route | Simpler operational model (one pool per ENI) |
| ha_set_id + ha_scope | HA membership | CP manages externally | FM understands HA topology for state machine |
| route_rules[] | Per-ENI route overrides | Use shared RouteGroup only | Support rare use cases (metadata redirect, per-ENI exceptions) |
| qos_id | ENI-level QoS binding | Not exposed in Phase 1 | Explicit QoS binding if needed |

### When to Implement NicConfig Enhancements

**Trigger:** After Phase 1 ENI hydration is working end-to-end.

**Approach:**
1. Phase 1: Ship with minimal NicConfig (phase 1 fields only)
2. Onboard first customers; validate core routing works
3. Phase 1.5: Gather feedback on operational convenience
4. Phase 2 (if needed): Add multi-stage ACL, meter, HA fields based on feedback
5. Phase 2+ (if needed): Add route_rules, qos fields for advanced use cases

### Backward Compatibility

**Ensure new fields are optional:**

```protobuf
// When FM receives old NicConfig (Phase 1 format):
// - Treat empty acl_group_ids_* as "single-stage binding in acl_group_in/out"
// - Treat missing meter_policy_id_* as "use route-level meters"
// - Treat missing tunnel_id as "use vnet.default_tunnel_id"
// etc.
```

---

## Implementation Sequence: Phase 2 Roadmap

**Month 1 (Post Phase 1):**
- Formalize tunnel.proto once tunnel provisioning is proven
- Formalize outbound-port-map.proto once SNAT pool provisioning is proven

**Month 2:**
- Enhance NicConfig with multi-stage ACL fields
- Add meter/tunnel/HA/pool fields to NicConfig
- Update NicRegistry.Hydrate() to handle multi-stage ACLs

**Month 3:**
- Add route_rules[] support (per-ENI overrides)
- Validate backward compatibility with Phase 1 NicConfig format

**Month 4+ (Optional):**
- Add PrefixTag proto if tag management becomes FM responsibility
- Add RoutingType proto if multivendor support needed
- Add PaValidation proto if anti-spoofing becomes critical

---

## Success Criteria: DASH Compliance After Phase 2

**Target: 95%+ DASH compliance (14/15 objects protobuffed)**

| Object | Phase 1 | Phase 2 | Status |
|--------|---------|---------|--------|
| Appliance | ✅ | — | COMPLETE |
| Vnet | ✅ | — | COMPLETE |
| VnetMapping | ✅ | — | COMPLETE |
| RouteGroup + RouteEntry | ✅ | — | COMPLETE |
| AclGroup + AclRule | ✅ | + multi-stage | ENHANCED |
| MeterPolicy + MeterRule | ✅ | — | COMPLETE |
| HaSet | ✅ | — | COMPLETE |
| ENI (NicConfig) | ✅ Core | + bindings | ENHANCED |
| **Tunnel** | Semantic | ✅ Proto | COMPLETE |
| **OutboundPortMap** | Semantic | ✅ Proto | COMPLETE |
| InboundRoutingRule | — | ✅ (if needed) | OPTIONAL |
| PrefixTag | — | ✅ (if needed) | OPTIONAL |
| RoutingType | — | ✅ (if needed) | OPTIONAL |
| PaValidation | — | ✅ (if needed) | OPTIONAL |

**Not included (by design):**
- Qos (dataplane concern; FM doesn't compose)
- HostSpec (abstracted into DeviceConfig)

---

## Recommendation: Before Starting Phase 2 Code

1. **Confirm tunnel provisioning is stable** → formalize tunnel.proto
2. **Confirm SNAT pool provisioning is stable** → formalize outbound-port-map.proto
3. **Gather customer feedback** on NicConfig from Phase 1 → decide which enhancements matter most
4. **Plan in sprints:** Proto formalization (sprint 1), NicConfig enhancement (sprint 2), optional objects (sprint 3+)

**Proceed with Phase 1 implementation now.** These Phase 2 designs are documented + frozen; can be implemented once Phase 1 is stable.
