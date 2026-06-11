# Published Config Schemas

Schemas for every config object the **orchestrator publishes to etcd** and
that FleetManager subscribes to and consumes. One file per kind.

Companion to [`Specs/FM/vm-eni-provisioning-design.md`](../../FM/vm-eni-provisioning-design.md)
which contains the 27 design decisions these schemas implement.

## Conventions

- Every published value is wrapped in [`ConfigEntry`](./envelope.md). The `kind`
  field on `ConfigMetadata` discriminates the payload.
- Field names: `snake_case` in proto, same in JSON.
- IDs are opaque strings unless noted (e.g. UUIDs, MACs).
- IPs follow the canonical-string rule from `Specs/protos/06-rest-mapping.md §1.2`.
- All revisions are monotonic per-key `uint64`.
- All timestamps are RFC 3339 UTC strings in JSON, `google.protobuf.Timestamp` in proto.

## Index

### Foundation
- [envelope](./envelope.md) — `ConfigEntry` + `ConfigMetadata` wrapper
- [error](./error.md) — `/status/v1/` validation-error sibling key schema

### Appliance-global scope — `/config/v1/global/<device_id>/...`
- [appliance](./appliance.md) — DPU appliance config
- [routing-type](./routing-type.md) — fleet-wide RoutingType catalog
- [tunnel](./tunnel.md) — underlay tunnel definitions
- [qos](./qos.md) — QoS profiles
- [prefix-tag](./prefix-tag.md) — named IP-prefix groups
- [ha-set](./ha-set.md) — HA peering configuration

### Reusable groups — `/config/v1/group/<device_id>/...`
- [route-group](./route-group.md) — RouteGroup + Route rows
- [acl-group](./acl-group.md) — AclGroup + AclRule rows
- [meter-policy](./meter-policy.md) — MeterPolicy + MeterRule rows
- [outbound-port-map](./outbound-port-map.md) — OutboundPortMap + ranges

### Per-VNET scope — `/config/v1/vnet/<device_id>/<vnet_id>/...`
- [vnet](./vnet.md) — VNET spec
- [pa-validation](./pa-validation.md) — PA validation list
- [vnet-mapping](./vnet-mapping.md) — sharded mapping chunks + manifest

### Host hierarchy — `/config/v1/hosts/<device_id>/...`
- [host-spec](./host-spec.md) — host-level config (rarely changes)
- [container-spec](./container-spec.md) — per-VM/container metadata
- [nic-spec](./nic-spec.md) — per-NIC reference bundle (the ENI input)

### Composed (not published — derived by NO actor)
- [nic-goal-state](./nic-goal-state.md) — the full denormalized ENI program

## Topic-tree summary

```
/config/v1/
├── global/<device_id>/
│   ├── appliance                       → Appliance
│   ├── routing_type/<name>             → RoutingType
│   ├── tunnel/<tunnel_id>              → Tunnel
│   ├── qos/<qos_id>                    → Qos
│   ├── prefix_tag/<tag_id>             → PrefixTag
│   └── ha_set/<ha_set_id>              → HaSet
├── group/<device_id>/
│   ├── route_group/<group_id>          → RouteGroup
│   ├── route_group/<group_id>/routes   → RouteList
│   ├── acl_group/<group_id>            → AclGroup
│   ├── acl_group/<group_id>/rules      → AclRuleList
│   ├── meter_policy/<policy_id>        → MeterPolicy
│   ├── meter_policy/<policy_id>/rules  → MeterRuleList
│   └── outbound_port_map/<map_id>      → OutboundPortMap (+ ranges sibling)
├── vnet/<device_id>/<vnet_id>/
│   ├── spec                            → Vnet
│   ├── pa_validation                   → PaValidation
│   ├── mapping/_manifest               → VnetMappingManifest
│   └── mapping/<chunk_id>              → VnetMappingChunk
└── hosts/<device_id>/
    ├── spec                            → HostSpec
    └── <container_guid>/
        ├── spec                        → ContainerSpec
        └── <nic_id>/spec               → NicSpec

/status/v1/...                          → ValidationError sibling keys
```
