# TEST_PLAN.md: Layer 1 Config Plane (Deduplication & Event Subscription)

**Component:** `pkg/layer1/` - Config Plane event subscription, deduplication, and gating  
**Version:** 1.0  
**Status:** Test Plan - Ready for Review  
**Date:** 2026-06-24

---

## 1. Objectives

### Primary Objectives
1. **Event Subscription:** Verify CB client connects, subscribes to topics sequentially, receives notifications
2. **SHA256 Fingerprinting:** Verify SHA256 hash computed correctly for events, consistent across calls
3. **Dedup Cache:** Verify cache detects duplicate events, stores content hash, implements LRU eviction
4. **Cache Hit Rate:** Verify 80%+ duplicate detection (80% reduction in unique events)
5. **Thread Safety:** Verify no race conditions under concurrent access (1000+ goroutines)
6. **Metrics:** Verify hit rate, miss rate, cache size tracked and exported (Prometheus)

### Success Criteria
- [ ] All 22 test cases passing
- [ ] 100% line coverage for Layer 1 code
- [ ] 100% branch coverage (all decision paths tested)
- [ ] Mutation kill rate >90% (bugs in code caught by tests)
- [ ] No race conditions detected (`go test -race`)
- [ ] Cache hit rate 75-85% in synthetic workload
- [ ] Thread-safe under 1000 concurrent goroutines

---

## 2. Test Scenarios

### 2.1 Happy Path Tests

| TC# | Name | Setup | Action | Expected Result | Priority |
|-----|------|-------|--------|-----------------|----------|
| TC-001 | Event Subscription Success | Mock CB ready | Subscribe to topic | ✓ Connected, watching | P0 |
| TC-002 | Receive Single Event | Subscribed | CB sends event | ✓ Event received, forwarded | P0 |
| TC-003 | Event Fingerprint Compute | Event object | Compute SHA256 | ✓ Hash 32 bytes, consistent | P0 |
| TC-004 | Cache Miss (New Event) | Cache empty | New event SHA256 | ✓ Miss, stored in cache | P0 |
| TC-005 | Cache Hit (Duplicate) | Event in cache | Same event again | ✓ Hit, event dropped | P0 |
| TC-006 | LRU Eviction | Cache full (10k) | Add new event | ✓ Oldest entry evicted | P0 |
| TC-007 | Metrics Tracking | Events flowing | Record hits/misses | ✓ Counters increment | P0 |

### 2.2 Edge Cases

| TC# | Name | Setup | Action | Expected Result | Priority |
|-----|------|-------|--------|-----------------|----------|
| TC-008 | Very Large Event | Event 1MB | Compute hash | ✓ Completes <100ms | P1 |
| TC-009 | Rapid Events | 1000 events/sec | Continuous flow | ✓ All processed, no drops | P1 |
| TC-010 | Cache Boundary | Cache at max-1 | Add 1 event | ✓ No eviction yet | P1 |
| TC-011 | Cache Full | Cache at max | Add 1 event | ✓ LRU evicts | P1 |
| TC-012 | Collision Resistant | Different events | Compute hashes | ✓ Different hashes (SHA256) | P1 |
| TC-013 | Empty Event | Nil/empty payload | Compute hash | ✓ Handled safely (no panic) | P1 |
| TC-014 | Cache Hit Rate 80% | 1000 events, 20% unique | Process all | ✓ Hit rate 80% ±5% | P1 |

### 2.3 Error Cases

| TC# | Name | Setup | Action | Expected Result | Priority |
|-----|------|-------|--------|-----------------|----------|
| TC-015 | CB Connection Fails | CB down | Subscribe | ✗ Error returned, retry scheduled | P1 |
| TC-016 | CB Timeout | CB hanging | Subscribe (timeout) | ✗ Error after timeout, retry | P1 |
| TC-017 | Out of Memory | Cache near OOM | Add events | ✓ Graceful degradation (stop caching) | P2 |
| TC-018 | Concurrent Writes | 100 goroutines | Write simultaneously | ✓ No data corruption, no deadlock | P0 |

### 2.4 Property-Based Tests

| TC# | Name | Property | Verification | Priority |
|-----|------|----------|--------------|----------|
| TC-019 | Cache Consistency | Duplicate events have same fingerprint | For N events, duplicates have identical hashes | P0 |
| TC-020 | LRU Bounded | Cache never exceeds max size | After N insertions, size ≤ max_size | P0 |
| TC-021 | Hit Rate Stable | Hit rate converges with more events | With 10k events, hit rate 75-85% | P0 |
| TC-022 | Thread Safety | Concurrent ops don't corrupt state | 1000 goroutines, 1M ops, no races | P0 |

---

## 3. Test Data & Fixtures

### Mock Events
```go
fixtures.SampleEvent1 = {
    ID: "evt-001",
    VnetID: "vnet-prod",
    Type: "RouteTableUpdate",
    Payload: {"routes": ["10.0/8", "10.1/8"]},
}

fixtures.SampleEvent2 = {
    ID: "evt-001",  // Same ID
    VnetID: "vnet-prod",
    Type: "RouteTableUpdate",
    Payload: {"routes": ["10.0/8", "10.1/8"]},
}  // Identical to Event1 → same SHA256

fixtures.SampleEvent3 = {
    ID: "evt-002",
    VnetID: "vnet-prod",
    Type: "RouteTableUpdate",
    Payload: {"routes": ["10.0/8", "10.2/8"]},  // Different
}  // Different payload → different SHA256
```

### Mock CB (Control Broker)
```go
mockCB.QueueEvent(fixtures.SampleEvent1)
mockCB.QueueEvent(fixtures.SampleEvent1)  // Duplicate
mockCB.QueueEvent(fixtures.SampleEvent3)  // New

// Subscribe will return events in order
// Expected dedup cache to see:
// 1. Event1 → cache miss, forward to Layer 2
// 2. Event1 → cache hit, drop
// 3. Event3 → cache miss, forward to Layer 2
```

---

## 4. Mutation Testing Expectations

| Mutation | Code Change | Tests That Should Fail | Kill |
|----------|-------------|------------------------|------|
| Remove SHA256 computation | Delete hash line | TC-003, TC-004, TC-005, TC-012 | ✓ 4 tests |
| Remove LRU eviction | Delete evict logic | TC-006, TC-011 | ✓ 2 tests |
| Change cache size | Hardcode 1000 instead of 10k | TC-006, TC-011 | ✓ 2 tests |
| Remove concurrency lock | Delete mutex lock | TC-018, TC-022 | ✓ 2 tests |
| Skip duplicate check | Remove if hit != miss | TC-005, TC-014 | ✓ 2 tests |
| Don't track metrics | Delete counter increment | TC-007 | ✓ 1 test |

**Target Mutation Kill Rate:** >90% (expect ~11/12 mutations killed)

---

## 5. Performance Benchmarks

| Benchmark | Target | Acceptable | Tool |
|-----------|--------|-----------|------|
| Hash computation | <1ms per event | <5ms | `go test -bench` |
| Cache lookup | <10 microseconds | <100 microseconds | `go test -bench` |
| Cache hit rate | 75-85% | >70% | Test TC-014 |
| Memory per event | <1KB | <5KB | `pprof` |
| Throughput | 50k events/sec | >10k events/sec | Synthetic load |

---

## 6. Test Execution Tracking

**To Be Filled During Implementation:**

| Test ID | Status | Executed | Duration | Notes | Mutation Coverage |
|---------|--------|----------|----------|-------|------------------|
| TC-001 | - | - | - | - | - |
| TC-002 | - | - | - | - | - |
| ... | | | | | |

---

## 7. Test Architecture

### Test Organization
```
tests/unit/layer1_test.go
├── TestSubscription_*           (CB subscription tests)
├── TestFingerprint_*            (SHA256 hash tests)
├── TestDedup_*                  (Cache dedup tests)
├── TestLRU_*                    (Cache eviction tests)
├── TestConcurrency_*            (Thread safety tests)
└── BenchmarkDedup_*             (Performance benchmarks)

tests/chaos/layer1_chaos_test.go
├── TestChaos_CBDown             (CB failure handling)
├── TestChaos_HighLoad           (1000 concurrent requests)
```

### Test Fixtures
```
pkg/testutil/fixtures.go
├── SampleEvent1-10              (Predefined test events)
├── SampleEventLarge             (1MB event for edge case)
├── MockCBWithEvents()           (CB with queued events)
```

---

## 8. Coverage Goals

| Metric | Target | How to Verify |
|--------|--------|---------------|
| Line Coverage | 100% | `go tool cover -func=coverage.out` |
| Branch Coverage | 100% | `go tool cover` report all branches |
| Mutation Kill Rate | >90% | Mutation testing tool report |
| Race Conditions | 0 | `go test -race ./...` |

---

## 9. Sign-Off

**Test Plan Review:**
- [ ] Team Lead (Architecture) - Reviewed
- [ ] QA Lead (Testing) - Approved
- [ ] Security Lead - Reviewed (no sensitive data in tests)

**Approved By:** _________________ (Date: _______)

---

## 10. Notes for Implementation

1. **CB Subscription:** Use etcd client to subscribe to `/fm/events/{topic}`
2. **SHA256 Fingerprint:** Use Go's `crypto/sha256` package for hashing
3. **LRU Cache:** Implement using Go's sync.Map for thread-safety, with custom eviction
4. **Metrics:** Export Prometheus counters: `layer1_cache_hits_total`, `layer1_cache_misses_total`
5. **Observability:** Log all events with trace IDs for debugging
6. **Error Handling:** Classify CB errors as retryable (timeout) vs terminal (auth)

---

**Next Step:** Once approved, implement following TDD: write test first, then implementation.
