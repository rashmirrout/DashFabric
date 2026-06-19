package layer1

import (
	"container/list"
	"sync"
	"time"
)

// LRUCache implements DedupCache using Least Recently Used eviction
type LRUCache struct {
	maxSize int
	data    map[string]*cacheEntry
	lru     *list.List // Doubly linked list for LRU ordering
	mu      sync.RWMutex

	// Statistics
	hits      int64
	misses    int64
	evictions int64
}

// cacheEntry represents a single cache entry
type cacheEntry struct {
	fingerprint string
	timestamp   time.Time
	element     *list.Element // Back pointer to linked list element
}

// NewLRUCache creates a new LRU dedup cache
func NewLRUCache(maxSize int) *LRUCache {
	if maxSize <= 0 {
		maxSize = 10000 // Default
	}
	return &LRUCache{
		maxSize: maxSize,
		data:    make(map[string]*cacheEntry),
		lru:     list.New(),
	}
}

// CheckAndStore returns true if fingerprint is duplicate (cache hit)
// If it's a miss, stores it and returns false
func (c *LRUCache) CheckAndStore(fingerprint string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if entry, exists := c.data[fingerprint]; exists {
		// Cache hit
		c.hits++
		// Move to front (most recently used)
		c.lru.MoveToFront(entry.element)
		return true
	}

	// Cache miss - store it
	c.misses++
	entry := &cacheEntry{
		fingerprint: fingerprint,
		timestamp:   time.Now(),
	}

	// Add to front (most recently used)
	elem := c.lru.PushFront(fingerprint)
	entry.element = elem
	c.data[fingerprint] = entry

	// Evict LRU if cache full
	if c.lru.Len() > c.maxSize {
		c.evictLRU()
	}

	return false
}

// evictLRU removes the least recently used entry
// NOTE: Must be called with lock held
func (c *LRUCache) evictLRU() {
	elem := c.lru.Back() // Least recently used
	if elem != nil {
		c.lru.Remove(elem)
		delete(c.data, elem.Value.(string))
		c.evictions++
	}
}

// Size returns current cache size
func (c *LRUCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.data)
}

// Stats returns cache statistics
func (c *LRUCache) Stats() CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	total := c.hits + c.misses
	hitRate := 0.0
	if total > 0 {
		hitRate = float64(c.hits) / float64(total)
	}

	return CacheStats{
		Size:      len(c.data),
		Capacity:  c.maxSize,
		Hits:      c.hits,
		Misses:    c.misses,
		Evictions: c.evictions,
		HitRate:   hitRate,
	}
}

// Clear empties the cache
func (c *LRUCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.data = make(map[string]*cacheEntry)
	c.lru = list.New()
	c.hits = 0
	c.misses = 0
	c.evictions = 0
}
