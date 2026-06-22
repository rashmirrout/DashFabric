# Runbooks Index — CRITICAL Error Codes

**Status:** AUTHORITATIVE
**Owner:** FM SRE
**Date:** 2026-06-22
**Mandate:** `error-handling-design.md` §9.2 — every CRITICAL code MUST have a runbook here.

---

## Scope

CRITICAL severity codes only. These are page-on-fire conditions where the system is at risk of data loss, split-brain, or sustained unavailability. Lower severities (WARNING / DEGRADED / ERROR) are handled by automated remediation and dashboards, not runbooks.

Each runbook follows the §9.2 template:

1. **Symptoms** — what you'll see on dashboards / logs
2. **Likely causes** — ordered by prior probability
3. **Diagnostic commands** — copy-paste safe, read-only
4. **Remediation** — step-by-step, with rollback at each step
5. **Rollback** — how to undo if remediation makes things worse
6. **When to escalate** — explicit trigger thresholds

---

## Code → Runbook Map

| Code | Title | Subsystem | File |
|------|-------|-----------|------|
| REG_007 | Refcount underflow in registry | Registry | [REG_007_REFCOUNT_UNDERFLOW.md](REG_007_REFCOUNT_UNDERFLOW.md) |
| REG_008 | Registry cache OOM / heap pressure | Registry | [REG_008_CACHE_OOM.md](REG_008_CACHE_OOM.md) |
| ACT_006 | Actor in repeated panic loop | Actor framework | [ACT_006_REPEATED_PANIC.md](ACT_006_REPEATED_PANIC.md) |
| ACT_007 | Actor state invariant broken | Actor framework | [ACT_007_STATE_INVARIANT_BROKEN.md](ACT_007_STATE_INVARIANT_BROKEN.md) |
| DRV_008 | Driver permanent failure (poison goal) | Southbound driver | [DRV_008_PERMANENT_FAILURE.md](DRV_008_PERMANENT_FAILURE.md) |
| ADP_005 | T1 (fm-data-store) unreachable | Storage adapter | [ADP_005_T1_UNREACHABLE.md](ADP_005_T1_UNREACHABLE.md) |
| ADP_006 | T2 (fm-cluster-state) unreachable | Storage adapter | [ADP_006_T2_UNREACHABLE.md](ADP_006_T2_UNREACHABLE.md) |
| ADP_008 | Watermark regression detected | Storage adapter | [ADP_008_WATERMARK_REGRESS.md](ADP_008_WATERMARK_REGRESS.md) |
| STO_002 | T1 out of memory | T1 fabric | [STO_002_T1_OOM.md](STO_002_T1_OOM.md) |
| STO_005 | T3 (RocksDB) corruption | T3 local | [STO_005_T3_CORRUPTION.md](STO_005_T3_CORRUPTION.md) |
| REC_005 | Reconciliation drift unknown class | Reconciler | [REC_005_DRIFT_UNKNOWN.md](REC_005_DRIFT_UNKNOWN.md) |
| REC_008 | Persistent drift past SLO | Reconciler | [REC_008_PERSISTENT_DRIFT.md](REC_008_PERSISTENT_DRIFT.md) |
| HA_003 | Split-brain suspected | HA / failover | [HA_003_SPLIT_BRAIN_SUSPECTED.md](HA_003_SPLIT_BRAIN_SUSPECTED.md) |

---

## On-Call Quick Reference

| Pager Subject Contains | Open First |
|------------------------|-----------|
| `HA_003` | HA_003 — split-brain is the highest-blast-radius event |
| `STO_002` or `STO_005` | Storage runbook — risk of data loss if mishandled |
| `ADP_005` / `ADP_006` | Adapter runbooks — usually transient infra; check infra page first |
| `ACT_006` | Actor panic loop — almost always a code regression; bisect recent deploys |
| `REC_008` | Persistent drift — operator action probably needed |
| Anything else | Per-code runbook above |

---

## Authoring Rules (for future runbooks)

- Diagnostic commands MUST be read-only (no mutations).
- Remediation MUST be reversible OR explicitly call out "non-reversible from this point."
- Every step has a "if this fails" branch — no dead ends.
- Escalation thresholds are time-bound, not opinion-bound ("if X is still true after 15 min, page Y").
- Update this README's table when adding a new CRITICAL code.

---

## References

- `error-handling-design.md` §9 — error taxonomy and severity definitions
- `recovery-and-failover-design.md` — broader operational context
- `security-design.md` §10 — incident-response hooks tied to these codes
