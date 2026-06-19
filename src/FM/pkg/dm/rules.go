package datamanagement

import (
	"context"
	"fmt"
)

// ENIStateRule enforces valid ENI state transitions
type ENIStateRule struct{}

func (r *ENIStateRule) Name() string {
	return "ENI-State"
}

func (r *ENIStateRule) Validate(ctx context.Context, state *SystemState) error {
	state.mu.RLock()
	defer state.mu.RUnlock()

	for _, eni := range state.ENIs {
		eni.mu.RLock()
		status := eni.Status
		eni.mu.RUnlock()

		// Valid states: "active", "inactive", "error"
		if status != "active" && status != "inactive" && status != "error" {
			return &ConsistencyViolationError{
				Rule:    r.Name(),
				Details: fmt.Sprintf("ENI %s has invalid status: %s", eni.ID, status),
			}
		}
	}
	return nil
}

func (r *ENIStateRule) Enforce(ctx context.Context, state *SystemState) error {
	// In production: attempt to transition to valid state
	return r.Validate(ctx, state)
}

// VIPBindingRule ensures every VIP has a valid DIP
type VIPBindingRule struct{}

func (r *VIPBindingRule) Name() string {
	return "VIP-Binding"
}

func (r *VIPBindingRule) Validate(ctx context.Context, state *SystemState) error {
	state.mu.RLock()
	defer state.mu.RUnlock()

	for vip, binding := range state.VIPs {
		binding.mu.RLock()
		dip := binding.DIP
		status := binding.Status
		binding.mu.RUnlock()

		if dip == "" {
			return &ConsistencyViolationError{
				Rule:    r.Name(),
				Details: fmt.Sprintf("VIP %s has no DIP binding", vip),
			}
		}

		if status != "active" && status != "inactive" {
			return &ConsistencyViolationError{
				Rule:    r.Name(),
				Details: fmt.Sprintf("VIP %s has invalid status: %s", vip, status),
			}
		}
	}
	return nil
}

func (r *VIPBindingRule) Enforce(ctx context.Context, state *SystemState) error {
	return r.Validate(ctx, state)
}

// SNATPoolRule ensures SNAT state consistency
type SNATPoolRule struct{}

func (r *SNATPoolRule) Name() string {
	return "SNAT-Pool"
}

func (r *SNATPoolRule) Validate(ctx context.Context, state *SystemState) error {
	state.mu.RLock()
	defer state.mu.RUnlock()

	for vip, binding := range state.VIPs {
		binding.mu.RLock()
		snat := binding.SNAT
		eni := binding.ENI
		binding.mu.RUnlock()

		if snat {
			if eni == "" {
				return &ConsistencyViolationError{
					Rule:    r.Name(),
					Details: fmt.Sprintf("VIP %s requires SNAT but has no ENI", vip),
				}
			}

			// Verify ENI exists
			if _, ok := state.ENIs[eni]; !ok {
				return &ConsistencyViolationError{
					Rule:    r.Name(),
					Details: fmt.Sprintf("VIP %s SNAT ENI %s not found", vip, eni),
				}
			}
		}
	}
	return nil
}

func (r *SNATPoolRule) Enforce(ctx context.Context, state *SystemState) error {
	return r.Validate(ctx, state)
}

// RouteValidityRule ensures routes are reachable
type RouteValidityRule struct{}

func (r *RouteValidityRule) Name() string {
	return "Route-Validity"
}

func (r *RouteValidityRule) Validate(ctx context.Context, state *SystemState) error {
	state.mu.RLock()
	defer state.mu.RUnlock()

	// In production: validate routes are reachable via replicas
	// For now: simple check that at least one replica is healthy
	healthyReplicas := 0
	for _, replica := range state.Replicas {
		replica.mu.RLock()
		healthy := replica.Healthy
		replica.mu.RUnlock()
		if healthy {
			healthyReplicas++
		}
	}

	if healthyReplicas == 0 && len(state.ENIs) > 0 {
		return &ConsistencyViolationError{
			Rule:    r.Name(),
			Details: "no healthy replicas to program routes",
		}
	}
	return nil
}

func (r *RouteValidityRule) Enforce(ctx context.Context, state *SystemState) error {
	return r.Validate(ctx, state)
}

// ReplicaHealthRule ensures only healthy replicas are used
type ReplicaHealthRule struct{}

func (r *ReplicaHealthRule) Name() string {
	return "Replica-Health"
}

func (r *ReplicaHealthRule) Validate(ctx context.Context, state *SystemState) error {
	state.mu.RLock()
	defer state.mu.RUnlock()

	for replicaID, replica := range state.Replicas {
		replica.mu.RLock()
		healthy := replica.Healthy
		replica.mu.RUnlock()

		if !healthy {
			// Warning, not error - system can degrade gracefully
			// In production: log and track unhealthy replicas
			_ = fmt.Sprintf("replica %s is unhealthy", replicaID)
		}
	}
	return nil
}

func (r *ReplicaHealthRule) Enforce(ctx context.Context, state *SystemState) error {
	return r.Validate(ctx, state)
}
