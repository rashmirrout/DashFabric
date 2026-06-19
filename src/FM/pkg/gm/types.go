package goalstatemanagement

import (
	"context"
	"time"

	"github.com/dashfabric/fm/pkg/dm"
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

// AggregatedState represents all constructs for a VNET
type AggregatedState struct {
	VnetID   string
	Constructs []*dm.ENIState
	Replicas   []*dm.ReplicaState
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
	HandleConstructChange(ctx context.Context, construct *dm.ENIState) error
	GetGoalState(eniID string) (*PerENIGoalState, bool)
	Start(ctx context.Context) error
	Stop() error
}

// ManagerStats represents manager statistics
type ManagerStats struct {
	GoalStatesGenerated int64
	CacheHits          int64
	CacheMisses        int64
	Uptime             time.Duration
}
