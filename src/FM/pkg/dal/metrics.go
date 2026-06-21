package dpuabstraction

import (
	obs "github.com/dashfabric/fm/pkg/observability"
)

// metricsRegistry is the package-level metrics registry
var metricsRegistry *obs.MetricsRegistry

// SetMetricsRegistry sets the package-level metrics registry for DAL module
func SetMetricsRegistry(registry *obs.MetricsRegistry) {
	if registry != nil {
		metricsRegistry = registry
	}
}

// incrementProgramAttempts increments the program attempts counter
func incrementProgramAttempts() {
	if metricsRegistry != nil {
		metricsRegistry.IncrementCounter(metricsRegistry.DALProgramAttempts)
	}
}

// incrementProgramSuccesses increments the program successes counter
func incrementProgramSuccesses() {
	if metricsRegistry != nil {
		metricsRegistry.IncrementCounter(metricsRegistry.DALProgramSuccesses)
	}
}

// incrementProgramFailures increments the program failures counter
func incrementProgramFailures() {
	if metricsRegistry != nil {
		metricsRegistry.IncrementCounter(metricsRegistry.DALProgramFailures)
	}
}
