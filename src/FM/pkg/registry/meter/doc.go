// Package meter holds MeterPolicyRegistry — the typed wrapper over
// registry.Registry that tracks per-meter-policy state.
//
// MeterPolicyState carries RateBps (required, >0) and BurstBps
// (optional; 0 means "same as RateBps" per the dataplane driver
// convention). The Wave 2 adapter will translate CB proto meter
// fields into these values before insertion.
//
// Direct construction from outside pkg/registry/... is forbidden;
// fm-lint NO_REGISTRY_BYPASS (Wave 1.9) enforces this.
//
// Design references:
//   - docs/registry/vnet.md — composition pattern template
//   - pkg/registry/semantics.go — shared contract
package meter
