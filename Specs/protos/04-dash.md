# `dash.proto` — Tagged Union over Upstream DASH Protos

**Source:** [`protos/dash.proto`](../../protos/dash.proto)
**Imports:** Upstream files from
[`sonic-net/sonic-dash-api`](https://github.com/sonic-net/sonic-dash-api/tree/master/proto):
`types.proto`, `appliance.proto`, `vnet.proto`, `vnet_mapping.proto`,
`eni.proto`, `eni_route.proto`, `route.proto`, `route_group.proto`,
`route_rule.proto`, `route_type.proto`, `acl_group.proto`, `acl_rule.proto`,
`acl_in.proto`, `acl_out.proto`, `meter.proto`, `meter_policy.proto`,
`meter_rule.proto`, `qos.proto`, `tunnel.proto`, `prefix_tag.proto`,
`pa_validation.proto`, `outbound_port_map.proto`,
`outbound_port_map_range.proto`, `ha_set.proto`, `ha_scope.proto`.

A glue layer. We don't redefine the DASH schema — we **wrap** it so it can
flow through `DeltaCommand` without leaking 26 separate types into every
caller.

---

## 1. Why a Wrapper at All?

`DeltaCommand.desired` is `google.protobuf.Any`, which works but has two
practical problems:

1. **Type discovery is awkward.** Callers have to parse `Any.type_url`
   (`type.googleapis.com/dash.eni.Eni`) to know what's inside.
2. **Schema is unbounded.** `Any` accepts *anything*, so the proto file
   doesn't document what FleetManager actually supports.

`DashObject` solves both:

- **`DashObjectKind` enum** is the discriminator — one tag per supported
  upstream type. Callers `switch` on it; no string parsing.
- **The `oneof`** acts as a **closed-world manifest**: if it's not in the
  oneof, FleetManager doesn't accept it. New DASH objects require an
  explicit, reviewable schema bump.

---

## 2. Upstream Layout (read this first)

`sonic-dash-api` uses **per-file packages**, not a single shared one. Pattern:

```
proto/types.proto       package dash.types       message IpAddress, IpPrefix, Guid, ...
proto/eni.proto         package dash.eni         message Eni, EniKey
proto/vnet.proto        package dash.vnet        message Vnet, VnetKey
proto/route.proto       package dash.route       message Route, RouteKey
... etc.
```

So a fully-qualified DASH name looks like `dash.eni.Eni`, `dash.vnet.Vnet`.
Every domain file defines `<Object>` plus `<Object>Key`. Common shared types
live in `dash.types` and are imported by anything using IPs or GUIDs.

The build system **must** add the upstream `proto/` folder to `--proto_path`.
See [`/protos/CMakeLists.txt.example`](../../protos/CMakeLists.txt.example).

---

## 3. `DashObjectKind` — discriminator enum

| Value | Maps to upstream type |
|-------|----------------------|
| `DASH_OBJECT_KIND_APPLIANCE` (1) | `dash.appliance.Appliance` |
| `DASH_OBJECT_KIND_VNET` (2) | `dash.vnet.Vnet` |
| `DASH_OBJECT_KIND_VNET_MAPPING` (3) | `dash.vnet_mapping.VnetMapping` |
| `DASH_OBJECT_KIND_ENI` (4) | `dash.eni.Eni` |
| `DASH_OBJECT_KIND_ENI_ROUTE` (5) | `dash.eni_route.EniRoute` |
| `DASH_OBJECT_KIND_ROUTE` (6) | `dash.route.Route` |
| `DASH_OBJECT_KIND_ROUTE_GROUP` (7) | `dash.route_group.RouteGroup` |
| `DASH_OBJECT_KIND_ROUTE_RULE` (8) | `dash.route_rule.RouteRule` |
| `DASH_OBJECT_KIND_ROUTE_TYPE` (9) | `dash.route_type.RouteType` |
| `DASH_OBJECT_KIND_ACL_GROUP` (10) | `dash.acl_group.AclGroup` |
| `DASH_OBJECT_KIND_ACL_RULE` (11) | `dash.acl_rule.AclRule` |
| `DASH_OBJECT_KIND_ACL_IN` (12) | `dash.acl_in.AclIn` |
| `DASH_OBJECT_KIND_ACL_OUT` (13) | `dash.acl_out.AclOut` |
| `DASH_OBJECT_KIND_METER` (14) | `dash.meter.Meter` |
| `DASH_OBJECT_KIND_METER_POLICY` (15) | `dash.meter_policy.MeterPolicy` |
| `DASH_OBJECT_KIND_METER_RULE` (16) | `dash.meter_rule.MeterRule` |
| `DASH_OBJECT_KIND_QOS` (17) | `dash.qos.Qos` |
| `DASH_OBJECT_KIND_TUNNEL` (18) | `dash.tunnel.Tunnel` |
| `DASH_OBJECT_KIND_PREFIX_TAG` (19) | `dash.prefix_tag.PrefixTag` |
| `DASH_OBJECT_KIND_PA_VALIDATION` (20) | `dash.pa_validation.PaValidation` |
| `DASH_OBJECT_KIND_OUTBOUND_PORT_MAP` (21) | `dash.outbound_port_map.OutboundPortMap` |
| `DASH_OBJECT_KIND_OUTBOUND_PORT_MAP_RANGE` (22) | `dash.outbound_port_map_range.OutboundPortMapRange` |
| `DASH_OBJECT_KIND_HA_SET` (23) | `dash.ha_set.HaSet` |
| `DASH_OBJECT_KIND_HA_SCOPE` (24) | `dash.ha_scope.HaScope` |

**Tag numbers are stable** — never reuse a tombstoned value. Adding a new
DASH object means appending a new enum value AND a new oneof arm.

---

## 4. `DashObject` — the union body

| Field | Type | Purpose |
|-------|------|---------|
| `kind` | `DashObjectKind` | Discriminator. Redundant with the oneof tag, but makes JSON dumps and logs readable |
| `value` | `oneof` | Exactly one upstream message — see table above |

### Why both `kind` and the oneof tag?

- The oneof tag lets generated code do `switch (dash_object.value_case())`,
  which is O(1) and exhaustive.
- The `kind` field round-trips through JSON as a string
  (`"DASH_OBJECT_KIND_ENI"`) which is readable in audit logs and dashboards
  without requiring a proto-aware viewer. Server validates that
  `kind` matches `value_case()`; mismatches return `VALIDATION_ERROR`.

---

## 5. `DashObjectKey` — lightweight reference for DELETE deltas

When a `DeltaCommand` is `OPERATION_TYPE_DELETE`, we don't want to ship the
full object body — just the key.

| Field | Type | Purpose |
|-------|------|---------|
| `kind` | `DashObjectKind` | Same discriminator as `DashObject` |
| `canonical_id` | `string` | Composite key as a JSON string. Always present |
| `typed_key` | `oneof` | Strongly-typed key for the most-frequently-deleted objects (`EniKey`, `VnetKey`, `RouteKey`, `AclGroupKey`, `AclRuleKey`) |

`canonical_id` is always populated — it's the universal fallback. `typed_key`
is exposed only for hot-path objects to spare the driver a JSON parse on
delete. Less-common objects use `canonical_id` only.

---

## 6. Where `DashObject` Appears in the System

```
DeviceGoalState.dash_objects[]   <-- repeated google.protobuf.Any wrapping DashObject
DeltaCommand.desired             <-- google.protobuf.Any wrapping DashObject (CREATE/UPDATE)
DeltaCommand.current             <-- google.protobuf.Any wrapping DashObject (UPDATE/DELETE)
```

The southbound DASH driver:

1. Unpacks the `Any` to a `DashObject`.
2. Switches on `value_case()`.
3. Dispatches to the matching gNMI/SAI call (`SetEni`, `SetVnet`, …).
4. Returns a `DeltaResult` with the SAI/gNMI status code in `diagnostics`.

Non-DASH drivers (Linux netlink) ignore deltas with DASH `target_type`
values — they're filtered out at dispatch time based on the device's
declared `DeviceType`.
