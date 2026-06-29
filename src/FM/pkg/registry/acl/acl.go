package acl

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/dashfabric/fm/pkg/registry"
	"github.com/dashfabric/fm/pkg/types"
)

// AclGroupState is FM's local projection of an ACL group.
// RuleCount is a Wave 1 placeholder — the full rule list lands in
// Wave 2 with the adapter. All fields are value types; Clone is a
// plain struct copy.
type AclGroupState struct {
	// AclGroupID is the CB-assigned identifier. Validated at Add. Required.
	AclGroupID types.AclGroupID

	// VnetID is the owning VNET. Validated at Add. Required.
	VnetID types.VnetID

	// RuleCount is the number of rules in this group as reported by
	// the producer. Must be ≥ 0. Wave 1 inventory only — rule objects
	// are added in Wave 2.
	RuleCount int

	// SpecRevision is the monotonic version supplied by the producer.
	SpecRevision types.SpecRevision

	// LastUpdated is the wall-clock time the producer assembled this snapshot.
	LastUpdated time.Time
}

// Clone returns a copy. All fields are value types so this is a plain
// struct copy.
func (a *AclGroupState) Clone() *AclGroupState {
	if a == nil {
		return nil
	}
	c := *a
	return &c
}

func (a *AclGroupState) validate() error {
	if a == nil {
		return errors.New("acl: nil AclGroupState")
	}
	if err := types.ValidateID(string(a.AclGroupID)); err != nil {
		return fmt.Errorf("acl: AclGroupID: %w", err)
	}
	if err := types.ValidateID(string(a.VnetID)); err != nil {
		return fmt.Errorf("acl: VnetID: %w", err)
	}
	if a.RuleCount < 0 {
		return fmt.Errorf("acl: RuleCount %d is negative", a.RuleCount)
	}
	return nil
}

// AclGroupRegistry is the typed registry for ACL groups.
type AclGroupRegistry struct {
	inner *registry.Registry[types.AclGroupID, *AclGroupState]
}

// New returns an empty AclGroupRegistry.
func New() *AclGroupRegistry {
	return &AclGroupRegistry{
		inner: registry.New[types.AclGroupID, *AclGroupState]("acl"),
	}
}

func (r *AclGroupRegistry) Name() string { return r.inner.Name() }

func (r *AclGroupRegistry) Acquire(ctx context.Context, id types.AclGroupID) <-chan struct{} {
	return r.inner.Acquire(ctx, id)
}

func (r *AclGroupRegistry) Release(id types.AclGroupID) error {
	return r.inner.Release(id)
}

// Add validates state and stores a copy under state.AclGroupID.
func (r *AclGroupRegistry) Add(state *AclGroupState) error {
	if err := state.validate(); err != nil {
		return err
	}
	r.inner.Add(state.AclGroupID, state.Clone())
	return nil
}

// Get returns a defensive copy of the AclGroupState for id, or (nil, false).
func (r *AclGroupRegistry) Get(id types.AclGroupID) (*AclGroupState, bool) {
	v, ok := r.inner.Get(id)
	if !ok {
		return nil, false
	}
	return v.Clone(), true
}

func (r *AclGroupRegistry) Ready(id types.AclGroupID) bool    { return r.inner.Ready(id) }
func (r *AclGroupRegistry) Refcount(id types.AclGroupID) int64 { return r.inner.Refcount(id) }
func (r *AclGroupRegistry) Len() int                           { return r.inner.Len() }

// Snapshot returns copies of every AclGroupState currently present.
func (r *AclGroupRegistry) Snapshot() map[types.AclGroupID]*AclGroupState {
	raw := r.inner.Snapshot()
	out := make(map[types.AclGroupID]*AclGroupState, len(raw))
	for k, v := range raw {
		out[k] = v.Clone()
	}
	return out
}
