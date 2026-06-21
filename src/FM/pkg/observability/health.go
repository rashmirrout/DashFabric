package observability

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// HealthStatus represents the health status of a service
type HealthStatus struct {
	Status    string            `json:"status"`
	Timestamp string            `json:"timestamp"`
	Uptime    string            `json:"uptime"`
	Services  map[string]string `json:"services"`
}

// HealthChecker checks the health of FM services
type HealthChecker struct {
	startTime time.Time
	services  map[string]bool
	mu        sync.RWMutex
}

// NewHealthChecker creates a new health checker
func NewHealthChecker() *HealthChecker {
	return &HealthChecker{
		startTime: time.Now(),
		services: map[string]bool{
			"cm":  false,
			"dm":  false,
			"gm":  false,
			"dal": false,
		},
	}
}

// SetServiceStatus updates the status of a service
func (hc *HealthChecker) SetServiceStatus(service string, ready bool) {
	hc.mu.Lock()
	defer hc.mu.Unlock()
	hc.services[service] = ready
}

// IsReady returns true if all services are ready
func (hc *HealthChecker) IsReady() bool {
	hc.mu.RLock()
	defer hc.mu.RUnlock()
	for _, ready := range hc.services {
		if !ready {
			return false
		}
	}
	return true
}

// HealthzHandler returns the liveness probe handler
func (hc *HealthChecker) HealthzHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		status := HealthStatus{
			Status:    "ok",
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Uptime:    time.Since(hc.startTime).String(),
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(status)
	}
}

// ReadyzHandler returns the readiness probe handler
func (hc *HealthChecker) ReadyzHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		hc.mu.RLock()
		services := make(map[string]string)
		allReady := true
		for service, ready := range hc.services {
			if ready {
				services[service] = "ready"
			} else {
				services[service] = "not_ready"
				allReady = false
			}
		}
		hc.mu.RUnlock()

		status := HealthStatus{
			Status:    "ok",
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Uptime:    time.Since(hc.startTime).String(),
			Services:  services,
		}

		if allReady {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		json.NewEncoder(w).Encode(status)
	}
}
