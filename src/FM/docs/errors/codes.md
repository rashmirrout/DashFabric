# pkg/errors — Canonical Error Catalog

**Wave / Slice:** 1 / 1.2
**Status:** 🟢 Landed 2026-06-22
**Package:** `github.com/dashfabric/fm/pkg/errors`
(import as `fmerrors "github.com/dashfabric/fm/pkg/errors"`)

---

## What this is

A Go mirror of `Specs/FM/error-handling-design.md §3`. 61 codes, every
one classified by severity, recoverability, blast radius, and (for
CRITICAL only) a runbook path.

No business logic. No I/O. Pure lookup.

---

## Layout

```
pkg/errors/
├── doc.go        # package overview (slice 0.1)
├── codes.go      # 61 Code constants in 8 namespaces
├── classify.go   # Severity / Recoverability / Blast enums + catalog map + Classify()
└── codes_test.go # catalog invariants
```

---

## Namespaces

| Prefix | Layer | Codes |
|---|---|---|
| `API_` | Gateway, REST/gRPC | 8 |
| `REG_` | Registries | 8 |
| `ACT_` | Actors (HDO, CO, NO) | 8 |
| `DRV_` | Southbound driver | 10 |
| `ADP_` | Adapter (T1/T2) | 8 |
| `STO_` | Storage tiers | 6 |
| `REC_` | Reconciler | 8 |
| `HA_`  | High availability | 5 |

---

## Naming convention

Constant name = string value, e.g.

```go
const REG_007_REFCOUNT_UNDERFLOW Code = "REG_007_REFCOUNT_UNDERFLOW"
```

This is deliberate. The constant in code, the string in a log line,
the metric label, and the runbook filename are all the same token —
`grep REG_007_REFCOUNT_UNDERFLOW` finds every reference. Worth the
departure from Go's CamelCase convention because the catalog is a
fixed vocabulary, not a domain model.

---

## Classification

Every code is classified along four axes:

### Severity (alert priority)
- `INFO` — telemetry only
- `WARN` — investigate when convenient
- `ERROR` — needs attention; budget impact
- `CRITICAL` — page a human; runbook required

### Recoverability (retry policy)
- `N/A` — informational; no action
- `TRANSIENT` — auto-retry with backoff (100ms → 10s, 10% jitter — universal policy)
- `PERMANENT` — stop retrying; quarantine
- `OPERATOR` — stop retrying; alert with runbook

`Info.Retryable()` returns true iff `Recoverability == TRANSIENT`. Backoff
parameters are not encoded per-code; the universal policy applies.

### Blast (max scope of one occurrence)
`SINGLE_ENI`, `SINGLE_DEVICE`, `SHARD`, `POD`, `HA_SCOPE`, `CLUSTER`

### Runbook
Path under `Specs/Runbooks/<code>.md`. **Required for CRITICAL, forbidden
for everything else** — enforced in `codes_test.go`. The repo has 13
runbooks today, matching the 13 CRITICAL codes.

---

## API

```go
import fmerrors "github.com/dashfabric/fm/pkg/errors"

info := fmerrors.Classify(fmerrors.REG_007_REFCOUNT_UNDERFLOW)
// info.Severity       == SeverityCritical
// info.Recoverability == RecovPermanent
// info.Retryable()    == false
// info.Runbook        == "Specs/Runbooks/REG_007_REFCOUNT_UNDERFLOW.md"

// Unknown codes do not panic — they return a sentinel:
info = fmerrors.Classify(fmerrors.Code("REG_999_NOT_REAL"))
// info.Code     == fmerrors.Unknown
// info.Severity == SeverityError
// info.Retryable() == false

// Pre-register every code with a metrics system:
for _, c := range fmerrors.All() { ... }
```

---

## Test invariants (`codes_test.go`)

- Catalog key matches `Info.Code` (no copy-paste drift).
- Exactly 61 codes (spec count).
- No duplicate code strings.
- Every code has a non-empty `Meaning`.
- Every CRITICAL code has a runbook starting with `Specs/Runbooks/` and ending in `.md`.
- Non-CRITICAL codes have **no** runbook (catches over-promotion).
- `Classify` returns Info for known codes; sentinel for unknowns.
- `Retryable()` is true iff Recoverability is TRANSIENT.

---

## Future Scopes

- **Spec-to-Go lint.** A `tools/fm-doclint` pass that parses
  `error-handling-design.md §3` tables and diffs against `catalog` will
  catch silent drift (new code added to spec, forgotten here). Targeted
  for Wave 6 alongside the audit/security work.
- **Runbook existence check.** Today `TestCriticalCodes_HaveRunbookPath`
  only checks string shape. A Wave 6 test will `os.Stat` each path under
  `Specs/Runbooks/` to verify the file exists.
- **Per-code metric pre-registration.** Once `pkg/observability/metrics/`
  lands (Wave 8), every code in `All()` should pre-register a counter so
  Prometheus exports include zero-valued series and dashboards never go
  "no data".
- **Wrap-style error type.** Today this package only classifies codes. A
  `type Err struct { Code; Cause error; Context map[string]any }` with
  `Error()`, `Unwrap()`, and `errors.Is` support would let call sites
  return a typed value instead of a `(Code, error)` pair. Defer until a
  call site actually needs it — premature now.
- **Reverse map: text → Code.** Useful when parsing DLQ entries or
  audit logs. Add when the audit slice (Wave 6) needs it.

---

## Cross-references

- `Specs/FM/error-handling-design.md §3` — canonical catalog (source of truth).
- `Specs/FM/error-handling-design.md §9.2` — runbook requirement.
- `Specs/Runbooks/README.md` — runbook index.
- `pkg/errors/doc.go` — package-level overview.
