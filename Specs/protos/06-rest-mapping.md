# REST ↔ Proto Mapping

How each proto field surfaces in REST JSON, how headers map to envelope
fields, and how gRPC errors translate to HTTP status codes.

The REST schema is **not** independent — it is a deterministic JSON view of
the same protos, produced by `RESTServiceImpl` (see `fleet-manager-rest-api.md
§6`). Both transports invoke the **same** backend handler.

---

## 1. Shape Conventions

### 1.1 String-wrapper IDs collapse to flat strings

Proto:
```protobuf
message DeviceId  { string value = 1; }
message Uuid      { string value = 1; }
message ShardId   { uint32 value = 1; }
```

JSON:
```json
{ "device_id": "host-12345", "uuid": "550e8400-...", "shard_id": 0 }
```

The wrapper exists in proto so we can extend with metadata later without
breaking wire compat. Today, REST just sees the inner scalar.

### 1.2 `IpAddress` and `MacAddress` collapse to canonical strings

Proto:
```protobuf
message IpAddress  { IpVersion version = 1; bytes octets = 2; }
message MacAddress { bytes octets = 1; }
```

JSON:
```json
{ "host_ip": "10.0.0.5",          // or "2001:db8::1"
  "mac_address": "aa:bb:cc:dd:ee:ff" }
```

The marshaller infers `version` from the dotted/colon syntax.

### 1.3 Enums drop their type prefix

Proto:
```protobuf
enum DeviceType  { DEVICE_TYPE_HOST_DPU_ATTACHED = 2; ... }
enum ObjectState { OBJECT_STATE_INITIALIZING = 1; ... }
```

JSON:
```json
{ "device_type": "HOST_DPU_ATTACHED",
  "state": "INITIALIZING" }
```

Unknown enum strings → `400 VALIDATION_ERROR`.

### 1.4 `Timestamp` ↔ RFC 3339, `Duration` ↔ string seconds

```json
{ "created_at":  "2026-06-11T14:23:45.123Z",
  "retry_after": "5s",
  "uptime":      "86400s" }
```

### 1.5 `map<string,string>` ↔ JSON object

```json
{ "labels":      { "rack": "R12", "az": "us-west-2a" },
  "annotations": { "owner": "team-net" } }
```

### 1.6 `repeated` ↔ JSON array

Direct, no surprises.

### 1.7 Where `TraceContext` lives

Always **out-of-band** in REST: pulled from request headers, returned in
response headers. Never in the JSON body.

| Header | Proto field |
|--------|-------------|
| `X-Trace-ID` request | `TraceContext.trace_id` |
| `X-Trace-State` request | `TraceContext.trace_state` |
| `traceparent` request | parsed into all four `TraceContext` fields per W3C spec |
| `X-Trace-ID` response | echo of `trace_id` for log correlation |

### 1.8 `expected_revision` lives in the `If-Match` header

```http
PUT /api/v1/devices/host-12345
If-Match: "7"
```

is equivalent to gRPC `UpdateDeviceRequest.expected_revision = 7`. `0` (or
omitting `If-Match`) skips the check.

### 1.9 `FieldMask` ↔ comma-separated query parameter

Proto:
```protobuf
google.protobuf.FieldMask update_mask = 4;
```

REST:
```http
PUT /api/v1/devices/host-12345?update_mask=hardware_capabilities.max_acl_rules
```

For `read_mask` on `GET`, same syntax (`?read_mask=device_id,state`).

---

## 2. Endpoint-by-Endpoint Mapping

Every REST endpoint maps to exactly one unary RPC. Streaming RPCs do not
have REST counterparts (REST clients should use the gRPC API or upgrade to
WebSocket if streaming is required).

| HTTP method + path | gRPC RPC |
|---|---|
| `POST /api/v1/devices` | `RegisterDevice` |
| `GET /api/v1/devices` | `ListDevices` |
| `GET /api/v1/devices/{device_id}` | `GetDevice` |
| `PUT /api/v1/devices/{device_id}` | `UpdateDevice` |
| `DELETE /api/v1/devices/{device_id}` | `DeregisterDevice` |
| `POST /api/v1/devices/{device_id}/heartbeat` | `Heartbeat` |
| `POST /api/v1/devices/{device_id}/telemetry` | `PushTelemetry` |
| `GET /api/v1/devices/{device_id}/state` | `GetDeviceState` |
| `GET /api/v1/devices/{device_id}/objects` | `ListDeviceObjects` |
| `GET /api/v1/devices/{device_id}/deltas` | `ListPendingDeltas` |
| `POST /api/v1/devices/{device_id}/goal-state` | `ApplyGoalState` |
| `POST /api/v1/devices/{device_id}/reconcile` | `Reconcile` |
| `GET /api/v1/health` | `Health` |
| `GET /api/v1/metrics` | (Prometheus exposition; not an RPC) |
| `GET /api/v1/swagger.json` | (Static OpenAPI doc) |

---

## 3. Request Body ↔ Proto Field Mapping

Below: each REST endpoint's JSON body keys mapped to the proto field path
they populate. Server-only fields (e.g. `host.audit`, `host.shard_id`) are
not accepted on input — sending them is silently ignored or, in strict mode,
rejected with `VALIDATION_ERROR`.

### 3.1 `POST /api/v1/devices`  →  `RegisterDeviceRequest`

| JSON path | Proto path |
|-----------|-----------|
| `device_id` | `profile.device_id.value` |
| `device_type` | `profile.device_type` (enum, prefix stripped) |
| `host_ip` | `profile.host_ip` (parsed from string) |
| `hardware_capabilities.*` | `profile.hardware_capabilities.*` (1:1 snake_case) |
| `labels` | `profile.labels` |
| `annotations` | `profile.annotations` |
| `subscription_prefix` | `profile.subscription_prefix` |
| `X-Trace-ID` header | `trace.trace_id` |

### 3.2 `PUT /api/v1/devices/{id}`  →  `UpdateDeviceRequest`

| JSON path | Proto path |
|-----------|-----------|
| Path `{id}` | `device_id.value` |
| Query `?update_mask=...` | `update_mask` |
| `If-Match` header | `expected_revision` |
| Body keys (any subset of HostDeviceObject spec fields) | `host.*` |

### 3.3 `DELETE /api/v1/devices/{id}`  →  `DeregisterDeviceRequest`

| Source | Proto field |
|--------|-------------|
| Path `{id}` | `device_id.value` |
| Query `?force=true` | `force` |
| Query `?drain_timeout=30s` | `drain_timeout` |

### 3.4 `POST /api/v1/devices/{id}/heartbeat`  →  `HeartbeatRequest`

| JSON path | Proto path |
|-----------|-----------|
| `timestamp` | `payload.timestamp` |
| `health` (enum) | `payload.health` |
| `cpu_usage_percent`, `memory_usage_percent` | `payload.*` |
| `active_connections`, `pps_in`, `pps_out` | `payload.*` |
| `extensions` | `payload.extensions` |

### 3.5 `POST /api/v1/devices/{id}/telemetry`  →  `PushTelemetryRequest`

| JSON path | Proto path |
|-----------|-----------|
| `timestamp` | `payload.timestamp` |
| `container_stats[]` | `payload.container_stats` |
| `nic_stats[]` | `payload.nic_stats` |

### 3.6 `POST /api/v1/devices/{id}/goal-state`  →  `ApplyGoalStateRequest`

| JSON path | Proto path |
|-----------|-----------|
| `revision` | `goal_state.revision` |
| `host` | `goal_state.host` |
| `containers[]`, `nics[]` | `goal_state.containers`, `goal_state.nics` |
| `dash_objects[]` | `goal_state.dash_objects` (each entry is `{ "kind": "...", "value": {...} }`) |
| `plan[]` | `goal_state.plan` (optional pre-computed) |
| Query `?dry_run=true` | `dry_run` |

### 3.7 `POST /api/v1/devices/{id}/reconcile`  →  `ReconcileRequest`

| Source | Proto field |
|--------|-------------|
| Query `?apply=true` | `apply` |

### 3.8 List endpoints  →  cursor pagination

```
GET /api/v1/devices?limit=50&cursor=<opaque>&shard_id=0&state=READY&label_selector=rack=R12
```

| Query param | Proto field |
|-------------|-------------|
| `limit` | `page.limit` |
| `cursor` | `page.cursor` |
| `shard_id`, `state`, `device_type`, `label_selector` | filters |

---

## 4. Response Body ↔ Proto Field Mapping

Same shape conventions as §1. Examples:

`HostDeviceObject` (`GetDevice` response):
```json
{
  "uuid":       "550e8400-e29b-41d4-a716-446655440000",
  "device_id":  "host-12345",
  "shard_id":   0,
  "state":      "READY",
  "health":     "HEALTHY",
  "profile":    { ... DeviceProfile ... },
  "container_count": 5,
  "nic_count":       12,
  "allocated_memory_bytes": 268435456,
  "allocated_cpu_cores":    8,
  "active_flows":           1234,
  "last_heartbeat":  "2026-06-11T14:35:12.456Z",
  "last_reconcile":  "2026-06-11T14:30:00.000Z",
  "state_hash":      "0xa1b2c3d4e5f6",
  "audit": {
    "created_at":       "2026-06-11T14:23:45.123Z",
    "updated_at":       "2026-06-11T14:35:12.456Z",
    "revision":         7,
    "last_modified_by": "system:fleet-controller"
  }
}
```

`DeltaCommand` (entry of `ListPendingDeltas` response):
```json
{
  "command_id":   "cmd-001",
  "trace":        { "trace_id": "550e8400-..." },
  "goal_revision": 42,
  "operation":    "CREATE",
  "target_type":  "NIC",
  "target_id":    "eth0",
  "device_id":    "host-12345",
  "depends_on":   ["cmd-000"],
  "max_retries":  3,
  "retry_backoff": "1s",
  "timeout":      "30s",
  "status":       "PENDING",
  "retry_count":  0,
  "created_at":   "2026-06-11T14:30:00.123Z"
}
```

`google.protobuf.Any` payloads (`desired`, `current`) marshal as:
```json
{
  "@type": "type.googleapis.com/fleetmanager.v1.NICObject",
  "uuid":  "...",
  "nic_id": "eth0",
  ...
}
```

---

## 5. Error Mapping

Server returns gRPC `Status{ code, details=[ErrorDetail] }`. REST translates:

| `ErrorCode` | gRPC code | HTTP status |
|-------------|-----------|-------------|
| `INVALID_REQUEST`, `VALIDATION_ERROR` | `INVALID_ARGUMENT` | 400 |
| `UNAUTHORIZED` | `UNAUTHENTICATED` | 401 |
| `FORBIDDEN` | `PERMISSION_DENIED` | 403 |
| `DEVICE_NOT_FOUND` | `NOT_FOUND` | 404 |
| `DEVICE_EXISTS` | `ALREADY_EXISTS` | 409 |
| `CONFLICT` | `FAILED_PRECONDITION` | 409 |
| `PRECONDITION` | `FAILED_PRECONDITION` | 412 |
| `RATE_LIMITED` | `RESOURCE_EXHAUSTED` | 429 |
| `INTERNAL` | `INTERNAL` | 500 |
| `UNAVAILABLE`, `NOT_PRIMARY` | `UNAVAILABLE` | 503 |
| `TIMEOUT`, `DEADLINE_EXCEEDED` | `DEADLINE_EXCEEDED` | 504 |

Body shape (every error, every endpoint):
```json
{
  "error": {
    "code":    "DEVICE_ALREADY_EXISTS",
    "message": "Device host-12345 already registered",
    "trace_id": "550e8400-e29b-41d4-a716-446655440000",
    "timestamp": "2026-06-11T14:23:45.123Z",
    "field_errors": { "device_id": "must be unique within shard" },
    "redirect_endpoint": "fleetmanager-1.svc.cluster.local:8080",
    "retry_after": "5s"
  }
}
```

`redirect_endpoint` and `retry_after` are populated only when relevant.

---

## 6. Why "One Schema, Two Surfaces" Works

- **Validation, shard assignment, persistence, audit stamping** all live in
  one C++ handler invoked by both frontends. No two parsers means no drift.
- The REST spec **cannot** accept a field the proto doesn't define — the
  parse step rejects unknown keys.
- **Observability parity:** the `X-Trace-ID` header on REST and the
  `trace` proto field on gRPC both feed the same `TraceContext` propagated
  to the actor, the state store, the southbound HAL, and any emitted
  `FleetEvent`.
- New RPCs only need to define request/response messages; REST handlers
  for them are mostly boilerplate (`json -> proto` then `proto -> json`),
  generated from a small template against the proto descriptor.
