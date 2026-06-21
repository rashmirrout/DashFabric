package configmanagement

import (
	obs "github.com/dashfabric/fm/pkg/observability"
)

// metricsRegistry is the package-level metrics registry
var metricsRegistry *obs.MetricsRegistry

// SetMetricsRegistry sets the package-level metrics registry for CM module
func SetMetricsRegistry(registry *obs.MetricsRegistry) {
	if registry != nil {
		metricsRegistry = registry
	}
}

// incrementEventsReceived increments the events received counter
func incrementEventsReceived() {
	if metricsRegistry != nil {
		metricsRegistry.IncrementCounter(metricsRegistry.CMEventsReceived)
	}
}

// incrementEventsDuplicated increments the duplicated events counter
func incrementEventsDuplicated() {
	if metricsRegistry != nil {
		metricsRegistry.IncrementCounter(metricsRegistry.CMEventsDuplicated)
	}
}

// incrementEventsForwarded increments the forwarded events counter
func incrementEventsForwarded() {
	if metricsRegistry != nil {
		metricsRegistry.IncrementCounter(metricsRegistry.CMEventsForwarded)
	}
}

// incrementValidationErrors increments the validation errors counter
func incrementValidationErrors() {
	if metricsRegistry != nil {
		metricsRegistry.IncrementCounter(metricsRegistry.CMValidationErrors)
	}
}
