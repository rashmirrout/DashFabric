// Package adapter is FM's northbound bridge to durable stores.
//
// FM's storage is split across two tiers:
//
//   - T1 (config plane): Control Bridge's etcd, watch-driven. Holds
//     authoritative intent (NicSpec, VnetSpec, etc.). FM is a read-only
//     consumer of T1.
//   - T2 (operational plane): FM-owned etcd. Holds HA leases, audit
//     log, and operational metadata. FM reads and writes T2.
//
// T3 (observed state) lives in pkg/registry/ — not in this package.
//
// Subpackages:
//
//   - t1  — etcd v3 client + watch + lease handling for the CB config store.
//   - t2  — etcd v3 client + lease for FM's own ops store.
//
// Shared:
//
//   - codec.go     — proto3 ↔ bytes round-trips.
//   - watermark.go — resume tokens, monotonicity enforcement (ADP_008
//                    detection: watermark must never regress).
//
// Design references:
//   - Specs/FM/adapter-protocol-design.md
//   - Specs/FM/storage-architecture.md
//   - Specs/Runbooks/ADP_005_T1_UNREACHABLE.md
//   - Specs/Runbooks/ADP_006_T2_UNREACHABLE.md
//   - Specs/Runbooks/ADP_008_WATERMARK_REGRESS.md
//   - Specs/Runbooks/STO_002_T1_OOM.md
//   - Specs/Runbooks/STO_005_T3_CORRUPTION.md
package adapter
