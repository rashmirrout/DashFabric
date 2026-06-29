package route

import (
	"context"
	"errors"
	"net/netip"
	"testing"
	"time"

	"github.com/dashfabric/fm/pkg/registry"
	"github.com/dashfabric/fm/pkg/types"
)

var testNextHop = netip.MustParseAddr("10.0.0.254")

func validRoute(id types.RouteGroupID) *RouteGroupState {
	return &RouteGroupState{RouteGroupID: id, VnetID: "vnet-1", NextHop: testNextHop}
}

func TestAdd_Get_PreservesFields(t *testing.T) {
	r := New()
	ts := time.Date(2026, 6, 23, 15, 0, 0, 0, time.UTC)
	in := &RouteGroupState{
		RouteGroupID: "rg-1",
		VnetID:       "vnet-1",
		NextHop:      netip.MustParseAddr("172.16.0.1"),
		SpecRevision: 4,
		LastUpdated:  ts,
	}
	if err := r.Add(in); err != nil {
		t.Fatalf("Add: %v", err)
	}
	got, ok := r.Get("rg-1")
	if !ok {
		t.Fatal("Get returned ok=false")
	}
	if got.RouteGroupID != "rg-1" || got.VnetID != "vnet-1" {
		t.Errorf("ID fields drifted: %+v", got)
	}
	if got.NextHop != netip.MustParseAddr("172.16.0.1") {
		t.Errorf("NextHop drifted: %s", got.NextHop)
	}
	if got.SpecRevision != 4 || !got.LastUpdated.Equal(ts) {
		t.Errorf("version/time fields drifted: %+v", got)
	}
}

func TestAdd_EmptyRouteGroupID_Rejected(t *testing.T) {
	r := New()
	if err := r.Add(&RouteGroupState{RouteGroupID: "", VnetID: "vnet-1", NextHop: testNextHop}); err == nil {
		t.Fatal("expected error for empty RouteGroupID")
	}
}

func TestAdd_EmptyVnetID_Rejected(t *testing.T) {
	r := New()
	if err := r.Add(&RouteGroupState{RouteGroupID: "rg-1", VnetID: "", NextHop: testNextHop}); err == nil {
		t.Fatal("expected error for empty VnetID")
	}
}

func TestAdd_ZeroNextHop_Rejected(t *testing.T) {
	r := New()
	if err := r.Add(&RouteGroupState{RouteGroupID: "rg-1", VnetID: "vnet-1", NextHop: netip.Addr{}}); err == nil {
		t.Fatal("expected error for zero NextHop")
	}
	if r.Len() != 0 {
		t.Errorf("Len = %d, want 0", r.Len())
	}
}

func TestAdd_UnspecifiedNextHop_IPv4_Rejected(t *testing.T) {
	r := New()
	if err := r.Add(&RouteGroupState{RouteGroupID: "rg-1", VnetID: "vnet-1", NextHop: netip.MustParseAddr("0.0.0.0")}); err == nil {
		t.Fatal("expected error for 0.0.0.0 NextHop")
	}
}

func TestAdd_UnspecifiedNextHop_IPv6_Rejected(t *testing.T) {
	r := New()
	if err := r.Add(&RouteGroupState{RouteGroupID: "rg-1", VnetID: "vnet-1", NextHop: netip.MustParseAddr("::")}); err == nil {
		t.Fatal("expected error for :: NextHop")
	}
}

func TestAdd_IPv6NextHop_Accepted(t *testing.T) {
	r := New()
	if err := r.Add(&RouteGroupState{RouteGroupID: "rg-1", VnetID: "vnet-1", NextHop: netip.MustParseAddr("fd00::1")}); err != nil {
		t.Fatalf("Add with IPv6 NextHop: %v", err)
	}
}

func TestGet_ReturnsIndependentCopy(t *testing.T) {
	r := New()
	_ = r.Add(validRoute("rg-1"))
	got, _ := r.Get("rg-1")
	got.NextHop = netip.MustParseAddr("9.9.9.9")
	again, _ := r.Get("rg-1")
	if again.NextHop != testNextHop {
		t.Errorf("registry mutated via Get copy: %s", again.NextHop)
	}
}

func TestRelease_Underflow_PropagatesREG007(t *testing.T) {
	r := New()
	err := r.Release("rg-ghost")
	if err == nil {
		t.Fatal("Release on never-acquired key returned nil")
	}
	if !errors.Is(err, registry.ErrRefcountUnderflow) {
		t.Errorf("errors.Is mismatch: %v", err)
	}
}

func TestAcquire_BeforeAdd_ReadyClosesOnAdd(t *testing.T) {
	r := New()
	ready := r.Acquire(context.Background(), "rg-1")
	select {
	case <-ready:
		t.Fatal("Ready should be open before Add")
	default:
	}
	_ = r.Add(validRoute("rg-1"))
	select {
	case <-ready:
	default:
		t.Fatal("Ready should close after Add")
	}
}

func TestSnapshot_ReturnsIndependentCopies(t *testing.T) {
	r := New()
	_ = r.Add(validRoute("rg-1"))
	_ = r.Add(validRoute("rg-2"))
	snap := r.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("Snapshot len = %d, want 2", len(snap))
	}
	snap["rg-1"].NextHop = netip.MustParseAddr("1.2.3.4")
	got, _ := r.Get("rg-1")
	if got.NextHop != testNextHop {
		t.Errorf("registry mutated via snapshot: %s", got.NextHop)
	}
}

func TestName(t *testing.T) {
	if New().Name() != "route" {
		t.Errorf("Name = %q, want route", New().Name())
	}
}
