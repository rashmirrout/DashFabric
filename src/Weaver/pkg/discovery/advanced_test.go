package discovery

import (
	"context"
	"testing"
	"time"
)

func TestConsulDiscovery(t *testing.T) {
	consul := NewConsulDiscovery("weaver", "dc1", []string{"localhost:8500"}, 30*time.Second)

	// Initially should have no replicas
	replicas, err := consul.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	if len(replicas) != 0 {
		t.Errorf("expected 0 replicas initially, got %d", len(replicas))
	}

	// Simulate service discovery
	mockReplicas := []*ReplicaInfo{
		{Name: "service-1", Address: "10.0.0.1:5000", Healthy: true},
		{Name: "service-2", Address: "10.0.0.2:5000", Healthy: true},
	}
	consul.UpdateCachedReplicas(mockReplicas)

	// Now should have replicas
	replicas, err = consul.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	if len(replicas) != 2 {
		t.Errorf("expected 2 replicas, got %d", len(replicas))
	}

	consul.Close()
}

func TestConsulDiscoveryWatch(t *testing.T) {
	consul := NewConsulDiscovery("weaver", "dc1", []string{"localhost:8500"}, 100*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	changes := consul.Watch(ctx)

	// Should get initial (empty) replicas
	select {
	case replicas := <-changes:
		if replicas == nil {
			t.Error("expected replicas, got nil")
		}
	case <-ctx.Done():
		t.Fatal("timeout waiting for initial replicas")
	}

	consul.Close()
}

func TestKubernetesDiscovery(t *testing.T) {
	k8s := NewKubernetesDiscovery("default", "app", "weaver", 30*time.Second)

	// Initially should have no replicas
	replicas, err := k8s.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	if len(replicas) != 0 {
		t.Errorf("expected 0 replicas initially, got %d", len(replicas))
	}

	// Simulate K8s endpoint discovery
	mockReplicas := []*ReplicaInfo{
		{Name: "weaver-pod-1", Address: "10.0.0.1:5000", Healthy: true},
		{Name: "weaver-pod-2", Address: "10.0.0.2:5000", Healthy: true},
		{Name: "weaver-pod-3", Address: "10.0.0.3:5000", Healthy: true},
	}
	k8s.UpdateCachedReplicas(mockReplicas)

	// Now should have replicas
	replicas, err = k8s.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	if len(replicas) != 3 {
		t.Errorf("expected 3 replicas, got %d", len(replicas))
	}

	k8s.Close()
}

func TestKubernetesDiscoveryWatch(t *testing.T) {
	k8s := NewKubernetesDiscovery("default", "app", "weaver", 100*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	changes := k8s.Watch(ctx)

	// Should get initial replicas
	select {
	case replicas := <-changes:
		if replicas == nil {
			t.Error("expected replicas, got nil")
		}
	case <-ctx.Done():
		t.Fatal("timeout waiting for initial replicas")
	}

	k8s.Close()
}

func TestDNSSRVDiscovery(t *testing.T) {
	dnsSRV := NewDNSSRVDiscovery("weaver", "tcp", "example.com", 30*time.Second)

	// Initially should have no replicas
	replicas, err := dnsSRV.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	if len(replicas) != 0 {
		t.Errorf("expected 0 replicas initially, got %d", len(replicas))
	}

	dnsSRV.Close()
}

func TestDNSSRVDiscoveryWatch(t *testing.T) {
	dnsSRV := NewDNSSRVDiscovery("weaver", "tcp", "example.com", 100*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	changes := dnsSRV.Watch(ctx)

	// Should get initial replicas
	select {
	case replicas := <-changes:
		if replicas == nil {
			t.Error("expected replicas, got nil")
		}
	case <-ctx.Done():
		t.Fatal("timeout waiting for initial replicas")
	}

	dnsSRV.Close()
}
