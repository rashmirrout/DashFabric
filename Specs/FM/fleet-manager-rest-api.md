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

### 2.8 Get Device State

```
GET /api/v1/devices/:device_id/state

Response (200 OK):
{
  "device_id": "host-12345",
  "state": "READY",
  "objects": {
    "host_device": {
      "id": "hd-uuid-123",
      "state": "READY",
      "created_at": "2026-06-11T14:23:45.123Z"
    },
    "containers": [
      {
        "id": "c-uuid-001",
        "container_guid": "container-001",
        "state": "READY",
        "nic_count": 2
      }
    ],
    "nics": [
      {
        "id": "n-uuid-001",
        "nic_id": "eth0",
        "state": "READY",
        "eni_id": "eni-123"
      }
    ]
  }
}
```

### 2.9 Get Device Objects

```
GET /api/v1/devices/:device_id/objects?type=NIC

Query Parameters:
  type - Filter by object type (optional: HOST | CONTAINER | NIC)

Response (200 OK):
{
  "objects": [
    {
      "type": "NIC",
      "id": "nic-001",
      "nic_id": "eth0",
      "container_id": "container-001",
      "state": "READY",
      "eni_id": "eni-123",
      "vpc_id": "vpc-001",
      "primary_ip": "10.1.2.3",
      "created_at": "2026-06-11T14:23:45.123Z"
    }
  ]
}
```

### 2.10 Get Pending Deltas

```
GET /api/v1/devices/:device_id/deltas?status=PENDING

Query Parameters:
  status - Filter by status (optional: PENDING | SUCCESS | FAILED | RETRYING)

Response (200 OK):
{
  "deltas": [
    {
      "command_id": "cmd-001",
      "trace_id": "550e8400-e29b-41d4-a716-446655440000",
      "operation": "CREATE",
      "target_object_type": "NIC",
      "target_object_id": "eth0",
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
