# `NicGoalState` — Composed ENI program (derived, not published)

> **TL;DR:** The fully-resolved, denormalized "what to ship to the DPU"
> object for one ENI. Built **inside** the NO actor by joining the
> `NicSpec` references against the global, group, and VNET caches. Never
> appears in etcd — it is the input to the HAL, and its content hash
> drives change detection.

**Topic:** N/A (not published)
**Kind:** N/A (not a `ConfigEntry`; in-process only)
**Scope:** per-NIC (per-ENI), in-memory in the NO actor
**Lifecycle owner:** NO actor (composer)
**Subscriber:** HAL (the only consumer)

## Example (illustrative — never serialized to etcd)

```json
{
  "eni_id": "ENI_dpu-007_aabbccddeeff",
  "compose_revision": 89421,
  "composed_at": "2026-06-11T14:23:46.012Z",
  "content_hash": "9f4c2a...3b1d",

  "source": {
    "nic_spec_revision": 42,
    "vnet_revision": 18742,
    "tunnel_revision": 5,
    "route_group_v4_revision": 142,
    "acl_group_revisions_v4": [11, 88, 0],
    "acl_group_revisions_v4_in": [9, 80, 17],
    "meter_policy_revision_out": 23,
    "meter_policy_revision_in": 19,
    "qos_revision": 7,
    "vnet_mapping_manifest_revision": 18742
  },

  "eni": {
    "eni_id": "ENI_dpu-007_aabbccddeeff",
    "mac_address": "aa:bb:cc:dd:ee:ff",
    "vnet_id": "vnet-tenant-acme-prod",
    "vni": 78215,
    "primary_ip_v4": "10.42.0.5",
    "underlay_ip_v4": "100.64.7.5",
    "qos": { "bw_gbps": 25, "queue_count": 8 }
  },

  "tunnel": {
    "tunnel_id": "tun-vxlan-default-westus2",
    "encap_type": "VXLAN",
    "src_underlay_ip_v4": "100.64.7.5",
    "udp_dst_port": 4789
  },

  "outbound": {
    "routes": [
      { "priority": 100, "dst_prefix": "10.42.0.0/16", "action": { "kind": "VNET" } },
      { "priority": 300, "dst_prefix": "0.0.0.0/0",
        "action": { "kind": "DEFAULT_TUNNEL", "tunnel_id": "tun-internet-westus2" } }
    ],
    "acl_stages": [
      { "stage": "VNIC",   "rules": [ /* expanded rules with prefix-tags resolved */ ] },
      { "stage": "SUBNET", "rules": [ /* ... */ ] },
      { "stage": "VNET",   "rules": [] }
    ],
    "meter": {
      "default_action": "PASS",
      "rules": [ /* meter rules with tags expanded */ ]
    },
    "port_map_ranges": []
  },

  "inbound": {
    "acl_stages": [ /* same shape as outbound */ ],
    "meter": { /* ... */ }
  },

  "route_rules": [
    { "match": { "dst_prefix": "169.254.169.254/32" }, "action": { "kind": "REDIRECT_LOCAL_METADATA" } }
  ],

  "mapping_entries_ref": {
    "vnet_id": "vnet-tenant-acme-prod",
    "manifest_revision": 18742,
    "entry_count": 51877
  },

  "ha": {
    "ha_set_id": "haset-westus2-pair-7",
    "role": "PRIMARY",
    "peer_underlay_ip_v4": "100.64.8.5"
  }
}
```

The actual in-memory form is a `NicGoalState` struct; the JSON above is
purely for human inspection (and for `dfctl explain` output).

## Purpose

Per decisions **#1, #14, #27**: NicSpec is a *reference bundle* — it
contains only ids. The DPU needs a *self-contained program* — every
route, ACL rule, meter, mapping, tunnel parameter spelled out. The NO
actor bridges the two:

1. Resolves every `*_id` reference against the HDO's caches.
2. Expands `prefix_tag_refs` into concrete prefix lists in each rule.
3. Stamps revisions from each source object into `source.*_revision`.
4. Hashes the canonical serialization → `content_hash`.
5. Diffs against the previous `NicGoalState` to compute per-field deltas.
6. Hands the diff to the HAL.

Because the inputs are revisioned, the same set of input revisions
always produces the same `content_hash`. That makes change detection
cheap: if the new `content_hash` equals the last applied one, no RPC
is needed.

## Fields (in-memory shape, not wire)

### Identity & provenance

| Field | Type | Description |
|-------|------|-------------|
| `eni_id` | `string` | Derived per decision **#13**: `ENI_<DPU>_<MAC>`. |
| `compose_revision` | `uint64` | NO actor's local compose counter (monotonic per NIC). |
| `composed_at` | `Timestamp` | Wall clock at compose time. |
| `content_hash` | `bytes(32)` | SHA-256 of canonical serialization. Used for idempotency vs. last-applied. |
| `source.*_revision` | `uint64` each | Revisions of every input object that fed this compose. Used for drift detection during Reconcile and for the audit trail. |

### Body sections

| Section | Type | Description |
|---------|------|-------------|
| `eni` | object | Identity, addresses, QoS (expanded). |
| `tunnel` | object | Full Tunnel record (denormalized). |
| `outbound` | object | Resolved routes + 3 ACL stages + meter + port map ranges. |
| `inbound` | object | 3 ACL stages + meter. |
| `route_rules` | repeated | Inline per-ENI override rules (carried through from NicSpec). |
| `mapping_entries_ref` | ref | Pointer to the HDO's in-memory VNET mapping (not copied — shared reference for size). |
| `ha` | object | HA pair info (when present). |

> Note: `mapping_entries_ref` is a **pointer**, not a copy. The mapping
> table can be hundreds of MB; the NO holds a refcount on the HDO's
> assembled VNet mapping, and the HAL reads through that pointer when
> programming. Decision **#27**: change detection still works because
> the manifest revision is in `source.vnet_mapping_manifest_revision`.

### Compose-time errors (no rejection, signal upward)

| Field | Type | Description |
|-------|------|-------------|
| `status` | `ComposeStatus` enum | `COMPOSED_OK`, `WAITING_REFS`, `INCOMPLETE_MAPPING`, `OVER_CAPACITY`. |
| `wait_reasons` | `repeated string` | When not OK: list of missing ref paths or completeness gaps. Mirrors what gets written to `/status/v1/.../_error`. |

## Composition algorithm (NO actor)

```
On input change (NicSpec, or any reference's revision):
  inputs = snapshot from HDO cache:
    vnet, tunnel, route_group_v4/v6, acl_groups[6], meter_policies[2],
    qos, ha_set (via ha_scope), pa_validation, vnet_mapping_manifest,
    prefix_tags (transitively from rules)

  if any required input missing:
    status = WAITING_REFS
    emit ValidationError(WAITING)
    return

  if vnet_mapping is incomplete (some chunk not yet validated):
    status = INCOMPLETE_MAPPING
    emit ValidationError(WAITING, code=MAPPING_INCOMPLETE)
    return

  expand prefix_tag_refs in every rule
  resolve all *_id references into denormalized bodies
  build NicGoalState struct
  compute content_hash = SHA256(canonical_proto(struct))

  if content_hash == last_applied_hash:
    no-op; metric noop_composes_total++
    return

  diff = full_proto_diff(last_composed, new)
  enqueue HAL.Apply(diff, fence_token=lease_resourceVersion)
  on success: last_applied_hash = content_hash
```

## Proto3 sketch (in-process representation)

`NicGoalState` is **not** wire-published, but the same proto3 types
defined for the published kinds are reused as sub-messages. The
composed struct lives in the NO actor's heap; for telemetry/diagnostics
it can be serialized to proto-binary and dumped to a debug endpoint.

```proto
syntax = "proto3";
package fleetmanager.v1;

import "google/protobuf/timestamp.proto";

enum ComposeStatus {
  COMPOSE_STATUS_UNSPECIFIED       = 0;
  COMPOSE_STATUS_COMPOSED_OK       = 1;
  COMPOSE_STATUS_WAITING_REFS      = 2;
  COMPOSE_STATUS_INCOMPLETE_MAPPING = 3;
  COMPOSE_STATUS_OVER_CAPACITY     = 4;
}

message ComposeSource {
  uint64 nic_spec_revision               = 1;
  uint64 vnet_revision                   = 2;
  uint64 tunnel_revision                 = 3;
  uint64 route_group_v4_revision         = 4;
  uint64 route_group_v6_revision         = 5;
  repeated uint64 acl_group_revisions_v4 = 6;   // length 3
  repeated uint64 acl_group_revisions_v6 = 7;
  repeated uint64 acl_group_revisions_v4_in = 8;
  repeated uint64 acl_group_revisions_v6_in = 9;
  uint64 meter_policy_revision_out       = 10;
  uint64 meter_policy_revision_in        = 11;
  uint64 qos_revision                    = 12;
  uint64 ha_set_revision                 = 13;
  uint64 vnet_mapping_manifest_revision  = 14;
  uint64 pa_validation_revision          = 15;
}

message NicGoalState {
  // Provenance
  string                    eni_id           = 1;
  uint64                    compose_revision = 2;
  google.protobuf.Timestamp composed_at      = 3;
  bytes                     content_hash     = 4;
  ComposeSource             source           = 5;

  // Status
  ComposeStatus    status        = 6;
  repeated string  wait_reasons  = 7;

  // Body (denormalized, sub-messages reused from published kinds)
  EniIdentity     eni        = 10;
  Tunnel          tunnel     = 11;
  GoalOutbound    outbound   = 12;
  GoalInbound     inbound    = 13;
  repeated RouteRule route_rules = 14;
  MappingRef      mapping_entries_ref = 15;
  HaBinding       ha         = 16;
}
```

(`EniIdentity`, `GoalOutbound`, `GoalInbound`, `MappingRef`, `HaBinding`
are sub-messages local to the in-process composition; they reuse the
published-kind sub-messages where shapes align.)

## Relationships

- Composed from: `NicSpec` (1) + every cache entry referenced by it.
- Consumed by: HAL only.
- Mirrored in: drift-detection compare against the device's reported
  state during Reconcile.

## Change semantics

- A new `content_hash` triggers an HAL `Apply`; an unchanged hash is a
  **no-op** even if `compose_revision` advanced (e.g., spurious
  re-compose).
- `WAITING_REFS` and `INCOMPLETE_MAPPING` are normal transient states;
  the NO actor re-composes when the HDO signals a relevant cache fill.
- The previous `NicGoalState` is retained in the NO until the new one
  is successfully applied — this enables rollback if HAL reports
  programming failure.
- `OVER_CAPACITY` (rule count > Appliance limit) is a hard rejection;
  emits `ValidationError(REJECTED)`.

## Why this is the right abstraction

- **Single point of denormalization** — keeps every other object small,
  cheap to edit, and reusable.
- **Content-hash idempotency** — replaying the same intent costs zero
  RPCs.
- **Source-revision tracking** — drift detection during Reconcile is
  exact, not approximate.
- **Pointer to mapping** — keeps memory bounded even for million-entry
  VNETs.

## Upstream DASH alignment

`NicGoalState` is **not** an upstream DASH table — it is FM's in-process
denormalized program for one ENI. The HAL's job is to fan it out to the
right `DASH_*_TABLE` rows on the DPU:

| `NicGoalState` section | Upstream tables written |
|------------------------|--------------------------|
| `eni` (id, MAC, VNI, underlay IP, QoS) | `DASH_ENI_TABLE` (one row, MAC-keyed) |
| `eni.qos` reference | `DASH_QOS_TABLE` (referenced, not rewritten) |
| `tunnel` | `DASH_TUNNEL_TABLE` (referenced) |
| `outbound.routes` (resolved RouteGroup) | `DASH_ENI_ROUTE_TABLE` (binding) + `DASH_ROUTE_TABLE` (rules under group) |
| `outbound.acl_stages[3]` | `DASH_ACL_OUT_TABLE` (binding per stage) + `DASH_ACL_RULE_TABLE` (rules under group) |
| `outbound.meter` | `DASH_METER_POLICY` / `DASH_METER_RULE` (referenced) |
| `outbound.port_map_ranges` | `DASH_OUTBOUND_PORT_MAP_TABLE` + `DASH_OUTBOUND_PORT_MAP_RANGE_TABLE` |
| `inbound.acl_stages[3]` | `DASH_ACL_IN_TABLE` (binding per stage) |
| `inbound.meter` | `DASH_METER_POLICY` (inbound) |
| `route_rules` (per-ENI overrides) | `DASH_ROUTE_RULE_TABLE` |
| `mapping_entries_ref` | `DASH_VNET_MAPPING_TABLE` (per-row writes against the VNET) |

The composed struct is therefore the *plan* for a Wave-5/Wave-6 burst
of southbound writes. `content_hash` idempotency means an unchanged
`NicGoalState` produces zero SAI calls even though many tables would
otherwise be touched. Read-only counters like `DASH_METER` (per-ENI
billing) are **not** written from `NicGoalState` — they surface on the
read path (see `fleet-manager-rest-api.md` §2.7).

## See also

- [nic-spec](./nic-spec.md) — the primary input.
- [vnet](./vnet.md), [vnet-mapping](./vnet-mapping.md) — VNET-side inputs.
- [route-group](./route-group.md), [acl-group](./acl-group.md), [meter-policy](./meter-policy.md), [outbound-port-map](./outbound-port-map.md) — shared groups.
- [prefix-tag](./prefix-tag.md), [tunnel](./tunnel.md), [qos](./qos.md), [ha-set](./ha-set.md) — global inputs.
- [error](./error.md) — where compose failures surface.
- [README](./README.md) — full kind index.