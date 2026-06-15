package discovery

import (
	"context"
)

// ReplicaInfo holds replica information
type ReplicaInfo struct {
	Name    string
	Address string
	Healthy bool
}

// Discovery interface for discovering replicas
type Discovery interface {
	Discover(ctx context.Context) ([]*ReplicaInfo, error)
	Watch(ctx context.Context) <-chan []*ReplicaInfo
	Close() error
}

// Config holds discovery configuration
type Config map[string]interface{}

// Get retrieves a config value
func (c Config) Get(key string) string {
	val, ok := c[key]
	if !ok {
		return ""
	}
	str, ok := val.(string)
	if !ok {
		return ""
	}
	return str
}

// GetInt retrieves an int config value
func (c Config) GetInt(key string) int {
	val, ok := c[key]
	if !ok {
		return 0
	}
	switch v := val.(type) {
	case int:
		return v
	case float64:
		return int(v)
	default:
		return 0
	}
}

// GetSlice retrieves a string slice config value
func (c Config) GetSlice(key string) []string {
	val, ok := c[key]
	if !ok {
		return nil
	}
	slice, ok := val.([]string)
	if !ok {
		return nil
	}
	return slice
}
