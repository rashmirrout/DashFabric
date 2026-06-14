# FM (Fleet Manager) Implementation Plan

> **Owner:** FM Team (implementers)
> **Scope:** Phase 1 (core ENI hydration + 6 routing constructs)
> **Timeline:** 16 weeks (4 sprints × 4 weeks)
> **Status:** Ready to start

---

## Executive Summary

FM Phase 1 implementation follows waterfall-like dependency chain: (1) registry interfaces → (2) core registries → (3) peering/VIP/meter gates → (4) PE/direction/tunnel gates → (5) dataplane integration → (6) observability → (7) testing.

**Total effort:** ~400 engineer-days (4 FTE × 16 weeks)

**Critical path:** Registry interfaces → VnetRegistry → NicRegistry → Hydrate() → HAL integration

---

## Phase 1 Breakdown: 4 Sprints × 4 Weeks

### Sprint 1: Foundation (Weeks 1–4)

**Goal:** Registry interfaces, data structures, async signal queue.

#### Tasks

| Task | Description | Owner | Duration | Dependencies | Deliverables |
|------|-------------|-------|----------|--------------|---|
| **1.1** | Design Registry interface contract | FM Architect | 3 days | None | `registry/interface.go` (Registry[K,V] contract, Acquire/Release/Read/Watch) |
| **1.2** | Implement in-memory storage backend | Storage Owner | 4 days | 1.1 | `storage/memory.go` (concurrent map, versioning, epochs) |
| **1.3** | Design async signal queue | Concurrency Owner | 3 days | 1.1 | `signals/queue.go` (FIFO, priority levels, backpressure) |
| **1.4** | Implement ENI state types & enums | Data Owner | 3 days | None | `types/eni.go` (FAILED, INCOMPLETE, READY states + soft-fail reasons) |
| **1.5** | Implement Vnet/NIC/Route/ACL/Meter types | Data Owner | 5 days | None | `types/*.go` (all DASH object types + FM extensions) |
| **1.6** | Write unit tests for interfaces | QA | 4 days | 1.1–1.5 | `*_test.go` (100% coverage on interfaces) |
| **1.7** | Setup logging, metrics skeleton | Observability | 3 days | None | `metrics/registry.go` (metric names, gauges, counters) |

**Sprint 1 Output:**
- ✅ Registry interface (Acquire/Release/Read/Watch/Update/Delete)
- ✅ Async signal queue (FIFO, priority)
- ✅ All data types (DASH-compliant)
- ✅ 100+ unit tests
- **Artifact:** `pkg/registry/` + `pkg/types/`

---

### Sprint 2: Core Registries (Weeks 5–8)

**Goal:** Implement 10 registries with state machines, signals, validation.

#### Tasks (Grouped by Registry)

| Registry | Task | Duration | Acceptance Criteria |
|----------|------|----------|---|
| **VnetRegistry** | Implement Vnet state machine (UNKNOWN → READY/FAILED) | 4 days | Events: VNET_CREATED, VNET_UPDATED, VNET_DELETED; signals peer readiness |
| | Implement Vnet validation (vni, prefixes, peers) | 2 days | Rejects invalid Vnet; logs errors |
| | Implement peer subscription trigger | 3 days | Auto-subscribes to peer VNETs; signals on peer list change |
| | Write unit tests (10+ scenarios) | 3 days | >95% coverage; tests: happy path, invalid vnet, peer missing, peer removed |
| **NicRegistry** | Implement NIC state machine (UNKNOWN → HYDRATING → READY/INCOMPLETE/FAILED) | 4 days | State transitions correct; signals ENI_READY, ENI_INCOMPLETE, ENI_FAILED |
| | Stub Hydrate() method (calls downstream gates) | 5 days | Skeleton only; calls VnetRegistry, RouteRegistry, etc. |
| | Implement NIC lifecycle (create, update, delete) | 3 days | Handles NIC arrival, config updates, deletion |
| | Write unit tests (15+ scenarios) | 4 days | >95% coverage; tests: hydration success, all 7 gates individually |
| **MappingManager** | Implement VnetMapping sharding (manifest + chunks) | 5 days | Tracks chunk arrivals; calculates readiness threshold |
| | Implement PE discovery (routing_action_hint="PRIVATELINK") | 3 days | Parses MappingEntry.routing_action_hint; signals PE_FOUND |
| | Implement peer mapping pre-fill (O(n) hybrid model) | 4 days | Proactively subscribes to peer VNETs; caches mappings |
| | Write unit tests (12+ scenarios) | 4 days | >95% coverage; tests: sharding, PE discovery, pre-fill |
| **RouteRegistry** | Implement RouteGroup state (UNKNOWN → READY/FAILED) | 3 days | Tracks route group changes; signals on update |
| | Validate route actions (all 9 RouteAction types) | 2 days | Rejects ROUTE_ACTION_UNSPECIFIED; validates action-specific fields |
| | Write unit tests (8+ scenarios) | 3 days | >95% coverage |
| **VipRegistry** | Implement VIP state (UNKNOWN → READY/INCOMPLETE/FAILED) | 3 days | Tracks VIP backend list; soft-fail on dependencies |
| | Implement backend membership detection (overlay_ip match) | 3 days | Detects ENIs that overlap with backend_dips |
| | Write unit tests (8+ scenarios) | 3 days | >95% coverage |
| **MeterRegistry** | Implement MeterPolicy state | 2 days | Simple; just track policy presence |
| | Write unit tests (5+ scenarios) | 2 days | >95% coverage |
| **AclRegistry** | Implement AclGroup state (for now: single-stage) | 2 days | Phase 1: only stage 1; defer multi-stage to Phase 2 |
| | Write unit tests (5+ scenarios) | 2 days | >95% coverage |
| **HaRegistry** | Implement HaSet state + HA membership tracking | 3 days | Tracks HA pairs; signals on role change |
| | Write unit tests (6+ scenarios) | 2 days | >95% coverage |
| **TunnelRegistry** | Implement Tunnel state (UNKNOWN → READY/FAILED) | 2 days | Semantic; no proto schema yet; in-memory state |
| | Validate tunnel (encap_type, src IP, dst IPs) | 2 days | Rejects invalid encap; requires src IP |
| | Write unit tests (5+ scenarios) | 2 days | >95% coverage |
| **SnatPoolRegistry** | Implement SNAT pool state | 2 days | Track pool IP, port range, availability |
| | Write unit tests (5+ scenarios) | 2 days | >95% coverage |

**Sprint 2 Output:**
- ✅ 10 registries fully implemented
- ✅ All state machines correct
- ✅ All signals flowing
- ✅ 200+ unit tests (>95% coverage per registry)
- **Artifact:** `pkg/registry/vnet/`, `pkg/registry/nic/`, `pkg/registry/mapping/`, etc.

---

### Sprint 3: ENI Hydration & Gate Validation (Weeks 9–12)

**Goal:** Implement Hydrate() with all 7 gates; compose full ENI state.

#### Tasks

| Task | Description | Duration | Acceptance Criteria |
|------|-------------|----------|---|
| **3.1** | Implement Peering_Gate (hard-fail) | 3 days | Rejects ENI if peer missing; allows all peered VNETs |
| **3.2** | Implement Mapping_Gate (soft-fail) | 3 days | INCOMPLETE if mappings absent; auto-recovers when mapping arrives |
| **3.3** | Implement VIP_Gate (soft-fail) | 2 days | INCOMPLETE if VIP not ready; signals when ready |
| **3.4** | Implement Meter_Gate (soft-fail) | 2 days | INCOMPLETE if meter policy missing |
| **3.5** | Implement PE_Gate (soft-fail) | 3 days | INCOMPLETE if PE mapping not found; signals when discovered |
| **3.6** | Implement ServiceTunnel_Gate (soft-fail) | 3 days | INCOMPLETE if tunnel/pool missing; recovers on signal |
| **3.7** | Implement Direction_Gate (mixed hard+soft) | 3 days | Partition routes by direction; validate separately |
| **3.8** | Compose full Hydrate() algorithm | 4 days | All 7 gates in order; correct state transitions |
| **3.9** | Implement signal re-hydration (async recovery) | 4 days | ENI re-hydrates when soft-fail deps arrive |
| **3.10** | Write integration tests (20+ scenarios) | 6 days | Happy path, all soft-fails, all hard-fails, recovery flows |
| **3.11** | Write conformance tests (per routing construct) | 5 days | Peering works, VIP works, meter works, PE works, tunnel works, direction works |

**Sprint 3 Output:**
- ✅ Full Hydrate() implementation
- ✅ All 7 gates working
- ✅ Async recovery (soft-fail signals trigger re-hydration)
- ✅ 50+ integration tests
- ✅ 6× conformance test suites (one per routing construct)
- **Artifact:** `pkg/hydration/hydrator.go`, `pkg/hydration/gates.go`

---

### Sprint 4: Dataplane Integration & Observability (Weeks 13–16)

**Goal:** HAL integration, rule programming, metrics, debug endpoints.

#### Tasks

| Task | Description | Duration | Acceptance Criteria |
|------|-------------|----------|---|
| **4.1** | Design HAL interface contract | 2 days | ApplyRules(), RemoveRules(), GetStats() |
| **4.2** | Implement rule composition (VNET/peering/PE/tunnel/meter) | 5 days | Converts FM state → DataplaneRule structs |
| **4.3** | Implement HAL mock (for testing) | 3 days | Mock HAL for unit tests; tracks rule applications |
| **4.4** | Integrate Hydrator with HAL | 3 days | On ENI READY: call HAL.ApplyRules() |
| **4.5** | Add metrics instrumentation (all registries) | 4 days | fm_eni_count, fm_eni_hydration_latency_ms, fm_vnet_count, etc. |
| **4.6** | Implement debug API endpoints | 4 days | GET /debug/registry/eni/{eni_id}, /debug/hydration/{eni_id}?trace=true |
| **4.7** | Add distributed tracing (OpenTelemetry) | 3 days | Spans: VnetValidation, MappingCheck, PeeringCheck, Dataplane, etc. |
| **4.8** | Write load test (100k ENIs, 10k VNETs, 1k VIPs) | 5 days | Provision 100k ENIs in <10 seconds; measure latency percentiles |
| **4.9** | Write failure scenario tests (20+ modes) | 6 days | CP config errors, stream stalls, race conditions, tunnel fail, etc. |
| **4.10** | Write documentation (architecture, API, runbook) | 4 days | README, architecture diagram, API docs, troubleshooting guide |

**Sprint 4 Output:**
- ✅ Dataplane integration (HAL interface implemented)
- ✅ Rule composition (all 6 routing constructs → dataplane rules)
- ✅ Full observability (metrics, debug endpoints, tracing)
- ✅ Load test passing (100k ENIs in <10s target)
- ✅ 20+ failure mode tests passing
- ✅ Complete documentation
- **Artifact:** `pkg/dataplane/`, `pkg/observability/`, `docs/`

---

## Implementation Tracker

| Phase | Sprint | Week | Milestone | Status | Completion % |
|-------|--------|------|-----------|--------|---|
| **1** | 1 | 1–4 | Registry interfaces + types | 🔄 | 0% |
| | 2 | 5–8 | 10 registries implemented | 📋 | 0% |
| | 3 | 9–12 | Hydrate() + all gates | 📋 | 0% |
| | 4 | 13–16 | HAL + observability + tests | 📋 | 0% |

---

## Critical Path & Dependencies

```
Sprint 1: Registry interfaces
    ↓
Sprint 2: VnetRegistry + NicRegistry (parallel with other registries)
    ↓
    MappingManager (depends on VnetRegistry)
    ↓
Sprint 3: Hydrate() (depends on all registries from Sprint 2)
    ↓
Sprint 4: HAL integration (depends on Hydrate)
          Observability (parallel with HAL)
          Tests (depends on HAL)
```

**Critical path:** Registry interfaces → VnetRegistry → NicRegistry → Hydrate() → HAL → done.

**Non-critical (can slip without blocking):** Detailed tracing, advanced metrics, load testing (can optimize post-Phase-1).

---

## Resource Requirements

- **Engineering Team:** 4 FTE (architect, senior engineer × 2, engineer × 1)
- **QA:** 1 FTE (test automation)
- **Infrastructure:** Laptop, git repo, metrics system, mock HAL
- **External:** CB team (for proto definitions, config delivery)

---

## Risks & Contingency

| Risk | Probability | Impact | Mitigation |
|------|-------------|--------|---|
| Proto schema changes from CB | Medium | High | Weekly CB sync; lock protos by week 2 |
| Deadlock in registry interactions | Low | Critical | Comprehensive deadlock analysis in LLD (already done) |
| Race conditions in hydration | Low | High | Unit tests + stress tests catch most; async signal architecture prevents worst |
| HAL interface mismatch with dataplane | Medium | High | Mock HAL early (sprint 2); real HAL integration sprint 4 |
| 100k ENI load test fails | Medium | Medium | Identify bottleneck; optimize Registry.Get() or composition algorithm |

---

## Success Criteria (Phase 1 Complete)

- ✅ 10 registries fully implemented + tested
- ✅ Hydrate() working for all 7 gates (peering, mapping, VIP, meter, PE, tunnel, direction)
- ✅ All 6 routing constructs operational (peering, VIP, meter, PE, direction, tunnel)
- ✅ HAL integration complete (mock HAL for testing, ready for real HAL)
- ✅ 100k ENI load test passing (<10s provisioning)
- ✅ 50+ integration tests + 20+ failure scenario tests passing
- ✅ Observability (metrics, debug endpoints, tracing) working
- ✅ Documentation complete + runbook available
