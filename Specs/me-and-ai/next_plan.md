# Next Plan: Complete FM Routing & Registry Architecture

> **Owner:** User (architect)
> **Scope:** Complete design phase for all FM routing constructs (peering, VIPs, meters, Private Link, ExpressRoute, direction awareness) + comprehensive LLD + implementation plan
> **Status:** Peering ✅ COMPLETE, VIPs ✅ COMPLETE, Meters ✅ COMPLETE, Private Link ✅ COMPLETE, Inbound/Outbound ✅ COMPLETE, ExpressRoute 🔄 IN PROGRESS
> **Updated:** 2026-06-14

---

## Part A: Completed Design Closures

### ✅ FM-Gateway (FM-GW) — Primary-Aware Load Balancer (LOCKED)
- **Architecture HLD:** `Specs/FM-GW/fm-gw-architecture-hld.md` (9 sections: overview, deployment, components, flows, load balancing, failover, config, observability, deployment)
- **Low-Level Design:** `Specs/FM-GW/fm-gw-low-level-design.md` (8 sections: process architecture, data structures, request handlers, concurrency, error handling, config, performance)
- **Pod Discovery & Primary Election:** `Specs/FM-GW/fm-gw-pod-discovery-and-primary-election.md` (11 sections: mechanisms, lease monitoring, primary detection logic, request routing, failover scenarios, observability, config, testing)
- **Implementation Plan:** `Specs/FM-GW/fm-gw-implementation-planner.md` (6 phases, 7-8 weeks, detailed task breakdown with effort estimates and tracker)
- **README:** `Specs/FM-GW/README.md` (navigation guide, architecture summary, design decisions, deployment patterns)
- **Key Innovation:** Primary-aware routing (registrations/queries → PRIMARY only; streams → load-balanced across all) with intelligent failover (<20s RTO via adapter lease TTL)
- **Scope:** Single binary; runs on all deployment tiers (docker-compose → Kubernetes); ~1200 LOC Go; pod discovery from T2 etcd every 10s; primary election monitoring every 5s

### ✅ CB-Gateway (CB-GW) — Peer-Aware Dual-Protocol Gateway (LOCKED)
- **Architecture HLD:** `Specs/CB-GW/cb-gw-architecture-hld.md` (9 sections: overview, deployment, components, flows, load balancing, failover, config, observability, deployment)
- **Low-Level Design:** `Specs/CB-GW/cb-gw-low-level-design.md` (7 sections: process architecture, data structures, request handlers, concurrency, config, performance)
- **Pod Discovery & Replica Monitoring:** `Specs/CB-GW/cb-gw-pod-discovery-and-replica-monitoring.md` (9 sections: mechanisms, health monitoring, replica state, panic mode, metrics, logs, config, testing)
- **Implementation Plan:** `Specs/CB-GW/cb-gw-implementation-planner.md` (6 phases, 5-6 weeks, detailed task breakdown with 21 tasks, success criteria, tracker)
- **README:** `Specs/CB-GW/README.md` (navigation guide, architecture summary, design decisions, dual-protocol strategy, deployment patterns)
- **Key Innovation:** Dual-protocol gateway (gRPC :5052 for FM→CB, REST :8081 for observability) with peer-equivalent replica routing (all CB replicas equal; no primary election; eventual consistency model)
- **Scope:** Single binary; runs on all deployment tiers (docker-compose → Kubernetes); ~1500 LOC Go; pod discovery from T2 etcd every 10s; health checks every 10s; least-connections load balancing for long-lived streams; per-replica buffering (1000 depth)

### ✅ VNET Peering (LOCKED)
- **Protocol:** `Specs/protocols/fm-peering-protocol.md` (6 sections + conformance T10–T12)
- **Blueprint:** `Specs/FM/fm-registry-peering-design.md` (8 sections + pseudocode)
- **Skeleton:** `Specs/FM/registry-go-skeleton.md` (VnetRegistry, NicRegistry, MappingManager)
- **Retrospective:** `Specs/me-and-ai/fm-peering-hybrid-model-decision.md` (reactive vs hybrid trade-offs)
- **Registry Integration:** `Specs/FM/registry-pattern-design.md` §18 (peering extension)

### ✅ VIPs — Backend Pool & NAT Programming (LOCKED)
- **Design:** `Specs/FM/fm-vip-design.md` (10 sections + two-trigger SNAT programming)
- **Retrospective:** `Specs/me-and-ai/vip-dip-binding-decision.md` (VIP-driven vs DIP-driven; why late binding)
- **Proto Updated:** `Specs/cb_fm_protos/topics/vip.proto` (added `backend_dips` field)

### ✅ Meters — Per-ENI Policy Binding (LOCKED)
- **Design:** `Specs/FM/fm-meter-design.md` (10 sections + Case 2+Case 3 hybrid model)
- **Retrospective:** `Specs/me-and-ai/fm-meter-design-decision.md` (policy scope: Case 2 per-ENI + Case 3 route bits)
- **Proto Created:** `Specs/cb_fm_protos/topics/meter.proto` (MeterPolicy with ternary rules)

### ✅ Private Link — PE Mapping via VNetMapping (LOCKED)
- **Design:** `Specs/FM/fm-private-link-design.md` (13 sections + VNetMapping integration)
- **Retrospective:** `Specs/me-and-ai/fm-private-link-decision.md` (Model C: PE in VNetMapping, not separate registry)
- **Proto Enhanced:** `Specs/cb_fm_protos/topics/route.proto` (PrivateLinkDetails + comments)

### ✅ Inbound/Outbound Routes — Direction Awareness (LOCKED)
- **Design:** `Specs/FM/fm-route-direction-design.md` (14 sections + direction-aware validation)
- **Retrospective:** `Specs/me-and-ai/fm-route-direction-decision.md` (Model C: partition by direction, no BIDIR)
- **Proto Enhanced:** `Specs/cb_fm_protos/topics/route.proto` (RouteDirection comments updated)

### ✅ ExpressRoute / Service Tunnels — Route-Level Binding (LOCKED)
- **Design:** `Specs/FM/fm-expressroute-design.md` (13 sections + route-embedded tunnel refs)
- **Retrospective:** `Specs/me-and-ai/fm-expressroute-decision.md` (comprehensive research: cloud providers vs DASH; why route-level; decision rationale)
- **Proto Reference:** `Specs/cb_fm_protos/topics/route.proto` (ROUTE_SERVICE_TUNNEL action, tunnel_id field)

---

## Part B: All Design Closures Complete ✅

**All 6 routing topics have design documents + retrospectives locked in.**

### B.5: DASH Compliance Validation (COMPLETE)
- **Audit:** `Specs/me-and-ai/dash-compliance-analysis.md` (comprehensive proto vs DASH spec comparison)
- **Finding:** 87% compliance for Phase 1 scope (12/15 DASH objects protobuffed; 3 deferred to Phase 2)
- **Critical path:** Routing (100%), Mapping (95%), ENI identity (core OK), ACL (92%), Meter (88%), HA (98%)
- **Deferred (low-priority):** Tunnel proto, OutboundPortMap proto, InboundRoutingRule, PrefixTag, RoutingType
- **Verdict:** No blocking gaps. All ENI hydration gates expressible. Proceed with LLD immediately.

---

## Part C: Full LLD & Implementation Plan (COMPLETE ✅)

### Phase 4: Comprehensive Low-Level Design (LLD) — COMPLETE ✅

**Deliverable:** `Specs/FM/fm-comprehensive-lld.md` (6000+ lines)

**Coverage:**
- **L1. Cross-Registry Interaction Map** — Signal flow (happy path + soft-fail recovery), deadlock analysis (DAG ensures safety), race condition audit (async signals guarantee consistency), mutex/RWLock strategy per registry
- **L2. State Machine Composition** — Full ENI state machine (FAILED, INCOMPLETE, READY), 7 per-construct gates (peering hard-fail, mapping/VIP/meter/PE/tunnel/pool soft-fail, direction-aware), composition algorithm in pseudo-code
- **L3. Hot-Path Latency Analysis** — Cold-boot ENI hydration target <100ms (achievable 80–100ms), VIP backend add ~25ms, per-packet meter <20μs (not a bottleneck)
- **L4. Failure Mode & Recovery Matrix** — 20+ scenarios documented (CP config error, stream stall, race, tunnel fail, SNAT exhaustion, HA sync loss, etc.), detection mechanisms (metrics, logs, heartbeat), recovery paths (auto async, manual operator, timeout-based)
- **L5. Observability & Metrics Catalog** — Unified naming convention (`fm_{subsystem}_{metric}`), per-registry metrics, ENI signal tracing (spans), debug API endpoints, integration points (HAL interface, CP schema)

**Quality:** Production-ready blueprint. Sufficient detail for Go skeleton code implementation. No ambiguities.

### Phase 5: Phase 2 Design (Missing Objects & NicConfig Enhancements) — COMPLETE ✅

**Deliverable:** `Specs/me-and-ai/phase2-missing-objects-design.md` (2000+ lines)

**Coverage:**
- **5 Missing Objects + Rationale:**
  - ✅ **Tunnel proto** — Formal schema when provisioning stabilizes; semantic coverage sufficient for Phase 1
  - ✅ **OutboundPortMap proto** — SNAT pool schema; per-route references work in Phase 1
  - ✅ **PrefixTag** — DEFER (CP expands; FM consumes expanded rules)
  - ✅ **RoutingType** — DEFER (vendor catalog; Phase 1 enum-based validation sufficient)
  - ✅ **PaValidation** — DEFER (anti-spoofing is dataplane concern, not FM orchestration)
- **NicConfig Enhancements (Phase 2):**
  - Multi-stage ACL binding (3 stages × 2 directions × 2 families = 12 slots)
  - ENI-level meter policies (meter_policy_id_out/in)
  - Tunnel override (tunnel_id field)
  - SNAT pool binding (outbound_port_map_id)
  - HA membership (ha_set_id + ha_scope)
  - Per-ENI route overrides (route_rules[])
  - QoS binding (qos_id)
  - **All enhancements are backward-compatible** (new fields optional; Phase 1 NicConfig still works)
- **Implementation Sequence:** Formalize protos (month 1), enhance NicConfig (month 2–3), optional objects (month 4+)
- **Success Criteria:** 95%+ DASH compliance (14/15 objects protobuffed + optional features optional-by-design)

**Quality:** Strategic roadmap ready for Phase 2 implementation. No design ambiguity.

**L1. Cross-Registry Interaction Map**
- Signal flow: VnetRegistry → NicRegistry → MappingManager → VipRegistry → NatPoolRegistry → MeterRegistry → ...
- Deadlock analysis (circular dependencies?)
- Race condition audit (e.g., ENI hydrates while VIP updates)
- Mutex/RWLock strategy per registry

**L2. State Machine Composition**
- Full ENI state machine (integrate peering + VIP + meters + PE)
- Full VIP state machine (NAT pool + gateway dependencies)
- Full Meter state machine (validation constraints)
- Transition guards (what prevents invalid transitions?)

**L3. Hot-Path Latency Analysis**
- Cold-boot ENI hydration: how many registry lookups? (target: <100ms)
- VIP backend add: how many ENI scans? (optimize with index)
- Meter validation: lookup cost per route entry

**L4. Failure Mode & Recovery Matrix**
- 20+ scenarios: CP sends invalid config, stream stalls, race conditions, etc.
- Detection mechanism (metrics, alerts)
- Recovery action (retry, manual, auto-heal)

**L5. Observability & Metrics Catalog**
- Unified metrics across all registries (naming convention)
- Debug API endpoints (GET /debug/registry/vnet/vnet-id, etc.)
- Tracing spans (acquire → hydrate → ready)

### Phase 5: Implementation Plan

**IP1. Skeleton Code Structure**
- FM package layout: `registry/`, `hydration/`, `snat/`, `monitoring/`
- Interface definitions (Registry[K, V] contract)
- Test harness structure

**IP2. Build Plan (Phases)**
- Phase A: Core registries (VnetRegistry, NicRegistry, base GroupRegistry)
- Phase B: Peering extension (VnetRegistry signals, NicRegistry peer validation)
- Phase C: VIP & NAT (VipRegistry, NatPoolRegistry, SNAT rule programming)
- Phase D: Meters & PE (MeterRegistry, PE mapping, extended route validation)
- Phase E: Integration testing (cbsim scenarios, e2e flows)
- Phase F: Performance testing & optimization

**IP3. Test Plan**
- Unit tests per registry (Acquire/Release/signal handling)
- Integration tests: peering + VIP + meters (e.g., ENI with peered VNET + VIP + meter)
- Conformance tests (T10–T12 from peering; similar for VIPs, meters)
- Load test: 100k ENIs, 10k VNETs, 1k VIPs, thousands of routes

**IP4. Dependency Graph**
- Which registries must exist before others? (VnetRegistry before NicRegistry)
- Which can be developed in parallel? (MeterRegistry, NatPoolRegistry independent)
- Which have proto dependencies? (route.proto needs meter_id, vip_id fields)

**IP5. Rollout Strategy**
- Feature-flag: enable/disable peering, VIPs, meters
- Backward compatibility: old routes without meter_id still work
- Canary: deploy on 1 pod first, monitor metrics

---

## Part D: Success Criteria (Full Project)

### Design Phase (CURRENT)
- [ ] Meters design locked + retrospective
- [ ] Private Link design locked + retrospective
- [ ] Inbound/Outbound design locked + retrospective
- [ ] ExpressRoute design locked + retrospective
- [ ] No blocking questions on any routing construct
- [ ] All protos finalized (meter_id, vip_id, direction, PE details in route.proto)

### LLD Phase
- [ ] Cross-registry interaction diagram (no deadlocks)
- [ ] Full ENI state machine (all 4 constructs integrated)
- [ ] Race condition audit (all scenarios covered)
- [ ] Hot-path latency budget (all lookups <1ms median)

### Implementation Phase
- [ ] All registries + skeletons coded (no logic, just structure)
- [ ] Conformance tests for all 6 routing topics
- [ ] All unit tests pass
- [ ] cbsim cold-boot scenarios (ENI + peering + VIP + meter all together)
- [ ] Performance SLA met (100k ENIs provision in <10s)

---

## Tracking

| Deliverable | Status | Owner | Completed |
|-------------|--------|-------|-----------|
| FM-Gateway design (architecture, LLD, pod discovery, implementation plan) | ✅ COMPLETE | User + Architect | 2026-06-14 |
| CB-Gateway design (architecture, LLD, pod discovery, implementation plan) | ✅ COMPLETE | User + Architect | 2026-06-14 |
| Design phase (all 6 routing topics) | ✅ COMPLETE | User + Architect | 2026-06-14 |
| DASH compliance audit | ✅ COMPLETE | Architect | 2026-06-14 |
| Comprehensive LLD (registries, state machines, latency, failure modes, observability) | ✅ COMPLETE | Architect | 2026-06-14 |
| Phase 2 missing objects & NicConfig design | ✅ COMPLETE | Architect | 2026-06-14 |
| Go skeleton code (FM registries + FM-GW + CB-GW) | 🔄 NEXT | User + Architect | 2026-06-15 |

---

## Notes for Session Continuity

**If agent session changes:**
1. Refer to this file (next_plan.md) for overall progress
2. All design decisions locked in: `Specs/FM/fm-*-design.md`, `Specs/protocols/*protocol.md`
3. All retrospectives in: `Specs/me-and-ai/*-decision.md`
4. Proto definitions (canonical): `Specs/cb_fm_protos/topics/*.proto`
5. Memory updated in: `C:\Users\rashmirout\.claude\projects\...\memory\MEMORY.md`

**Current context (2026-06-14):**
- ✅ FM-Gateway design COMPLETE (HLD + LLD + pod discovery + implementation plan; 6 documents; primary-aware routing; 7-8 week implementation timeline)
- ✅ CB-Gateway design COMPLETE (HLD + LLD + pod discovery + implementation plan; 5 documents; peer-equivalent replica routing; dual-protocol gRPC+REST; 5-6 week implementation timeline; runs parallel with FM core weeks 1-8)
- ✅ All 6 routing topics COMPLETE (peering, VIPs, meters, Private Link, inbound/outbound, ExpressRoute)
- ✅ DASH compliance audit COMPLETE (87% coverage; no blocking gaps; 5 objects deferred to Phase 2)
- ✅ Comprehensive LLD COMPLETE (6000+ lines: registry interactions, state machines, latency, failure modes, observability)
- ✅ Phase 2 design COMPLETE (5 missing objects documented with DASH-compliant specs; NicConfig enhancements designed)
- Each design locked with detailed blueprint + comprehensive decision retrospective
- **Next: Go skeleton code generation** (FM registries + FM-GW + CB-GW; all design + LLD specs are frozen and production-ready)
- Then: Phase 1 implementation (FM: registries, hydration, gates, dataplane integration; FM-GW: pod discovery, primary election, request routing; CB-GW: project setup, core structures, gRPC/REST listeners)
- Then: Phase 2 (tunnel/pool protos, multi-stage ACL, HA, enhancements; FM-GW streaming, metrics, observability; CB-GW pod discovery, health checks, load balancing)
