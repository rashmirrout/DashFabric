package integration_test

import (
	"context"
	"testing"
	"time"

	gm "github.com/dashfabric/fm/pkg/gm"
	dal "github.com/dashfabric/fm/pkg/dal"
)

// TestGMGoalStateGeneration tests Goal State Management aggregation and generation
func TestGMGoalStateGeneration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create stub aggregator with test constructs
	enis := []*gm.ConstructState{
		{ID: "eni-001", VnetID: "vnet-001", Status: "active", ConfigVersion: 1},
		{ID: "eni-002", VnetID: "vnet-001", Status: "active", ConfigVersion: 1},
	}
	aggregator := &testAggregator{
		vnetID: "vnet-001",
		enis:   enis,
	}

	// Create composer and cache
	composer := gm.NewGoalStateGenerator()
	cache := gm.NewGoalStateCache()

	// Create manager
	manager := gm.NewGoalStateManager(aggregator, composer, cache)

	// Start manager
	if err := manager.Start(ctx); err != nil {
		t.Fatalf("Failed to start manager: %v", err)
	}
	defer manager.Stop()

	// Construct a test ENI state
	testENI := &gm.ConstructState{
		ID:              "eni-001",
		VnetID:          "vnet-001",
		Status:          "active",
		ConfigVersion:   1,
		ReplicaID:       "replica-001",
	}

	// Handle construct change
	if err := manager.HandleConstructChange(ctx, testENI); err != nil {
		t.Fatalf("Failed to handle construct change: %v", err)
	}

	// Retrieve goal state
	goalState, found := manager.GetGoalState("eni-001")
	if !found {
		t.Fatal("Expected to find goal state for eni-001")
	}

	if goalState == nil {
		t.Fatal("Goal state is nil")
	}

	if goalState.ENI_ID != "eni-001" {
		t.Errorf("Expected ENI_ID eni-001, got %s", goalState.ENI_ID)
	}

	if goalState.Version != 1 {
		t.Errorf("Expected version 1, got %d", goalState.Version)
	}

	// Verify fingerprint exists
	if goalState.Fingerprint == "" {
		t.Error("Expected fingerprint to be generated")
	}

	t.Logf("✓ Goal state generated: ENI=%s, Version=%d, Fingerprint=%s",
		goalState.ENI_ID, goalState.Version, goalState.Fingerprint[:8])
}

// TestDALPluginDispatch tests DPU Abstraction Layer plugin dispatch
func TestDALPluginDispatch(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create plugin factory
	factory := dal.NewPluginFactory()

	// Create DAL manager
	manager, err := factory.CreateDPUManager(2)
	if err != nil {
		t.Fatalf("Failed to create DPU manager: %v", err)
	}

	// Start manager
	if err := manager.Start(ctx); err != nil {
		t.Fatalf("Failed to start manager: %v", err)
	}
	defer manager.Stop()

	// Create test goal state
	goalState := &gm.PerENIGoalState{
		ENI_ID:      "eni-001",
		Routes:      make([]*gm.Route, 0),
		ACLs:        make([]*gm.ACLRule, 0),
		VIPMappings: make([]*gm.VIPMapping, 0),
		Fingerprint: "test-fingerprint-001",
		Version:     1,
	}

	// Program the device
	result, err := manager.Program(ctx, goalState)
	if err != nil {
		t.Fatalf("Failed to program device: %v", err)
	}

	if result == nil {
		t.Fatal("Programming result is nil")
	}

	if !result.Success {
		t.Errorf("Programming failed: %s", result.Error)
	}

	if result.ENI_ID != "eni-001" {
		t.Errorf("Expected ENI_ID eni-001, got %s", result.ENI_ID)
	}

	if result.AppliedVersion != 1 {
		t.Errorf("Expected applied version 1, got %d", result.AppliedVersion)
	}

	t.Logf("✓ Device programmed: ENI=%s, Version=%d, Success=%v",
		result.ENI_ID, result.AppliedVersion, result.Success)
}

// TestGMDALIntegration tests full GM→DAL pipeline
func TestGMDALIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create test constructs
	testConstructs := make([]*gm.ConstructState, 1)
	testConstructs[0] = &gm.ConstructState{
		ID:            "eni-001",
		VnetID:        "vnet-001",
		Status:        "active",
		ConfigVersion: 1,
	}

	// Create GM manager with pre-populated aggregator
	gmAggregator := &testAggregator{vnetID: "vnet-001", enis: testConstructs}
	gmComposer := gm.NewGoalStateGenerator()
	gmCache := gm.NewGoalStateCache()
	gmManager := gm.NewGoalStateManager(gmAggregator, gmComposer, gmCache)

	if err := gmManager.Start(ctx); err != nil {
		t.Fatalf("Failed to start GM: %v", err)
	}
	defer gmManager.Stop()

	// Create DAL manager
	dalFactory := dal.NewPluginFactory()
	dalManager, err := dalFactory.CreateDPUManager(2)
	if err != nil {
		t.Fatalf("Failed to create DAL: %v", err)
	}

	if err := dalManager.Start(ctx); err != nil {
		t.Fatalf("Failed to start DAL: %v", err)
	}
	defer dalManager.Stop()

	// Step 1: Generate goal state in GM
	testENI := &gm.ConstructState{
		ID:            "eni-001",
		VnetID:        "vnet-001",
		Status:        "active",
		ConfigVersion: 1,
	}

	if err := gmManager.HandleConstructChange(ctx, testENI); err != nil {
		t.Fatalf("Failed to generate goal state: %v", err)
	}

	// Step 2: Retrieve goal state
	goalState, found := gmManager.GetGoalState("eni-001")
	if !found {
		t.Fatal("Expected goal state to be found")
	}

	// Step 3: Program device using DAL
	result, err := dalManager.Program(ctx, goalState)
	if err != nil {
		t.Fatalf("Failed to program device: %v", err)
	}

	if !result.Success {
		t.Errorf("Device programming failed: %s", result.Error)
	}

	t.Logf("✓ Full pipeline complete: GM generated goal state, DAL programmed device")
	t.Logf("  Goal State: ENI=%s, Version=%d", goalState.ENI_ID, goalState.Version)
	t.Logf("  Programming Result: ENI=%s, Success=%v", result.ENI_ID, result.Success)
}

// TestGoalStateCaching tests fingerprint-based caching in GM
func TestGoalStateCaching(t *testing.T) {
	// Create cache
	cache := gm.NewGoalStateCache()

	// Create test goal state
	goalState1 := &gm.PerENIGoalState{
		ENI_ID:      "eni-001",
		Fingerprint: "fp-001",
		Version:     1,
	}

	// Store goal state
	cache.Set("fp-001", goalState1)

	// Retrieve goal state
	retrieved, found := cache.Get("fp-001")
	if !found {
		t.Fatal("Expected to find goal state in cache")
	}

	if retrieved.ENI_ID != "eni-001" {
		t.Errorf("Expected ENI_ID eni-001, got %s", retrieved.ENI_ID)
	}

	// Test cache miss
	_, found = cache.Get("fp-999")
	if found {
		t.Fatal("Expected cache miss for non-existent fingerprint")
	}

	// Test cache clear
	cache.Clear()
	_, found = cache.Get("fp-001")
	if found {
		t.Fatal("Expected cache miss after clear")
	}

	t.Logf("✓ Goal state caching working correctly")
}

// Test helper structures

type testAggregator struct {
	vnetID string
	enis   []*gm.ConstructState
}

func (ta *testAggregator) Aggregate(ctx context.Context, vnetID string) (*gm.AggregatedState, error) {
	// Return pre-populated constructs for the VNET
	return &gm.AggregatedState{
		VnetID:     vnetID,
		Constructs: ta.enis,
		Replicas:   make([]*gm.ReplicaState, 0),
	}, nil
}
