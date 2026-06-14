# FM Pod Lifecycle Design

> **Status:** Draft v1
> **Module:** Fleet Manager (FM)
> **Scope:** Kubernetes StatefulSet pod startup, role acquisition, shard claiming, and recovery
> **Audience:** FM implementers, cluster operators

## 1. Overview

Each FM pod follows a deterministic lifecycle from startup through role acquisition (primary or standby) and into steady-state operation. This document specifies:
- Pod startup and initialization order
- T2 (fm-cluster-state) etcd lease claiming for adapter leadership
- Shard assignment via rendezvous hashing
- T3 (local RocksDB) warm-load strategy for fast recovery
- Registry pre-population from T1 (fm-data-store)
- State readiness transitions

## 2. Pod Lifecycle States

```
[Starting] → [Initializing] → [Claiming] → [Loading] → [Ready:Standby] ⟷ [Active:Primary]
```

| State | Duration | What happens |
|-------|----------|--------------|
| **Starting** | <100ms | Process starts, config loaded, connections initialized |
| **Initializing** | <1s | T1/T2 connectivity verified, RocksDB opened, tables created if needed |
| **Claiming** | <5s | Poll T2 for adapter lease; attempt CAS write to claim it; compute shard assignments |
| **Loading** | 10s-5min | Populate registries from T1; resume watches from T3 cursors; warm RocksDB cache |
| **Ready:Standby** | ∞ | Watches active, fully hydrated, but not programming DPUs. Ready to become primary within 5s. |
| **Active:Primary** | ∞ | Adapter lease held; southbound gRPC active to DPU; programming HDOs/VNETs/ENIs |

## 3. Startup Phase (Starting)

**Entry:** Pod receives `SIGSTART` or Kubernetes liveness probe succeeds.

**Steps:**

1. **Config load:** Read environment and mounted configmap:
   - `FM_POD_NAME` → pod identity (e.g., `fm-0`, `fm-1`, `fm-2`)
   - `FM_POD_ORDINAL` → parse ordinal (0, 1, 2)
   - `T1_ENDPOINT` → etcd T1 cluster URLs
   - `T2_ENDPOINT` → etcd T2 cluster URLs
   - `T3_PATH` → local RocksDB path (e.g., `/var/lib/fm/rocksdb`)
   - `FM_SHARD_COUNT` → total shards in cluster (e.g., 3)
   - `FM_ADAPTER_LEASE_TTL_SECONDS` → adapter lease TTL (default 15s)
   - `FM_ADAPTER_RENEW_INTERVAL_SECONDS` → lease renewal interval (default 5s)

2. **Startup tracing:** Log all config values for audit trail.

3. **gRPC setup:** Bind gRPC server on `0.0.0.0:5051` (not yet accepting subscriptions).

4. **REST API setup:** Bind REST server on `0.0.0.0:8080` (responds with `503 Service Unavailable` until Ready).

5. **Transition:** → **Initializing**

**Duration SLA:** <100ms from process start to Initializing state.

## 4. Initialization Phase (Initializing)

**Entry:** Config loaded; servers bound.

**Steps:**

1. **T1 connectivity:** Attempt dial T1 etcd cluster with exponential backoff (3 tries, 1s timeout each).
   - If fails: log fatal error and exit (pod will be restarted by Kubernetes).
   - Success: log `"T1 connected at endpoint=<ip:port>"`.

2. **T2 connectivity:** Attempt dial T2 etcd cluster with exponential backoff (3 tries, 1s timeout each).
   - If fails: same fatal exit.
   - Success: log `"T2 connected"`.

3. **RocksDB init:**
   - Open local RocksDB at `FM_T3_PATH/pod-{FM_POD_ORDINAL}.db`.
   - Create column families if missing: `compact_topics`, `append_topics`, `watermarks`, `registries`.
   - Set up incremental backup cursors for T1 watch resume (stored in `watermarks` CF).
   - Success: log `"T3 RocksDB opened, size_bytes=<X>"`.

4. **Health probe ready:** Respond `200 OK` to `/healthz` (readiness check); `/livez` still responds.

5. **Transition:** → **Claiming**

**Duration SLA:** <1s from Initializing start to Claiming state.

## 5. Claiming Phase (Claiming) — Adapter Lease & Shard Assignment

**Entry:** T1/T2/T3 connected; RocksDB open.

**Steps:**

### 5.1 Adapter Lease Claiming

1. **Compute lease key:** `{T2_PREFIX}/adapter/lease` (all pods use same key).

2. **Read current lease:** Fetch lease from T2 etcd with key `adapter/lease`.
   - If no lease exists: lease is free; proceed to CAS claim.
   - If lease exists and not expired (`now < lease.expiry`): pod is standby; skip to 5.3.
   - If lease expired: stale lease; attempt to claim.

3. **CAS claim attempt:**
   ```
   New lease value: {
     holder_pod_name: FM_POD_NAME,
     holder_ordinal: FM_POD_ORDINAL,
     claimed_at_unix_ms: now(),
     expires_at_unix_ms: now() + FM_ADAPTER_LEASE_TTL_SECONDS * 1000,
     version: 0,
   }
   CAS condition: lease.version == previous_version (or not exists)
   ```
   - If CAS succeeds: this pod is **primary** candidate (adapter leader).
     - Log: `"Adapter lease claimed. holder=<FM_POD_NAME>"`.
     - Start background heartbeat goroutine (see 5.2).
     - Go to 5.3 (compute shard assignments).
   - If CAS fails (someone else claimed): this pod is **standby**.
     - Log: `"Adapter lease held by <holder_pod_name>, entering standby"`.
     - Go to 5.3 (compute same shards).

### 5.2 Adapter Lease Renewal (Background)

**Only if this pod holds the lease:**

- Every `FM_ADAPTER_RENEW_INTERVAL_SECONDS` (default 5s):
  - Read current lease from T2.
  - Increment `version`, update `expires_at_unix_ms = now + TTL`.
  - CAS write to T2 with condition `lease.version == current_version`.
  - If CAS succeeds: renewal OK; continue.
  - If CAS fails: **lease lost** (another pod claimed it).
    - Log warning: `"Adapter lease lost; transitioning to standby"`.
    - Cancel southbound gRPC adapter (if active).
    - Transition this pod to **Ready:Standby** state.
    - Stop programming DPUs until lease is reclaimed.

**Renewal failure handling:**
- If T2 is unavailable (network partition): after 3 consecutive failed renewals, assume split-brain risk.
  - Log error: `"T2 unreachable for <time>; stopping DPU programming"`.
  - Cease southbound gRPC adapter to DPUs (safe-fail).
  - Retry reconnect every 5s; if T2 reconnects and lease is still held, resume programming.

### 5.3 Shard Assignment (All Pods)

All pods compute the same shard assignment deterministically using rendezvous hashing:

1. **Collect active pods:** Query T2 etcd for all active pod records under `/dashfabric/cluster/pods/{pod_name}`.
   - Expect exactly 3 pods (or configured `FM_SHARD_COUNT`).
   - If fewer: incomplete cluster; log warning but proceed (standby pods will dedupe work anyway).
   - If more: cluster growing; use consistent hash to decide which shards each pod owns.

2. **Build hash ring:** For each shard ID 0..`FM_SHARD_COUNT - 1`:
   - Compute `rendezvous_hash(shard_id, [pod_0_name, pod_1_name, ..., pod_N_name])`.
   - Assign shard to the pod with highest hash value.
   - All pods compute the **same assignments** (deterministic).

3. **Record local shards:** Store shard assignment list in RocksDB `metadata` CF (not critical; for observability).

4. **Log assignments:**
   ```
   "Shard assignments: shards=[0,1,2], pod=fm-0; shards=[], pod=fm-1; shards=[], pod=fm-2"
   ```
   (example: only pod fm-0 owns all 3 shards if FM_SHARD_COUNT=3)

5. **Transition:** → **Loading**

**Duration SLA:** <5s from Claiming start to Loading state.

## 6. Loading Phase (Loading) — Registry Hydration & T3 Warmup

**Entry:** Adapter lease status determined; shard assignments computed.

**Steps:**

### 6.1 T1 Cursor Resume (Append-log recovery)

1. **Read watermark from T3:** Query RocksDB `watermarks` CF for keys like `t1/watch/{topic_pattern}/cursor`.
   - If cursor exists: resume from that watermark.
   - If no cursor: start from `watermark_begin` (full resync).

2. **Establish T1 watch streams:** For each topic pattern in registry subscriptions:
   - `/dashfabric/v1/config/vnets/**`
   - `/dashfabric/v1/config/mappings/**`
   - `/dashfabric/v1/config/routes/**`
   - `/dashfabric/v1/config/groups/**`
   - `/dashfabric/v1/config/ha/**`

   Open watch stream with `fromWatermark = cursor` (or begin if no cursor).
   - If watch fails: wait 5s, retry (exponential backoff up to 60s).
   - Events are buffered in per-topic channel (default 1024 events).

3. **Initial snapshot scan:** If resuming from cursor (warm start):
   - T1 provides events from cursor forward.
   - Events populate registries incrementally as they arrive.
   - Expect catch-up in <10s for typical shard.

   If no cursor (cold start):
   - Call `Resync(topic_pattern)` on CB (or T1 etcd for direct access).
   - Receive full snapshot of all config objects.
   - This is slower (~1-5 min for 100k ENIs) but happens on first pod start only.

### 6.2 Registry Population

As T1 events arrive (snapshot or live stream):

1. **VnetRegistry:** Insert all VnetConfig objects.
   - Key: vnet_id
   - Subscribers (ENIs) auto-populated as ENI hydration progresses.

2. **NicRegistry:** Insert all ENI objects.
   - Key: eni_id
   - Store ENI goal-state from T1.

3. **VnetMappingRegistry:** Insert all VnetMapping (CIDR → vnet routing) objects.
   - Pre-hydrate peer VNETs' mappings during this phase (hybrid peering model).

4. **GroupRegistry:** Insert all Group (DPU group / appliance group) objects.

5. **HaRegistry:** Insert all HA failover config objects.

6. **Progress tracking:** Each registry records a high-water mark (latest event processed).
   - Stored in RocksDB `watermarks` CF.
   - Used to resume if pod restarts mid-Loading.

### 6.3 RocksDB Cache Sync

1. **Populate T3 cache:** For each registry, write compacted view to RocksDB:
   - `compact_topics` CF stores latest state per (topic, key).
   - Example: `vnets/vnet-1234 → VnetConfig{...}` (serialized as protobuf).

2. **Durability check:** Fsync RocksDB to disk.
   - RocksDB is local-only, not replicated.
   - Durability goal: survive single pod restart; loss OK on node failure (resync from T1).

### 6.4 Watch Resume Cursor Storage

1. **Store T1 watermark:** After Loading completes, record the final watermark in RocksDB.
   - Key: `t1/watch/topics/cursor`
   - Value: last watermark received
   - Fsync to disk.

2. **Next pod restart:** Will resume from this watermark, skipping full resync.

**Duration SLA:** 
- Warm start (with cursor): 10s–30s (catch-up time).
- Cold start (no cursor): 1–5 min (depends on shard size).

## 7. Ready States

### 7.1 Ready:Standby State

**Entry:** Loading complete; registries hydrated; T1 watches live.

**Exit conditions:**
- Adapter lease is reclaimed (CAS succeeds) → transition to **Active:Primary**.
- Pod receives SIGTERM → graceful shutdown.

**Behavior:**
- T1 watches remain active; registries updated live.
- gRPC Subscribe streams accept connections but do **not** program DPUs.
- REST API responds `200 OK` to all requests (read-only status).
- RocksDB cache updated for each T1 event (keeps T3 fresh).
- Southbound gRPC adapter is idle (no open connections to DPUs).
- Ready to become primary within **<5 seconds** (adapter lease renewal period).

**Observability:**
- Pod status: `Ready`, `Mode: Standby`.
- Metrics: `fm_pod_role{pod,role="standby"}`, `fm_registries_size{registry,pod}`.
- No ENI programming events.

### 7.2 Active:Primary State

**Entry:** Adapter lease claimed; **only** if lease renewal succeeds continuously.

**Exit conditions:**
- Adapter lease renewal fails (CAS fails) → transition to **Ready:Standby**.
- Pod receives SIGTERM → graceful shutdown.
- T1 or T2 becomes unreachable (see 5.2) → cease DPU programming; downgrade to standby.

**Behavior:**
- **Southbound gRPC adapter active:** Open persistent gRPC connection to assigned DPUs.
  - gNMI-style telemetry stream for ENI updates.
  - Program ENI goal-state using DASH standard protos.
  - Receive ACKs and apply state updates.
- **Continuous lease renewal:** Every 5s, renew lease in T2.
  - If renewal fails: immediately cease DPU programming and revert to **Ready:Standby**.
- **Registry-driven programming:** When registries change (via T1 watches), propagate changes to DPUs.
  - Actor model: HostDeviceActor (HDO), ContainerActor (CO), NicActor (NO).
  - Wave-ordered dataplane programming (waves 0–6).
- **Publish ACKs:** As DPUs program ENIs, primary publishes state acks to T1 `…/ack/state` topics.
  - Consumed by standbys for observability (but not actuation).

**Observability:**
- Pod status: `Ready`, `Mode: Primary`.
- Metrics: `fm_pod_role{pod,role="primary"}`, `fm_enis_programmed{pod}`, `fm_dpu_latency_ms{pod}`.
- Trace: gRPC spans for DPU programming; ETW events for ENI state transitions.

## 8. Failover Scenario: Primary Pod Crash

**Scenario:** Primary pod (holding adapter lease) crashes or becomes unresponsive.

**Timeline:**

| Time | Event | Actor | Action |
|------|-------|-------|--------|
| T=0s | Primary pod crashes | OS | Pod process exits; kernel cleans up connections |
| T=0s | Southbound gRPC to DPUs breaks | gRPC runtime | gNMI stream disconnects; ENI updates stop |
| T=+1s | Standbys detect no lease renewal | T2 watcher | Lease record remains but `now > expires_at_unix_ms` |
| T=+5s | Standbys attempt lease CAS claim | Standby pod 1, 2 | Race: one succeeds, one fails; winner becomes new primary |
| T=+5s | New primary elected | New primary | Adapter lease version incremented; claimed_at updated |
| T=+5s | New primary opens southbound gRPC | New primary | Connects to DPUs; resumes ENI programming |
| T=+6s | DPUs resume receiving updates | DPUs | New primary sends pending ENI deltas |

**RTO (Recovery Time Objective):** <5s (duration of adapter lease TTL).

**Traffic impact:** During T=0s to T=+5s, ENI updates are blocked (no programming). After T=+5s, new primary catches up and resumes.

## 9. Graceful Shutdown

**Trigger:** Pod receives `SIGTERM` (e.g., cluster scale-down, node maintenance).

**Steps:**

1. **Stop accepting new requests:** gRPC and REST servers stop listening.
   - In-flight requests are allowed to complete (timeout 30s).

2. **Release adapter lease (if held):**
   - Delete lease record from T2 etcd.
   - Log: `"Adapter lease released"`.

3. **Close southbound gRPC adapter:**
   - Send graceful disconnect to DPUs.
   - Wait up to 5s for clean close; force-close after.

4. **Close T1/T2 watches:** Orderly close of etcd watch streams.

5. **Flush RocksDB:** Compact and close.

6. **Exit:** Process exits with code 0.

**Duration SLA:** Graceful shutdown completes in <30s.

**Emergency shutdown (no graceful period):**
- Pod killed via `kill -9` or node failure.
- Next pod startup re-claims resources; no orphaned state.

## 10. Edge Cases

### 10.1 Split-brain Prevention

**Scenario:** Network partition; primary pod isolated but not crashed.

**Mitigation:**
- Primary must renew lease every `FM_ADAPTER_RENEW_INTERVAL_SECONDS` (5s).
- Lease TTL is `FM_ADAPTER_LEASE_TTL_SECONDS` (15s).
- If primary is partitioned from T2 etcd, lease renewal fails; primary detects this and stops DPU programming.
- Standbys can see expired lease and claim it; only one can win CAS.
- Result: primary-side and standby-side are now split; primary stops programming (safe-fail); standby takes over.

**RTO:** If primary detects partition immediately: <1s. If delayed: up to 15s.

### 10.2 Cluster Expansion (Add pod FM-3)

**Scenario:** K8s scales FM from 3 to 4 pods.

**Process:**
- FM-3 starts; goes through Startup → Initializing → Claiming.
- During Claiming: rendezvous hash recomputed for 4 pods.
- Shard distribution changes: each pod now owns 1/4 of devices (rendezvous ensures only 1/4 of keyspace shifts).
- Only newly-assigned ENIs are programmed by new pod; existing ENIs remain on original pods.
- Standbys also recompute shards and adjust watchers (no role change; still standbys).

### 10.3 Cluster Shrinkage (Remove pod FM-2)

**Scenario:** K8s scales FM from 3 to 2 pods.

**Process:**
- FM-2 receives SIGTERM; releases lease and closes.
- FM-0 and FM-1 detect FM-2 missing from pod list (T2 heartbeat timeout).
- Rendezvous hash recomputed for 2 pods.
- ENIs previously owned by FM-2 now assigned to FM-0 or FM-1.
- If FM-2 was primary: lease expires, new primary elected among FM-0/FM-1.

### 10.4 RocksDB Corruption

**Scenario:** Local T3 RocksDB corrupted (filesystem failure).

**Mitigation:**
- On RocksDB open, check integrity (RocksDB `CheckConsistency()` API).
- If corrupted: log error, delete RocksDB, restart.
- Next startup: Loading phase performs cold-start (full resync from T1).
- Expected recovery time: 1–5 min (for large shard).

### 10.5 T1 or T2 Unavailable During Loading

**Scenario:** T1 etcd cluster is partitioned during Loading.

**Behavior:**
- Pod startup fails during Initializing phase (T1 connectivity check).
- Kubernetes restarts pod (liveness probe times out).
- Pod retries; if T1 recovers, succeeds.
- If T1 remains down: pod stuck in CrashLoopBackOff; alerts fire.

**Recovery:** Restore T1 etcd or scale to new T1 cluster; pods restart automatically.

## 11. Observability

### 11.1 Logs

```
[INFO] Pod startup: pod_name=fm-0, ordinal=0, shard_count=3
[INFO] Initializing: T1 connected, T2 connected, T3 opened
[INFO] Claiming: Adapter lease claimed. holder=fm-0
[INFO] Loading: Registries hydrating from T1 (warm start, resume from watermark=etcd:12345)
[INFO] Ready: mode=primary, adapters_active=1, registries_size={vnets:42, enis:5000, mappings:200}
[INFO] Pod graceful shutdown: releasing adapter lease
```

### 11.2 Metrics

- `fm_pod_role{pod, role}` → gauge (0=standby, 1=primary)
- `fm_pod_state{pod, state}` → gauge (0=starting, 1=initializing, 2=claiming, 3=loading, 4=ready)
- `fm_registries_size{pod, registry}` → gauge (vnets, enis, mappings, groups, ha)
- `fm_adapter_lease_ttl_seconds{pod}` → gauge (remaining TTL if held by this pod)
- `fm_t3_size_bytes{pod}` → gauge (RocksDB disk usage)
- `fm_t1_watch_lag_seconds{pod, topic}` → gauge (distance behind live T1 writes)

### 11.3 Traces

- Span: `pod.startup` (root)
  - Child: `t1.connect`
  - Child: `t2.connect`
  - Child: `t3.open`
  - Child: `adapter.claim_lease`
  - Child: `registries.load`

## 12. Configuration Knobs

| Knob | Default | Purpose |
|------|---------|---------|
| `FM_ADAPTER_LEASE_TTL_SECONDS` | `15` | Adapter lease time-to-live (failover window) |
| `FM_ADAPTER_RENEW_INTERVAL_SECONDS` | `5` | How often primary renews lease |
| `FM_SHARD_COUNT` | `3` | Number of shards in cluster |
| `FM_T3_PATH` | `/var/lib/fm/rocksdb` | Local RocksDB directory |
| `FM_STARTUP_TIMEOUT_SECONDS` | `60` | Max time to reach Ready state |
| `FM_GRACEFUL_SHUTDOWN_TIMEOUT_SECONDS` | `30` | Max time for graceful shutdown |
| `FM_POD_DISCOVERY_INTERVAL_SECONDS` | `10` | How often to refresh pod list from T2 |

## 13. References

- `storage-architecture.md` — T1/T2/T3 detailed design
- `recovery-and-failover-design.md` — comprehensive failure scenarios
- `registry-pattern-design.md` — in-pod registry contract
- `deployment-tiers.md` — tier-specific configurations
- `02-cb-low-level-design-lld.md` — ControllerBridge (T1 source)
