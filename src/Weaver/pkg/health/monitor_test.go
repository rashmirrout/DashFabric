package health

import (
	"context"
	"testing"
	"time"

	"github.com/dashfabric/weaver/pkg/gateway"
)

// TestMonitorCreation verifies monitor creation
func TestMonitorCreation(t *testing.T) {
	replicas := []*gateway.Replica{
		{Name: "r1", Address: "localhost:5051", Healthy: true},
		{Name: "r2", Address: "localhost:5052", Healthy: true},
	}

	config := HealthConfig{
		CheckInterval:    100 * time.Millisecond,
		CheckTimeout:     50 * time.Millisecond,
		FailureThreshold: 2,
		SuccessThreshold: 1,
	}

	m := NewMonitor(replicas, config)
	if m == nil {
		t.Fatal("monitor should not be nil")
	}

	if len(m.states) != 2 {
		t.Errorf("expected 2 states, got %d", len(m.states))
	}
}

// TestHealthyState verifies healthy state
func TestHealthyState(t *testing.T) {
	replicas := []*gateway.Replica{
		{Name: "r1", Address: "localhost:5051", Healthy: true},
	}

	m := NewMonitor(replicas, HealthConfig{})

	if m.GetState("r1") != StateHealthy {
		t.Error("replica should start in healthy state")
	}

	if !m.IsHealthy("r1") {
		t.Error("IsHealthy should return true")
	}
}

// TestGetHealthyReplicas verifies getting healthy replicas
func TestGetHealthyReplicas(t *testing.T) {
	replicas := []*gateway.Replica{
		{Name: "r1", Address: "localhost:5051", Healthy: true},
		{Name: "r2", Address: "localhost:5052", Healthy: true},
		{Name: "r3", Address: "localhost:5053", Healthy: false},
	}

	m := NewMonitor(replicas, HealthConfig{})

	healthy := m.GetHealthyReplicas()
	if len(healthy) != 3 { // All start as healthy in our implementation
		t.Errorf("expected 3 healthy replicas, got %d", len(healthy))
	}
}

// TestPanicMode verifies panic mode detection
func TestPanicMode(t *testing.T) {
	replicas := []*gateway.Replica{
		{Name: "r1", Address: "localhost:5051", Healthy: true},
	}

	m := NewMonitor(replicas, HealthConfig{})

	if m.IsPanicMode() {
		t.Error("should not be in panic mode initially")
	}

	// Manually mark all replicas as unhealthy
	state := m.getState("r1")
	state.mu.Lock()
	state.state = StateUnhealthy
	state.mu.Unlock()

	m.updatePanicMode()

	if !m.IsPanicMode() {
		t.Error("should be in panic mode when all unhealthy")
	}
}

// TestMonitorStart verifies monitor can start and stop
func TestMonitorStart(t *testing.T) {
	replicas := []*gateway.Replica{
		{Name: "r1", Address: "localhost:5051", Healthy: true},
	}

	config := HealthConfig{
		CheckInterval: 100 * time.Millisecond,
	}

	m := NewMonitor(replicas, config)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	m.Start(ctx)

	// Wait for monitoring to run
	time.Sleep(150 * time.Millisecond)

	// Stop should complete without error
	err := m.Stop()
	if err != nil {
		t.Errorf("Stop failed: %v", err)
	}
}
