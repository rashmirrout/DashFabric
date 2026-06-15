# Weaver: Verify Deployment (10 Minutes)

> **Read Time:** 10 minutes  
> **Audience:** Operators, QA  
> **Previous:** [12-standalone-binary.md](./12-standalone-binary.md) | **Next:** [SCENARIOS/](../SCENARIOS/)

---

## Verification Checklist

After deploying Weaver (via Kubernetes, Docker Compose, or standalone), verify these components are working:

---

## ✅ Check 1: Weaver Is Running

### **Kubernetes**

```bash
# Check pods
kubectl get pods -n weaver

# Expected: 3 pods in "Running" state
kubectl logs -n weaver -l app=weaver --tail=5
```

### **Docker Compose**

```bash
# Check containers
docker-compose ps

# Expected: All containers in "Up" state
docker-compose logs weaver | tail -5
```

### **Standalone**

```bash
# Check process
ps aux | grep weaver

# Expected: weaver process running
```

---

## ✅ Check 2: Weaver Is Listening

### **Test gRPC Port (5051)**

```bash
# Check port is listening
netstat -tuln | grep 5051

# Or use grpcurl
grpcurl -plaintext localhost:5051 list

# Expected: List of gRPC services
```

### **Test HTTP Port (8080)**

```bash
# Check port is listening
curl -i http://localhost:8080/debug/replicas

# Expected: 200 OK with replica JSON
```

### **Test Metrics Port (9090)**

```bash
# Check port is listening
curl http://localhost:9090/metrics | head -10

# Expected: Prometheus metrics
```

---

## ✅ Check 3: Replicas Are Discovered

```bash
# Query debug API for replica list
curl http://localhost:8080/debug/replicas | jq .

# Expected output:
# {
#   "replicas": [
#     {
#       "name": "fm-1",
#       "address": "10.0.1.5",
#       "port": 5051,
#       "status": "HEALTHY",
#       "metrics": {
#         "requests_total": 1000,
#         "errors": 0
#       }
#     },
#     ...
#   ]
# }
```

**Diagnostics:**

| Issue | Check |
|-------|-------|
| No replicas listed | Verify discovery config points to correct etcd/Consul |
| Replicas show "UNHEALTHY" | Check health check config; verify replicas are running |
| Old replicas still listed | Pod discovery may be cached; wait 10 seconds and retry |

---

## ✅ Check 4: Health Checks Are Working

```bash
# Monitor replica status changes
watch 'curl -s http://localhost:8080/debug/replicas | jq ".replicas[] | {name, status}"'

# Expected: Should show transitions as replicas go up/down
```

### **Test Health Check Manually**

```bash
# Get a replica address
REPLICA_ADDR=$(curl -s http://localhost:8080/debug/replicas | jq -r '.replicas[0].address')

# Test health check directly
curl -v http://$REPLICA_ADDR:5050/api/v1/health

# Expected: 200 OK
```

---

## ✅ Check 5: Load Balancing Is Working

```bash
# Send multiple requests and observe which replica handles them
for i in {1..10}; do
  curl -s http://localhost:8080/debug/current-replica | jq '.replica_name'
done

# Expected: Should see distribution across multiple replicas (if using least-connections)
```

### **Check Load Balancer Metrics**

```bash
# Query metrics to see load distribution
curl -s http://localhost:9090/metrics | grep fm_gw_replica_load

# Expected:
# fm_gw_replica_load{replica="fm-1"} = 2
# fm_gw_replica_load{replica="fm-2"} = 3
# fm_gw_replica_load{replica="fm-3"} = 1
```

---

## ✅ Check 6: Circuit Breaker Is Configured

```bash
# Check circuit breaker state for each replica
curl -s http://localhost:8080/debug/circuit-breaker | jq '.state_by_replica'

# Expected:
# {
#   "fm-1": "CLOSED",
#   "fm-2": "CLOSED",
#   "fm-3": "CLOSED"
# }
```

### **Test Circuit Breaker Activation**

```bash
# Shut down a replica
docker-compose stop fm-replica-1  # (Docker Compose example)

# Wait for health checks to fail (3 × interval = 30 seconds)
sleep 35

# Check circuit breaker state
curl -s http://localhost:8080/debug/circuit-breaker | jq '.state_by_replica'

# Expected: fm-1 should be "OPEN"

# Restart replica
docker-compose start fm-replica-1

# Wait for recovery
sleep 20

# Expected: fm-1 should be "CLOSED" again
```

---

## ✅ Check 7: Retries Are Working

```bash
# Monitor retry metrics
curl -s http://localhost:9090/metrics | grep retry_total

# Expected:
# fm_gw_retry_total{replica="fm-1"} = 5
# fm_gw_retry_total{replica="fm-2"} = 3
```

### **Trigger Retries Intentionally**

```bash
# Send request that times out
timeout 5 grpcurl -plaintext \
  -d '{"topic":"slow-topic"}' \
  localhost:5051 FM.Broker/Subscribe

# Should see retry attempts in logs
kubectl logs -n weaver -l app=weaver | grep "retry"
```

---

## ✅ Check 8: Observability Is Working

### **Check Prometheus Metrics**

```bash
# Query a metric
curl -s http://localhost:9090/metrics | grep "fm_gw_requests_total" | head -3

# Expected:
# fm_gw_requests_total{replica="fm-1"} 1000
# fm_gw_requests_total{replica="fm-2"} 950
# fm_gw_requests_total{replica="fm-3"} 980
```

### **Check Jaeger Traces (if enabled)**

```bash
# Open Jaeger UI
open http://localhost:16686

# Search for recent traces
# You should see traces from Weaver showing request flow
# Each trace should show:
#   - Pod discovery (1ms)
#   - Load balancing (0.1ms)
#   - Connect (5ms)
#   - Request (2ms)
#   Total: 8ms
```

### **Check Logs**

```bash
# View structured logs
kubectl logs -n weaver -l app=weaver | jq 'select(.level=="ERROR")'

# Expected: Minimal or no ERROR logs during normal operation
```

---

## ✅ Check 9: Rate Limiting Is Active

```bash
# Send requests above rate limit
for i in {1..15000}; do
  curl -s http://localhost:8080/api/v1/operation > /dev/null &
done

# Check rate limiter metrics
curl -s http://localhost:9090/metrics | grep rate_limit_rejected

# Expected:
# fm_gw_rate_limit_rejected_total{dimension="global"} > 0
```

---

## ✅ Check 10: Configuration Is Hot-Reloadable

```bash
# Update config
kubectl edit configmap weaver-config -n weaver

# Change something (e.g., log level to "DEBUG")
# Save and exit

# Check Weaver reloaded within 1 second
kubectl logs -n weaver -l app=weaver --tail=5 | grep "reloaded"

# Expected: [INFO] Configuration reloaded (no restart!)
```

---

## 🧪 Integration Test: Full Request Flow

```bash
# Test gRPC Subscribe (bidirectional stream)
grpcurl -plaintext -d '{"topic":"config"}' \
  localhost:5051 FM.Broker/Subscribe

# Expected:
# Subscription opens
# Updates stream from replica
# Trace visible in Jaeger
# Metrics incremented
# No errors in logs
```

---

## 📊 Production Readiness Checklist

Before moving to production, verify:

```
□ All 3 replicas are HEALTHY
□ Circuit breaker is CLOSED for all replicas
□ Health check interval is < 10s
□ Rate limiting is configured
□ Metrics are being scraped (check Prometheus UI)
□ Tracing is enabled (check Jaeger)
□ Logging is at INFO level (not DEBUG)
□ Configuration is hot-reloadable
□ At least 3 Weaver pods are running (HA)
□ Resource limits are set
□ Network policies are in place
□ TLS is enabled for upstream communication
□ Authentication is enabled
```

---

## ⚠️ Troubleshooting

| Issue | Diagnosis | Fix |
|-------|-----------|-----|
| **Replicas show UNHEALTHY** | Health check failing | Check replica health endpoint; verify network connectivity |
| **No metrics in Prometheus** | Scrape target down | Verify ServiceMonitor; check firewall |
| **Jaeger shows no traces** | Tracing not configured | Check `observability.tracing.enabled` in config |
| **High error rates** | Check logs | `kubectl logs -n weaver -l app=weaver -f` |
| **Requests timeout** | Replicas slow | Check replica performance; increase timeout in config |

---

## Performance Baselines

After verification, expect:

| Metric | Typical Range |
|--------|---------------|
| Request latency (p50) | < 1ms |
| Request latency (p99) | < 5ms |
| Throughput | 10k - 100k req/s (depending on replica performance) |
| CPU per pod | 10% - 50% (under load) |
| Memory per pod | 50MB - 200MB |
| Replica failover time | 5 - 30 seconds |

---

## Next Steps

- **See Real Scenarios** → [20-fm-primary-aware.md](../SCENARIOS/20-fm-primary-aware.md)
- **Configure Weaver** → [30-configuration-reference.md](../REFERENCE/30-configuration-reference.md)
- **Production Deployment** → [50-kubernetes-deployment.md](../OPERATIONS/50-kubernetes-deployment.md)
- **Troubleshooting** → [54-troubleshooting.md](../OPERATIONS/54-troubleshooting.md)

---

**Navigation:**
- [← Previous](./12-standalone-binary.md)
- [Index](../INDEX.md)
- [Next →](../SCENARIOS/20-fm-primary-aware.md)
