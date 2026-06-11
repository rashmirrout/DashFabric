# `ValidationError` — Async error sink (`/status/v1/...`)

> **TL;DR:** When FleetManager rejects or stalls on a config it just read,
> it writes a small "what went wrong" record to a sibling key under
> `/status/v1/...`. The orchestrator watches that prefix to see which
> intents need fixing or retrying.

**Topic:** `/status/v1/<original_config_path>/_error`
**Kind:** `STATUS_KIND_VALIDATION_ERROR` (independent enum from `ConfigKind`)
**Scope:** sibling to every `/config/v1/...` key
**Lifecycle owner:** FleetManager subscriber (writer)
**Subscriber:** orchestrator + operator tooling (reader)
**TTL:** 1 hour (etcd lease), refreshed on every re-emission

## Example

Original config path:
```
/config/v1/hosts/dpu-007/cont-vm-42/eth0/spec
```

Error sibling path:
```
/status/v1/hosts/dpu-007/cont-vm-42/eth0/spec/_error
```

Value:

```json
{
  "metadata": {
    "schema_version": 1,
    "kind": "STATUS_KIND_VALIDATION_ERROR",
    "revision": 7,
    "tenant_id": "tenant-acme-corp",
    "trace_context": { "trace_id": "...", "span_id": "...", "trace_flags": 1 },
    "payload_digest": "...",
    "issued_at": "2026-06-11T14:24:01.500Z",
    "producer_id": "fm-shard-0007-pod-1"
  },
  "payload": {
    "original_kind": "CONFIG_KIND_NIC_SPEC",
    "original_revision": 42,
    "phase": "REFERENCE_RESOLUTION",
    "code": "MISSING_ACL_GROUP",
    "severity": "WAITING",
    "message": "outbound.acl_v4[1] references acl-group 'acl-acme-subnet-prod' which is not present in cache",
    "details": {
      "missing_ref_path": "/config/v1/group/dpu-007/acl_group/acl-acme-subnet-prod/spec",
      "missing_ref_kind": "CONFIG_KIND_ACL_GROUP"
    },
    "first_seen_at": "2026-06-11T14:23:50.100Z",
    "retry_count": 3
  }
}
```

The orchestrator watches `/status/v1/` and reacts: for `WAITING` it
might publish the missing dependency; for `REJECTED` it surfaces to the
customer or rolls back the offending change request.

## Purpose

Per decision **#25**: FleetManager is a stream subscriber, not an RPC
server for validation. Rejections and waits must be **asynchronous and
discoverable**. Writing them to a deterministic sibling key keeps the
contract symmetric with the config tree and avoids a custom protocol.

The error is keyed by the *config path* it refers to, not by a UUID, so
the latest error per intent is always one read away. Replacement is
in-place; previous errors are overwritten when the next attempt produces
a new outcome.

## Fields (payload)

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `original_kind` | `ConfigKind` enum | yes | The kind of the config entry being validated when this error was raised. |
| `original_revision` | `uint64` | yes | The `metadata.revision` of the config entry that failed. Used by the orchestrator to know whether its latest write has been re-evaluated. |
| `phase` | `ValidationPhase` enum | yes | Where in the pipeline the failure occurred: `ENVELOPE`, `SCHEMA_DECODE`, `PAYLOAD_VALIDATION`, `REFERENCE_RESOLUTION`, `COMPOSITION`, `HAL_PROGRAMMING`. |
| `code` | `string` | yes | Stable machine-readable code (e.g. `MAC_COLLISION`, `MISSING_ACL_GROUP`, `UNSUPPORTED_SCHEMA`, `DIGEST_MISMATCH`). Per-`phase` namespace. |
| `severity` | `Severity` enum | yes | `WAITING` (will retry on dep change), `REJECTED` (terminal), `WARNING` (non-blocking, programming proceeded). |
| `message` | `string` | yes | Human-readable single-sentence summary. |
| `details` | `map<string,string>` | no | Structured context — e.g., the missing ref path/kind, conflicting MAC, oversize byte count. |
| `first_seen_at` | `Timestamp` | yes | First time this exact `(code, original_revision)` pair was observed. Useful for SLA / aging alerts. |
| `retry_count` | `uint32` | no | Number of times the subscriber re-evaluated this entry without success. Resets on `original_revision` change. |
| `attempt_trace_id` | `string` | no | Trace id of the most recent failed attempt. |

## Validation phases (and what produces them)

| Phase | Producer | Typical codes |
|-------|----------|---------------|
| `ENVELOPE` | Envelope unwrap (see `envelope.md` §Validation rules 1–6) | `UNSUPPORTED_SCHEMA`, `UNKNOWN_KIND`, `PAYLOAD_TOO_LARGE`, `DIGEST_MISMATCH`, `TENANT_MISMATCH` |
| `SCHEMA_DECODE` | Proto decode | `PROTO_DECODE_FAILED` |
| `PAYLOAD_VALIDATION` | Kind-specific structural rules | `INVALID_IP`, `BAD_ENUM`, `MAC_COLLISION`, `ARRAY_LENGTH_MISMATCH` |
| `REFERENCE_RESOLUTION` | Resolving foreign keys against cache | `MISSING_ACL_GROUP`, `MISSING_ROUTE_GROUP`, `MISSING_TUNNEL`, `MISSING_VNET` |
| `COMPOSITION` | NicGoalState composition | `MAPPING_INCOMPLETE`, `ROUTE_AND_RULE_CONFLICT` |
| `HAL_PROGRAMMING` | DPU programming attempt | `DEVICE_REJECTED`, `RPC_TIMEOUT`, `FENCE_TOKEN_STALE` |

## Severity semantics

- `WAITING` — The actor is in `WAITING_REFS` (or analogous). The error is
  informational; the system will retry automatically when the missing
  dependency arrives. Orchestrator may publish the dep.
- `REJECTED` — The actor refuses to program this revision. A new
  `original_revision` is required to clear it. Orchestrator must fix or
  roll back the source intent.
- `WARNING` — Programming succeeded but with a non-fatal anomaly (e.g.,
  PA validation recommended but absent). For visibility/alerting.

## Validation rules

1. `original_kind` must match the kind of the value at the parent path
   (sibling key relationship is enforced).
2. `code` must be non-empty and within the namespace defined for `phase`.
3. `severity` is required; `WAITING` errors must include the missing ref
   in `details.missing_ref_path` (so orchestrator can act).
4. `first_seen_at` is set by the producer on first emission of this
   `(code, original_revision)` and preserved across retries.

## Example: terminal rejection

```json
{
  "original_kind": "CONFIG_KIND_NIC_SPEC",
  "original_revision": 51,
  "phase": "PAYLOAD_VALIDATION",
  "code": "MAC_COLLISION",
  "severity": "REJECTED",
  "message": "mac_address aa:bb:cc:dd:ee:ff is already in use by eni ENI_dpu-007_aabbccddeeff on container cont-vm-17",
  "details": {
    "conflicting_eni_id": "ENI_dpu-007_aabbccddeeff",
    "conflicting_container_guid": "cont-vm-17"
  },
  "first_seen_at": "2026-06-11T14:24:00.000Z",
  "retry_count": 0
}
```

## Proto3 sketch

```proto
syntax = "proto3";
package fleetmanager.v1;

import "google/protobuf/timestamp.proto";

enum ValidationPhase {
  VALIDATION_PHASE_UNSPECIFIED         = 0;
  VALIDATION_PHASE_ENVELOPE            = 1;
  VALIDATION_PHASE_SCHEMA_DECODE       = 2;
  VALIDATION_PHASE_PAYLOAD_VALIDATION  = 3;
  VALIDATION_PHASE_REFERENCE_RESOLUTION = 4;
  VALIDATION_PHASE_COMPOSITION         = 5;
  VALIDATION_PHASE_HAL_PROGRAMMING     = 6;
}

enum Severity {
  SEVERITY_UNSPECIFIED = 0;
  SEVERITY_WAITING     = 1;
  SEVERITY_REJECTED    = 2;
  SEVERITY_WARNING     = 3;
}

message ValidationError {
  ConfigKind          original_kind     = 1;
  uint64              original_revision = 2;
  ValidationPhase     phase             = 3;
  string              code              = 4;
  Severity            severity          = 5;
  string              message           = 6;
  map<string,string>  details           = 7;
  google.protobuf.Timestamp first_seen_at  = 8;
  uint32              retry_count       = 9;
  string              attempt_trace_id  = 10;
}
```

## Relationships

- Sibling of: every `/config/v1/...` key (one error key per failing config).
- Wraps: nothing (its envelope is the standard `ConfigEntry`, with
  `kind=STATUS_KIND_VALIDATION_ERROR`).
- Referenced by: orchestrator's reconcile loop, operator dashboards.

## Change semantics

- Each subscriber re-emit overwrites the previous value at the same key
  (single revision per config intent).
- TTL of 1 hour ensures the key auto-evicts after the subscriber stops
  re-emitting (e.g., once the config is finally accepted or the dep
  arrives — the actor stops writing errors, the etcd lease expires).
- `WAITING` → `REJECTED` transition is a normal update (same key, new
  payload, new revision).
- Successful programming **does not** write a `success` value; absence
  of the error key is the success signal.

## See also

- [envelope](./envelope.md) — the wrapper this error rides inside.
- [README](./README.md) — full kind index and topic tree.