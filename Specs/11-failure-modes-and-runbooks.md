# 11 — Failure Modes & Runbooks

> A failure matrix is more useful than any feature list at 03:00. This
> document enumerates every failure DashFabric is designed to survive, how it
> manifests, how to detect it, and the operator runbook to recover. Each row
> links the alerts in `09-observability-and-diagnostics.md` to concrete
> action.

---

## 1. Failure Mode Index

| ID | Class | Failure | Severity | Detection | Recovery |
|---|---|---|---|---|---|
| F-001 | Pod | Shard Primary crash | P1 | Lease expiry; alert `PrimaryAbsent` | Auto-failover to Standby; ≤ 2 s |
| F-002 | Pod | Shard Standby crash | P3 | StatefulSet replacement | Auto by K8s |
| F-003 | Pod | NBG crash | P3 | HPA / readyz | Auto by K8s |
| F-004 | Pod | PM crash | P3 | Leader election to standby | Auto |
| F-005 | Node | Worker node crash | P2 | K8s scheduling | Pods rescheduled; PVC binding may delay if not local NVMe RWX |
| F-006 | AZ | AZ outage | P1 | Many alerts; lost 1 pod/shard | 2-of-3 quorum; Primary may move; bandwidth halves |
| F-007 | Region | Region outage | P0 | All metrics dark | Disaster recovery; region rebuild |
| F-008 | Network | etcd unreachable | P1 | `EtcdDown`; lease loss | Fail-static; existing programming continues; new intent delayed |
| F-009 | Network | DPU unreachable | P2 | Liveness failures; `dashfabric_hal_apply_total{result="UNAVAILABLE"}` | Retry+backoff; after N misses, HDO destruction or quarantine |
| F-010 | Network | NBG → shard split | P2 | Registration latency spike | Devices retry; secondary path via OOB mgmt if configured |
| F-011 | Application | Actor stuck `FAILED` | P1 | `ActorsStuckFailed` | `dfctl object dump` → triage → `dfctl object reset` |
| F-012 | Application | Mailbox overflow | P2 | `dashfabric_event_mailbox_depth` high | Coalescing should engage; if not, root-cause slow handler |
| F-013 | Application | Reconcile backlog | P2 | `ReconcileBacklog` | Reduce reconcile concurrency cap or split shard |
| F-014 | Application | High drift rate | P2 | `DriftCriticalRising` | Investigate device or codec; `dfctl drift report` |
| F-015 | Application | Schema poison (invalid envelope from upstream) | P2 | `envelope_invalid_total` | Notify upstream; envelopes are quarantined; service continues |
| F-016 | Storage | BadgerDB corrupt | P1 | Pod fails to start | `dfctl wal repair` → rebuilds from etcd; or PVC restore |
| F-017 | Storage | PVC full | P1 | ENOSPC on flush | Increase PVC; in interim, drain incoming events |
| F-018 | Storage | Slow disk | P3 | `WalFlushSlow` | Investigate hardware; consider PVC migration |
| F-019 | HA | Lease churn / flapping | P2 | `LeaseChurn` | Inspect logs; usually network partition or undersized pod CPU |
| F-020 | HA | Fence token rejection (stale Primary) | P1 | `FenceRejection > 0` | Investigate; likely split-brain risk; restart suspect pod |
| F-021 | HA | All 3 pods lost (ShardSet destroyed) | P1 | StatefulSet `Pending` | PM provisions fresh ShardSet; ~30 s rebuild |
| F-022 | Partitioning | Hot shard (devs > splitThreshold) | P2 | `ShardOverloaded` | Auto-split (or manual) |
| F-023 | Partitioning | Cold shards | P3 | `ShardUnderloaded` | Operator merge |
| F-024 | Partitioning | Split phase B sync timeout | P2 | Operator dashboard | Rollback split; investigate |
| F-025 | Northbound | Registration storm (DC boot) | P2 | NBG QPS spike | NBG autoscales; rate limit if abusive |
| F-026 | Northbound | TPM attestation failures | P2 | `register_total{result="PERMISSION_DENIED"}` | Validate firmware whitelist; investigate device |
| F-027 | Security | Cert expiry | P1 | SPIRE alerts | Rotate immediately; investigate why automation failed |
| F-028 | Security | Unauthorized operator action | P1 | Audit log signal | Revoke credentials; investigate |
| F-029 | Capacity | etcd write rate ceiling | P1 | etcd latency spike | Shard etcd by region sub-keys; raise hardware |
| F-030 | Upstream | Upstream silent for hours | P2 | event rate near zero | Confirm upstream health; nothing for DashFabric to do |
| F-031 | Upstream | Upstream mass-delete event | P1 | huge delete rate | Quarantine policy: if `delete_rate > threshold`, pause + alert operator before propagating |
| F-032 | DPU | DPU firmware bug rejects valid Set | P1 | HAL non-retryable errors per device | Quarantine HDO; rollback DPU firmware; refile with vendor |

---

## 2. Runbooks

Each runbook follows the same structure: **Symptoms → Confirm → Mitigate →
Resolve → Post-mortem checklist**.

### 2.1 RB-001: Shard Primary Crash (F-001)

**Symptoms**
- Alert `PrimaryAbsent{shard=...}`.
- Brief spike in `dashfabric_failover_seconds`.
- Possible spike in `dashfabric_hal_apply_total{result="UNAVAILABLE"}` for ≤ 5 s.

**Confirm**
```
kubectl -n dashfabric-system get pods -l shard=<shard>
kubectl -n dashfabric-system logs shard-<id>-1 --previous | tail -50
```
Look for OOM, panic, or SIGKILL signature.

**Mitigate**
- Auto-failover should already have occurred (lease moved to a Standby).
- Verify via `dfctl shard show <shard>` — new Primary present.

**Resolve**
- K8s will respawn the failed pod automatically.
- Inspect logs / pprof if root cause is unclear (OOM → bump memory; panic →
  open bug).

**Post-mortem**
- [ ] Failover time recorded?
- [ ] No `FAILED` actors introduced?
- [ ] No fence rejections?
- [ ] If OOM, file `tune-mem-<shard>` ticket.

---

### 2.2 RB-008: etcd Unreachable (F-008)

**Symptoms**
- Alerts: `EtcdDown`, `LeaseAge` rising for all shards.
- `dashfabric_configbus_events_total` flatlines.

**Confirm**
```
kubectl -n dashfabric-data get pods -l app=etcd
etcdctl --endpoints=... endpoint health
etcdctl --endpoints=... endpoint status
```

**Mitigate**
- DashFabric **fails static**: existing programming continues. No operator
  action needed for device traffic.
- For fresh intent: nothing we can do until etcd is back.

**Resolve**
- Standard etcd ops: restart members, restore quorum, snapshot restore if
  needed.

**Post-mortem**
- [ ] Confirm no FAILED actors created during outage.
- [ ] Confirm catch-up replayed all queued revisions.
- [ ] Confirm devices unaffected by checking DPU dashboards.

---

### 2.3 RB-011: Actor Stuck FAILED (F-011)

**Symptoms**
- Alert `ActorsStuckFailed`.
- `dashfabric_actor_failed > 0` with the offending labels.

**Confirm**
```
dfctl object dump <objectID>
# Look at .last_error, .intent_revision, .hash, .device_actual_hash.
```

**Mitigate**
- Identify the failure mode from `.last_error`:
  - `INVALID_ARGUMENT` from gNMI → likely DASH schema bug or vendor codec
    mismatch. File defect; consider rolling back intent.
  - `RESOURCE_EXHAUSTED` → device over capacity; quarantine; engage capacity
    team.
  - `PERMISSION_DENIED` → cert / fence issue; investigate.

**Resolve**
- After fix: `dfctl object reset <objectID>` to clear FAILED and re-attempt.

**Post-mortem**
- [ ] Document the underlying cause.
- [ ] If schema-related, add to codec test fixtures.
- [ ] If vendor-specific, file vendor ticket and add capability filter.

---

### 2.4 RB-014: High Drift Rate (F-014)

**Symptoms**
- Alert `DriftCriticalRising`.
- Surge in `dashfabric_drift_detected_total{severity="CRITICAL"}`.

**Confirm**
```
dfctl drift report --severity CRITICAL --json | jq .
dfctl drift report --host <suspicious-host>
```

**Common causes**
- Out-of-band CLI on the DPU (someone ran SAI debug).
- A vendor firmware bug ignoring certain leaves.
- A control-plane bug compiling wrong GoalState (less common; symptoms
  consistent across multiple devices, not one).

**Mitigate**
- Reconcile loop should auto-correct CRITICAL drift on next cycle; force
  with `dfctl reconcile now`.
- If drift returns immediately after correction → device-side bug; quarantine.

**Resolve**
- For repeated drift on one device → quarantine; capture telemetry; file
  vendor ticket.
- For repeated drift on many devices in one tenant → suspect upstream issue.

---

### 2.5 RB-020: Fence Token Rejection (F-020)

**Symptoms**
- Alert `FenceRejection > 0` (any nonzero).
- Implies a stale Primary attempted a write.

**Confirm**
```
kubectl -n dashfabric-system logs shard-<id>-* | grep "fence"
```

**Mitigate**
- Identify the offending pod; restart it (`kubectl delete pod ...`).
- Confirm lease holder per `dfctl shard show`.

**Root-cause**
- Likely a long GC pause or network stall delayed the lease-loss notification.
  Investigate runtime metrics; consider lowering `GOGC` or increasing
  `leaseDuration` if recurrent.

**Post-mortem**
- [ ] Filed runtime-tuning ticket?
- [ ] Confirmed no double-actuation impact (device-side fence rejection
      prevents corruption by design).

---

### 2.6 RB-021: Entire ShardSet Lost (F-021)

**Symptoms**
- StatefulSet pods Pending or all Crashing.
- Alert flurry: PrimaryAbsent, EventLagHigh for that shard's devices, etc.

**Confirm**
```
kubectl -n dashfabric-system describe statefulset shard-<id>
kubectl -n dashfabric-system describe pvc -l shard=<id>
```

**Mitigate**
- PM should detect via Lease+pod status and provision a fresh ShardSet.
- If PM is also down → manual:
  ```
  kubectl delete statefulset shard-<id>
  kubectl delete pvc -l shard=<id>
  kubectl apply -f manifests/shard-<id>.yaml
  ```
- New pods boot empty; snapshot bootstrap from etcd takes ~5 min for 2k devices.

**Resolve**
- During the gap, devices keep operating on previously-programmed state
  (data plane unaffected).
- Fresh intent for those devices delayed by the rebuild time.

**Post-mortem**
- [ ] Root cause the loss (storage class issue, node group eviction, etc.).
- [ ] Improve detection if PM was slow.

---

### 2.7 RB-031: Upstream Mass Delete (F-031)

**Symptoms**
- Alert `MassDeleteDetected`: delete events > threshold per minute.

**Confirm**
```
dfctl admin freeze --region <R>     # PAUSES new intent ingest (safety)
dfctl drift report --json | jq '.delete_pending | length'
```

**Mitigate**
- **Default policy: pause-and-confirm.** DashFabric will pause propagation
  of deletes beyond `massDeleteThreshold` per minute and require operator
  acknowledgment.
- Confirm legitimacy with upstream (e.g. tenant offboarding vs. accident).
- If legitimate: `dfctl admin thaw --region <R>`; deletes resume.
- If accident: have upstream re-publish; deletes never propagated, no
  customer impact.

**Why this exists:** the single biggest risk of an automated control plane
is propagating a mistake faster than humans can stop it. We trade a few
seconds of delete latency for catastrophic-blast-radius protection.

---

### 2.8 RB-032: DPU Firmware Bug (F-032)

**Symptoms**
- One device has steady-state FAILED actors with same gNMI error code.
- Other devices on same SKU may also be affected.

**Confirm**
- Compare across devices via `dfctl drift report --host <h>` etc.
- Engage vendor with telemetry capture.

**Mitigate**
- `dfctl device pin <h> --shard <s>` to keep the device on a known shard.
- Add a temporary capability filter to skip the failing feature for that
  firmware version.
- Quarantine the HDO if intent cannot be safely applied; document customer
  impact.

**Resolve**
- Vendor firmware fix → planned upgrade → unquarantine.

---

## 3. Chaos Engineering Test Matrix

| Scenario | Frequency | Pass criteria |
|---|---|---|
| Random Primary kill | weekly | failover < 2 s; no FAILED actors; no fence rejections |
| Random pod kill | weekly | no programming gap > 5 s |
| etcd outage (5 m) | monthly | service stays static; recovers cleanly |
| Network partition (shard ↔ etcd) | monthly | shards revert to read-only; recovery clean |
| Network partition (shard ↔ DPU fleet) | monthly | reconcile backlog rises; recovers within 1 reconcile cycle |
| ISSU rolling upgrade | per release | zero programming gap; all canary gates pass |
| Shard split under sustained load | per release | no event loss; checksums match |
| Mass-delete simulation | per release | pause-and-confirm engaged; no propagation past threshold |
| DPU firmware downgrade | per quarter | capability filter activates; service unaffected for other devices |

---

## 4. SLO Summary

| SLO | Target | Error budget |
|---|---|---|
| End-to-end intent → device-ACK | p99 ≤ 500 ms | 0.1 % above 1 s |
| Planned failover gap | 0 ms | 1 minute / month |
| Unplanned failover gap | ≤ 2 s p99 | 1 minute / month |
| Reconcile cadence (NO) | ≤ 90 s (max age) | 1 % > 120 s |
| CRITICAL drift correction time | ≤ 60 s | 0.5 % > 5 min |
| Registration success rate | ≥ 99.9 % | 0.1 % rejected (excluding policy denies) |

---

## 5. Operator On-Call Cheat Sheet

| Question | Command |
|---|---|
| Is the region healthy? | `dfctl admin region <R> status` |
| Which shard owns my device? | `dfctl device get <HostID>` |
| Why is intent slow? | check `EventLagHigh` panel; `dfctl trace replay <TraceID>` |
| Why is this ENI failing? | `dfctl object dump <ObjectID>` |
| Drift across all hosts? | `dfctl drift report --severity CRITICAL` |
| Force a reconcile? | `dfctl reconcile now <ObjectID>` |
| Move a device to a different shard? | `dfctl device pin <HostID> --shard <S>` |
| Quarantine a device? | `dfctl device throttle <HostID> --max-events-per-sec 0` |
| Pause new registrations? | `dfctl admin freeze --region <R>` |
| Inspect a pod's WAL? | `dfctl wal dump --host <HostID>` |

---

## 6. Open Questions

| ID | Question | Default |
|---|---|---|
| OQ-1101 | Should mass-delete protection be on by default? | **Yes.** Surprised operators prefer pause over irreversible damage. |
| OQ-1102 | Should we auto-generate runbook stubs from alerts? | **Yes** — keep runbooks colocated with alert definitions; PR review enforces stub. |
| OQ-1103 | Should chaos tests run in production canary regions? | **Pod-level yes; etcd/region-level only in staging.** |
