# FM Design: Consistent Modeling & Data Schemas

**Version**: 1.0  
**Status**: Design Complete  
**Parent Document**: [FM_ARCHITECTURE_SPEC.md](FM_ARCHITECTURE_SPEC.md)

---

## Canonical Data Model

Every construct follows this structure:

```go
type Construct struct {
  // Identity
  ID        string    // RouteTable_<tenant>_<VNET> (unique)
  Type      string    // "VNET", "RouteTable", "ACL", "ENI", "Mapping"
  
  // Versioning
  Version   int64     // Monotonic (v1 → v2 → v3)
  Sequence  int64     // Global order
  Hash      string    // SHA256(canonical_json) for idempotency
  
  // Ownership (for cascading delete)
  Owner     string    // Parent VNET ID or Tenant ID
  
  // Lifecycle
  CreatedAt time.Time
  UpdatedAt time.Time
  DeletedAt *time.Time // Soft delete
  
  // Data
  Spec      json.RawMessage // Construct-specific spec
  Metadata  map[string]string // Tags, labels
  
  // Relations
  DependsOn    []string // Which constructs this depends on
  ReferencedBy []string // Which constructs reference this
}
```

---

## Naming Conventions

### Deterministic, Hierarchical

```
Global constructs (tenant-level):
  Tenant_<tenant_id>
  SubscriptionConfig_<tenant_id>

VNET-scoped:
  VNET_<tenant_id>_<vnet_id>
  RouteTable_<tenant_id>_<vnet_id>
  ACL_<tenant_id>_<vnet_id>
  Mapping_<tenant_id>_<vnet_id>

ENI-scoped:
  ENI_<tenant_id>_<host_id>_<eni_index>
```

**Benefit**: Can derive VNET from ENI name, or query all RouteTable in VNET using pattern.

---

## Construct Specifications

### VNET Spec

```proto
message VNETSpec {
  string id = 1;
  string tenant_id = 2;
  string name = 3;
  string address_space = 4;  // CIDR e.g., "10.0.0.0/8"
  
  repeated Subnet subnets = 5;
  
  message Subnet {
    string id = 1;
    string name = 2;
    string address_prefix = 3;  // CIDR
  }
}
```

### RouteTable Spec

```proto
message RouteTableSpec {
  string id = 1;
  string vnet_id = 2;
  string name = 3;
  
  repeated Route routes = 4;
  
  message Route {
    string destination = 1;  // CIDR
    string next_hop = 2;     // IP or "Internet"
    int32 metric = 3;
    string name = 4;
  }
}
```

### ACL Spec

```proto
message ACLSpec {
  string id = 1;
  string vnet_id = 2;
  string name = 3;
  
  repeated Rule ingress_rules = 4;
  repeated Rule egress_rules = 5;
  
  message Rule {
    int32 priority = 1;
    string source = 2;
    string destination = 3;
    string protocol = 4;      // TCP, UDP, *
    string action = 5;        // ALLOW, DENY
    string name = 6;
  }
}
```

### Mapping Spec

```proto
message MappingSpec {
  string id = 1;
  string vnet_id = 2;
  
  repeated Mapping mappings = 3;
  
  message Mapping {
    string vip = 1;
    repeated string dips = 2;
    string snat_mode = 3;     // "Static", "Dynamic"
  }
}
```

### ENI Spec

```proto
message ENISpec {
  string id = 1;
  string tenant_id = 2;
  string host_id = 3;
  int32 eni_index = 4;
  
  string vnet_id = 5;
  string subnet_id = 6;
  
  string private_ip = 7;
  string mac_address = 8;
  
  // Plugin selection
  string plugin_id = 9;      // e.g., "intel-dpu-v1.2.3"
}
```

---

## Consistency Rules at Each Layer

### CM: Config Plane

✓ Schema validation (required fields, types)
✓ Syntax validation (valid CIDR, IPs, etc.)

### DM: Database/Model

✓ No circular dependencies
✓ No dangling references
✓ Version monotonicity (v5 → v6)
✓ VNET isolation (RouteTable_A not in VNET_B)
✓ Owner consistency (RouteTable owned by VNET)

### GM: Southbound Provider

✓ All constructs for ENI exist and have same version
✓ VNET references valid
✓ No missing required constructs

### DAL: Plugin

✓ Goal State serializable (valid proto)
✓ Size limits respected
✓ Extension fields valid

---

## Index Patterns

```
Indices for fast O(log n) lookups:

type_vnet: {type="RouteTable", vnet="vnet1"} → [construct_ids]
  Query: "Get all RouteTable in VNET_1"
  Cost: O(log n)

owner: {owner="vnet1"} → [construct_ids]
  Query: "Get all constructs owned by VNET_1" (for cascading delete)
  Cost: O(log n)

version: {version=6} → [construct_ids]
  Query: "Get all constructs at version 6"
  Cost: O(log n)

eni: {eni_id="eni-123"} → [construct_ids]
  Query: "Get all constructs for ENI"
  Cost: O(log n)
```

---

## Reference vs. Embedding

### Decision Matrix

| Scenario | Decision | Reason |
|----------|----------|--------|
| RouteTable shared by 100 ENIs | **Reference** | Dedup, update propagates to all |
| ACL specific to one VNET | **Embed** | Simple, no sharing |
| Subnet list in VNET | **Embed** | Small, always together |
| DIP list in Mapping | **Reference** | Shared pool, versioned separately |

### Example: Reference Implementation

```proto
// Construct A (RouteTable)
message RouteTable {
  string id = 1;
  int64 version = 2;
  repeated Route routes = 3;
}

// Construct B (ENI) references RouteTable
message ENI {
  string id = 1;
  int64 version = 2;
  
  string route_table_ref = 3;  // Reference: "RouteTable_tenant_vnet"
  int64 route_table_version = 4;  // Version of referenced construct
}

// Query: When RouteTable updates, notify all ENIs
// Action: Regenerate Goal State for all ENIs referencing this RouteTable
```

---

## Soft Delete Semantics

Constructs are **soft deleted** (marked with deleted_at), not hard-removed:

```proto
message Construct {
  string id = 1;
  ...
  google.protobuf.Timestamp deleted_at = 9;  // null = alive, timestamp = deleted
}
```

**Benefits**:
- Audit trail (can see what was deleted when)
- Recovery (can undelete if needed)
- Cascading (mark children deleted, not parent)

---

## Protobuf Message Definitions

### ConfigUpdate (CM → DM)

```proto
message ConfigUpdate {
  string event_id = 1;
  string config_id = 2;
  int64 version = 3;
  int64 sequence = 4;
  string content_hash = 5;
  
  google.protobuf.Timestamp created_at = 6;
  
  message Construct {
    string id = 1;
    string type = 2;
    bytes spec = 3;
    map<string, string> metadata = 4;
  }
  
  Construct construct = 7;
  string idempotency_key = 8;
  int32 retry_count = 9;
  string tenant_id = 10;
}
```

### VNETSnapshot (DM → GM)

```proto
message VNETSnapshot {
  string vnet_id = 1;
  int64 version = 2;
  int64 sequence = 3;
  
  message ConstructRef {
    string id = 1;
    string type = 2;
    int64 version = 3;
    string hash = 4;
    bytes spec = 5;
  }
  
  repeated ConstructRef constructs = 4;
  google.protobuf.Timestamp created_at = 5;
  
  message ConsistencyMarker {
    repeated string construct_ids = 1;
    string global_hash = 2;  // Hash of all construct hashes
  }
  
  ConsistencyMarker marker = 6;
}
```

### GoalState (GM → DAL)

```proto
message GoalState {
  string eni_id = 1;
  string vnet_id = 2;
  int64 version = 3;
  string fingerprint = 4;
  
  RouteTableConfig route_table = 5;
  ACLConfig acl = 6;
  MappingConfig mapping = 7;
  
  map<string, bytes> extensions = 8;
  map<string, string> metadata = 9;
}
```

### ProgrammingResult (DAL → feedback)

```proto
message ProgrammingResult {
  string eni_id = 1;
  string status = 2;  // success, partial, failure
  int64 applied_version = 3;
  string actual_fingerprint = 4;
  
  repeated string failed_constructs = 5;
  map<string, string> construct_status = 6;
  
  google.protobuf.Duration latency = 7;
  google.protobuf.Timestamp timestamp = 8;
}
```

---

## Data Flow Example

```
Input: User updates RouteTable in VNET_1

┌─────────────────────────────────────────────────────┐
│ Config Plane receives update                         │
│ ConfigUpdate {                                       │
│   config_id: "RouteTable_tenant1_vnet1"              │
│   version: 6,                                        │
│   sequence: 1002,                                    │
│   hash: "abc123...",                                 │
│   construct: {spec: [...routes...]}                  │
│ }                                                    │
└────────────────┬────────────────────────────────────┘
                 ↓
┌─────────────────────────────────────────────────────┐
│ Database/Model writes and validates                  │
│ Construct {                                          │
│   id: "RouteTable_tenant1_vnet1",                    │
│   type: "RouteTable",                                │
│   version: 6,                                        │
│   sequence: 1002,                                    │
│   owner: "vnet1",                                    │
│   spec: {...},                                       │
│   depends_on: ["VNET_tenant1_vnet1"]                 │
│ }                                                    │
│ Indices updated: type_vnet, owner, version          │
└────────────────┬────────────────────────────────────┘
                 ↓
┌─────────────────────────────────────────────────────┐
│ GM watches, regenerates Goal State             │
│ For each ENI in VNET_1:                              │
│ GoalState {                                          │
│   eni_id: "eni-tenant1-host1-0",                     │
│   version: 6,                                        │
│   fingerprint: "xyz789...",                          │
│   route_table: {id, version, routes}                │
│ }                                                    │
└────────────────┬────────────────────────────────────┘
                 ↓
┌─────────────────────────────────────────────────────┐
│ DAL plugins apply to devices                     │
│ Result: {status: "success", applied_version: 6}     │
└─────────────────────────────────────────────────────┘
```

---

## Summary

**Consistent Modeling**:
- Canonical construct model (ID, Type, Version, Hash, Owner)
- Deterministic naming (RouteTable_<tenant>_<VNET>)
- Strict consistency rules at each layer
- Idempotent hashing and references
- Soft deletes for audit trail

**Result**: No inconsistent state, full traceability, self-describing data model.

**Next**: [FM_IMPLEMENTATION_ROADMAP.md](FM_IMPLEMENTATION_ROADMAP.md)
