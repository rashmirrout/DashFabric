# 06 — Northbound: Device Registration

> This document specifies how a DPU comes online, identifies itself, and gets
> bound to a ShardSet. Goals: zero-touch onboarding, strong identity,
> stable steady-state channels, scale to 10k+ devices.

---

## 1. Roles

| Role | Component |
|---|---|
| **Registrar** | Northbound Gateway (NBG) — stateless K8s Deployment |
| **Identity authority** | SPIRE Server (per region) issuing SVIDs |
| **Device** | DPU running the SONiC-DASH stack with a DashFabric-Agent sidecar (or, in v2, native gRPC client in DASH container) |
| **Shard** | Owner of the device's hierarchy after assignment |

---

## 2. Identity Model

### 2.1 Device Identity
Each DPU is provisioned at manufacturing/deployment time with:
- A **hardware-rooted private key** stored in a TPM 2.0 or NIC-side secure
  element.
- A **device GUID** derived from the hardware root.
- The ability to generate **CSRs** signed by the hardware key.

At boot, the device-agent uses SPIRE's **Node Attestation** (TPM attestation
plugin) to obtain a SPIFFE SVID:

```
spiffe://dashfabric/dc/<region>/dpu/<HostID>
```

This SVID is the **x.509 cert** presented in mTLS to the NBG.

### 2.2 Why SPIFFE
- Workload identities are vendor-neutral and forward-compatible.
- Rotation is automatic (default cert TTL 1 hour).
- Same identity infrastructure used inside the cluster for DashFabric pods
  and DPU-side agents.

### 2.3 Operator Identity
Operators use Kubernetes RBAC + dex-issued OIDC tokens. The `dfctl` tool
authenticates to a small admin gateway, not to the NBG directly.

---

## 3. Registration Protocol

A single gRPC service exposed by NBG:

```protobuf
service DeviceRegistration {
  // One-shot, low frequency. Establishes shard assignment.
  rpc Register(RegisterRequest) returns (RegisterResponse);

  // Voluntary teardown. Frees the HDO and its subtree.
  rpc Unregister(UnregisterRequest) returns (UnregisterResponse);

  // Lightweight presence heartbeat.
  rpc Heartbeat(HeartbeatRequest) returns (HeartbeatResponse);
}

message RegisterRequest {
  string  host_id              = 1;   // must match SVID SAN
  DeviceCapabilities  caps     = 2;
  string  sw_version           = 3;
  string  firmware_version     = 4;
  string  hardware_sku         = 5;
  repeated NetworkAddress  reachable_addresses = 6;
  string  preferred_az         = 7;
  string  bootstrap_nonce      = 8;   // random, for replay protection
  map<string, string>  labels  = 9;
}

message DeviceCapabilities {
  uint32 max_enis              = 1;
  uint32 max_routes_per_eni    = 2;
  uint64 max_ca_pa_mappings    = 3;
  uint32 max_acl_rules_per_grp = 4;
  bool   supports_fnic         = 5;
  bool   supports_st_pl        = 6;
  bool   supports_fastpath     = 7;
  string gnmi_schema_version   = 8;
  // ... see DASH HLD §1.4 for full scaling matrix
}

message RegisterResponse {
  string  shard_endpoint       = 1;   // gRPC host:port to open steady-state stream
  string  shard_id             = 2;
  uint32  heartbeat_interval_s = 3;   // suggested by server
  bytes   server_cert_chain    = 4;   // pinning aid
  string  assigned_region      = 5;
}
```

### 3.1 Flow
```
1. Device starts. Agent obtains SVID from SPIRE.
2. Agent dials nbg.<region>.dashfabric.svc:443 with mTLS.
3. Agent calls Register({host_id, caps, ...}).
4. NBG:
     a. Verifies SVID SAN matches host_id.
     b. Verifies bootstrap_nonce not previously seen (replay window).
     c. Validates caps against per-region admission policy.
     d. Reads /shardmap/v1/devices/<host_id>:
          • if exists and pinned → use existing shard.
          • else → compute shard = ringLookup(hash(host_id)); upsert.
     e. Returns RegisterResponse.
5. Device closes the NBG channel.
6. Device opens persistent gRPC channel to shard_endpoint.
7. Shard Primary receives the connection; spawns HDO if not already present;
   writes /state/v1/hosts/<host_id>/registered (leased, TTL=2×heartbeat).
8. Steady-state begins: heartbeats + gNMI Subscribe streams to the device's
   internal gNMI endpoint (the agent forwards to the local SONiC-DASH
   container).
```

### 3.2 Heartbeat
```protobuf
message HeartbeatRequest {
  string host_id    = 1;
  uint64 monotonic  = 2;   // increments each heartbeat
  HealthSnapshot health = 3;
}
```
- Default interval: **5 s**.
- Server (the assigned ShardSet) renews `/state/v1/hosts/<host_id>/registered`
  lease (TTL 15 s, K=3 missed).
- On 3 missed heartbeats: HDO drains and the subtree is destroyed (per
  §02 §7).

### 3.3 Unregister
Initiated by:
- Device (graceful planned shutdown).
- Operator via `dfctl device unregister <HostID>`.
- Lease expiry (implicit).

All paths converge to the same HDO destruction sequence (per spec).

---

## 4. Pre-Registration: Hardware Attestation

We require **attestation** of device firmware before accepting registration in
the production tier. Two options, both supported:

### 4.1 TPM 2.0 Attestation (preferred)
- SPIRE TPM plugin verifies PCRs match a whitelist of known-good firmware
  hashes.
- Whitelist is per-SKU and rotated via a signed manifest in object storage.
- Devices failing attestation are returned `PERMISSION_DENIED` and an
  alert is raised.

### 4.2 BMC-side Attestation
For DPUs without TPM, the host BMC vouches for the DPU (signed by an
operator-controlled CA). Less secure; an explicit policy flag enables this.

---

## 5. Steady-State Channel

After registration, the device opens a persistent gRPC stream to its assigned
ShardSet. This stream carries:

- **Heartbeats** (device → shard).
- **Liveness probes** (shard → device): "are you there?" RPC.
- **Telemetry stream** (device → shard): gNMI Subscribe SAMPLE on key paths.
- **Programming push** (shard → device): gNMI Set.
- **Operator pushes** (shard → device): on-demand diagnostic captures.

### 5.1 Why Persistent
- Avoids TCP/TLS handshake on every Set RPC.
- Allows server-push from shard to device (e.g., flush cache requests).
- Eliminates NAT keepalive issues on long-lived connections.

### 5.2 Connection Topology
- Device → ShardSet exposes **3 endpoints** (one per pod) via headless
  Service.
- Device's gRPC client opens connection only to the **current Primary**
  (resolved via DNS or via NBG response field).
- On Primary change, the previously-Primary pod sends `GOAWAY`; client
  reconnects to the new Primary.

### 5.3 Connection Pool Sizing
- Per device: 1 logical bidirectional stream over 1 HTTP/2 connection.
- Per shard primary: thousands of inbound streams; standard Go gRPC server
  handles 50k+ on commodity hardware.

---

## 6. Per-Region NBG Sizing

For 10k devices, registration rate is bursty:
- **Normal**: ~0.1 reg/sec (rare).
- **DC startup**: 10k regs in 5 min = **33 reg/sec**.
- **Heartbeats**: 10k devices × 0.2 Hz = **2000 RPS** (these go to ShardSets,
  not NBG).

NBG handles registers + heartbeats forwarding (we proxy heartbeats too as
fallback if the device cannot reach the ShardSet directly). NBG sizing:
- 3 pods baseline (HA).
- 1 vCPU + 1 GiB RAM per pod handles 1000 RPS comfortably.
- HPA on CPU 60%.

---

## 7. Network Reachability

DASH devices typically have an in-band management path via the SONiC-DASH
container; out-of-band BMC is rare for gNMI. We support both:

| Path | Use case | Notes |
|---|---|---|
| In-band (front-panel L3) | Default | Reachable via TOR fabric; tied to underlay BGP |
| Out-of-band (mgmt NIC) | Optional | Slower but isolated from data plane outages |

The `reachable_addresses` field in `RegisterRequest` advertises both. The
HAL prefers in-band but fails over to OOB if the in-band gNMI channel
goes silent for `>livenessTimeout`.

---

## 8. Trust-on-First-Use vs Strict Whitelist

| Mode | Behavior |
|---|---|
| **Strict** (production) | Device must be in `/policy/v1/global/admitted-devices` allow-list (populated by inventory). Unknown devices → `NOT_FOUND`. |
| **TOFU + Attestation** (staging) | First registration auto-admits if attestation passes. Identity locked thereafter. |
| **Lab** | Any device with a valid cert from a lab CA accepted. |

Mode is set per region via `/policy/v1/regional/admission-mode`.

---

## 9. Replay Protection

The `bootstrap_nonce` is a random 256-bit value chosen by the device. The NBG
tracks recently-seen nonces in a short-lived Bloom filter (15-minute TTL).
This protects against an attacker capturing a `RegisterRequest` and replaying
it elsewhere.

---

## 10. Telemetry Channel (Device → Shard)

After registration, the device opens a **gNMI Subscribe** stream from its
local gNMI endpoint, multiplexed back through the steady-state gRPC channel.
The shard's HAL listener subscribes to:

| Path | Sampling |
|---|---|
| `/sonic-dash/eni-stats/*` | 10 s |
| `/sonic-dash/meter-counters/*` | 30 s |
| `/sonic-dash/system/health` | 30 s |
| `/sonic-dash/ha/peer-status` | event-driven |

This stream gives the reconcile loop fresh state without per-cycle Get
storms.

---

## 11. Errors and Their Meanings

| Code | Meaning | Device action |
|---|---|---|
| `UNAUTHENTICATED` | mTLS handshake or SVID validation failed | Refresh SVID; retry |
| `PERMISSION_DENIED` | Attestation failed or not admitted | Stop; log; raise BMC alert |
| `INVALID_ARGUMENT` | Caps inconsistent with hardware SKU on file | Operator intervention |
| `FAILED_PRECONDITION` | Region in admission freeze | Backoff; retry per policy |
| `RESOURCE_EXHAUSTED` | Region at device cap | Backoff; alert |
| `UNAVAILABLE` | NBG transient | Retry with jitter |
| `OK` | Success | Open steady-state channel |

---

## 12. Open Questions

| ID | Question | Default |
|---|---|---|
| OQ-601 | Use SPIRE Node Attestation or a custom protocol? | **SPIRE** — production-tested, vendor-neutral. |
| OQ-602 | Should heartbeats carry full health, or just liveness? | **Liveness + tiny health snapshot**. Detailed health via gNMI Subscribe. |
| OQ-603 | Do we model BMC as a separate identity? | **No** in v1; BMC paths are operator-tooling only. |
| OQ-604 | What about secure boot validation? | Required for production tier; folded into TPM attestation policy. |
