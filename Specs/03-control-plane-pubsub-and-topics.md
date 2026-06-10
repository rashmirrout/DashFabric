# 03 — Control Plane PubSub & Topic Taxonomy

> The PubSub is the **nervous system** of DashFabric. This document defines the
> key/topic hierarchy, message schemas, ordering guarantees, versioning, and
> the consumption pattern that maps PubSub events to actor mailboxes.

---

## 1. Why etcd

We surveyed four candidates:

| System | Pros | Cons | Verdict |
|---|---|---|---|
| **etcd v3** | Hierarchical key prefixes; native prefix watches; revisions = global ordering per cluster; leases for presence; mTLS + RBAC built-in; well-known operability | Throughput ceiling ~10k writes/sec/cluster; key sizes limited; not designed for huge values | ✅ **Primary** |
| Apache Kafka | Massive write throughput; mature; partitioning | No hierarchical topic semantics; consumer offset management painful for per-host slicing; watches are partition-grained | Reject (mismatch) |
| NATS JetStream KV | Subject hierarchies; per-key watches; light footprint | Less mature ops; cross-region replication weaker; smaller community for our use case | Acceptable alt |
| ZooKeeper | Hierarchical, mature | Java, declining ecosystem, weaker watches | Reject |

**Decision:** etcd v3 as the primary. We isolate the actual API behind a
`ConfigBus` Go interface so a future swap to NATS JetStream KV (or a
horizontally-sharded etcd-of-etcds) is contained.

---

## 2. Key Hierarchy

All keys are UTF-8 strings under a single root namespace.

```
/config/v1/hosts/<HostID>                               # HDO spec
/config/v1/hosts/<HostID>/<ContainerGUID>               # CO spec
/config/v1/hosts/<HostID>/<ContainerGUID>/<NicID>       # NO spec (ENI)

/state/v1/hosts/<HostID>/registered                      # device presence (leased)
/state/v1/hosts/<HostID>/programmed                      # last successful program ts
/state/v1/hosts/<HostID>/<ContainerGUID>/<NicID>/status  # per-NO programming status

/event/v1/registrations/<HostID>/registered              # upstream hook (leased)
/event/v1/registrations/<HostID>/unregistered            # tombstone

/shardmap/v1/shards/<ShardID>                            # PM ownership records
/shardmap/v1/leases/<ShardID>                            # primary election lease
/shardmap/v1/devices/<HostID>                            # current ShardID for device

/policy/v1/global/...                                    # fleet-wide knobs (rate limits, caps)
/policy/v1/tenants/<TenantID>/...                        # per-tenant quotas
```

**Conventions:**
- `/config/` — **intent** owned by upstream. DashFabric reads.
- `/state/` — **observed** state. DashFabric writes.
- `/event/` — **transient, leased** signals across the boundary.
- `/shardmap/` — **internal** to DashFabric. PM owns.
- `/policy/` — operator-set, low-write-rate.

### 2.1 Naming Rules
- `HostID`: 32-char URL-safe base64 of a hardware-derived GUID.
- `ContainerGUID`: GUID assigned by upstream (Azure ARM ID, K8s UID, etc.).
- `NicID`: human-friendly stable name (`nic-primary`, `nic-storage-01`).

Slashes inside any segment are forbidden; the dispatcher relies on segment
count for routing.

---

## 3. Value Schema

Every value is a **Protobuf-encoded message** with a stable header:

```protobuf
message Envelope {
  string  schema_version  = 1;   // e.g. "v1.3.0"
  string  resource_kind   = 2;   // HostDeviceSpec | ContainerSpec | NicSpec | ...
  bytes   payload         = 3;   // serialized resource message
  string  payload_hash    = 4;   // sha256(payload); for idempotency
  string  publisher       = 5;   // upstream identity
  google.protobuf.Timestamp issued_at = 6;
  map<string, string> trace_context  = 7;  // W3C traceparent + tracestate
  uint64  monotonic_rev   = 8;   // upstream's monotonic per-key counter
}
```

We deliberately **do not** rely on `monotonic_rev` for ordering — etcd's
`ModRevision` is the source of truth for ordering within DashFabric.
`monotonic_rev` exists to detect upstream regressions (it should never
decrease for a key; if it does, alert).

### 3.1 Resource Schemas (sketches)

```protobuf
message HostDeviceSpec {
  string host_id          = 1;
  string region           = 2;
  string availability_zone= 3;
  DeviceCapabilities caps = 4;
  HAConfig             ha = 5;  // pairing peer hostID, role hints
  map<string, string> labels  = 6;
  AdminState   admin_state    = 7;  // UP | DOWN | MAINTENANCE
}

message ContainerSpec {
  string container_guid  = 1;
  string tenant_id       = 2;
  string vm_type         = 3;
  repeated string vnet_refs = 4;
  AdminState admin_state = 5;
}

message NicSpec {
  string nic_id          = 1;
  string mac_address     = 2;
  EniMode mode           = 3;  // REGULAR | FNIC (floating NIC, per DASH HLD)
  string  vnet_ref       = 4;
  repeated AclGroupRef inbound_acl_stages  = 5;  // VNIC, Subnet, VNET
  repeated AclGroupRef outbound_acl_stages = 6;
  RouteGroupRef route_group  = 7;
  MeteringPolicyRef metering = 8;
  HaRole ha_role             = 9;  // ACTIVE | PASSIVE
  AdminState admin_state     = 10;
  // Inline data only if upstream chose to inline; usually references to child keys.
  repeated Route inline_routes = 11;
  repeated MappingEntry inline_mappings = 12;
}
```

### 3.2 In-line vs Child Keys for Large Sub-resources

Some DASH resources are *huge* (100k routes/ENI, 8M CA-PA mappings/DPU). We
do not put 8M entries inside one etcd value. Instead:

- The `NicSpec` references a **route-group key**:
  `/config/v1/hosts/H/C/N/routes/<RouteGroupID>` — itself a paged collection.
- Mappings (`/mappings/<MappingTableID>`) follow the same pattern.
- Each page is ≤ 1 MiB (etcd recommended max value); etcd transactional
  multi-put updates pages atomically.

NO actors subscribe to the relevant paged-collection prefixes and assemble
the full table in memory.

---

## 4. Ordering Guarantees

| Scope | Guarantee | Source |
|---|---|---|
| Single key | Strict total order by `ModRevision` | etcd guarantee |
| Single watch (prefix) | Strict total order across all keys in prefix | etcd guarantee |
| Cross-shard | **No** global ordering guarantee | By design — shards are independent |
| Cross-object causality | Parent must reach PROGRAMMED before child events are flushed | Enforced by bootstrap buffer (see 02 §6.4) |

**Why no global ordering:** it's expensive and unnecessary. Each DPU is
independently programmable; intent for HOST-1 has no causal relationship
with intent for HOST-2.

---

## 5. Watch Topology Inside a Shard

Each shard pod (primary or standby) maintains **N etcd Watch streams**, one
per **HostID it owns**:

```
ShardSet 7 owns devices in range [hash(HOST-X)..hash(HOST-Y)].
Each pod opens:
   Watch( /config/v1/hosts/HOST-X/, prefix=true )
   Watch( /config/v1/hosts/HOST-A1/, prefix=true )
   ... one per owned device
```

**Why per-device watches** rather than one giant prefix watch on
`/config/v1/hosts/`:
- Cleaner resource accounting: we can rate-limit, pause, or restart a
  device's watch in isolation.
- Tightens authorization (etcd RBAC scopes can be applied per prefix).
- Simplifies partition rebalance: when a device moves to another shard,
  the originating shard simply closes its one watch.

**Connection pooling:** Watches are multiplexed over a small pool of HTTP/2
connections to etcd (default 4). The Go etcd client handles this; we just
tune `MaxConcurrentStreams` and `KeepAlive`.

### 5.1 Watch Resumption
- Each watch records the latest observed `ModRevision`.
- On a transient disconnect, resumption starts at `lastRev + 1`.
- On `ErrCompacted`, the watch **falls back to a snapshot**:
  1. `Get(prefix=true, snapshot at currentRev)` reads all current keys.
  2. Each key is dispatched as a synthetic `CREATE_OR_UPDATE` event.
  3. Children no longer present are dispatched as synthetic `DELETE` events.
  4. Watch resumes at `currentRev + 1`.

The dispatcher's idempotency (revision-stamped) makes this safe.

---

## 6. Event Dispatch Pipeline

```
etcd watch goroutine ──► dispatcher (per shard)
                              │
                              │ deserialize Envelope; verify hash; extract TraceContext
                              │
                              │ route by deepest existing ancestor
                              ▼
                       ┌──────────────────┐
                       │  Actor lookup    │  O(1) sync.Map keyed by ObjectID
                       └────┬─────────────┘
                            │ if found
                            ▼ push MsgConfigEvent into mailbox
                       (else)
                            ▼
                       bootstrap buffer keyed by parent prefix
```

### 6.1 Backpressure
- If an actor's mailbox is full, the dispatcher applies **coalesce-by-key**:
  it replaces a pending older-revision event for that key with the new one,
  dropping the older. A counter `dashfabric_event_coalesced_total{shard,objectKind}`
  is incremented.
- This is safe: actors only need the *latest* intent.
- If coalescing cannot help (different keys), the dispatcher blocks the
  watch read for that device. A metric `dashfabric_watch_paused_seconds_total`
  fires; alert if > 0 for sustained windows.

### 6.2 Trace Propagation
The dispatcher creates a span:
```
span "watch.dispatch"
  parent = traceparent from envelope (or root if absent)
  attributes:
    object.id  = <objectID>
    object.kind = HDO|CO|NO
    etcd.rev   = <ModRevision>
```
The span is attached to the `Message.TraceCtx`. The actor's handler
becomes a child span. The HAL call becomes a grandchild span. End-to-end
traces are usable in Tempo.

---

## 7. Schema Evolution

### 7.1 Forward Compatibility
- All schemas are Protobuf with `reserved` rules enforced by `buf lint`.
- Unknown fields are preserved (`proto.MarshalOptions{UseProtoNames: true}`
  not used; default behavior keeps unknowns).
- `schema_version` in the Envelope acts as the **producer-side declared**
  version; consumers reject `MAJOR` mismatches with a structured error.

### 7.2 Backward Compatibility
- Removing a field requires it to be `reserved` for ≥ 2 minor releases.
- Renaming requires double-write for 1 release, then rename in next.

### 7.3 Migration Strategy
- New schema lives under `/config/v2/...` initially.
- DashFabric runs **dual consumers** during migration windows.
- Per-host `migrationFlag` allows phased cutover.

---

## 8. Capacity Sizing for etcd

For a region of **10,000 devices, 32 ENIs each = 320,000 ENIs**:

| Resource | Count | Avg value size | Total storage |
|---|---|---|---|
| Host keys | 10k | 4 KiB | 40 MiB |
| Container keys | ~100k (10 VMs/host) | 2 KiB | 200 MiB |
| ENI spec keys | 320k | 8 KiB | 2.5 GiB |
| Route-group page keys | ~3.2M (100k routes/ENI ≈ 100 pages × 320k) | 1 MiB | 3.2 TiB ❌ |

The naive math says routes don't fit in etcd. **Decision:** routes &
mappings are **NOT stored in etcd** for production scale. Instead:

- Etcd carries *references*: `/config/v1/hosts/H/C/N/routes` → a single small
  value pointing to a **bulk-blob store** (S3-compatible, CDN-fronted) URL +
  hash + version.
- The NO actor downloads the route blob from the blob store, verifies hash,
  caches locally.
- Updates publish a new blob and update the etcd reference; the actor sees
  the etcd change and re-downloads.

**Net etcd footprint for 10k-device region:** ≈ **3 GiB** — well within etcd
limits. **Watch event rate:** dominated by NIC spec key churn, target ≤ 10k
events/sec/shard sustained.

### 8.1 etcd Cluster Sizing
- 5-node cluster per DC region.
- Hardware: 32 vCPU, 64 GiB RAM, NVMe SSD ≥ 1 TiB, dedicated 25 GbE.
- Compaction: every 15 min, auto-defrag nightly.
- Quotas: 16 GiB DB size cap (we operate well below).

---

## 9. Pluggable ConfigBus Interface

```go
type ConfigBus interface {
    // Get returns the latest value (or nil) and the ModRevision.
    Get(ctx context.Context, key string) (Envelope, int64, error)

    // List enumerates a prefix (snapshot at currentRev).
    List(ctx context.Context, prefix string) ([]KeyValue, int64, error)

    // Watch returns a stream of events from `fromRev` onwards.
    Watch(ctx context.Context, prefix string, fromRev int64) (<-chan Event, error)

    // Write is only used by the dispatcher to publish /state/* observations.
    Write(ctx context.Context, key string, val Envelope, opts ...WriteOpt) (int64, error)

    // Lease lifecycle for /event/* presence keys.
    GrantLease(ctx context.Context, ttl time.Duration) (LeaseID, error)
    KeepAlive(ctx context.Context, l LeaseID) error
}
```

Adapters:
- `etcdv3` — production.
- `natsjs` — for high-write-rate workloads if etcd becomes a bottleneck.
- `memory` — for unit tests.
- `fileyaml` — for offline-dev / lab use; watches inotify on a directory tree.

---

## 10. Producer Expectations

DashFabric does **not** publish `/config/*` keys. Upstream producers must:

1. Use the documented Envelope + Protobuf schemas.
2. Publish parent before children (best-effort; out-of-order handled by us).
3. Issue **all-or-nothing transactional writes** when several keys must
   land together — etcd's `Txn(...).Then(Put(...), Put(...)).Commit()` works.
4. Treat etcd as the system of record; do not double-publish to other paths.
5. Garbage-collect their own keys on tenant deletion.
6. Honor a `dashfabric.publisher` mTLS identity scoped via etcd RBAC.

### 10.1 Anti-patterns we reject
- Polling instead of watching.
- Heartbeat traffic on `/config/*` (use `/event/*`).
- Embedding 100k entries in one value.

---

## 11. Operational Considerations

| Concern | Mechanism |
|---|---|
| etcd leader transitions | Transparent to clients; watch resumption handles |
| Slow consumer in a shard | Per-watch metrics; alert; coalesce buffer |
| Schema poison value | Dispatcher catches deserialize errors, increments
`dashfabric_envelope_invalid_total{publisher}`, parks the actor in
`CONFIGURING` with the error, alerts |
| Producer impersonation | etcd RBAC + mTLS; `publisher` field cross-checked
against the cert SAN |
| Watch fan-out limits | Pool watches per device; cap connections; the etcd
client multiplexes; we run our own per-host limiter |

---

## 12. Open Questions

| ID | Question | Default |
|---|---|---|
| OQ-301 | Single global etcd cluster vs per-region? | **Per-region.** Cross-region replication done at the upstream producer layer, not via etcd mirroring. |
| OQ-302 | Should we adopt CRDB / FoundationDB for the bulk-route blob store? | **No.** Object-storage (S3-compatible) + version pointer is simpler and cheaper. |
| OQ-303 | Use etcd `Txn` for cross-key atomicity? | **Yes**, encouraged for upstream producers; DashFabric reads tolerate either pattern. |
