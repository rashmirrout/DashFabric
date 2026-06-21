package dpuabstraction

import (
	"fmt"
	"sync"
)

// PluginRegistry manages vendor-to-plugin mappings
type PluginRegistry struct {
	mu      sync.RWMutex
	plugins map[string]Plugin
}

// NewPluginRegistry creates a new plugin registry
func NewPluginRegistry() *PluginRegistry {
	return &PluginRegistry{
		plugins: make(map[string]Plugin),
	}
}

// Register registers a plugin for a vendor
func (r *PluginRegistry) Register(vendor string, plugin Plugin) error {
	if vendor == "" {
		return fmt.Errorf("vendor name required")
	}
	if plugin == nil {
		return fmt.Errorf("plugin required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.plugins[vendor]; exists {
		return fmt.Errorf("plugin already registered for vendor: %s", vendor)
	}

	r.plugins[vendor] = plugin
	return nil
}

// Get retrieves a plugin by vendor name
func (r *PluginRegistry) Get(vendor string) (Plugin, error) {
	if vendor == "" {
		return nil, fmt.Errorf("vendor name required")
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	plugin, exists := r.plugins[vendor]
	if !exists {
		return nil, fmt.Errorf("plugin not found for vendor: %s", vendor)
	}

	return plugin, nil
}

// ListVendors returns all registered vendor names
func (r *PluginRegistry) ListVendors() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	vendors := make([]string, 0, len(r.plugins))
	for vendor := range r.plugins {
		vendors = append(vendors, vendor)
	}
	return vendors
}

// Clear removes all registered plugins
func (r *PluginRegistry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.plugins = make(map[string]Plugin)
}
