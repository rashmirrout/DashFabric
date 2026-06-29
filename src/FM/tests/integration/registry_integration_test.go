package integration_test

// Cross-registry integration tests (Slice 1.8).
//
// These tests verify that the six per-object registries (vnet, nic,
// mapping, acl, route, meter) compose correctly under realistic
// actor-like patterns.
//
// Lock order followed throughout (mirrors Wave 4 actor convention and
// registry-semantics-exact.md §8.2):
//
//	VnetRegistry → NicRegistry → MappingRegistry →
//	AclGroupRegistry → RouteGroupRegistry → MeterPolicyRegistry

import (
	"context"
	"errors"
	"net/netip"
	"sync"
	"testing"

	"github.com/dashfabric/fm/pkg/registry"
	"github.com/dashfabric/fm/pkg/registry/acl"
	"github.com/dashfabric/fm/pkg/registry/mapping"
	"github.com/dashfabric/fm/pkg/registry/meter"
	"github.com/dashfabric/fm/pkg/registry/nic"
	"github.com/dashfabric/fm/pkg/registry/route"
	"github.com/dashfabric/fm/pkg/registry/vnet"
	"github.com/dashfabric/fm/pkg/types"
)

// TC-REG-INT-001: Multi-registry Acquire/Add/Get/Release.
//
// One actor acquires on VnetRegistry + NicRegistry for the same VNET,
// waits for both to hydrate, reads consistent state from both, then
// releases cleanly. Verifies Len()==0 on both after full release.
func TestRegistryIntegration_MultiRegistryAcquireRelease(t *testing.T) {
	vnetReg := vnet.New()
	nicReg := nic.New()

	vid := types.VnetID("vnet-1")
	eid := types.ENIID("eni-1")

	// Actor acquires interest in both (lock order: vnet before nic).
	vnetReady := vnetReg.Acquire(context.Background(), vid)
	nicReady := nicReg.Acquire(context.Background(), eid)

	// Producer hydrates both registries.
	if err := vnetReg.Add(&vnet.VnetState{VnetID: vid, VNI: 100}); err != nil {
		t.Fatalf("vnetReg.Add: %v", err)
	}
	if err := nicReg.Add(&nic.NicState{
		ENIID:      eid,
		VnetID:     vid,
		MacAddress: "00:1a:2b:3c:4d:5e",
	}); err != nil {
		t.Fatalf("nicReg.Add: %v", err)
	}

	// Both ready channels must close.
	<-vnetReady
	<-nicReady

	// State is accessible and consistent.
	v, ok := vnetReg.Get(vid)
	if !ok || v.VNI != 100 {
		t.Fatalf("vnetReg.Get: ok=%v VNI=%d", ok, v.VNI)
	}
	n, ok := nicReg.Get(eid)
	if !ok || n.VnetID != vid {
		t.Fatalf("nicReg.Get: ok=%v VnetID=%s", ok, n.VnetID)
	}
	if n.VnetID != v.VnetID {
		t.Errorf("NIC VnetID %s != VNET VnetID %s", n.VnetID, v.VnetID)
	}

	// Release in reverse order (nic before vnet).
	if err := nicReg.Release(eid); err != nil {
		t.Fatalf("nicReg.Release: %v", err)
	}
	if err := vnetReg.Release(vid); err != nil {
		t.Fatalf("vnetReg.Release: %v", err)
	}

	if vnetReg.Len() != 0 {
		t.Errorf("vnetReg.Len = %d after release, want 0", vnetReg.Len())
	}
	if nicReg.Len() != 0 {
		t.Errorf("nicReg.Len = %d after release, want 0", nicReg.Len())
	}
}

// TC-REG-INT-002: Snapshot consistency across all six registries.
//
// Populates all six registries with interlinked IDs, calls Snapshot()
// on all six, then verifies the returned maps are independent copies —
// mutating a snapshot value does not corrupt the live registry.
func TestRegistryIntegration_SnapshotConsistencyAllSix(t *testing.T) {
	vnetReg := vnet.New()
	nicReg := nic.New()
	mappingReg := mapping.New()
	aclReg := acl.New()
	routeReg := route.New()
	meterReg := meter.New()

	vid := types.VnetID("vnet-1")
	eid := types.ENIID("eni-1")
	mid := types.MappingID("map-1")
	aid := types.AclGroupID("acl-1")
	rid := types.RouteGroupID("rg-1")
	mpid := types.MeterPolicyID("mp-1")

	// Populate (lock order: vnet → nic → mapping → acl → route → meter).
	if err := vnetReg.Add(&vnet.VnetState{VnetID: vid, VNI: 42}); err != nil {
		t.Fatalf("vnetReg.Add: %v", err)
	}
	if err := nicReg.Add(&nic.NicState{
		ENIID: eid, VnetID: vid, MacAddress: "00:aa:bb:cc:dd:ee",
		AclGroupIDs: []types.AclGroupID{aid}, RouteGroupID: rid, MeterPolicyID: mpid,
	}); err != nil {
		t.Fatalf("nicReg.Add: %v", err)
	}
	if err := mappingReg.Add(&mapping.MappingState{
		MappingID: mid, VnetID: vid,
		VIP: netip.MustParseAddr("10.0.0.1"), DIP: netip.MustParseAddr("192.168.0.1"),
	}); err != nil {
		t.Fatalf("mappingReg.Add: %v", err)
	}
	if err := aclReg.Add(&acl.AclGroupState{AclGroupID: aid, VnetID: vid, RuleCount: 2}); err != nil {
		t.Fatalf("aclReg.Add: %v", err)
	}
	if err := routeReg.Add(&route.RouteGroupState{
		RouteGroupID: rid, VnetID: vid, NextHop: netip.MustParseAddr("10.0.0.254"),
	}); err != nil {
		t.Fatalf("routeReg.Add: %v", err)
	}
	if err := meterReg.Add(&meter.MeterPolicyState{
		MeterPolicyID: mpid, VnetID: vid, RateBps: 1_000_000,
	}); err != nil {
		t.Fatalf("meterReg.Add: %v", err)
	}

	// Snapshot all six.
	vsnap := vnetReg.Snapshot()
	nsnap := nicReg.Snapshot()
	msnap := mappingReg.Snapshot()
	asnap := aclReg.Snapshot()
	rsnap := routeReg.Snapshot()
	mpsnap := meterReg.Snapshot()

	// Mutate every snapshot value.
	vsnap[vid].VNI = 9999
	nsnap[eid].MacAddress = "ff:ff:ff:ff:ff:ff"
	msnap[mid].SNAT = true
	asnap[aid].RuleCount = 999
	rsnap[rid].NextHop = netip.MustParseAddr("1.2.3.4")
	mpsnap[mpid].RateBps = 1

	// Live registries must be unaffected.
	if v, _ := vnetReg.Get(vid); v.VNI != 42 {
		t.Errorf("vnetReg mutated via snapshot: VNI = %d", v.VNI)
	}
	if n, _ := nicReg.Get(eid); n.MacAddress != "00:aa:bb:cc:dd:ee" {
		t.Errorf("nicReg mutated via snapshot: Mac = %s", n.MacAddress)
	}
	if m, _ := mappingReg.Get(mid); m.SNAT {
		t.Error("mappingReg mutated via snapshot: SNAT flipped to true")
	}
	if a, _ := aclReg.Get(aid); a.RuleCount != 2 {
		t.Errorf("aclReg mutated via snapshot: RuleCount = %d", a.RuleCount)
	}
	if r, _ := routeReg.Get(rid); r.NextHop != netip.MustParseAddr("10.0.0.254") {
		t.Errorf("routeReg mutated via snapshot: NextHop = %s", r.NextHop)
	}
	if mp, _ := meterReg.Get(mpid); mp.RateBps != 1_000_000 {
		t.Errorf("meterReg mutated via snapshot: RateBps = %d", mp.RateBps)
	}
}

// TC-REG-INT-003: REG_007 surfaces identically across all six wrappers.
//
// Release on a never-acquired key must return an error satisfying
// errors.Is(err, registry.ErrRefcountUnderflow) for every registry.
func TestRegistryIntegration_REG007_AllSixRegistries(t *testing.T) {
	cases := []struct {
		name    string
		release func() error
	}{
		{"vnet", func() error { return vnet.New().Release("x") }},
		{"nic", func() error { return nic.New().Release("x") }},
		{"mapping", func() error { return mapping.New().Release("x") }},
		{"acl", func() error { return acl.New().Release("x") }},
		{"route", func() error { return route.New().Release("x") }},
		{"meter", func() error { return meter.New().Release("x") }},
	}
	for _, tc := range cases {
		err := tc.release()
		if err == nil {
			t.Errorf("%s: Release returned nil, want REG_007", tc.name)
			continue
		}
		if !errors.Is(err, registry.ErrRefcountUnderflow) {
			t.Errorf("%s: errors.Is(err, ErrRefcountUnderflow) = false; err=%v", tc.name, err)
		}
	}
}

// TC-REG-INT-004: Cold-write semantics are uniform across all six.
//
// Add before any Acquire → Ready()==true, Refcount()==0, Get() returns
// the value. Verified for every registry.
func TestRegistryIntegration_ColdWriteUniform(t *testing.T) {
	vnetReg := vnet.New()
	nicReg := nic.New()
	mappingReg := mapping.New()
	aclReg := acl.New()
	routeReg := route.New()
	meterReg := meter.New()

	vid := types.VnetID("vnet-1")
	eid := types.ENIID("eni-1")
	mid := types.MappingID("map-1")
	aid := types.AclGroupID("acl-1")
	rid := types.RouteGroupID("rg-1")
	mpid := types.MeterPolicyID("mp-1")

	_ = vnetReg.Add(&vnet.VnetState{VnetID: vid, VNI: 1})
	_ = nicReg.Add(&nic.NicState{ENIID: eid, VnetID: vid, MacAddress: "00:11:22:33:44:55"})
	_ = mappingReg.Add(&mapping.MappingState{
		MappingID: mid, VnetID: vid,
		VIP: netip.MustParseAddr("10.0.0.1"), DIP: netip.MustParseAddr("192.168.0.1"),
	})
	_ = aclReg.Add(&acl.AclGroupState{AclGroupID: aid, VnetID: vid})
	_ = routeReg.Add(&route.RouteGroupState{RouteGroupID: rid, VnetID: vid, NextHop: netip.MustParseAddr("10.0.0.1")})
	_ = meterReg.Add(&meter.MeterPolicyState{MeterPolicyID: mpid, VnetID: vid, RateBps: 500})

	checks := []struct {
		name      string
		ready     bool
		refcount  int64
		hasValue  bool
	}{
		{"vnet", vnetReg.Ready(vid), vnetReg.Refcount(vid), func() bool { _, ok := vnetReg.Get(vid); return ok }()},
		{"nic", nicReg.Ready(eid), nicReg.Refcount(eid), func() bool { _, ok := nicReg.Get(eid); return ok }()},
		{"mapping", mappingReg.Ready(mid), mappingReg.Refcount(mid), func() bool { _, ok := mappingReg.Get(mid); return ok }()},
		{"acl", aclReg.Ready(aid), aclReg.Refcount(aid), func() bool { _, ok := aclReg.Get(aid); return ok }()},
		{"route", routeReg.Ready(rid), routeReg.Refcount(rid), func() bool { _, ok := routeReg.Get(rid); return ok }()},
		{"meter", meterReg.Ready(mpid), meterReg.Refcount(mpid), func() bool { _, ok := meterReg.Get(mpid); return ok }()},
	}
	for _, c := range checks {
		if !c.ready {
			t.Errorf("%s: Ready=false after cold-write Add", c.name)
		}
		if c.refcount != 0 {
			t.Errorf("%s: Refcount=%d after cold-write Add, want 0", c.name, c.refcount)
		}
		if !c.hasValue {
			t.Errorf("%s: Get returned ok=false after cold-write Add", c.name)
		}
	}
}

// TC-REG-INT-005: Concurrent multi-registry hydration.
//
// 50 goroutines each Acquire on both VnetRegistry and NicRegistry
// (lock order: vnet before nic). A single producer then Add-s to
// both. Verifies all 50 ready channels close and a full drain yields
// Len()==0 on both registries.
func TestRegistryIntegration_ConcurrentMultiRegistryHydration(t *testing.T) {
	vnetReg := vnet.New()
	nicReg := nic.New()

	vid := types.VnetID("vnet-1")
	eid := types.ENIID("eni-1")

	const N = 50
	type pair struct{ v, n <-chan struct{} }
	pairs := make([]pair, N)

	var wg sync.WaitGroup
	wg.Add(N)
	for i := range pairs {
		i := i
		go func() {
			defer wg.Done()
			// Lock order: vnet → nic (must be consistent across goroutines).
			pairs[i].v = vnetReg.Acquire(context.Background(), vid)
			pairs[i].n = nicReg.Acquire(context.Background(), eid)
		}()
	}
	wg.Wait()

	// Producer hydrates both registries.
	if err := vnetReg.Add(&vnet.VnetState{VnetID: vid, VNI: 7}); err != nil {
		t.Fatalf("vnetReg.Add: %v", err)
	}
	if err := nicReg.Add(&nic.NicState{
		ENIID: eid, VnetID: vid, MacAddress: "00:de:ad:be:ef:00",
	}); err != nil {
		t.Fatalf("nicReg.Add: %v", err)
	}

	// All 50 pairs of ready channels must be closed.
	for i, p := range pairs {
		select {
		case <-p.v:
		default:
			t.Errorf("goroutine %d: vnet ready not closed", i)
		}
		select {
		case <-p.n:
		default:
			t.Errorf("goroutine %d: nic ready not closed", i)
		}
	}

	// Full drain: N releases each.
	for i := 0; i < N; i++ {
		if err := nicReg.Release(eid); err != nil {
			t.Fatalf("nicReg.Release #%d: %v", i, err)
		}
		if err := vnetReg.Release(vid); err != nil {
			t.Fatalf("vnetReg.Release #%d: %v", i, err)
		}
	}

	if vnetReg.Len() != 0 {
		t.Errorf("vnetReg.Len = %d after full drain, want 0", vnetReg.Len())
	}
	if nicReg.Len() != 0 {
		t.Errorf("nicReg.Len = %d after full drain, want 0", nicReg.Len())
	}
}
