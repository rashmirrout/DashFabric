# Weaver: Production Runbook

> **Read Time:** 15 minutes  
> **Previous:** [52-monitoring-setup.md](./52-monitoring-setup.md) | **Next:** [54-troubleshooting.md](./54-troubleshooting.md)

---

## Production Operations

Standard procedures for running Weaver in production.

---

## Starting Weaver

### Kubernetes

```bash
# Deploy Weaver
kubectl apply -f weaver-deployment.yaml

# Verify rollout
kubectl rollout status deployment/weaver -n weaver
```

### Docker

```bash
# Start stack
docker-compose up -d

# Verify container running
docker ps | grep weaver
```

### Standalone Binary

```bash
# Start systemd service
sudo systemctl start weaver

# Check status
sudo systemctl status weaver
```

---

## Stopping Weaver (Graceful Shutdown)

### Kubernetes

```bash
# Trigger rolling termination (respects drain_timeout)
kubectl scale deployment weaver --replicas=0 -n weaver

# Wait for pods to terminate
kubectl wait --for=delete pod -l app=weaver -n weaver --timeout=60s
```

### Docker

```bash
# Stop container (graceful, respects drain_timeout)
docker stop weaver --time=30

# Wait for stopped
docker wait weaver
```

### Standalone

```bash
# Stop systemd service (graceful shutdown)
sudo systemctl stop weaver

# Wait for it to stop
sleep 5
sudo systemctl status weaver
```

**Note:** If drain_timeout is set, Weaver will wait that duration for in-flight requests before terminating.

---

## Restarting Weaver

### Kubernetes (Zero Downtime)

```bash
# Trigger rolling restart
kubectl rollout restart deployment/weaver -n weaver

# Monitor rollout
kubectl rollout status deployment/weaver -n weaver
```

### Docker

```bash
# Restart container
docker restart weaver

# Wait for healthy
sleep 10
curl http://localhost:8080/debug/replicas
```

### Standalone

```bash
# Restart systemd service
sudo systemctl restart weaver

# Verify restarted
sudo systemctl status weaver
```

---

## Updating Configuration

### Kubernetes

```bash
# 1. Update ConfigMap
kubectl edit configmap weaver-config -n weaver

# 2. Save (editor exits)

# 3. Trigger rolling restart to apply config
kubectl rollout restart deployment/weaver -n weaver

# 4. Monitor
kubectl rollout status deployment/weaver -n weaver
```

### Docker

```bash
# 1. Edit config file
vi /etc/weaver/config.yaml

# 2. Restart container
docker restart weaver

# 3. Verify new config applied
curl http://localhost:8080/debug/config
```

### Standalone

```bash
# 1. Edit config file
sudo vi /etc/weaver/config.yaml

# 2. Restart service
sudo systemctl restart weaver

# 3. Verify
curl http://localhost:8080/debug/config
```

---

## Health Checks

**Quick health status**
```bash
curl http://localhost:8080/debug/replicas
```

Expected response:
```json
{
  "gateway_name": "weaver-prod",
  "replicas": [
    {
      "name": "fm-primary-1",
      "status": "HEALTHY",
      "address": "10.0.1.10:5051"
    },
    {
      "name": "fm-primary-2",
      "status": "HEALTHY",
      "address": "10.0.1.11:5051"
    }
  ],
  "replicas_total": 2,
  "replicas_healthy": 2
}
```

**Check circuit breaker**
```bash
curl http://localhost:8080/debug/circuit-breaker
```

Expected:
- All replicas should show state: 0 (CLOSED)
- If any show 1 (OPEN) → investigate why replica is failing

---

## Scaling

### Scale Up (Add Replicas)

**Kubernetes**
```bash
kubectl scale deployment weaver --replicas=5 -n weaver
```

**Docker Swarm**
```bash
docker service scale weaver=5
```

**Manual (Docker)**
```bash
# Start new container
docker run -d --name weaver-2 -p 5052:5051 myregistry/weaver:v1.0.0

# Add to load balancer
# (Update nginx/HAProxy config to include new container)
```

### Scale Down (Remove Replicas)

**Kubernetes**
```bash
kubectl scale deployment weaver --replicas=2 -n weaver
```

**Docker Swarm**
```bash
docker service scale weaver=2
```

---

## Backup & Restore

**Backup Configuration**
```bash
# Kubernetes
kubectl get configmap weaver-config -n weaver -o yaml > weaver-config-backup.yaml

# Docker/Standalone
cp /etc/weaver/config.yaml /etc/weaver/config.yaml.backup
```

**Restore Configuration**
```bash
# Kubernetes
kubectl apply -f weaver-config-backup.yaml

# Docker/Standalone
cp /etc/weaver/config.yaml.backup /etc/weaver/config.yaml
systemctl restart weaver
```

---

## Log Collection

**Kubernetes**
```bash
# Real-time logs
kubectl logs -f -l app=weaver -n weaver

# Logs from last hour
kubectl logs --since=1h -l app=weaver -n weaver

# Logs from specific pod
kubectl logs pod/weaver-abc123 -n weaver
```

**Docker**
```bash
# Real-time logs
docker logs -f weaver

# Last 100 lines
docker logs --tail 100 weaver

# Since 1 hour ago
docker logs --since 1h weaver
```

**Standalone**
```bash
# journalctl
sudo journalctl -u weaver -f

# Last 50 lines
sudo journalctl -u weaver --no-pager -n 50
```

---

## Performance Baseline

**Establish baseline before production**

1. **Request rate** (requests/sec)
   - Typical: 1k-100k rps (depends on hardware)
   - Record under normal load

2. **P50 / P99 latency** (milliseconds)
   - Typical: P50 5-20ms, P99 50-100ms
   - Record under normal load

3. **Circuit breaker transitions** (per 24h)
   - Typical: 0 (no transitions expected)
   - Alert if > 10/day

4. **Error rate** (percentage)
   - Typical: < 0.1% (< 1 error per 1000 requests)
   - Alert if > 1%

**Measure at deployment:**
```bash
# Run for 1 hour under normal load
curl http://localhost:9090/metrics | grep fm_gw
```

Record:
- fm_gw_requests_total (increment over 1 hour / 3600s = rps)
- fm_gw_request_duration_p99 (milliseconds)
- fm_gw_circuit_breaker_transitions_total (should be 0)

---

## Maintenance Windows

**Schedule maintenance for:**
- OS security patches (immediate, but plan gracefully)
- Weaver upgrades (during low-traffic hours)
- Infrastructure changes (planned, with advance notice)

**During maintenance:**
1. Notify stakeholders (ops channel, status page)
2. Scale Weaver to minimal replicas (if possible) to minimize impact
3. Monitor metrics during maintenance
4. Document any issues
5. Post-maintenance validation (run verification checklist)

---

## On-Call Runbook

**If paged at 3 AM:**

1. **Is the gateway responding?**
   ```bash
   curl http://localhost:8080/debug/replicas
   ```
   If no response → restart immediately (jump to step 5)

2. **Are all replicas healthy?**
   ```bash
   curl http://localhost:8080/debug/replicas | grep status
   ```
   If any UNHEALTHY → check that replica's logs

3. **Are requests succeeding?**
   ```bash
   curl http://localhost:9090/metrics | grep fm_gw_request_errors_total
   ```
   If errors increasing → check circuit breaker

4. **Is circuit breaker in OPEN state?**
   ```bash
   curl http://localhost:8080/debug/circuit-breaker
   ```
   If OPEN → likely backend overloaded; scale backend replicas

5. **Restart if still broken**
   ```bash
   # Kubernetes
   kubectl rollout restart deployment/weaver
   
   # Docker
   docker restart weaver
   
   # Standalone
   systemctl restart weaver
   ```

6. **Escalate if still broken**
   - Page on-call platform engineer
   - Check status page (did something else change?)
   - Check recent deployments (any related PRs merged?)

---

## Incident Response

**When things go wrong:**

| Symptom | Likely Cause | Action |
|---------|--------------|--------|
| No requests flowing | Gateway down | Restart gateway; check logs |
| High error rate | Backend failing | Check backend health; scale backend |
| High latency | Gateway or backend overloaded | Add gateway / backend replicas |
| Circuit breaker OPEN | Cascading failures | Check backend; reduce rate limiting; increase timeouts |
| Memory usage high | Replica leak or memory burst | Restart; monitor closely after |
| CPU usage high | CPU-intensive operation | Check if scanning replicas; restart if needed |

---

**Navigation:**
- [← Previous](./52-monitoring-setup.md)
- [Index](../INDEX.md)
- [Next →](./54-troubleshooting.md)
