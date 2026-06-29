package mapping

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"github.com/dashfabric/fm/pkg/registry"
	"github.com/dashfabric/fm/pkg/types"
)

// MappingState is FM's local projection of a single VIP→DIP binding.
// All fields are value types (no slices), so Clone is a plain struct
// copy — no separate allocation is required.
//
// IP addresses are netip.Addr: comparable, zero-copy, and
// self-describing on the zero value (addr.IsValid() == false).
type MappingState struct {
	// MappingID is the CB-assigned binding identifier. Validated at
	// Add time. Required.
	MappingID types.MappingID

	// VnetID is the VNET that owns this binding. Validated at Add
	// time. Required.
	VnetID types.VnetID

	// VIP is the Virtual IP presented to clients. Must be a valid,
	// non-unspecified address (not 0.0.0.0 / ::). Required.
	VIP netip.Addr

	// DIP is the Destination / Underlay IP (backend). Must be a
	// valid, non-unspecified address. Required.
	DIP netip.Addr

	// SNAT indicates whether source-NAT is active for this binding.
	SNAT bool

	// BindingTime is the wall-clock time when the binding was
	// established, as reported by the producer.
	BindingTime time.Time

	// SpecRevision is the monotonic version supplied by the producer.
	SpecRevision types.SpecRevision

	// LastUpdated is the wall-clock time the producer assembled this
	// snapshot. Readability only — use SpecRevision for ordering.
	LastUpdated time.Time
}

// Clone returns a copy of m. All fields are value types, so this is a
// plain struct copy with no heap allocations.
func (m *MappingState) Clone() *MappingState {
	if m == nil {
		return nil
	}
	c := *m
	return &c
}

// validate enforces FM-side invariants at Add time.
func (m *MappingState) validate() error {
	if m == nil {
		return errors.New("mapping: nil MappingState")
	}
	if err := types.ValidateID(string(m.MappingID)); err != nil {
		return fmt.Errorf("mapping: MappingID: %w", err)
	}
	if err := types.ValidateID(string(m.VnetID)); err != nil {
		return fmt.Errorf("mapping: VnetID: %w", err)
	}
	if !m.VIP.IsValid() {
		return errors.New("mapping: VIP is zero (unset)")
	}
	if m.VIP.IsUnspecified() {
		return fmt.Errorf("mapping: VIP %s is unspecified (0.0.0.0 / ::)", m.VIP)
	}
	if !m.DIP.IsValid() {
		return errors.New("mapping: DIP is zero (unset)")
	}
	if m.DIP.IsUnspecified() {
		return fmt.Errorf("mapping: DIP %s is unspecified (0.0.0.0 / ::)", m.DIP)
	}
	return nil
}

// MappingRegistry is the typed registry for VIP-DIP bindings.
// Composes (does not inherit) the shared registry.Registry — keeping
// the inner registry unexported so callers cannot bypass the type
// wrapper.
type MappingRegistry struct {
	inner *registry.Registry[types.MappingID, *MappingState]
}

// New returns an empty MappingRegistry.
func New() *MappingRegistry {
	return &MappingRegistry{
		inner: registry.New[types.MappingID, *MappingState]("mapping"),
	}
}

// Name returns the wrapped registry's name ("mapping").
func (r *MappingRegistry) Name() string { return r.inner.Name() }

// Acquire increments the refcount for mid and returns a channel that
// closes once a MappingState has been Added.
func (r *MappingRegistry) Acquire(ctx context.Context, mid types.MappingID) <-chan struct{} {
	return r.inner.Acquire(ctx, mid)
}

// Release decrements the refcount for mid. Underflow returns
// *registry.UnderflowError carrying REG_007_REFCOUNT_UNDERFLOW.
func (r *MappingRegistry) Release(mid types.MappingID) error {
	return r.inner.Release(mid)
}

// Add validates state and stores a copy under state.MappingID.
// Returns a validation error if state fails invariants; nothing is
// stored on error.
func (r *MappingRegistry) Add(state *MappingState) error {
	if err := state.validate(); err != nil {
		return err
	}
	r.inner.Add(state.MappingID, state.Clone())
	return nil
}

// Get returns a defensive copy of the MappingState for mid, or
// (nil, false) if no value is present.
func (r *MappingRegistry) Get(mid types.MappingID) (*MappingState, bool) {
	v, ok := r.inner.Get(mid)
	if !ok {
		return nil, false
	}
	return v.Clone(), true
}

// Ready reports whether mid has a MappingState present.
func (r *MappingRegistry) Ready(mid types.MappingID) bool { return r.inner.Ready(mid) }

// Refcount returns the current number of live Acquires for mid.
func (r *MappingRegistry) Refcount(mid types.MappingID) int64 { return r.inner.Refcount(mid) }

// Len returns the number of mappings currently registered.
func (r *MappingRegistry) Len() int { return r.inner.Len() }

// Snapshot returns copies of every MappingState currently present.
// Because MappingState has no slice fields, each copy is a plain
// struct value.
func (r *MappingRegistry) Snapshot() map[types.MappingID]*MappingState {
	raw := r.inner.Snapshot()
	out := make(map[types.MappingID]*MappingState, len(raw))
	for k, v := range raw {
		out[k] = v.Clone()
	}
	return out
}
