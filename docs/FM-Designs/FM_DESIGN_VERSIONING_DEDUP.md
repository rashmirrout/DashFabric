# FM Design: Versioning & Deduplication Strategy

**Version**: 1.0  
**Status**: Design Complete  
**Parent Document**: [FM_ARCHITECTURE_SPEC.md](FM_ARCHITECTURE_SPEC.md)

---

## Overview

**Versioning & Deduplication** enables FM to scale without reprocessing identical inputs. Every construct has:
- **Version**: Logical causality (v5 → v6)
- **Content Hash**: For idempotency (SHA256)
- **Sequence**: Global ordering (monotonic)

---

## Three-Part Versioning Model

### 1. Version Number

Monotonic per construct, incremented on every change:
```
RouteTable_v1 → RouteTable_v2 → RouteTable_v3 → ...
```

**Benefits**:
- Causality: Can tell which update came first
- Rollback: Can rollback to specific version
- Generation: Layer 3 knows which version generated this Goal State

### 2. Content Hash

SHA256 of canonical JSON serialization:
```
RouteTable_v1: {routes: [...]}
Hash: SHA256(canonical_json) = "abc123def456..."

Same content applied twice:
  First: hash = "abc123..." → process
  Second: hash = "abc123..." → SKIP (duplicate)
```

**Benefits**:
- Idempotency detection (no reprocessing)
- Data integrity (detect corruption)
- Deduplication (99% latency reduction)

### 3. Sequence Number

Global monotonic counter, enforced at Config Plane:
```
Event 1: sequence = 1001
Event 2: sequence = 1002
Event 3: sequence = 1003

Events arrive out-of-order?
  (1002, 1003, 1001) → reorder by sequence → (1001, 1002, 1003)
  Apply in sequence order = deterministic result
```

**Benefits**:
- Causal ordering across all events
- Replayability (can re-run events in order)
- Consistency (same sequence = same state)

---

## Deduplication Flow

### Algorithm

```
Input: Event from etcd
Output: Process or SKIP

┌──────────────────────────────────┐
│ Event: {event_id, content, ...}  │
└────────────┬─────────────────────┘
             ↓
┌──────────────────────────────────────┐
│ Step 1: Compute Hash                 │
│ contentHash = SHA256(canonical_json) │
└────────────┬─────────────────────────┘
             ↓
┌──────────────────────────────────────┐
│ Step 2: Check Cache                  │
│ cache.Get(event_id)                  │
└────────────┬────────────┬────────────┘
             ↓            ↓
        MISS           HIT
         │             │
         ↓             ↓
    ┌─────────┐  ┌──────────────────┐
    │ PROCESS │  │ Compare Hash     │
    │ Event   │  │ cached vs actual │
    └─────────┘  └────┬─────────────┘
                      ↓
                 ┌──────────┐
                 │ MATCH?   │
                 └────┬──┬──┘
                     YES NO
                      │  │
                      ↓  ↓
                   SKIP PROCESS
```

### Example: 1000 Duplicate Retries

```
Scenario: etcd retries same subscription 1000 times

Without deduplication:
  1000 × 50ms processing = 50 seconds wasted

With deduplication:
  First: 50ms (process) → hash = "abc123"
  Next 999: 1ms each (hash cache hit) → SKIP
  Total: 50ms + 999ms = ~1.05 seconds
  
Savings: 49 seconds (98% reduction)
```

---

## Hash Computation

### Canonical Serialization

To ensure same content always produces same hash:

```go
func CanonicalJSON(data interface{}) string {
  // 1. Marshal to JSON
  bytes, _ := json.Marshal(data)
  
  // 2. Parse back to normalize
  var obj interface{}
  json.Unmarshal(bytes, &obj)
  
  // 3. Marshal with sorted keys (canonical form)
  canonical := canonicalMarshal(obj)  // Keys in sorted order
  
  return canonical
}

func SHA256Hash(data interface{}) string {
  canonical := CanonicalJSON(data)
  return hex.EncodeToString(sha256.Sum256([]byte(canonical)))
}
```

### Hash Example

```
Input RouteTable:
{
  "routes": [
    {"destination": "10.0.0.0/24", "next_hop": "10.0.0.1"},
    {"destination": "10.1.0.0/24", "next_hop": "10.1.0.1"}
  ]
}

Canonical (keys sorted):
{
  "routes": [
    {"destination": "10.0.0.0/24", "next_hop": "10.0.0.1"},
    {"destination": "10.1.0.0/24", "next_hop": "10.1.0.1"}
  ]
}

Hash: "abc123def456..."

Duplicate (same content, different field order):
{
  "routes": [
    {"next_hop": "10.0.0.1", "destination": "10.0.0.0/24"},
    {"next_hop": "10.1.0.1", "destination": "10.1.0.0/24"}
  ]
}

Canonical (normalized):
{
  "routes": [
    {"destination": "10.0.0.0/24", "next_hop": "10.0.0.1"},
    {"destination": "10.1.0.0/24", "next_hop": "10.1.0.1"}
  ]
}

Hash: "abc123def456..." (MATCH → DUPLICATE)
```

---

## Cache Strategy

### In-Memory LRU Cache

```go
type DeduplicationCache struct {
  entries map[string]*CacheEntry
  lru     *list.List                    // For LRU eviction
  
  maxSize    int                        // Max entries (default 10k)
  ttl        time.Duration              // TTL per entry (default 24h)
  
  mu sync.RWMutex
}

type CacheEntry struct {
  Hash      string
  Version   int64
  Timestamp time.Time
  Size      int                        // Bytes
}

func (dc *DeduplicationCache) CheckAndRecord(eventID string, hash string) (isDup bool, version int64) {
  dc.mu.Lock()
  defer dc.mu.Unlock()
  
  // Check if entry exists and hash matches
  if entry, exists := dc.entries[eventID]; exists {
    if entry.Hash == hash && time.Since(entry.Timestamp) < dc.ttl {
      // Duplicate: move to front (LRU)
      dc.lru.MoveToFront(entry.Node)
      return true, entry.Version
    }
  }
  
  // Not a duplicate: record new entry
  if len(dc.entries) >= dc.maxSize {
    // Evict LRU
    back := dc.lru.Back()
    delete(dc.entries, back.Value.(string))
    dc.lru.Remove(back)
  }
  
  newEntry := &CacheEntry{
    Hash:      hash,
    Version:   nextVersion(),
    Timestamp: time.Now(),
    Size:      len(eventID) + len(hash),
  }
  
  node := dc.lru.PushFront(eventID)
  newEntry.Node = node
  dc.entries[eventID] = newEntry
  
  return false, newEntry.Version
}
```

### Cache Metrics

```
fm_dedup_cache_size_bytes         # Current cache size
fm_dedup_cache_entries            # Number of entries
fm_dedup_cache_hits_total         # Cache hits
fm_dedup_cache_misses_total       # Cache misses
fm_dedup_cache_evictions_total    # LRU evictions
fm_dedup_events_skipped_total     # Duplicate events skipped
fm_dedup_hit_rate                 # Percentage
```

---

## Sequence Number Ordering

### Global Sequencer

```go
type Sequencer struct {
  mu       sync.Mutex
  sequence int64
  
  // Persist to etcd for durability
  etcdKey string  // "/fm/sequencer/next"
}

func (s *Sequencer) Next() int64 {
  s.mu.Lock()
  defer s.mu.Unlock()
  
  s.sequence++
  
  // Periodically persist to etcd (every 1000 allocations)
  if s.sequence % 1000 == 0 {
    etcdClient.Put(context.Background(), s.etcdKey, fmt.Sprintf("%d", s.sequence))
  }
  
  return s.sequence
}
```

### Out-of-Order Handling

```
Events arrive:
  Event A: sequence 1002, timestamp T2
  Event B: sequence 1001, timestamp T1
  Event C: sequence 1003, timestamp T3

Reorder by sequence:
  1001 → 1002 → 1003

Apply in order:
  Event B (seq 1001) → Event A (seq 1002) → Event C (seq 1003)
  
Result: Deterministic outcome regardless of arrival order
```

---

## Idempotency & Goal State

### Goal State Fingerprint

Goal State also versioned by fingerprint:

```proto
message GoalState {
  string eni_id = 1;
  string vnet_id = 2;
  int64 version = 3;
  string fingerprint = 4;  // SHA256(canonical_goalstate)
  
  RouteTableConfig route_table = 5;
  ACLConfig acl = 6;
  MappingConfig mapping = 7;
}
```

### Idempotency Check in Plugin

```
First application:
  Goal State: {eni, version=6, fingerprint="xyz789"}
  Plugin calls DASH Programmer API
  Caches result: fingerprint "xyz789" → Result{status: success}

Second application (duplicate):
  Goal State: {eni, version=6, fingerprint="xyz789"}
  Plugin checks cache: fingerprint "xyz789" exists
  Returns cached result (idempotent)
  Cost: 1ms (cache lookup) instead of 100ms (DASH API call)
```

---

## Scale Impact

### At 100k ENIs, 1000 hosts

**Without versioning/dedup**:
```
1 RouteTable change → 10,000 ENI updates
Each ENI: parse config, validate, write (50ms)
Total: 10,000 × 50ms = 500 seconds
```

**With versioning/dedup**:
```
1 RouteTable change → 10,000 ENI updates
Each ENI: check fingerprint (1ms), generate Goal State (5ms)
Only program different fingerprints (90% are same)
Total: 10,000 × (1ms + 5ms) + 1,000 × 100ms = 150 seconds
  = 67% latency reduction
```

---

## Summary

**Versioning & Deduplication**:
- **Version**: Causality & rollback (v5 → v6)
- **Hash**: Idempotency detection (99% dedup hit on retries)
- **Sequence**: Global ordering (deterministic outcome)
- **Impact**: 67% latency reduction at 100k ENI scale

**Next**: [FM_DESIGN_FEEDBACK_RECONCILIATION.md](FM_DESIGN_FEEDBACK_RECONCILIATION.md)
