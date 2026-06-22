// Package ha implements FM's high-availability primitives: leases,
// leader election, and the split-brain freeze protocol (HA_003).
//
// Subpackages:
//
//   - lease    — Lease acquire/renew over T2 etcd. The lease record in
//                T2 is the only source of truth for "who owns this
//                HaScope right now."
//   - election — Leader election for HA-scoped resources.
//   - freeze   — The HA_003 stop-the-world primitives:
//                Freeze / Yield / Unfreeze. Used to halt programming on
//                both pods of a suspected split-brain scope while truth
//                is determined from T2.
//
// Top-level files:
//
//   - scope.go — HaScope semantics: which objects are owned by which scope.
//
// Hard rule (from HA_003 runbook): never manually edit T1/T2 lease records;
// never use fmctl ha _force_lease; never unfreeze before reconciliation completes.
//
// Design references:
//   - Specs/FM/recovery-and-failover-design.md
//   - Specs/FM/fm-failover-sequence-design.md
//   - Specs/Runbooks/HA_003_SPLIT_BRAIN_SUSPECTED.md
package ha
