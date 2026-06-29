package vnet

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/dashfabric/fm/pkg/registry"
	"github.com/dashfabric/fm/pkg/types"
)

// VnetState is FM's local projection of a VNET. The shape is owned by
// FM — the wire-format proto (cb_fm_protos/vnet) lives in the adapter
// package (Wave 2) and is translated into VnetState before insertion.
//
// Fields are exported for snapshot / audit convenience but the struct
// should be treated as immutable post-Add. Callers that need to mutate
// must Clone, mutate, and Add the new value.
type VnetState struct {
	// VnetID is the CB-assigned identifier. Validated by ValidateID
	// at Add time. Required.
	VnetID types.VnetID

	// VNI is the VXLAN network identifier carried in dataplane
	// encapsulation. 0 is reserved (= "unassigned"); validated at
	// Add time.
	VNI uint32

	// PeerVnetIDs is the sorted, deduplicated list of VNETs this
	// VNET is peered with. Stored sorted so set-diff against an
	// incoming update is O(n) merge rather than O(n²) compare.
	PeerVnetIDs []types.VnetID

	// SpecRevision is the monotonic version supplied by the producer
	// (typically the adapter). Used by reconcilers to detect stale
	// snapshots. Wave 2 will enforce non-regression here.
	SpecRevision types.SpecRevision

	// LastUpdated is the wall-clock time the producer assembled this
	// snapshot. Carried for operator readability, not used for
	// ordering (use SpecRevision for that).
	LastUpdated time.Time
}

// Clone returns a deep copy of v. Slice fields are independently
// allocated so the returned value can be mutated without touching
// registry state.
func (v *VnetState) Clone() *VnetState {
	if v == nil {
		return nil
	}
	out := *v
	if v.PeerVnetIDs != nil {
		out.PeerVnetIDs = make([]types.VnetID, len(v.PeerVnetIDs))
		copy(out.PeerVnetIDs, v.PeerVnetIDs)
	}
	return &out
}

// validate enforces the FM-side invariants every VnetState must
// satisfy on Add: ID validates, VNI is non-zero, peer IDs all
// validate, no self-peer, no duplicates after sort.
func (v *VnetState) validate() error {
	if v == nil {
		return errors.New("vnet: nil VnetState")
	}
	if err := types.ValidateID(string(v.VnetID)); err != nil {
		return fmt.Errorf("vnet: VnetID: %w", err)
	}
	if v.VNI == 0 {
		return errors.New("vnet: VNI=0 reserved (means unassigned)")
	}
	for i, p := range v.PeerVnetIDs {
		if err := types.ValidateID(string(p)); err != nil {
			return fmt.Errorf("vnet: PeerVnetIDs[%d]: %w", i, err)
		}
		if p == v.VnetID {
			return fmt.Errorf("vnet: self-peer not allowed (VnetID=%s)", v.VnetID)
		}
	}
	return nil
}

// VnetRegistry is the typed registry for VNETs. Composes (does not
// inherit) the shared registry.Registry — keeping the inner registry
// unexported so callers cannot bypass the type wrapper.
type VnetRegistry struct {
	inner *registry.Registry[types.VnetID, *VnetState]
}

// New returns an empty VnetRegistry.
func New() *VnetRegistry {
	return &VnetRegistry{
		inner: registry.New[types.VnetID, *VnetState]("vnet"),
	}
}

// Name returns the wrapped registry's name ("vnet").
func (r *VnetRegistry) Name() string { return r.inner.Name() }

// Acquire increments the refcount for vid and returns a channel that
// closes once a VnetState has been Added. Mirrors
// registry.Registry.Acquire.
func (r *VnetRegistry) Acquire(ctx context.Context, vid types.VnetID) <-chan struct{} {
	return r.inner.Acquire(ctx, vid)
}

// Release decrements the refcount for vid. Underflow returns
// *registry.UnderflowError carrying REG_007_REFCOUNT_UNDERFLOW;
// callers may check with errors.Is(err, registry.ErrRefcountUnderflow).
func (r *VnetRegistry) Release(vid types.VnetID) error {
	return r.inner.Release(vid)
}

// Add stores a normalised copy of state under state.VnetID. The
// PeerVnetIDs slice is sorted and deduplicated in the stored copy;
// the caller's slice is not mutated.
//
// Returns a validation error if state fails invariants; in that case
// nothing is stored.
func (r *VnetRegistry) Add(state *VnetState) error {
	if err := state.validate(); err != nil {
		return err
	}
	stored := state.Clone()
	sortDedupPeers(stored)
	r.inner.Add(stored.VnetID, stored)
	return nil
}

// Get returns a defensive copy of the VnetState for vid, or
// (nil, false) if no value is present.
//
// Returning a copy means a caller that mutates the result cannot
// corrupt the registry. The cost is one allocation per Get; if this
// shows up in profiling the API can grow a GetRef() that returns the
// raw pointer with documented immutability.
func (r *VnetRegistry) Get(vid types.VnetID) (*VnetState, bool) {
	v, ok := r.inner.Get(vid)
	if !ok {
		return nil, false
	}
	return v.Clone(), true
}

// Ready reports whether vid has a VnetState present.
func (r *VnetRegistry) Ready(vid types.VnetID) bool { return r.inner.Ready(vid) }

// Refcount returns the current number of live Acquires for vid.
func (r *VnetRegistry) Refcount(vid types.VnetID) int64 { return r.inner.Refcount(vid) }

// Len returns the number of VNETs currently registered (refcount-held
// or cold-written value cache).
func (r *VnetRegistry) Len() int { return r.inner.Len() }

// Snapshot returns deep copies of every VnetState currently present.
// The returned map and its values are independent of registry state.
func (r *VnetRegistry) Snapshot() map[types.VnetID]*VnetState {
	raw := r.inner.Snapshot()
	out := make(map[types.VnetID]*VnetState, len(raw))
	for k, v := range raw {
		out[k] = v.Clone()
	}
	return out
}

// sortDedupPeers normalises the PeerVnetIDs slice in place: lex-sort,
// then collapse runs of duplicates. The result is the canonical form
// used for set-diff in Wave 2 adapters.
func sortDedupPeers(s *VnetState) {
	if len(s.PeerVnetIDs) <= 1 {
		return
	}
	sort.Slice(s.PeerVnetIDs, func(i, j int) bool {
		return s.PeerVnetIDs[i] < s.PeerVnetIDs[j]
	})
	w := 1
	for i := 1; i < len(s.PeerVnetIDs); i++ {
		if s.PeerVnetIDs[i] != s.PeerVnetIDs[i-1] {
			s.PeerVnetIDs[w] = s.PeerVnetIDs[i]
			w++
		}
	}
	s.PeerVnetIDs = s.PeerVnetIDs[:w]
}
