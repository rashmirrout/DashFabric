# FM Failover Sequence Design

> **Status:** Draft v1
> **Module:** Fleet Manager (FM)
> **Scenario:** Primary pod failure and recovery via 3-replica standby takeover
> **Audience:** On-call operators, FM developers, debugging specialists

## 1. Overview

This document details the **exact sequence of events and timing** when FM's primary pod fails. It covers:
- What happens when primary pod crashes or becomes unresponsive
- How standbys detect the failure
- How a standby becomes the new primary
- Traffic impact and recovery window
- Detailed state transitions with timestamps
- Observability signals for each step
- Debugging guidance for failure scenarios

## 2. Failure Scenarios Covered

| Scenario | Primary Pod State | Cause | RTO | Notes |
|----------|-------------------|-------|-----|-------|
| **Process crash** | Dead; exit code != 0 | Panic, OOM, SIGKILL | <5s | Most common; clean failover |
| **Node failure** | Dead; no gRPC, no heartbeat | VM/host crash, network partition | <5s | Same as process crash |
| **Hung process** | Alive but unresponsive | Deadlock, high CPU, GC pause | <15s | T2 lease renewal fails |
| **Network partition** | Alive but isolated from T2 | Split-brain; primary sees DPUs, standbys see each other | <1s (detect) + <5s (failover) | Primary self-stops; standby takes over |
| **Graceful shutdown** | Intentional exit | K8s rolling update, scale-down | <5s | Primary releases lease before exit |

## 3. Nominal Failover: Primary Process Crash

### 3.1 T=0s: Primary Pod Alive

**State:**
- Primary pod (e.g., `fm-0`) holds adapter lease in T2 etcd.
  - Lease key: `/dashfabric/cluster/adapter/lease`
  - Lease holder: `fm-0`
  - Expires at: T=15s (TTL=15s)
  - Last renewal: T=-5s (renewal interval=5s)
- Primary actively programs DPUs via gNMI gRPC stream.
- Standbys (`fm-1`, `fm-2`) watch T1 topics; do not program DPUs.

**Metrics:**
- `fm_pod_role{pod="fm-0"} = 1` (primary)
- `fm_pod_role{pod="fm-1"} = 0` (standby)
- `fm_pod_role{pod="fm-2"} = 0` (standby)

**Logs:**
```
[INFO] fm-0: Adapter lease renewed. expires_at=T+15s, holder=fm-0
[INFO] fm-0: DPU gNMI stream active. eni_id=eni-1000, programmed=true
```

### 3.2 T=0s+0ms: Crash Event

**Event:** Primary pod process crashes (e.g., panic, OOM, SIGKILL).

**Immediate effects:**
1. **Process exit:** Exit code != 0; goroutines killed; memory freed.
2. **Kubernetes detection:** Liveness probe fails (5s grace before probe timeout).
3. **gNMI stream breaks:** Open connections to DPUs close abruptly.
   - DPUs see TCP RST from FM; stop sending telemetry.
   - DPUs do NOT close active ENI rules (safe: rules remain active).

**Logs (from primary, before death):**
```
[FATAL] fm-0: Panic: runtime error: index out of range [123] with length 100
runtime.panic()
[FATAL] fm-0: Stack trace: <truncated>
```

**Pod status (from Kubernetes):**
- CrashLoopBackOff (after 3+ crash restarts)
- Or: Pending (if node is dead)

### 3.3 T=+1s: Standbys Detect Crash (Implicit)

**What happens:**
- Standbys do **not** explicitly monitor primary pod heartbeat.
- Standbys monitor T2 lease record (adapter lease in etcd).
- Lease is still valid (`now < expires_at`).
- **No action yet.**

**Why no immediate action:**
- T2 lease TTL = 15s; plenty of time remains.
- Standbys might be partitioned from primary but connected to T2; ambiguous state.
- Waiting for TTL expiry prevents false-positive failover.

**Logs (standbys):**
```
[INFO] fm-1: T1 watch active. topics_in_flight=42, registries_size={vnets:100, enis:5000}
[INFO] fm-2: T1 watch active. topics_in_flight=42, registries_size={vnets:100, enis:5000}
```

### 3.4 T=+5s: Primary Should Renew Lease (But Can't)

**What should happen:**
- Primary is scheduled to renew adapter lease every 5s.
- T=5s: renewal attempt #1 (normally succeeds at T=0s, T=5s, T=10s, T=15s, ...).

**What actually happens:**
- Primary process is dead; no renewal happens.
- T2 etcd still holds stale lease (not expired yet; TTL clock still running).
- Lease record:
  ```
  /dashfabric/cluster/adapter/lease = {
    holder: "fm-0",
    expires_at: T+15s,  // Still 10s remaining
    version: 42
  }
  ```

**Impact:**
- **Southbound gRPC to DPUs is broken.**
  - DPU connections are dead (TCP reset).
  - No new ENI updates flowing.
  - Existing ENI rules remain active (safe).
- **No new registrations accepted** (primary was single writer).
  - Or: if request hits standby, standby writes to T1 (all replicas can register devices).

**Logs (none from primary; dead process produces no logs).**

### 3.5 T=+10s: Kubernetes Restarts Crashed Pod

**Kubernetes actions:**
1. **Liveness probe failed** (at ~T=+5s, but grace period extends to ~T=+10s).
2. **Pod marked for restart:** Kubelet sends SIGTERM to pod container.
   - No effect (process already dead).
   - After grace period (default 30s), Kubelet kills container.
3. **New pod container starts** (same `fm-0` pod, new PID).
   - Pod enters **Starting** state (see fm-pod-lifecycle-design.md).
   - Initializes T1, T2, RocksDB connections.
   - Attempts to claim adapter lease (will fail; lease still held by old instance).
   - Enters **Ready:Standby** state.

**Logs (new pod instance):**
```
[INFO] fm-0: Pod startup: pod_name=fm-0, ordinal=0, shard_count=3
[INFO] fm-0: Initializing: T1 connected, T2 connected, T3 opened
[INFO] fm-0: Claiming: Adapter lease held by fm-0 (already claimed), entering standby
[INFO] fm-0: Loading: Registries hydrating from T1
[INFO] fm-0: Ready: mode=standby, registries_size={vnets:100, enis:5000}
```

**Metrics:**
- `fm_pod_role{pod="fm-0"} = 0` (standby, due to stale lease from old instance)
- Note: This is confusing; K8s operator may see old metrics until they flush.

### 3.6 T=+15s: Adapter Lease Expires

**Event:** T2 etcd clock reaches `expires_at = T+15s`.

**T2 etcd behavior:**
- Lease record is now stale (expired).
- Any subsequent read of lease will see it as expired.
- But the record is NOT automatically deleted from etcd (etcd doesn't garbage-collect leases).
- Standbys now see: `now > expires_at`; lease is available for claiming.

**Logs (from standbys, first detection):**
```
[INFO] fm-1: Adapter lease check: holder=fm-0, expires_at=T+15s, now=T+15.050s, status=EXPIRED
[INFO] fm-1: Adapter lease expired. Attempting CAS claim...
[INFO] fm-2: Adapter lease check: holder=fm-0, expires_at=T+15s, now=T+15.051s, status=EXPIRED
[INFO] fm-2: Adapter lease expired. Attempting CAS claim...
```

### 3.7 T=+15s+50ms: CAS Claim Race (Standbys)

**Event:** Both `fm-1` and `fm-2` attempt to claim the expired lease (roughly simultaneously).

**CAS write attempt #1 (from fm-1):**
```
Key: /dashfabric/cluster/adapter/lease
New value: {
  holder: "fm-1",
  expires_at: T+30s,
  version: 43,
  claimed_at: T+15.040s
}
CAS condition: version == 42 (old lease version)
```

**CAS write attempt #2 (from fm-2):**
```
Key: /dashfabric/cluster/adapter/lease
New value: {
  holder: "fm-2",
  expires_at: T+30s,
  version: 43,
  claimed_at: T+15.042s
}
CAS condition: version == 42 (old lease version)
```

**T2 etcd processing (serial):**
- CAS #1 (from fm-1, arrives ~50ms earlier): Read current lease (version=42). Condition matches. Write succeeds. Lease version bumped to 43; holder changed to fm-1.
- CAS #2 (from fm-2, arrives ~100ms after #1): Read current lease (version=43, holder=fm-1). Condition requires version=42. Mismatch. Write fails.

**Results:**
- **fm-1 wins:** CAS succeeds. Lease now held by fm-1. Becomes **new primary**.
- **fm-2 loses:** CAS fails. Remains **standby**.

**Logs:**
```
[fm-1]
[INFO] Adapter lease claimed. holder=fm-1, version=43
[INFO] Starting adapter lease renewal (every 5s)

[fm-2]
[WARN] Adapter lease CAS failed. Already held by fm-1. Remaining standby.
[INFO] fm-2: Adapter lease check: holder=fm-1, expires_at=T+30s, status=ACTIVE
```

### 3.8 T=+15s+50ms to +15s+500ms: New Primary Activation

**Event:** FM-1 now holds adapter lease; must activate southbound gNMI stream to DPUs.

**Steps on fm-1:**

1. **Open gNMI connection to DPUs:**
   - Iterate through assigned DPUs (all DPUs previously served by fm-0).
   - For each DPU, open gRPC connection (gNMI dialout).
   - DPU initiates connection (typical for gNMI telemetry; FM listens).
   - Establish bidirectional gNMI stream.

2. **Resync from last programmed state:**
   - Read T1 `…/ack/state` topics for each ENI (consumed by FM).
   - Extract `last_known_version`, `ref_count`, `consumer_list`.
   - Query T3 RocksDB for ENI goal-state.
   - Compare: goal-state vs. last-acked-state.
   - If diverged: re-send ENI updates to DPU (idempotent via content-hash).

3. **Resume wave-ordered programming:**
   - ENIs waiting in waves 1-6 are resumed.
   - Waves 0 (base plumbing) already applied to DPUs (from old primary).
   - Send wave 1+ deltas to DPU.

4. **Start lease renewal loop:**
   - Every 5s, renew adapter lease in T2 etcd.
   - If renewal fails: immediately stop southbound gNMI; revert to standby.

**Duration:** <200ms (gRPC connect + state resync).

**Logs (fm-1):**
```
[INFO] fm-1: Adapter lease acquired. Starting primary activation.
[INFO] fm-1: Opening gNMI to DPU group-0 (dpus=[dpu-1000, dpu-1001, dpu-1002])
[INFO] fm-1: gNMI stream established to dpu-1000. Resync from watermark=etcd:12345
[INFO] fm-1: ENI eni-1000 resuming wave 2. goal_state={...}, last_acked_state={...}
[INFO] fm-1: Primary activation complete. DPU group-0 active.
[INFO] fm-1: Role transition: standby → primary. 42 DPUs resuming.
```

### 3.9 T=+15s+500ms: New Primary Fully Active

**Event:** FM-1 is now the operational primary. Traffic flows through FM-1.

**State:**
- **fm-1 (new primary):**
  - Holds adapter lease in T2.
  - gNMI streams active to all DPUs.
  - Accepting registration requests (REST API).
  - Publishing ENI acks to T1.
  - Role: `Active:Primary`.

- **fm-2 (standby):**
  - Watching T1 topics.
  - Sees new acks published by fm-1.
  - Remains ready to take over if fm-1 fails.
  - Role: `Ready:Standby`.

- **fm-0 (new restart attempt, standby):**
  - Just finished Loading phase.
  - Sees lease held by fm-1.
  - Ready to take over if fm-1 fails.
  - Role: `Ready:Standby`.

**Metrics:**
- `fm_pod_role{pod="fm-1"} = 1` (primary)
- `fm_pod_role{pod="fm-0"} = 0` (standby)
- `fm_pod_role{pod="fm-2"} = 0` (standby)
- `fm_pod_uptime_seconds{pod="fm-1"} = 15` (restarted at T=10s; now T=25s)

**Logs:**
```
[fm-0]
[INFO] fm-0: Ready: mode=standby, registries_fully_hydrated

[fm-1]
[INFO] fm-1: Primary mode active. DPUs responding. eni_programmed=5000

[fm-2]
[INFO] fm-2: Standby mode active. T1 watches live. Ready to failover in <5s
```

### 3.10 T=+20s: Steady-State (Normal Operation Resumes)

**Event:** Failover complete; cluster stabilized.

**State:**
- **fm-1:** Primary; actively programming ENIs; renewing lease every 5s.
- **fm-0, fm-2:** Standby; watching T1; ready to failover.

**Metrics stabilize:**
- `fm_dpu_latency_p50_ms{pod="fm-1"} ≈ 8` (new primary catching up on backlog)
- `fm_dpu_latency_p99_ms{pod="fm-1"} ≈ 150` (some delayed acks during resync)
- `fm_eni_wave_distribution{wave}` shows waves 2-6 completing.

**Client impact (if any):**
- Clients connected to fm-0 REST API see `503 Service Unavailable` (no longer primary).
- Clients connected to fm-1 REST API see `200 OK` (primary is responding).
- Clients using load balancer: automatically routed to fm-1 (alive replica).

**Traffic recovery window:** T+15s to T+15.5s (500ms outage for ENI programming).

## 4. Alternative Scenario: Network Partition (Primary Isolated)

### 4.1 Scenario Setup

**Event:** Primary pod (fm-0) is alive but network-partitioned from T2 etcd cluster.

**Initial state:**
- fm-0 holds adapter lease in T2.
- fm-0 sees DPUs (gNMI stream works; different network link).
- fm-0 cannot reach T2 etcd (can reach T1 and T3; different network links).

### 4.2 T=+0s to +5s: Primary Detects Partition (Renewal Fails)

**Step 1:** Primary's lease renewal goroutine fires (every 5s).

**Step 2:** Attempt CAS write to T2 etcd (hold lease; update expiry).
- **T2 connection times out** (5s timeout).
- **Renewal fails.**

**Step 3:** Retry logic (3 attempts, 1s apart):
```
Attempt 1: timeout
Attempt 2: timeout
Attempt 3: timeout
```

**Step 4:** Primary detects partition.

**Logs (primary, fm-0):**
```
[WARN] fm-0: Adapter lease renewal failed (attempt 1/3). T2 unreachable.
[WARN] fm-0: Adapter lease renewal failed (attempt 2/3). T2 unreachable.
[WARN] fm-0: Adapter lease renewal failed (attempt 3/3). T2 unreachable.
[ERROR] fm-0: T2 etcd unreachable for 5s. Assuming split-brain. Stopping DPU programming.
```

### 4.3 T=+5s: Primary Safe-Fails (Stops Programming)

**Action:** Primary closes gNMI streams to DPUs.

**Logs (primary):**
```
[WARN] fm-0: Closing all gNMI streams (safe-fail on T2 partition).
[INFO] fm-0: gNMI stream closed to dpu-1000
[INFO] fm-0: gNMI stream closed to dpu-1001
[INFO] fm-0: gNMI stream closed to dpu-1002
[INFO] fm-0: Role transition: primary → standby. Reason: T2 unreachable.
```

**State:**
- **fm-0 (partitioned):** Role downgraded to standby; DPU programming stopped (safe).
- **fm-1, fm-2 (connected to T2):** Lease held by fm-0 but not renewed (T2 still has stale lease from fm-0).

### 4.4 T=+10s: Standbys Detect Expired Lease

**Event:** T2 etcd lease expires (TTL=15s from T=0s; now expired at T=15s).

**Processing (fm-1, fm-2):**
```
[INFO] fm-1: Adapter lease check: holder=fm-0, expires_at=T+15s, now=T+10s, status=VALID
...
(wait for T=+15s)
```

Actually, let me revise: T2 lease was renewed at T=0s with TTL=15s. Primary dies at T=0s (immediately after renewing). Renewal was NOT made at T=5s. So lease expires at T=15s.

At T=10s, lease is still valid (5s remaining).
At T=15s, lease expires.

**See section 3.6 onwards:** Same as nominal failover (process crash scenario).

### 4.5 T=+15s: Standbys Claim Lease; New Primary Activated

**Result:** One of fm-1 or fm-2 wins CAS race; becomes new primary.

**Traffic impact:** 15s partition detection + failover window. But from the moment standby detects partition (not possible; standbys don't directly detect primary partition), failover is clean.

**Key insight:** FM relies on T2 lease expiry to detect primary failure (whether crash or partition). No explicit heartbeat channel. This is safe-fail but slower (~15s window).

## 5. Timeline Summary

| Phase | Duration | Key Events |
|-------|----------|-----------|
| **Crash to detection** | 0–15s | Primary crashes; no lease renewal; T2 lease counts down. |
| **Detection to failover** | 15s–15.1s | Standbys see expired lease; CAS race; one wins. |
| **Failover to activation** | 15.1s–15.6s | New primary opens gNMI streams; resync state. |
| **Steady-state** | 15.6s+ | Normal operation resumes. |
| **Total RTO** | ~15.5s | From crash to full recovery. |
| **Traffic impact** | ~0.5s | ENI updates blocked during gNMI resync (T+15s to T+15.5s). |

## 6. Detailed State Transitions

```
PRIMARY (fm-0)
  ├─ T=0s: Process crash (SIGKILL, panic, OOM)
  │  └─ State: Dead; no lease renewal
  ├─ T=1s: Kubernetes detects crash (liveness probe fails)
  ├─ T=10s: Kubernetes restarts pod (new process)
  │  └─ New fm-0 enters Standby (lease still held by old instance)
  ├─ T=15s: Old instance's lease expires
  ├─ T=15.05s: fm-1 wins CAS; becomes new primary
  ├─ T=15.5s: New primary fully active

STANDBY-1 (fm-1)
  ├─ T=0s: Watching T1 topics; standby mode
  ├─ T=15s: Detects expired lease; CAS race starts
  ├─ T=15.05s: CAS succeeds; acquires lease
  │  └─ State transition: Standby → Claiming → Loading → Active:Primary
  ├─ T=15.1s: Opens gNMI to DPUs
  ├─ T=15.5s: Primary fully active

STANDBY-2 (fm-2)
  ├─ T=0s: Watching T1 topics; standby mode
  ├─ T=15s: Detects expired lease; CAS race starts
  ├─ T=15.05s: CAS fails; remains standby
  │  └─ State: Ready:Standby
  ├─ T=15.5s: Steady-state
```

## 7. Observability During Failover

### 7.1 Logs to Monitor

**Primary crash detection (standbys, at T=15s):**
```
[INFO] fm-1: Adapter lease check: holder=fm-0, expires_at=1718400015000, now=1718400015050, status=EXPIRED
[INFO] fm-1: Attempting to claim adapter lease...
[WARN] fm-2: Adapter lease CAS failed. Already held by fm-1.
```

**New primary activation (fm-1, at T=15.05s):**
```
[INFO] fm-1: Adapter lease claimed. version=43, holder=fm-1
[INFO] fm-1: Opening gNMI to DPU group-0
[INFO] fm-1: ENI eni-1000 resuming wave 2. goal_state_watermark=etcd:12345
[INFO] fm-1: Primary activation complete. 42 DPUs active.
```

**Old primary restart (fm-0, at T=10s):**
```
[INFO] fm-0: Pod restart: Initializing FM pod
[INFO] fm-0: Claiming: Adapter lease held by fm-0 (another instance), entering standby
[INFO] fm-0: Loading: Registries hydrating from T1 (warm start, cursor=etcd:12400)
```

### 7.2 Metrics to Alert On

**Alert 1: Primary failover in progress**
```
Condition: fm_pod_role change from 1→0 on any pod (except graceful shutdown)
Severity: CRITICAL
Duration: >1s (transient; expected during failover; should resolve within 20s)
```

**Alert 2: Failover takes too long**
```
Condition: No primary pod (all fm_pod_role=0) for >30s
Severity: CRITICAL
Duration: >30s (indicates both standby CAS failed and network partition; requires manual intervention)
```

**Alert 3: DPU programming latency spike**
```
Condition: fm_dpu_latency_p99_ms > 500 for >5s
Severity: WARNING
Duration: >5s (indicates catch-up backlog; may be part of failover or high load)
Reason: New primary resync during failover; normal (self-resolves within 30s)
```

**Alert 4: Multiple primary transitions**
```
Condition: fm_pod_role transitions (1→0 or 0→1) >3 times in 5 minutes
Severity: CRITICAL
Reason: Possible flapping; indicate unstable primary (network issues, high CPU, etc.)
```

### 7.3 Traces

**Trace: Failover sequence (start at T=15s)**
```
Trace ID: trace-failover-20260614-T1518505
Span: lease.renewal.failed (primary, T=+5s)
  └─ gNMI.stream.closed (primary, T=+5s)
Span: lease.expired.detected (standbys, T=+15s)
  └─ Span: lease.cas.race (both standbys)
    └─ lease.cas.success (fm-1, T=+15.05s)
    └─ lease.cas.fail (fm-2, T=+15.05s)
Span: primary.activation (fm-1, T=+15.1s)
  └─ gNMI.stream.open (dpu-1000)
  └─ gNMI.stream.open (dpu-1001)
  └─ gNMI.stream.open (dpu-1002)
  └─ eni.resync (eni-1000, watermark=etcd:12345)
Span: primary.ready (fm-1, T=+15.5s)
```

## 8. Debugging Guide

### 8.1 "Why didn't failover happen?"

**Symptom:** Primary pod crashed; standbys did not take over; cluster is down.

**Checklist:**
1. **Is T2 etcd reachable?**
   ```
   kubectl exec fm-1 -c fm -- etcdctl --endpoints=$T2_ENDPOINT health
   ```
   If error: T2 is down; replicas cannot claim lease. Fix T2.

2. **Is adapter lease stuck?**
   ```
   kubectl exec fm-0 -c fm -- etcdctl --endpoints=$T2_ENDPOINT get /dashfabric/cluster/adapter/lease
   ```
   If output shows `holder=dead-pod` with recent `claimed_at`: lease TTL may be too long. Reduce `FM_ADAPTER_LEASE_TTL_SECONDS`.

3. **Did standbys attempt CAS?**
   ```
   kubectl logs fm-1 | grep "Adapter lease CAS"
   kubectl logs fm-2 | grep "Adapter lease CAS"
   ```
   If no log: standbys never detected lease expiry. Check T2 connectivity.

4. **Did new primary fail to open gNMI?**
   ```
   kubectl logs fm-1 | grep "gNMI.stream"
   ```
   If errors: DPUs may be unreachable. Verify network connectivity to DPUs.

### 8.2 "Why did both standbys become primary?"

**Symptom:** Both fm-1 and fm-2 report `fm_pod_role=1` (primary). Split-brain detected.

**Cause:** CAS race had a bug; both won (or both think they won without verifying).

**Debug:**
1. **Check T2 lease in etcd:**
   ```
   kubectl exec fm-0 -c fm -- etcdctl --endpoints=$T2_ENDPOINT get /dashfabric/cluster/adapter/lease --print-lease-info
   ```
   Should show only one holder. If both pods' names in history, indicate CAS failure.

2. **Check pod logs for CAS result verification:**
   ```
   kubectl logs fm-1 | grep "CAS.*success\|CAS.*fail"
   kubectl logs fm-2 | grep "CAS.*success\|CAS.*fail"
   ```
   Only one should have `success`. Other should have `fail`.

3. **Check if pods are actually programming DPUs:**
   ```
   kubectl exec fm-1 -c fm -- curl localhost:8080/api/v1/health | jq .dpu_status
   kubectl exec fm-2 -c fm -- curl localhost:8080/api/v1/health | jq .dpu_status
   ```
   If both show DPU connections: both think they're primary (split-brain).

**Fix:**
1. Force one pod to release lease:
   ```
   kubectl delete pod fm-1
   ```
   Restarting pod will attempt to claim lease; if other pod holds it, new pod enters standby.

2. Verify only one primary remains:
   ```
   kubectl get pods -l app=fm -o jsonpath='{.items[*].status}' | jq 'map(select(.label_role=="primary")) | length'
   ```
   Should be 1.

### 8.3 "Why is failover slow (>30s)?"

**Symptom:** Primary crashes at T=0s; new primary active at T=35s (instead of T=15s).

**Causes:**
1. **T2 lease TTL is too long:**
   - Default TTL=15s; if set to 30s+, failover takes longer.
   - Check: `FM_ADAPTER_LEASE_TTL_SECONDS` environment variable.
   - Fix: Reduce to 10–15s.

2. **New primary slow to open gNMI:**
   - Resync from T1 taking >5s (check T1 latency; may be overloaded).
   - Or: DPU connection timeouts (network issues).
   - Check logs: `gNMI.stream.open` span duration.
   - Fix: Reduce T1 load; verify DPU network connectivity.

3. **CAS race delayed (etcd contention):**
   - Multiple pods simultaneously attempting CAS; etcd slow.
   - Check: `fm_t2_cas_latency_ms` metrics.
   - Fix: Upgrade T2 etcd cluster (more CPU, faster SSD).

### 8.4 "Primary is flapping (repeatedly crashing and restarting)"

**Symptom:** Multiple failovers in 5 minutes; cluster unstable.

**Causes:**
1. **High memory pressure:** Primary pod OOM-killed; restarted by Kubernetes.
   ```
   kubectl describe pod fm-0 | grep -i "out of memory\|oom"
   ```
   Fix: Increase pod memory limit; or reduce shard size per pod.

2. **gNMI stream issues:** Primary connects to DPU; DPU disconnects abruptly.
   - Check: `fm_dpu_connection_errors_total` metric.
   - Fix: Investigate DPU stability; check network MTU.

3. **T1 etcd latency causing timeouts:** Watches slow; primary starves.
   - Check: `fm_t1_watch_latency_ms` metric.
   - Fix: Upgrade T1 cluster; reduce FM shard size.

## 9. Failover Prevention & Tuning

### 9.1 Reduce Failover Window

**Goal:** Minimize RTO from 15s to <5s.

**Tuning:**
- Reduce `FM_ADAPTER_LEASE_TTL_SECONDS` to `5s` (minimum: 1x renewal interval).
- Increase `FM_ADAPTER_RENEW_INTERVAL_SECONDS` to `1s` (more frequent heartbeat).
- Trade-off: More frequent T2 writes; higher etcd load.

**Config:**
```yaml
env:
  - name: FM_ADAPTER_LEASE_TTL_SECONDS
    value: "5"
  - name: FM_ADAPTER_RENEW_INTERVAL_SECONDS
    value: "1"
```

### 9.2 Faster New Primary Activation

**Goal:** Reduce gNMI resync time from 500ms to <100ms.

**Tuning:**
- Pre-cache DPU connection state in standby pods (read-only).
  - Standbys build open gNMI connections but don't send updates.
  - On failover, new primary "reuses" pre-built connections.
  - Saves ~200ms connection time.
- **Status:** Not yet implemented; future enhancement.

## 10. Configuration Knobs

| Knob | Default | Min | Max | Purpose |
|------|---------|-----|-----|---------|
| `FM_ADAPTER_LEASE_TTL_SECONDS` | 15 | 1 | 60 | Lease TTL; failover window. |
| `FM_ADAPTER_RENEW_INTERVAL_SECONDS` | 5 | 1 | 15 | Renewal frequency; heartbeat. |
| `FM_T2_DIAL_TIMEOUT_SECONDS` | 5 | 1 | 30 | T2 connection timeout. |
| `FM_GNMI_CONNECT_TIMEOUT_SECONDS` | 10 | 5 | 60 | gNMI connection timeout. |
| `FM_LEASE_RENEWAL_RETRIES` | 3 | 1 | 10 | Retries before giving up. |
| `FM_FAILOVER_DETECTION_INTERVAL_SECONDS` | 1 | 1 | 5 | How often standbys check lease expiry. |

## 11. References

- `fm-pod-lifecycle-design.md` — Pod lifecycle states and transitions
- `storage-architecture.md` — T1/T2 etcd clusters and consistency
- `recovery-and-failover-design.md` — Comprehensive failure recovery
- `02-cb-low-level-design-lld.md` — ControllerBridge topic store (T1 source)
- `deployment-tiers.md` — Deployment configurations for T1/T2 clusters
