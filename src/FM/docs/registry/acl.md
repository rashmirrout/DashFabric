# pkg/registry/acl — AclGroupRegistry

**Wave / Slice:** 1 / 1.7a  
**Status:** 🟢 Landed 2026-06-23  
**Package:** `github.com/dashfabric/fm/pkg/registry/acl`

---

## What this is

Typed wrapper over `registry.Registry[types.AclGroupID, *AclGroupState]`.
Wave 1 carries a rule-count placeholder; the full rule-object list
arrives with the Wave 2 adapter.

---

## AclGroupState schema

```go
type AclGroupState struct {
    AclGroupID   types.AclGroupID   // CB-assigned; validated at Add. Required.
    VnetID       types.VnetID       // owning VNET; validated at Add. Required.
    RuleCount    int                // count of rules; ≥0. Wave 1 placeholder.
    SpecRevision types.SpecRevision
    LastUpdated  time.Time
}
```

`RuleCount` is named deliberately — not `Rules` — so a future
`Rules []Rule` slice field can be added as an additive change without
renaming or breaking existing code.

---

## Validation invariants

- `AclGroupID` passes `types.ValidateID`.
- `VnetID` passes `types.ValidateID`.
- `RuleCount >= 0` (negative is a producer bug; zero means "empty group, not yet populated").

---

## Future Scopes

- **Full rule-object list (Wave 2).** Replace `RuleCount int` with a
  `Rules []Rule` field once the adapter carries per-rule proto entries.
  The `Clone` method will need to deep-copy the slice; the validation
  will grow a per-rule check.
- **Rule-count cap enforcement.** DASH spec implies an upper bound on
  rules per ACL group. Once the cap is known, `validate()` can enforce
  it here rather than in the adapter. Defer until the spec pins the
  number.
- **Spec-revision monotonicity and watch-signal fanout** — same Wave 2
  additions as the other registries (see `docs/registry/vnet.md`).

---

## Cross-references

- `pkg/registry/semantics.go`, `pkg/types/ids.go`
- `docs/registry/vnet.md` — composition pattern template
- `docs/registry/nic.md` — NicState references AclGroupIDs
