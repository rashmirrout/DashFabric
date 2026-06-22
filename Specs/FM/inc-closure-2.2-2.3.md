## INC 2.2 & INC 2.3 Closure: Slim HDO + NicSpec Proto3

**Status:** AUTHORITATIVE
**Owner:** FM Architecture
**Date:** 2026-06-22
**Supersedes:**
- `fleet-manager-lld.md` §1.1 — `HostDeviceState` struct (removed)
- `vm-eni-provisioning-design.md` §3 — informal NicSpec shape (replaced by proto3)

---

## 1. PURPOSE

Closes two outstanding inconsistencies from `ARCHITECTURE_REVIEW.md`:

- **INC 2.2 — HDO caching vs. Registry pattern conflict.** Earlier LLD draft had HostDeviceActor (HDO) holding its own `HostDeviceState` struct with per-device caches of VNETs, NICs, and mappings. This duplicated the registry layer and created two sources of truth.
- **INC 2.3 — NicSpec under-specified.** `nicgoalstate-schema-design.md` references `ReadFromT1(eni_id)` returning a `NicSpec`, but no proto3 definition existed for it. Composition algorithm cannot be deterministic without a schema.

This document is the single source of truth for both.

---

## 2. INC 2.2 RESOLUTION — SLIM HDO

### 2.1 Rule

> **HostDeviceActor MUST NOT hold object caches.** Its in-memory state is restricted to session/IO concerns — gNMI/SAI connection handle, device identity, lifecycle state machine pointer, and child-actor supervision metadata. All read access to VNET / NIC / Group / Mapping / Routing-Type data MUST go through the corresponding Registry `Acquire()` API.

### 2.2 Authoritative Struct: `DeviceIOState`

```go
// pkg/fm/actor/device_io_state.go
package actor

// DeviceIOState is the ONLY in-memory state the HostDeviceActor holds.
// It is session-scoped: discarded on actor restart, rebuilt from device handshake.
type DeviceIOState struct {
    DeviceID         string              // canonical device id
    Tenant           string              // owning tenant scope
    Endpoint         NetworkEndpoint     // IP:port + transport
    LifecycleState   DeviceLifecycle     // UNREGISTERED → REGISTERED → READY → QUARANTINED
    SessionHandle    DriverSession       // gNMI/SAI/Custom session (owned)
    LastHeartbeatAt  time.Time           // for liveness only
    DriverEpoch      int64               // bumps on session reconnect
    Children         []ContainerActorRef // child CO supervision (refs only, no state)
    Stop             chan struct{}       // shutdown signal
    Done             chan struct{}       // shutdown ack
}
```

### 2.3 What HDO MUST NOT Hold

| Forbidden Field | Where It Lives Instead |
|------------------|------------------------|
| `VnetData` / `VnetMap` | `VnetRegistry.Acquire(vnet_id)` |
| `NicSpec` cache | T1 watch in NicActor (per-NIC), HDO never touches |
| `RouteGroup`, `AclGroup`, `PrefixTag` | `GroupRegistry.Acquire(id)` from NicActor |
| `RoutingType` table | `GlobalRegistry.AcquireAll("routing_type")` |
| `MappingData` | `VnetMappingRegistry.Acquire(vnet_id)` |
| `HaScope` | `HaRegistry.Acquire(ha_scope_id)` |
| `MeterPolicy` | `GlobalRegistry.Acquire("meter_policy", id)` |
| Programmed-state mirror | `T3` (RocksDB) via `StorageAdapter`, NOT in-memory |

### 2.4 Read Path Contract

Any HDO method or child CO/NO that needs VNET / Group / Mapping / Global data **MUST**:

1. Call the relevant `Registry.Acquire(key)` once at construction or on dependency change.
2. Use `WaitReady()` if the caller is a NicActor at COMPOSING-state; HDO itself rarely blocks.
3. Release on actor stop or dependency change (refcount → 0 triggers eventual T1 watch shutdown).

Reads that bypass registries (direct T1 fetch, ad-hoc gRPC) are a CI lint failure (`fm-lint: NO_REGISTRY_BYPASS`).

### 2.5 Impact on Other Specs

| Spec | Change |
|------|--------|
| `fleet-manager-lld.md` §1.1 | Replace `HostDeviceState` block with reference to `DeviceIOState` here |
| `threading-model-design.md` §3 (P1 pool) | HDO worker memory budget drops to ~4 KB/device (was ~120 KB) |
| `recovery-and-failover-design.md` | HDO crash recovery is now session-rebuild only; no cache rehydration |
| `storage-architecture.md` §T3 | T3 is the only persistent device-side mirror; HDO never reads T3 directly except via `StorageAdapter` |

---

## 3. INC 2.3 RESOLUTION — NicSpec PROTO3

### 3.1 Rule

> NicSpec is the contract between ControlBroker (CB) and FM for "a NIC the operator wants programmed." It MUST be a proto3 message under `dashfabric.fm.v1`, watch-readable from T1 at key `/config/v1/nic/{eni_id}`, and the **only** input shape NicActor reads via `ReadFromT1(eni_id)`.

### 3.2 Authoritative Schema

```protobuf
syntax = "proto3";
package dashfabric.fm.v1;

import "google/protobuf/timestamp.proto";

// NicSpec is the operator-intent contract for a single ENI.
// Stored at T1 key:  /config/v1/nic/{eni_id}
// Owner of writes:   ControlBroker (CB)
// Owner of reads:    NicActor (NO) only
// Hash discipline:   NicActor MUST NOT include NicSpec in NicGoalState content_hash
//                    directly — only the *composed* derivatives flow into the hash.
message NicSpec {
  // ─── Identity ─────────────────────────────────────────────────
  string eni_id              = 1;  // canonical "ENI_<DPU>_<MAC>"; immutable
  string device_id           = 2;  // parent HDO routing key; immutable after assign
  string vnet_id             = 3;  // owning VNET; rebind not allowed
  string tenant_id           = 4;  // tenant scope; mirrors VNET tenant

  // ─── L2/L3 ─────────────────────────────────────────────────────
  string mac                 = 10; // "02:00:00:11:22:33" canonical lowercase hex
  string primary_ip          = 11; // CanonicalIP form (v4 or v6)
  uint32 vlan_id             = 12; // 0 = untagged

  // ─── Group References (resolved by NicActor through GroupRegistry) ─
  string route_group_id      = 20; // exactly one
  repeated string acl_group_ids = 21; // EXACTLY 6 entries; empty string = empty slot
                                      // slot order: [v4_in_s1, v4_out_s1,
                                      //              v6_in_s1, v6_out_s1,
                                      //              v4_in_s2, v4_out_s2]
  string meter_policy_id     = 22; // exactly one; may reference a default policy
  string ha_scope_id         = 23; // empty → standalone

  // ─── Lifecycle ────────────────────────────────────────────────
  NicAdminState admin_state  = 30; // ADMIN_UP | ADMIN_DOWN | ADMIN_QUARANTINED
  google.protobuf.Timestamp created_at = 31;
  google.protobuf.Timestamp updated_at = 32;
  int64  spec_revision       = 33;  // monotonic per (eni_id); supplied by CB

  // ─── Audit ────────────────────────────────────────────────────
  string created_by          = 90;  // CB principal SVID
  string change_request_id   = 91;  // links back to CB change object
  string spec_hash           = 92;  // CB-computed SHA256 over fields 1..33 (audit only)
}

enum NicAdminState {
  NIC_ADMIN_STATE_UNSPECIFIED = 0;
  NIC_ADMIN_STATE_UP          = 1;
  NIC_ADMIN_STATE_DOWN        = 2;
  NIC_ADMIN_STATE_QUARANTINED = 3;
}
```

### 3.3 Validation Rules (NicActor-side, on every read)

| # | Rule | Failure → State |
|---|------|-----------------|
| 1 | `eni_id`, `device_id`, `vnet_id`, `tenant_id` non-empty | VALIDATION_REJECTED |
| 2 | `mac` matches `^[0-9a-f]{2}(:[0-9a-f]{2}){5}$` | VALIDATION_REJECTED |
| 3 | `primary_ip` parses to canonical IPv4 or IPv6 | VALIDATION_REJECTED |
| 4 | `acl_group_ids.size() == 6` (exactly) | VALIDATION_REJECTED |
| 5 | `route_group_id` non-empty | VALIDATION_REJECTED |
| 6 | `meter_policy_id` non-empty (default ID allowed) | VALIDATION_REJECTED |
| 7 | `admin_state != UNSPECIFIED` | VALIDATION_REJECTED |
| 8 | `spec_revision` strictly greater than last seen (per eni_id) | reject write, retain prior |
| 9 | Tenant on referenced `vnet_id` matches `tenant_id` | VALIDATION_REJECTED |

Validation failures publish to `quarantine/nic/{eni_id}` and emit `VAL_001_NIC_SPEC_REJECTED` (NOT in the CRITICAL set; tooling-recoverable).

### 3.4 NicSpec → NicGoalState Composition Inputs

Refers to `nicgoalstate-schema-design.md` §3.1 table. This document fills in the previously-implied row:

| Input | Source | Acquire Key | Required? |
|-------|--------|-------------|-----------|
| **NicSpec** | **T1 watch** | **`/config/v1/nic/{eni_id}`** | **Yes — composition aborts if missing** |

All other rows in that table are unchanged.

### 3.5 Lifecycle Coupling

| NicSpec change | NicActor response |
|----------------|-------------------|
| First write (new eni_id) | Spawn NicActor under correct HDO/CO; state = WAITING_REFS |
| Field-only change (acl_group_ids, etc.) | Re-compose; if `content_hash` changed → emit new DeltaPlan |
| `admin_state` = DOWN | Drain flows, transition to ADMIN_DOWN, withdraw VIP bindings |
| `admin_state` = QUARANTINED | Withdraw all programmed state; freeze actor; emit audit |
| Delete (T1 key gone) | Standard tombstone path; release all registry refs |

### 3.6 Forbidden Edits (CB MUST reject)

- `eni_id`, `device_id`, `vnet_id`, `tenant_id`, `mac` after first write
- `acl_group_ids.size()` ≠ 6
- Decreasing `spec_revision`

Enforced both at CB write-validation and FM read-validation (defense in depth).

---

## 4. CROSS-REFERENCES TO UPDATE

| File | Action |
|------|--------|
| `fleet-manager-lld.md` | Replace HostDeviceState struct with link to §2.2 here |
| `nicgoalstate-schema-design.md` §3.1 | Insert §3.4 row reference |
| `vm-eni-provisioning-design.md` §3 | Replace informal NicSpec pseudo-shape with link to §3.2 here |
| `registry-pattern-design.md` | Add HDO-bypass-forbidden lint reference |
| `MASTER_DESIGN_INDEX.md` | Add this doc under "Closures" section |

---

## 5. ACCEPTANCE CRITERIA

1. ✅ HDO `DeviceIOState` struct contains no object caches
2. ✅ `fm-lint NO_REGISTRY_BYPASS` rule lands in CI
3. ✅ Proto3 NicSpec compiles cleanly with `protoc --go_out`
4. ✅ Validation rules 1–9 implemented as a single `ValidateNicSpec(spec) error`
5. ✅ Round-trip test: T1 write → NicActor parse → revalidate → identical bytes
6. ✅ All forbidden-edit cases produce VAL_001 in both CB and FM logs
7. ✅ `ARCHITECTURE_REVIEW.md` INC 2.2 and INC 2.3 marked RESOLVED with link to this doc

---

## 6. REFERENCES

- `ARCHITECTURE_REVIEW.md` §INC 2.2, §INC 2.3 — original gap descriptions
- `nicgoalstate-schema-design.md` — downstream consumer of NicSpec
- `registry-pattern-design.md` — Acquire/Release semantics enforced by INC 2.2
- `threading-model-design.md` §3 — revised HDO memory budget
- `error-handling-design.md` — VAL_001 classification
