package goalstatemanagement

import (
	"context"
	"time"
)

// PerENIGoalState represents the desired configuration for a single ENI
type PerENIGoalState struct {
	ENI_ID      string
	Routes      []*Route
	ACLs        []*ACLRule
	VIPMappings []*VIPMapping
	Fingerprint string
	Version     int64
	LastUpdated time.Time
}

// Route represents a routing entry
type Route struct {
	Destination string
	NextHop     string
	Metric      int
}

// ACLRule represents an access control rule
type ACLRule struct {
	ID       string
	Action   string // "allow" or "deny"
	Protocol string
	SrcIP    string
	DstIP    string
	SrcPort  int
	DstPort  int
}

// VIPMapping represents a VIP-to-DIP mapping
type VIPMapping struct {
	VIP string
	DIP string
}

// ConstructState represents a generic construct state (simplified for testing)
type ConstructState struct {
	ID              string
	VnetID          string
	Status          string
	ConfigVersion   int
	TenantID        string
	ConstructType   string
	LastUpdated     time.Time
	ReplicaID       string
}

// ReplicaState represents a replica state (simplified for testing)
type ReplicaState struct {
	ID       string
	VnetID   string
	Status   string
	HealthOK bool
}

// AggregatedState represents all constructs for a VNET
type AggregatedState struct {
	VnetID     string
	Constructs []*ConstructState
	Replicas   []*ReplicaState
}

// VNETAggregator aggregates constructs for a VNET
type VNETAggregator interface {
	Aggregate(ctx context.Context, vnetID string) (*AggregatedState, error)
}

// GoalStateGenerator generates per-ENI goal states
type GoalStateGenerator interface {
	Generate(ctx context.Context, agg *AggregatedState) ([]*PerENIGoalState, error)
}

// GoalStateCache caches fingerprint-based goal states
type GoalStateCache interface {
	Get(fingerprint string) (*PerENIGoalState, bool)
	Set(fingerprint string, state *PerENIGoalState)
	Clear()
}

// GoalStateManager orchestrates goal state management
type GoalStateManager interface {
	HandleConstructChange(ctx context.Context, construct *ConstructState) error
	GetGoalState(eniID string) (*PerENIGoalState, bool)
	Start(ctx context.Context) error
	Stop() error
}

// ManagerStats represents manager statistics
type ManagerStats struct {
	VNETsProcessed       int64
	GoalStatesGenerated  int64
	CacheHits            int64
	CacheMisses          int64
	AggregationErrors    int64
	GenerationErrors     int64
	AggregationTimeMs    float64
	GenerationTimeMs     float64
	Uptime               time.Duration
}
