# Weaver: Testing & Chaos Engineering Scenario

> **Read Time:** 15 minutes  
> **Scenario:** Intentionally breaking systems to test resilience  
> **Audience:** QA, SREs, Engineers  
> **Previous:** [24-failover-dr.md](./24-failover-dr.md) | **Next:** [REFERENCE/](../REFERENCE/)

---

## Chaos Engineering Tests

### **Test 1: Kill a Replica**

```bash
# Chaos: Stop fm-2
docker-compose stop fm-replica-2  # (or kubectl delete pod...)

# Expected Behavior (Weaver):
# T=0s:   fm-2 unavailable
# T=10s:  Health check fails (timeout)
# T=20s:  Health check fails (2x)
# T=30s:  Health check fails (3x) → fm-2 marked UNHEALTHY
# T=30s:  Circuit breaker OPENS (fail-fast)
# T=40s+: Requests route to fm-1, fm-3 only

# Verification:
curl http://localhost:8080/debug/replicas | jq '.replicas[] | {name, status}'
# Expected: fm-2 status = "UNHEALTHY"

# Restart:
docker-compose start fm-replica-2

# Recovery:
# T=0s:    fm-2 responding
# T=10s:   Health check passes → HALF_OPEN
# T=15s:   Requests succeed → CLOSED (recovered)

# Verify recovery:
curl http://localhost:8080/debug/replicas | jq '.replicas[] | {name, status}'
# Expected: All status = "HEALTHY"
```

**Pass Criteria:** Automatic failover within 30s; automatic recovery within 20s

---

### **Test 2: Inject Latency**

```bash
# Chaos: Delay responses from fm-1 by 50ms
tc qdisc add dev eth0 root netem delay 50ms

# Expected Behavior (Weaver):
# Least-connections: Routes away from fm-1 (has more active connections due to slower responses)
# Requests succeed: But with increased latency (p99 ↑ from 2ms to 52ms)

# Metrics verify:
curl http://localhost:9090/metrics | grep request_duration_ms
# Expected: fm_gw_request_duration_p99{replica="fm-1"} ≈ 52ms

# Cleanup:
tc qdisc del dev eth0 root
```

**Pass Criteria:** Weaver routes around slow replica; latency recovers

---

### **Test 3: Reject Requests (Simulated Overload)**

```bash
# Chaos: Have fm-2 reject 50% of requests with errors
# (Simulate via mock replica that randomly returns 500)

# Expected Behavior (Weaver):
# T=0-30s: Requests to fm-2 fail ~50%
#          Retry on different replica (succeeds)
#          Retry latency: +20ms (backoff delay)
# T=30s+:  Circuit breaker opens (threshold reached)
#          Requests to fm-2 FAIL-FAST (no retry)
#          Clients get clear error (not long timeout)

# Metrics:
curl http://localhost:9090/metrics | grep circuit_breaker_state
# Expected: cb_gw_circuit_breaker_state{replica="fm-2"} = 1 (OPEN)

# Recovery:
# Stop error injection on fm-2
# T=30s:   Circuit breaker HALF_OPEN (try recovery)
# T=30s+:  First request succeeds → CLOSED (recovered)
```

**Pass Criteria:** Fail-fast when circuit opens; quick recovery when replica recovers

---

### **Test 4: Network Partition**

```bash
# Chaos: Isolate fm-3 from network (firewall blocks all traffic)
iptables -I INPUT -s 10.0.1.7 -j DROP  # fm-3 IP

# Expected Behavior (Weaver):
# T=0-30s: Health check to fm-3 times out (5s timeout)
#          Retry health check (3 attempts)
#          fm-3 marked UNHEALTHY
# T=30s+:  All requests route to fm-1, fm-2 only
#          No impact on other replicas (bulkhead isolation)

# Verify:
curl http://localhost:8080/debug/replicas | jq '.replicas[] | select(.name=="fm-3")'
# Expected: status = "UNHEALTHY"

# Heal network:
iptables -D INPUT -s 10.0.1.7 -j DROP

# Recovery: Same as Test 1 (automatic within ~20s)
```

**Pass Criteria:** Isolation detected within 30s; other replicas unaffected

---

### **Test 5: Configuration Change Under Load**

```bash
# Chaos: Update load balancer strategy while requests ongoing
kubectl edit configmap weaver-config -n weaver

# Change:
# load_balancers:
#   - type: "least_connections"
# to:
#   - type: "round_robin"

# Expected Behavior (Weaver):
# - Configuration reloaded within 1 second
# - No request drops (zero-restart)
# - Load distribution changes to round-robin
# - No spike in errors or latency

# Verify:
curl http://localhost:9090/metrics | grep load_balancer
# Expected: Metric shows new strategy active
```

**Pass Criteria:** Zero-restart; no errors during reload

---

## Chaos Test Suite

**Automation (Kubernetes):**
```yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: chaos-test
spec:
  schedule: "0 2 * * *"  # 2 AM daily
  jobTemplate:
    spec:
      template:
        spec:
          containers:
          - name: chaos
            image: chaos-test:1.0.0
            command:
            - ./run-chaos-suite.sh  # Runs all 5 tests, verifies pass criteria
          restartPolicy: Never
```

---

## Observability During Chaos

**Live Dashboard:**
```bash
# Terminal 1: Watch replicas
watch 'curl -s http://localhost:8080/debug/replicas | jq'

# Terminal 2: Watch metrics
watch 'curl -s http://localhost:9090/metrics | grep "fm_gw_" | head -10'

# Terminal 3: Tail logs
kubectl logs -n weaver -l app=weaver -f

# Terminal 4: Run chaos test
./chaos-test.sh
```

---

## Success Criteria (SLA Testing)

| Test | Criteria | Status |
|------|----------|--------|
| **Replica Failure** | Recovery < 30s | ✅ Pass if automatic failover within 30s |
| **Latency Injection** | P99 recovers | ✅ Pass if p99 latency returns to baseline within 10s of fix |
| **Circuit Breaker** | Fail-fast works | ✅ Pass if circuit opens within 30s of failures |
| **Network Partition** | Detected < 30s | ✅ Pass if replica marked UNHEALTHY within 30s |
| **Config Reload** | Zero-restart | ✅ Pass if config reloaded without request loss |

**Overall SLA Goal:** 99.99% uptime (<=52 min downtime/year) even under chaos

---

## Next Steps

- **Troubleshooting Guide** → [54-troubleshooting.md](../OPERATIONS/54-troubleshooting.md)
- **Performance Tuning** → [55-performance-tuning.md](../OPERATIONS/55-performance-tuning.md)
- **Configuration Reference** → [30-configuration-reference.md](../REFERENCE/30-configuration-reference.md)

---

**Navigation:**
- [← Previous](./24-failover-dr.md)
- [Index](../INDEX.md)
- [Next →](../REFERENCE/30-configuration-reference.md)
