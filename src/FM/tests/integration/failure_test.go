package integration_test

import (
	"context"
	"testing"
	"time"

	gm "github.com/dashfabric/fm/pkg/gm"
	dal "github.com/dashfabric/fm/pkg/dal"
)

// TestManagerNotRunning tests behavior when manager is not started
func TestManagerNotRunning(t *testing.T) {
	// Create manager but don't start it
	aggregator := &testAggregator{vnetID: "vnet-001"}
	composer := gm.NewGoalStateGenerator()
	cache := gm.NewGoalStateCache()
	manager := gm.NewGoalStateManager(aggregator, composer, cache)

	ctx := context.Background()
	construct := &gm.ConstructState{ID: "eni-001", VnetID: "vnet-001"}

	// Try to handle construct change without starting manager
	err := manager.HandleConstructChange(ctx, construct)
	if err == nil {
		t.Error("Expected error when manager not running")
	}

	t.Logf("✓ Proper error handling when manager not running: %v", err)
}

// TestContextCancellation tests proper handling of context cancellation
func TestContextCancellation(t *testing.T) {
	// Create manager
	aggregator := &testAggregator{vnetID: "vnet-001"}
	composer := gm.NewGoalStateGenerator()
	cache := gm.NewGoalStateCache()
	manager := gm.NewGoalStateManager(aggregator, composer, cache)

	// Start with context that will be cancelled
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := manager.Start(ctx); err != nil {
		t.Fatalf("Failed to start manager: %v", err)
	}

	// Cancel context
	cancel()

	// Try to handle construct change after cancellation
	construct := &gm.ConstructState{ID: "eni-001", VnetID: "vnet-001"}
	err := manager.HandleConstructChange(ctx, construct)

	if err == nil {
		t.Error("Expected error after context cancellation")
	}

	manager.Stop()

	t.Logf("✓ Proper error handling for context cancellation: %v", err)
}

// TestNilConstructState tests handling of nil construct state
func TestNilConstructState(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	aggregator := &testAggregator{vnetID: "vnet-001"}
	composer := gm.NewGoalStateGenerator()
	cache := gm.NewGoalStateCache()
	manager := gm.NewGoalStateManager(aggregator, composer, cache)

	if err := manager.Start(ctx); err != nil {
		t.Fatalf("Failed to start manager: %v", err)
	}
	defer manager.Stop()

	// Try to handle nil construct
	err := manager.HandleConstructChange(ctx, nil)
	if err == nil {
		t.Error("Expected error for nil construct state")
	}

	t.Logf("✓ Proper error handling for nil construct: %v", err)
}

// TestNilGoalState tests handling of nil goal state in DAL
func TestNilGoalState(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	factory := dal.NewPluginFactory()
	manager, err := factory.CreateDPUManager(2)
	if err != nil {
		t.Fatalf("Failed to create DAL: %v", err)
	}

	if err := manager.Start(ctx); err != nil {
		t.Fatalf("Failed to start manager: %v", err)
	}
	defer manager.Stop()

	// Try to program nil goal state
	result, err := manager.Program(ctx, nil)
	if err == nil {
		t.Error("Expected error for nil goal state")
	}

	if result != nil {
		t.Error("Expected nil result for nil goal state")
	}

	t.Logf("✓ Proper error handling for nil goal state: %v", err)
}

// TestDoubleStart tests that starting an already-running manager returns error
func TestDoubleStart(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	aggregator := &testAggregator{vnetID: "vnet-001"}
	composer := gm.NewGoalStateGenerator()
	cache := gm.NewGoalStateCache()
	manager := gm.NewGoalStateManager(aggregator, composer, cache)

	// Start manager
	if err := manager.Start(ctx); err != nil {
		t.Fatalf("Failed to start manager: %v", err)
	}

	// Try to start again
	err := manager.Start(ctx)
	if err == nil {
		t.Error("Expected error when starting already-running manager")
	}

	manager.Stop()

	t.Logf("✓ Proper error handling for double start: %v", err)
}

// TestDoubleStop tests that stopping an already-stopped manager returns error
func TestDoubleStop(t *testing.T) {
	ctx := context.Background()

	aggregator := &testAggregator{vnetID: "vnet-001"}
	composer := gm.NewGoalStateGenerator()
	cache := gm.NewGoalStateCache()
	manager := gm.NewGoalStateManager(aggregator, composer, cache)

	if err := manager.Start(ctx); err != nil {
		t.Fatalf("Failed to start manager: %v", err)
	}

	// Stop manager
	if err := manager.Stop(); err != nil {
		t.Fatalf("Failed to stop manager: %v", err)
	}

	// Try to stop again
	err := manager.Stop()
	if err == nil {
		t.Error("Expected error when stopping already-stopped manager")
	}

	t.Logf("✓ Proper error handling for double stop: %v", err)
}

// TestTimeout tests behavior with very short timeout
func TestTimeout(t *testing.T) {
	// Create context with immediate timeout
	ctx, cancel := context.WithTimeout(context.Background(), 0)
	defer cancel()

	aggregator := &testAggregator{vnetID: "vnet-001"}
	composer := gm.NewGoalStateGenerator()
	cache := gm.NewGoalStateCache()
	manager := gm.NewGoalStateManager(aggregator, composer, cache)

	// Try to start with already-expired context
	err := manager.Start(ctx)
	// Note: Start might succeed because it just sets state, but subsequent operations will timeout

	construct := &gm.ConstructState{ID: "eni-001", VnetID: "vnet-001"}
	err = manager.HandleConstructChange(ctx, construct)
	if err == nil {
		t.Error("Expected timeout error")
	}

	// Clean up
	manager.Stop()

	t.Logf("✓ Proper error handling for timeout: %v", err)
}

// TestEmptyVnetID tests handling of empty VNET ID
func TestEmptyVnetID(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	aggregator := &testAggregator{vnetID: ""}
	composer := gm.NewGoalStateGenerator()
	cache := gm.NewGoalStateCache()
	manager := gm.NewGoalStateManager(aggregator, composer, cache)

	if err := manager.Start(ctx); err != nil {
		t.Fatalf("Failed to start manager: %v", err)
	}
	defer manager.Stop()

	// Try to handle construct with empty VNET ID
	construct := &gm.ConstructState{ID: "eni-001", VnetID: ""}
	err := manager.HandleConstructChange(ctx, construct)

	// Should handle gracefully (may succeed or fail, but shouldn't panic)
	t.Logf("✓ Handled empty VNET ID: %v", err)
}
