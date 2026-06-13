# FleetManager: Low-Level Design (LLD) - COMPREHENSIVE

**Version:** 1.0  
**Language:** C++17/20  
**Status:** Implementation Ready  
**Scope:** Ultra-detailed component design, algorithms, class hierarchy, threading, memory management

---

## 1. Class Hierarchy & Object Model

### 1.1 Core Entity Classes

```cpp
// ============================================================================
// DATA MODELS
// ============================================================================

namespace fleetmanager {
namespace models {

// ----------------------------------------------------------------------------
// DASH object catalog — the 15 published kinds, organized by scope.
// Each kind is defined in protos/published/<kind>.proto and arrives wrapped
// in a `ConfigEntry { metadata, payload: bytes }` envelope (vm-eni #18).
// `revision` is per-object monotonic (per Learning-DashNet myth #21):
// it orders writes to the SAME object, not across objects, not across DPUs.
// ----------------------------------------------------------------------------

// Fleet scope — one canonical catalog, reconciled per device but never overridden.
using RoutingType         = proto::RoutingType;          // fleet-wide singleton

// Device / Appliance scope — per-DPU records cached at HostDeviceActor.
using Appliance           = proto::Appliance;
using HostSpec            = proto::HostSpec;             // host the DPU is plugged into
using Tunnel              = proto::Tunnel;
using Qos                 = proto::Qos;
using PrefixTag           = proto::PrefixTag;
using HaSet               = proto::HaSet;                // appliance-pair primary/standby

// VNET scope — materialized per device-that-has-an-ENI.
using Vnet                = proto::Vnet;
using VnetMappingManifest = proto::VnetMappingManifest;  // small header (chunk ids + SHA-256)
using VnetMappingChunk    = proto::VnetMappingChunk;     // <1 MiB shard of CA→PA entries
using PaValidation        = proto::PaValidation;

// Group scope — reusable rule bundles, referenced by ENIs.
using RouteGroup          = proto::RouteGroup;
using AclGroup            = proto::AclGroup;
using MeterPolicy         = proto::MeterPolicy;
using OutboundPortMap     = proto::OutboundPortMap;

// ENI scope — per-NIC published spec + the in-process composed program.
using NicSpec             = proto::NicSpec;              // published reference bundle
using Eni                 = proto::Eni;                  // DASH ENI object (synthesized)
using ContainerSpec       = proto::ContainerSpec;        // VM/container parent record
using HaScope             = proto::HaScope;              // per-ENI HA scope ref

// Top-level message envelope for every etcd value (vm-eni #18).
using ConfigEntry         = proto::ConfigEntry;
using ConfigMetadata      = proto::ConfigMetadata;
using DeltaCommand        = proto::DeltaCommand;

// Discriminator for the published object kinds — the southbound driver
// dispatches on this enum, not on a fixed ENI verb (see §4).
enum class DashObjectKind : uint16_t {
    // Fleet
    ROUTING_TYPE              = 1,
    // Device
    APPLIANCE                 = 10,
    HOST_SPEC                 = 11,
    TUNNEL                    = 12,
    QOS                       = 13,
    PREFIX_TAG                = 14,
    HA_SET                    = 15,
    // VNET
    VNET                      = 20,
    VNET_MAPPING_MANIFEST     = 21,
    VNET_MAPPING_CHUNK        = 22,
    PA_VALIDATION             = 23,
    // Group
    ROUTE_GROUP               = 30,
    ACL_GROUP                 = 31,
    METER_POLICY              = 32,
    OUTBOUND_PORT_MAP         = 33,
    // ENI / lifecycle
    NIC_SPEC                  = 40,
    CONTAINER_SPEC            = 41,
    HA_SCOPE                  = 42,
};

// Discriminated union — every object handled by the registry, persistence,
// and southbound layers travels through this variant. Avoids 15 parallel maps.
struct DashObject {
    DashObjectKind kind;
    std::string    object_id;     // 1–64 ASCII handle
    std::string    tenant_id;     // from ConfigMetadata; "" for fleet-scope
    uint64_t       revision;      // per-object monotonic
    std::array<uint8_t,32> payload_digest;  // SHA-256 from ConfigMetadata

    // Exactly one of these is populated based on `kind`. We keep them in a
    // std::variant for type safety; std::any/proto::Any is used at the wire.
    std::variant<
        std::monostate,
        RoutingType,
        Appliance, HostSpec, Tunnel, Qos, PrefixTag, HaSet,
        Vnet, VnetMappingManifest, VnetMappingChunk, PaValidation,
        RouteGroup, AclGroup, MeterPolicy, OutboundPortMap,
        NicSpec, ContainerSpec, HaScope
    > body;
};

enum class DeviceType {
    HOST_LINUX,
    HOST_DPU_ATTACHED,
    APPLIANCE_DASH_STANDALONE,
    APPLIANCE_DASH_CHASSIS
};

// Lifecycle state of any actor (HDO / CO / NO). Per vm-eni #6 + #14, the NO
// actor cannot leave WAITING_BOOTSTRAP until its parent HDO has hydrated the
// /global + /group cache, and cannot leave WAITING_REFS until every published
// ref (vnet, route_group, acl_group, meter_policy, ha_scope) is present.
enum class ObjectState {
    WAITING_BOOTSTRAP    = 0,   // HDO not yet hydrated /global + /group
    WAITING_REFS         = 1,   // NO has NicSpec but referenced objects missing
    COMPOSING            = 2,   // NO is building/updating NicGoalState
    READY                = 3,   // composed, hash matches device-reported hash
    INCOMPLETE_MAPPING   = 4,   // VnetMapping manifest present, chunks missing
    OVER_CAPACITY        = 5,   // device reported capacity exceeded for this ENI
    RECONFIGURING        = 6,   // recomposing on input change
    DRAINING             = 7,   // tearing down in reverse wave order
    TERMINATED           = 8,
    VALIDATION_REJECTED  = 9,   // proto-decode / ref-integrity failed; quarantined
};

enum class OperationType {
    CREATE = 0,
    UPDATE = 1,
    DELETE = 2
};

enum class DeltaStatus {
    PENDING = 0,
    SUCCESS = 1,
    FAILED = 2,
    RETRYING = 3
};

enum class TargetType {
    DASH = 0,
    SONIC = 1,
    LINUX = 2
};

// Per-device hot cache held by the HostDeviceActor (HDO). Group/global caches
// are shared with subordinate ContainerActors (CO) and NicActors (NO) via
// in-process actor messages — no extra etcd watches per NIC (vm-eni #7, #16).
struct HostDeviceState {
    std::string device_id;
    DeviceType  type;
    ObjectState state = ObjectState::WAITING_BOOTSTRAP;

    // /global subtree
    std::unordered_map<std::string, RoutingType>     routing_types;
    std::optional<Appliance>                          appliance;
    std::optional<HostSpec>                           host_spec;
    std::unordered_map<std::string, Tunnel>           tunnels;
    std::unordered_map<std::string, Qos>              qos_policies;
    std::unordered_map<std::string, PrefixTag>        prefix_tags;
    std::unordered_map<std::string, HaSet>            ha_sets;

    // /group subtree (shared rule bundles)
    std::unordered_map<std::string, RouteGroup>       route_groups;
    std::unordered_map<std::string, AclGroup>         acl_groups;
    std::unordered_map<std::string, MeterPolicy>      meter_policies;
    std::unordered_map<std::string, OutboundPortMap>  outbound_port_maps;

    // /vnet subtree — cached only for vnets with at least one local ENI.
    // VnetMapping arrives as manifest + chunks (vm-eni #8, myth #3).
    std::unordered_map<std::string, Vnet>                  vnets;
    std::unordered_map<std::string, VnetMappingManifest>   vnet_manifests;
    std::unordered_map<std::string, std::unordered_map<std::string, VnetMappingChunk>>
                                                            vnet_chunks;     // [vnet_id][chunk_id]
    std::unordered_map<std::string, PaValidation>          pa_validations;
    std::unordered_map<std::string, uint32_t>              vnet_refcount;   // last NO detach unsubscribes

    // Children
    std::unordered_set<std::string> container_ids;

    // Per-key revision tracking — the value compared against incoming
    // ConfigEntry.metadata.revision before applying. Strictly per-object.
    std::unordered_map<std::string, uint64_t> last_applied_revision;

    // etcd watch resume cursor (vm-eni #11).
    int64_t last_known_mod_revision = 0;

    std::chrono::system_clock::time_point last_update;
    std::unordered_set<std::string> active_topics;
};

// Compiled delta for execution. One delta corresponds to ONE DASH object
// CREATE/UPDATE/DELETE call on the southbound. Deltas are emitted by the
// NicActor's diff between the previous and newly composed NicGoalState; the
// dispatcher actuates them only on the K8s-Lease primary pod (vm-eni #12).
struct CompiledDelta {
    std::string   command_id;          // UUID, idempotency token on the wire
    std::string   trace_id;
    uint32_t      sequence_number;
    uint32_t      wave;                // 0..6 — see §4 programming order
    OperationType operation;

    DashObjectKind target_kind;
    std::string    target_object_id;
    std::string    eni_id;             // owning NicActor (empty for HDO-scope objects)

    // SHA-256 of canonical_serialization(target object body) — the
    // idempotency key the agent uses to skip work (vm-eni #26, #27).
    std::array<uint8_t,32> content_hash;

    // Device-specific binary config (protobuf serialized)
    std::vector<uint8_t> target_config_bytes;

    // Execution state
    DeltaStatus status = DeltaStatus::PENDING;
    int retry_count = 0;
    std::string error_message;
    std::chrono::system_clock::time_point created_at;
    std::chrono::system_clock::time_point executed_at;
};

// Plan emitted by NicActor::Compose_() — a wave-ordered list of deltas plus
// the new NicGoalState content_hash. Standby pods cache the plan for instant
// failover; only the Primary's dispatcher actuates.
struct DeltaPlan {
    std::string                eni_id;
    std::array<uint8_t,32>     nic_goal_state_hash;     // SHA-256 of the composed state
    std::vector<CompiledDelta> commands;                // wave-sorted
    std::chrono::system_clock::time_point composed_at;
};

}  // namespace models
}  // namespace fleetmanager
```

### 1.2 Core Component Classes

```cpp
// ============================================================================
// REGISTRY & DEVICE MANAGEMENT
// ============================================================================

namespace fleetmanager {
namespace registry {

class DeviceRegistry {
public:
    // Register new device
    Result<std::string> RegisterDevice(const DeviceProfile& profile);
    
    // Lookup device
    std::optional<DeviceProfile> GetDevice(const std::string& device_id) const;
    
    // Update device state
    void UpdateDeviceState(const std::string& device_id, ObjectState state);
    
    // Get all devices for shard
    std::vector<DeviceProfile> GetDevicesForShard(uint32_t shard_id) const;
    
    // Iterator for scanning all devices
    class Iterator {
    public:
        bool Next(DeviceProfile& out);
        size_t Count() const;
    };
    
    Iterator Iterate() const;
    
    // Metrics
    size_t TotalDevices() const;
    size_t DevicesInState(ObjectState state) const;

private:
    mutable std::shared_mutex registry_lock_;
    std::unordered_map<std::string, DeviceProfile> devices_;
    std::unordered_map<uint32_t, std::vector<std::string>> shard_assignments_;
};

}  // namespace registry
}  // namespace fleetmanager


// ============================================================================
// ACTOR FRAMEWORK
// ============================================================================

namespace fleetmanager {
namespace actor {

// Lock-free mailbox for actor messages
template<typename MessageT>
class Mailbox {
public:
    explicit Mailbox(size_t capacity);
    
    // Try to enqueue message (non-blocking, returns false if full)
    bool TryEnqueue(MessageT&& msg);
    
    // Try to dequeue message (non-blocking)
    bool TryDequeue(MessageT& out);
    
    // Blocking enqueue with timeout
    Result<void> Enqueue(MessageT&& msg, std::chrono::milliseconds timeout);
    
    size_t Size() const;
    size_t Capacity() const;

private:
    // Lock-free MPMC queue (Boost.Lockfree or custom ring buffer)
    std::vector<std::optional<MessageT>> queue_;
    std::atomic<uint64_t> head_{0};
    std::atomic<uint64_t> tail_{0};
};

// Message types that actors can receive. Three actor classes share the same
// envelope (HDO/CO/NO); the payload variants drive behavior.
struct ActorMessage {
    enum Type {
        // /hosts/<device>/spec or /global/** /group/** updates
        CONFIG_UPDATE_GLOBAL_OR_GROUP = 1,
        // /vnet/<id>/** updates (manifest, chunks, vnet spec, pa_validation)
        CONFIG_UPDATE_VNET            = 2,
        // /hosts/<device>/<container>/<nic>/spec updates
        CONFIG_UPDATE_NIC_SPEC        = 3,
        // HDO → NO fan-out: a referenced group/global object changed
        REF_INVALIDATION              = 4,
        DEVICE_HEARTBEAT              = 5,
        RECONCILIATION_TICK           = 6,
        VALIDATION_REJECTED_EVENT     = 7,
        SHUTDOWN                      = 8,
    } type;

    std::string trace_id;

    // Discriminator for which DASH object kind this message refers to.
    // Empty for HEARTBEAT / RECONCILIATION_TICK / SHUTDOWN.
    std::optional<models::DashObjectKind> kind;
    std::string object_id;

    // Wire payload (proto-binary of the kind-specific message). The receiving
    // actor decodes by `kind` and validates against ConfigMetadata.
    std::vector<uint8_t> payload;
    uint64_t             revision = 0;
};

// ----------------------------------------------------------------------------
// Three-tier actor split (per Specs/02 + vm-eni §5):
//   HostDeviceActor (HDO)  — one per device. Owns /global + /group + /vnet
//                            caches; programs Wave 0–2; fan-out to children.
//   ContainerActor  (CO)   — one per VM/container. Thin lifecycle parent;
//                            spawns/reaps NOs and propagates teardown.
//   NicActor        (NO)   — one per NIC. Composes NicGoalState in-process,
//                            diffs against previous, emits DeltaPlan.
// ----------------------------------------------------------------------------

// Per-device actor — caches /global, /group, /vnet subtrees for the device,
// performs Wave 0–2 programming, owns vnet refcounts, and dispatches
// REF_INVALIDATION to subscribed NicActors when any cached object changes.
class HostDeviceActor {
public:
    HostDeviceActor(
        const std::string& device_id,
        const models::DeviceProfile& profile,
        std::shared_ptr<persistence::StateStore> state_store,
        std::shared_ptr<compilation::StateCompilationEngine> compiler,
        std::shared_ptr<southbound::SouthboundExecutor> executor,
        std::shared_ptr<observability::ObservabilityContext> otel_context
    );

    // Non-blocking message enqueue (lock-free MPSC mailbox)
    Result<void> SendMessage(ActorMessage msg, std::chrono::milliseconds timeout);

    // Actor main loop (runs in ActorScheduler thread pool)
    void Run();
    void Shutdown();

    // Read-only views for child actors (CO/NO) — protected by per-key
    // shared_mutex inside HostDeviceState; group/global lookups are cheap.
    std::optional<models::Vnet>         GetVnet(const std::string& vnet_id) const;
    std::optional<models::RouteGroup>   GetRouteGroup(const std::string& group_id) const;
    std::optional<models::AclGroup>     GetAclGroup(const std::string& group_id) const;
    std::optional<models::MeterPolicy>  GetMeterPolicy(const std::string& policy_id) const;
    std::optional<models::Tunnel>       GetTunnel(const std::string& tunnel_id) const;
    std::optional<models::HaSet>        GetHaSet(const std::string& ha_set_id) const;
    std::optional<models::RoutingType>  GetRoutingType(const std::string& name) const;

    // VnetMapping watch refcounting (vm-eni #7). First NO attaching to a vnet
    // triggers etcd subscribe to /vnet/<id>/**; last detach unsubscribes.
    void AttachNicToVnet(const std::string& vnet_id, const std::string& nic_id);
    void DetachNicFromVnet(const std::string& vnet_id, const std::string& nic_id);

    // Query
    models::ObjectState GetState() const;
    size_t GetMailboxDepth() const;

private:
    std::string device_id_;
    models::DeviceProfile profile_;

    std::shared_ptr<persistence::StateStore> state_store_;
    std::shared_ptr<compilation::StateCompilationEngine> compiler_;
    std::shared_ptr<southbound::SouthboundExecutor> executor_;
    std::shared_ptr<observability::ObservabilityContext> otel_context_;

    // State machine (see ObjectState). HDO starts in WAITING_BOOTSTRAP and
    // can only transition to READY once /global+/group hydration drains.
    std::atomic<models::ObjectState> current_state_{models::ObjectState::WAITING_BOOTSTRAP};

    Mailbox<ActorMessage> mailbox_{4096};
    models::HostDeviceState state_;
    mutable std::shared_mutex state_lock_;

    // Subscribed children for fan-out
    std::unordered_map<std::string, std::weak_ptr<class NicActor>> nic_actors_;
    std::unordered_map<std::string, std::weak_ptr<class ContainerActor>> container_actors_;

    std::atomic<uint64_t> processed_messages_{0};
    std::chrono::system_clock::time_point last_reconciliation_;

    // Private handlers
    void ProcessMessage_(ActorMessage msg);
    Result<void> HandleGlobalOrGroupUpdate_(const ActorMessage& msg);
    Result<void> HandleVnetUpdate_(const ActorMessage& msg);
    Result<void> HandleHeartbeat_(const std::string& trace_id);
    Result<void> TriggerReconciliation_(const std::string& trace_id);
    void FanOutInvalidation_(models::DashObjectKind kind, const std::string& object_id);
};

// Per-VM/container actor. Lifecycle parent only — does no composition; just
// spawns NicActor children and tears them down in reverse-wave order on
// container delete.
class ContainerActor {
public:
    ContainerActor(
        const std::string& container_id,
        std::weak_ptr<HostDeviceActor> parent,
        std::shared_ptr<observability::ObservabilityContext> otel_context
    );

    Result<void> SendMessage(ActorMessage msg, std::chrono::milliseconds timeout);
    void Run();
    void Shutdown();

    models::ObjectState GetState() const;

private:
    std::string container_id_;
    std::weak_ptr<HostDeviceActor> parent_;
    std::shared_ptr<observability::ObservabilityContext> otel_context_;

    std::atomic<models::ObjectState> current_state_{models::ObjectState::WAITING_BOOTSTRAP};
    Mailbox<ActorMessage> mailbox_{256};
    std::unordered_map<std::string, std::shared_ptr<class NicActor>> nics_;
    models::ContainerSpec spec_;

    void ProcessMessage_(ActorMessage msg);
    Result<void> SpawnNic_(const std::string& nic_id);
    Result<void> ReapNic_(const std::string& nic_id);
};

// Per-NIC actor — the heart of ENI provisioning. On every input change
// (NicSpec, vnet, group, global) it composes a fresh NicGoalState in-process,
// SHA-256 hashes the canonical serialization, diffs against the previous
// composed copy, and emits a wave-ordered DeltaPlan to the dispatcher.
//
// CRITICAL: NicGoalState is composed in-actor — it is NEVER published over
// the southbound and NEVER written by the control plane (vm-eni #17, myth #13).
class NicActor {
public:
    NicActor(
        const std::string& eni_id,                   // ENI_<DPU>_<MAC>
        const std::string& nic_id,
        std::weak_ptr<HostDeviceActor> hdo,
        std::weak_ptr<ContainerActor> co,
        std::shared_ptr<compilation::StateCompilationEngine> compiler,
        std::shared_ptr<southbound::SouthboundExecutor> executor,
        std::shared_ptr<observability::ObservabilityContext> otel_context
    );

    Result<void> SendMessage(ActorMessage msg, std::chrono::milliseconds timeout);
    void Run();
    void Shutdown();

    models::ObjectState GetState() const;
    std::array<uint8_t,32> GetCurrentHash() const;        // last composed SHA-256

private:
    std::string eni_id_;
    std::string nic_id_;
    std::weak_ptr<HostDeviceActor> hdo_;
    std::weak_ptr<ContainerActor>  co_;

    std::shared_ptr<compilation::StateCompilationEngine> compiler_;
    std::shared_ptr<southbound::SouthboundExecutor>      executor_;
    std::shared_ptr<observability::ObservabilityContext> otel_context_;

    std::atomic<models::ObjectState> current_state_{models::ObjectState::WAITING_BOOTSTRAP};
    Mailbox<ActorMessage> mailbox_{512};

    // Inputs
    std::optional<models::NicSpec>                      spec_;
    std::unordered_set<std::string>                     missing_refs_;

    // Last composed program — diffed against fresh composition to produce deltas.
    std::optional<models::NicGoalState>                 last_composed_;
    std::array<uint8_t,32>                              last_hash_{};

    // ------------------------------------------------------------------------
    // The reactive loop:
    //   1. On any input change, call Compose_() to build a new NicGoalState.
    //   2. SHA-256 the canonical serialization (vm-eni #27).
    //   3. If hash unchanged, no-op.
    //   4. Else, diff old vs new → emit DeltaPlan ordered by wave (§4).
    //   5. Hand DeltaPlan to SouthboundExecutor; only the K8s-Lease primary
    //      pod actually issues SAI calls (vm-eni #12).
    // ------------------------------------------------------------------------
    Result<models::NicGoalState> Compose_(const std::string& trace_id);
    std::array<uint8_t,32>       HashGoalState_(const models::NicGoalState& gs);
    Result<models::DeltaPlan>    DiffAndPlan_(
        const std::optional<models::NicGoalState>& prev,
        const models::NicGoalState&                next,
        const std::string&                         trace_id
    );

    void ProcessMessage_(ActorMessage msg);
    Result<void> HandleNicSpecUpdate_(const ActorMessage& msg);
    Result<void> HandleRefInvalidation_(const ActorMessage& msg);
};

// Actor scheduler (thread pool)
class ActorScheduler {
public:
    explicit ActorScheduler(size_t worker_thread_count);
    ~ActorScheduler();
    
    // Submit actor task
    void Submit(std::function<void()> task);
    
    // Wait for all pending tasks
    void WaitAll();
    
    // Shutdown thread pool
    void Shutdown();
    
    // Metrics
    size_t PendingTaskCount() const;
    size_t CompletedTaskCount() const;

private:
    std::vector<std::thread> workers_;
    std::queue<std::function<void()>> task_queue_;
    mutable std::mutex queue_lock_;
    std::condition_variable cv_;
    std::atomic<bool> shutting_down_{false};
};

}  // namespace actor
}  // namespace fleetmanager
```

---

## 2. NicGoalState Composition Engine

### 2.1 The composed `NicGoalState`

`NicGoalState` is the fully-resolved, denormalized program for one ENI. It is
**composed in-process by the NicActor's `Compose_()` method on every input
change** — never published over the southbound, never written by the control
plane (vm-eni #17, Learning-DashNet myth #13). The control plane publishes
intent as a `NicSpec` reference bundle (vm-eni #1, myth #1); the agent
produces program.

```cpp
// ============================================================================
// NIC GOAL STATE — composed in-process by NicActor::Compose_()
// ============================================================================

namespace fleetmanager {
namespace models {

// One stage of the DASH ACL pipeline: stage1 (VNIC) → stage2 (SUBNET) →
// stage3 (VNET). Each stage produces an outcome that interacts with the
// next via *_AND_CONTINUE semantics (Learning-DashNet myth #4).
enum class AclStage : uint8_t { VNIC = 1, SUBNET = 2, VNET = 3 };

// Per vm-eni #21 + myth #4: every ENI has exactly six ACL slots —
// {inbound, outbound} × {stage1, stage2, stage3}, per address family.
// A `null` slot disables that stage. The slot is the binding; the slot
// holds an AclGroupId; the group holds the rules.
struct AclBindingSet {
    // Index 0 = stage1 (VNIC), 1 = stage2 (SUBNET), 2 = stage3 (VNET).
    std::array<std::optional<std::string>, 3> inbound_acl_v4{};
    std::array<std::optional<std::string>, 3> outbound_acl_v4{};
    std::array<std::optional<std::string>, 3> inbound_acl_v6{};
    std::array<std::optional<std::string>, 3> outbound_acl_v6{};
};

// Per-direction outbound program. Optional — inbound-only NICs (SLB VIPs)
// leave it std::nullopt (vm-eni #10).
struct NicOutbound {
    std::optional<std::string>     route_group_v4;
    std::optional<std::string>     route_group_v6;
    std::vector<RouteGroup>        route_groups_resolved;   // expanded for the device
    std::vector<Tunnel>            tunnels_used;            // joined from /global
    std::array<std::optional<AclGroup>, 3> acl_stages_v4;   // stage1/2/3
    std::array<std::optional<AclGroup>, 3> acl_stages_v6;
    std::optional<std::string>     meter_policy_id;
    std::optional<MeterPolicy>     meter_policy;
};

// Per-direction inbound program. Optional — outbound-only patterns leave
// it std::nullopt.
struct NicInbound {
    std::array<std::optional<AclGroup>, 3> acl_stages_v4;
    std::array<std::optional<AclGroup>, 3> acl_stages_v6;
    std::optional<std::string>     meter_policy_id;
    std::optional<MeterPolicy>     meter_policy;
    // Per-ENI inbound overrides; usually empty (rare, see DASH catalog).
    std::vector<proto::InboundRoutingRule> inbound_routing_rules;
};

// Reference to the manifest+chunks pair held in HostDeviceActor's cache.
// Mappings can be ~500 KiB to multi-MiB; the goal state carries a *reference*
// (vm-eni #8, myth #3), not the rows themselves.
struct VnetMappingRef {
    std::string                 vnet_id;
    std::array<uint8_t,32>      manifest_digest;        // SHA-256 of manifest
    std::vector<std::string>    chunk_ids;              // ordered chunk list
    uint64_t                    row_count;
};

// The composed program. Built by NicActor::Compose_() on every input change.
struct NicGoalState {
    // Identity (from NicSpec)
    std::string device_id;
    std::string container_id;
    std::string nic_id;
    std::string eni_id;                  // ENI_<DPU>_<MAC>  (vm-eni #3)
    std::string mac;
    std::string primary_ip;
    uint32_t    vlan_id;
    proto::NicType nic_type;             // VM_NIC | APPLIANCE_NIC | SLB_VIP
    proto::NicMode mode;                 // FULL_DUPLEX | INBOUND_ONLY | OUTBOUND_ONLY

    // ENI body — denormalized from NicSpec + joined globals
    Eni                eni;
    Vnet               vnet;
    PaValidation       pa_validation;
    std::optional<Qos> qos;

    // Per-direction programs (either may be std::nullopt per vm-eni #10).
    std::optional<NicOutbound> outbound;
    std::optional<NicInbound>  inbound;

    // Mapping table — referenced, not inlined.
    VnetMappingRef vnet_mapping_ref;

    // HA scope reference (HaSet lives on HDO; HaScope lives here per vm-eni #9).
    std::optional<std::string> ha_scope_ref;
    std::optional<HaScope>     ha_scope;

    // Routing-type lookup table (small, fleet-scope catalog).
    std::unordered_map<std::string, RoutingType> routing_types;

    // Composition metadata
    std::chrono::system_clock::time_point composed_at;
    std::unordered_map<std::string, uint64_t> source_revisions;  // per-input revision

    // SHA-256 of canonical_serialization(NicGoalState minus this field).
    // Per vm-eni #27 + myth #22: this is the idempotency key — equal hashes
    // mean zero SAI calls. It is also the value Reconcile compares against
    // the device-reported state hash.
    std::array<uint8_t,32> content_hash;
};

}  // namespace models
}  // namespace fleetmanager
```

### 2.2 NicActor composition algorithm

```cpp
// ============================================================================
// COMPOSITION — performed in-actor by NicActor::Compose_().
// All three ShardSet pods compose identically; only the K8s-Lease primary
// dispatches deltas to the wire (vm-eni #12, Specs/04 + Specs/05).
// ============================================================================

namespace fleetmanager {
namespace compilation {

class StateCompilationEngine {
public:
    // Composes NicGoalState from a NicSpec + joined globals/groups/vnet.
    // Called by NicActor::Compose_(); does NOT touch the southbound.
    Result<models::NicGoalState> ComposeNicGoalState(
        const models::NicSpec&                              spec,
        const actor::HostDeviceActor&                       hdo,    // for /global+/group/vnet lookup
        const std::string&                                  trace_id
    );

    // Diffs old vs new NicGoalState and produces a wave-ordered DeltaPlan.
    // The diff walks the composed structure field-by-field and maps changes
    // to per-DASH-object CREATE/UPDATE/DELETE commands (vm-eni #27).
    Result<models::DeltaPlan> DiffNicGoalState(
        const std::optional<models::NicGoalState>& prev,
        const models::NicGoalState&                next,
        const std::string&                         trace_id
    );

private:
    std::shared_ptr<persistence::StateStore>             state_store_;
    std::shared_ptr<observability::ObservabilityContext> otel_context_;

    // ========================================================================
    // ALGORITHM: Compose
    // ========================================================================
    //
    // Steps (referenced from vm-eni §3 and Specs/02 actor cascade):
    //   1. Read NicSpec.{vnet_id, route_group_ids, acl_group_slots[6 per fam],
    //      meter_policy_ids, ha_scope_ref, qos_id, prefix_tag refs}.
    //   2. For every referenced object, look up in HDO's cache (no etcd I/O).
    //      Any miss → return WAITING_REFS sentinel; NicActor stays parked.
    //   3. Expand PrefixTag refs inline at compose time (myth #16).
    //   4. Merge per-ENI route_rules with RouteGroup rules into a single LPM
    //      table inside NicGoalState (myth #15).
    //   5. Resolve VnetMapping as manifest+chunk_ids reference (myth #3) —
    //      do NOT inline rows.
    //   6. Stamp source_revisions with each input's per-object revision
    //      (myth #21 — revisions order writes to the SAME object only).
    //   7. SHA-256 the canonical serialization → content_hash.

    Result<std::vector<models::CompiledDelta>> ComputeDeltas_(
        const std::optional<models::NicGoalState>& prev,
        const models::NicGoalState&                next,
        const std::string&                         trace_id
    ) {
        std::vector<models::CompiledDelta> deltas;
        const bool first_time = !prev.has_value();

        auto emit = [&](models::DashObjectKind kind, models::OperationType op,
                        uint32_t wave, const std::string& object_id,
                        const std::array<uint8_t,32>& hash) {
            deltas.push_back({
                .command_id        = GenerateUUID(),
                .trace_id          = trace_id,
                .sequence_number   = (uint32_t)deltas.size(),
                .wave              = wave,
                .operation         = op,
                .target_kind       = kind,
                .target_object_id  = object_id,
                .eni_id            = next.eni_id,
                .content_hash      = hash,
                .status            = models::DeltaStatus::PENDING,
                .created_at        = std::chrono::system_clock::now(),
            });
        };

        // Wave 5 — ENI body (depends on VNET, RouteGroup, MeterPolicy, HaSet).
        if (first_time || HashOf_(prev->eni) != HashOf_(next.eni)) {
            emit(models::DashObjectKind::NIC_SPEC,
                 first_time ? models::OperationType::CREATE : models::OperationType::UPDATE,
                 /*wave=*/5, next.eni_id, HashOf_(next.eni));
        }

        // Wave 6 — per-ENI bindings: ACLs (6 slots per family), routes, meters.
        if (next.outbound) {
            for (size_t stage = 0; stage < 3; ++stage) {
                if (next.outbound->acl_stages_v4[stage]) {
                    emit(models::DashObjectKind::ACL_GROUP, models::OperationType::UPDATE,
                         6, next.outbound->acl_stages_v4[stage]->id(),
                         HashOf_(*next.outbound->acl_stages_v4[stage]));
                }
                if (next.outbound->acl_stages_v6[stage]) {
                    emit(models::DashObjectKind::ACL_GROUP, models::OperationType::UPDATE,
                         6, next.outbound->acl_stages_v6[stage]->id(),
                         HashOf_(*next.outbound->acl_stages_v6[stage]));
                }
            }
        }
        if (next.inbound) {
            for (size_t stage = 0; stage < 3; ++stage) {
                if (next.inbound->acl_stages_v4[stage]) {
                    emit(models::DashObjectKind::ACL_GROUP, models::OperationType::UPDATE,
                         6, next.inbound->acl_stages_v4[stage]->id(),
                         HashOf_(*next.inbound->acl_stages_v4[stage]));
                }
                if (next.inbound->acl_stages_v6[stage]) {
                    emit(models::DashObjectKind::ACL_GROUP, models::OperationType::UPDATE,
                         6, next.inbound->acl_stages_v6[stage]->id(),
                         HashOf_(*next.inbound->acl_stages_v6[stage]));
                }
            }
        }

        // Wave 4 — VnetMapping (manifest + chunks). Reference-only; the actual
        // chunk fetch happens against HDO's cache. We emit one UPDATE per
        // changed chunk_id, keyed by content_hash from the manifest.
        if (first_time ||
            prev->vnet_mapping_ref.manifest_digest != next.vnet_mapping_ref.manifest_digest) {
            emit(models::DashObjectKind::VNET_MAPPING_MANIFEST,
                 models::OperationType::UPDATE, /*wave=*/4,
                 next.vnet_mapping_ref.vnet_id, next.vnet_mapping_ref.manifest_digest);
            // Per-chunk deltas would be emitted by HDO when chunks change;
            // NicActor only re-references them.
        }

        return deltas;
    }

    // ========================================================================
    // ALGORITHM: SHA-256 content hash
    // ========================================================================
    //
    // Per vm-eni #26 + #27: every CompiledDelta carries the SHA-256 digest
    // of the canonical proto serialization of its target object body. The
    // agent compares this against its last-applied digest to skip work.

    std::array<uint8_t,32> HashOf_(const google::protobuf::Message& msg) {
        std::string canonical;
        google::protobuf::io::StringOutputStream out(&canonical);
        google::protobuf::io::CodedOutputStream coded(&out);
        coded.SetSerializationDeterministic(true);
        msg.SerializeToCodedStream(&coded);
        coded.Trim();

        std::array<uint8_t,32> digest{};
        SHA256(reinterpret_cast<const uint8_t*>(canonical.data()),
               canonical.size(), digest.data());
        return digest;
    }

    // ========================================================================
    // ALGORITHM: Dependency Graph Resolution (wave order, vm-eni §4)
    // ========================================================================
    //
    // CREATE order:  Wave 0 (idempotent globals) → 1 (Tunnel/HaSet) →
    //                2 (RouteGroup, AclGroup, MeterPolicy, OutboundPortMap) →
    //                3 (Vnet, PaValidation) → 4 (VnetMapping manifest+chunks) →
    //                5 (Eni, HaScope) → 6 (per-ENI bindings).
    // DELETE order:  reverse — enforced on DRAINING transitions.

    struct DependencyEdge {
        std::string from_delta_id;
        std::string to_delta_id;
        bool is_blocking;
    };

    Result<std::vector<models::CompiledDelta>> ResolveDependencies_(
        std::vector<models::CompiledDelta> deltas,
        const std::string& trace_id
    ) {
        // Sort by wave first (CREATE) or reverse-wave (DELETE), then topo-sort
        // within wave for objects that mutually reference (e.g., RouteGroup
        // entries that point at Tunnels in the same wave).
        std::stable_sort(deltas.begin(), deltas.end(),
            [](const auto& a, const auto& b) {
                if (a.operation == models::OperationType::DELETE &&
                    b.operation == models::OperationType::DELETE) {
                    return a.wave > b.wave;   // reverse for delete
                }
                return a.wave < b.wave;        // forward for create/update
            });

        // Topological tie-break: the dependency_edges collection encodes
        // intra-wave references (e.g., RouteGroup → Tunnel). Run Kahn's
        // algorithm over the surviving edges.
        std::unordered_map<std::string, int> in_degree;
        std::unordered_map<std::string, std::vector<std::string>> adj;
        for (const auto& d : deltas) in_degree[d.command_id] = 0;
        // ... (edges populated from a pre-built kind-to-kind dependency map)

        std::queue<std::string> q;
        for (const auto& [id, deg] : in_degree) if (deg == 0) q.push(id);

        std::vector<models::CompiledDelta> sorted;
        sorted.reserve(deltas.size());
        while (!q.empty()) {
            auto id = q.front(); q.pop();
            auto it = std::find_if(deltas.begin(), deltas.end(),
                [&](const auto& d){ return d.command_id == id; });
            if (it != deltas.end()) {
                sorted.push_back(*it);
                sorted.back().sequence_number = sorted.size() - 1;
            }
            for (const auto& nxt : adj[id]) {
                if (--in_degree[nxt] == 0) q.push(nxt);
            }
        }
        if (sorted.size() != deltas.size())
            return Error("Circular dependency in delta graph");
        return sorted;
    }

    // ========================================================================
    // ALGORITHM: Device-Specific Config Compilation
    // ========================================================================

    Result<void> CompileDeviceConfigs_(
        std::vector<models::CompiledDelta>& deltas,
        const models::DeviceProfile& device_profile,
        const std::string& trace_id
    ) {
        for (auto& delta : deltas) {
            switch (device_profile.device_type()) {
                case models::DeviceType::APPLIANCE_DASH_STANDALONE:
                case models::DeviceType::APPLIANCE_DASH_CHASSIS:
                    TRY(CompileToDASH_(delta, device_profile));
                    break;
                case models::DeviceType::HOST_DPU_ATTACHED:
                    TRY(CompileToSONiC_(delta, device_profile));
                    break;
                case models::DeviceType::HOST_LINUX:
                    TRY(CompileToLinux_(delta, device_profile));
                    break;
            }
        }
        return Ok();
    }

    // CompileToDASH_ dispatches on DashObjectKind — the southbound contract
    // covers all 15 kinds, not just ENI (see §4).
    Result<void> CompileToDASH_(models::CompiledDelta& delta,
                                const models::DeviceProfile& profile) {
        // Serialize the kind-specific message into delta.target_config_bytes.
        // The driver picks the right SAI verb at dispatch time.
        return Ok();
    }

    Result<void> CompileToSONiC_(models::CompiledDelta& delta,
                                 const models::DeviceProfile& profile) { return Ok(); }
    Result<void> CompileToLinux_(models::CompiledDelta& delta,
                                 const models::DeviceProfile& profile) { return Ok(); }
};

}  // namespace compilation
}  // namespace fleetmanager
```

---

## 3. State Persistence Layer

### 3.1 RocksDB Abstraction

```cpp
// ============================================================================
// STATE STORE (ROCKSDB)
// ============================================================================

namespace fleetmanager {
namespace persistence {

class StateStore {
public:
    explicit StateStore(const std::string& db_path);
    ~StateStore();
    
    // ========================================================================
    // WRITE OPERATIONS
    // ========================================================================
    
    // Save device profile
    Result<void> SaveDeviceProfile(const std::string& device_id, const models::DeviceProfile& profile);

    // Save HostDeviceActor cache snapshot (full /global+/group+/vnet hot cache).
    Result<void> SaveHostDeviceState(const std::string& device_id,
                                     const models::HostDeviceState& state);

    // Save the last-composed NicGoalState per ENI, keyed by eni_id.
    // Used on warm restart so the NicActor can short-circuit composition
    // when the inputs match what the previous primary computed.
    Result<void> SaveNicGoalState(const std::string& eni_id,
                                  const models::NicGoalState& goal_state);

    // Save delta command (for recovery)
    Result<void> SaveDeltaCommand(const std::string& device_id,
                                  const models::CompiledDelta& delta);

    // Per-object revision watermark (vm-eni #11). Compared against
    // ConfigEntry.metadata.revision before applying a write — orders
    // updates to the SAME object, never across objects.
    Result<void> SaveLastAppliedRevision(const std::string& object_path,
                                         uint64_t revision);

    // Save reconciliation checkpoint — the SHA-256 of the most recently
    // composed NicGoalState (vm-eni #27).
    Result<void> SaveReconciliationCheckpoint(
        const std::string& eni_id,
        const std::array<uint8_t,32>& nic_goal_state_hash,
        std::chrono::system_clock::time_point timestamp
    );
    
    // Batch write (atomic)
    class BatchWriter {
    public:
        void Put(const std::string& key, const std::string& value);
        void Delete(const std::string& key);
        Result<void> Commit();
    private:
        rocksdb::WriteBatch batch_;
        std::shared_ptr<rocksdb::DB> db_;
    };
    
    BatchWriter StartBatch();
    
    // ========================================================================
    // READ OPERATIONS
    // ========================================================================
    
    // Load device profile
    Result<models::DeviceProfile> LoadDeviceProfile(const std::string& device_id);

    // Load HostDeviceActor cache snapshot
    Result<models::HostDeviceState> LoadHostDeviceState(const std::string& device_id);

    // Load last-composed NicGoalState for warm restart
    Result<models::NicGoalState> LoadNicGoalState(const std::string& eni_id);

    // Load pending delta commands for a device
    Result<std::vector<models::CompiledDelta>> LoadPendingDeltas(const std::string& device_id);

    // Load reconciliation checkpoint (SHA-256 of last-composed NicGoalState)
    Result<std::array<uint8_t,32>> LoadReconciliationCheckpoint(const std::string& eni_id);
    
    // ========================================================================
    // ITERATION
    // ========================================================================
    
    // Scan all devices for a shard
    class Iterator {
    public:
        bool Next(std::string& device_id, models::HostDeviceState& out);
        void Seek(const std::string& start_key);
    private:
        std::unique_ptr<rocksdb::Iterator> iter_;
        std::shared_ptr<rocksdb::DB> db_;
    };
    
    Iterator Scan(uint32_t shard_id);
    
    // ========================================================================
    // UTILITIES
    // ========================================================================
    
    Result<void> Compact();
    Result<std::string> GetProperty(const std::string& prop_name);
    Result<void> Backup(const std::string& backup_path);

private:
    std::unique_ptr<rocksdb::DB> db_;
    rocksdb::Options options_;
    
    // Key generation
    static std::string MakeKey(uint32_t shard_id, const std::string& device_id, 
                               const std::string& object_type, const std::string& object_id);
};

}  // namespace persistence
}  // namespace fleetmanager
```

---

## 4. Southbound Driver Layer

### 4.1 Driver Architecture

```cpp
// ============================================================================
// SOUTHBOUND DRIVER LAYER
// ============================================================================

namespace fleetmanager {
namespace southbound {

// Base driver interface. The southbound contract covers all 15 DASH object
// kinds — the `Apply` verb dispatches on `delta.target_kind`. ENI is just
// one of those kinds; it is NOT privileged at the wire level.
//
// Per vm-eni #12, only the K8s-Lease primary pod actually invokes Apply()
// on a real channel. Standby pods build identical state but their drivers
// are kept in HAL shadow mode (Apply is a no-op that records the would-be
// SAI calls for instant failover diffing).
class SouthboundDriver {
public:
    virtual ~SouthboundDriver() = default;

    // Single typed verb — the driver dispatches internally on
    // delta.target_kind to the matching DASH SAI / gNMI call.
    // Idempotent on (delta.command_id, delta.content_hash): if the device
    // already has a record with this digest, the call returns OK without
    // re-programming silicon (vm-eni #26 + myth #22).
    virtual Result<void> Apply(const models::CompiledDelta& delta) = 0;

    // Read back the device's reported content hash for an object — used by
    // ReconciliationEngine to detect drift without re-fetching the body.
    virtual Result<std::array<uint8_t,32>> ReadHash(
        models::DashObjectKind kind,
        const std::string&     object_id) = 0;

    // Bulk read for a kind (used during cold-start drift scan).
    virtual Result<std::unordered_map<std::string, std::array<uint8_t,32>>>
        ListHashes(models::DashObjectKind kind) = 0;
};

// ========================================================================
// DASH DRIVER (gNMI)
// ========================================================================

class DashSouthboundDriver : public SouthboundDriver {
public:
    explicit DashSouthboundDriver(const std::string& device_ip, uint16_t port = 6030);

    Result<void> Apply(const models::CompiledDelta& delta) override;
    Result<std::array<uint8_t,32>> ReadHash(
        models::DashObjectKind kind, const std::string& object_id) override;
    Result<std::unordered_map<std::string, std::array<uint8_t,32>>>
        ListHashes(models::DashObjectKind kind) override;

private:
    std::string device_ip_;
    uint16_t port_;

    // gNMI stub (persistent connection)
    std::unique_ptr<gnmi::gNMI::Stub> gnmi_stub_;
    std::shared_ptr<grpc::Channel> channel_;

    // Per-kind dispatcher: maps DashObjectKind to the gNMI path + SAI verb.
    // Covers all 15 published kinds (RoutingType, Appliance, HostSpec,
    // Tunnel, Qos, PrefixTag, HaSet, Vnet, VnetMappingManifest,
    // VnetMappingChunk, PaValidation, RouteGroup, AclGroup, MeterPolicy,
    // OutboundPortMap, NicSpec/Eni, ContainerSpec, HaScope).
    Result<gnmi::SetRequest> DeltaToSetRequest_(const models::CompiledDelta& delta);
};

// ========================================================================
// SONIC DRIVER (SAI Thrift)
// ========================================================================

class SONiCDriver : public SouthboundDriver {
public:
    explicit SONiCDriver(const std::string& device_ip, uint16_t port = 9092);

    Result<void> Apply(const models::CompiledDelta& delta) override;
    Result<std::array<uint8_t,32>> ReadHash(
        models::DashObjectKind kind, const std::string& object_id) override;
    Result<std::unordered_map<std::string, std::array<uint8_t,32>>>
        ListHashes(models::DashObjectKind kind) override;

private:
    std::string device_ip_;
    uint16_t port_;

    std::shared_ptr<SAIClient> sai_client_;
    std::shared_ptr<TTransport> transport_;

    Result<std::vector<SAIAttribute>> DeltaToSAIAttributes_(const models::CompiledDelta& delta);
};

// ========================================================================
// LINUX DRIVER (netlink)
// ========================================================================

class LinuxDriver : public SouthboundDriver {
public:
    explicit LinuxDriver(const std::string& device_ip);

    Result<void> Apply(const models::CompiledDelta& delta) override;
    Result<std::array<uint8_t,32>> ReadHash(
        models::DashObjectKind kind, const std::string& object_id) override;
    Result<std::unordered_map<std::string, std::array<uint8_t,32>>>
        ListHashes(models::DashObjectKind kind) override;

private:
    std::string device_ip_;
    int netlink_socket_;

    Result<struct nl_msg*> DeltaToNetlinkMsg_(const models::CompiledDelta& delta);
};

// ========================================================================
// SOUTHBOUND EXECUTOR (Async Execution Engine)
// ========================================================================
//
// Receives DeltaPlans from NicActor::Compose_() (and Wave 0–4 plans from
// HostDeviceActor). Only the K8s-Lease primary pod actually issues the
// southbound calls; standby pods record the plan in HAL shadow mode for
// instant failover (vm-eni #12).

class SouthboundExecutor {
public:
    explicit SouthboundExecutor(
        size_t worker_thread_count,
        std::shared_ptr<ha::HACoordinator> ha_coordinator);
    ~SouthboundExecutor();

    // Submit a wave-ordered DeltaPlan for asynchronous execution. On standby
    // pods, this no-ops the wire calls but updates the in-memory shadow.
    void ExecutePlan(
        const models::DeltaPlan& plan,
        const std::string& device_id,
        models::TargetType target_type,
        std::function<void(const Result<void>&)> callback
    );

    // Submit a single delta (used by HDO for Wave 0–4 globals/groups/vnet).
    void ExecuteDelta(
        const models::CompiledDelta& delta,
        const std::string& device_id,
        models::TargetType target_type,
        std::function<void(const Result<void>&)> callback
    );

    size_t GetQueueDepth(models::TargetType target_type) const;

    struct ExecutionMetrics {
        uint64_t total_executed = 0;
        uint64_t total_succeeded = 0;
        uint64_t total_failed = 0;
        uint64_t skipped_idempotent = 0;   // hash-equal short-circuit count
        double   avg_latency_ms = 0.0;
    };

    ExecutionMetrics GetMetrics(models::TargetType target_type) const;

private:
    std::shared_ptr<ha::HACoordinator> ha_coordinator_;
    std::unordered_map<models::TargetType, std::shared_ptr<SouthboundDriver>> drivers_;

    // Per-target-type executor threads
    std::unordered_map<models::TargetType, std::vector<std::thread>> executor_pools_;

    // Per-target-type queues (thread-safe)
    std::unordered_map<models::TargetType,
                       std::queue<std::pair<models::CompiledDelta,
                                           std::function<void(const Result<void>&)>>>> queues_;
};

}  // namespace southbound
}  // namespace fleetmanager
```

---

## 5. PubSub Subscription Manager

### 5.1 Configuration Reception

```cpp
// ============================================================================
// PUBSUB SUBSCRIPTION MANAGER
// ============================================================================

namespace fleetmanager {
namespace pubsub {

// Abstraction for different PubSub backends (Redis Streams, Kafka, etcd)
class PubSubBackend {
public:
    virtual ~PubSubBackend() = default;
    
    virtual Result<void> Subscribe(
        const std::string& topic,
        std::function<void(const std::string& message_body)> callback
    ) = 0;
    
    virtual Result<void> Unsubscribe(const std::string& topic) = 0;
};

class RedisPubSubBackend : public PubSubBackend {
public:
    explicit RedisPubSubBackend(const std::string& redis_url);
    
    Result<void> Subscribe(
        const std::string& topic,
        std::function<void(const std::string& message_body)> callback
    ) override;
    
    Result<void> Unsubscribe(const std::string& topic) override;

private:
    std::string redis_url_;
    std::unique_ptr<redis::Redis> redis_client_;
    std::unordered_map<std::string, std::thread> subscription_threads_;
};

// ========================================================================
// SUBSCRIPTION MANAGER
//
// The pod is the single etcd watch holder; per vm-eni #16 there is one
// process-wide cache per object kind, fan-out happens via in-process actor
// messages. The HostDeviceActor (HDO) holds /global+/group watches; a NIC's
// referenced /vnet subtree is watched at the HDO with refcount (vm-eni #7).
// VnetMapping arrives as a manifest watch + on-demand chunk fetches (vm-eni
// #8, myth #3).
// ========================================================================

class SubscriptionManager {
public:
    explicit SubscriptionManager(std::shared_ptr<PubSubBackend> backend);

    // Watch /config/v1/hosts/<device_id>/spec and /global/<device_id>/**
    // and /group/<device_id>/** — these feed the HostDeviceActor cache.
    // Wires WAITING_BOOTSTRAP → READY transition (vm-eni #14).
    Result<void> SubscribeHostDevice(
        const std::string& device_id,
        std::function<void(const std::string& trace_id,
                           models::DashObjectKind kind,
                           const std::string& object_id,
                           const std::vector<uint8_t>& payload,
                           uint64_t revision)> callback
    );

    // Watch /config/v1/vnet/<device_id>/<vnet_id>/spec, .../pa_validation,
    // and .../mapping/_manifest. The manifest callback is what triggers
    // chunk fetches.
    Result<void> SubscribeVnet(
        const std::string& device_id,
        const std::string& vnet_id,
        std::function<void(const std::string& trace_id,
                           models::DashObjectKind kind,
                           const std::string& object_id,
                           const std::vector<uint8_t>& payload,
                           uint64_t revision)> callback
    );

    // Fetch a single VnetMapping chunk on demand. Called by the HDO
    // when a manifest update lists a chunk_id whose digest differs from
    // the cached copy. Chunks are <1 MiB and content-addressed by SHA-256.
    Result<models::VnetMappingChunk> FetchVnetMappingChunk(
        const std::string& device_id,
        const std::string& vnet_id,
        const std::string& chunk_id);

    // Watch a container subtree to discover NICs (CO actor lifecycle).
    Result<void> SubscribeContainer(
        const std::string& device_id,
        const std::string& container_id,
        std::function<void(const std::string& trace_id,
                           const std::vector<uint8_t>& container_spec_bytes,
                           uint64_t revision)> callback
    );

    // Watch a single NIC's spec — feeds the NicActor's CONFIG_UPDATE_NIC_SPEC
    // mailbox path.
    Result<void> SubscribeNicSpec(
        const std::string& device_id,
        const std::string& container_id,
        const std::string& nic_id,
        std::function<void(const std::string& trace_id,
                           const std::vector<uint8_t>& nic_spec_bytes,
                           uint64_t revision)> callback
    );

    Result<void> Unsubscribe(const std::string& device_id);

private:
    std::shared_ptr<PubSubBackend> backend_;

    std::unordered_map<std::string, std::vector<std::string>> device_subscriptions_;
    mutable std::shared_mutex subscriptions_lock_;

    // Helpers
    std::string MakeTopic_(const std::string& device_id,
                           const std::string& container_id = "",
                           const std::string& nic_id = "") const;
};

}  // namespace pubsub
}  // namespace fleetmanager
```

---

## 6. Reconciliation Engine

### 6.1 Drift Detection & Recovery

```cpp
// ============================================================================
// RECONCILIATION ENGINE
// ============================================================================

namespace fleetmanager {
namespace reconciliation {

class ReconciliationEngine {
public:
    explicit ReconciliationEngine(
        std::shared_ptr<StateStore> state_store,
        std::shared_ptr<StateCompilationEngine> compiler,
        std::shared_ptr<SouthboundExecutor> executor,
        std::shared_ptr<ObservabilityContext> otel_context
    );
    
    // Trigger reconciliation for a single ENI. The unit of reconciliation is
    // the composed NicGoalState (vm-eni #27, myth #22): we compare the
    // SHA-256 of the locally composed NicGoalState against the digest the
    // device reports. Equal → no-op. Drift → recompose, diff, dispatch.
    Result<void> ReconcileEni(
        const std::string& eni_id,
        const models::DeviceProfile& device_profile,
        const std::string& trace_id
    );

    // Cold-start drift scan: walks every CompiledDelta state in the device
    // for the configured kinds, compares hashes against the locally cached
    // last-applied digests, and re-emits Apply() for any mismatch.
    Result<void> ReconcileDevice(
        const std::string& device_id,
        const models::DeviceProfile& device_profile,
        const std::string& trace_id
    );
    
    // Metrics
    struct ReconciliationMetrics {
        uint64_t total_reconciliations = 0;
        uint64_t devices_in_sync = 0;
        uint64_t devices_drifted = 0;
        uint64_t corrective_actions_taken = 0;
        double avg_reconciliation_time_ms = 0.0;
    };
    
    ReconciliationMetrics GetMetrics() const;

private:
    std::shared_ptr<StateStore> state_store_;
    std::shared_ptr<StateCompilationEngine> compiler_;
    std::shared_ptr<SouthboundExecutor> executor_;
    std::shared_ptr<ObservabilityContext> otel_context_;
    
    // ========================================================================
    // ALGORITHM: NicGoalState SHA-256 — the idempotency key
    // ========================================================================
    //
    // Per vm-eni #26 + #27, the reconciliation primitive is the SHA-256 of
    // the canonical proto serialization of NicGoalState. The control plane
    // and the device compute the same digest from the same composed program;
    // if they agree, silicon is in sync and no SAI calls are needed. This
    // is also the value carried in CompiledDelta.content_hash.

    std::array<uint8_t,32> ComputeStateHash_(const models::NicGoalState& gs) {
        // Serialize deterministically (proto3 canonical form: maps sorted by
        // key, repeated fields in declaration order, no unknown fields).
        std::string canonical;
        google::protobuf::io::StringOutputStream out(&canonical);
        google::protobuf::io::CodedOutputStream coded(&out);
        coded.SetSerializationDeterministic(true);
        gs.SerializeToCodedStream(&coded);
        coded.Trim();

        std::array<uint8_t,32> digest{};
        SHA256(reinterpret_cast<const uint8_t*>(canonical.data()),
               canonical.size(), digest.data());
        return digest;
    }

    // ========================================================================
    // ALGORITHM: Drift Detection (per ENI)
    // ========================================================================

    Result<bool> DetectDrift_(
        const std::string& eni_id,
        const models::DeviceProfile& device_profile,
        const std::string& trace_id
    ) {
        // Step 1: Load the last-composed NicGoalState (warm cache or RocksDB).
        auto cached_result = state_store_->LoadNicGoalState(eni_id);
        if (!cached_result.has_value()) {
            return Error("Failed to load cached NicGoalState");
        }
        const auto& cached = cached_result.value();

        // Step 2: Read the device-reported content hash for the ENI.
        // Single round-trip via SouthboundDriver::ReadHash; no full
        // re-fetch of the program body.
        auto device_hash_result = ReadDeviceReportedHash_(eni_id, device_profile);
        if (!device_hash_result.has_value()) {
            return Error("Failed to query device-reported hash");
        }
        const auto& device_hash = device_hash_result.value();

        // Step 3: Recompute the local digest from the cached NicGoalState
        // (defense in depth — the cached `content_hash` field should match,
        // but recomputing catches storage corruption).
        auto local_hash = ComputeStateHash_(cached);

        // Step 4: Compare.
        bool drifted = (local_hash != device_hash);

        if (drifted) {
            LOG(WARNING, "NicGoalState drift for eni {}: local_sha256={}, device_sha256={}",
                eni_id, ToHex(local_hash), ToHex(device_hash));
        }

        return drifted;
    }

    // Reads the device-reported content_hash for an ENI's program.
    Result<std::array<uint8_t,32>> ReadDeviceReportedHash_(
        const std::string& eni_id,
        const models::DeviceProfile& device_profile
    );
};

}  // namespace reconciliation
}  // namespace fleetmanager
```

---

## 7. gRPC Service Implementation

### 7.1 Service Definition & Implementation

```cpp
// ============================================================================
// GRPC SERVICE IMPLEMENTATION
// ============================================================================

namespace fleetmanager {
namespace grpc_service {

// Generated from fleet_manager.proto
class FleetManagerServiceImpl final : public FleetManagerService::Service {
public:
    explicit FleetManagerServiceImpl(
        std::shared_ptr<registry::DeviceRegistry> device_registry,
        std::shared_ptr<actor::ActorScheduler> actor_scheduler,
        std::shared_ptr<persistence::StateStore> state_store,
        std::shared_ptr<pubsub::SubscriptionManager> pubsub_manager,
        std::shared_ptr<ObservabilityContext> otel_context,
        std::shared_ptr<HACoordinator> ha_coordinator
    );
    
    // ========================================================================
    // RPC: RegisterDevice
    // ========================================================================
    
    ::grpc::Status RegisterDevice(
        ::grpc::ServerContext* context,
        const proto::RegisterDeviceRequest* request,
        proto::RegisterDeviceResponse* response
    ) override {
        
        // Extract trace context from gRPC metadata
        std::string trace_id = ExtractTraceID_(context->client_metadata());
        auto span = otel_context_->CreateSpan("RegisterDevice", trace_id);
        
        try {
            // Step 1: Validate device profile
            const auto& profile = request->device_profile();
            
            if (profile.device_id().empty()) {
                return grpc::Status(grpc::StatusCode::INVALID_ARGUMENT, "device_id required");
            }
            
            span->AddEvent("device_profile_validated");
            
            // Step 2: Check if device already registered
            if (device_registry_->GetDevice(profile.device_id()).has_value()) {
                return grpc::Status(grpc::StatusCode::ALREADY_EXISTS, "device already registered");
            }
            
            // Step 3: Register device
            auto register_result = device_registry_->RegisterDevice(profile);
            if (!register_result.has_value()) {
                return grpc::Status(grpc::StatusCode::INTERNAL, register_result.error());
            }
            
            std::string shard_id = register_result.value();
            span->AddEvent("device_registered", {{"shard_id", shard_id}});
            
            // Step 4: Spawn the HostDeviceActor (HDO). It begins life in
            // WAITING_BOOTSTRAP and only transitions to READY once /global +
            // /group hydration drains (vm-eni #14). Container/NIC actors are
            // spawned reactively as their specs appear under /hosts/<dev>/...
            auto device_profile = device_registry_->GetDevice(profile.device_id());
            auto hdo = std::make_shared<actor::HostDeviceActor>(
                profile.device_id(),
                device_profile.value(),
                state_store_,
                compiler_,
                executor_,
                otel_context_
            );

            // Step 5: Schedule actor to run in thread pool
            actor_scheduler_->Submit([hdo]() { hdo->Run(); });

            span->AddEvent("host_device_actor_created_and_scheduled");
            
            // Step 6: Trigger subscription. The HDO watches /global+/group
            // and the host spec; it lazily sub-watches /vnet/<id>/** the
            // first time a NIC binds to that vnet (vm-eni #7).
            auto sub_result = pubsub_manager_->SubscribeHostDevice(
                profile.device_id(),
                [this, device_id = profile.device_id()](
                    const std::string& trace_id,
                    models::DashObjectKind kind,
                    const std::string& object_id,
                    const std::vector<uint8_t>& payload,
                    uint64_t revision) {
                    OnConfigurationReceived_(device_id, trace_id, kind,
                                             object_id, payload, revision);
                }
            );
            
            if (!sub_result.has_value()) {
                LOG(ERROR, "Failed to subscribe device {}: {}", profile.device_id(), sub_result.error());
            }
            
            span->AddEvent("subscription_initiated");
            
            // Step 7: Fill response
            response->set_shard_id(shard_id);
            response->add_subscription_topics(
                "/config/hosts/" + profile.device_id()
            );
            
            span->AddEvent("response_prepared");
            
            return grpc::Status::OK;
            
        } catch (const std::exception& e) {
            span->AddEvent("error", {{"message", e.what()}});
            return grpc::Status(grpc::StatusCode::INTERNAL, e.what());
        }
    }
    
    // ========================================================================
    // RPC: Heartbeat
    // ========================================================================
    
    ::grpc::Status Heartbeat(
        ::grpc::ServerContext* context,
        const proto::HeartbeatRequest* request,
        proto::HeartbeatResponse* response
    ) override {
        
        std::string trace_id = ExtractTraceID_(context->client_metadata());
        
        // Validate device exists
        if (!device_registry_->GetDevice(request->device_id()).has_value()) {
            return grpc::Status(grpc::StatusCode::NOT_FOUND, "device not found");
        }
        
        // Send heartbeat message to the HostDeviceActor mailbox.
        actor::ActorMessage msg{
            .type     = actor::ActorMessage::DEVICE_HEARTBEAT,
            .trace_id = trace_id
        };
        
        // Update metrics
        metrics_->RecordHeartbeat(request->device_id());
        
        response->set_status("OK");
        return grpc::Status::OK;
    }
    
    // ========================================================================
    // RPC: ReportTelemetry
    // ========================================================================
    
    ::grpc::Status ReportTelemetry(
        ::grpc::ServerContext* context,
        const proto::TelemetryReport* request,
        proto::TelemetryResponse* response
    ) override {
        
        std::string trace_id = ExtractTraceID_(context->client_metadata());
        
        // Process telemetry asynchronously
        // (could update RocksDB with device telemetry, emit metrics)
        
        response->set_ack_id(GenerateUUID());
        return grpc::Status::OK;
    }

private:
    std::shared_ptr<registry::DeviceRegistry> device_registry_;
    std::shared_ptr<actor::ActorScheduler> actor_scheduler_;
    std::shared_ptr<persistence::StateStore> state_store_;
    std::shared_ptr<pubsub::SubscriptionManager> pubsub_manager_;
    std::shared_ptr<ObservabilityContext> otel_context_;
    std::shared_ptr<HACoordinator> ha_coordinator_;
    std::shared_ptr<compilation::StateCompilationEngine> compiler_;
    std::shared_ptr<southbound::SouthboundExecutor> executor_;
    
    // Helpers
    std::string ExtractTraceID_(const std::multimap<grpc::string_ref, grpc::string_ref>& metadata) {
        auto it = metadata.find("traceparent");
        if (it != metadata.end()) {
            return std::string(it->second.data(), it->second.length());
        }
        return GenerateUUID();  // Generate if not present
    }
    
    void OnConfigurationReceived_(
        const std::string& device_id,
        const std::string& trace_id,
        models::DashObjectKind kind,
        const std::string& object_id,
        const std::vector<uint8_t>& payload,
        uint64_t revision
    ) {
        // Route to the per-device HostDeviceActor. HDO decodes by `kind`
        // and dispatches to itself (global/group/vnet caches) or fans out
        // REF_INVALIDATION to the affected NicActors.
        actor::ActorMessage msg{
            .type      = actor::ActorMessage::CONFIG_UPDATE_GLOBAL_OR_GROUP,
            .trace_id  = trace_id,
            .kind      = kind,
            .object_id = object_id,
            .payload   = payload,
            .revision  = revision,
        };
        // ... HDO mailbox enqueue (non-blocking) ...
    }
};

}  // namespace grpc_service
}  // namespace fleetmanager
```

---

## 8. HA Coordination & Leader Election

### 8.1 K8s Lease Integration

```cpp
// ============================================================================
// HA COORDINATOR (K8s LEASE)
// ============================================================================

namespace fleetmanager {
namespace ha {

class HACoordinator {
public:
    explicit HACoordinator(
        const std::string& shard_id,
        const std::string& pod_name,
        const std::string& namespace_name,
        std::shared_ptr<KubernetesClient> k8s_client
    );
    
    // Initialize lease and contend for leadership
    Result<void> Initialize();
    
    // Check if this pod is PRIMARY
    bool IsPrimary() const {
        return am_i_primary_.load(std::memory_order_acquire);
    }
    
    // Graceful shutdown (relinquish lease)
    Result<void> GracefulShutdown();
    
    // Background lease renewal loop
    void LeaseRenewalLoop();

private:
    std::string shard_id_;
    std::string pod_name_;
    std::string namespace_name_;
    std::shared_ptr<KubernetesClient> k8s_client_;
    
    std::atomic<bool> am_i_primary_{false};
    std::atomic<bool> shutting_down_{false};
    std::thread lease_renewal_thread_;
    
    // ========================================================================
    // ALGORITHM: Leader Election via K8s Lease
    // ========================================================================
    
    Result<void> ContendForLease_() {
        // Attempt to acquire K8s Lease object
        std::string lease_name = "dashfabric-shard-" + shard_id_;
        
        auto lease_result = k8s_client_->GetLease(lease_name, namespace_name_);
        
        if (!lease_result.has_value()) {
            // Lease doesn't exist, create it
            return k8s_client_->CreateLease(
                lease_name,
                namespace_name_,
                pod_name_,
                std::chrono::seconds(30)  // TTL
            );
        }
        
        const auto& existing_lease = lease_result.value();
        
        // Check if lease is held by another pod and still valid
        if (!existing_lease.owner().empty() && existing_lease.IsValid()) {
            // Lease held by another pod, wait and retry
            return Error("Lease held by " + existing_lease.owner());
        }
        
        // Lease expired or unowned, try to acquire it
        return k8s_client_->UpdateLease(
            lease_name,
            namespace_name_,
            pod_name_,
            std::chrono::seconds(30)
        );
    }
    
    // ========================================================================
    // ALGORITHM: Lease Renewal
    // ========================================================================
    
    void RenewLeaseLoop_() {
        while (!shutting_down_.load()) {
            std::this_thread::sleep_for(std::chrono::seconds(10));  // Renew every 10s
            
            auto renew_result = k8s_client_->UpdateLease(
                "dashfabric-shard-" + shard_id_,
                namespace_name_,
                pod_name_,
                std::chrono::seconds(30)
            );
            
            if (renew_result.has_value()) {
                am_i_primary_.store(true, std::memory_order_release);
                LOG(DEBUG, "Lease renewed for pod {}", pod_name_);
            } else {
                // Failed to renew, might have lost lease to another pod
                am_i_primary_.store(false, std::memory_order_release);
                LOG(WARNING, "Failed to renew lease: {}", renew_result.error());
                
                // Attempt to re-acquire
                auto reacquire = ContendForLease_();
                if (reacquire.has_value()) {
                    am_i_primary_.store(true, std::memory_order_release);
                }
            }
        }
    }
};

}  // namespace ha
}  // namespace fleetmanager
```

---

## 9. Memory Management & Error Handling

### 9.1 Error Handling Strategy

```cpp
// ============================================================================
// ERROR HANDLING & RESULT TYPE
// ============================================================================

namespace fleetmanager {

// Result<T> type (similar to Rust Result enum)
template<typename T>
class Result {
public:
    // Construct success result
    static Result<T> Ok(T value) {
        return Result(std::move(value), "", true);
    }
    
    // Construct error result
    static Result<T> Error(const std::string& message) {
        return Result({}, message, false);
    }
    
    // Check if result is success
    bool has_value() const { return is_ok_; }
    
    // Get value (panics if error)
    T& value() {
        if (!is_ok_) {
            throw std::runtime_error("Result is error: " + error_message_);
        }
        return value_;
    }
    
    // Get error message
    const std::string& error() const { return error_message_; }
    
    // Operator overloads
    explicit operator bool() const { return is_ok_; }
    T* operator->() { return &value(); }
    T& operator*() { return value(); }

private:
    T value_;
    std::string error_message_;
    bool is_ok_;
    
    Result(T val, const std::string& err, bool ok) 
        : value_(std::move(val)), error_message_(err), is_ok_(ok) {}
};

// Macro for convenient error propagation
#define TRY(result) \
    do { \
        auto res = (result); \
        if (!res.has_value()) { \
            return res; \
        } \
    } while(0)

}  // namespace fleetmanager
```

### 9.2 Memory Management Patterns

```cpp
// ============================================================================
// MEMORY MANAGEMENT
// ============================================================================

namespace fleetmanager {

// Smart pointer usage throughout
using DeviceRegistryPtr   = std::shared_ptr<registry::DeviceRegistry>;
using StateStorePtr       = std::shared_ptr<persistence::StateStore>;
using DriverPtr           = std::shared_ptr<southbound::SouthboundDriver>;
using HostDeviceActorPtr  = std::shared_ptr<actor::HostDeviceActor>;
using ContainerActorPtr   = std::shared_ptr<actor::ContainerActor>;
using NicActorPtr         = std::shared_ptr<actor::NicActor>;

// RAII for resource management
class ResourceGuard {
public:
    explicit ResourceGuard(std::function<void()> cleanup) : cleanup_(cleanup) {}
    ~ResourceGuard() { cleanup_(); }

private:
    std::function<void()> cleanup_;
};

// Lock guards (RAII locking)
std::shared_mutex lock;
std::unique_lock<std::shared_mutex> write_lock(lock);
std::shared_lock<std::shared_mutex> read_lock(lock);

}  // namespace fleetmanager
```

---

## 10. Threading & Synchronization Details

### 10.1 Thread Safety Analysis

```
Global State Access Patterns:
┌─────────────────────────────────────────────────────────┐
│ DeviceRegistry (shared_ptr<RWMutex>)                    │
│ ├─ gRPC threads: Read during RegisterDevice            │
│ └─ Main thread: Read during initialization              │
│ THREAD-SAFE: YES (RWMutex protects)                     │
└─────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────┐
│ K8s Lease (shared_ptr<HACoordinator>)                   │
│ ├─ Lease renewal thread: Update (atomic CAS)           │
│ ├─ gRPC thread: Read IsPrimary()                        │
│ └─ Southbound thread: Conditional on IsPrimary()       │
│ THREAD-SAFE: YES (std::atomic<bool> used)              │
└─────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────┐
│ Per-HostDeviceActor Mailbox (lock-free queue)           │
│ ├─ gRPC threads: Enqueue messages (TryEnqueue)          │
│ ├─ PubSub threads: Enqueue config (TryEnqueue)          │
│ ├─ HDO actor thread: Dequeue (TryDequeue)               │
│ ├─ Per-NicActor mailbox: similar pattern                │
│ THREAD-SAFE: YES (lock-free MPMC queue)                 │
└─────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────┐
│ RocksDB State Store (synchronized writes per key)       │
│ ├─ HDO thread: Write HostDeviceState (async)            │
│ ├─ NO  thread: Write NicGoalState (async)               │
│ ├─ Actor threads: Read current state (sync)             │
│ ├─ Failover thread: Read all state (scan)               │
│ THREAD-SAFE: YES (RocksDB uses internal locking)        │
└─────────────────────────────────────────────────────────┘
```

---

## 11. Observability Implementation Details

### 11.1 OpenTelemetry Integration

```cpp
// ============================================================================
// OBSERVABILITY CONTEXT
// ============================================================================

namespace fleetmanager {
namespace observability {

class ObservabilityContext {
public:
    explicit ObservabilityContext(
        const std::string& service_name,
        const std::string& jaeger_endpoint
    );
    
    // Create span with parent trace context
    std::shared_ptr<opentelemetry::trace::Span> CreateSpan(
        const std::string& span_name,
        const std::string& parent_trace_id = ""
    );
    
    // Create metric recorder
    std::shared_ptr<MetricsCollector> CreateMetricsCollector(const std::string& namespace_name);
    
    // Propagate trace context to outbound calls
    void PropagateTraceContext(
        grpc::ClientContext& client_context,
        const std::string& trace_id
    );

private:
    std::unique_ptr<opentelemetry::sdk::trace::TracerProvider> tracer_provider_;
    std::unique_ptr<opentelemetry::sdk::metrics::MeterProvider> meter_provider_;
};

}  // namespace observability
}  // namespace fleetmanager
```

---

## Conclusion

This LLD provides **ultra-comprehensive** low-level design covering:

✅ **Class hierarchy** with full method signatures  
✅ **Detailed algorithms** (delta compilation, dependency resolution, reconciliation)  
✅ **Threading model** with thread safety analysis  
✅ **State machines** with transitions and invariants  
✅ **Memory management** via smart pointers and RAII  
✅ **Error handling** via Result<T> pattern  
✅ **Observability** via OpenTelemetry spans/metrics  

Ready for implementation in C++17/20.
