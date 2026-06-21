# FM Design: DM - Database/Model Management Specification

**Version**: 1.0  
**Status**: Design Complete  
**Parent Document**: [FM_ARCHITECTURE_SPEC.md](FM_ARCHITECTURE_SPEC.md)

---

## Table of Contents

1. [Overview](#overview)
2. [Responsibilities](#responsibilities)
3. [Actor Model](#actor-model)
4. [Storage Architecture](#storage-architecture)
5. [Consistency Enforcement](#consistency-enforcement)
6. [Index Management](#index-management)
7. [Cascading Deletes](#cascading-deletes)
8. [Concurrency Model](#concurrency-model)
9. [APIs & Interfaces](#apis--interfaces)
10. [Testing Strategy](#testing-strategy)

---

## Overview

**DM: Database/Model Management** is the **single source of truth** for all constructs. It:
- Stores normalized construct data with versions
- Maintains consistency invariants (no dangling refs, no circular deps)
- Provides fast indexed lookups (O(log n))
- Handles atomic writes and cascading deletes
- Emits watch notifications for GM

### Guiding Principle

**All queries return fresh state.** There is no caching at this layer; every read comes from etcd. This ensures consistency at the cost of slightly higher latency (acceptable for FM's use case where consistency > speed).

---

## Responsibilities

### 1. Construct Storage

Store every construct with full metadata:
```
{
  id: "RouteTable_tenant1_vnet1",
  type: "RouteTable",
  version: 6,
  sequence: 1002,
  hash: "abc123...",
  owner: "vnet1",
  spec: {...},
  created_at: timestamp,
  updated_at: timestamp,
  deleted_at: null
}
```

### 2. Consistency Validation

Before accepting any write, validate:
- ✓ No circular dependencies (A → B → A)
- ✓ No dangling references (RouteTable_v6 references Subnet_v3 which exists)
- ✓ VNET isolation (RouteTable from VNET_A not referenced by VNET_B)
- ✓ Version monotonicity (v5 → v6, never v5 → v4)

### 3. Index Management

Maintain multiple indices for fast lookups:
- By construct type and VNET
- By owner
- By version
- By ENI (for ENI-scoped constructs)

### 4. Watch Notifications

Emit notifications when constructs change:
- ENI aggregator in GM listens for changes
- Triggered by: write, update, delete operations

### 5. Cascading Deletes

When VNET deleted:
- Mark VNET as deleted
- Find all RouteTable, ACL, Mapping owned by VNET
- Mark them as deleted (cascade)
- Find all ENIs in VNET
- Mark them as deleted
- Emit notifications for each deletion

---

## Actor Model

### Actor Hierarchy

```
┌─────────────────────────────────────┐
│ Database/Model Layer                │
│                                     │
│ ┌──────────────┬──────────────┐    │
│ │ RouteTable   │ ACL          │    │
│ │ Actor        │ Actor        │    │
│ ├──────────────┼──────────────┤    │
│ │ Mapping      │ ENI          │    │
│ │ Actor        │ Actor        │    │
│ └──────────────┴──────────────┘    │
│ (Each actor: single goroutine,     │
│  serializes updates to its type)   │
│                                    │
│ Shared: etcd storage, indices      │
└─────────────────────────────────────┘
```

### Actor Responsibilities

Each actor type (RouteTableActor, ACLActor, etc.) handles:
1. **Receive updates** from CM (ConfigUpdate)
2. **Validate** using consistency rules
3. **Write to etcd** (atomic transaction)
4. **Update indices**
5. **Emit notifications**

### Actor Interface

```go
type ConstructActor interface {
  // Handle ConfigUpdate for this construct type
  Handle(ctx context.Context, cu *ConfigUpdate) error
  
  // Get construct by ID
  Get(ctx context.Context, id string) (*Construct, error)
  
  // List constructs by criteria
  List(ctx context.Context, criteria *ListCriteria) ([]*Construct, error)
  
  // Watch for changes
  Watch(ctx context.Context, predicate func(*Construct) bool) <-chan *Construct
  
  // Close actor gracefully
  Close() error
}

type RouteTableActor struct {
  name        string
  etcdClient  *clientv3.Client
  indexMgr    *IndexManager
  consistencyChecker *ConsistencyChecker
  
  // Serialize all writes to RouteTable
  writeMu sync.Mutex
}

func (rta *RouteTableActor) Handle(ctx context.Context, cu *ConfigUpdate) error {
  rta.writeMu.Lock()
  defer rta.writeMu.Unlock()
  
  // Step 1: Validate
  if err := rta.consistencyChecker.Validate(cu); err != nil {
    return fmt.Errorf("consistency check failed: %w", err)
  }
  
  // Step 2: Get existing (if update)
  existing, _ := rta.Get(ctx, cu.ConfigID)
  
  // Step 3: Write to etcd (atomic)
  key := fmt.Sprintf("/fm/constructs/RouteTable/%s", cu.ConfigID)
  construct := &Construct{
    ID:        cu.ConfigID,
    Type:      "RouteTable",
    Version:   cu.Version,
    Sequence:  cu.Sequence,
    Hash:      cu.ContentHash,
    Owner:     extractOwner(cu.ConfigID),  // Extract VNET ID
    Spec:      cu.Construct.Spec,
    CreatedAt: timestamppb.New(time.Now()),
    UpdatedAt: timestamppb.New(time.Now()),
  }
  
  _, err := rta.etcdClient.Put(ctx, key, proto.Marshal(construct))
  if err != nil {
    return fmt.Errorf("etcd write failed: %w", err)
  }
  
  // Step 4: Update indices
  rta.indexMgr.UpdateIndex("type_vnet", "RouteTable", extractVNET(cu.ConfigID), cu.ConfigID)
  rta.indexMgr.UpdateIndex("owner", extractOwner(cu.ConfigID), cu.ConfigID)
  rta.indexMgr.UpdateIndex("version", cu.Version, cu.ConfigID)
  
  return nil
}
```

---

## Storage Architecture

### etcd Schema

**Construct Storage**:
```
Key:   /fm/constructs/<type>/<id>
Value: Protobuf(Construct)

Example:
  /fm/constructs/RouteTable/RouteTable_tenant1_vnet1
  /fm/constructs/ACL/ACL_tenant1_vnet1
  /fm/constructs/ENI/ENI_tenant1_host1_0
  /fm/constructs/Mapping/Mapping_tenant1_vnet1
```

**Index Storage**:
```
Key:   /fm/idx/<index_type>/<index_key>
Value: JSON array of construct IDs

Examples:
  /fm/idx/type_vnet/RouteTable/vnet1 → ["RouteTable_tenant1_vnet1", ...]
  /fm/idx/owner/vnet1 → ["RouteTable_tenant1_vnet1", "ACL_tenant1_vnet1", ...]
  /fm/idx/version/6 → ["RouteTable_tenant1_vnet1", ...]
```

**Lifecycle Tracking**:
```
Key:   /fm/lifecycle/<id>
Value: JSON {created_at, updated_at, deleted_at}

Used for: Garbage collection, audit trail
```

### Construct Protobuf

```proto
message Construct {
  // Identity
  string id = 1;              // Globally unique: RouteTable_<tenant>_<vnet>
  string type = 2;            // "RouteTable", "ACL", "ENI", "Mapping"
  
  // Versioning & Hash
  int64 version = 3;          // Monotonic per construct
  int64 sequence = 4;         // Global order
  string hash = 5;            // SHA256(spec) for idempotency
  
  // Ownership
  string owner = 6;           // VNET ID for cascading delete
  
  // Lifecycle
  google.protobuf.Timestamp created_at = 7;
  google.protobuf.Timestamp updated_at = 8;
  google.protobuf.Timestamp deleted_at = 9;  // Soft delete
  
  // Data
  bytes spec = 10;            // Serialized construct spec (JSON)
  map<string, string> metadata = 11;  // Tags, labels
  
  // Relations (for consistency checking)
  repeated string depends_on = 12;    // Which constructs this depends on
  repeated string referenced_by = 13; // Which constructs reference this
}
```

---

## Consistency Enforcement

### Consistency Checker

```go
type ConsistencyChecker struct {
  db *etcd.Client
}

func (cc *ConsistencyChecker) Validate(cu *ConfigUpdate) error {
  // Rule 1: Construct doesn't reference itself
  spec := parseSpec(cu.Construct.Spec)
  for _, ref := range spec.References {
    if ref == cu.ConfigID {
      return errors.New("circular reference: construct references itself")
    }
  }
  
  // Rule 2: All referenced constructs exist (must be in same VNET or global)
  vnetID := extractVNET(cu.ConfigID)
  for _, ref := range spec.References {
    refVNET := extractVNET(ref)
    if refVNET != vnetID && !isGlobalConstruct(ref) {
      return fmt.Errorf("dangling reference: %s not in same VNET", ref)
    }
    
    // Check existence in etcd
    existing, err := cc.db.Get(context.Background(), 
      fmt.Sprintf("/fm/constructs/*/%s", ref))
    if err != nil || existing.Count == 0 {
      return fmt.Errorf("referenced construct does not exist: %s", ref)
    }
  }
  
  // Rule 3: No circular dependencies (detect cycles in DAG)
  if hasCycle, cycle := cc.detectCycle(cu.ConfigID, spec.References); hasCycle {
    return fmt.Errorf("circular dependency detected: %v", cycle)
  }
  
  // Rule 4: Version is monotonic
  existing, _ := cc.db.Get(context.Background(),
    fmt.Sprintf("/fm/constructs/*/%s", cu.ConfigID))
  if existing.Count > 0 {
    existingConstruct := &Construct{}
    proto.Unmarshal(existing.Kvs[0].Value, existingConstruct)
    if cu.Version <= existingConstruct.Version {
      return fmt.Errorf("version not monotonic: %d → %d", 
        existingConstruct.Version, cu.Version)
    }
  }
  
  // Rule 5: VNET isolation
  // (Checked implicitly by extractVNET rules above)
  
  return nil
}

func (cc *ConsistencyChecker) detectCycle(nodeID string, neighbors []string) (bool, []string) {
  visited := make(map[string]bool)
  recStack := make(map[string]bool)
  
  var path []string
  if hasCycleDFS(nodeID, neighbors, visited, recStack, &path, cc.db) {
    return true, path
  }
  return false, nil
}

func hasCycleDFS(node string, neighbors []string, visited, recStack map[string]bool, 
  path *[]string, db *etcd.Client) bool {
  visited[node] = true
  recStack[node] = true
  *path = append(*path, node)
  
  for _, neighbor := range neighbors {
    if !visited[neighbor] {
      if hasCycleDFS(neighbor, []string{}, visited, recStack, path, db) {
        return true
      }
    } else if recStack[neighbor] {
      return true  // Back edge, cycle detected
    }
  }
  
  recStack[node] = false
  *path = (*path)[:len(*path)-1]
  return false
}
```

### Consistency Rules Matrix

| Rule | Check | Failure Action |
|------|-------|-----------------|
| **Self-reference** | construct doesn't reference itself | Reject, log, metric |
| **Dangling ref** | all referenced constructs exist | Reject, log, metric |
| **Circular dep** | no cycles in reference graph | Reject, log, metric |
| **Version mono** | version only increases | Reject, log, metric |
| **VNET isolation** | constructs stay within VNET | Reject, log, metric |
| **Type validity** | construct type is valid | Reject in CM |

---

## Index Management

### Index Strategy

```
Primary indices:
  type_vnet: {type, vnet_id} → [construct_ids]
  owner: {owner} → [construct_ids]
  version: {version} → [construct_ids]
  
Secondary indices:
  eni: {eni_id} → [construct_ids]  (for ENI-scoped queries)
  tenant: {tenant_id} → [construct_ids]  (for multi-tenancy)
```

### Index Manager

```go
type IndexManager struct {
  mu      sync.RWMutex
  indices map[string]map[string][]string  // index_type → key → values
}

func (im *IndexManager) UpdateIndex(indexType, key, value string) {
  im.mu.Lock()
  defer im.mu.Unlock()
  
  if _, exists := im.indices[indexType]; !exists {
    im.indices[indexType] = make(map[string][]string)
  }
  
  // Append if not exists
  list := im.indices[indexType][key]
  for _, v := range list {
    if v == value {
      return  // Already in index
    }
  }
  im.indices[indexType][key] = append(list, value)
}

func (im *IndexManager) RemoveFromIndex(indexType, key, value string) {
  im.mu.Lock()
  defer im.mu.Unlock()
  
  if indexList, exists := im.indices[indexType][key]; exists {
    for i, v := range indexList {
      if v == value {
        // Remove without preserving order (swap with last)
        indexList[i] = indexList[len(indexList)-1]
        im.indices[indexType][key] = indexList[:len(indexList)-1]
        return
      }
    }
  }
}

func (im *IndexManager) QueryIndex(indexType, key string) []string {
  im.mu.RLock()
  defer im.mu.RUnlock()
  
  return im.indices[indexType][key]
}
```

### Lookup Performance

```
Query: "Get all RouteTable in VNET_tenant1_vnet1"
Index: type_vnet["RouteTable"]["vnet1"]
Cost: O(log n) in etcd, O(1) in memory index
Time: < 1ms
```

---

## Cascading Deletes

### Delete Semantics

When parent construct deleted, all children are **soft deleted** (marked as deleted, not hard-removed).

```
Delete VNET_tenant1_vnet1:
  1. Mark VNET as deleted
  2. Find children: RouteTable_tenant1_vnet1, ACL_tenant1_vnet1, Mapping_tenant1_vnet1
  3. Mark each child as deleted (set deleted_at timestamp)
  4. Find all ENIs in VNET
  5. Mark each ENI as deleted
  6. Emit "Deleted" event for each construct
  7. Notify GM (stops generating Goal State for this VNET)
```

### Cascade Algorithm

```go
func (ca *CascadeManager) Delete(ctx context.Context, constructID string) error {
  ca.mu.Lock()
  defer ca.mu.Unlock()
  
  // Get construct
  construct, err := ca.db.Get(ctx, fmt.Sprintf("/fm/constructs/*/%s", constructID))
  if err != nil || construct.Count == 0 {
    return fmt.Errorf("construct not found: %s", constructID)
  }
  
  c := &Construct{}
  proto.Unmarshal(construct.Kvs[0].Value, c)
  
  // If construct is VNET, cascade to children
  if c.Type == "VNET" {
    // Find all constructs owned by this VNET
    children := ca.indexMgr.QueryIndex("owner", c.Owner)
    for _, childID := range children {
      if err := ca.softDelete(ctx, childID); err != nil {
        logError("failed to delete child: %s, error: %v", childID, err)
        // Continue cascading despite error
      }
    }
    
    // Find all ENIs in this VNET
    eniList := ca.indexMgr.QueryIndex("vnet", c.Owner)
    for _, eniID := range eniList {
      if err := ca.softDelete(ctx, eniID); err != nil {
        logError("failed to delete ENI: %s, error: %v", eniID, err)
      }
    }
  }
  
  // Soft delete the construct itself
  return ca.softDelete(ctx, constructID)
}

func (ca *CascadeManager) softDelete(ctx context.Context, constructID string) error {
  key := fmt.Sprintf("/fm/constructs/*/%s", constructID)
  construct, _ := ca.db.Get(ctx, key)
  
  c := &Construct{}
  proto.Unmarshal(construct.Kvs[0].Value, c)
  
  // Set deleted_at timestamp
  c.DeletedAt = timestamppb.Now()
  
  // Write back to etcd
  _, err := ca.db.Put(ctx, key, proto.Marshal(c))
  if err != nil {
    return err
  }
  
  // Remove from indices
  ca.indexMgr.RemoveFromIndex("type_vnet", c.Type, constructID)
  ca.indexMgr.RemoveFromIndex("owner", c.Owner, constructID)
  
  return nil
}
```

---

## Concurrency Model

### Actor Isolation

Each construct type runs in a **separate goroutine**, with a **write mutex** per actor:

```
Goroutine 1: RouteTableActor (serializes all RouteTable writes)
Goroutine 2: ACLActor (serializes all ACL writes)
Goroutine 3: MappingActor (serializes all Mapping writes)
Goroutine 4: ENIActor (serializes all ENI writes)

Result: Different types run in parallel, same type is serialized
```

### Example: Concurrent Updates

```
Input: 3 simultaneous ConfigUpdate events
  Event A: RouteTable_v6_vnet1 (ActorA)
  Event B: ACL_v4_vnet1 (ActorB)
  Event C: ENI_v3_host1_0 (ActorC)

Execution:
  ActorA receives Event A, acquires writeMu, writes to etcd (parallel with B, C)
  ActorB receives Event B, acquires writeMu, writes to etcd (parallel with A, C)
  ActorC receives Event C, acquires writeMu, writes to etcd (parallel with A, B)
  
Result: All 3 complete in < 100ms (vs. 300ms if serial)
```

---

## APIs & Interfaces

### Public API

```go
type Database interface {
  // Get construct by ID
  Get(ctx context.Context, id string) (*Construct, error)
  
  // List constructs matching criteria
  List(ctx context.Context, criteria *ListCriteria) ([]*Construct, error)
  
  // Watch for changes (blocking channel)
  Watch(ctx context.Context, predicate func(*Construct) bool) <-chan *WatchEvent
  
  // Process ConfigUpdate from CM
  ProcessConfigUpdate(ctx context.Context, cu *ConfigUpdate) error
  
  // Delete construct (soft delete with cascading)
  Delete(ctx context.Context, id string) error
  
  // Get metrics
  GetMetrics() *DatabaseMetrics
  
  // Close database
  Close() error
}

type ListCriteria struct {
  Type    string  // "RouteTable", "ACL", etc.
  VnetID  string  // Filter by VNET
  Owner   string  // Filter by owner
  Version int64   // Filter by version
}

type WatchEvent struct {
  Type      string     // "created", "updated", "deleted"
  Construct *Construct
  Timestamp time.Time
}
```

---

## Testing Strategy

### Unit Tests

1. **Consistency Checking**:
   - Circular reference rejection
   - Dangling reference rejection
   - Version monotonicity enforcement

2. **Cascading Deletes**:
   - VNET delete cascades to children
   - ENIs in VNET are deleted
   - Soft delete (not hard delete)

3. **Index Management**:
   - Index updates on write
   - Index removal on delete
   - Index queries return correct results

### Integration Tests

1. **Real etcd**:
   - Write to real etcd and verify persistence
   - Watch notifications work
   - Atomic transactions

2. **Concurrent Updates**:
   - 10 concurrent updates to different types
   - Verify all succeed
   - Verify consistency maintained

3. **Cascading Delete**:
   - Create VNET with 5 children
   - Delete VNET
   - Verify all children marked deleted

---

## Configuration

```yaml
database:
  # etcd connection
  etcd_endpoints: ["localhost:2379"]
  etcd_dial_timeout: "5s"
  
  # Consistency checking
  enable_consistency_checks: true
  circular_dependency_check_depth: 10
  
  # Indices
  maintain_indices: true
  index_sync_interval: "10s"
  
  # Cascading deletes
  cascade_delete_parallelism: 5
  soft_delete_retention: "30d"
  
  # Metrics
  metrics_interval: "10s"
```

---

## Summary

**DM (Database/Model)** is the **source of truth** for FM:
- Stores all constructs with version history
- Enforces consistency invariants at write-time
- Provides fast indexed lookups
- Handles cascading deletes with soft-delete semantics
- Emits watch notifications for downstream layers

**Key property**: Consistency guaranteed at input (every write validated), enabling downstream layers to assume correctness.

**Next layer**: [FM_DESIGN_LAYER3_SOUTHBOUND.md](FM_DESIGN_LAYER3_SOUTHBOUND.md)
