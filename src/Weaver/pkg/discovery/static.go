package discovery

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// StaticDiscovery implements Discovery with a static replica list
type StaticDiscovery struct {
	replicas []*ReplicaInfo
	changes  chan []*ReplicaInfo
	mu       sync.RWMutex
	done     chan struct{}
}

// NewStaticDiscovery creates a new static discovery service
func NewStaticDiscovery(addresses []string) *StaticDiscovery {
	replicas := make([]*ReplicaInfo, len(addresses))
	for i, addr := range addresses {
		replicas[i] = &ReplicaInfo{
			Name:    fmt.Sprintf("replica-%d", i+1),
			Address: addr,
			Healthy: true,
		}
	}

	return &StaticDiscovery{
		replicas: replicas,
		changes:  make(chan []*ReplicaInfo, 1),
		done:     make(chan struct{}),
	}
}

// Discover returns current replica list
func (s *StaticDiscovery) Discover(ctx context.Context) ([]*ReplicaInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	replicas := make([]*ReplicaInfo, len(s.replicas))
	copy(replicas, s.replicas)
	return replicas, nil
}

// Watch sends replica list whenever it changes
func (s *StaticDiscovery) Watch(ctx context.Context) <-chan []*ReplicaInfo {
	resChan := make(chan []*ReplicaInfo, 1)

	go func() {
		defer close(resChan)

		// Send initial replicas
		initial := s.getCurrentReplicas()
		select {
		case resChan <- initial:
		case <-ctx.Done():
			return
		case <-s.done:
			return
		}

		// For static discovery, no changes expected
		// But keep listening for manual updates via Watch
		<-ctx.Done()
	}()

	return resChan
}

// UpdateReplicas updates the replica list (for testing/admin)
func (s *StaticDiscovery) UpdateReplicas(addresses []string) {
	s.mu.Lock()
	replicas := make([]*ReplicaInfo, len(addresses))
	for i, addr := range addresses {
		replicas[i] = &ReplicaInfo{
			Name:    fmt.Sprintf("replica-%d", i+1),
			Address: addr,
			Healthy: true,
		}
	}
	s.replicas = replicas
	s.mu.Unlock()

	// Send notification (non-blocking)
	select {
	case s.changes <- replicas:
	default:
	}
}

// Close closes the discovery service
func (s *StaticDiscovery) Close() error {
	close(s.done)
	return nil
}

// getCurrentReplicas returns a copy of current replicas
func (s *StaticDiscovery) getCurrentReplicas() []*ReplicaInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	replicas := make([]*ReplicaInfo, len(s.replicas))
	copy(replicas, s.replicas)
	return replicas
}

// EtcdDiscovery implements Discovery using etcd
type EtcdDiscovery struct {
	endpoints    []string
	cacheTimeout time.Duration
	replicas     []*ReplicaInfo
	changes      chan []*ReplicaInfo
	mu           sync.RWMutex
	done         chan struct{}
	lastUpdate   time.Time
}

// NewEtcdDiscovery creates a new etcd discovery service
func NewEtcdDiscovery(endpoints []string, cacheTimeout time.Duration) *EtcdDiscovery {
	if cacheTimeout == 0 {
		cacheTimeout = 30 * time.Second
	}

	return &EtcdDiscovery{
		endpoints:    endpoints,
		cacheTimeout: cacheTimeout,
		replicas:     make([]*ReplicaInfo, 0),
		changes:      make(chan []*ReplicaInfo, 1),
		done:         make(chan struct{}),
		lastUpdate:   time.Now(),
	}
}

// Discover returns current replica list from cache
func (e *EtcdDiscovery) Discover(ctx context.Context) ([]*ReplicaInfo, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	replicas := make([]*ReplicaInfo, len(e.replicas))
	copy(replicas, e.replicas)
	return replicas, nil
}

// Watch sends replica list from etcd with caching
func (e *EtcdDiscovery) Watch(ctx context.Context) <-chan []*ReplicaInfo {
	resChan := make(chan []*ReplicaInfo)

	go func() {
		defer close(resChan)

		ticker := time.NewTicker(e.cacheTimeout)
		defer ticker.Stop()

		for {
			// Send current replicas
			current := e.getCurrentReplicas()
			select {
			case resChan <- current:
			case <-ctx.Done():
				return
			case <-e.done:
				return
			}

			// Wait for next refresh
			select {
			case <-ticker.C:
				// Refresh from etcd (TODO: implement actual etcd client)
				continue
			case <-ctx.Done():
				return
			case <-e.done:
				return
			}
		}
	}()

	return resChan
}

// UpdateCachedReplicas updates cached replica list (for testing)
func (e *EtcdDiscovery) UpdateCachedReplicas(replicas []*ReplicaInfo) {
	e.mu.Lock()
	e.replicas = replicas
	e.lastUpdate = time.Now()
	e.mu.Unlock()

	// Send notification
	select {
	case e.changes <- replicas:
	default:
	}
}

// Close closes the discovery service
func (e *EtcdDiscovery) Close() error {
	close(e.done)
	return nil
}

// getCurrentReplicas returns a copy of current replicas
func (e *EtcdDiscovery) getCurrentReplicas() []*ReplicaInfo {
	e.mu.RLock()
	defer e.mu.RUnlock()

	replicas := make([]*ReplicaInfo, len(e.replicas))
	copy(replicas, e.replicas)
	return replicas
}
