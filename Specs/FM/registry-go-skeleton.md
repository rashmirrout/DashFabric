# FM Registry Implementation: Go Code Skeleton

> **Status:** Implementation guide (method signatures, invariants, pseudocode)
> **Audience:** FM engineers implementing VnetRegistry, NicRegistry, MappingManager
> **Depends on:** `Specs/protocols/fm-peering-protocol.md`, `Specs/FM/fm-registry-peering-design.md`

This document provides Go struct definitions, method signatures, invariant contracts, and pseudocode algorithms for all three peering-aware registries.

## 1. VnetRegistry (Peering State Machine)

### 1.1 Struct Definition

```go
package registry

import (
	"context"
	"sync"
	"time"

	pb "dashfabric/cb_fm_protos/vnet"
)

// VnetRegistry tracks VNET peering state and signals downstream systems.
// Single-threaded writes (via subscription worker); concurrent reads.
type VnetRegistry struct {
	vnets   map[string]*VnetState  // vnet_id → VnetState
	mu      sync.RWMutex
	signals chan VnetSignal  // Broadcast channel to listeners

	// Downstream consumers
	nicRegistry     *NicRegistry
	mappingManager  *MappingManager
}

type VnetState struct {
	VnetID         string
	VNI            uint32
	PeerVnetIDs    []string     // Sorted for deterministic diffs
	LastUpdated    time.Time
	State           VnetStateEnum
}

type VnetStateEnum int
const (
	VnetStateUNKNOWN VnetStateEnum = iota
	VnetStateREADY
	VnetStateDELETED
)

// VnetSignal represents a peering change event propagated to downstream registries.
type VnetSignal struct {
	Event          string      // "PeerAdded", "PeerRemoved", "VnetDeleted"
	VnetID         string
	AddedPeers     []string    // New peers in this update
	RemovedPeers   []string    // Peers removed in this update
}

func NewVnetRegistry(nicReg *NicRegistry, mapMgr *MappingManager) *VnetRegistry {
	return &VnetRegistry{
		vnets:          make(map[string]*VnetState),
		signals:        make(chan VnetSignal, 100),
		nicRegistry:    nicReg,
		mappingManager: mapMgr,
	}
}
```

### 1.2 Method Signatures & Contracts

```go
// OnVnetEvent processes a VNET configuration update from the broker stream.
// Detects peer additions/removals and broadcasts signals.
//
// Invariant: Called sequentially by subscription worker (no concurrent calls).
// Invariant: peer_vnet_ids are sorted before storage.
// Invariant: Signals must be processed in order (FIFO channel).
func (r *VnetRegistry) OnVnetEvent(ctx context.Context, vnet *pb.VnetConfig) error {
	// 1. Lookup old state
	// 2. Detect added/removed peers (set diff)
	// 3. Update vnets[vnet_id]
	// 4. Broadcast signals for each change
	// 5. Return error if validation fails (e.g., vnet_id empty)
	panic("TODO: implement")
}

// GetVnetState returns the current state of a VNET (read-safe).
// Returns nil if VNET not found.
func (r *VnetRegistry) GetVnetState(vnetID string) *VnetState {
	// RWLock read
	panic("TODO: implement")
}

// ListPeers returns sorted peer list for a VNET.
// Returns empty slice if VNET not found.
func (r *VnetRegistry) ListPeers(vnetID string) []string {
	// RWLock read; return copy of PeerVnetIDs
	panic("TODO: implement")
}

// WatchSignals returns a channel on which peer changes are broadcast.
// Consumer must drain channel to avoid blocking broadcasts.
func (r *VnetRegistry) WatchSignals() <-chan VnetSignal {
	return r.signals
}

// isPeerOf checks if targetID is a peer of sourceID.
// Used by NicRegistry.ValidatePeerTargets.
func (r *VnetRegistry) isPeerOf(sourceID, targetID string) bool {
	if sourceID == targetID {
		return true  // Self is always reachable
	}
	state := r.GetVnetState(sourceID)
	if state == nil {
		return false
	}
	// Binary search on sorted PeerVnetIDs
	// return binarySearch(state.PeerVnetIDs, targetID)
	panic("TODO: implement")
}
```

### 1.3 Pseudocode Algorithm

```go
// OnVnetEvent pseudocode
func (r *VnetRegistry) OnVnetEvent(ctx context.Context, vnet *pb.VnetConfig) error {
	if vnet.VnetId == "" {
		return fmt.Errorf("vnet_id empty")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Lookup old state
	oldState := r.vnets[vnet.VnetId]
	oldPeers := []string{}
	if oldState != nil {
		oldPeers = oldState.PeerVnetIDs
	}

	// New state
	newPeers := vnet.PeerVnetIds
	sort.Strings(newPeers)  // Ensure sorted for deterministic diffs

	// Detect changes
	addedPeers := setDiff(newPeers, oldPeers)     // In new, not in old
	removedPeers := setDiff(oldPeers, newPeers)   // In old, not in new

	// Update state
	r.vnets[vnet.VnetId] = &VnetState{
		VnetID:      vnet.VnetId,
		VNI:         vnet.Vni,
		PeerVnetIDs: newPeers,
		LastUpdated: time.Now(),
		State:       VnetStateREADY,
	}

	// Broadcast signals
	if len(addedPeers) > 0 {
		r.signals <- VnetSignal{
			Event:      "PeerAdded",
			VnetID:     vnet.VnetId,
			AddedPeers: addedPeers,
		}
	}

	if len(removedPeers) > 0 {
		r.signals <- VnetSignal{
			Event:          "PeerRemoved",
			VnetID:         vnet.VnetId,
			RemovedPeers:   removedPeers,
		}
		// Signal NicRegistry and MappingManager to re-validate
		// (Will be handled by separate listener goroutines)
	}

	return nil
}
```

---

## 2. NicRegistry (ENI Hydration & Peer Validation)

### 2.1 Struct Definition

```go
package registry

import (
	"context"
	"sync"
	"time"

	pb "dashfabric/cb_fm_protos/eni"
)

// NicRegistry hydrates ENIs and tracks their peering-dependent state.
type NicRegistry struct {
	enis        map[string]*ENIState  // eni_id → ENIState
	mu          sync.RWMutex
	vnetReg     *VnetRegistry        // For peer lookups
	mapMgr      *MappingManager      // For mapping readiness
	signals     chan ENISignal       // State change notifications
}

type ENIState struct {
	ENIID                  string
	VnetID                 string
	State                  ENIStateEnum
	RoutesToPeers          []string           // Peer VNET IDs this ENI routes to
	MappingsReadyFor       map[string]bool    // peer_vnet_id → is_ready
	LastStateChange        time.Time
	FailureReason          string             // If state == FAILED, why
}

type ENIStateEnum int
const (
	ENIStateFAILED ENIStateEnum = iota
	ENIStatePROGRAMMED_INCOMPLETE
	ENIStatePROGRAMMED_READY
)

type ENISignal struct {
	Event         string        // "NeedsMappings", "StateChange", "Failed"
	ENIID         string
	NewState      ENIStateEnum
	OldState      ENIStateEnum
	NeededPeers   []string      // Peers whose mappings are missing
}

func NewNicRegistry(vnetReg *VnetRegistry, mapMgr *MappingManager) *NicRegistry {
	return &NicRegistry{
		enis:      make(map[string]*ENIState),
		vnetReg:   vnetReg,
		mapMgr:    mapMgr,
		signals:   make(chan ENISignal, 1000),
	}
}
```

### 2.2 Method Signatures & Contracts

```go
// Hydrate processes an ENI configuration event.
// Runs synchronous hydration gates; signals MappingManager if all gates pass.
//
// Invariant: Hydration is fast (in-memory lookups, no I/O).
// Invariant: Either returns FAILED or PROGRAMMED_INCOMPLETE (never READY from hydration).
// Invariant: Hard-fail on vendor errors (non-peered targets); do NOT retry.
func (r *NicRegistry) Hydrate(ctx context.Context, eniID string) error {
	// 1. Resolve gates (vnet, routes, acls, ha)
	// 2. Validate route targets are peered or self
	// 3. Collect peer VNET IDs that this ENI routes to
	// 4. If gates fail: setState(eniID, FAILED)
	// 5. If gates pass: setState(eniID, INCOMPLETE); signal MappingManager
	panic("TODO: implement")
}

// GetENIState returns the current ENI state (read-safe).
func (r *NicRegistry) GetENIState(eniID string) *ENIState {
	// RWLock read
	panic("TODO: implement")
}

// ValidatePeerTargets checks that all route targets in an ENI are either
// peered VNETs or the ENI's own VNET.
//
// Returns error if any route targets a non-peered VNET (vendor error).
// Error message should be descriptive for operator alert.
func (r *NicRegistry) ValidatePeerTargets(ctx context.Context, eniID string, vnetID string, routeGroupID string) error {
	// 1. Lookup route group
	// 2. For each route with action == ROUTE_VNET:
	//    - Check if target == vnetID (self, always OK)
	//    - Check if target in vnet.PeerVnetIDs
	//    - If neither: return error with route target and current peers
	panic("TODO: implement")
}

// OnPeerChange handles a peering change signal from VnetRegistry.
// Re-validates all ENIs in the affected VNET.
func (r *NicRegistry) OnPeerChange(ctx context.Context, signal VnetSignal) error {
	// 1. Lock enis
	// 2. For each ENI in signal.VnetID:
	//    - Call Hydrate(eni.ENIID)
	//    - If state changes to FAILED, log error and alert operator
	panic("TODO: implement")
}

// setState updates ENI state and broadcasts signal.
// Caller must hold lock if needed (this method does NOT lock).
func (r *NicRegistry) setState(eniID string, newState ENIStateEnum) {
	// 1. Lookup old state
	// 2. If newState == oldState, no-op
	// 3. Update eni.State
	// 4. Broadcast ENISignal
	panic("TODO: implement")
}

// WatchSignals returns channel for ENI state changes.
func (r *NicRegistry) WatchSignals() <-chan ENISignal {
	return r.signals
}
```

### 2.3 Pseudocode Algorithm

```go
func (r *NicRegistry) Hydrate(ctx context.Context, eniID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Lookup or create ENI state
	eni, exists := r.enis[eniID]
	if !exists {
		eni = &ENIState{
			ENIID: eniID,
			MappingsReadyFor: make(map[string]bool),
		}
		r.enis[eniID] = eni
	}

	// Gate 1: Resolve VNET
	vnetState := r.vnetReg.GetVnetState(eni.VnetID)
	if vnetState == nil {
		r.setState(eniID, ENIStateFAILED)
		eni.FailureReason = fmt.Sprintf("vnet %s not found", eni.VnetID)
		return fmt.Errorf("vnet gate failed: %s", eni.FailureReason)
	}

	// Gate 2: Resolve routes
	routes := lookupRouteGroup(eni.RouteGroupID)  // TODO: define
	if routes == nil {
		r.setState(eniID, ENIStateFAILED)
		eni.FailureReason = "route group not found"
		return fmt.Errorf("route gate failed")
	}

	// Gate 3: Resolve ACLs
	for _, aclGroupID := range eni.ACLGroupIDs {  // TODO: define in ENIState
		acls := lookupACLGroup(aclGroupID)         // TODO: define
		if acls == nil {
			r.setState(eniID, ENIStateFAILED)
			eni.FailureReason = fmt.Sprintf("acl group %s not found", aclGroupID)
			return fmt.Errorf("acl gate failed")
		}
	}

	// Gate 4: Validate peering targets
	peersNeeded := make(map[string]bool)
	for _, route := range routes {
		if route.Action == RouteActionROUTE_VNET {
			target := route.TargetVnetID
			if target != eni.VnetID && !r.vnetReg.isPeerOf(eni.VnetID, target) {
				// Vendor error: route targets non-peered VNET
				r.setState(eniID, ENIStateFAILED)
				eni.FailureReason = fmt.Sprintf(
					"route targets non-peered vnet %s (peers: %v)",
					target, vnetState.PeerVnetIDs)
				return fmt.Errorf("peer validation failed: %s", eni.FailureReason)
			}
			if target != eni.VnetID {
				peersNeeded[target] = true
			}
		}
	}

	// All gates pass; ENI is hydrated but mappings may be pending
	r.setState(eniID, ENIStatePROGRAMMED_INCOMPLETE)
	eni.RoutesToPeers = keysOf(peersNeeded)  // TODO: helper

	// Signal MappingManager that this ENI needs peer mappings
	r.mapMgr.SignalENINeedsMappings(eniID, eni.RoutesToPeers)

	return nil
}

func (r *NicRegistry) OnPeerChange(ctx context.Context, signal VnetSignal) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, eni := range r.enis {
		if eni.VnetID == signal.VnetID {
			// Re-hydrate this ENI; peer removal may invalidate routes
			if err := r.Hydrate(ctx, eni.ENIID); err != nil {
				// If hydration now fails, state is FAILED; operator alerted
				log.Warnf("ENI %s re-validation failed after peer change: %v", eni.ENIID, err)
			}
		}
	}
	return nil
}
```

---

## 3. MappingManager (Async Mapping Fill)

### 3.1 Struct Definition

```go
package registry

import (
	"context"
	"sync"
	"time"

	pb "dashfabric/cb_fm_protos/mapping"
)

// MappingManager tracks mapping availability across peer VNETs.
// Proactively ensures peer mappings are subscribed; reactively updates ENI state.
type MappingManager struct {
	mappingsByVnet    map[string]*MappingTracker  // vnet_id → MappingTracker
	eniWaitingFor     map[string][]string         // eni_id → []peer_vnet_ids (ENI needs these peers ready)
	mu                sync.RWMutex
	nicReg            *NicRegistry
	signals           chan MappingSignal
}

type MappingTracker struct {
	VnetID       string
	Arrivals     []time.Time  // When mapping events started arriving
	IsComplete   bool         // Heuristic: are all mappings for this VNET arrived?
	LastUpdate   time.Time
}

type MappingSignal struct {
	Event         string  // "ENIReady", "MappingStalled"
	ENIID         string
	ReadyPeers    []string
}

func NewMappingManager(nicReg *NicRegistry) *MappingManager {
	return &MappingManager{
		mappingsByVnet: make(map[string]*MappingTracker),
		eniWaitingFor:  make(map[string][]string),
		nicReg:         nicReg,
		signals:        make(chan MappingSignal, 1000),
	}
}
```

### 3.2 Method Signatures & Contracts

```go
// SignalENINeedsMappings registers an ENI that is waiting for peer mappings.
// Called by NicRegistry after hydration gates pass.
//
// Invariant: Idempotent; calling twice with same ENI is no-op.
// Invariant: If all peers are already ready, CheckENIReady should be called immediately.
func (m *MappingManager) SignalENINeedsMappings(eniID string, peerVnetIDs []string) {
	// 1. Store eni_id → peers in eniWaitingFor
	// 2. Call CheckENIReady (may transition ENI to READY immediately)
	panic("TODO: implement")
}

// OnMappingEvent processes a mapping configuration event from the broker stream.
// Tracks per-VNET mapping completeness; signals NicRegistry if ENI can transition to READY.
//
// Invariant: Mapping completeness is a heuristic (e.g., "100+ mapping events = complete").
func (m *MappingManager) OnMappingEvent(ctx context.Context, mapping *pb.MappingUpdate) error {
	// 1. Extract vnet_id from mapping topic
	// 2. Record mapping arrival
	// 3. Update IsComplete heuristic
	// 4. For each ENI waiting for vnet_id: CheckENIReady
	panic("TODO: implement")
}

// CheckENIReady atomically checks if an ENI can transition to READY.
// Returns true if all peer mappings exist and ENI state is INCOMPLETE.
// Side effect: Transitions ENI to READY and broadcasts signal.
func (m *MappingManager) CheckENIReady(eniID string) bool {
	// 1. Lookup ENI
	// 2. For each peer in eni.RoutesToPeers:
	//    - Check mappingsByVnet[peer].IsComplete
	//    - If any peer NOT complete: return false
	// 3. If all peers complete:
	//    - Call nicReg.setState(eniID, PROGRAMMED_READY)
	//    - Remove eniID from eniWaitingFor
	//    - Return true
	panic("TODO: implement")
}

// IsMappingReady checks if a VNET's mappings are ready (read-safe).
// Used for monitoring and debugging.
func (m *MappingManager) IsMappingReady(vnetID string) bool {
	// RWLock read
	panic("TODO: implement")
}

// WatchSignals returns channel for mapping readiness events.
func (m *MappingManager) WatchSignals() <-chan MappingSignal {
	return m.signals
}
```

### 3.3 Pseudocode Algorithm

```go
func (m *MappingManager) OnMappingEvent(ctx context.Context, mapping *pb.MappingUpdate) error {
	vnetID := extractVnetID(mapping.Topic)  // TODO: parse /config/mappings/<vnet_id>
	if vnetID == "" {
		return fmt.Errorf("malformed mapping topic")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Track mapping arrival
	tracker := m.mappingsByVnet[vnetID]
	if tracker == nil {
		tracker = &MappingTracker{
			VnetID:   vnetID,
			Arrivals: []time.Time{time.Now()},
		}
		m.mappingsByVnet[vnetID] = tracker
	} else {
		tracker.Arrivals = append(tracker.Arrivals, time.Now())
	}

	// Heuristic: if 100+ mapping events received, assume complete
	// (Real implementation: query CB for mapping count and compare)
	if len(tracker.Arrivals) >= 100 {
		tracker.IsComplete = true
	}

	tracker.LastUpdate = time.Now()

	// Check if any waiting ENIs can now transition to READY
	readyENIs := []string{}
	for eniID := range m.eniWaitingFor {
		if m.CheckENIReady(eniID) {
			readyENIs = append(readyENIs, eniID)
		}
	}

	// Broadcast signals
	for _, eniID := range readyENIs {
		m.signals <- MappingSignal{
			Event:      "ENIReady",
			ENIID:      eniID,
			ReadyPeers: m.eniWaitingFor[eniID],
		}
	}

	return nil
}

func (m *MappingManager) CheckENIReady(eniID string) bool {
	eni := m.nicReg.GetENIState(eniID)
	if eni == nil || eni.State != ENIStatePROGRAMMED_INCOMPLETE {
		return false  // ENI doesn't exist or not in INCOMPLETE state
	}

	// Check if all peer mappings are ready
	for _, peer := range eni.RoutesToPeers {
		tracker := m.mappingsByVnet[peer]
		if tracker == nil || !tracker.IsComplete {
			return false  // Peer mapping not yet ready
		}
	}

	// All peers ready; transition ENI to READY
	m.nicReg.setState(eniID, ENIStatePROGRAMMED_READY)
	delete(m.eniWaitingFor, eniID)  // Remove from waitlist

	return true
}

func (m *MappingManager) SignalENINeedsMappings(eniID string, peerVnetIDs []string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.eniWaitingFor[eniID] = peerVnetIDs

	// Optimistic: check if all peers are already ready
	m.CheckENIReady(eniID)
}
```

---

## 4. Integration: Signal Flow

### 4.1 Listener Setup

```go
// In FM main bootstrap:

// Create registries
vnetReg := registry.NewVnetRegistry(nil, nil)  // Will fill pointers after
nicReg := registry.NewNicRegistry(vnetReg, nil)
mapMgr := registry.NewMappingManager(nicReg)

// Fix pointers (circular dependencies)
vnetReg.nicRegistry = nicReg
vnetReg.mappingManager = mapMgr
nicReg.mapMgr = mapMgr

// Spawn listener goroutines
go vnetRegistry.ListenForPeerChanges(ctx)   // Consumes VnetRegistry.signals
go nicRegistry.ListenForPeerSignals(ctx)    // Consumes signals from VnetRegistry
go mappingManager.ListenForMappings(ctx)    // Consumes /config/mappings/** stream
```

### 4.2 Signal Propagation Chain

```
1. Broker emits /config/vnets/<vnet_id>
   → Root subscription worker calls VnetRegistry.OnVnetEvent()
   → VnetRegistry detects peer changes; broadcasts VnetSignal

2. VnetRegistry.VnetSignal ("PeerAdded" or "PeerRemoved")
   → NicRegistry listener calls OnPeerChange()
   → NicRegistry re-hydrates all ENIs in affected VNET
   → If peer removed and route targeted non-peer: ENI.state = FAILED

3. NicRegistry.Hydrate() passes all gates
   → Calls MappingManager.SignalENINeedsMappings(eniID, peers)
   → MappingManager adds eniID to waitlist; calls CheckENIReady()
   → If all peers already ready: transitions ENI to READY immediately

4. Broker emits /config/mappings/<peer_vnet_id>
   → Root subscription worker calls MappingManager.OnMappingEvent()
   → MappingManager updates IsComplete heuristic
   → For each waiting ENI: calls CheckENIReady()
   → If all peers now ready: transitions ENI to READY
   → Broadcasts MappingSignal ("ENIReady")
```

---

## 5. Error Handling & Recovery

### 5.1 Hard Failures (No Retry)

```go
// NicRegistry.ValidatePeerTargets fails:
// Reason: Route targets non-peered VNET (vendor configuration error)
// Action: Mark ENI FAILED; log error with route target and current peers
// Recovery: Operator must fix vendor config (remove route or re-add peer)
// Code: setState(eniID, ENIStateFAILED); eni.FailureReason = "<details>"
```

### 5.2 Soft Failures (Async Retry)

```go
// MappingManager: Peer mappings not yet arrived
// Reason: Mappings can be large; may take seconds to transmit
// Action: ENI stays in INCOMPLETE; MappingManager monitors for arrival
// Recovery: When mappings arrive, MappingManager.OnMappingEvent() triggers re-check
// Timeout: Monitoring alert if ENI stuck INCOMPLETE > 5 minutes
```

### 5.3 Peer Removal Recovery

```go
// Peer removed while routes target it
// Trigger: VnetRegistry signal ("PeerRemoved")
// Action: NicRegistry re-validates all ENIs in VNET
// Outcome: If route targets removed peer, ENI.state = FAILED
// Recovery: Operator must fix route config or re-add peer
// Alert: "ENI <id> in VNET <id> routes to non-peered VNET <id>"
```

---

## 6. Concurrency Model

### 6.1 Lock Strategy

| Component | Lock Type | Scope | Held During |
|-----------|-----------|-------|-------------|
| VnetRegistry | RWMutex | vnets map | Read: GetVnetState; Write: OnVnetEvent |
| NicRegistry | RWMutex | enis map | Read: GetENIState; Write: Hydrate, setState |
| MappingManager | RWMutex | mappingsByVnet, eniWaitingFor | Read: IsMappingReady; Write: OnMappingEvent, CheckENIReady |

### 6.2 Signal Channel Design

```go
// Signal channels are broadcast points (not request-response).
// Consumers must drain channels to avoid blocking broadcasts.

// Bad: Synchronous signal handler (can block broadcaster)
// signalChan <- signal  // Blocks if receiver slow

// Good: Async listener with buffered channel
signals := make(chan Signal, 1000)  // Buffer prevents broadcaster blocking
go func() {
	for signal := range signals {
		handleSignal(signal)
	}
}()
```

---

## 7. Monitoring & Observability Hooks

### 7.1 Metrics (Prometheus)

```go
// In each registry:

// VnetRegistry metrics
fm_vnet_count{state="READY"} = len(vnets)
fm_peer_count_by_vnet{vnet_id} = len(peers)

// NicRegistry metrics
fm_eni_state_count{state="FAILED"} = count
fm_eni_state_count{state="INCOMPLETE"} = count
fm_eni_state_count{state="READY"} = count
fm_eni_incomplete_duration_seconds{quantile="0.95"} = histogram

// MappingManager metrics
fm_mapping_ready_by_vnet{vnet_id} = bool (0 or 1)
fm_mapping_arrival_latency_seconds{vnet_id, quantile="0.95"} = histogram
fm_eni_waiting_for_mappings_count = len(eniWaitingFor)
```

### 7.2 Alerts

```
Alert "ENI Hydration Failed":
  IF fm_eni_state_count{state="FAILED"} > 100 for 5 min:
    ACTION: Page oncall; check logs for vendor config errors

Alert "Mappings Stalled":
  IF fm_eni_state_count{state="INCOMPLETE"} > 100 for 5 min:
    ACTION: Page oncall; check CB mapping stream health

Alert "Peer Validation Error":
  IF ENISignal.Event == "Failed" AND FailureReason contains "non-peered":
    ACTION: Alert operator; route targets non-peered VNET (vendor error)
```

---

## 8. Testing

### 8.1 Unit Tests

```go
// registry_test.go

func TestVnetRegistry_OnVnetEvent_PeerAdded(t *testing.T) {
	// Setup: VnetRegistry with empty vnets
	// Action: OnVnetEvent({ vnet_id: "A", peer_vnet_ids: ["B"] })
	// Assert: vnets["A"].PeerVnetIDs == ["B"]
	// Assert: VnetSignal sent on signals channel with Event="PeerAdded"
	panic("TODO: implement")
}

func TestNicRegistry_Hydrate_ValidatesNonPeeredTarget(t *testing.T) {
	// Setup: VNET-A { peers: [] }, Route A→B, ENI-1 in A
	// Action: NicRegistry.Hydrate(eniID)
	// Assert: ENI-1.state == FAILED
	// Assert: FailureReason contains "non-peered"
	panic("TODO: implement")
}

func TestMappingManager_CheckENIReady_AllPeersReady(t *testing.T) {
	// Setup: ENI-1 routes to peers [B, C]; mappings for B,C marked complete
	// Action: CheckENIReady(eniID)
	// Assert: Returns true
	// Assert: ENI-1.state transitioned to READY
	panic("TODO: implement")
}
```

### 8.2 Integration Tests (Conformance)

```go
// conformance_test.go

func TestT10_PeeringValidation(t *testing.T) {
	// Given: VNET-A { peers: [] }
	// When: Route A→B arrives; ENI-1 hydrates
	// Then: ENI-1.state == FAILED (hard fail)
	panic("TODO: implement")
}

func TestT11_MappingConsistency(t *testing.T) {
	// Given: VNET-A { peers: [B] }, route A→B, ENI-1 (no mappings for B)
	// When: NicRegistry.Hydrate(eniID)
	// Then: ENI-1.state == INCOMPLETE
	// When: MappingManager receives mapping for B
	// Then: ENI-1.state == READY (no traffic loss)
	panic("TODO: implement")
}

func TestT12_PeerRemovalDetection(t *testing.T) {
	// Given: VNET-A { peers: [B] }, route A→B, ENI-1 (READY)
	// When: Update VNET-A { peers: [] }
	// Then: VnetRegistry broadcasts PeerRemoved signal
	// Then: NicRegistry re-validates ENI-1
	// Then: ENI-1.state == FAILED (operator alerted)
	panic("TODO: implement")
}
```

---

## References

- `Specs/protocols/fm-peering-protocol.md` — Design decisions
- `Specs/FM/fm-registry-peering-design.md` — Component architecture
- `Specs/me-and-ai/fm-peering-hybrid-model-decision.md` — Hybrid model rationale
