# 12 — Roadmap & Open Questions

> What ships in v1, what waits for v2, what is explicitly out of scope, and
> the living list of open questions.

---

## 1. Versioning Plan

| Version | Theme | Target |
|---|---|---|
| **v0.1** | Walking skeleton: single shard, single device, DASH gNMI codec, mock device | Internal lab |
| **v0.5** | Single ShardSet with 3-replica HA, real ENI programming, full FSM, basic dfctl | Lab integration with one vendor |
| **v1.0** | Multi-shard, Partition Manager, ISSU, observability, runbooks, two vendors, SOC2 controls | Production pilot |
| **v1.x** | Polish, performance, capability extensions, vendor breadth | Production GA |
| **v2.0** | Cross-region orchestration; live ENI migration coordination; adaptive reconcile; eBPF diagnostic sidecar | Year 2 |

---

## 2. v1.0 Scope (Production Pilot)

### 2.1 In Scope
- Full architecture as described in `01–11`.
- Two vendor DASH codecs validated against the conformance test suite.
- Helm chart + Kustomize overlays.
- `dfctl` with all commands listed in `09 §8`.
- SPIRE-based identity, TPM attestation for production tier.
- Multi-tenancy with quotas and isolation.
- Online split, manual merge.
- Operator dashboards (Grafana JSON shipped in repo).
- Chaos test suite running weekly in staging.

### 2.2 Out of Scope (v1)
- Live ENI migration coordination between DPUs (the upstream owns this;
  DashFabric just programs the result).
- Cross-region replication of state (each region is independent).
- Auto-merge of cold shards (operator-driven only).
- Dynamic loading of vendor plugins (`plugin.so`) — all plugins are built in.
- Native non-K8s deployment (bare-metal `systemd` is degraded mode only).
- Generating intent (DashFabric only consumes it).
- Customer billing aggregation (we expose counters; aggregation is downstream).

---

## 3. Explicitly Deferred / v2 Topics

| Topic | Why deferred | What v1 ships in its place |
|---|---|---|
| Auto-merge shards | Risk vs. value; operators prefer control | Manual `dfctl shard merge` |
| Adaptive reconcile cadence | Needs telemetry corpus to tune | Fixed intervals with operator overrides |
| Plugin SDK (3rd-party codecs) | API stability concerns | Vendor codecs upstreamed into our tree |
| Cross-region operator view | Per-region tools sufficient at pilot scale | Per-region `dfctl` |
| DASH HA pairing protocol orchestration | The DASH spec leaves this between cards | We program the pairing config; cards execute the protocol |
| eBPF socket-latency probes | Sidecar overhead; ops complexity | Standard tracing covers 95 % of needs |
| WebUI operator console | CLI sufficient at pilot | Open-question OQ-1201 |
| Tenancy with cryptographic isolation | Today's labels + RBAC sufficient | Open-question OQ-1202 |
| Integration with upstream SDN HA models (e.g. Azure HA spec) | Need upstream collaboration | Compatibility hooks designed; full integration v2 |

---

## 4. Phased Implementation Path

### Phase 1 — Foundations (8 weeks)
- Core actor runtime + mailbox + FSM library.
- `ConfigBus` interface + etcd adapter.
- BadgerDB WAL layer.
- Mock HAL + DASH gNMI codec skeleton.
- Single-pod end-to-end on a BMv2-simulated DASH device.
- Initial dfctl with `object dump` and `device list`.

**Exit criteria:** create + program + delete one ENI end-to-end with traces.

### Phase 2 — HA & Persistence (6 weeks)
- ShardSet StatefulSet template.
- Lease-based primary election.
- Shadow-mode HAL on standby.
- Warm restart with WAL replay.
- Cold-start with snapshot bootstrap.
- Chaos pod-kill test in CI.

**Exit:** sub-2-second unplanned failover demo with no FAILED actors.

### Phase 3 — Partitioning (6 weeks)
- Partition Manager controller.
- ShardMap in etcd.
- Online split with sync-barrier protocol.
- NBG routing.
- Manual merge.

**Exit:** split a 4k-device shard into 2 × 2k under sustained 10k events/sec
load with no event loss.

### Phase 4 — Production Hardening (8 weeks)
- SPIRE integration end-to-end.
- All metrics + dashboards + alerts wired.
- Runbook authoring for every alert.
- Two vendor codecs validated.
- TPM attestation flow.
- Helm chart polish.
- Security review + threat model document.
- Load testing at 10k devices.

**Exit:** SOC2 control matrix complete; pilot region deployed; soak test
passes 7 days at full scale.

---

## 5. Risk Register

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| DASH gNMI schema churn during v1 development | Medium | Medium | Track upstream PRs; codec ships with version pinning |
| Vendor DASH conformance varies | High | High | Conformance test gate in CI; capability filter; vendor partner program |
| etcd write-rate ceiling hit before sharding etcd | Low | High | Sharding plan documented (§03); monitoring catches early |
| Bulk routes / 8M mappings cause memory explosion | Medium | High | Blob cache + LRU + per-pod RAM cap; sharded NO actors if needed |
| Mass-delete from buggy upstream | Low | Catastrophic | Pause-and-confirm policy (RB-031) |
| Operator runs destructive `dfctl` by mistake | Medium | High | Confirmations + RBAC tiers + audit |
| Side-effects of warm-restart replay vs. running standby | Low | Medium | Snapshot tests; chaos coverage |
| HA fence tokens not adopted by vendors | Medium | Medium | Lease-side check is the fallback; degrade gracefully |
| Performance regression in OTel SDK | Low | Medium | Continuous profiling + perf gates in CI |

---

## 6. Open Questions Master List

> Aggregated from each chapter; ordered by impact.

| ID | Question | Default | Owner |
|---|---|---|---|
| OQ-201 | COs cache VNET-scope artifacts? | **Yes** | architecture |
| OQ-202 | Model ACL groups as sub-actors? | **No v1** | architecture |
| OQ-203 | Quarantine substate for firmware-update devices? | **Yes** | architecture |
| OQ-204 | Support ENI live-migration in core? | **No** (upstream) | architecture |
| OQ-301 | Single global etcd vs per-region? | **Per-region** | data plane |
| OQ-302 | Adopt CRDB/FoundationDB for blob store? | **No, S3** | data plane |
| OQ-303 | Encourage etcd Txn? | **Yes** | data plane |
| OQ-401 | Resizable shards? | **No** | partitioning |
| OQ-402 | Auto-merge by default? | **No** | partitioning |
| OQ-403 | Shard the PM itself? | **No** | partitioning |
| OQ-404 | Cross-region ShardMap? | **No** | partitioning |
| OQ-501 | 3 or 5 replicas per shard? | **3** | HA |
| OQ-502 | Use Raft inside shard? | **No** | HA |
| OQ-503 | Standardize fence tokens upstream? | **Yes, file upstream** | HA |
| OQ-504 | Allow 2-replica config? | **Yes, opt-in** | HA |
| OQ-601 | SPIRE Node Attestation? | **Yes** | security |
| OQ-602 | Full-health heartbeats? | **No, liveness only** | northbound |
| OQ-603 | BMC as separate identity? | **No v1** | security |
| OQ-604 | Secure-boot validation? | **Required prod** | security |
| OQ-701 | gNMI lib choice? | **openconfig/gnmi** | HAL |
| OQ-702 | Parallelize codec chunks? | **No v1** | HAL |
| OQ-703 | Expose vendor extensions? | **Via Caps.Extensions** | HAL |
| OQ-704 | HAL models DASH HA pairing? | **Config only; protocol off-box** | HAL |
| OQ-705 | Support non-DASH SmartNICs? | **No v1** | HAL |
| OQ-801 | Use RocksDB? | **No, Badger** | storage |
| OQ-802 | Pull reconcile from telemetry stream? | **Yes when possible** | reconcile |
| OQ-803 | Backup WAL to S3? | **No** | storage |
| OQ-804 | Adaptive reconcile cadence? | **v2** | reconcile |
| OQ-901 | eBPF sidecar probes? | **Optional, high tier** | obs |
| OQ-902 | Log retention 14 d? | **Yes** | obs |
| OQ-903 | Operator-overridable drift severity? | **v2** | obs |
| OQ-904 | Publish "intent ack" trace span for upstream? | **Yes** | obs |
| OQ-1001 | Self-host etcd? | **Yes** | deploy |
| OQ-1002 | Service mesh vs SPIRE+gRPC? | **SPIRE+gRPC v1** | deploy |
| OQ-1003 | DPU sidecar agent? | **Yes v1; upstream into DASH container v2** | deploy |
| OQ-1004 | Blob store backup strategy? | **Object-storage native** | deploy |
| OQ-1005 | Helm vs Kustomize? | **Both** | deploy |
| OQ-1101 | Mass-delete protection default-on? | **Yes** | failure |
| OQ-1102 | Auto-generate runbook stubs from alerts? | **Yes** | failure |
| OQ-1103 | Chaos in production canary? | **Pod-level only** | failure |
| OQ-1201 | WebUI operator console? | **v2; CLI sufficient v1** | UX |
| OQ-1202 | Cryptographic per-tenant isolation? | **v2** | security |
| OQ-1203 | Open-source the project? | **TBD** | strategy |

---

## 7. Glossary Pointers
See `README.md §4` for the full glossary.

---

## 8. Living Document Discipline

This document is **expected to change**. Conventions:

- Every PR that resolves an OQ moves it from this file to the relevant
  chapter as a decided design and adds an entry to the **Decisions Log** in
  `README.md §3`.
- Every PR that *adds* a new OQ adds it here with the next available ID.
- ADRs (architectural decision records) are added under
  `Specs/adr/NNNN-<slug>.md` for substantial reversals or extensions; the
  `README.md` decisions log references them.

---

## 9. Quick Wins (Backlog Suggestions)

Beyond the phased plan, these are small efforts with disproportionate value:

- **Synthetic device simulator** (Go-native, no BMv2 dependency) for unit
  tests and local dev — 2 weeks.
- **Per-host trace dashboard** mapping HostID → ENI tree → trace links — 1
  week (post-OTel collector configured).
- **Replay tooling**: `dfctl trace replay <TraceID>` against a dry-run HAL
  — 1 week (huge incident-investigation lift).
- **Schema diff visualizer**: show DASH schema differences between vendor
  codecs — 1 week.
- **Capacity dry-run**: feed a hypothetical intent file → estimate HAL
  bytes/sec, mailbox depth, reconcile load — 2 weeks.

---

## 10. Calls to Action for Upstream Communities

These are items we should propose / contribute upstream:

| Project | Proposal |
|---|---|
| sonic-net/DASH | Standardize fence-token metadata on gNMI Set RPCs to prevent split-brain double-actuation |
| sonic-net/DASH | Codify capability advertisement schema (we propose `DeviceCapabilities` as a starting point) |
| sonic-net/DASH | Publish reference SDN-side client library (currently the gNMI client side is left to each implementer) |
| openconfig/gnmi | Streaming bulk-update extension to avoid chunking for huge tables |
| spiffe/spire | TPM PCR policy templates for SmartNIC fleets |

---

## 11. Closing

DashFabric's bet is that **the right object model + the right boundary
between intent and projection + relentless observability** beats any one
clever subsystem. Every component in this spec is deliberately as simple as
it can be while honoring the invariants in §02 §11.

When in doubt, the rule of thumb is: **the upstream is right; we converge
to it.**
