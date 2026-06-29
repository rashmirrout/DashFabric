# Next Plan

**Active wave:** 2 (Adapter)
**Active slice:** 2.1 (`pkg/adapter/t1/` — T1 etcd watch)

---

## What just landed (this turn)

- ✅ Slice 1.10 — Retire `pkg/dm/`:
  - `pkg/dm/` deleted entirely; 7 files removed.
  - `cmd/fm/main.go`: Removed dm.DataManager orchestrator; CM→GM→DAL startup chain.
  - `pkg/api/handler.go`: Removed dmManager dependency; endpoints stubbed for Wave 2.
  - Tests: `layer2_test.go`, `api_test.go`, `cm_dm_pipeline_test.go` deleted.
- ✅ Wave 1 closed: All 10 slices 🟢; build green; 64 tests passing.
- ✅ FM binary: 16MB, `go build -v ./...` clean.
- ✅ Registry suite production-ready: VnetRegistry, NicRegistry, MappingRegistry,
  AclGroupRegistry, RouteGroupRegistry, MeterPolicyRegistry; Acquire/Release/Ready
  contract enforced; REG_007 propagates; fm-lint rule live.

---

## What runs next (immediately)

### Slice 2.1 — T1 etcd watch

**Purpose:** Foundation for external config ingestion. A ControlBroker T1 etcd
instance streams config events (VNET/ENI/VIP updates) via etcd `Watch` API.
FM subscribes, deduplicates, validates, and feeds the event stream into the
registry system (Wave 2+) or a test harness (Wave 1 closure).

**Implementation approach — `pkg/adapter/t1/`:**

```
pkg/adapter/t1/
├── etcd.go              # T1Adapter interface + EtcdAdapter implementation
├── watch.go             # Watch stream management, resume semantics
├── etcd_test.go         # unit tests (mock etcd via Txn response)
└── integration_test.go  # integration test (testcontainers etcd)
```

**Core types (`adapter/t1/etcd.go`):**

```go
// T1Adapter defines the ControlBroker T1 etcd watch contract
type T1Adapter interface {
	// Subscribe begins watching the given key prefix.
	// Returns a channel of ConfigEvent and an error.
	// Events flow until context is cancelled or connection breaks.
	Subscribe(ctx context.Context, prefix string) (<-chan ConfigEvent, error)
	
	// Close stops the adapter and releases resources.
	Close() error
}

// EtcdAdapter implements T1Adapter using etcd Watch API
type EtcdAdapter struct {
	client *etcd.Client
	// ... connection/auth fields ...
}

// ConfigEvent represents a single CB T1 config update
type ConfigEvent struct {
	EventID   string // UUID, assigned by ControlBroker
	Timestamp time.Time
	Operation string // "create", "update", "delete"
	Key       string // etcd key (e.g., "/config/vnet/v-001")
	Value     []byte // protobuf-encoded object
	Revision  int64  // etcd revision for ordering
}
```

**Contract & semantics:**

1. **Subscribe(ctx, prefix)**: Open a long-lived Watch stream on etcd at the
   given key prefix. Events flow in order (etcd revision monotonic). If watch
   breaks, resume from last seen revision (persisted via Slice 2.4 watermark).
2. **Dedup via revision**: Events with the same revision are duplicates;
   skip. Events with increasing revisions are unique.
3. **Connection resilience**: etcd client handles reconnect automatically
   (via grpc-keepalive). Adapter layer does NOT retry; responsibility of
   caller to re-Subscribe.
4. **Error handling**: Transient etcd errors (network, timeouts) surface via
   error returns from Subscribe; caller decides on backoff/retry.
5. **Graceful close**: Close() drains pending events and releases the etcd
   connection.

**Tests (`adapter/t1/etcd_test.go`):**

- TC-T1-001: Subscribe succeeds; events flow in revision order.
- TC-T1-002: Duplicate event (same revision) skipped.
- TC-T1-003: etcd connection error → Subscribe returns error.
- TC-T1-004: Context cancellation stops event flow.
- TC-T1-005: Close() drains pending events.

**Integration test (`adapter/t1/integration_test.go`):**

- TC-T1-INT-001: Real etcd instance (testcontainers); Subscribe to live
  events; inject events via etcd CLI; verify event flow and ordering.

**Doc to write:** `docs/adapter/t1.md`
- T1 watch semantics, revision-based dedup, connection resilience, integration
  path to Wave 2 reconciler, 4 Future Scopes (rate limiting, priority events,
  namespace sharding, event filtering DSL).

---

## Wave 1 acceptance criteria (VERIFIED ✅)

- ✅ All 10 slices closed in tracker.
- ✅ `go build -v ./...` clean.
- ✅ `go test -race -v ./...` green (64 tests, no races).
- ✅ Each feature has doc: `docs/registry/{vnet,nic,mapping,shared-objects}.md` +
  `docs/registry/semantics.md` + `docs/tools/fm-lint.md`.
- ✅ `docs/trackers/00-MASTER_TRACKER.md` rolled forward; Wave 1 row: 🟢 Done,
  10/10, closed 2026-06-28.
- ✅ `docs/next_plan.md` rewritten for Wave 2, Slice 2.1.

---

## Wave 2 overview (Adapter T1/T2)

| Slice | Description | Est. LOC | Tests |
|---|---|---|---|
| 2.1 | T1 etcd watch (this) | 200–300 | unit + integration |
| 2.2 | T1 dedup + codec | 150–200 | unit |
| 2.3 | T2 lease + state | 250–350 | unit + integration |
| 2.4 | Watermark / resume | 100–150 | unit + integration |
| 2.5 | Retire `pkg/cm/` | — | build + tests |
| **Total** | | ~900–1100 | 30+ new tests |

**Wave 2 closes when:**
- T1 watch survives etcd restart (resume via watermark).
- T2 lease acquire/renew tested under HA scenario (two pods, one leases).
- ADP_008 (watermark regress) detected, adapter fails closed.
- `pkg/cm/` retired; Wave 3 (driver) begins.

---

## Risks & blockers

**None known.** Slice 2.1 is self-contained and has no deep dependencies.
etcd testcontainers support in Go is well-established (`testcontainers-go`).

---

## Key files to create

| Path | Purpose |
|---|---|
| `pkg/adapter/t1/etcd.go` | T1Adapter interface + EtcdAdapter impl |
| `pkg/adapter/t1/watch.go` | Watch stream + event channel management |
| `pkg/adapter/t1/etcd_test.go` | 5 unit tests |
| `pkg/adapter/t1/integration_test.go` | 1 integration test (testcontainers) |
| `docs/adapter/t1.md` | Watch semantics, integration path, Future Scopes |
| `go.mod` | Add `github.com/coreos/go-etcd` (etcd client) |

---

## Update protocol

This file is rewritten **every turn** to describe the next concrete slice.
The previous next_plan is preserved in git history.
Tracker (`00-MASTER_TRACKER.md`) accumulates and is appended, not rewritten.
