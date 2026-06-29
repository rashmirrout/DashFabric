package types

// SpecRevision is a monotonic per-object revision number carried on every
// NicSpec, VnetSpec, etc. emitted by the Control Bridge. FM uses it to
// detect stale writes and to enforce the spec_revision monotonicity
// invariant (Specs/FM/inc-closure-2.2-2.3.md §3).
//
// Comparison is total: a < b means a is strictly older than b.
type SpecRevision int64

// Newer reports whether s is strictly newer (greater) than other.
// Equal revisions are NOT newer — caller chooses how to treat ties.
func (s SpecRevision) Newer(other SpecRevision) bool { return s > other }

// Epoch is a driver-session epoch. It is bumped on every reconnect to a
// device. Apply results stamped with a stale epoch are discarded
// (Specs/FM/southbound-driver-interface-redesign.md §4).
type Epoch int64

// Bumped reports whether e is a strictly later session than other.
func (e Epoch) Bumped(other Epoch) bool { return e > other }

// Watermark is an etcd revision (T1 or T2). Watermarks may only advance —
// a regression indicates either split-brain or backup-restore drift and
// must trip ADP_008 (Specs/Runbooks/ADP_008_WATERMARK_REGRESSION.md).
type Watermark int64

// Regressed reports whether w is strictly older than prev. A true result
// is a hard fault, not a recoverable condition.
func (w Watermark) Regressed(prev Watermark) bool { return w < prev }
