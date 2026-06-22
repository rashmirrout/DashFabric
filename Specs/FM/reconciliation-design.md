# Reconciliation: Drift Detection & Recovery Protocol

**Status:** AUTHORITATIVE
**Owner:** FM Architecture
**Supersedes:** fleet-manager-hld.md §8 (drift detection — high-level only)
**Date:** 2026-06-22

---

## 1. PURPOSE

Resolves `ARCHITECTURE_REVIEW.md` GAP 1.6: drift detection cadence, telemetry contract, classification, and recovery strategy were unspecified. This document defines:

- When and how often we sample device state
- What "drift" means (taxonomy, not vibes)
- How drift translates to actor state transitions
- How standby pods coordinate (no double-reconcile, no missed drift)
- Performance envelope (read budget per device-second)

**Implementation contract:** Reconciliation is the safety net that catches what waves miss. It must be cheap enough to run continuously but thorough enough to find real bugs.

---

## 2. WHY RECONCILIATION EXISTS

The wave-ordered programming pipeline (`southbound-driver-interface-redesign.md`) is "fire and verify ACK." It does NOT guarantee device state matches FM intent over time. Drift sources:

| Source | Frequency | Detection Path |
|--------|-----------|----------------|
| Device reboot losing un-persisted state | Rare (hours/days) | Reconciliation |
| Partial wave failure not rolled back | Uncommon (operator override) | Reconciliation |
| Vendor SDK bug silently dropping config | Rare but real | Reconciliation |
| Operator manual override on device CLI | Should not happen, but does | Reconciliation |
| Concurrent control-plane (DASH HA peer) edit | By design (HA failover) | Reconciliation + HA channel |
| FM crash mid-program | Common (rolling restart) | Reconciliation on next leader |

**Reconciliation is the truth-mirror.** If wave programming is "push," reconciliation is "pull-and-compare."

---

## 3. SAMPLING CADENCE (DERIVED, NOT GUESSED)

### 3.1 Drift Detection Latency Target

**SLO:** P99 time-from-drift-occurs to drift-detected ≤ 90 seconds. (Operator-set; matches DASH availability targets.)

### 3.2 Cadence Derivation

If we sample each ENI every T seconds, worst-case detection latency = T (drift happens just after a sample).

T ≤ 90s satisfies SLO.

**Choice: T = 60s per ENI** (10s headroom; round number for human ops).

### 3.3 Read Budget

Reconciliation reads `content_hash` per ENI (not full state). One hash read ≈ 100 bytes RPC + 5ms device CPU.

Per pod: 8,000 ENIs ÷ 60s = ~134 reads/sec.
Per device (16 ENIs avg): 16 ÷ 60s = ~0.27 reads/sec — negligible load.

**Sweeper goroutines (from `threading-model-design.md` §8): 4 workers/pod.** Each handles 8000/4 = 2,000 ENIs in 60s = 33 reads/sec per worker. Well within 100 RPS per worker comfort.

### 3.4 Adaptive Cadence

- **Healthy ENI:** 60s default
- **Recently changed (last 5 min):** 15s (catch programming-induced drift fast)
- **Quarantined ENI:** 300s (don't waste reads on broken hardware)
- **Disconnected device:** Skip entirely

Per-ENI state stored in T2 at `/fm/v1/reconcile/{eni_id}` with last_sample_at, last_hash, next_sample_at.

---

## 4. DEVICE TELEMETRY CONTRACT

### 4.1 Required RPC

```go
// Defined in southbound-driver-interface-redesign.md §2
ReadContentHash(ctx context.Context, eni_id string) (string, error)
```

**Semantics:**
- Returns SHA-256 hex string of canonical device state for that ENI
- Device computes hash over its own copy of the same fields FM hashes (`nicgoalstate-schema-design.md` §4)
- MUST be deterministic across reads (no clock fields, no monotonic counters)
- MUST be O(1) on device (precomputed during apply, not recomputed on read)
- Timeout: 2 seconds (reads are cheap; slow read = device issue)

### 4.2 Extended Telemetry (Optional, Used on Drift)

When drift detected, sweeper escalates to `ReadFullState`:

```go
ReadFullState(ctx context.Context, eni_id string) (DeviceNicState, error)
```

Returns:
- Object-level hashes (per route, per ACL, per VIP)
- Per-object timestamps if device supports them
- Used to localize drift to specific objects without re-reading everything

**Why two RPCs:** Cheap continuous check + expensive deep-dive only on suspicion.

### 4.3 Vendor Conformance

Drivers (gNMI, SAI, vendor SDK) MUST implement BOTH RPCs. Drivers without `ReadFullState` get coarse-grained recovery (full ENI reprogram).

---

## 5. DRIFT TAXONOMY

Not all "hashes don't match" are equal. Classification drives recovery:

| Class | Symptom | Cause | Recovery |
|-------|---------|-------|----------|
| **TRANSIENT** | Hash differs once, matches on retry within 5s | Device computing mid-apply | Ignore, log debug |
| **STALE_FM** | Device hash = FM's *previous* target_hash | FM lagging (slow compose) | Wait one compose cycle |
| **STALE_DEVICE** | Device hash = some old FM target hash from history | Device lost recent program | Re-apply latest plan |
| **UNKNOWN_DEVICE** | Device hash matches nothing FM knows | External edit OR severe corruption | Quarantine + alert |
| **OBJECT_DRIFT** | Top hash differs; deep read shows one object off | Vendor SDK partial loss | Targeted re-apply |
| **TOTAL_DRIFT** | Top hash differs; >50% objects off | Device wiped | Full re-program |
| **PEER_DRIFT** | HA peer wrote conflicting state | HA channel out-of-sync | Resolve via HA priority |

### 5.1 Classification Algorithm

```
function ClassifyDrift(eni_id, device_hash, fm_target_hash):
  if device_hash == fm_target_hash:
    return MATCH  # No drift

  # Confirm with second read (filter TRANSIENT)
  sleep(5s)
  device_hash_2 = ReadContentHash(eni_id)
  if device_hash_2 == fm_target_hash:
    return TRANSIENT

  # Look up in T2: has this hash been a target of ours, ever?
  history = T2.Get("/fm/v1/applied_plans/" + eni_id)
  if device_hash in history.previous_hashes:
    return STALE_DEVICE

  # Compare with current compose buffer (maybe NicActor is mid-compose)
  if nic_actor.in_state(COMPOSING) and device_hash == nic_actor.last_emitted_hash:
    return STALE_FM

  # Deep dive
  device_state = ReadFullState(eni_id)
  fm_state = LoadAppliedPlan(eni_id)
  delta = DiffStates(device_state, fm_state)

  if delta.object_count == 0:
    return UNKNOWN_DEVICE  # Top hash differs but objects match — corruption in hash field
  if delta.object_count / fm_state.total_objects > 0.5:
    return TOTAL_DRIFT
  if delta.has_peer_signature():  # See §8
    return PEER_DRIFT
  return OBJECT_DRIFT
```

### 5.2 Action Table

| Class | Sweeper Action | NicActor State | Operator Alert |
|-------|---------------|----------------|----------------|
| MATCH | Update last_sample_at | unchanged | No |
| TRANSIENT | Increment counter | unchanged | No (debug log) |
| STALE_FM | Skip; wait next cycle | unchanged | No |
| STALE_DEVICE | Enqueue REAPPLY | COMPOSING (recompose) | No (info log) |
| UNKNOWN_DEVICE | QUARANTINE | QUARANTINED | YES (critical) |
| OBJECT_DRIFT | Enqueue TARGETED_FIX | COMPOSING (delta only) | No (warning) |
| TOTAL_DRIFT | Enqueue FULL_REAPPLY | COMPOSING (full plan) | YES (warning) |
| PEER_DRIFT | Defer to HA logic | unchanged (HA-driven) | No (handled by HA) |

---

## 6. RECONCILIATION STATE MACHINE (PER ENI)

```
                ┌──────────────┐
                │   IDLE       │  (last sample == target)
                └──────┬───────┘
                       │ sample_due
                       ▼
                ┌──────────────┐
                │   SAMPLING   │  (RPC in flight)
                └──────┬───────┘
                       │
            ┌──────────┼──────────┐
            │ MATCH    │ MISMATCH │
            ▼          ▼          ▼
       IDLE       ┌──────────────┐
                  │  CONFIRMING  │  (5s wait + reread)
                  └──────┬───────┘
                         │
              ┌──────────┼──────────┐
              │ TRANSIENT│ DRIFT    │
              ▼          ▼
            IDLE     ┌──────────────┐
                     │ CLASSIFYING  │  (deep read if needed)
                     └──────┬───────┘
                            │
                            ▼
                     ┌──────────────┐
                     │  REMEDIATING │  (enqueued to NicActor)
                     └──────┬───────┘
                            │ NicActor reports
                            ▼
                          IDLE (or QUARANTINED)
```

### 6.1 Per-State Time Budgets

| State | Max Duration | On Exceed |
|-------|-------------|-----------|
| SAMPLING | 2s | Mark device flaky; back off |
| CONFIRMING | 6s | Treat as DRIFT (proceed) |
| CLASSIFYING | 15s | Treat as TOTAL_DRIFT |
| REMEDIATING | 60s | Escalate to QUARANTINED |

---

## 7. PRIMARY / STANDBY POD COORDINATION

### 7.1 Why This Matters

Each ENI is owned by one FM pod (the shard owner). HA layer maintains primary/standby per pod. **Only the primary reconciles.** Standby observes but does not act.

### 7.2 Coordination Protocol

- Primary holds lease `/fm/v1/lease/fm-pod/{shard_id}` (TTL 15s, renewed every 5s)
- Standby watches the lease; on loss → standby campaigns
- Standby tracks reconciliation state from T2 (`/fm/v1/reconcile/{eni_id}`) but does not write
- On failover:
  1. New primary reads all `next_sample_at` from T2
  2. ENIs with `next_sample_at` in the past → schedule immediately (with 60s jitter to avoid storming)
  3. Resume normal cadence

### 7.3 Avoiding Double Reconcile (Split-Brain)

If old primary is still alive (network partition):
- Old primary's RPCs may succeed if device doesn't enforce lease — accepts a write
- New primary's reconciliation reads device → finds drift → re-applies → ping-pong

**Mitigation:** Drivers MUST include `fm_pod_id + lease_term` in every write. Device-side adapter (CB) drops writes from non-current leader (out-of-band check). See `adapter-protocol-design.md` §4.3.

**Fallback if device cannot enforce:** Old primary self-fences on lease loss within 10s (gRPC client checks lease before every RPC). Race window: ≤10s.

---

## 8. HA PEER DRIFT (PEER_DRIFT CLASS)

### 8.1 Scenario

DASH supports HA pairs: two ENIs share state (`ha_config.peer_eni_id`). On HA failover, peer takes over flow ownership. During the failover window, FM may see "drift" that's actually a legitimate peer write.

### 8.2 Signature

Each ENI's content_hash includes `ha_config.last_known_role` (PRIMARY/STANDBY). When `role` changes:
- Sweeper checks T2 `/fm/v1/ha_events/{ha_scope_id}` for recent failover events
- If failover within last 30s → mark as PEER_DRIFT → defer to HA reconciliation logic
- If no recent failover → treat as normal drift

### 8.3 HA Reconciliation

Separate sweeper for HA scopes (1 goroutine per HA pair):
- Reads both peers' content_hashes
- Verifies role consistency (exactly one PRIMARY)
- Verifies session state replication lag < 1s
- On violation: trigger HA layer's repair (out of scope here; see `fleet-manager-hld.md` §9)

---

## 9. INTEGRATION WITH ACTOR FRAMEWORK

### 9.1 Sweeper → NicActor Communication

Sweeper does NOT modify NIC state directly. It enqueues a `ReconcileEvent` to the NicActor's mailbox:

```protobuf
message ReconcileEvent {
  string eni_id = 1;
  DriftClass class = 2;
  string device_hash = 3;
  string expected_hash = 4;
  RemediationHint hint = 5;
  google.protobuf.Timestamp detected_at = 6;
}

enum RemediationHint {
  HINT_NONE = 0;
  HINT_REAPPLY_LAST = 1;       // STALE_DEVICE
  HINT_TARGETED_FIX = 2;        // OBJECT_DRIFT (with object_id list)
  HINT_FULL_REPROGRAM = 3;      // TOTAL_DRIFT
  HINT_QUARANTINE = 4;          // UNKNOWN_DEVICE
}
```

NicActor handles ReconcileEvent:
- If currently COMPOSING/PROGRAMMING: ignore (will re-check next cycle)
- If READY: transition to COMPOSING with hint
- If QUARANTINED: ignore (already broken)

### 9.2 Mailbox Coalescing

Multiple ReconcileEvents for same ENI within 5min collapse to one (newest wins). Prevents thrash if drift is persistent.

### 9.3 Reconciliation-Triggered Compose

When NicActor enters COMPOSING from a ReconcileEvent (vs. a normal input change):
- Sets `composition_reason = RECONCILE` in audit log
- Increments metric `fm_nic_reconcile_recompose_total`
- Bypasses dedup (force recompute, since device state differs from cache)

---

## 10. METRICS (REQUIRED)

```
fm_reconcile_samples_total{shard, class}                       counter
fm_reconcile_sample_duration_ms{shard}                          histogram
fm_reconcile_drift_detected_total{shard, class}                 counter (ALERT on UNKNOWN_DEVICE)
fm_reconcile_remediation_total{shard, hint, outcome}            counter
fm_reconcile_lag_seconds{shard} (max time since last sample)   gauge (ALERT > 120s)
fm_reconcile_queue_depth{shard}                                 gauge
fm_reconcile_classify_duration_ms{shard}                        histogram
fm_reconcile_full_state_reads_total{shard}                      counter (cost tracker)
fm_ha_peer_drift_total{ha_scope}                                counter
fm_reconcile_quarantined_eni_count                              gauge (ALERT > 0)
```

---

## 11. CONFIGURATION

```yaml
reconciliation:
  default_cadence_seconds: 60
  recent_change_cadence_seconds: 15
  quarantine_cadence_seconds: 300
  confirm_wait_ms: 5000
  sweeper_workers: 4
  full_state_read_budget_per_minute: 20  # Cap on expensive reads
  transient_threshold: 2                 # Two consecutive same-hash reads = TRANSIENT cleared
  drift_alert_threshold: 5               # Alert if same ENI drifts 5x in 1hr
```

---

## 12. TEST MATRIX

| Test | Behavior Asserted |
|------|-------------------|
| REC-001 | All 8k ENIs sampled within 60s ± 10% |
| REC-002 | TRANSIENT drift filtered (no remediation) |
| REC-003 | STALE_DEVICE → NicActor recomposes within 90s |
| REC-004 | UNKNOWN_DEVICE → ENI quarantined, alert fires |
| REC-005 | OBJECT_DRIFT → targeted re-apply (only diff objects) |
| REC-006 | Sweeper crash → standby picks up; no double remediation |
| REC-007 | HA failover within 30s → drift classified as PEER_DRIFT, not remediated |
| REC-008 | Read budget (full_state) respected: ≤20 expensive reads/min |
| REC-009 | Reconcile-triggered compose has reason=RECONCILE in audit |
| REC-010 | Quarantined ENI cadence drops to 300s |
| REC-011 | Drift on 100 ENIs simultaneously: queue absorbs, no actor loss |
| REC-012 | Mid-program drift detected and deferred (NicActor in PROGRAMMING) |

---

## 13. FAILURE SCENARIOS

### 13.1 Device Hash RPC Times Out

- Mark ENI sample as FAILED (not DRIFT)
- Increment device flakiness counter
- After 3 consecutive failures: HDO → DEGRADED (per `device-lifecycle-design.md` §6.6)
- Skip reconciliation until device healthy

### 13.2 Sweeper Cannot Reach T2

- Cannot update next_sample_at
- Buffer local schedule (in-memory) for up to 60s
- After 60s without T2 → pause sweeper, emit alert
- Resume from T2 schedule on recovery

### 13.3 NicActor Mailbox Full During Reconciliation

- Sweeper drops oldest ReconcileEvent (mailbox policy)
- Metric `fm_reconcile_mailbox_dropped_total` increments
- Next sweep cycle re-detects drift; eventually applied

### 13.4 Vendor Driver Lacks ReadFullState

- Classifier cannot distinguish OBJECT_DRIFT from TOTAL_DRIFT
- Default to TOTAL_DRIFT (safe but expensive)
- Operator alert: "driver X lacks deep-read support; reconciliation is coarse"

---

## 14. PERFORMANCE ENVELOPE (PER POD)

| Metric | Steady | Burst (post-incident) | Cap |
|--------|--------|----------------------|-----|
| Hash reads/sec | 134 | 600 | 1,000 |
| Full state reads/sec | <1 | 20 | 33 (= budget) |
| RPC bandwidth in | ~50 KB/s | ~250 KB/s | — |
| Sweeper CPU | <5% of 1 core | ~15% | 25% (admission throttle) |
| Memory (reconcile state in T2 cache) | ~10 MiB | ~20 MiB | 50 MiB |

---

## 15. INTEGRATION WITH ERROR HANDLING

When reconciliation classifies UNKNOWN_DEVICE or repeated TOTAL_DRIFT:
- Emit `ErrCode = REC_PERSISTENT_DRIFT` (see `error-handling-design.md`)
- Quarantine policy applies (`error-handling-design.md` §6)
- Operator runbook: `Specs/Runbooks/reconcile-quarantine.md` (TBD)

---

## 16. REFERENCES

- `southbound-driver-interface-redesign.md` §2 — ReadContentHash, ReadFullState
- `nicgoalstate-schema-design.md` §4 — Content-hash determinism
- `device-lifecycle-design.md` §8 — When HDO permits reconciliation
- `threading-model-design.md` §8 — Sweeper pool sizing (P7)
- `adapter-protocol-design.md` §4 — Leader lease (drives primary/standby)
- `error-handling-design.md` — Error codes used in remediation outcomes
- `MASTER_DESIGN_INDEX.md` — Canonical drift class registry
