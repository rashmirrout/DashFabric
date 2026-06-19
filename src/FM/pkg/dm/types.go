package datamanagement

import (
	"context"
	"sync"
	"time"

	cm "github.com/dashfabric/fm/pkg/cm"
)

// ENIState represents the state of an ENI in the system
type ENIState struct {
	ID              string
	VnetID          string
	MAC             string
	IPAddress       string
	Status          string // "active", "inactive", "error"
	LastUpdated     time.Time
	ReplicaID       string
	ConfigVersion   int
	mu              sync.RWMutex
}

// VIPBinding represents VIP-to-DIP binding state
type VIPBinding struct {
	VIP         string
	DIP         string
	SNAT        bool
	ENI         string
	BindingTime time.Time
	Status      string
	mu          sync.RWMutex
}

// ConsistencyRule defines a constraint that must be enforced
type ConsistencyRule interface {
	// Name returns the rule name
	Name() string

	// Validate checks if the rule is satisfied given current state
	Validate(ctx context.Context, state *SystemState) error

	// Enforce applies corrective actions if rule is violated
	Enforce(ctx context.Context, state *SystemState) error
}

// SystemState represents the complete state of the FM system
type SystemState struct {
	ENIs       map[string]*ENIState      // ENI ID → ENIState
	VIPs       map[string]*VIPBinding    // VIP → Binding
	Replicas   map[string]*ReplicaState  // Replica ID → State
	Metadata   map[string]interface{}    // Indexed metadata
	Version    int64                     // Global version counter
	mu         sync.RWMutex
}

// ReplicaState tracks state of a replica
type ReplicaState struct {
	ID         string
	Address    string
	Healthy    bool
	LastHeartbeat time.Time
	CommittedVersion int64
	mu         sync.RWMutex
}

// VnetRegistry manages all ENIs and state for a VNET
type VnetRegistry struct {
	vnetID string
	enis   map[string]*ENIState
	mu     sync.RWMutex
}

// NicRegistry manages mappings at NIC/ENI level
type NicRegistry struct {
	nicID  string
	bindings map[string]*VIPBinding // VIP → Binding for this NIC
	mu     sync.RWMutex
}

// MappingManager coordinates registry and consistency
type MappingManager struct {
	vnetRegistries map[string]*VnetRegistry
	nicRegistries  map[string]*NicRegistry
	systemState    *SystemState
	rules          []ConsistencyRule
	mu             sync.RWMutex
}

// Actor represents a serialized work unit
type Actor struct {
	ID     string
	Type   string // "vnet", "eni", "vip"
	Queue  chan ActorMessage
	State  interface{}
	mu     sync.Mutex
}

// ActorMessage is the message type for actor communication
type ActorMessage struct {
	Event   *cm.Event
	Sender  string
	ReplyTo chan error
}

// NewVnetRegistry creates a new registry for a VNET
func NewVnetRegistry(vnetID string) *VnetRegistry {
	return &VnetRegistry{
		vnetID: vnetID,
		enis:   make(map[string]*ENIState),
	}
}

// AddENI adds or updates an ENI in the registry
func (r *VnetRegistry) AddENI(eni *ENIState) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.enis[eni.ID] = eni
}

// GetENI retrieves an ENI by ID
func (r *VnetRegistry) GetENI(eniID string) (*ENIState, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	eni, ok := r.enis[eniID]
	return eni, ok
}

// ListENIs returns all ENIs in this VNET
func (r *VnetRegistry) ListENIs() []*ENIState {
	r.mu.RLock()
	defer r.mu.RUnlock()
	enis := make([]*ENIState, 0, len(r.enis))
	for _, e := range r.enis {
		enis = append(enis, e)
	}
	return enis
}

// NewNicRegistry creates a new registry for a NIC
func NewNicRegistry(nicID string) *NicRegistry {
	return &NicRegistry{
		nicID:    nicID,
		bindings: make(map[string]*VIPBinding),
	}
}

// AddBinding adds or updates a VIP binding
func (r *NicRegistry) AddBinding(vip string, binding *VIPBinding) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.bindings[vip] = binding
}

// GetBinding retrieves a VIP binding
func (r *NicRegistry) GetBinding(vip string) (*VIPBinding, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	b, ok := r.bindings[vip]
	return b, ok
}

// ListBindings returns all bindings for this NIC
func (r *NicRegistry) ListBindings() []*VIPBinding {
	r.mu.RLock()
	defer r.mu.RUnlock()
	bindings := make([]*VIPBinding, 0, len(r.bindings))
	for _, b := range r.bindings {
		bindings = append(bindings, b)
	}
	return bindings
}

// NewMappingManager creates a new manager
func NewMappingManager() *MappingManager {
	return &MappingManager{
		vnetRegistries: make(map[string]*VnetRegistry),
		nicRegistries:  make(map[string]*NicRegistry),
		systemState: &SystemState{
			ENIs:     make(map[string]*ENIState),
			VIPs:     make(map[string]*VIPBinding),
			Replicas: make(map[string]*ReplicaState),
			Metadata: make(map[string]interface{}),
		},
		rules: make([]ConsistencyRule, 0),
	}
}

// RegisterRule adds a consistency rule
func (mm *MappingManager) RegisterRule(rule ConsistencyRule) {
	mm.mu.Lock()
	defer mm.mu.Unlock()
	mm.rules = append(mm.rules, rule)
}

// GetOrCreateVnetRegistry gets or creates a VNET registry
func (mm *MappingManager) GetOrCreateVnetRegistry(vnetID string) *VnetRegistry {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	if reg, ok := mm.vnetRegistries[vnetID]; ok {
		return reg
	}

	reg := NewVnetRegistry(vnetID)
	mm.vnetRegistries[vnetID] = reg
	return reg
}

// GetOrCreateNicRegistry gets or creates a NIC registry
func (mm *MappingManager) GetOrCreateNicRegistry(nicID string) *NicRegistry {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	if reg, ok := mm.nicRegistries[nicID]; ok {
		return reg
	}

	reg := NewNicRegistry(nicID)
	mm.nicRegistries[nicID] = reg
	return reg
}

// ValidateConsistency validates all rules
func (mm *MappingManager) ValidateConsistency(ctx context.Context) error {
	mm.mu.RLock()
	rules := make([]ConsistencyRule, len(mm.rules))
	copy(rules, mm.rules)
	state := mm.systemState
	mm.mu.RUnlock()

	for _, rule := range rules {
		if err := rule.Validate(ctx, state); err != nil {
			return err
		}
	}
	return nil
}

// NewActor creates a new actor for serialized processing
func NewActor(id string, actorType string) *Actor {
	return &Actor{
		ID:    id,
		Type:  actorType,
		Queue: make(chan ActorMessage, 100),
		State: nil,
	}
}

// SendMessage sends a message to the actor
func (a *Actor) SendMessage(msg ActorMessage) error {
	select {
	case a.Queue <- msg:
		return nil
	default:
		return ErrActorQueueFull
	}
}

// ProcessMessages begins processing messages from the queue
func (a *Actor) ProcessMessages(ctx context.Context, handler func(ActorMessage) error) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case msg := <-a.Queue:
				err := handler(msg)
				if msg.ReplyTo != nil {
					msg.ReplyTo <- err
				}
			}
		}
	}()
}
