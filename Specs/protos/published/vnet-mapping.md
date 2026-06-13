# `VnetMappingManifest` + `VnetMappingChunk` — Sharded VNET address map

> **TL;DR:** The "which IP lives where?" table for a VNET. Can be
> millions of entries, so it's sharded into chunks (each <1 MiB). The
> manifest is the table of contents that says how many chunks exist and
> their content hashes; the chunks carry the actual mappings.

**Topics:**
- `/config/v1/vnet/<device_id>/<vnet_id>/mapping/_manifest` → `VnetMappingManifest`
- `/config/v1/vnet/<device_id>/<vnet_id>/mapping/<chunk_id>` → `VnetMappingChunk`

**Kinds:** `CONFIG_KIND_VNET_MAPPING_MANIFEST`, `CONFIG_KIND_VNET_MAPPING_CHUNK`
**Scope:** per-VNET, per device
**Lifecycle owner:** orchestrator
**Subscriber:** HDO actor (subscribes when the parent VNET is referenced; refcounted)

## Example

### Manifest

```json
// /config/v1/vnet/dpu-007/vnet-tenant-acme-prod/mapping/_manifest
{
  "vnet_id": "vnet-tenant-acme-prod",
  "manifest_revision": 18742,
  "shard_strategy": "PREFIX_BUCKET_HASH_V1",
  "shard_count": 12,
  "chunks": [
    { "chunk_id": "0000", "entry_count": 4321, "content_hash": "ab12...cd" },
    { "chunk_id": "0001", "entry_count": 4287, "content_hash": "ef34...90" },
    { "chunk_id": "0002", "entry_count": 4309, "content_hash": "12ab...34" }
    // ... 9 more ...
  ]
}
```

### Chunk

```json
// /config/v1/vnet/dpu-007/vnet-tenant-acme-prod/mapping/0001
{
  "vnet_id": "vnet-tenant-acme-prod",
  "chunk_id": "0001",
  "entries": [
    {
      "overlay_ip_v4": "10.42.1.7",
      "overlay_mac": "00:0d:3a:11:22:33",
      "underlay_ip_v4": "100.64.7.11",
      "tunnel_override_id": "",
      "routing_action_hint": "VNET"
    },
    {
      "overlay_ip_v4": "10.42.1.8",
      "overlay_mac": "00:0d:3a:11:22:34",
      "underlay_ip_v4": "100.64.8.12",
      "tunnel_override_id": "",
      "routing_action_hint": "VNET"
    }
    // ... thousands more ...
  ]
}
```

The HDO reads the manifest to know how many chunks to wait for; reads
each chunk and validates its `content_hash` against the manifest's entry;
assembles the full mapping in memory; gates NIC programming on
completeness.

## Purpose

Per decision **#8**: the mapping table can grow to millions of entries per
VNET, far exceeding etcd's 1 MiB value cap. Chunking keeps each value
under the cap and allows partial updates (touching one chunk doesn't
re-publish the whole table).

The manifest ensures consistency: subscribers know the *expected* shape
of the table at every manifest revision. A chunk whose hash doesn't
match the manifest's is rejected. Late-arriving chunks block NIC
programming until the manifest's view is fully assembled.

## `VnetMappingManifest` fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `vnet_id` | `string` | yes | Mirrors path; self-containment. |
| `manifest_revision` | `uint64` | yes | Mirrors `metadata.revision`. Used for ordering across chunk updates. |
| `shard_strategy` | `string` | yes | Algorithm name (e.g. `PREFIX_BUCKET_HASH_V1`). Documents how entries are assigned to chunks. |
| `shard_count` | `uint32` | yes | Expected number of chunks (`chunks` length must equal this). |
| `chunks` | `repeated ChunkRef` | yes | Table of contents. |
| `attributes` | `map<string,string>` | no | Free-form. |

### `ChunkRef`

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `chunk_id` | `string` | yes | Identifier matching the chunk's path segment. |
| `entry_count` | `uint32` | yes | Number of entries in the chunk (sanity check after decode). |
| `content_hash` | `bytes(32)` | yes | SHA-256 of the chunk's canonical serialization. |

## `VnetMappingChunk` fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `vnet_id` | `string` | yes | Mirrors path. |
| `chunk_id` | `string` | yes | Mirrors path. |
| `entries` | `repeated MappingEntry` | yes | The mapping rows. |

### `MappingEntry`

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `overlay_ip_v4` | `IpAddress` | yes¹ | Tenant-facing IPv4 address. |
| `overlay_ip_v6` | `IpAddress` | yes¹ | Tenant-facing IPv6 address. |
| `overlay_mac` | `MacAddress` | no | Used by VNETs that program L2-aware behavior. |
| `underlay_ip_v4` | `IpAddress` | yes² | Hosting DPU's PA address (where to send encap). |
| `underlay_ip_v6` | `IpAddress` | yes² | IPv6 PA. |
| `tunnel_override_id` | `string` | no | If set, use this Tunnel instead of VNET's default. |
| `routing_action_hint` | `string` | no | Hint of the action that should fire (`VNET`, `PRIVATELINK`, etc.). |

¹ At least one of overlay v4/v6.
² At least one of underlay v4/v6 matching the overlay family.

### Validation rules

1. `VnetMappingManifest.shard_count` must equal `len(chunks)`.
2. `VnetMappingChunk.entries.count` must equal the manifest's
   `ChunkRef.entry_count` for the same `chunk_id`.
3. `SHA-256(canonical(chunk)) == manifest.chunks[chunk_id].content_hash`.
4. Each `entries[*].overlay_*` must be within one of `Vnet.address_prefixes_*`.
5. Each `entries[*].underlay_*` SHOULD be within one of
   `PaValidation.allowed_pa_*` when `pa_validation_required=true` (warning, not reject).
6. Per-chunk serialized size ≤ 900 KiB (leaves headroom below etcd cap).
7. Total entry count across chunks ≤ 4,000,000 (sanity cap; HERO is ~1M per VNET).

## Proto3 sketch

```proto
syntax = "proto3";
package fleetmanager.v1;

import "common.proto";   // IpAddress, MacAddress

message ChunkRef {
  string chunk_id     = 1;
  uint32 entry_count  = 2;
  bytes  content_hash = 3;   // 32-byte SHA-256
}

message VnetMappingManifest {
  string             vnet_id           = 1;
  uint64             manifest_revision = 2;
  string             shard_strategy    = 3;
  uint32             shard_count       = 4;
  repeated ChunkRef  chunks            = 5;
  map<string,string> attributes        = 20;
}

message MappingEntry {
  IpAddress  overlay_ip_v4       = 1;
  IpAddress  overlay_ip_v6       = 2;
  MacAddress overlay_mac         = 3;
  IpAddress  underlay_ip_v4      = 4;
  IpAddress  underlay_ip_v6      = 5;
  string     tunnel_override_id  = 6;
  string     routing_action_hint = 7;
}

message VnetMappingChunk {
  string                 vnet_id = 1;
  string                 chunk_id = 2;
  repeated MappingEntry  entries  = 3;
}
```

## HDO assembly lifecycle

```
Watch fires on /vnet/<id>/mapping/_manifest
   │
   ▼
Open / refresh watches on every chunk listed in manifest.chunks[*]
   │
   ▼  (per chunk arrival)
Validate chunk.content_hash against manifest.chunks[chunk_id].content_hash
   ├── match: store in cache slot for (vnet_id, chunk_id)
   └── mismatch: write ValidationError, do not apply
   │
   ▼  (when all chunks present & validated for this manifest_revision)
Mark VNET mapping COMPLETE at manifest_revision
   │
   ▼
Unblock NIC programming for any NO actors waiting on this VNET
```

A new manifest revision atomically swaps the chunk set — old chunks
not referenced by the new manifest are evicted; new chunks not yet
arrived put the VNET back into INCOMPLETE state and NIC programming
pauses on the affected NOs.

## Relationships

- Manifest references: every chunk under the same VNET subtree.
- Chunk references: indirectly references `Tunnel` (via `tunnel_override_id`).
- Referenced by: every NIC in the VNET (via composition, not by explicit id).

## Change semantics

- **Adding entries** in an existing chunk: new chunk revision; manifest's
  `entry_count` and `content_hash` for that chunk update; HDO replaces
  the cached chunk on validation success.
- **Adding chunks** (`shard_count` increases): manifest revision bumps
  with new entries in `chunks[]`; HDO opens watches on the new chunks;
  NIC programming pauses until they arrive.
- **Re-sharding**: orchestrator publishes a new manifest with different
  chunk_ids and new content; HDO treats the old chunks as evictable
  once the new ones are validated.

## Upstream DASH alignment

Upstream `DASH_VNET_MAPPING_TABLE` is a **single, monolithic per-VNET
table** keyed by `vnet:overlay_ip`. Each row carries the underlay PA,
overlay MAC, and an optional routing/encap override — written one row
at a time over the SAI/gNMI surface.

The manifest + chunk split here is **FM-side packaging**, not an
upstream concept. Reasons:

- etcd's 1 MiB value cap forces large VNETs (millions of rows) to be
  split into <1 MiB chunks for transport.
- The manifest gives subscribers (HDO actors) a content-addressed view
  of the *expected* table at every revision so partial publishes can be
  detected and gated.
- Chunking enables incremental updates: a single mapping change rewrites
  one chunk, not the whole table.

At the southbound, the HAL flattens manifest+chunks back into per-row
writes against upstream `DASH_VNET_MAPPING_TABLE`. Self-entries (one
per local NIC's `primary_ip_*`) are *injected* by FM at compose time
since upstream DASH has no overlay-IP attribute on ENI — see
[nic-spec.md](./nic-spec.md) "Upstream DASH alignment". The
`tunnel_override_id` and `routing_action_hint` fields on
`MappingEntry` map directly to upstream's per-row tunnel and
routing-type override attributes.

## See also

- [vnet](./vnet.md) — parent (and consumer of mapping completeness).
- [pa-validation](./pa-validation.md) — sibling list for underlay sanity.
- [tunnel](./tunnel.md) — resolved via `tunnel_override_id`.
- [README](./README.md) — full kind index.