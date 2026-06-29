package nic

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sort"
	"time"

	"github.com/dashfabric/fm/pkg/registry"
	"github.com/dashfabric/fm/pkg/types"
)

// NicState is FM's local projection of an ENI / NIC. The shape is
// owned by FM — the wire-format proto lives in the adapter package
// (Wave 2) and is translated into NicState before insertion.
//
// Fields are exported for snapshot / audit convenience but the struct
// should be treated as immutable post-Add. Callers that need to mutate
// must Clone, mutate, and Add the new value.
type NicState struct {
	// ENIID is the CB-assigned ENI identifier. Validated by ValidateID
	// at Add time. Required.
	ENIID types.ENIID

	// VnetID is the VNET this NIC belongs to. Validated by ValidateID
	// at Add time. Required.
	VnetID types.VnetID

	// MacAddress is the IEEE 802 MAC address in colon-separated hex
	// notation (e.g. "00:0a:95:9d:68:16"). Validated via net.ParseMAC
	// at Add time. Required.
	MacAddress string

	// AclGroupIDs is the sorted, deduplicated list of ACL groups
	// attached to this NIC. DASH supports up to 6 slots
	// (stage1+stage2, v4+v6, in+out); the Wave 1 inventory stores a
	// flat list — slot identity is added in Wave 2 when the adapter
	// carries slot context. Each ID is validated at Add time.
	AclGroupIDs []types.AclGroupID

	// RouteGroupID identifies the outbound-routing group. Zero value
	// ("") means unassigned. Validated only when non-zero.
	RouteGroupID types.RouteGroupID

	// MeterPolicyID identifies the rate-limit / billing policy. Zero
	// value ("") means unassigned. Validated only when non-zero.
	MeterPolicyID types.MeterPolicyID

	// SpecRevision is the monotonic version supplied by the producer.
	SpecRevision types.SpecRevision

	// LastUpdated is the wall-clock time the producer assembled this
	// snapshot. Readability only — use SpecRevision for ordering.
	LastUpdated time.Time
}

// Clone returns a deep copy of n. The AclGroupIDs slice is
// independently allocated so the returned value can be mutated
// without touching registry state.
func (n *NicState) Clone() *NicState {
	if n == nil {
		return nil
	}
	out := *n
	if n.AclGroupIDs != nil {
		out.AclGroupIDs = make([]types.AclGroupID, len(n.AclGroupIDs))
		copy(out.AclGroupIDs, n.AclGroupIDs)
	}
	return &out
}

// validate enforces FM-side invariants at Add time.
func (n *NicState) validate() error {
	if n == nil {
		return errors.New("nic: nil NicState")
	}
	if err := types.ValidateID(string(n.ENIID)); err != nil {
		return fmt.Errorf("nic: ENIID: %w", err)
	}
	if err := types.ValidateID(string(n.VnetID)); err != nil {
		return fmt.Errorf("nic: VnetID: %w", err)
	}
	if n.MacAddress == "" {
		return errors.New("nic: MacAddress is empty")
	}
	if _, err := net.ParseMAC(n.MacAddress); err != nil {
		return fmt.Errorf("nic: MacAddress %q: %w", n.MacAddress, err)
	}
	for i, id := range n.AclGroupIDs {
		if err := types.ValidateID(string(id)); err != nil {
			return fmt.Errorf("nic: AclGroupIDs[%d]: %w", i, err)
		}
	}
	if n.RouteGroupID != "" {
		if err := types.ValidateID(string(n.RouteGroupID)); err != nil {
			return fmt.Errorf("nic: RouteGroupID: %w", err)
		}
	}
	if n.MeterPolicyID != "" {
		if err := types.ValidateID(string(n.MeterPolicyID)); err != nil {
			return fmt.Errorf("nic: MeterPolicyID: %w", err)
		}
	}
	return nil
}

// NicRegistry is the typed registry for ENI NICs. Composes (does not
// inherit) the shared registry.Registry — keeping the inner registry
// unexported so callers cannot bypass the type wrapper.
type NicRegistry struct {
	inner *registry.Registry[types.ENIID, *NicState]
}

// New returns an empty NicRegistry.
func New() *NicRegistry {
	return &NicRegistry{
		inner: registry.New[types.ENIID, *NicState]("nic"),
	}
}

// Name returns the wrapped registry's name ("nic").
func (r *NicRegistry) Name() string { return r.inner.Name() }

// Acquire increments the refcount for eid and returns a channel that
// closes once a NicState has been Added.
func (r *NicRegistry) Acquire(ctx context.Context, eid types.ENIID) <-chan struct{} {
	return r.inner.Acquire(ctx, eid)
}

// Release decrements the refcount for eid.
func (r *NicRegistry) Release(eid types.ENIID) error {
	return r.inner.Release(eid)
}

// Add validates state, stores a normalised copy under state.ENIID, and
// sorts+deduplicates AclGroupIDs in the stored copy. The caller's
// slice is not mutated.
func (r *NicRegistry) Add(state *NicState) error {
	if err := state.validate(); err != nil {
		return err
	}
	stored := state.Clone()
	sortDedupAclGroups(stored)
	r.inner.Add(stored.ENIID, stored)
	return nil
}

// Get returns a defensive copy of the NicState for eid, or
// (nil, false) if no value is present.
func (r *NicRegistry) Get(eid types.ENIID) (*NicState, bool) {
	v, ok := r.inner.Get(eid)
	if !ok {
		return nil, false
	}
	return v.Clone(), true
}

// Ready reports whether eid has a NicState present.
func (r *NicRegistry) Ready(eid types.ENIID) bool { return r.inner.Ready(eid) }

// Refcount returns the current number of live Acquires for eid.
func (r *NicRegistry) Refcount(eid types.ENIID) int64 { return r.inner.Refcount(eid) }

// Len returns the number of NICs currently registered.
func (r *NicRegistry) Len() int { return r.inner.Len() }

// Snapshot returns deep copies of every NicState currently present.
func (r *NicRegistry) Snapshot() map[types.ENIID]*NicState {
	raw := r.inner.Snapshot()
	out := make(map[types.ENIID]*NicState, len(raw))
	for k, v := range raw {
		out[k] = v.Clone()
	}
	return out
}

// sortDedupAclGroups normalises AclGroupIDs in place: lex-sort then
// collapse duplicate runs. Canonical form for Wave 2 set-diff.
func sortDedupAclGroups(n *NicState) {
	if len(n.AclGroupIDs) <= 1 {
		return
	}
	sort.Slice(n.AclGroupIDs, func(i, j int) bool {
		return n.AclGroupIDs[i] < n.AclGroupIDs[j]
	})
	w := 1
	for i := 1; i < len(n.AclGroupIDs); i++ {
		if n.AclGroupIDs[i] != n.AclGroupIDs[i-1] {
			n.AclGroupIDs[w] = n.AclGroupIDs[i]
			w++
		}
	}
	n.AclGroupIDs = n.AclGroupIDs[:w]
}
