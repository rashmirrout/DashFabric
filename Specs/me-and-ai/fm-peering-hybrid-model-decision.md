# FM Peering Design: Reactive vs Hybrid Model Decision

> **Status:** Design retrospective (decision rationale)
> **Date:** 2026-06-14
> **Participants:** Architect (FM), User (DASH requirements)
> **Outcome:** Hybrid model chosen for production; reactive valid for small deployments

## 1. The Problem Statement

FM must support VNET peering as a first-class reachability dependency. The question: **When a VNET declares peers, how should FM track and subscribe to peer resources?**

Two proposals emerged:

1. **Reactive Worker Model** (User's proposal): On VNET peer declaration, open subscriptions to each peer VNET's resources
2. **Hybrid Model** (Architect's proposal): Open all subscriptions upfront; VnetRegistry and MappingManager react to updates within existing streams

## 2. Reactive Worker Model (Detailed)

**Mechanism:**
- FM root subscription opens: `/config/vnets/**`
- When VnetConfig arrives with `peer_vnet_ids=[B, C]`, spawn worker thread
- Worker opens new subscriptions: `/config/vnets/B/**`, `/config/vnets/C/**`, `/config/mappings/B/**`, `/config/mappings/C/**`
- Symmetry: Each VNET independently manages peer subscriptions

**Elegance:**
- Orthogonal logic per VNET
- No central state machine
- Intuitive: "VNET-A peers B? Start listening to B"
- Easy to reason about: Each VNET owns its peer binding

**Advantages:**
- Simple mental model: per-VNET autonomy
- Subscriptions only open if peering declared (conservative resource usage for small deployments)
- Easy debugging: trace one VNET's subscription history

**Disadvantages at Hyperscale:**
- **Fan-out explosion**: At 10k VNETs, if each declares average 5 peers, root must manage 50k subscription streams
- **Subscription overhead**: Each stream has metadata, buffering, error tracking
- **Convergence time**: New peer takes ~100ms to open subscription; in that window, ENI hydration stalls or fails
- **Cascade risk**: One slow peer subscription blocks hydration of all ENIs in that VNET
- **Memory footprint**: Broker must track 50k active subscriptions with per-stream state

**Failure scenario:**
```
T1: VNET-A declares peer B; worker spawns
T2: /config/mappings/B subscription opens (takes 150ms)
T3: ENI in A routes to B arrives; hydration waits for B's mappings
T4: Traffic begins flowing; but B's mappings not yet available
T5: (50ms later) Mappings arrive
T6: No data plane window, but control plane blocked
```

## 3. Hybrid Model (Detailed)

**Mechanism:**
- FM root subscription opens all streams upfront (once at startup):
  - `/config/vnets/**` (all VNETs)
  - `/config/nics/**` (all ENIs)
  - `/config/routes/**` (all route groups)
  - `/config/mappings/**` (all mappings, all VNETs)
  - `/config/acls/**`, `/config/ha/**`
- VnetRegistry: Consumes `/config/vnets/**`; detects peer changes; signals downstream
- NicRegistry: Hydrates ENIs; validates routes target peered VNETs; transitions to INCOMPLETE
- MappingManager: Consumes `/config/mappings/**`; tracks completeness per peer; transitions ENIs to READY

**Elegance:**
- Single root subscription (O(1) overhead, not O(n²))
- Decoupled concerns: VnetRegistry tracks peering; MappingManager fills mappings independently
- Three-tier ENI state (FAILED, INCOMPLETE, READY) provides observability

**Advantages:**
- **Linear scalability**: One `/config/mappings/**` stream at 10k VNETs, not 50k streams
- **No convergence delay**: Mappings already flowing before ENI hydration begins
- **Proactive fill**: MappingManager pre-warms peer mappings; when ENI hydrates, data likely ready
- **Observable intermediate state**: INCOMPLETE state allows monitoring "ENIs waiting for peer mappings"
- **Simpler recovery**: If peer removed, VnetRegistry signal triggers re-validation (not subscription cleanup)

**Disadvantages:**
- More state to track: MappingManager must monitor all mappings (not just declared peers)
- More complex logic: ENI state machine (3 states vs 2)
- Initial resource usage: Open subscriptions even if no peering declared

**Success scenario:**
```
T1: Root FM subscription opens /config/mappings/** (all peers' mappings flow)
T2: VNET-A declares peer B; no new subscriptions opened
T3: Mappings for B arrive (already in stream)
T3': ENI in A routes to B arrives; hydrates to INCOMPLETE (gates pass, mappings ready)
T4: Traffic begins; no delay, no stall
```

## 4. Trade-off Analysis: Detailed Comparison

| Dimension | Reactive | Hybrid | Winner |
|-----------|----------|--------|--------|
| **Subscription fan-out** | O(n²) at 10k VNETs | O(n) | Hybrid |
| **Convergence time** | 50–150ms per peer declaration | 0ms (subscriptions pre-open) | Hybrid |
| **ENI hydration delay** | Up to 150ms if peer sub slow | Sub-millisecond (in-memory) | Hybrid |
| **Traffic loss window** | If ENI hydrates before peer mappings arrive | Proactive manager pre-fills mappings | Hybrid |
| **Observable state** | Binary (programmed or not) | Three-tier (FAILED, INCOMPLETE, READY) | Hybrid |
| **Code complexity** | Lower (fewer branches) | Higher (async manager, state machine) | Reactive |
| **Memory overhead** | Low (only subscriptions opened) | High (track all mappings always) | Reactive |
| **Deployment scale** | <1k VNETs | 10k+ VNETs | — |
| **Recovery on peer removal** | Close subscription for peer | Signal re-validation (async) | Hybrid |
| **Monitoring difficulty** | High (track subscription state) | Low (ENI state metric) | Hybrid |

## 5. Hyperscale Risk: The Traffic Loss Window

**Critical insight:** At hyperscale, reactive model opens a traffic loss window.

**Scenario:**
```
Reactive model at 10k VNETs:
- VNET-A declares 5 peers
- Subscribe to /config/mappings/A, /config/mappings/B, /config/mappings/C, ...
- Total: 5 subscriptions; each takes ~100ms to open
- Meanwhile, route A→B arrives; ENI hydrates (all gates pass)
- ENI marked PROGRAMMED (ready for traffic)
- But mappings for B not yet arrived (subscription still opening)
- Traffic forwards to B with no underlay destination
- 50–100ms traffic drop window
- Then mappings arrive; traffic resumes

Hybrid model:
- All mapping subscriptions open at startup
- VNET-A declares peer B; mappings for B already flowing
- Route A→B arrives; ENI hydrates; sees mappings ready
- ENI marked PROGRAMMED_READY
- No traffic loss window
```

**Cost of traffic loss window at hyperscale:**
- 100k ENIs, 5% churn per minute = 5k new ENIs/min
- Each affected by 50ms loss = 250s aggregate loss/min
- At 1M flows/ENI = 1.25B flow-loss events/min
- SLA impact: ~0.1–0.2% packet loss visible in monitoring

**Reactive model mitigation (not solving):**
- Delay ENI hydration until all peer subscriptions open (adds 100–500ms latency)
- Or: Mark ENI INCOMPLETE, retry async (adds retry complexity)

**Hybrid model solution (native):**
- Mappings already flowing; ENI hydrates directly to READY
- No traffic loss, no delay, no retry logic

## 6. Decision Rationale

**Why Hybrid Wins for Production:**

1. **Scalability**: Linear vs quadratic matters at 10k+ VNETs; hyperscalers cannot ignore O(n²)
2. **Latency**: Convergence time (0ms vs 100ms) is material for multi-tenant cloud platforms
3. **Reliability**: Traffic loss window is unacceptable; proactive mapping fill eliminates it
4. **Observability**: Three-tier ENI state enables "stuck INCOMPLETE for 5+ min" alerts
5. **Simplicity at scale**: Fewer subscription streams = fewer failure modes

**Why Reactive Is Still Valid:**

1. **Small deployments**: <1k VNETs, reactive model's simplicity may outweigh overhead
2. **Code simplicity**: Fewer branches, easier to reason about
3. **Resource efficiency**: No pre-warming of mappings; only open what's needed

## 7. Implementation Implication

**Hybrid model choice means:**
- VnetRegistry: Tracks peer state; signals on changes
- NicRegistry: Hydrates fast (sync); fails hard on vendor errors (non-peered targets)
- MappingManager: New component; manages async mapping fill; transitions ENIs to READY
- ENI state machine: Three states (FAILED, INCOMPLETE, READY), not two

**Code organization:**
- No per-VNET subscription loop in NicRegistry
- One MappingManager singleton (or sharded at extreme scale)
- Signal channels: VnetRegistry → NicRegistry, NicRegistry → MappingManager, MappingManager → ENI state updates

## 8. References

- `Specs/protocols/fm-peering-protocol.md` — Hybrid model decision documented
- `Specs/FM/fm-registry-peering-design.md` — Implementation blueprint
- `Specs/me-and-ai/next_plan.md` — T1–T5 decisions from this retrospective
