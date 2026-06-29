// Package vnet holds VnetRegistry — the typed wrapper over
// registry.Registry that tracks per-VNET state.
//
// VnetRegistry is the first concrete per-object registry built on
// pkg/registry's shared Acquire/Release/Add/Get/Snapshot contract
// (Wave 1.3). Subsequent slices (nic, mapping, acl, route, meter)
// follow the same composition pattern: a small type wrapping
// registry.Registry[<TypedID>, *<TypedState>] with defensive copies
// on the slice-bearing fields.
//
// Direct construction from outside pkg/registry/... is forbidden;
// fm-lint NO_REGISTRY_BYPASS (Wave 1.9) enforces this.
//
// Design references:
//   - Specs/FM/registry-go-skeleton.md §1 — VnetRegistry skeleton
//   - Specs/me-and-ai/fm-data-model-sync.md §2 — VNET reference model
//   - pkg/registry/semantics.go — shared contract
package vnet
