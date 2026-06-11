# FleetManager: High-Level Design (HLD)

**Version:** 1.0  
**Language:** C++17/20  
**Framework:** gRPC, Protocol Buffers, Boost.ASIO  
**Status:** Design Phase  
**Date:** June 2026

---

## 1. Executive Summary

**FleetManager** is a high-performance, Kubernetes-native microservice that manages the lifecycle of thousands of DPU-enabled hosts and DASH appliances. It exposes **dual APIs** (gRPC for inter-service, REST for external clients), ingests hierarchical configuration from a PubSub store, compiles deltas into device-specific programming commands, and orchestrates those commands across a heterogeneous fleet of data plane devices.

**Design Goals:**
- **Performance**: Sub-100ms device registration, <50ms delta compilation per device
- **Reliability**: Zero-downtime failover via hot standby replication (RTO ≈30s)
- **Scale**: Manage 5,000-10,000 devices per shard via actor-per-device concurrency
- **Flexibility**: Multi-protocol southbound layer (DASH/gNMI, SONiC/SAI, Linux/netlink)
- **Accessibility**: REST API for device registration, CRUD ops, and client tooling
- **Observability**: End-to-end OpenTelemetry tracing from intent to device programming

---

## 2. System Context

### 2.1 External Interfaces

```
┌──────────────────────────┐    ┌────────────────────────┐
│  Device Clients          │    │  Internal Services     │
│  (curl, HTTP clients)    │    │  (gRPC clients)        │
└────────┬─────────────────┘    └──────────┬─────────────┘
         │ REST                             │ gRPC
         │ (Port 8080)                      │ (Port 5051)
         │                                  │
┌────────▼──────────────────────────────────▼──────────────┐
│                                                           │
│                    FLEETMANAGER                          │
│                                                           │
│  ┌─────────────────────────────────────────────────────┐ │
│  │ Dual API Layer                                      │ │
│  │ ├─ REST Server (8080): Device registration, CRUD   │ │
│  │ └─ gRPC Server (5051): Inter-service RPCs          │ │
│  └─────────────────────────────────────────────────────┘ │
│                                                           │
│  Responsibilities:                                       │
│  - Device registration & profiling                      │
│  - Configuration intent processing                      │
│  - Delta compilation & dependency resolution            │
│  - Multi-protocol device programming                    │
│  - State reconciliation & drift detection               │
│  - HA coordination (K8s Lease)                          │
│                                                           │
└────────┬──────────────────┬──────────────────┬───────────┘
         │                  │                  │
         ▼                  ▼                  ▼
    [DASH Devices]    [SONiC Hosts]    [Linux Hosts]
    (gNMI/P4Runtime)   (SAI Thrift)      (netlink)
         │ Configuration & State Sync
         │ Southbound RPC Calls
         │

┌─────────────────────────────────────────────────────────┐
│           UPSTREAM CONTROL PLANE                        │
│       (SDN Controller / Portal)                         │
│                                                          │
│   PubSub Configuration Store                           │
│   (Redis Streams / Kafka / etcd)                       │
└─────────────────────────────────────────────────────────┘
```

### 2.2 Deployment Architecture

```
Kubernetes Cluster
└─ Namespace: dashfabric
   └─ StatefulSet: fleetmanager-workers
      ├─ Pod[0] (PRIMARY)    ├─ Pod[1] (STANDBY)
      │ - Processes deltas   │ - Reads config
      │ - Programs devices   │ - Updates cache
      │ - Holds K8s Lease    │ - Ready for failover
      │ - Persistent PVC     │ - Shared PVC
      │
      ├─ Pod[2] (PRIMARY)    ├─ Pod[3] (STANDBY)
      │ - Shard 1            │ - Shard 1
      │
      └─ ... (more shard pairs)

Persistent Storage
└─ RocksDB (per pod PVC)
   └─ Device state cache
   └─ Delta command queue
   └─ Reconciliation checkpoints
```

---

## 3. Core Components

### 3.1 Component Architecture

```
┌─────────────────────────────────────────────────────────────┐
│              FLEETMANAGER SERVICE INTERNALS                 │
│                                                              │
│  ┌───────────────────────────────────────────────────────┐  │
│  │ Dual API Server                                       │  │
│  ├──────────────────────┬──────────────────────────────┤  │
│  │ REST Server (8080)   │ gRPC Server (5051)           │  │
│  │ - POST /devices      │ - RegisterDevice RPC         │  │
│  │ - GET /devices       │ - Heartbeat RPC              │  │
│  │ - GET /devices/:id   │ - Telemetry RPC              │  │
│  │ - PUT /devices/:id   │                              │  │
│  │ - DELETE /devices/:id│                              │  │
│  │ - POST /devices/:id  │ (Inter-service calls)        │  │
│  │   /heartbeat         │                              │  │
│  │ - POST /devices/:id  │                              │  │
│  │   /telemetry         │                              │  │
│  └──────────────────────┴──────────────────────────────┘  │
│                         │                                   │
│  ┌──────────────────────▼──────────────────────────────┐  │
│  │ Shared Request Handler Layer                        │  │
│  │ - Validation (schema, ACL)                          │  │
│  │ - Request tracing (W3C Trace Context)               │  │
│  │ - Error handling & formatting                       │  │
│  └──────────────────────┬──────────────────────────────┘  │
│                         │                                   │
│  ┌──────────────────────▼──────────────────────────────┐  │
│  │ Device Registry                                     │  │
│  │ - Map<device_id, DeviceProfile>                    │  │
│  │ - Shard assignment tracking                        │  │
│  └─────────────────────────────────────────────────────┘  │
│                         │                                   │
│  ┌──────────────────────▼──────────────────────────────┐  │
│  │ PubSub Subscription Manager                         │  │
│  │ - Per-device topic subscriptions                   │  │
│  │ - Concurrent receivers                             │  │
│  │ - Backpressure handling                            │  │
│  └──────────────────┬───────────────────────────────────┘ │
│                     │                                      │
│  ┌──────────────────▼──────────────────────────────────┐  │
│  │ Actor Framework (per-device)                        │  │
│  │ - DeviceActor[0] → State cache + delta queue        │  │
│  │ - DeviceActor[1] → State cache + delta queue        │  │
│  │ - ...                                               │  │
│  │ (Async processing, no cross-device locks)           │  │
│  └──────────────────┬───────────────────┬──────────────┘  │
│                     │                   │                  │
│  ┌──────────────────▼────────┐  ┌───────▼────────────────┐ │
│  │ State Compilation Engine  │  │ Reconciliation Engine  │ │
│  │ - Delta computation       │  │ - Periodic audits      │ │
│  │ - Dependency graph        │  │ - Drift detection      │ │
│  │ - Goal state compile      │  │ - Corrective actions   │ │
│  └──────────────────┬────────┘  └───────┬────────────────┘ │
│                     │                   │                  │
│  ┌──────────────────▼──────────────────▼──────────────────┐ │
│  │ Southbound Driver Layer (Multi-Protocol)             │ │
│  │ ┌────────────┐  ┌────────────┐  ┌────────────┐       │ │
│  │ │ DASH Driver│  │SONiC Driver│  │Linux Driver│       │ │
│  │ │(gNMI/Proto)│  │(SAI Thrift)│  │(netlink)   │       │ │
│  │ └────────────┘  └────────────┘  └────────────┘       │ │
│  └─────────────────────────────────────────────────────┘ │
│                     │                                      │
│  ┌──────────────────▼──────────────────────────────────┐  │
│  │ State Persistence (RocksDB)                         │  │
│  │ - Device cache                                      │  │
│  │ - Delta queue                                       │  │
│  │ - Reconciliation checkpoints                        │  │
│  └─────────────────────────────────────────────────────┘  │
│                                                             │
│  ┌─────────────────────────────────────────────────────┐  │
│  │ Observability Layer                                 │  │
│  │ - OpenTelemetry (tracing)                           │  │
│  │ - Prometheus (metrics)                              │  │
│  │ - Structured logging (JSON)                         │  │
│  └─────────────────────────────────────────────────────┘  │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```
│  │ Device Registry                                         ││
│  │ - Map<device_id, DeviceProfile>                        ││
│  │ - Shard assignment tracking                            ││
│  └─────────────────────────────────────────────────────────┘│
│                         │                                    │
│  ┌──────────────────────▼──────────────────────────────────┐│
│  │ PubSub Subscription Manager                            ││
│  │ - Per-device topic subscriptions                       ││
│  │ - Concurrent receivers                                 ││
│  │ - Backpressure handling                                ││
│  └──────────────────────┬──────────────────────────────────┘│
│                         │                                    │
│  ┌──────────────────────▼──────────────────────────────────┐│
│  │ Actor Framework (per-device)                           ││
│  │ - DeviceActor[0]  → State cache + delta queue          ││
│  │ - DeviceActor[1]  → State cache + delta queue          ││
│  │ - ...                                                   ││
│  │ - DeviceActor[N]  → State cache + delta queue          ││
│  │ (Async processing, no cross-device locks)              ││
│  └──────────────┬──────────────────┬──────────────────────┘│
│                 │                  │                       │
│  ┌──────────────▼────────┐  ┌──────▼─────────────────────┐│
│  │ State Compilation     │  │ Reconciliation Engine      ││
│  │ Engine                │  │ - Periodic audits (60s)    ││
│  │ - Delta computation   │  │ - Drift detection          ││
│  │ - Dependency graph    │  │ - Corrective actions       ││
│  │ - Goal state compile  │  │ - State hash validation    ││
│  └──────────────┬────────┘  └──────┬──────────────────────┘│
│                 │                  │                       │
│  ┌──────────────▼──────────────────▼──────────────────────┐│
│  │ Southbound Driver Layer (Multi-Protocol)              ││
│  │ ┌──────────────┐  ┌──────────────┐  ┌──────────────┐  ││
│  │ │ DASH Driver  │  │ SONiC Driver │  │ Linux Driver │  ││
│  │ │ (gNMI/Proto) │  │ (SAI Thrift) │  │ (netlink)    │  ││
│  │ └──────────────┘  └──────────────┘  └──────────────┘  ││
│  │ - Device connection pooling                           ││
│  │ - RPC execution + retry logic                         ││
│  │ - Error handling & telemetry                          ││
│  └──────────────┬───────────────────────────────────────┘│
│                 │                                         │
│  ┌──────────────▼───────────────────────────────────────┐│
│  │ State Persistence (RocksDB)                          ││
│  │ - Device cache: "shard:device_id:object_type:id"    ││
│  │ - Delta queue: pending commands                      ││
│  │ - Checkpoints: reconciliation state                  ││
│  └─────────────────────────────────────────────────────┘│
│                                                          │
│  ┌──────────────────────────────────────────────────────┐│
│  │ Observability Layer                                  ││
│  │ - OpenTelemetry (tracing)                            ││
│  │ - Prometheus (metrics)                               ││
│  │ - Structured logging (JSON)                          ││
│  └─────────────────────────────────────────────────────┘│
│                                                          │
└─────────────────────────────────────────────────────────┘
```

### 3.2 Component Responsibilities

| Component | Responsibility | Key Classes |
|-----------|-----------------|------------|
| **REST API Server** | Expose REST endpoints for device registration, CRUD, heartbeat | `RESTServiceImpl`, `DeviceRouter`, `JSONValidator` |
| **gRPC Server** | Expose gRPC RPCs for inter-service communication | `FleetManagerServiceImpl` |
| **Device Registry** | Track device profiles and shard assignments | `DeviceRegistry`, `DeviceProfile` |
| **PubSub Manager** | Establish/maintain topic subscriptions | `PubSubSubscriptionManager`, `TopicReceiver` |
| **Actor Framework** | Per-device concurrent state management | `DeviceActor`, `ActorMailbox`, `ActorScheduler` |
| **State Compilation** | Compute deltas from intent | `StateCompilationEngine`, `DeltaCompiler` |
| **Reconciliation** | Periodic audits + drift detection | `ReconciliationEngine`, `DriftDetector` |
| **Southbound Drivers** | Device-specific programming | `DASHDriver`, `SONiCDriver`, `LinuxDriver` |
| **State Persistence** | RocksDB cache + checkpointing | `StateStore`, `RocksDBAdapter` |
| **Observability** | Tracing, metrics, logging | `OTelTracer`, `MetricsCollector`, `StructuredLogger` |

---

## 4. Data Flow

### 4.1 Device Registration Flow

```
Device (host-12345)
    │
    ├─ gRPC Call: RegisterDevice(DeviceProfile)
    │   └─ Trace ID generated here
    │
    ▼
FleetManager API Server
    ├─ Validate device_id, hardware capabilities
    ├─ Compute shard assignment: hash(device_id) % shard_count
    ├─ Route to assigned shard worker
    │
    ▼
Shard Worker (e.g., worker-0)
    ├─ Create DeviceActor[device_id]
    ├─ Create DeviceRegistry entry
    ├─ Trigger Upstream Sync Engine: subscribe /config/hosts/{device_id}
    ├─ Persist to RocksDB
    ├─ Update metrics: devices_total++
    │
    ▼
Response to Device
    └─ RegisterDeviceResponse(shard_id, subscription_topics)
```

### 4.2 Configuration Update Flow

```
Upstream Control Plane
    │
    ├─ Publish config to PubSub: /config/hosts/{device_id}/containers/{cid}/nics/{nic_id}
    │   └─ Trace ID propagated in message header
    │
    ▼
Shard Worker (PubSub Receiver Thread)
    ├─ Receive configuration notification
    ├─ Extract trace_id
    ├─ Route to DeviceActor[device_id]
    │   └─ Enqueue to actor mailbox (non-blocking)
    │
    ▼
DeviceActor (async)
    ├─ Pop from mailbox
    ├─ Invoke StateCompilationEngine::ComputeDeltas()
    │   ├─ Load cached state
    │   ├─ Compare with new intent
    │   ├─ Compute CREATE/UPDATE/DELETE ops
    │   └─ Resolve dependencies
    │
    ├─ Invoke DeltaCompiler (based on device_type)
    │   └─ Compile to device-specific config (protobuf/Thrift/netlink)
    │
    ├─ Save to RocksDB (checkpoint)
    │
    ├─ Enqueue DeltaCommands to Southbound Driver queue
    │   └─ Each delta pushed to appropriate driver (DASH/SONiC/Linux)
    │
    ▼
Southbound Driver (async)
    ├─ Pop DeltaCommand from queue
    ├─ Execute RPC (gNMI Set / SAI Thrift / netlink)
    ├─ Update delta status (PENDING → SUCCESS/FAILED)
    ├─ Emit trace event
    │
    ▼
Data Plane Device
    ├─ Receive configuration
    ├─ Program ENI / routes / ACLs
    └─ Return status
```

---

## 5.1 REST API Endpoints

**Base URL:** `http://localhost:8080/api/v1`

### Device Management

| Method | Endpoint | Purpose | Request | Response |
|--------|----------|---------|---------|----------|
| **POST** | `/devices` | Register new device | `DeviceProfile` (JSON) | `RegisterResponse` (device_id, shard_id) |
| **GET** | `/devices` | List all devices (paginated) | Query: `limit`, `offset`, `shard_id` | `Device[]` |
| **GET** | `/devices/:device_id` | Get device details | Path param: device_id | `DeviceProfile` |
| **PUT** | `/devices/:device_id` | Update device profile | `DeviceProfile` (JSON) | `UpdateResponse` |
| **DELETE** | `/devices/:device_id` | Deregister device | Path param: device_id | `DeleteResponse` |
| **POST** | `/devices/:device_id/heartbeat` | Send device heartbeat | `HeartbeatRequest` | `HeartbeatResponse` |
| **POST** | `/devices/:device_id/telemetry` | Report device telemetry | `TelemetryReport` (JSON) | `TelemetryResponse` |

### Device State & Diagnostics

| Method | Endpoint | Purpose | Response |
|--------|----------|---------|----------|
| **GET** | `/devices/:device_id/state` | Get current device state | `DeviceState` |
| **GET** | `/devices/:device_id/objects` | List device objects (containers, NICs) | `DeviceObject[]` |
| **GET** | `/devices/:device_id/deltas` | Get pending delta commands | `CompiledDelta[]` |
| **GET** | `/health` | Service health check | `{"status": "OK", "primary": bool, "shard_id": int}` |
| **GET** | `/metrics` | Prometheus metrics | Prometheus text format |

### Example REST Calls

```bash
# Register device
curl -X POST http://localhost:8080/api/v1/devices \
  -H "Content-Type: application/json" \
  -H "X-Trace-ID: 550e8400-e29b-41d4-a716-446655440000" \
  -d '{
    "device_id": "host-12345",
    "device_type": "HOST_DPU_ATTACHED",
    "hardware_capabilities": {
      "max_flow_table_entries": 1000000,
      "max_routes_per_eni": 10000,
      "max_acl_rules": 100000
    }
  }'

# Response:
# {
#   "device_id": "host-12345",
#   "shard_id": "0",
#   "subscription_topics": ["/config/hosts/host-12345"],
#   "status": "OK"
# }

# Get device state
curl -X GET http://localhost:8080/api/v1/devices/host-12345/state \
  -H "X-Trace-ID: 550e8400-e29b-41d4-a716-446655440000"

# Send heartbeat
curl -X POST http://localhost:8080/api/v1/devices/host-12345/heartbeat \
  -H "Content-Type: application/json" \
  -d '{"timestamp": "2026-06-11T14:23:45Z"}'

# List all devices
curl -X GET "http://localhost:8080/api/v1/devices?limit=50&offset=0"
```

---

## 5. Threading & Concurrency Model

### 5.1 Thread Pool Architecture

```
FleetManager Process
│
├─ Main Thread
│  └─ Initializes components, starts threads
│
├─ REST Worker Thread Pool (M threads, default=6)
│  ├─ Handles POST /devices (registration)
│  ├─ Handles GET /devices (queries)
│  ├─ Handles PUT /devices (updates)
│  ├─ Handles heartbeat/telemetry endpoints
│  └─ Shared request validator & response formatter
│
├─ gRPC Worker Thread Pool (N threads, default=4)
│  ├─ Handles RegisterDevice RPCs
│  ├─ Handles Heartbeat RPCs
│  └─ Handles Telemetry RPCs
│
├─ PubSub Receiver Thread Pool (P threads, default=2)
│  ├─ Per-shard receiver thread
│  └─ Processes incoming configuration notifications
│
├─ Actor Executor Thread Pool (K threads, default=16)
│  ├─ Executes DeviceActor tasks
│  ├─ Runs State Compilation Engine
│  ├─ Runs Reconciliation Engine
│  └─ No cross-actor locks (per-device isolation)
│
├─ Southbound Driver Thread Pool (L threads, default=8)
│  ├─ Executes gRPC/Thrift/netlink calls
│  ├─ Manages connection pooling
│  └─ Handles retry logic
│
└─ Metrics/Observability Thread
   ├─ Periodically flushes OpenTelemetry
   └─ Exports Prometheus metrics
```

### 5.2 Synchronization Primitives

```
Global State (K8s Lease, DeviceRegistry)
    ├─ RWMutex: device_registry_lock (R/W by gRPC server)
    └─ Atomic: am_i_primary (CAS for PRIMARY/STANDBY switch)

Per-Device State (RocksDB key-space)
    ├─ No cross-device locks
    ├─ Actor-local mailbox (lock-free queue)
    └─ RocksDB serialization (single writer per key via RocksDB internals)

Southbound Driver Queues
    ├─ Lock-free queue per driver type (DASH/SONiC/Linux)
    └─ Backpressure: queue size monitored, propagated to actor
```

---

## 6. State Management

### 6.1 Object State Machines

**HostDeviceObject States:**
```
INITIALIZING → READY ⇄ RECONFIGURING → DRAINING → TERMINATED
                │         ↑
                └─────────┘
```

**ContainerObject States:**
```
INITIALIZING → READY ⇄ RECONFIGURING → DESTROYING → TERMINATED
```

**NICObject States:**
```
INITIALIZING → READY ⇄ POLICY_UPDATE → DESTROYING → TERMINATED
```

### 6.2 Cached vs. Persistent State

**In-Memory Cache (RapidJSON):**
- Device actor state (warm cache)
- Current object state machines
- Outstanding delta commands
- Subscription channel references

**Persistent (RocksDB):**
- Device profiles + capabilities
- Compiled goal state (DeviceGoalState)
- Reconciliation checkpoints
- Audit logs (append-only)

**Recovery on Failover:**
1. New PRIMARY pod mounts same PVC
2. Loads RocksDB into memory cache
3. Recomputes in-flight deltas from checkpoint
4. Resumes as if primary always knew state (no hydration delay)

---

## 7. HA & Failover

### 7.1 PRIMARY/STANDBY Coordination

```
K8s Lease: "dashfabric-shard-0"

PRIMARY (worker-0)
├─ Holds lease (renewed every 10s, TTL=30s)
├─ Programs devices (southbound RPCs)
├─ Writes to RocksDB
└─ Relinquishes lease on graceful shutdown

STANDBY (worker-1)
├─ Watches lease (attempts to acquire every 5s)
├─ Receives same PubSub notifications (same subscriptions)
├─ Reads RocksDB (concurrent, non-blocking)
├─ Updates local cache asynchronously
└─ Acquires lease on PRIMARY failure
    ├─ Sets am_i_primary = true
    ├─ Resumes southbound RPC execution
    ├─ RTO ≈ 30-35 seconds (lease TTL + detection)
    └─ No data loss (all state in RocksDB)
```

### 7.2 ISSU (In-Service Software Upgrade)

```
Old PRIMARY (v1.0)
├─ Receives SIGTERM
├─ Stops accepting new subscriptions
├─ Flushes all cached state to RocksDB
├─ Drains southbound queue
├─ Proactively relinquishes K8s lease
└─ Terminates gracefully

New STANDBY (v1.0) → becomes PRIMARY
├─ Acquires lease immediately
├─ Continues device programming

K8s creates new pod (v2.0)
├─ Mounts same PVC
├─ Loads RocksDB on startup
├─ Starts as STANDBY (am_i_primary=false)
├─ Subscribes to topics
├─ Updates cache asynchronously
└─ Ready for next failover

Optional Switchback
├─ Current PRIMARY flushes and relinquishes
├─ New PRIMARY acquires lease
└─ System load balanced
```

---

## 8. Scalability via Sharding

### 8.1 Consistent Hashing

```cpp
uint32_t GetShardIndex(const std::string& device_id, uint32_t shard_count) {
    uint32_t hash = crc32(device_id);
    return hash % shard_count;
}
```

### 8.2 Dynamic Scale-Out

```
Initial: replicas=4 (2 shards)
Scale: replicas=8 (3 shards)
├─ K8s creates worker-4, worker-5
├─ API Gateway updates hash ring: shard_count = 3
├─ New devices register: hash() % 3 → routed to Shard 2
├─ Existing devices: still mapped to Shard 0/1 (no migration)
└─ Result: better distribution, existing devices unaffected
```

---

## 9. Southbound HAL Design

### 9.1 Protocol Support

| Protocol | Target | Encoding | Connectivity |
|----------|--------|----------|--------------|
| **gNMI** | DASH appliances | Protobuf | gRPC channel (persistent) |
| **SAI Thrift** | SONiC hosts | Thrift binary | TCP connection pool |
| **netlink** | Linux hosts | Kernel netlink API | Local socket or remote rtnl |

### 9.2 Driver Interface

```cpp
class SouthboundDriver {
    virtual Status CreateENI(const ENIConfig& config) = 0;
    virtual Status UpdateENI(const ENIUpdate& update) = 0;
    virtual Status DeleteENI(const std::string& eni_id) = 0;
    
    virtual Status ProgramACL(const ACLRules& rules) = 0;
    virtual Status ProgramRoutes(const RouteTable& routes) = 0;
};

class DASHDriver : public SouthboundDriver {
    // gNMI-based implementation
};

class SONiCDriver : public SouthboundDriver {
    // SAI Thrift implementation
};

class LinuxDriver : public SouthboundDriver {
    // netlink-based implementation
};
```

---

## 10. Observability Integration

### 10.1 OpenTelemetry Tracing

**Trace Context Propagation:**
- Upstream intent → W3C Trace Context header
- FleetManager extracts trace_id, creates child span
- Southbound RPC includes trace context
- End-to-end visibility in Jaeger/Tempo

### 10.2 Metrics Collection

**Key Metrics:**
- `fleetmanager_devices_total{shard_id}` — device count
- `fleetmanager_delta_compilation_duration_ms` — compilation latency
- `fleetmanager_southbound_rpc_latency_ms{target_type}` — RPC latency
- `fleetmanager_reconciliation_drift_detected_total` — drift events
- `fleetmanager_primary_failovers_total{shard_id}` — failover count

### 10.3 Structured Logging

```json
{
  "timestamp": "2026-06-11T14:23:45.123Z",
  "level": "INFO",
  "message": "Delta command executed",
  "trace_id": "550e8400-e29b-41d4-a716-446655440000",
  "component": "southbound_driver",
  "device_id": "host-12345",
  "operation": "CREATE_NIC",
  "duration_ms": 45
}
```

---

## 11. Deployment Model

### 11.1 Kubernetes StatefulSet

```yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: fleetmanager-workers
spec:
  serviceName: fleetmanager
  replicas: 4  # 2 shards
  
  template:
    spec:
      containers:
      - name: fleetmanager
        image: fleetmanager:latest
        ports:
        - name: rest
          containerPort: 8080
        - name: grpc
          containerPort: 5051
        - name: metrics
          containerPort: 8081
        
        volumeMounts:
        - name: state-storage
          mountPath: /var/lib/fleetmanager/state
  
  volumeClaimTemplates:
  - metadata:
      name: state-storage
    spec:
      accessModes: ["ReadWriteOnce"]
      resources:
        requests:
          storage: 100Gi
```

---

## 12. Performance Targets

| Operation | Target Latency | SLO |
|-----------|----------------|-----|
| Device Registration | <100ms | p99 <150ms |
| Delta Compilation | <50ms per device | p99 <100ms |
| Southbound ENI Creation | <50ms | p99 <100ms |
| Reconciliation (5000 devices) | <60s | p99 <120s |
| Failover (STANDBY takes over) | ~30-35s | RTO per K8s lease TTL |

---

## 13. Security Considerations

- **mTLS** for all gRPC channels (inter-pod, southbound)
- **RBAC** for API Gateway (device registration permissions)
- **Encrypted state** in RocksDB (optional, AES-256)
- **Audit logging** for all device operations (immutable log)
- **Secrets management** (K8s Secrets for credentials)

---

## Conclusion

FleetManager is a production-grade gRPC microservice that:
✅ Manages 5,000-10,000 DPU devices per shard  
✅ Achieves zero-downtime failover via hot standby  
✅ Supports heterogeneous southbound protocols  
✅ Provides end-to-end observability  
✅ Scales horizontally via consistent hashing  

The HLD emphasizes **simplicity** (single service, standard frameworks), **reliability** (hot standby, reconciliation), and **scale** (sharding, actor model).
