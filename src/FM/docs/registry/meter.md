# pkg/registry/meter — MeterPolicyRegistry

**Wave / Slice:** 1 / 1.7c  
**Status:** 🟢 Landed 2026-06-23  
**Package:** `github.com/dashfabric/fm/pkg/registry/meter`

---

## What this is

Typed wrapper over `registry.Registry[types.MeterPolicyID, *MeterPolicyState]`.
Tracks rate-limit / billing policies attached to ENIs and VNETs.

---

## MeterPolicyState schema

```go
type MeterPolicyState struct {
    MeterPolicyID types.MeterPolicyID  // CB-assigned; validated at Add. Required.
    VnetID        types.VnetID         // owning VNET; validated at Add. Required.
    RateBps       uint64               // sustained rate in bits/s; must be > 0.
    BurstBps      uint64               // burst capacity in bits/s; 0 = same as RateBps.
    SpecRevision  types.SpecRevision
    LastUpdated   time.Time
}
```

---

## BurstBps=0 convention

`BurstBps == 0` means "burst capacity equals sustained rate" per the
dataplane driver convention. This is explicitly **allowed** at the
registry level — the driver, not the registry, interprets the zero
value. The registry stores it as-received.

---

## Validation invariants

- `MeterPolicyID` passes `types.ValidateID`.
- `VnetID` passes `types.ValidateID`.
- `RateBps > 0` — a zero rate is always a producer bug (it would mean
  "block all traffic", which CB expresses by removing the binding, not
  setting a zero rate).
- `BurstBps` is unrestricted (0 is valid; values < RateBps are
  permitted — the driver enforces semantics).

---

## Future Scopes

- **Per-ENI meter inheritance (Wave 4).** A NIC actor may inherit a
  meter policy from its VNET if none is directly assigned. The
  inheritance logic belongs in the actor layer, not the registry.
  Document the lookup order in the actor design doc.
- **Bidirectional rates.** Some meter specs distinguish ingress vs.
  egress rates. A future `EgressRateBps` / `IngressRateBps` split would
  replace the single `RateBps` field. Defer until CB proto carries the
  distinction.
- **Spec-revision monotonicity and watch-signal fanout** — same Wave 2
  additions as the other registries.

---

## Cross-references

- `pkg/registry/semantics.go`, `pkg/types/ids.go`
- `docs/registry/vnet.md` — composition pattern template
- `docs/registry/nic.md` — NicState references MeterPolicyID
