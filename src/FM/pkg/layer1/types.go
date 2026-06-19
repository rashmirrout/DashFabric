package layer1

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"
)

// Event represents an incoming device event from ControlBroker
type Event struct {
	ID        string            `json:"id"`
	VnetID    string            `json:"vnet_id"`
	Type      string            `json:"type"`
	Timestamp time.Time         `json:"timestamp"`
	Payload   map[string]interface{} `json:"payload"`
	Fingerprint string `json:"-"`  // SHA256 hash (computed)
}

// ComputeFingerprint computes SHA256 hash of the event payload
func (e *Event) ComputeFingerprint() string {
	// Canonical serialization: sort keys, hash payload consistently
	data := canonicalJSON(e.Payload)
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}

// SubscriptionConfig configures CB subscription
type SubscriptionConfig struct {
	ControlBrokerAddr string        // "etcd:2379"
	Topics            []string      // Topics to subscribe to
	MaxRetries        int           // Max subscription retries
	RetryBackoff      time.Duration // Initial backoff: 100ms
	MaxBackoff        time.Duration // Max backoff: 30s
}

// DeduplicationConfig configures dedup cache
type DeduplicationConfig struct {
	CacheSize  int           // LRU cache max entries (default 10000)
	TTL        time.Duration // Cache entry TTL (default 5 minutes)
	EvictionPolicy string     // "lru" or "lfu"
}

// Subscriber interface for receiving events from CB
type Subscriber interface {
	Subscribe(ctx context.Context, topics []string) (<-chan Event, error)
	Close() error
}

// DedupCache interface for deduplication logic
type DedupCache interface {
	// CheckAndStore returns true if event is duplicate (cache hit)
	CheckAndStore(fingerprint string) bool

	// Size returns current cache size
	Size() int

	// Stats returns cache statistics
	Stats() CacheStats

	// Clear clears the cache
	Clear()
}

// CacheStats represents cache statistics
type CacheStats struct {
	Size      int     `json:"size"`
	Capacity  int     `json:"capacity"`
	Hits      int64   `json:"hits"`
	Misses    int64   `json:"misses"`
	Evictions int64   `json:"evictions"`
	HitRate   float64 `json:"hit_rate"`
}

// Layer1 represents the Config Plane layer
type Layer1 interface {
	// Start begins event subscription and deduplication
	Start(ctx context.Context) error

	// Stop gracefully stops the layer
	Stop() error

	// GetEvents returns a channel of deduplicated events
	GetEvents() <-chan Event

	// GetStats returns layer statistics
	GetStats() Layer1Stats
}

// Layer1Stats represents Layer 1 statistics
type Layer1Stats struct {
	EventsReceived   int64
	EventsForwarded  int64
	EventsDropped    int64
	DedupCacheStats  CacheStats
	Uptime           time.Duration
}

// Internal helper: canonical JSON encoding (sorted keys for consistent hashing)
func canonicalJSON(data map[string]interface{}) string {
	// Simplified implementation; production would use stable serialization
	// For now, use a deterministic ordering
	var result string
	for _, k := range sortedKeys(data) {
		v := data[k]
		result += k + ":" + interfaceToString(v) + ";"
	}
	return result
}

// sortedKeys returns sorted keys from a map
func sortedKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// In production, use sort.Strings(keys)
	return keys
}

// interfaceToString converts interface{} to string (simplified)
func interfaceToString(v interface{}) string {
	switch val := v.(type) {
	case string:
		return val
	case float64:
		return formatFloat(val)
	case bool:
		return formatBool(val)
	default:
		return ""
	}
}

func formatFloat(f float64) string {
	// Implementation would use strconv
	return ""
}

func formatBool(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
