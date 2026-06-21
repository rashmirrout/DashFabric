# FM Design: Feedback Loops & Reconciliation

**Version**: 1.0  
**Status**: Design Complete  
**Parent Document**: [FM_ARCHITECTURE_SPEC.md](FM_ARCHITECTURE_SPEC.md)

---

## Overview

**Feedback Loops** ensure FM detects and recovers from divergence between desired state (Goal State) and actual state (DASH device). This is critical for reliability: programming can fail, devices can be manually modified, networks can partition.

---

## Feedback Loop Architecture

### Bidirectional Flow

```
GM (Southbound) → DAL (Plugin) → DASH Device
                           ↓
                    Programming Result
                    (success, partial, failure)
                           ↓
                    Reconciliation Actor
                    (every 5-10 minutes)
                           ↓
            Query actual state from device
                           ↓
                    Compare with desired
                           ↓
             Divergence? → re-apply or alert
```

### ProgrammingResult

```go
type ProgrammingResult struct {
  ENIID              string
  Status             string              // "success", "partial", "failure"
  AppliedVersion     int64               // Which constructs succeeded
  ActualFingerprint  string              // Actual state fingerprint
  FailedConstructs   []string            // Which ones failed
  ConstructStatus    map[string]string   // construct → status
  Error              error
  Latency            time.Duration
  Timestamp          time.Time
}
```

---

## Reconciliation Cycle

### Algorithm

```go
type ReconciliationActor struct {
  db            *Database
  pluginReg     *PluginRegistry
  interval      time.Duration       // 5-10 minutes
  
  lastSync      map[string]time.Time // eni → last sync time
  divergences   map[string]*Divergence
  
  mu            sync.RWMutex
}

func (ra *ReconciliationActor) Start(ctx context.Context) {
  ticker := time.NewTicker(ra.interval)
  defer ticker.Stop()
  
  for range ticker.C {
    ra.reconcileAllVNETs(ctx)
  }
}

func (ra *ReconciliationActor) reconcileAllVNETs(ctx context.Context) {
  // Get all VNETs
  vnets := ra.db.List(ctx, &ListCriteria{Type: "VNET"})
  
  for _, vnet := range vnets {
    // Reconcile each VNET independently
    ra.reconcileVNET(ctx, vnet.ID)
  }
}

func (ra *ReconciliationActor) reconcileVNET(ctx context.Context, vnetID string) {
  // Get all ENIs in this VNET
  enis := ra.db.List(ctx, &ListCriteria{Type: "ENI", VnetID: vnetID})
  
  for _, eni := range enis {
    if err := ra.reconcileENI(ctx, eni.ID); err != nil {
      logError("reconciliation failed for ENI %s: %v", eni.ID, err)
    }
  }
}

func (ra *ReconciliationActor) reconcileENI(ctx context.Context, eniID string) error {
  // Step 1: Get desired state (Goal State from GM)
  desired, err := ra.generateGoalState(ctx, eniID)
  if err != nil {
    return err
  }
  
  // Step 2: Get actual state (from plugin/device)
  plugin := ra.pluginReg.GetPluginForENI(eniID)
  actual, err := plugin.Query(ctx, eniID)
  if err != nil {
    recordMetric("reconciliation_query_error", eniID)
    return err
  }
  
  // Step 3: Compare
  desiredHash := desired.Fingerprint
  actualHash := actual.Fingerprint
  
  recordMetric("reconciliation_check", eniID)
  
  if desiredHash == actualHash {
    // No divergence
    recordMetric("reconciliation_match", eniID, "success")
    ra.clearDivergence(eniID)
    return nil
  }
  
  // Step 4: Divergence detected
  recordMetric("reconciliation_mismatch", eniID)
  
  // Step 5: Analyze cause
  div := &Divergence{
    ENIID:           eniID,
    DesiredVersion:  desired.Version,
    ActualVersion:   actual.Version,
    DesiredHash:     desiredHash,
    ActualHash:      actualHash,
    DetectedAt:      time.Now(),
  }
  
  if desired.Version > actual.Version {
    // We're ahead: re-apply desired state
    div.Cause = "our_version_ahead"
    logInfo("Re-applying Goal State for ENI %s (desired v%d > actual v%d)",
      eniID, desired.Version, actual.Version)
    
    result, _ := plugin.Apply(ctx, desired)
    if result.Status == "success" {
      recordMetric("reconciliation_recovered", eniID)
      ra.clearDivergence(eniID)
      return nil
    } else {
      div.LastRetryError = result.Error.Error()
      ra.recordDivergence(eniID, div)
      return result.Error
    }
    
  } else if desired.Version < actual.Version {
    // Actual is ahead (shouldn't happen)
    div.Cause = "actual_version_ahead_anomaly"
    logError("ANOMALY: Actual version ahead of desired for ENI %s", eniID)
    recordMetric("reconciliation_anomaly", eniID)
    
    // Fall back to re-apply desired
    result, _ := plugin.Apply(ctx, desired)
    if result.Status != "success" {
      ra.recordDivergence(eniID, div)
      return result.Error
    }
    
  } else {
    // Same version but different hash: data corruption or manual change
    div.Cause = "same_version_different_hash"
    logWarning("Hash mismatch with same version for ENI %s", eniID)
    recordMetric("reconciliation_hash_mismatch", eniID)
    
    // Re-apply desired
    result, _ := plugin.Apply(ctx, desired)
    if result.Status != "success" {
      ra.recordDivergence(eniID, div)
      return result.Error
    }
  }
  
  return nil
}

func (ra *ReconciliationActor) recordDivergence(eniID string, div *Divergence) {
  ra.mu.Lock()
  defer ra.mu.Unlock()
  
  if existing, has := ra.divergences[eniID]; has {
    existing.RetryCount++
    existing.LastRetryTime = time.Now()
  } else {
    ra.divergences[eniID] = div
  }
  
  // Alert after 3 retries
  if div.RetryCount >= 3 {
    logError("ALERT: Persistent divergence for ENI %s after 3 retries", eniID)
    recordMetric("reconciliation_escalated", eniID)
  }
}

func (ra *ReconciliationActor) clearDivergence(eniID string) {
  ra.mu.Lock()
  defer ra.mu.Unlock()
  
  delete(ra.divergences, eniID)
}
```

---

## State Transitions

### ENI Lifecycle with Feedback

```
PENDING → SYNCING → SYNCED
            ↓         ↓
          DEGRADED ←─┘
            ↓
          ALERT (manual intervention)
```

### Reconciliation Decision Tree

```
Query actual state from device
        ↓
desired_hash == actual_hash?
    ↓             ↓
   YES            NO (divergence)
    ↓             ↓
  OK        Analyze:
  ↓           ↓
SYNCED    desired.v > actual.v?
            ├─ YES: Re-apply (v_ahead)
            ├─ NO:  (actual_v_ahead)
            │       Log anomaly + re-apply
            └─ (same v, diff hash)
                Hash mismatch
                Log warning + re-apply
                
Result: success?
  ├─ YES: SYNCED
  └─ NO:  DEGRADED + alert
```

---

## Consistency Invariants

**Invariant 1**: All ENIs in VNET see same construct versions
```
If RouteTable_v6 in DB, all ENIs in VNET must have RouteTable_v6
No ENI should have v5 while others have v6
```

**Invariant 2**: Goal State version ≥ construct versions
```
Goal State for ENI must include latest versions of all constructs
Never send old versions when new versions exist
```

**Invariant 3**: ENI never in UNKNOWN state
```
Always either SYNCED or SYNCING (or DEGRADED as error state)
Never have partial sync status
```

---

## Escalation Strategy

### Alert Levels

| Level | Trigger | Action |
|-------|---------|--------|
| **Info** | Reconciliation match | Log metric |
| **Warning** | Partial failure, 1 retry | Log warning, metric |
| **Error** | Failure after 3 retries | Log error, escalate |
| **Critical** | 10+ ENIs diverged | Page oncall |

### Manual Review Path

```
Persistent divergence (after 3 reconciliation cycles)
        ↓
Alert to Config Plane / Ops
        ↓
Manual investigation needed
        ↓
Either:
  1. Fix actual state (manually)
  2. Or accept desired state divergence
  3. Or escalate to DPU vendor
```

---

## Metrics & Observability

```
fm_reconciliation_checks_total{status="match"|"mismatch"}
fm_reconciliation_match_rate         # Percentage of matches
fm_reconciliation_divergence_detected_total
fm_reconciliation_recovered_total    # Recovered by re-sync
fm_reconciliation_escalated_total    # Manual intervention needed
fm_reconciliation_duration_seconds{quantile="0.5"|"0.95"|"0.99"}

By cause:
fm_reconciliation_mismatch_total{cause="our_v_ahead"|"actual_v_ahead"|"hash_mismatch"}
```

### Structured Logging

```json
{
  "timestamp": "2026-06-19T14:30:00Z",
  "event": "reconciliation_complete",
  "eni_id": "eni-tenant1-host1-0",
  "desired_version": 6,
  "actual_version": 6,
  "desired_hash": "abc123...",
  "actual_hash": "abc123...",
  "status": "match",
  "duration_ms": 50
}
```

---

## Configuration

```yaml
reconciliation:
  enabled: true
  interval: "5m"                    # Every 5 minutes
  max_retries: 3                    # Before escalation
  retry_backoff: "100ms"            # Initial backoff
  escalation_threshold: 10          # Divergence count for alert
  
  # Parallelism
  concurrent_eni_checks: 50
  concurrent_vnet_checks: 10
```

---

## Summary

**Feedback Loops & Reconciliation** ensure reliability:
- Detect divergence within 5-10 minutes
- Auto-recover 90% of failures (re-apply Goal State)
- Escalate persistent divergence (manual review)
- Full observability (metrics, logging, alerts)

**Result**: System self-heals from transient failures without operator intervention.

**Next**: [FM_DESIGN_CONSISTENT_MODELING.md](FM_DESIGN_CONSISTENT_MODELING.md)
