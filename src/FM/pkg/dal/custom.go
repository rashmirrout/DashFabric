package dpuabstraction

import (
	"context"
	"fmt"
	"log"

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

	log.Printf("[CustomPlugin] Programming ENI %s (version: %d, fingerprint: %s)",
		goal.ENI_ID, goal.Version, goal.Fingerprint)

	// Log goal state configuration
	log.Printf("[CustomPlugin]   Routes: %d, ACLs: %d, VIPs: %d",
		len(goal.Routes), len(goal.ACLs), len(goal.VIPMappings))

	// Stub implementation: return success
	result := &ProgramResult{
		ENI_ID:         goal.ENI_ID,
		Success:        true,
		Error:          "",
		AppliedVersion: goal.Version,
	}

	log.Printf("[CustomPlugin] ENI %s programmed successfully (version: %d)",
		goal.ENI_ID, goal.Version)

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
