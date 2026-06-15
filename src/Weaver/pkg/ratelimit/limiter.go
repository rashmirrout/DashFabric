package ratelimit

import (
	"sync"
	"sync/atomic"
	"time"
)

type TokenBucket struct {
	capacity   float64
	tokens     float64
	refillRate float64
	lastRefill time.Time
	mu         sync.Mutex
}

func NewTokenBucket(capacity, refillRate float64) *TokenBucket {
	return &TokenBucket{
		capacity:   capacity,
		tokens:     capacity,
		refillRate: refillRate,
		lastRefill: time.Now(),
	}
}

func (tb *TokenBucket) Allow(tokensRequested float64) bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	elapsed := time.Since(tb.lastRefill).Seconds()
	tb.tokens = min(tb.capacity, tb.tokens+elapsed*tb.refillRate)
	tb.lastRefill = time.Now()

	if tb.tokens >= tokensRequested {
		tb.tokens -= tokensRequested
		return true
	}

	return false
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

type Dimension string

const (
	DimensionGlobal   Dimension = "global"
	DimensionPerClient Dimension = "per_client"
	DimensionPerIP    Dimension = "per_ip"
	DimensionPerTenant Dimension = "per_tenant"
)

type MultiDimensionalLimiter struct {
	mu         sync.RWMutex
	limiters   map[string]*TokenBucket
	dimensions []Dimension
	config     map[Dimension]float64
}

func NewMultiDimensionalLimiter(config map[Dimension]float64, dimensions []Dimension) *MultiDimensionalLimiter {
	return &MultiDimensionalLimiter{
		limiters:   make(map[string]*TokenBucket),
		dimensions: dimensions,
		config:     config,
	}
}

func (m *MultiDimensionalLimiter) Allow(clientID, clientIP, tenantID string) bool {
	checks := map[string]string{
		string(DimensionGlobal):    "global",
		string(DimensionPerClient): clientID,
		string(DimensionPerIP):     clientIP,
		string(DimensionPerTenant): tenantID,
	}

	for _, dim := range m.dimensions {
		if !m.allowForDimension(dim, checks[string(dim)]) {
			return false
		}
	}

	return true
}

func (m *MultiDimensionalLimiter) allowForDimension(dim Dimension, id string) bool {
	if id == "" {
		return true
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	limiter, ok := m.limiters[string(dim)+":"+id]
	if !ok {
		rate, exists := m.config[dim]
		if !exists {
			rate = 1000.0
		}
		limiter = NewTokenBucket(rate, rate/60.0)
		m.limiters[string(dim)+":"+id] = limiter
	}

	return limiter.Allow(1.0)
}

func (m *MultiDimensionalLimiter) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.limiters = make(map[string]*TokenBucket)
}

func (m *MultiDimensionalLimiter) GetStats(dim Dimension, id string) map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	key := string(dim) + ":" + id
	limiter, ok := m.limiters[key]
	if !ok {
		return map[string]interface{}{
			"dimension": string(dim),
			"id":        id,
			"capacity":  0,
			"tokens":    0,
		}
	}

	limiter.mu.Lock()
	defer limiter.mu.Unlock()

	return map[string]interface{}{
		"dimension": string(dim),
		"id":        id,
		"capacity":  limiter.capacity,
		"tokens":    limiter.tokens,
	}
}

type RateLimitCounter struct {
	exceeded int64
}

func NewRateLimitCounter() *RateLimitCounter {
	return &RateLimitCounter{exceeded: 0}
}

func (r *RateLimitCounter) RecordExceeded() {
	atomic.AddInt64(&r.exceeded, 1)
}

func (r *RateLimitCounter) GetExceeded() int64 {
	return atomic.LoadInt64(&r.exceeded)
}

func (r *RateLimitCounter) Reset() {
	atomic.StoreInt64(&r.exceeded, 0)
}
