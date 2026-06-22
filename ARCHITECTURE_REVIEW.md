# FleetManager Architecture Review & Robustness Analysis

**Date:** 2026-06-21  
**Status:** COMPREHENSIVE DESIGN AUDIT  
**Scope:** All design documents in Specs/FM/, Specs/CB/, and implementation alignment

---

## EXECUTIVE SUMMARY

The FM architecture is **well-founded but has critical gaps and inconsistencies** that will cause implementation friction:

1. ✅ **Strengths**: Hierarchical actor model, registry pattern, three-tier storage, CB plugin separation
2. ⚠️ **Gaps**: Thread model underspecified, NicGoalState schema incomplete, error handling scattered
3. ❌ **Inconsistencies**: Device registration flow vs. lifecycle design, wave ordering vs. HAL interface, reconciliation semantics unclear
4. **Risk Level**: **HIGH** — without resolution, AI code generation will produce incompatible implementations

**Recommendation**: Complete this review before any implementation. Each gap below has remediation steps.

---

## PART 1: ARCHITECTURAL GAPS

### GAP 1.1: Thread Model Completely Unspecified

**Current State:**
- HLD §6.2 lists 5 thread pools but no sizes, queue depths, or scaling policies
- No mention of actor scheduling strategy (coroutines vs. OS threads vs. thread-per-actor)
- LLD doesn't specify synchronization primitives for registries
- No guidance on backpressure between layers (adapter → registries → actors → HAL driver)

**Impact:**
- Implementation will guess at pool sizing (risk of deadlock or resource exhaustion)
- No coordinated backpressure → queues unbounded → OOM on burst
- Registry lock contention unknown → can't predict scaling

**Remediation:**
Create `Specs/FM/threading-model-design.md`:
- Actor executor pool size: K=16 (tunable), autoscale 8-32 on queue depth
- Registry lock strategy: fine-grained per-key RWMutex + per-registry lock for refcount updates
- Backpressure: buffered channels (capacity=10k between layers); Adapter → T1 writer enforces rate
- Memory budgets: T3 RocksDB max 10GB, in-mem registry max 5GB, actor pools max 100 tasks each

---

### GAP 1.2: NicGoalState Schema Incomplete

**Current State:**
- vm-eni-provisioning-design.md §3 defines shape in pseudocode ("proposed shape — not yet a proto")
- Missing: exact proto3 schema, composition algorithm pseudocode, content-hash algorithm
- Unclear: how prefix_tag expansion works, route merge semantics, multi-stage ACL layout
- No mention: how RoutingType templates are applied to routes

**Impact:**
- Implementer will invent schema → incompatible goal state → devices fail to program
- Composition bugs hard to detect (runs locally, visible only on device mismatch)
- Content-hash collisions possible if algorithm undefined

**Remediation:**
Create `Specs/FM/nicgoalstate-schema-design.md`:
- Proto3 schema for NicGoalState (with all nested types)
- Composition algorithm in pseudocode (including prefix_tag expansion)
- Content-hash algorithm: canonical JSON → SHA256 (exact canonicalization rules)
- Routing template application: RouteEntry references RoutingType by id; template fills in transform op
- ACL layout: 6 slots per family (3 stages × 2 directions) + stage ordering guarantees

---

### GAP 1.3: Device Lifecycle Inconsistency

**Current State:**
- Device registration flow (fm-device-registration-flow-design.md) says REST-only entry point
- Device state machine (§6) lists states: WAITING_BOOTSTRAP, HYDRATING, READY_FOR_ENI, PROGRAMMING, READY
- But: Actor (HDO) state machine (fleet-manager-lld.md §1.1) has NO WAITING_HYDRATION state — goes directly INITIALIZING → WAITING_BOOTSTRAP
- Ambiguous: when does HDO transition WAITING_BOOTSTRAP → READY? On first global/group cache complete, or on first NIC arrival?

**Impact:**
- Implementation will sync state machine vs. REST response states → mismatch
- Multiple devices registering simultaneously → ambiguous when pool begins programming

**Remediation:**
Clarify `Specs/FM/device-lifecycle-design.md` (new doc):
- Device REST response: shard_id, subscription_topics, status="REGISTERED"
- HDO state (internal): INITIALIZING → (subscribe /global, /group) → WAITING_BOOTSTRAP → (cache hydrated) → READY
- NIC can only program after HDO::READY (enforced by NicActor: parents_ready() check)
- Device "READY_FOR_ENI" in REST response happens when HDO::READY (one-time transition)

---

### GAP 1.4: Wave Ordering vs. HAL Interface Mismatch

**Current State:**
- vm-eni-provisioning-design.md §4 lists 7 waves (0–6) for programming order
- DeltaPlan created with wave_offsets per object
- But: SouthboundDriver interface (fleet-manager-lld.md §10.2) defines **individual Apply* methods per object**, not wave-aware entry point
- Unclear: does driver execute waves internally, or does dispatcher orchestrate waves?

**Impact:**
- Dispatcher doesn't know how to order waves if driver only has individual Apply methods
- Driver sees mixed waves in single batch → can violate topological ordering
- No clear ownership: who detects unfinished waves?

**Remediation:**
Redesign `Specs/FM/southbound-driver-interface.md` (update existing):
```
SouthboundDriver interface:
  ApplyDeltaPlan(plan: DeltaPlan) → Result
    // plan includes wave_offsets; driver responsible for wave ordering
    // for each wave 0..6:
    //   for each command in wave:
    //     call Apply*(cmd.kind, cmd.obj)
    //   wait for all in-flight RPCs for this wave
    //   if any fail: rollback to last-good content_hash
```
- Driver owns wave sequencing (not dispatcher)
- Dispatcher validates: all Wave 0 commands before Wave 1, etc.

---

### GAP 1.5: Registry Acquire/Release Semantics Ambiguous

**Current State:**
- registry-pattern-design.md §3 defines Acquire/Release contract
- Acquire returns Subscription with Updates channel + Initial value + Ready channel
- But: Initial value — is it the value at Acquire time, or the first-emit-after-Acquire?
- Release debounce: "default 30s grace period" — configurable? Global or per-registry?
- What if Release called before Ready fires? Can subscription be cancelled in WAITING_HYDRATION state?

**Impact:**
- NicActor might use stale Initial value → old config programmed
- Inconsistent debounce timing → some VNETs cached longer than others
- Race: NIC deleted, registry still pending hydration, Release called → dangling subscription

**Remediation:**
Update `registry-pattern-design.md` §3:
```
Acquire semantics (exact):
  1. If key already in cache: return (cached value as Initial, ready chan already closed)
  2. If key not cached but watched: return (zero-value as Initial, open ready chan)
  3. If key not watched: open T1 watch, return (hydrating, open ready chan)
  
Ready channel:
  - Closed when first full snapshot received from T1 (after watch created)
  - May close after Acquire returns (async hydration)
  - All subscribers waiting on same ready chan see same close

Release semantics:
  - refcount--; if 0, schedule debounce timer (30s default, configurable per-registry)
  - If Release called again within grace period: debounce resets
  - After grace expiry: unsubscribe from T1, evict from cache
  - If Release called with pending hydration: refcount still goes to 0, grace starts, hydration continues (wasted work)
  
Recovery: NicActor must Acquire BEFORE starting composition; if Acquire returns waiting, park until Ready fires
```

---

### GAP 1.6: Reconciliation Semantics Undefined

**Current State:**
- HLD §8.2 mentions "Reconciliation Engine periodically re-reads keys from scratch and compares device-reported content_hash against composed hash"
- But: HOW OFTEN? What device telemetry provides hash? What triggers reconciliation kick-off?
- Unclear: if drift detected, does NicActor recompose automatically or does human intervene?
- Missing: what data does device provide? Just content_hash, or full telemetry?

**Impact:**
- Drift goes undetected if reconciliation never runs or device doesn't report hash
- Manual intervention unclear → devices might stay misconfig
- Device reporting format undefined → incompatible southbound drivers

**Remediation:**
Create `Specs/FM/reconciliation-design.md`:
- Reconciliation runs every 60s (configurable)
- For each programmed ENI: read content_hash from device via gNMI/SAI telemetry call
- Compare device_hash vs. fm_composed_hash (from T1 per eni_id)
- If mismatch: log "drift detected", emit metric, trigger NicActor recompose + reprogram (if Primary pod)
- Device must report: {eni_id, content_hash, applied_revision} tuple
- Telemetry contract: every ENI returns hash within 5s of gNMI read

---

### GAP 1.7: Adapter Watermark & Replay Semantics Unclear

**Current State:**
- storage-architecture.md mentions "Adapter pod writes T1 with CAS based on content hash"
- CB spec (superseded doc) mentions watermark resumption
- But: Where is watermark stored? (T2? T3? etcd lease?)
- What happens on adapter pod failure mid-write?
- How does CB know FM received an event?

**Impact:**
- Adapter pod fails → events lost or re-processed
- CB has no feedback → might replay old events
- Duplicate events possible → idempotency must be perfect

**Remediation:**
Create `Specs/FM/adapter-protocol-design.md`:
- Watermark stored in T2 (fm-cluster-state) under key: `/fm/v1/watermarks/adapter/<adapter_id>`
- On Subscribe: pass last-known watermark to CB
- After T1 CAS write succeeds: update T2 watermark atomically (T1 write + T2 watermark in same txn if possible, else ordered)
- On adapter pod failure: new adapter pod reads last watermark from T2, resumes from there
- CB-FM ack: FM emits StateAck to CB for each processed event (eni_id, event_id, status=success/dlq)
- Idempotency: FM stores (CB_event_id, content_hash) in T1 DLQ; duplicate event = same hash = no-op

---

## PART 2: INCONSISTENCIES & CONFLICTS

### INCONSISTENCY 2.1: Device Registration Response vs. HDO Subscription Topics

**Problem:**
- Device registration API returns: `subscription_topics: ["/config/v1/global/...", "/config/v1/group/...", "/config/v1/vnet/...", "/config/v1/hosts/..."]`
- But registry-pattern design §5A says: "NicActor Acquire(VnetRegistry, eni_id)" — not direct etcd subscription
- Device client doesn't need subscription_topics (it's FM-internal)

**Fix:**
- Remove `subscription_topics` from REST response
- Replace with `shard_id` (device knows which shard owns it for debug)
- Response becomes: `{device_id, shard_id, status, created_at, content_hash}`

---

### INCONSISTENCY 2.2: HDO Caching vs. Registries — ✅ RESOLVED (2026-06-22)

**Resolution:** See `Specs/FM/inc-closure-2.2-2.3.md` §2.

- HDO struct renamed to `DeviceIOState`; holds ONLY session/IO state (gNMI/SAI handle, device id, lifecycle pointer, child refs). No object caches.
- All VNET/NIC/Group/Mapping/Global reads MUST go through `Registry.Acquire(key)`.
- Bypass forbidden — enforced by `fm-lint NO_REGISTRY_BYPASS` CI rule.
- `fleet-manager-lld.md` §1.1 `HostDeviceState` is superseded by the closure doc.

**Problem (historical):**
- Fleet-manager-lld.md §1.1 defines HostDeviceState with maps: `routing_types`, `appliance`, `tunnels`, `vnet_chunks`, etc. (per-HDO cache)
- But registry-pattern-design.md §4–5 says: "HDO no longer holds caches; registries do"
- Conflict: which is source of truth? If per-HDO cache, then per-pod registry redundant

---

### INCONSISTENCY 2.3: NIC Spec Contents — ✅ RESOLVED (2026-06-22)

**Resolution:** See `Specs/FM/inc-closure-2.2-2.3.md` §3.

Proto3 `NicSpec` message declared under `dashfabric.fm.v1`, watch-readable from T1 at `/config/v1/nic/{eni_id}`. Explicit fields: eni_id, device_id, vnet_id, tenant_id, mac, primary_ip, vlan_id, route_group_id, acl_group_ids[6], meter_policy_id, ha_scope_id, admin_state, timestamps, spec_revision, audit envelope. Validation rules (9 checks), lifecycle coupling, and forbidden-edits list defined in closure doc §3.3, §3.5, §3.6.

**Problem (historical):**
- vm-eni-provisioning-design.md decision #1 says: NIC payload = reference bundle (vnet_id, route_group_ids, acl_group_ids[6], meter_policy_id)
- But fleet-manager-lld.md lists NicSpec as proto, not just references
- Unclear: does NIC carry MAC, primary_ip, vlan? Or are those only in compose-time HDO context?

---

### INCONSISTENCY 2.4: Error Handling Strategy Completely Undefined

**Problem:**
- No mention of how validation errors are handled (Decision #6 says "quarantine in WAITING_VALID")
- No error catalog or recovery strategy
- What happens if: T1 unavailable? CB disconnects? Device offline? Composition fails?
- HLD mentions DLQ for plugin events but no FM-side error handling flow

**Fix:**
- Create `Specs/FM/error-handling-design.md`:
  - Proto-decode failure → quarantine NO in VALIDATION_REJECTED state, emit FleetEvent, write `/status/v1/<original_path>/_error`
  - T1 unavailable → retry with exponential backoff, return 503 to client
  - Device offline → HD O in DISCONNECTED state, NicActor waits for reconnect
  - Composition failure → log, increment counter, retry on next input change
  - HAL Apply failure → rollback to last-good content_hash, emit alert
  - DLQ for unparseable events, manual operator review

---

## PART 3: MISSING SPECIFICATIONS

### MISSING 3.1: Shard Assignment Algorithm

**Current State:**
- fm-device-registration-flow-design.md §4.2 says: "rendezvous hash"
- But: no pseudocode, no hash function specified, no collision handling

**Remediation:**
```
Algorithm: Rendezvous Hashing (deterministic shard selection)

Input: device_id, [pod_0, pod_1, ..., pod_N]
Output: shard_id ∈ [0, N)

1. For each pod in list:
2.    hash_i = CRC32(device_id + pod_i)  // concatenate strings
3. shard_id = argmax_i(hash_i) % pod_count
4. Return shard_id

Guarantee: same device_id + same pod list → same shard_id
Benefit: consistent hashing (adding/removing pods reshuffles minimally)

Implementation: See circleHash library (reference implementation)
```

---

### MISSING 3.2: Content-Hash Algorithm Specification

**Current State:**
- vm-eni-provisioning.md decision #26 says: "SHA-256"
- But: what is serialized? What is canonical form? Float encoding?

**Remediation:**
```
Algorithm: NicGoalState Content-Hash

Input: NicGoalState (with all nested objects)
Output: hex string (SHA-256 digest)

Steps:
1. Serialize NicGoalState to proto3 binary (deterministic)
   - Use standard proto3 wire format
   - field tag order: ascending
   - repeated fields: sorted by content
2. Compute SHA256(binary)
3. Output: lowercase hex string (64 chars)

Proto3 ordering guarantees:
   - Message order: field number ascending (enforced by compiler)
   - Repeated field order: must be sorted (client responsibility at compose time)
   - Map order: sorted by key string (Go's SortedMap or equiv)

Validation: Two compose calls with same input → identical content_hash
```

---

### MISSING 3.3: Multi-Region / Multi-Cluster Scope

**Current State:**
- Storage architecture says "not multi-region active-active"
- But: no mention of how device failover works across AZs
- Unclear: is FM-cluster regional? Global?

**Remediation:**
```
Scope Definition:

- One FM cluster manages one regional fleet (e.g., us-east-1)
- Devices register to their regional FM instance via CB-FM gRPC
- Device failover: DPU in AZ-1 fails → orchestrator updates VNET mapping, NIC recomposed, programmed on AZ-2 DPU
- No special FM logic: VNET/mapping changes trigger recompose automatically
- Cross-region failover: orchestrator handles (out of FM scope)

Future: Multi-region synchronization via T1 replication (not in v1)
```

---

### MISSING 3.4: Autoscaling Policy for Registries & Thread Pools

**Current State:**
- No mention of when to grow/shrink
- No metrics for queue depth, registry cache hit rate, actor busy time

**Remediation:**
```
Autoscaling (FM-internal, no operator knobs needed):

1. Actor Executor Pool:
   - Monitor: actor_queue_depth (buffered channel size)
   - If > 80% capacity for 30s: grow pool K → min(K+4, 32)
   - If < 10% capacity for 5min: shrink pool K → max(K-2, 8)
   - Adjustments happen on 10s poll interval

2. Registry Cache Eviction:
   - Monitor: registry_memory_usage per type
   - If > 80% of max (5GB): LRU evict oldest-unused objects
   - Refcount logic prevents in-use eviction

3. T3 RocksDB Compaction:
   - Automatic on threshold (size > 10GB or stale tombstones > 50%)
   - Blocks pod briefly (~100ms) during compaction

Metrics to expose: actor_queue_depth, registry_cache_hit_rate, t3_compaction_latency
```

---

## PART 4: STRUCTURED DESIGN CHECKLIST

### Component Readiness

| Component | Status | Priority | Blocker |
|-----------|--------|----------|---------|
| Device Registration | ✅ Complete | — | None |
| HDO Actor | ⚠️ Incomplete | HIGH | Thread model, slim design |
| CO Actor | ✅ Complete | — | None |
| NO Actor | ⚠️ Incomplete | HIGH | NicGoalState schema, composition algorithm |
| GlobalRegistry | ⚠️ Incomplete | HIGH | Acquire/Release semantics, lock strategy |
| VnetRegistry | ⚠️ Incomplete | HIGH | Refcount, watch resume, Ready-chan timing |
| VnetMappingRegistry | ⚠️ Incomplete | HIGH | Assembler FSM, chunk assembly algorithm |
| GroupRegistry | ⚠️ Incomplete | HIGH | Prefix expansion, group versioning |
| HaRegistry | ✅ Complete | — | None |
| SouthboundDriver | ⚠️ Incomplete | HIGH | Wave ordering, ApplyDeltaPlan() impl |
| Adapter Pod | ⚠️ Incomplete | HIGH | Watermark storage, CB ack protocol, idempotency |
| Reconciliation Engine | ⚠️ Incomplete | MEDIUM | Device telemetry contract |
| Storage Layer (T1/T2/T3) | ✅ Complete | — | None |

---

### Proto Definitions Status

| Proto | Location | Status |
|-------|----------|--------|
| ConfigEntry, ConfigMetadata | Published | ✅ |
| DeltaCommand | Pending | ❌ |
| NicGoalState | Sketched | ⚠️ |
| NicSpec | Partial | ⚠️ |
| DashObject union | Sketched | ⚠️ |
| CB-FM gRPC contract | Published | ✅ |

---

## PART 5: IMPLEMENTATION ROADMAP

### Phase 1: Schema & Contracts (Week 1)
1. Finalize all proto3 schemas (NicGoalState, NicSpec, DeltaCommand, DashObject)
2. Update registry-pattern-design.md with exact Acquire/Release semantics
3. Define SouthboundDriver interface (wave-aware ApplyDeltaPlan)
4. Spec adapter watermark protocol

### Phase 2: Thread Model & Concurrency (Week 2)
1. Create threading-model-design.md with pool sizes, backpressure, memory budgets
2. Define synchronization strategy (RWMutex per registry, lock order)
3. Spec actor scheduling (coroutine or OS-thread?)

### Phase 3: Implementation (Weeks 3+)
1. Start with registries (foundational)
2. Implement HDO/CO/NO actors
3. Southbound driver skeleton
4. Adapter pod
5. Integration tests

---

## PART 6: HIGH-RISK AREAS FOR CODE GENERATION

| Risk Area | Root Cause | Mitigation |
|-----------|-----------|-----------|
| **NicGoalState composition** | Schema incomplete, algorithm undefined | Define proto + compose algorithm before coding |
| **Registry deadlocks** | Lock strategy unclear, refcount semantics fuzzy | Spec exact lock order, refcount FSM |
| **Adapter idempotency** | Watermark storage location TBD | Finalize T2 watermark schema first |
| **Wave ordering** | Driver interface ambiguous | Redesign SouthboundDriver to own wave sequencing |
| **Device lifecycle transitions** | States scattered across docs | Consolidate state machine in one diagram |
| **Thread pool autoscaling** | No policy defined | Implement simple threshold-based scaling |

---

## CONCLUSION

**Before AI code generation starts:**

1. ✅ **APPROVED**: Device registration API, CB-FM gRPC contract, storage architecture, actor hierarchy
2. ✅ **RESOLVED (2026-06-22)**: Registry semantics, wave ordering, composition algorithm, device lifecycle
3. ✅ **RESOLVED (2026-06-22)**: NicGoalState schema, adapter watermark protocol
4. ✅ **RESOLVED (2026-06-22)**: Thread model, reconciliation protocol, error handling catalog
5. ✅ **STITCHED (2026-06-22)**: Master design index — canonical registries for pools, locks, states, errors, metrics, constants
6. ✅ **HARDENED (2026-06-22, Track B)**: Security spec, INC 2.2/2.3 closure, runbooks for every CRITICAL error code

**ALL HIGH-PRIORITY GAPS, INCONSISTENCIES, AND RESIDUAL HARDENING ITEMS RESOLVED. CLEARED FOR IMPLEMENTATION.**

**Phase 1 Specs Created (architecture):**
- `Specs/FM/nicgoalstate-schema-design.md` — Proto3 schema, composition algorithm, content-hash spec (GAP 1.2)
- `Specs/FM/registry-semantics-exact.md` — Acquire/Release/Ready exact contract, lock order, debounce (GAP 1.5)
- `Specs/FM/southbound-driver-interface-redesign.md` — Wave-aware ApplyDeltaPlan, rollback, conformance (GAP 1.4)
- `Specs/FM/adapter-protocol-design.md` — Watermark, leader lease, CB ack, idempotency (GAP 1.7)
- `Specs/FM/device-lifecycle-design.md` — Unified HDO state machine, REST mapping, NIC gating (GAP 1.3, INC 2.1)

**Phase 2 Specs Created (runtime + safety net):**
- `Specs/FM/threading-model-design.md` — Pool inventory (P1-P10), Little's Law sizing, mailbox backpressure, lock order (L1-L8), panic recovery, shutdown ordering, memory budget (GAP 1.1)
- `Specs/FM/reconciliation-design.md` — 60s default cadence, drift taxonomy (7 classes), primary/standby coordination, HA peer drift handling (GAP 1.6)
- `Specs/FM/error-handling-design.md` — Canonical error catalog (~60 codes across 8 layers), classification matrix, SLO budgets, DLQ ownership, escalation paths (INC 2.4)

**Track B Hardening Specs (Closures + Security + Runbooks):**
- `Specs/FM/security-design.md` — Trust domains, SPIFFE/X.509/OIDC identity, mTLS-everywhere, RBAC+ABAC via OPA, secrets handling (`Secret[T]` opaque type, mlock, HSM/TPM CA keys), audit-log hash-chaining, threat model, hardening checklist
- `Specs/FM/inc-closure-2.2-2.3.md` — **INC 2.2 RESOLVED**: HDO is now slim (`DeviceIOState` only — no object caches; all reads through registries). **INC 2.3 RESOLVED**: Proto3 `NicSpec` schema with explicit fields, validation rules, lifecycle coupling, and forbidden-edit list
- `Specs/Runbooks/` — Operator runbooks for all 13 CRITICAL error codes (REG_007, REG_008, ACT_006, ACT_007, DRV_008, ADP_005, ADP_006, ADP_008, STO_002, STO_005, REC_005, REC_008, HA_003) plus index README per `error-handling-design.md` §9.2 mandate

**Master Integration Document:**
- `Specs/FM/MASTER_DESIGN_INDEX.md` — **The stitching document.** Canonical registries (pools, locks, states, error codes, metrics, T1/T2 keys, numerical constants) plus a cross-spec dependency matrix. §2 now lists 11 authoritative specs. Any contradiction with this document is a bug.

**Recommendation:** Implementation may begin module-by-module per `MASTER_DESIGN_INDEX.md §16` checklist. Specs are referenced from this checklist; deviations require waiver. Runbooks under `Specs/Runbooks/` are operational; security controls from `security-design.md` §11 are CI-enforced (e.g., `InsecureSkipVerify` rejected by lint).

---

