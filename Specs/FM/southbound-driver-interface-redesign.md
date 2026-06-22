# SouthboundDriver Interface (Wave-Aware Redesign)

**Status:** AUTHORITATIVE
**Owner:** FM Architecture
**Supersedes:** fleet-manager-lld.md §10.2 (individual Apply* methods)
**Date:** 2026-06-22

---

## 1. PURPOSE

Resolves the wave-ordering vs. HAL-interface mismatch identified in `ARCHITECTURE_REVIEW.md` GAP 1.4. The current LLD defines individual `Apply*` methods per object type, but the wave-ordered programming model (7 waves, 0-6) requires the driver to enforce topological ordering. This document defines a wave-aware `ApplyDeltaPlan` interface where the driver owns wave sequencing.

---

## 2. CORE INTERFACE

```go
type SouthboundDriver interface {
    // Wave-aware single entry point — primary method.
    // Driver is responsible for executing waves in strict ascending order
    // and waiting for each wave's in-flight RPCs before starting next.
    ApplyDeltaPlan(ctx context.Context, plan DeltaPlan) ApplyResult

    // Telemetry: read content_hash from device (for reconciliation).
    ReadContentHash(ctx context.Context, eni_id string) (string, error)

    // Rollback: revert to last-good content_hash state.
    Rollback(ctx context.Context, eni_id string, target_hash string) error

    // Connection lifecycle (driver-internal).
    Connect(ctx context.Context, device_endpoint string) error
    Disconnect(ctx context.Context) error
    IsHealthy() bool
}
```

### 2.1 Why ApplyDeltaPlan (Not Individual Apply*)

| Concern | Individual Apply* (old) | ApplyDeltaPlan (new) |
|---------|-------------------------|----------------------|
| Wave ordering | Caller (dispatcher) must orchestrate | Driver enforces internally |
| Partial failure recovery | Caller tracks per-call | Driver knows full plan, rolls back |
| Batching for efficiency | Hard (driver doesn't see full picture) | Easy (driver sees all commands) |
| Topological correctness | Easy to violate | Enforced by single entry point |
| Vendor-specific optimization | Limited | Driver can reorder within wave |

---

## 3. DELTAPLAN STRUCTURE

```protobuf
message DeltaPlan {
  string eni_id = 1;                    // Target ENI
  string target_content_hash = 2;       // Goal state hash
  string previous_content_hash = 3;     // For rollback ("" if first apply)
  int64  plan_revision = 4;             // Monotonic per ENI

  repeated DeltaCommand commands = 10;  // Pre-sorted by wave_offset asc

  // Execution policy
  ExecutionPolicy policy = 20;
}

message DeltaCommand {
  uint32 wave_offset = 1;               // 0..6 (see §4)
  CommandKind kind = 2;                 // CREATE | UPDATE | DELETE
  string object_id = 3;                 // Stable ID for telemetry
  ObjectKind object_kind = 4;           // VRF | VLAN | IDENTITY | ROUTE | ACL | METER | VIP | HA

  // The actual object payload (one-of per kind)
  oneof payload {
    VrfObject       vrf       = 10;
    VlanObject      vlan      = 11;
    IdentityObject  identity  = 12;
    RouteObject     route     = 13;
    AclStageObject  acl_stage = 14;
    MeterObject     meter     = 15;
    VipMappingObject vip      = 16;
    HaObject        ha        = 17;
  }

  // For DELETE: only object_id + object_kind are populated
}

message ExecutionPolicy {
  bool   stop_on_first_wave_failure = 1;   // Default: true
  uint32 max_parallel_in_wave = 2;          // Default: 16
  uint32 rpc_timeout_ms = 3;                // Default: 5000
  bool   rollback_on_failure = 4;           // Default: true
}

message ApplyResult {
  string eni_id = 1;
  ResultStatus status = 2;                  // SUCCESS | PARTIAL | FAILED | ROLLED_BACK
  string final_content_hash = 3;            // Device hash after apply (or rollback)

  uint32 last_completed_wave = 10;
  repeated CommandResult command_results = 11;

  string error_message = 90;
  google.protobuf.Timestamp completed_at = 91;
  int64 total_duration_ms = 92;
}

message CommandResult {
  string object_id = 1;
  ObjectKind object_kind = 2;
  uint32 wave_offset = 3;
  ResultStatus status = 4;
  string error_message = 5;
  int64 duration_ms = 6;
}

enum ResultStatus {
  RESULT_UNSPECIFIED = 0;
  RESULT_SUCCESS = 1;
  RESULT_PARTIAL = 2;
  RESULT_FAILED = 3;
  RESULT_ROLLED_BACK = 4;
  RESULT_SKIPPED = 5;
}
```

---

## 4. WAVE EXECUTION ALGORITHM

### 4.1 Wave Assignment (Recap from nicgoalstate-schema-design.md §7)

| Wave | Object Types | Dependency Reason |
|------|-------------|-------------------|
| 0 | VRF, VLAN | Foundational namespaces |
| 1 | Identity (MAC, IP, VLAN bind) | ENI identity |
| 2 | MeterPolicy | Required for flow programming |
| 3 | Routes | Forwarding state |
| 4 | AclStage (Stage 1, pre-routing) | Inbound policy |
| 5 | AclStage (Stage 2, post-routing) | Outbound policy |
| 6 | VipMappings, HA links | Depend on routes + ACLs |

**Delete order = reverse:** Wave 6 → 5 → 4 → 3 → 2 → 1 → 0.

### 4.2 Driver Execution Pseudocode

```
function ApplyDeltaPlan(ctx, plan) -> ApplyResult:

  result = ApplyResult{eni_id: plan.eni_id, status: RESULT_UNSPECIFIED}
  start = now()

  # Partition commands by wave
  waves = GroupBy(plan.commands, key=c.wave_offset)

  # Determine wave order: ascending for CREATE/UPDATE plans, descending if all DELETE
  if AllCommandsAreDelete(plan):
    wave_order = [6, 5, 4, 3, 2, 1, 0]
  else:
    wave_order = [0, 1, 2, 3, 4, 5, 6]

  for wave_num in wave_order:
    if wave_num not in waves:
      continue

    wave_commands = waves[wave_num]
    wave_result = ExecuteWave(ctx, wave_commands, plan.policy)

    result.command_results.append(wave_result.command_results)
    result.last_completed_wave = wave_num

    if wave_result.has_failure:
      if plan.policy.stop_on_first_wave_failure:
        # Halt remaining waves
        if plan.policy.rollback_on_failure and plan.previous_content_hash != "":
          rollback_result = Rollback(ctx, plan.eni_id, plan.previous_content_hash)
          result.status = RESULT_ROLLED_BACK
          result.final_content_hash = plan.previous_content_hash
        else:
          result.status = RESULT_PARTIAL
          # final_content_hash unknown; read it
          result.final_content_hash = ReadContentHash(ctx, plan.eni_id)
        result.error_message = wave_result.error_summary
        result.total_duration_ms = now() - start
        return result

  # All waves succeeded
  result.status = RESULT_SUCCESS
  result.final_content_hash = plan.target_content_hash
  result.total_duration_ms = now() - start
  return result


function ExecuteWave(ctx, commands, policy) -> WaveResult:

  semaphore = NewSemaphore(policy.max_parallel_in_wave)
  results = []
  wg = NewWaitGroup()

  for cmd in commands:
    wg.Add(1)
    go func(cmd):
      defer wg.Done()
      semaphore.Acquire()
      defer semaphore.Release()

      cmd_ctx = WithTimeout(ctx, policy.rpc_timeout_ms)
      cmd_start = now()

      switch cmd.kind, cmd.object_kind:
        case CREATE, ROUTE:        err = device.CreateRoute(cmd_ctx, cmd.payload.route)
        case UPDATE, ROUTE:        err = device.UpdateRoute(cmd_ctx, cmd.payload.route)
        case DELETE, ROUTE:        err = device.DeleteRoute(cmd_ctx, cmd.object_id)
        case CREATE, ACL_STAGE:    err = device.CreateAcl(cmd_ctx, cmd.payload.acl_stage)
        ... etc ...

      results.append(CommandResult{
        object_id: cmd.object_id,
        object_kind: cmd.object_kind,
        wave_offset: cmd.wave_offset,
        status: err == nil ? RESULT_SUCCESS : RESULT_FAILED,
        error_message: err.message_or_empty(),
        duration_ms: now() - cmd_start,
      })
    (cmd)

  wg.Wait()

  has_failure = any(r.status == RESULT_FAILED for r in results)
  return WaveResult{command_results: results, has_failure: has_failure, ...}
```

### 4.3 Within-Wave Parallelism

Commands within a single wave can execute in parallel (default: 16 concurrent RPCs). The driver MAY reorder within a wave for vendor-specific optimization (e.g., batching routes).

**Constraint:** All commands in wave N MUST complete before wave N+1 starts. No exceptions.

---

## 5. DISPATCHER ↔ DRIVER CONTRACT

### 5.1 Dispatcher Responsibilities

1. Build `DeltaPlan` from NicGoalState diff (new vs. old)
2. Assign `wave_offset` to each command per §4.1
3. Pre-sort `commands` by `wave_offset` ascending (driver may rely on this)
4. Set `target_content_hash` from new NicGoalState
5. Set `previous_content_hash` from last-applied state (read from T2)
6. Call `driver.ApplyDeltaPlan(ctx, plan)`
7. Persist `ApplyResult` to T2 (for audit) and update `last_applied_hash`

### 5.2 Driver Responsibilities

1. Execute waves in strict order
2. Enforce max_parallel_in_wave
3. Apply rpc_timeout_ms per command
4. On failure: respect stop_on_first_wave_failure and rollback_on_failure
5. Return complete ApplyResult with per-command details
6. NEVER reorder waves (within wave OK, across waves NOT OK)
7. NEVER skip waves

### 5.3 Idempotency

- `ApplyDeltaPlan` MUST be safe to retry with the same plan
- If `target_content_hash == device.current_hash`: return SUCCESS immediately (no-op)
- Per-command idempotency: CREATE on existing object = UPDATE; DELETE on absent object = SUCCESS

---

## 6. ROLLBACK SEMANTICS

### 6.1 When Rollback Triggers

- Wave fails AND `policy.rollback_on_failure == true` AND `previous_content_hash != ""`
- Reconciliation detects drift AND operator requests rollback

### 6.2 Rollback Algorithm

```
function Rollback(ctx, eni_id, target_hash) -> error:
  # Driver must have access to last-good state (cached or readable from T2)
  last_good_plan = T2.Read("/fm/v1/applied_plans/" + eni_id + "/" + target_hash)
  if last_good_plan == nil:
    return errors.New("rollback target not found")

  # Build inverse plan (CREATE↔DELETE, UPDATE→UPDATE with old payload)
  inverse_plan = BuildInversePlan(current_state, last_good_plan)
  result = ApplyDeltaPlan(ctx, inverse_plan)
  return result.error_or_nil()
```

### 6.3 Failure During Rollback

- Status: `ROLLED_BACK_PARTIAL`
- Emit critical alert
- Operator intervention required
- ENI state → `QUARANTINED`

---

## 7. ERROR TAXONOMY

```go
type DriverErrorCode int

const (
    DRV_OK                    DriverErrorCode = 0
    DRV_CONNECTION_LOST       DriverErrorCode = 1   // Retry with backoff
    DRV_TIMEOUT               DriverErrorCode = 2   // Per-command timeout
    DRV_INVALID_PAYLOAD       DriverErrorCode = 3   // Permanent — operator alert
    DRV_RESOURCE_EXHAUSTED    DriverErrorCode = 4   // Device full (table overflow)
    DRV_VERSION_MISMATCH      DriverErrorCode = 5   // Plan stale; reload
    DRV_PARTIAL_APPLY         DriverErrorCode = 6   // Wave halfway; rollback recommended
    DRV_DEVICE_REJECTED       DriverErrorCode = 7   // Device-level validation failed
    DRV_PERMANENT_FAILURE     DriverErrorCode = 8   // Unrecoverable — quarantine ENI
)
```

| Code | Retry? | Action |
|------|--------|--------|
| CONNECTION_LOST | Yes (3x backoff 100ms→1s) | Reconnect, retry plan |
| TIMEOUT | Yes (1x) | Retry command |
| INVALID_PAYLOAD | No | Park NIC in VALIDATION_REJECTED |
| RESOURCE_EXHAUSTED | No | Alert; ENI → DEGRADED |
| VERSION_MISMATCH | No | NicActor recomposes |
| PARTIAL_APPLY | N/A | Rollback per policy |
| DEVICE_REJECTED | No | Park NIC; operator review |
| PERMANENT_FAILURE | No | Quarantine ENI |

---

## 8. CONNECTION LIFECYCLE

```go
type DriverConnState int

const (
    CONN_DISCONNECTED DriverConnState = iota
    CONN_CONNECTING
    CONN_CONNECTED
    CONN_RECONNECTING
    CONN_FAILED
)
```

**Connect flow:**
1. Driver dials device endpoint (gNMI/SAI/vendor-specific)
2. Performs capability exchange
3. Loads device's current `content_hash` (for delta computation)
4. Transitions to CONNECTED

**Disconnect detection:**
- Heartbeat or stream error → CONN_RECONNECTING
- Reconnect attempts with exponential backoff (1s → 10s cap, 10% jitter)
- After 5 failed attempts → CONN_FAILED → emit alert
- HDO sees `IsHealthy() == false` → parks all NICs in WAITING_DEVICE

---

## 9. PER-VENDOR DRIVER IMPLEMENTATION

Multiple SouthboundDriver implementations live behind this interface:

| Driver | Target | Protocol | Status |
|--------|--------|----------|--------|
| GnmiDriver | OpenConfig devices | gNMI Set/Get | Required |
| SaiDriver | SONiC/DASH SAI | SAI gRPC | Required |
| MockDriver | Tests | In-memory | Required |
| VendorXDriver | Vendor-specific | Vendor SDK | Plugin (CB) |

All implementations MUST pass the conformance suite in §10.

---

## 10. CONFORMANCE TEST SUITE

| Test | Behavior Asserted |
|------|-------------------|
| SBD-001 | Wave ordering: cmd in W3 never executes before any W2 cmd |
| SBD-002 | Within-wave parallelism: 16 cmds in W3 → max 16 concurrent RPCs |
| SBD-003 | Idempotency: same plan applied twice → second is no-op |
| SBD-004 | Rollback on wave failure: W3 fails → W0-2 unchanged on device, rollback applied |
| SBD-005 | Connection loss mid-plan: PARTIAL status, reconnect, retry succeeds |
| SBD-006 | Timeout per command: slow cmd → TIMEOUT, others in wave unaffected |
| SBD-007 | Reverse order on DELETE: all-delete plan executes W6→W0 |
| SBD-008 | Content-hash verification: SUCCESS plan → ReadContentHash matches target |
| SBD-009 | Concurrent ApplyDeltaPlan on different ENIs → no cross-ENI interference |
| SBD-010 | Invalid payload → INVALID_PAYLOAD error, no device state change |

---

## 11. METRICS (REQUIRED)

```
fm_sbd_apply_plan_total{driver, eni_id, status}
fm_sbd_apply_plan_duration_ms{driver} (histogram)
fm_sbd_wave_duration_ms{driver, wave} (histogram)
fm_sbd_command_total{driver, kind, object_kind, status}
fm_sbd_command_duration_ms{driver, object_kind} (histogram)
fm_sbd_rollback_total{driver, reason}
fm_sbd_connection_state{driver, state} (gauge)
fm_sbd_reconnect_total{driver}
fm_sbd_errors_total{driver, error_code}
```

---

## 12. MIGRATION FROM EXISTING LLD

`fleet-manager-lld.md` §10.2 currently defines:
```
ApplyRoute(route)
ApplyAcl(acl)
ApplyMeter(meter)
... etc
```

**Replace with:** Single `ApplyDeltaPlan(plan)`. Internal methods (`device.CreateRoute`, etc.) remain as driver-private helpers invoked from within wave execution. They are NOT part of the public SouthboundDriver interface.

---

## 13. REFERENCES

- `nicgoalstate-schema-design.md` §7 — Wave assignment per object kind
- `vm-eni-provisioning-design.md` §4 — Provisioning wave overview
- `Specs/CB/` — ControllerBridge plugin contract (CB-FM gRPC)
- `reconciliation-design.md` (to be created) — How ReadContentHash is used for drift detection
