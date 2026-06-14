# Private Link Design Decision: Why VNetMapping Integration (Not Separate PeRegistry)

> **Status:** Design decision retrospective
> **Date:** 2026-06-14
> **Participants:** Architect (FM), User (DASH requirements)
> **Outcome:** PE endpoints modeled as VNET-scoped VnetMapping entries (not separate PE resource); routes embed PE details inline

## 1. The Problem: Where Does FM Learn About PE Endpoints?

When an operator creates a PrivateLink endpoint:
- Allocates overlay IP `10.42.99.100/32` in tenant VNET
- Maps it to service ingress underlay IP `100.72.5.20`
- Assigns VNI `900050` for encap identification
- Specifies port range `(50000, 60000)` for NAT reverse path

How does FM know:
- "This overlay IP is a PE endpoint (not a regular VM)"
- "Direct it to this underlay IP with this VNI"
- "Use this tunnel and port range"?

## 2. Three Candidate Models

### Model A: PE as Separate Registry
```
PeRegistry: "Here are all PE endpoints"
  10.42.99.100 → (100.72.5.20, vni=900050, port_range=...)
  
Routes reference PE explicitly: route.pe_id
Routes with action=ROUTE_PRIVATELINK → lookup route.pe_id in PeRegistry
```
**Tradeoff:**
- (+) Clear separation: routes say "use PE X"
- (-) New registry type (more complexity)
- (-) Not how DASH upstream models it (PE is in VnetMapping)

### Model B: PE as Route-Embedded Details
```
Routes embed PE inline: route.privatelink = {overlay_sip, underlay_dip, vni, port_range}

Routes with action=ROUTE_PRIVATELINK carry all PE info
```
**Tradeoff:**
- (+) Simple: all info in route
- (-) Duplicates PE info across multiple routes
- (-) Hard to change PE endpoint without touching all routes
- (-) Loses single source of truth

### Model C: PE as VNetMapping Entry (DASH Native)
```
VnetMapping entry:
  overlay_ip=10.42.99.100
  underlay_ip=100.72.5.20
  tunnel_override_id=...
  routing_action_hint="PRIVATELINK"

Routes reference PE via action=ROUTE_PRIVATELINK (overlay_sip in route matches mapping)
MappingManager already populates and tracks VnetMapping
```
**Tradeoff:**
- (+) Single source of truth (VnetMapping)
- (+) PE managed same as other VNET addresses
- (+) Matches DASH upstream exactly (VnetMappingTable per VNET)
- (+) No new registry (reuse MappingManager)
- (+) Late binding supported (PE added to mapping after route exists)
- (-) Requires understanding VnetMapping architecture

## 3. DASH Upstream Research

### What DASH Scenario Shows (from `12-Scenario-PrivateLink-and-ServiceTunnel.md`)

**PrivateLink setup:**
```yaml
VNET Mapping entry:
  overlay_ip_v4: "10.42.99.100"
  underlay_ip_v4: "100.72.5.20"
  tunnel_override_id: "tun-privatelink-acme-storage"
  routing_action_hint: "PRIVATELINK"

Route:
  dst_prefix: "10.42.99.100/32"
  action: "PRIVATELINK"
  privatelink:
    overlay_sip: "10.42.99.100"
    underlay_dip: "100.72.5.20"
    vni: 900050
    nat_port_range_start: 50000
    nat_port_range_end: 60000
```

**Key insight**: PE endpoint is stored in **VnetMapping** (per-VNET), not a separate resource. The mapping entry carries:
- `overlay_ip`: the PE's private IP (allocated in VNET)
- `underlay_ip`: the PE's backend ingress address
- `routing_action_hint="PRIVATELINK"`: signals this is a PE, not a regular VNET member
- `tunnel_override_id`: optional tunnel for this specific PE

### Why DASH Does This

1. **VnetMapping is the VNET's address-to-underlay index** — every IP in the VNET needs a mapping (regular VMs, PE endpoints, reserved IPs, etc.)
2. **PE is just another mapped address** — with `routing_action_hint` to signal "this is special"
3. **Scales naturally** — millions of VNET addresses including hundreds of PE endpoints, all in one sharded mapping table
4. **One source of truth** — operator updates VnetMapping, not a separate PE catalog + routes

## 4. Why Model C Wins

### Operational Simplicity
```
Create PE endpoint:
  1. Update VnetMapping: add entry with overlay_ip=10.42.99.100, underlay_ip=100.72.5.20, routing_action_hint=PRIVATELINK
  2. Create Route: dst_prefix=10.42.99.100/32, action=ROUTE_PRIVATELINK
  
  FM automatically detects PE from VnetMapping (no PE ID needed)
  Single source of truth: VnetMapping
```

### Implementation Simplicity
```
No new registry type. MappingManager already:
  - Subscribes to VnetMapping chunks
  - Validates chunks against manifest
  - Notifies NicRegistry when mapping complete
  
Just add: "If routing_action_hint=PRIVATELINK, track as PE mapping"
```

### Alignment with DASH SAI
From `protos/published/vnet-mapping.md`:
- DASH_VNET_MAPPING_TABLE is per-VNET, keyed by `overlay_ip`
- Each row carries `overlay_ip`, `underlay_ip`, `tunnel_override_id`, `routing_action_hint`
- PE entries are indistinguishable from regular entries except for `routing_action_hint`

### Handles Late Binding
```
Timeline:
  T0: Operator creates Route with action=ROUTE_PRIVATELINK (mapping doesn't exist yet)
      → FM sees route, marks ENI INCOMPLETE (waiting for PE mapping)
  
  T5: Operator creates VnetMapping entry for PE
      → MappingManager detects new entry with routing_action_hint=PRIVATELINK
      → Signals NicRegistry to re-validate affected ENIs
      → ENI transitions INCOMPLETE → READY
```

### Supports Multiple PE per VNET
```
One VNET can have multiple PE endpoints:
  10.42.99.1/32   → Azure Storage (underlay 100.72.5.20)
  10.42.99.2/32   → Azure SQL (underlay 100.72.6.30)
  10.42.99.3/32   → KeyVault (underlay 100.72.7.40)

Each = one VnetMapping entry with routing_action_hint=PRIVATELINK
Routes reference via overlay_sip (no extra ID field)
```

## 5. Trade-Off Summary

| Aspect | Model A (Separate PeRegistry) | Model B (Route-Embedded) | Model C (VNetMapping) | Winner |
|--------|------|------|------|--------|
| **DASH alignment** | ❌ (not upstream pattern) | ❌ (details embedded, not in mapping) | ✅ (exact match) | C |
| **Single source of truth** | ✅ (PeRegistry) | ❌ (duplicated in routes) | ✅ (VnetMapping) | C |
| **New registry needed** | Yes (complexity) | No | No (reuse MappingManager) | C |
| **Late binding support** | ✅ | ✅ | ✅ | Tie |
| **Multiple PE per VNET** | ✅ | ✅ | ✅ | Tie |
| **Operator simplicity** | Medium (manage PE resource + routes) | Low (all in routes, duplicated) | High (one VnetMapping update) | C |
| **Implementation effort** | High (new registry, signals) | Low (just use route details) | Low (enhance MappingManager) | C |
| **Scales to 100k PEs** | ✅ (refcounted) | ✅ (routes track all) | ✅ (sharded VnetMapping) | Tie |

**Winner: Model C (VNetMapping Integration)** — simplest, most aligned with DASH, reuses existing infrastructure.

## 6. Implementation Strategy

**No separate PeRegistry.** Instead:

1. **MappingManager enhancement** — already subscribes to VnetMapping chunks; add PE detection:
   ```
   If MappingEntry.routing_action_hint == "PRIVATELINK":
     Track as PE mapping
     Signal: "PeMappingAdded" or "PeMappingRemoved"
   ```

2. **NicRegistry enhancement** — during ENI hydration, when validating route with `action=ROUTE_PRIVATELINK`:
   ```
   Check: does route.privatelink.overlay_sip exist in VnetMapping?
   If missing: soft-fail (ENI → INCOMPLETE)
   If present: extract underlay_dip, vni, port_range from route + mapping
   ```

3. **Route details** — route.privatelink already carries:
   - `overlay_sip`: overlay source IP (PE's private IP)
   - `underlay_dip`: underlay destination (PE ingress)
   - `vni`: encap VNI
   - `nat_port_range_start/end`: port range for reverse-path NAT

**Result:** PE programming emerges from MappingManager + NicRegistry collaboration (same pattern as peering + VIPs).

## 7. References

- `Specs/Learning-DashNet/12-Scenario-PrivateLink-and-ServiceTunnel.md` — DASH model (PE in VnetMapping)
- `Specs/protos/published/vnet-mapping.md` — VnetMapping structure with routing_action_hint
- `Specs/cb_fm_protos/topics/route.proto` — RouteAction.ROUTE_PRIVATELINK, PrivateLinkDetails
- `Specs/FM/fm-private-link-design.md` — Detailed implementation blueprint
