# `cb_fm_protos/` — CB ↔ FM Wire Contract

> **Status:** Draft v1
> **Module version:** `v1` (mirrored in topic root `/dashfabric/v1/...`)
> **Audience:** Anyone implementing or consuming the CB↔FM wire.

This folder is the **single locked source of truth** for the wire
between any ControllerBridge (CB) implementation and DashFabric's
Fleet Manager (FM). Vendors compile against these protos; FM compiles
against these protos; the conformance suite verifies these protos.

## Layout

```
cb_fm_protos/
├── service/
│   ├── cb_service.proto      # gRPC service: Subscribe, Publish, Get, List, Resync, Health, Init, Topics
│   ├── cb_events.proto       # Event envelope (topic, key, payload, watermark, type)
│   ├── cb_acks.proto         # DeliveryAck, StateAck, ResourceState
│   └── cb_errors.proto       # Status / error codes specific to CB
├── topics/
│   ├── device.proto          # /config/devices/*
│   ├── nic.proto             # /config/nics/*          (DASH_ENI_TABLE)
│   ├── vnet.proto            # /config/vnets/*         (DASH_VNET_TABLE)
│   ├── mapping.proto         # /config/mappings/*      (DASH_VNET_MAPPING_TABLE)
│   ├── acl.proto             # /config/acls/*          (DASH_ACL_GROUP/RULE_TABLE)
│   ├── route.proto           # /config/routes/*        (DASH_ROUTE_GROUP/TABLE)
│   ├── vip.proto             # /config/vnets/*/vips/*  (DASH_VIP_TABLE)
│   ├── meter.proto           # /config/meters/*        (DASH_METER_TABLE)
│   ├── vm.proto              # /config/vms/*           (FM-extension)
│   ├── container.proto       # /config/containers/*    (FM-extension)
│   └── ha.proto              # /config/ha/*            (DASH_HA_SET_TABLE)
└── common/
    ├── ids.proto             # eni_id, vnet_id, dpu_id, mac, prefix
    ├── dash_types.proto      # CA/PA, encap, address-family, prefix
    └── annotations.proto     # upstream/envelope/synthetic field markers
```

## Versioning

| Layer | Version anchor | Bump rule |
|-------|----------------|-----------|
| Topic root | `/dashfabric/v1/...` | Major bump on any breaking topic semantics change |
| `option (cb.proto_major) = 1` | Per-file | Major bump on any breaking proto change |
| Field tags | Reserved on removal | Never reused |

Breaking changes ship as a new major prefix (`v2`), kept in parallel
with the previous version for at least one release. `Init` negotiates
the highest mutually supported major.

## Stability commitment

- Adding fields: backward-compatible (new tags, optional).
- Adding RPCs: backward-compatible (new methods).
- Adding topics: backward-compatible (new files in `topics/`).
- Removing or renaming fields / RPCs / topics: requires major bump.
- Changing field semantics without renaming: forbidden.

## DASH alignment marker

Each topic proto declares its lineage via `common/annotations.proto`:

```proto
message VnetConfig {
  option (cb.dash_table) = "DASH_VNET_TABLE";

  string vnet_id        = 1 [(cb.field) = UPSTREAM];
  uint32 vni            = 2 [(cb.field) = UPSTREAM];
  EncapType encap_type  = 3 [(cb.field) = UPSTREAM];
  string tenant         = 4 [(cb.field) = ENVELOPE];   // FM-added
  string region         = 5 [(cb.field) = ENVELOPE];   // FM-added
}
```

`UPSTREAM` / `ENVELOPE` / `SYNTHETIC` per project Protocol 1.

## Build

```bash
# Protoc with Go + gRPC plugins
buf generate cb_fm_protos
```

Build configurations for Go, Rust, Python, Java live in
`buf.gen.<lang>.yaml`.

## Compatibility tests

The conformance harness (see `Specs/CB/04-cb-conformance-suite.md`)
exercises every RPC and every topic payload against a target CB.
`cb_fm_protos/` is the contract under test.

## Files in this folder

The current draft contains **skeleton** definitions — RPCs are wired,
field shape is sketched, but field-by-field finalization happens in
the next round of design. Each `.proto` carries a `// TODO(v1):`
banner where field set is provisional.

| Status | File |
|--------|------|
| Skeleton | `service/cb_service.proto` |
| Skeleton | `service/cb_events.proto` |
| Skeleton | `service/cb_acks.proto` |
| Skeleton | `service/cb_errors.proto` |
| Skeleton | `topics/device.proto` |
| Skeleton | `topics/nic.proto` |
| Skeleton | `topics/vnet.proto` |
| Skeleton | `topics/mapping.proto` |
| Skeleton | `topics/acl.proto` |
| Skeleton | `topics/route.proto` |
| Skeleton | `topics/vip.proto` |
| Skeleton | `topics/meter.proto` |
| Skeleton | `topics/vm.proto` |
| Skeleton | `topics/container.proto` |
| Skeleton | `topics/ha.proto` |
| Skeleton | `common/ids.proto` |
| Skeleton | `common/dash_types.proto` |
| Skeleton | `common/annotations.proto` |
