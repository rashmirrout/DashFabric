package route

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"github.com/dashfabric/fm/pkg/registry"
	"github.com/dashfabric/fm/pkg/types"
)

// RouteGroupState is FM's local projection of an outbound-routing group.
// NextHop is a single address for Wave 1; ECMP ([]netip.Addr) is Wave 2.
// All fields are value types; Clone is a plain struct copy.
type RouteGroupState struct {
	// RouteGroupID is the CB-assigned identifier. Validated at Add. Required.
	RouteGroupID types.RouteGroupID

	// VnetID is the owning VNET. Validated at Add. Required.
	VnetID types.VnetID

	// NextHop is the next-hop IP for this route group. Must be valid
	// and non-unspecified. Required.
	NextHop netip.Addr

	// SpecRevision is the monotonic version supplied by the producer.
	SpecRevision types.SpecRevision

	// LastUpdated is the wall-clock time the producer assembled this snapshot.
	LastUpdated time.Time
}

// Clone returns a copy. All fields are value types.
func (s *RouteGroupState) Clone() *RouteGroupState {
	if s == nil {
		return nil
	}
	c := *s
	return &c
}

func (s *RouteGroupState) validate() error {
	if s == nil {
		return errors.New("route: nil RouteGroupState")
	}
	if err := types.ValidateID(string(s.RouteGroupID)); err != nil {
		return fmt.Errorf("route: RouteGroupID: %w", err)
	}
	if err := types.ValidateID(string(s.VnetID)); err != nil {
		return fmt.Errorf("route: VnetID: %w", err)
	}
	if !s.NextHop.IsValid() {
		return errors.New("route: NextHop is zero (unset)")
	}
	if s.NextHop.IsUnspecified() {
		return fmt.Errorf("route: NextHop %s is unspecified (0.0.0.0 / ::)", s.NextHop)
	}
	return nil
}

// RouteGroupRegistry is the typed registry for outbound-routing groups.
type RouteGroupRegistry struct {
	inner *registry.Registry[types.RouteGroupID, *RouteGroupState]
}

// New returns an empty RouteGroupRegistry.
func New() *RouteGroupRegistry {
	return &RouteGroupRegistry{
		inner: registry.New[types.RouteGroupID, *RouteGroupState]("route"),
	}
}

func (r *RouteGroupRegistry) Name() string { return r.inner.Name() }

func (r *RouteGroupRegistry) Acquire(ctx context.Context, id types.RouteGroupID) <-chan struct{} {
	return r.inner.Acquire(ctx, id)
}

func (r *RouteGroupRegistry) Release(id types.RouteGroupID) error {
	return r.inner.Release(id)
}

// Add validates state and stores a copy under state.RouteGroupID.
func (r *RouteGroupRegistry) Add(state *RouteGroupState) error {
	if err := state.validate(); err != nil {
		return err
	}
	r.inner.Add(state.RouteGroupID, state.Clone())
	return nil
}

// Get returns a defensive copy of the RouteGroupState for id, or (nil, false).
func (r *RouteGroupRegistry) Get(id types.RouteGroupID) (*RouteGroupState, bool) {
	v, ok := r.inner.Get(id)
	if !ok {
		return nil, false
	}
	return v.Clone(), true
}

func (r *RouteGroupRegistry) Ready(id types.RouteGroupID) bool     { return r.inner.Ready(id) }
func (r *RouteGroupRegistry) Refcount(id types.RouteGroupID) int64  { return r.inner.Refcount(id) }
func (r *RouteGroupRegistry) Len() int                              { return r.inner.Len() }

// Snapshot returns copies of every RouteGroupState currently present.
func (r *RouteGroupRegistry) Snapshot() map[types.RouteGroupID]*RouteGroupState {
	raw := r.inner.Snapshot()
	out := make(map[types.RouteGroupID]*RouteGroupState, len(raw))
	for k, v := range raw {
		out[k] = v.Clone()
	}
	return out
}
