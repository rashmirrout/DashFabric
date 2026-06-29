package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	cm "github.com/dashfabric/fm/pkg/cm"
	gm "github.com/dashfabric/fm/pkg/gm"
	obs "github.com/dashfabric/fm/pkg/observability"
)

// APIHandler handles FM API requests
type APIHandler struct {
	cmPipeline cm.EventPipeline
	gmService  gm.GoalStateManager
	logger     obs.Logger
	tracer     *obs.TracingContext
}

// NewAPIHandler creates a new API handler
func NewAPIHandler(cmPipeline cm.EventPipeline, gmService gm.GoalStateManager, logger obs.Logger, tracer *obs.TracingContext) *APIHandler {
	return &APIHandler{
		cmPipeline: cmPipeline,
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

	// Placeholder: In Wave 2+, would query registry for actual VNETs
	vnets := []VNET{}

	resp := ListVNETsResponse{
		VNETs:     vnets,
		Total:     0,
		Returned:  0,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	h.logger.Info("Listed VNETs", "count", 0)
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

	// Placeholder: In Wave 2+, would query registry for actual ENI state
	goalState := &GoalState{
		ENIID:       req.ENIID,
		VnetID:      "vnet-placeholder",
		Fingerprint: fmt.Sprintf("fp-%s-1", req.ENIID),
		Version:     1,
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
