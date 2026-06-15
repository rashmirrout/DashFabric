package testutil

import (
	"sync"
	"time"
)

// MockReplica is a controllable mock replica for testing
type MockReplica struct {
	Name              string
	Address           string
	Latency           time.Duration
	ErrorRate         float64 // 0.0 to 1.0
	CrashAfterReqCount int
	recordedRequests  []MockRequest
	requestCount      int
	mu                sync.Mutex
}

// MockRequest represents a recorded request
type MockRequest struct {
	Timestamp time.Time
	Payload   []byte
	Headers   map[string]string
}

// NewMockReplica creates a new mock replica
func NewMockReplica(name, address string) *MockReplica {
	return &MockReplica{
		Name:     name,
		Address:  address,
		Latency:  0,
		ErrorRate: 0,
		recordedRequests: []MockRequest{},
	}
}

// SetLatency sets the response latency
func (m *MockReplica) SetLatency(latency time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Latency = latency
}

// SetErrorRate sets the error rate (0.0 to 1.0)
func (m *MockReplica) SetErrorRate(rate float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if rate < 0 {
		rate = 0
	}
	if rate > 1 {
		rate = 1
	}
	m.ErrorRate = rate
}

// SetCrashAfter sets crash trigger after N requests
func (m *MockReplica) SetCrashAfter(count int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.CrashAfterReqCount = count
}

// GetRecordedRequests returns all recorded requests
func (m *MockReplica) GetRecordedRequests() []MockRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	requests := make([]MockRequest, len(m.recordedRequests))
	copy(requests, m.recordedRequests)
	return requests
}

// RecordRequest records an incoming request
func (m *MockReplica) RecordRequest(payload []byte, headers map[string]string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.requestCount++

	headersCopy := make(map[string]string)
	for k, v := range headers {
		headersCopy[k] = v
	}

	m.recordedRequests = append(m.recordedRequests, MockRequest{
		Timestamp: time.Now(),
		Payload:   payload,
		Headers:   headersCopy,
	})
}

// ShouldCrash returns true if replica should crash (for chaos testing)
func (m *MockReplica) ShouldCrash() bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.CrashAfterReqCount > 0 && m.requestCount >= m.CrashAfterReqCount {
		return true
	}
	return false
}

// Reset clears all recorded requests and state
func (m *MockReplica) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.recordedRequests = []MockRequest{}
	m.requestCount = 0
	m.Latency = 0
	m.ErrorRate = 0
	m.CrashAfterReqCount = 0
}
