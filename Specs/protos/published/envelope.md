# `ConfigEntry` — Envelope for every published value

> **TL;DR:** The outer wrapper every etcd value uses. Carries identity,
> revision, tracing, and a SHA-256 of the inner payload so subscribers
> validate the same way no matter what kind of object is inside.

**Topic prefix:** N/A (wraps every other kind)
**Scope:** envelope on all published etcd values
**Lifecycle owner:** orchestrator
**Subscriber:** every FleetManager subscriber unwraps before kind-specific decode

## Example

```json
{
  "metadata": {
    "schema_version": 1,
    "kind": "NIC_SPEC",
    "revision": 42,
    "tenant_id": "tenant-acme-corp",
    "trace_context": {
      "trace_id": "550e8400-e29b-41d4-a716-446655440000",
      "span_id": "00f067aa0ba902b7",
      "trace_flags": 1
    },
    "payload_digest": "iH9ck0...sNk=",
    "issued_at": "2026-06-11T14:23:45.123Z",
    "producer_id": "orch-westus2-node-7",
    "attributes": {
      "change_request_id": "CR-2026-06-11-44892",
      "source_system": "tenant-portal"
    }
  },
  "payload": "<base64 of NicSpec proto-binary>"
}
```

The `metadata` describes *what this thing is* and *whether it's intact*;
the `payload` is the proto-binary bytes of the kind named by
`metadata.kind` (here, a `NicSpec`).

## Purpose

Decision **#18** of the design doc: every etcd value FleetManager subscribes
to is a `ConfigEntry`. The envelope carries identity, tracing, versioning,
and integrity metadata uniformly so that subscriber-side validation can be
implemented once.

## `ConfigEntry`

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `metadata` | `ConfigMetadata` | yes | Out-of-band identity & integrity data |
| `payload` | `bytes` | yes | Proto-binary serialization of the kind-specific message named by `metadata.kind` |

## `ConfigMetadata`

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `schema_version` | `uint32` | yes | Schema version of the *payload* type. Bumped only on incompatible field rename/removal. Initial value `1`. |
| `kind` | `ConfigKind` (enum) | yes | Discriminator for `payload` decoding. See enum below. |
| `revision` | `uint64` | yes | Monotonic per-key revision assigned by orchestrator. FleetManager rejects `<= last_applied_revision` for the same etcd key (idempotency + out-of-order protection). |
| `tenant_id` | `string` | yes (may be `"system"`) | Tenant ownership; enforced by etcd RBAC at write time. Cross-tenant references (RoutingType, Appliance) use `"system"`. |
| `trace_context` | `TraceContext` (see `01-common.md`) | yes | W3C trace context for end-to-end correlation. |
| `payload_digest` | `bytes(32)` | yes | SHA-256 of `payload`. Decision **#26**. Used for: integrity check after etcd read, idempotency on retries, drift detection during Reconcile. |
| `issued_at` | `Timestamp` | yes | Orchestrator wall clock at publish time. Used for telemetry only — never for ordering (that's `revision`). |
| `producer_id` | `string` | yes | Identifier of the orchestrator instance that emitted this entry (e.g. `"orch-westus2-node-7"`). Recorded in audit. |
| `attributes` | `map<string,string>` | no | Free-form metadata (`source_system`, `correlation_id`, `change_request_id`). Never participates in decoding. |

## `ConfigKind` enum

```
CONFIG_KIND_UNSPECIFIED               = 0
CONFIG_KIND_APPLIANCE                 = 1
CONFIG_KIND_ROUTING_TYPE              = 2
CONFIG_KIND_TUNNEL                    = 3
CONFIG_KIND_QOS                       = 4
CONFIG_KIND_PREFIX_TAG                = 5
CONFIG_KIND_HA_SET                    = 6
CONFIG_KIND_ROUTE_GROUP               = 10
CONFIG_KIND_ROUTE_LIST                = 11
CONFIG_KIND_ACL_GROUP                 = 12
CONFIG_KIND_ACL_RULE_LIST             = 13
CONFIG_KIND_METER_POLICY              = 14
CONFIG_KIND_METER_RULE_LIST           = 15
CONFIG_KIND_OUTBOUND_PORT_MAP         = 16
CONFIG_KIND_OUTBOUND_PORT_MAP_RANGES  = 17
CONFIG_KIND_VNET                      = 20
CONFIG_KIND_PA_VALIDATION             = 21
CONFIG_KIND_VNET_MAPPING_MANIFEST     = 22
CONFIG_KIND_VNET_MAPPING_CHUNK        = 23
CONFIG_KIND_HOST_SPEC                 = 30
CONFIG_KIND_CONTAINER_SPEC            = 31
CONFIG_KIND_NIC_SPEC                  = 32
```

**Tag numbers are stable** — never reuse a tombstoned value. New kinds get
the next available number; gaps within ranges are reserved for future
related kinds.

## Validation rules

Performed in this order by every subscriber on every receive:

1. `metadata.schema_version` is recognized for `metadata.kind`. Unrecognized → `VALIDATION_REJECTED{code=UNSUPPORTED_SCHEMA}`.
2. `metadata.kind` is recognized. Unrecognized → `VALIDATION_REJECTED{code=UNKNOWN_KIND}`.
3. `len(payload)` ≤ 1 MiB (etcd value cap minus envelope overhead headroom). Larger → `VALIDATION_REJECTED{code=PAYLOAD_TOO_LARGE}`. (For large objects like VnetMapping use the chunked manifest pattern.)
4. `SHA-256(payload) == metadata.payload_digest`. Mismatch → `VALIDATION_REJECTED{code=DIGEST_MISMATCH}` — indicates etcd corruption or malicious write.
5. `metadata.revision > last_applied_revision_for_key`. Lower or equal → silently ignore (idempotent replay).
6. `metadata.tenant_id` is non-empty and (for tenant-scoped paths) matches the path's tenant prefix. Mismatch → `VALIDATION_REJECTED{code=TENANT_MISMATCH}`.
7. Decode `payload` per `kind` and run kind-specific validation. Failure → `VALIDATION_REJECTED{code=PAYLOAD_INVALID, details=...}`.
8. Resolve referential integrity for the decoded payload (e.g., `Vnet.tunnel_id` exists in cache). Missing refs → actor enters `WAITING_REFS` state; not an outright reject.

## Proto3 sketch

```proto
syntax = "proto3";
package fleetmanager.v1;

import "google/protobuf/timestamp.proto";
import "common.proto";   // TraceContext

enum ConfigKind {
  CONFIG_KIND_UNSPECIFIED = 0;
  CONFIG_KIND_APPLIANCE = 1;
  CONFIG_KIND_ROUTING_TYPE = 2;
  CONFIG_KIND_TUNNEL = 3;
  CONFIG_KIND_QOS = 4;
  CONFIG_KIND_PREFIX_TAG = 5;
  CONFIG_KIND_HA_SET = 6;
  CONFIG_KIND_ROUTE_GROUP = 10;
  CONFIG_KIND_ROUTE_LIST = 11;
  CONFIG_KIND_ACL_GROUP = 12;
  CONFIG_KIND_ACL_RULE_LIST = 13;
  CONFIG_KIND_METER_POLICY = 14;
  CONFIG_KIND_METER_RULE_LIST = 15;
  CONFIG_KIND_OUTBOUND_PORT_MAP = 16;
  CONFIG_KIND_OUTBOUND_PORT_MAP_RANGES = 17;
  CONFIG_KIND_VNET = 20;
  CONFIG_KIND_PA_VALIDATION = 21;
  CONFIG_KIND_VNET_MAPPING_MANIFEST = 22;
  CONFIG_KIND_VNET_MAPPING_CHUNK = 23;
  CONFIG_KIND_HOST_SPEC = 30;
  CONFIG_KIND_CONTAINER_SPEC = 31;
  CONFIG_KIND_NIC_SPEC = 32;
}

message ConfigMetadata {
  uint32 schema_version           = 1;
  ConfigKind kind                 = 2;
  uint64 revision                 = 3;
  string tenant_id                = 4;
  TraceContext trace_context      = 5;
  bytes payload_digest            = 6;   // 32-byte SHA-256
  google.protobuf.Timestamp issued_at = 7;
  string producer_id              = 8;
  map<string,string> attributes   = 9;
}

message ConfigEntry {
  ConfigMetadata metadata = 1;
  bytes payload           = 2;
}
```

## Relationships

- Wraps: every other published kind in this directory.
- Referenced by: none (it is the outermost shell).

## Change semantics

A change to `ConfigEntry`/`ConfigMetadata` shape is a **breaking infrastructure
change** and requires coordinated orchestrator + FleetManager rollout. Bump
`schema_version` on all kinds simultaneously, deploy subscriber first
(accepts both old and new), then publisher.

## See also

- [README](./README.md) — full index of published kinds.
- [error](./error.md) — async validation errors that report rejection of any of the steps above.
- Any kind file (e.g. [nic-spec](./nic-spec.md)) — the payload that gets wrapped.
