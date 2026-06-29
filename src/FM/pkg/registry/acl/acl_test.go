package acl

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dashfabric/fm/pkg/registry"
	"github.com/dashfabric/fm/pkg/types"
)

func validAcl(id types.AclGroupID) *AclGroupState {
	return &AclGroupState{AclGroupID: id, VnetID: "vnet-1", RuleCount: 3}
}

func TestAdd_Get_PreservesFields(t *testing.T) {
	r := New()
	ts := time.Date(2026, 6, 23, 15, 0, 0, 0, time.UTC)
	in := &AclGroupState{
		AclGroupID:   "acl-1",
		VnetID:       "vnet-1",
		RuleCount:    7,
		SpecRevision: 2,
		LastUpdated:  ts,
	}
	if err := r.Add(in); err != nil {
		t.Fatalf("Add: %v", err)
	}
	got, ok := r.Get("acl-1")
	if !ok {
		t.Fatal("Get returned ok=false")
	}
	if got.AclGroupID != "acl-1" || got.VnetID != "vnet-1" || got.RuleCount != 7 {
		t.Errorf("fields drifted: %+v", got)
	}
	if got.SpecRevision != 2 || !got.LastUpdated.Equal(ts) {
		t.Errorf("version/time fields drifted: %+v", got)
	}
}

func TestAdd_EmptyAclGroupID_Rejected(t *testing.T) {
	r := New()
	if err := r.Add(&AclGroupState{AclGroupID: "", VnetID: "vnet-1"}); err == nil {
		t.Fatal("expected error for empty AclGroupID")
	}
	if r.Len() != 0 {
		t.Errorf("Len = %d, want 0", r.Len())
	}
}

func TestAdd_EmptyVnetID_Rejected(t *testing.T) {
	r := New()
	if err := r.Add(&AclGroupState{AclGroupID: "acl-1", VnetID: ""}); err == nil {
		t.Fatal("expected error for empty VnetID")
	}
}

func TestAdd_NegativeRuleCount_Rejected(t *testing.T) {
	r := New()
	if err := r.Add(&AclGroupState{AclGroupID: "acl-1", VnetID: "vnet-1", RuleCount: -1}); err == nil {
		t.Fatal("expected error for negative RuleCount")
	}
}

func TestAdd_ZeroRuleCount_Allowed(t *testing.T) {
	r := New()
	if err := r.Add(&AclGroupState{AclGroupID: "acl-1", VnetID: "vnet-1", RuleCount: 0}); err != nil {
		t.Fatalf("Add with RuleCount=0: %v", err)
	}
}

func TestGet_ReturnsIndependentCopy(t *testing.T) {
	r := New()
	_ = r.Add(validAcl("acl-1"))
	got, _ := r.Get("acl-1")
	got.RuleCount = 999
	again, _ := r.Get("acl-1")
	if again.RuleCount != 3 {
		t.Errorf("registry mutated via Get copy: RuleCount = %d", again.RuleCount)
	}
}

func TestRelease_Underflow_PropagatesREG007(t *testing.T) {
	r := New()
	err := r.Release("acl-ghost")
	if err == nil {
		t.Fatal("Release on never-acquired key returned nil")
	}
	if !errors.Is(err, registry.ErrRefcountUnderflow) {
		t.Errorf("errors.Is mismatch: %v", err)
	}
}

func TestAcquire_BeforeAdd_ReadyClosesOnAdd(t *testing.T) {
	r := New()
	ready := r.Acquire(context.Background(), "acl-1")
	select {
	case <-ready:
		t.Fatal("Ready should be open before Add")
	default:
	}
	_ = r.Add(validAcl("acl-1"))
	select {
	case <-ready:
	default:
		t.Fatal("Ready should close after Add")
	}
}

func TestSnapshot_ReturnsIndependentCopies(t *testing.T) {
	r := New()
	_ = r.Add(validAcl("acl-1"))
	_ = r.Add(validAcl("acl-2"))
	snap := r.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("Snapshot len = %d, want 2", len(snap))
	}
	snap["acl-1"].RuleCount = 999
	got, _ := r.Get("acl-1")
	if got.RuleCount != 3 {
		t.Errorf("registry mutated via snapshot: RuleCount = %d", got.RuleCount)
	}
}

func TestName(t *testing.T) {
	if New().Name() != "acl" {
		t.Errorf("Name = %q, want acl", New().Name())
	}
}
