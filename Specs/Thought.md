
---

# Project Specification: Cloud-Native DC Virtual Network Configuration Engine

**Objective:** Build a highly available, ultra-low latency, horizontally scalable distributed network configuration manager. This control plane handles registration, ingests global intent profiles from an upstream data store, tracks network states, and programs virtual networking rules (such as routing tables, ACLs, and VXLAN tunnels) onto Data Center hosts, DPU-based servers, and DPU DASH appliances.

---

## Part 1: Application Design, Modules, and Domain Model

### 1.1 Core Domain Models

The application domain must model heterogeneous compute and network offload fabrics. The data models must utilize zero-copy structures optimized for rapid state transformations.

```
                    ┌──────────────────────────────┐
                    │      Device Profile          │
                    ├──────────────────────────────┤
                    │ - ID (UUID)                  │
                    │ - Type (Host / DPU / DASH)   │
                    │ - SKU Capabilities Mapping   │
                    │ - Hardware Resource Matrix   │
                    └──────────────┬───────────────┘
                                   │
                                   ▼
┌─────────────────────────┐  Compiles Into  ┌─────────────────────────┐
│     Upstream Intent     │ ──────────────► │    Compiled Goal State  │
├─────────────────────────┤                 ├─────────────────────────┤
│ - Virtual Network IDs   │                 │ - Exact Pipeline Rules  │
│ - Subnet / Route Tables │                 │ - Generated Tunnel Keys │
│ - Security Group Rules  │                 │ - State Checksum Hash   │
└─────────────────────────┘                 └─────────────────────────┘

```

* **`DeviceProfile`**: Captured during registration. Models individual device personalities (e.g., standard Hypervisor Host, DPU-embedded Host, or a high-performance standalone DASH Appliance). Includes physical features: MAC, management IP, CPU core count, and hardware capabilities (e.g., Max Flow Table Size, NVMe-oF offload, P4-pipeline layout).
* **`NetworkIntent`**: The abstract configuration state consumed from the upstream pub/sub database (e.g., "Virtual Network X should route to Subnet Y").
* **`DeviceGoalState`**: The compiled, concrete, target byte configuration that the physical device understands natively (e.g., exact P4 match-action table entries, Linux `tc` rules, or hardware API configurations).

### 1.2 Application Core Modules

```
 ┌────────────────────────────────────────────────────────────────────────┐
 │                      APPLICATION CORE PROCESSING ENGINE                │
 │                                                                        │
 │  ┌──────────────────────┐   Intent   ┌──────────────────────────────┐  │
 │  │ Upstream Sync Engine │ ─────────► │ State Compilation & Delta G. │  │
 │  └──────────────────────┘            └──────────────┬───────────────┘  │
 │             ▲                                       │                  │
 │             │ Register Event                        ▼ Calculated Delta │
 │  ┌──────────┴───────────┐            ┌──────────────────────────────┐  │
 │  │   Inventory & Topo   │            │   Southbound Driver Layer    │  │
 │  └──────────────────────┘            └──────────────┬───────────────┘  │
 └─────────────────────────────────────────────────────┼──────────────────┘
                                                       ▼ gRPC / gNMI / P4Runtime
                                                [ Physical DPUs & Hosts ]

```

* **`Inventory & Topology Engine`**: Exposes the Northbound registration API endpoint. Validates incoming device capabilities against strict hardware compatibility matrices.
* **`Upstream Sync Engine`**: Dynamically establishes thread-safe, targeted topic subscriptions to the upstream configuration store based on active device lists. It isolates network intent streaming on a per-device basis.
* **`State Compilation & Delta Graph Engine`**: The core execution engine. Uses an async actor-per-device configuration. It compares the newly received `NetworkIntent` with the historical state in local cache storage, builds a dependency graph, and produces an atomic array of delta commands (`Create`, `Update`, `Delete`).
* **`Southbound Driver Layer`**: Translates abstract delta structures into physical programming actions using gRPC, gNMI, or P4Runtime. Manages persistent network multiplexing pipelines to individual DPUs and Hosts.

### 1.3 Module Interaction Model

1. **Discovery Loop:** Device issues registration to `Inventory Engine`.
2. **Subscription Escalation:** `Inventory Engine` triggers `Upstream Sync Engine` to subscribe to `intent/device_id`.
3. **Reactive Processing Loop:** `Upstream Sync Engine` catches intent updates $\rightarrow$ feeds payload to the designated device task in the `Delta Graph Engine`.
4. **Reconciliation Loop:** A periodic tick evaluates the current live status of the physical hardware against local storage, correcting configuration drift without re-subscribing to upstream paths.

---

## Part 2: Cloud-Native & Kubernetes Infrastructure Design

### 2.1 Kubernetes Workload Topology

To achieve high throughput alongside predictable state ownership, workloads are partitioned into two tiers:

```
                  [ Ingress Control Commands / Pings ]
                                   │
                                   ▼
                     ┌───────────────────────────┐
                     │    API Router / Gateway   │ (K8s Deployment - Stateless)
                     └─────────────┬─────────────┘
                                   │ Consistent Hashing Ring
                                   ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                    WORKER STATE SHARD POOL (StatefulSet)                │
│                                                                         │
│  ┌──────────────────────────────────┐  ┌─────────────────────────────┐  │
│  │             SHARD 0              │  │           SHARD 1           │  │
│  │ ┌──────────────┐  ┌────────────┐ │  │ ┌────────────┐  ┌─────────┐ │  │
│  │ │   worker-0   │  │  worker-1  │ │  │ │  worker-2  │  │ worker-3│ │  │
│  │ │  (PRIMARY)   │  │ (STANDBY)  │ │  │ │ (PRIMARY)  │  │(STANDBY)│ │  │
│  │ └──────┬───────┘  └─────┬──────┘ │  │ └─────┬──────┘  └────┬────┘ │  │
│  └────────┼────────────────┼────────┘  └───────┼──────────────┼──────┘  │
└───────────┼────────────────┼───────────────────┼──────────────┼─────────┘
            ▼                ▼                   ▼              ▼
     [Attached PVC]   [Attached PVC]      [Attached PVC] [Attached PVC]

```

#### API Router / Gateway (Stateless Deployment)

Exposes an HTTP/3 or gRPC endpoint for device registrations and heartbeats. Applies an in-memory consistent hashing algorithm over incoming device identity tags to pick the correct stateful processing worker pair.

#### Shard Workers (StatefulSet Workload)

Deployed as an explicit paired architecture (`replicas = 2N`).

* Pairs are identified via index groupings: `worker-0` and `worker-1` run Shard 0; `worker-2` and `worker-3` run Shard 1.
* Each single pod provisions a dedicated Persistent Volume Claim (PVC) backed by local NVMe-based cloud storage to manage an embedded database instance (RocksDB).

---

### 2.2 Dynamic Scaling & Real-time Partitioning

* **Dynamic Registrations:** Devices register without prior cluster-side provisioning. When the API Router maps a device name to Shard 0, both `worker-0` and `worker-1` receive the registration profile via local gRPC multiplexing, dynamically establishing separate consumer listening paths to the upstream database.
* **Scale-Out Operations:** Scaling is handled by incrementing the StatefulSet replica parameter by groups of 2. When the partition count modifies, the API Router updates its consistent hashing ring configuration. Existing device paths remain locked onto their current shards, while newly added host registrations route into the newly created shard space.

---

### 2.3 High Availability & Fault Tolerance Engine

* **Active-Passive Coordination via K8s Leases:** Workers contend for an exclusive coordination lock (`coordination.k8s.io/v1`) using their Shard ID string format. The node holding the lock handles outbound device data programming. The alternate node consumes telemetry inputs and updates its storage cache concurrently, operating as a hot standby.
* **Zero-Blackout Failover Matrix:** If a Primary pod fails:
1. The K8s Lease times out.
2. The Hot Standby node captures the lease lock and changes its internal operational flag to Primary (`am_i_primary = true`).
3. Because the Standby node has been tracking real-time configurations dynamically, it assumes control immediately with no data hydration delay.



---

### 2.4 In-Service Software Upgrade (ISSU) Strategy

Upgrading network core infrastructure requires maintaining state continuity without dropping active management connections to the data center fabric.

```
[Old Primary Pod Version A] ──► Flushes State to RocksDB ──► Signals Safe Handoff
                                                                  │
                                                                  ▼
[New Standby Pod Version B] ◄── Mounts Same PVC Disk ◄───── K8s Container Upgrade

```

1. **Rolling Update Orchestration:** The StatefulSet is updated using an `OnDelete` or partitioned `RollingUpdate` strategy, targeting one node per shard group sequentially.
2. **Warm State Handoff Process:** Before a worker pod enters termination during an upgrade, it handles a graceful exit sequence:
* It traps the incoming `SIGTERM` signal.
* It executes an immediate flush of all dirty volatile RAM structures to its local RocksDB store.
* It relinquishes its K8s lease proactively, allowing its peer node to handle device programming duties seamlessly.


3. **State Restoration On Boot:** The newly upgraded container image mounts the identical underlying PVC block storage. On startup, the application populates its in-memory state engine directly from the local disk cache, registers missing deltas via peer sync protocols, and assumes its position as a hot standby node.

---

### 2.5 Observability, Telemetry, and Debugging Suite

To debug network events across thousands of DPU appliances, deep tracing must be embedded directly inside the execution loop:

```
                     ┌─────────────────────────────┐
                     │      OpenTelemetry SDK      │
                     └──────────────┬──────────────┘
                                    │
         ┌──────────────────────────┼──────────────────────────┐
         ▼                          ▼                          ▼
┌──────────────────┐       ┌──────────────────┐       ┌──────────────────┐
│ Runtime Metrics  │       │ Context Tracing  │       │ eBPF Log Probes  │
│ (Prometheus/Push)│       │  (Jaeger/Tempo)  │       │  (System Health) │
└──────────────────┘       └──────────────────┘       └──────────────────┘

```

* **Context Tracing via OpenTelemetry:** Every inbound configuration update receives a W3C Trace Context header wrapper. This Trace ID must pass from the upstream database, through the compilation loops, and down to the final southbound RPC programming call to enable clear end-to-end performance debugging in Jaeger or Grafana Tempo.
* **Deep Runtime Telemetry:** The application exports structured metrics via a Prometheus scratch endpoint (`:8081/metrics`), monitoring parameters such as:
* Delta calculation duration ($t_{\text{compilation}}$).
* Southbound RPC error rates and transit times.
* Active device state machines currently tracked per shard container.


* **Automated eBPF Diagnostic Probes:** Worker pods run alongside sidecar tools that use eBPF scripts to capture raw TCP socket latency and connection anomalies between the control plane cluster and physical DPU hardware interfaces without adding overhead to the runtime processing loop.

---
