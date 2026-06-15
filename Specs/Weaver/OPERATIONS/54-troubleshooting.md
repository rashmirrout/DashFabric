# Weaver: Troubleshooting Guide

> **Read Time:** 20 minutes  
> **Previous:** [53-production-runbook.md](./53-production-runbook.md) | **Next:** [55-performance-tuning.md](./55-performance-tuning.md)

---

## Troubleshooting Decision Tree

Start here when something goes wrong.

---

## Step 1: Is Weaver Running?

```bash
# Kubernetes
kubectl get pods -l app=weaver -n weaver

# Docker
docker ps | grep weaver

# Standalone
systemctl status weaver
```

**All pods/containers running?**
- YES → Go to Step 2
- NO → Restart Weaver; check logs for startup errors

---

## Step 2: Are Replicas Discovered?

```bash
curl http://localhost:8080/debug/replicas
```

**Output shows replicas?**
- YES → Go to Step 3
- NO → Check discovery configuration

### If Replicas Not Discovered

```bash
# Check discovery method is correct
curl http://localhost:8080/debug/config | jq '.discovery'

# Verify discovery endpoints are reachable
# For etcd:
kubectl run -it --rm debug --image=alpine:3 /bin/sh
nslookup etcd.default.svc.cluster.local
nc -zv etcd 2379

# For Consul:
curl http://consul:8500/v1/catalog/services
```

**Fix:**
- Verify etcd/Consul is running
- Verify network connectivity from Weaver pod to discovery service
- Check firewall rules
- Update ConfigMap with correct endpoints; restart Weaver

---

## Step 3: Are Replicas Healthy?

```bash
curl http://localhost:8080/debug/replicas | jq '.replicas[] | {name, status}'
```

**All replicas show HEALTHY?**
- YES → Go to Step 4
- NO → Investigate unhealthy replicas

### If Replicas Unhealthy

```bash
# Check health check configuration
curl http://localhost:8080/debug/config | jq '.health'

# Test health endpoint manually
curl -v http://replica-host:replica-port/health

# Check replica logs
kubectl logs pod/replica-name
docker logs replica-container
```

**Common causes:**
- Health check endpoint doesn't exist (wrong path)
- Health check timeout too short
- Replica not responding (crashed/hung)
- Network connectivity issues

**Fix:**
- Verify replica is running: `docker ps` / `kubectl get pods`
- Verify health check endpoint: curl directly to replica
- Increase health check timeout if consistently timing out
- Restart unhealthy replica

---

## Step 4: Is Load Distributed?

```bash
# Send 10 requests, check which replica handles each
for i in {1..10}; do
  curl http://localhost:8080/debug/current-replica | jq '.replica'
done | sort | uniq -c
```

**All replicas get requests?**
- YES → Go to Step 5
- NO → Load balancer algorithm issue

### If Load Not Distributed

```bash
# Check load balancer strategy
curl http://localhost:8080/debug/config | jq '.load_balancers'

# Check replica load
curl http://localhost:9090/metrics | grep fm_gw_replica_load

# Check if all replicas in same AZ (affects sticky LB)
kubectl describe pods -l app=weaver
```

**Common causes:**
- Using Sticky LB; need multiple clients
- Consistent Hash LB; distribution depends on key
- Some replicas overloaded (resource-aware LB)

**Fix:**
- If using Sticky: expected (one client → one replica)
- If using Consistent Hash: check if key is uniformly distributed
- If using Resource-Aware: check replica CPU/memory

---

## Step 5: Are Requests Succeeding?

```bash
# Send test request
curl -v http://localhost:5051/test-endpoint

# Check error rate
curl http://localhost:9090/metrics | grep fm_gw_request_errors_total
```

**Requests returning 200?**
- YES → Go to Step 6
- NO → See error codes below

### HTTP Error Codes

| Code | Meaning | Fix |
|------|---------|-----|
| 500 | Backend error | Check backend replica logs |
| 503 | Service unavailable | All replicas unhealthy? Check circuit breaker |
| 429 | Rate limited | Check rate_limit metrics; increase limit or reduce load |
| 504 | Gateway timeout | Increase timeout in config; check backend latency |

---

## Step 6: Check Circuit Breaker

```bash
curl http://localhost:8080/debug/circuit-breaker
```

**All replicas show state 0 (CLOSED)?**
- YES → Weaver is healthy; check application
- NO → Circuit breaker is OPEN/HALF_OPEN

### If Circuit Breaker OPEN

```bash
# Check how many failures happened
curl http://localhost:9090/metrics | grep fm_gw_circuit_breaker_failures_total

# Check if failures are still happening
curl http://localhost:9090/metrics | grep fm_gw_request_errors_total | grep -A 1 replica_name
```

**Failures still happening?**
- YES → Backend is still failing; fix backend first
- NO → Wait 60s for HALF_OPEN retry (configurable)

**Fix:**
- Check if backend replicas are in panic mode
- Check if backend is overloaded (CPU/memory high)
- Check if health checks are too strict
- Reduce rate limit to reduce backend load
- Scale backend horizontally

---

## Step 7: Monitor Metrics

```bash
# Key metrics to check
curl http://localhost:9090/metrics | grep -E 'fm_gw_requests_total|fm_gw_request_errors_total|fm_gw_request_duration|fm_gw_circuit_breaker'

# Or use Grafana
http://localhost:3000
```

**Metrics look normal?**
- Request rate stable
- Error rate < 0.1%
- P99 latency < 500ms
- Circuit breaker CLOSED

---

## Common Issues & Solutions

### Issue: "replica not found"

**Symptoms:** Requests return "replica not found" error

**Diagnosis:**
```bash
curl http://localhost:8080/debug/replicas
# Shows 0 replicas
```

**Causes:**
1. Discovery service not running (etcd/Consul down)
2. Discovery endpoints wrong in config
3. No replicas registered in discovery service
4. Network connectivity broken

**Fix:**
1. Verify etcd/Consul is running: `docker ps` / `kubectl get pods`
2. Verify endpoints in config: `curl http://localhost:8080/debug/config | jq '.discovery.config.endpoints'`
3. Verify replicas registered: `curl http://consul:8500/v1/catalog/services` or `etcdctl get --prefix /weaver`
4. Test network: `nc -zv etcd 2379` or `nc -zv consul 8500`

---

### Issue: "connection refused"

**Symptoms:** `curl: (7) Failed to connect to localhost port 5051`

**Diagnosis:**
```bash
# Check if port is listening
netstat -an | grep 5051
docker port weaver | grep 5051
kubectl port-forward service/weaver 5051:5051
```

**Causes:**
1. Weaver not running
2. Port not exposed/published
3. Firewall blocking
4. Wrong port number in config

**Fix:**
1. Start Weaver: `docker run ...` / `kubectl apply -f deployment.yaml`
2. Expose port: `docker run -p 5051:5051 ...`
3. Check firewall: `iptables -L` / Security Group rules
4. Verify port in config: `curl http://localhost:8080/debug/config | jq '.listeners'`

---

### Issue: "high latency"

**Symptoms:** P99 latency > 500ms (baseline dependent)

**Diagnosis:**
```bash
# Check Jaeger traces
# http://localhost:16686
# Select Weaver service, view latency breakdown

# Check replica load
curl http://localhost:9090/metrics | grep fm_gw_replica_load

# Check backend latency
# Query backend directly to isolate gateway overhead
```

**Causes:**
1. Backend is slow (not gateway's fault)
2. Weaver overloaded (needs scaling)
3. Health checks too frequent (consuming resources)
4. Network latency (high packet loss)

**Fix:**
1. Check backend latency separately
2. Scale Weaver: `kubectl scale deployment weaver --replicas=5`
3. Reduce health check frequency: set interval to 10s+
4. Check network: `ping -c 10 replica-host` (check packet loss)

---

### Issue: "rate limited"

**Symptoms:** Some requests return 429 (Too Many Requests)

**Diagnosis:**
```bash
curl http://localhost:9090/metrics | grep fm_gw_rate_limit_exceeded_total

# Calculate rate-limited requests
increase(fm_gw_rate_limit_exceeded_total[5m])
```

**Causes:**
1. Incoming load exceeds rate limit
2. Rate limit too strict
3. Tenant quota exceeded

**Fix:**
1. Increase global rate limit in config
2. Increase per-client/per-tenant limits
3. Reduce load (scale backend, shed load elsewhere)
4. Implement client-side backoff

---

### Issue: "panic mode"

**Symptoms:** All replicas showing UNHEALTHY despite being running

**Diagnosis:**
```bash
curl http://localhost:8080/debug/replicas
# All show UNHEALTHY

# Check health check config
curl http://localhost:8080/debug/config | jq '.health'
```

**Causes:**
1. Health check endpoint doesn't exist
2. All replicas crashed
3. Network connectivity broken
4. Health check timeout too short

**Fix:**
1. Verify health endpoint exists: `curl http://replica:5051/health`
2. Verify replicas running: `docker ps` / `kubectl get pods`
3. Check network: `ping replica-host`
4. Increase health check timeout

---

## Checking Logs

### Kubernetes

```bash
# Follow logs in real-time
kubectl logs -f -l app=weaver -n weaver

# Filter for errors
kubectl logs -l app=weaver -n weaver | grep -i error

# Get logs from last 30 minutes
kubectl logs --since=30m -l app=weaver -n weaver
```

### Docker

```bash
# Follow logs
docker logs -f weaver

# Filter for errors
docker logs weaver | grep -i error

# Get last 100 lines
docker logs --tail 100 weaver
```

### Standalone

```bash
# Follow journalctl
sudo journalctl -u weaver -f

# Filter for errors
sudo journalctl -u weaver | grep -i error

# Last 50 lines
sudo journalctl -u weaver -n 50 --no-pager
```

---

## Escalation

If still stuck, escalate to:

1. **Platform team** - check if infrastructure issue (etcd down, network partition)
2. **Backend team** - if backend replicas unhealthy
3. **On-call engineer** - if unsure next step

---

**Navigation:**
- [← Previous](./53-production-runbook.md)
- [Index](../INDEX.md)
- [Next →](./55-performance-tuning.md)
