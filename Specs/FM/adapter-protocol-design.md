# Adapter Protocol: Watermark, Idempotency, and CB Acknowledgment

**Status:** AUTHORITATIVE
**Owner:** FM Architecture
**Supersedes:** storage-architecture.md (adapter section, partial)
**Date:** 2026-06-22

---

## 1. PURPOSE

Defines the exact protocol between the FM Adapter pod, ControllerBridge (CB), and T1/T2 storage. Resolves ambiguity in:

1. Where watermarks live (T2)
2. How adapter failover preserves at-least-once delivery
3. How CB knows FM received an event
4. How duplicate CB events are deduplicated (idempotency)
5. How the leader-election lease coordinates with watermark writes

This spec eliminates the "events lost or re-processed" risk identified in `ARCHITECTURE_REVIEW.md` GAP 1.7.

---

## 2. ROLES

| Component | Responsibility |
|-----------|---------------|
| **CB (ControllerBridge)** | Out-of-process plugin emitting state-change events to FM |
| **Adapter Pod** | FM-side translator: receives CB events, validates, writes T1 |
| **Adapter Leader** | One adapter pod holds T2 lease; only the leader writes T1 |
| **T1 (fm-data-store)** | Authoritative configuration; CAS-based writes by content_hash |
| **T2 (fm-cluster-state)** | Coordination: leader lease, watermarks, DLQ index |

---

## 3. KEY LAYOUT (T2)

```
/fm/v1/lease/adapter                              → leader lease (15s TTL)
/fm/v1/watermarks/adapter/{cb_endpoint_id}        → last-processed CB event_id + offset
/fm/v1/dlq/adapter/{event_id}                     → events that failed processing
/fm/v1/idempotency/adapter/{cb_event_id}          → 24h TTL; maps to T1 content_hash written
```

### 3.1 Watermark Schema

```protobuf
message AdapterWatermark {
  string cb_endpoint_id = 1;                 // CB identity (multi-CB support)
  string last_event_id  = 2;                 // Monotonic from CB
  int64  last_offset    = 3;                 // CB's stream offset (resume hint)
  google.protobuf.Timestamp updated_at = 4;
  string adapter_pod_id = 5;                 // Who wrote this watermark
  int64  lease_term     = 6;                 // Leader term when written
}
```

### 3.2 Idempotency Record

```protobuf
message IdempotencyRecord {
  string cb_event_id = 1;
  string t1_path     = 2;                    // Where the result landed
  string content_hash = 3;                   // What was written
  google.protobuf.Timestamp processed_at = 4;
  string outcome     = 5;                    // "applied" | "noop_dedup" | "dlq"
}
```

TTL: 24 hours (covers CB replay windows; configurable).

---

## 4. LEADER ELECTION

### 4.1 Lease Mechanics

- Lease key: `/fm/v1/lease/adapter`
- Lease TTL: **15 seconds**
- Renewal interval: **5 seconds** (every renewal extends TTL by 15s)
- Election protocol: etcd leader election (standard library, e.g. `concurrency.Election`)
- Term: monotonic integer; incremented on each new leader

### 4.2 Leader Lifecycle

```
function AdapterPodLoop():
  for {
    election = NewElection(t2_client, "/fm/v1/lease/adapter", pod_id)
    err = election.Campaign(ctx)  // Blocks until won

    if err != nil:
      log.Error("election failed", err)
      sleep(backoff)
      continue

    leader_term = election.Term()
    log.Info("became leader", "term", leader_term)

    err = RunAsLeader(ctx, leader_term)

    if err != nil:
      log.Error("leader exiting", err)
      election.Resign(ctx)
    # Loop and re-campaign
  }


function RunAsLeader(ctx, leader_term):
  # 1. Read all watermarks
  watermarks = T2.ReadPrefix("/fm/v1/watermarks/adapter/")

  # 2. Open CB subscriptions resumed from watermarks
  for cb_endpoint in config.cb_endpoints:
    wm = watermarks.get(cb_endpoint.id, ZeroWatermark())
    cb_stream = cb_endpoint.Subscribe(resume_offset=wm.last_offset)
    go ProcessCBStream(ctx, cb_endpoint, cb_stream, leader_term)

  # 3. Hold leader role; renew lease in background
  ctx.Wait()  # blocks until ctx cancelled OR lease lost
```

### 4.3 Lease Loss Handling

If lease is lost mid-write (e.g., network partition):
- All in-flight writes MUST be rejected (driver checks lease before T1 CAS)
- Adapter pod resigns and re-campaigns
- New leader resumes from last-persisted watermark (no event loss, at-least-once)

---

## 5. CB EVENT PROCESSING ALGORITHM

```
function ProcessCBStream(ctx, cb_endpoint, cb_stream, leader_term):
  for event in cb_stream:
    # === Step 1: Lease check ===
    current_lease = T2.GetLease("/fm/v1/lease/adapter")
    if current_lease.term != leader_term:
      log.Warn("lease lost, exiting stream processor")
      return  # Re-campaign

    # === Step 2: Idempotency check ===
    existing = T2.Get("/fm/v1/idempotency/adapter/" + event.cb_event_id)
    if existing != nil:
      # Already processed; send ack and skip
      cb_endpoint.Ack(event.cb_event_id, status="duplicate", t1_path=existing.t1_path)
      continue

    # === Step 3: Validate and decode ===
    err = ValidateEvent(event)
    if err != nil:
      WriteDLQ(event, err)
      cb_endpoint.Ack(event.cb_event_id, status="rejected", reason=err.message)
      continue

    # === Step 4: Translate to FM canonical form ===
    fm_record, t1_path = TranslateEvent(event)
    new_hash = ComputeContentHash(fm_record)

    # === Step 5: T1 CAS write ===
    # Read current value (for CAS)
    current = T1.Get(t1_path)
    if current != nil and current.content_hash == new_hash:
      # No-op dedup at T1 level
      WriteIdempotencyRecord(event.cb_event_id, t1_path, new_hash, "noop_dedup")
      AdvanceWatermark(cb_endpoint.id, event, leader_term)
      cb_endpoint.Ack(event.cb_event_id, status="noop", t1_path=t1_path)
      continue

    # CAS: write only if revision matches
    expected_revision = current.revision if current else 0
    success, new_revision = T1.CompareAndSwap(
      key=t1_path,
      value=fm_record,
      expected_revision=expected_revision,
    )

    if !success:
      # Concurrent write detected; back off and retry next iteration
      log.Warn("CAS conflict; will reprocess", "event", event.cb_event_id)
      continue  # do NOT ack; CB will re-deliver

    # === Step 6: Write idempotency record + watermark (atomically via T2 txn) ===
    T2.Transaction([
      Put("/fm/v1/idempotency/adapter/" + event.cb_event_id, IdempotencyRecord{
        cb_event_id: event.cb_event_id,
        t1_path: t1_path,
        content_hash: new_hash,
        processed_at: now(),
        outcome: "applied",
      }, ttl=24*hour),
      Put("/fm/v1/watermarks/adapter/" + cb_endpoint.id, AdapterWatermark{
        cb_endpoint_id: cb_endpoint.id,
        last_event_id: event.cb_event_id,
        last_offset: event.stream_offset,
        updated_at: now(),
        adapter_pod_id: pod_id,
        lease_term: leader_term,
      }),
    ])

    # === Step 7: Ack to CB ===
    cb_endpoint.Ack(event.cb_event_id, status="applied", t1_path=t1_path)
```

---

## 6. ATOMICITY GUARANTEES

### 6.1 The Two-Write Problem

T1 (data) and T2 (watermark) are separate stores. A naive sequence:
1. Write T1
2. Write T2 watermark
…can fail between (1) and (2), causing:
- Event applied to T1, but watermark not updated → CB replays event → IdempotencyRecord catches it (no double-apply, but extra work)

### 6.2 Resolution

**T1 write happens first (with CAS). T2 idempotency+watermark writes happen second (in a single T2 transaction).**

Why this is safe:
- If T1 succeeds but T2 fails: CB re-delivers event → adapter does idempotency lookup (miss, because T2 write failed) → adapter retries T1 CAS → succeeds (because content_hash matches existing) → enters "noop_dedup" path → writes T2 idempotency record → ack to CB. **No corruption, just extra work.**
- If T1 fails (CAS conflict): no T2 writes; CB will re-deliver (no ack sent).
- If both succeed: ack to CB; CB drops event from its outbox.

### 6.3 CAS on T1

T1 supports etcd-style revision-based CAS:
```
T1.CompareAndSwap(key, value, expected_revision) → (success_bool, new_revision)
```

If `expected_revision` differs from current → returns `success=false`. Adapter retries on next stream iteration (event remains unacked).

---

## 7. CB ACKNOWLEDGMENT PROTOCOL

### 7.1 Ack Message

```protobuf
message StateAck {
  string cb_event_id = 1;                    // The event being acked
  string status      = 2;                    // "applied" | "noop" | "duplicate" | "rejected" | "dlq"
  string t1_path     = 3;                    // Where it landed (if applied)
  string content_hash = 4;                   // For caller verification
  string reason      = 5;                    // For rejected/dlq
  google.protobuf.Timestamp processed_at = 6;
  int64  fm_revision = 7;                    // T1 revision after apply
}
```

### 7.2 Ack Channels

CB MUST expose two endpoints:
- **gRPC unary Ack:** `rpc Ack(StateAck) returns (AckReceipt)` — for synchronous confirmation
- **Streaming AckStream:** `rpc AckStream(stream StateAck) returns (AckSummary)` — for batched (higher throughput)

Adapter uses streaming Ack when load is high; falls back to unary for low-traffic mode.

### 7.3 Ack Failure Handling

If Ack RPC to CB fails:
- T1 write is already committed (irreversible)
- T2 idempotency record exists (will catch replay)
- Adapter logs warning, continues processing
- CB will re-deliver event on its own retry timer (typically 60s); adapter responds with "duplicate" via idempotency check

---

## 8. DLQ (DEAD LETTER QUEUE)

### 8.1 When to DLQ

| Condition | DLQ? |
|-----------|------|
| Schema validation failed | Yes |
| Unknown CB event type | Yes |
| Translation failed (logic error) | Yes |
| T1 unreachable for >5 min | Yes (with retry queue separately) |
| CAS conflict (transient) | No (just retry) |
| Idempotency hit | No (just ack) |

### 8.2 DLQ Schema

```protobuf
message DlqEntry {
  string cb_event_id = 1;
  string cb_endpoint_id = 2;
  bytes  raw_payload = 3;
  string error_code = 4;
  string error_message = 5;
  google.protobuf.Timestamp received_at = 6;
  google.protobuf.Timestamp dlq_at = 7;
}
```

Stored at `/fm/v1/dlq/adapter/{event_id}` with 7-day TTL.

### 8.3 Operator Interface

- REST endpoint: `GET /api/v1/dlq/adapter` — list DLQ entries
- REST endpoint: `POST /api/v1/dlq/adapter/{event_id}/retry` — re-enqueue
- REST endpoint: `DELETE /api/v1/dlq/adapter/{event_id}` — acknowledge as unrecoverable

---

## 9. FAILURE SCENARIOS

### 9.1 Adapter Pod Crash Mid-Write

```
t=0:  Receive event E1
t=1:  T1 CAS write succeeds (revision=42, hash=ABC)
t=2:  ←  POD CRASH (T2 writes not done)
t=3:  Election: pod_B becomes new leader (term++)
t=4:  pod_B reads watermark (still points to E0)
t=5:  pod_B resubscribes from E0+1 → receives E1 again
t=6:  pod_B idempotency lookup: miss (T2 record wasn't written)
t=7:  pod_B T1 CAS: current value already has hash=ABC → revision=42 → CAS expected_revision=42 → succeeds (no-op semantically)
t=8:  pod_B writes T2 idempotency + watermark
t=9:  pod_B acks CB
```

**Result:** No data loss, no double-apply, eventual consistency in <10s.

### 9.2 CB Disconnect / Reconnect

```
t=0:  Adapter subscribed; processing events at offset=100
t=1:  Network partition; CB stream broken
t=2:  Adapter retry loop (exponential backoff)
t=10: Connection re-established
t=11: Adapter resumes from watermark (e.g., offset=98 — last fully processed)
t=12: CB re-delivers events 99, 100, ... → adapter dedupes via idempotency
```

### 9.3 T1 Outage

- T1 CAS fails → adapter pauses processing, retries with backoff
- Watermark not advanced → CB will re-deliver on its own timer
- After T1 recovery → processing resumes; no events lost

### 9.4 T2 Outage

- Lease cannot be renewed → adapter loses leader role → resigns
- Other adapter pods cannot win lease (T2 down) → adapter cluster paused
- CB events queue up at CB side (CB has its own buffering)
- After T2 recovery → election runs → leader resumes from last watermark in T2

---

## 10. MULTI-CB SUPPORT

The protocol supports multiple CB endpoints simultaneously:

```yaml
cb_endpoints:
  - id: "cb_dpu_intel"
    address: "cb-intel.fm.local:8443"
    auth: "mtls"
  - id: "cb_dpu_nvidia"
    address: "cb-nvidia.fm.local:8443"
    auth: "mtls"
  - id: "cb_test_simulator"
    address: "cb-sim.fm.local:8443"
    auth: "none"
```

Each CB:
- Has its own watermark in T2
- Has independent stream processor goroutine
- Independent idempotency namespace (cb_event_id is scoped per CB)

---

## 11. METRICS (REQUIRED)

```
fm_adapter_events_received_total{cb_endpoint}
fm_adapter_events_applied_total{cb_endpoint, outcome="applied|noop|duplicate"}
fm_adapter_events_rejected_total{cb_endpoint, reason}
fm_adapter_events_dlq_total{cb_endpoint, reason}
fm_adapter_cas_conflicts_total{cb_endpoint}
fm_adapter_watermark_offset{cb_endpoint} (gauge)
fm_adapter_lease_state{state="leader|follower|election|lost"} (gauge)
fm_adapter_lease_term (gauge, monotonic)
fm_adapter_cb_ack_latency_ms{cb_endpoint} (histogram)
fm_adapter_t1_write_latency_ms (histogram)
fm_adapter_t2_txn_latency_ms (histogram)
fm_adapter_processing_lag_seconds{cb_endpoint} (gauge: now() - last_event_time)
```

---

## 12. ACCEPTANCE TESTS

| Test | Behavior Asserted |
|------|-------------------|
| ADP-001 | Adapter crash after T1 write, before T2 → recovery applies idempotently |
| ADP-002 | Adapter crash after T2 write, before ack → CB retries, adapter dedupes |
| ADP-003 | CAS conflict → event NOT acked → CB re-delivers → succeeds |
| ADP-004 | T1 outage 60s → events buffered at CB → recovery drains backlog |
| ADP-005 | T2 outage → lease lost → no T1 writes during outage |
| ADP-006 | Lease term bump → old leader's pending writes rejected |
| ADP-007 | Duplicate CB event (same cb_event_id) → ack as "duplicate", no T1 write |
| ADP-008 | Multi-CB: events from CB-A and CB-B processed concurrently, independent watermarks |
| ADP-009 | Malformed event → DLQ; metric incremented; CB acked as "rejected" |
| ADP-010 | 10k events/sec sustained: no event loss, watermark advances monotonically |

---

## 13. REFERENCES

- `storage-architecture.md` — T1/T2 layout
- `Specs/CB/01-cb-architecture-hld.md` — CB-side protocol
- `Specs/CB/05-cb-ack-and-versioning.md` — CB ack semantics
- `nicgoalstate-schema-design.md` §4 — Content-hash algorithm (used by adapter for dedup)
