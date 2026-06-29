// Package route holds RouteGroupRegistry — the typed wrapper over
// registry.Registry that tracks per-route-group state.
//
// RouteGroupState carries a single NextHop (netip.Addr) for Wave 1
// inventory. ECMP / multiple next-hops require a []netip.Addr slice
// and are deferred to Wave 2 when the adapter carries the full ECMP
// table. The field is named NextHop (not NextHops) to make the
// upgrade a clear additive step.
//
// Direct construction from outside pkg/registry/... is forbidden;
// fm-lint NO_REGISTRY_BYPASS (Wave 1.9) enforces this.
//
// Design references:
//   - docs/registry/vnet.md — composition pattern template
//   - pkg/registry/semantics.go — shared contract
package route
