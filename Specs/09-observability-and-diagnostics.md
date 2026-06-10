# 09 — Observability & Diagnostics

> If we can't see it, we can't operate it. Observability is **first-class** in
> DashFabric — every config event, FSM transition, HAL call, reconcile cycle,
> failover, and operator command emits structured signals.

This document specifies the metrics catalog, log schema, tracing model, the
diagnostic CLI (`dfctl`), debug APIs, and the on-call runbook surface.

---

## 1. Pillars

| Pillar | Backend | Storage hot/cold |
|---|---|---|
| **Metrics** | Prometheus → Mimir/Cortex (or VictoriaMetrics) | 30 d hot / 1 y cold |
| **Traces** | OpenTelemetry → Tempo (or Jaeger) | 7 d hot / 30 d sampled cold |
| **Logs** | Structured JSON → Loki | 14 d hot / 90 d cold |
| **Events** | K8s Events + DashFabric audit log → Loki | 90 d |
| **Profiles** | pprof on demand + continuous profiling (e.g. Parca) | 7 d |

---

## 2. The Three Operator Questions

Every dashboard must answer three questions at a glance:

1. **Is intent flowing?**  (event ingress, program latency, queue depth)
2. **Is the system healthy?**  (errors, restarts, lease churn, drift rate)
3. **Are devices in sync?**  (drift counts by severity, reconcile lag)

All three questions are answered by SLO-style metrics with documented
alerting thresholds (§7).

---

## 3. Metrics Catalog

Naming convention: `dashfabric_<subsystem>_<thing>_<unit>` (Prometheus-style).
All metrics include the labels `{region, shard, pod}` at minimum.

### 3.1 Event Ingress (ConfigBus)
| Metric | Type | Labels | Meaning |
|---|---|---|---|
| `dashfabric_configbus_events_total` | counter | `kind`, `object_kind` | All events received (incl. snapshot replay) |
| `dashfabric_configbus_event_lag_seconds` | histogram | — | Wall-clock from `issued_at` → dispatch |
| `dashfabric_configbus_watch_paused_seconds_total` | counter | `host_id` | Backpressure incidents |
| `dashfabric_configbus_envelope_invalid_total` | counter | `publisher` | Schema-rejected envelopes |
| `dashfabric_configbus_coalesced_total` | counter | `object_kind` | Mailbox-coalescing drops |
| `dashfabric_event_mailbox_depth` | gauge | `object_kind` | Current mailbox occupancy (avg) |

### 3.2 Actor FSM
| Metric | Type | Labels | Meaning |
|---|---|---|---|
| `dashfabric_actor_alive` | gauge | `object_kind` | Current actor count |
| `dashfabric_actor_transitions_total` | counter | `object_kind`, `from`, `to` | FSM transition rate |
| `dashfabric_actor_failed` | gauge | `object_kind` | Count in FAILED state — alert > 0 |
| `dashfabric_actor_spawn_seconds` | histogram | `object_kind` | Time from spawn to PROGRAMMED |

### 3.3 HAL / Southbound
| Metric | Type | Labels | Meaning |
|---|---|---|---|
| `dashfabric_hal_apply_seconds` | histogram | `object_kind`, `result` | Apply call duration |
| `dashfabric_hal_apply_total` | counter | `object_kind`, `result` | OK / retryable / non-retryable / shadow |
| `dashfabric_hal_get_seconds` | histogram | — | Get duration |
| `dashfabric_hal_throttle_seconds_total` | counter | `host_id` | Pacing-induced delay |
| `dashfabric_hal_inflight_calls` | gauge | `host_id` | Current concurrent calls |
| `dashfabric_hal_shadow_apply_total` | counter | `object_kind` | Standby shadow throughput |
| `dashfabric_hal_fence_rejected_total` | counter | — | Device rejected our token (means we lost lease without knowing) |

### 3.4 Reconciliation
| Metric | Type | Labels | Meaning |
|---|---|---|---|
| `dashfabric_reconcile_cycles_total` | counter | `object_kind`, `result` | Cycles completed |
| `dashfabric_reconcile_duration_seconds` | histogram | `object_kind` | Per-cycle latency |
| `dashfabric_drift_detected_total` | counter | `object_kind`, `severity` | Drift events |
| `dashfabric_drift_current` | gauge | `object_kind`, `severity` | Current outstanding drift count |
| `dashfabric_reconcile_lag_seconds` | gauge | `object_kind` | Age of oldest object reconciled |

### 3.5 HA / Lease
| Metric | Type | Labels | Meaning |
|---|---|---|---|
| `dashfabric_primary_pod` | gauge | `shard`, `holder` | Which pod is primary now |
| `dashfabric_lease_transitions_total` | counter | `shard`, `reason` | Failover events |
| `dashfabric_failover_seconds` | histogram | `shard`, `kind` | Failover duration (kind=planned/unplanned) |
| `dashfabric_lease_age_seconds` | gauge | `shard` | Time since last renewal |

### 3.6 Partition Manager
| Metric | Type | Labels | Meaning |
|---|---|---|---|
| `dashfabric_shard_count` | gauge | `state` | Shards by state |
| `dashfabric_devs_per_shard` | gauge | `shard` | Load distribution |
| `dashfabric_shard_split_total` | counter | — | Splits since boot |
| `dashfabric_shard_merge_total` | counter | — | Merges |
| `dashfabric_shard_rebuild_seconds` | histogram | `shard` | Recovery time |

### 3.7 Northbound Gateway
| Metric | Type | Labels | Meaning |
|---|---|---|---|
| `dashfabric_register_total` | counter | `result` | Registration attempts |
| `dashfabric_register_latency_seconds` | histogram | — | NBG latency |
| `dashfabric_devices_registered` | gauge | — | Currently registered count |
| `dashfabric_heartbeats_received_total` | counter | — | All shards combined |

### 3.8 Storage / WAL
| Metric | Type | Labels | Meaning |
|---|---|---|---|
| `dashfabric_wal_flush_seconds` | histogram | — | Badger sync latency |
| `dashfabric_wal_size_bytes` | gauge | — | On-disk WAL size |
| `dashfabric_wal_replay_seconds` | histogram | — | Cold-start replay duration |
| `dashfabric_blob_cache_bytes` | gauge | — | Blob LRU size |
| `dashfabric_blob_cache_hit_ratio` | gauge | — | Effectiveness |

### 3.9 Runtime
Standard Go runtime metrics + `process_*`. Plus:
| Metric | Type | Labels |
|---|---|---|
| `dashfabric_goroutines_per_object` | gauge | `object_kind` |
| `dashfabric_message_processed_seconds` | histogram | `object_kind`, `kind` |

---

## 4. Logging Schema

All logs are **structured JSON** to stderr; Promtail ships to Loki.

```json
{
  "ts": "2026-06-11T02:00:00.123Z",
  "level": "INFO",
  "logger": "actor.no",
  "msg": "PROGRAMMING → PROGRAMMED",
  "trace_id": "8a1b2c3d4e5f...",
  "span_id": "abcd1234",
  "region": "westus2",
  "shard": "shard-0007",
  "pod": "shard-0007-1",
  "host_id": "HOST-1",
  "container_guid": "VM-abc",
  "nic_id": "nic-primary",
  "intent_revision": 9876,
  "apply_ms": 87,
  "applied": true,
  "hash_old": "8f3a...",
  "hash_new": "9b21..."
}
```

### 4.1 Log Levels
- `ERROR`: action required; paired with alert.
- `WARN`: anomaly to track.
- `INFO`: lifecycle / decisions / important transitions.
- `DEBUG`: per-message, off by default.

### 4.2 Sensitive Fields
Tenant tags, PA addresses, customer prefixes are tagged with a `sensitive: true`
note in log schema docs; Loki has a redaction pipeline for export to
non-trusted sinks.

### 4.3 Sampling
- `INFO`: 100 %.
- `DEBUG`: 0 % default; per-actor toggle via `dfctl object debug on <id>`.

---

## 5. Distributed Tracing

### 5.1 Span Tree (Happy Path)
```
upstream.publish                       (root, in upstream service)
  └── etcd.watch_event
      └── shard.dispatch
          └── actor.handle_event       (per actor handler)
              └── hal.apply
                  └── gnmi.set_rpc
                      └── device.process
```

Spans carry:
- `dashfabric.object_id`, `object.kind`, `intent.revision`
- `dashfabric.shard`, `pod`, `host_id`
- For HAL spans: `gnmi.path_count`, `gnmi.bytes`, `result`

### 5.2 Cross-process Propagation
W3C `traceparent` header carried in:
- The ConfigBus envelope (producer must set).
- The internal Message struct.
- The HAL outbound gRPC metadata.

### 5.3 Sampling
- **Tail-based** sampling at the OTel Collector: 100 % of error traces, 10 %
  of slow traces (above SLO), 1 % of others.

---

## 6. K8s Events & Audit

DashFabric emits K8s `Events` for ShardSet lifecycle and PM actions:
```
shard-0007    Split Initiated   reason=HotShardDetected
shard-0007    Split Phase B    reason=Sync barrier reached
shard-0007a   Activated        reason=Split complete
```

A separate **audit log** records every operator command (CLI or API):
```json
{"ts": "...", "actor": "alice@corp", "action": "device.unregister",
 "args": {"host_id":"HOST-1"}, "result": "OK", "trace_id": "..."}
```

Audit logs are write-once (Loki tenant with deletion disabled).

---

## 7. Alerts (Sample)

| Alert | Condition | Severity |
|---|---|---|
| **EventLagHigh** | `histogram_quantile(0.99, dashfabric_configbus_event_lag_seconds) > 5` for 5 m | P2 |
| **ActorsStuckFailed** | `sum(dashfabric_actor_failed) > 0` for 10 m | P1 |
| **DriftCriticalRising** | `increase(dashfabric_drift_detected_total{severity="CRITICAL"}[5m]) > 10` | P1 |
| **ReconcileBacklog** | `dashfabric_reconcile_lag_seconds > 600` for 10 m | P2 |
| **LeaseChurn** | `rate(dashfabric_lease_transitions_total[10m]) > 1/min` | P2 |
| **FenceRejection** | `dashfabric_hal_fence_rejected_total > 0` | P1 (split-brain risk) |
| **EtcdDown** | `up{job="etcd"} == 0` for 1 m | P1 |
| **ShardOverloaded** | `dashfabric_devs_per_shard > splitThreshold` for 1 h | P2 |
| **ShardUnderloaded** | `dashfabric_devs_per_shard < mergeThreshold` for 24 h | P3 |
| **WalFlushSlow** | `histogram_quantile(0.99, dashfabric_wal_flush_seconds) > 0.2` for 10 m | P2 |
| **PrimaryAbsent** | `absent(dashfabric_primary_pod{shard="X"})` for 30 s | P1 |

Every alert links to a runbook (see `11-failure-modes-and-runbooks.md`).

---

## 8. The Diagnostic CLI — `dfctl`

`dfctl` is the operator's swiss-army knife. **All operations route through
the shard's Admin API**, which serializes them through actor mailboxes —
never bypassing the FSM.

### 8.1 Commands

```
dfctl device list [--region R] [--state S]
dfctl device get <HostID>                   # capabilities + FSM + last seen
dfctl device unregister <HostID>            # graceful
dfctl device pin <HostID> --shard <S>
dfctl device debug on|off <HostID>
dfctl device throttle <HostID> --max-events-per-sec=N

dfctl object dump <ObjectID>                # full ActorSnapshot
dfctl object dump --tree <HostID>           # entire host's actor tree
dfctl object reset <ObjectID>               # FAILED → CONFIGURING
dfctl object simulate <ObjectID>            # dry-run reconcile, print diff
dfctl reconcile now <ObjectID>
dfctl reconcile pause <ObjectID>            # alerts after 1h

dfctl shard list
dfctl shard split <ShardID>                 # operator-driven
dfctl shard merge <S_a> <S_b>
dfctl shard drain <ShardID>                 # for maintenance
dfctl shard show <ShardID>                  # primary, devices, load

dfctl wal dump --host <HostID>              # offline forensics
dfctl wal repair --shard <ShardID>          # rebuild from etcd

dfctl drift report --host <HostID>          # human-readable drift summary
dfctl drift report --severity CRITICAL --json

dfctl trace replay <TraceID>                # re-run an event through dry-run

dfctl admin freeze --region <R>             # stop accepting registrations
dfctl admin thaw --region <R>
```

### 8.2 Output Modes
- Default: human-friendly tables.
- `--json` / `--yaml`: for piping.
- `--watch`: tails updates.

### 8.3 Safety
- Destructive commands prompt; bypass with `--yes`.
- All commands are audit-logged with user identity (OIDC).
- RBAC: `device.*` for SREs, `shard.*` for senior SREs, `admin.*` for
  release managers.

---

## 9. Debug APIs

Beyond `dfctl`, each pod exposes:

| Endpoint | Purpose |
|---|---|
| `:8080/healthz` | Liveness |
| `:8080/readyz` | Readiness (catch-up + lease eligible) |
| `:8081/metrics` | Prometheus scrape |
| `:8082/debug/pprof/*` | Standard Go pprof |
| `:8083/admin/*` | Authenticated admin API (used by `dfctl`) |
| `:8083/admin/dump/host/<HostID>` | full snapshot JSON |
| `:8083/admin/dump/shard` | shard-level snapshot |

---

## 10. Dashboards (Reference Set)

| Dashboard | Audience | Key panels |
|---|---|---|
| **Intent Flow Health** | SRE on-call | event ingress rate, p99 lag, mailbox depth, coalescing rate |
| **Programming Health** | SRE on-call | HAL apply latency, error rates, FAILED actor count |
| **Reconciliation & Drift** | Network ops | drift by severity, reconcile cadence per shard, lag |
| **HA & Failover** | SRE on-call | lease holders, transitions/h, fence rejections |
| **Capacity & Partitioning** | Capacity team | devs/shard, shard count by state, NBG QPS |
| **Per-Device Forensics** | Network ops | filter by HostID; FSM, drift, traces |
| **Per-Tenant View** | Customer success | event rate by tenant, ENIs by tenant, drift |

Dashboards live in `deploy/dashboards/*.json` (Grafana).

---

## 11. Continuous Profiling

Parca (or pprof-poll) collects CPU + heap profiles every 10 s. Retained for
7 days. Useful for catching slow regressions during canary upgrades.

---

## 12. Open Questions

| ID | Question | Default |
|---|---|---|
| OQ-901 | eBPF probes for socket latency between us and DPUs? | **Yes, optional sidecar** for high-fidelity diagnosis. Off by default; on for high-tier regions. |
| OQ-902 | Log retention 14 d hot? | Yes; up from 7 to give enough for cross-shift incident review. |
| OQ-903 | Should drift severity classes be operator-overridable per leaf? | **Yes** in v2; v1 ships with the HAL's defaults. |
| OQ-904 | Should we emit a dedicated "intent ack" trace span observable by upstream? | **Yes**; helps the upstream SDN measure end-to-end SLOs. |
