# NicGoalState Schema & Composition Algorithm

**Status:** AUTHORITATIVE
**Owner:** FM Architecture
**Supersedes:** vm-eni-provisioning-design.md §3 (pseudocode shape)
**Date:** 2026-06-22

---

## 1. PURPOSE

Defines the exact proto3 schema, composition algorithm, and content-hash specification for `NicGoalState` — the unit of programmable state that the NicActor (NO) emits to the SouthboundDriver. This is the contract between FM's compose layer and the HAL/CB plugins.

**Why it matters:** Two compose runs with identical inputs MUST produce byte-identical `NicGoalState` and identical `content_hash`. Without canonicalization rules, the dedup/idempotency guarantees break.

---

## 2. PROTO3 SCHEMA

```protobuf
syntax = "proto3";
package dashfabric.fm.v1;

import "google/protobuf/timestamp.proto";

// Top-level goal state for a single ENI.
// Composed by NicActor from: NicSpec + VNET data + Group data + Global data.
message NicGoalState {
  string eni_id = 1;                    // Canonical: "ENI_<DPU>_<MAC>"
  string device_id = 2;                 // Parent device for routing
  string vnet_id = 3;                   // Owning VNET

  // Identity (from NicSpec)
  NicIdentity identity = 10;

  // Programmable objects (ordered for canonical serialization)
  repeated RouteEntry routes = 20;      // Sorted by (prefix, vrf_id)
  repeated AclStage acl_stages = 21;    // 6 fixed slots, indexed
  repeated VipMapping vip_mappings = 22;// Sorted by vip
  MeterPolicy meter_policy = 23;
  HaConfig ha_config = 24;

  // Composition metadata
  string content_hash = 90;             // SHA256 hex (lowercase, 64 chars)
  int64 composition_revision = 91;      // Monotonic counter from NicActor
  google.protobuf.Timestamp composed_at = 92;
  repeated string source_keys = 93;     // T1 keys read during composition (for audit)
}

// ─── Identity ─────────────────────────────────────────
message NicIdentity {
  string mac = 1;                       // Format: "02:00:00:11:22:33"
  string primary_ip = 2;                // IPv4 or IPv6 string
  uint32 vlan_id = 3;                   // 0 = untagged
  string tenant_id = 4;
  string ha_scope_id = 5;
}

// ─── Routes ──────────────────────────────────────────
message RouteEntry {
  string prefix = 1;                    // "10.0.0.0/24" — must be canonical CIDR
  string vrf_id = 2;
  RoutingAction action = 3;
  uint32 priority = 4;                  // Lower = higher precedence
  string source_group_id = 5;           // For audit/trace
}

message RoutingAction {
  oneof action_type {
    EncapAction encap = 1;
    DecapAction decap = 2;
    DropAction drop = 3;
    ForwardAction forward = 4;
  }
}

message EncapAction {
  string encap_type = 1;                // "vxlan" | "geneve" | "mpls"
  uint32 vni = 2;
  string underlay_dst = 3;
  string underlay_src = 4;
}

message DecapAction {
  string encap_type = 1;
  uint32 vni = 2;
}

message DropAction {
  string reason = 1;                    // "policy" | "blackhole" | "tenant_isolation"
}

message ForwardAction {
  string next_hop = 1;
  uint32 next_vrf_id = 2;
}

// ─── ACLs ────────────────────────────────────────────
// Fixed 6-slot layout: [v4_inbound_s1, v4_outbound_s1, v6_inbound_s1, v6_outbound_s1, v4_inbound_s2, v4_outbound_s2]
// Stage 1 = pre-routing, Stage 2 = post-routing
message AclStage {
  uint32 slot_index = 1;                // 0..5 (fixed positions)
  string family = 2;                    // "ipv4" | "ipv6"
  string direction = 3;                 // "inbound" | "outbound"
  uint32 stage = 4;                     // 1 | 2
  string acl_group_id = 5;              // Reference for audit
  repeated AclRule rules = 6;           // Sorted by priority ascending
}

message AclRule {
  uint32 priority = 1;
  string src_prefix = 2;                // Canonical CIDR
  string dst_prefix = 3;
  uint32 src_port_min = 4;
  uint32 src_port_max = 5;
  uint32 dst_port_min = 6;
  uint32 dst_port_max = 7;
  string protocol = 8;                  // "tcp" | "udp" | "icmp" | "*"
  AclAction action = 9;
}

enum AclAction {
  ACL_ACTION_UNSPECIFIED = 0;
  ACL_ACTION_PERMIT = 1;
  ACL_ACTION_DENY = 2;
  ACL_ACTION_LOG = 3;
}

// ─── VIP Mappings ────────────────────────────────────
message VipMapping {
  string vip = 1;                       // Canonical IP string
  string dip = 2;
  bool snat_enabled = 3;
  string nat_pool_id = 4;               // Empty if SNAT disabled
  string binding_state = 5;             // "active" | "pending" | "soft_fail"
}

// ─── Meter Policy ────────────────────────────────────
message MeterPolicy {
  string policy_id = 1;
  uint64 rate_bps = 2;
  uint64 burst_bytes = 3;
  string overflow_action = 4;           // "drop" | "remark" | "log"
}

// ─── HA Config ───────────────────────────────────────
message HaConfig {
  string ha_scope_id = 1;
  string role = 2;                      // "primary" | "standby" | "standalone"
  string peer_eni_id = 3;               // Empty for standalone
  uint32 failover_priority = 4;
}
```

---

## 3. COMPOSITION ALGORITHM

### 3.1 Input Sources (via Registry Acquire)

Each NicActor composes from these inputs, acquired once at construction:

| Input | Source Registry | Acquire Key | Required? |
|-------|----------------|-------------|-----------|
| NicSpec | (direct from T1 watch) | `eni_id` | Yes |
| VnetData | VnetRegistry | `vnet_id` | Yes |
| MappingData | VnetMappingRegistry | `vnet_id` | Yes |
| RouteGroup | GroupRegistry | `route_group_id` | Yes |
| AclGroups[6] | GroupRegistry | `acl_group_id` × 6 | Yes (6 slots, some may resolve to empty) |
| MeterPolicy | GlobalRegistry | `meter_policy_id` | Yes |
| HaScope | HaRegistry | `ha_scope_id` | Yes |
| RoutingTypes | GlobalRegistry | (all) | Yes |
| PrefixTags | GroupRegistry | (referenced) | Conditional |

### 3.2 Algorithm Pseudocode

```
function ComposeNicGoalState(eni_id) -> NicGoalState:

  # === Step 1: Acquire all inputs (blocking on Ready) ===
  nic_spec       = ReadFromT1(eni_id)
  vnet_data      = VnetRegistry.Acquire(nic_spec.vnet_id).WaitReady()
  mapping_data   = VnetMappingRegistry.Acquire(nic_spec.vnet_id).WaitReady()
  route_group    = GroupRegistry.Acquire(nic_spec.route_group_id).WaitReady()
  acl_groups     = []
  for slot_id in nic_spec.acl_group_ids:  # exactly 6 entries
    if slot_id == "":
      acl_groups.append(EmptyAclGroup())
    else:
      acl_groups.append(GroupRegistry.Acquire(slot_id).WaitReady())
  meter_policy   = GlobalRegistry.Acquire("meter_policy", nic_spec.meter_policy_id).WaitReady()
  ha_scope       = HaRegistry.Acquire(nic_spec.ha_scope_id).WaitReady()
  routing_types  = GlobalRegistry.AcquireAll("routing_type")

  # === Step 2: Build Identity ===
  identity = NicIdentity{
    mac:          nic_spec.mac,
    primary_ip:   nic_spec.primary_ip,
    vlan_id:      nic_spec.vlan_id,
    tenant_id:    vnet_data.tenant_id,
    ha_scope_id:  ha_scope.id,
  }

  # === Step 3: Compose Routes (with prefix_tag expansion) ===
  routes = []
  for route_entry in route_group.routes:
    # Expand prefix_tag references
    if route_entry.prefix_tag_id != "":
      prefix_tag = GroupRegistry.Acquire(route_entry.prefix_tag_id).WaitReady()
      for prefix in prefix_tag.prefixes:
        action = ResolveRoutingTemplate(route_entry.routing_type_id, routing_types, mapping_data)
        routes.append(RouteEntry{
          prefix:           CanonicalCIDR(prefix),
          vrf_id:           route_entry.vrf_id,
          action:           action,
          priority:         route_entry.priority,
          source_group_id:  route_group.id,
        })
    else:
      action = ResolveRoutingTemplate(route_entry.routing_type_id, routing_types, mapping_data)
      routes.append(RouteEntry{
        prefix:           CanonicalCIDR(route_entry.prefix),
        vrf_id:           route_entry.vrf_id,
        action:           action,
        priority:         route_entry.priority,
        source_group_id:  route_group.id,
      })

  # Apply mapping-derived routes (VNET peering, BGP)
  for peer_mapping in mapping_data.peer_mappings:
    routes.append(BuildPeerRoute(peer_mapping, routing_types))

  # Deterministic sort: (prefix string ascending, vrf_id ascending)
  routes = sort(routes, key=(r.prefix, r.vrf_id))

  # === Step 4: Compose ACL Stages (6 fixed slots) ===
  acl_stages = []
  slot_layout = [
    (slot=0, family="ipv4", direction="inbound",  stage=1),
    (slot=1, family="ipv4", direction="outbound", stage=1),
    (slot=2, family="ipv6", direction="inbound",  stage=1),
    (slot=3, family="ipv6", direction="outbound", stage=1),
    (slot=4, family="ipv4", direction="inbound",  stage=2),
    (slot=5, family="ipv4", direction="outbound", stage=2),
  ]
  for i, slot in enumerate(slot_layout):
    acl_group = acl_groups[i]
    rules = []
    for rule in acl_group.rules:
      rules.append(AclRule{
        priority:      rule.priority,
        src_prefix:    CanonicalCIDR(rule.src_prefix),
        dst_prefix:    CanonicalCIDR(rule.dst_prefix),
        src_port_min:  rule.src_port_min,
        src_port_max:  rule.src_port_max,
        dst_port_min:  rule.dst_port_min,
        dst_port_max:  rule.dst_port_max,
        protocol:      rule.protocol.lowercase(),
        action:        rule.action,
      })
    # Sort by priority ascending (deterministic)
    rules = sort(rules, key=r.priority)
    acl_stages.append(AclStage{
      slot_index:    i,
      family:        slot.family,
      direction:     slot.direction,
      stage:         slot.stage,
      acl_group_id:  acl_group.id,
      rules:         rules,
    })

  # === Step 5: Compose VIP Mappings ===
  vip_mappings = []
  for vip_binding in mapping_data.vip_bindings:
    if vip_binding.target_eni_id != eni_id:
      continue  # skip VIPs not bound to this ENI
    vip_mappings.append(VipMapping{
      vip:            CanonicalIP(vip_binding.vip),
      dip:            CanonicalIP(vip_binding.dip),
      snat_enabled:   vip_binding.snat,
      nat_pool_id:    vip_binding.nat_pool_id,
      binding_state:  vip_binding.state,
    })
  # Sort by vip string ascending
  vip_mappings = sort(vip_mappings, key=v.vip)

  # === Step 6: Build composite ===
  goal_state = NicGoalState{
    eni_id:               eni_id,
    device_id:            nic_spec.device_id,
    vnet_id:              nic_spec.vnet_id,
    identity:             identity,
    routes:               routes,
    acl_stages:           acl_stages,
    vip_mappings:         vip_mappings,
    meter_policy:         BuildMeterPolicy(meter_policy),
    ha_config:            BuildHaConfig(ha_scope, nic_spec),
    composition_revision: AtomicIncrement(nic_composition_counter),
    composed_at:          now(),
    source_keys:          AllAcquiredKeys(),  # for audit
  }

  # === Step 7: Compute content_hash (AFTER all fields populated EXCEPT hash itself) ===
  goal_state.content_hash = ""  # zero out before hash
  binary = SerializeCanonicalProto(goal_state)
  goal_state.content_hash = SHA256(binary).hex().lowercase()

  return goal_state
```

### 3.3 Helper Functions

```
function CanonicalCIDR(cidr_string) -> string:
  # "10.00.0.0/24" → "10.0.0.0/24"
  # "::1/128"     → "::1/128"
  # "2001:0db8::/32" → "2001:db8::/32"
  ip, prefix = parse(cidr_string)
  return format(NormalizeIP(ip)) + "/" + str(prefix)

function CanonicalIP(ip_string) -> string:
  # IPv4: leading zeros removed
  # IPv6: lowercase hex, :: compression applied per RFC 5952
  return NormalizeIP(parse(ip_string))

function ResolveRoutingTemplate(routing_type_id, routing_types, mapping_data) -> RoutingAction:
  template = routing_types[routing_type_id]
  if template.action_kind == "encap":
    vni = mapping_data.get_vni(template.encap_type)
    return EncapAction{
      encap_type:   template.encap_type,
      vni:          vni,
      underlay_dst: template.underlay_dst,
      underlay_src: mapping_data.local_underlay_src,
    }
  elif template.action_kind == "decap":
    return DecapAction{encap_type: template.encap_type, vni: template.vni}
  elif template.action_kind == "drop":
    return DropAction{reason: template.drop_reason}
  elif template.action_kind == "forward":
    return ForwardAction{next_hop: template.next_hop, next_vrf_id: template.next_vrf_id}
```

---

## 4. CONTENT-HASH ALGORITHM

### 4.1 Canonical Serialization Rules

**MANDATORY:** All NicActor implementations MUST follow these rules to guarantee bit-identical output for identical inputs.

1. **Proto3 binary wire format** (not text/JSON)
2. **Field tag order**: ascending by field number (enforced by proto3 compilers)
3. **Repeated field order**: sorted by the canonical key documented per-field:
   - `routes[]` → sorted by `(prefix, vrf_id)` ascending
   - `acl_stages[]` → sorted by `slot_index` ascending
   - `acl_stages[].rules[]` → sorted by `priority` ascending
   - `vip_mappings[]` → sorted by `vip` ascending
   - `source_keys[]` → sorted alphabetically
4. **Map order**: NONE — use repeated message fields with sorted keys instead
5. **String normalization**: All IP/CIDR fields pass through `CanonicalIP` / `CanonicalCIDR`
6. **Enum encoding**: Use enum integer values (proto3 default)
7. **`content_hash` field**: Zero out before computing hash, then populate

### 4.2 Hash Computation

```
function ComputeContentHash(goal_state) -> string:
  goal_state_copy = deep_copy(goal_state)
  goal_state_copy.content_hash = ""
  goal_state_copy.composed_at  = TIMESTAMP_ZERO  # exclude time from hash
  binary = SerializeCanonicalProto(goal_state_copy)
  digest = SHA256(binary)
  return digest.hex().lowercase()  # 64 chars [0-9a-f]
```

**Fields excluded from hash:**
- `content_hash` (obvious — recursive)
- `composed_at` (time-varying, not part of state)
- `composition_revision` (counter, not state)
- `source_keys` (audit metadata, not state)

**Fields INCLUDED in hash:** all programmable state (identity, routes, acl_stages, vip_mappings, meter_policy, ha_config, eni_id, device_id, vnet_id).

### 4.3 Validation Test

```
input_A  = NicSpec{...} + VnetData{...} + Groups{...}
input_B  = NicSpec{...} + VnetData{...} + Groups{...}  # identical
state_A  = ComposeNicGoalState(input_A)
state_B  = ComposeNicGoalState(input_B)
ASSERT: state_A.content_hash == state_B.content_hash
ASSERT: SerializeCanonicalProto(state_A) == SerializeCanonicalProto(state_B)
```

This MUST be a CI gate on every NicActor implementation.

---

## 5. PREFIX-TAG EXPANSION SEMANTICS

### 5.1 Source Format (in T1)

```
/config/v1/group/{group_id}/prefix_tag/{tag_id} = {
  tag_id:    "datacenter_subnets",
  family:    "ipv4",
  prefixes:  ["10.0.0.0/16", "10.1.0.0/16", "172.16.0.0/12"],
  version:   42,
}
```

### 5.2 Reference in Routes

```
RouteGroup.routes[i] = {
  prefix_tag_id:    "datacenter_subnets",  # OR prefix: "..."
  routing_type_id:  "encap_vxlan_to_underlay",
  vrf_id:           "vrf_tenant_1",
  priority:         100,
}
```

### 5.3 Expansion Rule

- If `prefix_tag_id` is set: expand to N routes (one per prefix in tag), each inheriting `routing_type_id`, `vrf_id`, `priority`
- If `prefix` is set: single route
- ERROR if both or neither set

### 5.4 Caching

PrefixTag objects are acquired via `GroupRegistry.Acquire(tag_id)`. NicActor maintains refcount → when ENI deleted, all tag references released → after debounce, T1 watch closed.

---

## 6. ERROR HANDLING DURING COMPOSITION

| Condition | Action | NIC State |
|-----------|--------|-----------|
| Required input missing (e.g., VNET deleted) | Park NicActor in `WAITING_REFS`; retry on next change | WAITING_REFS |
| ACL group not found | Treat as empty slot; log warning; emit metric | READY (degraded) |
| Prefix tag malformed | Skip route entry; emit error metric; log; continue | READY (degraded) |
| Routing type undefined | Fail composition; park in `INCOMPLETE_MAPPING` | INCOMPLETE_MAPPING |
| Mapping data incomplete (assembler not done) | Park in `WAITING_MAPPING`; retry on assembly complete | WAITING_MAPPING |
| HA scope unresolved | Fail composition; park in `WAITING_REFS` | WAITING_REFS |
| CIDR parse error | Reject NicSpec → quarantine in `VALIDATION_REJECTED` | VALIDATION_REJECTED |

---

## 7. PROGRAMMING ORDER (Wave Assignment)

When DeltaPlan is generated from NicGoalState, each command receives a `wave_offset`:

| Wave | Object Type | Reason |
|------|------------|--------|
| 0 | VRF, VLAN | Foundational namespaces must exist first |
| 1 | Identity (MAC, IP, VLAN binding) | ENI-level identity before per-flow |
| 2 | MeterPolicy | Required before flow programming |
| 3 | Routes | Forwarding state |
| 4 | AclStages (Stage 1) | Pre-routing policy |
| 5 | AclStages (Stage 2) | Post-routing policy |
| 6 | VipMappings, HA links | Last (depends on routes + ACLs) |

This wave_offset is assigned by NicActor when emitting DeltaCommands; SouthboundDriver executes waves in strict ascending order. See `southbound-driver-interface-redesign.md`.

---

## 8. ACCEPTANCE CRITERIA FOR IMPLEMENTATION

1. ✅ Two compose runs with identical inputs produce byte-identical proto3 binary
2. ✅ `content_hash` deterministic and reproducible across processes
3. ✅ Composition completes within 50ms for typical inputs (P50)
4. ✅ Composition completes within 200ms for large inputs (P99, 500 routes + 6 ACL stages)
5. ✅ Memory usage per composed NicGoalState ≤ 100KB typical, ≤ 1MB max
6. ✅ All sort orders documented and enforced by code (not assumed)
7. ✅ Round-trip test: serialize → hash → deserialize → re-serialize → re-hash → MUST match

---

## 9. REFERENCES

- `vm-eni-provisioning-design.md` — End-to-end provisioning flow
- `registry-pattern-design.md` — Acquire/Release semantics for input sources
- `southbound-driver-interface-redesign.md` — DeltaPlan wave execution
- `fleet-manager-lld.md` §1.1 — ObjectState enum (WAITING_REFS, COMPOSING, READY, etc.)
- DASH proto definitions — vendor object schemas (where applicable)
