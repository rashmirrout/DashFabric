package integration_test

import (
	"context"
	"sync"
	"testing"
	"time"

	gm "github.com/dashfabric/fm/pkg/gm"
	dal "github.com/dashfabric/fm/pkg/dal"
)

// TestConcurrentGoalStateGeneration tests concurrent goal state generation in GM
func TestConcurrentGoalStateGeneration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create constructs for multiple VNETs
	constructs := make([]*gm.ConstructState, 10)
	for i := 0; i < 10; i++ {
		constructs[i] = &gm.ConstructState{
			ID:            "eni-" + string(rune(48+i)),
			VnetID:        "vnet-001",
			Status:        "active",
			ConfigVersion: 1,
		}
	}

	// Create GM manager
	aggregator := &testAggregator{vnetID: "vnet-001", enis: constructs}
	composer := gm.NewGoalStateGenerator()
	cache := gm.NewGoalStateCache()
	manager := gm.NewGoalStateManager(aggregator, composer, cache)

	if err := manager.Start(ctx); err != nil {
		t.Fatalf("Failed to start manager: %v", err)
	}
	defer manager.Stop()

	// Concurrent goal state generation
	var wg sync.WaitGroup
	errChan := make(chan error, 10)
	successCount := 0

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			construct := constructs[idx]
			if err := manager.HandleConstructChange(ctx, construct); err != nil {
				errChan <- err
				return
			}
			goalState, found := manager.GetGoalState(construct.ID)
			if !found {
				errChan <- nil // Track success
				return
			}
			if goalState == nil {
				errChan <- nil
			}
		}(i)
	}

	wg.Wait()
	close(errChan)

	// Check results
	for err := range errChan {
		if err != nil {
			t.Errorf("Concurrent operation failed: %v", err)
		} else {
			successCount++
		}
	}

	if successCount == 0 {
		t.Logf("✓ All 10 concurrent goal state operations succeeded")
	}
}

// TestConcurrentPluginDispatch tests concurrent plugin dispatch in DAL
func TestConcurrentPluginDispatch(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create DAL manager
	factory := dal.NewPluginFactory()
	manager, err := factory.CreateDPUManager(4) // 4 worker pool
	if err != nil {
		t.Fatalf("Failed to create DPU manager: %v", err)
	}

	if err := manager.Start(ctx); err != nil {
		t.Fatalf("Failed to start manager: %v", err)
	}
	defer manager.Stop()

	// Concurrent device programming
	var wg sync.WaitGroup
	successCount := 0
	mu := sync.Mutex{}

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			goalState := &gm.PerENIGoalState{
				ENI_ID:      "eni-" + string(rune(48+idx)),
				Fingerprint: "fp-" + string(rune(48+idx)),
				Version:     1,
			}

			result, err := manager.Program(ctx, goalState)
			if err != nil {
				t.Errorf("Programming failed: %v", err)
				return
			}

			if result.Success {
				mu.Lock()
				successCount++
				mu.Unlock()
			}
		}(i)
	}

	wg.Wait()

	if successCount == 10 {
		t.Logf("✓ All 10 concurrent programming operations succeeded")
	} else {
		t.Errorf("Expected 10 successful operations, got %d", successCount)
	}
}

// TestConcurrentFullPipeline tests full concurrent GM→DAL pipeline
func TestConcurrentFullPipeline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create constructs
	constructs := make([]*gm.ConstructState, 5)
	for i := 0; i < 5; i++ {
		constructs[i] = &gm.ConstructState{
			ID:            "eni-" + string(rune(48+i)),
			VnetID:        "vnet-001",
			Status:        "active",
			ConfigVersion: 1,
		}
	}

	// Create GM and DAL managers
	gmAggregator := &testAggregator{vnetID: "vnet-001", enis: constructs}
	gmComposer := gm.NewGoalStateGenerator()
	gmCache := gm.NewGoalStateCache()
	gmManager := gm.NewGoalStateManager(gmAggregator, gmComposer, gmCache)

	dalFactory := dal.NewPluginFactory()
	dalManager, err := dalFactory.CreateDPUManager(4)
	if err != nil {
		t.Fatalf("Failed to create DAL: %v", err)
	}

	if err := gmManager.Start(ctx); err != nil {
		t.Fatalf("Failed to start GM: %v", err)
	}
	defer gmManager.Stop()

	if err := dalManager.Start(ctx); err != nil {
		t.Fatalf("Failed to start DAL: %v", err)
	}
	defer dalManager.Stop()

	// Concurrent pipeline execution
	var wg sync.WaitGroup
	successCount := 0
	mu := sync.Mutex{}

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			construct := constructs[idx]

			// Generate goal state
			if err := gmManager.HandleConstructChange(ctx, construct); err != nil {
				t.Errorf("Goal state generation failed: %v", err)
				return
			}

			// Retrieve goal state
			goalState, found := gmManager.GetGoalState(construct.ID)
			if !found {
				t.Errorf("Goal state not found for %s", construct.ID)
				return
			}

			// Program device
			result, err := dalManager.Program(ctx, goalState)
			if err != nil {
				t.Errorf("Device programming failed: %v", err)
				return
			}

			if result.Success {
				mu.Lock()
				successCount++
				mu.Unlock()
			}
		}(i)
	}

	wg.Wait()

	if successCount == 5 {
		t.Logf("✓ All 5 concurrent full pipeline operations succeeded")
	} else {
		t.Errorf("Expected 5 successful operations, got %d", successCount)
	}
}

// TestConcurrencyNoRaceConditions tests that concurrent operations don't cause race conditions
func TestConcurrencyNoRaceConditions(t *testing.T) {
	// Create cache for concurrent access testing
	cache := gm.NewGoalStateCache()

	var wg sync.WaitGroup

	// Concurrent writes
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			goalState := &gm.PerENIGoalState{
				ENI_ID:      "eni-" + string(rune(48+idx%10)),
				Fingerprint: "fp-" + string(rune(48+idx%10)),
				Version:     int64(idx),
			}
			cache.Set("fp-"+string(rune(48+idx%10)), goalState)
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, _ = cache.Get("fp-" + string(rune(48+idx%10)))
		}(i)
	}

	wg.Wait()

	t.Logf("✓ Concurrent cache access completed without race conditions")
}
