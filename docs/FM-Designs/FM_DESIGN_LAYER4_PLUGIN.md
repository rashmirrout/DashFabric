# FM Design: Layer 4 - Goal State Programming Plugin Architecture

**Version**: 1.0  
**Status**: Design Complete  
**Parent Document**: [FM_ARCHITECTURE_SPEC.md](FM_ARCHITECTURE_SPEC.md)

---

## Overview

**Layer 4: Goal State Programming Plugin** executes Goal States on DASH devices. It's a **pluggable, library-based architecture** supporting multiple vendors without re-deployment.

### Key Design

- **Not gRPC**: Library calls, not network calls (lower latency, no serialization)
- **Multi-vendor**: Intel, Nvidia, Custom plugins loaded independently
- **Extensible**: New DASH proto versions instantiate new plugin without affecting existing
- **Thread-safe**: Worker pool for concurrent programming
- **Idempotent**: Same Goal State fingerprint returns cached result

---

## Plugin Interface

### Core Contract

```go
type DASHProgrammer interface {
  // Metadata
  Name() string                           // "intel-dpu-v1.2.3"
  Version() string                        // Semantic version
  SupportedDASHVersion() string           // "2024-03"
  MaxConcurrentPrograms() int             // e.g., 10
  
  // Lifecycle
  Initialize(config map[string]interface{}) error
  Close() error
  
  // Programming
  Apply(ctx context.Context, gs *GoalState) (*Result, error)
  Query(ctx context.Context, eniID string) (*ActualState, error)
  Rollback(ctx context.Context, eniID string, targetVersion int64) error
  
  // Observability
  GetMetrics() map[string]interface{}
  GetStatus() string  // "healthy", "degraded", "offline"
  GetDetailedStatus() map[string]interface{}
}

type Result struct {
  Status            string                        // "success", "partial", "failure"
  AppliedVersion    int64                         // Which constructs succeeded
  ActualFingerprint string                        // SHA256 of actual state after
  FailedConstructs  []string                      // Which ones failed
  ConstructStatus   map[string]string             // construct → status
  Error             error
  Latency           time.Duration
  Timestamp         time.Time
}

type ActualState struct {
  ENIID      string
  Version    int64
  Fingerprint string
  Constructs map[string]interface{}  // RouteTable, ACL, Mapping as returned
}
```

---

## Plugin Registry

```go
type PluginRegistry struct {
  mu      sync.RWMutex
  plugins map[string]DASHProgrammer  // plugin_id → programmer
  
  // ENI → Plugin mapping
  eniPluginMap map[string]string     // eni_id → plugin_id (configured at ENI creation)
}

func (pr *PluginRegistry) RegisterPlugin(id string, programmer DASHProgrammer) error {
  pr.mu.Lock()
  defer pr.mu.Unlock()
  
  if _, exists := pr.plugins[id]; exists {
    return fmt.Errorf("plugin already registered: %s", id)
  }
  
  if err := programmer.Initialize(pr.globalConfig); err != nil {
    return fmt.Errorf("plugin initialization failed: %w", err)
  }
  
  pr.plugins[id] = programmer
  logInfo("registered plugin: %s (version: %s)", id, programmer.Version())
  return nil
}

func (pr *PluginRegistry) GetPluginForENI(eniID string) DASHProgrammer {
  pr.mu.RLock()
  defer pr.mu.RUnlock()
  
  pluginID := pr.eniPluginMap[eniID]
  return pr.plugins[pluginID]
}

func (pr *PluginRegistry) ListPlugins() []string {
  pr.mu.RLock()
  defer pr.mu.RUnlock()
  
  var names []string
  for name := range pr.plugins {
    names = append(names, name)
  }
  return names
}
```

---

## Example Implementation: Intel DPU Plugin

```go
type IntelDPUProgrammer struct {
  name              string
  version           string
  supportedDASH     string
  maxConcurrent     int
  
  connPool          *ConnectionPool      // To Intel DPU devices
  resultCache       map[string]*Result   // fingerprint → cached result
  cacheMu           sync.RWMutex
  
  workerPool        *ThreadPool          // Concurrent programming
  
  metrics           *ProgrammerMetrics
}

func NewIntelDPUProgrammer(config map[string]interface{}) (*IntelDPUProgrammer, error) {
  return &IntelDPUProgrammer{
    name:              "intel-dpu-v1.2.3",
    version:           "1.2.3",
    supportedDASH:     "2024-03",
    maxConcurrent:     10,
    resultCache:       make(map[string]*Result),
    workerPool:        NewThreadPool(10),  // 10 workers
    metrics:           &ProgrammerMetrics{},
  }, nil
}

func (idp *IntelDPUProgrammer) Apply(ctx context.Context, gs *GoalState) (*Result, error) {
  // Step 1: Check cache for idempotency
  idp.cacheMu.RLock()
  if cached, exists := idp.resultCache[gs.Fingerprint]; exists {
    idp.cacheMu.RUnlock()
    idp.metrics.CacheHits++
    return cached, nil  // Idempotent: same fingerprint = same result
  }
  idp.cacheMu.RUnlock()
  idp.metrics.CacheMisses++
  
  // Step 2: Submit to worker pool
  start := time.Now()
  resultCh := idp.workerPool.Submit(func() interface{} {
    return idp.programENI(ctx, gs)
  })
  
  // Step 3: Wait for result (with timeout)
  select {
  case result := <-resultCh:
    res := result.(*Result)
    res.Latency = time.Since(start)
    res.Timestamp = timestamppb.Now()
    
    // Cache result
    idp.cacheMu.Lock()
    idp.resultCache[gs.Fingerprint] = res
    idp.cacheMu.Unlock()
    
    return res, nil
  case <-ctx.Done():
    return nil, ctx.Err()
  }
}

func (idp *IntelDPUProgrammer) programENI(ctx context.Context, gs *GoalState) *Result {
  result := &Result{
    AppliedVersion:   gs.Version,
    ConstructStatus:  make(map[string]string),
  }
  
  // Step 1: Get connection to device
  conn, err := idp.connPool.GetConnection(gs.ENIID)
  if err != nil {
    result.Status = "failure"
    result.Error = err
    return result
  }
  defer conn.Close()
  
  // Step 2: Program RouteTable
  if gs.RouteTable != nil {
    err := idp.programRouteTable(ctx, conn, gs.RouteTable)
    if err != nil {
      result.ConstructStatus[gs.RouteTable.ID] = fmt.Sprintf("error: %v", err)
      result.FailedConstructs = append(result.FailedConstructs, gs.RouteTable.ID)
    } else {
      result.ConstructStatus[gs.RouteTable.ID] = "success"
    }
  }
  
  // Step 3: Program ACL
  if gs.ACL != nil {
    err := idp.programACL(ctx, conn, gs.ACL)
    if err != nil {
      result.ConstructStatus[gs.ACL.ID] = fmt.Sprintf("error: %v", err)
      result.FailedConstructs = append(result.FailedConstructs, gs.ACL.ID)
    } else {
      result.ConstructStatus[gs.ACL.ID] = "success"
    }
  }
  
  // Step 4: Program Mapping
  if gs.Mapping != nil {
    err := idp.programMapping(ctx, conn, gs.Mapping)
    if err != nil {
      result.ConstructStatus[gs.Mapping.ID] = fmt.Sprintf("error: %v", err)
      result.FailedConstructs = append(result.FailedConstructs, gs.Mapping.ID)
    } else {
      result.ConstructStatus[gs.Mapping.ID] = "success"
    }
  }
  
  // Step 5: Determine overall status
  if len(result.FailedConstructs) == 0 {
    result.Status = "success"
  } else if len(result.FailedConstructs) < 3 {  // 3 construct types total
    result.Status = "partial"
  } else {
    result.Status = "failure"
  }
  
  // Step 6: Query actual state (for fingerprint)
  actual, _ := idp.queryENI(ctx, conn, gs.ENIID)
  if actual != nil {
    result.ActualFingerprint = actual.Fingerprint
  }
  
  return result
}

func (idp *IntelDPUProgrammer) Query(ctx context.Context, eniID string) (*ActualState, error) {
  conn, err := idp.connPool.GetConnection(eniID)
  if err != nil {
    return nil, err
  }
  defer conn.Close()
  
  return idp.queryENI(ctx, conn, eniID)
}

func (idp *IntelDPUProgrammer) queryENI(ctx context.Context, conn *Connection, eniID string) (*ActualState, error) {
  // Query Intel DPU device
  resp, err := conn.Query(ctx, &QueryRequest{ENIID: eniID})
  if err != nil {
    return nil, err
  }
  
  // Parse response and compute fingerprint
  fingerprint := SHA256(resp.State)
  
  return &ActualState{
    ENIID:       eniID,
    Version:     resp.Version,
    Fingerprint: fingerprint,
    Constructs:  resp.Constructs,
  }, nil
}

func (idp *IntelDPUProgrammer) GetStatus() string {
  conn, err := idp.connPool.Ping()
  if err != nil {
    return "offline"
  }
  if idp.metrics.ErrorRate > 0.1 {  // >10% errors
    return "degraded"
  }
  return "healthy"
}

func (idp *IntelDPUProgrammer) GetMetrics() map[string]interface{} {
  return map[string]interface{}{
    "cache_hits":    idp.metrics.CacheHits,
    "cache_misses":  idp.metrics.CacheMisses,
    "programs_total": idp.metrics.ProgramsTotal,
    "errors_total":  idp.metrics.ErrorsTotal,
    "avg_latency":   idp.metrics.AvgLatency,
  }
}
```

---

## Multi-Vendor Support

### Runtime Plugin Loading

```go
// At startup, load vendor plugins
func LoadPlugins(config *Config) (*PluginRegistry, error) {
  registry := &PluginRegistry{}
  
  // Load Intel plugin
  intelPlugin, _ := NewIntelDPUProgrammer(config.Intel)
  registry.RegisterPlugin("intel-dpu-v1.2.3", intelPlugin)
  
  // Load Nvidia plugin
  nvidiaPlugin, _ := NewNvidiaDPUProgrammer(config.Nvidia)
  registry.RegisterPlugin("nvidia-dpu-v2.0.1", nvidiaPlugin)
  
  // Load custom plugin (customer-provided)
  customPlugin, _ := loadCustomPlugin(config.CustomPlugin)
  registry.RegisterPlugin("custom-dash-v1", customPlugin)
  
  return registry, nil
}
```

### Vendor Extensions

New DASH proto version or vendor attribute doesn't require FM changes:

```
Scenario: Intel adds new telemetry attribute to Goal State

Step 1: Vendor updates Intel plugin to support new attribute
Step 2: Goal State proto extended with intel_telemetry field
Step 3: Intel plugin processes extensions from Goal State:
        gs.Extensions["intel_telemetry"] → programTelemetry()
Step 4: Other plugins ignore intel_telemetry (unknown extension)
Step 5: No FM core changes needed
```

---

## Error Handling

| Scenario | Handling |
|----------|----------|
| Device offline | Retry with exponential backoff, return "failure" |
| Device timeout | Timeout context, return "failure" |
| Partial failure | Return "partial" with failed_constructs list |
| Cache miss | First program call, cache result, idempotent for retries |
| Version mismatch | Query actual, reconcile, re-program if needed |

---

## Thread Safety

```go
type ThreadPool struct {
  workers int
  taskCh  chan func() interface{}
}

func (tp *ThreadPool) Submit(task func() interface{}) <-chan interface{} {
  resultCh := make(chan interface{}, 1)
  
  go func() {
    // Worker picks up task
    result := task()
    resultCh <- result
  }()
  
  return resultCh
}

// Result: 10 concurrent ENI programming tasks without blocking
```

---

## Metrics & Observability

```
fm_plugin_apply_duration_seconds{plugin="intel", quantile="0.5"|"0.95"|"0.99"}
fm_plugin_cache_hits_total{plugin="intel"}
fm_plugin_cache_misses_total{plugin="intel"}
fm_plugin_errors_total{plugin="intel", reason="timeout"|"offline"|"version_mismatch"}
fm_plugin_workers_active{plugin="intel"}
fm_plugin_status{plugin="intel", status="healthy"|"degraded"|"offline"}
```

---

## Testing

### Unit Tests
- Idempotency: Same fingerprint returns cached result
- Error handling: Device offline, timeout
- Partial failure: 2 of 3 constructs fail

### Integration Tests
- Real device programming (if available)
- Plugin switching (Intel → Nvidia)
- Multi-vendor concurrency

---

## Summary

**Layer 4 (Plugin Architecture)** enables extensibility:
- **Library-based** (not gRPC) for low latency
- **Multi-vendor** (Intel, Nvidia, Custom) without re-deployment
- **Thread-safe** worker pool for concurrent programming
- **Idempotent** (same fingerprint = same result)
- **Extensible** (new DASH versions instantiate new plugins)

**Key achievement**: Add new DPU vendor or experimental DASH extension in < 1 day.

**Final step**: [FM_DESIGN_FEEDBACK_RECONCILIATION.md](FM_DESIGN_FEEDBACK_RECONCILIATION.md) for reliability
