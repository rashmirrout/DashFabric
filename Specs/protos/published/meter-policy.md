# `MeterPolicy` + `MeterRuleList` — Rate-limit / billing policy

> **TL;DR:** A reusable named meter (token bucket + match rules) for
> rate-limiting or accounting traffic on an ENI. NICs reference it
> per-direction (`outbound.meter_policy_id`, `inbound.meter_policy_id`).

**Topics:**
- `/config/v1/group/<device_id>/meter_policy/<policy_id>` → `MeterPolicy`
- `/config/v1/group/<device_id>/meter_policy/<policy_id>/rules` → `MeterRuleList`

**Kinds:** `CONFIG_KIND_METER_POLICY`, `CONFIG_KIND_METER_RULE_LIST`
**Scope:** appliance-global
**Lifecycle owner:** orchestrator
**Subscriber:** HDO actor (process-wide cache)

## Example

```json
// /config/v1/group/dpu-007/meter_policy/mp-acme-tier-gold-egress
{
  "policy_id": "mp-acme-tier-gold-egress",
  "direction_hint": "OUTBOUND",
  "default_action": "PASS",
  "rule_count": 2,
  "attributes": { "tier": "gold", "purpose": "egress-bw-limit" }
}
```

```json
// /config/v1/group/dpu-007/meter_policy/mp-acme-tier-gold-egress/rules
{
  "policy_id": "mp-acme-tier-gold-egress",
  "revision": 23,
  "rules": [
    { "priority": 100,
      "match": { "dst_prefix_tag_refs": ["tag-internet"] },
      "meter": { "cir_bps": 10000000000, "cbs_bytes": 1310720, "metering_class": 7 },
      "action": "PASS" },
    { "priority": 200,
      "match": { "dst_prefix_tag_refs": ["tag-azure-storage"] },
      "meter": { "cir_bps": 25000000000, "cbs_bytes": 3276800, "metering_class": 9 },
      "action": "PASS" }
  ]
}
```

## Purpose

Per decision **#24**: separate `MeterPolicy` (header) and `MeterRuleList`
(body), with `NicSpec` binding **per direction** (`outbound.meter_policy_id`
and `inbound.meter_policy_id` independently). Two directions because
billing/QoS often diverge: egress is metered for chargeback while ingress
is metered for DDoS protection.

The same policy can be shared across many ENIs in the same tier.

## `MeterPolicy` fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `policy_id` | `string` | yes | Matches path segment. |
| `direction_hint` | `MeterDirectionHint` enum | no | `INBOUND`, `OUTBOUND`, `EITHER`. Documentation only. |
| `default_action` | `MeterAction` enum | yes | Action when no rule matches: `PASS`, `DROP`. |
| `rule_count` | `uint32` | no | Hint for sibling list size. |
| `attributes` | `map<string,string>` | no | Free-form. |

## `MeterRuleList` fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `policy_id` | `string` | yes | Mirrors parent. |
| `revision` | `uint64` | yes | Mirrors `metadata.revision`. |
| `rules` | `repeated MeterRule` | yes | Ordered evaluation. |

### `MeterRule`

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `priority` | `uint32` | yes | Lower = higher priority. |
| `match` | `MeterMatch` | yes | 5-tuple-ish match. |
| `meter` | `MeterBucket` | yes | Token bucket params. |
| `action` | `MeterAction` enum | yes | `PASS`, `DROP`. |

### `MeterMatch`

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `src_prefixes` | `repeated IpPrefix` | no | Inline source CIDRs. |
| `dst_prefixes` | `repeated IpPrefix` | no | Inline destination CIDRs. |
| `src_prefix_tag_refs` | `repeated string` | no | PrefixTag refs. |
| `dst_prefix_tag_refs` | `repeated string` | no | PrefixTag refs. |
| `protocol` | `uint32` | no | IANA proto number. |
| `dst_port_lo` | `uint32` | no | Inclusive low. |
| `dst_port_hi` | `uint32` | no | Inclusive high. |

### `MeterBucket`

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `cir_bps` | `uint64` | yes | Committed information rate (bits per second). |
| `cbs_bytes` | `uint64` | yes | Committed burst size (bytes). |
| `pir_bps` | `uint64` | no | Peak information rate. |
| `pbs_bytes` | `uint64` | no | Peak burst size. |
| `metering_class` | `uint32` | no | Tag emitted in telemetry; used by billing pipeline to group flows. |

### Validation rules

1. `MeterRuleList.policy_id` matches path segment.
2. `meter.cir_bps > 0`; `meter.pir_bps ≥ cir_bps` when set.
3. Empty `match.*` means "any" (5-tuple wildcard).
4. `rules` count ≤ 256 per policy (sanity cap).

## Proto3 sketch

```proto
syntax = "proto3";
package fleetmanager.v1;

import "common.proto";   // IpPrefix

enum MeterDirectionHint {
  METER_DIRECTION_HINT_UNSPECIFIED = 0;
  METER_DIRECTION_HINT_INBOUND     = 1;
  METER_DIRECTION_HINT_OUTBOUND    = 2;
  METER_DIRECTION_HINT_EITHER      = 3;
}

enum MeterAction {
  METER_ACTION_UNSPECIFIED = 0;
  METER_ACTION_PASS        = 1;
  METER_ACTION_DROP        = 2;
}

message MeterBucket {
  uint64 cir_bps         = 1;
  uint64 cbs_bytes       = 2;
  uint64 pir_bps         = 3;
  uint64 pbs_bytes       = 4;
  uint32 metering_class  = 5;
}

message MeterMatch {
  repeated IpPrefix src_prefixes        = 1;
  repeated IpPrefix dst_prefixes        = 2;
  repeated string   src_prefix_tag_refs = 3;
  repeated string   dst_prefix_tag_refs = 4;
  uint32            protocol            = 5;
  uint32            dst_port_lo         = 6;
  uint32            dst_port_hi         = 7;
}

message MeterRule {
  uint32       priority = 1;
  MeterMatch   match    = 2;
  MeterBucket  meter    = 3;
  MeterAction  action   = 4;
}

message MeterPolicy {
  string             policy_id      = 1;
  MeterDirectionHint direction_hint = 2;
  MeterAction        default_action = 3;
  uint32             rule_count     = 4;
  map<string,string> attributes     = 20;
}

message MeterRuleList {
  string   policy_id           = 1;
  uint64   revision            = 2;
  repeated MeterRule rules     = 3;
}
```

## Relationships

- Referenced by: `NicSpec.outbound.meter_policy_id`, `NicSpec.inbound.meter_policy_id`.
- References: `PrefixTag` (via `MeterMatch.*_prefix_tag_refs`).
- Sibling: `MeterRuleList`.

## Change semantics

- **MeterRuleList edit**: cascades to every NicGoalState that references this policy.
- **MeterPolicy metadata edit**: no programming impact.
- Removing/changing `cir_bps` is data-plane disruptive on hot flows.
- Policy deletion rejected if NICs still reference it.

## See also

- [nic-spec](./nic-spec.md) — primary consumer (per direction).
- [prefix-tag](./prefix-tag.md) — resolved targets of tag refs.
- [README](./README.md) — full kind index.