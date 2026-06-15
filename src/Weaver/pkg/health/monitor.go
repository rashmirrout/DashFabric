package health

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dashfabric/weaver/pkg/gateway"
)

// Monitor monitors health of replicas
type Monitor struct {
	replicas         []*gateway.Replica
	config           HealthConfig
	checkers         map[string]gateway.HealthChecker
	states           map[string]*replicaState
	globalPanicMode  bool
	globalPanicTime  time.Time
	stateChanges     chan StateChange
	mu               sync.RWMutex
	stopCh           chan struct{}
	wg               sync.WaitGroup
	lastGlobalCheck  time.Time
	consecutiveCheck int32
}

// HealthConfig holds health monitor configuration
type HealthConfig struct {
	CheckType       string        // "http", "grpc", "tcp"
	CheckInterval   time.Duration // How often to check
	CheckTimeout    time.Duration // Timeout per check
	FailureThreshold int           // Failures before UNHEALTHY
	SuccessThreshold int           // Successes before HEALTHY
	PanicTimeout    time.Duration // Time before panic mode activation
}

// replicaState tracks state of a single replica
type replicaState struct {
	name                string
	state               ReplicaHealthState
	consecutiveFailures int32
	consecutiveSuccess  int32
	lastFailureTime     time.Time
	lastCheckTime       time.Time
	mu                  sync.Mutex
}

// ReplicaHealthState represents health state
type ReplicaHealthState string

const (
	StateHealthy   ReplicaHealthState = "HEALTHY"
	StateUnhealthy ReplicaHealthState = "UNHEALTHY"
	StatePanic     ReplicaHealthState = "PANIC"
)

// StateChange represents a state change event
type StateChange struct {
	ReplicaName string
	OldState    ReplicaHealthState
	NewState    ReplicaHealthState
	Timestamp   time.Time
}

// NewMonitor creates a new health monitor
func NewMonitor(replicas []*gateway.Replica, config HealthConfig) *Monitor {
	if config.FailureThreshold <= 0 {
		config.FailureThreshold = 3
	}
	if config.SuccessThreshold <= 0 {
		config.SuccessThreshold = 2
	}
	if config.CheckInterval <= 0 {
		config.CheckInterval = 5 * time.Second
	}
	if config.CheckTimeout <= 0 {
		config.CheckTimeout = 2 * time.Second
	}
	if config.PanicTimeout <= 0 {
		config.PanicTimeout = 30 * time.Second
	}

	m := &Monitor{
		replicas:     replicas,
		config:       config,
		checkers:     make(map[string]gateway.HealthChecker),
		states:       make(map[string]*replicaState),
		stateChanges: make(chan StateChange, 10),
		stopCh:       make(chan struct{}),
	}

	// Initialize states for all replicas
	for _, replica := range replicas {
		m.states[replica.Name] = &replicaState{
			name:  replica.Name,
			state: StateHealthy,
		}
	}

	return m
}

// Start begins health monitoring
func (m *Monitor) Start(ctx context.Context) {
	m.wg.Add(len(m.replicas))

	// Start health check goroutine for each replica
	for _, replica := range m.replicas {
		go m.monitorReplica(ctx, replica)
	}
}

// monitorReplica monitors a single replica
func (m *Monitor) monitorReplica(ctx context.Context, replica *gateway.Replica) {
	defer m.wg.Done()

	ticker := time.NewTicker(m.config.CheckInterval)
	defer ticker.Stop()

	state := m.getState(replica.Name)

	for {
		select {
		case <-ctx.Done():
			return
		case <-m.stopCh:
			return
		case <-ticker.C:
			// Perform health check
			checkCtx, cancel := context.WithTimeout(ctx, m.config.CheckTimeout)
			result := m.performCheck(checkCtx, replica)
			cancel()

			// Process result and update state
			m.processCheckResult(replica, result, state)
		}
	}
}

// performCheck performs a health check on a replica
func (m *Monitor) performCheck(ctx context.Context, replica *gateway.Replica) gateway.HealthCheckResult {
	// TODO: Implement actual health checks
	// For now, replicas are always considered healthy
	return gateway.HealthCheckResult{
		Healthy:        true,
		ResponseTimeMs: 1,
		CheckedAt:      time.Now(),
	}
}

// processCheckResult updates replica state based on check result
func (m *Monitor) processCheckResult(replica *gateway.Replica, result gateway.HealthCheckResult, state *replicaState) {
	state.mu.Lock()
	defer state.mu.Unlock()

	oldState := state.state
	state.lastCheckTime = result.CheckedAt

	if result.Healthy {
		// Success - reset failures, increment successes
		atomic.StoreInt32(&state.consecutiveFailures, 0)
		atomic.AddInt32(&state.consecutiveSuccess, 1)

		successes := atomic.LoadInt32(&state.consecutiveSuccess)
		if state.state == StateUnhealthy && successes >= int32(m.config.SuccessThreshold) {
			state.state = StateHealthy
			atomic.StoreInt32(&state.consecutiveSuccess, 0)
		} else if state.state == StateHealthy {
			atomic.StoreInt32(&state.consecutiveSuccess, 0)
		}
	} else {
		// Failure - reset successes, increment failures
		atomic.StoreInt32(&state.consecutiveSuccess, 0)
		atomic.AddInt32(&state.consecutiveFailures, 1)
		state.lastFailureTime = time.Now()

		failures := atomic.LoadInt32(&state.consecutiveFailures)
		if failures >= int32(m.config.FailureThreshold) {
			state.state = StateUnhealthy
			atomic.StoreInt32(&state.consecutiveFailures, 0)
		}
	}

	// Update replica health status
	replica.Healthy = (state.state == StateHealthy)

	// Send state change notification if changed
	if oldState != state.state {
		select {
		case m.stateChanges <- StateChange{
			ReplicaName: replica.Name,
			OldState:    oldState,
			NewState:    state.state,
			Timestamp:   time.Now(),
		}:
		default:
		}
	}

	// Check for panic mode
	m.updatePanicMode()
}

// updatePanicMode checks if all replicas are unhealthy and updates panic mode
func (m *Monitor) updatePanicMode() {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Count healthy and unhealthy replicas
	healthyCount := 0
	for _, state := range m.states {
		if state.state == StateHealthy {
			healthyCount++
		}
	}

	wasPanic := m.globalPanicMode
	m.globalPanicMode = (healthyCount == 0) && len(m.states) > 0

	// If entering panic mode, record time
	if m.globalPanicMode && !wasPanic {
		m.globalPanicTime = time.Now()
	}

	// If exiting panic mode, reset time
	if !m.globalPanicMode && wasPanic {
		m.globalPanicTime = time.Time{}
	}
}

// Stop stops the health monitor
func (m *Monitor) Stop() error {
	close(m.stopCh)
	m.wg.Wait()
	return nil
}

// GetState returns current state of a replica
func (m *Monitor) GetState(replicaName string) ReplicaHealthState {
	state := m.getState(replicaName)
	if state == nil {
		return StateHealthy
	}

	state.mu.Lock()
	defer state.mu.Unlock()
	return state.state
}

// getState returns internal state (requires no locking)
func (m *Monitor) getState(replicaName string) *replicaState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.states[replicaName]
}

// IsHealthy returns true if replica is healthy
func (m *Monitor) IsHealthy(replicaName string) bool {
	return m.GetState(replicaName) == StateHealthy
}

// IsPanicMode returns true if all replicas are unhealthy
func (m *Monitor) IsPanicMode() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.globalPanicMode
}

// GetHealthyReplicas returns list of healthy replicas
func (m *Monitor) GetHealthyReplicas() []*gateway.Replica {
	healthy := make([]*gateway.Replica, 0)
	for _, replica := range m.replicas {
		if m.IsHealthy(replica.Name) {
			healthy = append(healthy, replica)
		}
	}
	return healthy
}

// GetStateChanges returns channel for state changes
func (m *Monitor) GetStateChanges() <-chan StateChange {
	return m.stateChanges
}
