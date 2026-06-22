# REC_005 — Reconciliation Drift of Unknown Class

**Severity:** CRITICAL
**Subsystem:** Reconciler (`pkg/fm/reconcile`)
**SLO Impact:** Drift exists between FM's goal and the device's actual state, AND the reconciler cannot classify it against the known taxonomy. Auto-remediation is suspended for the object.

---

## 1. Symptoms

- Alert `fm.reconcile.drift_unknown_total > 0`.
- Logs: `reconciler: drift unclassified; device_state=<...>; goal=<...>` for a specific object.
- The object is parked at `RECONCILE_HOLD` — FM neither overwrites nor accepts the device state.
- Often co-fires with `ACT_007` if the unknown drift implies a broken invariant.

## 2. Likely Causes (ordered)

1. New driver behavior (firmware upgrade introduced a state shape FM doesn't recognize).
2. Operator side-channel mutation — someone configured the device directly (CLI, vendor portal).
3. A peer FM (in HA scope) programmed a change FM-local didn't yet see (race; usually transient).
4. Bug in the drift classifier — recently changed `reconciliation-design.md` taxonomy without updating code.

## 3. Diagnostics (read-only)

```bash
# Pull both views for the offending object
fmctl reconcile inspect --object=<id>
# Returns: { goal: <proto>, observed: <proto>, diff: <hunks> }

# Recent classifier decisions
fmctl reconcile log --object=<id> --tail=50

# Driver version & firmware
fmctl driver version --device=<device_id>
```

```promql
sum by (object_type) (fm_reconcile_drift_unknown_total)
```

## 4. Remediation

**Goal:** Decide whether to *adopt* the device state (it's correct, FM was wrong), *enforce* the goal (FM is right, device is wrong), or *quarantine* (cannot decide safely).

1. **Manual classification by operator** is the safe default for this code.
   Look at the diff hunks from step 3. Decide:
   - Fields under FM's authority (e.g., ACL rules) → enforce.
   - Fields under device's authority (e.g., counters, hardware-derived ids) → adopt.
   - If unsure → quarantine and escalate.

2. **Adopt** (treat device-state as new truth, update goal to match):
   `fmctl reconcile adopt --object=<id> --justification="<ticket>"`
   Audit-logged. *Rollback:* `fmctl reconcile re-enforce --object=<id> --from-revision=<prior>`.

3. **Enforce** (overwrite device with goal):
   `fmctl reconcile enforce --object=<id> --justification="<ticket>"`
   Audit-logged. The next DeltaPlan will reprogram the diff hunks.
   *Rollback:* `fmctl reconcile adopt --object=<id>` if enforce was wrong.

4. **Quarantine** (no autoremediation, leave for human review):
   `fmctl reconcile quarantine --object=<id> --reason="STO_005 runbook step 4"`

5. **If Cause #1 (firmware upgrade)**: file a driver-mapping update to extend the taxonomy. Do not adopt blindly — the new state shape may be a vendor bug.

## 5. Rollback

- Adopt/enforce/quarantine are all reversible by the inverse command. Each writes an audit log entry. Do not script around them.

## 6. Escalate When

- > 5 unknown-class drifts on the same object_type within 1 hour → likely classifier bug or firmware regression; page reconciler owner.
- The diff contains fields not present in any known schema → driver/firmware mismatch; page driver team.
- Adoption was used and the device immediately drifts again — there's an external mutator; coordinate with security.

## 7. References

- `reconciliation-design.md` §Drift taxonomy (TRANSIENT, STALE_FM, STALE_DEVICE, UNKNOWN_DEVICE, OBJECT_DRIFT, TOTAL_DRIFT, PEER_DRIFT)
- `error-handling-design.md` REC_005 entry
- `security-design.md` §7 — audit log captures adopt/enforce decisions
