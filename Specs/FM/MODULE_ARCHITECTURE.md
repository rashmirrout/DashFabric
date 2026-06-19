# FM Module Architecture & Naming Guide

## Quick Reference

| Abbreviation | Full Name | Purpose | Folder | Package | Status |
|--------------|-----------|---------|--------|---------|--------|
| **CM** | Config Management | Event dedup pipeline (50k→10k) | `pkg/cm/` | `configmanagement` | Phase 1 ✓ |
| **DM** | Data Management | Consistency enforcement & registry | `pkg/dm/` | `datamanagement` | Phase 1 ✓ |
| **GM** | Goal State Management | VNET aggregation & ENI composition | `pkg/gm/` | `goalstatemanagement` | Phase 2 (TBD) |
| **DAL** | DPU Abstraction Layer | Multi-vendor device programming | `pkg/dal/` | `dpuabstraction` | Phase 2 (TBD) |

---

## Module Organization

### CM (Config Management)

**Location:** `pkg/cm/`  
**Package:** `configmanagement`  
**Purpose:** Event subscription and deduplication pipeline

**Key Files:**
- `types.go` — Shared types: `Event`, `CacheStats`, `SubscriptionConfig`, `DeduplicationConfig`
- `cache.go` — LRU dedup cache implementation
- `subscriber.go` — EtcdSubscriber for CB event subscription
- `errors.go` — Module-specific errors

**Interfaces:**
- `DedupCache` — Event deduplication
- `CBSubscriber` — Event subscription
- `EventValidator` — Event validation (planned)

**Key Statistics:**
- 14 tests, 100% coverage
- Cache hit rate ~50% (10 duplicates out of 20 events typical)
- Default size: 10,000 entries, TTL: 5 minutes

### DM (Data Management)

**Location:** `pkg/dm/`  
**Package:** `datamanagement`  
**Purpose:** Data consistency and registry management

**Key Files:**
- `types.go` — Shared types: `ENIState`, `VIPBinding`, `SystemState`, `ConsistencyRule`
- `rules.go` — 5 consistency rule implementations
- `errors.go` — Module-specific errors
- `registry.go` (planned) — Construct registry interface

**Interfaces:**
- `ConsistencyRule` — Defines enforceable constraints
- `ConstructRegistry` — Manages constructs (planned)
- `DataManager` — Orchestrator (planned)

**Consistency Rules (5 total):**
1. `ENIStateRule` — Valid ENI states: active, inactive, error
2. `VIPBindingRule` — VIP must have DIP bound
3. `SNATPoolRule` — SNAT requires ENI reference
4. `RouteValidityRule` — At least one healthy replica
5. `ReplicaHealthRule` — Warning-only for unhealthy replicas

**Key Statistics:**
- 29 tests, comprehensive coverage
- Actor model for per-type serialization
- Thread-safe registry operations

### GM (Goal State Management)

**Location:** `pkg/gm/` (Phase 2 - planned)  
**Package:** `goalstatemanagement`  
**Purpose:** Generate per-ENI goal states from aggregated constructs

**Key Files (Stubs):**
- `types.go` — Shared types: `PerENIGoalState`, `AggregatedState`, `Route`, `ACLRule`, `VIPMapping`
- `errors.go` — Module-specific errors
- `manager.go` (planned) — Orchestrator

**Planned Interfaces:**
- `VNETAggregator` — Aggregate all constructs for a VNET
- `GoalStateGenerator` — Generate per-ENI states
- `GoalStateCache` — Fingerprint-based caching
- `GoalStateManager` — Orchestrator

### DAL (DPU Abstraction Layer)

**Location:** `pkg/dal/` (Phase 2 - planned)  
**Package:** `dpuabstraction`  
**Purpose:** Abstract vendor-specific device programming

**Key Files (Stubs):**
- `types.go` — Shared types: `Plugin`, `ProgramResult`, `ManagerStats`
- `errors.go` — Module-specific errors
- `manager.go` (planned) — Orchestrator

**Planned Interfaces:**
- `Plugin` — Vendor plugin contract
- `PluginDispatcher` — Route goals to vendor plugins
- `PluginPool` — Worker pool management
- `DPUAbstractionManager` — Orchestrator

---

## Data Flow Architecture

```
Device → CB Subscription (CM)
          ↓
        Event Dedup Cache (CM)
          ↓
        Config Event Stream
          ↓
        Process Event (DM)
          ↓
        Consistency Rules (DM) → Registry (DM)
          ↓
        SystemState
          ↓
        VNET Aggregation (GM)
          ↓
        Per-ENI Goal State (GM) → Cache (GM)
          ↓
        Plugin Dispatch (DAL)
          ↓
        Program Device (DAL plugins)
          ↓
        ProgramResult
```

---

## Naming Conventions

### File Organization

| Level | Convention | Example | Notes |
|-------|-----------|---------|-------|
| **Folder** | 2-letter abbreviation | `cm`, `dal` | Clean, IDE-friendly, concise |
| **Package** | Full English name | `configmanagement`, `dpuabstraction` | Explicit, searchable, self-documenting |
| **File** | Functional purpose | `cache.go`, `rules.go`, `dispatcher.go` | Indicates what's inside |
| **Type** | PascalCase, domain-specific | `ConfigEvent`, `PerENIGoalState`, `ConsistencyRule` | Action-oriented, not generic |
| **Interface** | Verb-Noun pattern | `DedupCache`, `EventPipeline`, `DataManager` | Clarifies purpose |
| **Method** | camelCase | `CheckAndStore()`, `Dispatch()`, `Validate()` | Clear action |
| **Constant** | UPPER_SNAKE_CASE | `DEFAULT_CACHE_SIZE` | Screaming constant convention |

### Package Import Pattern

```go
// Full import path includes module path
import (
    cm "github.com/dashfabric/fm/pkg/cm"
    dm "github.com/dashfabric/fm/pkg/dm"
)

// Use via alias or full package name (both same)
cache := cm.NewLRUCache(10000)  // alias usage
state := dm.ENIState{ID: "eni-001"}  // full package
```

---

## Adding New Code

### To CM (Config Management)

**Adding a new cache eviction algorithm:**
1. Create `eviction_<algo_name>.go` in `pkg/cm/`
2. Implement `Eviction` interface
3. Add unit tests to `tests/unit/` with `cm_` prefix
4. Update `cache.go` to support selection

**Example:**
```go
// pkg/cm/eviction_lfu.go
package configmanagement

type LFUEviction struct { /* ... */ }
func (e *LFUEviction) Evict(cache *LRUCache) { /* ... */ }
```

### To DM (Data Management)

**Adding a new consistency rule:**
1. Create `rule_<name>.go` in `pkg/dm/`
2. Implement `ConsistencyRule` interface with `Name()`, `Validate()`, `Enforce()`
3. Add unit tests with `dm_` prefix
4. Register in `MappingManager.RegisterRule()`

**Example:**
```go
// pkg/dm/rule_dns_consistency.go
package datamanagement

type DNSConsistencyRule struct {}
func (r *DNSConsistencyRule) Name() string { return "DNS Consistency" }
func (r *DNSConsistencyRule) Validate(ctx context.Context, state *SystemState) error { /* ... */ }
```

### To GM (Goal State Management — Phase 2)

**Adding aggregation logic:**
1. Create `aggregator_<source>.go` in `pkg/gm/`
2. Implement `VNETAggregator` interface
3. Add parallel construct fetching via goroutines

### To DAL (DPU Abstraction Layer — Phase 2)

**Adding vendor support:**
1. Create `plugin_<vendor>.go` in `pkg/dal/`
2. Implement `Plugin` interface with `Program()` and `Name()`
3. Register in `PluginDispatcher`
4. Add vendor-specific worker pool

---

## Testing Strategy

### Unit Tests

**Location:** `tests/unit/`  
**Naming:** `<module>_<component>_test.go`

**Coverage Requirements:**
- CM: 100% (cache, subscriber, fingerprinting)
- DM: 100% (rules, registry, actor model)
- GM: 90%+ (planned)
- DAL: 90%+ (planned)

**Makefile Targets:**
```bash
make test-cm        # Config Management tests
make test-dm        # Data Management tests
make test-gm        # Goal State Management tests
make test-dal       # DPU Abstraction Layer tests
make test-coverage  # All tests with coverage report
```

### Integration Tests

**Location:** `tests/integration/`  
**Purpose:** Multi-module workflows (CM → DM → GM → DAL)

### Chaos Tests

**Location:** `tests/chaos/`  
**Purpose:** Concurrent operations, race conditions, failure modes

---

## Performance Characteristics

### CM (Config Management)

| Operation | Complexity | Notes |
|-----------|-----------|-------|
| `CheckAndStore()` | O(1) | Hash table lookup + LRU ordering |
| Eviction | O(log N) | Linked list removal, 1-2 nodes max |
| Full scan | O(N) | Rarely needed |

**Throughput:** ~1M operations/sec on typical hardware  
**Memory:** 10,000 entries × 64 bytes ≈ 640 KB

### DM (Data Management)

| Operation | Complexity | Notes |
|-----------|-----------|-------|
| Add ENI | O(1) | Map insertion |
| Registry lookup | O(1) | Direct map access |
| Consistency check | O(R × E) | R rules × E ENIs |

**Typical latency:** <1ms for 1000 ENIs

---

## Dependency Graph

```
CM (top layer)
  ├── types.go → Event, CacheStats
  ├── cache.go → LRU eviction
  └── subscriber.go → etcd client/v3
  
DM (mid layer)
  ├── depends on CM.Event
  ├── types.go → ENIState, VIPBinding
  ├── rules.go → Consistency rule engine
  └── registry.go → Construct storage
  
GM (planned)
  ├── depends on DM.ENIState, DM.ReplicaState
  ├── aggregator.go → Parallel VNET fetch
  └── composer.go → Per-ENI goal generation
  
DAL (planned)
  ├── depends on GM.PerENIGoalState
  ├── plugins/ → Vendor implementations
  └── dispatcher.go → Route to vendor
```

---

## Future Enhancements

### CM (Config Management)
- [ ] LFU eviction variant
- [ ] Multiple cache tiers (L1 in-memory, L2 disk)
- [ ] Event filtering by topic pattern
- [ ] TTL-based eviction

### DM (Data Management)
- [ ] Versioned construct history
- [ ] Eventual consistency model
- [ ] Conflict resolution strategies
- [ ] Audit logging

### GM (Goal State Management — Phase 2)
- [ ] Incremental goal state diffing
- [ ] Predictive caching
- [ ] Multi-VNET aggregation batching
- [ ] Lazy goal state generation

### DAL (DPU Abstraction Layer — Phase 2)
- [ ] Vendor-specific optimizations
- [ ] Telemetry per vendor
- [ ] Circuit breaker for failed vendors
- [ ] Fallback routing

---

## Troubleshooting

### High cache miss rate in CM
- Check fingerprint computation (mutation issue?)
- Verify hash consistency (canonical JSON?)
- Increase cache size or TTL

### Consistency rule violations in DM
- Check rule priority ordering
- Verify enforce() implementation
- Review transaction semantics

### Goal state generation failures in GM (planned)
- Validate aggregator output schema
- Check composer error handling
- Review parallel fetch timeouts

### Device programming failures in DAL (planned)
- Check vendor plugin compatibility
- Verify device reachability
- Review error handling in dispatcher

---

## References

- Design: `docs/FM-Designs/FM_DESIGN_*.md`
- Protocol: `Specs/FM/CB_proto_contract.md`
- Implementation: `Specs/FM/implementation-plan.md`
- Binding: `Specs/FM/AGENT_INSTRUCTIONS.md`
