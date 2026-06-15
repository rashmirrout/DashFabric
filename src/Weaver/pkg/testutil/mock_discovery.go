package testutil

import (
	"context"
	"sync"
	"time"
)

// MockDiscovery is a controllable mock service discovery
type MockDiscovery struct {
	replicas     []*MockReplicaInfo
	changes      chan []*MockReplicaInfo
	etcdDownUntil time.Time // zero time = always up
	mu           sync.RWMutex
}

// MockReplicaInfo represents replica information
type MockReplicaInfo struct {
	Name    string
	Address string
}

// NewMockDiscovery creates a new mock discovery service
func NewMockDiscovery(replicas []string) *MockDiscovery {
	info := make([]*MockReplicaInfo, len(replicas))
	for i, addr := range replicas {
		info[i] = &MockReplicaInfo{
			Name:    "replica-" + string(rune(i+1)),
			Address: addr,
		}
	}

	return &MockDiscovery{
		replicas: info,
		changes:  make(chan []*MockReplicaInfo, 1),
	}
}

// GetReplicas returns current replica list
func (m *MockDiscovery) GetReplicas() []*MockReplicaInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	replicas := make([]*MockReplicaInfo, len(m.replicas))
	copy(replicas, m.replicas)
	return replicas
}

// SetReplicas updates the replica list and sends change notification
func (m *MockDiscovery) SetReplicas(replicas []*MockReplicaInfo) {
	m.mu.Lock()
	m.replicas = replicas
	m.mu.Unlock()

	// Send notification (non-blocking)
	select {
	case m.changes <- replicas:
	default:
	}
}

// ChangeReplicasAfter changes replicas after a delay
func (m *MockDiscovery) ChangeReplicasAfter(delay time.Duration, newReplicas []*MockReplicaInfo) {
	go func() {
		time.Sleep(delay)
		m.SetReplicas(newReplicas)
	}()
}

// SimulateEtcdDown simulates etcd being down for a duration
func (m *MockDiscovery) SimulateEtcdDown(duration time.Duration) {
	m.mu.Lock()
	m.etcdDownUntil = time.Now().Add(duration)
	m.mu.Unlock()

	go func() {
		time.Sleep(duration)
		m.mu.Lock()
		m.etcdDownUntil = time.Time{}
		m.mu.Unlock()
	}()
}

// IsDown returns true if etcd is currently simulated as down
func (m *MockDiscovery) IsDown() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.etcdDownUntil.IsZero() {
		return false
	}
	return time.Now().Before(m.etcdDownUntil)
}

// Watch returns channel that sends replica changes
func (m *MockDiscovery) Watch(ctx context.Context) <-chan []*MockReplicaInfo {
	replicas := m.GetReplicas()
	resChan := make(chan []*MockReplicaInfo)

	go func() {
		defer close(resChan)

		// Send initial replicas
		select {
		case resChan <- replicas:
		case <-ctx.Done():
			return
		}

		// Send changes as they occur
		for {
			select {
			case <-ctx.Done():
				return
			case change := <-m.changes:
				select {
				case resChan <- change:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return resChan
}

// Close closes the discovery service
func (m *MockDiscovery) Close() error {
	return nil
}
