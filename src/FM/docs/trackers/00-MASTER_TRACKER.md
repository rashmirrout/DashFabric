# FM Implementation — Master Tracker

**Last updated:** 2026-06-22
**Active wave:** 1 (Registries) — Wave 0 closed
**Tier target:** Hyperscale

---

## Roll-up

| Wave | Theme | Status | Slices Done | Slices Total | Notes |
|---|---|---|---|---|---|
| 0 | Scaffold & Plan | 🟢 Done | 3 | 3 | Closed 2026-06-22 |
| 1 | Registries | ⚪ Not started | 0 | 10 | Replaces `pkg/dm/` |
| 2 | Adapter | ⚪ Not started | 0 | 5 | Replaces `pkg/cm/` |
| 3 | Driver iface | ⚪ Not started | 0 | 6 | Replaces `pkg/dal/` |
| 4 | Actors | ⚪ Not started | 0 | 6 | Replaces `pkg/gm/` |
| 5 | Reconciler + HA | ⚪ Not started | 0 | 7 | |
| 6 | Audit + Security | ⚪ Not started | 0 | 7 | |
| 7 | Tier + Lifecycle + E2E | ⚪ Not started | 0 | 5 | |
| 8 | Observability + Hardening | ⚪ Not started | 0 | 5 | |

**Legend:** 🟢 Done · 🟡 In progress · ⚪ Not started · 🔴 Blocked

---

## Wave 0 — Scaffold & Plan

| Slice | Description | Files | Tests | Doc | Status |
|---|---|---|---|---|---|
| 0.1 | doc.go stubs for 12 new packages | `pkg/{registry,actor,adapter,driver,reconcile,ha,audit,security,lifecycle,tier,errors,types}/doc.go` | n/a | inline | 🟢 |
| 0.2 | Plan + tracker + next_plan | `docs/IMPLEMENTATION_PLAN.md`, `docs/trackers/00-MASTER_TRACKER.md`, `docs/next_plan.md` | n/a | self | 🟢 |
| 0.3 | Build sanity | `go build ./cmd/fm` clean, `go vet ./...` clean, `go test ./...` pass | build + existing tests | — | 🟢 |

---

## Wave 1 — Registries (planned)

| Slice | Description | Status |
|---|---|---|
| 1.1 | `pkg/types/` — ID aliases, versions | ⚪ |
| 1.2 | `pkg/errors/` — all error codes + classify | ⚪ |
| 1.3 | `pkg/registry/semantics.go`, `refcount.go` | ⚪ |
| 1.4 | `pkg/registry/vnet/` — VnetRegistry | ⚪ |
| 1.5 | `pkg/registry/nic/` — NicRegistry | ⚪ |
| 1.6 | `pkg/registry/mapping/` — MappingManager | ⚪ |
| 1.7 | `pkg/registry/{acl,route,meter}/` — shared object registries | ⚪ |
| 1.8 | Cross-registry integration test | ⚪ |
| 1.9 | `tools/fm-lint/` — NO_REGISTRY_BYPASS rule | ⚪ |
| 1.10 | Retire `pkg/dm/` | ⚪ |

---

## Wave 2+ — see IMPLEMENTATION_PLAN.md §§4–10

---

## Migration progress (legacy → new)

| Legacy package | Replacement wave | Status |
|---|---|---|
| `pkg/cm/` | Wave 2 (adapter) | Active, not yet replaced |
| `pkg/dm/` | Wave 1 (registries) | Active, not yet replaced |
| `pkg/gm/` | Wave 4 (actors) | Active, not yet replaced |
| `pkg/dal/` | Wave 3 (driver) | Active, not yet replaced |
| `pkg/api/` | Wave 7/8 (REST surface) | Active |
| `pkg/resilience/` | Wave 3 / Wave 5 (split) | Active |
| `pkg/config/` | Kept | Active, refined in Wave 7 |
| `pkg/observability/` | Wave 8 (subdivide into log/metric/trace/health) | Active |
| `pkg/testutil/` | Kept, expanded each wave | Active |

---

## Sign-offs

| Wave | Closed date | Closed by | Notes |
|---|---|---|---|
| 0 | 2026-06-22 | pending user sign-off | Build green; race detector unavailable on this host (gcc not on PATH) — re-run on CI |
| 1 | — | — | |
| 2 | — | — | |
| 3 | — | — | |
| 4 | — | — | |
| 5 | — | — | |
| 6 | — | — | |
| 7 | — | — | |
| 8 | — | — | |

---

## Open risks / decisions

| Date | Topic | Status |
|---|---|---|
| 2026-06-22 | CB ID format coupling (FM inherits CB's ID scheme) | Accepted (see `Specs/me-and-ai/fm-data-model-sync.md` §3) |
| 2026-06-22 | Tier specialization deferred to Wave 7 (default hyperscale) | Accepted |
| 2026-06-22 | gRPC vs in-proc default driver: stub for tests, gRPC for prod | Accepted |
| 2026-06-22 | Race detector not runnable locally (no gcc) — must run in CI before each wave close | Open |
