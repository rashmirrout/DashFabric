// Package reconcile is FM's drift detection and remediation loop.
//
// The reconciler periodically (and event-driven) compares device state
// (via driver Read) against the goal state composed from registries.
// Differences are classified per the drift taxonomy in
// Specs/FM/reconciliation-design.md and remediated according to the
// taxonomy class — or, when classification is ambiguous, parked at
// RECONCILE_HOLD (REC_005) for operator review.
//
// Drift classes:
//
//   - TRANSIENT      — device catching up; retry with SLO clock.
//   - STALE_FM       — FM's observed state is stale; refresh from driver.
//   - STALE_DEVICE   — device's actual state lags goal; reprogram.
//   - PEER_DRIFT     — HA peer programmed a conflicting change.
//   - UNKNOWN_DEVICE — device state shape not in taxonomy (REC_005).
//   - OBJECT_DRIFT   — single-object divergence.
//   - TOTAL_DRIFT    — wide divergence; consider full re-program.
//
// Subpackages:
//
//   - classify — taxonomy implementation; the only place drift classes are decided.
//
// Top-level files:
//
//   - loop.go  — main reconciler loop.
//   - retry.go — SLO clocks, retry budgets, REC_008 escalation.
//
// Design references:
//   - Specs/FM/reconciliation-design.md
//   - Specs/Runbooks/REC_005_DRIFT_UNKNOWN.md
//   - Specs/Runbooks/REC_008_PERSISTENT_DRIFT.md
package reconcile
