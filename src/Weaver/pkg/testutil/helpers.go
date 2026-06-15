package testutil

import (
	"context"
	"testing"
	"time"
)

// TestHelper provides common test utilities
type TestHelper struct {
	t       *testing.T
	cleanup []func()
}

// NewTestHelper creates a new test helper
func NewTestHelper(t *testing.T) *TestHelper {
	return &TestHelper{
		t:       t,
		cleanup: []func(){},
	}
}

// Cleanup registers a cleanup function to run at end of test
func (h *TestHelper) Cleanup(fn func()) {
	h.cleanup = append(h.cleanup, fn)
}

// Finish runs all registered cleanup functions
func (h *TestHelper) Finish() {
	for i := len(h.cleanup) - 1; i >= 0; i-- {
		h.cleanup[i]()
	}
}

// Must helpers - fail test immediately if error

// MustNot fails test if error is not nil
func (h *TestHelper) MustNot(err error, msg string) {
	if err != nil {
		h.t.Fatalf("%s: %v", msg, err)
	}
}

// Equal fails test if values are not equal
func (h *TestHelper) Equal(expected, actual interface{}, msg string) {
	if expected != actual {
		h.t.Fatalf("%s: expected %v, got %v", msg, expected, actual)
	}
}

// True fails test if condition is false
func (h *TestHelper) True(condition bool, msg string) {
	if !condition {
		h.t.Fatalf("assertion failed: %s", msg)
	}
}

// False fails test if condition is true
func (h *TestHelper) False(condition bool, msg string) {
	if condition {
		h.t.Fatalf("assertion failed: %s", msg)
	}
}

// NotNil fails test if value is nil
func (h *TestHelper) NotNil(val interface{}, msg string) {
	if val == nil {
		h.t.Fatalf("%s: expected non-nil value", msg)
	}
}

// Nil fails test if value is not nil
func (h *TestHelper) Nil(val interface{}, msg string) {
	if val != nil {
		h.t.Fatalf("%s: expected nil value, got %v", msg, val)
	}
}

// Context helpers

// ContextWithTimeout creates context with timeout
func (h *TestHelper) ContextWithTimeout(timeout time.Duration) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	h.Cleanup(func() { cancel() })
	return ctx
}

// ContextWithDeadline creates context with absolute deadline
func (h *TestHelper) ContextWithDeadline(deadline time.Time) context.Context {
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	h.Cleanup(func() { cancel() })
	return ctx
}
