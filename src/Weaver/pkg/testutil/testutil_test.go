package testutil

import (
	"testing"
	"time"
)

// TestHelpers verifies test helpers work correctly
func TestHelpers(t *testing.T) {
	h := NewTestHelper(t)
	defer h.Finish()

	// Test Must helpers
	h.True(true, "true should be true")
	h.False(false, "false should be false")
	h.NotNil("value", "value should not be nil")
	h.Equal("expected", "expected", "equal values")

	// Test context helpers
	ctx := h.ContextWithTimeout(100 * time.Millisecond)
	if ctx == nil {
		t.Fatal("context should not be nil")
	}

	// Test cleanup runs
	cleanupCalled := false
	h.Cleanup(func() { cleanupCalled = true })
	h.Finish()

	if !cleanupCalled {
		t.Fatal("cleanup function should have been called")
	}
}

// TestMockReplica verifies mock replica works correctly
func TestMockReplica(t *testing.T) {
	m := NewMockReplica("test-replica", "localhost:5051")

	if m.Name != "test-replica" {
		t.Errorf("expected name test-replica, got %s", m.Name)
	}

	// Test error rate
	m.SetErrorRate(0.5)
	if m.ErrorRate != 0.5 {
		t.Errorf("expected error rate 0.5, got %f", m.ErrorRate)
	}

	// Test latency
	m.SetLatency(100 * time.Millisecond)
	if m.Latency != 100*time.Millisecond {
		t.Errorf("expected latency 100ms, got %v", m.Latency)
	}

	// Test request recording
	m.RecordRequest([]byte("test"), map[string]string{"key": "value"})
	requests := m.GetRecordedRequests()
	if len(requests) != 1 {
		t.Errorf("expected 1 recorded request, got %d", len(requests))
	}

	// Test reset
	m.Reset()
	requests = m.GetRecordedRequests()
	if len(requests) != 0 {
		t.Errorf("expected 0 requests after reset, got %d", len(requests))
	}
}

// TestMockDiscovery verifies mock discovery works correctly
func TestMockDiscovery(t *testing.T) {
	replicas := HealthyReplicas()
	md := NewMockDiscovery(replicas)

	discovered := md.GetReplicas()
	if len(discovered) != 3 {
		t.Errorf("expected 3 replicas, got %d", len(discovered))
	}

	// Test set replicas
	newReplicas := []*MockReplicaInfo{
		{Name: "replica-4", Address: "replica-4:5051"},
	}
	md.SetReplicas(newReplicas)

	discovered = md.GetReplicas()
	if len(discovered) != 1 {
		t.Errorf("expected 1 replica after set, got %d", len(discovered))
	}

	// Test etcd down simulation
	md.SimulateEtcdDown(100 * time.Millisecond)
	if !md.IsDown() {
		t.Error("expected etcd to be down")
	}

	time.Sleep(150 * time.Millisecond)
	if md.IsDown() {
		t.Error("expected etcd to be up after timeout")
	}
}

// TestFixtures verifies test fixtures are available
func TestFixtures(t *testing.T) {
	if len(HealthyReplicas()) != 3 {
		t.Error("HealthyReplicas should return 3 replicas")
	}

	if len(LargeReplicaList()) != 100 {
		t.Error("LargeReplicaList should return 100 replicas")
	}

	if len(SampleRequest()) == 0 {
		t.Error("SampleRequest should not be empty")
	}

	if len(SampleHeaders()) == 0 {
		t.Error("SampleHeaders should not be empty")
	}
}
