# FM Data Model Sync — Identity, Ownership, and DASH Alignment

**Date:** 2026-06-22
**Status:** Converged
**Participants:** Architect + AI
**Context:** Sync check before implementation kickoff. Validate that the mental model, our specs, and DASH align — and lock the identity convention so every downstream module agrees on what an "ID" is.

---

## 1. Mental Model Confirmed

Subscription path from Control Bridge (CB):

- FM subscribes to CB for VM/NIC programming intent.
- A VM's programming surface = one or more NICs (ENIs).
- Each ENI binds to exactly one VNET.
- VNET is the umbrella for shared policy: mappings (VIP→PA), route groups, ACL groups, PA validation lists, meter policies, tunnel/private-link config.
- Knowing a `vnet_id` → enumerate all dependent constructs.
- Knowing an `eni_id` → resolve NicSpec → traverse to VNET → traverse to mappings/routes/ACLs.

This matches DASH SAI:

- `DASH_VNET` owns mapping/encap state.
- `DASH_ENI` references `vnet_id`, `outbound_v4_routing_group_id`, `acl_group_id[]`, `meter_policy_id`.
- 6-slot ACL layout (stage1 + stage2, v4/v6, in/out) is the DASH ACL pipeline.

## 2. Critical Refinement — Reference, Not Ownership

When we say "NIC has Routes/ACLs," we mean **the NIC carries IDs that point to shared, VNET- or globally-owned objects**. The NIC does not own the rules inline.

Encoded in specs as:

- `NicSpec` carries `route_group_id`, `acl_group_ids[6]`, `meter_policy_id` — IDs only, no inline rules. (Per `Specs/FM/inc-closure-2.2-2.3.md`.)
- Registries (`VnetRegistry`, `NicRegistry`, `MappingManager`) use Acquire/Release/Ready with refcounts so shared objects can be GC'd only after the last referent releases.
- REG_007 (refcount underflow) is a CRITICAL runbook — the integrity of this reference model is monitored, not assumed.

Load-bearing: conflating "has" with "references" breaks GC, HA failover, and audit-trail correctness.

## 3. Identity Convention — Decision

**Decision:** Adopt CB-supplied IDs verbatim. No FM-internal ID minting, no translation layer.

- `eni_id`, `vnet_id`, `route_group_id`, `acl_group_id`, `mapping_id`, `meter_policy_id` flow from CB → T1 → registries → drivers → audit logs unchanged.
- Watch paths use CB IDs directly: `/config/v1/nic/{eni_id}`, `/config/v1/vnet/{vnet_id}`.
- Registry keys, log lines, metric labels, error messages, and runbook diagnostics all use the same ID string.

### Why

- One ID = one search hit across logs, traces, metrics, T1, T2, drivers, and CB.
- No translation table to keep consistent across HA pods (eliminates an entire class of split-brain edge cases).
- DASH semantics propagate cleanly — no FM-flavored aliases for upstream concepts.

### Implications

- CB ID format becomes part of FM's contract. If CB changes its ID scheme, FM must coordinate.
- ID validity rules (charset, length, uniqueness scope) are inherited from CB. `NicSpec` proto3 schema reserves `spec_revision` for object versioning; ID format itself is opaque to FM.
- Audit logs include `change_request_id` from CB to trace every ID's origin.

### Out of scope (explicitly)

- No vanity names. Human-readable names, if needed, are CB's responsibility to render on top of IDs.
- No FM-side ID rotation. IDs are stable for the lifetime of the object.
- No FM-generated synthetic IDs for "implicit" objects — every entity FM tracks must be named by CB.

## 4. Scale Dimensions Acknowledged

| Dimension | Order of Magnitude | Sharding/Indexing Strategy |
|---|---|---|
| VNET count | 10³ – 10⁴ | Single registry shard per FM pod; HA failover at VNET-scope granularity |
| ENI count | 10⁵ – 10⁶ | NicRegistry sharded by ENI ID hash; HDO actor per device |
| Mapping count (VIP↔PA) | 10⁷ – 10⁸ | MappingManager with VNET-scoped sub-indexes; lazy hydration |
| ACL/Route group references | scales with ENI × slot count | Refcounted; shared groups amortize storage |

Sharding decisions in registries already encode this asymmetry.

## 5. What This Locks In

- ✅ Registry keys = CB IDs (no translation).
- ✅ Watch paths use CB IDs.
- ✅ Refcount-based GC for shared objects (REG_007 runbook applies).
- ✅ DASH-aligned object model (VNET umbrella + ENI references shared groups).
- ✅ `NicSpec` proto3 schema in `inc-closure-2.2-2.3.md` is consistent with this decision.

## 6. Next

Implementation gate cleared. Per `MASTER_DESIGN_INDEX.md §16`:

- **Wave 1:** Registries (VnetRegistry, NicRegistry, MappingManager) — keyed on CB IDs.
- **Wave 2:** HDO/CO/NO actors — operating on registry-resolved state.
- **Wave 3:** Southbound driver skeleton.
- **Wave 4:** Adapter pod + integration tests.

User to confirm before planner kickoff:

1. Target tier (DV / VM / Container / Hyperscale) — affects which adapters/drivers land in early waves.
2. Depth per slice — skeleton + tests, or full feature-complete with e2e per Feature Addition Protocol.

## 7. References

- `Specs/FM/inc-closure-2.2-2.3.md` — NicSpec proto3 schema (IDs as foreign keys)
- `Specs/FM/registry-go-skeleton.md` — registry struct defs and Acquire/Release/Ready sigs
- `Specs/FM/registry-semantics-exact.md` — Acquire/Release/Ready semantics
- `Specs/Runbooks/REG_007_REFCOUNT_UNDERFLOW.md` — what happens when references go wrong
- `Specs/FM/MASTER_DESIGN_INDEX.md §16` — implementation wave order
- DASH SAI: `DASH_VNET`, `DASH_ENI` object definitions (canonical reference for object shapes)
