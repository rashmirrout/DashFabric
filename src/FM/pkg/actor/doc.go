// Package actor holds FM's execution units. Each actor owns a piece of
// state, processes messages from a bounded mailbox, and is supervised
// for restart/quarantine on panic (ACT_006) or invariant break (ACT_007).
//
// Subpackages:
//
//   - hdo  — Host Device Object: one per device. Owns the driver session
//            handle (DriverSession), heartbeats, and the per-device
//            DeviceIOState (see Specs/FM/inc-closure-2.2-2.3.md §2).
//            Does NOT cache configuration — it Acquires from registries.
//   - co   — Container Object: groups multiple NicObjects belonging to
//            the same container/pod (tier=Container).
//   - no   — NIC Object: per-ENI compose-and-program loop. Builds
//            NicGoalState from registries, computes DeltaPlan, hands to
//            the driver via its HDO.
//
// The supervisor (supervisor.go) implements the spawn/restart/quarantine
// policy. The mailbox (mailbox.go) is bounded; overflow degrades to the
// fallback path defined in Specs/FM/threading-model-design.md.
//
// Design references:
//   - Specs/FM/threading-model-design.md
//   - Specs/FM/device-lifecycle-design.md
//   - Specs/FM/inc-closure-2.2-2.3.md §2 (HDO slim model)
//   - Specs/FM/nicgoalstate-schema-design.md
//   - Specs/Runbooks/ACT_006_REPEATED_PANIC.md
//   - Specs/Runbooks/ACT_007_STATE_INVARIANT_BROKEN.md
package actor
