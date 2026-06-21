package goalstatemanagement

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
)

// GoalStateGeneratorImpl generates per-ENI goal states from aggregated constructs
type GoalStateGeneratorImpl struct{}

// NewGoalStateGenerator creates a new goal state generator
func NewGoalStateGenerator() GoalStateGenerator {
	return &GoalStateGeneratorImpl{}
}

// Generate creates per-ENI goal states from aggregated state
func (g *GoalStateGeneratorImpl) Generate(ctx context.Context, agg *AggregatedState) ([]*PerENIGoalState, error) {
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("generation cancelled: %w", ctx.Err())
	default:
	}

	if agg == nil {
		return nil, fmt.Errorf("aggregated state required")
	}

	states := make([]*PerENIGoalState, 0, len(agg.Constructs))

	for _, construct := range agg.Constructs {
		state := &PerENIGoalState{
			ENI_ID:      construct.ID,
			Routes:      make([]*Route, 0),
			ACLs:        make([]*ACLRule, 0),
			VIPMappings: make([]*VIPMapping, 0),
			Version:     int64(construct.ConfigVersion),
		}

		// Generate fingerprint from canonical JSON
		payload := map[string]interface{}{
			"eni_id":      construct.ID,
			"vnet_id":     construct.VnetID,
			"status":      construct.Status,
			"config_ver":  construct.ConfigVersion,
		}

		data, _ := json.Marshal(payload)
		hash := sha256.Sum256(data)
		state.Fingerprint = fmt.Sprintf("%x", hash[:])

		states = append(states, state)
	}

	return states, nil
}
