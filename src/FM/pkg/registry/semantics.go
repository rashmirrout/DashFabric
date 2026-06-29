package registry

import (
	"context"
	"sync"
)

// Registry is the shared in-memory contract that every per-object
// registry (vnet, nic, mapping, acl, route, meter) composes.
//
// This is the WAVE 1 contract: synchronous, in-memory, no T1 watch
// integration yet. The Ready / Updates / Err fanout described in
// Specs/FM/registry-semantics-exact.md will be layered on in Wave 2
// when the adapter and T1 storage exist. Until then:
//
//   - Acquire returns a Ready channel that is closed when the key is
//     present in the registry (either at Acquire time or on subsequent
//     Add). No timeout is enforced here — callers manage their own
//     deadlines via context.
//   - Release decrements the refcount. A Release that would drive the
//     refcount below zero returns REG_007_REFCOUNT_UNDERFLOW; the
//     refcount is NOT mutated on the error path.
//   - Add is idempotent at the key level: the second Add for a key
//     overwrites the value and closes the Ready channel exactly once.
//     The refcount is unaffected by Add (only by Acquire/Release).
//   - Eviction happens only on Release-to-zero (no grace period yet).
//     Wave 2 will introduce the debounce timer per §5.
//
// The contract is generic over key and value type. Per-object
// registries instantiate it with their own (K, V).
type Registry[K comparable, V any] struct {
	mu      sync.RWMutex
	entries map[K]*entry[V]
	name    string
}

// New returns an empty Registry tagged with the given name. The name is
// surfaced in error wrapping and (Wave 8) metrics labels.
func New[K comparable, V any](name string) *Registry[K, V] {
	return &Registry[K, V]{
		entries: make(map[K]*entry[V]),
		name:    name,
	}
}

// Name returns the registry's identifying name.
func (r *Registry[K, V]) Name() string { return r.name }

// Acquire increments the refcount for key and returns a channel that
// closes once a value has been Added. If a value is already present,
// the returned channel is already closed.
//
// Acquire never blocks waiting for a value; the caller selects on the
// returned channel to wait for readiness. The ctx parameter is
// accepted for symmetry with the post-Wave-2 contract; today it is
// not consulted because this Acquire is purely synchronous.
//
// The caller MUST eventually call Release(key) to balance this Acquire.
// Failure to do so leaks the entry past the last logical referent.
func (r *Registry[K, V]) Acquire(ctx context.Context, key K) <-chan struct{} {
	_ = ctx // reserved for Wave 2 (T1 watch start)
	r.mu.Lock()
	defer r.mu.Unlock()

	e, ok := r.entries[key]
	if !ok {
		e = newEntry[V]()
		r.entries[key] = e
	}
	e.refs++
	return e.ready
}

// Release decrements the refcount for key. When the refcount reaches
// zero the entry is evicted (no grace period in this slice — Wave 2
// adds the debounce per registry-semantics-exact.md §5).
//
// If the key is not present, or the refcount would underflow,
// Release returns *UnderflowError carrying REG_007_REFCOUNT_UNDERFLOW
// and does NOT mutate the refcount. The same code distinguishes
// "released too many times" from "released a key that was never
// acquired" — both are caller-correctness bugs of the same kind.
func (r *Registry[K, V]) Release(key K) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	e, ok := r.entries[key]
	if !ok {
		return newUnderflow(r.name, key)
	}
	if e.refs <= 0 {
		return newUnderflow(r.name, key)
	}
	e.refs--
	if e.refs == 0 {
		delete(r.entries, key)
	}
	return nil
}

// Add inserts or overwrites the value for key. On the first Add for a
// key the Ready channel is closed; subsequent Adds overwrite the value
// but leave the (already-closed) Ready channel alone.
//
// Add does NOT change the refcount. A value may be Added before any
// Acquire (cold-write); Acquires that arrive later will see the value
// and an already-closed Ready immediately. Cold-written entries with
// refs==0 are NOT evicted automatically — that policy is owned by the
// caller (typically: adapter writes value, actor acquires, then
// releases on detach, at which point refs returns to 0 and the entry
// is evicted).
//
// Add is safe to call concurrently with Acquire/Release/Get/Snapshot.
func (r *Registry[K, V]) Add(key K, value V) {
	r.mu.Lock()
	defer r.mu.Unlock()

	e, ok := r.entries[key]
	if !ok {
		e = newEntry[V]()
		r.entries[key] = e
	}
	first := !e.hasValue
	e.value = value
	e.hasValue = true
	if first {
		close(e.ready)
	}
}

// Get returns the current value for key and a present-flag. If the key
// has been Acquired but no value Added yet, present is false and the
// zero value of V is returned — callers should select on the Acquire
// Ready channel before relying on Get.
func (r *Registry[K, V]) Get(key K) (V, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	e, ok := r.entries[key]
	if !ok || !e.hasValue {
		var zero V
		return zero, false
	}
	return e.value, true
}

// Ready returns true if key has a value Added. Non-blocking; intended
// for status queries and tests.
func (r *Registry[K, V]) Ready(key K) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	e, ok := r.entries[key]
	return ok && e.hasValue
}

// Refcount returns the current number of live Acquires for key, or 0
// if key is not present. Intended for tests and (Wave 8) metrics.
func (r *Registry[K, V]) Refcount(key K) int64 {
	r.mu.RLock()
	defer r.mu.RUnlock()

	e, ok := r.entries[key]
	if !ok {
		return 0
	}
	return e.refs
}

// Len returns the number of entries (both refcount-held and
// cold-written value caches).
func (r *Registry[K, V]) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.entries)
}

// Snapshot returns a copy of the current key→value map for all entries
// that have a value Added. The returned map is independent of the
// registry — mutating it does not affect registry state. Used by the
// reconciler and the audit log.
func (r *Registry[K, V]) Snapshot() map[K]V {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make(map[K]V, len(r.entries))
	for k, e := range r.entries {
		if e.hasValue {
			out[k] = e.value
		}
	}
	return out
}
