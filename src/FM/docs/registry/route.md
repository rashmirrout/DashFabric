# pkg/registry/route — RouteGroupRegistry

**Wave / Slice:** 1 / 1.7b  
**Status:** 🟢 Landed 2026-06-23  
**Package:** `github.com/dashfabric/fm/pkg/registry/route`

---

## What this is

Typed wrapper over `registry.Registry[types.RouteGroupID, *RouteGroupState]`.
Wave 1 carries a single `NextHop` address. ECMP (multiple next-hops) is
deferred to Wave 2 when the adapter carries the full ECMP table.

---

## RouteGroupState schema

```go
type RouteGroupState struct {
    RouteGroupID types.RouteGroupID  // CB-assigned; validated at Add. Required.
    VnetID       types.VnetID        // owning VNET; validated at Add. Required.
    NextHop      netip.Addr          // valid + non-unspecified. Required.
    SpecRevision types.SpecRevision
    LastUpdated  time.Time
}
```

`NextHop` uses `netip.Addr` for the same reasons as `MappingState.VIP`
— comparable, zero-copy, self-describing zero value. See
`docs/registry/mapping.md §Why netip.Addr`.

---

## Validation invariants

- `RouteGroupID` passes `types.ValidateID`.
- `VnetID` passes `types.ValidateID`.
- `NextHop.IsValid()` — rejects zero value.
- `!NextHop.IsUnspecified()` — rejects `0.0.0.0` and `::`.
- IPv4 and IPv6 addresses both accepted.

---

## Future Scopes

- **ECMP / multiple next-hops (Wave 2).** Replace `NextHop netip.Addr`
  with `NextHops []netip.Addr` (sorted + deduped, following the
  `PeerVnetIDs` pattern from `VnetRegistry`). The field rename is the
  only breaking change; the validation and Clone updates are
  mechanical.
- **Prefix-route support.** A `Prefix netip.Prefix` field for
  longest-prefix-match routing would pair with `NextHop`. Deferred
  until the DASH route-group spec pins the prefix semantics.
- **Spec-revision monotonicity and watch-signal fanout** — same Wave 2
  additions as the other registries.

---

## Cross-references

- `pkg/registry/semantics.go`, `pkg/types/ids.go`
- `docs/registry/mapping.md` — netip.Addr rationale
- `docs/registry/vnet.md` — composition pattern template
- `docs/registry/nic.md` — NicState references RouteGroupID
