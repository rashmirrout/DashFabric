package gateway

import (
	"context"
)

// Discovery interface for finding replicas
type Discovery interface {
	Discover(ctx context.Context) ([]*Replica, error)
	Watch(ctx context.Context) <-chan []*Replica
	Close() error
}

// LoadBalancer interface for selecting replicas
type LoadBalancer interface {
	Select(ctx context.Context, replicas []*Replica) (*Replica, error)
	UpdateLoad(replica *Replica, delta int)
	Rebalance(replicas []*Replica) error
}

// HealthChecker interface for checking replica health
type HealthChecker interface {
	Check(ctx context.Context, replica *Replica) HealthCheckResult
	Close() error
}

// RateLimiter interface for rate limiting
type RateLimiter interface {
	Allow(clientID string) bool
	Reset()
}
