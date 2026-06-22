// Package audit emits FM's immutable, hash-chained audit log.
//
// Every state-mutating operation in FM — registry Acquire/Release,
// driver Apply, reconciler adopt/enforce/quarantine, HA freeze/yield,
// security policy decisions — produces an audit record. Records are
// hash-chained (each record carries the SHA256 of the previous record)
// so any in-place edit is detectable.
//
// Subpackages:
//
//   - sink — destinations (file, T2 etcd, remote SIEM via gRPC/HTTP).
//
// Top-level files:
//
//   - log.go — Append API + hash chain bookkeeping.
//
// Design references:
//   - Specs/FM/security-design.md §7 (audit log integrity)
//   - Specs/FM/error-handling-design.md (audit-log entries per error code)
package audit
