package plugins

import (
	"context"
	"fmt"
	"plugin"
	"sync"

	"github.com/dashfabric/weaver/pkg/gateway"
)

// PluginManager manages plugin lifecycle and registration
type PluginManager struct {
	loadedPlugins map[string]*LoadedPlugin
	hooks         map[string][]PluginHook
	mu            sync.RWMutex
}

// LoadedPlugin represents a loaded plugin
type LoadedPlugin struct {
	Name   string
	Module *plugin.Plugin
	Symbol interface{}
}

// PluginHook is a callback for plugin lifecycle events
type PluginHook func(event string, plugin *LoadedPlugin) error

// PluginType defines different types of plugins
type PluginType string

const (
	PluginTypeLoadBalancer   PluginType = "load_balancer"
	PluginTypeDiscovery      PluginType = "discovery"
	PluginTypeHealthChecker  PluginType = "health_checker"
	PluginTypeAuthenticator  PluginType = "authenticator"
	PluginTypeRateLimiter    PluginType = "rate_limiter"
)

// LoadBalancerPlugin interface for custom load balancer plugins
type LoadBalancerPlugin interface {
	Name() string
	Create(config map[string]interface{}) (gateway.LoadBalancer, error)
}

// DiscoveryPlugin interface for custom discovery plugins
type DiscoveryPlugin interface {
	Name() string
	Create(config map[string]interface{}) (gateway.Discovery, error)
}

// HealthCheckerPlugin interface for custom health checker plugins
type HealthCheckerPlugin interface {
	Name() string
	Create(config map[string]interface{}) (gateway.HealthChecker, error)
}

// NewPluginManager creates a new plugin manager
func NewPluginManager() *PluginManager {
	return &PluginManager{
		loadedPlugins: make(map[string]*LoadedPlugin),
		hooks:         make(map[string][]PluginHook),
	}
}

// LoadPlugin loads a plugin from a .so file
func (pm *PluginManager) LoadPlugin(name, path string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Check if already loaded
	if _, exists := pm.loadedPlugins[name]; exists {
		return fmt.Errorf("plugin %s already loaded", name)
	}

	// Load the plugin
	p, err := plugin.Open(path)
	if err != nil {
		return fmt.Errorf("failed to load plugin %s: %w", name, err)
	}

	// Fire pre-load hooks
	if err := pm.fireHooks("plugin_preload", &LoadedPlugin{Name: name, Module: p}); err != nil {
		return err
	}

	loaded := &LoadedPlugin{
		Name:   name,
		Module: p,
	}

	pm.loadedPlugins[name] = loaded

	// Fire post-load hooks
	if err := pm.fireHooks("plugin_loaded", loaded); err != nil {
		delete(pm.loadedPlugins, name)
		return err
	}

	return nil
}

// GetLoadBalancerPlugin retrieves a load balancer plugin
func (pm *PluginManager) GetLoadBalancerPlugin(name string) (LoadBalancerPlugin, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	loaded, exists := pm.loadedPlugins[name]
	if !exists {
		return nil, fmt.Errorf("plugin %s not loaded", name)
	}

	symbol, err := loaded.Module.Lookup("LoadBalancer")
	if err != nil {
		return nil, fmt.Errorf("plugin %s missing LoadBalancer symbol: %w", name, err)
	}

	lbPlugin, ok := symbol.(LoadBalancerPlugin)
	if !ok {
		return nil, fmt.Errorf("plugin %s LoadBalancer is not of type LoadBalancerPlugin", name)
	}

	return lbPlugin, nil
}

// GetDiscoveryPlugin retrieves a discovery plugin
func (pm *PluginManager) GetDiscoveryPlugin(name string) (DiscoveryPlugin, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	loaded, exists := pm.loadedPlugins[name]
	if !exists {
		return nil, fmt.Errorf("plugin %s not loaded", name)
	}

	symbol, err := loaded.Module.Lookup("Discovery")
	if err != nil {
		return nil, fmt.Errorf("plugin %s missing Discovery symbol: %w", name, err)
	}

	discPlugin, ok := symbol.(DiscoveryPlugin)
	if !ok {
		return nil, fmt.Errorf("plugin %s Discovery is not of type DiscoveryPlugin", name)
	}

	return discPlugin, nil
}

// GetHealthCheckerPlugin retrieves a health checker plugin
func (pm *PluginManager) GetHealthCheckerPlugin(name string) (HealthCheckerPlugin, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	loaded, exists := pm.loadedPlugins[name]
	if !exists {
		return nil, fmt.Errorf("plugin %s not loaded", name)
	}

	symbol, err := loaded.Module.Lookup("HealthChecker")
	if err != nil {
		return nil, fmt.Errorf("plugin %s missing HealthChecker symbol: %w", name, err)
	}

	hcPlugin, ok := symbol.(HealthCheckerPlugin)
	if !ok {
		return nil, fmt.Errorf("plugin %s HealthChecker is not of type HealthCheckerPlugin", name)
	}

	return hcPlugin, nil
}

// RegisterHook registers a hook for plugin events
func (pm *PluginManager) RegisterHook(event string, hook PluginHook) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pm.hooks[event] = append(pm.hooks[event], hook)
}

// fireHooks fires all hooks for an event
func (pm *PluginManager) fireHooks(event string, loaded *LoadedPlugin) error {
	hooks, exists := pm.hooks[event]
	if !exists {
		return nil
	}

	for _, hook := range hooks {
		if err := hook(event, loaded); err != nil {
			return err
		}
	}

	return nil
}

// UnloadPlugin unloads a plugin
func (pm *PluginManager) UnloadPlugin(name string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	loaded, exists := pm.loadedPlugins[name]
	if !exists {
		return fmt.Errorf("plugin %s not loaded", name)
	}

	// Fire pre-unload hooks
	if err := pm.fireHooks("plugin_preunload", loaded); err != nil {
		return err
	}

	delete(pm.loadedPlugins, name)

	// Fire post-unload hooks
	if err := pm.fireHooks("plugin_unloaded", loaded); err != nil {
		return err
	}

	return nil
}

// ListPlugins lists all loaded plugins
func (pm *PluginManager) ListPlugins() []string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	names := make([]string, 0, len(pm.loadedPlugins))
	for name := range pm.loadedPlugins {
		names = append(names, name)
	}

	return names
}

// PluginRegistry provides a built-in plugin registry for plugins
type PluginRegistry struct {
	loadBalancers  map[string]func(config map[string]interface{}) (gateway.LoadBalancer, error)
	discoveries    map[string]func(config map[string]interface{}) (gateway.Discovery, error)
	healthCheckers map[string]func(config map[string]interface{}) (gateway.HealthChecker, error)
	mu             sync.RWMutex
}

// NewPluginRegistry creates a new plugin registry
func NewPluginRegistry() *PluginRegistry {
	return &PluginRegistry{
		loadBalancers:  make(map[string]func(config map[string]interface{}) (gateway.LoadBalancer, error)),
		discoveries:    make(map[string]func(config map[string]interface{}) (gateway.Discovery, error)),
		healthCheckers: make(map[string]func(config map[string]interface{}) (gateway.HealthChecker, error)),
	}
}

// RegisterLoadBalancer registers a load balancer factory
func (pr *PluginRegistry) RegisterLoadBalancer(name string, factory func(config map[string]interface{}) (gateway.LoadBalancer, error)) error {
	pr.mu.Lock()
	defer pr.mu.Unlock()

	if _, exists := pr.loadBalancers[name]; exists {
		return fmt.Errorf("load balancer %s already registered", name)
	}

	pr.loadBalancers[name] = factory
	return nil
}

// RegisterDiscovery registers a discovery factory
func (pr *PluginRegistry) RegisterDiscovery(name string, factory func(config map[string]interface{}) (gateway.Discovery, error)) error {
	pr.mu.Lock()
	defer pr.mu.Unlock()

	if _, exists := pr.discoveries[name]; exists {
		return fmt.Errorf("discovery %s already registered", name)
	}

	pr.discoveries[name] = factory
	return nil
}

// RegisterHealthChecker registers a health checker factory
func (pr *PluginRegistry) RegisterHealthChecker(name string, factory func(config map[string]interface{}) (gateway.HealthChecker, error)) error {
	pr.mu.Lock()
	defer pr.mu.Unlock()

	if _, exists := pr.healthCheckers[name]; exists {
		return fmt.Errorf("health checker %s already registered", name)
	}

	pr.healthCheckers[name] = factory
	return nil
}

// GetLoadBalancer creates a load balancer instance
func (pr *PluginRegistry) GetLoadBalancer(ctx context.Context, name string, config map[string]interface{}) (gateway.LoadBalancer, error) {
	pr.mu.RLock()
	factory, exists := pr.loadBalancers[name]
	pr.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("load balancer %s not registered", name)
	}

	return factory(config)
}

// GetDiscovery creates a discovery instance
func (pr *PluginRegistry) GetDiscovery(ctx context.Context, name string, config map[string]interface{}) (gateway.Discovery, error) {
	pr.mu.RLock()
	factory, exists := pr.discoveries[name]
	pr.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("discovery %s not registered", name)
	}

	return factory(config)
}

// GetHealthChecker creates a health checker instance
func (pr *PluginRegistry) GetHealthChecker(ctx context.Context, name string, config map[string]interface{}) (gateway.HealthChecker, error) {
	pr.mu.RLock()
	factory, exists := pr.healthCheckers[name]
	pr.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("health checker %s not registered", name)
	}

	return factory(config)
}

// ListRegistered lists all registered components of a type
func (pr *PluginRegistry) ListRegistered(componentType string) []string {
	pr.mu.RLock()
	defer pr.mu.RUnlock()

	switch componentType {
	case "load_balancer":
		names := make([]string, 0, len(pr.loadBalancers))
		for name := range pr.loadBalancers {
			names = append(names, name)
		}
		return names
	case "discovery":
		names := make([]string, 0, len(pr.discoveries))
		for name := range pr.discoveries {
			names = append(names, name)
		}
		return names
	case "health_checker":
		names := make([]string, 0, len(pr.healthCheckers))
		for name := range pr.healthCheckers {
			names = append(names, name)
		}
		return names
	default:
		return []string{}
	}
}
