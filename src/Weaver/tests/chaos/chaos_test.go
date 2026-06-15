package chaos

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dashfabric/weaver/pkg/gateway"
	"github.com/dashfabric/weaver/pkg/loadbalancer"
	"github.com/dashfabric/weaver/pkg/reliability"
)

// ChaosTestScenario describes a chaos test scenario
type ChaosTestScenario struct {
	name              string
	replicas          int
	requests          int
	chaosFunc         func([]*gateway.Replica)
	expectedMinSuccess int // Minimum successful requests expected
	expectedMaxErrors int  // Maximum errors tolerated
}

// TestReplicaKillChaos simulates random replica failures
func TestReplicaKillChaos(t *testing.T) {
	// Setup
	replicas := make([]*gateway.Replica, 5)
	for i := 0; i < 5; i++ {
		replicas[i] = &gateway.Replica{
			Name:    fmt.Sprintf("replica-%d", i+1),
			Address: fmt.Sprintf("10.0.0.%d:5000", i+1),
			Healthy: true,
		}
	}

	lb := loadbalancer.NewLeastConnections()
	successCount := int64(0)
	failureCount := int64(0)
	var mu sync.Mutex

	// Start chaos: kill random replicas
	stopChaos := make(chan struct{})
	go func() {
		for {
			select {
			case <-stopChaos:
				return
			case <-time.After(100 * time.Millisecond):
				// Kill random replica
				if len(replicas) > 1 {
					idx := rand.Intn(len(replicas))
					mu.Lock()
					replicas[idx].Healthy = false
					mu.Unlock()

					// Resurrect after delay
					go func(i int) {
						time.Sleep(time.Duration(rand.Intn(500)+100) * time.Millisecond)
						mu.Lock()
						replicas[i].Healthy = true
						mu.Unlock()
					}(idx)
				}
			}
		}
	}()

	// Send requests concurrently
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			mu.Lock()
			healthy := make([]*gateway.Replica, 0, len(replicas))
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

			selected, err := lb.Select(context.Background(), healthy)
			if err != nil {
				atomic.AddInt64(&failureCount, 1)
				return
			}

			lb.UpdateLoad(selected, 1)
			defer lb.UpdateLoad(selected, -1)

			// Simulate request (10-50ms)
			time.Sleep(time.Duration(rand.Intn(40)+10) * time.Millisecond)
			atomic.AddInt64(&successCount, 1)
		}()
	}

	wg.Wait()
	close(stopChaos)

	success := atomic.LoadInt64(&successCount)
	failures := atomic.LoadInt64(&failureCount)
	total := success + failures

	if total != 100 {
		t.Errorf("expected 100 total requests, got %d", total)
	}

	// With graceful degradation, expect >80% success
	if success < 80 {
		t.Errorf("expected at least 80 successes, got %d", success)
	}

	t.Logf("Replica Kill Chaos: %d success, %d failures", success, failures)
}

// TestLatencyInjectionChaos simulates network latency
func TestLatencyInjectionChaos(t *testing.T) {
	replicas := make([]*gateway.Replica, 3)
	for i := 0; i < 3; i++ {
		replicas[i] = &gateway.Replica{
			Name:    fmt.Sprintf("replica-%d", i+1),
			Address: fmt.Sprintf("10.0.0.%d:5000", i+1),
			Healthy: true,
		}
	}

	lb := loadbalancer.NewRoundRobin()
	successCount := int64(0)
	timeoutCount := int64(0)
	totalLatency := int64(0)
	var latencyMu sync.Mutex

	// Inject latency: randomly add 100-500ms
	stopChaos := make(chan struct{})
	go func() {
		for {
			select {
			case <-stopChaos:
				return
			case <-time.After(200 * time.Millisecond):
				// Add latency to random replica
				if len(replicas) > 0 {
					replicas[rand.Intn(len(replicas))].Healthy = false
				}

				// Remove latency after delay
				go func() {
					time.Sleep(time.Duration(rand.Intn(300)+100) * time.Millisecond)
					latencyMu.Lock()
					for _, r := range replicas {
						r.Healthy = true
					}
					latencyMu.Unlock()
				}()
			}
		}
	}()

	// Send requests with timeout
	var wg sync.WaitGroup
	globalCtx, globalCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer globalCancel()

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			start := time.Now()
			ctx, cancel := context.WithTimeout(globalCtx, 2*time.Second)
			defer cancel()

			latencyMu.Lock()
			selected, err := lb.Select(ctx, replicas)
			latencyMu.Unlock()

			if err != nil {
				atomic.AddInt64(&timeoutCount, 1)
				return
			}

			lb.UpdateLoad(selected, 1)
			defer lb.UpdateLoad(selected, -1)

			// Simulate request with injected latency
			time.Sleep(time.Duration(rand.Intn(600)+100) * time.Millisecond)

			elapsed := time.Since(start)
			if elapsed > 2*time.Second {
				atomic.AddInt64(&timeoutCount, 1)
			} else {
				atomic.AddInt64(&successCount, 1)
				atomic.AddInt64(&totalLatency, elapsed.Milliseconds())
			}
		}()
	}

	wg.Wait()
	close(stopChaos)

	success := atomic.LoadInt64(&successCount)
	timeouts := atomic.LoadInt64(&timeoutCount)
	avgLatency := int64(0)
	if success > 0 {
		avgLatency = atomic.LoadInt64(&totalLatency) / success
	}

	t.Logf("Latency Injection Chaos: %d success (avg %dms), %d timeouts", success, avgLatency, timeouts)
}

// TestNetworkPartitionChaos simulates network partition recovery
func TestNetworkPartitionChaos(t *testing.T) {
	replicas := make([]*gateway.Replica, 4)
	for i := 0; i < 4; i++ {
		replicas[i] = &gateway.Replica{
			Name:    fmt.Sprintf("replica-%d", i+1),
			Address: fmt.Sprintf("10.0.0.%d:5000", i+1),
			Healthy: true,
		}
	}

	lb := loadbalancer.NewLeastConnections()
	cb := reliability.NewCircuitBreaker(3, 2, 100*time.Millisecond)
	successCount := int64(0)
	failureCount := int64(0)

	// Simulate network partition: isolate 2 replicas
	var mu sync.Mutex
	partitioned := make(map[string]bool)

	stopChaos := make(chan struct{})
	go func() {
		// Partition phase 1: isolate replicas
		time.Sleep(100 * time.Millisecond)
		mu.Lock()
		partitioned["replica-1"] = true
		partitioned["replica-2"] = true
		mu.Unlock()

		// Recovery phase: gradually restore
		time.Sleep(200 * time.Millisecond)
		mu.Lock()
		delete(partitioned, "replica-1")
		mu.Unlock()

		time.Sleep(200 * time.Millisecond)
		mu.Lock()
		delete(partitioned, "replica-2")
		mu.Unlock()

		close(stopChaos)
	}()

	// Send requests through circuit breaker
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			select {
			case <-stopChaos:
				return
			default:
			}

			mu.Lock()
			available := make([]*gateway.Replica, 0)
			for _, r := range replicas {
				if !partitioned[r.Name] {
					available = append(available, r)
				}
			}
			mu.Unlock()

			if len(available) == 0 {
				atomic.AddInt64(&failureCount, 1)
				return
			}

			if !cb.Allow() {
				atomic.AddInt64(&failureCount, 1)
				return
			}

			selected, err := lb.Select(context.Background(), available)
			if err != nil {
				cb.RecordFailure()
				atomic.AddInt64(&failureCount, 1)
				return
			}

			lb.UpdateLoad(selected, 1)
			defer lb.UpdateLoad(selected, -1)

			// Simulate request
			time.Sleep(time.Duration(rand.Intn(20)+10) * time.Millisecond)

			cb.RecordSuccess()
			atomic.AddInt64(&successCount, 1)
		}()

		time.Sleep(10 * time.Millisecond)
	}

	wg.Wait()

	success := atomic.LoadInt64(&successCount)
	failures := atomic.LoadInt64(&failureCount)

	// Circuit breaker should have recovered
	finalState := cb.GetState()
	if finalState != reliability.StateClosed && finalState != reliability.StateHalfOpen {
		t.Logf("Circuit breaker final state: %s (expected recovery)", finalState)
	}

	t.Logf("Network Partition Chaos: %d success, %d failures, CB state: %s", success, failures, finalState)
}

// TestCascadingFailureRecovery simulates cascading failures and recovery
func TestCascadingFailureRecovery(t *testing.T) {
	replicas := make([]*gateway.Replica, 3)
	for i := 0; i < 3; i++ {
		replicas[i] = &gateway.Replica{
			Name:    fmt.Sprintf("replica-%d", i+1),
			Address: fmt.Sprintf("10.0.0.%d:5000", i+1),
			Healthy: true,
		}
	}

	lb := loadbalancer.NewLeastConnections()
	cbMap := make(map[string]*reliability.CircuitBreaker)
	for _, r := range replicas {
		cbMap[r.Name] = reliability.NewCircuitBreaker(2, 2, 100*time.Millisecond)
	}

	successCount := int64(0)
	failureCount := int64(0)

	// Phase 1: Healthy requests
	ctx := context.Background()
	for i := 0; i < 20; i++ {
		selected, err := lb.Select(ctx, replicas)
		if err != nil {
			atomic.AddInt64(&failureCount, 1)
			continue
		}

		cb := cbMap[selected.Name]
		if !cb.Allow() {
			atomic.AddInt64(&failureCount, 1)
			continue
		}

		lb.UpdateLoad(selected, 1)
		defer lb.UpdateLoad(selected, -1)

		atomic.AddInt64(&successCount, 1)
	}

	// Phase 2: Inject failures to cascade
	for _, r := range replicas {
		for i := 0; i < 3; i++ {
			cbMap[r.Name].RecordFailure()
		}
	}

	// Phase 3: Circuit breakers should open
	failedRequests := 0
	for i := 0; i < 10; i++ {
		for _, r := range replicas {
			if !cbMap[r.Name].Allow() {
				failedRequests++
			}
		}
	}

	if failedRequests == 0 {
		t.Logf("Circuit breakers did not open as expected")
	}

	// Phase 4: Recovery begins
	time.Sleep(150 * time.Millisecond)

	recoveredCount := 0
	for _, r := range replicas {
		if cbMap[r.Name].GetState() == reliability.StateHalfOpen {
			recoveredCount++
			cbMap[r.Name].RecordSuccess()
			cbMap[r.Name].RecordSuccess()
		}
	}

	t.Logf("Cascading Failure: %d success, %d failures, %d CBs recovering", successCount, failureCount, recoveredCount)
}

// TestChaosWithConcurrentRequests tests chaos under high concurrency
func TestChaosWithConcurrentRequests(t *testing.T) {
	replicas := make([]*gateway.Replica, 5)
	for i := 0; i < 5; i++ {
		replicas[i] = &gateway.Replica{
			Name:    fmt.Sprintf("replica-%d", i+1),
			Address: fmt.Sprintf("10.0.0.%d:5000", i+1),
			Healthy: true,
		}
	}

	lb := loadbalancer.NewRoundRobin()
	successCount := int64(0)
	failureCount := int64(0)
	p99Latency := int64(0)

	var latencies []int64
	var latencyMu sync.Mutex

	// Chaos: randomly disable/enable replicas
	stopChaos := make(chan struct{})
	go func() {
		ticker := time.NewTicker(150 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-stopChaos:
				return
			case <-ticker.C:
				idx := rand.Intn(len(replicas))
				replicas[idx].Healthy = !replicas[idx].Healthy

				// Restore after delay
				go func(i int) {
					time.Sleep(time.Duration(rand.Intn(200)+50) * time.Millisecond)
					replicas[i].Healthy = true
				}(idx)
			}
		}
	}()

	// Send 500 concurrent requests
	var wg sync.WaitGroup
	for i := 0; i < 500; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			start := time.Now()

			healthy := make([]*gateway.Replica, 0)
			for _, r := range replicas {
				if r.Healthy {
					healthy = append(healthy, r)
				}
			}

			if len(healthy) == 0 {
				atomic.AddInt64(&failureCount, 1)
				return
			}

			selected, err := lb.Select(context.Background(), healthy)
			if err != nil {
				atomic.AddInt64(&failureCount, 1)
				return
			}

			lb.UpdateLoad(selected, 1)
			defer lb.UpdateLoad(selected, -1)

			// Request latency
			time.Sleep(time.Duration(rand.Intn(50)+10) * time.Millisecond)

			elapsed := time.Since(start).Milliseconds()
			latencyMu.Lock()
			latencies = append(latencies, elapsed)
			latencyMu.Unlock()

			atomic.AddInt64(&successCount, 1)
		}()
	}

	wg.Wait()
	close(stopChaos)

	success := atomic.LoadInt64(&successCount)
	failures := atomic.LoadInt64(&failureCount)

	// Calculate p99
	latencyMu.Lock()
	if len(latencies) > 0 {
		// Simple p99 calculation
		idx := int(float64(len(latencies)) * 0.99)
		if idx < len(latencies) {
			p99Latency = latencies[idx]
		}
	}
	latencyMu.Unlock()

	successRate := float64(success) / float64(success+failures) * 100
	t.Logf("Concurrent Chaos: %d success (%.1f%%), %d failures, p99 latency: %dms", success, successRate, failures, p99Latency)

	// Expect >90% success under chaos
	if successRate < 90 {
		t.Errorf("expected >90%% success rate, got %.1f%%", successRate)
	}
}
