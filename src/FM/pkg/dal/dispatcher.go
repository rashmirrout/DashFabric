package dpuabstraction

import (
	"context"
	"fmt"
	"sync"
	"time"

	gm "github.com/dashfabric/fm/pkg/gm"
	res "github.com/dashfabric/fm/pkg/resilience"
)

// PluginDispatcherImpl routes goal states to appropriate vendor plugins
type PluginDispatcherImpl struct {
	mu               sync.RWMutex
	registry         *PluginRegistry
	stats            DispatcherStats
	circuitBreakers  map[string]*res.CircuitBreaker
	retryPolicy      res.RetryPolicy
	timeoutManager   *res.TimeoutManager
	vendorFallbacks  map[string][]string
}

// DispatcherStats tracks dispatcher metrics
type DispatcherStats struct {
	GoalsDispatched   int64
	SuccessCount      int64
	FailureCount      int64
	TimeoutCount      int64
	RetryCount        int64
	CircuitOpenCount  int64
	FallbackActivated int64
}

// NewPluginDispatcher creates a new plugin dispatcher with resilience
func NewPluginDispatcher(registry *PluginRegistry) PluginDispatcher {
	return &PluginDispatcherImpl{
		registry:        registry,
		stats:           DispatcherStats{},
		circuitBreakers: make(map[string]*res.CircuitBreaker),
		retryPolicy:     res.DefaultRetryPolicy(),
		timeoutManager:  res.NewTimeoutManager(30 * time.Second),
		vendorFallbacks: map[string][]string{
			"Intel":   {"Custom"},
			"Nvidia":  {"Custom"},
			"Custom":  {},
		},
	}
}

// Dispatch routes a goal state to the appropriate vendor plugin with resilience
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
	vendor := "Custom"

	// Try primary vendor with circuit breaker protection
	result, err := d.dispatchWithResilience(ctx, vendor, goal)
	if err == nil {
		return result, nil
	}

	// Try fallback vendors on failure
	fallbacks := d.vendorFallbacks[vendor]
	for _, fallback := range fallbacks {
		d.mu.Lock()
		d.stats.FallbackActivated++
		d.mu.Unlock()

		result, fallbackErr := d.dispatchWithResilience(ctx, fallback, goal)
		if fallbackErr == nil {
			return result, nil
		}
		err = fallbackErr
	}

	d.mu.Lock()
	d.stats.FailureCount++
	d.mu.Unlock()

	return nil, err
}

// dispatchWithResilience dispatches with retry and circuit breaker
func (d *PluginDispatcherImpl) dispatchWithResilience(ctx context.Context, vendor string, goal *gm.PerENIGoalState) (*ProgramResult, error) {
	// Get or create circuit breaker for vendor
	d.mu.Lock()
	cb, exists := d.circuitBreakers[vendor]
	if !exists {
		cb = res.NewCircuitBreaker(5, 2, 30*time.Second)
		d.circuitBreakers[vendor] = cb
	}
	d.mu.Unlock()

	// Check circuit breaker state
	if cb.State() == res.StateOpen {
		d.mu.Lock()
		d.stats.CircuitOpenCount++
		d.mu.Unlock()
		return nil, fmt.Errorf("circuit breaker open for vendor %s", vendor)
	}

	// Execute with retry and circuit breaker protection
	var result *ProgramResult
	err := cb.Call(func() error {
		var innerErr error
		innerErr = d.retryPolicy.Attempt(ctx, func() error {
			var programErr error

			// Set stage-specific timeout
			stageCtx, cancel := d.timeoutManager.WithTimeout(ctx, vendor)
			defer cancel()

			// Get plugin from registry
			plugin, lookupErr := d.registry.Get(vendor)
			if lookupErr != nil {
				return fmt.Errorf("vendor plugin lookup failed: %w", lookupErr)
			}

			// Program the device
			result, programErr = plugin.Program(stageCtx, goal)
			if programErr != nil {
				// Classify error to determine if retry is appropriate
				if res.IsRetryable(programErr) {
					d.mu.Lock()
					d.stats.RetryCount++
					d.mu.Unlock()
					return programErr
				}
				return programErr
			}

			if !result.Success {
				return fmt.Errorf("programming failed: %s", result.Error)
			}

			return nil
		})

		return innerErr
	})

	if err != nil {
		return nil, err
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

// ResetCircuitBreaker resets a vendor's circuit breaker
func (d *PluginDispatcherImpl) ResetCircuitBreaker(vendor string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if cb, exists := d.circuitBreakers[vendor]; exists {
		cb.Reset()
	}
}
