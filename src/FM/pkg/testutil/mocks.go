package testutil

import (
	"context"
	"math/rand"
	"sync"
	"time"
)

// MockEvent represents a mock device event for testing
type MockEvent struct {
	ID        string
	VnetID    string
	Type      string
	Timestamp time.Time
	Payload   map[string]interface{}
}

// MockReplicaConfig configures mock replica behavior
type MockReplicaConfig struct {
	FailureRate   float64       // 0.0-1.0: probability of failure
	LatencyMs     int           // Base latency in milliseconds
	LatencyJitter int           // Jitter in milliseconds
	Crash         bool          // Simulate crash
	CrashAfterN   int           // Crash after N requests
}

// MockReplica simulates a device replica for testing
type MockReplica struct {
	ID       string
	Config   MockReplicaConfig
	Healthy  bool
	mu       sync.RWMutex
	requests int
	errors   int
}

// NewMockReplica creates a new mock replica
func NewMockReplica(id string, config MockReplicaConfig) *MockReplica {
	return &MockReplica{
		ID:      id,
		Config:  config,
		Healthy: true,
	}
}

// Program simulates sending a Goal State to the replica
func (m *MockReplica) Program(ctx context.Context, goalState map[string]interface{}) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.requests++

	// Simulate crash condition
	if m.Config.Crash && m.requests > m.Config.CrashAfterN {
		m.Healthy = false
		m.errors++
		return ErrReplicaCrashed
	}

	// Simulate latency
	time.Sleep(time.Duration(m.Config.LatencyMs) * time.Millisecond)

	// Simulate failure rate
	if rand.Float64() < m.Config.FailureRate {
		m.errors++
		return ErrReplicaFailed
	}

	return nil
}

// Health returns replica health status
func (m *MockReplica) Health() ReplicaHealth {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return ReplicaHealth{
		Healthy:      m.Healthy,
		Requests:     m.requests,
		Errors:       m.errors,
		LastUpdate:   time.Now(),
		ErrorRate:    float64(m.errors) / float64(m.requests),
	}
}

// Reset resets the mock replica state
func (m *MockReplica) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests = 0
	m.errors = 0
	m.Healthy = true
}

// ReplicaHealth represents replica health status
type ReplicaHealth struct {
	Healthy    bool
	Requests   int
	Errors     int
	LastUpdate time.Time
	ErrorRate  float64
}

// MockDiscovery simulates service discovery for testing
type MockDiscovery struct {
	Replicas map[string]*MockReplica
	mu       sync.RWMutex
}

// NewMockDiscovery creates a new mock discovery
func NewMockDiscovery() *MockDiscovery {
	return &MockDiscovery{
		Replicas: make(map[string]*MockReplica),
	}
}

// AddReplica adds a replica to discovery
func (m *MockDiscovery) AddReplica(replica *MockReplica) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Replicas[replica.ID] = replica
}

// RemoveReplica removes a replica from discovery
func (m *MockDiscovery) RemoveReplica(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.Replicas, id)
}

// GetReplicas returns all healthy replicas
func (m *MockDiscovery) GetReplicas(ctx context.Context) ([]*MockReplica, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var healthy []*MockReplica
	for _, r := range m.Replicas {
		if r.Health().Healthy {
			healthy = append(healthy, r)
		}
	}
	return healthy, nil
}

// Test Helpers

// AssertEqual is a test helper for equality assertion
func AssertEqual(t interface{ Errorf(string, ...interface{}) }, expected, actual interface{}, msg string) {
	if expected != actual {
		t.Errorf("%s: expected %v, got %v", msg, expected, actual)
	}
}

// AssertTrue is a test helper for boolean assertion
func AssertTrue(t interface{ Errorf(string, ...interface{}) }, condition bool, msg string) {
	if !condition {
		t.Errorf("%s: expected true, got false", msg)
	}
}

// AssertNil is a test helper for nil assertion
func AssertNil(t interface{ Errorf(string, ...interface{}) }, value interface{}, msg string) {
	if value != nil {
		t.Errorf("%s: expected nil, got %v", msg, value)
	}
}

// AssertNotNil is a test helper for not-nil assertion
func AssertNotNil(t interface{ Errorf(string, ...interface{}) }, value interface{}, msg string) {
	if value == nil {
		t.Errorf("%s: expected not nil, got nil", msg)
	}
}
