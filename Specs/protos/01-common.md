# `common.proto` — Shared Primitives

**Source:** [`protos/common.proto`](../../protos/common.proto)
**Imports:** `google/protobuf/timestamp.proto`, `google/protobuf/duration.proto`

Foundation file: every other proto in the package depends on this. Contains
identity wrappers, network primitives, trace context, error envelopes,
pagination, and audit metadata. Nothing here is FleetManager-specific —
these are the building blocks reused across `models`, `delta`, and `service`.

---

## 1. Identifiers

### `Uuid`

| Field | Type | Purpose |
|-------|------|---------|
| `value` | `string` | Canonical RFC 4122 form, e.g. `"550e8400-e29b-41d4-a716-446655440000"` |

Used as the **internal surrogate key** for every persistent object
(`HostDeviceObject.uuid`, `ContainerObject.uuid`, `NICObject.uuid`). Stored
as a string so it round-trips through JSON without bytes encoding. Generated
server-side at object creation; never set by the caller.

### `DeviceId`

| Field | Type | Purpose |
|-------|------|---------|
| `value` | `string` | Stable, human-meaningful id (e.g. `"host-12345"`) |

The **caller-supplied primary key** for a device. Used as:
- RocksDB row-key prefix (one row per object under this device).
- PubSub topic suffix: `/config/hosts/{device_id}`.
- Consistent-hash input for shard assignment: `xxhash64(device_id) % shard_count`.
- URL path parameter on REST: `GET /api/v1/devices/{device_id}`.

### `ShardId`

| Field | Type | Purpose |
|-------|------|---------|
| `value` | `uint32` | Logical shard within the FleetManager fleet |

Computed server-side at registration. Devices migrate across shards only
during a planned rebalance. Returned to callers so they know which replica
endpoint owns this device's hot path.

---

## 2. Network Primitives

These mirror `dash.types` so non-DASH callers (e.g. a heartbeat-only client)
don't need the upstream proto tree just to send an IP address.

### `IpVersion` (enum)

| Value | Meaning |
|-------|---------|
| `IP_VERSION_UNSPECIFIED` (0) | Default; treat as missing |
| `IP_VERSION_V4` (1) | 4-byte octets |
| `IP_VERSION_V6` (2) | 16-byte octets |

### `IpAddress`

| Field | Type | Purpose |
|-------|------|---------|
| `version` | `IpVersion` | Disambiguates `octets` length |
| `octets` | `bytes` | Network-byte-order packed bytes (4 for v4, 16 for v6) |

JSON marshalling collapses both fields to a single dotted/colon string for
readability (`"10.0.0.5"`, `"2001:db8::1"`).

### `IpPrefix`

| Field | Type | Purpose |
|-------|------|---------|
| `address` | `IpAddress` | Base address |
| `prefix_length` | `uint32` | CIDR prefix length |

### `MacAddress`

| Field | Type | Purpose |
|-------|------|---------|
| `octets` | `bytes` | 6 bytes, big-endian |

JSON form: `"aa:bb:cc:dd:ee:ff"`.

---

## 3. Trace Context

### `TraceContext` — W3C Trace Context, propagated end-to-end

| Field | Type | Purpose |
|-------|------|---------|
| `trace_id` | `string` | 32-char hex (16 bytes); identifies the entire distributed operation |
| `span_id` | `string` | 16-char hex (8 bytes); identifies the immediate parent span |
| `trace_state` | `string` | W3C `tracestate` header value, opaque vendor key=value pairs |
| `trace_flags` | `uint32` | W3C `traceflags` octet; bit 0 = sampled |

Embedded on **every request message** in `service.proto`. Critical for
debugging: a single registration call can be correlated through
PubSub publish, actor scheduling, southbound HAL programming, and any
emitted `FleetEvent`.

REST surfaces this via the `X-Trace-ID` request header; `RESTServiceImpl`
copies it into the proto before invoking the shared backend handler.

---

## 4. Errors

FleetManager prefers gRPC status codes for transport errors and embeds a
structured `ErrorDetail` in `google.rpc.Status.details` (or in the response
body for streaming RPCs that can't surface trailing metadata).

### `ErrorCode` (enum)

| Value | gRPC code | HTTP status | Meaning |
|-------|-----------|-------------|---------|
| `INVALID_REQUEST` | `INVALID_ARGUMENT` | 400 | Malformed request |
| `VALIDATION_ERROR` | `INVALID_ARGUMENT` | 400 | Field validation failed |
| `DEVICE_NOT_FOUND` | `NOT_FOUND` | 404 | Device does not exist |
| `DEVICE_EXISTS` | `ALREADY_EXISTS` | 409 | Device already registered |
| `CONFLICT` | `FAILED_PRECONDITION` | 409 | Operation conflicts with current state |
| `PRECONDITION` | `FAILED_PRECONDITION` | 412 | `expected_revision` mismatch |
| `UNAUTHORIZED` | `UNAUTHENTICATED` | 401 | Missing/invalid auth |
| `FORBIDDEN` | `PERMISSION_DENIED` | 403 | Insufficient scope |
| `RATE_LIMITED` | `RESOURCE_EXHAUSTED` | 429 | Too many requests |
| `NOT_PRIMARY` | `UNAVAILABLE` | 503 | Caller hit standby; retry on primary |
| `SHARD_MISMATCH` | `FAILED_PRECONDITION` | 409 | Device belongs to a different shard |
| `INTERNAL` | `INTERNAL` | 500 | Unexpected server error |
| `UNAVAILABLE` | `UNAVAILABLE` | 503 | Service temporarily down |
| `TIMEOUT`, `DEADLINE_EXCEEDED` | `DEADLINE_EXCEEDED` | 504 | Operation took too long |

### `ErrorDetail`

| Field | Type | Purpose |
|-------|------|---------|
| `code` | `ErrorCode` | Structured error category (see table above) |
| `message` | `string` | Human-readable description |
| `trace_id` | `string` | Echo of the request `TraceContext.trace_id` for log correlation |
| `timestamp` | `Timestamp` | When the error was generated |
| `field_errors` | `map<string,string>` | Per-field diagnostics (`"device_id" -> "must be non-empty"`) |
| `redirect_endpoint` | `string` | For `NOT_PRIMARY`: the endpoint currently holding the lease |
| `retry_after` | `Duration` | For `RATE_LIMITED`: how long to back off |

---

## 5. Pagination

### `PageRequest`

| Field | Type | Purpose |
|-------|------|---------|
| `limit` | `uint32` | Max items to return (server may cap; default 50, max 1000) |
| `cursor` | `string` | Opaque cursor from previous response; empty for first page |

### `PageResponse`

| Field | Type | Purpose |
|-------|------|---------|
| `next_cursor` | `string` | Empty when no further pages exist |
| `total_count` | `int64` | Best-effort total; `-1` when the server cannot compute cheaply |

Cursor format is server-defined and opaque to clients. Current
implementation: base64 of `(shard_id, device_id)` of the last returned row.

---

## 6. Audit Metadata

### `AuditMetadata` — stamped onto every persistent object

| Field | Type | Purpose |
|-------|------|---------|
| `created_at` | `Timestamp` | Set once at object creation |
| `updated_at` | `Timestamp` | Refreshed on every mutation |
| `revision` | `uint64` | Monotonic counter; increments per mutation. Drives optimistic concurrency |
| `last_modified_by` | `string` | Identity of the last mutator (service-account / bearer-token subject) |

`revision` is the contract for `expected_revision` checks on `UpdateDevice`,
`DeregisterDevice`, etc. A client reads `audit.revision`, mutates, and submits
the original revision in the next write — server rejects with `PRECONDITION`
if anyone else moved it forward in between.
