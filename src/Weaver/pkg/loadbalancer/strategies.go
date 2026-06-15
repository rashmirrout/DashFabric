package loadbalancer

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/dashfabric/weaver/pkg/gateway"
)

type RoundRobin struct {
	mu      sync.Mutex
	counter int64
}

func NewRoundRobin() gateway.LoadBalancer {
	return &RoundRobin{counter: 0}
}

func (rr *RoundRobin) Select(ctx context.Context, replicas []*gateway.Replica) (*gateway.Replica, error) {
	if len(replicas) == 0 {
		return nil, fmt.Errorf("no replicas available")
	}

	rr.mu.Lock()
	idx := rr.counter % int64(len(replicas))
	rr.counter++
	rr.mu.Unlock()

	return replicas[idx], nil
}

func (rr *RoundRobin) UpdateLoad(replica *gateway.Replica, delta int) {
	if replica != nil {
		atomic.AddInt64(&replica.ActiveConnections, int64(delta))
	}
}

func (rr *RoundRobin) Rebalance(replicas []*gateway.Replica) error {
	return nil
}

type LeastConnections struct {
	mu sync.RWMutex
}

func NewLeastConnections() gateway.LoadBalancer {
	return &LeastConnections{}
}

func (lc *LeastConnections) Select(ctx context.Context, replicas []*gateway.Replica) (*gateway.Replica, error) {
	if len(replicas) == 0 {
		return nil, fmt.Errorf("no replicas available")
	}

	lc.mu.RLock()
	defer lc.mu.RUnlock()

	selected := replicas[0]
	minLoad := atomic.LoadInt64(&replicas[0].ActiveConnections)

	for i := 1; i < len(replicas); i++ {
		load := atomic.LoadInt64(&replicas[i].ActiveConnections)
		if load < minLoad {
			minLoad = load
			selected = replicas[i]
		}
	}

	return selected, nil
}

func (lc *LeastConnections) UpdateLoad(replica *gateway.Replica, delta int) {
	if replica != nil {
		atomic.AddInt64(&replica.ActiveConnections, int64(delta))
	}
}

func (lc *LeastConnections) Rebalance(replicas []*gateway.Replica) error {
	return nil
}

type Weighted struct {
	mu      sync.RWMutex
	weights map[string]int64
	counter int64
}

func NewWeighted(weights map[string]int64) gateway.LoadBalancer {
	return &Weighted{weights: weights, counter: 0}
}

func (w *Weighted) Select(ctx context.Context, replicas []*gateway.Replica) (*gateway.Replica, error) {
	if len(replicas) == 0 {
		return nil, fmt.Errorf("no replicas available")
	}

	w.mu.RLock()
	defer w.mu.RUnlock()

	totalWeight := int64(0)
	for _, replica := range replicas {
		weight := w.weights[replica.Name]
		if weight == 0 {
			weight = 1
		}
		totalWeight += weight
	}

	if totalWeight == 0 {
		return replicas[0], nil
	}

	w.counter++
	target := w.counter % totalWeight

	currentWeight := int64(0)
	for _, replica := range replicas {
		weight := w.weights[replica.Name]
		if weight == 0 {
			weight = 1
		}

		if target < currentWeight+weight {
			return replica, nil
		}
		currentWeight += weight
	}

	return replicas[len(replicas)-1], nil
}

func (w *Weighted) UpdateLoad(replica *gateway.Replica, delta int) {
	if replica != nil {
		atomic.AddInt64(&replica.ActiveConnections, int64(delta))
	}
}

func (w *Weighted) Rebalance(replicas []*gateway.Replica) error {
	return nil
}

type Random struct {
	mu sync.Mutex
}

func NewRandom() gateway.LoadBalancer {
	return &Random{}
}

func (r *Random) Select(ctx context.Context, replicas []*gateway.Replica) (*gateway.Replica, error) {
	if len(replicas) == 0 {
		return nil, fmt.Errorf("no replicas available")
	}

	r.mu.Lock()
	idx := int(atomic.LoadInt64(new(int64))) % len(replicas)
	if idx < 0 {
		idx = -idx
	}
	r.mu.Unlock()

	return replicas[idx], nil
}

func (r *Random) UpdateLoad(replica *gateway.Replica, delta int) {
	if replica != nil {
		atomic.AddInt64(&replica.ActiveConnections, int64(delta))
	}
}

func (r *Random) Rebalance(replicas []*gateway.Replica) error {
	return nil
}

// ResourceAware selects replica with lowest resource usage (proxied by active connections)
type ResourceAware struct {
	mu sync.RWMutex
}

func NewResourceAware() gateway.LoadBalancer {
	return &ResourceAware{}
}

func (ra *ResourceAware) Select(ctx context.Context, replicas []*gateway.Replica) (*gateway.Replica, error) {
	if len(replicas) == 0 {
		return nil, fmt.Errorf("no replicas available")
	}

	ra.mu.RLock()
	defer ra.mu.RUnlock()

	selected := replicas[0]
	minConnections := atomic.LoadInt64(&selected.ActiveConnections)

	for _, replica := range replicas[1:] {
		connections := atomic.LoadInt64(&replica.ActiveConnections)
		if connections < minConnections {
			selected = replica
			minConnections = connections
		}
	}

	return selected, nil
}

func (ra *ResourceAware) UpdateLoad(replica *gateway.Replica, delta int) {
	if replica != nil {
		atomic.AddInt64(&replica.ActiveConnections, int64(delta))
	}
}

func (ra *ResourceAware) Rebalance(replicas []*gateway.Replica) error {
	return nil
}

// Sticky routes same client to same replica (consistent hash-based)
type Sticky struct {
	mu       sync.RWMutex
	affinity map[string]int // clientID → replicaIndex
}

func NewSticky() gateway.LoadBalancer {
	return &Sticky{
		affinity: make(map[string]int),
	}
}

func (s *Sticky) Select(ctx context.Context, replicas []*gateway.Replica) (*gateway.Replica, error) {
	if len(replicas) == 0 {
		return nil, fmt.Errorf("no replicas available")
	}

	clientID := extractClientID(ctx)

	s.mu.RLock()
	replicaIdx, exists := s.affinity[clientID]
	s.mu.RUnlock()

	if exists && replicaIdx < len(replicas) {
		return replicas[replicaIdx], nil
	}

	// Hash-based selection for new clients
	hash := hashClient(clientID)
	replicaIdx = int(hash) % len(replicas)

	s.mu.Lock()
	s.affinity[clientID] = replicaIdx
	s.mu.Unlock()

	return replicas[replicaIdx], nil
}

func (s *Sticky) UpdateLoad(replica *gateway.Replica, delta int) {
	if replica != nil {
		atomic.AddInt64(&replica.ActiveConnections, int64(delta))
	}
}

func (s *Sticky) Rebalance(replicas []*gateway.Replica) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Clear affinity on topology changes
	s.affinity = make(map[string]int)
	return nil
}

// extractClientID extracts client identifier from context or request headers
func extractClientID(ctx context.Context) string {
	if clientID, ok := ctx.Value("client_id").(string); ok {
		return clientID
	}
	return "unknown"
}

// hashClient computes consistent hash for client
func hashClient(clientID string) uint64 {
	hash := uint64(5381)
	for _, ch := range clientID {
		hash = ((hash << 5) + hash) + uint64(ch)
	}
	return hash
}

// CustomStrategy allows user-defined load balancing via plugin
type CustomStrategy struct {
	selectFn func(ctx context.Context, replicas []*gateway.Replica) (*gateway.Replica, error)
	updateFn func(replica *gateway.Replica, delta int)
	mu       sync.RWMutex
}

func NewCustomStrategy(
	selectFn func(ctx context.Context, replicas []*gateway.Replica) (*gateway.Replica, error),
	updateFn func(replica *gateway.Replica, delta int),
) gateway.LoadBalancer {
	return &CustomStrategy{
		selectFn: selectFn,
		updateFn: updateFn,
	}
}

func (cs *CustomStrategy) Select(ctx context.Context, replicas []*gateway.Replica) (*gateway.Replica, error) {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	if cs.selectFn == nil {
		return nil, fmt.Errorf("custom select function not defined")
	}
	return cs.selectFn(ctx, replicas)
}

func (cs *CustomStrategy) UpdateLoad(replica *gateway.Replica, delta int) {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	if cs.updateFn != nil {
		cs.updateFn(replica, delta)
	}
}

func (cs *CustomStrategy) Rebalance(replicas []*gateway.Replica) error {
	return nil
}
