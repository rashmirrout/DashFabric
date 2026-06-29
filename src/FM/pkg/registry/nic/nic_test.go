package nic

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dashfabric/fm/pkg/registry"
	"github.com/dashfabric/fm/pkg/types"
)

// minimal valid NicState for use as a base in tests that focus on a
// single field.
func validNic(eid types.ENIID) *NicState {
	return &NicState{
		ENIID:      eid,
		VnetID:     "vnet-1",
		MacAddress: "00:0a:95:9d:68:16",
	}
}

// Round-trip Add/Get preserves every scalar and slice field.
func TestAdd_Get_PreservesFields(t *testing.T) {
	r := New()
	ts := time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC)
	in := &NicState{
		ENIID:         "eni-1",
		VnetID:        "vnet-1",
		MacAddress:    "00:1a:2b:3c:4d:5e",
		AclGroupIDs:   []types.AclGroupID{"acl-2", "acl-1"},
		RouteGroupID:  "rg-1",
		MeterPolicyID: "mp-1",
		SpecRevision:  3,
		LastUpdated:   ts,
	}
	if err := r.Add(in); err != nil {
		t.Fatalf("Add: %v", err)
	}
	got, ok := r.Get("eni-1")
	if !ok {
		t.Fatal("Get returned ok=false after Add")
	}
	if got.ENIID != "eni-1" || got.VnetID != "vnet-1" || got.MacAddress != "00:1a:2b:3c:4d:5e" {
		t.Errorf("scalar fields drifted: %+v", got)
	}
	if got.RouteGroupID != "rg-1" || got.MeterPolicyID != "mp-1" || got.SpecRevision != 3 {
		t.Errorf("optional/version fields drifted: %+v", got)
	}
	if !got.LastUpdated.Equal(ts) {
		t.Errorf("LastUpdated drifted: %v", got.LastUpdated)
	}
	// AclGroupIDs are stored sorted
	if len(got.AclGroupIDs) != 2 || got.AclGroupIDs[0] != "acl-1" || got.AclGroupIDs[1] != "acl-2" {
		t.Errorf("AclGroupIDs = %v, want [acl-1 acl-2]", got.AclGroupIDs)
	}
}

// Get returns a deep copy: mutating the returned AclGroupIDs must not
// corrupt registry state.
func TestGet_AclGroupIDsIsDeepCopy(t *testing.T) {
	r := New()
	_ = r.Add(&NicState{
		ENIID:       "eni-1",
		VnetID:      "vnet-1",
		MacAddress:  "00:0a:95:9d:68:16",
		AclGroupIDs: []types.AclGroupID{"acl-1", "acl-2"},
	})
	got, _ := r.Get("eni-1")
	got.AclGroupIDs[0] = "tampered"
	got.AclGroupIDs = append(got.AclGroupIDs, "acl-9")

	again, _ := r.Get("eni-1")
	if again.AclGroupIDs[0] != "acl-1" {
		t.Errorf("registry mutated via Get: AclGroupIDs[0] = %s", again.AclGroupIDs[0])
	}
	if len(again.AclGroupIDs) != 2 {
		t.Errorf("registry mutated via Get: len = %d, want 2", len(again.AclGroupIDs))
	}
}

// Add copies the caller's AclGroupIDs slice: mutating the original
// after Add must not corrupt the registry.
func TestAdd_CallerSliceIsCopied(t *testing.T) {
	r := New()
	acls := []types.AclGroupID{"acl-1", "acl-2"}
	_ = r.Add(&NicState{
		ENIID:       "eni-1",
		VnetID:      "vnet-1",
		MacAddress:  "00:0a:95:9d:68:16",
		AclGroupIDs: acls,
	})
	acls[0] = "tampered"

	got, _ := r.Get("eni-1")
	if got.AclGroupIDs[0] != "acl-1" {
		t.Errorf("registry mutated via caller slice: %s", got.AclGroupIDs[0])
	}
}

// Empty ENIID is rejected; nothing is stored.
func TestAdd_EmptyENIID_Rejected(t *testing.T) {
	r := New()
	err := r.Add(&NicState{ENIID: "", VnetID: "vnet-1", MacAddress: "00:0a:95:9d:68:16"})
	if err == nil {
		t.Fatal("Add with empty ENIID returned nil error")
	}
	if r.Len() != 0 {
		t.Errorf("Len after rejected Add = %d, want 0", r.Len())
	}
}

// Empty VnetID is rejected.
func TestAdd_EmptyVnetID_Rejected(t *testing.T) {
	r := New()
	err := r.Add(&NicState{ENIID: "eni-1", VnetID: "", MacAddress: "00:0a:95:9d:68:16"})
	if err == nil {
		t.Fatal("Add with empty VnetID returned nil error")
	}
}

// Empty MacAddress is rejected.
func TestAdd_EmptyMacAddress_Rejected(t *testing.T) {
	r := New()
	err := r.Add(&NicState{ENIID: "eni-1", VnetID: "vnet-1", MacAddress: ""})
	if err == nil {
		t.Fatal("Add with empty MacAddress returned nil error")
	}
}

// Malformed MAC address is rejected.
func TestAdd_MalformedMac_Rejected(t *testing.T) {
	r := New()
	err := r.Add(&NicState{ENIID: "eni-1", VnetID: "vnet-1", MacAddress: "not-a-mac"})
	if err == nil {
		t.Fatal("Add with malformed MAC returned nil error")
	}
}

// An invalid AclGroupID (empty string) is rejected.
func TestAdd_InvalidAclGroupID_Rejected(t *testing.T) {
	r := New()
	err := r.Add(&NicState{
		ENIID:       "eni-1",
		VnetID:      "vnet-1",
		MacAddress:  "00:0a:95:9d:68:16",
		AclGroupIDs: []types.AclGroupID{"acl-1", ""},
	})
	if err == nil {
		t.Fatal("Add with empty AclGroupID returned nil error")
	}
}

// AclGroupIDs are sorted and deduplicated in the stored copy.
func TestAdd_SortsAndDedupsAclGroups(t *testing.T) {
	r := New()
	_ = r.Add(&NicState{
		ENIID:       "eni-1",
		VnetID:      "vnet-1",
		MacAddress:  "00:0a:95:9d:68:16",
		AclGroupIDs: []types.AclGroupID{"acl-5", "acl-1", "acl-3", "acl-1", "acl-5"},
	})
	got, _ := r.Get("eni-1")
	want := []types.AclGroupID{"acl-1", "acl-3", "acl-5"}
	if len(got.AclGroupIDs) != len(want) {
		t.Fatalf("AclGroupIDs = %v, want %v", got.AclGroupIDs, want)
	}
	for i, id := range want {
		if got.AclGroupIDs[i] != id {
			t.Errorf("AclGroupIDs[%d] = %s, want %s", i, got.AclGroupIDs[i], id)
		}
	}
}

// Zero-value RouteGroupID and MeterPolicyID are allowed (unassigned).
func TestAdd_UnassignedOptionalIDs_Allowed(t *testing.T) {
	r := New()
	err := r.Add(&NicState{
		ENIID:         "eni-1",
		VnetID:        "vnet-1",
		MacAddress:    "00:0a:95:9d:68:16",
		RouteGroupID:  "",
		MeterPolicyID: "",
	})
	if err != nil {
		t.Fatalf("Add with zero optional IDs: %v", err)
	}
}

// REG_007 underflow propagates through the typed wrapper.
func TestRelease_Underflow_PropagatesREG007(t *testing.T) {
	r := New()
	err := r.Release("eni-ghost")
	if err == nil {
		t.Fatal("Release on never-acquired key returned nil")
	}
	if !errors.Is(err, registry.ErrRefcountUnderflow) {
		t.Errorf("errors.Is(err, registry.ErrRefcountUnderflow) = false; err=%v", err)
	}
}

// Cold-write (Add before Acquire) then Acquire returns already-closed
// Ready channel.
func TestAcquire_AfterColdWrite_ReadyAlreadyClosed(t *testing.T) {
	r := New()
	_ = r.Add(validNic("eni-1"))

	ready := r.Acquire(context.Background(), "eni-1")
	select {
	case <-ready:
	default:
		t.Fatal("Ready should be closed when value already present (cold-write)")
	}
}

// Acquire before Add: ready closes when Add fires.
func TestAcquire_BeforeAdd_ReadyClosesOnAdd(t *testing.T) {
	r := New()
	ready := r.Acquire(context.Background(), "eni-1")

	select {
	case <-ready:
		t.Fatal("Ready should be open before Add")
	default:
	}

	_ = r.Add(validNic("eni-1"))

	select {
	case <-ready:
	default:
		t.Fatal("Ready should close after Add")
	}
}

// Snapshot returns independent deep copies; mutating a value in the
// snapshot must not affect the registry.
func TestSnapshot_DeepCopiesValues(t *testing.T) {
	r := New()
	_ = r.Add(&NicState{ENIID: "eni-1", VnetID: "vnet-1", MacAddress: "00:0a:95:9d:68:16", AclGroupIDs: []types.AclGroupID{"acl-1"}})
	_ = r.Add(&NicState{ENIID: "eni-2", VnetID: "vnet-1", MacAddress: "00:0a:95:9d:68:17", AclGroupIDs: []types.AclGroupID{"acl-2"}})

	snap := r.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("Snapshot len = %d, want 2", len(snap))
	}
	snap["eni-1"].AclGroupIDs[0] = "tampered"
	snap["eni-1"].MacAddress = "ff:ff:ff:ff:ff:ff"

	got, _ := r.Get("eni-1")
	if got.AclGroupIDs[0] != "acl-1" {
		t.Errorf("registry mutated via snapshot: AclGroupIDs[0] = %s", got.AclGroupIDs[0])
	}
	if got.MacAddress != "00:0a:95:9d:68:16" {
		t.Errorf("registry mutated via snapshot: MacAddress = %s", got.MacAddress)
	}
}

// Clone produces a deep copy; mutating the clone doesn't touch the source.
func TestNicState_Clone(t *testing.T) {
	src := &NicState{
		ENIID:       "eni-1",
		VnetID:      "vnet-1",
		MacAddress:  "00:0a:95:9d:68:16",
		AclGroupIDs: []types.AclGroupID{"acl-1", "acl-2"},
	}
	dup := src.Clone()
	dup.AclGroupIDs[0] = "tampered"
	dup.MacAddress = "ff:ff:ff:ff:ff:ff"

	if src.AclGroupIDs[0] != "acl-1" {
		t.Errorf("source mutated via clone: AclGroupIDs[0] = %s", src.AclGroupIDs[0])
	}
	if src.MacAddress != "00:0a:95:9d:68:16" {
		t.Errorf("source mutated via clone: MacAddress = %s", src.MacAddress)
	}
}

// Name() returns the constant fed to the inner generic registry.
func TestName(t *testing.T) {
	if New().Name() != "nic" {
		t.Errorf("Name = %q, want nic", New().Name())
	}
}
