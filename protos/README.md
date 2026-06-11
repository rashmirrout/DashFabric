# FleetManager Proto Contracts

**Package:** `fleetmanager.v1`  
**Protoc target:** `proto3`, `cc_enable_arenas`  
**Status:** v1.0 — Stable; only additive changes after freeze.

---

## File Layout

| File | Purpose |
|------|---------|
| `common.proto`  | Shared scalars: `Uuid`, `DeviceId`, `ShardId`, `IpAddress`, `IpPrefix`, `MacAddress`, `TraceContext`, `ErrorDetail`, `PageRequest/Response`, `AuditMetadata` |
| `models.proto`  | Hierarchical objects: `DeviceProfile`, `HardwareCapabilities`, `HostDeviceObject`, `ContainerObject`, `NICObject`, `DeviceState`, `HeartbeatPayload`, `TelemetryPayload`, plus all object-state enums |
| `delta.proto`   | Delta compilation contract: `DeltaCommand`, `DeltaPlan`, `DeltaResult`, `DeviceGoalState`, `OperationType`, `TargetType`, `DeltaStatus` |
| `dash.proto`    | Glue over upstream DASH protos: `DashObject` (oneof tagged union), `DashObjectKind` (discriminator), `DashObjectKey` |
| `service.proto` | gRPC `FleetManagerService` — 13 unary RPCs (REST parity) + 4 streaming RPCs (`StreamDeviceState`, `StreamTelemetry`, `StreamEvents`, `DeviceChannel`) |

Files are split **by concern**, not by message size. Adding a new object family (e.g. policies) belongs in a new file, not a bigger `models.proto`.

---

## Dependency Graph

```
common.proto   <-- (no internal deps, only WKT)
   ^
   |
models.proto   <-- common
   ^
   |
delta.proto    <-- common, models
   ^
   |
service.proto  <-- common, models, delta
                                                            
dash.proto     <-- (independent: depends only on upstream sonic-dash-api)
```

`service.proto` does **not** import `dash.proto` directly — DASH objects flow through `delta.proto` via `google.protobuf.Any` so non-DASH callers don't pull in the upstream proto tree.

---

## Upstream DASH Imports (`dash.proto`)

`dash.proto` imports the following files from
[`sonic-net/sonic-dash-api`](https://github.com/sonic-net/sonic-dash-api/tree/master/proto):

```
types.proto        appliance.proto    eni.proto          eni_route.proto
vnet.proto         vnet_mapping.proto route.proto        route_group.proto
route_rule.proto   route_type.proto   acl_group.proto    acl_rule.proto
acl_in.proto       acl_out.proto      meter.proto        meter_policy.proto
meter_rule.proto   qos.proto          tunnel.proto       prefix_tag.proto
pa_validation.proto                   outbound_port_map.proto
outbound_port_map_range.proto         ha_set.proto       ha_scope.proto
```

Upstream uses **per-file packages** (`dash.types`, `dash.eni`, `dash.vnet`, …),
so qualified names look like `dash.eni.Eni`, `dash.vnet.Vnet`, etc.

### Where the upstream tree lives

We do **not** copy DASH `.proto` files into this repository. They are pulled in
at build time by one of two mechanisms (pick one per build environment):

| Strategy | Location on disk | Pros | Cons |
|----------|------------------|------|------|
| **A. Git submodule** (recommended for production) | `third_party/sonic-dash-api/` (top-level repo path) | Pinned SHA in `.gitmodules`; offline-friendly; reviewable | Requires `git submodule update --init` after clone |
| **B. CMake `FetchContent`** (default in `CMakeLists.txt.example`) | `<build-dir>/_deps/sonic_dash_api-src/` | No submodule wiring; one-command CI bootstrap | Requires network on first configure; not air-gap-safe |

In **both** strategies the proto root used by `protoc --proto_path` is the
upstream `proto/` subdirectory (e.g. `third_party/sonic-dash-api/proto/`).
That is the only path `dash.proto`'s `import "types.proto";` resolves
against — there is no equivalent to "copying into our tree".

To switch from FetchContent to a submodule:

```bash
git submodule add https://github.com/sonic-net/sonic-dash-api.git \
    third_party/sonic-dash-api
git -C third_party/sonic-dash-api checkout <pinned-sha>
git add .gitmodules third_party/sonic-dash-api
```

Then in CMake, replace the `FetchContent_*` block with:

```cmake
set(DASH_PROTO_DIR ${CMAKE_SOURCE_DIR}/third_party/sonic-dash-api/proto)
```

The build system **must** add `${DASH_PROTO_DIR}` to `--proto_path`.
See `CMakeLists.txt.example` for both wiring options.

---

## Versioning Policy

* Package is **versioned in the path**: `fleetmanager.v1` and
  `package fleetmanager.v1;` in every file.
* Wire-compatibility within v1: only additive changes (new fields, new enum
  values, new RPCs). Removing or renumbering a field requires a `v2` package.
* Enum value `0` is always reserved for `_UNSPECIFIED`. Never reuse a
  tombstoned value — append.
* Every mutating RPC carries `expected_revision` (optimistic concurrency).
  Pass `0` to skip the check.

---

## Generation

### Option A — `protoc` directly

```bash
PROTO_ROOT=protos                                  # this folder
DASH_ROOT=third_party/sonic-dash-api/proto         # submodule path
# (or for FetchContent layout: build/_deps/sonic_dash_api-src/proto)

protoc \
  --proto_path=$PROTO_ROOT \
  --proto_path=$DASH_ROOT \
  --proto_path=/usr/include \
  --cpp_out=build/gen \
  --grpc_out=build/gen \
  --plugin=protoc-gen-grpc=$(which grpc_cpp_plugin) \
  $PROTO_ROOT/fleetmanager/v1/*.proto \
  $DASH_ROOT/*.proto
```

Note: source files live at the repo top level under `protos/` for design-time
review; the build copies (or symlinks) them into `fleetmanager/v1/` on the
proto path so that `import "fleetmanager/v1/common.proto";` resolves.

### Option B — CMake (reference)

See `CMakeLists.txt.example` in this folder. It uses `FetchContent` to pin
`sonic-dash-api` and wires both proto trees into a single
`fleetmanager_proto` target.

---

## Streaming RPC Cheat Sheet

| RPC | Direction | Use Case |
|-----|-----------|----------|
| `StreamDeviceState` | server → client | UI / dashboards: full snapshot then diffs |
| `StreamTelemetry`   | server → client | Telemetry pipeline (Kafka shovel, Grafana) |
| `StreamEvents`      | server → client | Audit + alerting feed for operators |
| `DeviceChannel`     | bidirectional   | Long-lived agent connection: client streams hb/telemetry up, server streams `DeltaCommand` down |

`DeviceChannel` replaces the per-RPC dance for the hot path between
FleetManager and the on-device agent. First client message **must** be
`DeviceHello`; first server message is `DeviceWelcome`.

---

## REST ↔ gRPC Parity

Every REST endpoint in `fleet-manager-rest-api.md` has a unary RPC analogue:

| REST | gRPC |
|------|------|
| `POST /api/v1/devices`                     | `RegisterDevice`     |
| `GET  /api/v1/devices`                     | `ListDevices`        |
| `GET  /api/v1/devices/:id`                 | `GetDevice`          |
| `PUT  /api/v1/devices/:id`                 | `UpdateDevice`       |
| `DELETE /api/v1/devices/:id`               | `DeregisterDevice`   |
| `POST /api/v1/devices/:id/heartbeat`       | `Heartbeat`          |
| `POST /api/v1/devices/:id/telemetry`       | `PushTelemetry`      |
| `GET  /api/v1/devices/:id/state`           | `GetDeviceState`     |
| `GET  /api/v1/devices/:id/objects`         | `ListDeviceObjects`  |
| `GET  /api/v1/devices/:id/deltas`          | `ListPendingDeltas`  |
| `GET  /api/v1/health`                      | `Health`             |

The REST handler implementations (`RESTServiceImpl::Handle*_`) translate
JSON → request proto, call the same backend service object the gRPC
handlers use, then translate response proto → JSON.
