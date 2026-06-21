package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// API Request/Response types

// ListVNETsRequest is the request for listing VNETs
type ListVNETsRequest struct {
	Filter map[string]string `json:"filter,omitempty"`
	Limit  int               `json:"limit,omitempty"`
	Offset int               `json:"offset,omitempty"`
}

// VNET represents a virtual network
type VNET struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Status       string    `json:"status"`
	ENICount     int       `json:"eni_count"`
	CreatedAt    time.Time `json:"created_at"`
	LastModified time.Time `json:"last_modified"`
}

// ListVNETsResponse is the response for listing VNETs
type ListVNETsResponse struct {
	VNETs      []VNET `json:"vnets"`
	Total      int    `json:"total"`
	Returned   int    `json:"returned"`
	Timestamp  string `json:"timestamp"`
	TraceID    string `json:"trace_id,omitempty"`
}

// GetGoalStateRequest is the request for getting goal state
type GetGoalStateRequest struct {
	ENIID string `json:"eni_id"`
}

// GoalState represents the desired state for an ENI
type GoalState struct {
	ENIID        string                 `json:"eni_id"`
	VnetID       string                 `json:"vnet_id"`
	Routes       []Route                `json:"routes,omitempty"`
	ACLs         []ACL                  `json:"acls,omitempty"`
	VIPMappings  []VIPMapping           `json:"vip_mappings,omitempty"`
	Fingerprint  string                 `json:"fingerprint"`
	Version      int64                  `json:"version"`
	Timestamp    string                 `json:"timestamp"`
	TraceID      string                 `json:"trace_id,omitempty"`
}

// Route represents a routing rule
type Route struct {
	Destination string `json:"destination"`
	NextHop     string `json:"next_hop"`
	Metric      int    `json:"metric,omitempty"`
}

// ACL represents an access control rule
type ACL struct {
	Direction string `json:"direction"`
	Protocol  string `json:"protocol"`
	Port      int    `json:"port,omitempty"`
	CIDR      string `json:"cidr,omitempty"`
	Action    string `json:"action"`
}

// VIPMapping represents a VIP to DIP mapping
type VIPMapping struct {
	VIP    string `json:"vip"`
	DIP    string `json:"dip"`
	Status string `json:"status"`
}

// GetGoalStateResponse is the response for getting goal state
type GetGoalStateResponse struct {
	GoalState *GoalState `json:"goal_state"`
	Cached    bool       `json:"cached"`
	Timestamp string     `json:"timestamp"`
	TraceID   string     `json:"trace_id,omitempty"`
}

// ProgramDeviceRequest is the request for programming a device
type ProgramDeviceRequest struct {
	ENIID      string `json:"eni_id"`
	VnetID     string `json:"vnet_id"`
	Version    int64  `json:"version"`
	Timeout    int    `json:"timeout,omitempty"` // seconds
}

// ProgramDeviceResult represents the result of programming a device
type ProgramDeviceResult struct {
	ENIID          string `json:"eni_id"`
	Status         string `json:"status"` // "success" or "failed"
	AppliedVersion int64  `json:"applied_version"`
	Error          string `json:"error,omitempty"`
	Duration       string `json:"duration"` // time taken
	Timestamp      string `json:"timestamp"`
	TraceID        string `json:"trace_id,omitempty"`
}

// ErrorResponse represents an error response
type ErrorResponse struct {
	Code      string    `json:"code"`
	Message   string    `json:"message"`
	Details   string    `json:"details,omitempty"`
	Timestamp string    `json:"timestamp"`
	TraceID   string    `json:"trace_id,omitempty"`
}

// Helper functions

// WriteJSON writes a JSON response
func WriteJSON(w http.ResponseWriter, statusCode int, data interface{}) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	return json.NewEncoder(w).Encode(data)
}

// WriteError writes an error response
func WriteError(w http.ResponseWriter, statusCode int, code string, message string, traceID string) error {
	errResp := ErrorResponse{
		Code:      code,
		Message:   message,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		TraceID:   traceID,
	}
	return WriteJSON(w, statusCode, errResp)
}

// ValidateENIID validates an ENI ID format
func ValidateENIID(eniID string) error {
	if eniID == "" {
		return fmt.Errorf("ENI ID cannot be empty")
	}
	if len(eniID) > 50 {
		return fmt.Errorf("ENI ID too long (max 50 characters)")
	}
	return nil
}

// ValidateVnetID validates a VNET ID format
func ValidateVnetID(vnetID string) error {
	if vnetID == "" {
		return fmt.Errorf("VNET ID cannot be empty")
	}
	if len(vnetID) > 50 {
		return fmt.Errorf("VNET ID too long (max 50 characters)")
	}
	return nil
}
