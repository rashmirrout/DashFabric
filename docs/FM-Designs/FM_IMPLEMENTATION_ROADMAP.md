# FM Design: Implementation Roadmap (26 Weeks)

**Version**: 1.0  
**Status**: Design Complete  
**Parent Document**: [FM_ARCHITECTURE_SPEC.md](FM_ARCHITECTURE_SPEC.md)

---

## Overview

4-phase implementation plan over 26 weeks (6.5 months):
- **Phase 1 (Weeks 1-7)**: Foundation (Layer 2)
- **Phase 2 (Weeks 8-13)**: Config Plane → Database (Layers 1-2 integration)
- **Phase 3 (Weeks 14-20)**: Southbound Provider (Layer 3)
- **Phase 4 (Weeks 21-26)**: Plugin Architecture & Reconciliation (Layers 4 + feedback)

---

## Phase 1: Foundation & Layer 2 (Weeks 1-7)

### Week 1: Setup & Design

**Goals**:
- Project structure
- etcd integration
- Testing framework
- Protobuf definitions

**Deliverables**:
- Directory structure created
- go.mod with dependencies
- Protobuf files (construct.proto, configupdate.proto, goalstate.proto)
- Unit test framework (testutil/helpers.go, fixtures.go)

**Success Criteria**:
- ✓ Build passes
- ✓ First unit test runs
- ✓ etcd client connects

**Estimated Effort**: 40 hours

---

### Weeks 2-7: Layer 2 Implementation

#### Week 2: Core Storage

**Build**: 
- Construct protobuf schema (with versioning, hash fields)
- etcd storage layer (Put, Get, Watch)
- Index manager (in-memory indices)

**Tests**:
- Store and retrieve construct
- Indices updated on write
- Watch notifications fire

**Success Criteria**:
- ✓ 50+ constructs stored and retrieved
- ✓ Index queries O(log n)
- ✓ Watch tests passing

---

#### Week 3: Consistency Checker

**Build**:
- Consistency validator (no self-ref, no dangling refs, no cycles)
- Relation graph analyzer (detect cycles)
- Version monotonicity enforcer

**Tests**:
- Circular dependency rejection
- Dangling reference rejection
- Cycle detection (50-node graph)

**Success Criteria**:
- ✓ 100% consistency rule coverage
- ✓ Cycle detection works on complex graphs
- ✓ False positive rate = 0

---

#### Week 4: Actor Model

**Build**:
- RouteTableActor (serializes updates)
- ACLActor, MappingActor, ENIActor (similar)
- Actor pool and routing

**Tests**:
- Concurrent updates to different actors (parallel)
- Sequential updates to same actor (serialized)
- Actor message ordering

**Success Criteria**:
- ✓ 10 concurrent actor updates complete in < 100ms
- ✓ Same-actor updates serialized correctly

---

#### Week 5: Cascading Deletes

**Build**:
- Cascade manager (soft delete with traversal)
- Ownership tracking
- Child enumeration

**Tests**:
- VNET delete cascades to children
- ENI delete clears references
- Soft delete (not hard delete)

**Success Criteria**:
- ✓ Cascade complete in < 50ms
- ✓ All children marked deleted
- ✓ Indices updated

---

#### Week 6: Integration & Testing

**Build**:
- Database API (Get, List, Watch, ProcessConfigUpdate, Delete)
- Error handling
- Metrics & logging

**Tests**:
- End-to-end: ConfigUpdate → validation → write → indices → watch
- Real etcd (not mocked)
- 1000 concurrent updates

**Success Criteria**:
- ✓ API stable and documented
- ✓ No race conditions (verified with -race)
- ✓ Throughput: > 1000 updates/sec

---

#### Week 7: Performance & Polish

**Build**:
- Optimize hot paths (caching, lock-free ops where possible)
- Benchmark suite
- Documentation

**Tests**:
- Latency percentiles (p50, p95, p99)
- Memory usage (with 100k constructs)
- CPU profile under load

**Success Criteria**:
- ✓ Get latency p99 < 10ms
- ✓ Put latency p99 < 50ms
- ✓ Memory < 1GB for 100k constructs

**Phase 1 Deliverables**:
- ✓ Layer 2 (Database/Model) fully functional
- ✓ 100% test coverage
- ✓ Zero race conditions
- ✓ Benchmark baseline established

---

## Phase 2: Config Plane Integration (Weeks 8-13)

### Week 8: Config Plane Components

**Build**:
- SubscriptionManager (etcd watch listener)
- DeduplicationEngine (hash cache)
- ValidationEngine (schema validator)
- Sequencer (global sequence)

**Tests**:
- Duplicate detection (1000 identical events)
- Schema validation (valid/invalid cases)
- Sequence assignment (gaps, ordering)

**Success Criteria**:
- ✓ Dedup hit rate > 95% on duplicates
- ✓ Hash check latency < 1ms
- ✓ Sequence monotonic, no gaps

---

### Week 9: Deduplication Algorithm

**Build**:
- Hash computation (canonical JSON, SHA256)
- LRU cache (TTL, eviction)
- Metrics tracking

**Tests**:
- Same content → same hash (deterministic)
- Different content → different hash
- Cache eviction on size limit

**Success Criteria**:
- ✓ Hash collision probability negligible
- ✓ Cache hit/miss metrics accurate
- ✓ Memory footprint predictable

---

### Week 10: Layer 1 ↔ Layer 2 Integration

**Build**:
- EventEmitter (Config Plane → Database channel)
- Error handling (Layer 2 validation fails)
- Backpressure (Layer 2 slow to consume)

**Tests**:
- ConfigUpdate flows to Database
- Invalid ConfigUpdate rejected (not stored)
- Backpressure handled gracefully

**Success Criteria**:
- ✓ End-to-end: etcd → Config Plane → Database
- ✓ 100% of valid updates stored
- ✓ 0 invalid updates stored

---

### Weeks 11-13: Integration Testing & Refinement

**Build**:
- Real etcd + Config Plane + Database
- Load tests (1000 events/sec, 50% duplicates)
- Chaos tests (etcd failures, network partitions)

**Tests**:
- Recovery from transient etcd outages
- Deduplication under load
- Memory stability (24h run)

**Success Criteria**:
- ✓ Throughput: > 5000 deduplicated events/sec
- ✓ No memory leaks
- ✓ All chaos scenarios handled

**Phase 2 Deliverables**:
- ✓ Layer 1 (Config Plane) fully functional
- ✓ Layers 1 & 2 integrated
- ✓ 99.9% availability under load
- ✓ Deduplication working at scale

---

## Phase 3: Southbound Data Provider (Weeks 14-20)

### Week 14: Goal State Generation

**Build**:
- ENI Aggregator (fetch constructs for ENI)
- Goal State Generator (compose full DASH model)
- VersionStamper (assign version, compute fingerprint)

**Tests**:
- Deterministic generation (same input = same fingerprint)
- All constructs included
- Fingerprint verified

**Success Criteria**:
- ✓ Goal State fingerprint deterministic
- ✓ Generation latency < 10ms
- ✓ All construct versions included

---

### Week 15: Watch Integration

**Build**:
- Watch Layer 2 for construct changes
- Regenerate affected ENI Goal States
- Route to Layer 4 (plugin registry)

**Tests**:
- Construct change → Goal State regeneration
- Only affected ENIs regenerated
- Plugin called with Goal State

**Success Criteria**:
- ✓ End-to-end: Layer 2 → Layer 3 → Layer 4 call
- ✓ Latency < 100ms from construct change to plugin call

---

### Week 16: Partial Failure Handling

**Build**:
- Retry logic (exponential backoff)
- Failure tracking
- Escalation after max retries

**Tests**:
- Partial failure retry succeeds
- Max retries exceeded → escalation
- Metrics track failures

**Success Criteria**:
- ✓ 90% of partial failures recover within 3 retries
- ✓ Escalation > threshold
- ✓ Metrics accurate

---

### Weeks 17-20: Integration & Scale Testing

**Build**:
- Full Layer 3 implementation
- Optimization (caching, batching)
- Observability (metrics, tracing)

**Tests**:
- 10,000 ENIs generating Goal States
- 1000 construct changes → regeneration
- Latency under load

**Success Criteria**:
- ✓ 10k ENIs, 1k constructs: < 500ms end-to-end
- ✓ Memory stable at 100k ENIs
- ✓ Zero loss of updates

**Phase 3 Deliverables**:
- ✓ Layer 3 (Southbound Provider) fully functional
- ✓ Layers 1-3 integrated
- ✓ Goal State generation deterministic
- ✓ Scales to 100k ENIs

---

## Phase 4: Plugins & Reconciliation (Weeks 21-26)

### Week 21: Plugin Architecture

**Build**:
- PluginRegistry (register, route plugins)
- Plugin interface contract
- ENI → Plugin mapping

**Tests**:
- Plugin registration
- Correct plugin routed for ENI
- Plugin lifecycle (init, close)

**Success Criteria**:
- ✓ Multiple plugins registered
- ✓ Correct routing per ENI
- ✓ Plugin lifecycle clean

---

### Week 22: Example Plugins

**Build**:
- IntelDPUProgrammer (example)
- NvidiaDPUProgrammer (example)
- MockPlugin (for testing)

**Tests**:
- Plugin.Apply() called correctly
- Result returned with status
- Metrics emitted

**Success Criteria**:
- ✓ Both plugins callable
- ✓ Result proto correct
- ✓ Idempotency guaranteed (same fingerprint → cached result)

---

### Week 23: Feedback Loop Implementation

**Build**:
- ReconciliationActor (periodic sync)
- Divergence detection (desired vs. actual)
- Re-sync on divergence

**Tests**:
- Divergence detected within cycle
- Re-sync recovers divergence
- No false positives

**Success Criteria**:
- ✓ Cycle runs every 5-10 minutes
- ✓ 95% divergence detected
- ✓ 90% auto-recovery

---

### Week 24: Reconciliation & Error Handling

**Build**:
- Consistency invariant enforcement
- Escalation on persistent divergence
- Alert generation

**Tests**:
- Invariant violations prevented
- Escalation after threshold
- Alerts triggered correctly

**Success Criteria**:
- ✓ No invariant violations
- ✓ Escalation accurate
- ✓ Alert delivery verified

---

### Weeks 25-26: Production Readiness & Documentation

**Build**:
- Comprehensive observability (metrics, logging, tracing)
- Operational runbook
- Production deployment guide
- Load testing (100k ENIs, 1000 hosts)

**Tests**:
- 24-hour stability test
- 1000 ENI churn (add/delete)
- Plugin failure scenarios

**Success Criteria**:
- ✓ 99.99% availability under sustained load
- ✓ All failure scenarios handled
- ✓ Ops team trained

**Phase 4 Deliverables**:
- ✓ Layer 4 (Plugin Architecture) fully functional
- ✓ Feedback Loop & Reconciliation working
- ✓ Complete FM Control Plane
- ✓ Production-ready deployment

---

## Implementation Milestones

| Milestone | Week | Criteria |
|-----------|------|----------|
| **M1: Layer 2 complete** | 7 | Database/Model fully tested, 100% coverage |
| **M2: Layers 1-2 integrated** | 13 | Config Plane → Database pipeline working |
| **M3: Southbound Provider** | 20 | Goal State generation at scale |
| **M4: Plugins & Reconciliation** | 26 | Full FM Control Plane, production-ready |

---

## Success Criteria Summary

### Code Quality
- ✓ 100% line coverage
- ✓ >90% mutation kill rate
- ✓ Zero race conditions (-race flag passing)
- ✓ No memory leaks (24h runtime)

### Performance
- ✓ Config Plane: < 1ms dedup check, 99% hit rate
- ✓ Database: < 50ms put latency p99
- ✓ Southbound: < 100ms Goal State gen, deterministic
- ✓ Plugin: < 500ms programming per ENI

### Scale
- ✓ 100k ENIs
- ✓ 10k constructs
- ✓ 1000s updates/sec with 90% dedup
- ✓ Memory < 5GB

### Reliability
- ✓ 99.99% availability
- ✓ 90% auto-recovery from failures
- ✓ Divergence detection < 10 minutes
- ✓ Zero data loss

### Operations
- ✓ Comprehensive observability (metrics, logs, traces)
- ✓ Runbook for common issues
- ✓ Deployment guide (K8s + Docker)
- ✓ Ops team trained

---

## Risk Mitigation

| Risk | Impact | Mitigation |
|------|--------|-----------|
| Layer 2 complexity | High | Intensive design review (Week 1), early testing |
| Scale issues | Medium | Load testing early (Week 6), optimize as needed |
| Plugin integration | Medium | Example plugins in Week 22, early testing |
| Ops complexity | Low | Runbook started Week 25 |

---

## Testing Strategy Throughout

### Unit Tests
- Every component: > 90% coverage
- Table-driven tests (parameters vary, logic constant)

### Integration Tests
- Layers flow end-to-end
- Real etcd (not mocked)
- 1000s concurrent operations

### Chaos Tests
- Component failures (etcd, plugin, device)
- Network partitions
- Latency injection

### Load Tests
- Sustained (100k ENIs, 1000 updates/sec)
- Ramp (gradually increase load)
- Spike (sudden 10x traffic)

---

## Deliverables by Phase

| Phase | Deliverables |
|-------|--------------|
| **1** | Layer 2 (Database/Model) + tests |
| **2** | Layer 1 (Config Plane) + L1-L2 integration |
| **3** | Layer 3 (Southbound Provider) + L1-3 integration |
| **4** | Layer 4 (Plugin) + Reconciliation + complete FM |

---

## Summary

**26-week roadmap** to build production-ready FM Control Plane:
- Phase 1: Foundation (Layer 2)
- Phase 2: Config Plane (Layer 1)
- Phase 3: Southbound (Layer 3)
- Phase 4: Plugins + Reconciliation (Layer 4 + feedback)

**Result**: 4-layer architecture with:
- 99.99% availability
- 100% consistency
- 100k ENI scale
- Multi-vendor plugin support
- Automatic self-healing

---

## Next Steps

1. **Review** all design documents
2. **Clarify** ambiguities with architecture team
3. **Start Phase 1** Week 1 with etcd setup
4. **Build comprehensive tests** (unit, integration, chaos)
5. **Implement observability** from day 1

---

**All design documents complete. Ready for implementation.**
