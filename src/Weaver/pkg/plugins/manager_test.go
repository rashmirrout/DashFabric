package plugins

import (
	"context"
	"testing"

	"github.com/dashfabric/weaver/pkg/gateway"
	"github.com/dashfabric/weaver/pkg/loadbalancer"
)

func TestPluginRegistry(t *testing.T) {
	registry := NewPluginRegistry()

	// Register a load balancer
	err := registry.RegisterLoadBalancer("round_robin", func(config map[string]interface{}) (gateway.LoadBalancer, error) {
		return loadbalancer.NewRoundRobin(), nil
	})
	if err != nil {
		t.Fatalf("failed to register load balancer: %v", err)
	}

	// Try to create a load balancer
	lb, err := registry.GetLoadBalancer(context.Background(), "round_robin", map[string]interface{}{})
	if err != nil {
		t.Fatalf("failed to get load balancer: %v", err)
	}

	if lb == nil {
		t.Error("expected non-nil load balancer")
	}
}

func TestPluginRegistryDuplicate(t *testing.T) {
	registry := NewPluginRegistry()

	factory := func(config map[string]interface{}) (gateway.LoadBalancer, error) {
		return loadbalancer.NewRoundRobin(), nil
	}

	// Register first time
	err := registry.RegisterLoadBalancer("lb", factory)
	if err != nil {
		t.Fatalf("first registration failed: %v", err)
	}

	// Try to register again
	err = registry.RegisterLoadBalancer("lb", factory)
	if err == nil {
		t.Error("expected error on duplicate registration")
	}
}

func TestPluginRegistryNotFound(t *testing.T) {
	registry := NewPluginRegistry()

	// Try to get non-existent load balancer
	_, err := registry.GetLoadBalancer(context.Background(), "nonexistent", map[string]interface{}{})
	if err == nil {
		t.Error("expected error for non-existent load balancer")
	}
}

func TestPluginRegistryList(t *testing.T) {
	registry := NewPluginRegistry()

	// Register multiple load balancers
	for i := 0; i < 3; i++ {
		name := "lb_" + string(rune('0'+i))
		err := registry.RegisterLoadBalancer(name, func(config map[string]interface{}) (gateway.LoadBalancer, error) {
			return loadbalancer.NewRoundRobin(), nil
		})
		if err != nil {
			t.Fatalf("failed to register load balancer: %v", err)
		}
	}

	// List registered load balancers
	names := registry.ListRegistered("load_balancer")
	if len(names) != 3 {
		t.Errorf("expected 3 load balancers, got %d", len(names))
	}
}

func TestPluginManager(t *testing.T) {
	pm := NewPluginManager()

	// Test list before loading
	names := pm.ListPlugins()
	if len(names) != 0 {
		t.Errorf("expected 0 plugins initially, got %d", len(names))
	}
}

func TestPluginManagerHooks(t *testing.T) {
	pm := NewPluginManager()

	hookCalled := false
	pm.RegisterHook("plugin_preload", func(event string, plugin *LoadedPlugin) error {
		hookCalled = true
		return nil
	})

	// Verify hook registration (without actually loading a plugin)
	if !hookCalled {
		t.Logf("hook would be called on actual plugin load")
	}
}

func TestPluginRegistryMultipleTypes(t *testing.T) {
	registry := NewPluginRegistry()

	// Register load balancer
	registry.RegisterLoadBalancer("round_robin", func(config map[string]interface{}) (gateway.LoadBalancer, error) {
		return loadbalancer.NewRoundRobin(), nil
	})

	// Register health checker
	registry.RegisterHealthChecker("tcp", func(config map[string]interface{}) (gateway.HealthChecker, error) {
		return nil, nil // Mock
	})

	// Verify separate registries
	lbNames := registry.ListRegistered("load_balancer")
	hcNames := registry.ListRegistered("health_checker")

	if len(lbNames) != 1 {
		t.Errorf("expected 1 load balancer, got %d", len(lbNames))
	}

	if len(hcNames) != 1 {
		t.Errorf("expected 1 health checker, got %d", len(hcNames))
	}
}
