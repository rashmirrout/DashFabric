# Next Plan

**Active wave:** 0 (Scaffold & Plan)
**Active slice:** 0.3 next → 1.1 immediately after

---

## What just landed (this turn)

- ✅ Folder structure agreed (see `IMPLEMENTATION_PLAN.md §1`).
- ✅ Data-model sync recorded (`Specs/me-and-ai/fm-data-model-sync.md`).
- ✅ 12 `doc.go` stubs created — new `pkg/` packages exist on disk.
- ✅ `IMPLEMENTATION_PLAN.md`, `00-MASTER_TRACKER.md`, `next_plan.md` live.
- 🟡 Build sanity check pending (this turn).

## What runs next (immediately)

### Slice 0.3 — Build sanity (closes Wave 0)

- Run `go build -v ./cmd/fm` in `src/FM/`.
- Run `go test -race -v ./...` (existing 64 tests must still pass).
- If green → mark Wave 0 closed in tracker.
- If red → diagnose; do not proceed to Wave 1 until clean.

### Slice 1.1 — `pkg/types/` (opens Wave 1)

**Files to create:**

```
src/FM/pkg/types/
├── ids.go          # ENIID, VnetID, RouteGroupID, AclGroupID, MappingID, MeterPolicyID, HaScopeID, DeviceID, TenantID
├── versions.go     # SpecRevision (monotonic int64), Epoch (int64), Watermark (etcd revision)
├── ids_test.go     # zero-value, marshal/unmarshal, equality, validation
└── doc.go          # (already created in slice 0.1)
```

**Design rules for IDs (per `Specs/me-and-ai/fm-data-model-sync.md §3`):**

- Each ID is a **named string** type (`type ENIID string`, not `type ENIID struct{ ID string }`).
- Reason: type-safety against argument-order mistakes (`func F(ENIID, VnetID)` won't compile if you pass them swapped) at zero runtime cost.
- IDs are opaque to FM. No parsing, no synthesis, no format assumptions inside FM.
- Constructors accept a `string` (typically from CB) and return the typed ID after a light non-empty check.
- Validation rule: `len > 0 && len ≤ 256 && !contains(' ', '\t', '\n', '\0')` — anything else is CB's contract.

**Tests:**

- Zero value is invalid (empty string).
- Boundary length validation (1, 256, 257).
- Round-trip through JSON.
- Equality and map-key behavior.
- Cross-type assignment is a compile error (verified via `go vet` golden file or example_test.go that won't compile if commented out — document as "intentional compile failure").

**Doc to write:** `docs/types/identity.md` covering:
- Why typed string aliases (not struct, not interface).
- CB-source contract.
- **Future Scopes:** ID rotation policy if CB ever needs it; format-version field on NicSpec for forward compat.

### Slice 1.2 — `pkg/errors/` (after 1.1)

Will pull all CRITICAL + non-CRITICAL error codes from `Specs/FM/error-handling-design.md` into Go constants, with classification (severity, retryable, runbook path).

---

## What is NOT happening next

- No work in `pkg/cm/`, `pkg/dm/`, `pkg/gm/`, `pkg/dal/` — those stay frozen until their replacement is ready.
- No `cmd/fm/main.go` changes yet (Wave 1 slice 1.10 does the swap).
- No tier specialization yet (Wave 7).
- No new observability subdivision yet (Wave 8).

---

## Risks for the next slice

- **Type alias vs distinct type:** Go's `type X string` is a *distinct* type (good for safety), but JSON marshalling defaults to string — fine. If we ever want UUID validation, that goes in CB, not here.
- **No ID minted by FM:** If a slice author is tempted to generate a synthetic ID inside `pkg/types/`, that violates `fm-data-model-sync.md §3`. The `fm-lint` rule landing in Slice 1.9 will catch this for registries but not for `types/`. We rely on review for `types/`.

---

## Update protocol

This file is rewritten **every turn** to describe the next concrete slice. The previous next_plan is preserved in git history.

Tracker (`00-MASTER_TRACKER.md`) accumulates and is appended, not rewritten.
