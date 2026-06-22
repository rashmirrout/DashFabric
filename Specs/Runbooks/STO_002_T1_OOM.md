# STO_002 — T1 (fm-data-store) Out of Memory

**Severity:** CRITICAL
**Subsystem:** T1 fabric (storage tier 1)
**SLO Impact:** T1 writes will start failing; control-plane intent stops flowing into FM. Existing programmed state preserved.

---

## 1. Symptoms

- Alert `t1.memory.usage > 90%` for > 60 s.
- Alert `t1.write_error_rate > 1%`.
- T1 server logs: `mvcc: database space exceeded`, `apply: OOM`.
- FM-side: `ADP_005` may co-fire as T1 becomes unresponsive.

## 2. Likely Causes (ordered)

1. MVCC compaction lag — too many revisions kept (compaction interval too long or disabled).
2. Hot keyspace — a runaway CB writer is hammering one prefix with thousands of revisions.
3. Snapshot lag — T1 hasn't snapshotted in too long; WAL grew unbounded.
4. T1 node count reduced (someone scaled down without re-balancing).

## 3. Diagnostics (read-only)

```bash
# T1 endpoint stats
fmctl probe t1 --memory --revisions

# Hot keys (by write rate)
fmctl t1 hot-keys --tail=20

# Compaction state
fmctl t1 compaction-status
```

```promql
t1_mvcc_db_total_size_in_use_in_bytes / t1_mvcc_db_total_size_in_bytes
rate(t1_writes_total[5m])
t1_compaction_lag_revisions
```

## 4. Remediation

**Goal:** Free memory in T1 without losing committed state. FM-side mitigations are limited; mostly coordinate with T1 owner.

1. **Trigger a compaction** to reclaim MVCC space:
   `fmctl t1 compact --to-revision=<recent_safe_rev>`
   Pick a revision at least 1 hour old to avoid colliding with in-flight reads.
   *Rollback:* none — compacted revisions are unrecoverable. This is why we pick "recent_safe_rev" carefully.

2. **Identify and throttle the hot writer** (Cause #2):
   - From step 3 diagnostic, find the top writer.
   - If it's CB-side, ask CB owner to back off. FM cannot throttle CB writes directly without explicit policy.
   - If it's FM-side (e.g., a runaway reconciler), find the culprit and quarantine the actor.

3. **Force a snapshot** (Cause #3):
   `fmctl t1 snapshot-now`
   This is safe and idempotent. Frees WAL space.

4. **If memory does not drop within 5 min**, request T1 owner to scale out T1 (`+1 node`). Do not attempt this from FM-side runbook.

5. **While T1 is degraded, FM-side actions:**
   - FM will continue with cached registry data; programmed state preserved.
   - Do NOT restart FM pods (loses warm registries; on reconnect FM rehydrates from T1 → more T1 pressure).

## 5. Rollback

- Compaction is irreversible. If `--to-revision` was set too aggressively, downstream consumers reading historical state may break — escalate to T1 owner.
- `snapshot-now` is safe; can be re-run.

## 6. Escalate When

- T1 memory still > 85% after compact + snapshot.
- T1 starts dropping writes (visible as ADP_005 + STO_002 together).
- A single hot key is responsible for > 50% of writes — security incident if the writer is unidentified (possible abuse).

## 7. References

- `storage-architecture.md` §T1 capacity model
- `adapter-protocol-design.md` §Backpressure
- `error-handling-design.md` STO_002 entry
- `Specs/Runbooks/ADP_005_T1_UNREACHABLE.md`
