package dpuabstraction

import (
	"context"
	"fmt"
	"sync"

	gm "github.com/dashfabric/fm/pkg/gm"
)

// PluginDispatcherImpl routes goal states to appropriate vendor plugins
type PluginDispatcherImpl struct {
	mu       sync.RWMutex
	registry *PluginRegistry
	stats    DispatcherStats
}

// DispatcherStats tracks dispatcher metrics
type DispatcherStats struct {
	GoalsDispatched int64
	SuccessCount    int64
	FailureCount    int64
	TimeoutCount    int64
}

// NewPluginDispatcher creates a new plugin dispatcher
func NewPluginDispatcher(registry *PluginRegistry) PluginDispatcher {
	return &PluginDispatcherImpl{
		registry: registry,
		stats:    DispatcherStats{},
	}
}

// Dispatch routes a goal state to the appropriate vendor plugin
func (d *PluginDispatcherImpl) Dispatch(ctx context.Context, goal *gm.PerENIGoalState) (*ProgramResult, error) {
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("dispatch cancelled: %w", ctx.Err())
	default:
	}

	if goal == nil {
		return nil, fmt.Errorf("goal state required")
	}

	d.mu.Lock()
	d.stats.GoalsDispatched++
	d.mu.Unlock()

	// Determine vendor from ENI ID or goal state metadata
	// Default to Custom vendor for now
	vendor := "Custom"

	// Get plugin from registry
	plugin, err := d.registry.Get(vendor)
	if err != nil {
		d.mu.Lock()
		d.stats.FailureCount++
		d.mu.Unlock()
		return nil, fmt.Errorf("vendor plugin lookup failed: %w", err)
	}

	// Program the device
	result, err := plugin.Program(ctx, goal)
	if err != nil {
		d.mu.Lock()
		d.stats.FailureCount++
		d.mu.Unlock()
		return nil, fmt.Errorf("plugin programming failed: %w", err)
	}

	if !result.Success {
		d.mu.Lock()
		d.stats.FailureCount++
		d.mu.Unlock()
		return result, fmt.Errorf("programming failed: %s", result.Error)
	}

	d.mu.Lock()
	d.stats.SuccessCount++
	d.mu.Unlock()

	return result, nil
}

// Stats returns dispatcher statistics
func (d *PluginDispatcherImpl) Stats() DispatcherStats {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.stats
}
