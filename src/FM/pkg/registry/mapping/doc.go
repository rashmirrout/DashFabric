// Package mapping holds MappingRegistry — the typed wrapper over
// registry.Registry that tracks per-VIP-DIP binding state.
//
// MappingState carries the FM-internal projection of a single VIP→DIP
// binding: the virtual IP, the destination/underlay IP, the SNAT flag,
// and binding metadata. Wave 2 adapters translate the CB wire proto
// into MappingState before insertion.
//
// IP addresses are stored as net/netip.Addr — a comparable, zero-copy
// value type with IsValid/IsUnspecified checks — rather than net.IP
// (a byte slice, not comparable, not self-describing on the zero value).
//
// VIP-DIP cardinality: Wave 1 stores one MappingState per MappingID
// (one VIP-DIP pair). A VIP-keyed backend-set index is a Wave 2
// addition once the adapter supplies the full membership list.
//
// Direct construction from outside pkg/registry/... is forbidden;
// fm-lint NO_REGISTRY_BYPASS (Wave 1.9) enforces this.
//
// Design references:
//   - Specs/FM/fm-vip-design.md — VIP-driven backend membership
//   - Specs/me-and-ai/vip-dip-binding-decision.md — why VIP-driven
//   - docs/registry/vnet.md — composition pattern template
//   - pkg/registry/semantics.go — shared contract
package mapping
