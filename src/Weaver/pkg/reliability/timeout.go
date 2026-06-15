package reliability

import (
	"context"
	"time"
)

type TimeoutConfig struct {
	GlobalTimeout  time.Duration
	ReplicaTimeout time.Duration
	ConnectTimeout time.Duration
	ReadTimeout    time.Duration
}

type TimeoutManager struct {
	config TimeoutConfig
}

func NewTimeoutManager(config TimeoutConfig) *TimeoutManager {
	if config.GlobalTimeout == 0 {
		config.GlobalTimeout = 15 * time.Second
	}
	if config.ReplicaTimeout == 0 {
		config.ReplicaTimeout = 5 * time.Second
	}
	if config.ConnectTimeout == 0 {
		config.ConnectTimeout = 1 * time.Second
	}
	if config.ReadTimeout == 0 {
		config.ReadTimeout = 4 * time.Second
	}

	return &TimeoutManager{config: config}
}

// CalculateAttemptTimeout returns safe timeout for attempt N
func (tm *TimeoutManager) CalculateAttemptTimeout(
	globalDeadline time.Time,
	maxAttempts int,
	currentAttempt int,
) time.Duration {
	timeRemaining := time.Until(globalDeadline)
	if timeRemaining <= 0 {
		return 0
	}

	// Divide remaining time across remaining attempts, minus overhead
	attemptsLeft := maxAttempts - currentAttempt + 1
	overhead := 50 * time.Millisecond
	perAttempt := (timeRemaining / time.Duration(attemptsLeft)) - overhead

	// Never exceed configured replica timeout
	if perAttempt > tm.config.ReplicaTimeout {
		perAttempt = tm.config.ReplicaTimeout
	}

	// Never go negative
	if perAttempt < 0 {
		perAttempt = 0
	}

	return perAttempt
}

// CreateRequestContext creates context with proper deadline
func (tm *TimeoutManager) CreateRequestContext(
	parent context.Context,
	clientTimeoutMs int64,
) (context.Context, context.CancelFunc) {
	timeout := tm.config.GlobalTimeout
	if clientTimeoutMs > 0 {
		timeout = time.Duration(clientTimeoutMs) * time.Millisecond
	}

	return context.WithTimeout(parent, timeout)
}

// CreateAttemptContext creates context for single attempt
func (tm *TimeoutManager) CreateAttemptContext(
	requestCtx context.Context,
	maxAttempts int,
	currentAttempt int,
) (context.Context, context.CancelFunc) {
	deadline, ok := requestCtx.Deadline()
	if !ok {
		deadline = time.Now().Add(tm.config.GlobalTimeout)
	}

	attemptTimeout := tm.CalculateAttemptTimeout(deadline, maxAttempts, currentAttempt)

	attemptDeadline := time.Now().Add(attemptTimeout)
	if attemptDeadline.After(deadline) {
		attemptDeadline = deadline
	}

	return context.WithDeadline(requestCtx, attemptDeadline)
}

// GetGlobalTimeout returns configured global timeout
func (tm *TimeoutManager) GetGlobalTimeout() time.Duration {
	return tm.config.GlobalTimeout
}

// GetReplicaTimeout returns configured per-replica timeout
func (tm *TimeoutManager) GetReplicaTimeout() time.Duration {
	return tm.config.ReplicaTimeout
}
