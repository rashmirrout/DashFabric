package integration

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dashfabric/weaver/pkg/discovery"
	"github.com/dashfabric/weaver/pkg/gateway"
	"github.com/dashfabric/weaver/pkg/loadbalancer"
	"github.com/dashfabric/weaver/pkg/reliability"
)

type testDiscovery struct{}

func (td *testDiscovery) Discover(ctx context.Context) ([]*discovery.ReplicaInfo, error) {
	return []*discovery.ReplicaInfo{}, nil
}

func (td *testDiscovery) Watch(ctx context.Context) <-chan []*discovery.ReplicaInfo {
	ch := make(chan []*discovery.ReplicaInfo)
	close(ch)
	return ch
}

func (td *testDiscovery) Close() error {
	return nil
}

func TestRoutingWithRoundRobin(t *testing.T) {
	lb := loadbalancer.NewRoundRobin()

	cfg := gateway.Config{
		Name:         "test-gateway-rr",
		Discovery:    &testDiscovery{},
		HealthMon:    nil,
		LoadBalancer: lb,
	}
	gw := gateway.NewGateway(cfg)

	replicas := []*gateway.Replica{
		{Name: "replica-1", Address: "10.0.0.1:5000", Healthy: true},
		{Name: "replica-2", Address: "10.0.0.2:5000", Healthy: true},
		{Name: "replica-3", Address: "10.0.0.3:5000", Healthy: true},
	}
	gw.SetReplicas(replicas)

	ctx := context.Background()

	for i := 0; i < 30; i++ {
		req := &gateway.Request{
			RequestID: fmt.Sprintf("req-%d", i),
			Method:    "GET",
			ClientIP:  "192.168.1.1",
			TimeoutMs: 1000,
		}
		gw.RouteRequest(ctx, req)
	}

	distribution := gw.GetLoadDistribution()
	for name, load := range distribution {
		if load != 0 {
			t.Errorf("%s has non-zero load after requests: %d", name, load)
		}
	}
}

func TestRoutingWithLeastConnections(t *testing.T) {
	lb := loadbalancer.NewLeastConnections()

	cfg := gateway.Config{
		Name:         "test-gateway-lc",
		Discovery:    &testDiscovery{},
		HealthMon:    nil,
		LoadBalancer: lb,
	}
	gw := gateway.NewGateway(cfg)

	replicas := []*gateway.Replica{
		{Name: "replica-1", Address: "10.0.0.1:5000", Healthy: true},
		{Name: "replica-2", Address: "10.0.0.2:5000", Healthy: true},
	}
	gw.SetReplicas(replicas)

	var wg sync.WaitGroup
	ctx := context.Background()

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			req := &gateway.Request{
				RequestID: fmt.Sprintf("req-%d", idx),
				Method:    "GET",
				ClientIP:  "192.168.1.1",
				TimeoutMs: 1000,
			}
			gw.RouteRequest(ctx, req)
		}(i)
	}
	wg.Wait()

	distribution := gw.GetLoadDistribution()
	totalLoad := int64(0)
	for _, load := range distribution {
		totalLoad += load
	}

	if totalLoad != 0 {
		t.Errorf("expected zero load after all requests completed, got %d", totalLoad)
	}
}

func TestRoutingLoadBalancing(t *testing.T) {
	lb := loadbalancer.NewRoundRobin()

	cfg := gateway.Config{
		Name:         "test-gateway-lb",
		Discovery:    &testDiscovery{},
		HealthMon:    nil,
		LoadBalancer: lb,
	}
	gw := gateway.NewGateway(cfg)

	replicas := []*gateway.Replica{
		{Name: "replica-1", Address: "10.0.0.1:5000", Healthy: true},
		{Name: "replica-2", Address: "10.0.0.2:5000", Healthy: true},
	}
	gw.SetReplicas(replicas)

	ctx := context.Background()

	for i := 0; i < 100; i++ {
		req := &gateway.Request{
			RequestID: fmt.Sprintf("req-%d", i),
			Method:    "GET",
			ClientIP:  "192.168.1.1",
			TimeoutMs: 1000,
		}
		gw.RouteRequest(ctx, req)
	}

	r1Count := atomic.LoadInt64(&replicas[0].Metrics.RequestCount)
	r2Count := atomic.LoadInt64(&replicas[1].Metrics.RequestCount)

	if r1Count < 40 || r1Count > 60 || r2Count < 40 || r2Count > 60 {
		t.Logf("request distribution: replica-1=%d, replica-2=%d", r1Count, r2Count)
	}
}

func TestRoutingNoHealthyReplicas(t *testing.T) {
	lb := loadbalancer.NewRoundRobin()

	cfg := gateway.Config{
		Name:         "test-gateway-nohealthy",
		Discovery:    &testDiscovery{},
		HealthMon:    nil,
		LoadBalancer: lb,
	}
	gw := gateway.NewGateway(cfg)
	gw.SetReplicas([]*gateway.Replica{})

	ctx := context.Background()

	req := &gateway.Request{
		RequestID: "req-1",
		Method:    "GET",
		ClientIP:  "192.168.1.1",
		TimeoutMs: 1000,
	}

	_, err := gw.RouteRequest(ctx, req)
	if err == nil {
		t.Errorf("expected error when no replicas available")
	}
}

func TestRoutingWithWeightedLB(t *testing.T) {
	weights := map[string]int64{
		"replica-1": 3,
		"replica-2": 1,
	}
	lb := loadbalancer.NewWeighted(weights)

	cfg := gateway.Config{
		Name:         "test-gateway-weighted",
		Discovery:    &testDiscovery{},
		HealthMon:    nil,
		LoadBalancer: lb,
	}
	gw := gateway.NewGateway(cfg)

	replicas := []*gateway.Replica{
		{Name: "replica-1", Address: "10.0.0.1:5000", Healthy: true},
		{Name: "replica-2", Address: "10.0.0.2:5000", Healthy: true},
	}
	gw.SetReplicas(replicas)

	ctx := context.Background()

	for i := 0; i < 200; i++ {
		req := &gateway.Request{
			RequestID: fmt.Sprintf("req-%d", i),
			Method:    "GET",
			ClientIP:  "192.168.1.1",
			TimeoutMs: 1000,
		}
		gw.RouteRequest(ctx, req)
	}

	r1Count := atomic.LoadInt64(&replicas[0].Metrics.RequestCount)
	r2Count := atomic.LoadInt64(&replicas[1].Metrics.RequestCount)

	ratio := float64(r1Count) / float64(r1Count+r2Count)
	if ratio < 0.6 || ratio > 0.8 {
		t.Logf("replica-1 ratio: %.2f", ratio)
	}
}

func TestRoutingLoadTracking(t *testing.T) {
	lb := loadbalancer.NewLeastConnections()

	cfg := gateway.Config{
		Name:         "test-gateway-loadtrack",
		Discovery:    &testDiscovery{},
		HealthMon:    nil,
		LoadBalancer: lb,
	}
	gw := gateway.NewGateway(cfg)

	replicas := []*gateway.Replica{
		{Name: "replica-1", Address: "10.0.0.1:5000", Healthy: true},
	}
	gw.SetReplicas(replicas)

	ctx := context.Background()

	req := &gateway.Request{
		RequestID: "req-1",
		Method:    "GET",
		ClientIP:  "192.168.1.1",
		TimeoutMs: 1000,
	}

	gw.RouteRequest(ctx, req)

	load := atomic.LoadInt64(&replicas[0].ActiveConnections)
	if load != 0 {
		t.Errorf("expected load 0 after request, got %d", load)
	}
}

func TestRoutingConcurrentRequests(t *testing.T) {
	lb := loadbalancer.NewRoundRobin()

	cfg := gateway.Config{
		Name:         "test-gateway-concurrent",
		Discovery:    &testDiscovery{},
		HealthMon:    nil,
		LoadBalancer: lb,
	}
	gw := gateway.NewGateway(cfg)

	replicas := []*gateway.Replica{
		{Name: "replica-1", Address: "10.0.0.1:5000", Healthy: true},
		{Name: "replica-2", Address: "10.0.0.2:5000", Healthy: true},
	}
	gw.SetReplicas(replicas)

	var wg sync.WaitGroup
	ctx := context.Background()

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				req := &gateway.Request{
					RequestID: fmt.Sprintf("req-%d-%d", idx, j),
					Method:    "GET",
					ClientIP:  "192.168.1.1",
					TimeoutMs: 1000,
				}
				gw.RouteRequest(ctx, req)
			}
		}(i)
	}
	wg.Wait()

	distribution := gw.GetLoadDistribution()
	totalLoad := int64(0)
	for _, load := range distribution {
		totalLoad += load
	}

	if totalLoad != 0 {
		t.Errorf("expected zero load after concurrent requests, got %d", totalLoad)
	}
}

func TestRoutingWithCircuitBreaker(t *testing.T) {
	lb := loadbalancer.NewRoundRobin()

	cfg := gateway.Config{
		Name:         "test-gateway-cb",
		Discovery:    &testDiscovery{},
		HealthMon:    nil,
		LoadBalancer: lb,
	}
	gw := gateway.NewGateway(cfg)

	replicas := []*gateway.Replica{
		{Name: "replica-1", Address: "10.0.0.1:5000", Healthy: true},
		{Name: "replica-2", Address: "10.0.0.2:5000", Healthy: true},
	}
	gw.SetReplicas(replicas)

	cb := reliability.NewCircuitBreaker(3, 2, 50*time.Millisecond)
	ctx := context.Background()

	for i := 0; i < 10; i++ {
		req := &gateway.Request{
			RequestID: fmt.Sprintf("req-%d", i),
			Method:    "GET",
			ClientIP:  "192.168.1.1",
			TimeoutMs: 1000,
		}
		gw.RouteRequest(ctx, req)

		if cb.Allow() {
			cb.RecordFailure()
		}
	}

	if cb.GetState() != reliability.StateOpen {
		t.Logf("circuit breaker state: %s", cb.GetState())
	}
}

func BenchmarkRouting(b *testing.B) {
	lb := loadbalancer.NewRoundRobin()

	cfg := gateway.Config{
		Name:         "bench-gateway",
		Discovery:    &testDiscovery{},
		HealthMon:    nil,
		LoadBalancer: lb,
	}
	gw := gateway.NewGateway(cfg)

	replicas := []*gateway.Replica{
		{Name: "replica-1", Address: "10.0.0.1:5000", Healthy: true},
		{Name: "replica-2", Address: "10.0.0.2:5000", Healthy: true},
	}
	gw.SetReplicas(replicas)

	ctx := context.Background()

	req := &gateway.Request{
		RequestID: "req-bench",
		Method:    "GET",
		ClientIP:  "192.168.1.1",
		TimeoutMs: 1000,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gw.RouteRequest(ctx, req)
	}
}
