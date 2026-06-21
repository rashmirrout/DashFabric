# FM Design: CM - Config Plane Specification

**Version**: 1.0  
**Status**: Design Complete  
**Parent Document**: [FM_ARCHITECTURE_SPEC.md](FM_ARCHITECTURE_SPEC.md)

---

## Table of Contents

1. [Overview](#overview)
2. [Responsibilities](#responsibilities)
3. [Components](#components)
4. [Deduplication Algorithm](#deduplication-algorithm)
5. [Data Structures](#data-structures)
6. [Interface & APIs](#interface--apis)
7. [Error Handling](#error-handling)
8. [Observability](#observability)
9. [Testing Strategy](#testing-strategy)

---

## Overview

**CM: Config Plane** is the entry point for all external subscription changes. Its primary responsibility is to **ingest, validate, deduplicate, and emit versioned events** to DM (Database/Model Management).

### Key Insight

In distributed systems, duplicate notifications are common due to:
- etcd PubSub retries
- Network timeouts and retransmissions
- Subscriber reconnections
- Manual re-triggering

**Config Plane eliminates this noise before it reaches expensive processing in DM**, using content-addressed hashing and a hash cache.

---

## Responsibilities

### 1. Subscription Management

**Input**: etcd notifications from `/weaver/subscriptions/<tenant>`

**Processing**:
- Listen to etcd watch for subscription changes
- Extract event metadata (subscription ID, version, content)
- Route to appropriate handler (VNET, RouteTable, ACL, ENI, Mapping)

**Output**: ConfigUpdate proto (versioned, sequenced, deduplicated)

### 2. Deduplication

**Algorithm**: Content-addressed hash with TTL cache

```
Input: Subscription event
1. Compute: contentHash = SHA256(Canonical JSON)
2. Check: cache.Get(event_id) exists?
3. If exists && cache.Hash == contentHash:
     return SKIP (duplicate detected)
4. Else:
     continue to validation
```

**Cache behavior**:
- Storage: In-memory LRU (configurable size, default 10k entries)
- TTL: 24 hours (or configurable)
- On cache miss: Reprocess event

### 3. Validation

**Schema validation**:
- Construct type is valid (VNET, RouteTable, ACL, ENI, Mapping)
- Required fields present
- Field types match schema

**Business logic validation**:
- Subscription is active (not revoked)
- Tenant ID is valid and non-zero
- No self-referential updates (VNET cannot update itself)

### 4. Versioning & Sequencing

**Per-event**:
- Assign: `version = max(previous_version) + 1`
- Assign: `sequence = global_sequence++`
- Both stored in ConfigUpdate proto

**Guarantee**: Monotonic ordering, no gaps

### 5. Event Emission

**Output**: Emit ConfigUpdate event to DM

```go
ConfigUpdate {
  event_id: string,
  config_id: string,        // RouteTable_<tenant>_<VNET>
  version: int64,           // v5 → v6
  sequence: int64,          // 1001, 1002, 1003, ...
  content_hash: string,     // SHA256(canonical_json)
  timestamp: time.Time,
  construct: {
    id, type, spec, metadata
  }
}
```

---

## Components

### 1. Subscription Manager

**Responsibility**: Connect to etcd and listen for changes

```go
type SubscriptionManager struct {
  etcdClient *clientv3.Client
  watcher    clientv3.Watcher
  channel    chan *ConfigUpdate
}

func (sm *SubscriptionManager) Start(ctx context.Context) {
  watchCh := sm.watcher.Watch(ctx, "/weaver/subscriptions/", clientv3.WithPrefix())
  
  for resp := range watchCh {
    for _, event := range resp.Events {
      cu := sm.parseEvent(event)
      sm.channel <- cu
    }
  }
}
```

### 2. Deduplication Engine

**Responsibility**: Track seen events, skip duplicates

```go
type DeduplicationEngine struct {
  mu    sync.RWMutex
  cache map[string]*CacheEntry      // event_id → CacheEntry
  ttl   time.Duration
}

type CacheEntry struct {
  Hash      string
  Version   int64
  Timestamp time.Time
}

func (de *DeduplicationEngine) CheckDuplicate(eventID string, contentHash string) (isDuplicate bool, version int64) {
  de.mu.RLock()
  defer de.mu.RUnlock()
  
  if entry, exists := de.cache[eventID]; exists {
    if entry.Hash == contentHash && time.Since(entry.Timestamp) < de.ttl {
      return true, entry.Version  // Duplicate
    }
  }
  return false, 0
}

func (de *DeduplicationEngine) Record(eventID string, contentHash string, version int64) {
  de.mu.Lock()
  defer de.mu.Unlock()
  
  de.cache[eventID] = &CacheEntry{
    Hash:      contentHash,
    Version:   version,
    Timestamp: time.Now(),
  }
}
```

### 3. Validation Engine

**Responsibility**: Schema and business logic validation

```go
type ValidationEngine struct {
  schemas map[string]*Schema  // construct_type → Schema
}

func (ve *ValidationEngine) Validate(cu *ConfigUpdate) error {
  // Schema validation
  schema, ok := ve.schemas[cu.Construct.Type]
  if !ok {
    return fmt.Errorf("unknown construct type: %s", cu.Construct.Type)
  }
  
  if err := schema.Validate(cu.Construct.Spec); err != nil {
    return fmt.Errorf("schema validation failed: %w", err)
  }
  
  // Business logic validation
  if cu.Construct.Type == "VNET" && cu.Construct.ID == "" {
    return errors.New("VNET ID cannot be empty")
  }
  
  return nil
}
```

### 4. Sequencer

**Responsibility**: Assign global monotonic sequence

```go
type Sequencer struct {
  mu       sync.Mutex
  sequence int64  // Persisted to etcd for durability
}

func (s *Sequencer) Next() int64 {
  s.mu.Lock()
  defer s.mu.Unlock()
  
  s.sequence++
  // Periodically persist to etcd (batched)
  return s.sequence
}
```

### 5. Event Emitter

**Responsibility**: Emit ConfigUpdate to DM

```go
type EventEmitter struct {
  channel chan<- *ConfigUpdate
}

func (ee *EventEmitter) Emit(cu *ConfigUpdate) {
  select {
  case ee.channel <- cu:
    // Sent
  case <-time.After(5 * time.Second):
    // If DM not consuming, log and drop (backpressure)
    logWarning("DM not consuming events, dropping event: %s", cu.EventID)
  }
}
```

---

## Deduplication Algorithm

### Complete Flow

```
┌─────────────────────────────────────────────────┐
│ Event arrives from etcd                         │
│ {event_id: "sub-123", content: {...}}           │
└────────────────┬────────────────────────────────┘
                 ↓
        ┌────────────────────┐
        │ Compute contentHash │
        │ SHA256(json)       │
        └────────┬───────────┘
                 ↓
        ┌────────────────────────────┐
        │ Check dedup cache          │
        │ cache.Get(event_id)        │
        └────────┬───────────────────┘
                 ↓
        ┌────────────────────────────┐
        │ Hash matches?              │
        │ (same content twice)       │
        └────┬───────────────────┬───┘
             │                   │
           YES                  NO
             ↓                   ↓
        ┌─────────┐      ┌──────────────┐
        │ SKIP    │      │ PROCESS      │
        │metric:  │      │+ validate    │
        │dedup_hit│      │+ version++   │
        │cost:    │      │+ sequence++  │
        │1ms hash │      │+ record cache│
        │lookup   │      │+ emit event  │
        └─────────┘      │cost: 50-100ms
                         └──────────────┘
```

### Deduplication Matrix

| Scenario | Event A | Event B | Result |
|----------|---------|---------|--------|
| Same subscription, same content | v1, hash1 | v1, hash1 | **SKIP** (dedup hit) |
| Same subscription, different content | v1, hash1 | v1, hash2 | **PROCESS** (change) |
| Different subscription, same content | v1, hash1 | v2, hash1 | **PROCESS** (new subscription) |
| All different | v1, hash1 | v2, hash2 | **PROCESS** (change) |

### Deduplication Impact on Scale

**Without deduplication**:
```
1000 identical retries × 50ms processing = 50 seconds wasted
```

**With deduplication**:
```
1000 identical retries × 1ms hash check = 1 second total
99% latency reduction
```

---

## Data Structures

### Protobuf: ConfigUpdate

```proto
message ConfigUpdate {
  string event_id = 1;              // UUID
  string config_id = 2;             // RouteTable_<tenant>_<VNET>
  int64 version = 3;                // Monotonic version
  int64 sequence = 4;               // Global sequence number
  string content_hash = 5;          // SHA256(canonical_json)
  
  google.protobuf.Timestamp created_at = 6;
  
  message Construct {
    string id = 1;                  // Fully qualified name
    string type = 2;                // VNET, RouteTable, ACL, ENI, Mapping
    bytes spec = 3;                 // Serialized construct data (JSON)
    map<string, string> metadata = 4;  // Tags, annotations
  }
  
  Construct construct = 7;
  
  // For idempotency (prevents duplicate processing)
  string idempotency_key = 8;       // UUID, same for retries
  int32 retry_count = 9;            // Number of retries
  
  // Tenant and source info
  string tenant_id = 10;
  string source = 11;               // "etcd", "api", "manual"
}
```

### In-Memory: Cache Entry

```go
type CacheEntry struct {
  EventID    string
  ConfigID   string
  Hash       string
  Version    int64
  Sequence   int64
  Timestamp  time.Time
  Size       int                    // bytes, for memory management
}

// Metrics for monitoring
type DeduplicationMetrics struct {
  CacheMisses       int64
  CacheHits         int64
  CacheEvictions    int64  // LRU evictions
  ProcessedEvents   int64
  SkippedDuplicates int64
  CacheSize         int64  // bytes
}
```

---

## Interface & APIs

### ConfigPlane Interface

```go
type ConfigPlane interface {
  // Start consuming subscriptions
  Start(ctx context.Context) error
  
  // Stop gracefully
  Close() error
  
  // Get output channel
  Events() <-chan *ConfigUpdate
  
  // Get deduplication metrics
  GetMetrics() *DeduplicationMetrics
  
  // Reset dedup cache (for testing)
  ClearCache()
}

type ConfigPlaneImpl struct {
  subMgr    *SubscriptionManager
  dedupEng  *DeduplicationEngine
  validEng  *ValidationEngine
  sequencer *Sequencer
  emitter   *EventEmitter
  
  eventCh   chan *ConfigUpdate
  metricsM  sync.RWMutex
  metrics   *DeduplicationMetrics
}

func (cp *ConfigPlaneImpl) Start(ctx context.Context) error {
  go cp.subMgr.Start(ctx)
  go cp.processEvents(ctx)
  return nil
}

func (cp *ConfigPlaneImpl) processEvents(ctx context.Context) {
  for event := range cp.subMgr.channel {
    // Step 1: Compute hash
    contentHash := cp.computeHash(event)
    
    // Step 2: Check dedup cache
    isDuplicate, cachedVersion := cp.dedupEng.CheckDuplicate(event.EventID, contentHash)
    if isDuplicate {
      cp.recordMetric("dedup_hit", 1)
      cp.recordMetric("skip_event", 1)
      continue  // Skip duplicate
    }
    
    // Step 3: Validate
    cu := &ConfigUpdate{
      EventID:       event.EventID,
      ConfigID:      event.ConfigID,
      ContentHash:   contentHash,
      IdempotencyKey: event.IdempotencyKey,
      RetryCount:    event.RetryCount,
    }
    
    if err := cp.validEng.Validate(cu); err != nil {
      cp.recordMetric("validation_error", 1)
      logError("validation failed: %v", err)
      continue  // Drop invalid event
    }
    
    // Step 4: Assign version and sequence
    cu.Version = cp.getNextVersion(cu.ConfigID)
    cu.Sequence = cp.sequencer.Next()
    cu.CreatedAt = timestamppb.Now()
    
    // Step 5: Record in cache
    cp.dedupEng.Record(cu.EventID, contentHash, cu.Version)
    
    // Step 6: Emit to DM
    cp.emitter.Emit(cu)
    cp.recordMetric("event_emitted", 1)
  }
}

func (cp *ConfigPlaneImpl) GetMetrics() *DeduplicationMetrics {
  cp.metricsM.RLock()
  defer cp.metricsM.RUnlock()
  return cp.metrics
}
```

---

## Error Handling

### Error Categories

| Category | Cause | Action |
|----------|-------|--------|
| **Schema Error** | Invalid construct type or missing fields | Log, drop event, metric counter |
| **Validation Error** | Business logic fails (invalid tenant, self-ref) | Log, drop event, alert ops |
| **Sequence Error** | Sequencer fails to allocate | Retry with exponential backoff, alert |
| **Channel Full** | DM not consuming events | Log warning, drop with metric |
| **etcd Error** | etcd unavailable | Retry watch, fallback to polling |

### Retry Strategy

```go
func (cp *ConfigPlaneImpl) processWithRetry(event *Event, maxRetries int) error {
  var err error
  for attempt := 0; attempt < maxRetries; attempt++ {
    err = cp.processEvent(event)
    if err == nil {
      return nil
    }
    
    // Exponential backoff: 100ms, 200ms, 400ms, 800ms
    backoff := 100 * (1 << uint(attempt))
    time.Sleep(time.Duration(backoff) * time.Millisecond)
  }
  return err
}
```

### Observability on Errors

```
Log level: ERROR
  - Validation failure: "validation_failed: construct={}, reason={}"
  - Sequence allocation: "sequencer_error: {}"
  - Channel blocked: "layer2_not_consuming: drops={}"

Metric: "config_plane_errors_total{error_type=validation|sequence|channel}"
Alert: If error rate > 10 errors/sec for 5 minutes
```

---

## Observability

### Metrics (Prometheus)

```
# Deduplication metrics
fm_config_dedup_cache_hits_total{tenant_id="T1"}
fm_config_dedup_cache_misses_total{tenant_id="T1"}
fm_config_dedup_cache_size_bytes{tenant_id="T1"}
fm_config_dedup_cache_evictions_total

# Processing metrics
fm_config_events_processed_total{status="success"|"dropped"|"error"}
fm_config_events_skipped_total{reason="duplicate"}
fm_config_validation_errors_total
fm_config_sequencer_errors_total
fm_config_layer2_channel_full_total

# Latency metrics
fm_config_processing_duration_seconds{quantile="0.5"|"0.95"|"0.99"}
fm_config_dedup_check_duration_seconds{quantile="0.5"|"0.95"|"0.99"}
```

### Structured Logging

```json
{
  "timestamp": "2026-06-19T14:30:00Z",
  "layer": "ConfigPlane",
  "event": "EventProcessed",
  "event_id": "sub-123",
  "config_id": "RouteTable_tenant1_vnet1",
  "version": 6,
  "sequence": 1002,
  "content_hash": "abc123...",
  "dedup_status": "hit|miss",
  "duration_ms": 5,
  "status": "success|error"
}
```

---

## Testing Strategy

### Unit Tests

1. **Deduplication**:
   - Identical events deduplicated
   - Different content processed
   - Cache TTL expiration
   - Cache LRU eviction

2. **Validation**:
   - Valid schema passes
   - Invalid schema fails
   - Missing required fields fails

3. **Versioning**:
   - Version monotonicity
   - Sequence no gaps
   - Idempotency key tracking

### Integration Tests

1. **End-to-End**: Real etcd → Config Plane → DM
2. **Duplicate Handling**: 1000 identical retries → 999 deduplicated
3. **Error Scenarios**: etcd down, DM slow, validation failures

### Load Tests

```
Setup: 10 concurrent etcd subscribers
Load: 1000 events/sec, 50% duplicates
Target:
  - Dedup hit rate: > 95%
  - Latency p99: < 10ms
  - Throughput: > 5000 events/sec
```

---

## Configuration

### ConfigPlane Settings

```yaml
config_plane:
  # Deduplication
  dedup_cache_size: 10000          # Max cache entries
  dedup_cache_ttl: "24h"           # Cache entry TTL
  dedup_eviction_policy: "lru"     # LRU eviction
  
  # Validation
  enable_schema_validation: true
  enable_business_logic_validation: true
  
  # Backpressure
  event_channel_size: 1000
  channel_full_timeout: "5s"
  
  # Metrics
  metrics_interval: "10s"
  enable_detailed_metrics: false
```

---

## Summary

**CM (Config Plane)** is the **noise filter** of FM:
- Ingests subscription changes from etcd
- **Deduplicates** identical changes (hash-based, ~1ms per check)
- **Validates** schema and business logic
- **Assigns versions and sequences** for deterministic ordering
- **Emits versioned events** to DM

**Key achievement**: 99% latency reduction on duplicate notifications (50ms → 1ms), enabling scale without reprocessing overhead.

**Next layer**: [FM_DESIGN_LAYER2_DATABASE_MODEL.md](FM_DESIGN_LAYER2_DATABASE_MODEL.md)
