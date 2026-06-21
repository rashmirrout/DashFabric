package dpuabstraction

import (
	"context"
	"time"

	gm "github.com/dashfabric/fm/pkg/gm"
)

// Plugin defines the interface for DPU vendor plugins
type Plugin interface {
	// Program applies goal state to device
	Program(ctx context.Context, goal *gm.PerENIGoalState) (*ProgramResult, error)
	// Name returns plugin name
	Name() string
	// Vendor returns vendor identifier (e.g., "intel", "nvidia")
	Vendor() string
}

// ProgramResult represents the result of a programming operation
type ProgramResult struct {
	ENI_ID         string
	Success        bool
	Error          string
	AppliedVersion int64
	Duration       time.Duration
}

// PluginDispatcher routes goals to appropriate vendor plugins
type PluginDispatcher interface {
	Dispatch(ctx context.Context, goal *gm.PerENIGoalState) (*ProgramResult, error)
}

// PluginPool manages worker pool for vendor-specific programming
type PluginPool interface {
	Submit(ctx context.Context, goal *gm.PerENIGoalState) <-chan *ProgramResult
	Shutdown(ctx context.Context) error
}

// DPUAbstractionManager orchestrates device programming
type DPUAbstractionManager interface {
	Program(ctx context.Context, goal *gm.PerENIGoalState) (*ProgramResult, error)
	Start(ctx context.Context) error
	Stop() error
	Stats() ManagerStats
}

// ManagerStats represents manager statistics
type ManagerStats struct {
	ProgramsSubmitted  int64
	ProgramsSucceeded  int64
	ProgramsFailed     int64
	WorkersPerVendor   map[string]int
	QueueDepthPerVendor map[string]int
	Uptime             time.Duration
}
