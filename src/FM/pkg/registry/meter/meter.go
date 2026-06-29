package meter

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/dashfabric/fm/pkg/registry"
	"github.com/dashfabric/fm/pkg/types"
)

// MeterPolicyState is FM's local projection of a rate-limit / billing
// policy. All fields are value types; Clone is a plain struct copy.
type MeterPolicyState struct {
	// MeterPolicyID is the CB-assigned identifier. Validated at Add. Required.
	MeterPolicyID types.MeterPolicyID

	// VnetID is the owning VNET. Validated at Add. Required.
	VnetID types.VnetID

	// RateBps is the sustained rate in bits-per-second. Must be > 0.
	RateBps uint64

	// BurstBps is the burst capacity in bits-per-second. 0 means
	// "same as RateBps" per the dataplane driver convention.
	BurstBps uint64

	// SpecRevision is the monotonic version supplied by the producer.
	SpecRevision types.SpecRevision

	// LastUpdated is the wall-clock time the producer assembled this snapshot.
	LastUpdated time.Time
}

// Clone returns a copy. All fields are value types.
func (m *MeterPolicyState) Clone() *MeterPolicyState {
	if m == nil {
		return nil
	}
	c := *m
	return &c
}

func (m *MeterPolicyState) validate() error {
	if m == nil {
		return errors.New("meter: nil MeterPolicyState")
	}
	if err := types.ValidateID(string(m.MeterPolicyID)); err != nil {
		return fmt.Errorf("meter: MeterPolicyID: %w", err)
	}
	if err := types.ValidateID(string(m.VnetID)); err != nil {
		return fmt.Errorf("meter: VnetID: %w", err)
	}
	if m.RateBps == 0 {
		return errors.New("meter: RateBps must be > 0")
	}
	return nil
}

// MeterPolicyRegistry is the typed registry for meter policies.
type MeterPolicyRegistry struct {
	inner *registry.Registry[types.MeterPolicyID, *MeterPolicyState]
}

// New returns an empty MeterPolicyRegistry.
func New() *MeterPolicyRegistry {
	return &MeterPolicyRegistry{
		inner: registry.New[types.MeterPolicyID, *MeterPolicyState]("meter"),
	}
}

func (r *MeterPolicyRegistry) Name() string { return r.inner.Name() }

func (r *MeterPolicyRegistry) Acquire(ctx context.Context, id types.MeterPolicyID) <-chan struct{} {
	return r.inner.Acquire(ctx, id)
}

func (r *MeterPolicyRegistry) Release(id types.MeterPolicyID) error {
	return r.inner.Release(id)
}

// Add validates state and stores a copy under state.MeterPolicyID.
func (r *MeterPolicyRegistry) Add(state *MeterPolicyState) error {
	if err := state.validate(); err != nil {
		return err
	}
	r.inner.Add(state.MeterPolicyID, state.Clone())
	return nil
}

// Get returns a defensive copy of the MeterPolicyState for id, or (nil, false).
func (r *MeterPolicyRegistry) Get(id types.MeterPolicyID) (*MeterPolicyState, bool) {
	v, ok := r.inner.Get(id)
	if !ok {
		return nil, false
	}
	return v.Clone(), true
}

func (r *MeterPolicyRegistry) Ready(id types.MeterPolicyID) bool      { return r.inner.Ready(id) }
func (r *MeterPolicyRegistry) Refcount(id types.MeterPolicyID) int64   { return r.inner.Refcount(id) }
func (r *MeterPolicyRegistry) Len() int                                { return r.inner.Len() }

// Snapshot returns copies of every MeterPolicyState currently present.
func (r *MeterPolicyRegistry) Snapshot() map[types.MeterPolicyID]*MeterPolicyState {
	raw := r.inner.Snapshot()
	out := make(map[types.MeterPolicyID]*MeterPolicyState, len(raw))
	for k, v := range raw {
		out[k] = v.Clone()
	}
	return out
}
