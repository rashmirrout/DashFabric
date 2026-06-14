# VIP-DIP Binding Model Decision: Why VIP-Driven Architecture

> **Status:** Design decision retrospective
> **Date:** 2026-06-14
> **Participants:** Architect (FM), User (DASH requirements)
> **Outcome:** VIP-driven backend membership (VIP declares backends); two-trigger SNAT programming (ENI hydration + VIP update)

## 1. The Problem: How Does FM Learn DIP-to-VIP Binding?

When an ENI comes online with overlay IP `10.0.0.5`, how does FM know:
- "This DIP is a backend for VIP 20.1.1.1"
- "Program SNAT rule: src_ip==10.0.0.5 → rewrite to 20.1.1.1"?

Three candidate models emerged:

### Model A: DIP-Driven (ENI Config)
```
ENI carries: "I am backend for these VIPs: [vip_1, vip_2]"
FM reads ENI, finds VIPs, programs SNAT
```
**Tradeoff:**
- (+) Simple: all info in ENI config
- (-) ENI config becomes VIP-aware (mixing concerns)
- (-) Adding/removing backends requires ENI re-config (churn)

### Model B: VIP-Driven (VIP Config)
```
VIP carries: "My backends are these DIPs: [10.0.0.5, 10.0.0.7]"
FM reads VIP, scans ENIs, finds matches by overlay_ip, programs SNAT
```
**Tradeoff:**
- (+) VIP is single source of truth for backend pool
- (+) Add/remove backend = VIP CRUD (no ENI touch)
- (+) Matches LB semantics (pool declared on LB, not on backends)
- (-) Requires DIP-to-ENI lookup (scan local ENIs)

### Model C: Separate BackendPool Resource
```
BackendPool: "vip_1 → [10.0.0.5, 10.0.0.7]"
FM reads pool, triggers both ENI and VIP updates
```
**Tradeoff:**
- (+) Decoupled from both ENI and VIP
- (-) New resource type (complexity)
- (-) More subscriptions to manage
- (-) Not how Azure/DASH models it

## 2. DASH Upstream Research

### What DASH SAI Spec Shows
VIP entry in SAI hardware is **minimal**:
```yaml
sai_vip_entry_t:
  switch_id: <switch>
  vip: <exact_match_key>
action: SAI_VIP_ENTRY_ACTION_ACCEPT
```

No backend list in hardware table. Backends are in the **SLB MUX** (external LB), not DPU.

### What DASH Fast-Path Reveals
```
Inbound flow:
  SLB MUX picks backend from "list of healthy VMs in backend pool"
  → Rewrites VIP → DIP
  → Forwards to DPU

Outbound flow:
  DPU SNAT: src_ip (DIP) → rewrite to return VIP
  → Sent back to SLB MUX
```

**Key insight**: Backend pool is external (SLB MUX owns it), but **FM must track it locally** to program SNAT rules.

## 3. Why VIP-Driven Model Wins

### Use Case: Scale Scenario
```
Scenario: LB adds new backend VM
  Old VIP pool: [10.0.0.5, 10.0.0.7]
  New VIP pool: [10.0.0.5, 10.0.0.7, 10.0.0.9]

DIP-Driven (ENI model):
  1. Update ENI-5 config: add vip_1 to backends
  2. Update ENI-7 config: add vip_1 to backends
  3. Create ENI-9 (new VM): add vip_1 to backends
  → 3 ENI updates; potential race conditions

VIP-Driven (VIP model):
  1. Update VIP-1.backend_dips: add 10.0.0.9
  2. VipRegistry detects change
  3. Scans local ENIs; finds ENI-9 (overlay_ip == 10.0.0.9)
  4. Programs SNAT for ENI-9
  → 1 VIP update; atomic
```

### Operational Benefit
```
Adding new backend to VIP:
  - DIP-Driven: Touch each ENI (N updates for N backends)
  - VIP-Driven: Touch VIP once (1 update, affects all local ENIs)
```

### Alignment with Load Balancer Semantics
In every LB (Azure, AWS, GCP):
- Backend pool is declared on **LB/VIP**
- Backends are added/removed by **pool membership**, not backend config
- DIP-Driven violates this principle (backend declares membership)

## 4. User's Key Insight

User asked: **"Not all DIP should be under a VIP. There can be multiple VIPs in a single VNET serving different DIPs. Is that not it?"**

This revealed the architectural question: **VIPs and DIPs are independent concepts**. Not every DIP has a VIP (some route directly), and one VIP can serve many DIPs.

**VIP-Driven model handles this naturally:**
- VIP.backend_dips = explicit list of backends
- ENI detects membership by overlay_ip matching
- DIP can exist without any VIP (no false dependencies)

## 5. Two-Trigger SNAT Programming: Why Both?

User requirement: "Crude DIP can be assigned (added/removed) later at any time"

This demanded **dual triggers**:

| Trigger | When | Why |
|---------|------|-----|
| **Trigger 1: ENI Hydration** | New ENI with overlay_ip arrives | Cold-boot case: ENI comes online, FM discovers it's a VIP backend |
| **Trigger 2: VIP Update** | VIP.backend_dips changes | Late-binding case: VIP gains/loses this DIP as backend |

**Example timeline:**
```
T0: ENI-5 provisions (overlay_ip=10.0.0.5)
    → NicRegistry.Hydrate() → DetectVipMembership() → finds VIP-1
    → Check VIP-1 state == READY?
    → Trigger 1: ProgramSnatRules(ENI-5, 10.0.0.5, [VIP-1])

T5: Operator adds ENI-5 as backend to VIP-2 (late binding)
    → VipRegistry.OnVipEvent() → backend_dips += 10.0.0.5
    → Trigger 2: Detects 10.0.0.5 in local ENIs
    → ProgramSnatRules(ENI-5, 10.0.0.5, [VIP-2])

T10: Operator removes ENI-5 from VIP-1 (backend failure)
    → VipRegistry.OnVipEvent() → backend_dips -= 10.0.0.5
    → Trigger 2: RemoveSnatRule(ENI-5, 10.0.0.5, [VIP-1])
```

Both triggers required for **operational flexibility**.

## 6. Soft-Fail Model (Non-Blocking Dependencies)

User clarified: **"NAT pool and gateway are not absolutely essential for VIP. DASH programs SNAT rule and sends packet out directly. In reverse path, DIP is in inbound route to decap."**

This meant NAT pool/gateway failures should **not block ENI** (unlike peering gates which hard-fail).

**VIP-Driven model handles this:**
```
If NAT pool missing:
  VIP.state = WAITING (soft fail)
  → ENI that's backend for this VIP → INCOMPLETE
  → But ENI can still program other VIPs (no cascade)

When NAT pool arrives:
  VIP.state = READY
  → NicRegistry re-checks all affected ENIs
  → Transitions INCOMPLETE → READY
```

**Contrast with hard-fail (peering):**
```
If peer VNET missing:
  Route validation fails
  → ENI → FAILED (hard fail)
  → Operator must fix CP config
```

## 7. Design Trade-Offs Summary

| Aspect | VIP-Driven | DIP-Driven | Winner |
|--------|-----------|-----------|--------|
| **Backend add/remove** | 1 VIP update | N ENI updates | VIP-Driven |
| **LB semantics alignment** | ✅ (pool on LB) | ❌ (on backend) | VIP-Driven |
| **DIP without VIP** | Natural (just not in any VIP) | Awkward (carries empty list) | VIP-Driven |
| **Multiple VIPs per DIP** | ✅ (scan all VIPs) | ✅ (declare in ENI) | Tie |
| **Lookup complexity** | Scan local ENIs (simple) | Scan VIPs (simple) | Tie |
| **State consistency** | Single source (VIP.backend_dips) | Split (ENI + VIP) | VIP-Driven |
| **Operational churn** | Low (VIP CRUD) | High (ENI CRUD) | VIP-Driven |

## 8. Implementation Implications

**VIP-Driven requires:**
1. VipRegistry tracks `backend_dips` (canonical list)
2. NicRegistry.DetectVipMembership(overlay_ip) scans local VIPs
3. Two signal handlers: ENI hydration + VIP update
4. SNAT rule programming in both paths

**Benefits:**
- Simpler mental model (VIP is pool manifest)
- Fewer state dependencies (no ENI-VIP coupling)
- Better observability (VIP.backend_dips is truth)

---

## 9. References

- `Specs/FM/fm-vip-design.md` — Detailed implementation blueprint
- `Specs/cb_fm_protos/topics/vip.proto` — VIP proto with backend_dips field
- DASH load-balancer docs — Fast-path ICMP redirect (backend selection)
