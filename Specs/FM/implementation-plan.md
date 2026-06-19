# Fabric Manager (FM): Production Implementation Plan

**Version:** 3.0 - 4-Phase, 24-Week Execution  
**Language:** Go 1.21  
**Status:** Ready for Execution  
**Team Size:** 3-4 engineers  
**Timeline:** 24 weeks (6 months)  
**Reference Design:** FM_IMPLEMENTATION_ROADMAP_SUPER_ENHANCED.md  

---

## Executive Summary

**Objective:** Implement production-grade Fabric Manager supporting 1M+ ENIs across multi-vendor environments (Intel DPU, Nvidia DPU, custom hardware).

**E2E Data Flow:**
```
Device Input → Register (T1 etcd) → CB Subscribe (Sequential Topics) 
→ Receive Notifications → Process Data → Store T1/T2/T3 
→ Create Goal State (Per-ENI) → Program Device (Plugins)
→ Reconciliation (5-10 min cycle)
```

**Success Criteria (ALL Must Be Met):**
- ✅ 100% line coverage + 100% branch coverage
- ✅ 99.9% state consistency (verified by reconciliation)
- ✅ 90%+ automatic failure recovery
- ✅ <1 second latency p99 (ingestion to device)
- ✅ 50k+ events/sec throughput (with 80% dedup)
- ✅ Multi-vendor support (Intel/Nvidia/Custom plugins)
- ✅ Production deployment in Kubernetes

**4-Phase Structure:**
- **Phase 1 (Weeks 1-6):** Foundation + MVP (all 4 layers)
- **Phase 2 (Weeks 7-12):** Consistency + Reliability (5 rules, actor model, feedback)
- **Phase 3 (Weeks 13-18):** Scale + Multi-Vendor (100k ENIs, sharding, observability)
- **Phase 4 (Weeks 19-24):** Production + Operations (K8s, runbooks, dashboards)

---

## 1. Phase 1: Foundation & MVP (Weeks 1-6)

### Objective
Establish all 4 layers working end-to-end with 100 ENIs, demonstrating core intent-to-outcome flow with deduplication, consistency, aggregation, and plugin-based device programming.

### Deliverables

| Component | Owner | LOC | Week | Status | Dependencies |
|-----------|-------|-----|------|--------|--------------|
| **Layer 1: Config Plane** | Team A | 1200 | 1-2 | - | - |
| L1.1 Event subscription (CB client) | A1 | 300 | 1 | Pending | - |
| L1.2 Dedup cache (SHA256 + LRU) | A1 | 400 | 1-2 | Pending | L1.1 |
| L1.3 Event gating (rate limiting) | A2 | 200 | 2 | Pending | L1.2 |
| L1.4 Monitoring/metrics (Layer 1) | A2 | 300 | 2 | Pending | L1.3 |
| **Layer 2: Database/Model** | Team B | 1500 | 2-4 | - | L1 |
| L2.1 Actor framework (mailbox, scheduler) | B1 | 400 | 2-3 | Pending | L1 |
| L2.2 5 consistency rules engine | B1 | 400 | 3-4 | Pending | L2.1 |
| L2.3 Registry (VnetRegistry, NicRegistry) | B2 | 400 | 3-4 | Pending | L2.2 |
| L2.4 Metadata indexing (enable lookups) | B2 | 300 | 4 | Pending | L2.3 |
| **Layer 3: Southbound Provider** | Team C | 1000 | 4-5 | - | L2 |
| L3.1 Per-VNET aggregator | C1 | 400 | 4 | Pending | L2.4 |
| L3.2 Per-ENI composition | C1 | 300 | 5 | Pending | L3.1 |
| L3.3 Fingerprint cache (idempotency) | C2 | 200 | 5 | Pending | L3.2 |
| L3.4 Vendor routing (route to plugins) | C2 | 100 | 5 | Pending | L3.3 |
| **Layer 4: Plugin System** | Team A | 800 | 5-6 | - | L3 |
| L4.1 Plugin interface + loader | A1 | 200 | 5 | Pending | L3.4 |
| L4.2 Intel DPU plugin (mock) | A2 | 300 | 5-6 | Pending | L4.1 |
| L4.3 Nvidia DPU plugin (mock) | A2 | 200 | 6 | Pending | L4.1 |
| L4.4 Custom plugin template | A1 | 100 | 6 | Pending | L4.1 |
| **Feedback Loop & Testing** | Team B | 1500 | 1-6 | - | - |
| FB.1 Unit tests (all layers) | B2 | 600 | 1-6 | Pending | All |
| FB.2 Integration test (e2e flow) | B2 | 400 | 4-6 | Pending | L4 |
| FB.3 Reconciliation (basic) | B1 | 300 | 6 | Pending | L2.4 |
| FB.4 Mock replica + test fixtures | B1 | 200 | 1-6 | Pending | - |

**Phase 1 Total: 6,000 LOC across 18 tasks**

### Weekly Breakdown

**Week 1: Layer 1 Subscription & Dedup Cache**
- **Mon-Tue:** CB proto contract review, design dedup algorithm (SHA256 + LRU), sketch cache eviction policy
- **Wed-Thu:** Implement L1.1 (CB client subscription, watch streams)
- **Fri-Mon:** Implement L1.2 (dedup cache, fingerprinting), 60% coverage unit tests
- **Status Check:** CB subscription working, mock data flowing

**Week 2: Layer 1 Rate Limiting & Monitoring**
- **Mon-Tue:** Implement L1.3 (event gating, token bucket), tests
- **Wed-Thu:** Implement L1.4 (Prometheus metrics: dedup_hit_rate, events_per_sec, latency histogram)
- **Fri-Mon:** Integration: L1 e2e test (subscription → dedup → metrics)
- **Status Check:** Layer 1 working 100 ENIs, dedup at 80%

**Week 3: Actor Framework & Consistency Rules**
- **Mon-Tue:** Implement L2.1 (actor mailbox, scheduler, per-type serialization)
- **Wed-Thu:** Implement L2.2 (5 consistency rules: no self-ref, no dangling refs, no cycles, version monotonicity, VNET isolation)
- **Fri-Mon:** Unit tests for actor model (concurrent messages, no deadlocks), consistency rule tests
- **Status Check:** Actor framework proven, no race conditions

**Week 4: Registry & Indexing**
- **Mon-Tue:** Implement L2.3 (VnetRegistry, NicRegistry, MappingManager from skeleton)
- **Wed-Thu:** Implement L2.4 (metadata indexing: vnet_id → [nics], eni_id → [mappings] for fast lookups)
- **Fri-Mon:** Integration: L2 test (register construct, consistency rules enforced, index lookups fast)
- **Status Check:** 100% referential integrity, <1ms lookup latency

**Week 5: Aggregation & Composition**
- **Mon-Tue:** Implement L3.1 (per-VNET aggregator: fetch RouteTable+ACL+ENI, compose Goal State template)
- **Wed-Thu:** Implement L3.2 (per-ENI composition: fill in ENI-specific bindings, fingerprint hash)
- **Fri-Mon:** Implement L4.1 + L4.2 (plugin interface, Intel mock plugin), L3.3 (fingerprint cache)
- **Status Check:** Goal States generated, fingerprint cache preventing redundant programs (23x speedup simulated)

**Week 6: Plugins & E2E Test**
- **Mon-Tue:** Implement L4.3 + L4.4 (Nvidia mock, custom template)
- **Wed-Thu:** Implement FB.1 + FB.2 (e2e integration test: device input → register → subscribe → compose → program device)
- **Fri-Mon:** Implement FB.3 (basic reconciliation: poll device state, compare hash, detect divergence), cleanup
- **Status Check:** Full e2e flow working, 100 ENIs registered, Goal States sent to plugins, reconciliation detects drift

### Test Coverage Target
- **Unit Tests:** 100+ tests, 70% line coverage (will improve in Phase 2)
- **Integration Tests:** 5+ e2e scenarios (normal flow, divergence, multi-vendor)
- **Performance:** <1s ingestion-to-program latency for 100 ENIs

### Acceptance Criteria Phase 1
- [ ] All 18 tasks completed
- [ ] Layer 1 dedup working (80% reduction verified)
- [ ] Layer 2 consistency rules enforced (no invalid states created)
- [ ] Layer 3 aggregation working (Goal States composed in <100ms)
- [ ] Layer 4 plugins callable (mock Intel/Nvidia/Custom plugins receive programs)
- [ ] E2E test passing (device input → Goal State → plugin)
- [ ] 70% line coverage
- [ ] Latency p99 <1 second for 100 ENIs
- [ ] No memory leaks (Go built-in leak detector)

---

## 2. Phase 2: Consistency & Reliability (Weeks 7-12)

### Objective
Harden consistency guarantees, implement full feedback loop with reconciliation, add retry/timeout/circuit-breaker patterns, achieve 100% test coverage and 90%+ auto-recovery rate.

### Deliverables

| Component | Owner | LOC | Week | Status | Dependencies |
|-----------|-------|-----|------|--------|--------------|
| **Consistency Rules Deep Dive** | Team B | 600 | 7-9 | - | P1 |
| C.1 Self-reference prevention (graph traversal) | B1 | 200 | 7 | Pending | - |
| C.2 Dangling reference detection | B1 | 200 | 7 | Pending | - |
| C.3 Circular dependency detection (topological sort) | B2 | 200 | 8 | Pending | - |
| **Feedback Loop & Reconciliation** | Team A | 900 | 8-10 | - | P1 |
| FB.1 Reconciliation engine (poll state, compare hash) | A1 | 300 | 8 | Pending | - |
| FB.2 Divergence detection state machine | A1 | 200 | 9 | Pending | FB.1 |
| FB.3 Auto-recovery flow (exponential backoff) | A2 | 200 | 9 | Pending | FB.2 |
| FB.4 Reconciliation dashboard (metrics: healthy %, recovery rate) | A2 | 200 | 10 | Pending | FB.3 |
| **Reliability Patterns** | Team C | 1000 | 9-11 | - | P1 |
| R.1 Circuit breaker (3-state FSM) | C1 | 300 | 9 | Pending | - |
| R.2 Retry with exponential backoff | C1 | 200 | 9 | Pending | - |
| R.3 Timeout management (global + per-replica) | C2 | 200 | 10 | Pending | - |
| R.4 Error classification (retryable vs non-retryable) | C2 | 300 | 10 | Pending | - |
| **Observability Enhancement** | Team B | 700 | 10-12 | - | P1 |
| O.1 Distributed tracing (OpenTelemetry + Jaeger) | B2 | 300 | 10 | Pending | - |
| O.2 Structured JSON logging (trace IDs, context) | B2 | 200 | 11 | Pending | - |
| O.3 Prometheus metrics expansion (20+ metrics) | B1 | 200 | 11 | Pending | - |
| **Chaos Engineering & Testing** | Team A | 1200 | 11-12 | - | All |
| CH.1 Chaos: Kill replica mid-request | A2 | 300 | 11 | Pending | - |
| CH.2 Chaos: Inject network latency | A2 | 300 | 11 | Pending | - |
| CH.3 Chaos: Device crash simulation | A1 | 300 | 12 | Pending | - |
| CH.4 Mutation testing (verify test quality) | A1 | 300 | 12 | Pending | - |

**Phase 2 Total: 4,400 LOC, 20 tasks**

### Weekly Breakdown

**Week 7: Consistency Deep Dive**
- **Mon-Tue:** Implement C.1 (self-reference detection: graph traversal on construct refs)
- **Wed-Thu:** Implement C.2 (dangling reference detection: check parent exists before write)
- **Fri-Mon:** Unit tests (1,000+ property-based tests for consistency rules)
- **Status Check:** All 5 rules tested with edge cases, no invalid states possible

**Week 8: Reconciliation Engine**
- **Mon-Tue:** Implement FB.1 (reconciliation engine: poll device hash, compare with local hash)
- **Wed-Thu:** Implement FB.2 (divergence state machine: HEALTHY → DIVERGED → RECOVERING → HEALTHY)
- **Fri-Mon:** Unit tests + integration test (trigger divergence, verify auto-recovery)
- **Status Check:** Divergence detected accurately, recovery initiated

**Week 9: Auto-Recovery Flow**
- **Mon-Tue:** Implement FB.3 (auto-recovery with exponential backoff: 100ms → 30s)
- **Wed-Thu:** Implement R.1 + R.2 (circuit breaker FSM, retry with backoff)
- **Fri-Mon:** Integration test (device fails → CB opens → retries → succeeds)
- **Status Check:** Auto-recovery rate measured (target 90%)

**Week 10: Timeout + Error Handling**
- **Mon-Tue:** Implement R.3 (timeout management: global timeout, per-attempt timeout)
- **Wed-Thu:** Implement R.4 (error classification: retryable errors trigger retry; non-retryable fail immediately)
- **Fri-Mon:** Tests (timeout boundaries, error classification matrix)
- **Status Check:** All timeout scenarios tested, error paths correct

**Week 11: Observability & Chaos**
- **Mon-Tue:** Implement O.1 + O.2 (OpenTelemetry tracing with Jaeger, structured logging)
- **Wed-Thu:** Implement CH.1 + CH.2 (chaos tests: replica kill, latency injection)
- **Fri-Mon:** Implement O.3 (Prometheus metrics: 20+ counters, gauges, histograms)
- **Status Check:** Full request flow traced end-to-end, all metrics exported

**Week 12: Chaos & Mutation Testing**
- **Mon-Tue:** Implement CH.3 + CH.4 (device crash simulation, mutation testing)
- **Wed-Thu:** Run full chaos suite (kill replicas, inject faults, verify recovery)
- **Fri-Mon:** Mutation testing (verify test quality, >90% kill rate target)
- **Status Check:** 100% coverage achieved, mutation kill rate >90%

### Test Coverage Target
- **Total Tests:** 500+ tests (unit + integration + chaos)
- **Line Coverage:** 100%
- **Branch Coverage:** 100%
- **Mutation Kill Rate:** >90%
- **Auto-Recovery Rate:** >90% (simulated under various faults)

### Acceptance Criteria Phase 2
- [ ] All 20 tasks completed
- [ ] 100% line + branch coverage (verified by coverage tool)
- [ ] 90%+ mutation kill rate (verified by mutation testing)
- [ ] Reconciliation cycle working (5-10 min, detects divergence)
- [ ] Auto-recovery rate >90% (measured by chaos tests)
- [ ] Full distributed tracing working (Jaeger visualizations)
- [ ] Structured logging + 20+ Prometheus metrics
- [ ] All chaos tests passing (replica kill, latency, crash scenarios)
- [ ] <1 second latency p99 maintained

---

## 3. Phase 3: Scale & Multi-Vendor (Weeks 13-18)

### Objective
Scale to 100k ENIs, implement horizontal sharding, add multi-vendor support (Intel/Nvidia/Custom), deploy multi-zone redundancy, achieve full observability with dashboards.

### Deliverables

| Component | Owner | LOC | Week | Status | Dependencies |
|-----------|-------|-----|------|--------|--------------|
| **Horizontal Sharding** | Team A | 1200 | 13-14 | - | P2 |
| S.1 Shard assignment algorithm (consistent hash) | A1 | 400 | 13 | Pending | - |
| S.2 Shard-aware aggregation (per-shard compositors) | A1 | 400 | 13-14 | Pending | S.1 |
| S.3 Cross-shard queries (map-reduce pattern) | A2 | 300 | 14 | Pending | S.2 |
| S.4 Shard rebalancing (when shards added/removed) | A2 | 100 | 14 | Pending | S.3 |
| **Multi-Vendor Plugins** | Team B | 1500 | 14-16 | - | P2 |
| MV.1 Intel DPU plugin (production-grade) | B1 | 500 | 14-15 | Pending | - |
| MV.2 Nvidia DPU plugin (production-grade) | B1 | 500 | 15-16 | Pending | - |
| MV.3 Custom plugin template + docs | B2 | 300 | 16 | Pending | - |
| MV.4 Plugin registry + hot-loading | B2 | 200 | 16 | Pending | - |
| **Multi-Zone Redundancy** | Team C | 800 | 15-17 | - | P2 |
| MZ.1 Zone awareness (zone-aware scheduling) | C1 | 300 | 15 | Pending | - |
| MZ.2 Zone failover (detect zone down, reroute) | C1 | 300 | 16 | Pending | MZ.1 |
| MZ.3 Cross-zone state sync (etcd replication) | C2 | 200 | 17 | Pending | MZ.2 |
| **Observability Dashboards** | Team A | 600 | 16-18 | - | P2 |
| OB.1 Grafana dashboards (real-time metrics) | A2 | 200 | 16 | Pending | - |
| OB.2 Alert rules (SLO violations) | A1 | 200 | 17 | Pending | - |
| OB.3 Jaeger trace visualization | A2 | 200 | 18 | Pending | - |
| **Scale Testing** | Team B | 1000 | 17-18 | - | All |
| ST.1 Load test 100k ENIs (registration throughput) | B2 | 300 | 17 | Pending | - |
| ST.2 Latency benchmarks (p50, p99, p99.9) | B2 | 300 | 17 | Pending | - |
| ST.3 Multi-vendor mixed workload | B1 | 200 | 18 | Pending | - |
| ST.4 24-hour sustained load test | B1 | 200 | 18 | Pending | - |

**Phase 3 Total: 5,100 LOC, 18 tasks**

### Weekly Breakdown

**Week 13: Sharding Algorithm**
- **Mon-Tue:** Design sharding strategy (consistent hash ring, key: vnet_id, shards: 64 by default)
- **Wed-Thu:** Implement S.1 (shard assignment using consistent hash, minimal key remapping on resize)
- **Fri-Mon:** Unit tests (verify keys map correctly, minimal churn on rebalance)
- **Status Check:** Shard distribution even, <1% key remapping on resize

**Week 14: Per-Shard Composition**
- **Mon-Tue:** Implement S.2 (shard-aware aggregation: each shard runs its own aggregator, processes its vnets independently)
- **Wed-Thu:** Implement S.3 (cross-shard queries: coordinator queries all shards, aggregates results)
- **Fri-Mon:** Implement S.4 (rebalancing: migrate keys from old shard to new, with minimal data movement)
- **Status Check:** 100k ENIs sharded across 4 shards, no hotspots

**Week 15: Multi-Vendor Plugins**
- **Mon-Tue:** Implement MV.1 (Intel DPU plugin: full feature set, idempotency, error handling)
- **Wed-Thu:** Implement MV.2 (Nvidia DPU plugin: same interface as Intel, different device API)
- **Fri-Mon:** Implement MZ.1 (zone awareness: scheduling respects zone affinity)
- **Status Check:** Both Intel and Nvidia plugins working in parallel, zone-aware scheduling verified

**Week 16: Plugin Registry & Multi-Zone**
- **Mon-Tue:** Implement MV.3 + MV.4 (custom plugin template, plugin registry with hot-loading)
- **Wed-Thu:** Implement MZ.2 (zone failover: detect zone loss, shift traffic to remaining zones)
- **Fri-Mon:** Implement OB.1 (Grafana dashboards: dedup hit rate, Goal State latency, plugin throughput, zone status)
- **Status Check:** Multiple plugins running, zone failover tested, dashboards showing real-time metrics

**Week 17: Cross-Zone Sync & Testing**
- **Mon-Tue:** Implement MZ.3 (cross-zone state sync: etcd clusters replicate, consistency maintained)
- **Wed-Thu:** Implement ST.1 + ST.2 (load test 100k ENIs, measure latency p50/p99/p99.9)
- **Fri-Mon:** Implement OB.2 (alert rules: latency SLO >1s, auto-recovery <85%, divergence >1%)
- **Status Check:** 100k ENIs registered in <10 minutes, latency p99 <1s, alerts firing correctly

**Week 18: Scale & Dashboards**
- **Mon-Tue:** Implement ST.3 + ST.4 (multi-vendor mixed workload, 24-hour sustained load)
- **Wed-Thu:** Implement OB.3 (Jaeger trace visualization: full request flow visible)
- **Fri-Mon:** Clean up, optimize hot paths, prepare for Phase 4
- **Status Check:** 100k ENIs with multi-vendor plugins working, 24-hour test passing, dashboards complete

### Performance Targets Phase 3
- **Throughput:** 50k+ events/sec (with 80% dedup → 10k unique/sec actual processing)
- **Latency p99:** <1 second (ingestion to device)
- **Auto-recovery:** 90%+ (verified by chaos tests)
- **Scale:** 100k ENIs across 4 shards
- **Multi-vendor:** Intel/Nvidia/Custom all working simultaneously

### Acceptance Criteria Phase 3
- [ ] All 18 tasks completed
- [ ] 100k ENIs registered and maintained
- [ ] Consistent hash sharding working (even distribution)
- [ ] Intel + Nvidia + Custom plugins working simultaneously
- [ ] Zone failover tested (detect zone down, traffic rerouted)
- [ ] Grafana dashboards created and functional
- [ ] Latency p99 <1s maintained under 100k ENI load
- [ ] 24-hour sustained load test passing
- [ ] Auto-recovery rate >90% at scale

---

## 4. Phase 4: Production & Operations (Weeks 19-24)

### Objective
Deploy to production Kubernetes cluster, document all operational procedures, establish SLA monitoring and alerting, perform production rehearsal, go live.

### Deliverables

| Component | Owner | LOC | Week | Status | Dependencies |
|-----------|-------|-----|------|--------|--------------|
| **Kubernetes Deployment** | Team C | 800 | 19-20 | - | P3 |
| K.1 StatefulSet definition (3+ replicas, headless service) | C1 | 300 | 19 | Pending | - |
| K.2 ConfigMap + Secret management | C2 | 200 | 19 | Pending | - |
| K.3 Persistent volumes (etcd, RocksDB state) | C1 | 150 | 20 | Pending | - |
| K.4 Network policies + RBAC | C2 | 150 | 20 | Pending | - |
| **HA & Disaster Recovery** | Team A | 600 | 20-21 | - | P3 |
| HA.1 K8s Lease coordination (primary/standby) | A1 | 300 | 20 | Pending | - |
| HA.2 Backup strategy (automated daily snapshots) | A2 | 150 | 21 | Pending | - |
| HA.3 Disaster recovery runbook + drills | A2 | 150 | 21 | Pending | - |
| **Documentation & Operations** | Team B | 1000 | 20-23 | - | P3 |
| D.1 Deployment guide (install, configure, bootstrap) | B2 | 200 | 20 | Pending | - |
| D.2 Operations runbook (common tasks, troubleshooting) | B2 | 300 | 21 | Pending | - |
| D.3 Performance tuning guide | B1 | 150 | 22 | Pending | - |
| D.4 SLA/SLO documentation (uptime %, latency targets) | B1 | 200 | 22 | Pending | - |
| D.5 Incident response playbook (on-call procedures) | B2 | 150 | 23 | Pending | - |
| **Production Rehearsal** | Team A,B,C | 1200 | 22-24 | - | All |
| PR.1 Canary deploy to staging (smoke tests) | A2 | 300 | 22 | Pending | - |
| PR.2 Full production dry-run (1,000 devices, 1 day) | B1 | 400 | 23 | Pending | - |
| PR.3 Failover drill (kill primary, verify standby takeover) | C1 | 300 | 23 | Pending | - |
| PR.4 Rollback procedures (practiced on staging) | A1 | 200 | 24 | Pending | - |
| **Go-Live & Monitoring** | Team C | 500 | 24 | - | All |
| GL.1 Production deployment | C1 | 150 | 24 | Pending | - |
| GL.2 Post-deployment monitoring (first 24h, then 7d) | C2 | 200 | 24 | Pending | - |
| GL.3 Team knowledge transfer to SRE/DevOps | C2 | 150 | 24 | Pending | - |

**Phase 4 Total: 4,100 LOC, 18 tasks**

### Weekly Breakdown

**Week 19: Kubernetes Manifests**
- **Mon-Tue:** Implement K.1 (StatefulSet with 3 replicas, headless service for stable networking)
- **Wed-Thu:** Implement K.2 (ConfigMap for config, Secrets for credentials/keys)
- **Fri-Mon:** Testing (deploy to dev K8s cluster, verify pods come up)
- **Status Check:** StatefulSet working, all 3 pods ready, services discoverable

**Week 20: Persistent Storage & HA**
- **Mon-Tue:** Implement K.3 (PersistentVolumes for etcd data + RocksDB state, backup volumes)
- **Wed-Thu:** Implement K.4 (network policies: allow intra-pod communication, deny by default; RBAC: service account with pod/lease read/write)
- **Fri-Mon:** Implement HA.1 (K8s Lease coordination: primary pod acquires lease, standby pods wait)
- **Status Check:** Storage persisting across pod restarts, lease coordination working

**Week 21: HA & Backup**
- **Mon-Tue:** Implement HA.2 (automated daily snapshots: export etcd data, RocksDB backup, store in external S3)
- **Wed-Thu:** Implement HA.3 (disaster recovery runbook: restore from backup, verify data integrity)
- **Fri-Mon:** Testing (simulate prod backup/restore, measure RTO)
- **Status Check:** Backup + restore working, RTO <1 hour

**Week 22: Documentation & Tuning**
- **Mon-Tue:** Implement D.1 + D.2 (deployment guide, operations runbook with common tasks)
- **Wed-Thu:** Implement D.3 (performance tuning: etcd tuning, RocksDB cache settings, Go gc tuning)
- **Fri-Mon:** Implement PR.1 (canary deploy to staging, smoke tests passing)
- **Status Check:** Documentation complete and reviewed, staging canary working

**Week 23: SLO & Production Rehearsal**
- **Mon-Tue:** Implement D.4 + D.5 (SLA/SLO docs, incident response playbook)
- **Wed-Thu:** Implement PR.2 (full dry-run: 1,000 devices, 1 day in staging, measure perf, verify stability)
- **Fri-Mon:** Implement PR.3 (failover drill: kill primary, verify standby takes over within 30s)
- **Status Check:** All documentation reviewed, dry-run completed successfully

**Week 24: Rollback & Go-Live**
- **Mon-Tue:** Implement PR.4 (rollback procedures: practiced on staging, rollback time < 5 min)
- **Wed-Thu:** Implement GL.1 + GL.2 (production deployment in multiple zones, post-deploy monitoring setup)
- **Fri-Mon:** Implement GL.3 (knowledge transfer to SRE team, handoff complete)
- **Status Check:** FM in production, monitoring all metrics, SRE team trained

### Production Readiness Checklist

**Pre-Production (Week 22-23):**
- [ ] All 18 Phase 4 tasks completed
- [ ] Canary deploy to staging working
- [ ] 1,000-device dry-run completed successfully
- [ ] Failover drill RTO <30 seconds
- [ ] All documentation reviewed by SRE/DevOps
- [ ] Backup/restore tested (RTO <1 hour)
- [ ] Monitoring dashboards created (Grafana)
- [ ] Alert rules tested (firing on simulated faults)
- [ ] Incident response playbook reviewed
- [ ] Team trained on deployment/operations

**Go-Live (Week 24):**
- [ ] Production deployment in multiple zones
- [ ] Post-deployment monitoring (first 24h critical, then 7d normal)
- [ ] SRE/DevOps team takes over on-call rotation
- [ ] No incidents during first 24h (target)
- [ ] Latency p99 <1s maintained (target)
- [ ] Auto-recovery rate >90% (target)

### Acceptance Criteria Phase 4
- [ ] All 18 tasks completed
- [ ] Production K8s deployment working (3+ replicas across zones)
- [ ] HA failover working (RTO <30 seconds)
- [ ] Backup + restore tested (RTO <1 hour)
- [ ] All documentation complete and reviewed
- [ ] Production dry-run (1,000 devices, 1 day) successful
- [ ] Failover drill successful
- [ ] Monitoring dashboard and alerts functional
- [ ] Team knowledge transfer complete
- [ ] Go-live executed with zero production incidents

---

## 5. Resource Allocation & Team Structure

### Team Composition (3-4 Engineers)

**Option A: 3-Engineer Team**
- **Team Lead (A1):** Architecture, Layer 1-2, HA, Go-Live (Weeks 1-24)
- **Senior Engineer (B1):** Layer 2 consistency, feedback loops, testing, observability (Weeks 1-24)
- **Engineer (C1):** Layer 3-4, multi-vendor plugins, Kubernetes, production ops (Weeks 1-24)

**Option B: 4-Engineer Team**
- **Architect (A):** Design, reviews, Phase 1-2, HA (Weeks 1-12)
- **Backend Lead (B):** Layer 2-3, consistency, feedback loops (Weeks 1-24)
- **Platform Engineer (C):** Layer 4 plugins, sharding, Kubernetes (Weeks 1-24)
- **Test/Ops Engineer (D):** Testing infrastructure, observability, operations (Weeks 1-24)

### Weekly Allocation (Weeks 1-24)
- **Weeks 1-6 (Phase 1):** 100% all team members (foundation critical)
- **Weeks 7-12 (Phase 2):** 100% all team members (consistency + coverage)
- **Weeks 13-18 (Phase 3):** 80% core team + 20% ops prep (scale work)
- **Weeks 19-24 (Phase 4):** 60% core team + 40% ops prep (K8s + handoff)

---

## 6. Risk Mitigation

| Risk | Probability | Impact | Mitigation | Owner |
|------|-------------|--------|-----------|-------|
| **100% coverage hard to achieve** | High | High | Code generation for boilerplate tests, mutation testing from day 1, pair programming on complex logic | B1 |
| **Performance targets missed** | Medium | High | Benchmarking in Week 2, profiling from Phase 1, targeted optimizations in Phase 3 | A1 |
| **Multi-vendor plugin integration complexity** | Medium | Medium | Plugin interface finalized by Week 5, mock plugins in Phase 1, real plugins in Phase 3 | C1 |
| **Inconsistency bugs after Phase 2** | Low | Critical | Property-based testing + mutation testing catch most issues, chaos tests verify recovery | B1 |
| **K8s Lease contention** | Low | Medium | Dry-run in Phase 4 weeks 22-23, failover drill practices takeover | A1 |
| **Timeline slip** | Medium | High | Weekly status checks, early detection, ready to defer nice-to-haves (custom plugin templates) | A1 |

---

## 7. Success Metrics (Final Verification)

### Phase 1 Completion (Week 6)
- ✅ E2E flow working (device input → Goal State → plugin)
- ✅ 100 ENIs managed end-to-end
- ✅ Dedup at 80% efficiency
- ✅ All 4 layers integrated
- ✅ 70% line coverage
- ✅ <1s latency p99

### Phase 2 Completion (Week 12)
- ✅ 100% line + branch coverage
- ✅ 90%+ mutation kill rate
- ✅ Reconciliation working (5-10 min cycles)
- ✅ Auto-recovery >90% (verified by chaos tests)
- ✅ Full distributed tracing
- ✅ 20+ Prometheus metrics

### Phase 3 Completion (Week 18)
- ✅ 100k ENIs managed
- ✅ Multi-vendor plugins working (Intel + Nvidia + Custom)
- ✅ Horizontal sharding proven
- ✅ Zone failover tested
- ✅ Grafana dashboards + alerts
- ✅ 24-hour sustained load test passing

### Phase 4 Completion (Week 24)
- ✅ Production K8s deployment
- ✅ HA + disaster recovery tested
- ✅ All documentation complete
- ✅ Production dry-run successful
- ✅ Team trained and handoff complete
- ✅ Zero production incidents (first 24h target)

---

## 8. Go-Live Checklist

**Final Verification (Week 24):**
- [ ] All 4 phases completed on schedule
- [ ] Code review completed for all components
- [ ] Security audit passed (no sensitive data in logs/metrics)
- [ ] Performance benchmarks hit targets (latency p99 <1s, throughput 50k+ events/sec)
- [ ] Test coverage 100% line + branch
- [ ] Mutation kill rate >90%
- [ ] Chaos tests passing (all fault scenarios)
- [ ] Monitoring dashboard functional (all metrics flowing)
- [ ] SRE/DevOps team trained and ready
- [ ] Incident response playbook reviewed and practiced
- [ ] Backup/restore tested (RTO <1 hour)
- [ ] HA failover working (RTO <30s)
- [ ] Documentation complete and reviewed
- [ ] Production canary completed (staging dry-run successful)

**Go-Live Execution (Week 24):**
1. Deploy primary pod (shard 0)
2. Verify health (metrics, logs, traces flowing)
3. Deploy secondary pods (shards 1-3)
4. Verify cluster health (all shards healthy, leader elected)
5. Enable production traffic (gradual ramp-up from 10% → 50% → 100%)
6. Monitor continuously (first 24h critical, then 7d normal)
7. Incident response team on-call (24x7 rotation)
8. Post-mortem after first week (document lessons learned)

---

## 9. Conclusion

This **4-phase, 24-week implementation plan** provides a structured path from design to production for Fabric Manager. Each phase builds on the previous, with clear deliverables, acceptance criteria, and risk mitigation.

**Key Success Factors:**
1. **Architecture alignment:** 4 layers, deduplication, consistency rules, plugins
2. **Testing discipline:** 100% coverage + 90%+ mutation kill rate from day 1
3. **Team focus:** Dedicated 3-4 person team for 24 weeks
4. **Regular status:** Weekly check-ins, early problem detection
5. **Production readiness:** Phase 4 dry-runs and drills ensure confidence

**Next Steps:**
1. Allocate team (3-4 engineers) and assign phase owners
2. Set up development environment (Go 1.21, CI/CD pipeline)
3. Conduct kickoff meeting (design review, tool setup)
4. Begin Phase 1 Week 1 (Monday)
5. Establish weekly status cadence (Friday 4pm)
