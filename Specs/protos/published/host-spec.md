# `HostSpec` — Per-host bootstrap config

> **TL;DR:** The "what host is this DPU plugged into?" record. Carries
> the host's identity, the agent endpoint FleetManager reaches, and
> fleet-wide knobs that apply at the host level (telemetry sampling,
> management VRF, etc.). One per device.

**Topic:** `/config/v1/hosts/<device_id>/spec`
**Kind:** `CONFIG_KIND_HOST_SPEC`
**Scope:** per device (top of the host subtree)
**Lifecycle owner:** orchestrator
**Subscriber:** HDO actor (one per device)

## Example

```json
{
  "device_id": "dpu-westus2-rack17-007",
  "host_name": "host-rack17-007.westus2.example.net",
  "host_role": "COMPUTE",
  "agent_endpoint": {
    "address": "100.64.7.5",
    "port": 8080,
    "protocol": "GNMI_GRPC"
  },
  "management_vrf": "mgmt",
  "telemetry": {
    "sampling_rate_pps": 1000,
    "export_collector": "telem.westus2.example.net:4317"
  },
  "feature_flags": {
    "enable_pa_validation": "true",
    "enable_ha_fast_failover": "true"
  },
  "attributes": {
    "rack": "rack17",
    "cluster": "compute-prod"
  }
}
```

## Purpose

`HostSpec` is the **first object** the HDO needs after the `Appliance`
record — it tells FleetManager where to send programs (the agent
endpoint), what role to apply, and which host-level feature flags are
on. The HDO will not proceed to container/NIC programming until
`HostSpec` and `Appliance` are both present.

It's separated from `Appliance` because the two have different owners
in practice: Appliance is owned by the device-management team
(firmware, capabilities), while HostSpec is owned by the
fleet-orchestration team (placement, agent endpoints, telemetry).

## Fields

### Identity

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `device_id` | `string` | yes | Matches path segment; mirrors Appliance. |
| `host_name` | `string` | yes | Hypervisor / bare-metal hostname. |
| `host_role` | `HostRole` enum | yes | `COMPUTE`, `STORAGE`, `EDGE`, `INFRA`. |

### Programming endpoint

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `agent_endpoint.address` | `IpAddress` | yes | DPU's DASH agent IP. |
| `agent_endpoint.port` | `uint32` | yes | DASH agent port. |
| `agent_endpoint.protocol` | `AgentProtocol` enum | yes | `GNMI_GRPC`, `GNMI_GNOI`. |

### Operations

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `management_vrf` | `string` | no | Name of management VRF, if isolated from data plane. |
| `telemetry.sampling_rate_pps` | `uint32` | no | Per-flow sample budget. |
| `telemetry.export_collector` | `string` | no | OTLP collector host:port. |
| `feature_flags` | `map<string,string>` | no | Per-host on/off toggles. |
| `attributes` | `map<string,string>` | no | Free-form. |

### Validation rules

1. `device_id` matches path segment.
2. `agent_endpoint.address` reachable from the FleetManager pod (probe at first attach).
3. `host_role=STORAGE` should disable `feature_flags.enable_ha_fast_failover` (warn if not).

## Proto3 sketch

```proto
syntax = "proto3";
package fleetmanager.v1;

import "common.proto";   // IpAddress

enum HostRole {
  HOST_ROLE_UNSPECIFIED = 0;
  HOST_ROLE_COMPUTE     = 1;
  HOST_ROLE_STORAGE     = 2;
  HOST_ROLE_EDGE        = 3;
  HOST_ROLE_INFRA       = 4;
}

enum AgentProtocol {
  AGENT_PROTOCOL_UNSPECIFIED = 0;
  AGENT_PROTOCOL_GNMI_GRPC   = 1;
  AGENT_PROTOCOL_GNMI_GNOI   = 2;
}

message AgentEndpoint {
  IpAddress     address  = 1;
  uint32        port     = 2;
  AgentProtocol protocol = 3;
}

message HostTelemetry {
  uint32 sampling_rate_pps = 1;
  string export_collector  = 2;
}

message HostSpec {
  // Identity
  string   device_id = 1;
  string   host_name = 2;
  HostRole host_role = 3;

  // Programming
  AgentEndpoint agent_endpoint = 10;

  // Ops
  string             management_vrf = 20;
  HostTelemetry      telemetry      = 21;
  map<string,string> feature_flags  = 22;
  map<string,string> attributes     = 30;
}
```

## Relationships

- Sibling of: `Appliance` (same device, different concern).
- Parent of: every `ContainerSpec` under the same device.
- Referenced by: HDO actor at boot.

## Change semantics

- **agent_endpoint** change: HDO closes the current gNMI session, opens
  a new one. Programming pauses briefly.
- **feature_flags** change: may cascade to NicGoalState (e.g. toggling
  `enable_pa_validation`); HDO marks all NOs dirty when a relevant flag
  flips.
- **host_role** change: usually only at re-provisioning; treat as
  managed change.

## See also

- [appliance](./appliance.md) — paired record about the DPU hardware.
- [container-spec](./container-spec.md) — child records under the same device.
- [README](./README.md) — full kind index.