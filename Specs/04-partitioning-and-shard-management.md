# 04 — Partitioning & Shard Management

> Goal: any number of devices, any number of pods, **online** scale in and out,
> no global single point of contention.

---

## 1. Why Sharding Is the Hardest Part

A single Go process can comfortably manage 2,000–5,000 devices (~100k actors,
gNMI fan-out to thousands of DPUs). Above that, GC pressure, scheduler tail
latency, and memory pressure begin to dominate. We must split the workload
across pods, and the split must support:

- **Online rebalance** when devices are added/removed.
- **Online split** of a hot shard.
- **Online merge** of cold shards.
- **Failure-domain awareness** (each ShardSet's 3 replicas in distinct AZs).
- **Locality-of-reference** (a device stays on the same shard as much as
  possible to maximize warm-cache hit ratio after upgrades).

This document describes the **range-shard + Partition Manager** model that
delivers these properties.

---

## 2. Shard Model

### 2.1 Definition
A **Shard** owns a contiguous range of the device key space:

```
Shard k:  [hashLow_k, hashHigh_k)  in a 64-bit hash space
```

Where `deviceShard(HostID) = shard whose [low, high) contains hash(HostID)`.

The hash function is `xxh64(HostID)`, chosen for speed and uniformity. The
hash space is `[0, 2^64)` and shards partition it without overlap.

### 2.2 ShardSet
A **ShardSet** is the K8s `StatefulSet` of **3 pods** owning one shard:

```
ShardSet shard-0007
├── pod shard-0007-0   (AZ-a)
├── pod shard-0007-1   (AZ-b)
└── pod shard-0007-2   (AZ-c)
```

The 3 pods elect a Primary via K8s `coordination.k8s.io/v1` Lease (see `05`).

### 2.3 Default Sizing
| Knob | Default | Rationale |
|---|---|---|
| Initial devices/shard | 2,000 | Comfort margin under the 5,000 ceiling |
| Splits at | 4,000 sustained devices | Doubles capacity headroom |
| Merges at | 500 sustained devices | Avoids fleet of tiny pods |
| Pods/shard | 3 | 2-of-3 quorum for any operator action |
| Initial shards (10k-device region) | 8 | 10000/2000 + headroom = 8 |
| Max shards/region | 256 | Capacity ceiling per region; new region beyond |

---

## 3. Shard Map (Source of Truth)

The `ShardMap` is a small etcd-backed registry:

```
/shardmap/v1/shards/<ShardID>  →  ShardRecord {
    shard_id:        string
    hash_low:        uint64
    hash_high:       uint64
    state:           ACTIVE | SPLITTING | MERGING | DRAINING | RETIRED
    pod_set:         "shard-0007"        # StatefulSet name
    replicas:        3
    region:          "westus2"
    revision:        int64                # incremented on each ownership change
    parent_shard:    string  (optional)   # for SPLITTING/MERGING
    siblings:        [string]
}
```

```
/shardmap/v1/devices/<HostID>  →  DeviceLocation {
    host_id:    string
    shard_id:   string
    pinned:     bool          # operator override
    last_moved: timestamp
}
```

Both writes are owned **exclusively** by the **Partition Manager (PM)**.
Northbound Gateway and shard pods only *read* this map.

---

## 4. Partition Manager (PM)

The PM is a Kubernetes-style operator: a small controller running as its own
`Deployment` with leader election (1 active PM at a time, 2 standbys).

### 4.1 Responsibilities
1. Maintain the `ShardMap`.
2. Provision and tear down `ShardSet` StatefulSets via the K8s API.
3. Orchestrate **split / merge / drain** workflows.
4. Compute and apply **device assignments** to shards.
5. Expose Prometheus metrics on shard balance and watch latency.

### 4.2 Inputs
- `ShardMap` (read/write).
- ShardSet liveness (via Lease activity + `/healthz`).
- Per-shard observed load metrics (devices count, event rate, p99 program
  latency) scraped from Prometheus.
- Operator-set policies (`/policy/v1/global/partition-policy`).

### 4.3 Outputs
- New/updated `ShardRecord` entries.
- New/updated `DeviceLocation` entries.
- K8s StatefulSet create/delete events.
- Audit log of every ownership change.

### 4.4 Control Loop
```
every 30 s:
  load = scrape per-shard metrics
  if any shard.dev_count > splitThreshold and not splitting:
       schedule SPLIT
  if any pair of adjacent shards both < mergeThreshold:
       schedule MERGE
  if any shard.lease_age > deadThreshold:
       schedule RECOVER
  apply scheduled actions one at a time (serial; safety)
```

The PM is intentionally **conservative and serial** — at most one structural
change per region at a time.

---

## 5. Split Protocol (Hot Shard)

Splitting must move **half a shard's hash range** to a new ShardSet without
losing programming continuity.

```
S0 = shard-0007, range [L, H), pods [p0, p1, p2], Primary p1.
Goal: split into S0' = [L, M) on existing pods, S1 = [M, H) on new pods.
```

### 5.1 Phases

**Phase A — Provision sibling**
- PM creates `ShardSet shard-0007a` (S1) with 3 pods, same config bus.
- S1 pods boot, open etcd watches **only on devices whose HostID ∈ [M, H)**
  — they read this from a `pending` ShardRecord with `state = SPLITTING`.
- S1 builds full in-memory state for those devices from etcd snapshot.
- S1's HAL is in **shadow mode** — actors compute GoalState and would-be
  RPCs but do not actuate. Metric `dashfabric_shadow_rpcs_total` rises.

**Phase B — Sync barrier**
- S0 and S1 each declare "ready" by writing
  `/shardmap/v1/shards/<id>/ready = true`.
- PM verifies both ready, and that S1's in-memory tree matches S0's by
  comparing per-host `goalStateHash` checksums (via a `/diag/checksums` API).

**Phase C — Handoff** (atomic flip)
- PM updates `ShardMap`:
  - `ShardRecord(S0).hash_high = M`
  - `ShardRecord(S1).hash_low = M`, `state = ACTIVE`
  - `ShardRecord(S0).state = ACTIVE`
- PM updates affected `/shardmap/v1/devices/<HostID>` entries.
- Northbound Gateway re-reads ShardMap on next request (and via a watch);
  device steady-state connections are torn down via gRPC GOAWAY and
  re-established to the new ShardSet endpoint.
- S0 closes etcd watches and tears down actors for devices now in S1.
- S1 exits shadow mode; its Primary begins actuating.

**Phase D — Settle**
- 5-minute observation window with metrics.
- If anomalies detected → ROLLBACK (rare; documented in `11`).

### 5.2 Key Properties
- **No double-actuation:** S0 stops actuating *before* S1 starts. The gap
  is bounded by the gRPC GOAWAY round-trip (sub-second). DPUs continue
  steady-state operation during this gap; only fresh intent might be
  delayed by ≤ 1 s.
- **No state loss:** S1 has been replaying upstream for the entire shadow
  phase; the hash-checksum comparison proves convergence.
- **No flow drops:** the data plane is independent of the control plane.

### 5.3 Why Not Use Consistent Hashing With Virtual Nodes
Consistent hashing with virtual nodes is simpler conceptually but moves a
**diffuse** set of devices on any topology change. Range sharding moves a
**contiguous** range, which is easier to orchestrate atomically and easier
to reason about during incidents. The trade-off is that hot keys can
concentrate; we counter this with proactive splits.

---

## 6. Merge Protocol (Cold Shards)

Inverse of split.

```
Adjacent shards S_a [L, M) and S_b [M, H) both under-utilized.
Goal: merge into S_a [L, H).
```

1. PM marks both `state = MERGING`.
2. PM picks the survivor (say S_a). Its pods open etcd watches on S_b's
   range in shadow mode.
3. Once warmed and checksum-verified, atomic ShardMap update flips ownership.
4. Devices that were on S_b reconnect to S_a's NBG endpoint.
5. S_b is decommissioned: PM deletes its StatefulSet, releases its PVCs
   (per retention policy), and removes the ShardRecord.

---

## 7. Failure Recovery

### 7.1 ShardSet Lost (all 3 pods down)
- PM detects via Lease expiry + StatefulSet `Pending` status.
- PM **provisions a fresh ShardSet** with the same `ShardID` and range.
- The fresh ShardSet rebuilds in-memory state from etcd in the usual way.
- During the rebuild (target ≤ 30 s), affected devices' steady-state
  operations are unaffected; only fresh intent is delayed.
- Metric `dashfabric_shard_rebuild_seconds` fires.

### 7.2 Quorum Lost (1 of 3 down)
- PM does nothing structural; K8s StatefulSet replaces the pod.
- Primary election is unaffected (Lease still held by majority survivor).

### 7.3 Quorum Lost (2 of 3 down)
- If the current Primary survives, no change.
- If the Primary was among the 2 lost, the surviving pod takes the lease.
- PM is paged; risk of next failure becoming RECOVER scenario.

---

## 8. Device Assignment Rules

### 8.1 New Device Registration
```
shardId = lookupShardMap(hash(HostID))
```
The hash-range lookup is O(log N) over a sorted slice of `(hashLow, ShardID)`
pairs cached in the NBG; refreshed via etcd watch on `/shardmap/v1/shards/`.

### 8.2 Pinning
Operators can pin a device to a specific shard via `dfctl device pin
<HostID> --shard <ShardID>`. Writes `pinned: true` and prevents future
auto-moves. Used for debugging hard cases.

### 8.3 Anti-Affinity
- 3 pods of a ShardSet land in 3 different AZs via `topologySpreadConstraints`.
- Different ShardSets prefer different node groups via `podAntiAffinity` to
  avoid noisy-neighbor and correlated-failure risks.

### 8.4 Locality
- ShardSets are scheduled in the **same region/AZ** as the etcd cluster they
  watch — this is for latency and to avoid cross-region egress.
- Devices physically located in AZ-a are not pinned to AZ-a; the latency
  from any AZ to a ShardSet's Primary is acceptable (low-ms intra-region).

---

## 9. Northbound Gateway Integration

The NBG must always return the **correct** ShardSet endpoint to a registering
device. Therefore:

1. NBG runs an etcd watch on `/shardmap/v1/shards/` and
   `/shardmap/v1/devices/<HostID>` keys (the latter only when handling that
   device).
2. On a register request:
   ```
   loc = etcd.Get(/shardmap/v1/devices/<HostID>)
   if loc exists and pinned:
        return loc.shard_endpoint
   shardId = ringLookup(hash(HostID))
   atomic upsert /shardmap/v1/devices/<HostID> = { shardId, ... } if absent
   return shardEndpoint(shardId)
   ```
3. During a SPLIT, the NBG sees `state = SPLITTING` on relevant shards and
   **routes new registrations** based on the new ranges already (PM writes
   the new ranges before phase C).

### 9.1 Idempotency
A device may register twice (network retry). The first registration writes
the `DeviceLocation`; the second is a no-op return.

### 9.2 Endpoint Discovery
The "shardEndpoint" for a ShardSet is a stable K8s `Service` name:
`shard-0007.dashfabric.svc.cluster.local:8443`. Devices outside the cluster
use a `LoadBalancer` or `Ingress` exposing per-shard hostnames.

---

## 10. Hot Spots and Operator Levers

| Symptom | Detection | Operator action |
|---|---|---|
| One shard processes 10× the events of others | Prom alert on `event_rate_per_shard` p95/p50 | Trigger SPLIT manually via `dfctl shard split <ShardID>` |
| Specific device generates pathological churn | Prom alert + log signal | `dfctl device throttle <HostID> --max-events-per-sec=50` |
| Tenant generates pathological churn | Per-tenant rate limiter | `dfctl tenant quota <TenantID> set <eventsPerSec>` |
| Cold shards waste resources | Prom alert on `devs_per_shard < 500` for 1 h | `dfctl shard merge <S_a> <S_b>` |

---

## 11. Capacity Math (Worked Example)

**Region**: westus2, 10k DPUs, 32 ENIs each, p99 event rate of 50/sec/DPU
during config storms.

| Quantity | Value |
|---|---|
| Devices | 10,000 |
| ENIs | 320,000 |
| Steady config-event rate | ~5,000 events/sec (regional) |
| Storm event rate | ~500,000 events/sec |
| Shards needed (storm-tolerant) | ⌈500k / 50k per shard⌉ = 10 shards |
| Pods (3 per shard) | 30 pods |
| Memory per pod | ~12 GiB (actors 6 GiB + watch buffers 3 GiB + working set 3 GiB) |
| CPU per pod | 8 cores |
| etcd events/sec sustained | 500k (within etcd's 1M event/sec ceiling on big hardware) |

If we *do* hit etcd's ceiling, we shard etcd by partitioning at the
`/config/v1/hosts/` key range — each etcd cluster handles a sub-range. We
present a single logical `ConfigBus` by composing the watches.

---

## 12. Open Questions

| ID | Question | Default |
|---|---|---|
| OQ-401 | Should shards be resizable independent of pod count? | **No.** 1 shard = 1 ShardSet for simplicity. |
| OQ-402 | Auto-merge enabled by default? | **No.** Auto-split yes; auto-merge requires operator confirmation. |
| OQ-403 | Should PM itself be sharded? | **No** at <1M devices. Single active PM per region with standby. |
| OQ-404 | Cross-region replication of ShardMap? | **No.** Each region is independent; upstream producer publishes per-region. |
