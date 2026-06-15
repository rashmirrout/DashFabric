package loadbalancer

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dashfabric/weaver/pkg/gateway"
)

func TestRoundRobinSequential(t *testing.T) {
	rr := NewRoundRobin()
	replicas := []*gateway.Replica{
		{Name: "replica-1", Address: "10.0.0.1:5000"},
		{Name: "replica-2", Address: "10.0.0.2:5000"},
		{Name: "replica-3", Address: "10.0.0.3:5000"},
	}

	ctx := context.Background()

	selected1, _ := rr.Select(ctx, replicas)
	selected2, _ := rr.Select(ctx, replicas)
	selected3, _ := rr.Select(ctx, replicas)
	selected4, _ := rr.Select(ctx, replicas)

	if selected1.Name != "replica-1" {
		t.Errorf("expected replica-1, got %s", selected1.Name)
	}
	if selected2.Name != "replica-2" {
		t.Errorf("expected replica-2, got %s", selected2.Name)
	}
	if selected3.Name != "replica-3" {
		t.Errorf("expected replica-3, got %s", selected3.Name)
	}
	if selected4.Name != "replica-1" {
		t.Errorf("expected replica-1 (wrap), got %s", selected4.Name)
	}
}

func TestRoundRobinDistribution(t *testing.T) {
	rr := NewRoundRobin()
	replicas := []*gateway.Replica{
		{Name: "replica-1", Address: "10.0.0.1:5000"},
		{Name: "replica-2", Address: "10.0.0.2:5000"},
	}

	ctx := context.Background()
	counts := make(map[string]int)

	for i := 0; i < 1000; i++ {
		replica, _ := rr.Select(ctx, replicas)
		counts[replica.Name]++
	}

	for name, count := range counts {
		if count < 450 || count > 550 {
			t.Errorf("%s got %d selections, expected ~500", name, count)
		}
	}
}

func TestRoundRobinNoReplicas(t *testing.T) {
	rr := NewRoundRobin()
	ctx := context.Background()

	_, err := rr.Select(ctx, []*gateway.Replica{})
	if err == nil {
		t.Errorf("expected error for no replicas")
	}
}

func TestLeastConnectionsSelection(t *testing.T) {
	lc := NewLeastConnections()
	replicas := []*gateway.Replica{
		{Name: "replica-1", Address: "10.0.0.1:5000"},
		{Name: "replica-2", Address: "10.0.0.2:5000"},
		{Name: "replica-3", Address: "10.0.0.3:5000"},
	}

	ctx := context.Background()

	atomic.StoreInt64(&replicas[0].ActiveConnections, 5)
	atomic.StoreInt64(&replicas[1].ActiveConnections, 2)
	atomic.StoreInt64(&replicas[2].ActiveConnections, 10)

	selected, _ := lc.Select(ctx, replicas)

	if selected.Name != "replica-2" {
		t.Errorf("expected replica-2 (least connections), got %s", selected.Name)
	}
}

func TestLeastConnectionsBalancing(t *testing.T) {
	lc := NewLeastConnections()
	replicas := []*gateway.Replica{
		{Name: "replica-1", Address: "10.0.0.1:5000"},
		{Name: "replica-2", Address: "10.0.0.2:5000"},
	}

	ctx := context.Background()
	counts := make(map[string]int)

	for i := 0; i < 100; i++ {
		selected, _ := lc.Select(ctx, replicas)
		counts[selected.Name]++

		lc.UpdateLoad(selected, 1)
	}

	for name, count := range counts {
		if count < 40 || count > 60 {
			t.Errorf("%s got %d selections, expected ~50", name, count)
		}
	}
}

func TestWeightedSelection(t *testing.T) {
	weights := map[string]int64{
		"replica-1": 3,
		"replica-2": 1,
	}
	w := NewWeighted(weights)

	replicas := []*gateway.Replica{
		{Name: "replica-1", Address: "10.0.0.1:5000"},
		{Name: "replica-2", Address: "10.0.0.2:5000"},
	}

	ctx := context.Background()
	counts := make(map[string]int)

	for i := 0; i < 400; i++ {
		selected, _ := w.Select(ctx, replicas)
		counts[selected.Name]++
	}

	r1Ratio := float64(counts["replica-1"]) / 400.0
	if r1Ratio < 0.6 || r1Ratio > 0.8 {
		t.Errorf("replica-1 ratio %.2f not in expected range 0.6-0.8", r1Ratio)
	}
}

func TestWeightedNoWeights(t *testing.T) {
	weights := make(map[string]int64)
	w := NewWeighted(weights)

	replicas := []*gateway.Replica{
		{Name: "replica-1", Address: "10.0.0.1:5000"},
		{Name: "replica-2", Address: "10.0.0.2:5000"},
	}

	ctx := context.Background()

	selected, _ := w.Select(ctx, replicas)
	if selected == nil {
		t.Errorf("expected non-nil selection")
	}
}

func TestRandomSelection(t *testing.T) {
	rand := NewRandom()
	replicas := []*gateway.Replica{
		{Name: "replica-1", Address: "10.0.0.1:5000"},
		{Name: "replica-2", Address: "10.0.0.2:5000"},
		{Name: "replica-3", Address: "10.0.0.3:5000"},
	}

	ctx := context.Background()
	counts := make(map[string]int)

	for i := 0; i < 300; i++ {
		selected, _ := rand.Select(ctx, replicas)
		counts[selected.Name]++
	}

	for name, count := range counts {
		if count < 50 || count > 150 {
			t.Logf("%s got %d selections", name, count)
		}
	}
}

func TestLoadBalancerConcurrent(t *testing.T) {
	tests := []struct {
		name string
		lb   gateway.LoadBalancer
	}{
		{"RoundRobin", NewRoundRobin()},
		{"LeastConnections", NewLeastConnections()},
		{"Random", NewRandom()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			replicas := []*gateway.Replica{
				{Name: "replica-1", Address: "10.0.0.1:5000"},
				{Name: "replica-2", Address: "10.0.0.2:5000"},
			}

			ctx := context.Background()
			counts := make(map[string]int)
			mu := sync.Mutex{}

			var wg sync.WaitGroup
			for i := 0; i < 50; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for j := 0; j < 20; j++ {
						selected, _ := tt.lb.Select(ctx, replicas)
						mu.Lock()
						counts[selected.Name]++
						mu.Unlock()

						tt.lb.UpdateLoad(selected, 1)
						time.Sleep(time.Microsecond)
						tt.lb.UpdateLoad(selected, -1)
					}
				}()
			}
			wg.Wait()

			total := 0
			for _, count := range counts {
				total += count
			}

			if total != 1000 {
				t.Errorf("%s: expected 1000 selections, got %d", tt.name, total)
			}
		})
	}
}

func TestLoadBalancerUpdateLoad(t *testing.T) {
	lb := NewLeastConnections()
	replica := &gateway.Replica{
		Name:    "replica-1",
		Address: "10.0.0.1:5000",
	}

	lb.UpdateLoad(replica, 5)
	load := atomic.LoadInt64(&replica.ActiveConnections)

	if load != 5 {
		t.Errorf("expected load 5, got %d", load)
	}

	lb.UpdateLoad(replica, -2)
	load = atomic.LoadInt64(&replica.ActiveConnections)

	if load != 3 {
		t.Errorf("expected load 3, got %d", load)
	}
}

func TestLoadBalancerRaceCondition(t *testing.T) {
	lb := NewLeastConnections()
	replicas := []*gateway.Replica{
		{Name: "replica-1", Address: "10.0.0.1:5000"},
		{Name: "replica-2", Address: "10.0.0.2:5000"},
	}

	ctx := context.Background()
	done := make(chan struct{})

	for i := 0; i < 10; i++ {
		go func() {
			for {
				select {
				case <-done:
					return
				default:
					selected, _ := lb.Select(ctx, replicas)
					lb.UpdateLoad(selected, 1)
					time.Sleep(time.Microsecond)
					lb.UpdateLoad(selected, -1)
				}
			}
		}()
	}

	time.Sleep(100 * time.Millisecond)
	close(done)
}

func TestResourceAwareSelection(t *testing.T) {
	ra := NewResourceAware()
	replicas := []*gateway.Replica{
		{Name: "replica-1", Address: "10.0.0.1:5000"},
		{Name: "replica-2", Address: "10.0.0.2:5000"},
		{Name: "replica-3", Address: "10.0.0.3:5000"},
	}

	// Simulate load: replica-1 has 10 connections, replica-2 has 5, replica-3 has 15
	atomic.StoreInt64(&replicas[0].ActiveConnections, 10)
	atomic.StoreInt64(&replicas[1].ActiveConnections, 5)
	atomic.StoreInt64(&replicas[2].ActiveConnections, 15)

	ctx := context.Background()
	selected, err := ra.Select(ctx, replicas)
	if err != nil {
		t.Fatalf("Select failed: %v", err)
	}

	if selected.Name != "replica-2" {
		t.Errorf("expected replica-2 (lowest load), got %s", selected.Name)
	}
}

func TestStickySelection(t *testing.T) {
	sticky := NewSticky()
	replicas := []*gateway.Replica{
		{Name: "replica-1", Address: "10.0.0.1:5000"},
		{Name: "replica-2", Address: "10.0.0.2:5000"},
		{Name: "replica-3", Address: "10.0.0.3:5000"},
	}

	// Client 1 should always get same replica
	ctx1 := context.WithValue(context.Background(), "client_id", "client-1")
	selected1, err := sticky.Select(ctx1, replicas)
	if err != nil {
		t.Fatalf("Select failed: %v", err)
	}

	for i := 0; i < 10; i++ {
		selected, _ := sticky.Select(ctx1, replicas)
		if selected.Name != selected1.Name {
			t.Errorf("iteration %d: expected %s, got %s", i, selected1.Name, selected.Name)
		}
	}

	// Client 2 should get different replica (or same, but be consistent)
	ctx2 := context.WithValue(context.Background(), "client_id", "client-2")
	selected2, err := sticky.Select(ctx2, replicas)
	if err != nil {
		t.Fatalf("Select failed: %v", err)
	}

	for i := 0; i < 10; i++ {
		selected, _ := sticky.Select(ctx2, replicas)
		if selected.Name != selected2.Name {
			t.Errorf("client-2 iteration %d: expected %s, got %s", i, selected2.Name, selected.Name)
		}
	}
}

func TestStickyRebalance(t *testing.T) {
	sticky := NewSticky()
	replicas := []*gateway.Replica{
		{Name: "replica-1", Address: "10.0.0.1:5000"},
		{Name: "replica-2", Address: "10.0.0.2:5000"},
	}

	ctx := context.WithValue(context.Background(), "client_id", "client-1")
	selected, _ := sticky.Select(ctx, replicas)

	// After rebalance, affinity should be cleared
	sticky.Rebalance(replicas)

	// Next selection should use new hash (affinity cleared)
	selected2, _ := sticky.Select(ctx, replicas)
	if selected2.Name != selected.Name {
		t.Logf("After rebalance, client may be rehashed to different replica (expected behavior)")
	}
}

func TestCustomStrategy(t *testing.T) {
	selectFn := func(ctx context.Context, replicas []*gateway.Replica) (*gateway.Replica, error) {
		if len(replicas) == 0 {
			return nil, fmt.Errorf("no replicas")
		}
		// Always select first replica
		return replicas[0], nil
	}

	updateFn := func(replica *gateway.Replica, delta int) {
		if replica != nil {
			atomic.AddInt64(&replica.ActiveConnections, int64(delta))
		}
	}

	custom := NewCustomStrategy(selectFn, updateFn)
	replicas := []*gateway.Replica{
		{Name: "replica-1", Address: "10.0.0.1:5000"},
		{Name: "replica-2", Address: "10.0.0.2:5000"},
	}

	ctx := context.Background()
	for i := 0; i < 10; i++ {
		selected, err := custom.Select(ctx, replicas)
		if err != nil {
			t.Fatalf("Select failed: %v", err)
		}

		if selected.Name != "replica-1" {
			t.Errorf("expected replica-1, got %s", selected.Name)
		}
	}
}

func TestResourceAwareConcurrent(t *testing.T) {
	ra := NewResourceAware()
	replicas := []*gateway.Replica{
		{Name: "replica-1", Address: "10.0.0.1:5000"},
		{Name: "replica-2", Address: "10.0.0.2:5000"},
		{Name: "replica-3", Address: "10.0.0.3:5000"},
	}

	ctx := context.Background()
	selected := make(map[string]int)
	var mu sync.Mutex

	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				s, _ := ra.Select(ctx, replicas)
				mu.Lock()
				selected[s.Name]++
				mu.Unlock()
			}
		}()
	}

	wg.Wait()

	total := 0
	for _, count := range selected {
		total += count
	}

	if total != 3000 {
		t.Errorf("expected 3000 selections, got %d", total)
	}
}

func BenchmarkRoundRobinSelect(b *testing.B) {
	rr := NewRoundRobin()
	replicas := []*gateway.Replica{
		{Name: "replica-1", Address: "10.0.0.1:5000"},
		{Name: "replica-2", Address: "10.0.0.2:5000"},
		{Name: "replica-3", Address: "10.0.0.3:5000"},
	}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rr.Select(ctx, replicas)
	}
}

func BenchmarkLeastConnectionsSelect(b *testing.B) {
	lc := NewLeastConnections()
	replicas := []*gateway.Replica{
		{Name: "replica-1", Address: "10.0.0.1:5000"},
		{Name: "replica-2", Address: "10.0.0.2:5000"},
		{Name: "replica-3", Address: "10.0.0.3:5000"},
	}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lc.Select(ctx, replicas)
	}
}

func BenchmarkResourceAwareSelect(b *testing.B) {
	ra := NewResourceAware()
	replicas := []*gateway.Replica{
		{Name: "replica-1", Address: "10.0.0.1:5000"},
		{Name: "replica-2", Address: "10.0.0.2:5000"},
		{Name: "replica-3", Address: "10.0.0.3:5000"},
	}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ra.Select(ctx, replicas)
	}
}

func BenchmarkStickySelect(b *testing.B) {
	sticky := NewSticky()
	replicas := []*gateway.Replica{
		{Name: "replica-1", Address: "10.0.0.1:5000"},
		{Name: "replica-2", Address: "10.0.0.2:5000"},
		{Name: "replica-3", Address: "10.0.0.3:5000"},
	}
	ctx := context.WithValue(context.Background(), "client_id", "client-1")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sticky.Select(ctx, replicas)
	}
}

func BenchmarkCustomStrategySelect(b *testing.B) {
	selectFn := func(ctx context.Context, replicas []*gateway.Replica) (*gateway.Replica, error) {
		return replicas[0], nil
	}
	custom := NewCustomStrategy(selectFn, nil)
	replicas := []*gateway.Replica{
		{Name: "replica-1", Address: "10.0.0.1:5000"},
		{Name: "replica-2", Address: "10.0.0.2:5000"},
		{Name: "replica-3", Address: "10.0.0.3:5000"},
	}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		custom.Select(ctx, replicas)
	}
}
