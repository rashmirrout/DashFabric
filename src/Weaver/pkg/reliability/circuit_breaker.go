package reliability

import (
	"sync"
	"time"
)

type CircuitBreakerState int

const (
	StateClosed CircuitBreakerState = iota
	StateOpen
	StateHalfOpen
)

func (s CircuitBreakerState) String() string {
	switch s {
	case StateClosed:
		return "CLOSED"
	case StateOpen:
		return "OPEN"
	case StateHalfOpen:
		return "HALF_OPEN"
	default:
		return "UNKNOWN"
	}
}

type CircuitBreaker struct {
	failureThreshold     int
	successThreshold     int
	timeout              time.Duration
	mu                   sync.RWMutex
	state                CircuitBreakerState
	lastFailureTime      time.Time
	consecutiveFailures  int
	consecutiveSuccesses int
	totalFailures        int64
	totalSuccesses       int64
	stateChanges         int64
}

func NewCircuitBreaker(failureThreshold, successThreshold int, timeout time.Duration) *CircuitBreaker {
	if failureThreshold == 0 {
		failureThreshold = 5
	}
	if successThreshold == 0 {
		successThreshold = 2
	}
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	return &CircuitBreaker{
		failureThreshold:     failureThreshold,
		successThreshold:     successThreshold,
		timeout:              timeout,
		state:                StateClosed,
		lastFailureTime:      time.Now(),
		consecutiveFailures:  0,
		consecutiveSuccesses: 0,
	}
}

// Allow determines if request should proceed
func (cb *CircuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case StateClosed:
		return true

	case StateOpen:
		if time.Since(cb.lastFailureTime) >= cb.timeout {
			cb.state = StateHalfOpen
			cb.consecutiveSuccesses = 0
			cb.consecutiveFailures = 0
			cb.stateChanges++
			return true
		}
		return false

	case StateHalfOpen:
		return true
	}

	return false
}

// RecordSuccess records successful request
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.totalSuccesses++
	cb.consecutiveFailures = 0

	switch cb.state {
	case StateClosed:
		// Already healthy

	case StateHalfOpen:
		cb.consecutiveSuccesses++
		if cb.consecutiveSuccesses >= cb.successThreshold {
			cb.state = StateClosed
			cb.consecutiveSuccesses = 0
			cb.stateChanges++
		}
	}
}

// RecordFailure records failed request
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.totalFailures++
	cb.lastFailureTime = time.Now()
	cb.consecutiveSuccesses = 0

	switch cb.state {
	case StateClosed:
		cb.consecutiveFailures++
		if cb.consecutiveFailures >= cb.failureThreshold {
			cb.state = StateOpen
			cb.consecutiveFailures = 0
			cb.stateChanges++
		}

	case StateHalfOpen:
		cb.state = StateOpen
		cb.consecutiveFailures = 0
		cb.stateChanges++
	}
}

// GetState returns current state
func (cb *CircuitBreaker) GetState() CircuitBreakerState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// GetMetrics returns metrics snapshot
func (cb *CircuitBreaker) GetMetrics() map[string]interface{} {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	return map[string]interface{}{
		"state":          cb.state.String(),
		"total_failures": cb.totalFailures,
		"total_successes": cb.totalSuccesses,
		"state_changes":  cb.stateChanges,
	}
}

// Reset clears state (for testing)
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.state = StateClosed
	cb.consecutiveFailures = 0
	cb.consecutiveSuccesses = 0
	cb.totalFailures = 0
	cb.totalSuccesses = 0
	cb.stateChanges = 0
	cb.lastFailureTime = time.Now()
}
