package resilience

import (
	"context"
	"fmt"
	"time"
)

// TimeoutManager manages timeouts across pipeline stages
type TimeoutManager struct {
	totalTimeout  time.Duration
	stageTimeouts map[string]time.Duration
}

// NewTimeoutManager creates a new timeout manager
func NewTimeoutManager(totalTimeout time.Duration) *TimeoutManager {
	return &TimeoutManager{
		totalTimeout:  totalTimeout,
		stageTimeouts: make(map[string]time.Duration),
	}
}

// SetStageTimeout sets timeout for a specific stage
func (tm *TimeoutManager) SetStageTimeout(stage string, timeout time.Duration) {
	tm.stageTimeouts[stage] = timeout
}

// WithTimeout returns context with appropriate timeout for stage
func (tm *TimeoutManager) WithTimeout(ctx context.Context, stage string) (context.Context, context.CancelFunc) {
	timeout := tm.totalTimeout

	if stageTimeout, ok := tm.stageTimeouts[stage]; ok {
		timeout = stageTimeout
	}

	if timeout == 0 {
		timeout = 30 * time.Second
	}

	return context.WithTimeout(ctx, timeout)
}

// IsExceeded checks if total timeout has been exceeded
func (tm *TimeoutManager) IsExceeded(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}

// ErrorType categorizes error for handling decisions
type ErrorType int

const (
	// ErrorTypeTransient: temporary error, retry recommended
	ErrorTypeTransient ErrorType = iota
	// ErrorTypePermanent: permanent error, no retry
	ErrorTypePermanent
	// ErrorTypeTimeout: timeout error
	ErrorTypeTimeout
	// ErrorTypeCircuitOpen: circuit breaker open
	ErrorTypeCircuitOpen
)

// ClassifyError determines error type for resilience handling
func ClassifyError(err error) ErrorType {
	if err == nil {
		return ErrorTypePermanent
	}

	errStr := err.Error()

	// Circuit breaker errors
	if errStr == "circuit breaker open: service unavailable" {
		return ErrorTypeCircuitOpen
	}

	// Timeout errors
	if errStr == "context deadline exceeded" || errStr == "i/o timeout" {
		return ErrorTypeTimeout
	}

	// Transient error patterns (retry-safe)
	transientPatterns := []string{
		"connection refused",
		"network unreachable",
		"temporary failure",
		"temporarily unavailable",
		"deadline exceeded",
	}

	for _, pattern := range transientPatterns {
		if errStr == pattern {
			return ErrorTypeTransient
		}
	}

	// Default to permanent for unknown errors
	return ErrorTypePermanent
}

// IsRetryable determines if error should trigger retry
func IsRetryable(err error) bool {
	return ClassifyError(err) == ErrorTypeTransient
}

// ErrorCatalog provides comprehensive error definitions
type ErrorCatalog struct{}

// Comprehensive error types for all pipeline stages
var (
	// Device Programming Errors
	ErrDeviceProgrammingFailed = fmt.Errorf("device programming failed")
	ErrDeviceProgrammingTimeout = fmt.Errorf("device programming timeout")
	ErrDeviceNotReady = fmt.Errorf("device not ready")
	ErrDeviceUnreachable = fmt.Errorf("device unreachable")

	// Plugin Errors
	ErrPluginNotFound = fmt.Errorf("plugin not found")
	ErrPluginInitFailed = fmt.Errorf("plugin initialization failed")
	ErrPluginExecutionFailed = fmt.Errorf("plugin execution failed")
	ErrPluginCrashed = fmt.Errorf("plugin crashed")

	// Vendor Errors
	ErrVendorNotSupported = fmt.Errorf("vendor not supported")
	ErrVendorUnreachable = fmt.Errorf("vendor unreachable")
	ErrVendorQuotaExceeded = fmt.Errorf("vendor quota exceeded")
	ErrVendorAuthFailed = fmt.Errorf("vendor authentication failed")

	// Configuration Errors
	ErrInvalidConfiguration = fmt.Errorf("invalid configuration")
	ErrMissingConfiguration = fmt.Errorf("missing required configuration")

	// Consistency Errors
	ErrConsistencyViolation = fmt.Errorf("consistency violation detected")
	ErrConsistencyCheckFailed = fmt.Errorf("consistency check failed")

	// Resource Errors
	ErrPoolExhausted = fmt.Errorf("worker pool exhausted")
	ErrQueueFull = fmt.Errorf("job queue full")
	ErrResourceLimitExceeded = fmt.Errorf("resource limit exceeded")
)
