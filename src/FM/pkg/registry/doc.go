// Package registry holds FM's typed in-memory registries — the
// authoritative T3 (observed state) layer.
//
// All registries follow the Acquire/Release/Ready semantics defined in
// Specs/FM/registry-semantics-exact.md. Shared objects (ACL groups,
// route groups, meter policies, mappings) use refcounting so they can
// be reclaimed only after the last referent has Released them. A
// refcount underflow trips REG_007 (see Specs/Runbooks/).
//
// Subpackages:
//
//   - vnet     — VnetRegistry: top-level umbrella per the DASH VNET model.
//   - nic      — NicRegistry: per-ENI state; HDO actors hold the Acquire().
//   - mapping  — MappingManager: VIP↔PA index, sharded per-VNET.
//   - acl      — ACL group registry (refcounted, shared across ENIs).
//   - route    — Route group registry (refcounted, shared across ENIs).
//   - meter    — Meter policy registry (refcounted, may be tenant-global).
//
// Direct construction of registry types from outside this tree is
// forbidden — the fm-lint NO_REGISTRY_BYPASS rule (tools/fm-lint/)
// enforces this.
//
// Design references:
//   - Specs/FM/registry-go-skeleton.md
//   - Specs/FM/registry-pattern-design.md
//   - Specs/FM/registry-semantics-exact.md
//   - Specs/Runbooks/REG_007_REFCOUNT_UNDERFLOW.md
//   - Specs/me-and-ai/fm-data-model-sync.md
package registry
