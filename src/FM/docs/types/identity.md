# pkg/types — Identity & Versioning Types

**Wave / Slice:** 1 / 1.1
**Status:** 🟢 Landed 2026-06-22
**Package:** `github.com/dashfabric/fm/pkg/types`

---

## What this is

Named string types for every ID FM passes around, plus three small numeric
types for versioning. No business logic, no I/O.

| Type | Underlying | Purpose |
|---|---|---|
| `ENIID` | `string` | Elastic Network Interface |
| `VnetID` | `string` | VNET (DASH umbrella) |
| `RouteGroupID` | `string` | Outbound routing group |
| `AclGroupID` | `string` | ACL group (1 of up to 6 per ENI) |
| `MappingID` | `string` | VNET mapping entry |
| `MeterPolicyID` | `string` | Meter / rate-limit policy |
| `HaScopeID` | `string` | HA lease scope |
| `DeviceID` | `string` | Managed device (DPU, NIC, vSwitch) |
| `TenantID` | `string` | Tenant — used by OPA / audit |
| `SpecRevision` | `int64` | Per-object monotonic revision |
| `Epoch` | `int64` | Driver session epoch |
| `Watermark` | `int64` | etcd revision (monotone-up only) |

---

## Why named strings, not structs

- **Type safety at compile time.** `func Bind(ENIID, VnetID)` rejects
  swapped arguments. A `struct{ID string}` wouldn't — both fields are
  positional `string` under the hood.
- **Zero runtime cost.** Same memory layout as `string`; no boxing.
- **JSON / proto compatibility.** Marshals as a plain string by default.
- **Map-key friendly.** Each typed ID is its own map-key type.

A `type X string` in Go is a *distinct* type, not a type *alias*. Assigning
`ENIID("x")` to a `VnetID` variable is a compile error. That is exactly the
guarantee we want.

---

## ID contract

IDs in FM are opaque. The Control Bridge is the sole minter
(see `Specs/me-and-ai/fm-data-model-sync.md §3`). FM does:

- Accept any string that passes `ValidateID`.
- Pass it through unchanged to registries, drivers, audit, logs.

FM does not:

- Parse the ID.
- Synthesize new IDs.
- Translate between ID formats.
- Assume UUID / ULID / hash / human-readable.

### `ValidateID(s string) error`

Universal gate at trust boundaries:

- `len(s) > 0`
- `len(s) <= MaxIDLen` (256 bytes)
- No `' '`, `'\t'`, `'\n'`, `'\r'`, `'\x00'`

That is the entire contract. Anything stricter belongs in CB.

---

## Versioning types

### `SpecRevision`

Carried on every spec object (NicSpec, VnetSpec, ...). The Adapter uses
`a.Newer(b)` to drop stale writes. Equal revisions are *not* newer — caller
chooses how to handle ties (usually idempotent re-apply).

### `Epoch`

Driver-session epoch. Bumped on every reconnect. Apply results stamped with
a stale epoch are discarded
(see `Specs/FM/southbound-driver-interface-redesign.md §4`).

### `Watermark`

etcd revision. May only advance. Regression trips
`Specs/Runbooks/ADP_008_WATERMARK_REGRESSION.md` — this is a hard fault,
not a recoverable condition.

---

## Tests (`ids_test.go`)

- Length bounds: 0, 1, 256, 257.
- Whitespace and NUL rejection.
- Constructor rejection on invalid input.
- `IsZero` for every typed ID.
- JSON round-trip with field tags.
- Map-key behavior across distinct types.
- `SpecRevision.Newer`, `Epoch.Bumped`, `Watermark.Regressed` ordering.

Run: `go test ./pkg/types/...`

---

## Future Scopes

- **Format-version field on spec objects.** If CB ever changes ID grammar
  (e.g., UUID → ULID), a `format_version` byte on each spec lets FM and CB
  evolve independently. Today FM ignores format entirely.
- **ID rotation.** If a security event requires CB to rotate an ID
  in-flight, FM needs a paired (`old_id`, `new_id`, `cutover_watermark`)
  message so it can swap registry keys atomically. Out of scope for Wave 1.
- **Cross-type compile-failure golden.** A `// +build ignore` example
  showing `var x ENIID = VnetID("y")` is a compile error. Useful for
  onboarding; not required for correctness.
- **Stringer codegen.** If we later want pretty-printed enums for, say,
  ACL stage, `go generate ./...` with `stringer` can land here without
  touching the ID types.

---

## Cross-references

- `Specs/me-and-ai/fm-data-model-sync.md §3` — identity decision record.
- `Specs/FM/inc-closure-2.2-2.3.md §3` — NicSpec schema, `spec_revision`.
- `Specs/FM/southbound-driver-interface-redesign.md §4` — epoch semantics.
- `Specs/Runbooks/ADP_008_WATERMARK_REGRESSION.md` — watermark fault.
- `pkg/types/doc.go` — package-level overview.
