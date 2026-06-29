package registry

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	fmerrors "github.com/dashfabric/fm/pkg/errors"
)

// Acquire on a missing key returns an open Ready chan, then closes
// on the next Add.
func TestAcquire_ColdKey_ReadyClosesOnAdd(t *testing.T) {
	r := New[string, int]("test")
	ready := r.Acquire(context.Background(), "k1")

	select {
	case <-ready:
		t.Fatal("Ready should be open on cold Acquire")
	default:
	}

	r.Add("k1", 42)

	select {
	case <-ready:
		// expected
	case <-time.After(time.Second):
		t.Fatal("Ready should close after Add")
	}

	v, ok := r.Get("k1")
	if !ok || v != 42 {
		t.Errorf("Get after Add = (%v, %v), want (42, true)", v, ok)
	}
}

// Acquire on a key that already has a value returns an
// already-closed Ready (cache-hit fast path).
func TestAcquire_HotKey_ReadyAlreadyClosed(t *testing.T) {
	r := New[string, int]("test")
	r.Add("k1", 100) // cold-write before any Acquire

	ready := r.Acquire(context.Background(), "k1")
	select {
	case <-ready:
		// expected
	default:
		t.Fatal("Ready should be closed when value already present")
	}
}

// Multiple Acquires on the same key share the same Ready channel —
// all subscribers see the close moment simultaneously.
func TestAcquire_MultipleSubscribers_ShareReady(t *testing.T) {
	r := New[string, string]("test")
	r1 := r.Acquire(context.Background(), "k")
	r2 := r.Acquire(context.Background(), "k")
	r3 := r.Acquire(context.Background(), "k")

	if r.Refcount("k") != 3 {
		t.Errorf("Refcount = %d, want 3", r.Refcount("k"))
	}

	r.Add("k", "hello")

	for i, c := range []<-chan struct{}{r1, r2, r3} {
		select {
		case <-c:
		case <-time.After(time.Second):
			t.Errorf("subscriber %d Ready did not close after Add", i)
		}
	}
}

// N Acquires then N Releases drives the entry to eviction.
func TestRelease_BalancedRefcount_Evicts(t *testing.T) {
	r := New[string, int]("test")
	r.Acquire(context.Background(), "k")
	r.Acquire(context.Background(), "k")
	r.Acquire(context.Background(), "k")
	r.Add("k", 7)

	if r.Len() != 1 {
		t.Errorf("Len after Adds = %d, want 1", r.Len())
	}

	for i := 0; i < 3; i++ {
		if err := r.Release("k"); err != nil {
			t.Fatalf("Release #%d: unexpected error %v", i+1, err)
		}
	}

	if r.Len() != 0 {
		t.Errorf("Len after balanced Releases = %d, want 0 (evicted)", r.Len())
	}
	if _, ok := r.Get("k"); ok {
		t.Error("Get after eviction should return ok=false")
	}
}

// One Release too many returns REG_007 and does NOT mutate refcount.
func TestRelease_Underflow_ReturnsREG007(t *testing.T) {
	r := New[string, int]("vnet")
	r.Acquire(context.Background(), "v1")
	if err := r.Release("v1"); err != nil {
		t.Fatalf("first Release: %v", err)
	}
	// Second Release on a now-evicted key.
	err := r.Release("v1")
	if err == nil {
		t.Fatal("expected REG_007 underflow, got nil")
	}
	var ue *UnderflowError
	if !errors.As(err, &ue) {
		t.Fatalf("expected *UnderflowError, got %T", err)
	}
	if ue.Code != fmerrors.REG_007_REFCOUNT_UNDERFLOW {
		t.Errorf("Code = %s, want REG_007_REFCOUNT_UNDERFLOW", ue.Code)
	}
	if ue.Registry != "vnet" {
		t.Errorf("Registry = %q, want vnet", ue.Registry)
	}
	if !errors.Is(err, ErrRefcountUnderflow) {
		t.Error("errors.Is(err, ErrRefcountUnderflow) = false, want true")
	}
}

// Release for a never-acquired key also returns REG_007 (same bug
// class as double-release).
func TestRelease_NeverAcquired_ReturnsREG007(t *testing.T) {
	r := New[string, int]("acl")
	err := r.Release("ghost")
	if err == nil {
		t.Fatal("expected REG_007, got nil")
	}
	if !errors.Is(err, ErrRefcountUnderflow) {
		t.Errorf("errors.Is mismatch: err=%v", err)
	}
}

// Add is idempotent: a second Add for the same key overwrites the
// value but does NOT close Ready again (it's already closed) and
// does NOT affect refcount.
func TestAdd_Idempotent(t *testing.T) {
	r := New[string, string]("test")
	ready := r.Acquire(context.Background(), "k")
	r.Add("k", "v1")
	r.Add("k", "v2") // overwrite — must not panic (close-of-closed)

	<-ready // already closed

	if r.Refcount("k") != 1 {
		t.Errorf("Refcount after two Adds = %d, want 1 (Add does not change refcount)", r.Refcount("k"))
	}
	v, _ := r.Get("k")
	if v != "v2" {
		t.Errorf("Get = %q, want %q (latest Add wins)", v, "v2")
	}
}

// A cold-write Add (no prior Acquire) creates a value-cache entry
// with refs==0 that is NOT evicted automatically.
func TestAdd_BeforeAcquire_ColdWrite(t *testing.T) {
	r := New[string, int]("test")
	r.Add("k", 99)
	if r.Refcount("k") != 0 {
		t.Errorf("Refcount of cold-write = %d, want 0", r.Refcount("k"))
	}
	if !r.Ready("k") {
		t.Error("Ready=false for cold-write key")
	}
	if v, ok := r.Get("k"); !ok || v != 99 {
		t.Errorf("Get cold-write = (%v, %v), want (99, true)", v, ok)
	}
}

// Snapshot returns a copy — mutating it doesn't change registry state.
func TestSnapshot_IsIndependent(t *testing.T) {
	r := New[string, int]("test")
	r.Add("a", 1)
	r.Add("b", 2)
	r.Add("c", 3)

	snap := r.Snapshot()
	if len(snap) != 3 {
		t.Fatalf("Snapshot len = %d, want 3", len(snap))
	}

	snap["a"] = 999
	delete(snap, "b")

	if v, _ := r.Get("a"); v != 1 {
		t.Errorf("registry mutated via snapshot: Get(a) = %d, want 1", v)
	}
	if _, ok := r.Get("b"); !ok {
		t.Error("registry mutated via snapshot: Get(b) missing")
	}
}

// Snapshot omits entries without values (an Acquired-but-not-Added key
// shouldn't surface in the audit / reconciler view).
func TestSnapshot_OmitsValuelessEntries(t *testing.T) {
	r := New[string, int]("test")
	r.Acquire(context.Background(), "pending")
	r.Add("ready", 1)

	snap := r.Snapshot()
	if _, ok := snap["pending"]; ok {
		t.Error("Snapshot included entry without value")
	}
	if v, ok := snap["ready"]; !ok || v != 1 {
		t.Errorf("Snapshot missing valued entry: got %v, %v", v, ok)
	}
}

// Concurrent Acquire / Add / Release / Get must not deadlock or
// produce inconsistent refcounts. The race detector is the real
// arbiter here (CI runs -race); this test exists so -race has
// something interesting to look at.
func TestConcurrent_AcquireAddReleaseGet(t *testing.T) {
	r := New[int, int]("test")
	const N = 200

	var wg sync.WaitGroup
	wg.Add(3 * N)

	var acquired atomic.Int64
	for i := 0; i < N; i++ {
		i := i
		go func() {
			defer wg.Done()
			r.Acquire(context.Background(), i%10)
			acquired.Add(1)
		}()
		go func() {
			defer wg.Done()
			r.Add(i%10, i)
		}()
		go func() {
			defer wg.Done()
			_, _ = r.Get(i % 10)
		}()
	}
	wg.Wait()

	if acquired.Load() != N {
		t.Fatalf("Acquire count = %d, want %d", acquired.Load(), N)
	}

	// Now Release exactly the number we Acquired across all 10 keys
	// and verify everything evicts.
	totalRefs := int64(0)
	for k := 0; k < 10; k++ {
		totalRefs += r.Refcount(k)
	}
	if totalRefs != int64(N) {
		t.Fatalf("sum of refcounts = %d, want %d", totalRefs, N)
	}

	released := 0
	for k := 0; k < 10; k++ {
		for r.Refcount(k) > 0 {
			if err := r.Release(k); err != nil {
				t.Fatalf("Release key=%d: %v", k, err)
			}
			released++
		}
	}
	if released != N {
		t.Errorf("Released %d, want %d", released, N)
	}
	if r.Len() != 0 {
		t.Errorf("Len after full drain = %d, want 0", r.Len())
	}
}

// Name() round-trips the constructor argument.
func TestName(t *testing.T) {
	if New[string, int]("mapping").Name() != "mapping" {
		t.Error("Name mismatch")
	}
}
