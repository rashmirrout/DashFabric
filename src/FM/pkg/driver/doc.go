// Package driver defines FM's southbound contract — the boundary
// between the FM control plane and the per-device programming agent.
//
// A Driver receives a DeltaPlan (set of typed Apply ops) and a goal
// state, executes against the device, and returns a DriverResult. FM is
// vendor-neutral: vendor specifics live behind this interface and never
// leak into actors or registries.
//
// Subpackages:
//
//   - iface  — The Driver interface, error codes (DRV_*), and the
//              retry policy that callers (HDO actors) follow.
//   - delta  — DeltaPlan: typed list of ops (UpsertNicSpec,
//              UpsertAclGroup, BindAclSlot, etc.). Computed from
//              (current observed) → (goal). Idempotent ops only.
//   - stub   — In-process driver for unit and integration tests.
//   - grpc   — gRPC driver client for production deployments.
//
// Design references:
//   - Specs/FM/southbound-driver-interface-redesign.md
//   - Specs/FM/orchestrator-plugin-interface.md
//   - Specs/Runbooks/DRV_008_PERMANENT_FAILURE.md
package driver
