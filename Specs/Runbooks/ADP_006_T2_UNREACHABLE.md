# ADP_006 — T2 (fm-cluster-state) Unreachable

**Severity:** CRITICAL
**Subsystem:** Storage adapter (`pkg/fm/storage`)
**SLO Impact:** Cluster state mutations (leader leases, peer view, distributed counters) blocked. HA failovers cannot complete. New device enrollments cannot commit.

---

## 1. Symptoms

- Alert `fm.adapter.t2.error_rate > 50%` for > 60 s.
- Logs: `adapter[t2]: lease lost: <error>`, `adapter[t2]: rpc: unavailable`.
- HA scope state machines stuck in `ELECTION_IN_PROGRESS`.
- Operator writes (enroll device, create change-window) hang or return 503.

## 2. Likely Causes (ordered)

1. T2 cluster genuinely unreachable (network partition, T2 quorum loss).
2. Quorum loss inside T2 itself — minority partition; T2 is up but won't accept writes.
3. Adapter cert rotation failure (same shape as ADP_005).
4. Clock skew on FM pod making lease validation fail (rare but seen post-VM-migration).

## 3. Diagnostics (read-only)

```bash
fmctl probe t2 --timeout=5s
fmctl adapter cert --target=t2
fmctl ha leases --scope=all       # which leases are held, by whom
date -u                             # local clock
kubectl get pods -l app=fm-cluster-state -n <ns>
```

```promql
fm_adapter_request_errors_total{target="t2"} / fm_adapter_request_total{target="t2"}
fm_ha_lease_renewal_failures_total
```

## 4. Remediation

**Goal:** Don't trigger a false HA failover. T2 unavailability is NOT failover input.

1. **Block manual failover triggers** while T2 is unreachable:
   `fmctl ha freeze --reason="ADP_006"` (this prevents operator-initiated failovers).
   *Rollback:* `fmctl ha unfreeze`.

2. **Check quorum.** If T2 reports ≥ ⌈n/2⌉+1 healthy members, it's not quorum loss → likely network or cert. If quorum is lost, only T2 admins can fix it (escalate immediately).

3. **Cert rotation** if Cause #3: `fmctl adapter cert-rotate --target=t2 --force`. Watch for HA_003 in window — a torn cert + lost lease can look like split-brain.

4. **Clock skew** if Cause #4:
   `chronyc tracking` (or `timedatectl status`) — drift > 500 ms is a problem.
   If skewed, do NOT just `ntpdate` and continue — restart the FM pod after time sync; in-flight leases must be re-issued from a clean clock.

5. **Existing programmed state is preserved.** Do NOT restart actors trying to "force" anything; that risks split-brain when T2 returns.

## 5. Rollback

- `fmctl ha unfreeze` should only happen after T2 has been stable for ≥ 5 min AND lease state has been re-acquired by the current leader.
- If unfreeze immediately triggers HA_003 — re-freeze and escalate to HA_003 runbook.

## 6. Escalate When

- T2 quorum lost → page T2 owner immediately. Do not attempt FM-side workarounds.
- HA_003 (split-brain suspected) appears alongside ADP_006 — drop everything else and follow HA_003.
- ADP_005 + ADP_006 simultaneous → cluster-failover runbook (separate doc).

## 7. References

- `adapter-protocol-design.md` §Connection state machine
- `recovery-and-failover-design.md` §HA leases
- `error-handling-design.md` ADP_006 entry
- `Specs/Runbooks/HA_003_SPLIT_BRAIN_SUSPECTED.md`
