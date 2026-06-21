package dpuabstraction

import (
	"fmt"
)

// PluginFactory provides abstract factory functions for plugin creation
type PluginFactory struct {
	registry *PluginRegistry
}

// NewPluginFactory creates a new plugin factory
func NewPluginFactory() *PluginFactory {
	return &PluginFactory{
		registry: NewPluginRegistry(),
	}
}

// Registry returns the underlying plugin registry
func (f *PluginFactory) Registry() *PluginRegistry {
	return f.registry
}

// CreatePlugin creates and registers a plugin by vendor name
func (f *PluginFactory) CreatePlugin(vendor string) (Plugin, error) {
	if vendor == "" {
		return nil, fmt.Errorf("vendor name required")
	}

	var plugin Plugin
	switch vendor {
	case "Custom":
		plugin = NewCustomPlugin()
	case "Intel":
		return nil, fmt.Errorf("Intel vendor not yet implemented")
	case "Nvidia":
		return nil, fmt.Errorf("Nvidia vendor not yet implemented")
	default:
		return nil, fmt.Errorf("unknown vendor: %s", vendor)
	}

	// Register the plugin
	if err := f.registry.Register(vendor, plugin); err != nil {
		return nil, fmt.Errorf("plugin registration failed: %w", err)
	}

	return plugin, nil
}

// InitializeDefaultPlugins registers all supported vendors
func (f *PluginFactory) InitializeDefaultPlugins() error {
	// Register Custom vendor (always available)
	if _, err := f.CreatePlugin("Custom"); err != nil {
		return fmt.Errorf("failed to initialize Custom vendor: %w", err)
	}

	return nil
}

// CreateDPUManager creates a fully configured DPU abstraction manager
func (f *PluginFactory) CreateDPUManager(poolWorkers int) (DPUAbstractionManager, error) {
	if err := f.InitializeDefaultPlugins(); err != nil {
		return nil, fmt.Errorf("plugin initialization failed: %w", err)
	}

	manager := NewDPUAbstractionManager(f.registry, poolWorkers)
	return manager, nil
}
