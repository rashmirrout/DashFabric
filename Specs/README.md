# DashFabric — Architecture Specification

> **DashFabric** is a cloud-native, horizontally-scalable, fault-tolerant
> **Central Control Unit (CCU)** that programs DASH-compliant
> DPUs, SmartNICs, SmartSwitches, and SmartAppliances across a datacenter from a
> declarative configuration plane.

The CCU translates **intent** published by an upstream control plane into
**concrete device programming** on hundreds of thousands of ENIs spread across
tens of thousands of devices, while remaining live through software upgrades,
host reboots, partial datacenter outages, and continuous scale-out.

---

## 0. Document Index

| # | Document | Purpose |
|---|---|---|
| 0 | [`Thought.md`](./Thought.md) | Original seed thinking (preserved verbatim) |
| 1 | [`01-architecture-overview.md`](./01-architecture-overview.md) | System view, principles, components, end-to-end flow |
| 2 | [`02-domain-model-and-object-lifecycle.md`](./02-domain-model-and-object-lifecycle.md) | HDO / CO / NO actor model, FSMs, hierarchy, destruction |
| 3 | [`03-control-plane-pubsub-and-topics.md`](./03-control-plane-pubsub-and-topics.md) | Topic taxonomy, message schemas, ordering, versioning |
| 4 | [`04-partitioning-and-shard-management.md`](./04-partitioning-and-shard-management.md) | Sharding strategy, partition manager, rebalance, scale-in/out |
| 5 | [`05-high-availability-and-issu.md`](./05-high-availability-and-issu.md) | 3-replica ShardSet, lease, replay-based standby, ISSU, crash recovery |
| 6 | [`06-northbound-registration.md`](./06-northbound-registration.md) | Device registration protocol, identity, attestation, assignment |
| 7 | [`07-hal-and-dash-southbound.md`](./07-hal-and-dash-southbound.md) | HAL design, DASH gNMI mapping, vendor plugin model |
| 8 | [`08-reconciliation-storage-and-warm-restart.md`](./08-reconciliation-storage-and-warm-restart.md) | Three-loop model, WAL, drift correction, warm restart |
| 9 | [`09-observability-and-diagnostics.md`](./09-observability-and-diagnostics.md) | OTel, metrics, logs, debug tooling, dump CLI |
| 10 | [`10-deployment-and-security.md`](./10-deployment-and-security.md) | K8s topology, networking, mTLS, RBAC, multi-tenancy |
| 11 | [`11-failure-modes-and-runbooks.md`](./11-failure-modes-and-runbooks.md) | Failure matrix, recovery procedures, chaos scenarios |
| 12 | [`12-roadmap-and-open-questions.md`](./12-roadmap-and-open-questions.md) | Phasing, TBDs, future work |

---

## 1. Executive Summary

### 1.1 What DashFabric Is
DashFabric is a stateful, sharded, hierarchically organized control-plane
service. Each managed device (DPU / SmartNIC / SmartAppliance / SmartSwitch
DPU) is represented inside DashFabric as a tree of **actor objects**:

```
HostDeviceObject (HDO)              one per registered DPU
└── ContainerObject (CO)            one per VM / workload / tenant slot
    └── NICObject (NO == ENI)       one per ENI / vPort
```

Each object is a **single-writer actor** with:
- A subscription on a deterministic path in the upstream PubSub
  (`/config/hosts/<HOST-ID>/...`)
- An FSM that drives device programming through the **HAL** (Hardware
  Abstraction Layer)
- Periodic **reconcile** to detect and repair drift between intent and the
  device's actual state
- Independent **destruction** propagation when intent is withdrawn at any level
- Full **observability** via OpenTelemetry + Prometheus + structured logs

### 1.2 What Makes It Different
| Concern | Approach |
|---|---|
| **Programming model** | Object-per-DPU-resource actors with deterministic per-object mailbox — no global locks, easy reasoning |
| **Source of truth** | Upstream PubSub. DashFabric is a *cache + reconciler*, never an authoritative store |
| **HA** | Per-shard 3-replica set; **all replicas independently consume the upstream stream** and maintain identical FSM state. Lease-elected primary actuates southbound. Failover ≈ lease TTL (sub-second). No log-shipping between replicas. |
| **Scaling** | Stateless front door + range-sharded stateful workers behind a partition manager. Add a Region for capacity expansion; shards split/merge online. |
| **Re-registration semantics** | Unregister → destroy hierarchy + WAL. Re-register → fresh start, no inherited state (per user requirement). |
| **Southbound** | gNMI per the published [DASH SAI/gNMI schema](https://github.com/sonic-net/SONiC/blob/master/doc/dash/dash-sonic-hld.md). Vendor variations isolated to thin codec plugins. |
| **Observability** | Every config event carries a W3C trace context end-to-end (PubSub → object → HAL → gNMI Set RPC). |

### 1.3 Scale Targets
| Dimension | Target | Source |
|---|---|---|
| Hosts / DCU region | 10,000 DPUs | User requirement |
| Hosts / fleet | 100,000+ DPUs | Multi-region |
| ENIs / DPU | 32 | DASH SONiC HLD §1.4 |
| ENIs / region | 320,000 | Derived |
| Outbound routes / ENI | 100,000 | DASH HLD |
| CA-PA mappings / DPU | 8,000,000 | DASH HLD |
| Active flows / DPU | 32,000,000 | DASH HLD |
| CPS / DPU | 3,000,000 | DASH HLD |
| Config-event ingress / shard | 100,000 events/sec | Design target |
| End-to-end programming p99 | ≤ 500 ms (intent → device ACK) | Design target |
| Reconcile cycle | 60–300 s configurable per object class | Design |
| Failover (planned) | 0 ms (lease handoff before drain) | Design |
| Failover (unplanned) | ≤ 2 s | DASH HA requirement |

### 1.4 Reference Stack
| Layer | Choice | Alternatives |
|---|---|---|
| Language | **Go 1.22+** | Rust + Tokio |
| RPC | **gRPC + Protobuf** | — |
| Config PubSub | **etcd v3** prefix watches | NATS JetStream KV, Kafka with key-compaction |
| Embedded WAL | **BadgerDB** | RocksDB (cgo) |
| Container runtime | **Kubernetes** StatefulSet | Bare-metal + systemd (degraded) |
| Tracing | **OpenTelemetry → Tempo/Jaeger** | — |
| Metrics | **Prometheus + Mimir/Cortex** | VictoriaMetrics |
| Logs | **Loki** (structured JSON) | ELK |
| Southbound | **gNMI 0.10+** | SAI-Thrift (test only) |

### 1.5 Quick End-to-End Flow

```
┌─────────────────┐      register       ┌──────────────────────┐
│  DPU / SmartNIC │ ─────────────────►  │ Northbound Gateway   │  (stateless)
└────────┬────────┘                     └──────────┬───────────┘
         │                                          │ HashRing(HostID)
         │                                          ▼
         │                            ┌─────────────────────────┐
         │                            │   Shard k  (3 replicas) │
         │                            │  ┌────────┐ ┌────────┐  │
         │                            │  │PRIMARY │ │STANDBY │  │
         │                            │  │ FSM    │ │ FSM    │  │
         │                            │  └───┬────┘ └───┬────┘  │
         │                            └──────┼──────────┼───────┘
         │                                   │          │
         │                ┌──────────────────┴──────────┴────────┐
         │                │  etcd cluster (config plane)         │
         │                │  /config/hosts/HOST-1/...            │
         │                └──────────────────────────────────────┘
         │                                   ▲
         │                                   │ writes from upstream
         │                                   │ DC-wide intent producer
         │                                   │ (Azure SDN / OpenStack / etc.)
         ▼ gNMI Set
   Device programmed
```

---

## 2. Reading Order

| You are… | Start with |
|---|---|
| Architect doing review | §1 → 01 → 02 → 05 → 04 |
| Service developer | 02 → 03 → 07 → 08 |
| SRE / on-call | 09 → 10 → 11 |
| Security / compliance | 06 → 10 |
| Product / planning | README → 01 → 12 |

---

## 3. Decisions Log

This section captures **why** the major decisions were made. When you disagree
with one, file a decision change in §`12-roadmap-and-open-questions.md`.

| ID | Decision | Alternatives considered | Rationale |
|---|---|---|---|
| D-001 | Actor-per-object (HDO/CO/NO) | Monolithic per-device FSM | Hierarchical destruction, independent reconcile cadence, localized blast radius, natural mailbox per topic |
| D-002 | Source of truth = upstream PubSub | Local strongly-consistent store | Removes the hardest distributed-systems problem (consensus on writes). DashFabric is a *projection*, not a database. |
| D-003 | etcd v3 as primary PubSub | Kafka, NATS JetStream KV, ZooKeeper | Hierarchical key prefix watches map 1:1 to object hierarchy; leases give us coordination + presence; K8s-native operator pattern |
| D-004 | 3-replica per shard with parallel upstream consumption | Raft, primary→standby log-shipping | Source of truth is upstream → no consensus needed. All replicas converge by consuming same ordered etcd revisions. Failover ≈ lease TTL. |
| D-005 | Range-sharded by `hash(HostID) → shardId` | Consistent hashing only | Range sharding lets the partition manager split/merge contiguous ranges atomically; consistent hash on top for placement of ranges |
| D-006 | gNMI as primary southbound | Custom REST, SAI-RPC | DASH publishes gNMI as the standard SDN channel; matches SONiC reference implementation |
| D-007 | Per-object goroutine + bounded mailbox | Worker pool over global queue | Eliminates head-of-line blocking between hosts; single-writer property makes reasoning + deterministic replay trivial |
| D-008 | Re-registration is *not* idempotent (per user spec) | Sticky identity with state carry-over | User explicit requirement: fresh state on re-register |
| D-009 | OTel trace context propagated from upstream PubSub event → gNMI RPC | Per-layer ad-hoc IDs | Single Trace ID gives operators end-to-end timing in Tempo/Jaeger |
| D-010 | BadgerDB embedded WAL | RocksDB, SQLite | Pure-Go, no cgo, MVCC + WAL semantics sufficient for cache + replay |

---

## 4. Glossary (See also `00` definitions in linked DASH docs)

| Term | Meaning |
|---|---|
| **DCU / DashFabric Control Unit** | The service described by this spec |
| **DASH** | Disaggregated APIs for SONiC Hosts |
| **DPU** | Data Processing Unit (programmable NIC SoC) |
| **HDO** | HostDeviceObject — actor representing one registered DPU |
| **CO** | ContainerObject — actor representing one VM/workload on a DPU |
| **NO** | NICObject — actor representing one ENI/vPort |
| **ENI** | Elastic Network Interface (DASH primary tenant resource) |
| **VNET** | Virtual Network (DASH) |
| **VNI** | VXLAN Network Identifier |
| **NSG** | Network Security Group (ACL group, DASH terminology) |
| **CA / PA** | Customer Address / Provider Address |
| **HAL** | Hardware Abstraction Layer (southbound driver layer) |
| **ShardSet** | Set of 3 replicas owning one shard |
| **Partition Manager** | Cluster-level controller assigning shards to ShardSets |
| **Intent** | Declarative configuration from upstream control plane |
| **GoalState** | Compiled, device-ready form of intent |
| **Drift** | Difference between GoalState and device's actual programmed state |
| **Reconcile** | Periodic loop that detects + corrects drift |
| **ISSU** | In-Service Software Upgrade |
| **gNMI** | gRPC Network Management Interface |

---

## 5. Status

This is a **design specification**. No code yet.

| Section | Status |
|---|---|
| Architecture | ✅ Draft v1 |
| Implementation | ⏳ Not started |
| Operator tooling | ⏳ Not started |
| Test plan | ⏳ Skeleton in `12-roadmap-and-open-questions.md` |
