# DashFabric Phase 1 Project Roadmap (16 Weeks)

> **Scope:** Parallel FM + CB development for DASH-compliant routing orchestration
> **Timeline:** 16 weeks (4 sprints × 4 weeks, weeks 1–16)
> **Teams:** FM Team (4 FTE) + CB Team (3 FTE) + QA (2 FTE) = 9 FTE total
> **Goal:** End-to-end control plane → forwarding manager integration with 6 routing constructs operational

---

## Overview: The Parallel Highway

**DashFabric Phase 1 is four parallel rivers that converge:**

1. **FM River (4 sprints):** Registry architecture → core registries → ENI hydration → dataplane integration
2. **FM-Gateway River (parallel):** Pod discovery + primary election → request routing (primary-aware) → buffering → gRPC streaming → observability (6 phases, overlaps with FM river weeks 1–8)
3. **CB-Gateway River (parallel):** Project setup → pod discovery + health monitoring → gRPC/REST routing → buffering + rate limiting → metrics/tracing (6 phases, overlaps with FM river weeks 1–6)
4. **CB River (4 sprints):** Proto finalization → data ingestion → event streaming → E2E integration

**Convergence points:**
- **Week 6:** CB-GW ready for FM testing; FM can test gRPC routing to CB replicas via CB-GW
- **Week 8:** FM core registries ready + FM-GW request routing ready; both can test integration with CB-GW as broker
- **Week 13:** CB starts FM integration tests; FM HAL integration ready to accept CB-streamed config; FM-GW + CB-GW metrics/observability integrated

**Finish line (Week 16):** All 6 routing constructs working end-to-end (peering, VIP, meter, Private Link, direction-aware, ExpressRoute) with FM-GW as primary entry point and CB-GW as CB broker; FM-GW + CB-GW handling 10k req/s with <10ms p95 latency; 100k ENI load test passing.

---

## Milestone Timeline (16 Weeks)

### **MILESTONE 1: Foundation & Foundations (Weeks 1–4)**
**Status:** 🟢 Planning → 🟡 Execution  
**FM Sprint 1 + CB Sprint 1**

#### What's Happening

**FM Side:**
- Design Registry[K, V] interface contract (Acquire/Release/Read/Watch/Update/Delete)
- Implement async signal queue (FIFO with priority levels, backpressure handling)
- Define all DASH object types (Vnet, NIC, Route, Mapping, ACL, Meter, HA, Tunnel, SNAT)
- Write 100+ unit tests for interfaces and types

**CB Side:**
- Audit & finalize all 12 Phase 1 protobuf schemas (route, vnet, mapping, acl, meter, ha, nic, device, vip)
- Setup protobuf code generation pipeline (build/proto.mk)
- Generate Go bindings from protos into pkg/generated/protos/
- Define CB-specific internal data models (pkg/models/)
- Write proto validation tests (>95% coverage)

#### Why This Matters

This sprint establishes the **contract foundation**: FM's registry pattern defines how state flows through the system, and CB's protos define what state flows in. Both teams move in parallel because they're independent (FM doesn't depend on protos yet; CB doesn't depend on FM registry design). The async signal queue is critical—it enables FM's soft-fail recovery without deadlock.

**Output:** Locked protos + locked interfaces = everyone downstream has a stable target to code against.

---

### **MILESTONE 2: Core State Machines & Ingestion Pipeline (Weeks 5–8)**
**Status:** 🟡 Execution  
**FM Sprint 2 + CB Sprint 2**

#### What's Happening

**FM Side:**
- Implement 10 registries with full state machines:
  - **VnetRegistry**: UNKNOWN → READY/FAILED; signals peer readiness
  - **NicRegistry**: UNKNOWN → HYDRATING → READY/INCOMPLETE/FAILED; core ENI state machine
  - **MappingManager**: Tracks VNetMapping shards; discovers PE endpoints; pre-fills peer mappings
  - **RouteRegistry, VipRegistry, MeterRegistry, AclRegistry, HaRegistry, TunnelRegistry, SnatPoolRegistry**
- Each registry: state transitions, signals, per-registry locking, 5–8 unit test scenarios
- 200+ unit tests across all registries (>95% coverage per registry)

**CB Side:**
- Design Vnet ingestion pipeline (CP data source → CB cache → streaming pipeline)
- Implement CRUD operations for all 12 proto types:
  - VnetConfig, NicConfig, RouteGroup, MappingEntry, AclGroup, MeterPolicy, HaSet, Tunnel (semantic), SNAT pool (semantic)
- Validation layer: schema + DASH constraints enforced at ingestion time
- Conflict detection: reject duplicate object IDs; log warnings
- In-memory cache structure for fast lookups
- 50+ integration tests (CRUD + validation)
- Performance test: ingest 100k objects in <5 seconds

#### Why This Matters

This is where **FM state machinery comes alive** and **CB becomes a real ingestion pipeline**. The 10 registries are the "thought center" of FM—they track state, signal changes, and drive ENI hydration later. VnetRegistry + NicRegistry are the critical path; MappingManager unlocks the hybrid peering model (O(n) not O(n²)). On CB side, CRUD + validation means the system can now accept control plane config, which will flow through the streaming pipeline in Sprint 3.

**Output:** FM can track state; CB can ingest & store config. Two independent subsystems ready to talk.

---

### **MILESTONE 3: Event Streaming & Async Communication (Weeks 9–12)**
**Status:** 🟡 Execution  
**FM Sprint 3 + CB Sprint 3**

#### What's Happening

**FM Side:**
- Implement all 7 ENI hydration gates (the heart of the system):
  - **Peering_Gate** (hard-fail): Validates peer VNETs exist; fails ENI if peer missing
  - **Mapping_Gate** (soft-fail): Checks VNetMapping exists; ENI INCOMPLETE if mapping absent; recovers on signal
  - **VIP_Gate** (soft-fail): Checks VipRegistry readiness; soft-fail on missing backends
  - **Meter_Gate** (soft-fail): Validates meter policies; soft-fail if missing
  - **PE_Gate** (soft-fail): Discovers Private Link PE via routing_action_hint; soft-fail if PE missing
  - **ServiceTunnel_Gate** (soft-fail): Validates tunnel + optional SNAT pool; recovers on signal
  - **Direction_Gate** (mixed): Partitions routes by INBOUND/OUTBOUND; validates separately per direction
- Compose full Hydrate() algorithm (all 7 gates in order; correct state transitions)
- Implement async signal re-hydration (when soft-fail deps arrive, ENI re-hydrates automatically)
- 20+ integration tests (happy path, all soft-fail scenarios, all hard-fail scenarios, recovery flows)
- 6× conformance test suites (one per routing construct: peering, VIP, meter, PE, direction, tunnel)

**CB Side:**
- Design pub/sub abstraction (backend-agnostic: etcd or Kafka)
- Implement etcd backend (watch-based streaming, reconnection handling)
- Implement Kafka backend (topic-based streaming, compacted topics for replay)
- Topic naming schema (/dashfabric/v1/config/vnets/<vnet_id>, /config/nics/<eni_id>, etc.)
- Message encoding (protobuf default, JSON fallback)
- Replay logic (FM catches up on startup by replaying compacted topics)
- Dead-letter queue (failed messages queued for operator inspection)
- Heartbeat + keepalive (30s timeout, connection health monitoring)
- 40+ integration tests (pub/sub, replay, failure recovery)
- Performance test: stream 10k config updates/sec without loss

#### Why This Matters

This is where **FM becomes intelligent** and **CB becomes a real message bus**. The 7 gates encode the DASH routing logic: they're the gating function that decides whether an ENI is ready to program the dataplane. The async signal architecture is the innovation—it enables soft-fail recovery without blocking the entire ENI (if VIP doesn't exist yet, other gates proceed; when VIP arrives, ENI re-hydrates automatically). On CB side, event streaming is the nervous system: config changes flow from CP through CB into FM, and the replay mechanism ensures FM never misses an update even if it crashes.

**Output:** FM can hydrate ENIs correctly with all 6 routing constructs. CB can stream config reliably to FM. The system can survive failures.

---

### **MILESTONE 4: Dataplane Integration & Full E2E (Weeks 13–16)**
**Status:** 🔴 Pending  
**FM Sprint 4 + CB Sprint 4**

#### What's Happening

**FM Side:**
- Design HAL (Hardware Abstraction Layer) interface contract:
  - ApplyRules(dataplane_rules [])
  - RemoveRules(dataplane_rules [])
  - GetStats()
- Implement rule composition (convert FM state → DataplaneRule structs)
  - Compose VNET rules, peering rules, PE rules, tunnel rules, meter rules
- Implement HAL mock (for testing without real dataplane)
- Integrate Hydrator with HAL (on ENI READY: call HAL.ApplyRules())
- Add comprehensive metrics (fm_eni_count, fm_eni_hydration_latency_ms, fm_vnet_count, fm_mapping_entries, etc.)
- Implement debug API endpoints:
  - GET /debug/registry/eni/{eni_id} (state + gate status)
  - GET /debug/registry/vnet/{vnet_id} (peers, mappings, readiness)
  - GET /debug/hydration/{eni_id}?trace=true (detailed trace of all gates)
- Add distributed tracing (OpenTelemetry spans for VnetValidation, MappingCheck, PeeringCheck, Dataplane)
- Load test: provision 100k ENIs + 10k VNETs + 1k VIPs in <10 seconds
- Failure scenario tests (20+ modes: CP config error, stream stall, race conditions, tunnel fail, SNAT exhaustion, HA sync loss)
- Complete documentation (architecture diagrams, runbook, troubleshooting guide)

**CB Side:**
- Define CB ↔ FM contract (error codes, retry logic)
- Implement CB error handling (listens for FM error responses)
- Implement retry logic (exponential backoff: 1s, 2s, 4s, 8s, 16s, 32s; max 5 retries)
- Implement config versioning + ordering guarantees (CB tracks version; FM sees updates in order)
- Write E2E tests for all 6 routing constructs:
  - **E2E Test 1: Peering** (vnet-acme ↔ vnet-shared) — CP → CB → FM; ENIs in both VNETs can forward to each other
  - **E2E Test 2: VIP** (backend + NAT) — CP → CB → FM; VIP backend add triggers ENI re-hydration; SNAT rules programmed
  - **E2E Test 3: Meter** (policy + traffic class) — CP → CB → FM; metered traffic respects CIR/PIR
  - **E2E Test 4: Private Link** (PE mapping) — CP → CB → FM; PE endpoint discovered via routing_action_hint; traffic tunneled
  - **E2E Test 5: Direction** (inbound + outbound) — CP → CB → FM; inbound/outbound routes validated separately; no BIDIR
  - **E2E Test 6: Service Tunnel** (tunnel + SNAT) — CP → CB → FM; service tunnel routes with per-route SNAT
- E2E failure recovery tests (config rollback, retry, eventual consistency)
- E2E load test (1k concurrent ENI provisions in <30 seconds)
- Complete documentation (integration guide, operator manual, troubleshooting)
- Security review (input validation, encryption, authentication)

#### Why This Matters

This is the **convergence sprint** where everything talks to each other. FM talks to the dataplane via HAL; CB talks to FM via streaming; both teams coordinate E2E tests. The 6 routing constructs are the "definition of done"—if all 6 work end-to-end, the system works. The 100k load test proves the system scales. The 20+ failure tests prove it's resilient. The observability (metrics, debug endpoints, tracing) proves operators can troubleshoot it.

**Output:** Complete, working, observable, tested system ready for production validation.

---

## Sprint-by-Sprint Parallel Timeline (including FM-Gateway and CB-Gateway)

```
WEEK 1–4: SPRINT 1 (Foundation)
├─ FM: Registry interfaces + types + async queue + 100+ unit tests
├─ CB: Proto finalization + code generation + validation tests
├─ FM-GW Phase 1: Project setup, core data structures, HTTP listener skeleton
├─ CB-GW Phase 1: Project setup, core data structures, gRPC + REST listener skeletons
├─ INTEGRATION POINT: None yet (independent foundation work)
└─ OUTPUT: Locked interfaces + locked protos + FM-GW + CB-GW foundations ready

WEEK 5–8: SPRINT 2 (State Machines & Routing)
├─ FM: 10 registries (VnetRegistry, NicRegistry, MappingManager, etc.) + 200+ unit tests
├─ CB: CRUD operations + validation layer + 50+ integration tests + 100k perf test
├─ FM-GW Phase 2-3: Pod discovery, primary election monitoring, HTTP request routing (primary-aware), gRPC streaming
├─ CB-GW Phase 2-3: Pod discovery, health monitoring, gRPC/REST request routing (peer-aware, load-balanced)
├─ INTEGRATION POINT: FM-GW can test routing to FM mock replicas; CB-GW can test routing to CB mock replicas; CB can test config ingestion
└─ OUTPUT: FM tracks state; CB ingests config; FM-GW routes with primary awareness; CB-GW routes with peer awareness

WEEK 9–12: SPRINT 3 (Hydration & Streaming)
├─ FM: All 7 gates + Hydrate() algorithm + async recovery + 50+ integration tests
├─ CB: Pub/sub abstraction (etcd + Kafka) + replay + DLQ + 40+ integration tests + 10k events/sec perf test
├─ FM-GW Phase 4-5: Buffering, health checks, rate limiting, Prometheus metrics, W3C tracing, JSON logging
├─ CB-GW Phase 4-5: REST observability API, buffering, rate limiting, Prometheus metrics, OpenTelemetry tracing
├─ INTEGRATION POINT: CB streams test config into FM; FM-GW buffers/forwards to FM replicas; CB-GW buffers/forwards to CB replicas; observability integrated
└─ OUTPUT: FM hydrates ENIs correctly; CB streams reliably; FM-GW + CB-GW buffering & observability ready for production

WEEK 13–16: SPRINT 4 (Integration & E2E)
├─ FM: HAL integration + rule composition + metrics + debug endpoints + 20+ failure tests + 100k load test + docs
├─ CB: Error handling + retry logic + 6× E2E tests + 1k concurrent load test + docs
├─ FM-GW Phase 6: Hardening, testing (unit/integration/load), K8s manifests, documentation, performance tuning
├─ CB-GW Phase 6: Hardening, testing (unit/integration/load), K8s manifests, documentation, performance tuning
├─ INTEGRATION POINT: Full E2E testing—CP → CB → FM-GW → FM → HAL; CB-GW metrics/observability; all 6 constructs working; FM-GW + CB-GW latency/throughput SLA proven
└─ OUTPUT: Complete, tested, documented system with dual gateways ready for production
```

---

## Dependency Matrix: FM ↔ CB

| Phase | FM Dependency | CB Dependency | Critical Path? |
|-------|---|---|---|
| Sprint 1 | Registry interfaces | Proto finalization | NO (independent) |
| Sprint 2 | 10 registries implemented | CRUD operations | NO (independent) |
| Sprint 3 | Hydrate() + all gates ready | Event streaming pipeline ready | **YES** (FM gates must be stable before CB tests streaming) |
| Sprint 4 | HAL mock ready + rule composition | E2E harness ready | **YES** (Full E2E depends on both) |

**Critical path:** FM must have Hydrate() + all gates working by end of week 12 so CB can run E2E tests in week 13–16. If FM slips, E2E testing is blocked.

---

## Week-by-Week Milestones (Super Descriptive)

### **Week 1: Foundation Kickoff**
- **FM:** Registry interface design complete; team agrees on Acquire/Release/Watch contract. Async signal queue architecture documented. Testing framework set up.
- **CB:** Proto audit complete; 12 Phase 1 protos identified + prioritized. Code generation pipeline designed; build/proto.mk skeleton ready.
- **Sentiment:** Teams synchronized; contracts locked.

### **Week 2: Interface & Proto Implementation Begins**
- **FM:** Registry interface coded in Go; first unit tests passing. In-memory storage backend skeleton. Async signal queue implementation started.
- **CB:** route.proto + vnet.proto finalized and locked. Protoc code generation working; first Go bindings generated.
- **Sentiment:** Rapid early progress; foundations hardening.

### **Week 3–4: Type System & Testing Ready**
- **FM:** All DASH object types defined (Vnet, NIC, Route, Mapping, ACL, Meter, HA, Tunnel, SNAT). 100+ unit tests written; interfaces 100% covered.
- **CB:** All 12 protos finalized + locked. Validation tests >95% coverage. Proto bindings stable. CB data models defined.
- **Sentiment:** Sprint 1 complete; ready for state machine implementation.

---

### **Week 5: Registry Implementation Acceleration**
- **FM:** VnetRegistry core logic coded; state machine transitions tested. NicRegistry skeleton; hydration flow outlined. MappingManager shard tracking logic started.
- **CB:** CRUD scaffolding for all 12 object types. VnetConfig ingestion pipeline designed. Conflict detection logic started.
- **Sentiment:** Core algorithms taking shape; team confidence growing.

### **Week 6–7: State Machines Solidifying**
- **FM:** All 10 registries implemented with state machines. 150+ unit tests passing. MappingManager pre-fill logic working. Signal flow tested between registries.
- **CB:** CRUD operations working for 8/12 types. Validation layer ~70% complete. In-memory cache structure proven. 30+ integration tests passing.
- **Sentiment:** System shape emerging; integration points visible.

### **Week 8: Sprint 2 Finishing & Performance Validation**
- **FM:** All 10 registries complete + tested. 200+ unit tests passing (>95% per registry). Performance baseline established (lookups <1ms).
- **CB:** CRUD complete for all 12 types. Validation layer enforces all DASH constraints. 50+ integration tests passing. 100k object ingestion test **PASSING** in <5 seconds.
- **Sentiment:** Sprint 2 complete; state tracking proven at scale.

---

### **Week 9: Hydration Gates Kickoff & Streaming Design**
- **FM:** Peering_Gate (hard-fail) implemented + tested. Mapping_Gate (soft-fail) implemented; soft-fail recovery mechanism tested. Gate composition algorithm outlined.
- **CB:** Pub/sub abstraction designed. etcd backend skeleton. Kafka backend skeleton. Topic naming schema finalized.
- **Sentiment:** The most complex FM logic (gates) enters implementation. Streaming architecture clear.

### **Week 10–11: All Gates Implemented & Async Recovery**
- **FM:** All 7 gates (Peering, Mapping, VIP, Meter, PE, ServiceTunnel, Direction) implemented. Hydrate() composition algorithm complete; state transitions correct. Async signal re-hydration working (when soft-fail deps arrive, ENI re-hydrates).
- **CB:** etcd backend streaming working (watch-based). Kafka backend streaming working (topic-based). Replay logic implemented; FM can catch up on startup. DLQ for failed messages.
- **Sentiment:** Two independent subsystems at peak capability. Integration ready.

### **Week 12: E2E Readiness & Performance Validation**
- **FM:** All gates tested in 50+ integration scenarios. 6× conformance test suites written (one per routing construct) and **PASSING**. Load test infrastructure ready. Async recovery tested under failure. HAL mock ready.
- **CB:** Event streaming tested at 10k events/sec. Replay tested. DLQ tested. Heartbeat + keepalive working. 40+ integration tests **PASSING**. 10k events/sec performance test **PASSING**.
- **Sentiment:** Sprint 3 complete; convergence imminent.

---

### **Week 13: Integration Begins**
- **FM:** HAL interface contract finalized. Rule composition logic (VNET + peering + PE + tunnel + meter → DataplaneRule) implemented. Hydrator integrated with HAL mock (on ENI READY: call ApplyRules). Metrics skeleton with 15+ KPIs.
- **CB:** Error handling for FM rejections implemented. Retry logic (exponential backoff) working. Config versioning tracked. E2E test harness set up; first E2E test (peering) running.
- **Sentiment:** Two rivers converging. First E2E signals visible.

### **Week 14: E2E Constructs & Observability**
- **FM:** All 20+ failure scenario tests written and **PASSING** (config error, stream stall, race, tunnel fail, SNAT exhaustion, HA sync loss, etc.). Debug API endpoints live (/debug/registry/eni, /debug/hydration, etc.). OpenTelemetry tracing integrated (spans for validation, mapping, peering, dataplane).
- **CB:** 4/6 E2E tests **PASSING** (peering, VIP, meter, PE). Direction + ServiceTunnel E2E tests in progress. Load test infrastructure ready (1k concurrent ENI provisions).
- **Sentiment:** System robustness proven. Observability complete.

### **Week 15: Full E2E & Load Testing**
- **FM:** 100k ENI + 10k VNET + 1k VIP load test **PASSING** in <10 seconds. All metrics instrumented. Debug endpoints fully populated with real scenarios.
- **CB:** All 6 E2E tests **PASSING** (peering, VIP, meter, PE, direction, tunnel). E2E failure recovery test **PASSING** (config rollback, retry, eventual consistency). 1k concurrent ENI provision test **PASSING** in <30 seconds.
- **Sentiment:** System proven at scale and under stress.

### **Week 16: Documentation & Readiness**
- **FM:** Complete documentation (architecture diagrams, registry contract details, gate composition algorithm, failure modes, runbook, troubleshooting guide). Security review complete (validation, encryption). All code reviewed.
- **CB:** Complete documentation (integration guide, operator manual, proto definitions, streaming architecture, retry semantics, error catalog). Security review complete. All code reviewed.
- **Sentiment:** Phase 1 complete. Production-ready.

---

## Critical Path Analysis

**The Chain That Cannot Slip:**

1. **Week 1:** Registry interface design ← **FM only**
2. **Week 2:** Async signal queue implemented ← **FM only**
3. **Weeks 3–4:** All DASH types defined ← **FM only** (CB protos are parallel)
4. **Weeks 5–8:** 10 registries implemented + tested ← **FM only**
5. **Weeks 9–11:** All 7 gates implemented + Hydrate() ← **FM only**
6. **Week 12:** Async recovery proven ← **FM only**
7. **Week 13–16:** E2E integration ← **FM + CB together**

**If FM slips after week 12, E2E testing is blocked.** CB can continue independently through week 12, but integration is blocked if FM gates aren't ready.

**Parallel work (not on critical path, can slip without blocking):**
- CB proto finalization (can slip by 1–2 weeks; FM doesn't depend on it until week 13)
- Detailed tracing + advanced metrics (can be optimized post-Phase-1)
- Load test optimization (can continue into week 16 if needed)

---

## Risk & Contingency

| Risk | Probability | Impact | Mitigation |
|---|---|---|---|
| **Proto schema changes mid-sprint** | Medium | Medium | Lock all protos by end of week 4. Any future changes require joint FM/CB review + limited scope. |
| **Registry deadlock (circular dependencies)** | Low | Critical | DAG architecture already proven in LLD; deadlock analysis complete. If detected during implementation, rollback to LLD and redesign. |
| **Race conditions in ENI hydration** | Low | High | Comprehensive race condition audit in LLD. Async signal architecture prevents worst cases. Stress tests in week 12 will flush remaining races. |
| **Event ordering issues (updates out of order)** | Medium | High | Implement version tracking + ordering checks in CB. FM validates monotonic version numbers. |
| **Performance targets missed** | Medium | Medium | Profile in week 8 (CB 100k test) and week 12 (FM load test). If 100k < 5s slips, optimize Registry.Get(). If load test misses, profile Hydrate() algorithm. |
| **Soft-fail recovery not working** | Low | High | Comprehensive testing in week 11–12. If async signals don't trigger re-hydration, redesign signal queue. |
| **HAL interface mismatch** | Medium | High | Mock HAL in sprint 2 (week 6–8). Week 13 integration will reveal mismatches early. Contingency: define adapter layer. |
| **FM not ready by week 12** | Low | Critical | Block E2E testing; escalate. Contingency: use mock FM for CB testing while FM implementation completes (1–2 week delay acceptable). |

---

## Success Criteria (End of Phase 1)

**All of the following must be ✅ PASSING by end of week 16:**

- ✅ All 12 Phase 1 protos finalized + locked (CB)
- ✅ CRUD operations for all proto types working (CB)
- ✅ Event streaming (etcd + Kafka) proven at 10k events/sec (CB)
- ✅ 100k object ingestion in <5 seconds (CB)
- ✅ 10 registries implemented + tested (FM)
- ✅ Full ENI Hydrate() with all 7 gates working (FM)
- ✅ All 6 routing constructs E2E tested (FM + CB)
- ✅ 100k ENI + 10k VNET + 1k VIP load test passing in <10s (FM)
- ✅ 1k concurrent ENI provisions complete in <30s (CB)
- ✅ Error handling + retry logic working (CB)
- ✅ 20+ failure scenario tests passing (FM)
- ✅ Observability (metrics, debug endpoints, tracing) complete (FM)
- ✅ FM-GW implementation complete (pod discovery, primary election, request routing, buffering, rate limiting, metrics, observability)
- ✅ FM-GW latency <10ms p95 on 10k req/s; throughput SLA proven
- ✅ FM-GW 6 phases + 7-8 week timeline locked; all 21 tasks in tracker
- ✅ CB-GW implementation complete (pod discovery, health monitoring, gRPC/REST routing, buffering, rate limiting, metrics, observability)
- ✅ CB-GW latency <10ms p95 gRPC, <100ms p95 REST; throughput SLA proven
- ✅ CB-GW 6 phases + 5-6 week timeline locked; all 21 tasks in tracker
- ✅ Documentation + operator manual complete (FM + CB + FM-GW + CB-GW)
- ✅ Security review passed (FM + CB + FM-GW + CB-GW)

**If ANY of the above is ❌ FAILING, Phase 1 is not complete.**

---

## Resource Allocation

| Role | Count | Allocation |
|---|---|---|
| **FM Architect** | 1 | Design + oversight (all 4 sprints) |
| **FM Senior Engineer × 2** | 2 | Registries + hydration (sprints 1–3); HAL integration (sprint 4) |
| **FM Engineer** | 1 | Testing + metrics + observability (sprints 2–4) |
| **FM-Gateway Engineer** | 1 | FM-GW development (phases 1–6; parallel to FM sprints 1–4) |
| **CB-Gateway Engineer** | 1 | CB-GW development (phases 1–6; parallel to FM sprints 1–3) |
| **CB Lead** | 1 | Proto design + oversight (sprints 1–4) |
| **CB Backend Engineer × 2** | 2 | Ingestion + streaming (sprints 2–3); integration (sprint 4) |
| **QA × 2** | 2 | Unit testing (sprint 1), integration testing (sprints 2–3), E2E testing (sprint 4), FM-GW + CB-GW testing |
| **Total** | **11 FTE** | |

---

## Go/No-Go Checkpoints

**End of Sprint 1 (Week 4):**
- [ ] Registry interface + types 100% complete
- [ ] All protos locked + validation tests passing
- **Decision:** Proceed to Sprint 2 (GO) or redesign interfaces (NO-GO)

**End of Sprint 2 (Week 8):**
- [ ] All 10 registries complete + 200+ unit tests passing
- [ ] CRUD complete + 100k test < 5s
- **Decision:** Proceed to Sprint 3 (GO) or fix registry design (NO-GO)

**End of Sprint 3 (Week 12):**
- [ ] All 7 gates complete + conformance tests passing
- [ ] Event streaming 10k events/sec proven
- **Decision:** Proceed to Sprint 4 E2E (GO) or fix gates (NO-GO)

**End of Sprint 4 (Week 16):**
- [ ] All 6 E2E tests passing
- [ ] Load tests passing (100k FM, 1k CB concurrent)
- [ ] Documentation complete
- **Decision:** Phase 1 COMPLETE (GO) or extend Phase 1 (NO-GO, escalate)

---

## Handoff & Next Phase (Phase 2 Readiness)

**At end of week 16, Phase 1 closes and Phase 2 begins:**

**Phase 2 will add (design documents already frozen in `Specs/me-and-ai/phase2-missing-objects-design.md`):**
- Tunnel.proto (formal schema once provisioning stabilizes)
- OutboundPortMap.proto (formal schema once SNAT pool provisioning stabilizes)
- NicConfig enhancements (multi-stage ACL, meter, HA, route overrides)
- Advanced features (PrefixTag, RoutingType, PaValidation if needed)

**Phase 1 code artifacts for Phase 2:**
- `pkg/registry/` (all 10 registries; ready for expansion)
- `pkg/hydration/` (Hydrate() + all gates; ready for Phase 2 gate additions)
- `pkg/dataplane/` (rule composition; ready for advanced rule types)
- `pkg/streaming/` (pub/sub + replay; ready for advanced streaming features)
- `pkg/ingestion/` (CRUD; ready for Phase 2 object types)

---

## Notes for Future Teams

1. **This roadmap is the master timeline.** Each sprint has a detailed planner (see `Specs/FM/fm-implementation-planner.md` and `Specs/CB/cb-implementation-planner.md`).

2. **Sprint-level detail** resides in the individual planners. Milestone-level detail resides here.

3. **Dependency between FM and CB is minimal until week 13.** Leverage this to hire, onboard, and parallelize. If either team is ahead, they can write tests for the downstream team.

4. **The critical path is FM's Hydrate() implementation.** If FM slips, the entire project slips. Monitor FM sprint 3 closely.

5. **Performance targets are aggressive but achievable.** Week 8 and week 12 are pressure-test weeks. Allocate buffer.

6. **Failure scenario testing (week 14) is not optional.** It reveals race conditions, deadlocks, and state machine bugs that unit tests miss.

7. **Observability (metrics, debug endpoints, tracing) must be built in, not bolted on.** Week 13–15 adds them, but the architecture must support it.

8. **Documentation is due week 16, not "after shipping."** Plan for it.

---

## Quick Reference: Sprint Cadence

| Sprint | Weeks | FM Focus | CB Focus | Integration Point |
|---|---|---|---|---|
| 1 | 1–4 | Interfaces + types | Protos + code gen | None (independent) |
| 2 | 5–8 | 10 registries | CRUD + ingestion | CB tests FM types |
| 3 | 9–12 | Hydrate() + gates | Streaming + replay | CB tests FM state machine |
| 4 | 13–16 | HAL + E2E | E2E tests + integration | Full E2E: CP → CB → FM → HAL |

---

**Status: READY TO START (Week 1)**

All design + LLD complete. All risks mitigated. All success criteria defined. Go.
