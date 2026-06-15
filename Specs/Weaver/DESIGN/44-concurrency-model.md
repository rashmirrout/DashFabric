# Weaver: Concurrency Model

> **Read Time:** 20 minutes  
> **Previous:** [43-algorithms.md](./43-algorithms.md) | **Next:** [../CONTRIBUTE/70-development-setup.md](../CONTRIBUTE/70-development-setup.md)

---

## Goroutine Architecture

### Main Goroutines

```
Main
  ├─ Discovery Goroutine
  │   └─ Polls etcd/Consul every 30s for replica changes
  │
  ├─ Health Check Goroutines (N=1 per replica)
  │   ├─ HC Goroutine for replica-1
  │   │  └─ Health check every 5s
  │   ├─ HC Goroutine for replica-2
  │   └─ HC Goroutine for replica-N
  │
  ├─ Listener Goroutine (gRPC listener)
  │   └─ Accepts incoming connections on port 5051
  │       ├─ Request Handler #1 (client 1)
  │       ├─ Request Handler #2 (client 2)
  │       └─ Request Handler #N (client N)
  │
  ├─ Metrics Goroutine
  │   └─ Collects metrics every 15s for Prometheus
  │
  ├─ Tracer Goroutine (if tracing enabled)
  │   └─ Batches and sends traces to Jaeger
  │
  └─ Signal Handler Goroutine
      └─ Listens for SIGTERM/SIGINT for graceful shutdown
```

### Total Goroutine Count

```
Example with 3 replicas:
  1 (main)
  + 1 (discovery)
  + 3 (health check, 1 per replica)
  + 1 (listener)
  + N (request handlers, varies with load)
  + 1 (metrics)
  + 1 (tracer)
  + 1 (signal handler)
  = 9 + N goroutines

Under load with 100 concurrent requests:
  = 9 + 100 = 109 goroutines
```

---

## Synchronization Primitives

### 1. Mutex (Replica List Protection)

```go
var replicasMu sync.Mutex
var replicas []*Replica

// Write (Discovery Goroutine)
func updateReplicas(newReplicas []*Replica) {
  replicasMu.Lock()
  defer replicasMu.Unlock()
  replicas = newReplicas
}

// Read (Request Handler Goroutines)
func selectReplica() *Replica {
  replicasMu.RLock()  // Read lock
  defer replicasMu.RUnlock()
  
  selected := loadBalancer.Select(replicas)
  return selected
}
```

**Contention:** LOW
- Discovery updates: rare (every 30s)
- Request reads: frequent (per request)
- RWMutex allows concurrent readers

### 2. Atomic Operations (Counters)

```go
var requestCount int64

// In request handler
func handleRequest() {
  atomic.AddInt64(&requestCount, 1)
  
  // OR (simpler but slower)
  // requestCountMu.Lock()
  // requestCount++
  // requestCountMu.Unlock()
}

// When reading metrics
func getMetrics() int64 {
  return atomic.LoadInt64(&requestCount)
}
```

**Benefit:** Lock-free; very fast

### 3. Channels (Goroutine Communication)

```go
// Discovery publishes replica changes
replicaChanges := make(chan []*Replica, 1)

// Discovery Goroutine
go func() {
  for changes := range replicaChanges {
    // Update internal replica list
    updateReplicas(changes)
  }
}()

// Reader (e.g., metrics)
go func() {
  for range replicaChanges {
    // Replicas changed; recalculate metrics
  }
}()
```

**Benefit:** Safe message passing; no race conditions

### 4. Condvar (Conditional Waits)

```go
var readyCond = sync.NewCond(&sync.Mutex{})
var ready = false

// Waiter Goroutine
readyCond.L.Lock()
for !ready {
  readyCond.Wait()  // Release lock and wait
}
readyCond.L.Unlock()
// Now ready!

// Notifier Goroutine
readyCond.L.Lock()
ready = true
readyCond.Broadcast()  // Wake all waiters
readyCond.L.Unlock()
```

---

## Request Processing Concurrency

### Per-Request Isolation

```
Request 1 Handler (Goroutine A)
  → Lock replica for connection
  → Send request
  → Unlock replica
  → Continue (no interference with Request 2)

Request 2 Handler (Goroutine B)
  → Lock different replica (or wait if same)
  → Send request
  → Unlock replica
  → Continue

No shared state between request handlers
(except read-only replica list)
```

### Connection Pool Synchronization

```go
type Replica struct {
  name string
  connMu sync.Mutex
  conn *grpc.ClientConn
}

func (r *Replica) GetConn() (*grpc.ClientConn, error) {
  r.connMu.Lock()
  defer r.connMu.Unlock()
  
  if r.conn == nil {
    // Create new connection
    r.conn, _ = grpc.Dial(r.Address)
  }
  return r.conn, nil
}
```

**Per-Replica Lock:** Ensures connection pool thread-safe

---

## Health Check Concurrency

### Parallel Health Checks

```
Health Check Goroutine 1 (replica 1)
  → T=0: Check replica 1
  → T=5: Check replica 1
  → T=10: Check replica 1 (concurrent with HC2)

Health Check Goroutine 2 (replica 2)
  → T=0: Check replica 2 (concurrent with HC1)
  → T=5: Check replica 2
  → T=10: Check replica 2

Health Check Goroutine 3 (replica 3)
  → T=0: Check replica 3 (concurrent with HC1, HC2)
  → T=5: Check replica 3
  → T=10: Check replica 3
```

**Benefit:** All replicas checked in parallel; health check latency minimized

---

## Discovery Polling

### Concurrent Access to Discovery Service

```
Discovery Goroutine
  → T=0: etcdctl watch /weaver/replicas
  → Receives update: replicas changed
  → Lock replicasMu
  → Update internal list
  → Unlock replicasMu
  → Notify all request handlers (via read-only copy)

Request Handlers (concurrent)
  → Check replica list (read lock, fast)
  → Send to selected replica
  → No blocking on discovery
```

---

## Rate Limiter Concurrency

### Token Bucket Thread Safety

```go
type TokenBucket struct {
  tokens float64
  mu sync.Mutex
}

func (tb *TokenBucket) Allow() bool {
  tb.mu.Lock()
  defer tb.mu.Unlock()
  
  // Refill
  elapsed = time.Since(tb.lastRefill)
  tb.tokens += elapsed.Seconds() * tb.refillRate
  if tb.tokens > tb.capacity {
    tb.tokens = tb.capacity
  }
  
  // Check
  if tb.tokens >= 1 {
    tb.tokens--
    return true
  }
  return false
}
```

**Contention:** Medium
- Every request calls this
- Locks are short (microseconds)
- Modern CPUs handle well

### Multi-Dimensional Buckets

```
Per-Tenant Buckets (map)
  → tb_tenant_1 (lock 1)
  → tb_tenant_2 (lock 2)
  → tb_tenant_N (lock N)

Benefit: Different tenants don't contend for same lock
Risk: Memory growth if many tenants
Mitigation: LRU cache of buckets; evict unused
```

---

## Circuit Breaker Concurrency

```go
type CircuitBreaker struct {
  state CircuitState
  mu sync.Mutex
}

func (cb *CircuitBreaker) RecordSuccess() {
  cb.mu.Lock()
  defer cb.mu.Unlock()
  
  cb.successes++
  if cb.successes >= cb.config.SuccessThreshold {
    cb.state = StateClosed
    cb.successes = 0
  }
}

func (cb *CircuitBreaker) RecordFailure() {
  cb.mu.Lock()
  defer cb.mu.Unlock()
  
  cb.failures++
  if cb.failures >= cb.config.FailureThreshold {
    cb.state = StateOpen
    cb.failures = 0
    cb.lastFailureTime = time.Now()
  }
}
```

---

## Graceful Shutdown

```
Signal received (SIGTERM)
  ↓
1. Stop accepting new connections
   (Listener closes socket)
  ↓
2. Request handlers finish in-flight requests
   (drain_timeout = 30s)
  ↓
3. Flush metrics to Prometheus
   (Metrics goroutine sends data)
  ↓
4. Flush traces to Jaeger
   (Tracer goroutine sends data)
  ↓
5. Wait for all goroutines to exit
   (sync.WaitGroup pattern)
  ↓
6. Exit process
```

### Shutdown Implementation

```go
var wg sync.WaitGroup

// Main
func main() {
  wg.Add(7)  // 7 background goroutines
  
  go func() {
    defer wg.Done()
    discovery()
  }()
  
  go func() {
    defer wg.Done()
    listener()  // Blocks until shutdown
  }()
  
  // ... other goroutines
  
  sigCh := make(chan os.Signal)
  signal.Notify(sigCh, syscall.SIGTERM)
  <-sigCh  // Wait for signal
  
  // Graceful shutdown
  closeListener()
  time.Sleep(drain_timeout)
  
  wg.Wait()  // Wait for all goroutines
  os.Exit(0)
}
```

---

## Race Condition Prevention

### Shared State

```
SAFE (protected):
  ✅ replicas []*Replica  (protected by replicasMu RWMutex)
  ✅ requestCount int64   (protected by atomic ops)
  ✅ circuitBreakerState  (protected by mu Mutex)

UNSAFE (unprotected):
  ❌ Goroutine-local variables (only accessed by one goroutine)
  ✅ (These are actually safe; no sharing)
```

### Go Race Detector

```bash
# Compile with race detection
go build -race

# Run tests with race detection
go test -race ./...

# Any race condition detected?
WARNING: DATA RACE
Write at 0x00c0001a2340 by goroutine 23:
  github.com/weaver.(*Gateway).updateReplicas()
      gateway.go:123 +0xXXX

Previous read at 0x00c0001a2340 by goroutine 22:
  github.com/weaver.(*Gateway).selectReplica()
      gateway.go:456 +0xXXX

Goroutine 23 (running):
...
```

---

## Performance Characteristics

| Operation | Concurrency Model | Latency | Scalability |
|-----------|------------------|---------|------------|
| Select replica | RWMutex read lock | <1μs | O(1) |
| Update replicas | Mutex write lock | <10μs | O(n) replicas |
| Rate limit check | Mutex | <5μs | O(1) |
| CB record | Mutex | <2μs | O(1) |

**Conclusion:** Low contention; scales to 100k+ rps

---

**Navigation:**
- [← Previous](./43-algorithms.md)
- [Index](../INDEX.md)
- [Next →](../CONTRIBUTE/70-development-setup.md)
