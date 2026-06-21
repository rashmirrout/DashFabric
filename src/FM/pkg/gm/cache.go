package goalstatemanagement

import (
	"sync"
)

// GoalStateCacheImpl implements thread-safe fingerprint-based caching
type GoalStateCacheImpl struct {
	mu    sync.RWMutex
	cache map[string]*PerENIGoalState
	stats CacheStats
}

// CacheStats tracks cache performance
type CacheStats struct {
	Size    int
	Hits    int64
	Misses  int64
	Evicted int64
}

// NewGoalStateCache creates a new goal state cache
func NewGoalStateCache() GoalStateCache {
	return &GoalStateCacheImpl{
		cache: make(map[string]*PerENIGoalState),
		stats: CacheStats{},
	}
}

// Get retrieves a goal state by fingerprint
func (c *GoalStateCacheImpl) Get(fingerprint string) (*PerENIGoalState, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	state, exists := c.cache[fingerprint]
	if exists {
		c.stats.Hits++
		return state, true
	}
	c.stats.Misses++
	return nil, false
}

// Set stores a goal state by fingerprint
func (c *GoalStateCacheImpl) Set(fingerprint string, state *PerENIGoalState) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.cache[fingerprint] = state
	c.stats.Size = len(c.cache)
}

// Clear removes all entries from the cache
func (c *GoalStateCacheImpl) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.stats.Evicted += int64(len(c.cache))
	c.cache = make(map[string]*PerENIGoalState)
	c.stats.Size = 0
}

// Stats returns cache statistics
func (c *GoalStateCacheImpl) Stats() CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.stats
}
