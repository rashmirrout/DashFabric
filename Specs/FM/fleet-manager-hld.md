# FleetManager: High-Level Design (HLD)

**Version:** 1.0  
**Language:** C++17/20  
**Framework:** gRPC, Protocol Buffers, Boost.ASIO  
**Status:** Design Phase  
**Date:** June 2026

---

> **⚠ Architecture redesign note (2026-06-13):**
> Sections 3.1–3.4 below describe the **original per-HDO cache** model.
> The current target architecture replaces it with the **Registry
> Pattern** (5 shared in-pod registries) and a **three-tier storage
> model** (`fm-data-store`, `fm-cluster-state`, local RocksDB) fed by
> a **vendor-neutral orchestrator plugin**. New normative material is
> in:
>
> - [storage-architecture.md](./storage-architecture.md) — three-tier model & pluggable backends.
> - [orchestrator-plugin-interface.md](./orchestrator-plugin-interface.md) — vendor plugin contract.
> - [registry-pattern-design.md](./registry-pattern-design.md) — registry semantics & per-pod sharing.
> - **§3.5 and §3.6 below** — updated component diagram & responsibilities.
>
> Sections 3.1–3.4 are retained for context; treat them as superseded
> wherever they conflict with §3.5/§3.6 or the linked docs.

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
│  │ Actor Framework (per-device tree, HDO → CO → NO)    │  │
│  │ - HostDeviceActor (HDO) per device                  │  │
│  │     · owns global + group + vnet caches             │  │
│  │     · refcount-based VNET watch                     │  │
│  │ - ContainerActor (CO) per VM/container              │  │
│  │     · thin parent; spawns one NO per NIC            │  │
│  │ - NicActor (NO) per ENI                             │  │
│  │     · composes NicGoalState in-actor                │  │
│  │     · SHA-256 content_hash for idempotency          │  │
│  │ (Async; no cross-actor locks; per-tree isolation)   │  │
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
│  │ Southbound Driver Layer (DASH primary; others fallback)│ │
│  │ ┌────────────┐  ┌────────────┐  ┌────────────┐       │ │
│  │ │ DashSB Drv │  │ SonicSB Drv│  │ LinuxSB Drv│       │ │
│  │ │(gNMI/Proto)│  │(SAI Thrift)│  │(netlink)   │       │ │
│  │ │ all 15 obj │  │  fallback  │  │  fallback  │       │ │
│  │ └────────────┘  └────────────┘  └────────────┘       │ │
│  │ ApplyDeltaPlan(...) — wave-aware, idempotent          │ │
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

### 3.2 Component Responsibilities

| Component | Responsibility | Key Classes |
|-----------|-----------------|------------|
| **REST API Server** | Expose REST endpoints for device registration, CRUD, heartbeat | `RESTServiceImpl`, `DeviceRouter`, `JSONValidator` |
| **gRPC Server** | Expose gRPC RPCs for inter-service communication | `FleetManagerServiceImpl` |
| **Device Registry** | Track device profiles and shard assignments | `DeviceRegistry`, `DeviceProfile` |
| **PubSub Manager** | Establish/maintain topic subscriptions across the four `/config/v1/` trees (global, group, vnet, hosts) | `PubSubSubscriptionManager`, `TopicReceiver` |
| **Actor Framework** | Per-device-tree concurrent state management; HDO owns global+group+vnet caches, CO is a thin parent, NO composes per-ENI `NicGoalState` | `HostDeviceActor`, `ContainerActor`, `NicActor`, `ActorMailbox`, `ActorScheduler` |
| **State Compilation** | Compose `NicGoalState`, hash (SHA-256), diff against last-applied, emit wave-ordered `DeltaPlan` | `StateCompilationEngine`, `NicGoalStateComposer`, `DeltaPlanner` |
| **Reconciliation** | Periodic audits + drift detection by comparing device-reported `content_hash` against composed hash | `ReconciliationEngine`, `DriftDetector` |
| **Southbound Drivers** | Wave-aware programming of all 15 DASH object kinds (DASH primary; SONiC and Linux as fallbacks for non-DPU targets) | `DashSouthboundDriver`, `SonicSouthboundDriver`, `LinuxSouthboundDriver` |
| **State Persistence** | RocksDB cache + checkpointing | `StateStore`, `RocksDBAdapter` |
| **Observability** | Tracing, metrics, logging | `OTelTracer`, `MetricsCollector`, `StructuredLogger` |

---

### 3.3 Object Catalog & Scope Ladder

DASH defines **15 object types** organized in a strict scope hierarchy from
narrowest to broadest: **Fleet → Device → VNET → Group → ENI → Rule**.
Scope determines who owns the object, how widely it is shared, and which
other objects can reference it. FleetManager's caches, actors, and topic
subscriptions are all organized along this ladder. See
[`Specs/Learning-DashNet/03-Object-Model-and-Scopes.md`](../Learning-DashNet/03-Object-Model-and-Scopes.md)
for the full catalog.

| Scope | Lifecycle | DASH objects | Cached in |
|-------|-----------|--------------|-----------|
| **Fleet** | Provider-wide | `RoutingType` (named action catalog: `vnet_direct`, `privatelink`, `service_tunnel`, …) | HostDeviceActor (per-device materialization, drift-checked) |
| **Device** | Per-DPU lifetime | `Appliance`, `HostSpec`, `Tunnel`, `Qos`, `PrefixTag`, `HaSet` | HostDeviceActor (HDO) |
| **VNET** | Per-tenant overlay (materialized only on devices that host an ENI in the VNET) | `Vnet`, `VnetMapping` (manifest + chunks), `PaValidation` | HostDeviceActor (refcount-based watch) |
| **Group** | Reusable rule bundles, shared across ENIs | `RouteGroup`, `AclGroup`, `MeterPolicy`, `OutboundPortMap` | HostDeviceActor (HDO) |
| **ENI** | Per-VM-NIC | `Eni` / `NicSpec`, `ContainerSpec`, `HaScope` | NicActor (NO) |
| **Rule / entry** | Inside a parent group | `RouteEntry`, `AclRule`, `MeterRule`, `MappingEntry`, `PortRange` | inline within parent group |

**The ENI is a binding declaration, not a configuration.** A `NicSpec` is
~30 fields — most of them `*_id` references — and an ENI binds **six**
`AclGroup` slots (3 stages × 2 directions, per family). Without the objects
it points to (`Vnet`, `VnetMapping`, `RouteGroup`, `AclGroup`s,
`MeterPolicy`, `Tunnel`, `Qos`), the ENI is an empty shell and the
NicActor sits in `WAITING_REFS` programming nothing into the silicon.

Three indirection objects deserve special attention:

- **`Tunnel`** (Device scope) — the encap profile (type, src/dst PA, UDP
  port). Acts as **one indirection so thousands of ENIs share one
  tunneling profile**: when the destination PA changes (HA failover, ECMP
  set change, ingress relocation), one `Tunnel` update propagates to every
  binding ENI by id. Without this indirection, a single fabric event
  becomes a per-ENI republish storm.
- **`RoutingType`** (Fleet scope) — catalog of named action templates.
  Routes reference a routing type by id; the type defines the actual
  transform (encap, NAT, mapping lookup). New behaviours (managed
  services, novel tunneling) slot in by publishing a new entry rather
  than changing route schemas. Per-device reconcile checks for drift; no
  per-device overrides.
- **`PrefixTag`** (Device scope) — named list of IP prefixes (e.g.,
  `tag-azure-storage`). Tags are **expanded at compose time, not at
  packet time**: the composer replaces every `tag_ref` in ACL/route rules
  with the concrete prefix list before producing `NicGoalState`. Adding
  a prefix to a popular tag is a fleet-wide republish event for every
  group that binds it.

`NicGoalState` is the in-process composed program for one ENI — fully
denormalized and **never published southbound**. The control plane
publishes intent (refs); the agent produces the program (goal state).

### 3.4 Topic Tree (Four-Tree Subscription Model)

Configuration is **not** a single per-device stream. It is **four parallel
trees** under `/config/v1/`, joined by reference inside the actor. This
matches DASH's scope ladder and lets each tree evolve at its own update
cadence (`Appliance` ≈ never; `VnetMapping` ≈ per VM birth/death). The
tree is authoritatively defined in
[`Specs/FM/vm-eni-provisioning-design.md` §2](./vm-eni-provisioning-design.md#2-topic-hierarchy).

```
/config/v1/
├── global/<device_id>/
│   ├── appliance                    → Appliance
│   ├── routing_type/<name>          → RoutingType (fleet catalog, drift-checked per device)
│   ├── tunnel/<tunnel_id>           → Tunnel
│   ├── qos/<qos_id>                 → Qos
│   ├── prefix_tag/<tag_id>          → PrefixTag
│   └── ha_set/<ha_set_id>           → HaSet
│
├── group/<device_id>/
│   ├── route_group/<group_id>       → RouteGroup spec
│   │   └── routes/                  → repeated Route entries (bulk)
│   ├── acl_group/<group_id>
│   │   └── rules/                   → repeated AclRule
│   ├── meter_policy/<policy_id>
│   │   └── rules/                   → repeated MeterRule
│   └── outbound_port_map/<map_id>
│       └── ranges/                  → repeated PortRange
│
├── vnet/<device_id>/<vnet_id>/
│   ├── spec                         → Vnet
│   ├── pa_validation                → PaValidation
│   └── mapping/
│       ├── _manifest                → VnetMappingManifest (lists chunks + digests)
│       └── <chunk-N>                → VnetMappingChunk (≤1 MiB of repeated VnetMapping)
│
└── hosts/<device_id>/
    ├── spec                         → HostSpec
    └── <container_guid>/
        ├── spec                     → ContainerSpec
        └── <nic_id>/
            └── spec                 → NicSpec  (reference bundle: vnet_id,
                                       route_group_ids, acl_group_ids[6],
                                       meter_policy_ids, ha_scope, primary_ip,
                                       mac, vlan)
```

**Subscription ownership:**

- **HostDeviceActor (HDO)** subscribes `/global/<device_id>/**` and
  `/group/<device_id>/**` once per device. It also owns
  `/vnet/<device_id>/<vnet_id>/**` watches with **refcount semantics**:
  the first NicActor attaching to `vnet_X` triggers HDO subscribe; the
  last detaching triggers unsubscribe.
- **ContainerActor (CO)** discovers and spawns NicActors for each
  `<nic_id>` under its container.
- **NicActor (NO)** subscribes only its own `<nic_id>/spec`. All shared
  config (Vnet, groups, globals) arrives via in-process actor messages
  from the HDO cache — no extra etcd traffic per NIC.

Centralizing the global/group/vnet watches at the HDO drops watch count
from ~10M (10k devices × ~32 NICs × ~30 topics) to ~300k (10k devices ×
~30 topics) — comfortably within etcd v3 limits.

**Wire envelope.** Every etcd value is a `ConfigEntry { metadata:
ConfigMetadata, payload: bytes }` where `ConfigMetadata = { schema_version,
kind, revision, tenant_id, trace_context, payload_digest, issued_at }`.
`payload_digest` is **SHA-256** over the proto-binary payload (used for
integrity, idempotency, and drift detection). `revision` is a **monotonic
counter per object** — bumped on every write, ordering writes to the same
object only; it is **not** a per-device version stamp.

---

### 3.5 Component Architecture (Redesign — Registry Pattern)

The redesign collapses the per-HDO caches into **five shared, in-pod
registries**. NicActors no longer subscribe to VNETs/mappings/groups
directly; they `Acquire` from the registries, which refcount, share,
and own all T1 watches. HDO becomes a thin device-I/O actor.

```
┌────────────────────────────────────────────────────────────────────┐
│                         FM POD PROCESS                             │
│                                                                    │
│  ┌──────────────────────────────────────────────────────────────┐ │
│  │ Plugin loader (vendor-supplied subscription plugin)          │ │
│  └──────────────────────────────┬───────────────────────────────┘ │
│                                 │ events                          │
│  ┌──────────────────────────────▼───────────────────────────────┐ │
│  │ Adapter (leader-elected via T2 lease)                        │ │
│  │  • decode → validate → translate → CAS write to T1           │ │
│  │  • DLQ for malformed; watermark advance after T1 ack         │ │
│  └──────────────────────────────┬───────────────────────────────┘ │
│                                 │                                 │
│                                 ▼  T1 watches                     │
│  ┌──────────────────────────────────────────────────────────────┐ │
│  │ Registries (in-mem, shared across all actors in this pod)    │ │
│  │  • GlobalRegistry     (RoutingType catalog)                  │ │
│  │  • VnetRegistry       (Vnet bodies)                          │ │
│  │  • VnetMappingReg.    (manager + per-VNET assemblers)        │ │
│  │  • GroupRegistry      (RouteGroup, AclGroup)                 │ │
│  │  • HaRegistry         (HaSet/HaScope)                        │ │
│  │  Acquire / Release / Read · refcounted · sub-id = eni_id     │ │
│  └────────────┬─────────────────────────────────┬───────────────┘ │
│               │ Acquire/Release                  │ shared cache    │
│  ┌────────────▼────────────┐         ┌──────────▼──────────────┐ │
│  │ Per-object actors       │         │ Local RocksDB (T3)      │ │
│  │  • HostDeviceActor      │         │  • registry warm cache  │ │
│  │      device-IO only     │         │  • HAL apply log        │ │
│  │  • ContainerActor       │         │  • watch resume cursors │ │
│  │  • NicActor             │         └─────────────────────────┘ │
│  │      compose & program  │                                     │
│  └────────────┬────────────┘                                     │
│               │ gNMI / SAI                                       │
│               ▼                                                  │
│  ┌──────────────────────────────────────────────────────────────┐ │
│  │ Southbound HAL (DASH primary; SONiC/Linux fallback)          │ │
│  └──────────────────────────────────────────────────────────────┘ │
│                                                                    │
└────────────────────────────────────────────────────────────────────┘
                  │                              │
                  ▼                              ▼
        ┌─────────────────┐            ┌─────────────────┐
        │  fm-data-store  │            │ fm-cluster-state│
        │  (T1, central,  │            │ (T2, coord, TTL │
        │   pluggable)    │            │  leases, shards)│
        └─────────────────┘            └─────────────────┘
```

#### What changed vs §3.1

| Concern | §3.1 (original) | §3.5 (redesign) |
|---------|-----------------|-----------------|
| Cache ownership | Per-HDO (per-DPU) caches; redundant across DPUs on same pod | Per-pod registries; one cache per object regardless of subscriber count |
| VNET watch fan-out | Each HDO opens its own VNET watch | One watch per pod per VNET, shared by all subscribed NicActors |
| Mapping assembly | Inside HDO; head-of-line blocks per device | Manager + per-VNET assembler sub-actors; parallel |
| Subscription source | etcd-direct from FM pods | Vendor-neutral plugin → adapter → T1; FM watches T1, never orchestrator |
| Authoritative store | etcd was assumed to be orchestrator's | `fm-data-store` (T1) — FM's own, pluggable backend |
| Restart recovery | Cold-list every prefix | Warm-load T3 RocksDB, then catch-up T1 watch from cursor |
| HDO role | Device IO + caches + composer inputs | Device IO only (gNMI/SAI session, ack/diag, telemetry) |
| Subscriber identity | implicit per-actor | explicit `eni_id` (Decision #13 format) |

#### Registry contract (recap)

Every registry implements:

```
Acquire(key, sub_id) -> Subscription{Initial, Updates, Ready, cancel}
Release(key, sub_id)                         // refcount--, debounced reap
Read(key) -> (V, ok)                          // peek without subscribing
```

Detailed semantics, state machine, and Go sketches: see
[registry-pattern-design.md](./registry-pattern-design.md).

#### Slimmed component responsibility table

| Component | Responsibility (redesign) |
|-----------|---------------------------|
| **Adapter** | Decode plugin events; validate; translate to FM domain; CAS-write to T1; advance watermark in T2; DLQ malformed events. Leader-elected via T2 lease. |
| **GlobalRegistry** | Hold fleet-wide singletons (RoutingType). Pod-scope subscriber. |
| **VnetRegistry** | Hold `Vnet` bodies. Refcounted by `eni_id`. One T1 watch per active VNET. |
| **VnetMappingRegistry** | Manager actor routes to per-VNET `MappingAssembler` sub-actors. Each assembler runs the manifest+chunks state machine for its VNET only. |
| **GroupRegistry** | Hold `RouteGroup` + `AclGroup`. Refcounted by `eni_id`. |
| **HaRegistry** | Hold `HaSet`/`HaScope`. Emits `FAILOVER` updates. |
| **HostDeviceActor (slim)** | Owns gNMI/SAI session for one DPU. Routes ack/diag back to NicActors. Rolls up device telemetry. **Holds no domain caches.** Subscribes GlobalRegistry (always) and HaRegistry (when HA participant). |
| **ContainerActor** | VM/container lifecycle; spawns one NicActor per NIC. |
| **NicActor** | `Acquire` Vnet/Mapping/Group entries; compose `NicGoalState`; SHA-256 hash; write hash + sketch to T1; append to HAL apply log in T3; program DPU via HDO. |
| **Local RocksDB (T3)** | Per-pod warm cache + HAL apply log + watch resume cursors. RocksDB default; pluggable to BadgerDB/LMDB. |
| **`fm-data-store` (T1)** | Authoritative central store of FM domain. etcd default; pluggable to SQLite/embedded etcd/TiKV/Postgres. |
| **`fm-cluster-state` (T2)** | Pod membership, leader leases, shard assignments, watermarks. |

### 3.6 Three-Tier Storage Architecture (Redesign)

| Tier | Name | Default | Pluggable | Hot? |
|------|------|---------|-----------|------|
| T1 | `fm-data-store` | etcd | SQLite, embedded etcd, etcd cluster, TiKV, FoundationDB, Postgres | cold path (10s of ms — seconds) |
| T2 | `fm-cluster-state` | etcd (sharable with T1) | same options as T1 | warm (ms) |
| T3 | Local RocksDB | RocksDB | BadgerDB, LMDB | warm (ms) |
| In-mem | Registry caches | — | — | hot (µs) |

The same FM binary runs in all customer tiers (ultra-small docker-compose
through large K8s+TiKV); only the storage backend config differs.
Sizing, knobs, and pluggability matrix: see
[storage-architecture.md](./storage-architecture.md).

The orchestrator's storage is **outside** these tiers — FM does not
talk to it directly. Data enters via the vendor-supplied plugin
described in
[orchestrator-plugin-interface.md](./orchestrator-plugin-interface.md).

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
    ├─ Spawn HostDeviceActor (HDO) for device_id
    ├─ Create DeviceRegistry entry
    ├─ HDO subscribes the four /config/v1/ trees:
    │     /config/v1/global/{device_id}/**
    │     /config/v1/group/{device_id}/**
    │     /config/v1/hosts/{device_id}/**
    │   (and /config/v1/vnet/{device_id}/<vnet_id>/** on first NIC attach)
    ├─ Persist to RocksDB
    ├─ Update metrics: devices_total++
    │
    ▼
Response to Device
    └─ RegisterDeviceResponse(shard_id, subscription_topics)
```

### 4.2 Configuration Update Flow (Four-Phase Provisioning)

The provisioning of one VM NIC into a fully programmed ENI follows **four
phases**. The phases match the actor subscription cascade in §3.4 and the
end-to-end sequence in
[`Specs/Learning-DashNet/11-Scenario-VM-NIC-Provisioning.md`](../Learning-DashNet/11-Scenario-VM-NIC-Provisioning.md).
DASH is **eventually consistent** — refs may publish in any order; ordering
is an optimization, not a correctness requirement.

#### Phase A — Ambient state hydration (HDO bootstrap)

```
HostDeviceActor (HDO) starts on device registration
    ├─ Subscribe /config/v1/global/<device_id>/**
    │   (Appliance, RoutingType, Tunnel, Qos, PrefixTag, HaSet)
    ├─ Subscribe /config/v1/group/<device_id>/**
    │   (RouteGroup, AclGroup, MeterPolicy, OutboundPortMap)
    ├─ Wait for etcd WatchResponse.created + initial snapshot drain
    ├─ State: WAITING_BOOTSTRAP
    │   └─ All NicActors block here — no NIC programs against a partial
    │      reference set
    ├─ Hydration complete → cache populated, refcounts initialized
    ▼
HDO transitions to READY; programs Wave 0–2 idempotently
   (Appliance, RoutingType[*], Qos[*], PrefixTag[*],
    Tunnel[*], HaSet[*],
    RouteGroup[*], AclGroup[*], MeterPolicy[*], OutboundPortMap[*])
```

#### Phase B — ENI publish (NicActor subscribes; refs may still be missing)

```
Upstream control plane writes to etcd directly:
    /config/v1/hosts/<device_id>/<container_guid>/spec        → ContainerSpec
    /config/v1/hosts/<device_id>/<container_guid>/<nic_id>/spec → NicSpec
    │   (Trace ID propagated in ConfigMetadata.trace_context)
    ▼
ContainerActor (CO) discovers NIC; spawns NicActor (NO)
    ▼
NicActor (NO):
    ├─ Reads vnet_id from NicSpec → asks HDO to ensure subscription
    │  on /config/v1/vnet/<device_id>/<vnet_id>/** (refcount++)
    ├─ Reads route_group_ids, acl_group_ids[6], meter_policy_ids —
    │  pulled from HDO cache via in-process actor message (no extra
    │  etcd traffic per NIC)
    ├─ State: WAITING_REFS  (auto-resolves when each missing object arrives)
    └─ State: INCOMPLETE_MAPPING  (auto-resolves when VnetMappingManifest fills)
```

#### Phase C — Compose + program

```
NicActor (all refs resolved):
    ├─ Compose NicGoalState in-actor by joining NicSpec + Vnet
    │  + groups + globals
    │   - Expand every prefix_tag_ref into concrete prefixes (compose-time)
    │   - Merge per-ENI route_rules[] with RouteGroup entries into one
    │     unified LPM table
    │   - Materialize derived EniRoute = { eni_id, route_group_id, family }
    │   - NicGoalState is composed in-process; NEVER published southbound
    ├─ Compute content_hash = SHA-256(canonical_serialization(NicGoalState))
    ├─ If content_hash == last_applied_hash → no-op
    ├─ Else diff vs last-composed → DeltaPlan with wave_offsets (0..6)
    │
    ├─ Save checkpoint to RocksDB
    ▼
DashSouthboundDriver.ApplyDeltaPlan(plan):
    Wave 0: Appliance, RoutingType, Qos, PrefixTag       (idempotent globals)
    Wave 1: Tunnel, HaSet                                (transports & HA)
    Wave 2: RouteGroup→Route, AclGroup→AclRule,          (shared groups)
            MeterPolicy→MeterRule, OutboundPortMap→Range
    Wave 3: Vnet, PaValidation                           (depend on Tunnel)
    Wave 4: VnetMappingManifest, VnetMappingChunk[*]     (chunked, batched)
    Wave 5: Eni, HaScope                                 (depend on Vnet, groups, HaSet)
    Wave 6: EniRoute, EniAclBinding, EniRouteRule        (per-ENI bindings)
    │
    │  DELETE order = strict reverse of CREATE/UPDATE
    ▼
Data plane device programs SAI / silicon tables
    └─ Returns ACK; trace event emitted
```

The driver issues only the **delta**: even though `NicGoalState` may be
80 KiB, a typical change (one ACL rule edit) produces a single Wave-6 SAI
call. The same `content_hash` is what `Reconcile` compares against the
device-reported hash to detect drift.

#### Phase D — READY + drift reconciliation

```
NicActor publishes status: READY at applied revision
    ├─ All 3 ShardSet pods compose identically; only the K8s-Lease
    │  holder (Primary) actuates. Standby pods retain the composed
    │  plan in cache for instant failover.
    ├─ ReconciliationEngine periodically re-reads keys from scratch
    │  and compares device-reported content_hash against composed hash
    └─ Any ref revision bump → NicActor recomposes → new DeltaPlan
       → repeat from Phase C
```

#### Failure modes

| Failure | Detected at | Recovery |
|---------|-------------|----------|
| Ref missing in HDO cache | NO compose | `WAITING_REFS`; auto-resolves on publish |
| `VnetMappingManifest` incomplete | NO compose | `INCOMPLETE_MAPPING`; auto-resolves on chunk fill |
| `Appliance.capabilities` exceeded | NO compose | `OVER_CAPACITY` (hard rejection); orchestrator must reshape rules |
| HAL `Apply` rejects | DashSouthboundDriver | `PROGRAMMING_FAIL`; rollback to last-good `content_hash` |
| Validation failure (proto parse, ref-integrity) | Subscriber side | `WAITING_VALID`; emit `FleetEvent{kind=VALIDATION_REJECTED}`; sibling `/status/v1/<original_path>/_error` (rejected_revision, error_code, ts; TTL 1h) |

---

## 5. REST API Endpoints

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
#   "subscription_topics": [
#     "/config/v1/global/host-12345/**",
#     "/config/v1/group/host-12345/**",
#     "/config/v1/hosts/host-12345/**"
#   ],
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

## 6. Threading & Concurrency Model

### 6.1 Thread Pool Architecture

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
│  ├─ Executes HostDeviceActor (HDO), ContainerActor (CO),
│  │  and NicActor (NO) tasks
│  ├─ Runs StateCompilationEngine (NicGoalState composition + diff)
│  ├─ Runs Reconciliation Engine
│  └─ No cross-actor locks (per-tree isolation)
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

### 6.2 Synchronization Primitives

```
Global State (K8s Lease, DeviceRegistry)
    ├─ RWMutex: device_registry_lock (R/W by gRPC server)
    └─ Atomic: am_i_primary (CAS for PRIMARY/STANDBY switch)

Per-Device Tree State (RocksDB key-space)
    ├─ No cross-tree (HDO/CO/NO) locks
    ├─ Actor-local mailbox (lock-free queue) for HDO, CO, NO
    └─ RocksDB serialization (single writer per key via RocksDB internals)

Southbound Driver Queues
    ├─ Lock-free queue per driver type (DASH/SONiC/Linux)
    └─ Backpressure: queue size monitored, propagated to actor
```

---

## 7. State Management

### 7.1 Object State Machines

State is tracked per-actor and reflects DASH's eventual-consistency model.
The NicActor's lifecycle is the core of provisioning; see
[`Specs/Learning-DashNet/05-ENI-Deep-Dive.md`](../Learning-DashNet/05-ENI-Deep-Dive.md).

**HostDeviceActor (HDO) — owns global/group/vnet caches + VNET refcounts:**
```
INITIALIZING → WAITING_BOOTSTRAP → READY ⇄ RECONFIGURING → DRAINING → TERMINATED
```
HDO blocks all child NicActor programming until `WAITING_BOOTSTRAP` clears
(the initial snapshot of `/global` + `/group` has drained from etcd).

**ContainerActor (CO) — thin parent to NicActors in a container:**
```
INITIALIZING → READY ⇄ RECONFIGURING → DESTROYING → TERMINATED
```

**NicActor (NO) — composes NicGoalState per ENI:**
```
[*] → WAITING_SPEC → WAITING_REFS → COMPOSING → PROGRAMMING → READY
                          ↑                                       │
                          └── (any input revision bump) ───────────┘
                                                                  │
                                                                  ▼
                                                              DRAINING → [*]
```
Auxiliary states:
- `INCOMPLETE_MAPPING` — `VnetMappingManifest` not fully populated.
- `OVER_CAPACITY` — appliance capability limits exceeded; hard rejection.
- `WAITING_VALID` — quarantined after subscriber-side validation failure.
- `PROGRAMMING_FAIL` — HAL `Apply` rejected; rolled back to last-good
  `content_hash`.

**Admin states on the ENI itself** (orthogonal to the NicActor lifecycle):
`ENABLED` (pipeline forwards), `DISABLED` (pipeline drops all packets),
`DRAINING` (existing flows complete; new flows dropped).

### 7.2 Cached vs. Persistent State

**In-Memory Cache (per-process, fanned out via actor messages):**
- HDO global / group / vnet caches (one concurrent map per object kind)
- Current actor state machines (HDO, CO, NO)
- NicActor's last-composed `NicGoalState` and `last_applied_hash`
- Outstanding delta commands
- Subscription channel references

**Persistent (RocksDB):**
- Device profiles + capabilities
- Composed `NicGoalState` per ENI + applied `content_hash`
- Per-object `last_applied_revision` (for etcd CAS resume)
- Reconciliation checkpoints
- Audit logs (append-only)

**Recovery on Failover:**
1. New PRIMARY pod mounts same PVC
2. Loads RocksDB into memory cache
3. Recomputes in-flight deltas from checkpoint
4. Resumes as if primary always knew state (no hydration delay)

---

## 8. HA & Failover

DASH defines **two independent HA axes**. Conflating them is a frequent
source of design errors; treat them separately.

| Axis | What fails over | Mechanism | Lives in |
|------|-----------------|-----------|----------|
| **FM-pod HA** | A FleetManager pod | K8s `Lease` (PRIMARY ⇄ STANDBY) | DashFabric control plane |
| **Data-plane DPU HA** | A DPU appliance | `HaSet` (Device scope) + `HaScope` (per-ENI) | DASH southbound objects |

Per [`vm-eni-provisioning-design.md` decision #9](./vm-eni-provisioning-design.md#locked-decisions-rounds-1-3),
`HaSet` (appliance-pair, Device scope) lives in the HostDeviceActor cache
and is referenced by `HaScope` (per-ENI), which lives in the NicActor.
`NicGoalState` references `ha_scope` by id; the DPU agent dereferences at
program time.

Standard DASH HA is **active/standby** — one member is `PRIMARY` and
forwards; the other is `STANDBY` and stays warm via the sync channel —
**not** active/active. Do not size capacity assuming 2× throughput.
`preempt: true` is **not** the default (it would ping-pong on flaky
primaries). See
[`Specs/Learning-DashNet/16-Common-Misconceptions.md`](../Learning-DashNet/16-Common-Misconceptions.md) #9.

### 8.1 FM-Pod HA — K8s Lease (PRIMARY/STANDBY Coordination)

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

### 8.2 ISSU (In-Service Software Upgrade)

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

### 8.3 Data-Plane DPU HA — `HaSet` / `HaScope`

```
HaSet  (Device scope, in HostDeviceActor cache)
├─ Pair of appliances: { primary_dpu_id, standby_dpu_id, sync_channel }
├─ Refcount-tracked: cached on every device that hosts an ENI in the set
└─ Updated on appliance-level events (PA change, role change)

HaScope (per-ENI, composed into NicGoalState by the NicActor)
├─ References HaSet by id
├─ Carries the local ENI's role (PRIMARY | STANDBY)
├─ Standby NicActors program identical pipeline state, kept warm
│  via the sync channel
└─ On primary failure: HaSet update flips role; both NicActors
   recompose and reprogram; preempt=false by default
```

`HaSet` and `HaScope` are independent of the K8s Lease used for FM-pod
HA in §7.1. A FleetManager-pod failover does not affect data-plane
forwarding; a DPU failover does not affect FleetManager state. The two
mechanisms are designed to be orthogonal.

---

## 9. Scalability via Sharding

### 9.1 Consistent Hashing

```cpp
uint32_t GetShardIndex(const std::string& device_id, uint32_t shard_count) {
    uint32_t hash = crc32(device_id);
    return hash % shard_count;
}
```

### 9.2 Dynamic Scale-Out

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

## 10. Southbound HAL Design

### 10.1 Driver Taxonomy

DASH (gNMI/protobuf) is the **primary southbound** for DPU appliances and
is where every new DashFabric capability is implemented first. SONiC/SAI
and Linux/netlink drivers exist as **fallbacks for non-DPU device types**
(legacy switching ASICs, host-stack-only servers) — they do not implement
the full DASH object catalog and are out of scope for ENI provisioning.

| Driver | Target | Encoding | Connectivity | Object coverage |
|--------|--------|----------|--------------|-----------------|
| **`DashSouthboundDriver`** (primary) | DPU appliances | Protobuf over gNMI | gRPC channel (persistent) | All 15 DASH objects + per-ENI bindings |
| **`SonicSouthboundDriver`** (fallback) | SONiC switches, non-DPU | Thrift binary | TCP connection pool | Subset (no per-ENI bindings) |
| **`LinuxSouthboundDriver`** (fallback) | Linux hosts | netlink | Local socket / remote rtnl | Host-stack only |

### 10.2 Driver Interface

The driver is the **wave-aware execution layer** for a `DeltaPlan`. It is
not framed around ENI-only verbs; it is framed around the **full DASH
object catalog**, with `CREATE` / `UPDATE` / `DELETE` flavors for each
kind. The dispatcher hands the driver a `DeltaPlan` whose
`wave_offsets` correspond to the programming order from Phase C (§4.2):
the driver executes objects in parallel within a wave, sequentially
across waves, and in **strict reverse-wave order** for deletes.

```cpp
class SouthboundDriver {
public:
    enum class Op { CREATE, UPDATE, DELETE };

    // Single entry point — the dispatcher hands a wave-ordered DeltaPlan
    // composed from the diff of NicGoalState. NicGoalState itself is
    // never sent over the wire; only the typed object verbs below are.
    virtual Status ApplyDeltaPlan(const DeltaPlan& plan) = 0;

    // Wave 0 — idempotent globals
    virtual Status ApplyAppliance   (Op, const Appliance&    o) = 0;
    virtual Status ApplyRoutingType (Op, const RoutingType&  o) = 0;
    virtual Status ApplyQos         (Op, const Qos&          o) = 0;
    virtual Status ApplyPrefixTag   (Op, const PrefixTag&    o) = 0;

    // Wave 1 — transports & HA (depend on Appliance)
    virtual Status ApplyTunnel      (Op, const Tunnel&       o) = 0;
    virtual Status ApplyHaSet       (Op, const HaSet&        o) = 0;

    // Wave 2 — shared groups
    virtual Status ApplyRouteGroup       (Op, const RouteGroup&       o) = 0;
    virtual Status ApplyAclGroup         (Op, const AclGroup&         o) = 0;
    virtual Status ApplyMeterPolicy      (Op, const MeterPolicy&      o) = 0;
    virtual Status ApplyOutboundPortMap  (Op, const OutboundPortMap&  o) = 0;

    // Wave 3 — VNETs (depend on Tunnel)
    virtual Status ApplyVnet         (Op, const Vnet&         o) = 0;
    virtual Status ApplyPaValidation (Op, const PaValidation& o) = 0;

    // Wave 4 — VnetMapping (manifest + chunks; large, batched)
    virtual Status ApplyVnetMappingManifest(Op, const VnetMappingManifest& o) = 0;
    virtual Status ApplyVnetMappingChunk   (Op, const VnetMappingChunk&    o) = 0;

    // Wave 5 — ENI body (depends on Vnet, groups, HaSet)
    virtual Status ApplyEni     (Op, const Eni&     o) = 0;
    virtual Status ApplyHaScope (Op, const HaScope& o) = 0;

    // Wave 6 — per-ENI bindings (FleetManager-derived from NicGoalState)
    virtual Status ApplyEniRoute      (Op, const EniRoute&      o) = 0;
    virtual Status ApplyEniAclBinding (Op, const EniAclBinding& o) = 0;
    virtual Status ApplyEniRouteRule  (Op, const EniRouteRule&  o) = 0;
};

class DashSouthboundDriver : public SouthboundDriver {
    // gNMI-based implementation for DASH-capable DPUs (primary).
    // Implements every Apply* verb above against the SAI-DASH schema.
};

class SonicSouthboundDriver : public SouthboundDriver {
    // SAI-Thrift fallback for non-DPU SONiC targets.
    // Only the subset of waves relevant to non-DPU forwarding is populated.
};

class LinuxSouthboundDriver : public SouthboundDriver {
    // netlink fallback for plain Linux hosts (host-stack only).
};
```

Implementations are **idempotent**: replaying with an unchanged
`content_hash` is a no-op, which is what makes drift reconcile cheap.

### 10.3 Indirection objects deserve the spotlight

Three Wave 0–1 objects carry disproportionate weight:

- **`Tunnel`** — one update covers thousands of binding ENIs. The driver
  applies `Tunnel` changes before any ENI dependency, but a `Tunnel`-only
  delta does **not** cascade through ENI verbs — the silicon dereferences
  by id at runtime (DASH-level indirection). This avoids per-ENI republish
  storms on PA changes (HA failover, ECMP set change, ingress relocation).
- **`RoutingType`** — fleet-scope catalog. Per-device reconcile compares
  the cached set against `/config/v1/global/<device_id>/routing_type/**`
  and surfaces drift via `FleetEvent{kind=DRIFT_DETECTED}`. New action
  templates (managed services, novel tunneling) ship by adding entries;
  routes reference the type by id rather than carrying inlined behavior.
- **`PrefixTag`** — never resolved at packet time. The composer expands
  every `tag_ref` in ACL/route rules into concrete prefixes before
  `NicGoalState` is hashed. Adding a prefix to `tag-azure-storage`
  republishes every binding `AclGroup` / `RouteGroup` to every DPU that
  caches them.

`NicGoalState` is composed in-process by the NicActor and is **never
sent southbound**. The driver receives the diff of the goal state
expressed as the typed object verbs above. See
[`Specs/Learning-DashNet/16-Common-Misconceptions.md`](../Learning-DashNet/16-Common-Misconceptions.md)
items #1, #6, #13, #16 for the full reasoning.

---

## 11. Observability Integration

### 11.1 OpenTelemetry Tracing

**Trace Context Propagation:**
- Upstream intent → W3C Trace Context header
- FleetManager extracts trace_id, creates child span
- Southbound RPC includes trace context
- End-to-end visibility in Jaeger/Tempo

### 11.2 Metrics Collection

**Key Metrics:**
- `fleetmanager_devices_total{shard_id}` — device count
- `fleetmanager_delta_compilation_duration_ms` — compilation latency
- `fleetmanager_southbound_rpc_latency_ms{target_type}` — RPC latency
- `fleetmanager_reconciliation_drift_detected_total` — drift events
- `fleetmanager_primary_failovers_total{shard_id}` — failover count

### 11.3 Structured Logging

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

## 12. Deployment Model

### 12.1 Kubernetes StatefulSet

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

## 13. Performance Targets

| Operation | Target Latency | SLO |
|-----------|----------------|-----|
| Device Registration | <100ms | p99 <150ms |
| Delta Compilation | <50ms per device | p99 <100ms |
| Southbound ENI Creation | <50ms | p99 <100ms |
| Reconciliation (5000 devices) | <60s | p99 <120s |
| Failover (STANDBY takes over) | ~30-35s | RTO per K8s lease TTL |

---

## 14. Security Considerations

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
