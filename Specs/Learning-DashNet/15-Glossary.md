# 15 — Glossary

> Alphabetical reference for every term, acronym, and object name used
> in this learning series. Use this as a lookup, not a read-through.

---

## A

**ACL (Access Control List)** — A list of match-action rules that
allow or deny traffic based on header fields. DASH runs three ACL
stages (VNIC, SUBNET, VNET) in each direction. See [chapter 07](./07-ACL-Pipeline.md).

**`AclGroup`** — A DASH object holding a bundle of ACL rules,
referenced by ENIs. Header/body split: `AclGroup` + `AclRuleList`.

**`AclRule`** — A single match-action row inside an `AclGroup`. Match
on 5-tuple + tag refs; action is `ALLOW` / `DENY` / `*_AND_CONTINUE`.

**Agent (DASH agent)** — The software running on the DPU's management
CPU that receives gNMI from the control plane and programs the
silicon via SAI. See [chapter 02](./02-Hardware-Foundation-DPU-Appliance.md).

**`Appliance`** — A DASH object representing one DPU: id, PA(s), ASN,
capability limits. See [chapter 03](./03-Object-Model-and-Scopes.md).

**ASN (Autonomous System Number)** — BGP identifier for the
appliance if it participates in underlay routing.

---

## B

**BGP (Border Gateway Protocol)** — The routing protocol the underlay
fabric typically runs. DASH itself doesn't define BGP behavior; it
just relies on PAs being routable.

**BMC (Baseboard Management Controller)** — Out-of-band management
controller present on most DPUs.

---

## C

**CA (Customer Address)** — The overlay IP a VM uses (e.g.,
`10.42.0.5`). Tenant-private; only meaningful inside its VNET.
Contrast with **PA**.

**CBS (Committed Burst Size)** — Token bucket depth for the
committed rate in a meter rule. Allows short bursts above CIR.

**CIR (Committed Information Rate)** — The guaranteed bandwidth in a
meter rule (bits per second). See [chapter 08](./08-Metering-and-QoS.md).

**CO actor (ContainerObject actor)** — The per-container actor in
the FleetManager design that owns one VM/container and its child
ENIs.

**Conformance tests** — DASH's test suite that verifies a vendor's
DPU implements the spec correctly.

**`ContainerSpec`** — A DASH object grouping one or more `NicSpec`s
under a common owner/tenant/lifecycle.

**Container** — Generic DASH term for "the thing that owns the
ENIs": VM, OS container, bare-metal server.

**Content hash** — SHA-256 of an object's canonical serialization,
used for idempotency checks. If the new hash equals the last-applied,
no programming work is needed.

---

## D

**DASH (Disaggregated API for SONiC Hosts)** — The project this
series teaches. <https://github.com/sonic-net/DASH>

**Default tunnel** — A `Tunnel` referenced as the fallback for
unmatched egress routes (e.g., internet gateway). See [chapter 09](./09-Tunnels-and-Encap.md).

**DPU (Data Processing Unit)** — Programmable SmartNIC: NIC ports +
CPUs + memory + match-action silicon. See [chapter 02](./02-Hardware-Foundation-DPU-Appliance.md).

**Drain** — A teardown mode that lets existing flows complete while
refusing new ones. Configured via `ContainerSpec.lifecycle.drain_on_remove`.

**DSCP (Differentiated Services Code Point)** — 6-bit priority field
in the IP header. `Qos.dscp_remap[]` can rewrite it on egress.

---

## E

**ECMP (Equal-Cost Multi-Path)** — Routing multiple equivalent
next-hops by per-flow hash. A `Tunnel` with multiple
`dst_underlay_ips_*` uses outer-header ECMP.

**Encap (Encapsulation)** — Wrapping the inner frame in an outer
header (VXLAN/GENEVE/NVGRE). The DPU does this on egress.

**`Eni` / ENI (Elastic Network Interface)** — The per-VM-NIC DASH
object. See [chapter 05](./05-ENI-Deep-Dive.md).

**Entry** — A single row inside a body object (e.g., `MappingEntry`,
`RouteEntry`). Not standalone objects.

---

## F

**Fabric (underlay fabric)** — The physical network connecting DPUs.
Routes outer (PA) headers; opaque to overlay.

**Failover** — Promotion of an HA standby to primary on failure of
the primary. See [chapter 13](./13-Scenario-HA-and-Failover.md).

**FleetManager** — This repo's specific control-plane design (the
"DashFabric" project). Not part of DASH proper.

---

## G

**GENEVE** — A flexible encapsulation format (RFC 8926), default
UDP port 6081. Alternative to VXLAN; supports TLV options.

**gNMI (gRPC Network Management Interface)** — The southbound RPC
protocol DASH uses; runs over gRPC with protobuf payloads.

**Goal state** — See `NicGoalState`.

**Group (scope)** — Reusable rule bundles shared across ENIs:
`RouteGroup`, `AclGroup`, `MeterPolicy`, `OutboundPortMap`, `Tunnel`,
`Qos`, `HaSet`, `PrefixTag`. See [chapter 03](./03-Object-Model-and-Scopes.md).

---

## H

**HA (High Availability)** — A pair of ENIs on two DPUs configured as
PRIMARY/STANDBY. See [chapter 13](./13-Scenario-HA-and-Failover.md).

**HAL (Hardware Abstraction Layer)** — The agent component that
issues SAI calls to the silicon.

**`HaSet`** — DASH object naming the two-DPU HA pair.

**HDO actor (HostDeviceObject actor)** — Per-host actor in the
FleetManager that owns the VNET / group caches for one DPU.

**Header/body split** — Pattern where a "header" object (small,
stable) holds metadata and references a separate "body" object
(large, hot). Used for `RouteGroup`/`RouteList`,
`AclGroup`/`AclRuleList`, `MeterPolicy`/`MeterRuleList`,
`OutboundPortMap`/`OutboundPortMapRanges`,
`VnetMappingManifest`/`VnetMappingChunk`.

**Heartbeat** — Periodic liveness probe between HA pair members.

**`HostSpec`** — DASH object: hostname, agent endpoint, host-level
feature flags.

---

## I

**Idempotency** — Applying the same intent twice produces the same
result. DASH achieves this via content-hash comparison.

**Inbound** — Traffic direction: wire → VM (after decap). Contrast
with **Outbound**.

**`InboundRoutingRule`** — Per-ENI override rules for inbound
routing. Rare; usually empty.

**Intent** — Tenant-level desired state ("VM with these
properties"). The control plane translates intent to DASH objects.

---

## L

**LPM (Longest Prefix Match)** — Algorithm used by the routing
lookup: among all matching prefixes, the most specific (longest)
wins. Ties broken by priority. See [chapter 06](./06-Routing-Pipeline.md).

---

## M

**MAC (Media Access Control)** — L2 address. Used by ENI lookup
(src MAC → which ENI) and by inner Ethernet frames inside encap.

**Manifest** — Table-of-contents object listing many chunk objects.
Used in `VnetMappingManifest`.

**`MappingEntry`** — A single CA→PA row inside a `VnetMappingChunk`.

**Match-action table** — Pipeline data structure: lookup on a key
(match), execute an associated action. The silicon's primitive.

**Mapping (VNET mapping)** — The CA→PA table for a VNET. Sharded
into chunks. See [chapter 04](./04-VNET-and-Address-Mapping.md).

**Meter (policer)** — Token-bucket rate limiter. See [chapter 08](./08-Metering-and-QoS.md).

**Metering class** — String label that identifies a counter bucket
in `MeterRule`. Used for billing and observability.

**`MeterPolicy`** / **`MeterRuleList`** / **`MeterRule`** — DASH
objects for traffic metering.

**MTU (Maximum Transmission Unit)** — Largest packet size. VXLAN
overhead = 50 bytes; underlay MTU must accommodate.

---

## N

**NAT (Network Address Translation)** — Rewriting source/dest IPs
or ports. DASH supports SNAT-style outbound NAT via
`OutboundPortMap`.

**`NicGoalState`** — The fully resolved, denormalized program for
one ENI. Composed by the NO actor; sent to HAL; never published.
See the schema: [`nic-goal-state.md`](../protos/published/nic-goal-state.md).

**`NicSpec`** — The published per-ENI object (the "intent" form,
all-references).

**NO actor (NicObject actor)** — Per-ENI actor in FleetManager that
composes goal-state and drives HAL.

**Northbound** — API toward higher-level systems (orchestrators,
cloud APIs). DASH doesn't define this — providers do.

**NVGRE (Network Virtualization GRE)** — Older encap format (RFC
7637), IP proto 47. Rare in greenfield deployments.

---

## O

**Outbound** — Traffic direction: VM → wire (before encap).

**`OutboundPortMap`** / **`OutboundPortMapRanges`** — DASH objects
for SNAT-style outbound NAT pools.

**Overlay** — Tenant-visible network. CA address space.

**Override (route_rules)** — Per-ENI inline routes that take
precedence over group routes.

---

## P

**P4** — A language for programming packet-processing pipelines.
DASH's pipeline reference is in P4.

**PA (Provider Address)** — Underlay IP belonging to a DPU; routable
on the fabric. Contrast with **CA**.

**PA validation** — Anti-spoof check on inbound decapped packets:
outer source PA must be in the VNET's allowlist. See `PaValidation`.

**`PaValidation`** — DASH object holding the allowlist of underlay
source PAs permitted to decap into a VNET.

**Peering (VNET peering)** — Allowed traffic between two VNETs. See
[chapter 04](./04-VNET-and-Address-Mapping.md).

**PIR (Peak Information Rate)** — The maximum bandwidth in a meter
rule (above CIR but below PIR is "yellow").

**Pipeline** — The DPU's packet-processing path: a series of
match-action tables in fixed order. See [chapter 10](./10-Packet-Processing-Lifecycle.md).

**Prefix** — IP address with mask (e.g., `10.42.0.0/16`).

**`PrefixTag`** — Named list of IP prefixes (e.g.,
`tag-azure-storage`) referenced by ACL/route rules. Expanded at
compose time.

**PrivateLink** — Pattern where a managed service appears as a
private IP inside the tenant's VNET. See [chapter 12](./12-Scenario-PrivateLink-and-ServiceTunnel.md).

---

## Q

**QoS (Quality of Service)** — Per-ENI bandwidth and queue
allocation. See `Qos` object and [chapter 08](./08-Metering-and-QoS.md).

**`Qos`** — DASH object: bw_gbps, burst, queue_count, DSCP remap.

---

## R

**Reconcile** — Compare desired state vs reported device state;
repair drift. The agent and control plane both do this.

**Revision** — Monotonic counter on every DASH object; bumps on
every write. Used for ordering and idempotency.

**`RouteEntry`** — A single LPM rule inside a `RouteGroup`/`RouteList`.

**`RouteGroup`** / **`RouteList`** — DASH objects for outbound LPM
routing tables.

**Routing type** — Named action template in the fleet-wide catalog.
See `RoutingType`.

**`RoutingType`** — Fleet-scope DASH object cataloging named action
behaviors (`privatelink-v1`, `vnet_direct-v1`, …).

---

## S

**SAI (Switch Abstraction Interface)** — Vendor-neutral C API for
switching silicon. DASH adds DPU-specific object types.

**Service tunnel** — Pattern where tenant traffic to a managed
service's public IP is intercepted and tunneled directly. See
[chapter 12](./12-Scenario-PrivateLink-and-ServiceTunnel.md).

**Shard** — Subset of a larger object (typically a `VnetMapping`).

**SmartNIC** — Synonym for DPU.

**SNAT (Source NAT)** — Rewriting the source IP/port; used for
service tunnel and outbound NAT.

**SONiC (Software for Open Networking in the Cloud)** — Open-source
NOS hosting DASH. <https://github.com/sonic-net/SONiC>

**Southbound** — API toward devices (DPUs). DASH defines this layer.

**SR-IOV (Single-Root I/O Virtualization)** — Mechanism by which one
physical PCIe device exposes many "virtual functions" (VFs) to VMs.
DPUs typically expose VFs to VMs.

**Stage** — One of the three ACL pipeline positions (VNIC, SUBNET,
VNET) per direction.

**Standby** — HA member not currently forwarding; ready to promote.

**Sync channel** — Direct DPU-to-DPU communication path for HA
flow-state replication.

---

## T

**Tag** — See `PrefixTag`.

**Tenant** — End-user organization owning VMs and VNETs.

**TLV (Type-Length-Value)** — Extensible field format used in
GENEVE headers.

**Topology** — Physical layout of DPUs, racks, fabric.

**ToR (Top-of-Rack switch)** — First-hop underlay switch above a
rack of servers.

**Transformation** — Header modification (NAT, encap, decap, DSCP
remap) applied by the pipeline.

**trTCM (two-rate three-color marker)** — RFC 2698, the policer
model DASH metering uses. Green/yellow/red outcomes.

**`Tunnel`** — DASH object naming an encap profile.

---

## U

**Underlay** — Provider physical network. PA address space.

---

## V

**VF (Virtual Function)** — SR-IOV virtual NIC presented to a VM.
One ENI typically corresponds to one VF.

**`Vnet`** — DASH object: the overlay tenant network. VNI, address
prefixes, peering refs.

**`VnetMappingChunk`** — One shard of a VNET mapping table.

**`VnetMappingManifest`** — Table-of-contents for chunks; holds
content hashes and counts.

**VNI (VXLAN Network Identifier)** — 24-bit overlay tag in the
VXLAN header. Identifies the tenant VNET.

**VNIC (Virtual NIC)** — Conceptually, the VM's network interface;
in DASH, the first ACL stage is named "VNIC stage."

**VXLAN (Virtual eXtensible LAN)** — Default DASH encap (RFC 7348),
UDP port 4789.

---

## W

**Warm restart** — Agent restarts without disturbing the data path.
SAI table contents preserved across restart.

---

## See also

- All previous chapters → [00 — README](./00-README.md)
- DASH upstream → <https://github.com/sonic-net/DASH>
- SAI DASH headers → <https://github.com/opencomputeproject/SAI>
