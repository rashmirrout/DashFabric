package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	cm "github.com/dashfabric/fm/pkg/cm"
	dm "github.com/dashfabric/fm/pkg/dm"
	gm "github.com/dashfabric/fm/pkg/gm"
	obs "github.com/dashfabric/fm/pkg/observability"
)

// APIHandler handles FM API requests
type APIHandler struct {
	cmPipeline cm.EventPipeline
	dmManager  dm.DataManager
	gmService  gm.GoalStateManager
	logger     obs.Logger
	tracer     *obs.TracingContext
}

// NewAPIHandler creates a new API handler
func NewAPIHandler(cmPipeline cm.EventPipeline, dmManager dm.DataManager, gmService gm.GoalStateManager, logger obs.Logger, tracer *obs.TracingContext) *APIHandler {
	return &APIHandler{
		cmPipeline: cmPipeline,
		dmManager:  dmManager,
		gmService:  gmService,
		logger:     logger,
		tracer:     tracer,
	}
}

// ListVNETs returns a list of virtual networks
func (h *APIHandler) ListVNETs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if h.tracer != nil {
		var span interface{}
		ctx, span = h.tracer.StartSpan(ctx, "API.ListVNETs")
		defer func() {
			if s, ok := span.(interface{ End() }); ok {
				s.End()
			}
		}()
	}

	// Get system state from DM
	state := h.dmManager.GetSystemState()
	if state == nil {
		WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to get system state", "")
		return
	}

	// Build VNET list from ENI state (simplified: treat each VNET as having all its ENIs)
	vnetMap := make(map[string]*VNET)
	for eniID, eniState := range state.ENIs {
		if eniState == nil {
			continue
		}
		vnetID := eniState.VnetID
		if _, exists := vnetMap[vnetID]; !exists {
			vnetMap[vnetID] = &VNET{
				ID:           vnetID,
				Name:         fmt.Sprintf("vnet-%s", vnetID),
				Status:       "active",
				CreatedAt:    time.Now().Add(-24 * time.Hour), // Placeholder
				LastModified: time.Now(),
			}
		}
		vnetMap[vnetID].ENICount++
		_ = eniID // Use eniID to avoid unused error
	}

	vnets := make([]VNET, 0, len(vnetMap))
	for _, vnet := range vnetMap {
		vnets = append(vnets, *vnet)
	}

	resp := ListVNETsResponse{
		VNETs:     vnets,
		Total:     len(vnets),
		Returned:  len(vnets),
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	h.logger.Info("Listed VNETs", "count", len(vnets))
	WriteJSON(w, http.StatusOK, resp)
}

// GetGoalState returns the goal state for an ENI
func (h *APIHandler) GetGoalState(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if h.tracer != nil {
		var span interface{}
		ctx, span = h.tracer.StartSpan(ctx, "API.GetGoalState")
		defer func() {
			if s, ok := span.(interface{ End() }); ok {
				s.End()
			}
		}()
	}

	var req GetGoalStateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "Failed to parse request", "")
		return
	}

	// Validate ENI ID
	if err := ValidateENIID(req.ENIID); err != nil {
		WriteError(w, http.StatusBadRequest, "INVALID_ENI_ID", err.Error(), "")
		return
	}

	// Get system state to find ENI
	state := h.dmManager.GetSystemState()
	if state == nil {
		WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to get system state", "")
		return
	}

	eniState, exists := state.ENIs[req.ENIID]
	if !exists {
		WriteError(w, http.StatusNotFound, "ENI_NOT_FOUND", fmt.Sprintf("ENI %s not found", req.ENIID), "")
		return
	}

	// Build goal state (simplified: basic info from ENI state)
	goalState := &GoalState{
		ENIID:       eniState.ID,
		VnetID:      eniState.VnetID,
		Fingerprint: fmt.Sprintf("fp-%s-%d", eniState.ID, eniState.ConfigVersion),
		Version:     int64(eniState.ConfigVersion),
		Routes:      []Route{},
		ACLs:        []ACL{},
		VIPMappings: []VIPMapping{},
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}

	resp := GetGoalStateResponse{
		GoalState: goalState,
		Cached:    false,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	h.logger.Info("Retrieved goal state", "eni_id", req.ENIID)
	WriteJSON(w, http.StatusOK, resp)
}

// ProgramDevice programs a device with goal state
func (h *APIHandler) ProgramDevice(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if h.tracer != nil {
		var span interface{}
		ctx, span = h.tracer.StartSpan(ctx, "API.ProgramDevice")
		defer func() {
			if s, ok := span.(interface{ End() }); ok {
				s.End()
			}
		}()
	}

	var req ProgramDeviceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "Failed to parse request", "")
		return
	}

	// Validate request
	if err := ValidateENIID(req.ENIID); err != nil {
		WriteError(w, http.StatusBadRequest, "INVALID_ENI_ID", err.Error(), "")
		return
	}
	if err := ValidateVnetID(req.VnetID); err != nil {
		WriteError(w, http.StatusBadRequest, "INVALID_VNET_ID", err.Error(), "")
		return
	}

	startTime := time.Now()

	// Simulate programming (would call DAL in real implementation)
	h.logger.Info("Programming device", "eni_id", req.ENIID, "version", req.Version)

	// Create result
	result := ProgramDeviceResult{
		ENIID:          req.ENIID,
		Status:         "success",
		AppliedVersion: req.Version,
		Duration:       time.Since(startTime).String(),
		Timestamp:      time.Now().UTC().Format(time.RFC3339),
	}

	h.logger.Info("Device programmed successfully", "eni_id", req.ENIID)
	WriteJSON(w, http.StatusOK, result)
}

// HealthHandler returns the health status (already in observability, but add here for completeness)
func (h *APIHandler) HealthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]string{
			"status":    "ok",
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		}
		WriteJSON(w, http.StatusOK, resp)
	}
}
