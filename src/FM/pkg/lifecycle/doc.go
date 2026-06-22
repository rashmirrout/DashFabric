// Package lifecycle holds the device and ENI state-machine definitions.
//
// Device lifecycle states (per Specs/FM/device-lifecycle-design.md):
//
//   - Discovered  — known to CB, not yet contacted.
//   - Connecting  — driver session being established.
//   - Ready       — heartbeating, eligible for programming.
//   - Degraded    — partial failure; reduced capacity.
//   - Quarantined — repeated faults (ACT_006 / DRV_008); not programmed.
//   - Decommissioning — graceful drain in progress.
//   - Deleted     — removed from FM's view.
//
// ENI lifecycle states (per Specs/FM/vm-eni-provisioning-design.md):
//
//   - Pending  — NicSpec known, not yet bound to device.
//   - Bound    — assigned to a device, programming in flight.
//   - Active   — programmed, observed steady-state.
//   - Quarantined — DRV_008 per-ENI quarantine (does not affect device).
//   - Releasing — graceful teardown.
//
// Transitions are explicit; invalid transitions trip ACT_007.
//
// Design references:
//   - Specs/FM/device-lifecycle-design.md
//   - Specs/FM/vm-eni-provisioning-design.md
//   - Specs/FM/fm-pod-lifecycle-design.md
//   - Specs/Runbooks/ACT_007_STATE_INVARIANT_BROKEN.md
package lifecycle
