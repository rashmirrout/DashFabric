package dpuabstraction

import (
	"context"
	"fmt"
	"sync"

	gm "github.com/dashfabric/fm/pkg/gm"
)

// DPUAbstractionManagerImpl orchestrates plugin dispatcher and pool
type DPUAbstractionManagerImpl struct {
	mu         sync.RWMutex
	registry   *PluginRegistry
	dispatcher PluginDispatcher
	pool       PluginPool
	running    bool
	cancel     context.CancelFunc
	ctx        context.Context
	stats      ManagerStats
}

// NewDPUAbstractionManager creates a new DPU abstraction manager
func NewDPUAbstractionManager(registry *PluginRegistry, poolWorkers int) DPUAbstractionManager {
	dispatcher := NewPluginDispatcher(registry)
	pool := NewPluginPool(dispatcher, poolWorkers)
	return &DPUAbstractionManagerImpl{
		registry:   registry,
		dispatcher: dispatcher,
		pool:       pool,
		stats:      ManagerStats{},
	}
}

// Start initializes the DPU abstraction manager
func (m *DPUAbstractionManagerImpl) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running {
		return fmt.Errorf("DPU abstraction manager already running")
	}

	m.ctx, m.cancel = context.WithCancel(ctx)
	m.running = true

	// Start the pool
	if err := m.pool.(*PluginPoolImpl).Start(m.ctx); err != nil {
		m.running = false
		return fmt.Errorf("pool startup failed: %w", err)
	}

	return nil
}

// Stop gracefully shuts down the DPU abstraction manager
func (m *DPUAbstractionManagerImpl) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.running {
		return fmt.Errorf("DPU abstraction manager not running")
	}

	// Stop the pool
	if err := m.pool.Shutdown(m.ctx); err != nil {
		return fmt.Errorf("pool shutdown failed: %w", err)
	}

	if m.cancel != nil {
		m.cancel()
	}
	m.running = false

	return nil
}

// Program submits a goal state for device programming
func (m *DPUAbstractionManagerImpl) Program(ctx context.Context, goal *gm.PerENIGoalState) (*ProgramResult, error) {
	m.mu.RLock()
	if !m.running {
		m.mu.RUnlock()
		return nil, fmt.Errorf("DPU abstraction manager not running")
	}
	m.mu.RUnlock()

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("programming cancelled: %w", ctx.Err())
	case <-m.ctx.Done():
		return nil, fmt.Errorf("DPU abstraction manager stopped")
	default:
	}

	if goal == nil {
		return nil, fmt.Errorf("goal state required")
	}

	m.mu.Lock()
	m.stats.ProgramsSubmitted++
	m.mu.Unlock()

	// Submit to pool
	resultChan := m.pool.Submit(ctx, goal)

	// Wait for result
	select {
	case result := <-resultChan:
		m.mu.Lock()
		if result != nil && result.Success {
			m.stats.ProgramsSucceeded++
		} else {
			m.stats.ProgramsFailed++
		}
		m.mu.Unlock()
		return result, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("result wait cancelled: %w", ctx.Err())
	case <-m.ctx.Done():
		return nil, fmt.Errorf("DPU abstraction manager stopped")
	}
}

// Stats returns DPU manager statistics
func (m *DPUAbstractionManagerImpl) Stats() ManagerStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.stats
}
