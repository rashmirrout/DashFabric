// Package types holds cross-package shared scalar types — primarily
// named string aliases for IDs.
//
// Why named string types (not plain string, not struct):
//
//   - Type safety: func F(ENIID, VnetID) won't compile if you pass them
//     in the wrong order.
//   - Zero runtime cost: still just a string under the hood.
//   - Self-documenting: the function signature carries the intent.
//   - JSON / proto compatibility: marshals as a plain string.
//
// IDs in FM are opaque. FM does not synthesize, parse, or rotate them
// (per Specs/me-and-ai/fm-data-model-sync.md §3). All IDs originate at
// the Control Bridge and flow through FM unchanged.
//
// Versioning types:
//
//   - SpecRevision — monotonic int64 carried on every NicSpec (and
//                    similar). Used to detect stale writes and to
//                    enforce the spec_revision monotonicity invariant.
//   - Epoch        — driver-session epoch, bumped on every reconnect.
//   - Watermark    — etcd revision; the only thing that may never
//                    regress (ADP_008).
//
// Design references:
//   - Specs/FM/inc-closure-2.2-2.3.md §3 (NicSpec schema)
//   - Specs/me-and-ai/fm-data-model-sync.md §3 (ID convention)
package types
