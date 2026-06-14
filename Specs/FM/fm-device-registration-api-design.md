# FM Device Registration API Design

> **Status:** Draft v1
> **Module:** Fleet Manager (FM)
> **Focus:** Device registration endpoint with idempotency, multi-replica coordination, and production semantics
> **Extends:** `fleet-manager-rest-api.md` §2.1

## 1. Overview

This document specifies production-grade semantics for the device registration API endpoint. It covers:
- Idempotency key for safe retry behavior
- Content-hash deduplication across all 3 replicas
- CAS (compare-and-swap) write semantics
- 409 Conflict response with existing registration details
- Replica routing and load balancing guidance
- Rate limiting and quota enforcement

## 2. Endpoint Specification

### 2.1 Register Device (Primary Endpoint)

**HTTP Method:** `POST`  
**Path:** `/api/v1/devices`  
**Content-Type:** `application/json`

### 2.2 Request Schema

```json
{
  "device_id": "string (required)",
  "device_name": "string (optional)",
  "device_type": "string (required)",
  "host_id": "string (optional)",
  "idempotency_key": "string (required, UUID v4)",
  "properties": {
    "cpu_cores": "integer",
    "memory_gb": "integer",
    "bandwidth_gbps": "integer",
    "firmware_version": "string",
    "serial_number": "string",
    "location": "string",
    "custom_fields": "object (arbitrary)"
  }
}
```

### 2.3 Request Field Definitions

| Field | Type | Required | Notes |
|-------|------|----------|-------|
| `device_id` | string | Yes | Unique device identifier (alphanumeric + hyphens; max 128 chars) |
| `device_name` | string | No | Human-readable name; may differ from device_id |
| `device_type` | string | Yes | One of: `dpu`, `appliance`, `nic-card` (extensible) |
| `host_id` | string | No | Parent host/VM ID for DPU attached to host |
| `idempotency_key` | string | Yes | UUID v4; ensures retry safety. All requests with same key are considered identical. |
| `properties` | object | No | Extensible properties; included in content-hash computation |

**Validation Rules:**
- `device_id`: Non-empty; match pattern `^[a-zA-Z0-9-_]+$`; max 128 chars.
- `idempotency_key`: Valid UUID v4 format.
- `properties`: Arbitrary JSON; max size 10 KB.

### 2.4 Response: Success (201 Created)

```json
{
  "device_id": "dpu-1234",
  "device_name": "DPU-in-AZ1-Rack3",
  "device_type": "dpu",
  "shard_id": 2,
  "replica_pod": "fm-2",
  "registered_by_replica": 0,
  "content_hash": "sha256:abcd1234567890abcdef1234567890abcdef1234567890abcdef1234567890ab",
  "idempotency_key": "550e8400-e29b-41d4-a716-446655440000",
  "subscription_topics": [
    "/dashfabric/v1/config/vnets/**",
    "/dashfabric/v1/config/mappings/**",
    "/dashfabric/v1/config/routes/**",
    "/dashfabric/v1/config/groups/**",
    "/dashfabric/v1/config/ha/**"
  ],
  "created_at_unix_ms": 1718400000000,
  "status": "registered",
  "trace_id": "550e8400-e29b-41d4-a716-446655440001"
}
```

**Response field definitions:**

| Field | Meaning |
|-------|---------|
| `device_id` | Echo of request device_id |
| `shard_id` | Assigned shard (0, 1, 2, ... computed via rendezvous hash) |
| `replica_pod` | FM pod name that holds adapter lease when registration completed (for debug) |
| `registered_by_replica` | Ordinal of replica that returned this response (0, 1, or 2) |
| `content_hash` | SHA-256 of request payload; used for idempotency verification |
| `subscription_topics` | Topics device should subscribe to for goal-state updates |
| `created_at_unix_ms` | Timestamp when device was registered (Unix milliseconds) |
| `status` | Always `"registered"` on 201 |
| `trace_id` | W3C Trace Context ID for request correlation |

**HTTP Headers (Response):**
```
Content-Type: application/json
X-Trace-ID: <trace_id>
X-Shard-ID: 2
X-Replica-Pod: fm-2
Cache-Control: no-cache
```

### 2.5 Response: Conflict — Already Registered (409 Conflict)

**Scenario:** Device already registered with different content (different properties).

```json
{
  "error": {
    "code": "DEVICE_ALREADY_REGISTERED",
    "message": "Device dpu-1234 already registered with different properties",
    "trace_id": "550e8400-e29b-41d4-a716-446655440002"
  },
  "device_id": "dpu-1234",
  "existing_registration": {
    "content_hash": "sha256:xyz789...",
    "shard_id": 2,
    "created_at_unix_ms": 1718399900000,
    "device_name": "DPU-in-AZ1",
    "device_type": "dpu"
  },
  "received_content_hash": "sha256:abc123...",
  "conflict_reason": "properties differ"
}
```

**When returned:**
- Device already exists in T1 etcd.
- CAS write failed (content_hash mismatch).
- Replica queried existing registration and returns details to help client debug.

**Client action:** 
- **Option A:** Delete and re-register with new device_id.
- **Option B:** Retry with original properties and same `idempotency_key` (if it was a transient typo).
- **Option C:** Accept the existing registration (call GET endpoint to retrieve it).

### 2.6 Response: Idempotent Success (201 Created) — Retry Safety

**Scenario:** Device already registered; client retries with same `idempotency_key` and identical properties.

```json
{
  "device_id": "dpu-1234",
  "status": "registered",
  "content_hash": "sha256:abcd1234...",
  "idempotency_match": true,
  "message": "Device registration already exists with identical content (idempotent retry)",
  "created_at_unix_ms": 1718400000000,
  "trace_id": "550e8400-e29b-41d4-a716-446655440003"
}
```

**HTTP Status:** `201 Created` (same as first registration).

**Why 201 (not 200)?** Semantically: "the resource is now registered" (same outcome as fresh registration). HTTP spec allows 201 for idempotent operations.

**Client behavior:** Retry is transparent; client sees same successful response.

### 2.7 Response: Validation Error (400 Bad Request)

```json
{
  "error": {
    "code": "INVALID_REQUEST",
    "message": "device_id is required and must be non-empty",
    "field": "device_id",
    "trace_id": "550e8400-e29b-41d4-a716-446655440004"
  }
}
```

**Possible error codes:**
- `INVALID_DEVICE_ID` — device_id format invalid or empty
- `INVALID_IDEMPOTENCY_KEY` — idempotency_key not a valid UUID v4
- `MISSING_REQUIRED_FIELD` — device_type missing
- `PAYLOAD_TOO_LARGE` — properties exceed 10 KB
- `INVALID_JSON` — malformed JSON body

### 2.8 Response: Service Unavailable (503 Service Unavailable)

```json
{
  "error": {
    "code": "SERVICE_UNAVAILABLE",
    "message": "FM unable to reach T1 etcd cluster. Please retry.",
    "reason": "t1_etcd_unreachable",
    "retry_after_seconds": 5,
    "trace_id": "550e8400-e29b-41d4-a716-446655440005"
  }
}
```

**When returned:**
- T1 etcd cluster unavailable (network partition, cluster recovering).
- FM replica is unable to perform CAS write.

**Client action:** Retry after `retry_after_seconds` (exponential backoff recommended).

**Idempotency guarantee:** If retry uses same `idempotency_key`, registration is safe even after T1 recovers.

### 2.9 Response: Rate Limited (429 Too Many Requests)

```json
{
  "error": {
    "code": "RATE_LIMITED",
    "message": "Device registration rate limit exceeded",
    "limit": 1000,
    "limit_window_seconds": 60,
    "requests_in_window": 1001,
    "retry_after_seconds": 10,
    "trace_id": "550e8400-e29b-41d4-a716-446655440006"
  }
}
```

**Rate Limits (configurable):**
- Per-pod: 1000 registrations/min
- Per-IP: 100 registrations/min
- Per-idempotency_key: 10 identical requests/min (duplicate filtering)

## 3. Multi-Replica Coordination Semantics

### 3.1 Load Balancing Strategy

**Recommended:** Round-robin across all 3 FM replicas.

```
Client request → Load Balancer
                    ├→ [50% chance] fm-0
                    ├→ [50% chance] fm-1
                    └→ [50% chance] fm-2
```

**Alternative:** Hash-based routing (consistent hashing of device_id).
- Pro: Same device always routes to same replica (warm cache on retry).
- Con: Less even load distribution if devices are clustered.

### 3.2 CAS Write Conflict Resolution

**All 3 replicas execute identical logic:**

1. **Parse & validate request** (local, deterministic).
2. **Compute content_hash** (SHA-256; all replicas compute same hash for identical input).
3. **Compute shard_id** (rendezvous hash; all replicas compute same shard for same device_id).
4. **Attempt CAS write to T1 etcd:**
   - If key doesn't exist: First writer wins; CAS succeeds; return 201.
   - If key exists with same content_hash: Idempotent success; return 201.
   - If key exists with different content_hash: Conflict; return 409.

### 3.3 No Direct Inter-Replica Communication

**Design principle:** Replicas do NOT coordinate directly.

- No gRPC calls between replicas.
- No pub-sub channels for registration events.
- All coordination flows through T1 etcd (CAS writes, watches).

**Benefit:** Tolerates network partitions; no quorum required for local logic.

### 3.4 Scenario: All 3 Replicas Receive Same Request Simultaneously

**Timeline:**

| Time | Replica | Action | T1 CAS Result |
|------|---------|--------|---------------|
| T=0ms | FM-0 | Receives request; computes hash; sends CAS | CAS #1 queued |
| T=0ms | FM-1 | Receives request; computes hash; sends CAS | CAS #2 queued |
| T=0ms | FM-2 | Receives request; computes hash; sends CAS | CAS #3 queued |
| T=5ms | T1 etcd | Processes CAS writes (first-come-first-served) | CAS #1 succeeds; #2 & #3 fail |
| T=5ms | FM-0 | CAS succeeded | Returns 201 Created |
| T=5ms | FM-1 | CAS failed; queries existing record | Returns 409 Conflict (or 201 if content matches) |
| T=5ms | FM-2 | CAS failed; queries existing record | Returns 409 Conflict (or 201 if content matches) |

**Client sees:** One 201, two 409 (if request routed to all 3).  
**Guarantee:** Exactly one successful 201; all others are conflict/idempotent. No duplication.

### 3.5 Scenario: Replica Crashes During Registration

**Timeline:**

| Time | Event |
|------|-------|
| T=0s | FM-0 receives request; computes hash; sends CAS to T1 |
| T=1s | FM-0 crashes before returning response |
| T=5s | Client times out (no response from FM-0) |
| T=10s | Client retries registration (same device_id, same idempotency_key, same properties) |
| T=10s | Request routes to FM-1 (load balancer) |
| T=10s | FM-1 computes identical content_hash |
| T=10s | FM-1 CAS write to T1: key already exists (from FM-0's prior write); content_hash matches |
| T=10s | T1: CAS succeeds (idempotent) |
| T=10s | FM-1 returns 201 Created |
| T=10s | Client sees success; request completes |

**Guarantee:** Despite FM-0 crash, device is registered (durable in T1); client can retry and see success.

## 4. Content-Hash Computation

### 4.1 Deterministic Hashing

**Goal:** All replicas compute the same hash for identical input.

**Algorithm:**

```
content_hash = SHA256(
  "device_registration" +                 // type marker
  device_id +                              // key
  device_type +                            // immutable
  device_name +                            // optional; empty string if not provided
  sorted_json(properties)                  // properties sorted by key
)
```

**Example:**
```
Input:
  device_id: "dpu-1234"
  device_type: "dpu"
  device_name: "DPU-AZ1"
  properties: { location: "us-east-1a", cpu_cores: 8, memory_gb: 16 }

Canonical form:
  "device_registrationdpu-1234dpuDPU-AZ1{cpu_cores:8,location:us-east-1a,memory_gb:16}"

SHA256: sha256:abcd1234567890abcdef1234567890abcdef1234567890abcdef1234567890ab
```

### 4.2 Idempotency Key vs. Content Hash

**Idempotency key:** Client-provided UUID; identifies the logical operation.  
**Content hash:** Computed by FM; verifies the payload hasn't changed.

| Scenario | Idempotency Key | Content Hash | Result |
|----------|-----------------|--------------|--------|
| First request | uuid-1 | hash-A | 201: device registered with hash-A |
| Retry (identical) | uuid-1 | hash-A | 201: idempotent success (same hash) |
| Retry (modified) | uuid-1 | hash-B | 409: conflict (hash differs) |
| Different request | uuid-2 | hash-A | 201: different operation (same payload is OK) |
| Stale retry after T1 data corruption | uuid-1 | hash-A | 201: idempotent success (if hash still matches) |

## 5. Request/Response Headers

### 5.1 Request Headers

```
POST /api/v1/devices HTTP/1.1
Host: fm.example.com:8080
Content-Type: application/json
Content-Length: 256
X-Trace-ID: 550e8400-e29b-41d4-a716-446655440000
X-Request-ID: req-12345
User-Agent: DeviceAgent/1.0
```

**Required headers:**
- `Content-Type: application/json`

**Optional (recommended):**
- `X-Trace-ID`: W3C Trace Context; enables end-to-end tracing.
- `X-Request-ID`: Client-assigned request ID (for logs).

### 5.2 Response Headers

```
HTTP/1.1 201 Created
Content-Type: application/json
Content-Length: 512
X-Trace-ID: 550e8400-e29b-41d4-a716-446655440001
X-Shard-ID: 2
X-Replica-Pod: fm-2
X-Replica-Ordinal: 2
X-Device-Registration-Time-Ms: 15
Cache-Control: no-cache, no-store, must-revalidate
```

**Standard headers:**
- `Content-Type: application/json`
- `X-Trace-ID`: Echoed from request (or generated).
- `Cache-Control: no-cache` (device registrations should not be cached).

**FM-specific headers:**
- `X-Shard-ID`: Assigned shard for the device.
- `X-Replica-Pod`: Pod name that processed the request.
- `X-Replica-Ordinal`: Pod ordinal (0, 1, or 2).
- `X-Device-Registration-Time-Ms`: Time spent in FM (T1 CAS write latency).

## 6. Rate Limiting & Quota

### 6.1 Rate Limits (Configurable)

**Per-pod (local rate limit):**
- 1000 device registrations/min
- 100 unique devices/min

**Per-IP (global, across all replicas):**
- 100 registrations/min
- 10 unique devices/min

**Per-idempotency_key (duplicate filter):**
- 10 identical requests/min (prevents retry storms)

### 6.2 Quota Enforcement

**Per device_id:**
- 1 registration per device_id (subsequent registrations with same device_id must match content or return 409).

**Per host (if host_id provided):**
- Soft limit: 10,000 devices per host (advisory; not enforced at registration).

### 6.3 Rate Limit Response

```json
{
  "error": {
    "code": "RATE_LIMITED",
    "message": "Rate limit exceeded: 100 registrations/min per IP",
    "limit_type": "per_ip",
    "limit": 100,
    "window_seconds": 60,
    "requests_in_window": 101,
    "retry_after_seconds": 10
  }
}
```

**Client action:** Retry after `retry_after_seconds` (e.g., 10s).

## 7. Observability

### 7.1 Metrics

```
fm_device_registration_total{status,replica}
  description: Total device registrations
  type: counter
  labels:
    - status: success, conflict, error
    - replica: pod ordinal (0, 1, 2)

fm_device_registration_latency_ms{replica,percentile}
  description: Device registration latency
  type: histogram
  labels:
    - replica: pod ordinal
    - percentile: p50, p99, p99.9

fm_device_registration_cas_latency_ms{replica}
  description: T1 CAS write latency
  type: histogram
  labels:
    - replica: pod ordinal

fm_device_registration_content_hash_mismatches{replica}
  description: CAS failures due to content mismatch
  type: counter
  labels:
    - replica: pod ordinal

fm_device_registration_rate_limit_violations{replica}
  description: Rate limit violations
  type: counter
  labels:
    - replica: pod ordinal
    - limit_type: per_ip, per_pod, per_key
```

### 7.2 Logs

```
[INFO] Device registration request: device_id=dpu-1234, trace_id=550e8400..., replica=fm-0
[INFO] Device registered successfully: device_id=dpu-1234, shard_id=2, content_hash=sha256:abcd..., latency_ms=12
[WARN] Device registration conflict: device_id=dpu-1234, existing_hash=sha256:xyz..., new_hash=sha256:abc..., latency_ms=8
[ERROR] Device registration T1 error: device_id=dpu-1234, error=unavailable, retry_after_ms=5000
```

### 7.3 Traces

**Span structure:**

```
device.register (root)
├── validation (parse, hash compute)
├── shard_assign (consistent hash)
├── t1.cas_write (CAS to T1 etcd)
│   ├── t1.connect (if needed)
│   ├── t1.cas_attempt (write + response)
│   └── t1.conflict_resolution (if needed)
├── t3.cache_update (local RocksDB)
└── response (format response)
```

**Span attributes:**

```
device.register:
  device_id: "dpu-1234"
  trace_id: "550e8400..."
  replica: "fm-0"
  shard_id: 2

t1.cas_write:
  topic: "/dashfabric/v1/devices"
  key: "dpu-1234"
  cas_condition_type: "not_exists" | "content_hash_match"
  cas_result: "success" | "conflict" | "error"
  latency_ms: 12
```

## 8. Configuration Knobs

| Knob | Default | Purpose |
|------|---------|---------|
| `FM_DEVICE_REGISTRATION_T1_TIMEOUT_SECONDS` | 5 | T1 CAS write timeout |
| `FM_DEVICE_REGISTRATION_T1_RETRIES` | 3 | Retries on T1 timeout |
| `FM_DEVICE_REGISTRATION_RATE_LIMIT_PER_IP` | 100 | Max registrations/min per IP |
| `FM_DEVICE_REGISTRATION_RATE_LIMIT_PER_POD` | 1000 | Max registrations/min per FM pod |
| `FM_DEVICE_REGISTRATION_RATE_LIMIT_PER_KEY` | 10 | Max identical requests/min per idempotency_key |
| `FM_DEVICE_REGISTRATION_CONTENT_HASH_ALGO` | sha256 | Hash algorithm for deduplication |
| `FM_DEVICE_REGISTRATION_PROPERTIES_MAX_SIZE_KB` | 10 | Max size of properties object |
| `FM_DEVICE_REGISTRATION_ENABLE_IDEMPOTENCY_CHECK` | true | Enforce idempotency key validation |

## 9. Example Workflows

### 9.1 Happy Path: First Registration

**Request:**
```bash
curl -X POST http://fm.example.com:8080/api/v1/devices \
  -H "Content-Type: application/json" \
  -H "X-Trace-ID: trace-123" \
  -d '{
    "device_id": "dpu-1234",
    "device_type": "dpu",
    "idempotency_key": "550e8400-e29b-41d4-a716-446655440000",
    "properties": { "cpu_cores": 8, "memory_gb": 16 }
  }'
```

**Response (201 Created):**
```json
{
  "device_id": "dpu-1234",
  "status": "registered",
  "shard_id": 2,
  "content_hash": "sha256:abcd1234...",
  "created_at_unix_ms": 1718400000000,
  "trace_id": "trace-123"
}
```

### 9.2 Retry After Timeout

**Request #1:** Same as above; times out (no response).

**Request #2 (after 5s delay):** Identical request, same idempotency_key.
```bash
curl -X POST http://fm.example.com:8080/api/v1/devices \
  -H "Content-Type: application/json" \
  -H "X-Trace-ID: trace-124" \
  -d '{
    "device_id": "dpu-1234",
    "device_type": "dpu",
    "idempotency_key": "550e8400-e29b-41d4-a716-446655440000",
    "properties": { "cpu_cores": 8, "memory_gb": 16 }
  }'
```

**Response (201 Created):** Same as Request #1 (idempotent success).

### 9.3 Modification Attempt (Fails)

**Request:** Same device_id, but different properties.
```bash
curl -X POST http://fm.example.com:8080/api/v1/devices \
  -H "Content-Type: application/json" \
  -d '{
    "device_id": "dpu-1234",
    "device_type": "dpu",
    "idempotency_key": "550e8401-e29b-41d4-a716-446655440001",
    "properties": { "cpu_cores": 16, "memory_gb": 32 }
  }'
```

**Response (409 Conflict):**
```json
{
  "error": {
    "code": "DEVICE_ALREADY_REGISTERED",
    "message": "Device dpu-1234 already registered with different properties"
  },
  "existing_registration": {
    "content_hash": "sha256:abcd1234...",
    "shard_id": 2,
    "created_at_unix_ms": 1718400000000
  }
}
```

## 10. References

- `fleet-manager-rest-api.md` — Full REST API surface
- `fm-device-registration-flow-design.md` — Replica coordination and CAS semantics
- `storage-architecture.md` — T1 etcd guarantees
- `fm-pod-lifecycle-design.md` — Pod replicas and failover
