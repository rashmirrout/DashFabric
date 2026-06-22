# HA_003 — Split-Brain Suspected

**Severity:** CRITICAL — highest blast radius in this set
**Subsystem:** HA / failover
**SLO Impact:** Two FM pods may both believe they own programming authority for the same scope. ANY further programming risks irreconcilable divergence. **Stop the world for the affected scope until resolved.**

---

## 1. Symptoms

- Alert `fm.ha.split_brain_suspected_total > 0` (any value is critical, page-on-fire).
- Logs from two pods within the same HA scope each report: `ha: holding programming lease scope=<id>`.
- Drivers report conflicting Apply() calls within seconds of each other.
- Reconciler is firing REC_005 / REC_008 with classification `PEER_DRIFT` on many objects.

## 2. Likely Causes (ordered)

1. T2 partition — neither pod could see the other; both believed peer was dead and took the lease.
2. Lease TTL too short relative to GC pauses or VM migration freezes — one pod paused, second pod legitimately took the lease, first pod un-paused not realizing it lost.
3. Clock skew on one pod making lease validation appear valid when it isn't.
4. Misconfigured HA scope — two pods accidentally assigned the same scope id.

## 3. Diagnostics (read-only)

```bash
# Per-pod view of the lease
kubectl exec <pod-A> -- fmctl ha leases --scope=<id>
kubectl exec <pod-B> -- fmctl ha leases --scope=<id>

# T2 view of the lease (the truth)
fmctl probe t2 --lease=<scope_id>

# Clock skew check on both pods
kubectl exec <pod-A> -- date -u
kubectl exec <pod-B> -- date -u

# Recent driver Apply() calls per pod
fmctl driver apply-log --pod=<pod-A> --scope=<scope_id> --tail=20
fmctl driver apply-log --pod=<pod-B> --scope=<scope_id> --tail=20
```

```promql
fm_ha_split_brain_suspected_total
# Count of distinct pods believing they hold the same lease
count by (scope_id) (fm_ha_lease_held{scope_id="<id>"})
```

## 4. Remediation

**Goal:** Freeze, determine truth, force one pod to yield, reconcile divergence. Never let both keep writing.

### Step 1 — STOP THE WORLD (within 60 s of alert)

Freeze programming for the scope across BOTH pods:
```bash
fmctl ha freeze --scope=<id> --reason="HA_003 runbook"
```
This sets a T2 flag both pods respect. Neither pod will issue further Apply() calls.
*Rollback:* `fmctl ha unfreeze --scope=<id>` — only after step 3 succeeds.

### Step 2 — Determine truth from T2

T2's lease record is the only source of truth. Whichever pod matches the T2 lease holder + valid TTL is the rightful owner.

```bash
fmctl probe t2 --lease=<scope_id> --verbose
```

Possible outcomes:
- **One pod matches T2, other doesn't.** Truth = matching pod.
- **Neither matches** (both have stale views): force a fresh election. `fmctl ha election --scope=<id> --force`.
- **Both claim valid match** (impossible unless T2 itself is split): ESCALATE to T2 owner immediately. Do NOT continue without T2 clarity.

### Step 3 — Force the false owner to yield

```bash
kubectl exec <false-owner-pod> -- fmctl ha yield --scope=<id> --reason="HA_003 runbook"
```
The false owner quarantines its actors for the scope (does NOT delete state). Programmed state on devices is unchanged.

### Step 4 — Reconcile divergence

The rightful owner now runs reconcile:
```bash
kubectl exec <true-owner-pod> -- fmctl reconcile run --scope=<id> --force-full
```
Any objects that diverged during the split-brain window will be classified (REC_005) and remediated per the REC_005 runbook.

### Step 5 — Unfreeze

```bash
fmctl ha unfreeze --scope=<id>
```
Watch HA_003 metric for 30 min. Must remain 0.

### Step 6 — Postmortem inputs

Capture for postmortem (REQUIRED, not optional for HA_003):
- T2 lease history for the scope
- Both pods' apply-log during the window
- Audit log of the freeze/yield/unfreeze commands

## 5. Rollback

- Freeze is fully reversible.
- Yield is reversible (`fmctl ha unyield`), but only do this if you got Step 2 wrong AND truth has flipped — almost never.
- Forced election is irreversible; once a new leaseholder is set, that's the new truth.

## 6. Escalate When

- T2 itself is split (Step 2 ambiguous) — page T2 owner + architecture, **immediately**.
- Clock skew detected — coordinate with infra; do not unfreeze until both pods are time-synced and have re-issued leases from a clean clock.
- HA_003 re-fires within 1 hour of remediation — the underlying lease/quorum problem is not fixed. Keep frozen, escalate.

## 7. Hard Rules

- NEVER unfreeze without completing Step 4.
- NEVER attempt to merge two divergent states manually by editing T1/T2 directly. The reconciler is the only sanctioned merge path.
- NEVER use `fmctl ha _force_lease` (debug-only, audit-logged, almost always wrong here).

## 8. References

- `recovery-and-failover-design.md` §Split-brain prevention and recovery
- `reconciliation-design.md` §PEER_DRIFT
- `security-design.md` §10 — incident response hooks
- `error-handling-design.md` HA_003 entry
- `Specs/Runbooks/REC_005_DRIFT_UNKNOWN.md`
- `Specs/Runbooks/ADP_006_T2_UNREACHABLE.md`
