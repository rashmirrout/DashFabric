# Error Handling: Canonical Catalog, Classification, and Recovery

**Status:** AUTHORITATIVE
**Owner:** FM Architecture
**Supersedes:** scattered error definitions across HLD/LLD docs
**Date:** 2026-06-22

---

## 1. PURPOSE

Resolves `ARCHITECTURE_REVIEW.md` INC 2.4: error semantics were inconsistent across docs (some used HTTP-style codes, some used grpc status, some had bespoke enums). This document defines:

1. The single canonical error catalog (codes, classification, recovery)
2. Classification dimensions (where, when, who recovers)
3. SLO-driven error budgets
4. Escalation paths (when does a human get paged)
5. Observability contract (every error has a metric and a log shape)

**Implementation contract:** No code path may invent a new error code. New error → add to catalog → review → use.

---

## 2. CLASSIFICATION DIMENSIONS

Every error has FOUR attributes:

| Dimension | Values | Drives |
|-----------|--------|--------|
| **Severity** | INFO, WARN, ERROR, CRITICAL | Logging level, alert routing |
| **Recoverability** | TRANSIENT, PERMANENT, OPERATOR | Auto-retry vs. quarantine vs. page |
| **Layer** | API, CONTROL, REGISTRY, ACTOR, STORAGE, DRIVER | Owner team, runbook |
| **Blast Radius** | SINGLE_ENI, SINGLE_DEVICE, SHARD, POD, CLUSTER | Throttle/halt rules |

The combination drives the action, not severity alone. A WARN/PERMANENT/SHARD is more urgent than an ERROR/TRANSIENT/SINGLE_ENI.

---

## 3. CANONICAL ERROR CATALOG

Codes follow `<LAYER>_<NUMERIC>_<KIND>` to allow grep across logs.

### 3.1 API Layer (gateway, REST/gRPC) — `API_xxx`

| Code | Severity | Recov | Blast | Meaning |
|------|----------|-------|-------|---------|
| API_001_BAD_REQUEST | WARN | OPERATOR | SINGLE_ENI | Schema violation in input |
| API_002_AUTH_FAILED | WARN | OPERATOR | SINGLE_ENI | mTLS/JWT rejected |
| API_003_NOT_FOUND | INFO | N/A | SINGLE_ENI | Device/ENI doesn't exist |
| API_004_CONFLICT | WARN | TRANSIENT | SINGLE_ENI | ETag mismatch (concurrent modify) |
| API_005_RATE_LIMITED | INFO | TRANSIENT | SINGLE_ENI | Client over quota |
| API_006_ADMISSION_REJECTED | WARN | OPERATOR | POD | Pod over capacity (see threading §9.4) |
| API_007_SHARD_NOT_OWNED | INFO | TRANSIENT | SINGLE_ENI | Request to wrong pod; redirect |
| API_008_TIMEOUT | ERROR | TRANSIENT | SINGLE_ENI | Backend slow |

### 3.2 Registry Layer — `REG_xxx`

| Code | Severity | Recov | Blast | Meaning |
|------|----------|-------|-------|---------|
| REG_001_KEY_NOT_FOUND | INFO | N/A | SINGLE_ENI | Acquire on non-existent key |
| REG_002_WATCH_LOST | WARN | TRANSIENT | SINGLE_ENI | etcd stream dropped |
| REG_003_WATCH_FAILED | ERROR | TRANSIENT | SHARD | etcd unreachable after retries |
| REG_004_ACQUIRE_TIMEOUT | WARN | TRANSIENT | SINGLE_ENI | Ready not closed within deadline |
| REG_005_ASSEMBLER_INCOMPLETE | WARN | TRANSIENT | SINGLE_ENI | VnetMapping chunks missing |
| REG_006_VALUE_DECODE_FAILED | ERROR | PERMANENT | SINGLE_ENI | T1 has invalid proto |
| REG_007_REFCOUNT_UNDERFLOW | CRITICAL | PERMANENT | POD | Bug: more Releases than Acquires |
| REG_008_CACHE_OOM | CRITICAL | TRANSIENT | POD | Memory budget exceeded |

### 3.3 Actor Layer — `ACT_xxx`

| Code | Severity | Recov | Blast | Meaning |
|------|----------|-------|-------|---------|
| ACT_001_PARENT_NOT_READY | INFO | TRANSIENT | SINGLE_ENI | NicActor waiting on HDO |
| ACT_002_INPUTS_NOT_READY | INFO | TRANSIENT | SINGLE_ENI | NicActor waiting on registries |
| ACT_003_COMPOSE_FAILED | ERROR | PERMANENT | SINGLE_ENI | Validation rejected (bad spec) |
| ACT_004_MAILBOX_DROPPED | WARN | TRANSIENT | SINGLE_ENI | Registry burst overflowed |
| ACT_005_PANIC_RECOVERED | ERROR | PERMANENT | SINGLE_ENI | Goroutine recovered from panic |
| ACT_006_REPEATED_PANIC | CRITICAL | PERMANENT | SINGLE_ENI | Quarantined (3 panics in 10min) |
| ACT_007_STATE_INVARIANT_BROKEN | CRITICAL | PERMANENT | POD | State machine corrupted (bug) |
| ACT_008_DEADLINE_EXCEEDED | WARN | TRANSIENT | SINGLE_ENI | Compose/program exceeded budget |

### 3.4 Driver Layer — `DRV_xxx`

(Aligned with `southbound-driver-interface-redesign.md` §7)

| Code | Severity | Recov | Blast | Meaning |
|------|----------|-------|-------|---------|
| DRV_001_CONNECTION_LOST | WARN | TRANSIENT | SINGLE_DEVICE | gRPC/SAI broken |
| DRV_002_TIMEOUT | WARN | TRANSIENT | SINGLE_ENI | Per-command deadline |
| DRV_003_INVALID_PAYLOAD | ERROR | OPERATOR | SINGLE_ENI | Device rejected schema |
| DRV_004_RESOURCE_EXHAUSTED | ERROR | OPERATOR | SINGLE_DEVICE | Device tables full |
| DRV_005_VERSION_MISMATCH | WARN | TRANSIENT | SINGLE_ENI | Plan stale; recompose |
| DRV_006_PARTIAL_APPLY | ERROR | TRANSIENT | SINGLE_ENI | Mid-wave failure |
| DRV_007_DEVICE_REJECTED | ERROR | OPERATOR | SINGLE_ENI | Device-level validation |
| DRV_008_PERMANENT_FAILURE | CRITICAL | PERMANENT | SINGLE_DEVICE | Quarantine |
| DRV_009_CAPABILITY_MISSING | ERROR | OPERATOR | SINGLE_DEVICE | Device lacks required feature |
| DRV_010_HASH_MISMATCH | ERROR | TRANSIENT | SINGLE_ENI | Post-apply hash ≠ target |

### 3.5 Adapter Layer — `ADP_xxx`

(Aligned with `adapter-protocol-design.md`)

| Code | Severity | Recov | Blast | Meaning |
|------|----------|-------|-------|---------|
| ADP_001_LEASE_LOST | WARN | TRANSIENT | POD | Adapter pod lost leadership |
| ADP_002_CAS_CONFLICT | INFO | TRANSIENT | SINGLE_ENI | Concurrent T1 write |
| ADP_003_SCHEMA_REJECTED | ERROR | OPERATOR | SINGLE_ENI | CB sent invalid event |
| ADP_004_UNKNOWN_EVENT_TYPE | ERROR | OPERATOR | SINGLE_ENI | CB plugin/FM version skew |
| ADP_005_T1_UNREACHABLE | CRITICAL | TRANSIENT | CLUSTER | etcd down |
| ADP_006_T2_UNREACHABLE | CRITICAL | TRANSIENT | CLUSTER | etcd down |
| ADP_007_DLQ_INSERTED | WARN | OPERATOR | SINGLE_ENI | Event dead-lettered |
| ADP_008_WATERMARK_REGRESS | CRITICAL | PERMANENT | CLUSTER | Bug: watermark went backwards |

### 3.6 Storage Layer — `STO_xxx`

| Code | Severity | Recov | Blast | Meaning |
|------|----------|-------|-------|---------|
| STO_001_T1_TIMEOUT | WARN | TRANSIENT | SHARD | etcd slow |
| STO_002_T1_OOM | CRITICAL | OPERATOR | CLUSTER | etcd memory exhausted |
| STO_003_T1_CAS_RETRY_EXHAUSTED | ERROR | TRANSIENT | SINGLE_ENI | Lost CAS race repeatedly |
| STO_004_T2_LEASE_EXPIRED | WARN | TRANSIENT | POD | Lease lost mid-op |
| STO_005_T3_CORRUPTION | CRITICAL | PERMANENT | POD | RocksDB integrity check failed |
| STO_006_T3_FULL | ERROR | OPERATOR | POD | Local cache full; eviction can't keep up |

### 3.7 Reconciliation — `REC_xxx`

(Aligned with `reconciliation-design.md` §5)

| Code | Severity | Recov | Blast | Meaning |
|------|----------|-------|-------|---------|
| REC_001_DRIFT_TRANSIENT | INFO | TRANSIENT | SINGLE_ENI | Cleared on retry |
| REC_002_DRIFT_STALE_DEVICE | WARN | TRANSIENT | SINGLE_ENI | Re-applying |
| REC_003_DRIFT_OBJECT | WARN | TRANSIENT | SINGLE_ENI | Targeted fix |
| REC_004_DRIFT_TOTAL | ERROR | TRANSIENT | SINGLE_ENI | Full re-program |
| REC_005_DRIFT_UNKNOWN | CRITICAL | OPERATOR | SINGLE_ENI | Quarantined; external edit suspected |
| REC_006_PEER_DRIFT | INFO | TRANSIENT | SINGLE_ENI | HA failover artifact |
| REC_007_HASH_RPC_FAIL | WARN | TRANSIENT | SINGLE_DEVICE | Device unreachable |
| REC_008_PERSISTENT_DRIFT | CRITICAL | OPERATOR | SINGLE_ENI | 5+ drifts in 1hr |

### 3.8 HA Layer — `HA_xxx`

| Code | Severity | Recov | Blast | Meaning |
|------|----------|-------|-------|---------|
| HA_001_FAILOVER_INITIATED | INFO | N/A | SINGLE_ENI | Normal HA op |
| HA_002_FAILOVER_TIMEOUT | ERROR | TRANSIENT | SINGLE_ENI | Peer didn't take over in budget |
| HA_003_SPLIT_BRAIN_SUSPECTED | CRITICAL | OPERATOR | HA_SCOPE | Both peers claim PRIMARY |
| HA_004_SESSION_REPL_LAG | WARN | TRANSIENT | HA_SCOPE | Replication lag > 1s |
| HA_005_ORPHANED_STANDBY | WARN | OPERATOR | HA_SCOPE | Standby has no primary |

---

## 4. RECOVERY STRATEGY MATRIX

### 4.1 By Recoverability

| Recov | Default Strategy | Backoff |
|-------|-----------------|---------|
| TRANSIENT | Auto-retry with exponential backoff | 100ms → 10s cap, 10% jitter |
| PERMANENT | Stop retrying; emit alert; quarantine affected entity | N/A |
| OPERATOR | Stop retrying; emit alert with runbook link; await human | N/A |

### 4.2 Per-Code Recovery (Selected, see catalog for full)

| Code | Retries | Action on Exhaust |
|------|---------|-------------------|
| DRV_001_CONNECTION_LOST | ∞ with backoff | HDO → DISCONNECTED; NICs → WAITING_DEVICE |
| DRV_002_TIMEOUT | 1 | Mark wave PARTIAL; rollback per policy |
| DRV_003_INVALID_PAYLOAD | 0 | NIC → VALIDATION_REJECTED; operator review |
| ACT_005_PANIC_RECOVERED | 0 (per actor) | Actor → QUARANTINED; pool respawns |
| REG_002_WATCH_LOST | ∞ | Reopen watch; refill cache from T1 snapshot |
| ADP_002_CAS_CONFLICT | ∞ | CB will re-deliver; no special action |
| REC_005_DRIFT_UNKNOWN | 0 | Quarantine ENI; page operator |

### 4.3 Quarantine Semantics

When an entity is quarantined:

| Scope | Allowed | Forbidden |
|-------|---------|-----------|
| Quarantined ENI | Read-only API access, telemetry export | Compose, program, reconcile-remediate |
| Quarantined Device (HDO) | All ENIs implicitly quarantined | Re-acceptance of registration |
| Quarantined Pod | Lease released; standby takes over | All operations |

Lift quarantine: operator API call `POST /api/v1/quarantine/{entity_id}/release`. Auto-lift NEVER happens (humans only).

---

## 5. ERROR BUDGETS (SLO-DRIVEN)

### 5.1 SLO Anchors

| SLO | Target | Measurement Window |
|-----|--------|-------------------|
| ENI program success rate | ≥ 99.9% | 1 hour rolling |
| API availability | ≥ 99.95% | 5 min rolling |
| Reconcile drift detection P99 | ≤ 90s | 1 hour rolling |
| End-to-end NIC ready-after-create P99 | ≤ 5 sec | 1 hour rolling |

### 5.2 Budget-to-Action

```
budget_remaining = 1.0 - (error_rate / (1 - slo_target))
```

| Budget Remaining | Action |
|------------------|--------|
| > 50% | Normal operations |
| 25–50% | Warning to ops channel; no auto-action |
| 10–25% | Admission throttling kicks in (gateway returns 503 for non-critical) |
| < 10% | Page on-call; freeze rollouts; activate runbook |
| 0% (exhausted) | Emergency: halt non-essential traffic, escalate |

### 5.3 Budget Per Error Class

Some errors do NOT consume the program-success budget:
- TRANSIENT errors that recover within retry budget: no consumption
- OPERATOR errors (bad client input): no consumption (not our SLO)
- PERMANENT errors: full consumption

---

## 6. DLQ POLICY

### 6.1 Who Owns the DLQ

| DLQ | Path | Owner | Retention |
|-----|------|-------|-----------|
| Adapter DLQ | `/fm/v1/dlq/adapter/{event_id}` | Adapter pod | 7 days |
| Compose DLQ | `/fm/v1/dlq/compose/{eni_id}` | NicActor | 24 hours |
| Driver DLQ | `/fm/v1/dlq/driver/{eni_id}` | HDO | 24 hours |
| Reconcile DLQ | `/fm/v1/dlq/reconcile/{eni_id}` | Sweeper | 7 days |

### 6.2 DLQ Entry Schema

```protobuf
message DlqEntry {
  string error_code = 1;        // From catalog above
  string entity_id = 2;
  string layer = 3;             // API/ACT/DRV/etc.
  string severity = 4;
  google.protobuf.Timestamp first_seen = 5;
  google.protobuf.Timestamp last_seen = 6;
  uint32 occurrence_count = 7;
  string error_message = 8;
  bytes context_snapshot = 9;   // Compressed; for debug
  string runbook_url = 10;
}
```

### 6.3 DLQ Operations

- `GET /api/v1/dlq` — list with filters (layer, code, age)
- `POST /api/v1/dlq/{id}/retry` — re-enqueue; counter resets
- `DELETE /api/v1/dlq/{id}` — acknowledge as unrecoverable
- `POST /api/v1/dlq/{id}/escalate` — file ticket; preserve

---

## 7. LOGGING CONTRACT

Every error log entry MUST include:

```json
{
  "timestamp": "...",
  "level": "ERROR",
  "code": "DRV_003_INVALID_PAYLOAD",
  "severity": "ERROR",
  "recoverability": "OPERATOR",
  "layer": "DRIVER",
  "blast_radius": "SINGLE_ENI",
  "entity_id": "eni-abc123",
  "shard_id": "fm-pod-3",
  "trace_id": "...",
  "message": "Device rejected payload: field 'mac_address' has invalid format",
  "runbook_url": "https://runbooks.fm.local/DRV_003",
  "context": {
    "device_id": "dpu-001",
    "wave_offset": 1,
    "object_kind": "IDENTITY"
  }
}
```

**Forbidden:** Free-form error strings without a code. If unsure of the code, use `UNKNOWN_CATEGORIZE_ME` and fail the build (lint rule).

---

## 8. METRICS CONTRACT

Every error code MUST be observable via:

```
fm_errors_total{code, severity, recoverability, layer, blast_radius}    counter
fm_errors_active{code} (currently active, e.g., quarantined entities)   gauge
fm_error_budget_remaining_ratio{slo}                                    gauge (0.0 to 1.0)
fm_dlq_depth{dlq}                                                       gauge
fm_dlq_oldest_age_seconds{dlq}                                          gauge
fm_recovery_attempts_total{code, outcome}                                counter
```

Alert rules live in `Specs/Alerts/error-alerts.yaml` (TBD). Each CRITICAL code MUST have an alert.

---

## 9. ESCALATION PATHS

### 9.1 Severity → Routing

| Severity | First Receiver | Escalate After |
|----------|---------------|----------------|
| INFO | Log only | N/A |
| WARN | Ops Slack channel | N/A |
| ERROR | Ops Slack + PagerDuty (low) | 15 min |
| CRITICAL | PagerDuty (high) immediately | 5 min if unack |

### 9.2 Runbook Requirements

Every CRITICAL code MUST have a runbook at `Specs/Runbooks/{code}.md` with:
- Symptoms (what the alert looks like)
- Likely causes
- Diagnostic commands (`kubectl`, T2 inspection queries)
- Remediation steps
- Rollback procedure
- "When to escalate to engineering"

### 9.3 Alert Storm Protection

- Same code within 5 min: deduplicated to one notification
- Storm threshold: > 100 events of same code in 5 min → suppress further; emit "STORM" meta-alert
- Per-entity dedup: same code on same entity → only count once per minute

---

## 10. ADMISSION CONTROL RULES

(Cross-ref `threading-model-design.md` §9.4)

The API gateway rejects new ENI creation (HTTP 503) when ANY of:

| Condition | Threshold | Reset When |
|-----------|-----------|------------|
| `fm_active_nic_actors` | > 45,000 | < 40,000 |
| `fm_actor_mailbox_dropped_total` rate | > 10/sec for 30s | < 1/sec for 60s |
| `fm_adapter_processing_lag_seconds` | > 30s | < 10s for 60s |
| `fm_sbd_errors_total{severity="CRITICAL"}` rate | > 5/min | < 1/min |
| Memory pressure | > 7 GiB | < 6 GiB |
| Error budget for program-success | < 10% | > 25% |

503 response includes:
```json
{
  "code": "API_006_ADMISSION_REJECTED",
  "reason": "shard at capacity",
  "retry_after_seconds": 30,
  "runbook_url": "..."
}
```

---

## 11. CROSS-LAYER ERROR PROPAGATION

When an error crosses layers, the code is preserved:

```
Driver  →  Actor  →  API gateway  →  Client

DRV_003_INVALID_PAYLOAD propagates as:
  - Internal log: DRV_003_INVALID_PAYLOAD
  - NicActor state: VALIDATION_REJECTED (with cause=DRV_003)
  - API response: 422 Unprocessable Entity, error.code = DRV_003_INVALID_PAYLOAD
  - Client sees the actual driver code (not API_004_CONFLICT or similar mismapping)
```

**Mapping table** (for HTTP status codes):

| Layer Code Prefix | HTTP Status |
|-------------------|-------------|
| API_001 | 400 |
| API_002 | 401/403 |
| API_003 | 404 |
| API_004 | 409 |
| API_005 | 429 |
| API_006 | 503 |
| API_007 | 307 (with Location) |
| API_008 | 504 |
| REG_*, ACT_*, DRV_003, DRV_007 | 422 |
| Anything CRITICAL recoverability=PERMANENT | 500 |
| Anything CRITICAL recoverability=TRANSIENT | 503 |

---

## 12. TEST MATRIX

| Test | Behavior Asserted |
|------|-------------------|
| ERR-001 | Every code in catalog has a metric label combination |
| ERR-002 | Every CRITICAL code has a runbook file |
| ERR-003 | OPERATOR errors do not auto-retry |
| ERR-004 | TRANSIENT errors retry with backoff |
| ERR-005 | Same code 100x in 5min → storm-suppressed |
| ERR-006 | Code propagates intact from driver to API response |
| ERR-007 | Admission control activates at threshold; 503 returned with correct fields |
| ERR-008 | Error budget calculation matches SLO definition |
| ERR-009 | Quarantined entity stays quarantined across pod restart |
| ERR-010 | DLQ entries respect TTL; stale entries purged |
| ERR-011 | Log entry without code fails lint (CI rule) |
| ERR-012 | Unknown error code → wrapped in UNKNOWN_CATEGORIZE_ME, build fails |

---

## 13. EVOLUTION RULES

### 13.1 Adding a New Code

1. Open PR adding row to this catalog
2. Specify all four classification dimensions
3. Reference the code from the code path that emits it
4. Add a metric label test
5. If CRITICAL: write the runbook in the same PR
6. Reviewer checks: no overlap with existing code, naming convention, blast radius accurate

### 13.2 Deprecating a Code

1. Mark `DEPRECATED` in catalog (keep row for log archaeology)
2. Replace emit sites with new code
3. After 90 days of zero emissions: remove from catalog

### 13.3 Re-classifying

Changing severity or recoverability is a breaking change for alerts/runbooks. Requires:
- Owner approval (FM Architecture)
- 30-day deprecation window with both old and new active
- Alert configurations updated

---

## 14. REFERENCES

- `threading-model-design.md` §9.4 — Admission control thresholds
- `adapter-protocol-design.md` §8 — Adapter DLQ semantics
- `southbound-driver-interface-redesign.md` §7 — Driver error taxonomy (subset of §3.4 here)
- `device-lifecycle-design.md` §6 — How HDO states absorb driver errors
- `reconciliation-design.md` §5 — Drift classification feeds REC_* codes
- `MASTER_DESIGN_INDEX.md` §5 — Canonical error code registry (this catalog is the source)
