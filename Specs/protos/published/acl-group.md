# `AclGroup` + `AclRuleList` — Reusable ACL stage

> **TL;DR:** One stage of a security rule list (e.g. VNIC, Subnet, or
> VNET-level). NICs reference up to **3 stages × 2 directions × 2
> families = 12 groups** to build their full ACL pipeline. Splitting
> header/body keeps the hot edit path cheap.

**Topics:**
- `/config/v1/group/<device_id>/acl_group/<group_id>` → `AclGroup`
- `/config/v1/group/<device_id>/acl_group/<group_id>/rules` → `AclRuleList`

**Kinds:** `CONFIG_KIND_ACL_GROUP`, `CONFIG_KIND_ACL_RULE_LIST`
**Scope:** appliance-global (per device)
**Lifecycle owner:** orchestrator
**Subscriber:** HDO actor (process-wide cache)

## Example

```json
// /config/v1/group/dpu-007/acl_group/acl-acme-subnet-prod
{
  "group_id": "acl-acme-subnet-prod",
  "family": "IPV4",
  "stage_hint": "SUBNET",
  "direction_hint": "OUTBOUND",
  "rule_count": 6,
  "attributes": { "tenant": "acme", "subnet": "prod" }
}
```

```json
// /config/v1/group/dpu-007/acl_group/acl-acme-subnet-prod/rules
{
  "group_id": "acl-acme-subnet-prod",
  "revision": 88,
  "rules": [
    { "priority": 100, "action": "ALLOW",
      "src_prefix_tag_refs": [], "dst_prefix_tag_refs": ["tag-azure-storage"],
      "protocol": 6, "dst_port_lo": 443, "dst_port_hi": 443 },
    { "priority": 200, "action": "DENY",
      "src_prefix_tag_refs": [], "dst_prefix_tag_refs": ["tag-internet"],
      "protocol": 0, "dst_port_lo": 0, "dst_port_hi": 0 },
    { "priority": 999, "action": "ALLOW",
      "src_prefix_tag_refs": [], "dst_prefix_tag_refs": [],
      "protocol": 0, "dst_port_lo": 0, "dst_port_hi": 0 }
  ]
}
```

The header announces the group's existence and intended slot; the body
carries the ordered rules. ACL stage assignment is decided by the
**NicSpec** (which slot in the 3-element array it goes into) — the
`stage_hint` here is descriptive only.

## Purpose

Per decision **#21**: ACL binding lives on `NicSpec` as a length-3
array per direction per family. The group itself is stage-agnostic —
the same group could be referenced from a different stage on a
different NIC. This keeps groups reusable.

The header/body split mirrors RouteGroup's rationale: hot-path edits
land on the rule list; existence/metadata queries hit the header.

## `AclGroup` fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `group_id` | `string` | yes | Matches path segment. |
| `family` | `AddressFamily` enum | yes | `IPV4` or `IPV6`. |
| `stage_hint` | `AclStageHint` enum | no | Documentation hint: `VNIC`, `SUBNET`, `VNET`. NicSpec is authoritative. |
| `direction_hint` | `AclDirectionHint` enum | no | `INBOUND`, `OUTBOUND`, `EITHER`. Documentation only. |
| `rule_count` | `uint32` | no | Hint for sibling list size. |
| `attributes` | `map<string,string>` | no | Free-form. |

## `AclRuleList` fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `group_id` | `string` | yes | Mirrors parent. |
| `revision` | `uint64` | yes | Mirrors `metadata.revision`. |
| `rules` | `repeated AclRule` | yes | Ordered evaluation, lower `priority` first. |

### `AclRule`

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `priority` | `uint32` | yes | Lower = higher priority. Ties broken by list order. |
| `action` | `AclAction` enum | yes | `ALLOW`, `DENY`, `ALLOW_AND_CONTINUE`, `DENY_AND_CONTINUE`. |
| `src_prefixes` | `repeated IpPrefix` | no | Inline source prefixes. |
| `dst_prefixes` | `repeated IpPrefix` | no | Inline destination prefixes. |
| `src_prefix_tag_refs` | `repeated string` | no | Foreign keys to PrefixTag. |
| `dst_prefix_tag_refs` | `repeated string` | no | Foreign keys to PrefixTag. |
| `protocol` | `uint32` | no | IANA proto number. 0 = any. |
| `src_port_lo` | `uint32` | no | Inclusive low port. |
| `src_port_hi` | `uint32` | no | Inclusive high port. |
| `dst_port_lo` | `uint32` | no | Inclusive low port. |
| `dst_port_hi` | `uint32` | no | Inclusive high port. |
| `counter_id` | `string` | no | Optional named counter for telemetry. |

### Validation rules

1. `AclRuleList.group_id` matches path segment.
2. `rules.count ≤ Appliance.capabilities.max_acl_rules_per_eni` after composition (HDO checks per-ENI total across all stages).
3. Empty `src_*`/`dst_*` means "any" (5-tuple wildcard).
4. `*_port_lo ≤ *_port_hi` when both present.
5. `*_prefix_tag_refs[*]` must resolve to PrefixTag of matching family.

## Proto3 sketch

```proto
syntax = "proto3";
package fleetmanager.v1;

import "common.proto";   // IpPrefix, AddressFamily

enum AclStageHint {
  ACL_STAGE_HINT_UNSPECIFIED = 0;
  ACL_STAGE_HINT_VNIC        = 1;
  ACL_STAGE_HINT_SUBNET      = 2;
  ACL_STAGE_HINT_VNET        = 3;
}

enum AclDirectionHint {
  ACL_DIRECTION_HINT_UNSPECIFIED = 0;
  ACL_DIRECTION_HINT_INBOUND     = 1;
  ACL_DIRECTION_HINT_OUTBOUND    = 2;
  ACL_DIRECTION_HINT_EITHER      = 3;
}

enum AclAction {
  ACL_ACTION_UNSPECIFIED        = 0;
  ACL_ACTION_ALLOW              = 1;
  ACL_ACTION_DENY               = 2;
  ACL_ACTION_ALLOW_AND_CONTINUE = 3;
  ACL_ACTION_DENY_AND_CONTINUE  = 4;
}

message AclGroup {
  string             group_id        = 1;
  AddressFamily      family          = 2;
  AclStageHint       stage_hint      = 3;
  AclDirectionHint   direction_hint  = 4;
  uint32             rule_count      = 5;
  map<string,string> attributes      = 20;
}

message AclRule {
  uint32             priority             = 1;
  AclAction          action               = 2;
  repeated IpPrefix  src_prefixes         = 3;
  repeated IpPrefix  dst_prefixes         = 4;
  repeated string    src_prefix_tag_refs  = 5;
  repeated string    dst_prefix_tag_refs  = 6;
  uint32             protocol             = 7;
  uint32             src_port_lo          = 8;
  uint32             src_port_hi          = 9;
  uint32             dst_port_lo          = 10;
  uint32             dst_port_hi          = 11;
  string             counter_id           = 12;
}

message AclRuleList {
  string   group_id     = 1;
  uint64   revision     = 2;
  repeated AclRule rules = 3;
}
```

## Relationships

- Referenced by: `NicSpec.outbound.acl_v4` / `acl_v6` and
  `NicSpec.inbound.acl_v4` / `acl_v6` (each is a length-3 array of group ids).
- References: `PrefixTag` (via rule `*_prefix_tag_refs`).
- Sibling: `AclRuleList` is always paired with `AclGroup`.

## Change semantics

- **AclRuleList edit**: cascades to every NicGoalState referencing this group.
- **AclGroup metadata edit**: no programming impact.
- **Group deletion**: rejected if any NIC still references it.

## See also

- [nic-spec](./nic-spec.md) — primary consumer via 3-stage arrays.
- [prefix-tag](./prefix-tag.md) — resolved targets of `*_prefix_tag_refs`.
- [route-group](./route-group.md) — twin pattern (header/body split).
- [README](./README.md) — full kind index.