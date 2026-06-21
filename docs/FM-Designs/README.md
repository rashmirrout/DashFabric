# FM Control Plane Design Documentation

This directory contains the complete design specification for the **FM (Fabric Management) Control Plane** - a production-grade system for managing DASH networking configuration at scale.

---

## What is FM Control Plane?

The FM Control Plane is a 4-layer distributed system that manages networking configuration for cloud provider's VMs and DASH appliances. It handles:
- **Service Discovery**: VNET provisioning, ENI management
- **Configuration**: RouteTable, ACL, mapping management
- **Multi-vendor Support**: Intel DPU, Nvidia DPU, custom DASH implementations
- **Reliability**: Self-healing from failures, automatic reconciliation
- **Scale**: 100k+ ENIs, multi-tenant workloads

---

## Documentation Overview

### Start Here
- **[FM_DESIGN_INDEX.md](FM_DESIGN_INDEX.md)** - Navigation guide for all documents (quick ref)
- **[FM_ARCHITECTURE_SPEC.md](FM_ARCHITECTURE_SPEC.md)** - Master architecture document (start reading here)

### Layer-by-Layer Design
1. **[FM_DESIGN_LAYER1_CONFIG_PLANE.md](FM_DESIGN_LAYER1_CONFIG_PLANE.md)** - Input layer (subscriptions, deduplication, versioning)
2. **[FM_DESIGN_LAYER2_DATABASE_MODEL.md](FM_DESIGN_LAYER2_DATABASE_MODEL.md)** - Storage layer (consistency, indices, cascading deletes)
3. **[FM_DESIGN_LAYER3_SOUTHBOUND.md](FM_DESIGN_LAYER3_SOUTHBOUND.md)** - Planning layer (Goal State generation)
4. **[FM_DESIGN_LAYER4_PLUGIN.md](FM_DESIGN_LAYER4_PLUGIN.md)** - Execution layer (multi-vendor plugin architecture)

### Cross-Cutting Concerns
- **[FM_DESIGN_VERSIONING_DEDUP.md](FM_DESIGN_VERSIONING_DEDUP.md)** - Versioning strategy (3-part: version + hash + sequence) and deduplication at scale
- **[FM_DESIGN_FEEDBACK_RECONCILIATION.md](FM_DESIGN_FEEDBACK_RECONCILIATION.md)** - Feedback loops and automatic recovery
- **[FM_DESIGN_CONSISTENT_MODELING.md](FM_DESIGN_CONSISTENT_MODELING.md)** - Data model, naming conventions, consistency rules
- **[FM_DESIGN_SCHEMAS.md](FM_DESIGN_SCHEMAS.md)** - Protobuf message definitions

### Implementation
- **[FM_IMPLEMENTATION_ROADMAP.md](FM_IMPLEMENTATION_ROADMAP.md)** - 26-week implementation plan (4 phases)

---

## Quick Facts

| Aspect | Details |
|--------|---------|
| **Architecture** | 4-layer (Config → Database → Southbound → Plugin) |
| **Scale** | 100k ENIs, 1000 hosts, 10k constructs |
| **Throughput** | 1000+ updates/sec with 90% deduplication |
| **Latency** | < 2 seconds end-to-end (subscription → device) |
| **Availability** | 99.99% with automatic self-healing |
| **Implementation** | 26 weeks (4 phases) |
| **Team** | Solo or small team (6-8 people) |

---

## Key Design Decisions

### 1. Versioning for Deduplication
- Every construct has: **version** (causality), **hash** (idempotency), **sequence** (ordering)
- Duplicate notifications detected via hash (1ms check vs. 50ms reprocessing)
- **Impact**: 99% latency reduction on retries

### 2. Per-ENI Goal State
- Each ENI gets complete, independent Goal State
- Failures isolated to single ENI, not cascading
- **Impact**: Faster recovery, better resilience

### 3. Pluggable Architecture
- Goal State Programming is library-based (not gRPC)
- Vendors (Intel, Nvidia, Custom) instantiate plugins independently
- New DASH proto versions don't require FM core changes
- **Impact**: 1-day onboarding for new vendors

### 4. Feedback Loops
- Reconciliation actor runs every 5-10 minutes
- Detects divergence between desired and actual state
- Auto-recovers 90% of failures
- **Impact**: Operational simplicity, high reliability

### 5. Strict Consistency
- Every write validated (no circular refs, no dangling refs, no isolation violations)
- Atomic per-construct, eventual across VNETs
- **Impact**: Zero inconsistent states, full audit trail

---

## Architecture Diagram

```
┌─────────────────────────────────────────────────────┐
│ CM: Config Plane                               │
│ (Subscriptions, deduplication, versioning)          │
└──────────────────┬────────────────────────────────┘
                   ↓
┌─────────────────────────────────────────────────────┐
│ DM: Database/Model Management                  │
│ (Storage, consistency, indices, cascading deletes)  │
└──────────────────┬────────────────────────────────┘
                   ↓
┌─────────────────────────────────────────────────────┐
│ GM: Southbound Data Provider                   │
│ (Goal State generation, per-ENI composition)        │
└──────────────────┬────────────────────────────────┘
                   ↓
┌─────────────────────────────────────────────────────┐
│ DAL: Goal State Programming Plugin              │
│ (Multi-vendor: Intel, Nvidia, Custom)               │
└──────────────────┬────────────────────────────────┘
                   ↓
         [DASH Devices (actual state)]
                   ↓
┌─────────────────────────────────────────────────────┐
│ Feedback Loop: Reconciliation                       │
│ (Divergence detection, auto-recovery)               │
└─────────────────────────────────────────────────────┘
```

---

## Implementation Roadmap (26 Weeks)

| Phase | Weeks | Focus | Deliverable |
|-------|-------|-------|-------------|
| **1** | 1-7 | Foundation | DM (Database/Model) |
| **2** | 8-13 | Integration | CM (Config Plane) |
| **3** | 14-20 | Southbound | GM (Goal State Gen) |
| **4** | 21-26 | Plugins & Reliability | DAL + Reconciliation |

---

## Success Criteria

### Code Quality
- ✓ 100% line coverage
- ✓ >90% mutation kill rate
- ✓ Zero race conditions
- ✓ No memory leaks

### Performance
- ✓ Dedup check: < 1ms
- ✓ Database get: < 10ms p99
- ✓ Goal State gen: < 10ms
- ✓ End-to-end: < 2 seconds

### Scale
- ✓ 100k ENIs
- ✓ 10k constructs
- ✓ 1000+ updates/sec
- ✓ < 5GB memory

### Reliability
- ✓ 99.99% availability
- ✓ 90% auto-recovery
- ✓ Divergence detection < 10 min
- ✓ Zero data loss

---

## How to Use These Documents

### For Understanding the System
1. Read **FM_ARCHITECTURE_SPEC.md** (complete overview)
2. Skim **FM_DESIGN_VERSIONING_DEDUP.md** and **FM_DESIGN_FEEDBACK_RECONCILIATION.md** (key strategies)
3. Reference layer designs as needed

### For Implementation
1. Start with **FM_ARCHITECTURE_SPEC.md**
2. Follow **FM_IMPLEMENTATION_ROADMAP.md** phase-by-phase
3. Refer to specific layer design for implementation details
4. Use **FM_DESIGN_CONSISTENT_MODELING.md** and **FM_DESIGN_SCHEMAS.md** for data structures

### For Operations
1. Review **FM_DESIGN_FEEDBACK_RECONCILIATION.md** (monitoring & alerts)
2. Check **FM_IMPLEMENTATION_ROADMAP.md** success criteria
3. Create runbook based on architecture (TBD)

---

## Design Principles

1. **Layered Architecture**: Clear separation of concerns
2. **Determinism**: Same input always produces same output
3. **Idempotency**: Applying twice = applying once
4. **Observability**: Full traceability from subscription to device
5. **Extensibility**: New vendors without modifying core
6. **Consistency**: Strict invariants, no corrupted state
7. **Reliability**: Feedback loops, automatic recovery

---

## Document Statistics

| Metric | Value |
|--------|-------|
| Total documents | 11 (including index) |
| Total sections | 200+ |
| Code examples | 50+ |
| Diagrams (Mermaid) | 10+ |
| Protobuf schemas | 8+ |
| Estimated read time | 4-5 hours (full) |

---

## Questions & Discussion

This design is ready for team review. Key discussion topics:

1. **Consistency Model**: Atomic per VNET, eventual across VNETs - acceptable?
2. **Plugin Failure Handling**: FM down if plugin crashes - risk acceptable?
3. **Reconciliation Frequency**: 5 minutes vs. 10 minutes - which is right?
4. **Soft Delete Retention**: How long to keep deleted constructs?
5. **Multi-tenancy**: Per-tenant indices sufficient for isolation?

---

## Next Steps

1. **Architecture Review**: Team reviews all documents
2. **Design Validation**: Clarify open questions
3. **Implementation Kickoff**: Phase 1 begins (Week 1)
4. **Weekly Sync**: Architecture → Implementation alignment

---

## Appendix: File Manifest

```
docs/FM/
├── FM_DESIGN_INDEX.md                     (document navigation)
├── FM_ARCHITECTURE_SPEC.md                (master architecture)
├── FM_DESIGN_LAYER1_CONFIG_PLANE.md       (Config Plane layer)
├── FM_DESIGN_LAYER2_DATABASE_MODEL.md     (Database/Model layer)
├── FM_DESIGN_LAYER3_SOUTHBOUND.md         (Southbound Provider layer)
├── FM_DESIGN_LAYER4_PLUGIN.md             (Plugin architecture)
├── FM_DESIGN_VERSIONING_DEDUP.md          (Versioning strategy)
├── FM_DESIGN_FEEDBACK_RECONCILIATION.md   (Feedback loops)
├── FM_DESIGN_CONSISTENT_MODELING.md       (Data model)
├── FM_DESIGN_SCHEMAS.md                   (Protobuf definitions)
├── FM_IMPLEMENTATION_ROADMAP.md           (26-week plan)
└── README.md                              (this file)
```

---

**Design Status**: ✅ Complete - Ready for implementation  
**Last Updated**: 2026-06-19  
**Version**: 1.0
