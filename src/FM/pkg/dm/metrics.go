package datamanagement

import (
	obs "github.com/dashfabric/fm/pkg/observability"
)

// metricsRegistry is the package-level metrics registry
var metricsRegistry *obs.MetricsRegistry

// SetMetricsRegistry sets the package-level metrics registry for DM module
func SetMetricsRegistry(registry *obs.MetricsRegistry) {
	if registry != nil {
		metricsRegistry = registry
	}
}

// incrementEventsProcessed increments the events processed counter
func incrementEventsProcessed() {
	if metricsRegistry != nil {
		metricsRegistry.IncrementCounter(metricsRegistry.DMEventsProcessed)
	}
}

// incrementConsistencyChecks increments the consistency checks counter
func incrementConsistencyChecks() {
	if metricsRegistry != nil {
		metricsRegistry.IncrementCounter(metricsRegistry.DMConsistencyChecks)
	}
}

// incrementConsistencyViolations increments the consistency violations counter
func incrementConsistencyViolations() {
	if metricsRegistry != nil {
		metricsRegistry.IncrementCounter(metricsRegistry.DMConsistencyViolations)
	}
}

// incrementConstructsStored increments the constructs stored counter
func incrementConstructsStored() {
	if metricsRegistry != nil {
		metricsRegistry.IncrementCounter(metricsRegistry.DMConstructsStored)
	}
}
