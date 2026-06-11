# `RouteGroup` + `RouteList` — Reusable route table

> **TL;DR:** A reusable, ordered set of routes that many ENIs can share.
> The "header" (`RouteGroup`) carries metadata; the "body" (`RouteList`)
> carries the actual routes. NICs reference the group by id; FleetManager
> materializes per-ENI route entries from it.

**Topics:**
- `/config/v1/group/<device_id>/route_group/<group_id>` → `RouteGroup`
- `/config/v1/group/<device_id>/route_group/<group_id>/routes` → `RouteList`

**Kinds:** `CONFIG_KIND_ROUTE_GROUP`, `CONFIG_KIND_ROUTE_LIST`
**Scope:** appliance-global (per device)
**Lifecycle owner:** orchestrator
**Subscriber:** HDO actor (process-wide cache)

## Example

```json
// /config/v1/group/dpu-007/route_group/rg-acme-prod-default-v4
{
  "group_id": "rg-acme-prod-default-v4",
  "family": "IPV4",
  "description": "ACME prod default outbound route table",
  "route_count": 4,
  "attributes": { "tenant": "acme", "env": "prod" }
}
```

```json
// /config/v1/group/dpu-007/route_group/rg-acme-prod-default-v4/routes
{
  "group_id": "rg-acme-prod-default-v4",
  "revision": 142,
  "routes": [
    { "priority": 100, "dst_prefix": "10.42.0.0/16",        "action": { "kind": "VNET" } },
    { "priority": 200, "dst_prefix": "10.0.0.0/8",          "action": { "kind": "VNET_PEERING", "peer_vnet_id": "vnet-shared-services" } },
    { "priority": 300, "dst_prefix": "0.0.0.0/0",           "action": { "kind": "DEFAULT_TUNNEL", "tunnel_id": "tun-internet-westus2" } },
    { "priority": 999, "dst_prefix": "169.254.169.254/32",  "action": { "kind": "DROP" } }
  ]
}
```

The header has metadata (count, family); the list has the actual routes.
Splitting them lets a NIC subscribe to the header (rarely changes) while
the list updates without rewriting per-NIC bindings.

## Purpose

Per decision **#22**: NicSpec binds `route_group_v4`/`route_group_v6`
directly. FleetManager materializes per-ENI `EniRoute` entries from the
RouteList at programming time (the device sees per-ENI routes; the
control plane edits one shared list).

The split into `RouteGroup` (header) and `RouteList` (body) keeps:
- Header revisions stable for the "does this group exist?" question.
- Body revisions hot for the "what's in it now?" question.
- Per-NIC subscribers can refcount the group and watch the list cheaply.

## `RouteGroup` fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `group_id` | `string` | yes | Matches path segment. |
| `family` | `AddressFamily` enum | yes | `IPV4` or `IPV6` (groups are family-specific). |
| `description` | `string` | no | Human-readable purpose. |
| `route_count` | `uint32` | no | Hint of expected route count in sibling list (sanity check). |
| `attributes` | `map<string,string>` | no | Free-form. |

## `RouteList` fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `group_id` | `string` | yes | Mirrors parent group id; payload self-containment. |
| `revision` | `uint64` | yes | Mirrors `metadata.revision`; convenience for diff. |
| `routes` | `repeated Route` | yes | Ordered route entries; iteration order matches list order. |

### `Route`

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `priority` | `uint32` | yes | Lower = higher priority. Used as tie-breaker for overlapping prefixes. |
| `dst_prefix` | `IpPrefix` | yes | Destination match. |
| `action` | `RouteAction` | yes | What to do with matching traffic. |

### `RouteAction`

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `kind` | enum | yes | `DROP`, `VNET`, `VNET_PEERING`, `DEFAULT_TUNNEL`, `NEXT_HOP`, `PRIVATELINK`, `MAPPING_LOOKUP`. |
| `peer_vnet_id` | `string` | no | When `kind=VNET_PEERING`. |
| `tunnel_id` | `string` | no | When `kind=DEFAULT_TUNNEL`. |
| `next_hop_underlay` | `IpAddress` | no | When `kind=NEXT_HOP`. |
| `metering_class` | `uint32` | no | Optional metering class tag for billing. |

### Validation rules

1. `RouteList.group_id` must match path segment.
2. `routes` count ≤ `RouteGroup.route_count` (warning, not reject).
3. `priority` need not be unique; ties broken by list order.
4. `action.kind=VNET_PEERING` requires `peer_vnet_id`.
5. `action.kind=NEXT_HOP` requires `next_hop_underlay`.
6. `action.kind=DEFAULT_TUNNEL` requires `tunnel_id` (resolved against Tunnel cache).
7. Total route entries ≤ `Appliance.capabilities.max_routes_per_group`.

## Proto3 sketch

```proto
syntax = "proto3";
package fleetmanager.v1;

import "common.proto";   // IpPrefix, IpAddress, AddressFamily

message RouteGroup {
  string         group_id     = 1;
  AddressFamily  family       = 2;
  string         description  = 3;
  uint32         route_count  = 4;
  map<string,string> attributes = 20;
}

message RouteAction {
  enum Kind {
    KIND_UNSPECIFIED    = 0;
    KIND_DROP           = 1;
    KIND_VNET           = 2;
    KIND_VNET_PEERING   = 3;
    KIND_DEFAULT_TUNNEL = 4;
    KIND_NEXT_HOP       = 5;
    KIND_PRIVATELINK    = 6;
    KIND_MAPPING_LOOKUP = 7;
  }
  Kind      kind              = 1;
  string    peer_vnet_id      = 2;
  string    tunnel_id         = 3;
  IpAddress next_hop_underlay = 4;
  uint32    metering_class    = 5;
}

message Route {
  uint32      priority   = 1;
  IpPrefix    dst_prefix = 2;
  RouteAction action     = 3;
}

message RouteList {
  string group_id     = 1;
  uint64 revision     = 2;
  repeated Route routes = 3;
}
```

## Relationships

- Referenced by: `NicSpec.outbound.route_group_v4` / `route_group_v6` (1:N).
- References (RouteAction): `Vnet` (via `peer_vnet_id`), `Tunnel` (via `tunnel_id`).
- Sibling: `RouteList` always paired with `RouteGroup`.

## Change semantics

- **RouteList edit**: cascades to every NicGoalState that references the group.
- **RouteGroup metadata edit** alone: no programming impact.
- **Group deletion**: rejected if any NIC still references it.
- The split header/body allows hot-path edits to the list without
  re-validating the group's existence.

## See also

- [nic-spec](./nic-spec.md) — consumer via `outbound.route_group_*`.
- [prefix-tag](./prefix-tag.md) — route rules may reference tags by name.
- [tunnel](./tunnel.md), [vnet](./vnet.md) — resolved targets of route actions.
- [README](./README.md) — full kind index.