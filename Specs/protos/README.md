# FleetManager Proto Contracts — Specification

**Source files:** `protos/` (top-level)
**Package:** `fleetmanager.v1`
**Status:** v1.0 — Stable; only additive changes after freeze.

This folder is the **design-time reference** for every `.proto` file in
`/protos/`. Each document explains *what* the messages mean, *why* the fields
exist, and *how* they flow through the system. The proto files themselves
remain the authoritative wire schema; these docs supply context that comments
in a `.proto` cannot.

---

## Reading Order

| # | Document | What you learn |
|---|----------|---------------|
| 1 | [`01-common.md`](01-common.md) | Shared scalars, identifiers, IPs, trace context, errors, pagination, audit metadata |
| 2 | [`02-models.md`](02-models.md) | Hierarchical object model — `DeviceProfile`, `HostDeviceObject`, `ContainerObject`, `NICObject`, `DeviceState`, all lifecycle/health enums |
| 3 | [`03-delta.md`](03-delta.md) | Programming pipeline — `DeltaCommand`, `DeltaPlan`, `DeltaResult`, `DeviceGoalState`, `OperationType`, `TargetType`, `DeltaStatus` |
| 4 | [`04-dash.md`](04-dash.md) | Tagged-union over upstream `sonic-dash-api` messages: `DashObject`, `DashObjectKind`, `DashObjectKey` |
| 5 | [`05-service.md`](05-service.md) | gRPC `FleetManagerService` — every RPC, request/response shape, streaming envelopes |
| 6 | [`06-rest-mapping.md`](06-rest-mapping.md) | How each proto field surfaces in the REST JSON schema; HTTP↔gRPC error mapping |

---

## Dependency Graph

```
common.proto    <-- (no internal deps, only google WKT)
   ^
   |
models.proto    <-- common
   ^
   |
delta.proto     <-- common, models
   ^
   |
service.proto   <-- common, models, delta

dash.proto      <-- (independent: depends only on upstream sonic-dash-api)
```

`service.proto` does **not** import `dash.proto` directly. DASH objects flow
through `delta.proto` via `google.protobuf.Any`, so non-DASH callers don't
pull in the upstream proto tree.

---

## Conventions Used Across All Protos

1. **Proto3 syntax**, `cc_enable_arenas = true`, `java_multiple_files = true`.
2. **Versioned package:** `fleetmanager.v1`. A breaking change forks to `v2`.
3. **Enum value `0` is always `_UNSPECIFIED`.** Never reuse a tombstoned value.
4. **String-wrapped IDs** (`DeviceId{value}`, `ShardId{value}`, `Uuid{value}`)
   so we can extend with metadata later without breaking wire compat.
5. **`TraceContext` on every request** — W3C Trace Context propagated end-to-end
   from upstream caller through PubSub, the per-device actor, the southbound
   HAL, and any emitted `FleetEvent`.
6. **Optimistic concurrency:** mutating RPCs accept `expected_revision`.
   Pass `0` to skip the check; otherwise must match `AuditMetadata.revision`.
7. **Spec / Status separation:** caller-supplied fields live on input messages
   (e.g. `DeviceProfile`); server-owned fields live on output messages
   (e.g. `HostDeviceObject.state`, `audit`, `state_hash`). Mixing them would
   let callers spoof reconciliation state.
8. **Field-mask updates:** `UpdateDeviceRequest` carries
   `google.protobuf.FieldMask` so partial updates do not require sending the
   full object.
