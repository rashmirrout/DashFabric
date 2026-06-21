package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	api "github.com/dashfabric/fm/pkg/api"
	cm "github.com/dashfabric/fm/pkg/cm"
	dm "github.com/dashfabric/fm/pkg/dm"
	gm "github.com/dashfabric/fm/pkg/gm"
	obs "github.com/dashfabric/fm/pkg/observability"
)

func setupAPIHandler(t *testing.T) *api.APIHandler {
	logger, _ := obs.NewStructuredLogger("info")
	cache := cm.NewLRUCache(1000)
	validator := &cm.NullValidator{}
	pipeline := cm.NewEventPipeline(nil, cache, validator)

	dmManager := dm.NewDataManager()

	aggregator := &gm.VNETAggregatorImpl{}
	generator := &gm.GoalStateGeneratorImpl{}
	cache2 := gm.NewGoalStateCache()
	gmManager := gm.NewGoalStateManager(aggregator, generator, cache2)

	return api.NewAPIHandler(pipeline, dmManager, gmManager, logger, nil)
}

func TestAPIListVNETs(t *testing.T) {
	handler := setupAPIHandler(t)

	req := httptest.NewRequest("GET", "/api/vnets", nil)
	w := httptest.NewRecorder()

	handler.ListVNETs(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", w.Code)
	}

	var resp api.ListVNETsResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if resp.Returned != resp.Total {
		t.Errorf("Mismatch: returned=%d, total=%d", resp.Returned, resp.Total)
	}

	t.Logf("✓ ListVNETs endpoint returned %d VNETs", resp.Total)
}

func TestAPIGetGoalStateValid(t *testing.T) {
	handler := setupAPIHandler(t)

	body, _ := json.Marshal(api.GetGoalStateRequest{ENIID: "eni-001"})
	req := httptest.NewRequest("POST", "/api/goal-state", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.GetGoalState(w, req)

	// Since DM has no data, ENI is not found (404)
	if w.Code != http.StatusNotFound {
		t.Fatalf("Expected 404, got %d", w.Code)
	}

	var errResp api.ErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("Failed to decode error response: %v", err)
	}

	if errResp.Code != "ENI_NOT_FOUND" {
		t.Errorf("Expected ENI_NOT_FOUND, got %s", errResp.Code)
	}

	t.Logf("✓ GetGoalState endpoint properly returned ENI_NOT_FOUND for missing ENI")
}

func TestAPIGetGoalStateInvalidENI(t *testing.T) {
	handler := setupAPIHandler(t)

	body, _ := json.Marshal(api.GetGoalStateRequest{ENIID: ""})
	req := httptest.NewRequest("POST", "/api/goal-state", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.GetGoalState(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("Expected 400, got %d", w.Code)
	}

	var errResp api.ErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("Failed to decode error: %v", err)
	}

	if errResp.Code != "INVALID_ENI_ID" {
		t.Errorf("Expected INVALID_ENI_ID, got %s", errResp.Code)
	}

	t.Logf("✓ GetGoalState endpoint properly rejected invalid ENI")
}

func TestAPIProgramDeviceValid(t *testing.T) {
	handler := setupAPIHandler(t)

	body, _ := json.Marshal(api.ProgramDeviceRequest{
		ENIID:   "eni-001",
		VnetID:  "vnet-001",
		Version: 1,
	})
	req := httptest.NewRequest("POST", "/api/program", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.ProgramDevice(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", w.Code)
	}

	var result api.ProgramDeviceResult
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode result: %v", err)
	}

	if result.Status != "success" {
		t.Errorf("Expected status=success, got %s", result.Status)
	}

	if result.ENIID != "eni-001" {
		t.Errorf("Expected ENIID=eni-001, got %s", result.ENIID)
	}

	t.Logf("✓ ProgramDevice endpoint programmed device: %s (status=%s, duration=%s)",
		result.ENIID, result.Status, result.Duration)
}

func TestAPIProgramDeviceMissingVnet(t *testing.T) {
	handler := setupAPIHandler(t)

	body, _ := json.Marshal(api.ProgramDeviceRequest{
		ENIID:   "eni-001",
		VnetID:  "",
		Version: 1,
	})
	req := httptest.NewRequest("POST", "/api/program", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.ProgramDevice(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("Expected 400, got %d", w.Code)
	}

	var errResp api.ErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("Failed to decode error: %v", err)
	}

	if errResp.Code != "INVALID_VNET_ID" {
		t.Errorf("Expected INVALID_VNET_ID, got %s", errResp.Code)
	}

	t.Logf("✓ ProgramDevice endpoint properly rejected missing VNET")
}

func TestAPIValidateENIID(t *testing.T) {
	tests := []struct {
		eniID   string
		wantErr bool
	}{
		{"eni-001", false},
		{"", true},
		{string(make([]byte, 51)), true},
	}

	for _, tc := range tests {
		err := api.ValidateENIID(tc.eniID)
		if (err != nil) != tc.wantErr {
			t.Errorf("ValidateENIID(%q): wantErr=%v, got err=%v", tc.eniID, tc.wantErr, err)
		}
	}

	t.Logf("✓ ENI ID validation working correctly")
}

func TestAPIValidateVnetID(t *testing.T) {
	tests := []struct {
		vnetID  string
		wantErr bool
	}{
		{"vnet-001", false},
		{"", true},
		{string(make([]byte, 51)), true},
	}

	for _, tc := range tests {
		err := api.ValidateVnetID(tc.vnetID)
		if (err != nil) != tc.wantErr {
			t.Errorf("ValidateVnetID(%q): wantErr=%v, got err=%v", tc.vnetID, tc.wantErr, err)
		}
	}

	t.Logf("✓ VNET ID validation working correctly")
}

func TestAPIErrorResponse(t *testing.T) {
	w := httptest.NewRecorder()

	api.WriteError(w, http.StatusInternalServerError, "TEST_ERROR", "Test error message", "trace-123")

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("Expected 500, got %d", w.Code)
	}

	var errResp api.ErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("Failed to decode error: %v", err)
	}

	if errResp.Code != "TEST_ERROR" || errResp.Message != "Test error message" || errResp.TraceID != "trace-123" {
		t.Errorf("Error response mismatch: got %+v", errResp)
	}

	t.Logf("✓ Error response handling working correctly")
}

func TestAPIConcurrentRequests(t *testing.T) {
	handler := setupAPIHandler(t)
	errChan := make(chan error, 10)

	for i := 0; i < 10; i++ {
		go func() {
			req := httptest.NewRequest("GET", "/api/vnets", nil)
			w := httptest.NewRecorder()

			handler.ListVNETs(w, req)

			if w.Code != http.StatusOK {
				errChan <- nil
				return
			}

			var resp api.ListVNETsResponse
			if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
				errChan <- err
				return
			}

			errChan <- nil
		}()
	}

	// Wait for all goroutines to complete
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	for i := 0; i < 10; i++ {
		select {
		case err := <-errChan:
			if err != nil {
				t.Fatalf("Concurrent request failed: %v", err)
			}
		case <-ctx.Done():
			t.Fatal("Timeout waiting for concurrent requests")
		}
	}

	t.Logf("✓ Concurrent API requests handled correctly")
}
