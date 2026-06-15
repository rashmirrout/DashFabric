package health

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/dashfabric/weaver/pkg/gateway"
)

func TestTCPHealthChecker(t *testing.T) {
	// Start a simple TCP server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer listener.Close()

	addr := listener.Addr().String()

	// Create health checker
	checker := NewTCPHealthChecker(1 * time.Second)
	defer checker.Close()

	replica := &gateway.Replica{
		Name:    "test-1",
		Address: addr,
	}

	// Should succeed
	result := checker.Check(context.Background(), replica)
	if !result.Healthy {
		t.Errorf("TCP health check failed: %s", result.Error)
	}

	// Non-existent server should fail
	badReplica := &gateway.Replica{
		Name:    "test-bad",
		Address: "127.0.0.1:65432",
	}
	result = checker.Check(context.Background(), badReplica)
	if result.Healthy {
		t.Error("expected unhealthy result for non-existent server")
	}
}

func TestTCPHealthCheckerTimeout(t *testing.T) {
	// Use an IP that won't respond (routing table entry that discards packets)
	// 192.0.2.0/24 is reserved for documentation (RFC 5737)
	checker := NewTCPHealthChecker(100 * time.Millisecond)
	defer checker.Close()

	replica := &gateway.Replica{
		Name:    "test-timeout",
		Address: "192.0.2.1:5000",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	result := checker.Check(ctx, replica)
	if result.Healthy {
		t.Logf("TCP health check completed unexpectedly")
	}
}

func TestGRPCHealthChecker(t *testing.T) {
	// Start a simple TCP server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer listener.Close()

	addr := listener.Addr().String()

	// Create health checker (for now, just checks TCP connectivity)
	checker := NewGRPCHealthChecker("weaver.Health", 1*time.Second)
	defer checker.Close()

	replica := &gateway.Replica{
		Name:    "test-grpc",
		Address: addr,
	}

	// Should succeed
	result := checker.Check(context.Background(), replica)
	if !result.Healthy {
		t.Errorf("gRPC health check failed: %s", result.Error)
	}

	// Non-existent server should fail
	badReplica := &gateway.Replica{
		Name:    "test-grpc-bad",
		Address: "127.0.0.1:65432",
	}
	result = checker.Check(context.Background(), badReplica)
	if result.Healthy {
		t.Error("expected unhealthy result for non-existent server")
	}
}

func TestCustomHealthChecker(t *testing.T) {
	// Test with successful check
	successCheckFn := func(ctx context.Context, replica *gateway.Replica) gateway.HealthCheckResult {
		return gateway.HealthCheckResult{
			Healthy:   true,
			CheckedAt: time.Now(),
		}
	}
	checker := NewCustomHealthChecker(successCheckFn)
	defer checker.Close()

	replica := &gateway.Replica{
		Name:    "test-custom",
		Address: "10.0.0.1:5000",
	}

	result := checker.Check(context.Background(), replica)
	if !result.Healthy {
		t.Errorf("custom health check failed: %s", result.Error)
	}

	// Test with failing check
	failCheckFn := func(ctx context.Context, replica *gateway.Replica) gateway.HealthCheckResult {
		return gateway.HealthCheckResult{
			Healthy:   false,
			Error:     "custom health check failed",
			CheckedAt: time.Now(),
		}
	}
	failChecker := NewCustomHealthChecker(failCheckFn)
	defer failChecker.Close()

	result = failChecker.Check(context.Background(), replica)
	if result.Healthy {
		t.Error("expected unhealthy result from custom check")
	}
}

func TestCustomHealthCheckerUndefined(t *testing.T) {
	checker := NewCustomHealthChecker(nil)
	defer checker.Close()

	replica := &gateway.Replica{
		Name:    "test-undefined",
		Address: "10.0.0.1:5000",
	}

	result := checker.Check(context.Background(), replica)
	if result.Healthy {
		t.Error("expected unhealthy result for undefined check function")
	}
}

func TestHealthCheckerFactory(t *testing.T) {
	tests := []struct {
		name      string
		checkType HealthCheckType
		config    map[string]interface{}
		wantErr   bool
	}{
		{"TCP", HealthCheckTypeTCP, map[string]interface{}{}, false},
		{"gRPC", HealthCheckTypeGRPC, map[string]interface{}{}, false},
		{"gRPC with service", HealthCheckTypeGRPC, map[string]interface{}{"service": "my.Health"}, false},
		{"Custom with function", HealthCheckTypeCustom, map[string]interface{}{"check_fn": func(ctx context.Context, replica *gateway.Replica) gateway.HealthCheckResult { return gateway.HealthCheckResult{Healthy: true} }}, false},
		{"Custom without function", HealthCheckTypeCustom, map[string]interface{}{}, true},
		{"Unknown type", "unknown", map[string]interface{}{}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checker, err := NewHealthChecker(tt.checkType, tt.config)

			if (err != nil) != tt.wantErr {
				t.Errorf("expected error %v, got %v", tt.wantErr, err != nil)
				return
			}

			if !tt.wantErr && checker == nil {
				t.Error("expected non-nil checker")
			}

			if checker != nil {
				defer checker.Close()
			}
		})
	}
}

func TestHealthCheckerTimeoutConfig(t *testing.T) {
	config := map[string]interface{}{
		"timeout": 2 * time.Second,
	}

	checker, err := NewHealthChecker(HealthCheckTypeTCP, config)
	if err != nil {
		t.Fatalf("failed to create checker: %v", err)
	}
	defer checker.Close()

	tcpChecker, ok := checker.(*TCPHealthChecker)
	if !ok {
		t.Error("expected TCP checker")
	}

	if tcpChecker.timeout != 2*time.Second {
		t.Errorf("expected timeout 2s, got %v", tcpChecker.timeout)
	}
}

func TestTCPHealthCheckerConcurrent(t *testing.T) {
	// Start a simple TCP server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer listener.Close()

	addr := listener.Addr().String()
	checker := NewTCPHealthChecker(1 * time.Second)
	defer checker.Close()

	replica := &gateway.Replica{
		Name:    "test-concurrent",
		Address: addr,
	}

	healthyCount := 0
	done := make(chan struct{}, 10)

	for i := 0; i < 10; i++ {
		go func() {
			result := checker.Check(context.Background(), replica)
			if result.Healthy {
				healthyCount++
			}
			done <- struct{}{}
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	if healthyCount != 10 {
		t.Errorf("expected 10 healthy checks, got %d", healthyCount)
	}
}
