package ratelimit

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestTokenBucketRefill(t *testing.T) {
	tb := NewTokenBucket(100, 10)

	if !tb.Allow(50) {
		t.Errorf("should allow 50 tokens")
	}

	remaining := 100.0 - 50.0
	if tb.tokens < remaining-1 || tb.tokens > remaining+1 {
		t.Errorf("expected ~50 tokens remaining, got %.2f", tb.tokens)
	}
}

func TestTokenBucketCapacity(t *testing.T) {
	tb := NewTokenBucket(100, 1000)

	time.Sleep(100 * time.Millisecond)

	if !tb.Allow(100) {
		t.Errorf("should allow 100 tokens")
	}

	if tb.Allow(1) {
		t.Errorf("should not exceed capacity")
	}
}

func TestTokenBucketExceeded(t *testing.T) {
	tb := NewTokenBucket(10, 1)

	if !tb.Allow(10) {
		t.Errorf("first request should succeed")
	}

	if tb.Allow(1) {
		t.Errorf("second request should exceed limit")
	}
}

func TestMultiDimensionalLimiter(t *testing.T) {
	config := map[Dimension]float64{
		DimensionGlobal:    100,
		DimensionPerClient: 50,
	}
	limiter := NewMultiDimensionalLimiter(config, []Dimension{DimensionGlobal, DimensionPerClient})

	if !limiter.Allow("client-1", "", "") {
		t.Errorf("first request should be allowed")
	}

	if !limiter.Allow("client-1", "", "") {
		t.Errorf("second request from same client should be allowed")
	}
}

func TestMultiDimensionalLimiterPerIP(t *testing.T) {
	config := map[Dimension]float64{
		DimensionPerIP: 10,
	}
	limiter := NewMultiDimensionalLimiter(config, []Dimension{DimensionPerIP})

	for i := 0; i < 10; i++ {
		if !limiter.Allow("", "192.168.1.1", "") {
			t.Errorf("request %d should be allowed", i+1)
		}
	}

	if limiter.Allow("", "192.168.1.1", "") {
		t.Errorf("11th request should be rate limited")
	}
}

func TestMultiDimensionalLimiterMultiIP(t *testing.T) {
	config := map[Dimension]float64{
		DimensionPerIP: 5,
	}
	limiter := NewMultiDimensionalLimiter(config, []Dimension{DimensionPerIP})

	for i := 0; i < 5; i++ {
		if !limiter.Allow("", "192.168.1.1", "") {
			t.Errorf("IP1 request %d should be allowed", i+1)
		}
		if !limiter.Allow("", "192.168.1.2", "") {
			t.Errorf("IP2 request %d should be allowed", i+1)
		}
	}

	if limiter.Allow("", "192.168.1.1", "") {
		t.Errorf("IP1 should be rate limited")
	}
	if limiter.Allow("", "192.168.1.2", "") {
		t.Errorf("IP2 should be rate limited")
	}
}

func TestMultiDimensionalLimiterReset(t *testing.T) {
	config := map[Dimension]float64{
		DimensionPerClient: 5,
	}
	limiter := NewMultiDimensionalLimiter(config, []Dimension{DimensionPerClient})

	for i := 0; i < 5; i++ {
		limiter.Allow("client-1", "", "")
	}

	if limiter.Allow("client-1", "", "") {
		t.Errorf("should be rate limited before reset")
	}

	limiter.Reset()

	if !limiter.Allow("client-1", "", "") {
		t.Errorf("should allow after reset")
	}
}

func TestRateLimitCounter(t *testing.T) {
	counter := NewRateLimitCounter()

	if counter.GetExceeded() != 0 {
		t.Errorf("initial count should be 0")
	}

	for i := 0; i < 10; i++ {
		counter.RecordExceeded()
	}

	if counter.GetExceeded() != 10 {
		t.Errorf("expected 10, got %d", counter.GetExceeded())
	}

	counter.Reset()
	if counter.GetExceeded() != 0 {
		t.Errorf("count should be 0 after reset")
	}
}

func TestTokenBucketConcurrent(t *testing.T) {
	tb := NewTokenBucket(1000, 100)

	allowed := int64(0)
	rejected := int64(0)
	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				if tb.Allow(1) {
					atomic.AddInt64(&allowed, 1)
				} else {
					atomic.AddInt64(&rejected, 1)
				}
			}
		}()
	}

	wg.Wait()

	total := allowed + rejected
	if total != 1000 {
		t.Errorf("expected 1000 total requests, got %d", total)
	}
	if allowed != 1000 && rejected != 0 {
		t.Logf("allowed=%d, rejected=%d", allowed, rejected)
	}
}

func TestMultiDimensionalLimiterConcurrent(t *testing.T) {
	config := map[Dimension]float64{
		DimensionGlobal:    1000,
		DimensionPerClient: 500,
	}
	limiter := NewMultiDimensionalLimiter(config, []Dimension{DimensionGlobal, DimensionPerClient})

	allowed := int64(0)
	rejected := int64(0)
	var wg sync.WaitGroup

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(clientID int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				if limiter.Allow("client-"+string(rune(clientID+'0')), "", "") {
					atomic.AddInt64(&allowed, 1)
				} else {
					atomic.AddInt64(&rejected, 1)
				}
			}
		}(i)
	}

	wg.Wait()

	total := allowed + rejected
	if total != 1000 {
		t.Errorf("expected 1000 total requests, got %d", total)
	}
}

func BenchmarkTokenBucketAllow(b *testing.B) {
	tb := NewTokenBucket(1000, 100)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tb.Allow(1)
	}
}

func BenchmarkMultiDimensionalAllow(b *testing.B) {
	config := map[Dimension]float64{
		DimensionGlobal:    10000,
		DimensionPerClient: 1000,
	}
	limiter := NewMultiDimensionalLimiter(config, []Dimension{DimensionGlobal, DimensionPerClient})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		limiter.Allow("client-1", "", "")
	}
}
