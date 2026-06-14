# Peering, VIPs, and Routing Completeness — Retrospective

> **Date:** 2026-06-14
> **Topic:** Filling routing gaps in the CB wire contract — VNET peering,
> VIPs, meters, Private Link, and direction-aware routes.
> **Outcome:** Updated `topics/vnet.proto`, enhanced `topics/route.proto`,
> new `topics/vip.proto` and `topics/meter.proto`, CB/02 §5.4 on
> peering-triggered subscriptions.

This round resolved a fundamental question: **what does "routing" actually
need to represent at the FM level?** The agent's earlier work suggested
bundling; we converged on scoping rules instead.

## 1. Opening question

User observed: "device topic gives VM/container details. subscribe to
container, you get vnet details. but DASH spec has more for routes —
SNAT, gateway, ExpressRoute, tunnels, Private Link. Where do we subscribe
for that, and how do we store and consume?"

This was a good catch. Our initial `/routes/<rg>` proto was skeletal:
only LOCAL, PEERING, ENCAP, DROP. Missing: tunnels, VIPs, SNAT pools,
Private Link endpoints, inbound validation.

## 2. My initial confusion

I asked four sharpening questions:

1. **VIP scoping:** Global or VNET-scoped?
2. **ExpressRoute binding:** Vendor decides (CP) or FM decides?
3. **Private Link:** Separate resource or routing attribute?
4. **Inbound:** Separate topic or direction discriminator?

User answered: **check DASH spec for VIPs** (before concluding), **CP
decides ER binding**, **PE is routing attribute**, **direction in
RouteGroup**.

Then user asked me to evaluate an agent's `EniProvisioningPayload` design
and extract the good parts.

## 3. Agent's work — what was good, what was misaligned

**Excellent:**
- Bundling concept (recognized that ENI provisioning is coordinated)
- Routing actions taxonomy (LOCAL, PEERING, PRIVATELINK, ENCAP)
- Direction discriminator (INBOUND/OUTBOUND)
- Redis translation spec (how DPU agent maps proto → APPL_DB)
- `transaction_id + timestamp` tracing

**Misaligned with our model:**
- Monolithic `EniProvisioningPayload` vs. symmetric topic broker — we receive
  separate events on separate topics, not one blob
- No watermark/content_hash/desired_version in the agent's proto
- Missing distinction between FM-owned and CP-owned topics
- Meters were inline; we needed them as independent policy objects

**Gap:** Agent didn't answer the VIP/ER/PE scoping questions.

## 4. Convergence on scoping rules

### VIPs are VNET-scoped

From DASH SAI: each VIP belongs to one VNET. Multiple ENIs *within* that
VNET share the same VIP (cardinal rule). Cross-VNET reach to a VIP is
via peering + routing, not direct VIP reference.

**Action:** New topic `/config/vnets/<vnet_id>/vips/<vip_id>` with
`vnet_id` canonical key. Includes direction (INBOUND/OUTBOUND/BIDIR).

### Meters are reusable policy objects

Like ACL groups, meters are independently named and referenced. Many ENIs
share one meter policy.

**Action:** New topic `/config/meters/<meter_id>`. Changed `RouteEntry`
to reference `meter_id` instead of inline CIR/CBS.

### Private Link is a route action, not a resource

PE (Private Endpoint) is not a separate object. It's a routing rule:
destination prefix → ROUTE_PRIVATELINK with PE endpoint details
(overlay_sip, underlay_dip, NAT port range).

**Action:** Added `ROUTE_PRIVATELINK` action + `PrivateLinkDetails`
message to `RouteEntry`. SNAT per-PE (inline).

### Peering is a dependency, not optional

This was the key insight. User said: "FM must know so it can form best
routes optimally — but I'm not so sure."

I reframed: **Peering is not an optimization lever.** It's a
**reachability constraint + subscription trigger.** When FM sees
`peer_vnet_ids = [B, C]`:

- FM immediately subscribes to B & C's `/mappings/`, `/routes/`, `/vnets/`
- During ENI hydration, FM validates: route target is peered or self
- Prevents "route present but target data not yet hydrated" race
- Pre-hydration keeps the dependency graph consistent

Not optimization; **dependency management**.

### Inbound and outbound in one topic

User confirmed: routes should include direction (INBOUND/OUTBOUND) as a
discriminator, not separate topics.

**Action:** Added `RouteDirection` enum to `RouteEntry`. Same route group,
two directions.

## 5. Before / after

### Before

```
/routes/<rg>:
  - route_entry { prefix, action=ROUTE_VNET | DIRECT | DROP, target_vnet }
```

Limited. No tunnels, VIPs, SNAT, PE, inbound. Peering was a comment in
architecture docs, not a subscription strategy.

### After

```
/routes/<rg>:
  - route_entry {
      prefix,
      direction: INBOUND | OUTBOUND,
      action: ROUTE_VNET | VNET_DIRECT | DIRECT | DROP | DENY | PERMIT
              | PRIVATELINK | SNAT | SERVICE_TUNNEL,
      target_vnet_id,        // for VNET/VNET_DIRECT
      underlay_ip,           // for DIRECT
      privatelink { overlay_sip, underlay_dip, vni, nat_port_range },  // for PE
      snat_pool_id,          // for SNAT
      tunnel_id,             // for SERVICE_TUNNEL (ER, etc.)
      meter_id,              // policy ref
      direction
    }

/vnets/<vnet>:
  + peer_vnet_ids: []    // triggers subscriptions to peers' resources

/vnets/<vnet>/vips/<vip>:  // NEW — VNET-scoped
  vip_type: LB_FRONTEND | GATEWAY | SERVICE_TUNNEL
  direction: INBOUND | OUTBOUND | BIDIR
  nat_pool_id  // optional, for reverse path

/meters/<meter>:  // NEW — reusable policies
  cir, cbs, [pir, pbs], algorithm
```

Peering flow:

```
FM receives VnetConfig { peer_vnet_ids = [B, C] }
  ↓
FM opens subscriptions:
  - /mappings/B, /mappings/C
  - /routes/rg_B, /routes/rg_C
  - /vnets/B, /vnets/C
  ↓
When ENI in A routes to B:
  FM validates: B ∈ peer_vnet_ids ∧ B's mappings hydrated
  ↓
If valid: route is usable immediately (no fetch on-demand)
```

## 6. Artefacts produced

- `topics/vnet.proto` — added `peer_vnet_ids`
- `topics/route.proto` — enhanced with direction, new actions, PrivateLinkDetails
- `topics/vip.proto` — NEW, VNET-scoped VIPs
- `topics/meter.proto` — NEW, reusable meter policies
- `CB/02-cb-low-level-design-lld.md` § 5.4 — peering-triggered subscriptions
- `cb_fm_protos/README.md` — layout updated with new topics

## 7. Lessons

1. **Distinguish "who decides" from "who subscribes."** CP decides ER
   binding; FM reads/validates it. This shapes whether it's a separate
   topic (no — it's metadata in a route) vs. a top-level object (yes —
   if FM orchestrates it).

2. **Peering is not an optimization.** It's dependency management. The
   question wasn't "should we pre-fetch?" but "how do we keep route
   validity consistent?" Pre-hydration is the answer.

3. **DASH-aligned scoping matters.** VIPs are VNET-local per DASH SAI.
   Not "what makes sense for FM" but "what does DASH say." Check first.

4. **The bundled-blob instinct was wrong.** The agent's
   `EniProvisioningPayload` bundling seemed natural (one ENI = one blob).
   But our symmetric topic broker keeps things simpler: FM subscribes to
   all topics, hydrates registries as data arrives, then stitches a view
   for DPU Agent on request. No bundling transport needed.

## 8. Open questions for FM design

Now the hard part: **Does FM's registry architecture actually support
peering?** The questions:

- Does `VnetRegistry` track peers and trigger cascading subscriptions?
- Can `NicRegistry.Hydrate(eni_id)` validate route targets are peered?
- What happens if a peer is removed while routes reference it?
- How does the cardinal-rule sharing matrix change with peering?

These are FM-side questions, not CB side. User will address them next
round.

## 9. Forward pointers

- FM design must model peering as a subscription dependency (not optional)
- Conformance suite T10: vendor emits routes only to peered/self VNETs
- DPU Agent translation spec (new section in CB/03): how to translate
  route actions + VIP refs + meter refs into Redis APPL_DB tables
- `cbsim` scenario: cold-boot with peering, verify ENI hydrates peer
  mappings before marking PROGRAMMED
