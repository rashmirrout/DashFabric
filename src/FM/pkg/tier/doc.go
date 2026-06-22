// Package tier specializes the FM binary for the four deployment tiers:
//
//   - DV          — Development. Single-pod, in-memory T1/T2, stub driver.
//   - VM          — Single-tenant VM appliance. File-backed T2, real driver.
//   - Container   — Multi-NIC container appliance. CO actors enabled.
//   - Hyperscale  — Multi-pod HA, real T1/T2 etcd cluster, full feature set.
//                   Default target for the codebase.
//
// One binary serves all four; this package provides the capability
// matrix and the boot-time gates that disable features per tier (e.g.,
// HA election is no-op on DV).
//
// Design rule: features are designed for Hyperscale first, then
// degraded for smaller tiers. The reverse (designing for DV then
// scaling up) leaves blind spots.
//
// Design references:
//   - Specs/FM/deployment-tiers.md
//   - Specs/FM/fm-pod-lifecycle-design.md
package tier
