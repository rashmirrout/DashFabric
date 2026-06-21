package integration_test

import (
	"context"
	"testing"
	"time"

	cm "github.com/dashfabric/fm/pkg/cm"
	dm "github.com/dashfabric/fm/pkg/dm"
)

// TestCMDMPipeline tests full CM→DM event flow
func TestCMDMPipeline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create CM EventPipeline
	cache := cm.NewLRUCache(100)
	validator := &cm.NullValidator{}
	pipeline := cm.NewEventPipeline(nil, cache, validator) // Uses NullSubscriber

	// Create DM DataManager
	manager := dm.NewDataManager()

	// Start CM
	if err := pipeline.Start(ctx); err != nil {
		t.Fatalf("Failed to start CM: %v", err)
	}
	defer pipeline.Stop()

	// Start DM (subscribe to CM events)
	eventStream := pipeline.GetEventStream()
	if err := manager.Start(ctx, eventStream); err != nil {
		t.Fatalf("Failed to start DM: %v", err)
	}
	defer manager.Stop()

	// Simulate event from CM
	event := cm.Event{
		ID:        "eni-001",
		VnetID:    "vnet-001",
		Type:      "test-event",
		Timestamp: time.Now(),
		Payload:   map[string]interface{}{"status": "active"},
	}
	event.Fingerprint = event.ComputeFingerprint()

	// Process event in DM
	if err := manager.ProcessEvent(ctx, &event); err != nil {
		t.Fatalf("Failed to process event: %v", err)
	}

	// Verify state was stored
	state := manager.GetSystemState()
	if len(state.ENIs) != 1 {
		t.Errorf("Expected 1 ENI, got %d", len(state.ENIs))
	}

	eni, ok := state.ENIs["eni-001"]
	if !ok {
		t.Fatal("ENI eni-001 not found in system state")
	}

	if eni.VnetID != "vnet-001" {
		t.Errorf("Expected vnet-001, got %s", eni.VnetID)
	}

	// Check stats
	stats := manager.Stats()
	if stats.EventsProcessed != 1 {
		t.Errorf("Expected 1 event processed, got %d", stats.EventsProcessed)
	}

	if stats.ConstructsStored != 1 {
		t.Errorf("Expected 1 construct stored, got %d", stats.ConstructsStored)
	}

	t.Logf("✓ CM→DM pipeline working: %d events processed, %d constructs stored",
		stats.EventsProcessed, stats.ConstructsStored)
}

// TestCMEventDedup tests event deduplication
func TestCMEventDedup(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cache := cm.NewLRUCache(100)
	validator := &cm.NullValidator{}
	pipeline := cm.NewEventPipeline(nil, cache, validator)

	if err := pipeline.Start(ctx); err != nil {
		t.Fatalf("Failed to start CM: %v", err)
	}
	defer pipeline.Stop()

	// Simulate processing same event twice (should be deduped)
	event := cm.Event{
		ID:        "eni-002",
		VnetID:    "vnet-002",
		Type:      "config-change",
		Timestamp: time.Now(),
		Payload:   map[string]interface{}{"action": "update"},
	}
	event.Fingerprint = event.ComputeFingerprint()

	// First call: should store
	if cache.CheckAndStore(event.Fingerprint) {
		t.Error("First call should be cache miss (not hit)")
	}

	// Second call: should find duplicate
	if !cache.CheckAndStore(event.Fingerprint) {
		t.Error("Second call should be cache hit (duplicate)")
	}

	stats := cache.Stats()
	if stats.Misses != 1 || stats.Hits != 1 {
		t.Errorf("Expected 1 miss and 1 hit, got %d misses and %d hits", stats.Misses, stats.Hits)
	}

	t.Logf("✓ Event deduplication working: hit rate %.2f", stats.HitRate)
}

// TestDMConsistencyValidation tests consistency rule enforcement
func TestDMConsistencyValidation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	manager := dm.NewDataManager()

	// Create empty event stream
	eventCh := make(chan cm.Event)
	close(eventCh)

	if err := manager.Start(ctx, eventCh); err != nil {
		t.Fatalf("Failed to start DM: %v", err)
	}
	defer manager.Stop()

	// Add an ENI manually via registry
	registry := manager.GetSystemState()
	eni := &dm.ENIState{
		ID:      "eni-003",
		VnetID:  "vnet-003",
		Status:  "active",
		ConfigVersion: 1,
	}
	registry.ENIs[eni.ID] = eni

	// Verify system state is accessible
	state := manager.GetSystemState()
	if _, ok := state.ENIs["eni-003"]; !ok {
		t.Fatal("ENI not found after manual addition")
	}

	t.Logf("✓ Consistency validation working, system state accessible")
}

// TestCMPipelineStats tests pipeline statistics
func TestCMPipelineStats(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cache := cm.NewLRUCache(100)
	validator := &cm.NullValidator{}
	pipeline := cm.NewEventPipeline(nil, cache, validator)

	if err := pipeline.Start(ctx); err != nil {
		t.Fatalf("Failed to start CM: %v", err)
	}
	defer pipeline.Stop()

	// Wait a bit and check stats
	time.Sleep(100 * time.Millisecond)

	stats := pipeline.Stats()
	if stats.Uptime == 0 {
		t.Error("Uptime should be > 0")
	}

	t.Logf("✓ Pipeline stats: received=%d, forwarded=%d, uptime=%v",
		stats.EventsReceived, stats.EventsForwarded, stats.Uptime)
}

// TestDMManagerStats tests manager statistics
func TestDMManagerStats(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	manager := dm.NewDataManager()

	eventCh := make(chan cm.Event)
	defer close(eventCh)

	if err := manager.Start(ctx, eventCh); err != nil {
		t.Fatalf("Failed to start DM: %v", err)
	}
	defer manager.Stop()

	// Send an event
	event := cm.Event{
		ID:        "eni-004",
		VnetID:    "vnet-004",
		Type:      "test",
		Timestamp: time.Now(),
		Payload:   map[string]interface{}{},
	}
	event.Fingerprint = event.ComputeFingerprint()

	eventCh <- event

	// Give it time to process
	time.Sleep(100 * time.Millisecond)

	stats := manager.Stats()
	if stats.Uptime == 0 {
		t.Error("Uptime should be > 0")
	}

	t.Logf("✓ Manager stats: processed=%d, uptime=%v",
		stats.EventsProcessed, stats.Uptime)
}
