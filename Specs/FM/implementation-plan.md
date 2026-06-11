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

message DeviceState {
    string device_id = 1;
    map<string, HostDeviceObject> host_objects = 2;
    map<string, ContainerObject> container_objects = 3;
    map<string, NICObject> nic_objects = 4;
    uint64 config_version = 5;
}

message CompiledDelta {
    string command_id = 1;
    string trace_id = 2;
    
    enum Operation {
        CREATE = 0;
        UPDATE = 1;
        DELETE = 2;
    }
    Operation operation = 3;
    
    string target_object_type = 4;
    bytes target_config_bytes = 5;
}

message DeviceGoalState {
    string device_id = 1;
    uint64 state_version = 2;
    repeated CompiledDelta compiled_config = 3;
    string consistency_hash = 4;
}
```

### 3.4 StateCompilationEngine Implementation Outline

```cpp
// src/state_compilation_engine.cpp
class StateCompilationEngine {
public:
    Result<DeviceGoalState> CompileDeltas(
        const std::string& device_id,
        const DeviceProfile& device_profile,
        const std::vector<uint8_t>& new_intent_bytes,
        const std::string& trace_id
    ) {
        // Step 1: Load cached state from RocksDB
        auto cached = state_store_->LoadDeviceState(device_id);
        
        // Step 2: Deserialize new intent
        DeviceState new_state;
        new_state.ParseFromArray(new_intent_bytes.data(), new_intent_bytes.size());
        
        // Step 3: Compute deltas (create/update/delete)
        auto deltas = ComputeDeltas_(device_id, cached, new_state, trace_id);
        
        // Step 4: Resolve dependencies (topological sort)
        auto sorted_deltas = ResolveDependencies_(deltas, trace_id);
        
        // Step 5: Compile device-specific configs
        CompileDeviceConfigs_(sorted_deltas, device_profile, trace_id);
        
        // Step 6: Save to RocksDB
        state_store_->SaveDeviceState(device_id, new_state);
        
        // Step 7: Return DeviceGoalState
        DeviceGoalState goal_state;
        goal_state.set_device_id(device_id);
        goal_state.set_state_version(new_state.config_version());
        for (const auto& delta : sorted_deltas) {
            *goal_state.add_compiled_config() = delta;
        }
        
        return goal_state;
    }
    
private:
    std::vector<CompiledDelta> ComputeDeltas_(...) { /* Implement */ }
    std::vector<CompiledDelta> ResolveDependencies_(...) { /* Implement */ }
    void CompileDeviceConfigs_(...) { /* Implement */ }
};
```

### 3.5 DASH Driver Implementation Outline

```cpp
// src/southbound/dash_driver.cpp
class DASHDriver : public SouthboundDriver {
public:
    Result<void> CreateENI(const CompiledDelta& delta) override {
        // Step 1: Deserialize delta config (DASH ENI protobuf)
        proto::DashEni eni;
        eni.ParseFromArray(delta.target_config_bytes.data(), delta.target_config_bytes.size());
        
        // Step 2: Build gNMI SetRequest
        auto set_request = DeltaToSetRequest_(delta);
        
        // Step 3: Execute gRPC call
        ::grpc::ClientContext context;
        gnmi::SetResponse response;
        auto status = gnmi_stub_->Set(&context, set_request, &response);
        
        if (!status.ok()) {
            return Error("gNMI Set failed: " + status.error_message());
        }
        
        return Ok();
    }

private:
    gnmi::SetRequest DeltaToSetRequest_(const CompiledDelta& delta) {
        gnmi::SetRequest request;
        auto* update = request.add_update();
        
        // Set path: /config/dash/enis/eni-123
        auto* path = update->mutable_path();
        path->add_elem()->set_name("config");
        path->add_elem()->set_name("dash");
        path->add_elem()->set_name("enis");
        path->add_elem()->set_name(delta.target_object_id);
        
        // Set value: protobuf bytes
        auto* val = update->mutable_val();
        val->set_bytes_val(delta.target_config_bytes.data(), delta.target_config_bytes.size());
        
        return request;
    }
};
```

### 3.6 Performance Benchmarks

```cpp
// tests/benchmarks/delta_compilation_benchmark.cpp
#include <benchmark/benchmark.h>

static void BenchmarkDeltaCompilation(benchmark::State& state) {
    auto state_store = std::make_shared<StateStore>("/tmp/test_state");
    auto compiler = std::make_shared<StateCompilationEngine>(state_store);
    
    for (auto _ : state) {
        DeviceProfile profile;
        profile.set_device_id("host-001");
        
        std::vector<uint8_t> intent_bytes;
        // Populate with sample intent
        
        compiler->CompileDeltas("host-001", profile, intent_bytes, "trace-001");
    }
}

BENCHMARK(BenchmarkDeltaCompilation);
```

### 3.7 Integration Tests

```cpp
// tests/integration/test_delta_compilation.cpp
TEST(DeltaCompilation, CreateContainerAndNIC) {
    auto state_store = std::make_shared<StateStore>("/tmp/test_state");
    auto compiler = std::make_shared<StateCompilationEngine>(state_store);
    
    // Create intent: 1 container with 2 NICs
    DeviceState intent;
    auto* container = (*intent.mutable_container_objects())["container-001"].mutable_container();
    // ... populate container config ...
    
    DeviceProfile profile;
    profile.set_device_id("host-001");
    
    std::string serialized;
    intent.SerializeToString(&serialized);
    
    auto result = compiler->CompileDeltas(
        "host-001",
        profile,
        std::vector<uint8_t>(serialized.begin(), serialized.end()),
        "trace-001"
    );
    
    ASSERT_TRUE(result.has_value());
    EXPECT_EQ(result.value().compiled_config_size(), 3);  // 1 container + 2 NICs
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

```cpp
// src/reconciliation_engine.cpp
class ReconciliationEngine {
public:
    Result<void> ReconcileDevice(
        const std::string& device_id,
        const DeviceProfile& device_profile,
        const std::string& trace_id
    ) {
        // Step 1: Load cached state
        auto cached_result = state_store_->LoadDeviceState(device_id);
        if (!cached_result.has_value()) {
            return Error("Failed to load cached state");
        }
        
        const auto& cached_state = cached_result.value();
        
        // Step 2: Query actual device state (via southbound RPC)
        auto actual_result = QueryDeviceActualState_(device_id, device_profile);
        if (!actual_result.has_value()) {
            return Error("Failed to query device state");
        }
        
        const auto& actual_state = actual_result.value();
        
        // Step 3: Compute hashes
        uint64_t cached_hash = ComputeStateHash_(cached_state);
        uint64_t actual_hash = ComputeStateHash_(actual_state);
        
        // Step 4: Detect drift
        if (cached_hash != actual_hash) {
            LOG(WARNING, "State drift detected for device {}", device_id);
            
            // Step 5: Compute corrective deltas
            auto deltas = compiler_->ComputeDeltas(device_id, cached_state, actual_state, trace_id);
            
            // Step 6: Execute corrective deltas
            for (const auto& delta : deltas) {
                executor_->ExecuteDelta(delta, device_id, device_profile.device_type(),
                    [this](const Result<void>& result) {
                        if (!result.has_value()) {
                            LOG(ERROR, "Failed to execute corrective delta: {}", result.error());
                        }
                    }
                );
            }
            
            metrics_.devices_drifted++;
            metrics_.corrective_actions_taken += deltas.size();
        } else {
            LOG(DEBUG, "State reconciliation OK for device {}", device_id);
            metrics_.devices_in_sync++;
        }
        
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

```cpp
// src/southbound/sonic_driver.cpp
class SONiCDriver : public SouthboundDriver {
public:
    Result<void> CreateENI(const CompiledDelta& delta) override {
        // Deserialize SAI config from delta
        std::vector<SAIAttribute> attributes;
        auto convert_result = DeltaToSAIAttributes_(delta, attributes);
        if (!convert_result.has_value()) {
            return convert_result;
        }
        
        // Call SAI API
        sai_object_id_t eni_id = SAI_NULL_OBJECT_ID;
        sai_status_t status = sai_eni_api->create_eni(&eni_id, attributes);
        
        if (status != SAI_STATUS_SUCCESS) {
            return Error("SAI ENI creation failed: " + std::to_string(status));
        }
        
        return Ok();
    }
};
```

### 5.4 Linux Driver

```cpp
// src/southbound/linux_driver.cpp
class LinuxDriver : public SouthboundDriver {
public:
    Result<void> CreateENI(const CompiledDelta& delta) override {
        // Use netlink to configure interface
        auto netlink_msg = DeltaToNetlinkMsg_(delta);
        
        int sock = socket(AF_NETLINK, SOCK_RAW, NETLINK_ROUTE);
        if (sock < 0) {
            return Error("Failed to create netlink socket");
        }
        
        // Send netlink message
        ssize_t ret = send(sock, netlink_msg->data(), netlink_msg->size(), 0);
        close(sock);
        
        if (ret < 0) {
            return Error("Netlink send failed");
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
