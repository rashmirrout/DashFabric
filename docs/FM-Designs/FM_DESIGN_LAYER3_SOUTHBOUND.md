# FM Design: Layer 3 - Southbound Data Provider Specification

**Version**: 1.0  
**Status**: Design Complete  
**Parent Document**: [FM_ARCHITECTURE_SPEC.md](FM_ARCHITECTURE_SPEC.md)

---

## Overview

**Layer 3: Southbound Data Provider** transforms stored constructs into **per-ENI Goal States**. It:
- Watches Layer 2 for construct changes
- Aggregates all constructs for each ENI (RouteTable, ACL, Mapping)
- Generates complete Goal State in DASH proto format
- Routes to appropriate plugin (Intel/Nvidia/Custom)
- Handles partial failures and retries

### Key Property

**Deterministic**: Same input constructs always generate identical Goal State (same fingerprint).

---

## Responsibilities

### 1. ENI Aggregation

For each ENI in a VNET, fetch and aggregate:
- RouteTable (routes, subnets)
- ACL (ingress/egress rules)
- Mapping (DIP↔VIP, local↔underlay)
- ENI metadata (IP, MAC, etc.)

### 2. Goal State Generation

Compose complete DASH model for one ENI:

```proto
message GoalState {
  string eni_id = 1;
  string vnet_id = 2;
  int64 version = 3;           // max_version(all_constructs)
  string fingerprint = 4;      // SHA256(canonical_json)
  
  RouteTableConfig route_table = 5;
  ACLConfig acl = 6;
  MappingConfig mapping = 7;
  
  map<string, bytes> extensions = 8;   // Vendor-specific
  map<string, string> metadata = 9;
}
```

### 3. Partial Failure Handling

If Goal State programming fails for some ENIs:
- Retry with exponential backoff (100ms → 800ms)
- Log failed constructs
- Escalate after max retries (default 3)

### 4. Feedback Integration

Receive programming result from Layer 4:
- Success: Record applied version and fingerprint
- Partial: Retry specific ENIs
- Failure: Escalate to Config Plane (manual review)

---

## Components

### 1. ENI Aggregator

```go
type ENIAggregator struct {
  db *Database  // Layer 2
  mu sync.RWMutex
  cache map[string]*AggregatedENI  // ENI_ID → cached constructs
}

type AggregatedENI struct {
  ENI       *Construct          // ENI itself
  RouteTable *Construct         // RouteTable for this VNET
  ACL       *Construct          // ACL for this VNET
  Mapping   *Construct          // Mapping for this VNET
  Timestamp time.Time
  Hash      string              // Cache invalidation
}

func (ea *ENIAggregator) Aggregate(ctx context.Context, eniID string) (*AggregatedENI, error) {
  // Get ENI
  eni, err := ea.db.Get(ctx, eniID)
  if err != nil {
    return nil, err
  }
  
  // Extract VNET from ENI
  vnetID := extractVNET(eniID)
  
  // Get RouteTable for this VNET
  routeTableID := fmt.Sprintf("RouteTable_%s", vnetID)
  routeTable, _ := ea.db.Get(ctx, routeTableID)
  
  // Get ACL for this VNET
  aclID := fmt.Sprintf("ACL_%s", vnetID)
  acl, _ := ea.db.Get(ctx, aclID)
  
  // Get Mapping for this VNET
  mappingID := fmt.Sprintf("Mapping_%s", vnetID)
  mapping, _ := ea.db.Get(ctx, mappingID)
  
  return &AggregatedENI{
    ENI:        eni,
    RouteTable: routeTable,
    ACL:        acl,
    Mapping:    mapping,
    Timestamp:  time.Now(),
  }, nil
}
```

### 2. Goal State Generator

```go
type GoalStateGenerator struct {
  aggregator *ENIAggregator
}

func (gsg *GoalStateGenerator) Generate(ctx context.Context, eniID string) (*GoalState, error) {
  // Aggregate constructs for this ENI
  agg, err := gsg.aggregator.Aggregate(ctx, eniID)
  if err != nil {
    return nil, err
  }
  
  // Validate: all constructs have consistent versions
  maxVersion := int64(0)
  if agg.RouteTable != nil && agg.RouteTable.Version > maxVersion {
    maxVersion = agg.RouteTable.Version
  }
  if agg.ACL != nil && agg.ACL.Version > maxVersion {
    maxVersion = agg.ACL.Version
  }
  if agg.Mapping != nil && agg.Mapping.Version > maxVersion {
    maxVersion = agg.Mapping.Version
  }
  
  // Compose Goal State
  gs := &GoalState{
    ENIID:   eniID,
    VNETID:  extractVNET(eniID),
    Version: maxVersion,
  }
  
  // Populate from constructs
  if agg.RouteTable != nil {
    gs.RouteTable = parseRouteTableSpec(agg.RouteTable.Spec)
  }
  if agg.ACL != nil {
    gs.ACL = parseACLSpec(agg.ACL.Spec)
  }
  if agg.Mapping != nil {
    gs.Mapping = parseMappingSpec(agg.Mapping.Spec)
  }
  
  // Compute fingerprint for idempotency
  canonical := canonicalizeGoalState(gs)
  gs.Fingerprint = SHA256(canonical)
  
  return gs, nil
}
```

### 3. Version Stamper

```go
type VersionStamper struct {
  generator *GoalStateGenerator
  stamper   *FingerprintStamper
}

type FingerprintStamper struct {
  cache map[string]string  // ENI_ID → last_fingerprint
  mu    sync.RWMutex
}

func (fs *FingerprintStamper) IsSame(eniID, fingerprint string) bool {
  fs.mu.RLock()
  defer fs.mu.RUnlock()
  
  return fs.cache[eniID] == fingerprint
}

func (fs *FingerprintStamper) Update(eniID, fingerprint string) {
  fs.mu.Lock()
  defer fs.mu.Unlock()
  
  fs.cache[eniID] = fingerprint
}
```

### 4. Partial Failure Handler

```go
type PartialFailureHandler struct {
  generator *GoalStateGenerator
  mu        sync.RWMutex
  retries   map[string]*RetryState  // ENI_ID → RetryState
}

type RetryState struct {
  ENIID          string
  FailedAttempts int
  LastError      string
  NextRetryTime  time.Time
  FailedConstructs []string
}

func (pfh *PartialFailureHandler) Handle(ctx context.Context, result *ProgrammingResult) error {
  pfh.mu.Lock()
  defer pfh.mu.Unlock()
  
  eniID := result.ENIID
  
  if result.Status == "success" {
    // Clean up retry state
    delete(pfh.retries, eniID)
    return nil
  }
  
  // Partial or failure
  retryState := pfh.retries[eniID]
  if retryState == nil {
    retryState = &RetryState{ENIID: eniID}
    pfh.retries[eniID] = retryState
  }
  
  retryState.FailedAttempts++
  retryState.LastError = result.Error.Error()
  retryState.FailedConstructs = result.FailedConstructs
  
  if retryState.FailedAttempts >= 3 {
    // Max retries exceeded
    logError("Max retries exceeded for ENI %s: %v", eniID, result.FailedConstructs)
    return fmt.Errorf("max retries exceeded: %w", result.Error)
  }
  
  // Schedule retry
  backoff := 100 * math.Pow(2, float64(retryState.FailedAttempts-1))
  retryState.NextRetryTime = time.Now().Add(time.Duration(backoff) * time.Millisecond)
  
  return nil
}

func (pfh *PartialFailureHandler) RetryPending(ctx context.Context, pluginRegistry *PluginRegistry) {
  pfh.mu.Lock()
  defer pfh.mu.Unlock()
  
  now := time.Now()
  for eniID, retryState := range pfh.retries {
    if now.After(retryState.NextRetryTime) {
      // Retry this ENI
      gs, _ := pfh.generator.Generate(ctx, eniID)
      plugin := pluginRegistry.GetPluginForENI(eniID)
      plugin.Apply(ctx, gs)  // async, result handled later
    }
  }
}
```

---

## Data Structures

### Goal State Proto

```proto
message GoalState {
  string eni_id = 1;
  string vnet_id = 2;
  int64 version = 3;
  string fingerprint = 4;  // SHA256 for idempotency
  
  message Route {
    string destination = 1;  // CIDR
    string next_hop = 2;
    int32 metric = 3;
  }
  
  message RouteTableConfig {
    string id = 1;
    int64 version = 2;
    repeated Route routes = 3;
  }
  
  message ACLRule {
    bool is_ingress = 1;
    string source = 2;
    string destination = 3;
    string action = 4;  // ALLOW, DENY
  }
  
  message ACLConfig {
    string id = 1;
    int64 version = 2;
    repeated ACLRule rules = 3;
  }
  
  message Mapping {
    string vip = 1;
    repeated string dips = 2;
    string snat_mode = 3;
  }
  
  message MappingConfig {
    string id = 1;
    int64 version = 2;
    repeated Mapping mappings = 3;
  }
  
  RouteTableConfig route_table = 5;
  ACLConfig acl = 6;
  MappingConfig mapping = 7;
  
  map<string, bytes> extensions = 8;
  map<string, string> metadata = 9;
}
```

---

## Algorithms

### Goal State Generation Flow

```
Input: ENI_ID
Output: Goal State (or error)

1. Aggregate constructs for ENI
   - Fetch RouteTable_<vnet>
   - Fetch ACL_<vnet>
   - Fetch Mapping_<vnet>

2. Validate consistency
   - All exist and are not deleted
   - All versions are recent (< 5 minutes old)
   - No missing required fields

3. Compose Goal State
   - Set eni_id, vnet_id
   - Set version = max(versions of all constructs)
   - Populate route_table, acl, mapping from specs

4. Compute fingerprint
   - Canonical JSON serialization
   - SHA256 hash

5. Return Goal State
```

### Idempotency Check

```
Input: Goal State, fingerprint
Output: cached_result or compute_new

1. Check: eni_id, fingerprint seen before?
2. If yes and timestamp < 1 hour:
     Return cached_result (idempotent)
3. Else:
     Send to plugin
     Cache result
```

---

## Watch Integration

Layer 3 watches Layer 2 for changes:

```go
type SouthboundProvider struct {
  generator *GoalStateGenerator
  pluginReg *PluginRegistry
  
  watchCh <-chan *WatchEvent  // From Layer 2
}

func (sp *SouthboundProvider) Start(ctx context.Context) {
  // Watch for any construct change
  watchCh := sp.db.Watch(ctx, func(c *Construct) bool {
    return true  // Watch all
  })
  
  for event := range watchCh {
    // Construct changed, regenerate affected Goal States
    sp.handleConstructChange(ctx, event)
  }
}

func (sp *SouthboundProvider) handleConstructChange(ctx context.Context, event *WatchEvent) {
  // If RouteTable changed, regenerate Goal States for all ENIs in that VNET
  if event.Construct.Type == "RouteTable" {
    vnetID := extractVNET(event.Construct.ID)
    enis := sp.db.List(ctx, &ListCriteria{Type: "ENI", VnetID: vnetID})
    
    for _, eni := range enis {
      gs, err := sp.generator.Generate(ctx, eni.ID)
      if err != nil {
        logError("failed to generate Goal State: %v", err)
        continue
      }
      
      // Route to plugin
      plugin := sp.pluginReg.GetPluginForENI(eni.ID)
      plugin.Apply(ctx, gs)  // async
    }
  }
}
```

---

## Error Scenarios

| Scenario | Action |
|----------|--------|
| RouteTable missing | Log warning, create Goal State without routes |
| ACL missing | Create Goal State without ACL (permissive default) |
| Mapping missing | Create Goal State without SNAT mapping |
| Version inconsistent | Use max version, log warning |
| Fingerprint collision | Treat as same (idempotent) |

---

## Testing

### Unit Tests
- Goal State generation deterministic
- Fingerprint identical for same input
- Idempotency check works
- Partial failure retry logic

### Integration Tests
- Real construct changes trigger regeneration
- Goal State sent to correct plugin
- Feedback from plugin handled correctly

---

## Metrics

```
fm_goalstate_generation_duration_seconds{quantile="0.5"|"0.95"|"0.99"}
fm_goalstate_fingerprint_cache_hits_total
fm_goalstate_fingerprint_cache_misses_total
fm_eni_retries_total{reason="partial"|"failure"}
fm_eni_retry_exhausted_total
```

---

## Summary

**Layer 3 (Southbound Provider)** composes Goal States:
- Aggregates constructs per ENI
- Generates deterministic Goal State (same fingerprint = same content)
- Routes to appropriate plugin
- Handles partial failures with exponential backoff

**Next layer**: [FM_DESIGN_LAYER4_PLUGIN.md](FM_DESIGN_LAYER4_PLUGIN.md)
