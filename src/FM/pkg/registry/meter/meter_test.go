package meter

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dashfabric/fm/pkg/registry"
	"github.com/dashfabric/fm/pkg/types"
)

func validMeter(id types.MeterPolicyID) *MeterPolicyState {
	return &MeterPolicyState{MeterPolicyID: id, VnetID: "vnet-1", RateBps: 1_000_000}
}

func TestAdd_Get_PreservesFields(t *testing.T) {
	r := New()
	ts := time.Date(2026, 6, 23, 15, 0, 0, 0, time.UTC)
	in := &MeterPolicyState{
		MeterPolicyID: "mp-1",
		VnetID:        "vnet-1",
		RateBps:       10_000_000,
		BurstBps:      20_000_000,
		SpecRevision:  3,
		LastUpdated:   ts,
	}
	if err := r.Add(in); err != nil {
		t.Fatalf("Add: %v", err)
	}
	got, ok := r.Get("mp-1")
	if !ok {
		t.Fatal("Get returned ok=false")
	}
	if got.MeterPolicyID != "mp-1" || got.VnetID != "vnet-1" {
		t.Errorf("ID fields drifted: %+v", got)
	}
	if got.RateBps != 10_000_000 || got.BurstBps != 20_000_000 {
		t.Errorf("rate fields drifted: Rate=%d Burst=%d", got.RateBps, got.BurstBps)
	}
	if got.SpecRevision != 3 || !got.LastUpdated.Equal(ts) {
		t.Errorf("version/time fields drifted: %+v", got)
	}
}

// BurstBps=0 means "same as RateBps" — it is explicitly allowed.
func TestAdd_ZeroBurstBps_Allowed(t *testing.T) {
	r := New()
	if err := r.Add(&MeterPolicyState{MeterPolicyID: "mp-1", VnetID: "vnet-1", RateBps: 1000, BurstBps: 0}); err != nil {
		t.Fatalf("Add with BurstBps=0: %v", err)
	}
	got, _ := r.Get("mp-1")
	if got.BurstBps != 0 {
		t.Errorf("BurstBps drifted: %d", got.BurstBps)
	}
}

func TestAdd_EmptyMeterPolicyID_Rejected(t *testing.T) {
	r := New()
	if err := r.Add(&MeterPolicyState{MeterPolicyID: "", VnetID: "vnet-1", RateBps: 1000}); err == nil {
		t.Fatal("expected error for empty MeterPolicyID")
	}
	if r.Len() != 0 {
		t.Errorf("Len = %d, want 0", r.Len())
	}
}

func TestAdd_EmptyVnetID_Rejected(t *testing.T) {
	r := New()
	if err := r.Add(&MeterPolicyState{MeterPolicyID: "mp-1", VnetID: "", RateBps: 1000}); err == nil {
		t.Fatal("expected error for empty VnetID")
	}
}

func TestAdd_ZeroRateBps_Rejected(t *testing.T) {
	r := New()
	if err := r.Add(&MeterPolicyState{MeterPolicyID: "mp-1", VnetID: "vnet-1", RateBps: 0}); err == nil {
		t.Fatal("expected error for RateBps=0")
	}
}

func TestGet_ReturnsIndependentCopy(t *testing.T) {
	r := New()
	_ = r.Add(validMeter("mp-1"))
	got, _ := r.Get("mp-1")
	got.RateBps = 999
	again, _ := r.Get("mp-1")
	if again.RateBps != 1_000_000 {
		t.Errorf("registry mutated via Get copy: RateBps = %d", again.RateBps)
	}
}

func TestRelease_Underflow_PropagatesREG007(t *testing.T) {
	r := New()
	err := r.Release("mp-ghost")
	if err == nil {
		t.Fatal("Release on never-acquired key returned nil")
	}
	if !errors.Is(err, registry.ErrRefcountUnderflow) {
		t.Errorf("errors.Is mismatch: %v", err)
	}
}

func TestAcquire_BeforeAdd_ReadyClosesOnAdd(t *testing.T) {
	r := New()
	ready := r.Acquire(context.Background(), "mp-1")
	select {
	case <-ready:
		t.Fatal("Ready should be open before Add")
	default:
	}
	_ = r.Add(validMeter("mp-1"))
	select {
	case <-ready:
	default:
		t.Fatal("Ready should close after Add")
	}
}

func TestSnapshot_ReturnsIndependentCopies(t *testing.T) {
	r := New()
	_ = r.Add(validMeter("mp-1"))
	_ = r.Add(validMeter("mp-2"))
	snap := r.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("Snapshot len = %d, want 2", len(snap))
	}
	snap["mp-1"].RateBps = 999
	got, _ := r.Get("mp-1")
	if got.RateBps != 1_000_000 {
		t.Errorf("registry mutated via snapshot: RateBps = %d", got.RateBps)
	}
}

func TestName(t *testing.T) {
	if New().Name() != "meter" {
		t.Errorf("Name = %q, want meter", New().Name())
	}
}
