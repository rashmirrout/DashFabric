# MASTER DESIGN INDEX — Canonical Registry for FleetManager

**Status:** AUTHORITATIVE — Single Source of Truth for All Named Entities
**Owner:** FM Architecture
**Date:** 2026-06-22

---

## 1. PURPOSE

This is the **stitching document** that prevents drift between specs. Every named entity (pool, lock, state, metric, error code, wave, etc.) that appears in any FM spec MUST appear in the corresponding registry here. Specs reference this document; this document is the authority.

**Implementation rule:** If you see a name in code or a spec that is NOT in this registry, it is a bug — either the name is wrong, or this document is out of date. Update one or the other.

**Reading order for implementers:**
1. This document (orientation: names + cross-refs)
2. Spec for the layer you're implementing (e.g., `nicgoalstate-schema-design.md`)
3. Adjacent specs flagged in §11 (cross-spec dependencies)
4. `ARCHITECTURE_REVIEW.md` (historical context — why we wrote these specs)

---

## 2. AUTHORITATIVE SPEC INVENTORY

These are the only documents with binding design decisions. Anything contradicting them is wrong.

| # | Spec | GAP/INC Resolved | Owns |
|---|------|------------------|------|
| 1 | `nicgoalstate-schema-design.md` | GAP 1.2 | NicGoalState proto, content-hash algorithm |
| 2 | `registry-semantics-exact.md` | GAP 1.5 | Acquire/Release/Ready contract, registry locks |
| 3 | `southbound-driver-interface-redesign.md` | GAP 1.4 | Driver interface, wave execution, ApplyDeltaPlan |
| 4 | `adapter-protocol-design.md` | GAP 1.7 | Adapter ↔ CB protocol, watermarks, idempotency |
| 5 | `device-lifecycle-design.md` | GAP 1.3 + INC 2.1 | HDO state machine, REST status mapping |
| 6 | `threading-model-design.md` | GAP 1.1 | Pool inventory, lock order, backpressure |
| 7 | `reconciliation-design.md` | GAP 1.6 | Drift detection, classification, recovery |
| 8 | `error-handling-design.md` | INC 2.4 | Error catalog, SLO budgets, escalation |
| 9 | `security-design.md` | (Track B) | Trust domains, mTLS, RBAC/ABAC, secrets, audit, threat model |
| 10 | `inc-closure-2.2-2.3.md` | INC 2.2, INC 2.3 | Slim `DeviceIOState`; proto3 `NicSpec` schema |
| 11 | `Specs/Runbooks/README.md` + 13 per-code files | error-handling §9.2 mandate | Operator runbooks for every CRITICAL error code |
| 12 | **`MASTER_DESIGN_INDEX.md`** (this) | — | Cross-spec canonical registries |

Pre-existing HLD/LLD docs (e.g., `fleet-manager-hld.md`, `fleet-manager-lld.md`) are NOW reference material. Where they conflict with specs 1-11, the specs win. Migration markers in each spec's §11/§12.

Specifically: `fleet-manager-lld.md` §1.1 `HostDeviceState` struct is superseded by `inc-closure-2.2-2.3.md` §2.2 (`DeviceIOState`). `vm-eni-provisioning-design.md` §3 NicSpec shape is superseded by `inc-closure-2.2-2.3.md` §3.2.

---

## 3. POOL REGISTRY (P1–P10)

Source: `threading-model-design.md` §3.

| ID | Name | Type | Size | Owning Spec |
|----|------|------|------|-------------|
| P1 | Actor Executor | goroutine-per-actor | 4k–32k (autoscale) | threading §4 |
| P2 | Southbound RPC | bounded worker pool | 16/device, 8k cap | threading §5 |
| P3 | Registry Watchers | one-per-key | ≤ 50k | threading §6 |
| P4 | Adapter Stream Processors | one-per-CB | ≤ 8 | threading §7 |
| P5 | T1/T2 Client Workers | gRPC conn pool | 32 conns | threading §3 |
| P6 | T3 RocksDB | RocksDB internal | 8 | threading §3 |
| P7 | Reconciliation Sweeper | bounded scheduler | 4 | threading §8, reconciliation §3.3 |
| P8 | HTTP/gRPC Server | std server | 256 max | threading §3 |
| P9 | Metrics Exporter | single goroutine | 1 | threading §3 |
| P10 | Health/Lease Renewer | single goroutine | 1/role | threading §3 |

**Forbidden:** Spawning goroutines outside this registry. Reviewers reject PRs that do.

---

## 4. LOCK REGISTRY (L1–L8) + LOCK ORDER

Source: `threading-model-design.md` §10.

| ID | Lock | Scope | Type | Owning Spec |
|----|------|-------|------|-------------|
| L1 | REGISTRY_LOCK (per-registry) | Per registry instance | sync.RWMutex | registry §8 |
| L2 | WATCH_LOCK (per-key, per-registry) | Per watched key | sync.Mutex | registry §8 |
| L3 | ACTOR_MAILBOX | Per actor | channel (not a lock) | threading §10.3 |
| L4 | HDO_STATE | Per HDO actor | sync.RWMutex | device-lifecycle §3 |
| L5 | NIC_STATE | Per NicActor | sync.RWMutex | device-lifecycle §5.1 |
| L6 | DRIVER_CONN | Per driver instance | sync.Mutex | southbound §8 |
| L7 | ADAPTER_LEASE | Per adapter pod | etcd lease | adapter §4 |
| L8 | T2_TXN | Per adapter pod | etcd transaction | adapter §6 |

### 4.1 MANDATORY LOCK ORDER

```
L1 (REGISTRY_LOCK)
  ↓
L2 (WATCH_LOCK)
  ↓
L4 (HDO_STATE)
  ↓
L5 (NIC_STATE)
  ↓
L6 (DRIVER_CONN)
```

L7, L8 are independent (single-acquisition contexts; not nested with the above).

**Critical rules:**
- Never hold L1 while making outbound RPCs (release before IO)
- Never send to a channel (L3) while holding any lock from {L1, L2, L4, L5, L6}
- L8 is short-lived; never held across goroutine boundaries

---

## 5. ERROR CODE REGISTRY

Source: `error-handling-design.md` §3.

**Format:** `<LAYER>_<NUM>_<KIND>`. Layers:

| Prefix | Layer | Code Range |
|--------|-------|------------|
| API_ | API Gateway | 001–099 |
| REG_ | Registry | 001–099 |
| ACT_ | Actor framework | 001–099 |
| DRV_ | Southbound Driver | 001–099 |
| ADP_ | Adapter | 001–099 |
| STO_ | Storage (T1/T2/T3) | 001–099 |
| REC_ | Reconciliation | 001–099 |
| HA_ | HA layer | 001–099 |

See `error-handling-design.md` §3 for the full catalog (currently 56 codes). All emit sites in code MUST reference a code from that catalog — no free-form strings.

**Adding new codes:** Process in `error-handling-design.md` §13.1.

---

## 6. STATE NAME REGISTRY

### 6.1 HDO Actor States

Source: `device-lifecycle-design.md` §3.

```
INITIALIZING → WAITING_BOOTSTRAP → HYDRATING_DEV → READY
                                                     ├→ DISCONNECTED → READY (on reconnect)
                                                     ├→ DEGRADED
                                                     └→ DRAINING → DEREGISTERED (terminal)
```

| State | Outbound Actions Allowed? |
|-------|-------------------------|
| INITIALIZING | None |
| WAITING_BOOTSTRAP | None |
| HYDRATING_DEV | None |
| READY | All |
| DISCONNECTED | None (NICs in WAITING_DEVICE) |
| DEGRADED | Limited (no new ENIs) |
| DRAINING | DELETE only |
| DEREGISTERED | — (terminal) |

### 6.2 NicActor States

Source: `device-lifecycle-design.md` §5.1.

```
WAITING_PARENT → WAITING_REFS → COMPOSING → PROGRAMMING → READY
                                                            ├→ COMPOSING (recompose on change)
                                                            ├→ WAITING_PARENT (parent !ready)
                                                            └→ DELETING (parent draining)
```

Special terminal states:
- `QUARANTINED` — operator review required (from `error-handling-design.md` §4.3)
- `VALIDATION_REJECTED` — driver permanently refused payload
- `THROTTLED` — driver returned RESOURCE_EXHAUSTED; retry on backoff

### 6.3 Adapter Leader States

Source: `adapter-protocol-design.md` §4.

```
ELECTING → LEADER → (lease loss) → ELECTING
         ↘ FOLLOWER
```

### 6.4 Driver Connection States

Source: `southbound-driver-interface-redesign.md` §8.

```
DISCONNECTED → CONNECTING → CONNECTED → RECONNECTING → CONNECTED
                                                       └→ FAILED (after 5 attempts)
```

### 6.5 Reconciliation Per-ENI States

Source: `reconciliation-design.md` §6.

```
IDLE → SAMPLING → CONFIRMING → CLASSIFYING → REMEDIATING → IDLE (or QUARANTINED)
```

---

## 7. WAVE ASSIGNMENT REGISTRY

Source: `nicgoalstate-schema-design.md` §7, enforced by `southbound-driver-interface-redesign.md` §4.

| Wave | Object Types | Dependency |
|------|--------------|------------|
| 0 | VRF, VLAN | Foundational namespaces |
| 1 | Identity (MAC/IP/VLAN bind) | ENI identity binding |
| 2 | MeterPolicy | Required by flow programming |
| 3 | Routes | Forwarding state |
| 4 | AclStage (Stage 1 / pre-routing) | Inbound policy |
| 5 | AclStage (Stage 2 / post-routing) | Outbound policy |
| 6 | VipMappings, HA links | Depend on routes + ACLs |

**Apply order:** 0 → 1 → 2 → 3 → 4 → 5 → 6
**Delete order:** 6 → 5 → 4 → 3 → 2 → 1 → 0

**Forbidden:** Reordering across waves; skipping waves.

---

## 8. METRIC NAMESPACE REGISTRY

All FM metrics live under prefix `fm_`. Sub-namespaces by component:

| Prefix | Owner | Examples |
|--------|-------|----------|
| `fm_pool_` | threading | `fm_pool_goroutines{pool}` |
| `fm_actor_` | threading | `fm_actor_mailbox_dropped_total{class}` |
| `fm_lock_` | threading | `fm_lock_wait_duration_ms{lock_id}` |
| `fm_registry_` | registry | `fm_registry_acquire_total{registry}` |
| `fm_hdo_` | device-lifecycle | `fm_hdo_state{device_id, state}` |
| `fm_nic_` | device-lifecycle | `fm_nic_compose_duration_ms` |
| `fm_sbd_` | southbound | `fm_sbd_apply_plan_total` |
| `fm_adapter_` | adapter | `fm_adapter_events_received_total` |
| `fm_reconcile_` | reconciliation | `fm_reconcile_drift_detected_total` |
| `fm_errors_` | error-handling | `fm_errors_total{code, severity, layer, ...}` |
| `fm_admission_` | error-handling | `fm_admission_rejected_total{reason}` |
| `fm_dlq_` | error-handling | `fm_dlq_depth{dlq}` |
| `fm_gc_` | runtime | `fm_gc_heap_inuse_bytes` |

**Forbidden:** Metrics outside `fm_` prefix in FM code. Metrics that don't belong to a sub-namespace get reviewed and assigned.

---

## 9. T1 / T2 / T3 KEY LAYOUT

Single source of truth for storage paths. Per-spec details retained in each spec.

### 9.1 T1 (fm-data-store) — Authoritative Configuration

```
/config/v1/global/**                — Global config (Vnet routing tables, etc.)
/config/v1/group/{group_id}         — Group-level config
/config/v1/vnet/{vnet_id}/spec      — VnetSpec
/config/v1/vnet/{vnet_id}/mapping/{prefix} — VnetMapping (chunked)
/config/v1/ha/{ha_scope_id}/spec    — HaScope spec
/config/v1/hosts/{device_id}/**     — Device-specific config (NIC manifests)
```

### 9.2 T2 (fm-cluster-state) — Coordination & Audit

```
/fm/v1/lease/adapter                            — Adapter leader lease (adapter §3.1)
/fm/v1/lease/fm-pod/{shard_id}                  — FM pod primary lease (reconcile §7.2)
/fm/v1/watermarks/adapter/{cb_endpoint_id}      — Adapter watermarks (adapter §3.1)
/fm/v1/dlq/adapter/{event_id}                   — Adapter DLQ (adapter §8)
/fm/v1/dlq/compose/{eni_id}                     — Compose DLQ (error-handling §6.1)
/fm/v1/dlq/driver/{eni_id}                      — Driver DLQ
/fm/v1/dlq/reconcile/{eni_id}                   — Reconcile DLQ
/fm/v1/idempotency/adapter/{cb_event_id}        — Adapter idempotency (adapter §3.1)
/fm/v1/applied_plans/{eni_id}/{content_hash}    — Last-applied plans (southbound §6.2)
/fm/v1/devices/{device_id}/status               — Device status (device-lifecycle §6.3)
/fm/v1/reconcile/{eni_id}                       — Reconcile state (reconcile §3.4)
/fm/v1/ha_events/{ha_scope_id}                  — HA failover history (reconcile §8.2)
/fm/v1/quarantine/{entity_id}                   — Quarantine markers (error-handling §4.3)
```

### 9.3 T3 (Local RocksDB) — Per-Pod Cache

Local-only; no canonical keys (used as opaque cache by registry layer per `registry-semantics-exact.md`).

---

## 10. PROTO/MESSAGE REGISTRY

Top-level messages and their owning spec:

| Message | Owning Spec |
|---------|-------------|
| `NicGoalState`, `NicIdentity`, `RouteEntry`, `AclStage`, `VipMapping`, `MeterPolicy`, `HaConfig` | nicgoalstate-schema §3 |
| `Subscription` (Acquire result) | registry-semantics §4 |
| `DeltaPlan`, `DeltaCommand`, `ApplyResult`, `CommandResult`, `ExecutionPolicy` | southbound-driver §3 |
| `AdapterWatermark`, `IdempotencyRecord`, `StateAck`, `DlqEntry` (adapter) | adapter-protocol §3 |
| `Device` (REST) | device-lifecycle §4.1 |
| `ReconcileEvent` | reconciliation §9.1 |
| `DlqEntry` (generic) | error-handling §6.2 |

**Rule:** Proto definitions live in `Specs/FM/proto/`. Any code generating proto bindings reads from there.

---

## 11. CROSS-SPEC DEPENDENCY MATRIX

Read this to understand which specs interact. Cells = "spec X depends on spec Y's contracts."

| Depends on → | nicgoal | registry | southbound | adapter | device-lc | threading | reconcile | error |
|---|---|---|---|---|---|---|---|---|
| **nicgoal** | — | acquires from | wave assignment | — | — | — | — | — |
| **registry** | — | — | — | — | — | locks L1/L2 | — | REG_* codes |
| **southbound** | hash, waves | — | — | — | — | pool P2 | hash RPCs | DRV_* codes |
| **adapter** | hash dedup | — | — | — | — | pool P4, L7/L8 | — | ADP_* codes |
| **device-lc** | — | acquires | conn lifecycle | events from | — | actor states | drift triggers | error states |
| **threading** | — | locks | pool P2 | pool P4 | actor mailboxes | — | pool P7 | admission rules |
| **reconcile** | hash semantics | — | hash RPC | leader hint | HDO states | pool P7 | — | REC_* codes |
| **error** | — | REG_* | DRV_* | ADP_* | quarantine | admission | REC_* | — |

If you change spec X, check every spec that depends on X.

---

## 12. NUMERICAL CONSTANTS REGISTRY

Values that appear in multiple specs MUST agree. Source of truth here.

| Constant | Value | Source Spec | Used By |
|----------|-------|------------|---------|
| Devices per pod (shard target) | 500 | threading §2 | All sizing |
| ENIs per device avg | 16 | threading §2 | Registry, threading |
| ENIs per device P99 | 64 | threading §2 | Registry, threading |
| Max NicActors per pod (hard cap) | 50,000 | threading §4.4 | Admission control |
| Soft cap warning | 40,000 | threading §4.4 | Admission |
| Admission throttle threshold | 45,000 | error §10 | API gateway |
| Adapter lease TTL | 15s | adapter §4.1 | Adapter, reconcile |
| Adapter lease renewal | 5s | adapter §4.1 | Adapter |
| FM pod lease TTL | 15s | reconcile §7.2 | HA |
| Idempotency TTL | 24h | adapter §3.2 | Adapter |
| Adapter DLQ TTL | 7 days | adapter §8.2 | Adapter |
| Compose/Driver DLQ TTL | 24h | error §6.1 | DLQ |
| Reconcile DLQ TTL | 7 days | error §6.1 | Sweeper |
| Per-RPC default timeout | 5s | southbound §3 ExecutionPolicy | Driver |
| Hash-read RPC timeout | 2s | reconcile §4.1 | Sweeper |
| Wave max parallel RPCs | 16 | southbound §3 ExecutionPolicy | Driver |
| Per-device driver queue | 256 | threading §5.3 | Driver |
| Mailbox NicActor | 32 | threading §4.3 | Actor |
| Mailbox HostDeviceActor | 64 | threading §4.3 | Actor |
| Mailbox Assembler | 128 | threading §4.3 | Registry |
| Reconcile cadence default | 60s | reconcile §3.2 | Sweeper |
| Reconcile cadence recent | 15s | reconcile §3.4 | Sweeper |
| Reconcile cadence quarantine | 300s | reconcile §3.4 | Sweeper |
| Reconcile drift confirm wait | 5s | reconcile §5.1 | Sweeper |
| Bootstrap timeout | 30s | device-lc §6.1 | HDO |
| Graceful shutdown budget | 60s | threading §12.2 | Supervisor |
| In-flight drain budget | 30s | threading §12.2 | NicActors |
| Device heartbeat fail count | 3 | device-lc §6.4 | HDO |
| Driver reconnect attempts | 5 | southbound §8 | Driver |
| Driver reconnect backoff | 1s → 10s cap, 10% jitter | southbound §8 | Driver |
| Registry release grace (default) | 30s | registry §5 | Registry |
| GlobalRegistry release grace | 5 min | registry §6 | GlobalRegistry |
| SLO: ENI program success | ≥ 99.9% | error §5.1 | SLO |
| SLO: API availability | ≥ 99.95% | error §5.1 | SLO |
| SLO: Drift detection P99 | ≤ 90s | reconcile §3.1 | Sweeper, SLO |
| SLO: NIC ready-after-create P99 | ≤ 5s | error §5.1 | SLO |
| Pod memory steady budget | 2.6 GiB | threading §13 | Sizing |
| Pod memory peak budget | 6 GiB | threading §13 | Sizing |
| Pod memory hard cap | 8 GiB | threading §13 | OOM guard |

**Rule:** Changing any value here = breaking change. Requires owner approval + cross-spec update in same PR.

---

## 13. NAMING CONVENTIONS

### 13.1 Identifiers

| Kind | Format | Example |
|------|--------|---------|
| Device ID | `dev-<uuid-or-hostname>` | `dev-abc123` |
| ENI ID | `eni-<uuid>` | `eni-7f3e9b2a` |
| VNET ID | `vnet-<uuid>` | `vnet-prod-east-1` |
| Group ID | `grp-<purpose>-<id>` | `grp-payments-01` |
| HA scope ID | `ha-<uuid>` | `ha-pair-42` |
| Shard / FM pod ID | `fm-pod-<num>` | `fm-pod-3` |
| CB endpoint ID | `cb-<vendor>-<purpose>` | `cb-intel-dpu` |
| Content hash | hex SHA-256 (64 chars) | `a3f4...` |

### 13.2 Metric Labels

Always-lowercase, snake_case. Cardinality budget: each metric < 100k unique label sets.

| Label | Cardinality Risk |
|-------|-----------------|
| `eni_id` | HIGH — avoid as label; use sampling |
| `device_id` | MED — OK for HDO metrics (500/pod) |
| `actor_class` | LOW — 4 values (HDO, CO, NIC, ASSEMBLER) |
| `pool` | LOW — 10 values (P1–P10) |
| `code` | MED — ~60 codes |
| `state` | LOW per state machine |

### 13.3 Code

- Pool IDs: ALL CAPS comments + constant in `internal/pools/registry.go`
- Lock IDs: same; constants in `internal/sync/locks.go`
- Error codes: constants in `internal/errors/catalog.go`
- States: enum per state machine, also in catalog file

---

## 14. AUDIT TRAIL REQUIREMENTS

Every action visible in T2 must be auditable. Common fields:

```protobuf
message AuditEnvelope {
  string actor_pod_id = 1;        // Which pod did this
  int64  lease_term = 2;          // Adapter/FM lease term
  string trace_id = 3;            // For correlation
  string user_or_system = 4;      // "system" | operator email
  google.protobuf.Timestamp at = 5;
  string reason = 6;              // "reconcile" | "compose-change" | "operator-cli" | ...
}
```

Plans (T2 `/fm/v1/applied_plans/`) and quarantine markers MUST include this envelope.

---

## 15. SECURITY BOUNDARIES (REFERENCE)

Full details: `security-design.md`. This section is a quick-lookup mirror — the linked spec is authoritative.

| Boundary | Auth | Notes |
|----------|------|-------|
| Operator → API gateway | mTLS + OIDC (JWT) | OIDC provider per env; SVID for service accounts |
| FM ↔ CB | mTLS (SPIFFE SVID) | CB plugin signs events; events traceable to SVID |
| FM ↔ Device (driver) | mTLS or device-native | X.509 device certs issued at enrollment (single-use token) |
| FM ↔ T1/T2 (etcd) | mTLS (SPIFFE SVID) | Mutual auth; cert rotation via SPIRE |
| Inter-pod (HA / gossip) | mTLS | Cluster-internal CA; TLS 1.3 minimum |
| Operator/system writes | OPA-evaluated RBAC + ABAC | Tenant scope, change-window claims; audit-hash-chained |

See `security-design.md` for: trust domains (§2), identity model (§3), transport rules (§4), RBAC/ABAC (§5), secrets handling (§6), audit-log chaining (§7), threat model (§8), hardening checklist (§9), incident hooks (§10).

---

## 16. CHECKLIST: BEFORE WRITING CODE

For any module you implement, verify:

- [ ] Module's spec exists in §2; you've read it end-to-end
- [ ] Every goroutine you spawn maps to a pool in §3
- [ ] Every mutex you take maps to a lock in §4, acquired in §4.1 order
- [ ] Every error you emit maps to a code in §5
- [ ] Every state name you introduce maps to a state in §6
- [ ] Every metric you emit follows §8 prefix
- [ ] Every constant you introduce: not duplicated; if shared, added to §12
- [ ] Every T1/T2 path you read/write: registered in §9
- [ ] You've consulted §11 dependency matrix for which other specs you must respect

If any item fails, fix the discrepancy (spec or code) before merging.

---

## 17. CHECKLIST: BEFORE WRITING A NEW SPEC

- [ ] Resolves a numbered gap or inconsistency, or fills a clearly identified hole
- [ ] Listed in §2 with GAP/INC reference and "Owns" summary
- [ ] All new names registered in §3–§9
- [ ] All new numbers registered in §12
- [ ] Cross-spec dependencies declared in §11 matrix
- [ ] Has Status, Owner, Date, Supersedes header
- [ ] Has §X References section pointing back to other specs
- [ ] Has Test Matrix section with at least 8 named test cases
- [ ] Has Metrics section listing required gauges/counters

---

## 18. EVOLUTION & REVIEW PROCESS

1. Author updates this index in the same PR as their spec change
2. Reviewer cross-checks: every new name in spec appears here
3. Conflicts with existing entries → resolved before merge (rename one)
4. Numerical constant changes → flagged for SLO impact analysis
5. Deprecated entries: marked `DEPRECATED` for 90 days, then removed
6. Quarterly review: stale specs flagged, removed, or marked archival

---

## 19. APPENDIX: GLOSSARY (TOP 30 TERMS)

| Term | Definition |
|------|------------|
| **HDO** | HostDeviceActor: per-device top-level actor |
| **CO** | ContainerActor: per-container child of HDO |
| **NicActor / NO** | per-ENI child actor |
| **Wave** | Topological dependency level (0-6) for object programming |
| **ENI** | Elastic Network Interface — per-NIC config unit |
| **Content hash** | SHA-256 of canonical proto3 of a NicGoalState |
| **Acquire / Release** | Refcounted registry subscription primitive |
| **Ready channel** | Per-Acquire signal: hydrated and safe to consume |
| **Compose** | NicActor's process of building NicGoalState from inputs |
| **Wave-aware driver** | Driver that internally enforces wave order |
| **DeltaPlan** | Pre-sorted command list passed to driver |
| **CAS** | Compare-And-Swap (revision-based etcd write) |
| **Watermark** | Adapter's last-processed CB event ID per endpoint |
| **DLQ** | Dead Letter Queue — unprocessable items |
| **Quarantine** | Entity flagged for operator review, no auto-recovery |
| **CB** | ControllerBridge — out-of-process plugin emitting events to FM |
| **Adapter** | FM-side translator from CB events to T1 writes |
| **T1** | fm-data-store: authoritative configuration |
| **T2** | fm-cluster-state: coordination, leases, watermarks |
| **T3** | Local RocksDB cache per FM pod |
| **Shard** | Subset of devices owned by one FM pod |
| **Lease** | etcd-backed leadership token with TTL |
| **Idempotency record** | T2 entry mapping CB event ID to applied result |
| **Mailbox** | Per-actor buffered channel |
| **Drop oldest** | Backpressure policy for idempotent update streams |
| **Drift** | Device state diverged from FM's intent |
| **Reconciliation** | Periodic check of device state vs. FM intent |
| **VRF / VLAN** | Foundational namespace objects (wave 0) |
| **HA pair** | Two ENIs sharing state via DASH HA |
| **Pod (FM pod)** | One Kubernetes pod running an FM instance |

---

## 20. SIGN-OFF

Implementation may begin once:

- [x] All eight core specs (§2 rows 1–8) reviewed and approved
- [x] Security spec (`security-design.md`) written and cross-linked
- [x] INC 2.2 / INC 2.3 closed (`inc-closure-2.2-2.3.md`)
- [x] This master index reviewed for completeness
- [x] Operator runbooks exist for every CRITICAL error code (`Specs/Runbooks/`)
- [ ] `ARCHITECTURE_REVIEW.md` conclusion reflects resolved status (Track B sweep — final step)
- [ ] Test matrices from each spec are tracked in test-plan repo

**This document is the implementation contract.** Code that contradicts it without explicit waiver is rejected.
