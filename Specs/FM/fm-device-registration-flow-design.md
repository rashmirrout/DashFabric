# FM Device Registration Flow Design

> **Status:** Draft v1
> **Module:** Fleet Manager (FM)
> **Topic:** Device (DPU / appliance) registration via 3-replica StatefulSet
> **Audience:** FM implementers, API consumers, integration teams

## 1. Overview

Device registration is the entry point for a new DPU (or bare-metal appliance) to become part of the FM-managed fabric. This document specifies:
- How all 3 FM replicas accept registration requests independently
- Idempotent CAS (compare-and-swap) writes to T1 (fm-data-store) for durability
- Content-hash deduplication across retries
- Replica coordination via T1 etcd (no direct inter-pod messaging)
- Shard assignment determinism (which replica "owns" this DPU)
- State transitions and observability

## 2. High-Level Flow

```
┌─────────┐         Registration Request (device metadata)
│  DPU    │──────────────────────────────┐
└─────────┘                              ▼
                    ┌────────────────────────────────┐
                    │  FM Load Balancer / DNS        │
                    │  (routes to any replica)       │
                    └──────┬─────────┬─────────┬──────┘
                           │         │         │
          ┌────────────────┘         │         └──────────────────┐
          ▼                          ▼                            ▼
      ┌─────────┐              ┌─────────┐                   ┌─────────┐
      │  FM-0   │              │  FM-1   │                   │  FM-2   │
      │Replica-0│              │Replica-1│                   │Replica-2│
      └────┬────┘              └────┬────┘                   └────┬────┘
           │                        │                            │
           │   All 3 try to CAS-write registration to T1        │
           │                        │                            │
           └────────────────────────┼────────────────────────────┘
                                    ▼
                          ┌──────────────────┐
                          │  T1 etcd cluster │
                          │  (authoritative) │
                          └──────────────────┘
```

**Key principle:** All 3 replicas attempt registration; T1's CAS write (first-writer-wins) enforces uniqueness. Idempotency via content-hash ensures retries are safe.

## 3. Device Registration Request Format

**HTTP endpoint:** `POST /api/v1/devices`

**Request body:**
```json
{
  "device_id": "dpu-1234",
  "device_name": "DPU-in-AZ1-Rack3",
  "device_type": "dpu",
  "host_id": "vm-5678",
  "properties": {
    "cpu_cores": 8,
    "memory_gb": 16,
    "bandwidth_gbps": 100,
    "firmware_version": "1.2.3",
    "serial_number": "ABC123XYZ",
    "location": "us-east-1a"
  },
  "idempotency_key": "reg-uuid-123e4567-e89b-12d3-a456-426614174000"
}
```

**Response (success, 201 Created):**
```json
{
  "device_id": "dpu-1234",
  "shard_id": 2,
  "replica_pod": "fm-2",
  "subscription_topics": [
    "/dashfabric/v1/config/vnets/**",
    "/dashfabric/v1/config/mappings/**",
    "/dashfabric/v1/config/routes/**",
    "/dashfabric/v1/config/groups/**"
  ],
  "content_hash": "sha256:abcd1234...",
  "created_at_unix_ms": 1718400000000,
  "registered_replica": 0,
  "status": "registered"
}
```

**Response (conflict, 409 Conflict) — already registered:**
```json
{
  "error": "device already registered",
  "device_id": "dpu-1234",
  "content_hash": "sha256:abcd1234...",
  "existing_registration": {
    "shard_id": 2,
    "created_at_unix_ms": 1718400000000
  }
}
```

## 4. Per-Replica Registration Logic

Each FM replica independently processes registration requests. A single replica handles the following steps:

### 4.1 Request Validation (Local, No T1 access yet)

1. **Parse request:** Deserialize JSON; validate required fields (device_id, device_type, host_id).
   - If parse error: return `400 Bad Request`.

2. **Compute content hash:**
   ```
   content_hash = SHA256(
     device_id + 
     device_type + 
     device_name + 
     sorted(properties) +  // deterministic order
     device_id  // ensure hash is unique per device
   )
   ```
   - All replicas compute the same hash for identical input (deterministic).
   - Store in local memory for this request only (not persisted yet).

3. **Extract idempotency key:** `idempotency_key` (UUID provided by client).
   - Used for deduplication across retries (see 4.5).

4. **Validate idempotency key format:** UUID v4; return `400` if invalid.

5. **Log request:**
   ```
   [INFO] Registration request received: device_id=dpu-1234, shard_id=?, idempotency_key=reg-uuid-...
   ```

### 4.2 Shard Assignment (Deterministic, All Replicas Same Result)

1. **Get active pod list:** Query local in-memory pod list (updated every 10s from T2).
   - Expect 3 pods: fm-0, fm-1, fm-2 (or current `FM_SHARD_COUNT`).
   - If pod count changed recently, this request may get shard_id that differs from next request (acceptable; T1 write is authoritative).

2. **Compute shard ID via rendezvous hash:**
   ```
   shard_id = rendezvous_hash(
     device_id,
     [pod_0_name, pod_1_name, ..., pod_N_name]
   ) % FM_SHARD_COUNT
   ```
   - All replicas compute the same shard_id for this device (deterministic).
   - Example: `device_id="dpu-1234"` hashes to shard_id=2 (replica owns that shard).

3. **Compute subscription topics (based on shard):**
   ```
   subscription_topics = [
     "/dashfabric/v1/config/vnets/**",
     "/dashfabric/v1/config/mappings/**",
     "/dashfabric/v1/config/routes/**",
     "/dashfabric/v1/config/groups/**",
     "/dashfabric/v1/config/ha/**"
   ]
   ```
   - Same for all shards; shard_id used for deterministic replica assignment only.

### 4.3 T1 CAS Write Attempt

1. **Construct Device object for T1:**
   ```proto
   message Device {
     string device_id = 1;
     string device_name = 2;
     string device_type = 3;
     string host_id = 4;
     string shard_id = 5;
     string idempotency_key = 6;
     string content_hash = 7;
     int64 created_at_unix_ms = 8;
     google.protobuf.Struct properties = 9;
     string replica_pod_name = 10;
   }
   ```

2. **Construct T1 event:**
   ```proto
   message Event {
     string topic = 1;  // "/dashfabric/v1/devices"
     string key = 2;    // device_id ("dpu-1234")
     EventType event_type = 3;  // UPSERT
     string watermark = 4;
     int64 producer_ts = 5;
     google.protobuf.Any payload = 6;  // marshalled Device
   }
   ```

3. **CAS write to T1:**
   ```
   Topic: /dashfabric/v1/devices
   Key: device_id
   New value: Event{ ... device ... }
   CAS condition: (key does not exist) OR 
                  (existing value has same content_hash)
   ```

   **Semantics:**
   - **First registration:** Key doesn't exist; CAS succeeds.
   - **Identical retry:** Key exists with same content_hash; CAS succeeds (idempotent).
   - **Different registration (from different replica):** Key exists with different content_hash; CAS fails; local replica returns `409 Conflict`.

4. **Handle CAS result:**

   **Success:**
   - Return `201 Created` to client with full Device details.
   - Log: `[INFO] Device registered: device_id=dpu-1234, shard_id=2, content_hash=sha256:...`.

   **CAS failure (key exists, different content_hash):**
   - Fetch existing event from T1; extract Device object.
   - Compare content_hash.
   - If different: Return `409 Conflict` to client with existing registration details.
   - If same (race with identical request from another replica): Safe; return `201` as if we won.

### 4.4 T3 (Local RocksDB) Caching

**After successful T1 CAS write:**

1. **Cache to local RocksDB:**
   - Store Device object in `devices` CF (column family).
   - Key: `device:{device_id}`
   - Value: marshalled Device event.

2. **Fsync?** No; T3 is ephemeral (warm cache only). Loss on pod restart is acceptable (resync from T1).

3. **Add to in-memory registry (NicRegistry):**
   - Index Device by device_id for fast lookup during HDO actor processing.
   - Mark device as `registered` state.

### 4.5 Idempotency via Content-Hash (Retry Safety)

**Scenario:** Client times out waiting for response; retries registration with same `idempotency_key`.

**Flow:**

1. **Request arrives (retry #2):**
   - Same device_id, same properties, same idempotency_key.
   - Compute content_hash: identical to previous request.

2. **T1 CAS attempt:**
   - Key already exists (from retry #1).
   - CAS condition: `existing value has same content_hash`.
   - CAS succeeds (because content is identical).
   - Idempotency preserved: retry is safe.

3. **Response to client:**
   - Return `201 Created` again (semantically: "device is now registered").
   - Client sees success; request completes.

**Scenario 2:** Client retries but with slightly different properties (e.g., typo corrected).

**Flow:**

1. **Request arrives (retry with modified properties):**
   - Same device_id, but properties differ.
   - Compute content_hash: **different** from original.

2. **T1 CAS attempt:**
   - Key exists with old content_hash.
   - CAS condition: `existing value has new content_hash`.
   - CAS fails.

3. **Response to client:**
   - Return `409 Conflict`.
   - Client must delete and re-register (or provide same idempotency_key with original properties).

## 5. All-Replica Coordination (No Direct Communication)

### 5.1 Three Replicas Handling Same Request Simultaneously

**Scenario:** Load balancer sends registration request to all 3 replicas (or client retries across replicas).

**Timeline:**

| Time | Replica | Action | T1 Result |
|------|---------|--------|-----------|
| T=0ms | FM-0 | Receives req; computes hash; sends CAS to T1 | CAS #1 arrives |
| T=0ms | FM-1 | Receives req; computes hash; sends CAS to T1 | CAS #2 arrives |
| T=0ms | FM-2 | Receives req; computes hash; sends CAS to T1 | CAS #3 arrives |
| T=5ms | T1 | Processes 3 CAS writes (order nondeterministic) | First CAS wins; others fail |
| T=5ms | FM-0 | CAS succeeded (assumed winner) | Returns `201 Created` |
| T=5ms | FM-1 | CAS failed (not winner) | Queries T1; gets existing record; returns `409 Conflict` |
| T=5ms | FM-2 | CAS failed (not winner) | Queries T1; gets existing record; returns `409 Conflict` |

**Client perspective:**
- If request sent to FM-0 only: `201 Created`.
- If request load-balanced to all 3: one `201`, two `409 Conflict`.
- If client implements retry on `409`: eventually sees `201` or `409 conflict` (idempotent).

**Guarantee:** Exactly one replica's response is `201 Created`; all others `409 Conflict`. No resource duplication; T1 is authoritative.

### 5.2 No Inter-Replica Direct Messaging

**Key design principle:** Replicas do NOT communicate directly about device registration.

- No gRPC calls between replicas.
- No pub-sub channel for registration events.
- All coordination flows through T1 (CAS, watch streams).
- **Benefit:** Tolerates network partitions; no quorum required for local logic.
- **Tradeoff:** Replicas may temporarily have different views (observability lag); but T1 is always correct.

### 5.3 Eventual Consistency via T1 Watch

After a successful registration:

1. **FM-0** (primary) publishes registration to T1 `/dashfabric/v1/devices`.

2. **FM-1, FM-2** (standbys) have active T1 watch on `/dashfabric/v1/devices/**`.
   - Within 100ms–1s, they receive the registration event.
   - Update local T3 cache and in-memory NicRegistry.

3. **Result:** All 3 replicas converge to same view of registered devices.
   - Primary acts on the device immediately (if hydration ready).
   - Standbys see the event via watch and warm their cache; ready to take over if primary fails.

## 6. Device Registration State Machine

```
[Requested]
    ↓
[CAS-write-to-T1]
    ├─ Success → [Registered] → [Hydrating] → [Ready-for-ENI]
    └─ Conflict (device exists with same hash) → [Idempotent-success]
    └─ Conflict (device exists with different hash) → [Conflict-error]
```

### 6.1 State Definitions

| State | When | Next | Action |
|-------|------|------|--------|
| **Requested** | Client sends registration POST | CAS-write-to-T1 | Parse, validate, compute hash |
| **CAS-write-to-T1** | Replica attempts T1 write | Registered, Idempotent-success, or Conflict-error | CAS write with device metadata |
| **Registered** | CAS succeeded; key is new | Hydrating | Device object stored in T1; cache in T3; update NicRegistry |
| **Idempotent-success** | CAS succeeded; content_hash matches existing | Return 201 | Retry with identical content detected; safe to succeed |
| **Conflict-error** | CAS failed; existing device has different hash | Return 409 | Different registration attempt; client must resolve |
| **Hydrating** | Device registered; awaiting goal-state | Ready-for-ENI | Registry waits for first HDO/VNET subscription; MappingManager pre-fills peer routes |
| **Ready-for-ENI** | Device hydrated; subscribed to all topics | Programming | Device ready; awaits ENI provisioning requests |
| **Programming** | ENI hydration in progress | Ready | Wave-ordered programming (waves 0-6); DPU applies config |
| **Ready** | Device fully programmed; stable | Programming (on config change) | Device active; ENIs on DPUs; software can communicate |

## 7. Failure Modes & Recovery

### 7.1 T1 Unavailable During Registration

**Scenario:** T1 etcd cluster is partitioned; FM replica cannot reach T1.

**Behavior:**
1. Replica attempts CAS write; timeout after 5s.
2. Returns `503 Service Unavailable` to client.
3. Client should retry (with exponential backoff).
4. **Idempotency guarantee:** If client retries with same `idempotency_key`, even after T1 recovers, the registration is safe (content_hash matches).

**Recovery:**
- T1 recovers; next client retry succeeds.
- Or: client gives up; tries different endpoint (different replica).
- No resources leaked; T1 is source of truth.

### 7.2 Replica Crashes During Registration

**Scenario:** FM-0 receives registration; crashes before returning response.

**Behavior:**
1. Client times out (no response); assumes failure.
2. Client retries (same `idempotency_key`, same properties).
3. If retry goes to FM-1 or FM-2:
   - They compute same content_hash.
   - CAS attempt: T1 already has key (from FM-0's prior successful write).
   - CAS matches content_hash (identical content).
   - Returns `201 Created` (idempotent).
   - Client sees success; request completes.
4. If retry goes to restarted FM-0:
   - Same flow as above.

**Guarantee:** Registration is either durable in T1 or lost; never partially applied. Client sees either `201` or `503`; never undefined state.

### 7.3 Replica Recovers After Reboot (RocksDB May Be Lost)

**Scenario:** FM-0 crashes; RocksDB on local PVC is corrupted; pod restarts.

**Behavior:**
1. FM-0 recovers; deletes corrupted RocksDB.
2. Pod enters Loading phase (see fm-pod-lifecycle-design.md).
3. Reads T1 etcd to rebuild T3 cache and NicRegistry.
4. Existing devices (already registered in T1) are re-added to local cache.
5. New registration requests proceed normally.

**Guarantee:** No re-registration needed; T1 is source of truth.

### 7.4 Concurrent Registrations of Different Devices

**Scenario:** Two different devices (device-A, device-B) register simultaneously.

**Behavior:**
1. Both CAS writes to different keys in T1.
2. Both succeed (different keys; no conflict).
3. Both replicas (if request routed to both) see `201` and `201` (or one `201` and two `409` if same device).
4. T1 contains both devices; T3 cache updated on all replicas within 1s.

**Guarantee:** No cross-device conflict; independent registrations proceed in parallel.

## 8. Observability

### 8.1 Logs

```
[INFO] Device registration request: device_id=dpu-1234, shard_id=2, from_pod=fm-0
[INFO] Device registered successfully: device_id=dpu-1234, content_hash=sha256:abcd1234..., t1_latency_ms=12
[WARN] Device registration conflict: device_id=dpu-1234, existing_hash=sha256:xyz..., new_hash=sha256:abc..., t1_latency_ms=15
[ERROR] Device registration T1 error: device_id=dpu-1234, error=unavailable, pod=fm-0
```

### 8.2 Metrics

- `fm_device_registration_total{device_type, status}` → counter (success, conflict, error)
- `fm_device_registration_latency_ms{percentile}` → histogram (p50, p99, p99.9 latency)
- `fm_devices_registered{pod, shard_id}` → gauge (count of registered devices)
- `fm_t1_cas_write_total{status}` → counter (success, conflict, error)
- `fm_t1_latency_ms{operation}` → histogram (device registration CAS latency)

### 8.3 Traces

- Span: `device.register` (root)
  - Span: `validation` (parse, hash compute)
  - Span: `shard_assign` (consistent hash compute)
  - Span: `t1.cas_write` (CAS write to T1)
  - Span: `t3.cache_update` (RocksDB update)
  - Span: `registry.update` (NicRegistry update)

## 9. Configuration Knobs

| Knob | Default | Purpose |
|------|---------|---------|
| `FM_DEVICE_REGISTRATION_TIMEOUT_SECONDS` | `5` | T1 CAS write timeout per attempt |
| `FM_DEVICE_REGISTRATION_RETRIES` | `3` | Number of retries on T1 timeout |
| `FM_CONTENT_HASH_ALGORITHM` | `sha256` | Hash algo for deduplication |
| `FM_SHARD_COUNT` | `3` | Number of device shards |
| `FM_DEVICE_HYDRATION_TIMEOUT_MINUTES` | `10` | Max time to reach Ready state |
| `FM_IDEMPOTENCY_KEY_VALIDATION` | `true` | Validate UUIDs strictly |

## 10. API Endpoint Reference

### 10.1 Register Device

**Endpoint:** `POST /api/v1/devices`

**Request:**
```json
{
  "device_id": "dpu-1234",
  "device_name": "DPU-in-AZ1",
  "device_type": "dpu",
  "host_id": "vm-5678",
  "idempotency_key": "uuid-v4-here",
  "properties": { ... }
}
```

**Response (201 Created):**
```json
{
  "device_id": "dpu-1234",
  "shard_id": 2,
  "replica_pod": "fm-2",
  "content_hash": "sha256:...",
  "subscription_topics": [ ... ],
  "created_at_unix_ms": 1718400000000,
  "status": "registered"
}
```

### 10.2 List Registered Devices

**Endpoint:** `GET /api/v1/devices?limit=50&offset=0&shard_id=2`

**Response (200 OK):**
```json
{
  "devices": [
    { "device_id": "dpu-1234", "shard_id": 2, "status": "ready", ... },
    { "device_id": "dpu-5678", "shard_id": 2, "status": "hydrating", ... }
  ],
  "total": 1024,
  "limit": 50,
  "offset": 0
}
```

### 10.3 Get Device Details

**Endpoint:** `GET /api/v1/devices/{device_id}`

**Response (200 OK):**
```json
{
  "device_id": "dpu-1234",
  "shard_id": 2,
  "replica_pod": "fm-2",
  "status": "ready",
  "content_hash": "sha256:...",
  "created_at_unix_ms": 1718400000000,
  "properties": { ... },
  "subscribed_topics": [ ... ]
}
```

## 11. References

- `fm-pod-lifecycle-design.md` — Pod lifecycle and shard assignment
- `storage-architecture.md` — T1/T2/T3 tiers; T1 CAS guarantees
- `registry-pattern-design.md` — NicRegistry details
- `recovery-and-failover-design.md` — Failure modes
- `fleet-manager-rest-api.md` — REST API design
- `02-cb-low-level-design-lld.md` — ControllerBridge (T1 backend)
