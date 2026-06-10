# 07 — HAL & DASH Southbound

> The HAL is the **only** part of DashFabric that knows about gNMI, SAI,
> DASH-specific paths, and vendor quirks. Everything above it speaks in
> vendor-agnostic `GoalState` objects.

---

## 1. Why a HAL Matters

DASH defines an **open behavioral model** and a published gNMI/SAI schema,
but in practice:

- Vendors ship at different DASH spec revisions.
- Some vendors expose extensions (faster mapping bulk-loads, custom telemetry).
- The DASH gNMI schema itself evolves (DASH HLD rev 2.6.1 as of 02/2026).
- Future devices may not speak gNMI at all (e.g., proprietary REST).

We isolate all of this behind a **stable internal interface**, so:
- Core actor code never imports a vendor package.
- Adding a new vendor or schema version is a self-contained package.
- We can run the same actor against a mock HAL in tests.

---

## 2. HAL Architecture

```
        ┌──────────────────────────────────────────────────────────┐
        │                       HAL Public API                     │
        │  Apply(GoalState) / Get(DeviceQuery) / Subscribe(...)    │
        └──────────────────┬──────────────────────────┬────────────┘
                           │                          │
                ┌──────────▼─────────┐    ┌──────────▼──────────────┐
                │  Codec Registry    │    │  Transport Registry     │
                │  per (vendor,      │    │  per protocol           │
                │       schemaVer)   │    │  (gnmi v0.10, restconf, │
                │                    │    │   loopback)             │
                └──────────┬─────────┘    └──────────┬──────────────┘
                           │                          │
                ┌──────────▼─────────┐    ┌──────────▼──────────────┐
                │  DashGnmiCodec     │    │  GnmiTransport          │
                │  VendorXCodec      │    │  LoopbackTransport      │
                │  MockCodec         │    │  RestconfTransport      │
                └────────────────────┘    └─────────────────────────┘
                              │                       │
                              └──────────┬────────────┘
                                         ▼
                              ┌─────────────────────┐
                              │  Capability Filter  │  drops/transforms ops
                              │                     │  the device can't do
                              └──────────┬──────────┘
                                         ▼
                              ┌─────────────────────┐
                              │  Pacing / Rate      │  per-device QoS to
                              │  Limiter            │  protect DPU CPU
                              └──────────┬──────────┘
                                         ▼
                              ┌─────────────────────┐
                              │  Wire (per device)  │
                              └─────────────────────┘
```

---

## 3. Public API

```go
package hal

// GoalState is the vendor-agnostic, hash-stable description of what an
// object wants the device to look like.
type GoalState struct {
    ObjectID    ObjectID
    Kind        ObjectKind         // Device | Container | Eni
    HostID      string
    Spec        proto.Message      // e.g. *eniv1.EniSpec
    Schema      SchemaRef          // e.g. "dash/v2.6.1"
    Hash        [32]byte           // sha256(canonical(Spec))
    Revision    int64              // upstream etcd ModRevision
}

type ApplyResult struct {
    Applied      bool              // false if no-op (already matches)
    SentBytes    int
    CallDuration time.Duration
    FenceToken   uint64            // device-acknowledged fence
}

type HAL interface {
    // Apply: idempotent push of GoalState. Must be safe to call repeatedly.
    Apply(ctx context.Context, g GoalState, opts ...ApplyOpt) (ApplyResult, error)

    // Get: read the current device state for the object's keys, used by
    // the reconcile loop.
    Get(ctx context.Context, q DeviceQuery) (DeviceState, error)

    // Delete: idempotent removal.
    Delete(ctx context.Context, q DeviceQuery) error

    // Subscribe: open a streaming subscription for telemetry/state.
    Subscribe(ctx context.Context, paths []string, mode SubscriptionMode) (<-chan TelemetryEvent, error)

    // Capabilities: returns device capability set (often cached).
    Capabilities(ctx context.Context) (DeviceCapabilities, error)

    // Drain: gracefully closes all in-flight calls (used by Lease handoff).
    Drain(ctx context.Context) error
}

type ApplyOpt func(*applyConfig)
func WithShadowMode() ApplyOpt          // standby pods
func WithDryRun() ApplyOpt              // diagnostic CLI
func WithIdempotentRetry(n int) ApplyOpt
func WithFenceToken(token uint64) ApplyOpt
```

### 3.1 Contract Notes
- `Apply` MUST be **idempotent** (DASH semantics agree; see SONiC-DASH HLD
  §1.6 #11).
- `Apply` MUST be **atomic per call**: either all leaves in the GoalState
  land or none do. Where the underlying schema doesn't allow native
  transactions (e.g., gNMI Set with multiple updates), the codec must use
  pre-commit/commit sequencing or compensating deletes on partial failure.
- `Apply` SHOULD return `Applied=false` when the hash matches the
  last-known-good for that object (avoid bytes on the wire).
- `Get` returns whatever the device is *currently* configured with, not
  what it *was last told*.

---

## 4. The DASH gNMI Codec (Reference)

This is the default codec, implementing the DASH/SONiC gNMI schema. It
translates internal Go structs to gNMI `SetRequest`/`GetResponse` payloads
keyed by paths under `/dash/...` per the [SONiC DASH HLD](https://github.com/sonic-net/SONiC/blob/master/doc/dash/dash-sonic-hld.md).

### 4.1 Object → gNMI Paths

For an `EniSpec` ENI `nic-primary` on Container `VM-abc` on HostID `H`:

| Spec field | gNMI path | DASH APP_DB table |
|---|---|---|
| `EniSpec` core | `/dash/eni/H/VM-abc/nic-primary` | `DASH_ENI_TABLE` |
| `vnet_ref` | (lookup) `/dash/vnet/<vnet_name>` | `DASH_VNET_TABLE` |
| `inbound_acl_stages[i]` | `/dash/eni/H/VM-abc/nic-primary/acl-in/<stage>` → group `<g>` | `DASH_ACL_IN_<S>` mapping |
| `outbound_acl_stages[i]` | `/dash/eni/H/VM-abc/nic-primary/acl-out/<stage>` | `DASH_ACL_OUT_<S>` |
| `route_group` | `/dash/eni/.../route-group` → ref `<g>` | `DASH_ROUTE_GROUP_TABLE` + `DASH_ROUTE_TABLE` |
| `MappingEntry[]` | `/dash/vnet-mapping/<vnet>/<dst-ip>` | `DASH_VNET_MAPPING_TABLE` |
| `metering` | `/dash/eni/.../meter-policy` | `DASH_METER_POLICY` + `DASH_METER_RULE` |
| `ha_role` | `/dash/eni/.../ha-role` | `DASH_HA` |

### 4.2 Atomic Application Pattern

```
codec.Apply(eniGoalState):
    1. compute delta = diff(currentDeviceHash, goalState.hash)
    2. construct gNMI SetRequest with:
         - delete: [path1, path2, ...]      # stale leaves
         - replace: [{path, value}, ...]    # idempotent-replace leaves
         - update: [{path, value}, ...]     # additive leaves
    3. send single Set RPC
    4. on OK: record currentDeviceHash := goalState.hash in WAL
    5. on FAIL:
         - parse SAI error code from response trailers
         - if retryable: backoff + retry up to N times
         - if non-retryable: return ErrSchemaInvalid → actor → FAILED
```

### 4.3 Bulk and Streaming Loads

CA-PA mappings (8M/DPU) and routes (100k/ENI) cannot be jammed into a single
Set. The codec implements **chunked Set**:

- Split delta into chunks of 1000 entries (configurable).
- Issue Set RPCs serially per chunk under a single OTel parent span.
- If any chunk fails, the codec drives the remainder to rollback by
  issuing compensating deletes for chunks that succeeded.
- Final commit: a sentinel `/dash/eni/.../ready = true` flip is the
  "transaction commit"; readers only observe the new ENI as serving when
  ready=true.

### 4.4 Telemetry Subscription Defaults
- `SAMPLE` mode for counters (10–30 s).
- `ON_CHANGE` mode for status flips (state changes).
- Heartbeats: gNMI `SUBSCRIBE` with `heartbeat_interval = 30s` so the
  shard's HAL detects channel death even when nothing changes.

---

## 5. Capability Filter

Before any `Apply`, the HAL consults the device's `DeviceCapabilities` and:

- **Rejects** GoalStates that exceed device limits with a clear error
  (e.g., `tooManyAclRules: device caps say 1000, spec has 1500`).
- **Transforms** GoalStates where the device exposes a different schema
  for the same intent (e.g., older devices use `tag-list-v1` vs
  `tag-list-v2`).
- **Skips** features the device doesn't support, returning a structured
  `unsupported` notice that surfaces as a warning in observability.

Capabilities are fetched on registration and cached; refreshed on firmware
version change.

---

## 6. Pacing & Rate Limiting

To protect DPU CPU (especially during bulk updates), HAL imposes per-device
pacing:

| Limit | Default | Configurable per device |
|---|---|---|
| Concurrent gNMI calls | 8 | Yes |
| Max throughput | 5 MB/s | Yes |
| Max RPS | 200 | Yes |
| Mapping batch chunk size | 1000 entries | Yes |

A small leaky-bucket per device delays oversubscribed calls; metric
`dashfabric_hal_throttle_seconds_total{host}` rises.

---

## 7. Transport Layer

| Transport | Purpose |
|---|---|
| `GnmiTransport` | Production. Reuses the steady-state mTLS gRPC channel. |
| `LoopbackTransport` | For unit tests; in-process mock device. |
| `RestconfTransport` | For non-gNMI devices (rare; future). |
| `DryRunTransport` | Wraps any transport; intercepts sends and just logs/returns success. Used by `dfctl simulate`. |

The transport is selected per-device based on capabilities.

---

## 8. Vendor Plugin Model

A vendor plugin is a Go package implementing:

```go
type VendorPlugin interface {
    Name() string
    SupportedSchemas() []SchemaRef
    NewCodec(schema SchemaRef) (Codec, error)
    Probe(ctx context.Context, devCaps DeviceCapabilities) (bool, error)
}
```

Registration is **static at build time** for v1 (avoid `plugin.so`
nightmares). All vendor plugins live under `internal/hal/vendors/<name>/`
and are wired in `cmd/dashfabric/wire.go`.

### 8.1 Codec Compatibility Matrix
```
Vendor    | DASH 2.4 | DASH 2.5 | DASH 2.6.1
----------|----------|----------|------------
Generic   |    Y     |    Y     |    Y          (reference impl, always tracks latest)
VendorA   |    Y     |    Y     |    -
VendorB   |    Y     |    -     |    -          (legacy)
```

The HAL picks the highest-supported schema for each device.

---

## 9. Fence Tokens

(See `05` §3.3.) The HAL injects a `x-dfabric-fence-token` metadata header on
every outbound RPC. Devices that have been upgraded to the
fence-aware SONiC-DASH container reject stale tokens; legacy devices
silently accept (the Lease-side check is the fallback guard).

---

## 10. Drift Computation

When the reconcile loop calls `Get` and compares to GoalState, the HAL
exposes a helper:

```go
func ComputeDrift(goal GoalState, actual DeviceState) (DriftReport, error)
```

`DriftReport` includes:
- `Severity` (INFO | WARN | CRITICAL)
- `Adds`, `Removes`, `Changes` per leaf
- A human-readable summary (for logs / `dfctl` output)
- A minimal `SetRequest` plan to bring actual → goal

The reconcile loop converts the plan into an `Apply` call.

---

## 11. Test Harness

We ship a **BMv2-based simulated DASH device** as a development/test target:

- Standard DASH P4 program ([`dash-pipeline/bmv2`](https://github.com/sonic-net/DASH/tree/main/dash-pipeline)).
- Wrapped with a gNMI front-end that maps our schema to the bmv2 control plane.
- Used in CI to validate every codec change.

For higher-realism CI we also support **integration with vendor SDK
container images** when licensable.

---

## 12. Failure Modes the HAL Surfaces

| Class | Example | Surfaced as |
|---|---|---|
| Transport | Connection dropped mid-Set | retryable; backoff; metric |
| Schema | Unknown field/path | non-retryable; actor → FAILED |
| Capacity | Device returns `RESOURCE_EXHAUSTED` | retryable with longer backoff; alert |
| Authorization | mTLS rejected | non-retryable; alert; HDO → DEVICE_QUARANTINED |
| Timing | Pacing throttle | transparent (delay, not error) |
| Fence | Stale fence token rejected | non-retryable on this Primary; triggers Lease release |
| Telemetry | gNMI Subscribe stream RST | reconnect; metric; alert if persistent |

---

## 13. Mapping a Real DASH Object — Worked Example

**Intent**: `nic-primary` on `VM-abc` on `HOST-1` is an ENI in VNET
`vnet-blue`, with 3 outbound ACL stages, a route group of 4 routes, and
2 CA-PA mappings.

`GoalState.Spec` is a Proto `NicSpec` of ~40 fields.

The codec generates (simplified):

```yaml
# gNMI SetRequest replace block
- path: /dash/eni/HOST-1/VM-abc/nic-primary
  value:
    mac: 00:0d:3a:0c:01:02
    vnet: vnet-blue
    admin_state: UP
    ha_role: ACTIVE

- path: /dash/eni/HOST-1/VM-abc/nic-primary/acl-out/stage-1
  value: { group_ref: "acl-grp-vnic-321", priority: 100 }

- path: /dash/eni/HOST-1/VM-abc/nic-primary/acl-out/stage-2
  value: { group_ref: "acl-grp-subnet-77", priority: 200 }

- path: /dash/eni/HOST-1/VM-abc/nic-primary/acl-out/stage-3
  value: { group_ref: "acl-grp-vnet-12", priority: 300 }

- path: /dash/eni/HOST-1/VM-abc/nic-primary/route-group
  value: rg-blue-default

# Route group entries (separate Set RPC: bulk, chunked)
- path: /dash/route-group/rg-blue-default/routes
  value:
    - { dst: 10.0.0.0/16, action: encap, vnet: vnet-blue-peer-1 }
    - { dst: 10.1.0.0/16, action: drop }
    - ... 2 more ...

# CA-PA mappings (separate Set: bulk, chunked)
- path: /dash/vnet-mapping/vnet-blue/10.0.1.5/32
  value: { pa: 100.64.0.5, mac: 00:0d:3a:0c:ff:01 }

- path: /dash/vnet-mapping/vnet-blue/10.0.1.6/32
  value: { pa: 100.64.0.6, mac: 00:0d:3a:0c:ff:02 }
```

All paths are atomic-per-RPC. The codec sequences ENI-core first, then
groups, then mappings, with rollback compensation on failure.

---

## 14. Open Questions

| ID | Question | Default |
|---|---|---|
| OQ-701 | Use `gnmic` library or roll our own gNMI client? | **`openconfig/gnmi` upstream** (more flexible). |
| OQ-702 | Should HAL parallelize across chunks? | **No** for v1 — keep DPU CPU predictable. Parallelism is a per-device tunable. |
| OQ-703 | How are vendor-specific extensions exposed up the stack? | Via `DeviceCapabilities.Extensions map[string]string`. Actors that don't know an extension never reference it. |
| OQ-704 | Should the HAL fully model the **HA pairing** between cards? | The HAL **programs** the pairing config based on `ha_role` and peer references in NicSpec, but it does not orchestrate the pairing protocol — that's between the cards themselves (DASH HA spec). |
| OQ-705 | What about non-DASH SmartNICs? | A separate non-DASH codec would be needed; out of scope for v1 (project name and DC profile are DASH-specific). |
