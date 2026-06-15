package discovery

import (
	"context"
	"testing"
	"time"
)

// TestStaticDiscovery verifies static discovery
func TestStaticDiscovery(t *testing.T) {
	addresses := []string{"localhost:5051", "localhost:5052"}
	sd := NewStaticDiscovery(addresses)

	replicas, err := sd.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	if len(replicas) != 2 {
		t.Errorf("expected 2 replicas, got %d", len(replicas))
	}

	if replicas[0].Name != "replica-1" {
		t.Errorf("expected replica-1, got %s", replicas[0].Name)
	}
}

// TestStaticDiscoveryWatch verifies watch functionality
func TestStaticDiscoveryWatch(t *testing.T) {
	addresses := []string{"localhost:5051"}
	sd := NewStaticDiscovery(addresses)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	changes := sd.Watch(ctx)

	// Should get initial replicas
	select {
	case replicas := <-changes:
		if len(replicas) != 1 {
			t.Errorf("expected 1 replica, got %d", len(replicas))
		}
	case <-ctx.Done():
		t.Fatal("timeout waiting for initial replicas")
	}

	sd.Close()
}

// TestEtcdDiscovery verifies etcd discovery
func TestEtcdDiscovery(t *testing.T) {
	endpoints := []string{"localhost:2379"}
	ed := NewEtcdDiscovery(endpoints, 30*time.Second)

	// Initially should have no replicas (since etcd is not running in test)
	replicas, err := ed.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	if len(replicas) != 0 {
		t.Errorf("expected 0 replicas initially, got %d", len(replicas))
	}

	ed.Close()
}
