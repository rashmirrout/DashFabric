# FM Implementation Plan

**Status:** Wave 0 in progress (2026-06-22)
**Target tier:** Hyperscale (default). One binary, capability-gated for DV / VM / Container / Hyperscale via `pkg/tier/`.
**Depth per slice:** Skeleton + unit tests + integration tests + e2e + per-feature doc with Future Scopes + tracker row.

---

## 0. How to read this plan

- Plan is **wave-based**. Each wave builds on the previous and ends in a green build with new tests.
- Each wave is broken into **slices** (small, individually shippable units).
- Each slice ends with:
  - All new code has unit tests.
  - At least one integration test exercising the new code with its neighbors.
  - Per-feature doc at `docs/<area>/<feature>.md` with **Future Scopes** section.
  - Tracker row updated in `docs/trackers/00-MASTER_TRACKER.md`.
  - `docs/next_plan.md` rewritten to describe the *next* slice.
  - `go build -v ./...` passes.
  - `go test -race ./...` passes.

Migration from legacy `cm/dm/gm/dal` is **incremental**. Old code is retired only when its replacement is functional and tested.

---

## 1. Wave map

| Wave | Theme | Replaces | New Packages | Done When |
|---|---|---|---|---|
| 0 | Scaffold & plan | — | `pkg/{registry,actor,adapter,driver,reconcile,ha,audit,security,lifecycle,tier,errors,types}/` (doc.go only) | Folder structure visible; build green; plan + tracker live |
| 1 | Registries (T3) | `pkg/dm/` | `registry/{vnet,nic,mapping,acl,route,meter}/` + `types/` + `errors/` | All six typed registries operational; refcounting works; `fm-lint NO_REGISTRY_BYPASS` rule live |
| 2 | Adapter (T1/T2) | `pkg/cm/` | `adapter/{t1,t2}/` | T1 config watch + T2 lease/ops state working against real etcd |
| 3 | Driver interface | `pkg/dal/` | `driver/{iface,delta,stub,grpc}/` | DeltaPlan computed; stub driver passes round-trip; gRPC client compiles |
| 4 | Actors | `pkg/gm/` | `actor/{hdo,co,no}/` + supervisor + mailbox | NicObject composes goal state; HDO holds driver session; supervisor restarts on panic |
| 5 | Reconciler + HA | — | `reconcile/{classify,loop,retry}` + `ha/{lease,election,freeze}` | Drift classified per taxonomy; lease acquire/renew/freeze working |
| 6 | Audit + Security | — | `audit/` + `security/{spiffe,opa,mtls,secret}/` | Hash-chained audit log; SPIFFE SVID auth; OPA policy eval |
| 7 | Tier + Lifecycle + E2E | — | `tier/`, `lifecycle/` | DV/VM/Container/Hyperscale gated; full CB→registry→actor→driver→audit e2e green |
| 8 | Observability polish + hardening | — | refactor `observability/{log,metric,trace,health}/` | Dashboards in `deploy/k8s/`; perf+chaos tests pass |

---

## 2. Wave 0 — Scaffold & Plan (CURRENT)

### Slices

| ID | Slice | Deliverables |
|---|---|---|
| 0.1 | Folder structure | 12 `doc.go` files in new `pkg/` subdirs declaring each package and its planned subpackages |
| 0.2 | Plan artifacts | `IMPLEMENTATION_PLAN.md`, `trackers/00-MASTER_TRACKER.md`, `next_plan.md` |
| 0.3 | Build sanity | `go build -v ./cmd/fm` passes; new empty packages don't break existing |

### Out of scope this wave

- No business logic. Nothing in `pkg/cm/dm/gm/dal/` is touched yet.
- No `fmctl` CLI yet. No tier specialization yet.

---

## 3. Wave 1 — Registries

### Slices

| ID | Slice | Files | Tests | Doc |
|---|---|---|---|---|
| 1.1 | Type aliases | `pkg/types/ids.go` (`ENIID`, `VnetID`, `RouteGroupID`, `AclGroupID`, `MappingID`, `MeterPolicyID`, `HaScopeID`), `pkg/types/versions.go` (`SpecRevision`, `Epoch`) | `types_test.go` (round-trip, zero-value, marshal) | `docs/types/identity.md` |
| 1.2 | Error codes | `pkg/errors/codes.go` (REG_001..REG_010, ACT_*, DRV_*, ADP_*, STO_*, REC_*, HA_*), `pkg/errors/classify.go` (severity, retryability) | `errors_test.go` | `docs/errors/codes.md` |
| 1.3 | Registry semantics | `pkg/registry/semantics.go` (Acquire/Release/Ready contract), `pkg/registry/refcount.go` (shared shard primitive) | `semantics_test.go` | `docs/registry/semantics.md` |
| 1.4 | VnetRegistry | `pkg/registry/vnet/{vnet.go, refcount.go}` | unit + integration | `docs/registry/vnet.md` |
| 1.5 | NicRegistry | `pkg/registry/nic/{nic.go, lifecycle.go}` | unit + integration | `docs/registry/nic.md` |
| 1.6 | MappingManager | `pkg/registry/mapping/{mapping.go, index.go}` | unit + integration | `docs/registry/mapping.md` |
| 1.7 | Shared registries | `pkg/registry/{acl,route,meter}/*.go` | unit + integration | `docs/registry/shared-objects.md` |
| 1.8 | Cross-registry integration | `tests/integration/registry_test.go` (vnet→nic→mapping→acl ref refs) | integration | — |
| 1.9 | fm-lint rule | `tools/fm-lint/no_registry_bypass.go` | golden test | `docs/tools/fm-lint.md` |
| 1.10 | Retire `pkg/dm/` | Delete `pkg/dm/`, switch `cmd/fm/main.go` wiring | `go build`, all tests pass | tracker note |

### Acceptance (Wave 1 done)

- All six typed registries implement Acquire/Release/Ready.
- Refcounts increment/decrement correctly; underflow trips REG_007.
- `fm-lint NO_REGISTRY_BYPASS` flags direct struct construction outside `pkg/registry/`.
- `pkg/dm/` deleted; build green; existing test count maintained or grown.

---

## 4. Wave 2 — Adapter (T1/T2)

### Slices

| ID | Slice | Files | Tests |
|---|---|---|---|
| 2.1 | T1 etcd watch | `pkg/adapter/t1/{etcd.go, watch.go}` | unit + integration (testcontainers etcd) |
| 2.2 | T1 dedup + codec | `pkg/adapter/t1/dedup.go`, `pkg/adapter/codec.go` | unit |
| 2.3 | T2 lease | `pkg/adapter/t2/{etcd.go, ha_lease.go}` | unit + integration |
| 2.4 | Watermark / resume | `pkg/adapter/watermark.go` (ADP_008 detection) | unit + integration |
| 2.5 | Retire `pkg/cm/` | Delete `pkg/cm/`, switch wiring | build + tests |

### Acceptance
- T1 watch survives etcd restart (resume via watermark).
- T2 lease acquire/renew tested under HA scenario (two pods, one leases).
- ADP_008 (watermark regress) detected, adapter fails closed per design.

---

## 5. Wave 3 — Driver Interface

### Slices

| ID | Slice | Files | Tests |
|---|---|---|---|
| 3.1 | Driver contract | `pkg/driver/iface/{driver.go, codes.go}` | unit |
| 3.2 | DeltaPlan | `pkg/driver/delta/{plan.go}` | unit (table-driven over goal states) |
| 3.3 | Stub driver | `pkg/driver/stub/stub.go` | unit + integration |
| 3.4 | gRPC driver client | `pkg/driver/grpc/client.go` | unit + integration (mock server) |
| 3.5 | Retry policy | `pkg/driver/iface/retry.go` | unit |
| 3.6 | Retire `pkg/dal/` | Delete `pkg/dal/`, switch wiring | build + tests |

---

## 6. Wave 4 — Actors

### Slices

| ID | Slice | Files | Tests |
|---|---|---|---|
| 4.1 | Supervisor + mailbox | `pkg/actor/{supervisor.go, mailbox.go}` | unit |
| 4.2 | HDO | `pkg/actor/hdo/{hdo.go, session.go, heartbeat.go}` | unit + integration |
| 4.3 | CO | `pkg/actor/co/co.go` | unit |
| 4.4 | NO | `pkg/actor/no/{no.go, goalstate.go, deltaplan.go}` | unit + integration |
| 4.5 | Panic / quarantine | supervisor restart policy | chaos test (ACT_006 simulated) |
| 4.6 | Retire `pkg/gm/` | Delete `pkg/gm/`, switch wiring | build + tests |

---

## 7. Wave 5 — Reconciler + HA

### Slices

| ID | Slice | Files | Tests |
|---|---|---|---|
| 5.1 | Drift classify | `pkg/reconcile/classify/{taxonomy.go, classify.go}` | unit (table-driven) |
| 5.2 | Reconcile loop | `pkg/reconcile/loop.go` | unit + integration |
| 5.3 | SLO clocks | `pkg/reconcile/retry.go` | unit |
| 5.4 | HA lease | `pkg/ha/lease/lease.go` | unit + integration |
| 5.5 | Election | `pkg/ha/election/election.go` | unit + integration |
| 5.6 | Freeze (HA_003) | `pkg/ha/freeze/freeze.go` | unit + chaos |
| 5.7 | HaScope | `pkg/ha/scope.go` | unit |

---

## 8. Wave 6 — Audit + Security

### Slices

| ID | Slice | Files | Tests |
|---|---|---|---|
| 6.1 | Audit log + hash chain | `pkg/audit/log.go` | unit |
| 6.2 | File sink | `pkg/audit/sink/file.go` | unit |
| 6.3 | T2 sink | `pkg/audit/sink/t2.go` | integration |
| 6.4 | SPIFFE SVID | `pkg/security/spiffe/` | unit + integration |
| 6.5 | OPA eval | `pkg/security/opa/` | unit (Rego golden) |
| 6.6 | mTLS | `pkg/security/mtls/` | unit + integration |
| 6.7 | Secret store | `pkg/security/secret/` | unit |

---

## 9. Wave 7 — Tier + Lifecycle + E2E

### Slices

| ID | Slice |
|---|---|
| 7.1 | `pkg/tier/` capability matrix; gated init |
| 7.2 | `pkg/lifecycle/` device state machine |
| 7.3 | Full e2e test: CB → T1 → registry → NO actor → driver → audit |
| 7.4 | `deploy/k8s/` manifests per tier |
| 7.5 | `deploy/helm/` charts |

---

## 10. Wave 8 — Observability + Hardening

### Slices

| ID | Slice |
|---|---|
| 8.1 | `pkg/observability/{log,metric,trace,health}/` refactor under new layout |
| 8.2 | Prometheus dashboards in `deploy/k8s/dashboards/` |
| 8.3 | `cmd/fmctl/` complete (registry/ha/reconcile/driver/audit subcommands per runbooks) |
| 8.4 | Load test harness; performance regression suite |
| 8.5 | Chaos tests: full HA_003 simulation, REG_007 underflow, ADP_005/006 partitions |

---

## 11. Acceptance gates (per wave)

A wave is "done" only when:

1. ✅ All slices closed in tracker.
2. ✅ `go build -v ./...` clean.
3. ✅ `go test -race -v ./...` green.
4. ✅ Each new feature has a doc at `docs/<area>/<feature>.md` with sections: Purpose / Design refs / API surface / Tests / **Future Scopes**.
5. ✅ Master tracker rolled forward.
6. ✅ `docs/next_plan.md` rewritten for the next wave's first slice.
7. ✅ User sign-off on the wave (one-line in tracker: "Wave N closed YYYY-MM-DD by <user>").

---

## 12. Cross-references

- Architecture: `Specs/FM/MASTER_DESIGN_INDEX.md`
- Track B closure: `Specs/FM/inc-closure-2.2-2.3.md`, `Specs/FM/security-design.md`, `Specs/Runbooks/`
- Data model: `Specs/me-and-ai/fm-data-model-sync.md`
- Architecture review: `ARCHITECTURE_REVIEW.md` (status: cleared for implementation)

---

## 13. Change log

| Date | Change |
|---|---|
| 2026-06-22 | Initial plan. Wave 0 opened. |
