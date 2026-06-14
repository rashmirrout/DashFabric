# ExpressRoute Design Decision: Why Route-Level (Not VNET-Level) Service Tunnels

> **Status:** Design decision retrospective (comprehensive research + debate + conclusion)
> **Date:** 2026-06-14
> **Participants:** Architect (FM), User (routing requirements)
> **Outcome:** ExpressRoute modeled as route-level SERVICE_TUNNEL (DASH native), not VNET-level binding (cloud provider pattern)

## 1. Initial Questions & User Guidance

User posed three design questions:

**Q1: Is ER binding route-level (like Service Tunnel) or VNET-level?**
- Option A: Route-level — each route has `tunnel_id` (matches service tunnel pattern)
- Option B: VNET-level — VNET declares which tunnel to use for ER
- Option C: ENI-level — ENI-specific ER tunnel binding

**Q2: Should ER tunnel validation be hard-fail or soft-fail?**
- Hard-fail: ENI → FAILED if tunnel missing (operator must fix)
- Soft-fail: ENI → INCOMPLETE if tunnel missing (wait for tunnel, retry)

**Q3: Does ER need special SNAT handling like Service Tunnels?**
- Yes: Each ER route includes snat_pool_id (port range allocation)
- No: SNAT handled centrally by CP (not per-route)

**User's initial recommendation:** "My recommendation (as architect): Option A (route-level), Soft-fail, Yes to SNAT."

**User's directive:** "I confirm as it looks good to me. but before moving on research in dash spec and internet on what other cloud providers do."

---

## 2. Research: Cloud Provider Models

### 2.1 Azure ExpressRoute

**Binding Model:**
- **VNET-level** (not route-level)
- One ExpressRoute circuit → one VNET via ExpressRoute virtual network gateway
- Peering configuration (private, Microsoft) at circuit level
- BGP-managed routing (routes exchanged dynamically via BGP sessions)

**Key architecture:**
```
On-premises ← Layer 3 connection ← ExpressRoute Circuit
                                          ↓
                                    (BGP peering)
                                          ↓
                         ExpressRoute Virtual Network Gateway
                                          ↓
                                    Azure VNET
```

**Key insight:** ExpressRoute is tied to the **VNET gateway**, not individual routes. Routes are managed via **BGP peering**, not explicit route-level binding.

**Operator workflow:**
1. Create ExpressRoute circuit
2. Link VNET to circuit via ExpressRoute gateway
3. Configure BGP peering to exchange routes
4. Routes are learned dynamically (not manually specified per route)

---

### 2.2 AWS Direct Connect

**Binding Model:**
- **VPC-level** (not route-level)
- Virtual Interface (VIF) connects to VPC via Virtual Private Gateway (VGW)
- BGP-managed routing (routes exchanged dynamically)
- Multiple VIFs can connect to different VPCs from same DX connection

**Key architecture:**
```
On-premises ← Layer 2/3 connection ← Direct Connect Connection
                                            ↓
                                    (Virtual Interface)
                                            ↓
                         Virtual Private Gateway (VGW)
                                            ↓
                                        AWS VPC
```

**Key insight:** VIF binds to **VGW (VNET/VPC scope)**, not to individual routes. **BGP manages routing dynamically.**

**Operator workflow:**
1. Create DX connection
2. Create Virtual Interface (VIF) for the connection
3. Associate VIF with VGW (VPC-scoped)
4. Configure BGP peering to exchange routes
5. Routes learned dynamically from on-premises via BGP

---

### 2.3 Google Cloud Interconnect

**Binding Model:**
- **VPC-level** (not route-level)
- Interconnect attachment → VPC via Cloud Router
- BGP-managed routing (Cloud Router exchanges routes with on-premises)
- Single Interconnect can attach to multiple VPCs

**Key architecture:**
```
On-premises ← Dedicated connection ← Cloud Interconnect
                                           ↓
                                    (Attachment)
                                           ↓
                              Cloud Router (BGP)
                                           ↓
                                       Google VPC
```

**Key insight:** Attachment binds to **Cloud Router / VPC**, not individual routes. **BGP is the routing mechanism.**

**Operator workflow:**
1. Provision Interconnect connection
2. Create Attachment (VPC scope)
3. Configure Cloud Router to exchange routes via BGP
4. Routes learned dynamically

---

### 2.4 Common Pattern Across All Cloud Providers

| Cloud Provider | Binding Scope | Routing Mechanism | Flexibility |
|---|---|---|---|
| **Azure** | VNET-level (gateway) | BGP-managed | Static (one circuit per VNET) |
| **AWS** | VPC-level (VGW) | BGP-managed | Semi-flexible (one VIF per VPC, but multiple VIFs per connection) |
| **Google** | VPC-level (Router) | BGP-managed | Flexible (one attachment per VPC, but shared Interconnect) |

**All use VNET/VPC scope, not route scope. All use BGP for dynamic routing.**

---

## 3. Research: DASH Service Tunnel Model

From `12-Scenario-PrivateLink-and-ServiceTunnel.md` and `protos/published/tunnel.md`:

**DASH Service Tunnel binding:**
```yaml
Route entry (per-route binding):
  priority: 250
  dst_prefix: "20.150.0.0/16"
  action: "SERVICE_TUNNEL"
  routing_type: "service-tunnel-storage-v1"
  tunnel_id: "tun-service-storage-westus2"
  service_vni: 900001
  snat_pool_id: "snat-pool-tenant-acme"

Tunnel object (device-global):
  tunnel_id: "tun-service-storage-westus2"
  encap_type: "VXLAN"
  src_underlay_ip_v4: "100.64.7.5"
  dst_underlay_ips_v4: ["100.71.0.10"]
  udp_dst_port: 4789
```

**Key differences from cloud providers:**
1. **Route-level binding** (not VNET-level)
2. **Explicit per-route tunnel references** (not BGP-managed discovery)
3. **Per-route SNAT pool** (not centralized NAT)
4. **Service VNI** for tenant identification (not learned from BGP)

**DASH flexibility:**
- Different routes to same service can use different tunnels
- Per-route SNAT pool allocation
- Operator has fine-grained control per prefix

---

## 4. Key Insight: DASH ≠ Cloud Providers

**Cloud providers assume:**
- One primary connectivity (ER/DX/Interconnect) per VNET/VPC
- BGP discovers and manages routes dynamically
- Operator configures at VNET/VPC scope, not per-route

**DASH assumes:**
- Mixed routing (some routes → managed service tunnels, some → internet, some → PE)
- Explicit per-route tunnel references (no BGP discovery)
- Operator has fine-grained control per prefix
- Per-route resource allocation (SNAT pools, meters, etc.)

**Why the difference:**
- Cloud providers optimize for simplicity (one tunnel per VNET)
- DASH optimizes for flexibility (many tunnels per VNET, per-prefix selection)

---

## 5. Why Route-Level is Correct for FM

### 5.1 DASH Alignment (Non-Negotiable)
FM must follow DASH spec, not cloud provider patterns. DASH service tunnels are **route-embedded**:
- `route.tunnel_id` (line 66 in route.proto)
- `route.snat_pool_id` (line 63 in route.proto)
- `route.service_vni` (implicit in DASH model)

**FM must be vendor-neutral.** Following Azure/AWS/GCP patterns would tie FM to cloud provider architecture.

### 5.2 Consistency Within FM
FM already handles similar constructs at route scope:
- **Private Link**: route carries `privatelink` details (overlay_sip, underlay_dip, VNI, port range)
- **Meters**: route carries `meter_id` for per-route metering
- **VIPs**: routes reference VIPs; ENI detects membership

**ExpressRoute should follow the same pattern** (route-embedded) for consistency.

### 5.3 Flexibility for Operators
Route-level binding enables per-prefix tunnel selection:
```yaml
# Route to production service via primary ER tunnel
- dst_prefix: "203.0.113.0/24"
  action: SERVICE_TUNNEL
  tunnel_id: "tun-expressroute-primary"
  snat_pool_id: "pool-production"

# Route to same service via backup ER tunnel (different carrier)
- dst_prefix: "203.0.113.0/25"
  action: SERVICE_TUNNEL
  tunnel_id: "tun-expressroute-backup"
  snat_pool_id: "pool-backup"
```

**VNET-level binding (cloud provider model) cannot express this.**

### 5.4 Soft-Fail Without Cascading Failures
Route-level tunnel references enable granular failure handling:
```
If tun-expressroute-primary fails:
  Routes using that tunnel → ENI INCOMPLETE (waits for tunnel)
  Other routes (using other tunnels) → remain READY
  
VNET-level binding would fail the entire VNET if tunnel fails.
```

### 5.5 Per-Route SNAT Resource Control
Each ER route can specify its SNAT pool:
```yaml
route_1:
  tunnel_id: "tun-er-production"
  snat_pool_id: "pool-production"  # Separate pool

route_2:
  tunnel_id: "tun-er-backup"
  snat_pool_id: "pool-backup"      # Different pool
```

**VNET-level binding forces one SNAT pool per VNET** (less flexible).

---

## 6. Design Decision Table

| Aspect | Route-Level (DASH) | VNET-Level (Cloud) | Winner |
|--------|------|------|--------|
| **DASH alignment** | ✅ (explicit in spec) | ❌ (not upstream pattern) | Route-level |
| **FM consistency** | ✅ (matches PE, meters, VIPs) | ❌ (deviates from FM pattern) | Route-level |
| **Per-prefix flexibility** | ✅ (different routes → different tunnels) | ❌ (one tunnel per VNET) | Route-level |
| **Per-route SNAT control** | ✅ (each route has snat_pool_id) | ❌ (centralized) | Route-level |
| **Granular failure handling** | ✅ (tunnel fails → only affected routes) | ❌ (tunnel fails → entire VNET affected) | Route-level |
| **Operator simplicity (cloud parity)** | ❌ (more explicit) | ✅ (simpler model) | VNET-level |
| **Scaling to 100k routes** | ✅ (routes reference shared tunnels) | ✅ (tunnels reference shared VNETs) | Tie |

---

## 7. Why NOT VNET-Level for FM

**Tempting argument:** "Cloud providers do VNET-level; why not FM?"

**Answer:** FM is not a cloud provider; FM is a DASH-aligned packet processing orchestrator.

- **Azure/AWS/GCP** optimize for user simplicity at cloud scale (one ER per VNET is good enough)
- **DashFabric/FM** optimizes for DASH compliance and operator control (per-prefix routing)

**If FM adopted VNET-level binding:**
1. Deviates from DASH spec (SERVICE_TUNNEL in route.proto)
2. Breaks consistency with PE, meters, VIPs (all route-embedded)
3. Loses per-prefix flexibility (operators cannot split routes across tunnels)
4. Cascading failures (one tunnel fail → entire VNET INCOMPLETE)
5. Requires inventing new resource type (not in route.proto)

---

## 8. Three Confirmations from Research

**Q1: Route-level (Option A)?**
✅ **YES** — DASH spec, FM consistency, per-prefix flexibility.

**Q2: Soft-fail?**
✅ **YES** — Tunnel missing → ENI INCOMPLETE, not FAILED. Allows async tunnel provisioning.

**Q3: Per-route SNAT?**
✅ **YES** — Each route carries `snat_pool_id`. Matches Service Tunnel pattern and route.proto design.

---

## 9. Implementation Implications

**For FM developers:**
- No new registry type (reuse TunnelRegistry + SnatPoolRegistry)
- Routes validate tunnel_id + optional snat_pool_id during hydration
- Soft-fail: ENI waits for tunnel/pool readiness
- Signal propagation: TunnelRegistry → NicRegistry (re-validate affected ENIs)

**For operators:**
- Create SERVICE_TUNNEL routes with explicit tunnel_id
- Assign per-route SNAT pools (optional)
- Specify service_vni for service-side identification
- Tunnel provisioning is async (FM auto-retries when ready)

---

## 10. References

- `Specs/Learning-DashNet/12-Scenario-PrivateLink-and-ServiceTunnel.md` — DASH service tunnel walkthrough
- `Specs/protos/published/tunnel.md` — Tunnel object definition (device-global scope)
- `Specs/cb_fm_protos/topics/route.proto` — ROUTE_SERVICE_TUNNEL action, tunnel_id field
- `Specs/FM/fm-private-link-design.md` — Route-embedded soft-fail pattern (reused)
- `Specs/FM/fm-route-direction-design.md` — Per-direction validation (reused)
- Azure ExpressRoute docs — VNET-level BGP-managed model (reference for contrast)
- AWS Direct Connect docs — VPC-level BGP-managed model (reference for contrast)
- Google Cloud Interconnect docs — VPC-level BGP-managed model (reference for contrast)

---

## 11. Debate Summary

**Initial concern:** "Aren't all cloud providers doing VNET-level? Why diverge?"

**Architect response:** FM is DASH-aligned, not cloud-provider-aligned. DASH explicitly models service tunnels as route-embedded (route.proto line 90). Adopting cloud provider patterns would:
1. Break DASH compliance
2. Sacrifice operator flexibility (per-prefix tunnel selection)
3. Create cascading failures (one tunnel fail → entire VNET)
4. Duplicate complexity (VNET-level binding requires new resource types not in route.proto)

**User agreement:** Research confirmed route-level is superior for FM's use case. Cloud providers optimize for user simplicity; FM optimizes for DASH compliance and fine-grained operator control.

---

## 12. Final Recommendation

**Route-level SERVICE_TUNNEL binding is correct for DashFabric FM.**

- ✅ Aligns with DASH spec (route.proto)
- ✅ Consistent with FM's other routing constructs (PE, meters, VIPs)
- ✅ Enables per-prefix tunnel flexibility
- ✅ Soft-fail model (no cascading failures)
- ✅ Per-route SNAT control
- ✅ Already protobuffed (no new resource types)

Implement accordingly.
