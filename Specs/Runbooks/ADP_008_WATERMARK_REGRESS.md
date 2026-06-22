# ADP_008 — Watermark Regression Detected

**Severity:** CRITICAL
**Subsystem:** Storage adapter (`pkg/fm/storage`)
**SLO Impact:** Monotonic-revision guarantee is at risk. Without it, dedup/idempotency, peer agreement, and "did I already apply this?" answers all become unreliable.

---

## 1. Symptoms

- Alert `fm.adapter.watermark_regression_total > 0` (any value is critical).
- Logs: `adapter: watermark regressed: seen=<X> now=<Y> Y<X target=t1|t2`.
- The adapter has already entered fail-closed mode for that target (designed safety).
- Downstream stalls: composition halts because adapter refuses to serve "older" snapshots as if they were new.

## 2. Likely Causes (ordered)

1. T1/T2 cluster restored from a backup older than current FM state (operational mistake).
2. Adapter pointed at the wrong cluster after a config rotation (look at `adapter.target.endpoint` history).
3. Clock-driven revision generator on the backend was reset (rare; vendor bug).
4. Genuine bug in adapter-side caching (look for adapter version recently deployed).

## 3. Diagnostics (read-only)

```bash
# What watermarks does FM have recorded?
fmctl adapter watermark --target=t1
fmctl adapter watermark --target=t2

# What does the backend report?
fmctl probe t1 --watermark
fmctl probe t2 --watermark

# Endpoint history (did the target change recently?)
fmctl adapter config history --target=t1 --tail=5
```

```promql
# Watermarks over time — should be monotonic
fm_adapter_watermark{target="t1"}
fm_adapter_watermark{target="t2"}
```

## 4. Remediation

**Goal:** Do NOT silently catch up. A regression means the system below us has lost data — the only safe move is operator-supervised resync.

1. **Leave the adapter fail-closed.** This is the safety property; do not bypass.

2. **Identify which side regressed:**
   - If backend watermark < FM-recorded → backend was restored from old snapshot.
   - If FM-recorded < backend AND FM was recently restarted → FM may have lost its watermark cache (recover from T3 or peer).

3. **Backend was restored from old snapshot (most common):**
   - Coordinate with T1/T2 owner. They must either roll forward to a newer snapshot or you accept the regression (operator decision, audit-logged).
   - To force-accept (operator-only):
     `fmctl adapter watermark-resync --target=<t> --accept-regression --justification="<ticket>"`
     This rewrites FM's watermark cache to backend's value. **Audit log** captures operator id + ticket.
     *Rollback:* none — once accepted, the regression is sealed.

4. **Wrong endpoint** (Cause #2):
   - `fmctl adapter config rollback --target=<t>` to revert to last-known-good endpoint.
   - Watermark should self-recover.

5. **Adapter version regression** (Cause #4):
   - `kubectl rollout undo deployment/fm` to revert adapter version.
   - Do NOT accept-regression until the adapter version is back to a known-good build.

## 5. Rollback

- `--accept-regression` is one-way and audit-logged. Do not use without an operator ticket.
- `adapter config rollback` is safe; the previous endpoint config remains in history.

## 6. Escalate When

- Cause is unclear after step 2 — page storage owner + security (could indicate tampering).
- Multiple targets regressed simultaneously — almost certainly endpoint mis-config or a malicious config push; security incident.
- `accept-regression` was just used in last 24 h and ADP_008 fires again — escalate to architecture.

## 7. References

- `adapter-protocol-design.md` §Watermarks and monotonicity
- `security-design.md` §7 — audit log capture for accept-regression
- `storage-architecture.md` §Backup/restore policy
- `error-handling-design.md` ADP_008 entry
