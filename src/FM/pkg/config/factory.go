package config

import (
	"context"
	"fmt"
	"log"

	gm "github.com/dashfabric/fm/pkg/gm"
	dal "github.com/dashfabric/fm/pkg/dal"
)

// Logger interface for dependency injection
type Logger interface {
	Printf(format string, v ...interface{})
	Fatalf(format string, v ...interface{})
}

// SimpleLogger wraps log.Printf for basic logging
type SimpleLogger struct{}

func (l *SimpleLogger) Printf(format string, v ...interface{}) {
	log.Printf(format, v...)
}

func (l *SimpleLogger) Fatalf(format string, v ...interface{}) {
	log.Fatalf(format, v...)
}

// ServiceFactory creates and wires GM and DAL services
type ServiceFactory struct {
	config DALConfig
	logger Logger
	gm     gm.GoalStateManager
	dal    dal.DPUAbstractionManager
}

// NewServiceFactory creates a new service factory
func NewServiceFactory(cfg AppConfig, logger Logger) *ServiceFactory {
	if logger == nil {
		logger = &SimpleLogger{}
	}
	return &ServiceFactory{
		config: cfg.DAL,
		logger: logger,
	}
}

// CreateGMService creates Goal State Management service
func (sf *ServiceFactory) CreateGMService(ctx context.Context) (gm.GoalStateManager, error) {
	if sf.gm != nil {
		return sf.gm, nil
	}

	sf.logger.Printf("[ServiceFactory] Creating GM (Goal State Management) service...")

	// Create aggregator (stub - would normally fetch from DM)
	aggregator := &stubAggregator{}

	// Create composer (generates per-ENI goal states)
	composer := gm.NewGoalStateGenerator()

	// Create cache (fingerprint-based)
	cache := gm.NewGoalStateCache()

	// Create manager (orchestrator)
	manager := gm.NewGoalStateManager(aggregator, composer, cache)

	sf.gm = manager
	sf.logger.Printf("[ServiceFactory] ✓ GM service created")
	return manager, nil
}

// CreateDALService creates DPU Abstraction Layer service
func (sf *ServiceFactory) CreateDALService(ctx context.Context) (dal.DPUAbstractionManager, error) {
	if sf.dal != nil {
		return sf.dal, nil
	}

	sf.logger.Printf("[ServiceFactory] Creating DAL (DPU Abstraction Layer) service...")

	// Create plugin factory
	pluginFactory := dal.NewPluginFactory()

	// Create DPU manager with configured worker pool
	manager, err := pluginFactory.CreateDPUManager(sf.config.PoolWorkers)
	if err != nil {
		return nil, fmt.Errorf("failed to create DPU manager: %w", err)
	}

	sf.dal = manager
	sf.logger.Printf("[ServiceFactory] ✓ DAL service created (workers: %d)", sf.config.PoolWorkers)
	return manager, nil
}

// CreateAllServices wires all services in dependency order
func (sf *ServiceFactory) CreateAllServices(ctx context.Context) error {
	sf.logger.Printf("[ServiceFactory] Initializing all services...")

	// GM (Goal State Management)
	if _, err := sf.CreateGMService(ctx); err != nil {
		return fmt.Errorf("failed to create GM: %w", err)
	}

	// DAL (DPU Abstraction Layer)
	if _, err := sf.CreateDALService(ctx); err != nil {
		return fmt.Errorf("failed to create DAL: %w", err)
	}

	sf.logger.Printf("[ServiceFactory] ✓ All services initialized successfully")
	return nil
}

// GetServices returns initialized services
func (sf *ServiceFactory) GetServices() (gm.GoalStateManager, dal.DPUAbstractionManager) {
	return sf.gm, sf.dal
}

// stubAggregator is a stub implementation for testing
type stubAggregator struct{}

func (sa *stubAggregator) Aggregate(ctx context.Context, vnetID string) (*gm.AggregatedState, error) {
	return &gm.AggregatedState{
		VnetID:     vnetID,
		Constructs: make([]*gm.ConstructState, 0),
		Replicas:   make([]*gm.ReplicaState, 0),
	}, nil
}

