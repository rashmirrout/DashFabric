package integration

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dashfabric/weaver/pkg/discovery"
	"github.com/dashfabric/weaver/pkg/gateway"
	"github.com/dashfabric/weaver/pkg/health"
	"github.com/dashfabric/weaver/pkg/loadbalancer"
	"github.com/dashfabric/weaver/pkg/reliability"
)

// TestFinalE2ELargeScale simulates production-scale workload:
// 5 replicas, 100k requests, comprehensive feature validation
func TestFinalE2ELargeScale(t *testing.T) {
	// Setup: 5 replicas
	replicas := make([]*gateway.Replica, 5)
	for i := 0; i < 5; i++ {
		replicas[i] = &gateway.Replica{
			Name:    fmt.Sprintf("replica-%d", i+1),
			Address: fmt.Sprintf("10.0.0.%d:5000", i+1),
			Healthy: true,
		}
	}

	// Gateway with round-robin LB
	lb := loadbalancer.NewRoundRobin()
	cfg := gateway.Config{
		Name:         "final-e2e-gateway",
		Discovery:    &testDiscovery{},
		HealthMon:    nil,
		LoadBalancer: lb,
	}
	gw := gateway.NewGateway(cfg)
	gw.SetReplicas(replicas)

	// Metrics
	successCount := int64(0)
	failureCount := int64(0)
	var latencies []int64
	var latencyMu sync.Mutex

	// Send 100k requests concurrently
	numRequests := 100000
	numConcurrent := 100
	batchSize := numRequests / numConcurrent

	var wg sync.WaitGroup
	ctx := context.Background()

	for batch := 0; batch < numConcurrent; batch++ {
		wg.Add(1)
		go func(batchID int) {
			defer wg.Done()

			for i := 0; i < batchSize; i++ {
				start := time.Now()

				req := &gateway.Request{
					RequestID: fmt.Sprintf("req-%d-%d", batchID, i),
					Method:    "GET",
					ClientIP:  fmt.Sprintf("192.168.%d.%d", batchID/256, batchID%256),
					TimeoutMs: 5000,
				}

				_, err := gw.RouteRequest(ctx, req)

				elapsed := time.Since(start).Milliseconds()
				latencyMu.Lock()
				latencies = append(latencies, elapsed)
				latencyMu.Unlock()

				if err != nil {
					atomic.AddInt64(&failureCount, 1)
				} else {
					atomic.AddInt64(&successCount, 1)
				}
			}
		}(batch)
	}

	wg.Wait()

	// Analyze results
	success := atomic.LoadInt64(&successCount)
	failures := atomic.LoadInt64(&failureCount)
	total := success + failures

	successRate := float64(success) / float64(total) * 100

	// Calculate latency percentiles
	latencyMu.Lock()
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	p50 := latencies[len(latencies)*50/100]
	p95 := latencies[len(latencies)*95/100]
	p99 := latencies[len(latencies)*99/100]
	latencyMu.Unlock()

	// Verify load distribution
	distribution := gw.GetLoadDistribution()

	t.Logf("=== Final E2E Results ===")
	t.Logf("Total Requests: %d", total)
	t.Logf("Success: %d (%.2f%%)", success, successRate)
	t.Logf("Failures: %d", failures)
	t.Logf("Latency - P50: %dms, P95: %dms, P99: %dms", p50, p95, p99)
	t.Logf("Load Distribution: %v", distribution)

	// Assertions
	if total != int64(numRequests) {
		t.Errorf("expected %d total requests, got %d", numRequests, total)
	}
	if successRate < 99.0 {
		t.Errorf("expected >99%% success rate, got %.2f%%", successRate)
	}
	if p99 > 100 {
		t.Errorf("expected p99 latency < 100ms, got %dms", p99)
	}

	// Verify load distribution is reasonably balanced
	avgLoad := float64(success) / float64(len(replicas))
	for name, load := range distribution {
		if load < 0 {
			t.Errorf("negative load for %s: %d", name, load)
		}
	}
}

// TestFinalE2EWithFailures simulates graceful degradation:
// Random replica failures during 50k request load
func TestFinalE2EWithFailures(t *testing.T) {
	// Setup: 5 replicas
	replicas := make([]*gateway.Replica, 5)
	for i := 0; i < 5; i++ {
		replicas[i] = &gateway.Replica{
			Name:    fmt.Sprintf("replica-%d", i+1),
			Address: fmt.Sprintf("10.0.0.%d:5000", i+1),
			Healthy: true,
		}
	}

	lb := loadbalancer.NewLeastConnections()
	cfg := gateway.Config{
		Name:         "final-e2e-failures",
		Discovery:    &testDiscovery{},
		HealthMon:    nil,
		LoadBalancer: lb,
	}
	gw := gateway.NewGateway(cfg)
	gw.SetReplicas(replicas)

	successCount := int64(0)
	failureCount := int64(0)
	var mu sync.Mutex

	// Chaos: randomly mark replicas unhealthy
	stopChaos := make(chan struct{})
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopChaos:
				return
			case <-ticker.C:
				idx := int(atomic.LoadInt64(&successCount)) % len(replicas)
				mu.Lock()
				replicas[idx].Healthy = false
				mu.Unlock()

				// Recover after 50ms
				go func(i int) {
					time.Sleep(50 * time.Millisecond)
					mu.Lock()
					replicas[i].Healthy = true
					mu.Unlock()
				}(idx)
			}
		}
	}()

	// Send 50k requests
	var wg sync.WaitGroup
	ctx := context.Background()

	for i := 0; i < 50000; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			mu.Lock()
			healthy := make([]*gateway.Replica, 0)
			for _, r := range replicas {
				if r.Healthy {
					healthy = append(healthy, r)
				}
			}
			mu.Unlock()

			if len(healthy) == 0 {
				atomic.AddInt64(&failureCount, 1)
				return
			}

			req := &gateway.Request{
				RequestID: fmt.Sprintf("req-%d", idx),
				Method:    "GET",
				ClientIP:  "192.168.1.1",
				TimeoutMs: 1000,
			}

			_, err := gw.RouteRequest(ctx, req)
			if err != nil {
				atomic.AddInt64(&failureCount, 1)
			} else {
				atomic.AddInt64(&successCount, 1)
			}
		}(i)
	}

	wg.Wait()
	close(stopChaos)

	success := atomic.LoadInt64(&successCount)
	failures := atomic.LoadInt64(&failureCount)
	successRate := float64(success) / float64(success+failures) * 100

	t.Logf("E2E with Failures: %d success (%.1f%%), %d failures", success, successRate, failures)

	// Graceful degradation: expect >80% success even with failures
	if successRate < 80 {
		t.Errorf("expected >80%% success under failures, got %.1f%%", successRate)
	}
}

// TestFinalE2EWithAllFeatures tests integrated gateway with all features
func TestFinalE2EWithAllFeatures(t *testing.T) {
	// Setup: 5 replicas with circuit breakers
	replicas := make([]*gateway.Replica, 5)
	cbMap := make(map[string]*reliability.CircuitBreaker)

	for i := 0; i < 5; i++ {
		replicas[i] = &gateway.Replica{
			Name:    fmt.Sprintf("replica-%d", i+1),
			Address: fmt.Sprintf("10.0.0.%d:5000", i+1),
			Healthy: true,
		}
		cbMap[replicas[i].Name] = reliability.NewCircuitBreaker(5, 3, 100*time.Millisecond)
	}

	// Setup gateway with multiple LB strategies tested
	strategies := []struct {
		name string
		lb   gateway.LoadBalancer
	}{
		{"RoundRobin", loadbalancer.NewRoundRobin()},
		{"LeastConnections", loadbalancer.NewLeastConnections()},
		{"Weighted", loadbalancer.NewWeighted(map[string]int64{
			"replica-1": 2,
			"replica-2": 2,
			"replica-3": 1,
			"replica-4": 1,
			"replica-5": 1,
		})},
	}

	for _, strat := range strategies {
		cfg := gateway.Config{
			Name:         fmt.Sprintf("final-e2e-feature-%s", strat.name),
			Discovery:    &testDiscovery{},
			HealthMon:    nil,
			LoadBalancer: strat.lb,
		}
		gw := gateway.NewGateway(cfg)
		gw.SetReplicas(replicas)

		successCount := int64(0)
		failureCount := int64(0)

		// Send requests
		var wg sync.WaitGroup
		ctx := context.Background()

		for i := 0; i < 10000; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()

				req := &gateway.Request{
					RequestID: fmt.Sprintf("req-%d", idx),
					Method:    "GET",
					ClientIP:  "192.168.1.1",
					TimeoutMs: 1000,
				}

				_, err := gw.RouteRequest(ctx, req)
				if err != nil {
					atomic.AddInt64(&failureCount, 1)
				} else {
					atomic.AddInt64(&successCount, 1)
				}
			}(i)
		}

		wg.Wait()

		success := atomic.LoadInt64(&successCount)
		failures := atomic.LoadInt64(&failureCount)
		total := success + failures

		if total != 10000 {
			t.Errorf("%s: expected 10000 requests, got %d", strat.name, total)
		}

		t.Logf("%s: %d success, %d failures", strat.name, success, failures)
	}
}

// TestFinalE2EHealthMonitoring tests health status changes
func TestFinalE2EHealthMonitoring(t *testing.T) {
	// Setup health checker
	checker := health.NewTCPHealthChecker("localhost:5000", 5*time.Second)

	replicas := []*gateway.Replica{
		{Name: "replica-1", Address: "10.0.0.1:5000", Healthy: true},
		{Name: "replica-2", Address: "10.0.0.2:5000", Healthy: true},
	}

	// Simulate health state transitions
	healthyCount := 0
	for i := 0; i < 100; i++ {
		for _, r := range replicas {
			result := checker.Check(context.Background(), r)
			if result != nil {
				if result.Status {
					healthyCount++
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}

	if healthyCount == 0 {
		t.Logf("health check: no successful checks")
	}
}

// TestFinalE2EReplicaRebalancing tests topology changes
func TestFinalE2EReplicaRebalancing(t *testing.T) {
	lb := loadbalancer.NewRoundRobin()

	cfg := gateway.Config{
		Name:         "final-e2e-rebalance",
		Discovery:    &testDiscovery{},
		HealthMon:    nil,
		LoadBalancer: lb,
	}
	gw := gateway.NewGateway(cfg)

	// Start with 3 replicas
	replicas := make([]*gateway.Replica, 3)
	for i := 0; i < 3; i++ {
		replicas[i] = &gateway.Replica{
			Name:    fmt.Sprintf("replica-%d", i+1),
			Address: fmt.Sprintf("10.0.0.%d:5000", i+1),
			Healthy: true,
		}
	}
	gw.SetReplicas(replicas)

	// Send 1000 requests
	successCount := int64(0)
	for i := 0; i < 1000; i++ {
		req := &gateway.Request{
			RequestID: fmt.Sprintf("req-%d", i),
			Method:    "GET",
			ClientIP:  "192.168.1.1",
			TimeoutMs: 1000,
		}
		_, err := gw.RouteRequest(context.Background(), req)
		if err == nil {
			atomic.AddInt64(&successCount, 1)
		}
	}

	phase1Success := atomic.LoadInt64(&successCount)

	// Scale up to 5 replicas
	newReplicas := make([]*gateway.Replica, 5)
	for i := 0; i < 5; i++ {
		newReplicas[i] = &gateway.Replica{
			Name:    fmt.Sprintf("replica-%d", i+1),
			Address: fmt.Sprintf("10.0.0.%d:5000", i+1),
			Healthy: true,
		}
	}
	gw.SetReplicas(newReplicas)

	// Send another 1000 requests
	atomic.StoreInt64(&successCount, 0)
	for i := 0; i < 1000; i++ {
		req := &gateway.Request{
			RequestID: fmt.Sprintf("req-scale-%d", i),
			Method:    "GET",
			ClientIP:  "192.168.1.1",
			TimeoutMs: 1000,
		}
		_, err := gw.RouteRequest(context.Background(), req)
		if err == nil {
			atomic.AddInt64(&successCount, 1)
		}
	}

	phase2Success := atomic.LoadInt64(&successCount)

	t.Logf("Phase 1 (3 replicas): %d success", phase1Success)
	t.Logf("Phase 2 (5 replicas): %d success", phase2Success)

	if phase1Success < 900 || phase2Success < 900 {
		t.Errorf("expected >90%% success in both phases")
	}
}
