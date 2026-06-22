# ACT_007 — Actor State Invariant Broken

**Severity:** CRITICAL
**Subsystem:** Actor framework (`pkg/fm/actor`)
**SLO Impact:** Programmed-state correctness suspect. Touching the device may make things worse.

---

## 1. Symptoms

- Alert `fm.actor.invariant_violation_total > 0` (any value is critical).
- Logs: `invariant: <name> violated: expected=<X> actual=<Y> actor=<id>`.
- The actor self-quarantines (designed behavior). State machine reads QUARANTINED_INVARIANT.
- Examples: NIC in READY but content_hash empty; NIC in DELETING with active VIP bindings; HDO in REGISTERED with no DriverSession.

## 2. Likely Causes (ordered)

1. Logic error in a recently changed state-transition function (no transition guard).
2. Out-of-order message delivery from a misbehaving driver or peer FM (look for HA/recovery codes in window).
3. T3 corruption replayed bad state on actor start (correlate with STO_005).
4. Manual operator intervention via debug tools that bypassed the actor's API (`fmctl actor _force_state`).

## 3. Diagnostics (read-only)

```bash
# Current actor state and last 50 transitions
fmctl actor inspect --id=<actor_id> --transitions=50

# Which invariant fired
fmctl logs --actor=<actor_id> --grep="invariant:" --tail=10

# T3 view (compare with in-memory)
fmctl t3 dump --actor=<actor_id>
```

```promql
# Per-invariant counter
sum by (invariant) (fm_actor_invariant_violation_total)
```

## 4. Remediation

**Goal:** Do NOT re-program the device from a broken in-memory state. Either repair from a known-good source or rebuild the actor from scratch.

1. **Leave the actor quarantined.** Self-quarantine already prevented further damage.

2. **Identify last-known-good state.** Source of truth:
   - For NIC: T1 NicSpec + Registry-acquired inputs (re-compose from scratch is allowed).
   - For HDO: device's actual programmed state via `DriverSession.Read()` (read-only).
   - For replicated state (HA scope): peer FM's view.

3. **Decide repair path**:
   - **Rebuild path (preferred):** drop the actor entirely; the supervisor will spawn a fresh one that hydrates from T1/Registry only. Use `fmctl actor purge --id=<actor_id> --confirm`.
     *Rollback:* re-purge is idempotent; the new actor must come up clean within 60 s.
   - **Surgical path:** if rebuild is too disruptive (e.g., would re-program all routes), use `fmctl actor repair --id=<actor_id> --field=<x> --from=<source>` with explicit source proof. Audit log captures the operator and source.
     *Rollback:* if repair makes invariants worse, immediately quarantine again and switch to rebuild.

4. **Verify** the new actor reaches READY (or its terminal-healthy state) within SLO. Watch `fm.actor.invariant_violation_total` for 30 min — must remain 0.

## 5. Rollback

- Purge is NOT reversible — the actor is gone. The rebuild relies on T1/Registry to be correct.
- If T1 itself is suspect, switch to STO_002 or ADP_005 runbook FIRST. Do not rebuild on top of bad inputs.

## 6. Escalate When

- The same invariant fires on > 3 actors within an hour (suggests framework bug or systemic input issue).
- Rebuild fails to reach a healthy state — the underlying input source is itself broken.
- Invariant violation appears immediately after a T3 corruption alert (STO_005) — page storage owner and proceed jointly.

## 7. References

- `threading-model-design.md` §State invariants
- `device-lifecycle-design.md` — legal transitions per actor type
- `error-handling-design.md` ACT_007 entry
- `recovery-and-failover-design.md` §Cold-rebuild path
