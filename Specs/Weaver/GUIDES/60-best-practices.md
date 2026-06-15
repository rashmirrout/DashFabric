# Weaver: Best Practices

> **Read Time:** 15 minutes  
> **Previous:** [62-glossary.md](./62-glossary.md) | **Next:** [61-faq.md](./61-faq.md)

---

## Configuration Best Practices

### ✅ DO's

**Use health checks < 10s interval**
- Enables faster failure detection (RTO < 30s)
- Configure: `interval: "5s", timeout: "2s"`

**Use exponential backoff for retries**
- Prevents thundering herd on cascading failures
- Recommended: base=100ms, multiplier=2, max=5s

**Monitor circuit breaker state transitions**
- Track `fm_gw_circuit_breaker_transitions_total` metric
- Alert on > 10 transitions/minute (sign of flapping replica)

**Tag metrics by tenant/service**
- Enables capacity planning and cost attribution
- Add labels: `tenant_id`, `service_name`, `environment`

**Enable rate limiting in production**
- Start with generous limits, tighten based on metrics
- Always use global rate limit as safety net

**Store configuration in version control**
- Git history for audit trail
- Use separate branches for environments (prod, staging, dev)

**Use separate replicas for different tenants (if possible)**
- Reduces blast radius of single tenant's misbehavior
- Enables per-tenant resource allocation

**Implement graceful shutdown**
- Drain in-flight requests before terminating
- Drain timeout: 30s (depends on longest expected request)

**Use descriptive names for replicas**
- Pattern: `{system}-{tier}-{index}` (e.g., `fm-primary-1`, `cb-read-2`)
- Aids debugging and metric correlation

**Test configuration changes in staging first**
- Prevents production rollback scenarios
- Use identical topology to production

---

### ❌ DON'Ts

**Don't set health check timeout > connection timeout**
- Health check waits forever if replica hangs on connect
- If connect_timeout=5s, health_check_timeout must be ≤ 4s

**Don't use Random load balancing for sticky sessions**
- Requests from same client may hit different replicas
- Use: Sticky, Consistent Hash, or Resource-Aware instead

**Don't disable circuit breaker in production**
- Risks cascading failures
- Exception: single-replica topology (rare)

**Don't route all traffic to a single replica**
- Defeats purpose of gateway
- Use load balancer (even if weighted)

**Don't forget to drain replicas before shutdown**
- Data loss if in-flight requests killed
- Configure `drain_timeout: "30s"` in reliability config

**Don't set rate limits too high initially**
- Defeats protection purpose
- Better to start conservative, raise based on metrics

**Don't store API keys/secrets in config directly**
- Use secret management system (Vault, K8s Secrets)
- Reference via environment variables: `${API_KEY}`

**Don't change load balancing strategy under load**
- Causes traffic spike while rebalancing
- Schedule during low-traffic window

**Don't use the same TLS cert for multiple gateways**
- Compromised cert affects all gateways
- One cert per gateway instance

**Don't rely solely on circuit breaker for protection**
- Combine with: rate limiting, timeout, health checks
- Defense in depth

---

## Deployment Best Practices

### Kubernetes

**Use resource requests and limits**
```yaml
resources:
  requests:
    cpu: 500m
    memory: 256Mi
  limits:
    cpu: 2000m
    memory: 1Gi
```

**Use readiness + liveness probes**
```yaml
readinessProbe:
  httpGet:
    path: /debug/replicas
    port: 8080
  initialDelaySeconds: 10
  periodSeconds: 5

livenessProbe:
  httpGet:
    path: /debug/replicas
    port: 8080
  initialDelaySeconds: 30
  periodSeconds: 10
```

**Use pod disruption budgets**
```yaml
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: weaver-pdb
spec:
  minAvailable: 1
  selector:
    matchLabels:
      app: weaver
```

**Use node affinity for redundancy**
- Spread replicas across availability zones
- Prevents single-zone failure from taking down gateway

**Use network policies to restrict traffic**
- Only allow: clients → gateway, gateway → backend services
- Deny all ingress by default

### Docker

**Use read-only root filesystem**
- Forces explicit mount of config directory
- Improves security posture

**Run as non-root user**
- User: `weaver` (UID 1000)
- No privileges required

**Use minimal base image**
- `alpine:3.18` reduces attack surface
- Smaller container = faster startup

**Don't use `latest` tag**
- Use semantic versioning: `v1.2.3`
- Ensures reproducible deployments

---

## Observability Best Practices

### Metrics

**Set up dashboard with golden signals**
- Request rate (requests/sec)
- Error rate (errors/sec)
- Duration (latency percentiles)
- Saturation (circuit breaker % OPEN)

**Alert on circuit breaker transitions**
- Alert if > 10 transitions/minute
- Alert if any replica OPEN > 5 minutes

**Baseline your metrics**
- Record p50/p99 latency with baseline load
- Use as reference for anomaly detection

**Use recording rules for complex queries**
```
fm_gw_error_rate = increase(fm_gw_request_errors_total[5m]) / increase(fm_gw_requests_total[5m])
```

### Tracing

**Sample all requests in development**
- Enables full visibility while debugging
- Set `sample_rate: 1.0`

**Sample 1-10% of requests in production**
- Balances visibility with performance
- Start with 1%, increase if needed

**Look at traces when debugging latency**
- Jaeger UI shows where time is spent (discovery, LB, connect, send, receive)

### Logging

**Use structured JSON logging**
- Enables parsing and aggregation
- Always include: timestamp, level, message, replica, error

**Filter logs by replica in production**
- Quickly narrow troubleshooting scope
- Example: `kubectl logs -l app=weaver,replica=fm-primary-1`

**Keep DEBUG logging in test/staging only**
- DEBUG level produces high volume
- Use INFO/WARN in production

---

## Troubleshooting Best Practices

### Start with these checks (in order)

1. **Is Weaver running?**
   - `kubectl get pods -l app=weaver`
   - `docker ps | grep weaver`

2. **Are replicas discovered?**
   - `curl http://localhost:8080/debug/replicas`
   - Should show all expected replicas with status

3. **Are replicas healthy?**
   - Check replica status in /debug/replicas (should be "HEALTHY")
   - Check Prometheus metric: `fm_gw_replica_status{replica="name"} == 1`

4. **Is the load balanced?**
   - `curl http://localhost:8080/debug/current-replica` (run 10 times)
   - Should see requests distributed across replicas

5. **Are requests succeeding?**
   - `curl -v http://localhost:5051/some-endpoint`
   - Check HTTP status code in response
   - Check Weaver logs: `kubectl logs -l app=weaver`

6. **Is circuit breaker healthy?**
   - `curl http://localhost:8080/debug/circuit-breaker`
   - Should show all replicas with state CLOSED
   - Any OPEN or HALF_OPEN warrants investigation

7. **Are metrics being collected?**
   - `curl http://localhost:9090/metrics | grep fm_gw_requests_total`
   - Should see non-zero request count

### When performance is slow

1. Check p99 latency: `fm_gw_request_duration_p99`
2. Check if any replica is near capacity (high load): `fm_gw_replica_load`
3. Check circuit breaker state (may be rejecting requests)
4. Check rate limiter: `fm_gw_rate_limit_exceeded_total` (increasing = being rate-limited)
5. Check Jaeger traces to see where time is spent

---

## Security Best Practices

**Enable TLS in production**
- Min version: 1.2 (use 1.3 if available)
- Cipher suite: prefer ECDHE + AEAD

**Rotate TLS certificates 30 days before expiration**
- Set calendar reminder for 60-day mark
- Perform rotation in zero-downtime manner

**Use authentication for all endpoints**
- Even debug endpoints should require API key
- Pattern: `curl -H "X-API-Key: secret" http://localhost:8080/debug/replicas`

**Enable RBAC and restrict permissions**
- Don't grant "admin" role broadly
- Use "operator" for operations team, "viewer" for monitoring

**Audit all configuration changes**
- Store in version control
- Review before applying to production

**Never commit secrets**
- Use `.gitignore` for `*.key`, `*.crt`, `secrets.yaml`
- Use secret management system instead

---

**Navigation:**
- [← Previous](./62-glossary.md)
- [Index](../INDEX.md)
- [Next →](./61-faq.md)
