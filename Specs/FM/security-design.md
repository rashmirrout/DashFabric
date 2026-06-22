# Security Architecture: Trust, Identity, Crypto, and Hardening

**Status:** AUTHORITATIVE
**Owner:** FM Architecture
**Supersedes:** scattered "TLS" / "auth" mentions across HLD/LLD
**Date:** 2026-06-22

---

## 1. PURPOSE

Resolves `ARCHITECTURE_REVIEW.md` Track B (security spec). Defines:

1. Trust model: who trusts whom, by what credential
2. Identity issuance and rotation (workload + device + operator)
3. Transport security (mTLS everywhere, ciphers, version floors)
4. Authorization (RBAC matrix, scopes, policy evaluation)
5. Secrets management (KMS, sealed secrets, in-process handling)
6. Audit & forensic logging
7. Threat model (attacker categories, abuse cases, mitigations)
8. Hardening checklist (container, kernel, syscall, network policy)

**Implementation contract:** No security control may be silently disabled. Every relaxation requires an explicit override flag, audit log entry, and time-bounded exception ticket.

---

## 2. TRUST DOMAINS

```
            ┌────────────────────────────────────────────────────────┐
            │              Operator Plane (humans)                   │
            │   kubectl, API gateway clients, dashboards, runbooks   │
            └─────────────────┬──────────────────────────────────────┘
                              │ mTLS + JWT (issued by OIDC IdP)
            ┌─────────────────▼──────────────────────────────────────┐
            │                FM Control Plane                        │
            │   API gateway · FM pods · Adapter pods · Sweepers      │
            │   Workload identity via SPIFFE SVID                    │
            └────────┬────────────────────┬──────────────────────────┘
                     │                    │
                     │ mTLS               │ mTLS + lease
                     │                    │
                     ▼                    ▼
            ┌────────────────────┐ ┌────────────────────────────────┐
            │  T1/T2 (etcd)      │ │  CB Plugins (vendor-supplied)  │
            │  T3 (RocksDB)      │ │  Run as untrusted code         │
            └────────────────────┘ └─────────────┬──────────────────┘
                                                  │ mTLS (SAN-pinned)
                                                  ▼
                                       ┌────────────────────────┐
                                       │   DASH Devices (DPUs)  │
                                       │   gNMI / SAI / vendor  │
                                       └────────────────────────┘
```

Boundaries:

| Boundary | Inside Threat Model? | Why |
|----------|---------------------|-----|
| Operator → API gateway | Yes | Tenant operators may be compromised |
| FM pod → FM pod (intra-cluster) | Limited | Cluster-internal; assumes K8s network policy enforced |
| FM → etcd | Yes | Compromised pod must not be able to pivot to other shards |
| FM → CB plugin | **Yes** | CB plugins are vendor code — TREATED AS UNTRUSTED |
| FM → Device | Yes | Network-attached; device could be hostile (lab/staging) |
| Device → FM (reverse RPC) | Yes | Devices initiate registration |

---

## 3. IDENTITY MODEL

### 3.1 Identity Types

| Subject | Identity Format | Issuer | Lifetime | Rotation |
|---------|----------------|--------|----------|----------|
| FM pod (workload) | SPIFFE SVID (`spiffe://dashfabric.local/fm/<shard_id>`) | SPIRE server | 1 hour | Automatic (every 30 min) |
| Adapter pod | SPIFFE SVID (`spiffe://dashfabric.local/adapter/<pod_id>`) | SPIRE server | 1 hour | Automatic |
| CB plugin instance | SPIFFE SVID (`spiffe://dashfabric.local/cb/<vendor>/<pod_id>`) | SPIRE server | 1 hour | Automatic |
| Device (DPU) | X.509 leaf cert; CN = `device_id`; SAN URI = `device://<region>/<device_id>` | Per-region intermediate CA | 30 days | Pre-expiry renewal at 75% |
| Operator (human) | OIDC ID token + short-lived workload cert | Corporate IdP + Vault | Token 1 h; cert 8 h | On expiry |
| Service account (CI, runbook automation) | Workload SVID with `serviceaccount` audience | SPIRE | 4 hours | Per session |

### 3.2 Device Onboarding (Bootstrap Trust)

Devices arrive without an identity. Bootstrap:

1. Operator generates per-device **enrollment token** (one-time, 15-min TTL, scoped to a region + intended `device_class`)
2. Device boots, presents enrollment token to registration endpoint over TLS (server-auth only at this step)
3. FM gateway validates token signature against KMS-held key; checks not consumed
4. FM issues X.509 leaf cert (private key generated device-side; CSR submitted)
5. Enrollment token marked consumed in T2 (`/fm/v1/enrollment_tokens/<token_id>`, TTL 90 days for audit)
6. Subsequent requests use mTLS with the issued cert

**Anti-replay:** Enrollment tokens are SINGLE-USE; consumption is CAS-protected in T2.

### 3.3 Identity Revocation

| Scenario | Action | Recovery |
|----------|--------|----------|
| Pod compromise suspected | Revoke SVID via SPIRE; force restart | New SVID on next start |
| Device compromised | Add device cert serial to CRL in T2; HDO transitions to `REVOKED` | Operator runbook |
| Operator credential lost | Revoke at IdP; revoke active sessions in T2 | Re-issue via IdP flow |
| CA compromised | Emergency: cross-sign new intermediate CA; mass rotation | DR runbook (separate doc) |

CRL distribution: `/fm/v1/crl/<ca_id>` in T2; pods refresh every 60s. Stale CRL (> 5 min old) → reject NEW handshakes, allow existing connections (fail-closed for new, fail-open for in-flight to avoid mass disconnect).

---

## 4. TRANSPORT SECURITY (mTLS EVERYWHERE)

### 4.1 Mandatory mTLS Endpoints

| Channel | Server Auth | Client Auth | Min TLS | Cipher Suite Floor |
|---------|-------------|-------------|---------|---------------------|
| Operator → API gateway | Cert + SAN | Cert OR OIDC bearer | 1.3 | TLS_AES_256_GCM_SHA384, TLS_CHACHA20_POLY1305_SHA256 |
| FM ↔ etcd (T1, T2) | Cert | Cert | 1.3 | Same |
| FM ↔ CB plugin | Cert (SAN-pinned to vendor) | Cert (SAN-pinned to FM workload) | 1.3 | Same |
| FM ↔ Device (gNMI / SAI / driver RPC) | Device cert | FM SVID | 1.3 (1.2 ONLY in legacy-device escape hatch) | TLS_AES_256_GCM_SHA384; fallback TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384 |
| Device → FM registration (bootstrap) | FM cert | Enrollment token (then upgraded to cert) | 1.3 | Same |
| FM ↔ SPIRE agent (UDS) | UDS peer creds | UDS peer creds | N/A (UDS) | N/A |

### 4.2 SAN Pinning

Every FM ↔ external connection pins the peer SAN (URI form, exact match). No CN fallback. No wildcard except for the `*.svc.cluster.local` SPIFFE namespace.

Anti-pattern (FORBIDDEN):
```
tls.Config{InsecureSkipVerify: true}        // CI lint rejects
tls.Config{ClientAuth: tls.NoClientCert}    // CI lint rejects on non-bootstrap paths
```

### 4.3 Key Material Handling

- Private keys: stored on tmpfs, `0600`, generated in-process; NEVER written to persistent disk
- HSM/TPM optional for prod (set via `tls.key_provider = "tpm"`); SPIRE supports
- Memory: keys held in `mlock`'d pages; zeroed on rotation
- Rotation: live rotation without dropping connections (graceful handshake replacement at next renegotiation window or natural close)

### 4.4 Forward Secrecy

ECDHE-only key agreement (X25519 or P-256). RSA key exchange disabled. PFS is non-negotiable.

---

## 5. AUTHORIZATION (RBAC + ABAC)

### 5.1 RBAC Roles

| Role | Permitted Actions | Forbidden |
|------|-------------------|-----------|
| `fleet:viewer` | GET ENIs, devices, status, telemetry, dlq | All mutations |
| `fleet:operator` | All `:viewer` + POST/PUT/DELETE ENIs, drain devices, retry DLQ | Quarantine release, security ops, CA ops |
| `fleet:sre` | All `:operator` + quarantine release, force-reconcile, rolling restart | CA ops, identity issuance |
| `fleet:security` | CRL ops, identity revocation, audit export | ENI mutations |
| `fleet:admin` | All of above | (none — but every action logged + alerted) |
| `fleet:device` (machine) | Self-registration, hash-report, telemetry push | Cross-device reads |
| `fleet:cb-plugin` (machine) | Stream events to assigned topics, ack | Read T1 outside its topic scope |

### 5.2 ABAC Constraints (Layered on RBAC)

| Attribute | Constraint |
|-----------|------------|
| Tenant scope | Operators authorized for tenant T can only touch ENIs where `nic_spec.tenant_id == T` |
| Region scope | Cross-region operations require `fleet:admin` OR explicit multi-region scope |
| Quarantine | Quarantined entities reject writes regardless of role except `fleet:sre` release call |
| Time-of-day | Destructive ops (drain-shard, mass-quarantine) require explicit "change window" assertion (encoded in JWT claim) outside which they 403 |

### 5.3 Policy Evaluation

- **Engine:** OPA (Open Policy Agent) sidecar; policy bundles signed by `fleet:security`
- **Decision cache:** 30 s in-process; invalidated on policy bundle update
- **Default:** DENY. Anything not explicitly permitted is denied.
- **Failure mode:** OPA unavailable → fail closed for mutations, fail open for reads (degraded, alerts fire)

### 5.4 Action Audit

Every authz decision (allow OR deny) emits:
```json
{
  "ts": "...",
  "subject": "spiffe://dashfabric.local/...",
  "subject_human": "user@corp.example",
  "action": "POST /api/v1/eni",
  "resource": "eni-abc123",
  "decision": "ALLOW|DENY",
  "policy_id": "fleet-policy-v42",
  "evaluation_ms": 0.8,
  "trace_id": "..."
}
```

Audit log goes to a write-once stream (see §7).

---

## 6. SECRETS MANAGEMENT

### 6.1 Secret Inventory

| Secret | Storage | Access Path | Rotation |
|--------|---------|-------------|----------|
| FM pod SVID private key | tmpfs (workload API socket) | Memory only | 30 min |
| etcd client cert | tmpfs | Memory only | 30 min |
| CB plugin client cert | tmpfs | Memory only | 30 min |
| Device root/intermediate CA private key | HSM (per region) | Sign-only API; no export | 1 year (planned), 24 h emergency |
| Enrollment-token signing key | KMS (envelope-encrypted) | Sign-only; CRL revocable | Quarterly |
| OPA policy bundle signing key | KMS | Sign-only | Quarterly |
| OIDC client secret (operator IdP) | Sealed secret in K8s | At pod startup | On IdP rotation |
| Telemetry export bearer (Prometheus remote-write) | Sealed secret | Loaded at startup | Quarterly |

### 6.2 Forbidden Patterns

- No secret in env vars (visible via `/proc/<pid>/environ`) except short-lived bootstrap tokens
- No secret in ConfigMaps
- No secret in command-line args
- No secret in container image layers
- No secret in T1, T2, or T3 (FM data plane is NOT a secret store)

### 6.3 In-Process Discipline

- `string` type for secrets only at the boundary; immediately wrapped in `Secret[T]` opaque type
- `Secret[T]` has `Use(fn)` method; never `String()`, `Format()`, or `MarshalJSON()` — those panic
- On scope exit: explicit `Zeroize()` call (or `defer secret.Zeroize()`)
- CI lint: prevents `%v` / `%s` / `json.Marshal` on any `Secret[T]`

---

## 7. AUDIT LOGGING

### 7.1 What Gets Audited (Mandatory)

| Event Class | Examples | Retention |
|-------------|----------|-----------|
| Authn | Login, cert issuance, token consumption | 1 year |
| Authz | Every allow/deny decision | 1 year |
| Mutation | ENI create/update/delete, device drain, quarantine | 1 year |
| Security ops | CA ops, CRL update, policy change, secret rotation | 7 years |
| Privileged read | DLQ inspection, audit export, secret read by `fleet:security` | 1 year |
| Failure modes | Auth bypass attempts, replay attempts, malformed enrollment | 1 year |

### 7.2 Audit Stream Properties

- **Append-only:** Backend is a write-once medium (S3 Object Lock, or equivalent)
- **Tamper-evident:** Each batch hash-chained with prior batch; chain root checkpointed daily into an out-of-band system
- **Two-channel:** Same record emitted to (a) the audit stream and (b) a separate SIEM. Divergence triggers alert.
- **High availability:** Audit write failure FAILS the operation it was logging (no silent loss). Exception: read-path audit can buffer up to 30 s.

### 7.3 Audit Envelope

```json
{
  "audit_version": 1,
  "ts": "2026-06-22T12:34:56.789Z",
  "actor": {
    "subject_uri": "spiffe://dashfabric.local/...",
    "human_principal": "alice@corp.example",
    "session_id": "...",
    "source_ip": "10.0.0.5",
    "user_agent": "..."
  },
  "action": "ENI_CREATE",
  "resource": {
    "kind": "ENI",
    "id": "eni-abc123",
    "tenant_id": "tenant-42"
  },
  "outcome": "SUCCESS|FAILURE",
  "error_code": "API_001_BAD_REQUEST",
  "trace_id": "...",
  "before_state_hash": "sha256:...",
  "after_state_hash": "sha256:...",
  "policy_version": "fleet-policy-v42",
  "chain_prev_hash": "sha256:..."
}
```

---

## 8. THREAT MODEL

### 8.1 Attacker Categories

| Attacker | Capabilities | Primary Defenses |
|----------|--------------|------------------|
| Curious tenant operator | Valid creds for tenant T | Tenant scope enforcement (§5.2); per-tenant rate limits |
| Compromised tenant operator | Valid creds, intent to escalate | Per-action audit; anomaly detection; quarantine on dangerous patterns |
| Compromised CB plugin | Sends arbitrary events to FM | Schema validation; CAS-based T1 writes; watermark monotonicity; DLQ |
| Compromised single FM pod | Can read its shard's T1 keys | SVID-scoped etcd ACLs; lateral movement blocked by per-shard creds |
| Compromised device | Returns crafted hash/state | Reconciliation classifier flags `UNKNOWN_DEVICE`; quarantine |
| Compromised SPIRE server | Can mint SVIDs | Out-of-scope (assumes secure SPIRE deployment); CA-of-CA detection via audit |
| Network attacker (MITM) | Sits on intra-cluster network | mTLS + SAN pinning; cipher floor TLS 1.3 |
| Insider with root on a node | Reads pod memory | Out-of-scope; mitigated only by kernel hardening (§9) |
| Supply-chain attack on vendor CB | Malicious code in plugin | CB plugins run in restricted namespace; no host access; audit of all CB-sourced T1 writes |

### 8.2 Notable Abuse Cases & Mitigations

| Abuse Case | Mitigation |
|------------|------------|
| Replay of CB event | Event-id idempotency + adapter watermark monotonicity (see `adapter-protocol-design.md` §5) |
| Replay of enrollment token | Single-use CAS in T2 |
| Stolen device cert used from a different IP | SAN URI checked; geographic anomaly raises alert (warning, not block) |
| Op floods API gateway | Token-bucket per `subject_uri`; burst+sustain limits; 429s |
| Split-brain HA write storm | `HA_003_SPLIT_BRAIN_SUSPECTED` (CRITICAL), see runbook |
| Tenant T tries to acquire ENI in tenant U | ABAC tenant scope (§5.2); denied + audited |
| Operator runs destructive op outside change window | JWT change-window claim required; otherwise 403 |
| Adversarial NicSpec with massive prefix-tag | Composition resource limits (§5.4 `nicgoalstate-schema-design.md` quotas) — TBD per-tenant route count cap |
| Audit log tampering | Hash chain + out-of-band checkpoint |

### 8.3 What This Spec Does NOT Defend Against (Documented Limits)

- Compromise of the K8s control plane (assumed secure; out of FM scope)
- Compromise of the SPIRE root key (mass identity revocation runbook required)
- Compromise of the HSM holding regional CA (regional reset required)
- Side-channel attacks on shared CPU (mitigated by node isolation policy, not by FM code)
- Active denial-of-service from a peer FM pod (mitigated by network policy, out of FM scope)

---

## 9. HARDENING CHECKLIST

### 9.1 Container

- [ ] Run as non-root UID (10001); `runAsNonRoot: true`
- [ ] Read-only root filesystem; only `/tmp` and `/var/run/spire` writable
- [ ] No host network, no host PID, no host IPC
- [ ] `securityContext.capabilities.drop = ["ALL"]`
- [ ] `seccompProfile = "RuntimeDefault"` (or stricter custom profile)
- [ ] Image scanned (Trivy / Snyk) on build; CVE blocking gate
- [ ] Image signed (cosign / Notation); admission controller verifies

### 9.2 Kernel & Runtime

- [ ] Go race detector enabled in test builds; disabled in prod
- [ ] `GODEBUG=netdns=go+v4` for predictable DNS behavior
- [ ] `GOMEMLIMIT` set to 7 GiB (1 GiB headroom below pod limit)
- [ ] AppArmor / SELinux profile applied (deny ptrace, deny `/etc/shadow`, etc.)

### 9.3 Network Policy (K8s)

- [ ] Default-deny ingress AND egress per namespace
- [ ] Explicit allow: FM ↔ etcd, FM ↔ SPIRE agent (UDS), FM ↔ CB plugin (TCP), FM ↔ device (TCP)
- [ ] No egress to public Internet from FM pods
- [ ] Adapter pods isolated to CB-namespace only

### 9.4 Process Limits

- [ ] `RLIMIT_NOFILE` = 65536 (room for 8k devices × few FDs)
- [ ] `RLIMIT_NPROC` = 1024 (we run goroutines, not processes)
- [ ] `RLIMIT_CORE` = 0 (no core dumps; replaced by panic stack to log)

### 9.5 Build & Supply Chain

- [ ] All deps pinned (`go.sum` committed; CI verifies)
- [ ] SBOM generated per build (CycloneDX)
- [ ] Reproducible builds (same source → same binary hash)
- [ ] No pre-built C dependencies; cgo disabled where possible
- [ ] Critical deps (etcd client, gRPC, OPA) on a vetted-version allowlist

---

## 10. INCIDENT RESPONSE HOOKS

| Incident | Auto Action | Operator Action |
|----------|-------------|-----------------|
| Repeated authz denies for one subject | Rate-limit subject (token bucket halves); alert | Investigate subject |
| Enrollment-token replay attempt | Block source IP for 1 h; alert | Investigate |
| Audit chain divergence (SIEM vs. write-once) | CRITICAL alert; freeze CA ops | Run forensic export |
| Mass cert revocation in <5 min | Alert; require `fleet:admin` confirmation to proceed | Confirm or abort |
| Unknown SVID seen | Drop connection; alert | Check SPIRE registrations |
| OPA decision latency P99 > 50ms for 60s | Switch to fail-open reads, fail-closed writes; alert | Investigate OPA |

---

## 11. CONFIGURATION

```yaml
security:
  tls:
    min_version: "1.3"
    cipher_suites:
      - TLS_AES_256_GCM_SHA384
      - TLS_CHACHA20_POLY1305_SHA256
    key_provider: "spire"   # "spire" | "tpm" | "file" (file only for dev)
    san_pinning: true
  identity:
    spire_socket: "/run/spire/agent.sock"
    svid_ttl_seconds: 3600
    rotation_overlap_seconds: 600
  authz:
    opa_url: "unix:///run/opa.sock"
    decision_cache_ttl_seconds: 30
    fail_open_for_reads_when_opa_down: true
    fail_closed_for_writes_when_opa_down: true
  audit:
    primary_sink: "s3://fm-audit-prod/<region>/"
    secondary_sink: "siem://corp.example/fm"
    flush_interval_ms: 500
    require_sync_for_mutations: true
  enrollment:
    token_ttl_seconds: 900
    consumed_record_retention_days: 90
  device_cert:
    leaf_ttl_days: 30
    renew_at_remaining_percent: 25
```

---

## 12. TEST MATRIX

| Test | Behavior Asserted |
|------|-------------------|
| SEC-001 | Connection without client cert → REJECT (mTLS enforced) |
| SEC-002 | Connection with SAN mismatch → REJECT |
| SEC-003 | Expired SVID → REJECT; rotation recovers automatically |
| SEC-004 | Enrollment token reuse → second use REJECTED + audited |
| SEC-005 | Operator outside tenant scope → DENY + audited |
| SEC-006 | OPA down → reads degrade, writes fail closed |
| SEC-007 | Audit sink unavailable → mutation operation FAILS (no silent drop) |
| SEC-008 | `Secret[T]` printed via `%v` → CI lint fails build |
| SEC-009 | `InsecureSkipVerify=true` in any code path → CI lint fails build |
| SEC-010 | Replayed CB event (same event_id) → idempotent no-op |
| SEC-011 | Hash chain break detected → CRITICAL alert fires |
| SEC-012 | Container runs as root → admission controller rejects |
| SEC-013 | Image without signature → admission controller rejects |
| SEC-014 | CRL refresh fails > 5 min → new handshakes REJECTED, existing allowed |
| SEC-015 | Cross-region op without admin role → DENY + audited |

---

## 13. EVOLUTION RULES

### 13.1 Adding a New External Channel

1. Add row to §4.1 (mTLS table)
2. Declare SAN form
3. Register SPIFFE namespace in SPIRE
4. Add OPA policy
5. Add audit event class
6. Add tests SEC-XXX
7. Security review sign-off required

### 13.2 Loosening a Control (Exception)

Time-bounded only. Requires:
- Ticket with business justification
- Approval from `fleet:security` role
- Audit entry at the time the override is set AND at every operation using the override
- Auto-expiry; cannot be permanently disabled

### 13.3 Cipher / TLS Floor Updates

Tracked via NIST + IETF deprecation announcements. Removals require 90-day deprecation window with metric tracking remaining usage.

---

## 14. REFERENCES

- `adapter-protocol-design.md` §4 — Adapter lease / leader election; §5 — Event idempotency
- `device-lifecycle-design.md` §6 — Device REVOKED state (cert revoked path)
- `error-handling-design.md` §3.2 (API_002_AUTH_FAILED), §3.5 (ADP_*) — Security-related error codes
- `threading-model-design.md` §10 — Lock order (audit log writes are short-lived L8-equivalent)
- `MASTER_DESIGN_INDEX.md` §12 — Numerical constants (TTLs cross-referenced here)
- SPIFFE/SPIRE docs — Workload identity
- OPA docs — Policy engine
- DASH security profile — Device-side requirements (out-of-band; vendor)
