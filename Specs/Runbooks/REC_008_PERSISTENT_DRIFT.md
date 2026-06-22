# REC_008 — Persistent Drift Past SLO

**Severity:** CRITICAL
**Subsystem:** Reconciler (`pkg/fm/reconcile`)
**SLO Impact:** A drift has been classified, retry budget exhausted, and SLO clock expired. Device state does not match intent. Customer-visible behavior may be wrong.

---

## 1. Symptoms

- Alert `fm.reconcile.persistent_drift_total > 0`.
- Logs: `reconciler: drift persists past SLO; object=<id>; classification=<class>; attempts=<n>`.
- The object is parked at `RECONCILE_FAILED`.
- Most often classification is `STALE_DEVICE` (device refuses to converge to goal) or `PEER_DRIFT` (HA peer's view conflicts and won't yield).

## 2. Likely Causes (ordered)

1. Device-side resource exhaustion that the driver classifies as transient (so we keep retrying) but really isn't (e.g., TCAM full).
2. A peer FM holding a conflicting goal due to stale lease / clock skew (correlate with HA_003 or ADP_006).
3. Driver bug — `Apply()` returns success but the state never converges (look for DRV warnings in window).
4. Operator side-channel reverts (someone keeps fixing the device manually, FM keeps overwriting).

## 3. Diagnostics (read-only)

```bash
# Object state + retry history
fmctl reconcile inspect --object=<id> --history

# Driver attempt timing
fmctl driver attempts --object=<id> --tail=20

# Resource counters on the device
fmctl driver call <device_id> read.resource_stats

# Peer view (for PEER_DRIFT case)
fmctl ha peer-view --object=<id>
```

```promql
fm_reconcile_persistent_drift_total
sum by (classification) (fm_reconcile_retry_total{object="<id>"})
```

## 4. Remediation

**Goal:** Stop retrying blindly. Pick a path from the four below based on classification.

### Path A — STALE_DEVICE due to resource exhaustion
1. Confirm via `read.resource_stats` (TCAM utilization, route table full, etc.).
2. Migration is needed: move some ENIs to another device.
   `fmctl nic migrate --eni=<id> --to-device=<other>`
3. Once resource pressure relieves, the original object reconciles naturally.

### Path B — PEER_DRIFT
1. **Do NOT just force overwrite.** That's how you get split-brain.
2. Compare lease validity: `fmctl ha leases --object=<id>`. The leaseholder wins.
3. If no clear leaseholder → escalate to HA_003.
4. If clear leaseholder: the non-leaseholder backs off via `fmctl reconcile defer --object=<id> --to-peer=<x>`.

### Path C — Driver bug (Apply returns success, no convergence)
1. Capture: `fmctl driver attempts --object=<id> --tail=20 > /tmp/rec008-drv.log`.
2. Quarantine the object: `fmctl reconcile quarantine --object=<id> --reason="REC_008 path C"`.
3. File driver bug with /tmp/rec008-drv.log.
4. Workaround: route the object through an alternative driver if available (rare).

### Path D — Operator side-channel reverts
1. Audit log will show: `read.observed` flipping between two values.
2. This is a process problem, not a tech problem. Coordinate with the operator team to stop the side-channel mutation.
3. Until then: `fmctl reconcile quarantine --object=<id> --reason="REC_008 path D - operator side-channel"`.

## 5. Rollback

- Migration (Path A) is reversible via `fmctl nic migrate --back`.
- Defer (Path B) is reversible via `fmctl reconcile claim --object=<id>` if leaseholder changes.
- Quarantine (Path C/D) is reversible via `fmctl reconcile unquarantine --object=<id>`.

## 6. Escalate When

- Multiple unrelated objects hit REC_008 within 1 hour → systemic issue; page reconciler + driver + HA owners.
- Path B with no clear leaseholder → switch to HA_003 runbook immediately.
- Path A migration fails because no target device has capacity → capacity-planning escalation, customer notification.

## 7. References

- `reconciliation-design.md` §SLO clocks, retry budgets
- `recovery-and-failover-design.md` §Lease ownership
- `error-handling-design.md` REC_008 entry
- `Specs/Runbooks/HA_003_SPLIT_BRAIN_SUSPECTED.md`
- `Specs/Runbooks/DRV_008_PERMANENT_FAILURE.md`
