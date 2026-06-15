# Weaver: Upgrade Guide

> **Read Time:** 15 minutes  
> **Previous:** [61-faq.md](./61-faq.md) | **Next:** [64-migration-guide.md](./64-migration-guide.md)

---

## Upgrade Checklist

```
Pre-Upgrade:
□ Review changelog for breaking changes (see 57-version-matrix.md)
□ Test in staging with identical topology
□ Backup current configuration (git commit or snapshot)
□ Plan upgrade window (30-60 min for large deployments)
□ Notify stakeholders

Upgrade:
□ Update Weaver image / binary
□ Apply changes (rolling restart for K8s)
□ Monitor: request rate, error rate, p99 latency
□ Run verification checklist (see QUICK_START/13-verify-deployment.md)
□ Check backward compatibility of config (if applicable)

Post-Upgrade:
□ Confirm all metrics flowing
□ Confirm all replicas healthy
□ Run end-to-end tests
□ Document lessons learned
```

---

## Upgrade Paths

### Kubernetes

**Rolling Restart (Zero Downtime)**

```bash
# Update ConfigMap (if config changed)
kubectl apply -f weaver-configmap.yaml

# Update Deployment image
kubectl set image deployment/weaver \
  weaver=registry/weaver:v1.2.0 \
  --record

# Monitor rollout
kubectl rollout status deployment/weaver

# Rollback if needed
kubectl rollout undo deployment/weaver
```

**Manual Upgrade (Full Control)**

```bash
# 1. Cordon current nodes
kubectl cordon node1 node2 node3

# 2. Scale to 0 (drain all traffic to other nodes)
kubectl scale deployment weaver --replicas=0

# 3. Update Deployment image
kubectl set image deployment/weaver weaver=registry/weaver:v1.2.0

# 4. Scale back up
kubectl scale deployment weaver --replicas=3

# 5. Uncordon nodes
kubectl uncordon node1 node2 node3
```

**Health Check After Upgrade**

```bash
# Wait for rollout complete
kubectl wait --for=condition=available --timeout=5m \
  deployment/weaver

# Verify all pods running
kubectl get pods -l app=weaver

# Check metrics flowing
kubectl port-forward service/weaver 9090:9090
curl http://localhost:9090/metrics | grep fm_gw_requests_total
```

---

### Docker

**Zero-Downtime Upgrade (Blue-Green)**

```bash
# 1. Start new container (blue)
docker run -d \
  --name weaver-new \
  -p 5051:5051 \
  -p 8080:8080 \
  -p 9090:9090 \
  -v $(pwd)/weaver-config.yaml:/etc/weaver/config.yaml \
  registry/weaver:v1.2.0

# 2. Verify new container healthy
curl http://localhost:5051/debug/replicas
# Should see all replicas

# 3. Switch load balancer to new container
# (Update your reverse proxy / nginx config to point to new container)

# 4. Stop old container (graceful shutdown)
docker stop weaver-old
docker wait weaver-old  # Wait for drain

# 5. Clean up
docker rm weaver-old
docker rename weaver-new weaver
```

**In-Place Upgrade (Will have brief downtime)**

```bash
# 1. Stop current container
docker stop weaver

# 2. Pull new image
docker pull registry/weaver:v1.2.0

# 3. Start new container
docker run -d \
  --name weaver \
  -p 5051:5051 \
  -p 8080:8080 \
  -p 9090:9090 \
  -v $(pwd)/weaver-config.yaml:/etc/weaver/config.yaml \
  registry/weaver:v1.2.0

# 4. Verify
curl http://localhost:5051/debug/replicas
```

---

### Standalone Binary

**Zero-Downtime Upgrade (systemd with hot-reload)**

```bash
# 1. Download new binary
curl -o /tmp/weaver-v1.2.0 \
  https://releases.example.com/weaver/v1.2.0/linux-amd64

# 2. Verify checksum
echo "expected_sha256_hash  /tmp/weaver-v1.2.0" | sha256sum -c -

# 3. Test new binary with current config
/tmp/weaver-v1.2.0 \
  -config /etc/weaver/config.yaml \
  -verify  # Dry-run mode

# 4. Backup current binary
cp /usr/local/bin/weaver /usr/local/bin/weaver.backup

# 5. Replace binary
sudo mv /tmp/weaver-v1.2.0 /usr/local/bin/weaver
sudo chmod +x /usr/local/bin/weaver

# 6. Reload systemd
sudo systemctl restart weaver

# 7. Verify upgrade
curl http://localhost:8080/debug/config | grep version

# Rollback if needed
sudo cp /usr/local/bin/weaver.backup /usr/local/bin/weaver
sudo systemctl restart weaver
```

---

## Configuration Changes Between Versions

### v1.0 → v1.1
- No breaking changes
- New optional fields: `reliability.panic_threshold`
- Backward compatible: existing v1.0 configs work as-is

### v1.1 → v1.2
- No breaking changes
- New load balancer strategy: `resource_aware`
- Backward compatible

### v1.2 → v2.0 (Hypothetical)
- **BREAKING**: `discovery.method` value changes
  - Old: `t2_etcd` → New: `etcd_fabric`
  - Migration: Update config before upgrading
- **BREAKING**: `health_check.type` field restructured
  - Old: `type: "http"` with `url` field
  - New: `type: "http"` with nested `endpoint` object
  - Migration: See config mapping below

### Config Migration Helper

For v1.2 → v2.0 (if breaking changes occur):

```bash
# Generate migration template
weaver --migrate-config v1.2 v2.0 \
  --from /etc/weaver/config-v1.2.yaml \
  --to /tmp/config-v2.0.yaml

# Review generated config
diff /etc/weaver/config-v1.2.yaml /tmp/config-v2.0.yaml

# Test new config
weaver --config /tmp/config-v2.0.yaml --verify

# Deploy when ready
sudo cp /tmp/config-v2.0.yaml /etc/weaver/config.yaml
```

---

## Monitoring During Upgrade

### Metrics to Watch

**Request Success Rate (should stay at 100%)**
```
success_rate = (fm_gw_requests_total - fm_gw_request_errors_total) / fm_gw_requests_total
```

**P99 Latency (should stay within baseline ± 10%)**
```
baseline_p99 = 50ms  (adjust to your baseline)
alert if fm_gw_request_duration_p99 > 55ms
```

**Circuit Breaker State (should stay CLOSED)**
```
fm_gw_circuit_breaker_state == 0  (CLOSED)
```

**Active Connections (should be continuous)**
```
fm_gw_replica_load should not spike
```

### Alerting Rules (Prometheus)

```yaml
groups:
- name: weaver_upgrade
  rules:
  # Fail if request success rate drops
  - alert: WeaverUpgradeFailure
    expr: |
      (increase(fm_gw_request_errors_total[5m]) / 
       increase(fm_gw_requests_total[5m])) > 0.05
    for: 2m
    annotations:
      summary: "Weaver upgrade - error rate spike"

  # Fail if latency doubles
  - alert: WeaverUpgradeLatencyRegression
    expr: |
      fm_gw_request_duration_p99 > 
      (historical_p99 * 2)
    for: 2m
    annotations:
      summary: "Weaver upgrade - latency regression"

  # Fail if circuit breaker flapping
  - alert: WeaverUpgradeCircuitBreakerFlap
    expr: |
      increase(fm_gw_circuit_breaker_transitions_total[5m]) > 10
    for: 1m
    annotations:
      summary: "Weaver upgrade - circuit breaker instability"
```

---

## Rollback Procedure

If upgrade goes wrong:

### Kubernetes

```bash
# Immediate rollback
kubectl rollout undo deployment/weaver

# Verify rollback
kubectl rollout status deployment/weaver
kubectl get pods -l app=weaver

# Restore config if needed
kubectl apply -f weaver-configmap-v1.1.yaml

# Check metrics normalize
curl http://localhost:9090/metrics | grep fm_gw_request_errors_total
```

### Docker

```bash
# Stop new container
docker stop weaver

# Restore old container
docker start weaver-old

# Update load balancer to point back to weaver-old

# Verify
curl http://localhost:5051/debug/replicas
```

### Standalone

```bash
# Restore backup binary
sudo cp /usr/local/bin/weaver.backup /usr/local/bin/weaver

# Restart service
sudo systemctl restart weaver

# Verify
curl http://localhost:8080/debug/config
```

---

## Post-Upgrade Validation

**Run full verification checklist:**

```bash
# 1. Check gateway running
curl http://localhost:8080/debug/replicas

# 2. Check replicas healthy (all should show HEALTHY)
curl http://localhost:8080/debug/replicas | jq '.replicas[] | {name, status}'

# 3. Check circuit breaker (all CLOSED)
curl http://localhost:8080/debug/circuit-breaker | jq '.circuit_breakers[] | {replica, state}'

# 4. Check metrics flowing
curl http://localhost:9090/metrics | grep -c fm_gw_requests_total

# 5. Send test traffic
for i in {1..100}; do curl http://localhost:5051/test-endpoint; done

# 6. Check error rate (should be 0%)
curl http://localhost:9090/metrics | grep fm_gw_request_errors_total

# 7. Check config applied correctly
curl http://localhost:8080/debug/config | jq '.discovery'
```

---

## Upgrade Troubleshooting

| Problem | Cause | Solution |
|---------|-------|----------|
| Pods stuck in CrashLoopBackOff | Bad config in new version | Rollback; fix config; retry |
| Connection refused | Ports not exposed | Check Service/firewall config |
| Replicas not discovered | Discovery config incompatible | Check 32-discovery-methods.md |
| Circuit breaker OPEN | New version has stricter health checks | Loosen health check settings |
| High latency after upgrade | New version uses slower LB algo | Change load_balancer strategy |
| Metrics missing | Metrics endpoint configuration changed | Check 39-metrics-reference.md |

---

**Navigation:**
- [← Previous](./61-faq.md)
- [Index](../INDEX.md)
- [Next →](./64-migration-guide.md)
