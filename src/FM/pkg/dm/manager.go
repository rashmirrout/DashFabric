package datamanagement

import (
	"context"
	"fmt"
	"sync"
	"time"

	cm "github.com/dashfabric/fm/pkg/cm"
)

// DataManager represents the Data Management orchestrator
type DataManager interface {
	// Start begins processing events from CM
	Start(ctx context.Context, eventStream <-chan cm.Event) error

	// Stop gracefully stops the manager
	Stop() error

	// ProcessEvent handles a single CM event
	ProcessEvent(ctx context.Context, event *cm.Event) error

	// GetSystemState returns a snapshot of current system state
	GetSystemState() *SystemState

	// Stats returns manager statistics
	Stats() ManagerStats
}

// DataManagerImpl implements the Data Manager orchestrator
type DataManagerImpl struct {
	ctx       context.Context
	registry  *MappingManager
	rules     []ConsistencyRule

	running   bool
	mu        sync.RWMutex
	startTime time.Time

	eventIn <-chan cm.Event
	errChan chan error
	doneCh  chan struct{}

	statsLock sync.RWMutex
	stats     ManagerStats
}

// ManagerStats represents data manager statistics
type ManagerStats struct {
	EventsProcessed        int64
	ConsistencyChecks      int64
	ConsistencyViolations  int64
	ConstructsStored       int64
	RuleEnforcements       int64
	ProcessingErrors       int64
	LastEventTime          time.Time
	Uptime                 time.Duration
}

// NewDataManager creates a new data manager orchestrator
func NewDataManager() *DataManagerImpl {
	// Initialize consistency rules
	rules := []ConsistencyRule{
		&ENIStateRule{},
		&VIPBindingRule{},
		&SNATPoolRule{},
		&RouteValidityRule{},
		&ReplicaHealthRule{},
	}

	registry := NewMappingManager()
	for _, rule := range rules {
		registry.RegisterRule(rule)
	}

	return &DataManagerImpl{
		registry: registry,
		rules:    rules,
		errChan:  make(chan error, 100),
		doneCh:   make(chan struct{}),
	}
}

// Start begins processing events from CM
func (dm *DataManagerImpl) Start(ctx context.Context, eventStream <-chan cm.Event) error {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	if dm.running {
		return fmt.Errorf("data manager already running")
	}

	dm.ctx = ctx
	dm.eventIn = eventStream
	dm.running = true
	dm.startTime = time.Now()
	dm.stats = ManagerStats{} // Reset stats

	// Spawn event processing goroutine
	go dm.handleEventLoop()

	return nil
}

// Stop gracefully stops the manager
func (dm *DataManagerImpl) Stop() error {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	if !dm.running {
		return fmt.Errorf("data manager not running")
	}

	dm.running = false

	// Signal goroutine to stop
	close(dm.doneCh)

	return nil
}

// ProcessEvent handles a single CM event
func (dm *DataManagerImpl) ProcessEvent(ctx context.Context, event *cm.Event) error {
	// Create span for event processing
	_, span := startSpan(ctx, "DM.ProcessEvent")
	defer span.End()

	if event == nil {
		return fmt.Errorf("event cannot be nil")
	}

	dm.statsLock.Lock()
	defer dm.statsLock.Unlock()

	// Extract ENI information from event (simplified: use event.ID as ENI ID)
	eni := &ENIState{
		ID:            event.ID,
		VnetID:        event.VnetID,
		Status:        "active", // Default to active
		LastUpdated:   time.Now(),
		ConfigVersion: 1, // Default version
	}

	// Store in registry
	vnetReg := dm.registry.GetOrCreateVnetRegistry(event.VnetID)
	vnetReg.AddENI(eni)
	dm.stats.ConstructsStored++
	incrementConstructsStored()

	// Update system state
	dm.registry.systemState.ENIs[eni.ID] = eni

	// Run consistency rules
	dm.stats.ConsistencyChecks++
	incrementConsistencyChecks()
	for _, rule := range dm.rules {
		if err := rule.Validate(ctx, dm.registry.systemState); err != nil {
			dm.stats.ConsistencyViolations++
			incrementConsistencyViolations()
			defaultLogger.Error("consistency violation detected",
				"rule", rule.Name(),
				"error", err.Error(),
			)

			// Try to enforce the rule
			if err := rule.Enforce(ctx, dm.registry.systemState); err != nil {
				dm.stats.RuleEnforcements++
				defaultLogger.Error("rule enforcement failed",
					"rule", rule.Name(),
					"error", err.Error(),
				)
			}
		}
	}

	dm.stats.EventsProcessed++
	incrementEventsProcessed()
	dm.stats.LastEventTime = time.Now()

	return nil
}

// GetSystemState returns a snapshot of current system state
func (dm *DataManagerImpl) GetSystemState() *SystemState {
	dm.registry.mu.RLock()
	defer dm.registry.mu.RUnlock()

	// Return the system state (in production, would deep copy)
	return dm.registry.systemState
}

// Stats returns manager statistics
func (dm *DataManagerImpl) Stats() ManagerStats {
	dm.statsLock.RLock()
	defer dm.statsLock.RUnlock()

	stats := dm.stats
	stats.Uptime = time.Since(dm.startTime)
	return stats
}

// handleEventLoop processes events from CM
func (dm *DataManagerImpl) handleEventLoop() {
	for {
		select {
		case event, ok := <-dm.eventIn:
			if !ok {
				// Channel closed
				return
			}

			if err := dm.ProcessEvent(dm.ctx, &event); err != nil {
				dm.statsLock.Lock()
				dm.stats.ProcessingErrors++
				dm.statsLock.Unlock()
				defaultLogger.Error("error processing event",
					"error", err.Error(),
				)
			}

		case <-dm.doneCh:
			// Manager stopping
			return

		case <-dm.ctx.Done():
			// Context cancelled
			return
		}
	}
}
