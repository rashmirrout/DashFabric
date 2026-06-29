# pkg/registry/nic — NicRegistry

**Wave / Slice:** 1 / 1.5  
**Status:** 🟢 Landed 2026-06-23  
**Package:** `github.com/dashfabric/fm/pkg/registry/nic`

---

## What this is

The second concrete per-object registry, following the composition
pattern established by `pkg/registry/vnet` (Slice 1.4). `NicRegistry`
is a thin typed wrapper over `registry.Registry[types.ENIID, *NicState]`
that validates state on `Add`, normalises `AclGroupIDs` to canonical
form, and returns defensive copies on every read path.

---

## NicState schema

```go
type NicState struct {
    ENIID         types.ENIID         // CB-assigned ENI ID; validated at Add. Required.
    VnetID        types.VnetID        // VNET this NIC belongs to; validated at Add. Required.
    MacAddress    string              // IEEE 802 colon-hex; net.ParseMAC validated. Required.
    AclGroupIDs   []types.AclGroupID  // sorted, deduplicated; each validated at Add.
    RouteGroupID  types.RouteGroupID  // zero-value "" = unassigned; validated when non-zero.
    MeterPolicyID types.MeterPolicyID // zero-value "" = unassigned; validated when non-zero.
    SpecRevision  types.SpecRevision  // monotonic; use for stale-snapshot detection.
    LastUpdated   time.Time           // wall-clock from producer; readability only.
}
```

Fields are exported for snapshot / audit convenience but the struct
must be treated as **immutable post-Add**. Clone, mutate, re-Add.

---

## AclGroupIDs invariants

`AclGroupIDs` is stored in **canonical form**: lex-sorted and
deduplicated. `sortDedupAclGroups` enforces this inside `Add`, matching
the `PeerVnetIDs` treatment in `VnetRegistry`.

Additional notes:
- DASH defines up to **6 ACL slots** (stage1+stage2 × v4+v6 × in+out).
  Wave 1 stores a flat list — slot identity is carried by Wave 2 when
  the adapter supplies the per-slot context. See Future Scopes.
- Each entry is validated via `types.ValidateID` at `Add` time.
- Duplicate entries in the input are silently collapsed to one.

---

## Reference IDs vs. refcount holders

`RouteGroupID` and `MeterPolicyID` are plain reference IDs stored in
`NicState`. They are **not** live refcount holders in Wave 1.

Cross-registry Acquire coupling — where an actor Acquires on the NIC's
VNET registry and shared-group registries before processing — is the
**actor layer's responsibility** (Wave 4). The registries are
intentionally decoupled in Wave 1 so they can be built and tested
independently.

---

## API

```go
r := nic.New()

// Producer:
err := r.Add(&nic.NicState{
    ENIID:        "eni-42",
    VnetID:       "vnet-7",
    MacAddress:   "00:1a:2b:3c:4d:5e",
    AclGroupIDs:  []types.AclGroupID{"acl-in", "acl-out"},
    RouteGroupID: "rg-default",
    SpecRevision: 1,
    LastUpdated:  time.Now(),
})

// Consumer:
ready := r.Acquire(ctx, "eni-42")
defer r.Release("eni-42")

select {
case <-ready:
    n, ok := r.Get("eni-42")  // returns a Clone — safe to mutate
    use(n)
case <-ctx.Done():
    return ctx.Err()
}
```

---

## Composition pattern reference

See `docs/registry/vnet.md §Composition pattern` for the complete
checklist. NicRegistry follows steps 1–8 verbatim; the only
NIC-specific additions are:

- MAC address validation via `net.ParseMAC` (cheap, catches producer
  bugs early).
- Conditional validation for `RouteGroupID` / `MeterPolicyID` (skip
  when zero-value, validate when set).

**Upcoming registries continuing the same pattern:**

| Slice | Registry | Key type | Notable additions vs. NicRegistry |
|---|---|---|---|
| 1.6 | `MappingManager` | `types.MappingID` | VIP/DIP as `net/netip.Addr`; SNAT flag |
| 1.7a | `AclGroupRegistry` | `types.AclGroupID` | Rule-count placeholder |
| 1.7b | `RouteGroupRegistry` | `types.RouteGroupID` | Next-hop IP validation |
| 1.7c | `MeterPolicyRegistry` | `types.MeterPolicyID` | Rate value non-zero |

---

## Future Scopes

- **ACL slot map (Wave 2).** Replace the flat `AclGroupIDs []AclGroupID`
  with a keyed `AclSlots map[AclSlot]AclGroupID` once the adapter
  carries DASH slot identifiers. The current flat list is a
  forward-compatible subset — the adapter can round-trip slot identity
  by carrying it in the `AclGroupID` value itself (e.g.
  `"acl-foo@stage1-v4-in"`) as an interim measure if needed.
- **Cross-registry Acquire chain (Wave 4).** Each NIC actor will
  `Acquire` on `VnetRegistry`, `AclGroupRegistry`, and
  `RouteGroupRegistry` before processing. The actor ordering is
  GlobalRegistry → VnetRegistry → NicRegistry → shared-group registries
  to satisfy the cross-registry lock-order rule
  (`registry-semantics-exact.md §8.2`).
- **Spec-revision monotonicity (Wave 2).** Same as VnetRegistry: `Add`
  will reject writes with `SpecRevision ≤` stored revision.
- **Watch-signal fanout (Wave 2).** Same `Subscription[*NicState]`
  pattern as described in `docs/registry/vnet.md`.
- **MAC uniqueness enforcement.** Currently each `Add` is keyed on
  `ENIID`; two ENIs could carry the same `MacAddress` without
  detection. If CB guarantees MAC uniqueness per-VNET, a secondary
  index `map[string]types.ENIID` inside `NicRegistry` can catch
  duplicates at `Add` time. Defer until the data-model spec mandates it.

---

## Cross-references

- `pkg/registry/semantics.go` — generic `Registry[K, V]` contract.
- `pkg/registry/refcount.go` — `UnderflowError`, `ErrRefcountUnderflow`.
- `pkg/types/ids.go` — `ENIID`, `VnetID`, `AclGroupID`, `RouteGroupID`, `MeterPolicyID`, `ValidateID`.
- `pkg/types/versions.go` — `SpecRevision`.
- `docs/registry/semantics.md` — lifecycle model, REG_007 runbook.
- `docs/registry/vnet.md` — composition pattern template.
- `Specs/FM/registry-go-skeleton.md §2` — NicRegistry skeleton spec.
- `Specs/me-and-ai/fm-data-model-sync.md §2` — ENI reference model.
