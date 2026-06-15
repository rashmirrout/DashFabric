package discovery

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"
)

// ConsulDiscovery implements Discovery using Consul service catalog
type ConsulDiscovery struct {
	serviceName string
	dataCenter  string
	endpoints   []string
	replicas    []*ReplicaInfo
	changes     chan []*ReplicaInfo
	mu          sync.RWMutex
	done        chan struct{}
	lastUpdate  time.Time
	cacheTTL    time.Duration
}

// NewConsulDiscovery creates a new Consul discovery service
func NewConsulDiscovery(serviceName, dataCenter string, consulEndpoints []string, cacheTTL time.Duration) *ConsulDiscovery {
	if cacheTTL == 0 {
		cacheTTL = 30 * time.Second
	}

	return &ConsulDiscovery{
		serviceName: serviceName,
		dataCenter:  dataCenter,
		endpoints:   consulEndpoints,
		replicas:    make([]*ReplicaInfo, 0),
		changes:     make(chan []*ReplicaInfo, 1),
		done:        make(chan struct{}),
		lastUpdate:  time.Now(),
		cacheTTL:    cacheTTL,
	}
}

// Discover returns current replica list from cache
func (c *ConsulDiscovery) Discover(ctx context.Context) ([]*ReplicaInfo, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	replicas := make([]*ReplicaInfo, len(c.replicas))
	copy(replicas, c.replicas)
	return replicas, nil
}

// Watch sends replica list with periodic updates
func (c *ConsulDiscovery) Watch(ctx context.Context) <-chan []*ReplicaInfo {
	resChan := make(chan []*ReplicaInfo, 1)

	go func() {
		defer close(resChan)

		ticker := time.NewTicker(c.cacheTTL)
		defer ticker.Stop()

		for {
			current := c.getCurrentReplicas()
			select {
			case resChan <- current:
			case <-ctx.Done():
				return
			case <-c.done:
				return
			}

			select {
			case <-ticker.C:
				// TODO: Query Consul service catalog
				continue
			case <-ctx.Done():
				return
			case <-c.done:
				return
			}
		}
	}()

	return resChan
}

// UpdateCachedReplicas updates cached replica list (for testing)
func (c *ConsulDiscovery) UpdateCachedReplicas(replicas []*ReplicaInfo) {
	c.mu.Lock()
	c.replicas = replicas
	c.lastUpdate = time.Now()
	c.mu.Unlock()

	select {
	case c.changes <- replicas:
	default:
	}
}

// Close closes the discovery service
func (c *ConsulDiscovery) Close() error {
	close(c.done)
	return nil
}

// getCurrentReplicas returns a copy of current replicas
func (c *ConsulDiscovery) getCurrentReplicas() []*ReplicaInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()

	replicas := make([]*ReplicaInfo, len(c.replicas))
	copy(replicas, c.replicas)
	return replicas
}

// KubernetesDiscovery implements Discovery using Kubernetes API
type KubernetesDiscovery struct {
	namespace  string
	labelKey   string
	labelValue string
	replicas   []*ReplicaInfo
	changes    chan []*ReplicaInfo
	mu         sync.RWMutex
	done       chan struct{}
	lastUpdate time.Time
	cacheTTL   time.Duration
}

// NewKubernetesDiscovery creates a new Kubernetes discovery service
func NewKubernetesDiscovery(namespace, labelKey, labelValue string, cacheTTL time.Duration) *KubernetesDiscovery {
	if cacheTTL == 0 {
		cacheTTL = 30 * time.Second
	}

	return &KubernetesDiscovery{
		namespace:  namespace,
		labelKey:   labelKey,
		labelValue: labelValue,
		replicas:   make([]*ReplicaInfo, 0),
		changes:    make(chan []*ReplicaInfo, 1),
		done:       make(chan struct{}),
		lastUpdate: time.Now(),
		cacheTTL:   cacheTTL,
	}
}

// Discover returns current replica list from cache
func (k *KubernetesDiscovery) Discover(ctx context.Context) ([]*ReplicaInfo, error) {
	k.mu.RLock()
	defer k.mu.RUnlock()

	replicas := make([]*ReplicaInfo, len(k.replicas))
	copy(replicas, k.replicas)
	return replicas, nil
}

// Watch sends replica list with periodic updates
func (k *KubernetesDiscovery) Watch(ctx context.Context) <-chan []*ReplicaInfo {
	resChan := make(chan []*ReplicaInfo, 1)

	go func() {
		defer close(resChan)

		ticker := time.NewTicker(k.cacheTTL)
		defer ticker.Stop()

		for {
			current := k.getCurrentReplicas()
			select {
			case resChan <- current:
			case <-ctx.Done():
				return
			case <-k.done:
				return
			}

			select {
			case <-ticker.C:
				// TODO: Query Kubernetes API for endpoints
				continue
			case <-ctx.Done():
				return
			case <-k.done:
				return
			}
		}
	}()

	return resChan
}

// UpdateCachedReplicas updates cached replica list (for testing)
func (k *KubernetesDiscovery) UpdateCachedReplicas(replicas []*ReplicaInfo) {
	k.mu.Lock()
	k.replicas = replicas
	k.lastUpdate = time.Now()
	k.mu.Unlock()

	select {
	case k.changes <- replicas:
	default:
	}
}

// Close closes the discovery service
func (k *KubernetesDiscovery) Close() error {
	close(k.done)
	return nil
}

// getCurrentReplicas returns a copy of current replicas
func (k *KubernetesDiscovery) getCurrentReplicas() []*ReplicaInfo {
	k.mu.RLock()
	defer k.mu.RUnlock()

	replicas := make([]*ReplicaInfo, len(k.replicas))
	copy(replicas, k.replicas)
	return replicas
}

// DNSSRVDiscovery implements Discovery using DNS SRV records
type DNSSRVDiscovery struct {
	service    string
	proto      string
	name       string
	replicas   []*ReplicaInfo
	changes    chan []*ReplicaInfo
	mu         sync.RWMutex
	done       chan struct{}
	lastUpdate time.Time
	cacheTTL   time.Duration
	resolver   *net.Resolver
}

// NewDNSSRVDiscovery creates a new DNS SRV discovery service
func NewDNSSRVDiscovery(service, proto, name string, cacheTTL time.Duration) *DNSSRVDiscovery {
	if cacheTTL == 0 {
		cacheTTL = 30 * time.Second
	}

	return &DNSSRVDiscovery{
		service:    service,
		proto:      proto,
		name:       name,
		replicas:   make([]*ReplicaInfo, 0),
		changes:    make(chan []*ReplicaInfo, 1),
		done:       make(chan struct{}),
		lastUpdate: time.Now(),
		cacheTTL:   cacheTTL,
		resolver:   net.DefaultResolver,
	}
}

// Discover returns current replica list from cache
func (d *DNSSRVDiscovery) Discover(ctx context.Context) ([]*ReplicaInfo, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	replicas := make([]*ReplicaInfo, len(d.replicas))
	copy(replicas, d.replicas)
	return replicas, nil
}

// Watch sends replica list with periodic updates from DNS SRV queries
func (d *DNSSRVDiscovery) Watch(ctx context.Context) <-chan []*ReplicaInfo {
	resChan := make(chan []*ReplicaInfo, 1)

	go func() {
		defer close(resChan)

		ticker := time.NewTicker(d.cacheTTL)
		defer ticker.Stop()

		for {
			current := d.getCurrentReplicas()
			select {
			case resChan <- current:
			case <-ctx.Done():
				return
			case <-d.done:
				return
			}

			select {
			case <-ticker.C:
				// Query DNS SRV
				_, targets, err := d.resolver.LookupSRV(context.Background(), d.service, d.proto, d.name)
				if err != nil {
					// Log error but continue, use cached data
					continue
				}

				replicas := make([]*ReplicaInfo, len(targets))
				for i, target := range targets {
					replicas[i] = &ReplicaInfo{
						Name:    target.Target,
						Address: fmt.Sprintf("%s:%d", target.Target, target.Port),
						Healthy: true,
					}
				}

				d.updateReplicas(replicas)

			case <-ctx.Done():
				return
			case <-d.done:
				return
			}
		}
	}()

	return resChan
}

// updateReplicas updates cached replicas
func (d *DNSSRVDiscovery) updateReplicas(replicas []*ReplicaInfo) {
	d.mu.Lock()
	d.replicas = replicas
	d.lastUpdate = time.Now()
	d.mu.Unlock()

	select {
	case d.changes <- replicas:
	default:
	}
}

// Close closes the discovery service
func (d *DNSSRVDiscovery) Close() error {
	close(d.done)
	return nil
}

// getCurrentReplicas returns a copy of current replicas
func (d *DNSSRVDiscovery) getCurrentReplicas() []*ReplicaInfo {
	d.mu.RLock()
	defer d.mu.RUnlock()

	replicas := make([]*ReplicaInfo, len(d.replicas))
	copy(replicas, d.replicas)
	return replicas
}
