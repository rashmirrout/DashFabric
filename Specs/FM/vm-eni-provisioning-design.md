# VM ENI Provisioning — End-to-End Design

**Status:** Design proposal (planning/brainstorm phase — no schemas yet)
**Scope:** How FleetManager subscribes to PubSub, composes a full per-VM ENI
goal state from DASH objects, orders programming, and pushes to the DPU.
**Inputs:**
- Specs/02-domain-model-and-object-lifecycle.md (HDO→CO→NO actor model)
- Specs/03-control-plane-pubsub-and-topics.md (etcd topology, envelope schema)
- Specs/07-hal-and-dash-southbound.md (DASH HAL contract)
- Upstream DASH spec: `sonic-net/DASH/documentation/general/dash-sonic-hld.md`
- Upstream protos: `sonic-net/sonic-dash-api/proto/*.proto`

The objective: when the upstream control plane creates a new VM/container with
a NIC, FleetManager must materialize **one fully-programmed ENI on the DPU**
with VNET binding, route table, ACL stages, meter, conntrack scope, and the
mapping rows that turn customer-VM IPs into encapsulated underlay traffic.

---

## 1. Why a Single Subscription Tree Doesn't Work

The naive model — "everything for a NIC lives at
`/config/{device}/{container}/{nic}`" — collapses for two reasons:

### 1.1 Object scopes don't match the host hierarchy

DASH objects fall into four natural scopes, only one of which is per-NIC:

| Scope | DASH objects | Sharing pattern |
|-------|--------------|-----------------|
| **Per-ENI** (lifecycle = NIC) | `Eni`, `EniRoute`, `AclIn`/`AclOut` bindings, `RouteRule`, `Meter` bindings, `HaScope` | One owner: this NIC |
| **Per-VNET** (lifecycle = tenant VPC) | `Vnet`, `VnetMapping[]` (millions of rows), `PaValidation` | Shared by every ENI in the VNET |
| **Appliance-global** (lifecycle = device) | `Appliance`, `RoutingType`, `Qos`, `PrefixTag`, `Tunnel`, `HaSet` | Shared by all ENIs on the device |
| **Reusable groups** (lifecycle = policy template) | `RouteGroup`+`Route`, `AclGroup`+`AclRule`, `MeterPolicy`+`MeterRule`, `OutboundPortMap`+`Range` | Many ENIs reference the same group by id |

If `VnetMapping` (potentially **millions** of rows per VNET) lived under each
NIC topic, every ENI sharing the VNET would re-receive the same payload — N×
amplification. Conversely, putting per-ENI ACL bindings under a global topic
forces every NIC to filter the firehose.

### 1.2 Update cadences differ by 3+ orders of magnitude

- `Appliance` config: changes per device lifetime (~never).
- `Vnet` itself: changes when tenants are added (hours-days).
- `VnetMapping`: changes per VM birth/death (seconds-minutes, **bursty**).
- ENI ACL/route bindings: change per policy edit (minutes).

Bundling these into one topic means low-cadence consumers re-fetch on
high-cadence churn.

**Decision:** four parallel topic trees, joined by reference inside the actor.

---

## 2. Topic Hierarchy

```
/config/v1/
├── global/<device_id>/
│   ├── appliance                    → Appliance
│   ├── routing_type/<name>          → RoutingType (bulk; rarely changes)
│   ├── tunnel/<tunnel_id>           → Tunnel
│   ├── qos/<qos_id>                 → Qos
│   ├── prefix_tag/<tag_id>          → PrefixTag
│   └── ha_set/<ha_set_id>           → HaSet
│
├── group/<device_id>/
│   ├── route_group/<group_id>       → RouteGroup spec
│   │   └── routes/                  → repeated Route entries (bulk) [blob ref if > 8 KiB]
│   ├── acl_group/<group_id>
│   │   └── rules/                   → repeated AclRule
│   ├── meter_policy/<policy_id>
│   │   └── rules/                   → repeated MeterRule
│   └── outbound_port_map/<map_id>
│       └── ranges/                  → repeated OutboundPortMapRange
│
├── vnet/<device_id>/<vnet_id>/
│   ├── spec                         → Vnet
│   ├── pa_validation                → PaValidation
│   └── mapping                      → VnetMapping[] (LARGE — blob ref to S3/MinIO,
│                                                    etcd holds only digest+url)
│
└── hosts/<device_id>/
    ├── spec                         → HostDeviceObject (host-level)
    └── <container_guid>/
        ├── spec                     → ContainerObject
        └── <nic_id>/
            └── spec                 → NICObject = ENI goal state stub
                                       (eni_id, vnet_ref, route_group_refs,
                                        acl_group_refs, meter_policy_ref,
                                        ha_scope_ref, primary_ip, mac, vlan)
```

### Tradeoff brainstorm

| Question | Option A | Option B | Recommendation |
|----------|----------|----------|----------------|
| Where do `Route` rows live — under their `RouteGroup`, or flat? | Nested under group (this design) | Flat `/route/<id>` | **Nested.** Group is the unit of subscription; iterating routes by group is the access pattern (driver programs whole group atomically). |
| `VnetMapping` in etcd or blob store? | Inline (3 GiB+ at scale) | Blob store, etcd holds reference | **Blob.** Already decided in Specs/03. NIC actors fetch on demand. |
| One topic per NIC, or one combined topic per container? | Per-NIC | Per-container with mv list | **Per-NIC.** Multi-NIC VMs are common (VPN, mgmt, data planes); each ENI binds to a different VNET. |
| Dynamic shared-group resolution: resolve refs on publisher side, or subscriber side? | Publisher inlines all referenced groups into NIC payload | Subscriber resolves refs by subscribing to group topics | **Subscriber.** Otherwise a 1-rule ACL change re-publishes every NIC referencing the group. |

---

## 3. Composing the NIC Goal State

`NICObject.spec` published under `hosts/.../<nic_id>/spec` is **not** the full
ENI program — it is a **reference bundle**. The NO actor *composes* the
final, denormalized goal state by joining four streams:

```
NIC spec (refs)         VNET subtree           Group subtree            Global subtree
       │                       │                     │                          │
       │  vnet_id ─────────────┤                     │                          │
       │  outbound_route_group_id ───────────────────┤                          │
       │  inbound_acl_group_ids[] ───────────────────┤                          │
       │  meter_policy_id ───────────────────────────┤                          │
       │  ha_scope ref                               │                          │
       │                       │                     │                          │
       │                       │                     │   tunnel_id (from route) ┤
       │                       │                     │   routing_type (from rt) ┤
       ▼                       ▼                     ▼                          ▼
       └──────────── NicGoalState (composed by NO actor) ──────────────────────┘
```

### The composed `NicGoalState` (proposed shape — not yet a proto)

```
NicGoalState {
  // Identity (from NICObject spec)
  device_id, container_id, nic_id, eni_id, mac, primary_ip, vlan_id

  // ENI body — denormalized
  eni: {
    spec: <Eni>,                 // from NIC.spec, fully expanded
    vnet: <Vnet>,                // joined from /vnet/<vnet_id>/spec
    pa_validation: <PaValidation>,
    qos: <Qos>,                  // joined from global
    ha_scope: <HaScope>,
  }

  // Outbound flow program
  outbound: {
    route_group: <RouteGroup>,
    routes: [<Route>...],        // each with tunnel/routing_type expanded
    tunnels_used: [<Tunnel>...],
    acl_stages: [{ stage: 1|2|3, group: <AclGroup>, rules: [<AclRule>...] }, ...]
  }

  // Inbound flow program
  inbound: {
    acl_stages: [{ stage: 1|2|3, group, rules }, ...]
    eni_routes: [<EniRoute>...]
  }

  // Mapping table — LARGE; carried by reference
  vnet_mapping_ref: { url: "blob://...", digest: "sha256:...", row_count: 1234567 }

  // Metering
  meter_policy: <MeterPolicy>,
  meter_rules: [<MeterRule>...]

  // Routing-type lookup table (small, global)
  routing_types: { "vnet-direct": <RoutingType>, "privatelink": <RoutingType>, ... }

  // Composition metadata
  composed_at, source_revisions: { vnet: 12, route_group: 7, ... }
  composition_hash: u64   // FNV-1a — used for drift detection
}
```

The composition hash is what `Reconcile` compares against the device-reported
state hash. Any input revision change invalidates the composition hash and
triggers re-derivation.

---

## 4. Programming Order (Dependency DAG)

DASH objects must be programmed in this topological order. Wave boundaries
permit parallelism within a wave (per `DeltaPlan.wave_offsets` in
`03-delta.md`):

```
Wave 0 (idempotent globals, program once per device):
  Appliance, RoutingType[*], Qos[*], PrefixTag[*]

Wave 1 (transports & HA — depend on Appliance):
  Tunnel[*], HaSet[*]

Wave 2 (shared groups — depend on tunnels & routing_types):
  RouteGroup→Route[*],  AclGroup→AclRule[*],
  MeterPolicy→MeterRule[*],  OutboundPortMap→Range[*]

Wave 3 (VNETs — depend on tunnel for underlay):
  Vnet[*],  PaValidation[*]

Wave 4 (mappings — depend on VNET; LARGE, may be batched):
  VnetMapping[*]    (driver chunks into 10k-row batches)

Wave 5 (ENI — depends on VNET, RouteGroup, MeterPolicy, HaSet):
  Eni,  HaScope

Wave 6 (per-ENI bindings — depend on ENI + groups):
  AclIn,  AclOut,  RouteRule,  EniRoute
```

DELETE order is the **reverse**: tear down per-ENI bindings → ENI → mappings
→ VNET → groups → tunnels → globals. The actor's FSM enforces this on
DRAINING transitions.

---

## 5. Actor Subscription Flow

This refines the subscription cascade in Specs/02 to account for the
multi-topic topology:

```
1. HDO actor spawns on device registration:
   - subscribes /config/v1/hosts/<device_id>/spec       (host config)
   - subscribes /config/v1/global/<device_id>/**         (appliance, tunnels, ...)
   - subscribes /config/v1/group/<device_id>/**          (shared groups — for ref resolution)
   - on receiving host spec, programs Wave 0–2 (globals + groups)

2. HDO discovers containers via etcd PREFIX list of
   /config/v1/hosts/<device_id>/<*>/spec — spawns CO per match.

3. CO actor:
   - subscribes /config/v1/hosts/<device_id>/<container_guid>/spec
   - discovers NICs via PREFIX list of <container_guid>/<*>/spec
   - spawns NO per NIC.

4. NO actor (the heart of ENI provisioning):
   a. subscribes /config/v1/hosts/<device_id>/<container_guid>/<nic_id>/spec
   b. on first spec, reads vnet_id; subscribes
      /config/v1/vnet/<device_id>/<vnet_id>/**
   c. reads route_group_ids, acl_group_ids, meter_policy_id; the HDO has
      already cached these — NO requests them via in-process actor message
      (no extra etcd traffic).
   d. composes NicGoalState (§3).
   e. requests Wave 3–6 deltas from StateCompilationEngine.
   f. dispatches via SouthboundDriver.

5. Updates:
   - Per-ENI change (ACL binding edit) → only NO recomputes.
   - Per-VNET change (new mapping) → all NOs in that VNET get a notification
     from the HDO's VNET watcher; recompose touches only mapping ref.
   - Global change (new tunnel) → HDO programs once; NOs invalidate cache.
```

### Why HDO holds the global/group caches (and not each NO)

10k devices × 32 ENIs = 320k NO actors. If each subscribed to global +
group topics independently, that is 320k × ~30 topics = ~10M etcd watches
per cluster. Centralizing global/group subscriptions at the HDO (one set per
device) drops it to 10k × 30 = 300k watches — manageable in etcd v3.

NO actors get group payloads via in-process actor messages from their HDO
parent. This makes the HDO a fan-out hub for shared config, not a strict
hierarchy gate.

---

## 6. Failure Modes & Edge Cases (brainstorm)

| Scenario | Behavior |
|----------|----------|
| NIC spec arrives before its referenced VNET exists | NO enters `WAITING_REFS` state; doesn't block the actor pool — just parks until VNET watch fires. Audit emits `MISSING_REFERENCE` event. |
| VNET deleted while ENIs reference it | Publisher policy: VNET delete is rejected if any NIC references it. FleetManager treats orphan VNET refs as `VALIDATION_ERROR` and surfaces in `Reconcile`. |
| Route group updated mid-program | New revision arrives → HDO bumps cached revision → NOs holding refs receive invalidation → recompose → new DeltaPlan with only the changed wave (Wave 6 typically). |
| Mapping blob URL changes (re-uploaded) | Blob store is immutable per digest; etcd update carries new digest. NO compares digests; equal = no-op, different = re-fetch + Wave 4 only. |
| Device disconnect mid-Wave 4 | DeltaCommands keyed by `command_id` (idempotent); on reconnect the agent's `last_known_revision` resumes from etcd; driver re-dispatches unfinished commands. |
| Two NICs in same VM target different VNETs | Independent NO actors, independent compositions. They share the HDO's global/group cache, but each NO subscribes to its own VNET subtree. |
| `Appliance` config changes | Wave 0 re-program. All ENIs MAY need re-validation but most fields (loopback IPs, sip prefixes) don't invalidate ENIs. Use a per-field invalidation table (TBD — schema design question). |

---

## 7. Open Schema-Design Questions

These need user input before I draft the per-message schemas.

1. **NICObject as goal state vs as ref bundle.** The proposal above treats
   the NIC payload published by the upstream control plane as a *reference*
   bundle (vnet_id, group_ids, …). Alternative: publisher denormalizes and
   inlines the full ENI body. Tradeoff: bandwidth + duplication vs
   subscriber-side join complexity. Which does the upstream plane emit?

2. **VnetMapping delivery format.** Three options:
   (a) blob = single proto-encoded `repeated VnetMapping` file;
   (b) blob = NDJSON / one-row-per-line for streaming parse;
   (c) blob = sorted by destination VIP for binary-search lookups.
   The driver's preference drives the choice.

3. **Group versioning granularity.** Do we version (a) the group as a whole,
   so any rule edit bumps `route_group.revision`, or (b) each rule
   individually? Per-rule versioning enables minimal Wave 6 patches but
   complicates referential integrity.

4. **Per-NIC vs per-ENI identity.** `NICObject.eni_id` is currently a
   spec field. Should it be **server-assigned** (FleetManager allocates an
   ENI id from a per-device pool) or **caller-supplied** (upstream emits a
   stable id)? Affects re-registration semantics (Specs/02 §4).

5. **Routing-type catalog.** `RoutingType` is an *appliance-global* table
   that ACTs as a small lookup catalog ("vnet-direct" → action chain).
   Should the upstream control plane publish it once per fleet, or per
   device? Cross-device drift here would be a debugging nightmare —
   recommend **fleet-wide singleton** with periodic per-device reconcile.

6. **HA scope ownership.** `HaSet` is appliance-global; `HaScope` is
   per-ENI; `HaScope` references `HaSet`. Confirm: HaSet lifecycle is owned
   by HDO actor, HaScope lifecycle is owned by NO actor?

7. **Inbound-only NICs (e.g. SLB VIPs).** Some ENIs accept only inbound
   traffic and have no outbound route table. Schema should model this as
   "outbound block optional in NicGoalState" rather than empty
   `route_group` — preserves the invariant that an empty group is an error.

---

## 8. Next Steps (after design review)

Once §7 is resolved:

1. Draft per-message **published config schemas** (one per topic) — these
   are what the upstream control plane emits and FleetManager parses.
2. Draft the composed **NicGoalState** schema.
3. Translate both sets to proto3 under `protos/` (extending
   `fleetmanager.v1` namespace; DASH-native fields stay as
   `repeated google.protobuf.Any` wrapping `DashObject`).
4. Update `Specs/02` and `Specs/03` cross-refs to point at the new schemas.

No proto file should be written before §7 is settled — schema decisions
there are load-bearing and hard to reverse without renumbering field tags.
