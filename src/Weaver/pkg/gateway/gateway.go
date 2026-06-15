package gateway

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/dashfabric/weaver/pkg/discovery"
)

// Gateway routes requests to replicas
type Gateway struct {
	name          string
	replicas      []*Replica
	replicasMu    sync.RWMutex
	discovery     discovery.Discovery
	healthMonitor HealthMonitor
	lb            LoadBalancer
	metrics       GatewayMetrics
	state         GatewayState
	stateMu       sync.RWMutex
}

// GatewayMetrics tracks gateway-wide metrics
type GatewayMetrics struct {
	RequestsTotal   int64
	ErrorsTotal     int64
	PanicModeTime   time.Time
	LastHealthCheck time.Time
}

// Config holds gateway configuration
type Config struct {
	Name        string
	Discovery   discovery.Discovery
	HealthMon   HealthMonitor
	LoadBalancer LoadBalancer
}

// NewGateway creates a new gateway
func NewGateway(cfg Config) *Gateway {
	return &Gateway{
		name:          cfg.Name,
		discovery:     cfg.Discovery,
		healthMonitor: cfg.HealthMon,
		lb:            cfg.LoadBalancer,
		replicas:      make([]*Replica, 0),
		state:         GatewayStateReady,
	}
}

// Start initializes the gateway
func (gw *Gateway) Start(ctx context.Context) error {
	if gw.discovery == nil {
		return errors.New("discovery service required")
	}

	// Get initial replicas and convert to Replica objects
	discReplicas, err := gw.discovery.Discover(ctx)
	if err != nil {
		return err
	}

	replicas := make([]*Replica, len(discReplicas))
	for i, info := range discReplicas {
		replicas[i] = &Replica{
			Name:    info.Name,
			Address: info.Address,
			Healthy: info.Healthy,
		}
	}

	gw.replicasMu.Lock()
	gw.replicas = replicas
	gw.replicasMu.Unlock()

	// Start health monitor if provided
	if gw.healthMonitor != nil {
		gw.healthMonitor.Start(ctx)
	}

	// Watch for replica changes
	go gw.watchDiscovery(ctx)

	gw.setGatewayState(GatewayStateReady)

	return nil
}

// watchDiscovery watches for changes in replica list
func (gw *Gateway) watchDiscovery(ctx context.Context) {
	changes := gw.discovery.Watch(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case discReplicas, ok := <-changes:
			if !ok {
				return
			}

			replicas := make([]*Replica, len(discReplicas))
			for i, info := range discReplicas {
				replicas[i] = &Replica{
					Name:    info.Name,
					Address: info.Address,
					Healthy: info.Healthy,
				}
			}

			gw.replicasMu.Lock()
			gw.replicas = replicas
			gw.replicasMu.Unlock()

			// Trigger rebalancing if load balancer supports it
			if gw.lb != nil {
				gw.lb.Rebalance(replicas)
			}
		}
	}
}

// RouteRequest routes a request to an appropriate replica
func (gw *Gateway) RouteRequest(ctx context.Context, req *Request) (*Response, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Get current replicas
	gw.replicasMu.RLock()
	replicas := gw.replicas
	gw.replicasMu.RUnlock()

	if len(replicas) == 0 {
		gw.setGatewayState(GatewayStatePanic)
		return nil, errors.New("no replicas available")
	}

	// Create request context
	deadline := time.Now().Add(time.Duration(req.TimeoutMs) * time.Millisecond)
	if deadline.Before(time.Now()) {
		return nil, errors.New("request timeout")
	}

	requestCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	reqCtx := &RequestContext{
		RequestID:   req.RequestID,
		ClientIP:    req.ClientIP,
		Method:      req.Method,
		Deadline:    deadline,
		StartTime:   time.Now(),
		MaxAttempts: 3,
	}

	// Select replica and route request
	for attempt := 1; attempt <= reqCtx.MaxAttempts; attempt++ {
		reqCtx.Attempt = attempt

		// Select replica
		replica, err := gw.lb.Select(requestCtx, replicas)
		if err != nil {
			return nil, err
		}

		if replica == nil {
			return nil, errors.New("no healthy replica selected")
		}

		reqCtx.Replica = replica

		// Route to replica (TODO: implement actual RPC)
		response := &Response{
			StatusCode: 200,
			Payload:    []byte("OK"),
			LatencyMs:  1,
		}

		// Record metrics
		replica.RecordSuccess(time.Duration(response.LatencyMs) * time.Millisecond)

		return response, nil
	}

	return nil, errors.New("all retry attempts failed")
}

// GetReplicas returns current replica list
func (gw *Gateway) GetReplicas() []*Replica {
	gw.replicasMu.RLock()
	defer gw.replicasMu.RUnlock()

	replicas := make([]*Replica, len(gw.replicas))
	copy(replicas, gw.replicas)
	return replicas
}

// SetReplicas sets the replica list (for testing)
func (gw *Gateway) SetReplicas(replicas []*Replica) {
	gw.replicasMu.Lock()
	defer gw.replicasMu.Unlock()
	gw.replicas = replicas
}

// GetState returns current gateway state
func (gw *Gateway) GetState() GatewayState {
	gw.stateMu.RLock()
	defer gw.stateMu.RUnlock()
	return gw.state
}

// setGatewayState sets gateway state
func (gw *Gateway) setGatewayState(state GatewayState) {
	gw.stateMu.Lock()
	defer gw.stateMu.Unlock()
	gw.state = state
}

// Stop stops the gateway
func (gw *Gateway) Stop(ctx context.Context) error {
	if gw.healthMonitor != nil {
		gw.healthMonitor.Stop()
	}

	if gw.discovery != nil {
		gw.discovery.Close()
	}

	return nil
}
