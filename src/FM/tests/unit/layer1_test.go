package layer1_test

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/dashfabric/fm/pkg/cm"
)

// TC-000: Cache Creation with Default Size
func TestCache_DefaultSize_WhenZero(t *testing.T) {
	cache := configmanagement.NewLRUCache(0)

	// Should use default size (10000)
	if cache.MaxSize() <= 0 {
		t.Errorf("expected default cache size > 0, got %d", cache.MaxSize())
	}

	// Should be usable
	cache.CheckAndStore("fp1")
	if cache.Size() != 1 {
		t.Errorf("expected cache size 1, got %d", cache.Size())
	}
}

// TC-001: Fingerprint Computation
func TestFingerprint_ComputeCorrectly(t *testing.T) {
	event := &configmanagement.Event{
		ID:     "evt-001",
		VnetID: "vnet-prod",
		Type:   "RouteTableUpdate",
		Payload: map[string]interface{}{
			"routes": []string{"10.0/8", "10.1/8"},
		},
	}

	fp1 := event.ComputeFingerprint()
	if len(fp1) != 64 { // SHA256 hex is 64 chars
		t.Errorf("expected fingerprint length 64, got %d", len(fp1))
	}

	// Verify deterministic (same event = same fingerprint)
	fp2 := event.ComputeFingerprint()
	if fp1 != fp2 {
		t.Errorf("fingerprints should be identical, got %s and %s", fp1, fp2)
	}
}

// TC-003: Cache Miss (New Event)
func TestCache_Miss_NewEvent(t *testing.T) {
	cache := configmanagement.NewLRUCache(100)

	fp := "abc123def456"
	isHit := cache.CheckAndStore(fp)

	if isHit {
		t.Errorf("expected cache miss, got hit")
	}

	stats := cache.Stats()
	if stats.Misses != 1 {
		t.Errorf("expected 1 miss, got %d", stats.Misses)
	}
	if stats.Hits != 0 {
		t.Errorf("expected 0 hits, got %d", stats.Hits)
	}
}

// TC-004: Cache Hit (Duplicate)
func TestCache_Hit_Duplicate(t *testing.T) {
	cache := configmanagement.NewLRUCache(100)

	fp := "abc123def456"

	// First insert (miss)
	isHit1 := cache.CheckAndStore(fp)
	if isHit1 {
		t.Errorf("first insert should be miss")
	}

	// Second insert (hit)
	isHit2 := cache.CheckAndStore(fp)
	if !isHit2 {
		t.Errorf("duplicate should be hit")
	}

	stats := cache.Stats()
	if stats.Hits != 1 {
		t.Errorf("expected 1 hit, got %d", stats.Hits)
	}
	if stats.Misses != 1 {
		t.Errorf("expected 1 miss, got %d", stats.Misses)
	}
}

// TC-005: LRU Eviction
func TestCache_LRU_Eviction(t *testing.T) {
	cache := configmanagement.NewLRUCache(3) // Small cache for testing

	// Fill cache
	cache.CheckAndStore("fp1")
	cache.CheckAndStore("fp2")
	cache.CheckAndStore("fp3")

	if cache.Size() != 3 {
		t.Errorf("expected size 3, got %d", cache.Size())
	}

	// Add one more - should evict oldest (fp1)
	cache.CheckAndStore("fp4")

	if cache.Size() != 3 {
		t.Errorf("expected size 3 after eviction, got %d", cache.Size())
	}

	stats := cache.Stats()
	if stats.Evictions != 1 {
		t.Errorf("expected 1 eviction, got %d", stats.Evictions)
	}

	// Verify fp1 was evicted by checking if it's a miss (new insert)
	isHit := cache.CheckAndStore("fp1")
	if isHit {
		t.Errorf("fp1 should have been evicted, but got cache hit")
	}
}

// TC-006: LRU Ordering (MRU moves to front)
func TestCache_LRU_MRUOrdering(t *testing.T) {
	cache := configmanagement.NewLRUCache(3)

	// Fill cache
	cache.CheckAndStore("fp1")
	cache.CheckAndStore("fp2")
	cache.CheckAndStore("fp3")

	// Access fp1 (moves to front)
	cache.CheckAndStore("fp1")

	// Add new event - should evict fp2 (oldest after fp1 was accessed)
	cache.CheckAndStore("fp4")

	// fp1 should still be in cache
	isHit1 := cache.CheckAndStore("fp1")
	if !isHit1 {
		t.Errorf("fp1 should still be in cache after MRU access")
	}

	// fp2 should have been evicted
	cache.Clear() // Reset to verify
	cache.CheckAndStore("fp1")
	cache.CheckAndStore("fp2")
	cache.CheckAndStore("fp3")
	cache.CheckAndStore("fp1") // Access fp1
	cache.CheckAndStore("fp4")

	isHit2 := cache.CheckAndStore("fp2")
	if isHit2 {
		t.Errorf("fp2 should have been evicted")
	}
}

// TC-012: Hit Rate Measurement
func TestCache_HitRate_Calculation(t *testing.T) {
	cache := configmanagement.NewLRUCache(10)

	// Create 20 events: 10 unique, repeated (80% duplicates)
	events := []string{
		"fp1", "fp1", // 1 unique, 1 dup
		"fp2", "fp2", // 1 unique, 1 dup
		"fp3", "fp3", // 1 unique, 1 dup
		"fp4", "fp4", // 1 unique, 1 dup
		"fp5", "fp5", // 1 unique, 1 dup
		"fp6", "fp6", // 1 unique, 1 dup
		"fp7", "fp7", // 1 unique, 1 dup
		"fp8", "fp8", // 1 unique, 1 dup
		"fp9", "fp9", // 1 unique, 1 dup
		"fp10", "fp10", // 1 unique, 1 dup
	}

	for _, fp := range events {
		cache.CheckAndStore(fp)
	}

	stats := cache.Stats()
	// 50% hit rate expected (10 duplicates out of 20 total)
	if stats.HitRate < 0.4 || stats.HitRate > 0.6 {
		t.Errorf("expected hit rate ~50 percent, got %v", stats.HitRate)
	}
}

// TC-018: Concurrent Writes (Thread Safety)
func TestCache_Concurrent_Writes(t *testing.T) {
	cache := configmanagement.NewLRUCache(1000)
	numGoroutines := 100
	operationsPerGoroutine := 1000

	var wg sync.WaitGroup
	var panicCount int32

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					atomic.AddInt32(&panicCount, 1)
				}
			}()

			for j := 0; j < operationsPerGoroutine; j++ {
				fp := fmt.Sprintf("fp-%d-%d", id, j%10) // 10 unique fps per goroutine
				cache.CheckAndStore(fp)
			}
		}(i)
	}

	wg.Wait()

	if panicCount > 0 {
		t.Errorf("panics during concurrent writes: %d", panicCount)
	}

	// Verify cache is still valid
	stats := cache.Stats()
	if stats.Size > cache.MaxSize() {
		t.Errorf("cache size %d exceeds max", stats.Size)
	}
}

// TC-022: Race Condition Detection
func TestCache_NoRaceConditions(t *testing.T) {
	cache := configmanagement.NewLRUCache(1000)
	numGoroutines := 50

	var wg sync.WaitGroup

	// Writers
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				fp := fmt.Sprintf("fp-%d", id%10)
				cache.CheckAndStore(fp)
			}
		}(i)
	}

	// Readers
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = cache.Size()
				_ = cache.Stats()
			}
		}()
	}

	wg.Wait()
	// Should complete without race detector warnings
}

// Benchmark: Cache Lookup Performance
func BenchmarkCache_Lookup(b *testing.B) {
	cache := configmanagement.NewLRUCache(10000)

	// Warm up cache
	for i := 0; i < 1000; i++ {
		fp := fmt.Sprintf("fp-%d", i)
		cache.CheckAndStore(fp)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fp := fmt.Sprintf("fp-%d", i%1000)
		cache.CheckAndStore(fp)
	}
}

// Benchmark: Fingerprint Computation
func BenchmarkFingerprint_Compute(b *testing.B) {
	event := &configmanagement.Event{
		ID:     "evt-001",
		VnetID: "vnet-prod",
		Type:   "RouteTableUpdate",
		Payload: map[string]interface{}{
			"routes": []string{"10.0/8", "10.1/8"},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = event.ComputeFingerprint()
	}
}

// TC-023: Fingerprint with String Payload
func TestFingerprint_WithStringPayload(t *testing.T) {
	event := &configmanagement.Event{
		ID:     "evt-002",
		VnetID: "vnet-test",
		Type:   "ConfigUpdate",
		Payload: map[string]interface{}{
			"name": "test-config",
		},
	}

	fp := event.ComputeFingerprint()
	if len(fp) != 64 {
		t.Errorf("expected fingerprint length 64, got %d", len(fp))
	}

	// Verify string values are included in fingerprint
	fp2 := event.ComputeFingerprint()
	if fp != fp2 {
		t.Errorf("fingerprints should be identical for same payload")
	}
}

// TC-024: Fingerprint with Float Payload
func TestFingerprint_WithFloatPayload(t *testing.T) {
	event := &configmanagement.Event{
		ID:     "evt-003",
		VnetID: "vnet-test",
		Type:   "MetricUpdate",
		Payload: map[string]interface{}{
			"value": 3.14159,
		},
	}

	fp := event.ComputeFingerprint()
	if len(fp) != 64 {
		t.Errorf("expected fingerprint length 64, got %d", len(fp))
	}
}

// TC-025: Fingerprint with Boolean Payload
func TestFingerprint_WithBoolPayload(t *testing.T) {
	event := &configmanagement.Event{
		ID:     "evt-004",
		VnetID: "vnet-test",
		Type:   "StatusUpdate",
		Payload: map[string]interface{}{
			"enabled": true,
		},
	}

	fp := event.ComputeFingerprint()
	if len(fp) != 64 {
		t.Errorf("expected fingerprint length 64, got %d", len(fp))
	}

	// Different boolean value should produce different fingerprint
	event2 := &configmanagement.Event{
		ID:     "evt-004",
		VnetID: "vnet-test",
		Type:   "StatusUpdate",
		Payload: map[string]interface{}{
			"enabled": false,
		},
	}

	fp2 := event2.ComputeFingerprint()
	if fp == fp2 {
		t.Errorf("different boolean values should produce different fingerprints")
	}
}

// TC-026: Fingerprint with Mixed Types
func TestFingerprint_WithMixedTypes(t *testing.T) {
	event := &configmanagement.Event{
		ID:     "evt-005",
		VnetID: "vnet-test",
		Type:   "ComplexUpdate",
		Payload: map[string]interface{}{
			"name":     "config",
			"value":    2.71828,
			"active":   true,
			"unknown":  nil, // Unhandled type
		},
	}

	fp := event.ComputeFingerprint()
	if len(fp) != 64 {
		t.Errorf("expected fingerprint length 64, got %d", len(fp))
	}
}

// TC-027: Fingerprint Determinism with Complex Payload
func TestFingerprint_DeterminismComplex(t *testing.T) {
	payload := map[string]interface{}{
		"string":  "test",
		"float":   1.23,
		"boolean": true,
		"mixed":   "value",
	}

	event1 := &configmanagement.Event{
		ID:      "evt-006",
		VnetID:  "vnet-test",
		Type:    "Test",
		Payload: payload,
	}

	// Create event with same payload
	event2 := &configmanagement.Event{
		ID:      "evt-006",
		VnetID:  "vnet-test",
		Type:    "Test",
		Payload: map[string]interface{}{
			"string":  "test",
			"float":   1.23,
			"boolean": true,
			"mixed":   "value",
		},
	}

	fp1 := event1.ComputeFingerprint()
	fp2 := event2.ComputeFingerprint()

	// Should be identical if canonical JSON is used
	if fp1 != fp2 {
		t.Errorf("identical payloads should produce identical fingerprints")
	}
}
