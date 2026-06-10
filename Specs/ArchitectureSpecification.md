# DashFabric: Comprehensive Architecture Specification

**Version:** 1.0  
**Status:** Specification Draft  
**Date:** June 2026  
**Audience:** Architecture Review, Implementation Teams, DevOps, SRE

---

## Executive Summary

DashFabric is a **production-grade, cloud-native distributed network control plane** designed to manage thousands of Data Processing Unit (DPU) enabled hosts and DASH-compliant appliances in hyperscale data centers. It implements a hierarchical object lifecycle management system where a central control unit:

1. Registers and profiles compute/DPU hosts
2. Creates and manages **Host Device Objects** representing physical hardware
3. Manages **Container Objects** representing virtual network endpoints
4. Provisions and lifecycle-manages **NIC Objects** representing network interfaces
5. Programs DASH-compliant data plane hardware through a Hardware Abstraction Layer
6. Maintains high availability through stateful replication, leader election, and zero-downtime failover
7. Scales horizontally across thousands of devices through partition-based sharding
8. Provides comprehensive observability via OpenTelemetry integration

The system is designed for **extreme reliability** (fault tolerance, automatic recovery) and **extreme scale** (10,000+ hosts per datacenter, distributed across multiple control plane replicas).

---

## Part 1: System Architecture Overview

### 1.1 System Context Diagram

```
┌────────────────────────────────────────────────────────────────────────────┐
│                                                                            │
│                          UPSTREAM CONTROL PLANE                            │
│                     (External SDN Controller / Portal)                     │
│                                                                            │
│                         PubSub Configuration Store                         │
│                    (Redis Streams / etcd / Kafka)                         │
│                                                                            │
└────────────────────────┬───────────────────────────────────────────────────┘
                         │ Configuration Topics
                         │ /config/hosts/{HOST-ID}
                         │ /config/hosts/{HOST-ID}/containers/{CONTAINER-GUID}
                         │ /config/hosts/{HOST-ID}/containers/{C}/nics/{NIC-ID}
                         │
┌────────────────────────▼───────────────────────────────────────────────────┐
│                                                                            │
│                       DASHFABRIC CONTROL PLANE                            │
│                     (Kubernetes StatefulSet Cluster)                      │
│                                                                            │
│  ┌────────────────────────────────────────────────────────────────────┐  │
│  │  API Gateway / Device Registration Router (Stateless Deployment)   │  │
│  │  - Validates device registration requests                          │  │
│  │  - Consistent hashing to select shard assignment                   │  │
│  │  - Load balances heartbeats and telemetry                          │  │
│  └────────────────────────────────────────────────────────────────────┘  │
│                         │                                                  │
│         ┌───────────────┼───────────────┐                                 │
│         ▼               ▼               ▼                                  │
│  ┌─────────────┐ ┌─────────────┐ ┌─────────────┐                         │
│  │  SHARD 0    │ │  SHARD 1    │ │  SHARD N    │ (StatefulSet)           │
│  │             │ │             │ │             │                         │
│  │ worker-0 ◄──┼─┼─┬─ worker-1 │ │ worker-2N ◄─┼─┼─┬─ ...              │
│  │ (PRIMARY)   │ │ │           │ │ (PRIMARY)   │ │ │                    │
│  │ [Subs/Prog] │ │ │ (STANDBY) │ │ [Subs/Prog] │ │ │                    │
│  │             │ │ │ [Listen]  │ │             │ │ │                    │
│  │ K8s Lease   │ │ │           │ │ K8s Lease   │ │ │                    │
│  │ Lock        │ │ │           │ │ Lock        │ │ │                    │
│  └──────┬──────┘ │ └─────┬─────┘ └──────┬──────┘ │ │                    │
│         │        │       │              │        │ │                    │
│         └┼───────┼───────┼──────────────┼────────┘ │                    │
│          │       │       │              │          │                    │
│  ┌───────▼───────▼───────▼──────────────▼──────────▼─┐                  │
│  │ Persistent Volume Claims (RocksDB State Store)    │                  │
│  │ - Device configuration cache                      │                  │
│  │ - Object lifecycle state                          │                  │
│  │ - Compiled goal state                             │                  │
│  │ - Reconciliation checkpoint                       │                  │
│  └───────────────────────────────────────────────────┘                  │
│                                                                            │
└────────────────────────┬───────────────────────────────────────────────────┘
                         │ Southbound APIs
                         │ gRPC / gNMI / P4Runtime
                         │
┌────────────────────────▼───────────────────────────────────────────────────┐
│                                                                            │
│                        DATA PLANE HOSTS & DPUs                            │
│                                                                            │
│  ┌──────────────────┐  ┌──────────────────┐  ┌──────────────────┐       │
│  │ DPU-Enabled Host │  │ DASH Appliance   │  │ SmartNIC Host    │  ...  │
│  │                  │  │ (Chassis)        │  │                  │       │
│  │ ┌──────────────┐ │  │ ┌──────────────┐ │  │ ┌──────────────┐ │       │
│  │ │ Host Device  │ │  │ │ Host Device  │ │  │ │ Host Device  │ │       │
│  │ │ Object       │ │  │ │ Object       │ │  │ │ Object       │ │       │
│  │ │              │ │  │ │              │ │  │ │              │ │       │
│  │ │┌────────────┐│ │  │ │┌────────────┐│ │  │ │┌────────────┐│ │       │
│  │ ││ Container1 ││ │  │ ││ Container1 ││ │  │ ││ Container1 ││ │       │
│  │ ││ Object     ││ │  │ ││ Object     ││ │  │ ││ Object     ││ │       │
│  │ ││            ││ │  │ ││            ││ │  │ ││            ││ │       │
│  │ ││┌──────────┐││ │  │ ││┌──────────┐││ │  │ ││┌──────────┐││ │       │
│  │ │││ NIC1 Obj│││ │  │ │││ NIC1 Obj│││ │  │ │││ NIC1 Obj│││ │       │
│  │ │││ [ENI Cfg]│││ │  │ │││ [ENI Cfg]│││ │  │ │││ [ENI Cfg]│││ │       │
│  │ ││└──────────┘││ │  │ ││└──────────┘││ │  │ ││└──────────┘││ │       │
│  │ ││┌──────────┐││ │  │ ││┌──────────┐││ │  │ ││┌──────────┐││ │       │
│  │ │││ NIC2 Obj│││ │  │ │││ NIC2 Obj│││ │  │ │││ NIC2 Obj│││ │       │
│  │ │││ [ENI Cfg]│││ │  │ │││ [ENI Cfg]│││ │  │ │││ [ENI Cfg]│││ │       │
│  │ ││└──────────┘││ │  │ ││└──────────┘││ │  │ ││└──────────┘││ │       │
│  │ │└────────────┘│ │  │ │└────────────┘│ │  │ │└────────────┘│ │       │
│  │ │              │ │  │ │              │ │  │ │              │ │       │
│  │ │┌────────────┐│ │  │ │┌────────────┐│ │  │ │┌────────────┐│ │       │
│  │ ││ Container2 ││ │  │ ││ Container2 ││ │  │ ││ Container2 ││ │       │
│  │ ││ Object     ││ │  │ ││ Object     ││ │  │ ││ Object     ││ │       │
│  │ ││            ││ │  │ ││            ││ │  │ ││            ││ │       │
│  │ ││┌──────────┐││ │  │ ││┌──────────┐││ │  │ ││┌──────────┐││ │       │
│  │ │││ NIC1 Obj│││ │  │ │││ NIC1 Obj│││ │  │ │││ NIC1 Obj│││ │       │
│  │ │││ [ENI Cfg]│││ │  │ │││ [ENI Cfg]│││ │  │ │││ [ENI Cfg]│││ │       │
│  │ ││└──────────┘││ │  │ ││└──────────┘││ │  │ ││└──────────┘││ │       │
│  │ │└────────────┘│ │  │ │└────────────┘│ │  │ │└────────────┘│ │       │
│  │ └──────────────┘ │  │ └──────────────┘ │  │ └──────────────┘ │       │
│  └──────────────────┘  └──────────────────┘  └──────────────────┘       │
│                                                                            │
│  Data Plane                                                              │
│  - DASH P4 pipeline (BMv2 simulator or hardware)                        │
│  - VPC routing, VNET peering                                            │
│  - ACL enforcement, metering                                            │
│  - ENI lifecycle management via SAI                                     │
└────────────────────────────────────────────────────────────────────────────┘
```

### 1.2 Design Philosophy

This architecture embraces the following core principles:

1. **Hierarchical Object Lifecycle** - Objects are created/destroyed in parent-child order with subscription cascades
2. **Eventual Consistency** - Hot standby replication enables zero-RTO failover without complex consensus
3. **Kubernetes-Native** - Leverages K8s Leases, StatefulSets, PVCs for primitives; no custom coordination protocols
4. **Scale-Out via Sharding** - Consistent hashing distributes device load across shard pairs
5. **Observable by Default** - OpenTelemetry spans wrap every operation for end-to-end tracing
6. **Fault-Tolerant by Construction** - Replication, reconciliation loops, and periodic audits recover from transient failures
7. **Multi-Southbound HAL** - Support DASH (gNMI/P4Runtime), SONiC (SAI), and standard Linux hosts

---

## Part 2: Object Domain Model

### 2.1 Core Object Definitions

#### 2.1.1 DeviceProfile

Captured at device registration time. Represents the **physical and capability matrix** of a host or DPU appliance.

```yaml
DeviceProfile:
  device_id: UUID                      # Globally unique device identifier
  host_ip: IPv4 / IPv6                 # Management IP for connectivity
  device_type:                         # Enumeration
    - HOST_LINUX                       # Standard Linux host (uses tc/netlink)
    - HOST_DPU_ATTACHED                # SmartNIC DPU attached to host
    - APPLIANCE_DASH_STANDALONE        # Standalone DASH appliance
    - APPLIANCE_DASH_CHASSIS           # Multi-card DASH chassis
  
  hardware_capabilities:
    max_flow_table_entries: uint32     # ASIC flow capacity
    max_routes_per_eni: uint32         # Routing table size
    max_acl_rules: uint32              # ACL capacity
    max_nics: uint32                   # Maximum ENI count
    max_containers: uint32             # Maximum workload count
    max_cps: uint32                    # Connections-per-second capacity
    supported_encapsulation: [VXLAN, NVGRE]
    p4_target: string                  # P4 target (BMv2, TNA, DPDK)
    cpu_cores: uint32                  # Processing cores
    memory_gb: uint32                  # RAM capacity
  
  registration_timestamp: ISO8601      # When device registered
  registration_version: uint64         # Sequence number for audit
  locality:
    datacenter_id: string              # DC identifier
    rack_id: string                    # Physical rack location
    position: string                   # Physical position (for spares)
```

#### 2.1.2 HostDeviceObject

Created upon successful device registration. Manages the lifecycle of all containers on a host.

```yaml
HostDeviceObject:
  id: UUID                             # Object instance ID
  device_id: UUID                      # Reference to DeviceProfile
  state: INITIALIZING | READY | DRAINING | TERMINATED
  
  subscriptions:
    config_topic: "/config/hosts/{device_id}"
    state_version: uint64              # Current config version tracked
  
  lifecycle:
    created_at: ISO8601
    registered_at: ISO8601
    last_heartbeat: ISO8601
    last_config_update: ISO8601
  
  containers:                          # Map<container_id, ContainerObject>
    [container_guid]: ContainerObject
  
  host_state:
    total_allocated_memory: uint64
    total_allocated_cpu: uint32
    total_nics_configured: uint32
    container_count: uint32
    nics_count: uint32
  
  audit_state:
    last_reconciliation: ISO8601
    drift_detected: boolean
    last_full_config_push: ISO8601
```

**Lifecycle Transitions:**
- **Registration Event** → Creates object in `INITIALIZING` state
- **Config Received** → Transitions to `READY` when initial config processed
- **Deregistration Event** → Transitions to `DRAINING`, orchestrates container/NIC deletion
- **Timeout/Heartbeat Loss** → Transitions to `TERMINATED`, triggers failover cleanup

#### 2.1.3 ContainerObject

Represents a logical network namespace, workload, or virtual machine on the host. Created in response to configuration indicating container intent.

```yaml
ContainerObject:
  id: UUID                             # Object instance ID
  container_guid: string               # Globally unique container GUID
  host_device_id: UUID                 # Parent reference
  state: INITIALIZING | READY | RECONFIGURING | DESTROYING | TERMINATED
  
  subscriptions:
    config_topic: "/config/hosts/{host_id}/containers/{container_guid}"
    state_version: uint64
  
  networking_config:
    vpc_id: string
    vnet_id: string
    namespace_id: string               # Linux netns or container namespace
    overlay_ip_primary: IPv4
    overlay_ip_secondary: [IPv4]       # Secondary IPs
    ipv6_addresses: [IPv6]
  
  nics:                                # Map<nic_id, NICObject>
    [nic_id]: NICObject
  
  container_state:
    primary_nic_id: string             # Designated primary NIC
    nic_count: uint32
    total_allocated_bandwidth: uint64
    total_acl_rules: uint32
  
  lifecycle:
    created_at: ISO8601
    config_applied_at: ISO8601
    last_policy_update: ISO8601
    expected_teardown: ISO8601        # If deregistration queued
```

**Lifecycle Transitions:**
- **Parent Ready + Config Received** → Creates object in `INITIALIZING`
- **NICs Configured** → Transitions to `READY`
- **Policy Update** → Enters `RECONFIGURING`, applies delta, returns to `READY`
- **Parent Draining** → Transitions to `DESTROYING`, orchestrates NIC deletion
- **NIC Cleanup Complete** → Transitions to `TERMINATED`

#### 2.1.4 NICObject

Represents an Elastic Network Interface (ENI) in DASH terminology. Manages the lifecycle of a single network interface attached to a container.

```yaml
NICObject:
  id: UUID                             # Object instance ID
  nic_id: string                       # NIC identifier (e.g., eth0, eni-xxx)
  container_id: UUID                   # Parent reference
  host_device_id: UUID                 # Grandparent reference
  state: INITIALIZING | READY | POLICY_UPDATE | DESTROYING | TERMINATED
  
  subscriptions:
    config_topic: "/config/hosts/{host_id}/containers/{container_id}/nics/{nic_id}"
    state_version: uint64
  
  eni_config:
    eni_id: string                     # DASH ENI ID
    vpc_id: string
    vnet_id: string
    primary_ipv4: IPv4
    secondary_ipv4s: [IPv4]
    ipv6_addresses: [IPv6]
    mac_address: string
    security_group_ids: [string]
    subnet_id: string
  
  dataplane_config:
    routing_table_id: string           # Associated route table
    acl_group_ids: [string]            # Associated ACL groups
    metering_policy_id: string
    encapsulation_type: VXLAN | NVGRE
    vni: uint32                        # VXLAN Network Identifier
    bandwidth_limit: uint64            # Mbps
    cps_limit: uint32                  # Connections per second
  
  eni_state:
    program_status: PENDING | SUCCESS | FAILED | RETRYING
    last_program_time: ISO8601
    program_attempt_count: uint32
    program_error_message: string      # Last error if FAILED
  
  hal_reference:
    target_type: DASH | SONIC | LINUX
    device_handle: string              # Device-specific reference
    configuration_hash: string         # Checksum of programmed config
```

**Lifecycle Transitions:**
- **Container Ready + Config Received** → Creates object in `INITIALIZING`
- **ENI Created in Data Plane** → Transitions to `READY`
- **Policy Update** → Enters `POLICY_UPDATE`, applies delta rules, returns to `READY`
- **Parent Destroying** → Transitions to `DESTROYING`
- **ENI Deleted from Data Plane** → Transitions to `TERMINATED`

### 2.2 Compiled State Objects

#### 2.2.1 DeviceGoalState

Produced by the **State Compilation Engine** when a raw configuration intent is received. Represents the **concrete, target byte configuration** that the physical device will execute.

```yaml
DeviceGoalState:
  device_id: UUID
  state_version: uint64                # Incremented on each compilation
  compilation_timestamp: ISO8601
  trace_id: string                     # OpenTelemetry trace ID
  
  compiled_config:
    host_device_deltas:
      - operation: CREATE | UPDATE | DELETE
        object_type: CONTAINER
        object_id: string
        target_config: object          # Device-specific compiled config
        
      - operation: CREATE | UPDATE | DELETE
        object_type: NIC
        object_id: string
        target_config:
          eni_creation:
            eni_id: string
            vpc_id: string
            mac_address: string
            ip_addresses: [IP]
          acl_rules:
            - rule_id: string
              priority: uint32
              match: {protocol, ports, addresses}
              action: ALLOW | DROP
          route_entries:
            - destination: CIDR
              nexthop: IP | APPLIANCE_ID
              encapsulation: VXLAN | DIRECT
          metering_buckets:
            - bucket_id: string
              prefix_list: [CIDR]
  
  consistency_hash: string              # SHA256(config) for audit
  dependency_graph:
    - from_object: string
      to_object: string
      operation: DEPENDS_ON | UNBLOCKS
```

#### 2.2.2 DeltaCommand

Atomic unit of work; produced by comparing DeviceGoalState with current cached state.

```yaml
DeltaCommand:
  command_id: UUID
  trace_id: string                     # Propagated from parent
  sequence_number: uint32              # Ordering within a delta set
  
  operation: CREATE | UPDATE | DELETE
  target_object: NICObject | ContainerObject | HostDeviceObject
  
  # For CREATE
  if operation == CREATE:
    resource_definition:
      type: string                     # Resource type
      config: protobuf | json          # Device-specific encoding
      
  # For UPDATE
  if operation == UPDATE:
    field_delta:
      changed_fields: [string]
      old_values: [value]
      new_values: [value]
      
  # For DELETE
  if operation == DELETE:
    resource_reference:
      resource_id: string
      cleanup_dependencies: boolean
  
  scheduling:
    created_at: ISO8601
    scheduled_for: ISO8601            # May be deferred
    executed_at: ISO8601              # When actually applied
    
  result:
    status: PENDING | SUCCESS | FAILED | RETRYING
    error_message: string
    retry_count: uint32
    last_error_time: ISO8601
```

---

## Part 3: Control Plane Architecture

### 3.1 API Gateway & Registration Router

**Purpose:** Stateless ingress point for device registrations, heartbeats, and telemetry.

**Deployment:** Kubernetes Deployment (stateless, horizontally scalable)

**Responsibilities:**

1. **Device Registration Validation**
   - Validate device_id format and uniqueness
   - Query DeviceProfile schema against hardware compatibility matrix
   - Reject devices with incompatible capabilities (e.g., insufficient ACL capacity)

2. **Consistent Hashing Ring**
   - Compute `hash(device_id) → shard_index`
   - Maintain in-memory hash ring with current shard replicas
   - Update ring on StatefulSet scale events (via K8s watch)

3. **Shard Assignment & Routing**
   - Route device to assigned shard pair via gRPC
   - Ensure both PRIMARY and STANDBY receive registration profile
   - Store assignment in distributed cache (Redis) for failover reference

4. **Telemetry & Heartbeat Aggregation**
   - Collect heartbeats from devices
   - Aggregate into per-shard health metrics
   - Forward to observability pipeline

**Data Flow:**

```
Device Registration Request (gRPC)
    ↓
API Gateway validates registration
    ↓
Compute shard assignment: hash(device_id) % shard_count
    ↓
Route to worker-0 (PRIMARY) via gRPC
    ├─ Call: RegisterDevice(DeviceProfile)
    ├─ Response: shard_id, lease_token, subscription_topics
    └─ Side effect: trigger Upstream Sync Engine subscription
```

### 3.2 Upstream Sync Engine

**Purpose:** Establish and maintain per-device subscriptions to the upstream PubSub configuration store.

**Deployment:** Embedded in each StatefulSet worker pod

**Responsibilities:**

1. **Dynamic Subscription Management**
   - Upon device registration: subscribe to `/config/hosts/{device_id}`
   - Upon container creation notification: subscribe to `/config/hosts/{device_id}/containers/{container_guid}`
   - Upon NIC creation notification: subscribe to `/config/hosts/{device_id}/containers/{container_id}/nics/{nic_id}`
   - Maintain per-device subscription map in memory

2. **Intent Reception & Routing**
   - Receive configuration intent updates from PubSub store
   - Route to appropriate **State Compilation Engine** actor for the device
   - Attach OpenTelemetry trace context (W3C Trace Context header)
   - Handle backpressure: reject new subscriptions if device queue is saturated

3. **Subscription Lifecycle**
   - On device deregistration: unsubscribe from all associated topics
   - On container deletion: unsubscribe from container/NIC topics
   - On network error: retry with exponential backoff (max 30s between retries)

4. **Concurrent State Replication (STANDBY-Specific)**
   - STANDBY worker also subscribes to same topics
   - Maintains read-only cache of device configuration
   - Processes telemetry updates without applying commands
   - Ready for zero-latency takeover if PRIMARY fails

**Topic Subscription Hierarchy:**

```
/config/hosts/{HOST-ID}
  ├─ Notification: host capacity, policies, deregistration intent
  ├─ Creates: HostDeviceObject if not exists
  │
  └─ /config/hosts/{HOST-ID}/containers/{CONTAINER-GUID}
      ├─ Notification: container IP, policies, VPC/VNET assignment
      ├─ Creates: ContainerObject if not exists
      │
      └─ /config/hosts/{HOST-ID}/containers/{CONTAINER-GUID}/nics/{NIC-ID}
          ├─ Notification: ENI config, security groups, routing table
          ├─ Creates: NICObject if not exists
          └─ Triggers: HAL layer ENI provisioning
```

### 3.3 State Compilation & Delta Graph Engine

**Purpose:** Compare received configuration intent with cached state; produce atomic set of delta commands.

**Deployment:** Embedded in each StatefulSet worker pod, async actor-per-device model

**Core Algorithm:**

```
CompileDeltas(device_id, new_intent):
  cached_state ← RocksDB[device_id]
  
  if cached_state == null:
    # First time seeing this intent
    deltas = [
      DeltaCommand(operation=CREATE, object_type=CONTAINER, ...)
      for each container in new_intent.containers
    ]
  else:
    # Incremental update
    # Step 1: Detect object creations (in new_intent but not in cached_state)
    for container in new_intent.containers:
      if container not in cached_state.containers:
        deltas.append(DeltaCommand(CREATE, CONTAINER, ...))
    
    # Step 2: Detect object deletions (in cached_state but not in new_intent)
    for container in cached_state.containers:
      if container not in new_intent.containers:
        deltas.append(DeltaCommand(DELETE, CONTAINER, ...))
    
    # Step 3: Detect object updates (modified fields)
    for container in intersection(new_intent, cached_state):
      if container.properties_differ():
        deltas.append(DeltaCommand(UPDATE, CONTAINER, ...))
    
    # Step 4: Process NIC-level changes (similar cascade)
    for container in new_intent.containers:
      for nic in container.nics:
        if nic not in cached_state[container].nics:
          deltas.append(DeltaCommand(CREATE, NIC, ...))
  
  # Build dependency graph: NIC creation depends on Container creation
  dependency_graph = ComputeDependencies(deltas)
  
  # Topological sort to determine execution order
  deltas_ordered = TopologicalSort(deltas, dependency_graph)
  
  # Compile device-specific configurations
  for delta in deltas_ordered:
    delta.target_config = CompileDeviceConfig(delta, device_profile)
  
  # Generate consistency hash for audit
  consistency_hash = SHA256(serialize(deltas_ordered))
  
  # Save to cache and RocksDB
  RocksDB[device_id] = deltas_ordered
  
  return DeviceGoalState(
    device_id=device_id,
    compiled_config=deltas_ordered,
    consistency_hash=consistency_hash
  )
```

**Dependency Resolution:**

The engine constructs a directed acyclic graph (DAG) of dependencies:

```
HostDeviceObject created
  ├─ DEPENDS_ON: device registration OK
  ├─ UNBLOCKS: ContainerObject creation
  └─ UNBLOCKS: Upstream Sync subscriptions
  
ContainerObject created
  ├─ DEPENDS_ON: HostDeviceObject in READY state
  ├─ DEPENDS_ON: parent config received
  ├─ UNBLOCKS: NICObject creation
  └─ UNBLOCKS: container namespace setup
  
NICObject created
  ├─ DEPENDS_ON: ContainerObject in READY state
  ├─ DEPENDS_ON: NIC config received
  ├─ UNBLOCKS: HAL layer ENI provisioning
  └─ UNBLOCKS: southbound RPC execution

NICObject deleted
  ├─ DEPENDS_ON: NICObject in READY state
  ├─ UNBLOCKS: ContainerObject cleanup
  └─ UNBLOCKS: resource reclamation

ContainerObject deleted
  ├─ DEPENDS_ON: all NICObjects TERMINATED
  ├─ UNBLOCKS: HostDeviceObject cleanup
  └─ UNBLOCKS: namespace teardown

HostDeviceObject deleted
  ├─ DEPENDS_ON: all ContainerObjects TERMINATED
  ├─ UNBLOCKS: device unregistration
  └─ UNBLOCKS: shard resource cleanup
```

### 3.4 Actor Model for Per-Device Processing

To achieve scale and parallelism, each device is assigned a **dedicated actor** within a shard worker:

```
ShardsWorker:
  actors:
    [device_0]: DeviceActor {
      state_cache: RocksDB key-space
      subscription_channels: PubSub consumers
      outstanding_commands: queue
      reconciliation_ticker: timer
    }
    [device_1]: DeviceActor { ... }
    ...
    [device_N]: DeviceActor { ... }
```

**DeviceActor Responsibilities:**

1. **Receive intent updates** from Upstream Sync Engine
2. **Invoke compilation** via State Compilation Engine
3. **Queue delta commands** to Southbound Driver Layer
4. **Track delta execution** status
5. **Trigger reconciliation** on periodic ticker (30-60s interval)
6. **Update RocksDB** checkpoint after successful command execution

**Concurrency Model:**

- All DeviceActors within a shard run **concurrently** via async/await or thread pool
- No lock contention between devices (each has independent state)
- RocksDB writes are serialized per device (single writer per key-space partition)
- Southbound RPC calls are multiplexed via persistent gRPC connections

### 3.5 Southbound Driver Layer (Hardware Abstraction)

**Purpose:** Translate abstract delta commands into device-specific programming actions.

**Deployment:** Embedded in each StatefulSet worker pod

**Multi-Protocol Support:**

```
DeltaCommand (abstract)
    ↓
    ├─→ [DASH Appliance] → gNMI Set RPC (protobuf + ZMQ)
    │   Example: Set /config/dash/enis/eni-123 with ENI spec
    │
    ├─→ [SONiC Host] → SAI Thrift RPC
    │   Example: sai_eni_api_create(eni_attr_list)
    │
    ├─→ [SmartNIC (P4)] → P4Runtime RPC
    │   Example: Write table entries via P4Runtime controller
    │
    └─→ [Linux Host] → gRPC or netlink API
        Example: ip link add eth0 type veth peer name veth0
```

#### 3.5.1 DASH Appliance Programming (gNMI)

```protobuf
// Example: Create ENI on DASH appliance
service DASH_gNMI {
  rpc Set(SetRequest) returns (SetResponse);
}

SetRequest {
  update: [
    UpdateVal {
      path: {
        elem: [ { name: "config" }, { name: "dash" }, { name: "enis" }, { name: "eni-123" } ]
      }
      val: {
        bytes_val: <protobuf-encoded ENI config>
      }
    }
  ]
}

// ENI Config (protobuf)
message DashEni {
  string eni_id = 1;
  string vpc_id = 2;
  repeated string acl_group_ids = 3;
  string routing_table_id = 4;
  repeated string ipv4_addresses = 5;
  string mac_address = 6;
}
```

**Southbound Driver Implementation:**

```python
class DASHSouthboundDriver:
  def __init__(self, host_ip, host_port=6030):
    self.stub = GNMIStub(channel=grpc.aio.secure_channel(
      f"{host_ip}:{host_port}",
      credentials=...
    ))
  
  async def create_eni(self, eni_config: DeltaCommand):
    """Translates DeltaCommand.target_config to gNMI Set RPC"""
    # Compile ENI config to protobuf
    dash_eni = DashEni(
      eni_id=eni_config.target_config.eni_id,
      vpc_id=eni_config.target_config.vpc_id,
      acl_group_ids=eni_config.target_config.acl_group_ids,
      ...
    )
    
    # Construct gNMI Set request
    set_request = gnmi_pb2.SetRequest(
      update=[
        gnmi_pb2.Update(
          path=gnmi_pb2.Path(
            elem=[
              gnmi_pb2.PathElem(name="config"),
              gnmi_pb2.PathElem(name="dash"),
              gnmi_pb2.PathElem(name="enis"),
              gnmi_pb2.PathElem(name=eni_config.target_config.eni_id),
            ]
          ),
          val=gnmi_pb2.TypedValue(
            bytes_val=dash_eni.SerializeToString()
          )
        )
      ]
    )
    
    # Execute RPC with timeout
    try:
      response = await asyncio.wait_for(
        self.stub.Set(set_request),
        timeout=5.0
      )
      return (SUCCESS, response)
    except asyncio.TimeoutError:
      return (FAILED, "gNMI Set timeout")
    except Exception as e:
      return (FAILED, str(e))
```

#### 3.5.2 SONiC SAI Programming (Thrift)

```python
class SONiCSouthboundDriver:
  def __init__(self, host_ip, host_port=9092):
    self.client = saithrift_client(host_ip, host_port)
  
  async def create_eni(self, eni_config: DeltaCommand):
    """Translates DeltaCommand to SAI API calls"""
    eni_attr_list = [
      sai.SaiAttribute(id=sai.SAI_ENI_ATTR_ID, value=eni_config.target_config.eni_id),
      sai.SaiAttribute(id=sai.SAI_ENI_ATTR_VPC_ID, value=eni_config.target_config.vpc_id),
      sai.SaiAttribute(id=sai.SAI_ENI_ATTR_MAC, value=eni_config.target_config.mac_address),
      ...
    ]
    
    try:
      eni_handle = await self.client.sai_eni_api_create(eni_attr_list)
      return (SUCCESS, eni_handle)
    except Exception as e:
      return (FAILED, str(e))
```

### 3.6 Reconciliation & Audit Engine

**Purpose:** Periodically verify that cached state matches actual device state.

**Deployment:** Embedded in each DeviceActor

**Reconciliation Loop (Triggered every 60 seconds per device):**

```
ReconciliationTick(device_id):
  # Step 1: Query device for current configuration
  actual_state = await GetDeviceState(device_id)
  
  # Step 2: Load cached expected state
  expected_state = RocksDB[device_id]
  
  # Step 3: Compute state hash
  actual_hash = SHA256(serialize(actual_state))
  expected_hash = SHA256(serialize(expected_state))
  
  # Step 4: Detect drift
  if actual_hash != expected_hash:
    log(ERROR, f"State drift detected on {device_id}")
    
    # Step 5: Compute corrective deltas
    corrective_deltas = ComputeDeltas(device_id, expected_state)
    
    # Step 6: Inject corrective deltas into execution queue
    for delta in corrective_deltas:
      southbound_queue.push(delta)
    
    # Step 7: Emit metric and trace
    metrics.counter_state_drift_detected.inc({"device_id": device_id})
    span.add_event("state_drift_detected", {"delta_count": len(corrective_deltas)})
  else:
    log(INFO, f"State reconciliation OK for {device_id}")
    metrics.gauge_devices_in_sync.set(1, {"device_id": device_id})
```

---

## Part 4: High Availability & Fault Tolerance

### 4.1 Stateful Replication Architecture

**Deployment Model: Paired Active-Passive**

```
Shard 0:
  ┌─────────────────────────────────────┐
  │ worker-0 (PRIMARY)                  │
  │ ┌──────────────────────────────────┐│
  │ │ K8s Lease Lock                   ││
  │ │ (coordination.k8s.io/v1)         ││
  │ │ Owner: worker-0                  ││
  │ │ TTL: 30 seconds                  ││
  │ │ Renew every: 10 seconds          ││
  │ └──────────────────────────────────┘│
  │ │
  │ ├─ Outbound RPC Calls             │
  │ │  (Programs devices)              │
  │ │                                  │
  │ ├─ Subscription Reception          │
  │ │  (Receives intent updates)       │
  │ │                                  │
  │ └─ Telemetry Updates (reads)       │
  │                                     │
  │ ┌──────────────────────────────────┐│
  │ │ RocksDB (PVC-backed NVMe)         ││
  │ │ Async writes per device key       ││
  │ └──────────────────────────────────┘│
  └─────────────────────────────────────┘
        Shared PVC Mount Point

  ┌─────────────────────────────────────┐
  │ worker-1 (STANDBY)                  │
  │ ┌──────────────────────────────────┐│
  │ │ K8s Lease Lock                   ││
  │ │ (coordination.k8s.io/v1)         ││
  │ │ Owner: worker-0                  ││
  │ │ Waiting to acquire...            ││
  │ └──────────────────────────────────┘│
  │ │
  │ ├─ NO Outbound RPC Calls          │
  │ │  (Read-only state)               │
  │ │                                  │
  │ ├─ Subscription Reception          │
  │ │  (Also receives intent updates)  │
  │ │                                  │
  │ ├─ Telemetry Updates (reads)       │
  │ │  (Keeps cache warm)              │
  │ │                                  │
  │ └─ State Cache Updates             │
  │    (Concurrent with PRIMARY)       │
  │                                     │
  │ ┌──────────────────────────────────┐│
  │ │ RocksDB (Same PVC-backed NVMe)    ││
  │ │ Read-only access (prepared)      ││
  │ └──────────────────────────────────┘│
  └─────────────────────────────────────┘
        Shared PVC Mount Point
```

**Key Design Decisions:**

1. **Both pods mount same PVC** - Enables hot standby without replay overhead
2. **PRIMARY holds K8s Lease** - Simple, K8s-native coordination
3. **STANDBY also receives subscriptions** - Maintains hot state cache
4. **Async RocksDB writes** - PRIMARY writes its state; STANDBY reads immediately

### 4.2 Failover Sequence

**Scenario: PRIMARY pod crashes**

```
T=0s: PRIMARY pod crashes or becomes unreachable
  ├─ Lease heartbeat stops
  └─ K8s detects stale lease (TTL expired after 30s)

T=30s: Lease expires in K8s API
  ├─ STANDBY attempts to acquire lease
  └─ K8s grants lease to STANDBY (atomic operation)

T=31s: STANDBY acquires lease
  ├─ Sets internal flag: am_i_primary = true
  ├─ Begins processing outstanding DeltaCommands
  ├─ Resumes outbound RPC calls
  ├─ Starts lease renewal heartbeat
  └─ Emits "PRIMARY_TAKEOVER" trace event

T=31.1s: Outstanding commands resume
  ├─ All queued DeltaCommands execute normally
  ├─ Device configuration continues seamlessly
  └─ No state hydration needed (was kept warm by STANDBY)

Result: RTO ≈ 30-31 seconds (Kubernetes lease TTL)
```

**Data Consistency Guarantee:**

- **Before failover:** STANDBY has read-only copy of all state (same RocksDB)
- **At failover:** No data loss; all committed commands present in RocksDB
- **After failover:** STANDBY may re-execute some commands (idempotent by design)
  - Retry logic in DeltaCommand execution handles idempotency

### 4.3 Zero-Downtime ISSU (In-Service Software Upgrade)

**Scenario: Upgrade application to new version**

```
Phase 1: Preparation
  ├─ New container image built and pushed to registry
  ├─ Rolling update policy set to "OnDelete" (manual pod replacement)
  └─ Operator initiates upgrade via kubectl patch

Phase 2: Graceful Primary Shutdown
  ├─ K8s sends SIGTERM to PRIMARY pod (worker-0)
  ├─ Application signal handler invoked:
  │   ├─ Stops accepting new subscriptions
  │   ├─ Flushes all volatile state to RocksDB
  │   ├─ Drains outstanding DeltaCommand queue
  │   └─ Relinquishes K8s Lease proactively
  └─ Pod terminates (grace period: 60 seconds)

Phase 3: STANDBY Promotion
  ├─ STANDBY (worker-1) detects lease is now free
  ├─ Immediately acquires K8s Lease
  ├─ Sets am_i_primary = true
  ├─ Resumes outbound RPC calls
  ├─ Emits "PRIMARY_TAKEOVER_PLANNED" event
  └─ OLD PRIMARY pod fully terminated

Phase 4: NEW PRIMARY Boot
  ├─ K8s deploys new pod (worker-0-new with new image)
  ├─ Mounts same PVC (RocksDB state)
  ├─ Application startup reads RocksDB:
  │   ├─ Loads device cache
  │   ├─ Loads outstanding DeltaCommands
  │   └─ Computes state hash to verify consistency
  ├─ Sets am_i_primary = false (starts as STANDBY)
  ├─ Subscribes to all device topics
  ├─ Begins receiving telemetry updates
  └─ Ready for next failover

Phase 5: Optional Switchback (after stability)
  ├─ Operator can trigger switchback to restore balanced load
  ├─ Current PRIMARY flushes state and relinquishes lease
  ├─ New PRIMARY (worker-0-new) acquires lease
  ├─ Old PRIMARY (worker-1) becomes STANDBY
  └─ System restored to baseline

Result: RTO ≈ 0 seconds (STANDBY already had state)
        Graceful shutdown ensures no lost commands
        New version becomes active without downtime
```

**Upgrade Validation Checklist:**

- [ ] Old PRIMARY successfully flushed all state to RocksDB
- [ ] Lease was proactively relinquished (no timeout)
- [ ] STANDBY detected lease change within 5 seconds
- [ ] New PRIMARY boots and reads RocksDB successfully
- [ ] State hash verification passes (no corruption)
- [ ] New PRIMARY subscribes to all topics
- [ ] Telemetry flow resumes without gaps

### 4.4 Split-Brain Prevention

**Limitation:** This architecture does NOT prevent split-brain in case of network partition within Kubernetes cluster.

**Mitigation Strategies:**

1. **Kubernetes Cluster Isolation**
   - Deploy DashFabric control plane in single AZ/cluster
   - Use multi-AZ control plane only for cross-DC scenarios
   - Keep quorum within single zone for tight coupling

2. **Watchdog Timer**
   - If STANDBY cannot communicate with PRIMARY for >60s, assume PRIMARY dead
   - Only then attempt lease acquisition
   - Ensures accidental dual-primary situation is detected quickly

3. **Device-Level Idempotency**
   - All DeltaCommands designed to be idempotent
   - Re-executing same command twice produces same result
   - Devices handle duplicate ENI creation gracefully

4. **External Health Monitoring**
   - SRE team monitors PRIMARY/STANDBY state machine transitions
   - Alert on unexpected dual-PRIMARY conditions
   - Manual intervention if split-brain detected

---

## Part 5: Scalability & Partitioning

### 5.1 Sharding Strategy

**Problem:** Single control plane cannot manage 10,000+ devices due to:
- Memory limits (device actor cache)
- CPU limits (delta compilation + RPC calls)
- Network I/O limits (subscription fanout + southbound calls)

**Solution: Partition devices across StatefulSet shards**

```
Device Population (10,000 hosts)
  │
  ├─ Shard 0 (worker-0/1): hosts 0-1,999
  ├─ Shard 1 (worker-2/3): hosts 2,000-3,999
  ├─ Shard 2 (worker-4/5): hosts 4,000-5,999
  └─ Shard N (worker-2N/2N+1): hosts ...
```

### 5.2 Consistent Hashing

**Algorithm:**

```python
def compute_shard_assignment(device_id: str, shard_count: int) -> int:
  """Map device to shard via consistent hashing."""
  hash_value = crc32(device_id)
  shard_index = hash_value % shard_count
  return shard_index
```

**Properties:**

- **Deterministic:** Same device_id always maps to same shard
- **Distributed:** Hash function distributes devices evenly
- **Stable:** Adding a new shard only re-hashes 1/N devices (minimal disruption)

**Example:**

```
device_id='host-12345' → hash=0xabcd1234 → shard_index = 0xabcd1234 % 4 = 2
                          Map to: worker-4/5 (Shard 2)

device_id='host-67890' → hash=0x5678abcd → shard_index = 0x5678abcd % 4 = 1
                          Map to: worker-2/3 (Shard 1)
```

### 5.3 Dynamic Scale-Out

**Operation: Add 1,000 new hosts to datacenter**

```
Initial State:
  StatefulSet replicas = 4 (2 shards: 0, 1)
  Devices per shard ≈ 5,000 (overloaded)

Operation:
  kubectl scale statefulset dashfabric-workers --replicas=8

Transition:
  ├─ K8s creates worker-4 and worker-5 (new shard pair)
  ├─ API Gateway updates consistent hash ring
  │  From: shard_count = 2 → shard_count = 3
  ├─ New devices (1000) register:
  │  hash(device) % 3 → routed to Shard 0, 1, or 2
  │  Approximately 333 new devices per shard
  ├─ Existing devices (5000):
  │  Still mapped to original shards (unchanged)
  │  No migration needed
  └─ Result: Load better distributed

Final State:
  Shard 0: 5,000 + 333 = ~5,333 devices
  Shard 1: 5,000 + 333 = ~5,333 devices
  Shard 2: 0 + 334 = ~334 devices (new)
```

**Load Observation:**

After scale-out, CPU/memory metrics show:
- Shard 0/1: Still high (existing devices)
- Shard 2: Low (new devices)

**Optional Rebalancing (manual operator task):**

If operators want perfectly balanced shards, perform **gradual device migration**:

```
for each device in shard_0:
  if device_hash % new_shard_count != current_shard:
    Orchestrate graceful device migration:
      ├─ Drain DeltaCommands from old shard
      ├─ Flush state to RocksDB
      ├─ Update device→shard mapping
      └─ Route subscriptions to new shard
```

**Trade-off:** Rebalancing adds operational complexity; often deferred until load becomes critical.

### 5.4 Partition-Level Fault Isolation

**Benefit:** Failure in one shard does NOT affect other shards.

```
Scenario: Shard 1 PRIMARY pod crashes

Impact:
  ├─ Shard 0: Unaffected (independent RocksDB, independent subscriptions)
  ├─ Shard 1: Enters PRIMARY→STANDBY failover (RTO ≈ 30s)
  ├─ Shard 2: Unaffected
  └─ API Gateway: Routes new registrations normally
       └─ Only devices for Shard 1 may experience brief delays

Devices affected:
  ├─ ~5,000 devices in Shard 1: Brief outage during failover
  ├─ ~5,000 devices in Shard 0: No impact
  ├─ ~5,000 devices in Shard 2: No impact
  └─ % of total DC impact: 33% of 15,000 devices
```

**Mitigation:**

- Design datacenters with smaller shards (e.g., 1,000 devices per shard)
- If Shard impacts too many devices, scale to more shards
- Each shard is a scaling unit; add shards proportionally to device count

---

## Part 6: Observability & Diagnostics

### 6.1 OpenTelemetry Integration

**Trace Context Propagation:**

Every operation traces end-to-end from upstream intent to device programming:

```
1. Upstream Control Plane emits configuration change
   ├─ Generates Trace ID (e.g., UUID: 550e8400-e29b-41d4-a716-446655440000)
   ├─ Wraps intent in W3C Trace Context header
   └─ Publishes to /config/hosts/{device_id}

2. DashFabric Upstream Sync Engine receives notification
   ├─ Extracts Trace ID from header
   ├─ Creates OpenTelemetry Span: "sync_intent_received"
   ├─ Sets attributes:
   │   trace_id = "550e8400-e29b-41d4-a716-446655440000"
   │   device_id = "host-12345"
   │   intent_version = 42
   │   config_hash = "abc123def456"
   └─ Routes to State Compilation Engine

3. State Compilation Engine processes intent
   ├─ Creates Span: "compile_deltas"
   ├─ Inherits parent trace_id
   ├─ Sets attributes:
   │   delta_count = 3
   │   compilation_time_ms = 45
   │   dependency_graph_depth = 2
   └─ Produces DeviceGoalState

4. Southbound Driver Layer executes deltas
   ├─ For each DeltaCommand:
   │   ├─ Creates Span: "execute_delta"
   │   ├─ Attributes:
   │   │   delta_id = "cmd-7890"
   │   │   operation = "CREATE_NIC"
   │   │   target_device = "eni-12345"
   │   └─ Calls gRPC/gNMI RPC
   ├─ gRPC automatically propagates trace context (W3C standard)
   └─ Device receives request with Trace ID embedded

5. Data Plane Device receives configuration
   ├─ Extracts Trace ID from RPC header
   ├─ Logs all programming steps with trace_id
   ├─ Completion logged: "eni_created"
   └─ Returns success to control plane

6. Observability Platform (Jaeger/Tempo) reconstructs trace
   ├─ All spans linked by trace_id
   ├─ Timeline visualization:
   │   0ms:  intent_published
   │   2ms:  sync_intent_received
   │   5ms:  compile_deltas (started)
   │   50ms: compile_deltas (completed)
   │   51ms: execute_delta[0] (started)
   │   75ms: gRPC_call (in-flight)
   │   100ms: device_response (received)
   │   102ms: execute_delta[0] (completed)
   │   ...
   │   500ms: all_deltas_executed
   └─ Total latency: 500ms (intent → fully programmed)
```

### 6.2 Metrics Collection

**Key Metrics to Export (via Prometheus):**

```
# Device-level metrics
dashfabric_devices_total{shard_id="0"} = 5,234
dashfabric_devices_ready{shard_id="0"} = 5,100
dashfabric_devices_degraded{shard_id="0"} = 134

# Delta compilation metrics
dashfabric_delta_compilation_duration_ms{device_id="host-12345", p50=10, p99=150}
dashfabric_delta_count{operation="CREATE"} = 45,123
dashfabric_delta_count{operation="DELETE"} = 12,456
dashfabric_delta_count{operation="UPDATE"} = 67,890

# Southbound RPC metrics
dashfabric_southbound_rpc_latency_ms{target_type="DASH", p50=5, p99=50}
dashfabric_southbound_rpc_latency_ms{target_type="SONIC", p50=8, p99=75}
dashfabric_southbound_rpc_latency_ms{target_type="LINUX", p50=2, p99=20}

dashfabric_southbound_rpc_errors_total{target_type="DASH", error="timeout"} = 12
dashfabric_southbound_rpc_errors_total{target_type="DASH", error="connection_refused"} = 3

# State reconciliation metrics
dashfabric_reconciliation_drift_detected_total{shard_id="0"} = 5
dashfabric_reconciliation_duration_ms{device_id="host-12345"} = 45
dashfabric_devices_in_sync{shard_id="0"} = 5,100
dashfabric_devices_out_of_sync{shard_id="0"} = 134

# Lease/HA metrics
dashfabric_lease_acquisitions_total{shard_id="0"} = 1
dashfabric_lease_renewals_total{shard_id="0"} = 1,234
dashfabric_primary_failovers_total{shard_id="0"} = 0
dashfabric_primary_graceful_shutdowns_total{shard_id="0"} = 2

# Memory/resource metrics
dashfabric_rocksdb_cache_size_bytes{shard_id="0"} = 10_737_418_240
dashfabric_subscription_channel_queue_depth{shard_id="0"} = 234
dashfabric_outstanding_commands_queue_depth{shard_id="0"} = 12
```

### 6.3 Structured Logging

**Log Format (JSON with correlation IDs):**

```json
{
  "timestamp": "2026-06-11T14:23:45.123Z",
  "level": "INFO",
  "message": "Delta command executed successfully",
  "trace_id": "550e8400-e29b-41d4-a716-446655440000",
  "span_id": "span-7890-abcd",
  "component": "southbound_driver",
  "device_id": "host-12345",
  "shard_id": 0,
  "delta_id": "cmd-7890",
  "operation": "CREATE_NIC",
  "target_device": "eni-12345",
  "duration_ms": 45,
  "rpc_method": "DASH_gNMI.Set",
  "rpc_status": "OK",
  "correlation_id": "reg-host-12345-config-42"
}
```

### 6.4 eBPF Diagnostic Probes

**Sidecar Container:** Deployed alongside each StatefulSet worker pod

**Purpose:** Capture low-level network and system metrics without instrumentation overhead

```yaml
# Example eBPF probe configuration
ebpf_probes:
  - name: tcp_latency
    hook: tcp_sendmsg
    event:
      - src_ip
      - dst_ip
      - src_port
      - dst_port
      - latency_us
    output: /metrics/tcp_latency.log
    
  - name: socket_errors
    hook: tcp_set_state
    event:
      - src_ip
      - dst_ip
      - error_code
      - timestamp
    output: /metrics/socket_errors.log
```

**Insights Captured:**

- **Southbound RPC latency** (actual TCP transit time to devices)
- **Connection anomalies** (resets, timeouts, refused connections)
- **System memory pressure** (if page faults spike, GC overhead detected)
- **CPU scheduling latency** (if off-CPU time increases, container contention detected)

---

## Part 7: Data Models & Serialization

### 7.1 Object Serialization

**RocksDB Storage Format:**

```
Key:   shard_id:device_id:object_type:object_id
Value: protobuf-encoded object

Examples:
  Key: "0:host-12345:HOST_DEVICE:host-12345"
  Value: <HostDeviceObject protobuf>
  
  Key: "0:host-12345:CONTAINER:container-guid-abc"
  Value: <ContainerObject protobuf>
  
  Key: "0:host-12345:NIC:nic-eth0"
  Value: <NICObject protobuf>
```

**Protobuf Definitions (Abbreviated):**

```protobuf
message HostDeviceObject {
  string id = 1;
  string device_id = 2;
  enum State {
    INITIALIZING = 0;
    READY = 1;
    DRAINING = 2;
    TERMINATED = 3;
  }
  State state = 3;
  
  Subscriptions subscriptions = 4;
  HostLifecycle lifecycle = 5;
  map<string, ContainerObject> containers = 6;
  HostState host_state = 7;
  AuditState audit_state = 8;
}

message ContainerObject {
  string id = 1;
  string container_guid = 2;
  string host_device_id = 3;
  enum State {
    INITIALIZING = 0;
    READY = 1;
    RECONFIGURING = 2;
    DESTROYING = 3;
    TERMINATED = 4;
  }
  State state = 4;
  
  Subscriptions subscriptions = 5;
  NetworkingConfig networking_config = 6;
  map<string, NICObject> nics = 7;
  ContainerState container_state = 8;
  Lifecycle lifecycle = 9;
}

message NICObject {
  string id = 1;
  string nic_id = 2;
  string container_id = 3;
  string host_device_id = 4;
  enum State {
    INITIALIZING = 0;
    READY = 1;
    POLICY_UPDATE = 2;
    DESTROYING = 3;
    TERMINATED = 4;
  }
  State state = 5;
  
  Subscriptions subscriptions = 6;
  ENIConfig eni_config = 7;
  DataPlaneConfig dataplane_config = 8;
  ENIState eni_state = 9;
  HALReference hal_reference = 10;
}

message DeltaCommand {
  string command_id = 1;
  string trace_id = 2;
  uint32 sequence_number = 3;
  
  enum Operation {
    CREATE = 0;
    UPDATE = 1;
    DELETE = 2;
  }
  Operation operation = 4;
  
  string target_object_type = 5;
  bytes target_config = 6;  // Device-specific protobuf
  
  enum Status {
    PENDING = 0;
    SUCCESS = 1;
    FAILED = 2;
    RETRYING = 3;
  }
  Status status = 7;
  
  google.protobuf.Timestamp created_at = 8;
  google.protobuf.Timestamp executed_at = 9;
  uint32 retry_count = 10;
  string error_message = 11;
}
```

### 7.2 PubSub Message Format

**Configuration Topic Message (from upstream control plane):**

```yaml
topic: "/config/hosts/host-12345"
message:
  metadata:
    timestamp: "2026-06-11T14:23:45Z"
    version: 42
    trace_id: "550e8400-e29b-41d4-a716-446655440000"
  
  intent:
    host_id: "host-12345"
    containers:
      - container_guid: "container-abc-def"
        vpc_id: "vpc-123"
        vnet_id: "vnet-456"
        overlay_ip: "10.1.2.3"
        nics:
          - nic_id: "eth0"
            eni_id: "eni-123"
            primary_ipv4: "10.1.2.3"
            vpc_id: "vpc-123"
            security_group_ids: ["sg-001", "sg-002"]
            routing_table_id: "rtb-123"
            acl_group_ids: ["acl-001"]
            metering_policy_id: "mp-001"
```

---

## Part 8: Deployment & Operations

### 8.1 Kubernetes Manifest Structure

```yaml
# namespace.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: dashfabric

---
# configmap.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: dashfabric-config
  namespace: dashfabric
data:
  config.yaml: |
    server:
      grpc_port: 5051
      metrics_port: 8081
    
    replication:
      lease_ttl_seconds: 30
      lease_renew_interval_seconds: 10
    
    reconciliation:
      interval_seconds: 60
      max_drift_tolerance_percent: 5
    
    sharding:
      initial_shard_count: 2
      devices_per_shard_target: 5000

---
# service.yaml
apiVersion: v1
kind: Service
metadata:
  name: dashfabric-api
  namespace: dashfabric
spec:
  type: LoadBalancer
  selector:
    app: dashfabric-api
  ports:
    - name: grpc
      port: 5051
      targetPort: 5051

---
# statefulset.yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: dashfabric-workers
  namespace: dashfabric
spec:
  serviceName: dashfabric-workers
  replicas: 4  # 2 shards × 2 (primary+standby)
  
  selector:
    matchLabels:
      app: dashfabric-worker
  
  template:
    metadata:
      labels:
        app: dashfabric-worker
    
    spec:
      containers:
      - name: dashfabric
        image: dashfabric:latest
        imagePullPolicy: IfNotPresent
        
        ports:
        - name: grpc
          containerPort: 5051
        - name: metrics
          containerPort: 8081
        
        volumeMounts:
        - name: state-storage
          mountPath: /var/lib/dashfabric/state
        
        resources:
          requests:
            memory: "4Gi"
            cpu: "2"
          limits:
            memory: "8Gi"
            cpu: "4"
        
        livenessProbe:
          grpc:
            port: 5051
          initialDelaySeconds: 10
          periodSeconds: 10
        
        readinessProbe:
          grpc:
            port: 5051
          initialDelaySeconds: 5
          periodSeconds: 5
      
      - name: ebpf-monitor
        image: dashfabric-ebpf:latest
        securityContext:
          privileged: true
        
        volumeMounts:
        - name: metrics
          mountPath: /metrics
  
  volumeClaimTemplates:
  - metadata:
      name: state-storage
    spec:
      accessModes: ["ReadWriteOnce"]
      resources:
        requests:
          storage: 100Gi

---
# hpa.yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: dashfabric-workers-hpa
  namespace: dashfabric
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: StatefulSet
    name: dashfabric-workers
  
  minReplicas: 4   # 2 shards
  maxReplicas: 100 # 50 shards max
  
  metrics:
  - type: Resource
    resource:
      name: cpu
      target:
        type: Utilization
        averageUtilization: 70
  
  - type: Resource
    resource:
      name: memory
      target:
        type: Utilization
        averageUtilization: 75
  
  behavior:
    scaleDown:
      stabilizationWindowSeconds: 300
      policies:
      - type: Percent
        value: 50
        periodSeconds: 60
    
    scaleUp:
      stabilizationWindowSeconds: 60
      policies:
      - type: Percent
        value: 100
        periodSeconds: 60
```

### 8.2 Operational Runbooks

#### 8.2.1 Adding a New Host to Datacenter

```bash
# 1. Prepare host (install DashFabric agent)
ssh host-new-001
sudo apt-get install dashfabric-agent

# 2. Configure agent
cat > /etc/dashfabric/agent.conf <<EOF
control_plane_ip=10.0.0.10
control_plane_port=5051
device_id=host-new-001
device_type=HOST_DPU_ATTACHED
capabilities_json=/etc/dashfabric/capabilities.json
EOF

# 3. Start agent
sudo systemctl start dashfabric-agent

# 4. Monitor registration in control plane
kubectl -n dashfabric logs -f deployment/dashfabric-api | grep "host-new-001"

# Expected output:
# [INFO] Device registration received: host-new-001
# [INFO] Assigned to shard 0 (worker-0/1)
# [INFO] Subscribed to /config/hosts/host-new-001

# 5. Verify in dashboards
kubectl -n dashfabric port-forward svc/dashfabric-metrics 9090:9090
# Open browser: localhost:9090
# Query: dashfabric_devices_total{shard_id="0"}
# Should show: 5,235 (incremented by 1)
```

#### 8.2.2 Scaling Out Control Plane

```bash
# Current capacity is strained (>80% CPU on all shards)

# 1. Check current replica count
kubectl -n dashfabric get statefulset dashfabric-workers
# NAME                    READY   AGE
# dashfabric-workers      4/4     365d

# 2. Scale to 6 replicas (add 1 shard pair)
kubectl -n dashfabric scale statefulset dashfabric-workers --replicas=6

# 3. Monitor new pods
kubectl -n dashfabric get pods -w | grep dashfabric-workers

# 4. Verify new shard is ready
kubectl -n dashfabric logs deployment/dashfabric-api | grep "shard.*initialized"

# 5. Verify load distribution
# Query Prometheus:
# dashfabric_devices_total{shard_id=~"0|1|2"}
# Should show devices distributed across 3 shards

# 6. Optional: Trigger rebalancing if desired (operator-initiated)
# This is optional; system functions fine without rebalancing
```

#### 8.2.3 Performing ISSU (Software Upgrade)

```bash
# 1. Build and push new application image
docker build -t dashfabric:v2.1.0 .
docker push registry.example.com/dashfabric:v2.1.0

# 2. Verify new image is available
docker pull registry.example.com/dashfabric:v2.1.0

# 3. Update StatefulSet to trigger rolling upgrade
kubectl -n dashfabric set image statefulset/dashfabric-workers \
  dashfabric=registry.example.com/dashfabric:v2.1.0 \
  --record

# 4. Monitor upgrade progress
kubectl -n dashfabric rollout status statefulset/dashfabric-workers

# 5. Watch logs to verify graceful shutdown
kubectl -n dashfabric logs -f pod/dashfabric-workers-0 | grep -E "SIGTERM|graceful|flush"

# Expected output:
# [INFO] Received SIGTERM, initiating graceful shutdown
# [INFO] Flushing device state to RocksDB...
# [INFO] Waiting for outstanding commands...
# [INFO] Relinquishing K8s Lease...
# [INFO] Shutdown complete

# 6. Monitor RTO during failover
# Open monitoring dashboard
# Watch: dashfabric_primary_failovers_total
# Watch: dashfabric_devices_ready (should stay constant)

# 7. Verify upgrade success
kubectl -n dashfabric describe pod dashfabric-workers-0 | grep Image
# Should show: registry.example.com/dashfabric:v2.1.0
```

---

## Part 9: Example Workflows

### 9.1 Device Registration & Initialization Workflow

```
Timeline:

T=0ms: Device registration request
  └─ Device (host-12345) sends: RegisterDeviceRequest(device_id, device_profile)
     └─ API Gateway receives request
     
T=2ms: Device validation
  └─ API Gateway validates against hardware compatibility matrix
     └─ Check: max_acl_rules >= 10,000? YES ✓
     └─ Check: max_nics >= 10? YES ✓
     └─ Validation: PASS ✓
     
T=4ms: Shard assignment
  └─ API Gateway computes: hash("host-12345") % 4 = 2
     └─ Assign to Shard 1 (worker-2/3)
     
T=6ms: Route to PRIMARY
  └─ API Gateway calls: worker-2.RegisterDevice(device_profile)
     └─ RPC IN-FLIGHT
     
T=11ms: PRIMARY receives registration
  └─ worker-2 receives RegisterDeviceRequest
     ├─ Creates HostDeviceObject in state INITIALIZING
     ├─ Stores to RocksDB: "1:host-12345:HOST_DEVICE:host-12345"
     └─ Emits Span: "host_device_object_created"
     
T=12ms: STANDBY mirrors registration (concurrent)
  └─ worker-3 also received registration (both share subscription)
     ├─ Creates same HostDeviceObject in local cache
     └─ Ready to take over if PRIMARY fails
     
T=13ms: Trigger subscription
  └─ worker-2 triggers Upstream Sync Engine to subscribe
     ├─ Subscribe to: /config/hosts/host-12345
     └─ Ready to receive config
     
T=15ms: Return to device
  └─ API Gateway returns: RegisterDeviceResponse(
       shard_id=1,
       subscription_topics=["/config/hosts/host-12345"],
       status=OK
     )
  
T=20ms: Device receives response
  └─ Device registered successfully
     └─ Waits for configuration from PubSub
     
T=25ms: Control plane publishes initial config
  └─ Upstream SDN controller sends intent:
     ├─ Topic: /config/hosts/host-12345
     ├─ Message: container[0] spec with NIC[0] and NIC[1]
     └─ Trace ID: 550e8400-e29b-41d4-a716-446655440000
     
T=27ms: Config received by Upstream Sync Engine
  └─ worker-2 Upstream Sync receives notification
     ├─ Extracts trace_id
     ├─ Routes to State Compilation Engine
     └─ Emits Span: "intent_received"
     
T=30ms: State compilation begins
  └─ State Compilation Engine invoked:
     ├─ Load cached state: empty (first time)
     ├─ New intent: create CONTAINER-abc-def with 2 NICs
     ├─ Compute deltas:
     │   ├─ CREATE CONTAINER "container-abc-def"
     │   ├─ CREATE NIC "eth0" (eni-123)
     │   └─ CREATE NIC "eth1" (eni-124)
     ├─ Resolve dependencies:
     │   ├─ NIC creation depends on CONTAINER ready
     │   └─ CONTAINER creation depends on HOST ready
     └─ Emit Span: "deltas_compiled" with delta_count=3
     
T=35ms: Compile device-specific configs
  └─ For each delta, invoke compiler based on device_type:
     ├─ device_type = HOST_DPU_ATTACHED
     └─ Compile to gNMI/SAI format
     
T=40ms: Save to RocksDB
  └─ Checkpoint compiled deltas:
     ├─ Key: "1:host-12345:CONTAINER:container-abc-def"
     ├─ Value: <ContainerObject protobuf>
     ├─ Key: "1:host-12345:NIC:eth0"
     └─ Value: <NICObject protobuf>
     
T=42ms: Execute deltas
  └─ Southbound Driver Layer processes delta queue:
     ├─ DELTA[0]: CREATE CONTAINER
     │   ├─ Call device: CreateContainer(container-abc-def)
     │   ├─ Device setup: create namespace
     │   └─ RPC latency: 8ms
     ├─ DELTA[1]: CREATE NIC (eni-123)
     │   ├─ Call device: CreateENI(eni-123, vpc-123, primary_ip=10.1.2.3)
     │   ├─ gNMI Set RPC to device
     │   └─ RPC latency: 15ms
     └─ DELTA[2]: CREATE NIC (eni-124)
         ├─ Call device: CreateENI(eni-124, vpc-456, primary_ip=10.1.2.4)
         └─ RPC latency: 12ms
     
T=77ms: All deltas executed
  └─ Update object states:
     ├─ HostDeviceObject: state = READY
     ├─ ContainerObject: state = READY
     ├─ NICObject[0]: state = READY
     └─ NICObject[1]: state = READY
     
T=78ms: Update RocksDB
  └─ Persist state changes
     
T=80ms: Emit completion span
  └─ Emits Span: "device_initialization_complete"
     ├─ duration_ms: 80
     ├─ delta_count: 3
     ├─ all_deltas_successful: true
     └─ End span, close trace
     
T=85ms: Observability platform receives spans
  └─ Jaeger/Tempo ingests complete trace
     └─ Visualization available: 80ms end-to-end latency

Result: Device fully initialized, all NICs configured, ready for traffic
```

### 9.2 Configuration Update Workflow (Policy Change)

```
Scenario: Add a new ACL rule to NIC eth0 on host-12345

Timeline:

T=0ms: Upstream policy change triggered
  └─ SDN controller determines: ACL rule needs to change
     ├─ New rule: allow TCP port 8443 from 0.0.0.0/0
     └─ Publish to /config/hosts/host-12345/containers/container-abc-def/nics/eth0
     
T=5ms: Config received by Upstream Sync Engine
  └─ worker-2 receives updated NIC config
     ├─ Extracts new ACL rules
     └─ Routes to State Compilation Engine
     
T=10ms: Delta computation
  └─ Compilation Engine compares:
     ├─ Cached state: NICObject with 5 ACL rules
     ├─ New intent: NICObject with 6 ACL rules
     └─ Delta: UPDATE NIC "eth0" with new ACL rule
     
T=15ms: Compile device config
  └─ Compiler generates:
     ├─ gNMI message: Set eni-123 with new ACL group
     ├─ ACL rules encoded in protobuf
     └─ Message serialized
     
T=20ms: Execute delta
  └─ Southbound Driver calls:
     ├─ gNMI.Set(path=/config/dash/enis/eni-123/acl_groups, value=acl-new)
     ├─ Device processes: add ACL rule
     └─ Device responds: OK
     
T=35ms: Update cache
  └─ NICObject state updated:
     ├─ acl_group_ids = [acl-001, acl-002, acl-new]
     └─ Saved to RocksDB
     
T=37ms: Completion
  └─ Trace event emitted: "nic_policy_update_complete"
     └─ Total latency: 37ms
     
Result: New ACL rule active on device; traffic matching rule now flows correctly
```

---

## Part 10: Future Enhancements & Evolution

### 10.1 Multi-Cluster Federation

**Future:** Support for multiple datacenters with federated control planes

**Design Considerations:**
- Global device registry (cross-DC unique device IDs)
- Inter-cluster synchronization of state for DR scenarios
- Cross-DC failover if entire DC control plane fails
- Global consistent hashing with DC-aware shard assignment

### 10.2 Advanced HA Patterns

**Future:** Explore active-active replication (instead of active-passive)

**Challenges:**
- Consensus algorithm (Raft) needed for global state consistency
- Increased operational complexity
- Potential performance trade-offs vs. current zero-RTO design

### 10.3 Intelligent Load Balancing

**Future:** Automatic device migration based on real-time metrics

**Benefits:**
- Automatic rebalancing without operator intervention
- Predictive scaling based on trend analysis
- Graceful degradation if resources exceed thresholds

### 10.4 Enhanced Security

**Future Additions:**
- mTLS for all inter-pod communication
- Role-based access control (RBAC) for API Gateway
- Audit logging for all administrative changes
- Encrypted state storage in RocksDB

---

## Conclusion

DashFabric provides a **production-grade, Kubernetes-native distributed network control plane** that:

✅ Manages thousands of DPU-enabled hosts with hierarchical object lifecycle  
✅ Provides zero-downtime failover via hot standby replication  
✅ Scales horizontally via partition-based sharding  
✅ Supports multiple southbound protocols (DASH, SONiC, Linux)  
✅ Delivers comprehensive observability via OpenTelemetry  
✅ Enables zero-downtime software upgrades (ISSU)  
✅ Detects and reconciles configuration drift via periodic audits  

The architecture balances **simplicity** (Kubernetes-native primitives), **reliability** (replication + reconciliation), and **scale** (sharding + consistent hashing) to meet the demands of modern cloud-native infrastructure.

---

**Document Version:** 1.0  
**Last Updated:** June 11, 2026  
**Maintainers:** Architecture Team  
