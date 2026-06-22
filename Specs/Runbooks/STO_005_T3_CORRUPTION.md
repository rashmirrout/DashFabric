# STO_005 — T3 (RocksDB) Corruption

**Severity:** CRITICAL
**Subsystem:** T3 local store (per-FM RocksDB)
**SLO Impact:** Local device-side mirror is unreliable for the affected FM pod. Cold restart cannot replay state. Cross-check with T1/T2 is REQUIRED before any device write.

---

## 1. Symptoms

- Alert `fm.t3.corruption_detected_total > 0` (any value is critical).
- Logs: `t3[rocksdb]: Corruption: <details>`, `t3: checksum mismatch sst=<...>`.
- The affected FM pod self-marks `T3_DEGRADED`; some paths fail fast rather than read possibly-corrupt state.
- On restart, the pod refuses to rehydrate from T3 and stays in `RECOVERING`.

## 2. Likely Causes (ordered)

1. Disk-level corruption (bad block, partial write during host kernel panic).
2. RocksDB version mismatch after a binary upgrade (read path crashes on new WAL format).
3. Filesystem-level event (snapshot, resize) that left RocksDB in an inconsistent state.
4. Hardware failing — check disk SMART before doing anything else.

## 3. Diagnostics (read-only)

```bash
# Pod-local
fmctl t3 verify          # runs rocksdb's built-in check
fmctl t3 stats           # SST files, levels, deleted entries

# Filesystem & disk
df -h /var/lib/fm/t3
dmesg | tail -200 | grep -iE 'error|fail|corrupt'
smartctl -a /dev/<disk>  # if accessible
```

```promql
fm_t3_corruption_detected_total
fm_t3_compaction_errors_total
rate(fm_t3_open_failures_total[15m])
```

## 4. Remediation

**Goal:** Do NOT trust T3 on this pod. Drain it and rebuild T3 from authoritative sources.

1. **Stop the pod from accepting new programmed-state writes:**
   `fmctl pod drain --reason="STO_005"`
   Existing actors complete in-flight work, but no new device programming runs from this pod.

2. **Failover ownership** of devices held by this pod to a peer FM in the same HA scope:
   `fmctl ha failover --from-pod=<this> --to-pod=<peer>`
   *Rollback:* `fmctl ha failover --back` once T3 is rebuilt and verified.

3. **Rebuild T3 from authoritative source:**
   - **Preferred:** wipe T3, restart pod; the pod rehydrates from T1 (intent) + driver reads (programmed-state mirror).
     ```bash
     kubectl exec <pod> -- rm -rf /var/lib/fm/t3/*
     kubectl delete pod <pod>   # supervisor restarts
     ```
   - **Optional:** copy a peer FM's T3 snapshot if it's in the same HA scope and within freshness SLO. This is faster but introduces peer-trust assumptions — use only if rehydration is too slow.

4. **Verify** post-rebuild:
   `fmctl t3 verify` (must pass), `fm.actor.invariant_violation_total == 0` for 30 min.

5. **Disk hardware** (Cause #4): if SMART shows bad sectors or pending reallocations, replace the node before re-enrolling this pod. Do not put T3 on the same disk.

## 5. Rollback

- T3 wipe is irreversible. The rebuild relies on T1 being correct — if T1 is also degraded, fix T1 first.
- HA failover back is safe once T3 is verified clean.

## 6. Escalate When

- T3 corruption appears on > 1 pod in the same HA scope within 30 min → likely a shared-storage issue or recent deploy bug; page storage owner + release captain.
- Rebuild does not converge within 15 min (rehydration stuck) — likely an input-side issue; switch to relevant adapter/reg runbook.
- Disk SMART shows imminent failure — coordinate with infra to migrate the node entirely.

## 7. References

- `storage-architecture.md` §T3 local store
- `recovery-and-failover-design.md` §Cold-rebuild from T1
- `error-handling-design.md` STO_005 entry
