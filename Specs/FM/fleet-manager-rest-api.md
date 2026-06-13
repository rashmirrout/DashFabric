# FleetManager: REST API Design & Implementation

**Version:** 1.0  
**Framework:** cpp-httplib or Pistache (C++ REST framework)  
**Status:** Design Complete  

---

## 1. REST API Overview

FleetManager exposes a **REST API** on port **8080** alongside the gRPC server (5051), enabling:
- Device registration via HTTP POST
- CRUD operations (Create, Read, Update, Delete) on devices
- Device telemetry and heartbeat endpoints
- Operational diagnostics and health checks
- OpenAPI/Swagger documentation

### 1.1 Design Principles

1. **Stateless**: Each request is independent; no session state maintained
2. **JSON-first**: All request/response payloads use JSON
3. **Trace-aware**: W3C Trace Context propagation via headers
4. **Error standardized**: Consistent error response format
5. **Idempotent**: Device operations are idempotent (safe to retry)

---

## 2. API Endpoints Reference

### 2.1 Device Registration

```
POST /api/v1/devices

Request:
{
  "device_id": "host-12345",
  "device_type": "HOST_DPU_ATTACHED",  // HOST_LINUX | HOST_DPU_ATTACHED | APPLIANCE_DASH_STANDALONE | APPLIANCE_DASH_CHASSIS
  "host_ip": "10.0.0.5",
  "hardware_capabilities": {
    "max_flow_table_entries": 1000000,
    "max_routes_per_eni": 10000,
    "max_acl_rules": 100000,
    "max_nics": 10,
    "max_containers": 100,
    "max_cps": 100000,
    "cpu_cores": 16,
    "memory_gb": 64
  }
}

Response (201 Created):
{
  "device_id": "host-12345",
  "shard_id": "0",
  "status": "INITIALIZING",
  "subscription_topics": [
    "/config/hosts/host-12345"
  ],
  "created_at": "2026-06-11T14:23:45.123Z"
}

Error Response (400):
{
  "error": {
    "code": "INVALID_DEVICE_ID",
    "message": "Device ID must be non-empty string",
    "trace_id": "550e8400-e29b-41d4-a716-446655440000"
  }
}

Error Response (409):
{
  "error": {
    "code": "DEVICE_ALREADY_EXISTS",
    "message": "Device host-12345 already registered",
    "trace_id": "550e8400-e29b-41d4-a716-446655440000"
  }
}
```

### 2.2 List Devices

```
GET /api/v1/devices?limit=50&offset=0&shard_id=0

Query Parameters:
  limit    - Max devices to return (default: 50, max: 1000)
  offset   - Pagination offset (default: 0)
  shard_id - Filter by shard (optional)
  state    - Filter by state (optional: INITIALIZING | READY | DRAINING | TERMINATED)

Response (200 OK):
{
  "devices": [
    {
      "device_id": "host-12345",
      "device_type": "HOST_DPU_ATTACHED",
      "state": "READY",
      "shard_id": 0,
      "container_count": 5,
      "nic_count": 12,
      "created_at": "2026-06-11T14:23:45.123Z",
      "last_heartbeat": "2026-06-11T14:35:12.456Z"
    },
    ...
  ],
  "total_count": 5234,
  "limit": 50,
  "offset": 0
}
```

### 2.3 Get Device Details

```
GET /api/v1/devices/:device_id

Response (200 OK):
{
  "device_id": "host-12345",
  "device_type": "HOST_DPU_ATTACHED",
  "host_ip": "10.0.0.5",
  "state": "READY",
  "shard_id": 0,
  "hardware_capabilities": {
    "max_flow_table_entries": 1000000,
    "max_routes_per_eni": 10000,
    ...
  },
  "device_state": {
    "container_count": 5,
    "nic_count": 12,
    "total_allocated_memory": 268435456,
    "total_allocated_cpu": 8
  },
  "created_at": "2026-06-11T14:23:45.123Z",
  "last_update": "2026-06-11T14:35:12.456Z",
  "last_heartbeat": "2026-06-11T14:35:12.456Z"
}

Error Response (404):
{
  "error": {
    "code": "DEVICE_NOT_FOUND",
    "message": "Device host-12345 not found",
    "trace_id": "550e8400-e29b-41d4-a716-446655440000"
  }
}
```

### 2.4 Update Device

```
PUT /api/v1/devices/:device_id

Request:
{
  "hardware_capabilities": {
    "max_acl_rules": 150000  // Update specific capability
  }
}

Response (200 OK):
{
  "device_id": "host-12345",
  "status": "UPDATED",
  "changes": {
    "max_acl_rules": { "old": 100000, "new": 150000 }
  },
  "updated_at": "2026-06-11T14:40:00.123Z"
}
```

### 2.5 Deregister Device

```
DELETE /api/v1/devices/:device_id

Query Parameters:
  force - Force deletion even if containers exist (default: false)

Response (204 No Content):
(empty body)

Or with `force=true`, returns (200 OK):
{
  "device_id": "host-12345",
  "status": "DEREGISTERED",
  "containers_terminated": 5,
  "nics_cleaned_up": 12,
  "deregistered_at": "2026-06-11T14:45:30.123Z"
}
```

### 2.6 Device Heartbeat

```
POST /api/v1/devices/:device_id/heartbeat

Request:
{
  "timestamp": "2026-06-11T14:35:12.456Z",
  "status": "OK",
  "metrics": {
    "cpu_usage_percent": 45,
    "memory_usage_percent": 62,
    "active_connections": 1234
  }
}

Response (200 OK):
{
  "device_id": "host-12345",
  "last_heartbeat": "2026-06-11T14:35:12.456Z",
  "ack_id": "ack-5678-abcd"
}

Error Response (429):
{
  "error": {
    "code": "RATE_LIMITED",
    "message": "Too many heartbeats. Max 1 per second.",
    "retry_after_seconds": 1
  }
}
```

### 2.7 Device Telemetry

```
POST /api/v1/devices/:device_id/telemetry

Request:
{
  "telemetry": {
    "timestamp": "2026-06-11T14:35:12.456Z",
    "container_stats": [
      {
        "container_id": "container-001",
        "cpu_usage_ns": 1234567890,
        "memory_usage_bytes": 536870912
      }
    ],
    "nic_stats": [
      {
        "nic_id": "eth0",
        "packets_in": 1000000,
        "packets_out": 2000000,
        "bytes_in": 1048576000,
        "bytes_out": 2097152000
      }
    ]
  }
}

Response (202 Accepted):
{
  "telemetry_id": "tel-9012-efgh",
  "accepted_at": "2026-06-11T14:35:12.456Z"
}
```

**`DASH_METER` per-ENI billing counters** — Upstream DASH exposes a
read-only `DASH_METER` table with per-ENI byte/packet counters bound
to meter classes (one row per ENI per meter class). FM does **not**
publish these as a config kind; they surface here as part of
`nic_stats[]`. Each NIC entry MAY include a `meter_classes[]` array
mirroring upstream `DASH_METER` rows:

```
"nic_stats": [
  {
    "nic_id": "eth0",
    "packets_in": 1000000,
    "packets_out": 2000000,
    "bytes_in": 1048576000,
    "bytes_out": 2097152000,
    "meter_classes": [
      { "class_id": 1, "direction": "OUT", "bytes": 524288000, "packets": 500000 },
      { "class_id": 2, "direction": "IN",  "bytes":  98304000, "packets": 100000 }
    ]
  }
]
```

The HAL polls `DASH_METER` on its standard counter cadence and the FM
agent forwards the deltas through this endpoint; consumers correlate
`class_id` against the meter rules in `MeterPolicy`.

### 2.8 Get Device State

Returns the device's slice of the DASH object cache. Objects are grouped by
**scope ladder** (Fleet → Device → VNET → Group → ENI). A given DPU
materializes only the objects bound to ENIs it hosts; see
[`vm-eni-provisioning-design.md`](./vm-eni-provisioning-design.md) §1.1.

```
GET /api/v1/devices/:device_id/state

Response (200 OK):
{
  "device_id": "host-12345",
  "state": "READY",
  "objects": {
    "device": {
      "host_spec": { "id": "host-12345", "state": "READY", "revision": 7 },
      "appliance": { "id": "appl-1", "state": "READY", "revision": 3 },
      "tunnels":   [{ "id": "tun-1", "revision": 5 }],
      "qos":       [{ "id": "qos-default", "revision": 1 }],
      "prefix_tags":[{ "id": "tag-azure-storage", "revision": 12 }],
      "ha_sets":   [{ "id": "haset-pair-A", "role": "PRIMARY", "revision": 2 }]
    },
    "vnet": [
      {
        "vnet_id": "vnet-100",
        "spec_revision": 4,
        "mapping": {
          "manifest_digest": "sha256:abcd...",
          "chunk_count": 23,
          "row_count": 487213
        },
        "pa_validation_revision": 1
      }
    ],
    "group": {
      "route_groups":  [{ "id": "rg-prod-egress", "revision": 8 }],
      "acl_groups":    [{ "id": "acl-vnic-default", "revision": 11 }],
      "meter_policies":[{ "id": "mp-tier1", "revision": 2 }],
      "outbound_port_maps": []
    },
    "eni": [
      {
        "eni_id": "ENI_dpu-east-12_aa:bb:cc:dd:ee:ff",
        "container_guid": "container-001",
        "nic_id": "eth0",
        "vnet_id": "vnet-100",
        "state": "READY",
        "ha_scope_ref": "hascope-eni-12",
        "nic_spec_revision": 6,
        "composed_hash": "sha256:7f3a..."
      }
    ]
  }
}
```

The `composed_hash` is the SHA-256 of the canonical `NicGoalState`
serialization the FleetManager NO actor produced for this ENI. It is the
idempotency key used by the agent to skip already-applied work and the
value `Reconcile` compares against device-reported state.

`NicGoalState` itself is **never returned over this API** — it is composed
in-process and applied via the southbound. Only the hash and the input
references are observable.

### 2.9 Get Device Objects

```
GET /api/v1/devices/:device_id/objects?kind=Eni

Query Parameters:
  kind - Filter by DASH object kind. One of:
         RoutingType (fleet)
         | Appliance | HostSpec | Tunnel | Qos | PrefixTag | HaSet (device)
         | Vnet | VnetMappingManifest | VnetMappingChunk | PaValidation (vnet)
         | RouteGroup | AclGroup | MeterPolicy | OutboundPortMap (group)
         | Eni | ContainerSpec | HaScope (eni)
  scope - Optional scope filter: Fleet | Device | Vnet | Group | Eni
  vnet_id - Optional VNET filter (for vnet- and eni-scoped kinds)

Response (200 OK):
{
  "objects": [
    {
      "kind": "Eni",
      "scope": "Eni",
      "id": "ENI_dpu-east-12_aa:bb:cc:dd:ee:ff",
      "revision": 6,
      "refs": {
        "vnet_id": "vnet-100",
        "outbound_route_group_v4": "rg-prod-egress",
        "outbound_acl_v4": ["acl-vnic-default", null, "acl-vnet-default"],
        "inbound_acl_v4":  ["acl-vnic-default", null, "acl-vnet-default"],
        "outbound_meter_policy": "mp-tier1",
        "inbound_meter_policy":  "mp-tier1",
        "ha_scope": "hascope-eni-12"
      },
      "primary_ip": "10.1.2.3",
      "mac": "aa:bb:cc:dd:ee:ff",
      "state": "READY",
      "created_at": "2026-06-11T14:23:45.123Z"
    }
  ]
}
```

Note that an `Eni` object is almost entirely **references** — the DPU only
programs anything once every referenced object (`Vnet`, `RouteGroup`,
`AclGroup` slots, `MeterPolicy`, `HaScope`, …) is present in its local
cache. ENIs whose refs are not yet resolved appear in state
`WAITING_REFS`. See
[Common Misconceptions §1](../Learning-DashNet/16-Common-Misconceptions.md).

### 2.10 Get Pending Deltas

Returns the in-flight `DeltaPlan` for this device — the diff between the
last-applied `NicGoalState` and the freshly-composed one. Each delta is
keyed by `(kind, object_id)` and carries SHA-256 idempotency hashes.
Programming order follows the wave taxonomy from
[`vm-eni-provisioning-design.md`](./vm-eni-provisioning-design.md) §4
(Wave 0 globals → Wave 6 per-ENI bindings).

```
GET /api/v1/devices/:device_id/deltas?status=PENDING

Query Parameters:
  status - Filter by status (optional: PENDING | SUCCESS | FAILED | RETRYING)
  kind   - Filter by DASH object kind (optional: Eni, Vnet, RouteGroup, ...)
  wave   - Filter by programming wave (optional: 0..6)

Response (200 OK):
{
  "deltas": [
    {
      "command_id": "cmd-001",
      "trace_id": "550e8400-e29b-41d4-a716-446655440000",
      "operation": "CREATE",
      "kind": "Eni",
      "object_id": "ENI_dpu-east-12_aa:bb:cc:dd:ee:ff",
      "wave": 5,
      "prior_hash": null,
      "target_hash": "sha256:7f3a...",
      "status": "PENDING",
      "created_at": "2026-06-11T14:30:00.123Z",
      "retry_count": 0
    }
  ]
}
```

### 2.11 Health Check

```
GET /api/v1/health

Response (200 OK):
{
  "status": "OK",
  "service": "FleetManager",
  "version": "1.0.0",
  "primary": true,
  "shard_id": 0,
  "total_devices": 5234,
  "devices_ready": 5100,
  "devices_degraded": 134,
  "uptime_seconds": 86400,
  "timestamp": "2026-06-11T14:35:12.456Z"
}
```

### 2.12 Prometheus Metrics

```
GET /api/v1/metrics

Response (200 OK):
# HELP fleetmanager_devices_total Total number of devices registered
# TYPE fleetmanager_devices_total gauge
fleetmanager_devices_total{shard_id="0"} 5234

# HELP fleetmanager_delta_compilation_duration_ms Delta compilation latency
# TYPE fleetmanager_delta_compilation_duration_ms summary
fleetmanager_delta_compilation_duration_ms{device_id="host-12345",quantile="0.5"} 25
fleetmanager_delta_compilation_duration_ms{device_id="host-12345",quantile="0.99"} 75
fleetmanager_delta_compilation_duration_ms{device_id="host-12345",sum} 12500
fleetmanager_delta_compilation_duration_ms{device_id="host-12345",count} 500

...
```

---

## 3. Error Response Format

All error responses follow this standardized format:

```json
{
  "error": {
    "code": "ERROR_CODE",
    "message": "Human-readable error message",
    "trace_id": "550e8400-e29b-41d4-a716-446655440000",
    "timestamp": "2026-06-11T14:35:12.456Z",
    "request_path": "/api/v1/devices/host-12345",
    "details": {
      "field": "device_id",
      "constraint": "must be non-empty"
    }
  }
}
```

### Error Codes

| Code | HTTP Status | Meaning |
|------|-------------|---------|
| `INVALID_REQUEST` | 400 | Malformed request (invalid JSON, missing fields) |
| `INVALID_DEVICE_ID` | 400 | Device ID format invalid |
| `VALIDATION_ERROR` | 400 | Request validation failed |
| `UNAUTHORIZED` | 401 | Missing or invalid authentication |
| `FORBIDDEN` | 403 | Insufficient permissions |
| `DEVICE_NOT_FOUND` | 404 | Device does not exist |
| `DEVICE_ALREADY_EXISTS` | 409 | Device already registered |
| `CONFLICT` | 409 | Operation conflicts with current state |
| `WAITING_BOOTSTRAP` | 409 | HDO has not yet hydrated `/global` + `/group` caches |
| `WAITING_REFS` | 409 | NicSpec published but referenced objects not yet cached |
| `INCOMPLETE_MAPPING` | 409 | VnetMapping manifest references chunks not yet present |
| `OVER_CAPACITY` | 409 | Device hardware capability exceeded |
| `RATE_LIMIT_EXCEEDED` | 429 | Too many requests |
| `INTERNAL_ERROR` | 500 | Server error (please retry) |
| `SERVICE_UNAVAILABLE` | 503 | Service temporarily unavailable (try later) |

---

## 4. Request Headers

### Standard Headers

| Header | Required | Example | Purpose |
|--------|----------|---------|---------|
| `Content-Type` | Yes (POST/PUT) | `application/json` | Request body format |
| `Accept` | No | `application/json` | Response format |
| `X-Trace-ID` | No | `550e8400-e29b...` | Trace context (OpenTelemetry) |
| `Authorization` | No | `Bearer <token>` | Authentication (if enabled) |
| `User-Agent` | No | `curl/7.68.0` | Client identification |

### Example Request with Headers

```bash
curl -X POST http://localhost:8080/api/v1/devices \
  -H "Content-Type: application/json" \
  -H "X-Trace-ID: 550e8400-e29b-41d4-a716-446655440000" \
  -H "Authorization: Bearer eyJhbGciOiJIUzI1NiIs..." \
  -d '{...}'
```

---

## 5. Response Headers

| Header | Example | Purpose |
|--------|---------|---------|
| `Content-Type` | `application/json` | Response body format |
| `X-Trace-ID` | `550e8400-e29b...` | Trace ID for debugging |
| `X-Request-ID` | `req-001-abcd` | Unique request identifier |
| `Cache-Control` | `no-cache` | Caching policy |
| `X-RateLimit-Limit` | `1000` | Rate limit quota |
| `X-RateLimit-Remaining` | `950` | Requests remaining |
| `X-RateLimit-Reset` | `1623427200` | Unix timestamp when limit resets |

---

## 6. Implementation: REST Server Class

```cpp
// include/fleetmanager/rest_service.hpp
namespace fleetmanager {
namespace rest {

class RESTServiceImpl {
public:
    explicit RESTServiceImpl(
        std::shared_ptr<registry::DeviceRegistry> device_registry,
        std::shared_ptr<actor::ActorScheduler> actor_scheduler,
        std::shared_ptr<persistence::StateStore> state_store,
        std::shared_ptr<pubsub::SubscriptionManager> pubsub_manager,
        std::shared_ptr<observability::ObservabilityContext> otel_context,
        uint16_t port = 8080
    );
    
    // Start REST server
    Result<void> Start();
    
    // Stop REST server
    void Stop();

private:
    // Endpoint handlers
    void HandlePostDevices_(const http::Request& req, http::Response& res);
    void HandleGetDevices_(const http::Request& req, http::Response& res);
    void HandleGetDevice_(const http::Request& req, http::Response& res, const std::string& device_id);
    void HandlePutDevice_(const http::Request& req, http::Response& res, const std::string& device_id);
    void HandleDeleteDevice_(const http::Request& req, http::Response& res, const std::string& device_id);
    void HandleHeartbeat_(const http::Request& req, http::Response& res, const std::string& device_id);
    void HandleTelemetry_(const http::Request& req, http::Response& res, const std::string& device_id);
    void HandleGetState_(const http::Request& req, http::Response& res, const std::string& device_id);
    void HandleGetObjects_(const http::Request& req, http::Response& res, const std::string& device_id);
    void HandleGetDeltas_(const http::Request& req, http::Response& res, const std::string& device_id);
    void HandleHealth_(const http::Request& req, http::Response& res);
    void HandleMetrics_(const http::Request& req, http::Response& res);
    
    // Helper methods
    Result<nlohmann::json> ValidateDeviceProfile_(const nlohmann::json& json);
    std::string ExtractTraceID_(const http::Request& req);
    void FormatErrorResponse_(http::Response& res, int status_code, 
                            const std::string& error_code, const std::string& message, 
                            const std::string& trace_id);
    
    // HTTP server instance
    std::unique_ptr<http::Server> server_;
    uint16_t port_;
    
    // Backend components (shared with gRPC)
    std::shared_ptr<registry::DeviceRegistry> device_registry_;
    std::shared_ptr<actor::ActorScheduler> actor_scheduler_;
    std::shared_ptr<persistence::StateStore> state_store_;
    std::shared_ptr<pubsub::SubscriptionManager> pubsub_manager_;
    std::shared_ptr<observability::ObservabilityContext> otel_context_;
};

}  // namespace rest
}  // namespace fleetmanager
```

---

## 7. OpenAPI/Swagger Documentation

The REST API includes auto-generated OpenAPI specification at:

```
GET /api/v1/swagger.json

Response:
{
  "openapi": "3.0.0",
  "info": {
    "title": "FleetManager REST API",
    "version": "1.0.0",
    "description": "API for managing DPU-enabled device fleets"
  },
  "servers": [
    {
      "url": "http://localhost:8080",
      "description": "Local development server"
    }
  ],
  "paths": {
    "/api/v1/devices": {
      "post": { ... },
      "get": { ... }
    },
    "/api/v1/devices/{device_id}": {
      "get": { ... },
      "put": { ... },
      "delete": { ... }
    },
    ...
  }
}
```

Interactive documentation available at: `http://localhost:8080/api/v1/swagger-ui`

---

## 8. Rate Limiting & Throttling

### Rate Limits

- **Per IP**: 1,000 requests/minute
- **Per device**: 100 heartbeats/minute
- **Per device**: 10 telemetry reports/minute

### Rate Limit Headers

```
X-RateLimit-Limit: 1000
X-RateLimit-Remaining: 950
X-RateLimit-Reset: 1623427200
```

### Rate Limit Response

```json
HTTP/1.1 429 Too Many Requests

{
  "error": {
    "code": "RATE_LIMIT_EXCEEDED",
    "message": "Rate limit exceeded: 1000 requests per minute",
    "retry_after_seconds": 60
  }
}
```

---

## 9. Authentication & Authorization (Optional)

If enabled, REST API requires bearer token authentication:

```cpp
// Configuration
authentication:
  enabled: true
  method: "bearer_token"
  jwks_url: "https://auth.example.com/.well-known/jwks.json"
  required_scopes: ["fleetmanager:write", "fleetmanager:read"]
```

---

## 10. Testing REST API

```bash
# Test device registration
curl -X POST http://localhost:8080/api/v1/devices \
  -H "Content-Type: application/json" \
  -d @device-profile.json | jq

# Test pagination
curl -X GET "http://localhost:8080/api/v1/devices?limit=10&offset=0" | jq

# Test health check
curl http://localhost:8080/api/v1/health | jq

# Test with trace context
curl -X GET http://localhost:8080/api/v1/devices/host-12345 \
  -H "X-Trace-ID: $(uuidgen)" | jq

# Benchmark registration latency
ab -n 1000 -c 10 -p device-profile.json \
  -T "application/json" \
  http://localhost:8080/api/v1/devices
```

---

## Conclusion

The REST API provides **easy device management** for external clients and tooling, while maintaining **high performance** through shared backend components with gRPC. Both APIs are **fully observability-integrated** with OpenTelemetry tracing and Prometheus metrics.
