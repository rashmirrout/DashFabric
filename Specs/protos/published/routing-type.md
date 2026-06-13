# `RoutingType` — Fleet-wide action-pipeline catalog

> **TL;DR:** A named recipe that tells the DPU *how to treat traffic* for
> a given role (e.g. "privatelink", "vnet_direct", "vnet_peering"). VNETs
> reference one of these by name to pick their pipeline.

**Topic:** `/config/v1/global/<device_id>/routing_type/<name>`
**Kind:** `CONFIG_KIND_ROUTING_TYPE`
**Scope:** fleet-wide singleton catalog (replicated under every device for locality)
**Lifecycle owner:** orchestrator (`tenant_id="system"`)
**Subscriber:** HDO actor (process-wide cache, lazy)

## Example

```json
{
  "name": "privatelink",
  "items": [
    { "action_name": "action1", "action_type": "4to6", "encap_type": "vxlan_decap" },
    { "action_name": "action2", "action_type": "staticencap", "encap_type": "vxlan" },
    { "action_name": "action3", "action_type": "mapping_lookup" }
  ],
  "attributes": {
    "owner_team": "dash-core",
    "description": "PrivateLink endpoint behavior (4→6 NAT + static encap + mapping lookup)"
  }
}
```

A VNET that sets `routing_type: "privatelink"` will have its data plane
program follow this 3-step action chain.

## Purpose

Per decision **#3**: RoutingType is **fleet-wide singleton** — every DPU
gets the same catalog. It's the most static and most centrally-owned
object in the system; tenants never modify it. The orchestrator
populates the catalog at fleet bootstrap and edits it only on a DASH
schema change.

Splitting RoutingType from VNET lets us evolve the action vocabulary
without rewriting every VNET, and lets the HDO maintain a single shared
map of these.

## Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | `string` | yes | Catalog key (e.g. `"privatelink"`, `"vnet_direct"`, `"vnet_peering"`, `"vnet"`, `"appliance"`). Matches path segment. |
| `items` | `repeated RoutingItem` | yes | Ordered list of action steps. Position is significant. |
| `attributes` | `map<string,string>` | no | Free-form: owning team, description, schema reference. |

### `RoutingItem`

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `action_name` | `string` | yes | Per-item label (e.g. `"action1"`). Matches the DASH gNMI list-key. |
| `action_type` | `string` | yes | DASH action_type enum value (e.g. `"4to6"`, `"staticencap"`, `"mapping_lookup"`, `"direct"`). |
| `encap_type` | `string` | no | When relevant (`"vxlan"`, `"vxlan_decap"`, `"nvgre"`). |
| `vni` | `uint32` | no | Static VNI override for encap actions; usually empty (VNET provides it). |

### Validation rules

1. `name` must match path segment and be lowercase snake_case.
2. `items` must be non-empty.
3. Each `items[*].action_name` unique within the type.
4. `items[*].action_type` must be from the recognized DASH enum.

## Proto3 sketch

```proto
syntax = "proto3";
package fleetmanager.v1;

message RoutingItem {
  string action_name = 1;
  string action_type = 2;
  string encap_type  = 3;
  uint32 vni         = 4;
}

message RoutingType {
  string name                    = 1;
  repeated RoutingItem items     = 2;
  map<string,string> attributes  = 10;
}
```

## Relationships

- Referenced by: `Vnet.routing_type` (every VNET picks exactly one).
- References: none.

## Change semantics

- **Add** a new RoutingType: safe, no existing VNET impacted.
- **Edit** an existing one: cascades to every VNET that references it →
  every NicGoalState in those VNETs recomposes. Treat as a fleet-wide
  change with a maintenance window.
- **Remove**: rejected by the orchestrator if any VNET still references
  it; orchestrator must drain references first.

## Upstream DASH alignment

Upstream DASH defines a fixed set of routing-type *enum values* — e.g.
`vnet`, `vnet_direct`, `privatelink`, `service_tunnel`, `direct` — and
hard-codes their action pipelines in the dataplane. There is **no
upstream `DASH_ROUTING_TYPE_TABLE`**: the routing type is consumed as
an enum field on routes, mapping entries, and similar objects.

FM's `RoutingType` is therefore a **fleet-wide indirection layer** on
top of the upstream enum:

- `name` (`"privatelink"`, `"vnet_direct"`, …) is what VNETs and
  routes reference; at composition time the NO actor compiles this
  back to the upstream enum value the DPU expects.
- `items[]` (the action chain) is FM-side documentation/audit metadata
  — it does not get written to the DPU. The chain it describes is
  already wired into the upstream pipeline for that enum.
- The benefit is purely on the control plane: VNET specs reference a
  *name*, so the orchestrator can rename or annotate routing types
  without rewriting every consumer, and a future upstream DASH that
  exposes pluggable action chains (rather than a fixed enum) plugs
  in here without changing the VNET shape.

In short: this object is FM bookkeeping; the southbound write is
always one of the upstream enum values.

## See also

- [vnet](./vnet.md) — primary consumer via `routing_type`.
- [README](./README.md) — full kind index.
- [envelope](./envelope.md) — wrapper around every published value.