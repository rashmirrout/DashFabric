# FM Peering Protocol

> **Status:** Design phase
> **Audience:** FM architects; CB and registry implementers
> **Scope:** How FM treats VNET peering as a reachability dependency
> **Depends on:** [project-protocols.md](project-protocols.md) (cardinal rule, one binary multi-tier)

## 1. Peering is a dependency, not metadata

Peering declares reachability: "VNET-A can reach VNET-B via routing."
It is **not** an optimization hint; it is a **subscription trigger and
ENI programming gate**.

When FM observes `VnetConfig { peer_vnet_ids = [B, C] }`, it must:

1. **Subscribe proactively** to all peer VNETs' resources
2. **Validate** during ENI hydration that routes only target peered or self VNETs
3. **Maintain consistency** — if a route is active, its target's data must be hydrated

## 2. Design decisions (Hybrid Model)

**Resolved tension:** User proposed reactive worker (open subscriptions on VNET update);
architect proposed proactive state machine (open subscriptions upfront). **Hybrid wins:**
- Proactive subscriptions (eliminate race windows)
- Reactive ENI state tracking (async mapping fill)
- Centralized, not fan-out (scales to 1M+ VNETs)

### 2.1 Subscription model: Centralized, proactive

FM **root subscription manager** opens one set of subscriptions (once, at startup):

```
Subscribe /config/vnets/**       (all VNETs)
Subscribe /config/nics/**        (all ENIs)
Subscribe /config/routes/**      (all route groups)
Subscribe /config/mappings/**    (all mappings, all VNETs)
Subscribe /config/acls/**
Subscribe /config/ha/**
```

**NOT per-VNET subscriptions.** At 10k+ VNETs, that fan-out is expensive.

VnetRegistry and MappingManager react to updates within these streams; they
do **not** open new subscriptions.

**Rationale:** Single subscription stream scales linearly. Per-VNET subscriptions
scale quadratically. At hyperscale, centralized is the only option.

### 2.2 ENI hydration gates (fast path, synchronous)

```
NicRegistry.Hydrate(eni_id) → State:
  
  gates := {
    vnet:   Resolve(eni.vnet_id),
    routes: Resolve(eni.route_group_id),
    acls:   Resolve(eni.acl_groups),
    ha:     Resolve(eni.ha_set_id),
  }
  
  IF any gate missing:
    RETURN FAILED
  
  // All gates pass. But mappings might not be ready.
  // Signal MappingManager to fill them async.
  MappingManager.SignalNeeded(eni_id, peers=eni.vnet.peer_vnet_ids)
  
  RETURN PROGRAMMED_INCOMPLETE
```

**Gates (hard requirements):**
- VNET must exist
- Route group must exist (or be derived from VNET defaults)
- All ACL groups must exist
- If HA, HA set must exist

**Non-gates (async, independent):**
- Mappings (can arrive later; manager handles retries)

**Why:** ENI hydration is fast (in-memory lookups, no retries). Mappings are
heavyweight (large table, can take seconds to transmit). Decouple them.

### 2.3 ENI state tiers (tracking completeness)

```
Enum ENIState:
  FAILED              // Gate failed; cannot program
  PROGRAMMED_INCOMPLETE // Gates pass; mappings pending
  PROGRAMMED_READY    // Gates pass; all peer mappings hydrated
```

**Transitions:**

```
mermaid
stateDiagram-v2
    [*] --> FAILED: Gate fails
    [*] --> PROGRAMMED_INCOMPLETE: All gates pass
    PROGRAMMED_INCOMPLETE --> PROGRAMMED_READY: Mappings arrive
    PROGRAMMED_INCOMPLETE --> FAILED: New gate fails
    FAILED --> [*]: ENI deleted
    PROGRAMMED_READY --> [*]: ENI deleted
```

**Why:** Operators need visibility into "ENI is programmed but incomplete."
Monitoring can alert: "2000 ENIs stuck INCOMPLETE for 5+ minutes — investigate mappings."

### 2.4 Mapping hydration (proactive manager)

```
MappingManager (singleton or sharded):
  
  On startup:
    Subscribe to /config/vnets/**
    Subscribe to /config/mappings/**
  
  On VnetRegistry.PeerDeclaration(vnet_id, peers=[B, C]):
    // Proactively ensure peer mappings are subscribed.
    // They already are (root subscription covers all mappings).
    // No action needed; mappings flow in naturally.
  
  On MappingEvent(mapping_vnet_id, status=arrived):
    For each ENI that routes to mapping_vnet_id:
      IF eni.state == PROGRAMMED_INCOMPLETE:
        Check: do all peer mappings exist?
        IF yes:
          ENI.state := PROGRAMMED_READY
          Publish(eni_id, state_change="READY")
```

**Why proactive matters:**
```
REACTIVE (bad):
  T1: Route → B arrives
  T2: ENI hydrates (routes valid) → INCOMPLETE
  T3: ENI drops traffic (no mapping for B)
  T4: (seconds later) Mapping arrives → signal to update ENI
  
PROACTIVE (good):
  T1: VNET declares peer B
  T2: MappingManager ensures /mappings/B is subscribed (already is, root sub)
  T1': Mapping for B arrives (or arrives soon after)
  T2: Route → B arrives
  T3: ENI hydrates, sees mapping exists → READY
  T3': No traffic drop window
```

### 2.5 Cascading peer validation (ENI hydration gate)

During ENI hydration, validate all route targets:

```
For each route in eni.routes:
  IF route.action == ROUTE_VNET:
    target := route.target_vnet_id
    
    // Peering check
    IF target != eni.vnet_id AND target NOT IN eni.vnet.peer_vnet_ids:
      RETURN FAILED  // Vendor error; non-peered target
```

**Never soft-fail on peering:** If a route targets a non-peer, that's a vendor
config bug. Fail immediately (hard fail, mark ENI FAILED).

### 2.6 Peer removal (conformance responsibility)

If `peer_vnet_ids` shrinks (peer B is removed):

```
VnetRegistry.OnPeerRemoval(vnet_id, removed_peer_id=B):
  For each ENI in vnet_id:
    For each route in eni.routes:
      IF route.target_vnet_id == B:
        ERROR: Vendor emitted route to non-peer
        ENI.state := FAILED
        Log incident; alert operator
```

**Not transient:** Operator must fix vendor config (remove route or re-add peer).

### 2.7 Transitive peering (no)

## 3. VnetRegistry responsibilities

VnetRegistry tracks VNET peering state and signals downstream systems:

```
VnetRegistry observes /config/vnets/** stream

On VnetConfig update { peer_vnet_ids = [B, C, ...] }:
  Store peer list (vnet_id → peer_vnet_ids)
  Signal NicRegistry: "peers changed for this VNET"
  Signal MappingManager: "proactively ensure these peer mappings exist"
  
On VNET deletion:
  Clean up peer tracking
  Signal ENIs in this VNET: "re-validate all routes"
```

**No subscription opening.** Subscriptions are already open at root level.
VnetRegistry just tracks and signals.

## 4. NicRegistry hydration flow

```
NicRegistry.Hydrate(eni_id):
  1. Resolve gates (vnet, routes, acls, ha)
     IF any gate missing:
       RETURN FAILED
  
  2. Validate routes
     FOR each route:
       IF action == ROUTE_VNET:
         IF target NOT peered and NOT self:
           RETURN FAILED  (vendor error, hard fail)
  
  3. All gates pass; mappings async
     RETURN PROGRAMMED_INCOMPLETE
     Signal MappingManager: "ENI needs mappings for peers"
```

**Key:** No retry loop in hydration. Either passes (INCOMPLETE) or fails.

## 5. MappingManager (new component)

Responsible for filling mappings asynchronously. Lives at FM level (not
inside NicRegistry).

```
MappingManager:
  Subscribes /config/mappings/**  (via root FM subscription)
  Tracks which ENIs need which peer mappings
  
  On MappingEvent(peer_vnet_id, status=arrived):
    For each ENI-in-peer-vnet that routes to peer_vnet_id:
      IF eni.state == PROGRAMMED_INCOMPLETE:
        Check: peer_vnet_id's mappings fully arrived?
        IF yes AND all other peers ready:
          ENI.state = PROGRAMMED_READY
          Publish StateChange event
```

**Retry:** If mapping is late, MappingManager waits (no active retry loop).
When it arrives, it signals immediately. If never arrives, monitoring alerts
on INCOMPLETE ENIs after timeout.

## 5. Conformance suite

### T10: Peering-target validation

**Test:** Vendor emits route to non-peered VNET.

**Expected:** ENI hydration FAILS (hard fail). ENI marked FAILED. Not transient.

**Invalid:** ENI programs anyway, or soft-fails and retries.

### T11: Peering + mapping consistency

**Test:** Sequence:
1. Emit VNET-A { peer_vnet_ids: [B] }
2. Emit route A → B
3. Emit ENI-1 in A with route to B
4. Emit VNET-B
5. (5 seconds later) Emit mappings for B

**Expected:** 
- T3: ENI-1 hydrates → PROGRAMMED_INCOMPLETE (gates pass, mappings pending)
- T5: Mappings arrive → ENI-1 transitions to PROGRAMMED_READY
- No traffic drop window

**Invalid:** ENI drops to FAILED waiting for mappings, or programs READY before mappings arrive.

### T12: Peer removal (vendor error detection)

**Test:** 
1. VNET-A peers [B]
2. Route A → B exists
3. Update A.peer_vnet_ids to [] (remove B)

**Expected:** FM detects inconsistency; marks ENI or VNET FAILED/DEGRADED; alerts operator.

**Invalid:** FM silently allows dangling routes; no operator visibility.

## 6. References

- `Specs/CB/02-cb-low-level-design-lld.md` § 5.4 — Peering-triggered subscriptions (CB side)
- `Specs/cb_fm_protos/topics/vnet.proto` — `peer_vnet_ids` field
- `Specs/FM/registry-pattern-design.md` — Registry architecture (to be updated)
- `Specs/me-and-ai/peering-vips-routing-completeness.md` — Retrospective on this decision
