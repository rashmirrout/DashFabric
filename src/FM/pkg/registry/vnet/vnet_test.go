package vnet

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dashfabric/fm/pkg/registry"
	"github.com/dashfabric/fm/pkg/types"
)

// Round-trip Add/Get/Release preserves every scalar and slice field.
func TestAdd_Get_PreservesFields(t *testing.T) {
	r := New()
	ts := time.Date(2026, 6, 23, 10, 0, 0, 0, time.UTC)
	in := &VnetState{
		VnetID:       "vnet-1",
		VNI:          4242,
		PeerVnetIDs:  []types.VnetID{"vnet-2", "vnet-3"},
		SpecRevision: 7,
		LastUpdated:  ts,
	}
	if err := r.Add(in); err != nil {
		t.Fatalf("Add: %v", err)
	}

	got, ok := r.Get("vnet-1")
	if !ok {
		t.Fatal("Get returned ok=false after Add")
	}
	if got.VnetID != "vnet-1" || got.VNI != 4242 || got.SpecRevision != 7 || !got.LastUpdated.Equal(ts) {
		t.Errorf("scalar fields drifted: %+v", got)
	}
	if len(got.PeerVnetIDs) != 2 || got.PeerVnetIDs[0] != "vnet-2" || got.PeerVnetIDs[1] != "vnet-3" {
		t.Errorf("PeerVnetIDs = %v, want [vnet-2 vnet-3]", got.PeerVnetIDs)
	}
}

// Get returns a deep copy: mutating the returned PeerVnetIDs slice
// must not affect the registry's stored state.
func TestGet_PeerListIsDeepCopy(t *testing.T) {
	r := New()
	if err := r.Add(&VnetState{
		VnetID:      "vnet-1",
		VNI:         100,
		PeerVnetIDs: []types.VnetID{"vnet-2", "vnet-3"},
	}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	got, _ := r.Get("vnet-1")
	got.PeerVnetIDs[0] = "tampered"
	got.PeerVnetIDs = append(got.PeerVnetIDs, "vnet-9")

	again, _ := r.Get("vnet-1")
	if again.PeerVnetIDs[0] != "vnet-2" {
		t.Errorf("registry mutated via Get: PeerVnetIDs[0] = %s, want vnet-2", again.PeerVnetIDs[0])
	}
	if len(again.PeerVnetIDs) != 2 {
		t.Errorf("registry mutated via Get: len = %d, want 2", len(again.PeerVnetIDs))
	}
}

// Add must also defensive-copy from the caller's slice: mutating the
// caller's slice after Add must not corrupt the registry.
func TestAdd_CallerSliceIsCopied(t *testing.T) {
	r := New()
	peers := []types.VnetID{"vnet-2", "vnet-3"}
	if err := r.Add(&VnetState{
		VnetID:      "vnet-1",
		VNI:         1,
		PeerVnetIDs: peers,
	}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	peers[0] = "tampered"

	got, _ := r.Get("vnet-1")
	if got.PeerVnetIDs[0] != "vnet-2" {
		t.Errorf("registry mutated via caller's slice: got %s, want vnet-2", got.PeerVnetIDs[0])
	}
}

// Empty VnetID is rejected by ValidateID at the wrapper boundary;
// nothing is stored.
func TestAdd_EmptyVnetID_Rejected(t *testing.T) {
	r := New()
	err := r.Add(&VnetState{VnetID: "", VNI: 1})
	if err == nil {
		t.Fatal("Add with empty VnetID returned nil error")
	}
	if r.Len() != 0 {
		t.Errorf("Len after rejected Add = %d, want 0", r.Len())
	}
}

// VNI=0 is reserved (unassigned); Add must reject it.
func TestAdd_ZeroVNI_Rejected(t *testing.T) {
	r := New()
	err := r.Add(&VnetState{VnetID: "vnet-1", VNI: 0})
	if err == nil {
		t.Fatal("Add with VNI=0 returned nil error")
	}
	if r.Len() != 0 {
		t.Errorf("Len after rejected Add = %d, want 0", r.Len())
	}
}

// Self-peer is a producer bug: vnet-1 cannot peer with vnet-1.
func TestAdd_SelfPeer_Rejected(t *testing.T) {
	r := New()
	err := r.Add(&VnetState{
		VnetID:      "vnet-1",
		VNI:         1,
		PeerVnetIDs: []types.VnetID{"vnet-2", "vnet-1"},
	})
	if err == nil {
		t.Fatal("Add with self-peer returned nil error")
	}
	if r.Len() != 0 {
		t.Errorf("Len after rejected Add = %d, want 0", r.Len())
	}
}

// An invalid peer ID (empty / whitespace) is rejected at Add time.
func TestAdd_InvalidPeerID_Rejected(t *testing.T) {
	r := New()
	err := r.Add(&VnetState{
		VnetID:      "vnet-1",
		VNI:         1,
		PeerVnetIDs: []types.VnetID{"vnet-2", ""},
	})
	if err == nil {
		t.Fatal("Add with empty peer ID returned nil error")
	}
}

// REG_007 underflow propagates through the typed wrapper: errors.Is
// against the sentinel from pkg/registry works without unwrapping.
func TestRelease_Underflow_PropagatesREG007(t *testing.T) {
	r := New()
	err := r.Release("vnet-ghost")
	if err == nil {
		t.Fatal("Release on never-acquired key returned nil")
	}
	if !errors.Is(err, registry.ErrRefcountUnderflow) {
		t.Errorf("errors.Is(err, registry.ErrRefcountUnderflow) = false, want true; err=%v", err)
	}
}

// Acquire/Release round-trip through the wrapper drives the underlying
// registry's eviction the same way the generic path does.
func TestAcquire_Release_RoundTrip(t *testing.T) {
	r := New()
	ready := r.Acquire(context.Background(), "vnet-1")

	select {
	case <-ready:
		t.Fatal("Ready should be open before Add")
	default:
	}

	if err := r.Add(&VnetState{VnetID: "vnet-1", VNI: 1}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	select {
	case <-ready:
	case <-time.After(time.Second):
		t.Fatal("Ready did not close after Add")
	}

	if r.Refcount("vnet-1") != 1 {
		t.Errorf("Refcount = %d, want 1", r.Refcount("vnet-1"))
	}
	if err := r.Release("vnet-1"); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if r.Len() != 0 {
		t.Errorf("Len after balanced Release = %d, want 0", r.Len())
	}
}

// Snapshot returns deep-copied VnetStates: mutating the returned
// values' slice fields does not corrupt registry state.
func TestSnapshot_DeepCopiesValues(t *testing.T) {
	r := New()
	_ = r.Add(&VnetState{VnetID: "vnet-1", VNI: 1, PeerVnetIDs: []types.VnetID{"vnet-2"}})
	_ = r.Add(&VnetState{VnetID: "vnet-3", VNI: 3, PeerVnetIDs: []types.VnetID{"vnet-4"}})

	snap := r.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("Snapshot len = %d, want 2", len(snap))
	}

	snap["vnet-1"].PeerVnetIDs[0] = "tampered"
	snap["vnet-1"].VNI = 9999

	got, _ := r.Get("vnet-1")
	if got.PeerVnetIDs[0] != "vnet-2" {
		t.Errorf("registry mutated via snapshot value: %s", got.PeerVnetIDs[0])
	}
	if got.VNI != 1 {
		t.Errorf("registry mutated via snapshot value: VNI = %d", got.VNI)
	}
}

// sortDedupPeers produces a sorted, duplicate-free peer list and the
// stored state reflects the canonical form.
func TestAdd_SortsAndDedupesPeers(t *testing.T) {
	r := New()
	_ = r.Add(&VnetState{
		VnetID:      "vnet-1",
		VNI:         1,
		PeerVnetIDs: []types.VnetID{"vnet-5", "vnet-2", "vnet-5", "vnet-3", "vnet-2"},
	})
	got, _ := r.Get("vnet-1")
	want := []types.VnetID{"vnet-2", "vnet-3", "vnet-5"}
	if len(got.PeerVnetIDs) != len(want) {
		t.Fatalf("PeerVnetIDs = %v, want %v", got.PeerVnetIDs, want)
	}
	for i, p := range want {
		if got.PeerVnetIDs[i] != p {
			t.Errorf("PeerVnetIDs[%d] = %s, want %s", i, got.PeerVnetIDs[i], p)
		}
	}
}

// Clone produces a deep copy: mutating the clone does not touch the
// source.
func TestVnetState_Clone(t *testing.T) {
	src := &VnetState{
		VnetID:      "vnet-1",
		VNI:         7,
		PeerVnetIDs: []types.VnetID{"vnet-2", "vnet-3"},
	}
	dup := src.Clone()
	dup.PeerVnetIDs[0] = "tampered"
	dup.VNI = 999

	if src.PeerVnetIDs[0] != "vnet-2" {
		t.Errorf("source mutated via clone: PeerVnetIDs[0] = %s", src.PeerVnetIDs[0])
	}
	if src.VNI != 7 {
		t.Errorf("source mutated via clone: VNI = %d", src.VNI)
	}
}

// Name() round-trips the constant fed to the inner generic registry.
func TestName(t *testing.T) {
	if New().Name() != "vnet" {
		t.Errorf("Name = %q, want vnet", New().Name())
	}
}
