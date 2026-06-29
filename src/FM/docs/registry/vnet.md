# pkg/registry/vnet — VnetRegistry

**Wave / Slice:** 1 / 1.4  
**Status:** 🟢 Landed 2026-06-23  
**Package:** `github.com/dashfabric/fm/pkg/registry/vnet`

---

## What this is

The first concrete per-object registry built on the shared
`registry.Registry[K, V]` contract (Slice 1.3). `VnetRegistry` is a thin,
typed wrapper that restricts the generic API to `types.VnetID` keys and
`*VnetState` values, adds FM-side validation on `Add`, and provides
defensive copies on every read path so callers cannot corrupt stored state.

---

## VnetState schema

```go
type VnetState struct {
    VnetID       types.VnetID      // CB-assigned, validated at Add time
    VNI          uint32            // VXLAN network identifier; 0 = reserved
    PeerVnetIDs  []types.VnetID    // sorted, deduplicated; see Peer-list invariants
    SpecRevision types.SpecRevision // monotonic; use for stale-snapshot detection
    LastUpdated  time.Time         // wall-clock from producer; readability only
}
```

Fields are exported for snapshot / audit convenience but the struct must
be treated as **immutable post-Add**. Callers that need to mutate should
`Clone()`, mutate the clone, and call `Add` with the new value.

---

## Peer-list invariants

`PeerVnetIDs` is stored in **canonical form**: lex-sorted and
deduplicated. The canonical form is enforced by `sortDedupPeers` inside
`Add`, so:

- Callers may pass the slice in any order; the registry normalises it.
- Duplicate entries in the input are silently collapsed.
- The canonical form enables O(n) set-diff against an incoming update
  (Wave 2 adapters will use this for peering change detection).

Additional invariants enforced at `Add` time:
1. `VnetID` passes `types.ValidateID` (non-empty, ≤ 256 chars, no
   whitespace/NUL).
2. `VNI != 0` (0 is reserved for "unassigned").
3. Every peer ID passes `types.ValidateID`.
4. No self-peer (`VnetID` must not appear in `PeerVnetIDs`).

Violations return a descriptive error and store nothing.

---

## API

```go
r := vnet.New()

// Producer writes state (typically the adapter, Wave 2):
err := r.Add(&vnet.VnetState{
    VnetID:       "vnet-42",
    VNI:          4242,
    PeerVnetIDs:  []types.VnetID{"vnet-7"},
    SpecRevision: 1,
    LastUpdated:  time.Now(),
})

// Consumer signals interest and waits for hydration:
ready := r.Acquire(ctx, "vnet-42")
defer r.Release("vnet-42")

select {
case <-ready:
    v, ok := r.Get("vnet-42")    // returns a Clone — safe to mutate
    use(v)
case <-ctx.Done():
    return ctx.Err()
}

// Bulk operator view:
snap := r.Snapshot()   // map[types.VnetID]*VnetState, all values deep-copied
```

All read operations (`Get`, `Snapshot`) return **deep copies** — the
caller owns the returned value outright. See Composition pattern below
for the rationale and cost note.

---

## Composition pattern — how to build the next 5 registries

Every per-object registry in Wave 1 follows this identical blueprint:

```
pkg/registry/<object>/
├── doc.go          # package overview, design refs
├── <object>.go     # <Object>State struct + Clone + validate + <Object>Registry
└── <object>_test.go
```

**Checklist for each new registry:**

1. Define `<Object>State` with the FM-internal projection fields (not the
   CB wire proto — the adapter in Wave 2 translates). Export every field
   for snapshot/audit.
2. Add `Clone() *<Object>State` with independent allocations for every
   slice field.
3. Add `validate() error` enforcing ID-validation, non-zero required
   fields, and any relational invariants (no self-reference, etc.).
4. Define `<Object>Registry` composing
   `*registry.Registry[types.<Object>ID, *<Object>State]` with the inner
   field **unexported**.
5. Proxy all public methods with typed args; call `state.validate()` then
   `state.Clone()` inside `Add` before forwarding to `inner.Add`.
6. Return `Clone()` from `Get` and deep-copy values in `Snapshot`.
7. Tests: round-trip Add/Get, deep-copy on Get, validation rejection
   cases, REG_007 propagation through the wrapper.
8. Doc: `docs/registry/<object>.md` following this template.

**Upcoming registries (Slices 1.5–1.7):**

| Slice | Registry | Key type | Notable validation |
|---|---|---|---|
| 1.5 | `NicRegistry` | `types.ENIID` | VnetID must validate; shared-group IDs must validate |
| 1.6 | `MappingManager` | `types.MappingID` | VIP/DIP are IP strings; binding uniqueness |
| 1.7a | `AclGroupRegistry` | `types.AclGroupID` | Rule count cap (Wave 1 placeholder) |
| 1.7b | `RouteGroupRegistry` | `types.RouteGroupID` | Next-hop IP validation |
| 1.7c | `MeterPolicyRegistry` | `types.MeterPolicyID` | Rate value non-zero |

---

## Concurrency

Inherits the single `sync.RWMutex` from `registry.Registry`. No
additional locking in `VnetRegistry` — the mutex boundary is the inner
generic instance. The `sortDedupPeers` call in `Add` occurs on the
already-cloned value before the inner lock is taken, so no in-lock
allocation.

---

## Future Scopes

- **Watch-signal fanout (Wave 2).** Wave 1 consumers poll via
  `Acquire`/`Ready`. In Wave 2 the adapter will feed `Add` from the T1
  watch stream; the `Subscription[*VnetState]` type (see
  `registry-semantics-exact.md §2`) will add `Updates <-chan *VnetState`
  to the ready-channel so actors can react to every state change, not
  just the first hydration. Today's `<-chan struct{}` from `Acquire` is
  the Ready field of that future Subscription.
- **Spec-revision monotonicity enforcement.** `Add` currently accepts any
  `SpecRevision`. Wave 2 will add a non-regression check: if the incoming
  `SpecRevision ≤` stored revision, `Add` returns
  `ErrStaleRevision` rather than overwriting. This is safe to add
  without breaking Wave 1 callers because they don't rely on the current
  overwrite-always behaviour.
- **Peering set-diff in adapter.** `PeerVnetIDs` is stored sorted so the
  Wave 2 adapter can compute added/removed peers in O(n) by merging the
  stored slice against the incoming slice. No registry change required —
  the canonical form is already in place.
- **VnetState equality short-circuit.** A future `Equal(*VnetState) bool`
  method could let `Add` skip the clone + store when the value is
  unchanged, reducing allocations during no-op reconcile loops. Defer
  until profiling shows this is hot.

---

## Cross-references

- `pkg/registry/semantics.go` — generic `Registry[K, V]` contract.
- `pkg/registry/refcount.go` — `UnderflowError`, `ErrRefcountUnderflow`.
- `pkg/types/ids.go` — `VnetID`, `ValidateID`.
- `pkg/types/versions.go` — `SpecRevision`.
- `docs/registry/semantics.md` — lifecycle model, REG_007 runbook.
- `Specs/FM/registry-go-skeleton.md §1` — VnetRegistry skeleton spec.
- `Specs/me-and-ai/fm-data-model-sync.md §2` — VNET reference model.
