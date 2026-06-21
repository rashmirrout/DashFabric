package resilience

import (
	"fmt"
	"sync"
	"time"
)

// CircuitState represents the state of a circuit breaker
type CircuitState int

const (
	// StateClosed means circuit is healthy, requests pass through
	StateClosed CircuitState = iota
	// StateOpen means circuit is unhealthy, requests fail immediately
	StateOpen
	// StateHalfOpen means circuit is testing recovery
	StateHalfOpen
)

// CircuitBreaker implements the circuit breaker pattern for fault tolerance
type CircuitBreaker struct {
	mu              sync.RWMutex
	state           CircuitState
	failureCount    int
	failureThreshold int
	successCount    int
	successThreshold int
	lastFailureTime time.Time
	timeout         time.Duration
}

// NewCircuitBreaker creates a new circuit breaker with default settings
func NewCircuitBreaker(failureThreshold, successThreshold int, timeout time.Duration) *CircuitBreaker {
	if failureThreshold < 1 {
		failureThreshold = 5
	}
	if successThreshold < 1 {
		successThreshold = 2
	}
	if timeout < 1 {
		timeout = 30 * time.Second
	}

	return &CircuitBreaker{
		state:            StateClosed,
		failureThreshold: failureThreshold,
		successThreshold: successThreshold,
		timeout:          timeout,
	}
}

// Call executes function protected by circuit breaker
func (cb *CircuitBreaker) Call(fn func() error) error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	// Transition from Open to HalfOpen if timeout elapsed
	if cb.state == StateOpen && time.Since(cb.lastFailureTime) > cb.timeout {
		cb.state = StateHalfOpen
		cb.successCount = 0
	}

	// Reject requests when circuit is open
	if cb.state == StateOpen {
		return fmt.Errorf("circuit breaker open: service unavailable")
	}

	// Execute function
	err := fn()

	if err != nil {
		cb.recordFailure()
	} else {
		cb.recordSuccess()
	}

	return err
}

// recordFailure increments failure counter and opens circuit if threshold exceeded
func (cb *CircuitBreaker) recordFailure() {
	cb.lastFailureTime = time.Now()
	cb.failureCount++
	cb.successCount = 0

	if cb.failureCount >= cb.failureThreshold {
		cb.state = StateOpen
	}
}

// recordSuccess increments success counter and closes circuit if threshold exceeded
func (cb *CircuitBreaker) recordSuccess() {
	cb.failureCount = 0
	cb.successCount++

	if cb.state == StateHalfOpen && cb.successCount >= cb.successThreshold {
		cb.state = StateClosed
	}
}

// State returns current circuit state
func (cb *CircuitBreaker) State() CircuitState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// Reset manually resets circuit to closed state
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.state = StateClosed
	cb.failureCount = 0
	cb.successCount = 0
	cb.lastFailureTime = time.Time{}
}

// StateString returns human-readable state name
func (cs CircuitState) String() string {
	switch cs {
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
