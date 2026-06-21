package goalstatemanagement

import (
	"context"
	"fmt"
)

// VNETAggregatorImpl aggregates all constructs for a VNET
type VNETAggregatorImpl struct {
	// Construct storage - simplified for Phase 2
	constructs map[string]*ConstructState
}

// NewVNETAggregator creates a new VNET aggregator
func NewVNETAggregator(constructs map[string]*ConstructState) VNETAggregator {
	if constructs == nil {
		constructs = make(map[string]*ConstructState)
	}
	return &VNETAggregatorImpl{
		constructs: constructs,
	}
}

// Aggregate fetches and aggregates all constructs for a VNET
func (a *VNETAggregatorImpl) Aggregate(ctx context.Context, vnetID string) (*AggregatedState, error) {
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("aggregation cancelled: %w", ctx.Err())
	default:
	}

	if vnetID == "" {
		return nil, fmt.Errorf("vnet ID required")
	}

	// Filter constructs for this VNET
	vnetConstructs := make([]*ConstructState, 0)
	replicas := make([]*ReplicaState, 0)

	for _, construct := range a.constructs {
		if construct.VnetID == vnetID {
			vnetConstructs = append(vnetConstructs, construct)
		}
	}

	state := &AggregatedState{
		VnetID:     vnetID,
		Constructs: vnetConstructs,
		Replicas:   replicas,
	}

	return state, nil
}

// AddConstruct adds a construct for aggregation (testing helper)
func (a *VNETAggregatorImpl) AddConstruct(construct *ConstructState) {
	if construct != nil && construct.ID != "" {
		a.constructs[construct.ID] = construct
	}
}
