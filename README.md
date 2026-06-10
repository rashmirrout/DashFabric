# DashFabric

**Cloud-Native DC Virtual Network Configuration & Management Engine**

DashFabric is a horizontally-scalable, fault-tolerant **Central Control Unit
(CCU)** that translates declarative network intent into device-level
programming on [DASH](https://github.com/sonic-net/DASH)-compliant DPUs,
SmartNICs, SmartSwitches, and SmartAppliances across a datacenter.

## Architecture Specification

The full design lives in [`Specs/`](./Specs/README.md):

| | |
|---|---|
| [`Specs/README.md`](./Specs/README.md) | Executive summary, index, decisions log |
| [`01-architecture-overview`](./Specs/01-architecture-overview.md) | System view, principles, end-to-end flow |
| [`02-domain-model-and-object-lifecycle`](./Specs/02-domain-model-and-object-lifecycle.md) | HDO/CO/NO actor model, FSMs, hierarchy |
| [`03-control-plane-pubsub-and-topics`](./Specs/03-control-plane-pubsub-and-topics.md) | etcd-based config bus, schemas, ordering |
| [`04-partitioning-and-shard-management`](./Specs/04-partitioning-and-shard-management.md) | Range-sharding, Partition Manager, online split/merge |
| [`05-high-availability-and-issu`](./Specs/05-high-availability-and-issu.md) | 3-replica ShardSet, lease, ISSU |
| [`06-northbound-registration`](./Specs/06-northbound-registration.md) | Device registration, identity, attestation |
| [`07-hal-and-dash-southbound`](./Specs/07-hal-and-dash-southbound.md) | HAL, DASH gNMI codec, vendor plugin model |
| [`08-reconciliation-storage-and-warm-restart`](./Specs/08-reconciliation-storage-and-warm-restart.md) | Three-loop model, WAL, drift correction |
| [`09-observability-and-diagnostics`](./Specs/09-observability-and-diagnostics.md) | OTel, metrics, logs, `dfctl` |
| [`10-deployment-and-security`](./Specs/10-deployment-and-security.md) | K8s topology, mTLS, multi-tenancy |
| [`11-failure-modes-and-runbooks`](./Specs/11-failure-modes-and-runbooks.md) | Failure matrix, runbooks, chaos |
| [`12-roadmap-and-open-questions`](./Specs/12-roadmap-and-open-questions.md) | Phasing, open questions, ADR backlog |
| [`Thought.md`](./Specs/Thought.md) | Original seed thinking (preserved) |

## Status

Design specification, v1 draft. Implementation has not started.
See [`Specs/12-roadmap-and-open-questions.md`](./Specs/12-roadmap-and-open-questions.md)
for the phased plan.

