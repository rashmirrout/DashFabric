# Weaver: Performance Tuning Guide

> **Read Time:** 20 minutes  
> **Previous:** [54-troubleshooting.md](./54-troubleshooting.md) | **Next:** [56-security-hardening.md](./56-security-hardening.md)

---

## Performance Optimization

Strategies to optimize Weaver for your specific workload.

---

## Baseline Measurement

Before tuning, establish baseline:

**1. Current Performance**
```bash
# For 1 hour, record:
- Request rate: curl http://localhost:9090/metrics | grep fm_gw_requests_total
- P50/P99 latency: curl http://localhost:9090/metrics | grep fm_gw_request_duration
- Error rate: curl http://localhost:9090/metrics | grep fm_gw_request_errors_total

# Calculate:
rps = increase(fm_gw_requests_total[1h]) / 3600
latency_p99 = fm_gw_request_duration_p99
error_rate = increase(fm_gw_request_errors_total[1h]) / increase(fm_gw_requests_total[1h])
```

**2. Resource Usage**
```bash
# CPU and memory
docker stats weaver  # Docker
kubectl top pods -l app=weaver  # Kubernetes

# Record:
- CPU utilization
- Memory utilization
- Network I/O
```

**3. Identify Bottleneck**
Is it:
- **Gateway** (high CPU/memory on Weaver pod)
- **Backend** (high latency to replicas)
- **Network** (high latency or packet loss)

---

## Tuning by Bottleneck

### If Gateway is Bottleneck

**Symptom:** Weaver CPU/memory high; backend replicas idle

**Optimization 1: Increase Replicas**
```yaml
# Kubernetes
spec:
  replicas: 5  # Increase from 3
```

**Optimization 2: Optimize Load Balancer Algorithm**
```yaml
load_balancers:
  default:
    # Fastest: O(1) constant time
    strategy: "round_robin"  # or "least_connections" (O(n))
    # Slower: O(log n) or O(n)
    # strategy: "consistent_hash"  # (O(log n))
```

| Strategy | Time | Notes |
|----------|------|-------|
| round_robin | O(1) | Fastest; strict fairness |
| least_connections | O(n) | Fair but scans replicas |
| random | O(1) | Fast; less fair |
| consistent_hash | O(log n) | Medium; enables sticky sessions |

**Optimization 3: Reduce Discovery Overhead**
```yaml
discovery:
  config:
    cache_ttl: "60s"  # Increase from 30s; trade off freshness for speed
```

**Optimization 4: Reduce Health Check Overhead**
```yaml
health:
  interval: "10s"  # Increase from 5s; trade off failure detection latency
```

**Optimization 5: Tune Buffer Sizes**
```yaml
listeners:
  grpc:
    max_connections: 10000  # Increase if hitting limit
    buffer_size: "64kb"  # Larger buffer = fewer reads/writes
```

---

### If Backend is Bottleneck

**Symptom:** Gateway has low CPU; backend replicas high CPU/memory

**Optimization 1: Increase Backend Replicas**
```bash
# This is backend team's problem, but Weaver can help:
# - Reduce retry attempts (so failed requests don't amplify load)
# - Increase timeout (so requests don't fail too quickly)
# - Enable circuit breaker (protects backend from cascading failures)
```

**Optimization 2: Use Resource-Aware Load Balancer**
```yaml
load_balancers:
  default:
    strategy: "resource_aware"
    config:
      cpu_weight: 0.7
      memory_weight: 0.3
      # Sends more traffic to less-loaded replicas
```

**Optimization 3: Reduce Request Rate (if needed)**
```yaml
rate_limiting:
  global:
    requests_per_second: 50000  # Reduce from 100000 if backend saturated
```

---

### If Network is Bottleneck

**Symptom:** High latency between Weaver and replicas; packet loss

**Optimization 1: Reduce Health Check Frequency (fewer network calls)**
```yaml
health:
  interval: "30s"  # Increase from 5s
  timeout: "5s"    # Increase timeout
```

**Optimization 2: Use Connection Pooling**
```yaml
listeners:
  grpc:
    connection_pool_size: 100  # Reuse connections; fewer TCP handshakes
```

**Optimization 3: Tune Timeout**
```yaml
reliability:
  timeout:
    connection: "10s"  # Increase if network is slow
    global: "60s"      # Increase if backend is slow
```

**Fix:** Talk to network team; check if there's congestion or poor peering.

---

## Workload-Specific Tuning

### High-Throughput (10k+ rps)

```yaml
gateway:
  name: "high-throughput"

listeners:
  grpc:
    max_connections: 50000
    buffer_size: "256kb"

discovery:
  config:
    cache_ttl: "60s"

health:
  interval: "30s"

load_balancers:
  default:
    strategy: "round_robin"  # Fastest algorithm

reliability:
  retry:
    enabled: false  # Disable retries to reduce backend load
  timeout:
    global: "5s"  # Fail fast

rate_limiting:
  global:
    requests_per_second: 100000
```

**Scale:** 5-10 replicas minimum

---

### Low-Latency (< 10ms target)

```yaml
gateway:
  name: "low-latency"

listeners:
  grpc:
    buffer_size: "512kb"  # Larger buffer = fewer context switches

discovery:
  config:
    cache_ttl: "120s"  # Reduce discovery lookups

health:
  interval: "60s"  # Reduce health check frequency
  timeout: "5s"

load_balancers:
  default:
    strategy: "round_robin"  # No overhead of calculating "least"

reliability:
  retry:
    enabled: false  # Disable to avoid latency spikes
  timeout:
    global: "50ms"  # Aggressive timeout

rate_limiting:
  global:
    requests_per_second: 1000000  # Very high; let backend rate-limit
```

**Scale:** 3-5 replicas (more replicas = more load balancing overhead)

---

### High-Availability (fault-tolerant)

```yaml
gateway:
  name: "high-availability"

health:
  interval: "2s"  # Detect failures fast
  timeout: "1s"
  unhealthy_threshold: 2  # Mark unhealthy quickly
  healthy_threshold: 1    # Mark healthy quickly

load_balancers:
  default:
    strategy: "least_connections"  # Distribute load fairly

reliability:
  circuit_breaker:
    enabled: true
    failure_threshold: 3  # Open circuit quickly
    success_threshold: 1  # Close quickly
    timeout: "30s"  # Retry soon
  retry:
    enabled: true
    max_attempts: 5
    backoff:
      base: "50ms"
      multiplier: 2
      max: "2s"
  timeout:
    global: "30s"
```

**Scale:** 5+ replicas (for fault tolerance)

---

## Measurement & Validation

### Before Tuning

Record baseline:
```bash
echo "Baseline ($(date)):"
echo "RPS: $(curl -s http://localhost:9090/metrics | grep 'fm_gw_requests_total' | head -1)"
echo "P99: $(curl -s http://localhost:9090/metrics | grep 'fm_gw_request_duration_p99' | head -1)"
echo "CPU: $(docker stats --no-stream weaver | tail -1 | awk '{print $3}')"
```

### After Each Tuning

Wait 5-10 minutes under load; record metrics:
```bash
echo "After tuning: $(date)"
echo "RPS: ..."
echo "P99: ..."
echo "CPU: ..."
```

### Success Criteria

Compare after vs. baseline:
- ✅ RPS increased 10%+
- ✅ P99 latency decreased 10%+
- ✅ CPU utilization decreased 10%+
- ✅ Error rate still < 0.1%
- ✅ No new alerts firing

---

## Common Tuning Mistakes

❌ **Increase cache_ttl too much**
- Problem: Stale replica list; slow failure detection
- Fix: Keep cache_ttl < health_check_interval

❌ **Disable health checks**
- Problem: Unhealthy replicas stay in pool; errors increase
- Fix: Keep health checks enabled; just increase interval

❌ **Increase timeout too much**
- Problem: Slow cascading failures; clients timeout waiting for gateway
- Fix: Balance: timeout > max_replica_latency but < acceptable_wait_time

❌ **Disable circuit breaker**
- Problem: Overloaded backend crashes; cascading failures
- Fix: Keep circuit breaker enabled in production

❌ **Use Consistent Hash for uneven distribution**
- Problem: Some replicas get more traffic (poor hashing)
- Fix: Use round_robin if hashing not ideal; or fix hash function

---

## Profiling

### CPU Profile

```bash
# Get CPU profile from running Weaver
curl http://localhost:8080/debug/pprof/profile > cpu.prof

# Analyze
go tool pprof cpu.prof
(pprof) top  # Show top CPU-consuming functions
```

### Memory Profile

```bash
curl http://localhost:8080/debug/pprof/heap > mem.prof
go tool pprof mem.prof
(pprof) top
```

### Goroutine Profile

```bash
curl http://localhost:8080/debug/pprof/goroutine > goroutines.prof
go tool pprof goroutines.prof
```

---

## Benchmarking Tools

**Load Testing:**
```bash
# Apache Bench
ab -n 10000 -c 100 http://localhost:5051/

# wrk
wrk -t 4 -c 100 -d 30s http://localhost:5051/

# k6
k6 run test.js

# ghz (for gRPC)
ghz --insecure -d '{"data":"test"}' -n 10000 -c 100 \
  localhost:5051 MyService/MyMethod
```

---

**Navigation:**
- [← Previous](./54-troubleshooting.md)
- [Index](../INDEX.md)
- [Next →](./56-security-hardening.md)
