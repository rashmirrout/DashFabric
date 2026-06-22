# REG_007 — Refcount Underflow in Registry

**Severity:** CRITICAL
**Subsystem:** Registry (`pkg/fm/registry`)
**SLO Impact:** Risk of premature T1 watch closure → stale reads across actors

---

## 1. Symptoms

- Alert `fm.registry.refcount_underflow_total > 0` (any value is critical).
- Logs: `panic: refcount underflow on key=<key>` from `registry.Release()`.
- Cascade: actors blocked on `Acquire().WaitReady()` because the underlying watch was torn down.
- Dashboard `Registry — Acquire/Release Imbalance` shows release rate > acquire rate sustained.

## 2. Likely Causes (ordered)

1. Double-`Release()` from a recently changed actor lifecycle path (most common).
2. Missing `defer Release()` paired with an early `return` on an error branch.
3. Test fixture leaking into prod build (look for `t.Cleanup` references in non-test paths).
4. Manual operator intervention (`fmctl registry release-force`) without matching prior `acquire-force`.

## 3. Diagnostics (read-only)

```bash
# Which key underflowed
fmctl registry inspect --underflow-since=15m

# Recent acquire/release events for that key
fmctl registry events --key=<key> --tail=200

# Which actor instance issued the bad release
fmctl actor where --acquired=<key>
```

```promql
# Per-key imbalance over last 1h
sum by (key) (rate(fm_registry_release_total[1h])) -
sum by (key) (rate(fm_registry_acquire_total[1h]))
```

## 4. Remediation

**Goal:** Stop the bleed, then restore correct refcount without corrupting other consumers.

1. **Freeze writes to the key** to prevent further damage:
   `fmctl registry freeze --key=<key> --duration=10m`
   *Rollback:* `fmctl registry unfreeze --key=<key>`

2. **Force refcount resync from live consumers** (NOT from logs):
   `fmctl registry recount --key=<key>`
   This walks all actors that currently hold an Acquire token and rebuilds the counter.
   *Rollback:* none needed — operation is idempotent; re-run is safe.

3. **Restart the affected actor pool** if the offending actor is still alive:
   `fmctl actor restart --pool=P1 --device=<device_id>`
   *Rollback:* actor restart is automatic on crash; no manual rollback.

4. **Unfreeze** once `fmctl registry inspect --key=<key>` shows the counter stable for 60 s.

## 5. Rollback (if remediation worsens state)

- `fmctl registry recount` is read-then-write; if its log shows "wrote refcount X, expected Y", do NOT re-run blindly — escalate.
- Do NOT manually edit the refcount value via T2 backdoor; this guarantees split-brain.

## 6. Escalate When

- Recount disagrees with consumer-count by > 1 (suggests in-flight Acquire/Release races outside the model).
- Underflow repeats on the same key within 30 minutes of remediation.
- Multiple keys underflow in the same 5-minute window (suggests a code-wide regression — page release captain).

## 7. References

- `registry-pattern-design.md` §Acquire/Release
- `registry-semantics-exact.md` §Refcount invariants
- `error-handling-design.md` REG_007 entry
