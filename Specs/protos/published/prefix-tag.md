# `PrefixTag` — Named IP-prefix group

> **TL;DR:** A reusable, named set of IP prefixes (e.g. `"internet"`,
> `"corp-internal"`, `"azure-storage"`). ACL rules and route rules
> reference these by name so a tenant can update "what counts as
> internet?" once and have every rule pick it up.

**Topic:** `/config/v1/global/<device_id>/prefix_tag/<tag_id>`
**Kind:** `CONFIG_KIND_PREFIX_TAG`
**Scope:** appliance-global (replicated per device)
**Lifecycle owner:** orchestrator
**Subscriber:** HDO actor (process-wide cache)

## Example

```json
{
  "tag_id": "tag-azure-storage",
  "prefixes_v4": [
    "20.150.0.0/15",
    "20.157.0.0/16",
    "52.239.0.0/17"
  ],
  "prefixes_v6": [],
  "attributes": {
    "owner_team": "tag-curators",
    "source": "azure-service-tags",
    "last_synced_at": "2026-06-10T03:00:00Z"
  }
}
```

A rule like "allow outbound to azure-storage on 443" references
`tag-azure-storage` rather than enumerating the prefixes inline.

## Purpose

Decoupling named prefix sets from rules lets large dynamic sets (cloud
service tags, BGP-derived prefixes) update in one place. The HDO
maintains a process-wide cache; NicGoalState composition expands tag
references into concrete prefix lists at materialization time.

`NicSpec.prefix_tag_refs[]` lists which tags the NIC's ACL/route rules
depend on, so HDO can index dirty-set fanout efficiently.

## Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `tag_id` | `string` | yes | Matches path segment. |
| `prefixes_v4` | `repeated IpPrefix` | no¹ | IPv4 CIDRs in the set. |
| `prefixes_v6` | `repeated IpPrefix` | no¹ | IPv6 CIDRs in the set. |
| `attributes` | `map<string,string>` | no | Free-form: source system, sync timestamp, owner. |

¹ At least one of v4/v6 must be non-empty (empty tag is allowed but warns).

### Validation rules

1. `tag_id` must match path segment.
2. Prefix lists deduplicated; overlapping prefixes warn but do not reject.
3. Total prefix count ≤ 16,384 per tag (sanity cap for the cache).

## Proto3 sketch

```proto
syntax = "proto3";
package fleetmanager.v1;

import "common.proto";   // IpPrefix

message PrefixTag {
  string tag_id                      = 1;
  repeated IpPrefix prefixes_v4      = 2;
  repeated IpPrefix prefixes_v6      = 3;
  map<string,string> attributes      = 20;
}
```

## Relationships

- Referenced by: ACL rules, RouteRules (indirectly via `NicSpec.prefix_tag_refs`).
- References: none.

## Change semantics

- Updating a tag's prefixes cascades to every NicGoalState whose rules
  reference the tag. HDO indexes by tag id to compute the dirty set in
  O(refs), not O(all NICs).
- Tag deletion is rejected if any NIC still references it via
  `prefix_tag_refs`. Orchestrator must drain references first.

## See also

- [nic-spec](./nic-spec.md) — `prefix_tag_refs` lists which tags a NIC depends on.
- [acl-group](./acl-group.md), [route-group](./route-group.md) — consumers via rule contents.
- [README](./README.md) — full kind index.