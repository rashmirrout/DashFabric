// Package nic holds NicRegistry — the typed wrapper over
// registry.Registry that tracks per-ENI NIC state.
//
// NicRegistry is the second concrete per-object registry built on
// pkg/registry's shared Acquire/Release/Add/Get/Snapshot contract
// (Wave 1.5), following the composition pattern established by
// pkg/registry/vnet (Wave 1.4).
//
// NicState references VnetID and up to six AclGroupIDs — it holds
// these as plain IDs, not as live refcount holders. Cross-registry
// Acquire coupling (NIC acquires on its VNET and shared-group
// registries) is the actor layer's responsibility (Wave 4).
//
// Direct construction from outside pkg/registry/... is forbidden;
// fm-lint NO_REGISTRY_BYPASS (Wave 1.9) enforces this.
//
// Design references:
//   - Specs/FM/registry-go-skeleton.md §2 — NicRegistry skeleton
//   - Specs/me-and-ai/fm-data-model-sync.md §2 — ENI reference model
//   - pkg/registry/semantics.go — shared contract
//   - docs/registry/vnet.md — composition pattern template
package nic
