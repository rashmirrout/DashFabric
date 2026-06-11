# `Vnet` — Per-VNET spec

> **TL;DR:** A tenant's virtual network: its VXLAN id, address prefixes,
> encap tunnel, and routing behavior. Identity + global properties only —
> the millions of address mappings live in separate
> [VnetMapping](./vnet-mapping.md) chunks.

**Topic:** `/config/v1/vnet/<device_id>/<vnet_id>/spec`
**Kind:** `CONFIG_KIND_VNET`
**Scope:** per-VNET, replicated under every device that hosts an ENI in it
**Lifecycle owner:** orchestrator
**Subscriber:** HDO actor (process-wide cache, refcounted by attached NOs)

## Example

```json
{
  "vnet_id": "vnet-tenant-acme-prod",
  "vni": 78215,
  "tenant_id": "tenant-acme-corp",
  "tunnel_id": "tun-vxlan-default-westus2",
  "routing_type": "privatelink",
  "address_prefixes_v4": ["10.42.0.0/16"],
  "address_prefixes_v6": ["fd00:acme:42::/48"],
  "subnet_prefixes_v4": ["10.42.0.0/24", "10.42.1.0/24"],
  "peer_vnet_ids": ["vnet-tenant-acme-shared-services"],
  "peering_policy": "BIDIRECTIONAL",
  "mapping_manifest_revision": 18742,
  "expected_mapping_chunk_count": 12,
  "pa_validation_required": true,
  "attributes": {
    "region": "westus2",
    "customer_tier": "gold"
  }
}
```

A VNET says *what address space exists* and *how to wrap traffic for it*;
who lives at each address is answered by the mapping chunks.

## Purpose

Defines a tenant Virtual Network: the L3 isolation domain that ENIs
attach to. A `Vnet` is **identity + global properties** — the address
mappings live separately in [VnetMapping](./vnet-mapping.md) chunks
because they can be millions of entries and change at a different cadence.

Per decision **#7**, the HDO holds a refcounted subscription to each VNET
its container/NIC actors reference. The first NIC to bind to `vnet_id=X`
triggers the HDO to start watching the VNET subtree; the last NIC to
detach triggers unsubscribe.

## Fields

### Identity

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `vnet_id` | `string` | yes | Globally unique VNET identifier (e.g. `"vnet-tenant-acme-prod"`). Matches the etcd path segment. |
| `vni` | `uint32` | yes | VXLAN Network Identifier (24-bit). Programmed into every Tunnel that terminates traffic for this VNET. Must be unique per tenant within the underlay routing domain. |
| `tenant_id` | `string` | yes | Owning tenant. Mirrors `metadata.tenant_id` in the envelope; duplicated here for payload self-containment. |

### Encapsulation

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `tunnel_id` | `string` | yes | Foreign key into `/config/v1/global/<device>/tunnel/<id>/`. Defines underlay encap (VXLAN/Geneve/GRE), source PA, MAC behavior. |
| `routing_type` | `string` | yes | Foreign key into `/config/v1/global/<device>/routing_type/<name>/`. Drives the action pipeline for traffic *destined* to this VNET (e.g., `privatelink`, `vnet_direct`, `vnet_peering`). **Decision #3.** |

### Address space

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `address_prefixes_v4` | `repeated IpPrefix` | no¹ | Customer-advertised IPv4 CIDR(s) for this VNET. Used for sanity checks against ENI primary IPs. |
| `address_prefixes_v6` | `repeated IpPrefix` | no¹ | Customer-advertised IPv6 CIDR(s). |
| `subnet_prefixes_v4` | `repeated IpPrefix` | no | Optional subnet breakdown for telemetry/diagnostics. Not used by the data plane. |
| `subnet_prefixes_v6` | `repeated IpPrefix` | no | Same for v6. |

¹ At least one of `address_prefixes_v4` or `address_prefixes_v6` must be non-empty.

### Peering

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `peer_vnet_ids` | `repeated string` | no | Other `vnet_id`s peered with this one. Drives composition of mapping fallback chains. |
| `peering_policy` | `PeeringPolicy` enum | no | `BIDIRECTIONAL` (default), `INBOUND_ONLY`, `OUTBOUND_ONLY`. |

### Mapping topology pointer

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `mapping_manifest_revision` | `uint64` | no | Hint: the orchestrator's last-known revision of `/vnet/<id>/mapping/_manifest`. Allows subscribers to skip a sweep if the manifest hasn't moved since last compose. Pure optimization — correctness relies on the watch. |
| `expected_mapping_chunk_count` | `uint32` | no | Sanity bound. HDO logs a warning if observed chunks ≠ this value after 30s. |

### Validation & telemetry

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `pa_validation_required` | `bool` | no | If true, traffic decap'd into this VNET must have its outer PA validated against the [PaValidation](./pa-validation.md) list. |
| `attributes` | `map<string,string>` | no | Free-form: `region`, `customer_tier`, `created_by`, etc. |

### Validation rules

1. `vni` must be > 0 and ≤ 16777215 (24-bit).
2. `tunnel_id` and `routing_type` must resolve in the global cache; missing ref ⇒ HDO stays in `WAITING_REFS`.
3. `address_prefixes_v4` and `address_prefixes_v6` cannot both be empty.
4. `peer_vnet_ids` must not contain `vnet_id` itself.
5. If `routing_type` resolves to one that requires PrivateLink behavior, then `pa_validation_required` SHOULD be true (warning, not reject).

## Proto3 sketch

```proto
syntax = "proto3";
package fleetmanager.v1;

import "common.proto";   // IpPrefix

enum PeeringPolicy {
  PEERING_POLICY_UNSPECIFIED   = 0;
  PEERING_POLICY_BIDIRECTIONAL = 1;
  PEERING_POLICY_INBOUND_ONLY  = 2;
  PEERING_POLICY_OUTBOUND_ONLY = 3;
}

message Vnet {
  // Identity
  string vnet_id   = 1;
  uint32 vni       = 2;
  string tenant_id = 3;

  // Encapsulation
  string tunnel_id    = 10;
  string routing_type = 11;

  // Address space
  repeated IpPrefix address_prefixes_v4 = 20;
  repeated IpPrefix address_prefixes_v6 = 21;
  repeated IpPrefix subnet_prefixes_v4  = 22;
  repeated IpPrefix subnet_prefixes_v6  = 23;

  // Peering
  repeated string peer_vnet_ids = 30;
  PeeringPolicy   peering_policy = 31;

  // Mapping topology pointer
  uint64 mapping_manifest_revision    = 40;
  uint32 expected_mapping_chunk_count = 41;

  // Validation & telemetry
  bool pa_validation_required        = 50;
  map<string, string> attributes     = 60;
}
```

## Relationships

- References:
  - `Tunnel` (1) — must be present in global cache.
  - `RoutingType` (1) — fleet-wide singleton lookup.
  - `Vnet` peers (N) — soft refs; missing peer doesn't block programming.
- Referenced by:
  - `NicSpec.vnet_id` — the primary consumer.
  - `VnetMappingManifest` — sibling under `/vnet/<id>/mapping/_manifest`.

## Change semantics

- `vni` change is a **destructive** edit — every ENI in the VNET must be
  reprogrammed. NO actor recomposes; HDO triggers fanout to all attached NOs.
- `tunnel_id` or `routing_type` change cascades to every NicGoalState in the VNET.
- `address_prefixes` change is non-disruptive (informational).
- `peer_vnet_ids` change triggers mapping-chain recomposition for affected NOs.
- `mapping_manifest_revision` change is an **optimization hint only**; correctness
  relies on the manifest's own etcd revision.

## HDO subscription lifecycle

```
NIC actor binds vnet_id=X
   │
   ▼
HDO.RefCount[X]++
   │
   ├── if RefCount[X] == 1 (first attach):
   │      open etcd watch on /config/v1/vnet/<device>/X/
   │      block NO programming until /spec, /pa_validation, /mapping/_manifest
   │      and at least one /mapping/<chunk_id> are present (or manifest says
   │      expected count = 0)
   │
   └── if RefCount[X] > 1: reuse existing subscription

NIC actor detaches (or migrates to new vnet_id)
   │
   ▼
HDO.RefCount[X]--
   │
   └── if RefCount[X] == 0: close watch, evict /vnet/<device>/X/ from cache
```

This guarantees that no NIC ever programs against a partially-loaded VNET,
and that idle VNETs don't bloat the in-process cache.

## See also

- [vnet-mapping](./vnet-mapping.md) — the sibling chunks + manifest carrying the actual address-to-PA mappings.
- [pa-validation](./pa-validation.md) — sibling list of allowed underlay PAs for decap.
- [tunnel](./tunnel.md) — referenced via `tunnel_id`.
- [routing-type](./routing-type.md) — referenced via `routing_type`.
- [nic-spec](./nic-spec.md) — references VNET via `vnet_id`.
- [envelope](./envelope.md) — wrapper around every published value.
