// Package acl holds AclGroupRegistry — the typed wrapper over
// registry.Registry that tracks per-ACL-group state.
//
// AclGroupState is a Wave 1 placeholder: it carries the group ID,
// owning VNET, and a rule count (int). The full rule-object list
// arrives in Wave 2 when the adapter supplies per-rule protos.
// The field is named RuleCount (not Rules) so a future
// []Rule slice field can be added without a rename.
//
// Direct construction from outside pkg/registry/... is forbidden;
// fm-lint NO_REGISTRY_BYPASS (Wave 1.9) enforces this.
//
// Design references:
//   - docs/registry/vnet.md — composition pattern template
//   - pkg/registry/semantics.go — shared contract
package acl
