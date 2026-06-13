# ControllerBridge (CB) Module

ControllerBridge (CB) is the **vendor-implemented service** that sits
between a vendor's control plane and DashFabric's Fleet Manager (FM).
CB is a peer module to FM — its own architecture, its own lifecycle,
its own conformance suite.

## What CB is

- A **separate service / pod / process**, never linked into FM.
- A **symmetric topic broker with a translation core**:
  - subscribes to vendor's CP, translates events to FM-spec, publishes
    them to FM-facing topics;
  - subscribes to FM's ack publications, translates them back to
    vendor-spec, delivers them to vendor's CP.
- A **vendor-implementable** service. FM owns the FM-facing schemas;
  vendor owns the CP-facing translation.

## What CB is not

- Not part of FM's binary or address space.
- Not the orchestrator. CB is a *bridge*; the orchestrator is whatever
  the vendor already runs.
- Not a queue or message bus on its own — it relies on its underlying
  topic store, which can be ephemeral or backed by a vendor choice
  (etcd, BoltDB, SQLite, in-memory).

## Document index

| # | File | Status |
|---|------|--------|
| 1 | [01-cb-architecture-hld.md](01-cb-architecture-hld.md) — high-level design | Draft |
| 2 | [02-cb-low-level-design-lld.md](02-cb-low-level-design-lld.md) — internal design | Draft |
| 3 | [03-cb-vendor-implementation-guide.md](03-cb-vendor-implementation-guide.md) — for vendor implementers | Draft |
| 4 | [04-cb-conformance-suite.md](04-cb-conformance-suite.md) — mandatory tests | Draft |
| 5 | [05-cb-ack-and-versioning.md](05-cb-ack-and-versioning.md) — ack model details | Draft |
| 6 | [06-cb-simulator-design.md](06-cb-simulator-design.md) — `cbsim` architecture | Draft |
| 7 | [07-cb-simulator-cli.md](07-cb-simulator-cli.md) — `cbsim` CLI reference | Draft |

## Wire contract

The CB↔FM contract is in [`Specs/cb_fm_protos/`](../cb_fm_protos/) —
locked, versioned, and shared between every vendor's CB implementation
and FM. See that folder's `README.md` for proto layout and versioning
policy.

## Topic tree (top level)

All FM-spec topics live under a versioned root:

```
/dashfabric/v1/config/devices/{dpu_id}
/dashfabric/v1/config/nics/{eni_id}
/dashfabric/v1/config/vnets/{vnet_id}
/dashfabric/v1/config/mappings/{vnet_id}
/dashfabric/v1/config/acls/{acl_group_id}
/dashfabric/v1/config/routes/{route_group_id}
/dashfabric/v1/config/vms/{vm_id}
/dashfabric/v1/config/containers/{container_id}
/dashfabric/v1/config/ha/{ha_set_id}

# FM-published acks (per resource):
/dashfabric/v1/config/<topic>/<key>/ack/delivery   ← append-log
/dashfabric/v1/config/<topic>/<key>/ack/state      ← compacted
```

Versioning policy: `v1` is the current major. Breaking changes bump
the version prefix; both old and new can coexist on the same CB during
migration.

## DASH alignment

CB topics are derived from upstream DASH SAI tables where applicable.
Lineage is called out per-topic in the proto files and in the HLD.
Reference: <https://github.com/sonic-net/DASH/tree/main>.

## Project protocols

CB is bound by every protocol in [`../protocols/`](../protocols/),
in particular:

- **Protocol 1 (DASH alignment)** — every CB topic that mirrors a DASH
  SAI table calls out the upstream artifact.
- **Protocol 2 (vendor-neutral FM)** — FM never imports vendor code;
  CB is the entire boundary.
- **Protocol 5 (one binary, multiple tiers)** — CB runs in compose,
  K8s sidecar, K8s Deployment; only configuration differs.
- **Protocol 6 (default + every knob a knob)** — CB defaults are
  set; every backend / store / mode is configurable.
