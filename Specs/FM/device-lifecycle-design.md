# Device Lifecycle: REST API ↔ HDO Actor State Alignment

**Status:** AUTHORITATIVE
**Owner:** FM Architecture
**Supersedes:** fm-device-registration-flow-design.md §6, fleet-manager-lld.md §1.1 (actor state machine)
**Date:** 2026-06-22

---

## 1. PURPOSE

Resolves the state-machine inconsistency identified in `ARCHITECTURE_REVIEW.md` GAP 1.3:
- Device registration flow doc says states are: WAITING_BOOTSTRAP, HYDRATING, READY_FOR_ENI, PROGRAMMING, READY
- Actor LLD has no WAITING_HYDRATION state; goes INITIALIZING → WAITING_BOOTSTRAP directly
- Ambiguous: when does HDO transition WAITING_BOOTSTRAP → READY?

This document defines a single, authoritative state machine spanning REST API responses, HDO actor internal states, and NicActor parent-readiness gating.

---

## 2. THREE LAYERS, ONE STATE MACHINE

The device lifecycle has three observable views, all derived from the same internal state machine:

| View | Audience | What it Shows |
|------|----------|---------------|
| **REST `device.status`** | External (orchestrator) | Coarse status (5 values) |
| **HDO Actor state** | FM-internal (debug, telemetry) | Fine-grained state (8 values) |
| **NicActor parent readiness** | NicActor gating | Boolean: `parent_ready()` |

The internal state machine drives all three. They are not independent.

---

## 3. HDO ACTOR STATE MACHINE (AUTHORITATIVE)

```
                         ┌─────────────────┐
                         │   INITIALIZING  │  (HDO struct created, registries not subscribed yet)
                         └────────┬────────┘
                                  │ subscribe to /config/v1/global/**, /config/v1/group/**
                                  ▼
                         ┌─────────────────┐
                         │ WAITING_BOOTSTRAP│  (waiting for first global+group snapshots)
                         └────────┬────────┘
                                  │ both registries' Ready chans closed
                                  ▼
                         ┌─────────────────┐
                         │  HYDRATING_DEV  │  (subscribe to /config/v1/hosts/{device_id}/**, fetch container list)
                         └────────┬────────┘
                                  │ device snapshot received from T1
                                  ▼
                         ┌─────────────────┐
                         │      READY      │  ✅ NICs may begin programming
                         └────────┬────────┘
                                  │
              ┌───────────────────┼────────────────────┐
              │                   │                    │
              ▼                   ▼                    ▼
    ┌──────────────────┐  ┌──────────────────┐  ┌──────────────────┐
    │  DISCONNECTED    │  │   DEGRADED       │  │  DRAINING        │
    │  (device offline,│  │  (partial state, │  │  (operator       │
    │   reconnect loop)│  │  some NICs OK)   │  │  initiated)      │
    └──────────────────┘  └──────────────────┘  └──────────────────┘
              │                                          │
              │ device reconnects                        │ all NICs drained
              ▼                                          ▼
         (back to READY)                          ┌──────────────────┐
                                                  │   DEREGISTERED   │ (terminal)
                                                  └──────────────────┘
```

### 3.1 State Definitions

| State | Meaning | Trigger to Next | Allowed Outbound Actions |
|-------|---------|-----------------|--------------------------|
| INITIALIZING | HDO struct exists, no subscriptions | Subscribe completes | None |
| WAITING_BOOTSTRAP | Awaiting global + group hydration | Both Ready closed | None |
| HYDRATING_DEV | Awaiting device-specific data | Device snapshot in T1 | None |
| READY | Fully hydrated; ENIs can program | Failure / disconnect | All (compose, program, reconcile) |
| DISCONNECTED | Device unreachable; ENIs paused | Reconnect succeeds | None (NICs in WAITING_DEVICE) |
| DEGRADED | Partial: some object reads failing | Recovery / drain | Limited (no new ENIs) |
| DRAINING | Operator-initiated shutdown | Last NIC removed | DELETE only |
| DEREGISTERED | Terminal; HDO destroyed | — | — |

### 3.2 Why HYDRATING_DEV (New State)

The old design lacked a state between "global+group cached" and "ready". This caused ambiguity: when device subscribes to `/config/v1/hosts/{device_id}/...`, there's a window where ambient data (global, group) is hydrated but device-specific data (container list, NIC manifests) is not.

**HYDRATING_DEV closes this gap.** During this state:
- HDO has subscribed to device-specific keys
- First snapshot is en route from T1
- NicActors may be created in `WAITING_PARENT` but cannot compose

---

## 4. REST API STATUS MAPPING

```
internal HDO state              REST device.status
────────────────────────        ──────────────────
INITIALIZING            ──►    REGISTERING
WAITING_BOOTSTRAP       ──►    REGISTERING
HYDRATING_DEV           ──►    REGISTERED
READY                   ──►    READY_FOR_ENI
DEGRADED                ──►    DEGRADED
DISCONNECTED            ──►    OFFLINE
DRAINING                ──►    DRAINING
DEREGISTERED            ──►    DEREGISTERED  (then 404 after grace)
```

### 4.1 REST Response Schema (Updated)

```protobuf
message Device {
  string device_id = 1;
  string shard_id  = 2;                   // Owning FM pod (debug)
  string status    = 3;                   // See mapping above
  string content_hash = 4;                // Of registered capabilities
  google.protobuf.Timestamp created_at = 5;
  google.protobuf.Timestamp last_seen_at = 6;
  DeviceCapabilities capabilities = 7;

  // INTERNAL — exposed only via /api/v1/debug/devices/{id}
  // NOT returned in normal GET /api/v1/devices/{id}
  string hdo_internal_state = 90;         // For operator debugging
}
```

### 4.2 INCONSISTENCY FIX

The previous registration design returned `subscription_topics` in the REST response. This is FM-internal data and MUST be removed. The client never needs it.

**Old (remove):**
```json
{
  "device_id": "dev-001",
  "subscription_topics": ["/config/v1/global/...", "/config/v1/group/...", ...]
}
```

**New (correct):**
```json
{
  "device_id": "dev-001",
  "shard_id": "fm-pod-3",
  "status": "REGISTERED",
  "content_hash": "a3f...",
  "created_at": "2026-06-22T10:00:00Z"
}
```

---

## 5. NICACTOR PARENT-READINESS GATE

NicActor MUST check `parent_ready()` before composing/programming. Definition:

```
function parent_ready(nic_actor) -> bool:
  hdo = nic_actor.parent_hdo()
  return hdo.state == READY || hdo.state == DEGRADED
```

States where NIC cannot proceed:
- INITIALIZING, WAITING_BOOTSTRAP, HYDRATING_DEV → NIC parks in WAITING_PARENT
- DISCONNECTED → NIC parks in WAITING_DEVICE
- DRAINING → NIC enters DELETING (if not already)
- DEREGISTERED → NIC destroyed

### 5.1 NicActor State Machine (Reference)

```
WAITING_PARENT ──(parent_ready)──► WAITING_REFS
WAITING_REFS   ──(all reg. ready)─► COMPOSING
COMPOSING      ──(compose ok)────► PROGRAMMING
PROGRAMMING    ──(apply ok)──────► READY
READY          ──(input changed)─► COMPOSING (recompose)
*              ──(parent !ready)─► WAITING_PARENT (back-off)
*              ──(parent draining)──► DELETING
```

---

## 6. STATE TRANSITION EVENTS

### 6.1 INITIALIZING → WAITING_BOOTSTRAP

**Trigger:** HDO constructor returns. All baseline subscriptions issued (`/config/v1/global/**`, `/config/v1/group/**`).

**Actions:**
- Open Ready channels on GlobalRegistry, GroupRegistry
- Start timeout timer (default: 30s for bootstrap)

### 6.2 WAITING_BOOTSTRAP → HYDRATING_DEV

**Trigger:** Both GlobalRegistry.Ready and GroupRegistry.Ready closed.

**Actions:**
- Subscribe to `/config/v1/hosts/{device_id}/**`
- Open Ready channel on device subscription

### 6.3 HYDRATING_DEV → READY

**Trigger:** Device subscription Ready closed AND device snapshot decoded successfully.

**Actions:**
- Emit `device_ready` event to all child NicActors (wake from WAITING_PARENT)
- Update T2: `/fm/v1/devices/{device_id}/status = READY_FOR_ENI`
- Update REST device status

### 6.4 READY → DISCONNECTED

**Trigger:** Southbound driver heartbeat failed N times (default: 3 missed).

**Actions:**
- Notify all child NicActors → they park in WAITING_DEVICE
- Start reconnect loop (exponential backoff)
- DO NOT recompose or reprogram while disconnected

### 6.5 DISCONNECTED → READY

**Trigger:** Driver heartbeat restored AND device content_hash compatible.

**Actions:**
- Wake NicActors → they re-verify state and resume PROGRAMMING or stay READY
- If device content_hash drifted: trigger reconciliation

### 6.6 READY → DEGRADED

**Trigger:** Critical registry permanent error (e.g., GlobalRegistry watch failed and unrecoverable).

**Actions:**
- New ENIs rejected (return 503 from API gateway)
- Existing ENIs continue operating with last-known state
- Operator alert emitted

### 6.7 * → DRAINING

**Trigger:** Operator calls `DELETE /api/v1/devices/{id}` (graceful) or `POST /api/v1/devices/{id}/drain`.

**Actions:**
- Stop accepting new ENIs
- For each child NicActor: send DELETE (reverse-wave teardown)
- Wait for all NicActors to reach DELETED state
- Transition to DEREGISTERED

### 6.8 DRAINING → DEREGISTERED

**Trigger:** Last NicActor terminated.

**Actions:**
- Release all registry subscriptions (Acquire counterparts)
- Remove device entry from T1
- Mark T2 `/fm/v1/devices/{device_id}/status = DEREGISTERED`
- HDO actor terminates
- After grace (60s): device entry purged from T1; REST returns 404

---

## 7. CONCURRENT DEVICE REGISTRATION

### 7.1 Pool Behavior

Multiple devices may register simultaneously. Each gets its own HDO actor.

**Critical:** HDO instances DO NOT share state. Each independently:
- Subscribes to its own `/config/v1/hosts/{device_id}/**`
- Acquires from shared GlobalRegistry, GroupRegistry (refcounted)
- Programs only its own device

### 7.2 First-Device-First-Pool-Ready

Old design ambiguity: "when does pool begin programming?" — RESOLVED: each HDO begins programming independently when its own state reaches READY. There is no pool-level gating.

### 7.3 Backpressure

If 1000 devices register in a burst:
- API gateway rate-limits at L7 (default: 100 req/s/pod)
- HDO actors created in batches (default: 50 concurrent inits)
- Excess registrations queue with `202 Accepted, Location: /api/v1/registrations/{txn_id}` for status polling

---

## 8. RECONCILIATION INTEGRATION

When in READY state, HDO triggers reconciliation:
- Every 60s (configurable), call `driver.ReadContentHash(eni_id)` for each owned ENI
- Compare with FM's last-applied hash (stored in T2)
- If drift: notify NicActor → enter COMPOSING → reapply

In DISCONNECTED state: skip reconciliation
In DEGRADED state: best-effort reconciliation, no auto-reapply

See `reconciliation-design.md` for detailed protocol (separate doc).

---

## 9. METRICS (REQUIRED)

```
fm_hdo_state{device_id, state} (gauge: 1 for current state, 0 for others)
fm_hdo_state_transitions_total{device_id, from, to}
fm_hdo_hydration_duration_ms{device_id} (histogram: time from INITIALIZING to READY)
fm_hdo_disconnect_total{device_id}
fm_hdo_reconnect_duration_ms{device_id} (histogram)
fm_device_status_total{status="REGISTERING|REGISTERED|READY_FOR_ENI|DEGRADED|OFFLINE|DRAINING|DEREGISTERED"}
```

---

## 10. TEST MATRIX

| Test | Behavior Asserted |
|------|-------------------|
| DLC-001 | Cold start: INITIALIZING → WAITING_BOOTSTRAP → HYDRATING_DEV → READY |
| DLC-002 | NicActor cannot compose while parent in WAITING_BOOTSTRAP |
| DLC-003 | NicActor unblocks when parent transitions to READY |
| DLC-004 | Bootstrap timeout: stuck in WAITING_BOOTSTRAP >30s → emit metric, retry |
| DLC-005 | Device disconnect → all child NICs to WAITING_DEVICE |
| DLC-006 | Reconnect: NICs resume from WAITING_DEVICE, no recomposition unless drift |
| DLC-007 | Drain: outstanding NICs deleted in reverse-wave order |
| DLC-008 | REST status accurately reflects internal state at all times |
| DLC-009 | subscription_topics NOT present in REST response |
| DLC-010 | 100 concurrent device registrations: each HDO independently reaches READY |
| DLC-011 | DEGRADED: existing NICs continue, new ENI requests rejected |
| DLC-012 | DEREGISTERED: device entry removed from T1 after grace |

---

## 11. MIGRATION FROM EXISTING DOCS

| Doc | Change |
|-----|--------|
| `fm-device-registration-flow-design.md` §6 | Remove states list; reference this doc |
| `fleet-manager-lld.md` §1.1 (HDO state) | Update to match §3 state machine here |
| `fleet-manager-rest-api.md` | Remove subscription_topics from response schema |
| `fm-pod-lifecycle-design.md` | Verify HDO lifecycle aligns with this doc |

---

## 12. REFERENCES

- `fm-device-registration-flow-design.md` — REST API spec (status mapping)
- `registry-semantics-exact.md` — Ready channel semantics used here
- `nicgoalstate-schema-design.md` §6 — NicActor states triggered by HDO state
- `southbound-driver-interface-redesign.md` §8 — Connection lifecycle integration
- `adapter-protocol-design.md` — How device updates land in T1
