package mapping

import (
	"context"
	"errors"
	"net/netip"
	"testing"
	"time"

	"github.com/dashfabric/fm/pkg/registry"
	"github.com/dashfabric/fm/pkg/types"
)

var (
	testVIP = netip.MustParseAddr("10.0.0.1")
	testDIP = netip.MustParseAddr("192.168.1.10")
)

func validMapping(id types.MappingID) *MappingState {
	return &MappingState{
		MappingID: id,
		VnetID:    "vnet-1",
		VIP:       testVIP,
		DIP:       testDIP,
	}
}

// Round-trip Add/Get preserves all fields.
func TestAdd_Get_PreservesFields(t *testing.T) {
	r := New()
	ts := time.Date(2026, 6, 23, 14, 0, 0, 0, time.UTC)
	in := &MappingState{
		MappingID:    "map-1",
		VnetID:       "vnet-1",
		VIP:          netip.MustParseAddr("10.0.0.1"),
		DIP:          netip.MustParseAddr("192.168.1.10"),
		SNAT:         true,
		BindingTime:  ts,
		SpecRevision: 5,
		LastUpdated:  ts,
	}
	if err := r.Add(in); err != nil {
		t.Fatalf("Add: %v", err)
	}
	got, ok := r.Get("map-1")
	if !ok {
		t.Fatal("Get returned ok=false after Add")
	}
	if got.MappingID != "map-1" || got.VnetID != "vnet-1" {
		t.Errorf("ID fields drifted: %+v", got)
	}
	if got.VIP != netip.MustParseAddr("10.0.0.1") || got.DIP != netip.MustParseAddr("192.168.1.10") {
		t.Errorf("IP fields drifted: VIP=%s DIP=%s", got.VIP, got.DIP)
	}
	if !got.SNAT {
		t.Error("SNAT drifted: want true")
	}
	if !got.BindingTime.Equal(ts) || !got.LastUpdated.Equal(ts) {
		t.Errorf("time fields drifted: BindingTime=%v LastUpdated=%v", got.BindingTime, got.LastUpdated)
	}
	if got.SpecRevision != 5 {
		t.Errorf("SpecRevision = %d, want 5", got.SpecRevision)
	}
}

// SNAT=false is preserved (zero-value must not be silently promoted).
func TestAdd_SNAT_False_Preserved(t *testing.T) {
	r := New()
	in := validMapping("map-1")
	in.SNAT = false
	_ = r.Add(in)
	got, _ := r.Get("map-1")
	if got.SNAT {
		t.Error("SNAT drifted to true, want false")
	}
}

// Get returns an independent copy: mutating a scalar on the returned
// pointer must not affect the registry (struct fields are values, not
// pointers, so this verifies the Clone path is exercised).
func TestGet_ReturnsIndependentCopy(t *testing.T) {
	r := New()
	_ = r.Add(validMapping("map-1"))
	got, _ := r.Get("map-1")
	got.SNAT = true
	got.VIP = netip.MustParseAddr("1.2.3.4")

	again, _ := r.Get("map-1")
	if again.SNAT {
		t.Error("registry SNAT mutated via Get copy")
	}
	if again.VIP != testVIP {
		t.Errorf("registry VIP mutated via Get copy: %s", again.VIP)
	}
}

// Zero VIP (netip.Addr{}) is rejected.
func TestAdd_ZeroVIP_Rejected(t *testing.T) {
	r := New()
	err := r.Add(&MappingState{MappingID: "m1", VnetID: "vnet-1", VIP: netip.Addr{}, DIP: testDIP})
	if err == nil {
		t.Fatal("Add with zero VIP returned nil error")
	}
	if r.Len() != 0 {
		t.Errorf("Len after rejected Add = %d, want 0", r.Len())
	}
}

// Zero DIP (netip.Addr{}) is rejected.
func TestAdd_ZeroDIP_Rejected(t *testing.T) {
	r := New()
	err := r.Add(&MappingState{MappingID: "m1", VnetID: "vnet-1", VIP: testVIP, DIP: netip.Addr{}})
	if err == nil {
		t.Fatal("Add with zero DIP returned nil error")
	}
}

// Unspecified VIP (0.0.0.0) is rejected.
func TestAdd_UnspecifiedVIP_IPv4_Rejected(t *testing.T) {
	r := New()
	err := r.Add(&MappingState{
		MappingID: "m1",
		VnetID:    "vnet-1",
		VIP:       netip.MustParseAddr("0.0.0.0"),
		DIP:       testDIP,
	})
	if err == nil {
		t.Fatal("Add with 0.0.0.0 VIP returned nil error")
	}
}

// Unspecified VIP (::) is rejected.
func TestAdd_UnspecifiedVIP_IPv6_Rejected(t *testing.T) {
	r := New()
	err := r.Add(&MappingState{
		MappingID: "m1",
		VnetID:    "vnet-1",
		VIP:       netip.MustParseAddr("::"),
		DIP:       testDIP,
	})
	if err == nil {
		t.Fatal("Add with :: VIP returned nil error")
	}
}

// Unspecified DIP (0.0.0.0) is rejected.
func TestAdd_UnspecifiedDIP_Rejected(t *testing.T) {
	r := New()
	err := r.Add(&MappingState{
		MappingID: "m1",
		VnetID:    "vnet-1",
		VIP:       testVIP,
		DIP:       netip.MustParseAddr("0.0.0.0"),
	})
	if err == nil {
		t.Fatal("Add with 0.0.0.0 DIP returned nil error")
	}
}

// Empty MappingID is rejected.
func TestAdd_EmptyMappingID_Rejected(t *testing.T) {
	r := New()
	err := r.Add(&MappingState{MappingID: "", VnetID: "vnet-1", VIP: testVIP, DIP: testDIP})
	if err == nil {
		t.Fatal("Add with empty MappingID returned nil error")
	}
}

// Empty VnetID is rejected.
func TestAdd_EmptyVnetID_Rejected(t *testing.T) {
	r := New()
	err := r.Add(&MappingState{MappingID: "m1", VnetID: "", VIP: testVIP, DIP: testDIP})
	if err == nil {
		t.Fatal("Add with empty VnetID returned nil error")
	}
}

// IPv6 VIP and DIP are accepted.
func TestAdd_IPv6_Accepted(t *testing.T) {
	r := New()
	err := r.Add(&MappingState{
		MappingID: "m1",
		VnetID:    "vnet-1",
		VIP:       netip.MustParseAddr("fd00::1"),
		DIP:       netip.MustParseAddr("fd00::2"),
	})
	if err != nil {
		t.Fatalf("Add with IPv6 addrs: %v", err)
	}
}

// REG_007 underflow propagates through the typed wrapper.
func TestRelease_Underflow_PropagatesREG007(t *testing.T) {
	r := New()
	err := r.Release("map-ghost")
	if err == nil {
		t.Fatal("Release on never-acquired key returned nil")
	}
	if !errors.Is(err, registry.ErrRefcountUnderflow) {
		t.Errorf("errors.Is(err, registry.ErrRefcountUnderflow) = false; err=%v", err)
	}
}

// Cold-write: Add before Acquire → hot Acquire returns closed Ready.
func TestAcquire_AfterColdWrite_ReadyAlreadyClosed(t *testing.T) {
	r := New()
	_ = r.Add(validMapping("map-1"))
	ready := r.Acquire(context.Background(), "map-1")
	select {
	case <-ready:
	default:
		t.Fatal("Ready should be closed after cold-write Add")
	}
}

// Acquire before Add: ready closes when Add fires.
func TestAcquire_BeforeAdd_ReadyClosesOnAdd(t *testing.T) {
	r := New()
	ready := r.Acquire(context.Background(), "map-1")
	select {
	case <-ready:
		t.Fatal("Ready should be open before Add")
	default:
	}
	_ = r.Add(validMapping("map-1"))
	select {
	case <-ready:
	default:
		t.Fatal("Ready should close after Add")
	}
}

// Snapshot returns independent copies.
func TestSnapshot_ReturnsIndependentCopies(t *testing.T) {
	r := New()
	_ = r.Add(validMapping("map-1"))
	_ = r.Add(validMapping("map-2"))

	snap := r.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("Snapshot len = %d, want 2", len(snap))
	}
	snap["map-1"].VIP = netip.MustParseAddr("9.9.9.9")
	snap["map-1"].SNAT = true

	got, _ := r.Get("map-1")
	if got.VIP != testVIP {
		t.Errorf("registry VIP mutated via snapshot: %s", got.VIP)
	}
	if got.SNAT {
		t.Error("registry SNAT mutated via snapshot")
	}
}

// Clone produces an independent copy.
func TestMappingState_Clone(t *testing.T) {
	src := validMapping("map-1")
	src.SNAT = true
	dup := src.Clone()
	dup.VIP = netip.MustParseAddr("1.2.3.4")
	dup.SNAT = false

	if src.VIP != testVIP {
		t.Errorf("source VIP mutated via clone: %s", src.VIP)
	}
	if !src.SNAT {
		t.Error("source SNAT mutated via clone")
	}
}

// Name() returns the registry name constant.
func TestName(t *testing.T) {
	if New().Name() != "mapping" {
		t.Errorf("Name = %q, want mapping", New().Name())
	}
}
