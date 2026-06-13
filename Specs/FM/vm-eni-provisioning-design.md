# VM ENI Provisioning — End-to-End Design

**Status:** Design proposal — rounds 1–3 of brainstorm locked. Schemas not yet drafted.

## Locked Decisions (rounds 1–3)

| # | Decision | Notes |
|---|----------|-------|
| 1 | **NIC payload = reference bundle** | Carries vnet_id, route_group_ids, acl_group_ids, meter_policy_id, ha_scope_ref. NO actor composes the full ENI program by joining VNET/group/global subtrees. |
| 2 | **Publisher boundary = orchestrator → etcd directly** | No FleetManager-mediated RPC for config writes. We design the topic tree and binary contract; orchestrator writes proto-binary values at agreed paths. |
| 3 | **ENI naming = `ENI_<DPU>_<VM-MAC>`** | FleetManager derives the eni_id deterministically from DPU name + VM MAC. Stable across re-registration; DPU prefix disambiguates VM migration between DPUs. Caller supplies NIC name + type (vm-nic vs appliance-nic) + MAC. |
| 4 | **RoutingType = fleet-wide singleton** | One canonical catalog at `/config/v1/global/routing_type/`. Per-device reconcile checks for drift but no overrides. |
| 5 | **Group versioning = whole-group revision** | Any rule add/remove/edit bumps `group.revision` by 1. Subscribers refetch the whole group. Atomic, simple referential integrity. |
| 6 | **Validation = strict subscriber-side, async reject events** | FleetManager treats etcd as untrusted input. Strict proto3 binary parse, ref-integrity check on subscribe. Failures emit `FleetEvent{kind=VALIDATION_REJECTED}` and quarantine the NO actor in `WAITING_VALID`. |
| 7 | **VNET watch ownership = HDO with refcount** | First NO attaching to vnet_X on this device triggers HDO subscribe to `/vnet/<X>/**`. Last NO detaching triggers unsubscribe. NOs receive updates via in-process actor messages. ~320k watches at fleet scale. |
| 8 | **VnetMapping delivery = sharded chunks + manifest** | Orchestrator chunks mapping into etcd keys `/vnet/<id>/mapping/<chunk-N>`, each <1 MiB of `repeated VnetMapping` proto-binary. Manifest key `/vnet/<id>/mapping/_manifest` lists chunks + digests. Subscriber watches manifest, fetches changed chunks. |
| 9 | **HA model = HaSet@HDO, HaScope@NO** | HaSet (appliance-global) lives in HDO cache; HaScope (per-ENI) lives in NO. NicGoalState references ha_scope_ref by id. |
| 10 | **Inbound-only NICs = optional inbound/outbound blocks** | NicGoalState has separate `outbound: NicOutbound?` and `inbound: NicInbound?`, each optional. Validation enforces at-least-one. SLB VIPs and similar patterns are first-class. |
| 11 | **Recovery = etcd CAS + mod_revision resume + periodic Reconcile** | Every write CAS'd against prior mod_revision. FleetManager records last-applied mod_revision per key. Watches resume from there on reconnect. Periodic Reconcile re-reads keys from scratch. |
| 12 | **Sharding & HA = ShardSet of 3 pods, range-shard by xxhash64(HostID), all replicas build identical state, Primary actuates** | Per Specs/04 + Specs/05: NBG/proxy does consistent-hash routing on device register; all 3 pods in the ShardSet watch the same etcd keys and build the same HDO/CO/NO tree; only the K8s-Lease holder issues southbound RPCs (others in HAL shadow mode). Failover ≈ leaseDuration (5s). No log-shipping, no extra cluster state — DashFabric is a deterministic projection of etcd. |
| 13 | **MAC = orchestrator-supplied** | NIC config payload carries the VM MAC. FleetManager derives `eni_id = ENI_<DPU>_<MAC>` from this. MAC space is owned by the tenant/orchestrator and survives migration. |
| 14 | **Bootstrap = HDO blocks NIC programming until global+group cache hydrated** | On HDO start, watch `/global/**` and `/group/**` and wait for the etcd `WatchResponse.created` + initial snapshot to drain. NICs sit in `WAITING_BOOTSTRAP` until then. Guarantees no NIC programs against a partial reference set. |
| 15 | **Multi-tenancy = single tree, `tenant_id` in payload, RBAC by prefix** | etcd tree shape is tenant-flat (`/config/v1/vnet/<vnet_id>/`). Each payload carries `tenant_id`. etcd RBAC scopes orchestrator credentials to specific prefix patterns. Cross-tenant references (shared appliance, fleet-wide RoutingType) live above tenancy. v1 ships the model; tenant enforcement is a key-policy concern, not a schema concern. |
| 16 | **Pod cache = single process-wide cache, fan-out via actor messages** | One concurrent map per object kind (Vnet, RouteGroup, AclGroup, Tunnel, etc.) keyed by id. A single watcher goroutine per topic prefix updates the cache and delivers invalidation messages to subscribing HDOs/NOs. Memory bounded by unique objects, not by device count. |
| 17 | **Composition = in-actor synchronous on every input change** | NO actor's reactive loop: on any input change (NIC spec, vnet, group, global), recompose NicGoalState in-actor, diff against last-composed, emit `DeltaCommand[]` to dispatcher. All 3 ShardSet pods do this work; only Primary's dispatcher actuates. Standbys cache the composed plan for instant failover. |
| 18 | **Wire format = ConfigEntry envelope** | Every etcd value is `ConfigEntry { metadata: ConfigMetadata, payload: bytes }` where `ConfigMetadata = { schema_version, kind, revision, tenant_id, trace_context, payload_digest, issued_at }`. Payload bytes are the proto-binary of the kind-specific message. Subscriber unwraps, validates digest, decodes by kind. |
| 19 | **Topic tree authority** | This document's §2 topic tree supersedes the sketch in `Specs/03 §3`. Specs/03 will be updated to cross-reference here. |
| 20 | **NIC spec carries explicit `nic_type` + `mode` enums** | `nic_type: { VM_NIC, APPLIANCE_NIC, SLB_VIP }`, `mode: { FULL_DUPLEX, INBOUND_ONLY, OUTBOUND_ONLY }`. Drives ENI naming variant, actor variant, and validation of which blocks must be present in NicGoalState. |
| 21 | **ACL binding = two ordered arrays per family (in/out), one slot per stage** | NIC spec carries `inbound_acl_v4: [stage1, stage2, stage3]` and `outbound_acl_v4: [...]` (and v6 equivalents). Each slot is `AclGroupId?` (null = stage disabled). Matches DASH's stage1/2/3 pipeline directly. |
| 22 | **Route binding = NIC binds RouteGroup directly; FleetManager materializes EniRoute** | NIC spec: `outbound_route_group_v4: RouteGroupId?`, `outbound_route_group_v6: RouteGroupId?`. FleetManager generates `EniRoute = { eni_id, route_group_id, family }` during composition as a derived object. Orchestrator never writes EniRoute directly. |
| 23 | **RouteRule = inline list inside NIC spec** | Per-ENI policy-routing rules (`route_rules: [RouteRule...]`) live in NIC spec, not in a shared group. RouteRule has match (prefix, port, proto) + action (next-hop, encap, drop). Flows with NIC's lifecycle. |
| 24 | **Meter binding = per-direction MeterPolicy** | NIC spec: `inbound_meter_policy: MeterPolicyId?`, `outbound_meter_policy: MeterPolicyId?`. Each direction independently bound. Matches typical billing/QoS patterns. |
| 25 | **Validation errors = sibling keys in `/status/v1/` subtree** | On reject, FleetManager writes `/status/v1/<original_path>/_error` carrying `{ rejected_revision, error_code, message, trace_id, ts }`, TTL 1h. Orchestrator watches `/status/v1/` to discover its own bad writes. Auto-clears on successful re-publish. |
| 26 | **`ConfigMetadata.payload_digest = SHA-256`** | 32-byte digest. Used for integrity, idempotency keys, and Reconcile drift detection. Hashing cost negligible vs proto-decode. |
| 27 | **Change detection = composed-state hash + full structure diff on change** | NO actor stores last-composed `NicGoalState`; on input change recomposes and computes `content_hash = SHA-256(canonical_serialization(NicGoalState))`. If unchanged → no-op. If changed → full proto diff produces per-field deltas mapped to DASH-object CREATE/UPDATE/DELETE. content_hash is also the value `Reconcile` compares against device-reported hash. |


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

## 7. Still-Open Design Questions

27 decisions locked across 7 rounds. Remaining edge cases (HaScope active/standby semantics, partial-DPU failure modes, multi-region routing) will be surfaced during schema drafting where they have concrete context.

---

## 8. Next Steps (after design review)

1. Resolve round-4 questions.
2. Draft per-topic **published config schemas** (one per agreed etcd path).
3. Draft the composed **NicGoalState** schema.
4. Translate both sets to proto3 under `protos/` (extending
   `fleetmanager.v1` namespace; DASH-native fields stay as
   `repeated google.protobuf.Any` wrapping `DashObject`).
5. Update `Specs/02` and `Specs/03` cross-refs to point at the new schemas.

No proto file should be written before round 4 is settled.
