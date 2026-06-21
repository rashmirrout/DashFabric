package goalstatemanagement

import (
	"context"
	"fmt"
	"sync"
)

// GoalStateManagerImpl orchestrates VNET aggregation, goal state generation, and caching
type GoalStateManagerImpl struct {
	mu         sync.RWMutex
	aggregator VNETAggregator
	generator  GoalStateGenerator
	cache      GoalStateCache
	running    bool
	cancel     context.CancelFunc
	ctx        context.Context
	stats      ManagerStats
}

// NewGoalStateManager creates a new goal state manager
func NewGoalStateManager(agg VNETAggregator, gen GoalStateGenerator, cache GoalStateCache) GoalStateManager {
	return &GoalStateManagerImpl{
		aggregator: agg,
		generator:  gen,
		cache:      cache,
		stats:      ManagerStats{},
	}
}

// Start initializes the goal state manager
func (m *GoalStateManagerImpl) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running {
		return fmt.Errorf("goal state manager already running")
	}

	m.ctx, m.cancel = context.WithCancel(ctx)
	m.running = true

	return nil
}

// Stop gracefully shuts down the goal state manager
func (m *GoalStateManagerImpl) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.running {
		return fmt.Errorf("goal state manager not running")
	}

	if m.cancel != nil {
		m.cancel()
	}
	m.running = false

	return nil
}

// HandleConstructChange processes a construct change and updates goal states
func (m *GoalStateManagerImpl) HandleConstructChange(ctx context.Context, construct *ConstructState) error {
	m.mu.RLock()
	if !m.running {
		m.mu.RUnlock()
		return fmt.Errorf("goal state manager not running")
	}
	m.mu.RUnlock()

	select {
	case <-ctx.Done():
		return fmt.Errorf("construct change handling cancelled: %w", ctx.Err())
	case <-m.ctx.Done():
		return fmt.Errorf("goal state manager stopped")
	default:
	}

	if construct == nil {
		return fmt.Errorf("construct required")
	}

	// Aggregate VNET state
	agg, err := m.aggregator.Aggregate(ctx, construct.VnetID)
	if err != nil {
		m.mu.Lock()
		m.stats.AggregationErrors++
		m.mu.Unlock()
		return fmt.Errorf("aggregation failed: %w", err)
	}

	m.mu.Lock()
	m.stats.VNETsProcessed++
	m.mu.Unlock()

	// Generate goal states
	states, err := m.generator.Generate(ctx, agg)
	if err != nil {
		m.mu.Lock()
		m.stats.GenerationErrors++
		m.mu.Unlock()
		return fmt.Errorf("goal state generation failed: %w", err)
	}

	m.mu.Lock()
	m.stats.GoalStatesGenerated += int64(len(states))
	m.mu.Unlock()

	// Cache goal states by fingerprint
	for _, state := range states {
		if state != nil {
			m.cache.Set(state.Fingerprint, state)
		}
	}

	return nil
}

// GetGoalState retrieves a cached goal state by ENI ID
func (m *GoalStateManagerImpl) GetGoalState(eniID string) (*PerENIGoalState, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if eniID == "" {
		return nil, false
	}

	// For now, search cache (in production, would index by ENI_ID)
	if cacheImpl, ok := m.cache.(*GoalStateCacheImpl); ok {
		cacheImpl.mu.RLock()
		for _, state := range cacheImpl.cache {
			if state != nil && state.ENI_ID == eniID {
				cacheImpl.mu.RUnlock()
				m.stats.CacheHits++
				return state, true
			}
		}
		cacheImpl.mu.RUnlock()
	}

	m.stats.CacheMisses++
	return nil, false
}

// Stats returns goal state manager statistics
func (m *GoalStateManagerImpl) Stats() ManagerStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.stats
}
