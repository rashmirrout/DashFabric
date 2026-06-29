# pkg/registry — Shared Semantics & Refcount

**Wave / Slice:** 1 / 1.3
**Status:** 🟢 Landed 2026-06-23
**Package:** `github.com/dashfabric/fm/pkg/registry`

---

## What this is

The shared in-memory contract that every per-object registry composes:
`Acquire`, `Release`, `Add`, `Get`, `Ready`, `Refcount`, `Len`,
`Snapshot`. One generic `Registry[K, V]` plus a refcount table with
REG_007 underflow detection.

This is the **Wave 1** contract — synchronous and in-process. The full
T1-backed contract (subscription with `Updates`/`Err` channels,
debounce timer, hydration timeout) described in
`Specs/FM/registry-semantics-exact.md` will be layered on in Wave 2
when the adapter and T1 storage exist. Scoping it tight here keeps the
foundation testable without needing etcd up.

---

## Layout

```
pkg/registry/
├── doc.go              # package overview (slice 0.1)
├── semantics.go        # Registry[K,V] type + public API
├── refcount.go         # entry record + UnderflowError + helpers
└── registry_test.go    # behavioural tests
```

---

## API

```go
import (
    "context"
    "errors"

    "github.com/dashfabric/fm/pkg/registry"
)

r := registry.New[string, MyValue]("vnet")

// Producer (adapter writes value from T1 into the registry):
r.Add("vnet-42", MyValue{...})

// Consumer (actor needs the value, signals interest, waits for hydration):
ready := r.Acquire(ctx, "vnet-42")
defer func() {
    if err := r.Release("vnet-42"); err != nil {
        // errors.Is(err, registry.ErrRefcountUnderflow) — caller bug
    }
}()

select {
case <-ready:
    v, _ := r.Get("vnet-42")
    use(v)
case <-ctx.Done():
    return ctx.Err()
}
```

`Acquire` never blocks. The returned channel is already closed if the
key has a value when Acquire runs; otherwise it closes on the first
`Add` for that key.

---

## Lifecycle

```
state           refs    hasValue    next event drives →
─────────────── ─────   ─────────   ──────────────────────────────────
(absent)        —       —           Acquire → cold; Add → cold-write
cold            ≥1      false       Add → ready
cold-write      0       true        Acquire → hot
hot             ≥1      true        Release(refs→0) → evicted
evicted         —       —           same as absent
```

`Acquire` is the only operation that increments `refs`.
`Release` is the only operation that decrements `refs`.
`Add` never touches `refs` (Add can happen before Acquire — "cold-write").
Eviction is immediate when refs reaches zero (Wave 2 adds the 30s
grace per `registry-semantics-exact.md §5`).

---

## REG_007 underflow

`Release` is the canonical underflow gate. Two failure modes both
return `*UnderflowError` carrying `REG_007_REFCOUNT_UNDERFLOW`:

1. **Double-release** — caller called Release more times than Acquire.
2. **Phantom release** — caller called Release for a key that was
   never Acquired.

Both are caller-correctness bugs of the same severity. The error
embeds the registry name and the offending key so an operator reading
the log can localise the bug without grepping for pointer values:

```
REG_007_REFCOUNT_UNDERFLOW: registry=vnet key=vnet-42
```

On the error path the refcount is **not mutated** — a buggy caller
that retries Release won't drive the count further negative. The
sentinel `ErrRefcountUnderflow` is provided for `errors.Is`:

```go
if errors.Is(err, registry.ErrRefcountUnderflow) {
    // caller bug — emit ACT_007 or quarantine the actor
}
```

REG_007 is classified CRITICAL (Severity=CRITICAL, Recoverability=
PERMANENT) by `pkg/errors`, with runbook
`Specs/Runbooks/REG_007_REFCOUNT_UNDERFLOW.md`.

---

## Concurrency model

One `sync.RWMutex` per `Registry` instance protects all entries.
- `Get`, `Ready`, `Refcount`, `Len`, `Snapshot` take RLock.
- `Acquire`, `Release`, `Add` take WLock.

No I/O occurs while the lock is held (Wave 2 will tighten this
explicitly when the T1 watch is wired up). The lock-order rule across
multiple registries (`Specs/FM/registry-semantics-exact.md §8.2`) is
the caller's responsibility — actors take their fixed
GlobalRegistry → VnetRegistry → MappingRegistry → GroupRegistry →
HaRegistry order to avoid cross-registry deadlock.

The race detector is the real arbiter here; CI runs `go test -race`
before each wave close. Local Windows hosts without CGO can skip.

---

## Test invariants (`registry_test.go`)

- Cold Acquire returns open Ready; closes on next Add.
- Hot Acquire (value already present) returns closed Ready.
- Multiple subscribers share the same Ready channel.
- N Acquires + N Releases evicts the entry.
- Underflow returns REG_007, doesn't mutate refcount, propagates
  registry name and key in the error.
- Phantom release (never-acquired key) returns REG_007 too.
- Add is idempotent — second Add overwrites value, leaves Ready
  closed-but-not-double-closed, doesn't touch refcount.
- Cold-write (Add before any Acquire) creates a refs==0 cache entry.
- Snapshot is an independent copy.
- Snapshot omits Acquired-but-not-Added entries.
- 200-goroutine concurrent Acquire/Add/Get drives no deadlocks; full
  drain returns Len()==0.

---

## Future Scopes

- **T1 watch + Subscription type (Wave 2).** Replace the bare
  `<-chan struct{}` returned by Acquire with the full `Subscription[V]`
  from `registry-semantics-exact.md §2` (Initial, Updates, Ready, Err
  fields). Today's Ready-only return is forward-compatible because the
  Subscription's Ready field has the same semantics.
- **Debounce timer on Release-to-zero (Wave 2).** Per §5, hold the
  entry for `GracePeriod` (default 30s) before evicting; an Acquire
  inside the grace cancels the eviction. Critical for NIC churn
  workloads where Acquire/Release/Acquire happens in milliseconds.
- **Hydration timeout (Wave 2).** Per §4.1, close Ready and emit
  `HYDRATION_TIMEOUT` if T1 hasn't populated within
  `HydrationTimeout` (default 10s).
- **Per-registry metrics (Wave 8).** Counters + histograms from
  `registry-semantics-exact.md §10`: acquire_total{result=hit|cold|
  debounce_rescue}, hydration_latency_seconds, etc. Today the
  `Refcount`/`Len` accessors give tests visibility; the metric layer
  will subscribe to the same accessors.
- **Cross-registry lock-order linter (Wave 1.9).** The fm-lint
  `NO_REGISTRY_BYPASS` rule will additionally enforce the
  GlobalRegistry → VnetRegistry → MappingRegistry → GroupRegistry →
  HaRegistry acquire order in actor code.
- **Snapshot streaming (Wave 6).** Today `Snapshot` materialises the
  full map at once — fine for in-cluster sizes. The audit-log slice
  will need a chunked iterator that doesn't allocate the whole map at
  once. Defer until measured pressure justifies it.
- **Generic value-equality / cold-write deduplication.** The current
  contract overwrites on every Add. A future contract could short-
  circuit identical writes via a `V.Equal(V) bool` constraint. Defer
  until profiling shows the writes are hot.

---

## Cross-references

- `Specs/FM/registry-semantics-exact.md` §3–§8 — full subscription
  contract (Wave 2 target).
- `Specs/FM/registry-go-skeleton.md` — per-object registry skeletons
  (Wave 1.4–1.7 consumers).
- `Specs/FM/error-handling-design.md §3` — REG_007 catalog entry.
- `Specs/Runbooks/REG_007_REFCOUNT_UNDERFLOW.md` — operator response.
- `pkg/errors/classify.go` — `REG_007_REFCOUNT_UNDERFLOW` constant.
- `pkg/registry/doc.go` — package-level overview.
