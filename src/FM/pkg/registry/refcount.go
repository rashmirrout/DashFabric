package registry

import (
	"errors"
	"fmt"

	fmerrors "github.com/dashfabric/fm/pkg/errors"
)

// entry is the per-key bookkeeping record held inside a Registry.
// Field access is protected by the parent Registry's mu — entry has
// no lock of its own.
type entry[V any] struct {
	value    V
	hasValue bool          // false until first Add
	refs     int64         // number of live Acquires
	ready    chan struct{} // closed when hasValue becomes true
}

func newEntry[V any]() *entry[V] {
	return &entry[V]{ready: make(chan struct{})}
}

// UnderflowError is the typed error returned by Release when the
// refcount would go negative, or when Release is called for a key
// that is not present. Both conditions surface the canonical
// REG_007_REFCOUNT_UNDERFLOW code (Specs/FM/error-handling-design.md
// §3, runbook Specs/Runbooks/REG_007_REFCOUNT_UNDERFLOW.md).
//
// The error carries the registry name and the offending key so an
// operator reading the log can localise the bug without grepping for
// the registry instance pointer.
type UnderflowError struct {
	Code     fmerrors.Code
	Registry string
	Key      any
}

func (e *UnderflowError) Error() string {
	return fmt.Sprintf("%s: registry=%s key=%v", string(e.Code), e.Registry, e.Key)
}

// Is supports errors.Is comparison against ErrRefcountUnderflow.
// Two UnderflowErrors are considered "equal for Is purposes" when
// they share the same Code — registry name and key are diagnostic
// fields, not identity.
func (e *UnderflowError) Is(target error) bool {
	var t *UnderflowError
	if errors.As(target, &t) {
		return t.Code == e.Code
	}
	return false
}

// ErrRefcountUnderflow is a sentinel for use with errors.Is.
//
//	if errors.Is(err, registry.ErrRefcountUnderflow) { ... }
var ErrRefcountUnderflow error = &UnderflowError{Code: fmerrors.REG_007_REFCOUNT_UNDERFLOW}

func newUnderflow(name string, key any) error {
	return &UnderflowError{
		Code:     fmerrors.REG_007_REFCOUNT_UNDERFLOW,
		Registry: name,
		Key:      key,
	}
}
