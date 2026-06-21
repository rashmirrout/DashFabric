package goalstatemanagement

import (
	obs "github.com/dashfabric/fm/pkg/observability"
)

// metricsRegistry is the package-level metrics registry
var metricsRegistry *obs.MetricsRegistry

// SetMetricsRegistry sets the package-level metrics registry for GM module
func SetMetricsRegistry(registry *obs.MetricsRegistry) {
	if registry != nil {
		metricsRegistry = registry
	}
}

// incrementGoalStatesGenerated increments the goal states generated counter
func incrementGoalStatesGenerated() {
	if metricsRegistry != nil {
		metricsRegistry.IncrementCounter(metricsRegistry.GMGoalStatesGenerated)
	}
}

// incrementCacheHits increments the cache hits counter
func incrementCacheHits() {
	if metricsRegistry != nil {
		metricsRegistry.IncrementCounter(metricsRegistry.GMCacheHits)
	}
}

// incrementCacheMisses increments the cache misses counter
func incrementCacheMisses() {
	if metricsRegistry != nil {
		metricsRegistry.IncrementCounter(metricsRegistry.GMCacheMisses)
	}
}
