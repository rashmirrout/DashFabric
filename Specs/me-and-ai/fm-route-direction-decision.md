# Route Direction Decision: Why Separate Inbound/Outbound Validation

> **Status:** Design decision retrospective
> **Date:** 2026-06-14
> **Participants:** Architect (FM), User (routing requirements)
> **Outcome:** Routes partitioned by direction during hydration; outbound uses full gates, inbound uses minimal gates; no BIDIR concept

## 1. The Problem: How Should FM Handle Mixed Inbound/Outbound Routes?

When CP sends a RouteGroup with both inbound and outbound entries mixed in one list, FM must decide:
- Should both directions follow the same validation pipeline?
- Should they be validated separately?
- Should both be programmed into the same SAI table, or separate tables?
- Can a single route serve both inbound and outbound ("BIDIR")?

## 2. Three Candidate Models

### Model A: Single Validation Pipeline (Direction Ignored)
```
All routes (inbound + outbound mixed) follow the same validation path
- Validate peering for ALL routes
- Validate mapping for ALL routes
- Validate tunnel for ALL routes
Program everything into one table (SAI decides internally)
```
**Tradeoff:**
- (-) Over-validates inbound routes (which are simple filters)
- (-) Misses that inbound doesn't need peering gates
- (-) Potential false failures (inbound rule waits for peer mapping that's not needed)
- (-) Inefficient: inbound rules stuck INCOMPLETE waiting for outbound dependencies

### Model B: BIDIR Support (One Route, Two Behaviors)
```
Routes can be BIDIR (apply to both inbound and outbound)
BIDIR route = "program this rule to both SAI tables with same action"
```
**Tradeoff:**
- (-) Inbound and outbound are fundamentally different pipelines
  - Outbound is "action" (PEERING means send here)
  - Inbound is "filter" (PERMIT means accept from here)
  - Same route can't mean both simultaneously
- (-) Operational confusion (which direction does the action apply to?)
- (-) Not how cloud providers model it (Azure, GCP, AWS all require separate rules)

### Model C: Direction-Aware Partitioning (SELECTED)
```
Routes have explicit direction field (INBOUND or OUTBOUND, not BIDIR)
FM partitions during hydration:
  - Outbound routes → full validation (peering, mapping, tunnel, meter, VIP, PE)
  - Inbound routes → minimal validation (just action sanity)
Program into separate SAI tables per direction
```
**Tradeoff:**
- (+) Matches DASH upstream (separate SAI tables)
- (+) Matches cloud provider models (separate rules for each direction)
- (+) Natural for operators (prefix with direction is normal)
- (+) Inbound rules never blocked by outbound dependencies
- (+) Clear semantics: outbound=action, inbound=filter
- (-) Requires CP to send explicit direction on each route

## 3. DASH Upstream Evidence

From `06-Routing-Pipeline.md`:

**Outbound (complex):**
- LPM lookup on dest CA
- Match route → action (VNET, PEERING, PRIVATELINK, etc.)
- Post-routing: mapping lookup, encap, tunnel decisions
- SAI table: `SAI_OUTBOUND_ROUTING_TABLE`

**Inbound (simple):**
- After decap, match inner dest CA against inbound rules
- Rare actions: PERMIT, DROP, REDIRECT (mostly identity)
- SAI table: `SAI_INBOUND_ROUTING_TABLE`

**Two separate SAI tables** at the hardware level confirms **direction-aware design**.

## 4. Why Model C Wins: Operational Reality

### Use Case: VNET-VNET Peering

Both directions REQUIRED for bidirectional traffic:

```
vnet-acme (10.0.0.0/16) ←→ vnet-shared (10.200.0.0/16)

vnet-acme outbound route:
  prefix: 10.200.0.0/16
  action: PEERING (send to vnet-shared)
  direction: OUTBOUND
  → requires: peer validation, mapping, tunnel

vnet-acme inbound route:
  prefix: 10.200.0.0/16
  action: PERMIT (accept from vnet-shared)
  direction: INBOUND
  → requires: basic sanity (that's it!)
```

**Why separate:** vnet-acme needs outbound ACTION ("send east-west") + inbound FILTER ("accept return traffic"). They're not the same rule.

**Model A failure:** If inbound route waits for "peering validation" that it doesn't need, return traffic stalls.

**Model B fails:** BIDIR={action:PEERING} has no meaning for inbound (PEERING doesn't make sense as a filter).

**Model C succeeds:** Outbound validates peering (yes, needed), inbound validates minimally (not needed). Both transition to READY independently.

### Use Case: PrivateLink

```
outbound route to PE:
  prefix: 10.254.0.1/32 (PE endpoint)
  action: PRIVATELINK
  direction: OUTBOUND
  → requires: PE mapping validation

inbound rule (rare):
  prefix: 10.254.0.1/32
  action: PERMIT
  direction: INBOUND
  → requires: just sanity (route prefix valid? yes.)
```

If inbound waits for outbound PE mapping (Model A), unnecessary delay. Model C: inbound validates immediately, outbound can be INCOMPLETE without blocking inbound.

## 5. Why BIDIR Doesn't Make Sense

**Question: "What if I want one rule for both directions?"**

**Answer: That's not a real operation.**

In every routing system (cloud, edge, appliance):
- **Ingress rule** = "from this prefix, take action X"
- **Egress rule** = "to this prefix, take action Y"

They're different concepts. Even if X and Y are the same (e.g., both PERMIT), they're:
- Evaluated in different pipelines
- Stored in different tables
- May have different performance characteristics

**Internet evidence:**
- Azure route tables: direction per route
- AWS route tables: always directional (separate for inbound/outbound)
- GCP firewall rules: explicit INGRESS/EGRESS
- eBPF tc rules: separate ingress/egress hooks

**No cloud provider supports BIDIR** because it doesn't align with hardware pipeline reality.

## 6. Gate Enforcement by Direction

**OUTBOUND routes (full gates):**
- Peering validation: is target VNET peered?
- Mapping validation: do mappings exist? (async, soft-fail if missing)
- Tunnel validation: does tunnel exist?
- Meter validation: do meter policies exist? (async, soft-fail)
- VIP validation: are VIP backends ready? (async, soft-fail)
- Private Link validation: does PE mapping exist? (async, soft-fail)

**INBOUND routes (minimal gates):**
- Action sanity: is action valid for inbound? (PERMIT, DROP, REDIRECT only)
- Prefix validation: is prefix valid? (basic parse check)
- No peering, no mapping, no tunnel, no meter, no VIP, no PE gates

**Why:** Inbound is a **filter** — it doesn't need resources. Outbound is an **action** — it needs all dependencies resolved.

## 7. CP Payload Structure

CP sends mixed routes with direction field:

```yaml
routes:
  outbound_routing_group_id: "rg-egress-v4"
  inbound_routing_group_id: "rg-ingress-v4"
  entries:
    - prefix: "10.1.0.0/16"
      action: "LOCAL"
      direction: "OUTBOUND"      # Explicit direction
    
    - prefix: "10.200.0.0/16"
      action: "PEERING"
      target_vnet_id: "vnet-shared"
      direction: "OUTBOUND"      # Different direction
    
    - prefix: "10.1.0.0/16"
      action: "PERMIT"
      direction: "INBOUND"       # Same prefix, different direction = separate rule
```

**FM's job:** Partition (split by direction) → validate separately → program into separate SAI tables.

## 8. Trade-Off Summary

| Aspect | Model A (Single) | Model B (BIDIR) | Model C (Partitioned) | Winner |
|--------|-----------------|-----------------|----------------------|--------|
| **DASH alignment** | ❌ (ignores separate SAI tables) | ❌ (BIDIR not in DASH) | ✅ (separate tables) | C |
| **Operator simplicity** | ❌ (confusing gates) | ❌ (ambiguous semantics) | ✅ (clear direction per route) | C |
| **Inbound not blocked by outbound** | ❌ (waits for outbound deps) | ❌ (both blocked if one fails) | ✅ (independent validation) | C |
| **Cloud provider alignment** | ❌ (Azure/AWS don't do this) | ❌ (no provider supports BIDIR) | ✅ (matches all clouds) | C |
| **Peering return path** | ⚠️ (works but delayed) | ❌ (confusing) | ✅ (natural two-rule model) | C |
| **Implementation effort** | Low (one pipeline) | Medium (BIDIR branching) | Low (simple partition) | C |

**Winner: Model C (Direction-Aware Partitioning)** — simplest, clearest, matches DASH + all cloud providers, prevents unnecessary blocking.

## 9. Implementation Strategy

**No complex state machine.** Just:

1. **Partition on arrival** — split routes into outbound/inbound lists
2. **Validate separately** — outbound gets full gates, inbound gets minimal gates
3. **Program separately** — outbound → SAI_OUTBOUND_ROUTING_TABLE, inbound → SAI_INBOUND_ROUTING_TABLE

Result: ENI has two readiness flags (outbound_ready, inbound_ready) that combine into direction_dependency_met.

## 10. References

- `Specs/Learning-DashNet/06-Routing-Pipeline.md` — DASH routing pipelines (two separate paths)
- `Specs/cb_fm_protos/topics/route.proto` — RouteDirection enum (INBOUND, OUTBOUND)
- `Specs/FM/fm-route-direction-design.md` — Detailed implementation blueprint
- DASH SAI spec — Separate routing tables per direction
