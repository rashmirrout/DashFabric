# Learning DASH Networking — A Structured Path

> **Audience:** Engineers new to DASH, SmartNIC/DPU networking, or
> hyperscale cloud overlay networks. By the end of this series you will
> understand every DASH object, what scope it lives at, how a packet is
> processed, and how to stitch a working VM NIC together end-to-end.

This series is written **chronologically** — read in order. Each book
builds on the previous one. Diagrams use Mermaid (rendered inline by
GitHub, VS Code, and most markdown viewers).

---

## Learning path

| # | Book | What you'll learn |
|---|------|-------------------|
| 00 | **README** (this file) | Roadmap and prerequisites |
| 01 | [Introduction & Motivation](./01-Introduction-and-Motivation.md) | Why DASH exists; the cloud-networking problem it solves |
| 02 | [Hardware Foundation — DPU & Appliance](./02-Hardware-Foundation-DPU-Appliance.md) | What a DPU is, how it plugs into a host, the appliance abstraction |
| 03 | [Object Model & Scopes](./03-Object-Model-and-Scopes.md) | Every DASH object, categorized by scope (device / VNET / ENI / group) |
| 04 | [VNET & Address Mapping](./04-VNET-and-Address-Mapping.md) | Virtual networks, VNI, overlay/underlay, the mapping table |
| 05 | [ENI Deep Dive](./05-ENI-Deep-Dive.md) | The per-VM NIC abstraction — every property and reference |
| 06 | [Routing Pipeline](./06-Routing-Pipeline.md) | Outbound LPM, route groups, route action types |
| 07 | [ACL Pipeline](./07-ACL-Pipeline.md) | The 3-stage ACL chain (VNIC → Subnet → VNET), in & out |
| 08 | [Metering & QoS](./08-Metering-and-QoS.md) | Token buckets, metering classes, ENI-level QoS |
| 09 | [Tunnels & Encap](./09-Tunnels-and-Encap.md) | VXLAN/GENEVE/NVGRE, PA vs CA, tunnel groups |
| 10 | [Packet Processing Lifecycle](./10-Packet-Processing-Lifecycle.md) | End-to-end packet walkthrough — outbound and inbound |
| 11 | [Scenario — VM NIC Provisioning](./11-Scenario-VM-NIC-Provisioning.md) | Full provisioning trace: orchestrator → DPU |
| 11A | [ENI Dependency Graph](./11A-ENI-Dependency-Graph.md) | Reference: every prerequisite for an ENI, layered DAG, programming order, gates, tear-down |
| 12 | [Scenario — PrivateLink & Service Tunnel](./12-Scenario-PrivateLink-and-ServiceTunnel.md) | The "talk to a managed service" pattern |
| 13 | [Scenario — HA & Failover](./13-Scenario-HA-and-Failover.md) | Active/standby ENI pairs across DPUs |
| 14 | [Stitching Everything Together](./14-Stitching-Everything-Together.md) | The big picture — how all pieces compose at runtime |
| 15 | [Glossary](./15-Glossary.md) | All acronyms and terms in one place |
| 16 | [Common Misconceptions & Myths](./16-Common-Misconceptions.md) | The wrong mental models people arrive with — and what's actually true |

---

## What is DASH?

**DASH** = **D**isaggregated **A**PI for **S**ONiC **H**osts.

It's an open-source project under SONiC (sonic-net) that defines:

1. A **standardized object model** for SmartNIC/DPU-accelerated networking.
2. A **southbound API** (gNMI + protobuf + SAI extensions) for control
   planes to program DPUs.
3. A **packet-processing pipeline** specification (the "DASH pipeline")
   that every conformant DPU implements.

It exists because cloud providers (Azure, AWS-style overlays) need to
offload millions of flows per second of overlay networking — VNET
routing, ACLs, encap/decap, NAT — from the hypervisor CPU to dedicated
silicon. Without a standard, every DPU vendor would invent its own API
and every cloud control plane would need N integrations.

---

## How to read this series

- **First time?** Read 01 → 02 → 03 → 04 → 05 in order, then skim
  [16 — Common Misconceptions](./16-Common-Misconceptions.md) to
  unlearn the wrong mental models before you build on them. Stop
  there if you only need the conceptual model.
- **Need to write code that programs a DPU?** Continue 06 → 07 → 08 →
  09, then the scenarios (11, 12, 13).
- **Debugging a packet drop?** Jump to 10 (packet lifecycle), use 06–09
  as references for each stage.
- **Designing a control plane?** Read everything; pay special attention
  to 03 (scopes), 11 (provisioning order), and 14 (stitching).

---

## Prerequisites (helpful, not required)

- Basic IP networking: routing, ACLs, encapsulation, MAC addresses.
- VXLAN at a conceptual level (an Ethernet frame inside a UDP packet).
- Comfort reading Mermaid diagrams and protobuf-style schemas.
- Understanding of "overlay vs underlay" in a cloud datacenter.

If any of these are new, skim the [Glossary](./15-Glossary.md) first.

---

## Reference sources

This series is built from deep reading of:

- **DASH project** — <https://github.com/sonic-net/DASH/tree/main>
  - `documentation/` — design docs, pipeline diagrams, HLDs.
  - `dash-pipeline/` — P4 reference of the packet processor.
  - `test/` — test scenarios that illuminate intended behavior.
- **SONiC DASH API** — <https://github.com/sonic-net/sonic-dash-api>
  - Protobuf schemas used by the southbound.
- **SAI DASH extensions** — the SAI headers under `sonic-net/SAI` that
  add DPU-aware object types (`sai_dash_eni.h`, `sai_dash_vnet.h`,
  `sai_dash_outbound_routing.h`, etc.).

Where this series simplifies for pedagogy, the source-of-truth is
those upstream repos. Cross-references are noted inline.

---

## Conventions used

- **CA** = Customer Address (overlay / tenant IP)
- **PA** = Provider Address (underlay / physical IP)
- **ENI** = Elastic Network Interface (per-VM NIC)
- **VNI** = VXLAN Network Identifier (the overlay tag in the VXLAN header)
- **DPU** = Data Processing Unit (the SmartNIC silicon)
- **HAL** = Hardware Abstraction Layer (the agent on the DPU)
- Block diagrams use `flowchart`; packet flows use `sequenceDiagram`;
  hierarchies use `graph TD`.

Now turn to [01 — Introduction & Motivation](./01-Introduction-and-Motivation.md).
