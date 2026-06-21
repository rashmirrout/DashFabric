package dpuabstraction

import (
	"context"
	"fmt"

	gm "github.com/dashfabric/fm/pkg/gm"
)

// CustomPlugin implements the Plugin interface for Custom/Generic vendor
type CustomPlugin struct {
	name string
}

// NewCustomPlugin creates a new Custom vendor plugin
func NewCustomPlugin() Plugin {
	return &CustomPlugin{
		name: "Custom",
	}
}

// Program programs the DPU device for the Custom vendor
func (p *CustomPlugin) Program(ctx context.Context, goal *gm.PerENIGoalState) (*ProgramResult, error) {
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("programming cancelled: %w", ctx.Err())
	default:
	}

	if goal == nil {
		return nil, fmt.Errorf("goal state required")
	}

	defaultLogger.Info("programming DPU device",
		"eni_id", goal.ENI_ID,
		"version", goal.Version,
		"fingerprint", goal.Fingerprint,
	)

	// Log goal state configuration
	defaultLogger.Debug("goal state configuration",
		"eni_id", goal.ENI_ID,
		"routes", len(goal.Routes),
		"acls", len(goal.ACLs),
		"vips", len(goal.VIPMappings),
	)

	// Stub implementation: return success
	result := &ProgramResult{
		ENI_ID:         goal.ENI_ID,
		Success:        true,
		Error:          "",
		AppliedVersion: goal.Version,
	}

	defaultLogger.Info("DPU device programmed successfully",
		"eni_id", goal.ENI_ID,
		"version", goal.Version,
	)

	return result, nil
}

// Name returns the vendor name
func (p *CustomPlugin) Name() string {
	return p.name
}

// Vendor returns the vendor identifier
func (p *CustomPlugin) Vendor() string {
	return "Custom"
}
