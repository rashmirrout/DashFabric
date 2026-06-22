# ACT_006 — Actor in Repeated Panic Loop

**Severity:** CRITICAL
**Subsystem:** Actor framework (`pkg/fm/actor`)
**SLO Impact:** One or more devices/ENIs are not making forward progress; potential for cascading supervision restarts to exhaust the pool.

---

## 1. Symptoms

- Alert `fm.actor.restart_total{actor=<id>} > 5 within 5 min`.
- Logs: repeated `actor: panic recovered: <stack>` for the same actor id.
- Per-device state stuck (e.g., NIC stuck in COMPOSING, device stuck in READY but not progressing).
- Pool saturation alerts on the parent pool (P1/P2/P3) if many actors panic at once.

## 2. Likely Causes (ordered)

1. Recent code regression — new deploy introduced a nil-deref or impossible-case panic.
2. Corrupt input from CB hitting an unhandled branch (e.g., NicSpec with field-9 missing after schema bump).
3. Race with registry teardown — actor still reading after Release happened (look for REG_007 in same window).
4. Driver returning a value the actor's switch statement doesn't cover (look for DRV codes in same window).

## 3. Diagnostics (read-only)

```bash
# Recent restarts for this actor
fmctl actor inspect --id=<actor_id> --history=20

# Last 5 panic stacks
fmctl logs --actor=<actor_id> --grep="panic recovered" --tail=5

# Correlate: what changed in the last 2 hours
git log --since="2 hours ago" --oneline -- pkg/fm/actor/
fmctl deploy history --tail=5
```

```promql
# Restart rate
rate(fm_actor_restart_total{actor="<id>"}[5m])

# Are restarts clustered in time? (regression vs flaky input)
histogram_quantile(0.95,
  rate(fm_actor_restart_interval_seconds_bucket{actor="<id>"}[15m]))
```

## 4. Remediation

**Goal:** Stop the panic loop. Preserve programmed state. Identify root cause.

1. **Quarantine the actor** to stop the restart loop and free supervision slots:
   `fmctl actor quarantine --id=<actor_id> --reason="ACT_006 runbook"`
   This freezes the actor — children remain in last-known state, but the actor stops accepting new work.
   *Rollback:* `fmctl actor unquarantine --id=<actor_id>` (only after fix verified).

2. **Capture panic stacks** for the postmortem before they roll off:
   `fmctl logs --actor=<actor_id> --grep="panic recovered" --tail=20 > /tmp/act006-<actor>-<ts>.log`

3. **Decide regression vs data**:
   - If panic stack points to recently-deployed code AND restarts started right after deploy → it's a deploy regression. **Roll back the deploy** (`kubectl rollout undo deployment/fm`). Re-quarantine if needed.
   - If the stack points to a specific input id (ENI / device / vnet) AND only this actor is affected → it's a poison input. Ask CB to quarantine the upstream object via tenant-scoped policy, then unquarantine the actor.

4. **Verify** after fix: unquarantine and watch `fm.actor.restart_total{actor=<id>}` for 10 min. If 0 → resolved.

## 5. Rollback

- Unquarantine is safe; if the panic returns, re-quarantine immediately — do NOT thrash.
- Deploy rollback is the safe default for any regression-shaped pattern (clustered restarts ≤ 30 min after deploy).

## 6. Escalate When

- > 10 distinct actors hit ACT_006 in the same 10-minute window → likely a framework-level bug, page actor-framework owner.
- The same actor returns to ACT_006 within 30 min of unquarantine.
- Panic stack contains pointer arithmetic / unsafe.Pointer / cgo frames — needs core dev triage.

## 7. References

- `threading-model-design.md` §11 — panic recovery and supervision
- `error-handling-design.md` ACT_006 entry
- `recovery-and-failover-design.md` §Actor-level recovery
