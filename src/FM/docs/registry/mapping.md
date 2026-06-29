# pkg/registry/mapping — MappingRegistry

**Wave / Slice:** 1 / 1.6  
**Status:** 🟢 Landed 2026-06-23  
**Package:** `github.com/dashfabric/fm/pkg/registry/mapping`

---

## What this is

The third concrete per-object registry, following the composition
pattern from `pkg/registry/vnet` and `pkg/registry/nic`. `MappingRegistry`
tracks VIP→DIP bindings — FM's internal projection of the SNAT/DNAT
table that adapters program into the dataplane. Because `MappingState`
has no slice fields, `Clone` is a plain struct copy with no heap
allocation beyond the single pointer.

---

## MappingState schema

```go
type MappingState struct {
    MappingID    types.MappingID    // CB-assigned; validated at Add. Required.
    VnetID       types.VnetID       // owning VNET; validated at Add. Required.
    VIP          netip.Addr         // Virtual IP; valid + non-unspecified. Required.
    DIP          netip.Addr         // Destination/Underlay IP; valid + non-unspecified. Required.
    SNAT         bool               // source-NAT active for this binding.
    BindingTime  time.Time          // wall-clock when binding was established.
    SpecRevision types.SpecRevision // monotonic; use for stale-snapshot detection.
    LastUpdated  time.Time          // wall-clock from producer; readability only.
}
```

---

## Why `netip.Addr` over `net.IP`

| Property | `net.IP` (byte slice) | `netip.Addr` (value type) |
|---|---|---|
| Comparable (`==`) | No | Yes |
| Map key | No | Yes |
| Zero value | `nil` (ambiguous) | `netip.Addr{}`, IsValid()==false |
| Unspecified check | Manual `bytes.Equal` | `addr.IsUnspecified()` |
| Heap allocation | Always | Never (fits in a register pair) |

`netip.Addr` lets the validator express the zero/unspecified contract
in two calls (`IsValid()`, `IsUnspecified()`) without any helper code,
and makes `MappingState` fully comparable for future equality checks
without implementing `Equal()`.

---

## IP validation invariants

Both `VIP` and `DIP` must satisfy:

1. `addr.IsValid()` — rejects the zero value (`netip.Addr{}`).
2. `!addr.IsUnspecified()` — rejects `0.0.0.0` and `::`.

IPv4 and IPv6 addresses are both accepted. IPv4-mapped IPv6 addresses
(`::ffff:1.2.3.4`) are also accepted; the Wave 2 adapter is responsible
for normalising address families before insertion if CB emits mixed
representations.

---

## VIP-DIP cardinality

Wave 1 stores **one `MappingState` per `MappingID`** — one VIP-DIP
pair. A VIP may appear as the `VIP` field of multiple `MappingState`
entries (one per backend DIP), but there is no VIP-keyed secondary
index in Wave 1. See Future Scopes.

This design is consistent with `Specs/FM/fm-vip-design.md`: the
VIP-driven backend membership list is assembled by the adapter (Wave 2)
and the actor (Wave 4) by scanning or subscribing to all mappings for a
given VnetID+VIP pair.

---

## API

```go
r := mapping.New()

// Producer:
err := r.Add(&mapping.MappingState{
    MappingID: "map-42",
    VnetID:    "vnet-7",
    VIP:       netip.MustParseAddr("10.0.0.1"),
    DIP:       netip.MustParseAddr("192.168.1.10"),
    SNAT:      true,
    BindingTime: time.Now(),
})

// Consumer:
ready := r.Acquire(ctx, "map-42")
defer r.Release("map-42")

select {
case <-ready:
    m, _ := r.Get("map-42")
    use(m)
case <-ctx.Done():
    return ctx.Err()
}
```

---

## Future Scopes

- **VIP-keyed backend-set index (Wave 2).** A secondary
  `map[netip.Addr][]types.MappingID` inside `MappingRegistry` would let
  adapters look up all DIPs for a given VIP in O(1) instead of scanning
  the full snapshot. Defer until the adapter's hot path shows this is
  needed.
- **VIP uniqueness enforcement per VNET.** The current contract allows
  multiple `MappingID` entries with the same `VIP` under the same
  `VnetID`. If the data model mandates uniqueness (one active binding
  per VIP per VNET), a `map[vnetVIPKey]types.MappingID` secondary index
  can enforce it at `Add` time. Defer until the spec mandates it.
- **Spec-revision monotonicity (Wave 2).** `Add` will reject writes with
  `SpecRevision ≤` stored revision, matching VnetRegistry behaviour.
- **Watch-signal fanout (Wave 2).** Same `Subscription[*MappingState]`
  pattern as described in `docs/registry/vnet.md`.
- **IPv4-mapped normalisation.** `::ffff:1.2.3.4` and `1.2.3.4` are
  semantically equal but compare unequal as `netip.Addr`. Wave 2
  adapter should normalise to the canonical form (pure IPv4 where
  possible) before inserting; the registry stores as-received.

---

## Cross-references

- `pkg/registry/semantics.go` — generic `Registry[K, V]` contract.
- `pkg/registry/refcount.go` — `UnderflowError`, `ErrRefcountUnderflow`.
- `pkg/types/ids.go` — `MappingID`, `VnetID`, `ValidateID`.
- `pkg/types/versions.go` — `SpecRevision`.
- `docs/registry/semantics.md` — lifecycle model, REG_007 runbook.
- `docs/registry/vnet.md` — composition pattern template.
- `Specs/FM/fm-vip-design.md` — VIP-driven backend membership.
- `Specs/me-and-ai/vip-dip-binding-decision.md` — why VIP-driven.
