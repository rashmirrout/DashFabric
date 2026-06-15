package health

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/dashfabric/weaver/pkg/gateway"
)

// HealthCheckType defines different health check methods
type HealthCheckType string

const (
	HealthCheckTypeTCP   HealthCheckType = "tcp"
	HealthCheckTypeGRPC  HealthCheckType = "grpc"
	HealthCheckTypeHTTP  HealthCheckType = "http"
	HealthCheckTypeCustom HealthCheckType = "custom"
)

// TCPHealthChecker performs TCP connection health checks
type TCPHealthChecker struct {
	timeout time.Duration
}

// NewTCPHealthChecker creates a new TCP health checker
func NewTCPHealthChecker(timeout time.Duration) *TCPHealthChecker {
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	return &TCPHealthChecker{
		timeout: timeout,
	}
}

// Check performs TCP health check
func (th *TCPHealthChecker) Check(ctx context.Context, replica *gateway.Replica) gateway.HealthCheckResult {
	start := time.Now()
	result := gateway.HealthCheckResult{
		CheckedAt: start,
		Healthy:   false,
	}

	// Use the timeout from the checker if context doesn't have one
	checkCtx := ctx
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		checkCtx, cancel = context.WithTimeout(ctx, th.timeout)
		defer cancel()
	}

	dialer := &net.Dialer{
		Timeout: th.timeout,
	}

	conn, err := dialer.DialContext(checkCtx, "tcp", replica.Address)
	if err != nil {
		result.Error = fmt.Sprintf("tcp health check failed: %v", err)
		return result
	}
	defer conn.Close()

	result.Healthy = true
	result.ResponseTimeMs = time.Since(start).Milliseconds()
	return result
}

// Close closes the health checker
func (th *TCPHealthChecker) Close() error {
	return nil
}

// GRPCHealthChecker performs gRPC health.Check RPC calls
type GRPCHealthChecker struct {
	timeout time.Duration
	service string
}

// NewGRPCHealthChecker creates a new gRPC health checker
func NewGRPCHealthChecker(service string, timeout time.Duration) *GRPCHealthChecker {
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	if service == "" {
		service = "weaver.gateway.Health"
	}
	return &GRPCHealthChecker{
		timeout: timeout,
		service: service,
	}
}

// Check performs gRPC health check
func (gh *GRPCHealthChecker) Check(ctx context.Context, replica *gateway.Replica) gateway.HealthCheckResult {
	start := time.Now()
	result := gateway.HealthCheckResult{
		CheckedAt: start,
		Healthy:   false,
	}

	// Use the timeout from the checker if context doesn't have one
	checkCtx := ctx
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		checkCtx, cancel = context.WithTimeout(ctx, gh.timeout)
		defer cancel()
	}

	// TODO: Implement actual gRPC health.Check RPC call
	// For now, do basic connectivity check
	dialer := &net.Dialer{
		Timeout: gh.timeout,
	}

	conn, err := dialer.DialContext(checkCtx, "tcp", replica.Address)
	if err != nil {
		result.Error = fmt.Sprintf("grpc health check failed: %v", err)
		return result
	}
	defer conn.Close()

	result.Healthy = true
	result.ResponseTimeMs = time.Since(start).Milliseconds()
	return result
}

// Close closes the health checker
func (gh *GRPCHealthChecker) Close() error {
	return nil
}

// CustomHealthChecker allows user-defined health check logic via plugin
type CustomHealthChecker struct {
	checkFn func(ctx context.Context, replica *gateway.Replica) gateway.HealthCheckResult
}

// NewCustomHealthChecker creates a new custom health checker
func NewCustomHealthChecker(checkFn func(ctx context.Context, replica *gateway.Replica) gateway.HealthCheckResult) *CustomHealthChecker {
	return &CustomHealthChecker{
		checkFn: checkFn,
	}
}

// Check performs custom health check
func (ch *CustomHealthChecker) Check(ctx context.Context, replica *gateway.Replica) gateway.HealthCheckResult {
	if ch.checkFn == nil {
		return gateway.HealthCheckResult{
			Healthy: false,
			Error:   "custom health check function not defined",
			CheckedAt: time.Now(),
		}
	}
	return ch.checkFn(ctx, replica)
}

// Close closes the health checker
func (ch *CustomHealthChecker) Close() error {
	return nil
}

// HealthCheckerFactory creates appropriate health checker based on type
func NewHealthChecker(checkType HealthCheckType, config map[string]interface{}) (gateway.HealthChecker, error) {
	timeout := 5 * time.Second
	if t, ok := config["timeout"]; ok {
		if timeoutDuration, ok := t.(time.Duration); ok {
			timeout = timeoutDuration
		}
	}

	switch checkType {
	case HealthCheckTypeTCP:
		return NewTCPHealthChecker(timeout), nil

	case HealthCheckTypeGRPC:
		service := "weaver.gateway.Health"
		if s, ok := config["service"]; ok {
			if serviceStr, ok := s.(string); ok {
				service = serviceStr
			}
		}
		return NewGRPCHealthChecker(service, timeout), nil

	case HealthCheckTypeCustom:
		if fn, ok := config["check_fn"]; ok {
			if checkFn, ok := fn.(func(ctx context.Context, replica *gateway.Replica) gateway.HealthCheckResult); ok {
				return NewCustomHealthChecker(checkFn), nil
			}
		}
		return nil, fmt.Errorf("custom health checker requires 'check_fn' in config")

	default:
		return nil, fmt.Errorf("unknown health check type: %s", checkType)
	}
}
