# `cbsim` CLI Reference

> **Status:** Draft v1
> **Audience:** Developers, demo operators, test authors using the CB
> simulator.

`cbsim` is a single binary that exposes both a CLI and an HTTP control
API. CLI commands are convenience wrappers over the HTTP API for
common operations.

## 1. Top-level commands

| Command | Purpose |
|---------|---------|
| `cbsim run` | Boot the simulator, optionally loading a scenario |
| `cbsim load` | Load a scenario into a running cbsim |
| `cbsim event` | Inject one ad-hoc event |
| `cbsim fault` | Inject one ad-hoc fault |
| `cbsim peek` | Read a value from the topic store |
| `cbsim ack-trace` | Stream acks observed for a key |
| `cbsim advance` | Advance the virtual clock |
| `cbsim dump` | Dump the full store as JSON |
| `cbsim scenarios` | List bundled scenarios |
| `cbsim version` | Print version + supported `cb_fm_protos` versions |

## 2. `cbsim run`

Boots the simulator's gRPC server and HTTP control API.

```bash
cbsim run \
  [--scenario=PATH]            \
  [--grpc-listen=:7443]        \
  [--http-listen=:7081]        \
  [--store=memory|boltdb]      \
  [--store-path=/var/lib/cbsim] \
  [--virtual-clock]            \
  [--tls-cert=PATH --tls-key=PATH --tls-ca=PATH] \
  [--log-level=info]
```

| Flag | Default | Notes |
|------|---------|-------|
| `--scenario` | (none) | If provided, the scenario is auto-loaded but not auto-run |
| `--grpc-listen` | `:7443` | FM-side gRPC bind |
| `--http-listen` | `:7081` | Control API bind (loopback recommended) |
| `--store` | `memory` | Topic store backend |
| `--virtual-clock` | off | Time advances only on `advance` command |
| `--tls-*` | off (loopback) | mTLS credentials; off by default in dev |

Examples:

```bash
# Quick start — boot empty cbsim
cbsim run

# Boot with a scenario, virtual clock for determinism
cbsim run --scenario=scenarios/cold-boot-1-vnet-1-eni.yaml --virtual-clock

# Production-style — durable store, mTLS
cbsim run --store=boltdb --store-path=/var/lib/cbsim \
  --tls-cert=cb.crt --tls-key=cb.key --tls-ca=ca.crt
```

## 3. `cbsim load`

Loads a scenario file into a running cbsim. Replaces any existing
loaded scenario but keeps the running clock.

```bash
cbsim load --scenario=scenarios/vnet-mapping-update.yaml
cbsim load --scenario=- < my-scenario.yaml   # stdin
cbsim load --replay=trace.jsonl              # load a recorded trace instead
```

Use `--clear` first to wipe state:

```bash
cbsim load --clear --scenario=scenarios/cb-crash-recovery.yaml
```

After load, run with:

```bash
cbsim run-scenario           # begin executing the loaded scenario
cbsim run-scenario --until=30s
```

## 4. `cbsim event`

Inject a single event without authoring a scenario file.

```bash
cbsim event \
  --topic=/dashfabric/v1/config/nics/ENI_dpu-001_aabbccddeeff \
  --op=upsert \
  --payload-file=nic.json
```

Or inline payload:

```bash
cbsim event \
  --topic=/dashfabric/v1/config/vnets/vnet-2 \
  --op=upsert \
  --payload='{"vnet_id":"vnet-2","vni":200002,"address_spaces":["10.2.0.0/16"]}'
```

Delete:

```bash
cbsim event --topic=/dashfabric/v1/config/nics/ENI_dpu-001_aabbccddeeff --op=delete
```

## 5. `cbsim fault`

Inject one fault.

```bash
cbsim fault --kind=drop --topic-pattern='/dashfabric/v1/config/mappings/*' --duration=10s
cbsim fault --kind=delay --topic-pattern='/dashfabric/v1/config/**' --jitter-ms=500 --duration=30s
cbsim fault --kind=resync_required --topic='/dashfabric/v1/config/vnets/*'
cbsim fault --kind=health_degraded --duration=20s
cbsim fault --kind=crash       # process exits; rely on supervisor
cbsim fault --kind=partition --duration=15s
```

| Flag | Notes |
|------|-------|
| `--kind` | One of `drop`, `delay`, `reorder`, `duplicate`, `truncate`, `resync_required`, `health_degraded`, `crash`, `partition`, `slow_publish` |
| `--topic-pattern` | Glob; required for topic-scoped faults |
| `--duration` | How long the fault stays active |
| `--jitter-ms` | For `delay` |

## 6. `cbsim peek`

Read a value from the topic store directly.

```bash
cbsim peek --topic=/dashfabric/v1/config/vnets/vnet-1234
cbsim peek --topic=/dashfabric/v1/config/vnets/vnet-1234 --key=vnet-1234
cbsim peek --topic-pattern='/dashfabric/v1/config/nics/*'  # list keys
```

Output is JSON with the FM-spec proto rendered.

## 7. `cbsim ack-trace`

Stream the acks FM has published for a given resource.

```bash
cbsim ack-trace --resource-key=vnet-1234
cbsim ack-trace --resource-key=ENI_dpu-001_aabbccddeeff --kind=state
cbsim ack-trace --resource-key=ENI_dpu-001_aabbccddeeff --kind=delivery --tail
```

Output (tail mode):

```
2026-06-14T10:01:02Z  delivery  /config/nics/ENI_.../ack/delivery   content_hash=H1 v=1
2026-06-14T10:01:03Z  state     /config/nics/ENI_.../ack/state      PROGRAMMED ref=1
2026-06-14T10:01:30Z  state     /config/nics/ENI_.../ack/state      RETIRED    ref=0
```

## 8. `cbsim advance` (virtual-clock only)

```bash
cbsim advance --by=30s
cbsim advance --to=2026-06-14T10:00:00Z
```

No-op outside virtual-clock mode.

## 9. `cbsim dump`

Dumps the entire topic store as JSON. Useful for snapshotting state in
tests.

```bash
cbsim dump > snapshot.json
cbsim dump --topic-pattern='/dashfabric/v1/config/vnets/*' > vnets.json
```

## 10. `cbsim scenarios`

```bash
cbsim scenarios              # list bundled
cbsim scenarios show cold-boot-1-vnet-1-eni
```

## 11. Demo runbooks

### Demo 1 — happy path cold boot

```bash
cbsim run --scenario=scenarios/cold-boot-1-vnet-1-eni.yaml --virtual-clock
# in another terminal:
fm-up --cb=localhost:7443       # start FM, point at cbsim
cbsim advance --by=10s          # let initial events flow
cbsim ack-trace --resource-key=ENI_dpu-001_aabbccddeeff --kind=state --tail
# Expect: PROGRAMMED with ref=1
```

### Demo 2 — vendor CP partition

```bash
cbsim run --scenario=scenarios/cold-boot-1-vnet-1-eni.yaml
# After steady state:
cbsim fault --kind=health_degraded --duration=30s
# FM sees DEGRADED health; existing ENIs stay programmed
cbsim event --topic=/dashfabric/v1/config/nics/ENI_dpu-001_NEW \
  --op=upsert --payload-file=new-nic.json
# After fault expires, FM catches up and programs the new ENI
```

### Demo 3 — large-mapping resync

```bash
cbsim run --scenario=scenarios/large-mapping-resync.yaml --virtual-clock
fm-up --cb=localhost:7443
# Let it stabilize:
cbsim advance --by=60s
# Force a resync:
cbsim fault --kind=resync_required --topic='/dashfabric/v1/config/mappings/*'
# FM should issue Resync; no ENIs should reprogram (mapping is the same)
cbsim ack-trace --resource-key=vnet-customer-1 --kind=state --tail
# Expect: no state transition (idempotent re-delivery)
```

## 12. Exit codes

| Code | Meaning |
|------|---------|
| 0 | Normal exit (Ctrl-C, scenario complete) |
| 1 | Argument / config error |
| 2 | Bind error (port in use) |
| 3 | Scenario load error (invalid YAML) |
| 4 | `crash` fault triggered (testing supervisor restart) |
| 5 | gRPC startup failure |

## 13. Environment

| Variable | Purpose |
|----------|---------|
| `CBSIM_LOG_FORMAT` | `text` (default) or `json` |
| `CBSIM_HTTP_AUTH_TOKEN` | If set, HTTP API requires `Authorization: Bearer <token>` |
| `CBSIM_PROFILE_BIND` | If set, exposes pprof on this address |

## 14. References

- `06-cb-simulator-design.md` — architecture.
- `04-cb-conformance-suite.md` — tests cbsim is the reference for.
- Bundled scenarios: `scenarios/*.yaml` (under repo root once
  implementation begins).
