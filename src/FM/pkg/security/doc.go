// Package security is FM's identity, authorization, transport-security,
// and secret-management layer.
//
// Subpackages:
//
//   - spiffe — SPIFFE SVID acquisition and rotation. Every FM pod runs
//              with a verifiable workload identity.
//   - opa    — Open Policy Agent integration. RBAC + ABAC decisions
//              for fmctl commands, driver Apply ops, and adapter writes
//              go through OPA-evaluated Rego policies.
//   - mtls   — mTLS configuration for inter-pod, driver-gRPC, and T2
//              etcd connections. Cert rotation is automatic; manual
//              rotation steps are in ADP_005 runbook.
//   - secret — Secret-store interface: CSI driver, HashiCorp Vault, or
//              Kubernetes Secret. FM never reads secrets from disk
//              directly; this package mediates.
//
// Design references:
//   - Specs/FM/security-design.md (full threat model and controls)
//   - Specs/Runbooks/ADP_005_T1_UNREACHABLE.md (cert rotation path)
package security
