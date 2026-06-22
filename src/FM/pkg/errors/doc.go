// Package errors is the single source of truth for FM error codes,
// their severity classification, retryability, and the path to the
// corresponding operator runbook.
//
// Code namespaces:
//
//   - REG_* — registry errors (refcount, lookup, cache OOM)
//   - ACT_* — actor errors (panic, mailbox overflow, invariant break)
//   - DRV_* — driver errors (transient, permanent, version mismatch)
//   - ADP_* — adapter errors (T1/T2 unreachable, watermark regress)
//   - STO_* — storage errors (OOM, corruption, quorum loss)
//   - REC_* — reconciler errors (unknown drift, persistent drift)
//   - HA_*  — high-availability errors (split-brain, lease lost)
//
// Each code has:
//
//   - Code     (string, stable identifier — e.g. "REG_007")
//   - Severity (INFO / WARN / ERROR / CRITICAL)
//   - Retryable (bool with a backoff hint)
//   - Runbook   (path to Specs/Runbooks/<code>.md for CRITICAL codes)
//
// Note: this package shadows the stdlib "errors" by import path, not by
// name — code that needs both imports this as `fmerrors "github.com/
// dashfabric/fm/pkg/errors"`.
//
// Design references:
//   - Specs/FM/error-handling-design.md (canonical code list, §9.2)
//   - Specs/Runbooks/README.md (runbook index)
package errors
