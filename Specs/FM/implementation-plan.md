# FleetManager: Implementation Plan

**Version:** 1.0  
**Language:** C++17/20  
**Status:** Ready for Execution  
**Estimated Timeline:** 16-20 weeks for full MVP

---

## 1. Project Overview & Goals

### 1.1 Objectives

**Primary:** Deliver a production-grade gRPC microservice that manages 5,000-10,000 DPU devices per shard with sub-100ms registration latency and zero-downtime failover.

**Success Criteria:**
- [ ] Device registration latency: <100ms (p99 <150ms)
- [ ] Delta compilation latency: <50ms per device
- [ ] HA failover RTO: <35 seconds
- [ ] Support 10,000 concurrent subscriptions per shard
- [ ] 99.99% uptime on production deployment
- [ ] Comprehensive observability (tracing, metrics, logs)

### 1.2 Phase Breakdown

| Phase | Duration | Deliverable | Metrics |
|-------|----------|-----------|---------|
| **Phase 1: Core Foundation** | 4 weeks | gRPC server, device registry, basic actors | Registration working, metrics exported |
| **Phase 2: Delta Compilation** | 4 weeks | State engine, compilation, DASH driver | Delta computation <50ms |
| **Phase 3: HA & Persistence** | 4 weeks | K8s Lease, RocksDB, hot standby | Failover tested, RTO measured |
| **Phase 4: Multi-Protocol Support** | 3 weeks | SONiC, Linux drivers | All three protocols functional |
| **Phase 5: Testing & Optimization** | 3 weeks | Unit tests, integration tests, load testing | Performance targets met |
| **Phase 6: Deployment & Documentation** | 2 weeks | Kubernetes manifests, runbooks, handoff | Ready for production |

---

## 2. Phase 1: Core Foundation (Weeks 1-4)

### 2.1 Objectives

Establish the foundation: **dual API servers** (REST + gRPC), device registry, actor framework basics, basic metrics.

### 2.2 Deliverables

| Component | Owner | Estimated LOC | Status |
|-----------|-------|--------------|--------|
| `RESTServiceImpl` (HTTP server, endpoints) | Team A | 1200 | To Do |
| `CoreServiceImpl` (gRPC skeleton) | Team A | 800 | To Do |
| `DeviceRegistry` | Team A | 400 | To Do |
| `ActorFramework` (mailbox, scheduler) | Team B | 600 | To Do |
| `Observability` (metrics, logging) | Team B | 500 | To Do |
| Unit tests (60% coverage) | Team C | 1200 | To Do |

**Total Phase 1: ~4,700 LOC**

### 2.3 Build System Setup

```cpp
// CMakeLists.txt (root)
cmake_minimum_required(VERSION 3.20)
project(FleetManager VERSION 1.0.0 LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)

# Dependencies
find_package(gRPC REQUIRED)
find_package(Protobuf REQUIRED)
find_package(RocksDB REQUIRED)
find_package(opentelemetry-cpp REQUIRED)
find_package(Boost REQUIRED COMPONENTS system thread)

# Library: fleet_manager_core
add_library(fleet_manager_core
    src/registry.cpp
    src/actor_framework.cpp
    src/observability.cpp
    src/state_store.cpp
)

target_include_directories(fleet_manager_core PUBLIC include)
target_link_libraries(fleet_manager_core PUBLIC
    gRPC::grpc++
    protobuf::libprotobuf
    RocksDB::rocksdb
    opentelemetry::trace
    opentelemetry::metrics
    Boost::system
    Boost::thread
)

# Executable: fleetmanager_service
add_executable(fleetmanager_service
    src/main.cpp
    src/grpc_service.cpp
)

target_link_libraries(fleetmanager_service PRIVATE fleet_manager_core)

# Tests
enable_testing()
add_subdirectory(tests)
```

### 2.4 Project Structure

```
FleetManager/
├── CMakeLists.txt
├── conanfile.txt (for dependency management)
├── include/
│   ├── fleetmanager/
│   │   ├── registry.hpp
│   │   ├── actor_framework.hpp
│   │   ├── state_store.hpp
│   │   ├── observability.hpp
│   │   ├── result.hpp
│   │   ├── rest_service.hpp          # NEW: REST API
│   │   ├── rest_handlers.hpp         # NEW: REST endpoint handlers
│   │   └── common.hpp
├── proto/
│   ├── fleet_manager.proto (service definition)
│   ├── models.proto (data models)
│   └── CMakeLists.txt
├── src/
│   ├── CMakeLists.txt
│   ├── registry.cpp
│   ├── actor_framework.cpp
│   ├── observability.cpp
│   ├── state_store.cpp
│   ├── rest_service.cpp              # NEW: REST server implementation
│   ├── rest_handlers.cpp             # NEW: REST endpoint handlers
│   ├── grpc_service.cpp
│   └── main.cpp
├── tests/
│   ├── CMakeLists.txt
│   ├── unit/
│   │   ├── test_registry.cpp
│   │   ├── test_actor_framework.cpp
│   │   ├── test_state_store.cpp
│   │   └── test_rest_handlers.cpp    # NEW: REST API unit tests
│   └── integration/
│       ├── test_grpc_service.cpp
│       └── test_rest_service.cpp     # NEW: REST API integration tests
├── docker/
│   ├── Dockerfile
│   └── docker-compose.yml
└── Makefile (convenience targets)
```

### 2.5 Key Class Implementations

**1. REST Service (1,200 LOC)**

```cpp
// include/fleetmanager/rest_service.hpp
namespace fleetmanager::rest {

class RESTServiceImpl {
public:
    explicit RESTServiceImpl(
        std::shared_ptr<registry::DeviceRegistry> registry,
        std::shared_ptr<actor::ActorScheduler> scheduler,
        std::shared_ptr<persistence::StateStore> store,
        std::shared_ptr<pubsub::SubscriptionManager> pubsub,
        uint16_t port = 8080
    );
    
    Result<void> Start();
    void Stop();

private:
    void SetupRoutes_();
    void HandlePostDevices_(const httplib::Request& req, httplib::Response& res);
    void HandleGetDevices_(const httplib::Request& req, httplib::Response& res);
    void HandleGetDevice_(const httplib::Request& req, httplib::Response& res);
    void HandleHealth_(const httplib::Request& req, httplib::Response& res);
    
    std::unique_ptr<httplib::Server> server_;
    std::shared_ptr<registry::DeviceRegistry> registry_;
};

}  // namespace fleetmanager::rest
```

**2. Device Registry (400 LOC)**

```cpp
// include/fleetmanager/registry.hpp
class DeviceRegistry {
public:
    Result<std::string> RegisterDevice(const DeviceProfile& profile);
    std::optional<DeviceProfile> GetDevice(const std::string& device_id) const;
    std::vector<DeviceProfile> GetDevicesForShard(uint32_t shard_id) const;
    size_t TotalDevices() const;

private:
    mutable std::shared_mutex registry_lock_;
    std::unordered_map<std::string, DeviceProfile> devices_;
    std::unordered_map<uint32_t, std::vector<std::string>> shard_assignments_;
};
```

**3. Actor Framework (600 LOC)**

```cpp
// include/fleetmanager/actor_framework.hpp
template<typename MessageT>
class Mailbox {
public:
    explicit Mailbox(size_t capacity);
    bool TryEnqueue(MessageT&& msg);
    bool TryDequeue(MessageT& out);

private:
    std::vector<std::optional<MessageT>> queue_;
    std::atomic<uint64_t> head_{0}, tail_{0};
};

class ActorScheduler {
public:
    explicit ActorScheduler(size_t worker_thread_count);
    void Submit(std::function<void()> task);
    void WaitAll();

private:
    std::vector<std::thread> workers_;
    std::queue<std::function<void()>> task_queue_;
    std::mutex queue_lock_;
    std::condition_variable cv_;
};
```

**4. Observability (500 LOC)**

```cpp
// include/fleetmanager/observability.hpp
class MetricsCollector {
public:
    void RecordRegistration(const std::string& device_type, double latency_ms);
    void RecordRESTRequest(const std::string& method, const std::string& path, int status, double latency_ms);
    void ExportMetrics();

private:
    std::unordered_map<std::string, double> latency_histogram_;
    std::unordered_map<std::string, int64_t> counters_;
};
```

### 2.6 Testing Strategy (Phase 1)

**Unit Tests (60% coverage target)**

```cpp
// tests/unit/test_rest_handlers.cpp
TEST(RESTHandlers, PostDeviceSuccess) {
    auto registry = std::make_shared<DeviceRegistry>();
    auto rest_service = RESTServiceImpl(registry, ...);
    
    httplib::Client cli("localhost", 8080);
    nlohmann::json device_json = {
        {"device_id", "host-001"},
        {"device_type", "HOST_DPU_ATTACHED"},
        {"hardware_capabilities", {...}}
    };
    
    auto res = cli.Post("/api/v1/devices", 
                        device_json.dump(), 
                        "application/json");
    
    EXPECT_EQ(res->status, 201);  // Created
    auto response = nlohmann::json::parse(res->body);
    EXPECT_EQ(response["device_id"], "host-001");
    EXPECT_FALSE(response["shard_id"].is_null());
}

TEST(RESTHandlers, PostDeviceDuplicate) {
    auto registry = std::make_shared<DeviceRegistry>();
    auto rest_service = RESTServiceImpl(registry, ...);
    
    httplib::Client cli("localhost", 8080);
    nlohmann::json device_json = {...};
    
    // First registration
    auto res1 = cli.Post("/api/v1/devices", device_json.dump(), "application/json");
    EXPECT_EQ(res1->status, 201);
    
    // Duplicate registration
    auto res2 = cli.Post("/api/v1/devices", device_json.dump(), "application/json");
    EXPECT_EQ(res2->status, 409);  // Conflict
    
    auto error_response = nlohmann::json::parse(res2->body);
    EXPECT_EQ(error_response["error"]["code"], "DEVICE_ALREADY_EXISTS");
}

TEST(RESTHandlers, GetDevices) {
    auto registry = std::make_shared<DeviceRegistry>();
    auto rest_service = RESTServiceImpl(registry, ...);
    
    // Register devices
    DeviceProfile profile;
    profile.set_device_id("host-001");
    registry->RegisterDevice(profile);
    
    httplib::Client cli("localhost", 8080);
    auto res = cli.Get("/api/v1/devices?limit=50&offset=0");
    
    EXPECT_EQ(res->status, 200);
    auto response = nlohmann::json::parse(res->body);
    EXPECT_GE(response["devices"].size(), 1);
}

TEST(RESTHandlers, GetHealth) {
    auto rest_service = RESTServiceImpl(...);
    
    httplib::Client cli("localhost", 8080);
    auto res = cli.Get("/api/v1/health");
    
    EXPECT_EQ(res->status, 200);
    auto response = nlohmann::json::parse(res->body);
    EXPECT_EQ(response["status"], "OK");
    EXPECT_TRUE(response.contains("primary"));
    EXPECT_TRUE(response.contains("shard_id"));
}

TEST(RESTHandlers, PostDeviceHeartbeat) {
    auto rest_service = RESTServiceImpl(...);
    
    httplib::Client cli("localhost", 8080);
    nlohmann::json heartbeat_json = {
        {"timestamp", "2026-06-11T14:35:12.456Z"},
        {"status", "OK"}
    };
    
    auto res = cli.Post("/api/v1/devices/host-001/heartbeat",
                        heartbeat_json.dump(),
                        "application/json");
    
    EXPECT_EQ(res->status, 200);
}
```

**Integration Tests (REST + gRPC)**

```cpp
// tests/integration/test_dual_apis.cpp
TEST(DualAPIs, RestAndGRPCShareRegistry) {
    // Start both servers
    auto rest_server = StartRESTServer(8080);
    auto grpc_server = StartGRPCServer(5051);
    
    // Register via REST
    httplib::Client rest_cli("localhost", 8080);
    auto rest_res = rest_cli.Post("/api/v1/devices", device_json, "application/json");
    EXPECT_EQ(rest_res->status, 201);
    
    // Query via gRPC
    auto grpc_stub = CreateGRPCClient("localhost:5051");
    auto device = grpc_stub->GetDevice("host-001");
    
    // Device should be visible in both APIs
    EXPECT_EQ(device.device_id(), "host-001");
}
```

### 2.7 Build & Test Commands

```bash
# Build
mkdir build && cd build
cmake ..
make -j$(nproc)

# Run tests
ctest --output-on-failure

# Run linter
clang-format -i src/*.cpp include/**/*.hpp

# Build Docker image
docker build -f docker/Dockerfile -t fleetmanager:phase1 .
```

### 2.8 Acceptance Criteria

- [ ] Both REST (8080) and gRPC (5051) servers start successfully
- [ ] All 70+ unit tests passing
- [ ] Device registration working via REST API (curl test)
- [ ] Device listing working via REST API (GET /devices)
- [ ] Device registration working via gRPC (grpcurl test)
- [ ] Health check endpoint returns correct status (/health)
- [ ] Metrics exposed on port 8081 (/metrics endpoint)
- [ ] REST and gRPC share same device registry (no duplication)
- [ ] Code coverage >= 60%
- [ ] Docker image builds successfully
- [ ] REST API latency <100ms for device registration (p99)

---

## 3. Phase 2: Delta Compilation (Weeks 5-8)

### 3.1 Objectives

Implement the state compilation engine and DASH southbound driver.

### 3.2 Deliverables

| Component | Owner | Estimated LOC | Status |
|-----------|-------|--------------|--------|
| `StateCompilationEngine` | Team A | 1200 | To Do |
| `DASHDriver` (gNMI) | Team B | 600 | To Do |
| `DeltaCompiler` | Team A | 400 | To Do |
| Integration tests | Team C | 800 | To Do |
| Performance benchmarks | Team C | 300 | To Do |

**Total Phase 2: ~3,300 LOC**

### 3.3 Protobuf Schema

The schema models the **15-object DASH catalog** organized by scope ladder
(Fleet → Device → VNET → Group → ENI). See
[`vm-eni-provisioning-design.md`](./vm-eni-provisioning-design.md) and
[Learning-DashNet ch 03](../Learning-DashNet/03-Object-Model-and-Scopes.md)
for the full model.

```protobuf
// proto/models.proto
syntax = "proto3";

message DeviceProfile {
    string device_id = 1;
    enum DeviceType {
        HOST_LINUX = 0;
        HOST_DPU_ATTACHED = 1;
        APPLIANCE_DASH_STANDALONE = 2;
        APPLIANCE_DASH_CHASSIS = 3;
    }
    DeviceType device_type = 2;

    message HardwareCapabilities {
        uint32 max_flow_table_entries = 1;
        uint32 max_routes_per_eni = 2;
        uint32 max_acl_rules = 3;
    }
    HardwareCapabilities capabilities = 3;
}

// All published objects are wrapped in ConfigEntry (vm-eni decision #18).
message ConfigEntry {
    ConfigMetadata metadata = 1;
    bytes payload = 2;             // proto-binary of kind-specific message
}

message ConfigMetadata {
    uint32 schema_version = 1;
    DashObjectKind kind = 2;
    uint64 revision = 3;           // per-object monotonic counter
    string tenant_id = 4;
    bytes payload_digest = 5;      // SHA-256, 32 bytes (vm-eni decision #26)
    google.protobuf.Timestamp issued_at = 6;
    string trace_context = 7;
}

enum DashObjectKind {
    DASH_OBJECT_UNSPECIFIED = 0;
    // Fleet scope
    ROUTING_TYPE = 1;
    // Device scope
    APPLIANCE = 10; HOST_SPEC = 11; TUNNEL = 12; QOS = 13;
    PREFIX_TAG = 14; HA_SET = 15;
    // VNET scope
    VNET = 20; VNET_MAPPING_MANIFEST = 21; VNET_MAPPING_CHUNK = 22;
    PA_VALIDATION = 23;
    // Group scope
    ROUTE_GROUP = 30; ACL_GROUP = 31; METER_POLICY = 32;
    OUTBOUND_PORT_MAP = 33;
    // ENI scope
    NIC_SPEC = 40; CONTAINER_SPEC = 41; HA_SCOPE = 42; ENI = 43;
}

// Per-device materialized cache slice. Note: NicGoalState is composed
// in-process by NicActor and is NOT carried in this message — it is never
// published.
message DeviceCache {
    string device_id = 1;
    HostSpec host = 2;
    map<string, Appliance> appliance = 3;
    map<string, Tunnel> tunnels = 4;
    map<string, Qos> qos = 5;
    map<string, PrefixTag> prefix_tags = 6;
    map<string, HaSet> ha_sets = 7;
    map<string, Vnet> vnets = 8;
    map<string, VnetMappingManifest> vnet_mapping_manifests = 9;
    map<string, RouteGroup> route_groups = 10;
    map<string, AclGroup> acl_groups = 11;
    map<string, MeterPolicy> meter_policies = 12;
    map<string, ContainerSpec> containers = 13;
    map<string, NicSpec> nic_specs = 14;
    map<string, HaScope> ha_scopes = 15;
}

message DeltaPlan {
    string device_id = 1;
    repeated CompiledDelta deltas = 2;
    string trace_id = 3;
}

message CompiledDelta {
    string command_id = 1;
    string trace_id = 2;
    enum Operation { CREATE = 0; UPDATE = 1; DELETE = 2; }
    Operation operation = 3;
    DashObjectKind kind = 4;
    string object_id = 5;
    uint32 wave = 6;               // 0..6 per vm-eni §4
    bytes prior_hash = 7;          // SHA-256, may be empty for CREATE
    bytes target_hash = 8;         // SHA-256
    bytes target_config = 9;       // proto-binary of the kind-specific message
}

// VnetMapping is delivered as manifest + sub-1MiB chunks (vm-eni #8).
message VnetMappingManifest {
    string vnet_id = 1;
    repeated Chunk chunks = 2;
    message Chunk {
        string chunk_id = 1;
        bytes digest = 2;          // SHA-256 of chunk payload
        uint32 row_count = 3;
        string blob_url = 4;
    }
}
```

### 3.4 NicActor Composition Outline

The `StateCompilationEngine` is invoked **from inside** the per-ENI
`NicActor`. The actor reads its `NicSpec` plus the HDO-cached references
(Vnet, RouteGroup, AclGroup slots, MeterPolicy, Tunnel, RoutingType,
HaScope), composes `NicGoalState` in-process, hashes it with SHA-256, and
diffs against the last-composed copy to produce a `DeltaPlan`.

`NicGoalState` is **never serialized to etcd, never sent over gNMI**. It
exists only inside the actor process. Per
[vm-eni decision #17](./vm-eni-provisioning-design.md) and
[Learning-DashNet myth #13](../Learning-DashNet/16-Common-Misconceptions.md).

```cpp
// src/actor/nic_actor.cpp
class NicActor {
public:
    // Reactive entry point: fired on any input change (NIC spec, vnet,
    // group, global). All three ShardSet pods compose; only Primary
    // dispatcher actuates.
    Result<void> OnInputChange(const InputEvent& ev) {
        auto refs = hdo_->ResolveRefs(nic_spec_);
        if (!refs.complete()) {
            TransitionTo_(State::WAITING_REFS);
            return Ok();
        }

        TransitionTo_(State::COMPOSING);
        NicGoalState goal = Compose_(nic_spec_, refs);

        std::array<uint8_t, 32> hash =
            sha256::Hash(CanonicalSerialize(goal));   // vm-eni #27

        if (hash == last_composed_hash_) {
            TransitionTo_(State::READY);              // no-op
            return Ok();
        }

        DeltaPlan plan = DiffToWaves_(last_composed_, goal);
        plan.set_trace_id(ev.trace_id);
        dispatcher_->Submit(plan);                    // primary-only

        last_composed_      = std::move(goal);
        last_composed_hash_ = hash;
        TransitionTo_(State::READY);
        return Ok();
    }

private:
    // Compose the denormalized ENI program by joining NIC refs against
    // the HDO's global/group/vnet caches. Shape per vm-eni §3.
    NicGoalState Compose_(const NicSpec& spec, const ResolvedRefs& r);

    // Map a structural diff of two NicGoalStates into per-DASH-object
    // CREATE/UPDATE/DELETE deltas, sorted into waves 0–6 (vm-eni §4).
    DeltaPlan DiffToWaves_(const NicGoalState& prior, const NicGoalState& next);

    HostDeviceActor*           hdo_;
    NicSpec                    nic_spec_;
    NicGoalState               last_composed_;
    std::array<uint8_t, 32>    last_composed_hash_{};
    State                      state_{State::WAITING_BOOTSTRAP};
};
```

**State machine** (from vm-eni / Learning-DashNet ch 11):

`WAITING_BOOTSTRAP` → `WAITING_REFS` → `COMPOSING` → `READY` ⇄
`RECONFIGURING`, with terminal `DRAINING` → `TERMINATED` and lateral
`INCOMPLETE_MAPPING` / `OVER_CAPACITY` / `VALIDATION_REJECTED`.

### 3.5 DASH Driver Implementation Outline

The driver is the **southbound actuator** for the full DASH 15-object
catalog, not just ENI. The dispatcher hands it a wave-ordered `DeltaPlan`;
the driver fans out per-kind verbs. Programming wave order (vm-eni §4):
Wave 0 globals → Wave 1 transports/HA → Wave 2 groups → Wave 3 vnets →
Wave 4 mappings → Wave 5 ENI → Wave 6 per-ENI bindings. DELETE is the
reverse.

```cpp
// src/southbound/dash_driver.cpp
class DashSouthboundDriver : public SouthboundDriver {
public:
    Result<void> ApplyDeltaPlan(const DeltaPlan& plan) override {
        for (uint32_t wave = 0; wave <= 6; ++wave) {
            for (const auto& d : plan.deltas()) {
                if (d.wave() != wave) continue;
                if (auto r = DispatchOne_(d); !r.has_value()) return r;
            }
        }
        return Ok();
    }

private:
    Result<void> DispatchOne_(const CompiledDelta& d) {
        switch (d.kind()) {
            case ROUTING_TYPE:           return ApplyRoutingType_(d);
            case APPLIANCE:              return ApplyAppliance_(d);
            case TUNNEL:                 return ApplyTunnel_(d);
            case QOS:                    return ApplyQos_(d);
            case PREFIX_TAG:             return ApplyPrefixTag_(d);
            case HA_SET:                 return ApplyHaSet_(d);
            case ROUTE_GROUP:            return ApplyRouteGroup_(d);
            case ACL_GROUP:              return ApplyAclGroup_(d);
            case METER_POLICY:           return ApplyMeterPolicy_(d);
            case OUTBOUND_PORT_MAP:      return ApplyOutboundPortMap_(d);
            case VNET:                   return ApplyVnet_(d);
            case PA_VALIDATION:          return ApplyPaValidation_(d);
            case VNET_MAPPING_MANIFEST:  return ApplyVnetMappingManifest_(d);
            case VNET_MAPPING_CHUNK:     return ApplyVnetMappingChunk_(d);
            case ENI:                    return ApplyEni_(d);
            case HA_SCOPE:               return ApplyHaScope_(d);
            // EniRoute, EniAclBinding, EniRouteRule are emitted as
            // sub-kinds of ENI in wave 6.
            default: return Error("unsupported kind");
        }
    }

    // Each Apply_() builds a gNMI SetRequest at the canonical path for
    // that kind, attaches the proto-binary payload, and CAS's against the
    // last-applied SHA-256 (skip if hash matches — idempotency).
    gnmi::SetRequest BuildSet_(const CompiledDelta& d);

    std::unique_ptr<gnmi::gNMI::Stub> gnmi_stub_;
};
```

The Linux and SONiC drivers (Phase 4) implement the same interface for
non-DPU device types; DASH is the primary southbound for DPU-enabled
hosts.

### 3.6 Performance Benchmarks

```cpp
// tests/benchmarks/nic_compose_benchmark.cpp
#include <benchmark/benchmark.h>

static void BenchmarkNicCompose(benchmark::State& state) {
    auto hdo = MakeHdoWithFixtures("host-001", /*full ref set*/);
    NicSpec spec = MakeRealisticNicSpec("eni-bench");
    NicActor no(hdo.get(), spec);

    for (auto _ : state) {
        no.OnInputChange(InputEvent{.trace_id="bench"});
    }
}

BENCHMARK(BenchmarkNicCompose);
```

### 3.7 Integration Tests

```cpp
// tests/integration/test_delta_compilation.cpp
TEST(DeltaCompilation, ComposeNicGoalStateForVmEni) {
    // Setup: HDO with global+group+vnet caches hydrated for one device.
    auto hdo = MakeHdoWithFixtures("host-001",
        /*tunnels=*/{"tun-1"},
        /*route_groups=*/{"rg-prod"},
        /*acl_groups=*/{"acl-vnic", "acl-vnet"},
        /*meter_policies=*/{"mp-tier1"},
        /*vnets=*/{"vnet-100"});

    // NIC spec = pure reference bundle (vm-eni decision #1).
    NicSpec spec;
    spec.set_eni_id("ENI_dpu-east-12_aa:bb:cc:dd:ee:ff");
    spec.set_vnet_id("vnet-100");
    spec.set_outbound_route_group_v4("rg-prod");
    *spec.mutable_outbound_acl_v4() = {"acl-vnic", "", "acl-vnet"};
    *spec.mutable_inbound_acl_v4()  = {"acl-vnic", "", "acl-vnet"};
    spec.set_outbound_meter_policy("mp-tier1");
    spec.set_inbound_meter_policy("mp-tier1");

    NicActor no(hdo.get(), spec);
    auto result = no.OnInputChange(InputEvent{.trace_id="t-001"});

    ASSERT_TRUE(result.has_value());
    EXPECT_EQ(no.state(), State::READY);
    EXPECT_EQ(no.last_composed_hash().size(), 32);  // SHA-256

    // DeltaPlan should target Eni in wave 5 plus 6 ACL bindings in wave 6.
    auto plan = no.last_dispatched_plan();
    EXPECT_TRUE(HasDeltaForKind(plan, ENI, /*wave=*/5));
    EXPECT_EQ(CountDeltasInWave(plan, /*wave=*/6), 6);
}

TEST(DeltaCompilation, WaitsForRefsWhenVnetMissing) {
    auto hdo = MakeHdoWithFixtures("host-001", /*no vnets*/);
    NicSpec spec; spec.set_vnet_id("vnet-missing");

    NicActor no(hdo.get(), spec);
    no.OnInputChange(InputEvent{.trace_id="t-002"});

    EXPECT_EQ(no.state(), State::WAITING_REFS);
    EXPECT_TRUE(no.last_dispatched_plan().deltas().empty());
}
```

### 3.8 Acceptance Criteria

- [ ] Delta compilation latency <50ms (p99 <100ms) via benchmarks
- [ ] All dependency resolution tests passing
- [ ] DASH driver can create/update/delete ENI (tested via gNMI mock)
- [ ] Integration tests with mock southbound endpoints
- [ ] 80+ unit tests (cumulative 800+ tests)

---

## 4. Phase 3: HA & Persistence (Weeks 9-12)

### 4.1 Objectives

Implement high availability (K8s Lease), RocksDB state persistence, and hot standby reconciliation.

### 4.2 Deliverables

| Component | Owner | Estimated LOC | Status |
|-----------|-------|--------------|--------|
| `HACoordinator` (K8s Lease) | Team A | 400 | To Do |
| `RocksDBStateStore` | Team B | 500 | To Do |
| `ReconciliationEngine` | Team A | 600 | To Do |
| Integration tests (failover scenarios) | Team C | 1000 | To Do |
| Stress tests | Team C | 500 | To Do |

**Total Phase 3: ~3,000 LOC**

### 4.3 K8s Integration

```cpp
// src/ha_coordinator.cpp
class HACoordinator {
public:
    Result<void> Initialize() {
        // Attempt to acquire K8s Lease
        auto lease_result = k8s_client_->CreateOrUpdateLease(
            "dashfabric-shard-" + shard_id_,
            pod_name_,
            std::chrono::seconds(30)  // TTL
        );
        
        if (lease_result.has_value()) {
            am_i_primary_.store(true);
            LOG(INFO, "Pod {} acquired lease for shard {}", pod_name_, shard_id_);
        } else {
            am_i_primary_.store(false);
            LOG(INFO, "Pod {} did not acquire lease (other pod has it)", pod_name_);
        }
        
        // Start renewal loop
        lease_renewal_thread_ = std::thread(&HACoordinator::LeaseRenewalLoop_, this);
        
        return Ok();
    }
    
    void LeaseRenewalLoop_() {
        while (!shutting_down_.load()) {
            std::this_thread::sleep_for(std::chrono::seconds(10));
            
            auto renew_result = k8s_client_->UpdateLease(
                "dashfabric-shard-" + shard_id_,
                pod_name_,
                std::chrono::seconds(30)
            );
            
            if (renew_result.has_value()) {
                am_i_primary_.store(true);
            } else {
                am_i_primary_.store(false);
                
                // Attempt to re-acquire
                auto reacquire = k8s_client_->CreateOrUpdateLease(
                    "dashfabric-shard-" + shard_id_,
                    pod_name_,
                    std::chrono::seconds(30)
                );
                
                if (reacquire.has_value()) {
                    am_i_primary_.store(true);
                }
            }
        }
    }
};
```

### 4.4 RocksDB State Persistence

```cpp
// src/state_store.cpp
class StateStore {
public:
    explicit StateStore(const std::string& db_path) {
        rocksdb::Options options;
        options.create_if_missing = true;
        options.write_buffer_size = 256 * 1024 * 1024;  // 256MB
        
        rocksdb::Status status = rocksdb::DB::Open(options, db_path, &db_);
        if (!status.ok()) {
            throw std::runtime_error("Failed to open RocksDB: " + status.ToString());
        }
    }
    
    Result<void> SaveDeviceState(const std::string& device_id, const DeviceState& state) {
        std::string key = MakeKey_(device_id, "STATE");
        std::string value;
        
        if (!state.SerializeToString(&value)) {
            return Error("Failed to serialize device state");
        }
        
        rocksdb::Status status = db_->Put(rocksdb::WriteOptions(), key, value);
        if (!status.ok()) {
            return Error("RocksDB write failed: " + status.ToString());
        }
        
        return Ok();
    }
    
    Result<DeviceState> LoadDeviceState(const std::string& device_id) {
        std::string key = MakeKey_(device_id, "STATE");
        std::string value;
        
        rocksdb::Status status = db_->Get(rocksdb::ReadOptions(), key, &value);
        if (status.IsNotFound()) {
            return Error("Device state not found");
        }
        if (!status.ok()) {
            return Error("RocksDB read failed: " + status.ToString());
        }
        
        DeviceState state;
        if (!state.ParseFromString(value)) {
            return Error("Failed to deserialize device state");
        }
        
        return state;
    }
};
```

### 4.5 Reconciliation Engine

Reconciliation compares the **SHA-256 content hash** of the locally
composed `NicGoalState` against the device-reported hash (vm-eni
decisions #26 and #27). A mismatch triggers re-composition and a fresh
`DeltaPlan`. There is no `config_version`; revisions are per-object
monotonic counters and don't compare across objects.

```cpp
// src/reconciliation_engine.cpp
class ReconciliationEngine {
public:
    Result<void> ReconcileEni(
        const std::string& eni_id,
        const std::string& trace_id
    ) {
        auto* no = nic_actors_.Find(eni_id);
        if (!no) return Error("no NicActor for eni " + eni_id);

        // Local SHA-256 of the actor's last-composed NicGoalState.
        std::array<uint8_t, 32> local_hash = no->last_composed_hash();

        // Device-reported SHA-256 of the same ENI's currently-applied
        // program (returned by the agent via the southbound).
        auto device_hash = driver_->QueryEniHash(eni_id);
        if (!device_hash.has_value()) {
            return Error("driver query failed");
        }

        if (local_hash == device_hash.value()) {
            metrics_.devices_in_sync++;
            return Ok();
        }

        LOG(WARNING, "drift on eni {} — re-composing", eni_id);
        // Force the actor to re-compose and re-dispatch.
        no->OnInputChange(InputEvent{.kind=DRIFT, .trace_id=trace_id});
        metrics_.corrective_actions_taken++;
        return Ok();
    }
};
```

### 4.6 Failover Testing

```cpp
// tests/integration/test_failover_scenario.cpp
TEST(HACoordinator, PrimaryFailoverToStandby) {
    // Setup: 2 pods (primary + standby)
    auto k8s_mock = std::make_shared<MockKubernetesClient>();
    
    auto primary_ha = HACoordinator("shard-0", "pod-0", "dashfabric", k8s_mock);
    auto standby_ha = HACoordinator("shard-0", "pod-1", "dashfabric", k8s_mock);
    
    // Initialize: pod-0 gets lease
    primary_ha.Initialize();
    standby_ha.Initialize();
    
    EXPECT_TRUE(primary_ha.IsPrimary());
    EXPECT_FALSE(standby_ha.IsPrimary());
    
    // Simulate primary pod crash: lease expires
    std::this_thread::sleep_for(std::chrono::seconds(35));
    
    // Standby should have acquired lease
    EXPECT_TRUE(standby_ha.IsPrimary());
}
```

### 4.7 Acceptance Criteria

- [ ] K8s Lease coordination working (tested with mock K8s client)
- [ ] RocksDB state persistence verified (data survives pod restart)
- [ ] Reconciliation detects state drift (drift detection working)
- [ ] Failover RTO <35 seconds (measured via integration test)
- [ ] Hot standby replication working (STANDBY keeps cache warm)
- [ ] All HA tests passing (100+ tests)

---

## 5. Phase 4: Multi-Protocol Support (Weeks 13-15)

### 5.1 Objectives

Implement SONiC (SAI Thrift) and Linux (netlink) southbound drivers.

### 5.2 Deliverables

| Component | Owner | Estimated LOC | Status |
|-----------|-------|--------------|--------|
| `SONiCDriver` (SAI Thrift) | Team B | 600 | To Do |
| `LinuxDriver` (netlink) | Team B | 500 | To Do |
| Integration tests (all 3 platforms) | Team C | 800 | To Do |

**Total Phase 4: ~1,900 LOC**

### 5.3 SONiC Driver

Falls back to the SAI verb set for non-DASH SONiC devices. Same
`SouthboundDriver` interface: `ApplyDeltaPlan(const DeltaPlan&)` walks
waves 0–6 and dispatches per-kind to SAI calls (only the kinds SONiC
supports — typically `RouteGroup`/`AclGroup` analogs, no DASH-native
ENI).

```cpp
// src/southbound/sonic_driver.cpp
class SONiCDriver : public SouthboundDriver {
public:
    Result<void> ApplyDeltaPlan(const DeltaPlan& plan) override {
        for (const auto& d : plan.deltas()) {
            switch (d.kind()) {
                case ROUTE_GROUP: ApplyRouteSai_(d); break;
                case ACL_GROUP:   ApplyAclSai_(d);   break;
                // ... other kinds SONiC handles natively
                default: continue;  // skip DASH-only kinds
            }
        }
        return Ok();
    }
};
```

### 5.4 Linux Driver

Fallback for plain Linux hosts (no DPU). Programs interfaces, routes, and
iptables via netlink/nftables for the subset of kinds that map to kernel
features.

```cpp
// src/southbound/linux_driver.cpp
class LinuxDriver : public SouthboundDriver {
public:
    Result<void> ApplyDeltaPlan(const DeltaPlan& plan) override {
        for (const auto& d : plan.deltas()) {
            switch (d.kind()) {
                case ENI:         ApplyInterfaceNetlink_(d); break;
                case ROUTE_GROUP: ApplyRouteNetlink_(d);     break;
                case ACL_GROUP:   ApplyNftables_(d);         break;
                default: continue;
            }
        }
        return Ok();
    }
};
```

### 5.5 Acceptance Criteria

- [ ] All 3 southbound drivers functional
- [ ] Integration tests passing for each driver
- [ ] Device-agnostic delta compilation working (select driver by device type)
- [ ] Cross-platform testing (DASH + SONiC + Linux in same shard)

---

## 6. Phase 5: Testing & Optimization (Weeks 16-18)

### 6.1 Comprehensive Test Suite

**Coverage Target: 85%+**

```bash
# Unit tests
ctest --output-on-failure

# Integration tests
./tests/integration/run_all_integration_tests.sh

# Load tests
./tests/load/benchmark_device_registration.sh  # Target: 10,000 devices in 100 seconds
./tests/load/benchmark_delta_compilation.sh    # Target: <50ms per device
./tests/load/benchmark_failover.sh             # Target: RTO <35s

# Stress tests
./tests/stress/sustained_load_24h.sh           # Run 24-hour load test
```

### 6.2 Performance Optimization

| Metric | Target | Current | Gap |
|--------|--------|---------|-----|
| Registration latency (p99) | <150ms | TBD | TBD |
| Delta compilation (p99) | <100ms | TBD | TBD |
| Southbound RPC latency | <50ms | TBD | TBD |
| Memory per device | <1MB | TBD | TBD |
| Throughput (deltas/sec) | >1000 | TBD | TBD |

**Optimization Techniques:**
- Lock-free data structures where applicable
- Memory pooling for frequently-allocated objects
- RocksDB tuning (write buffer size, block cache)
- gRPC connection pooling

### 6.3 Acceptance Criteria

- [ ] Unit test coverage >= 85%
- [ ] All performance targets met
- [ ] 24-hour stress test passing
- [ ] No memory leaks detected (valgrind/ASan)
- [ ] No race conditions detected (ThreadSanitizer)

---

## 7. Phase 6: Deployment & Documentation (Weeks 19-20)

### 7.1 Kubernetes Manifests

```yaml
# k8s/fleetmanager-statefulset.yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: fleetmanager-workers
  namespace: dashfabric
spec:
  serviceName: fleetmanager
  replicas: 4
  selector:
    matchLabels:
      app: fleetmanager-worker
  template:
    metadata:
      labels:
        app: fleetmanager-worker
    spec:
      containers:
      - name: fleetmanager
        image: fleetmanager:1.0.0
        ports:
        - name: grpc
          containerPort: 5051
        - name: metrics
          containerPort: 8081
        volumeMounts:
        - name: state-storage
          mountPath: /var/lib/fleetmanager
  volumeClaimTemplates:
  - metadata:
      name: state-storage
    spec:
      accessModes: ["ReadWriteOnce"]
      resources:
        requests:
          storage: 100Gi
```

### 7.2 Documentation

**Deliverables:**
- [ ] Deployment guide (K8s installation, configuration)
- [ ] Operations runbook (common tasks, troubleshooting)
- [ ] API documentation (gRPC service definition)
- [ ] Monitoring guide (Prometheus metrics, Jaeger traces)
- [ ] Developer guide (building, testing, contributing)

### 7.3 Acceptance Criteria

- [ ] Kubernetes deployment working in test cluster
- [ ] Monitoring dashboard created (Grafana)
- [ ] All documentation complete and reviewed
- [ ] Handoff to DevOps/SRE team

---

## 8. Development Environment Setup

### 8.1 Prerequisites

```bash
# Ubuntu 20.04+
sudo apt-get update
sudo apt-get install -y \
    build-essential \
    cmake \
    protobuf-compiler \
    libprotobuf-dev \
    libgrpc++-dev \
    grpc-tools \
    librocksdb-dev \
    libboost-all-dev \
    libopentelemetry-dev

# Docker
sudo apt-get install docker-ce docker-compose

# Optional: clang-format, clang-tidy
sudo apt-get install clang-format clang-tools
```

### 8.2 Local Development Workflow

```bash
# Clone repository
git clone <repo> FleetManager
cd FleetManager

# Setup build environment
mkdir build && cd build
cmake -DCMAKE_BUILD_TYPE=Debug ..
make -j$(nproc)

# Run tests
ctest --output-on-failure -V

# Run service locally
./fleetmanager_service --config=../config/local.yaml

# In another terminal: test gRPC
grpcurl -plaintext localhost:5051 list
```

---

## 9. Risk Mitigation

| Risk | Probability | Impact | Mitigation |
|------|-------------|--------|-----------|
| **Performance latency targets missed** | Medium | High | Early benchmarking (Phase 2), profiling, optimization sprints |
| **K8s Lease contention issues** | Low | Medium | Mock K8s integration tests, failover drills |
| **Multi-driver integration complexity** | Medium | Medium | Driver abstraction layer, comprehensive unit tests per driver |
| **RocksDB durability concerns** | Low | High | Backup testing, recovery drills, WAL verification |
| **Observability overhead** | Medium | Medium | Sampling strategies, async metrics collection |

---

## 10. Deliverables Timeline

```
Week 1-4    ✓ Phase 1: Core Foundation
            - gRPC service running
            - Device registry working
            - Basic metrics exported
            
Week 5-8    ✓ Phase 2: Delta Compilation
            - Delta compilation engine
            - DASH driver functional
            - <50ms compilation latency
            
Week 9-12   ✓ Phase 3: HA & Persistence
            - K8s Lease coordination
            - RocksDB persistence
            - <35s failover RTO
            
Week 13-15  ✓ Phase 4: Multi-Protocol Support
            - SONiC and Linux drivers
            - Cross-platform testing
            
Week 16-18  ✓ Phase 5: Testing & Optimization
            - 85%+ test coverage
            - Performance targets met
            - 24-hour stress test passing
            
Week 19-20  ✓ Phase 6: Deployment & Documentation
            - K8s manifests
            - Complete documentation
            - Ready for production
```

---

## 11. Success Metrics & Go-Live Checklist

### 11.1 Final Acceptance Criteria

- [ ] All 4 production performance targets met (registration, compilation, RPC, reconciliation)
- [ ] Zero production incidents during 1-week pre-production soak test
- [ ] HA failover tested and verified (RTO <35 seconds)
- [ ] All three southbound protocols working (DASH, SONiC, Linux)
- [ ] Comprehensive observability (tracing, metrics, logs) functional
- [ ] Documentation complete and reviewed
- [ ] Team trained on deployment and operations

### 11.2 Go-Live Readiness

**Pre-Production Checklist:**
- [ ] Load test: 10,000 devices registered
- [ ] Failover test: PRIMARY crash → STANDBY takeover
- [ ] Upgrade test: Rolling update with zero downtime
- [ ] Disaster recovery: Full state recovery from RocksDB backup
- [ ] Monitoring: All metrics and traces flowing to observability platform
- [ ] Runbooks: All operational procedures documented and tested

---

## Conclusion

This implementation plan provides a **structured, phased approach** to delivering FleetManager as a production-grade microservice. Each phase has clear objectives, deliverables, and acceptance criteria. The timeline (16-20 weeks) is realistic for a team of 3-4 engineers with proper tooling and dependencies pre-vetted.

**Next Steps:**
1. Allocate team and assign owners
2. Set up development environment (CI/CD pipeline)
3. Begin Phase 1 implementation
4. Establish weekly review cadence
