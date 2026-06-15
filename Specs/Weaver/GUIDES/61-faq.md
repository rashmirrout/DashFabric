# Weaver: FAQ

> **Read Time:** 20 minutes  
> **Previous:** [60-best-practices.md](./60-best-practices.md) | **Next:** [63-upgrade-guide.md](./63-upgrade-guide.md)

---

## General Questions

**Q1: What's the difference between Weaver and Envoy / Kong / Istio?**
- Weaver is a **configuration-driven universal gateway** for DashFabric topologies (FM primary-aware, CB peer-equivalent, custom systems)
- Envoy: Lower-level proxy (C++); requires service mesh control plane integration
- Kong: API gateway (Lua plugins); designed for REST/HTTP APIs; heavy operational overhead
- Istio: Service mesh (uses Envoy + control plane); requires Kubernetes; more distributed
- Choose Weaver if: You need simple config-driven gateway for DashFabric or custom systems

**Q2: Can I use Weaver for [my specific use case]?**
- Yes, if you have: multiple backend replicas + need load balancing / health checks / reliability patterns
- No, if you need: WebSocket upgrades (current limitation) / custom protocol filters / plugin ecosystem
- See [02-architecture-overview.md](../GET_STARTED/02-architecture-overview.md) to evaluate fit

**Q3: What's the latency overhead?**
- Typical: 1-5ms per request (varies by load balancer algorithm)
- Measured: Pod discovery → replica selection → connection → request forwarding
- Can optimize by: using Least-Connections LB, pre-warmed connections (future), tuning health checks
- See [55-performance-tuning.md](../OPERATIONS/55-performance-tuning.md) for detailed profiling

**Q4: Does Weaver support my discovery method (etcd / Consul / Kubernetes / DNS / static)?**
- Yes: 5 methods supported natively
- See [32-discovery-methods.md](../REFERENCE/32-discovery-methods.md) for each
- Can't find yours? Custom discovery possible via [71-plugin-development.md](./71-plugin-development.md)

**Q5: What happens if a replica crashes while Weaver is forwarding a request?**
- In-flight request: returns error (client should retry)
- Future requests: Weaver detects failure via health check (< 10s), removes replica from pool
- All clients: automatically start sending to healthy replicas (no config change needed)
- See [34-reliability-patterns.md](../REFERENCE/34-reliability-patterns.md)

---

## Deployment Questions

**Q6: How do I deploy Weaver on Kubernetes?**
- Quick: 5 min - see [10-kubernetes.md](../QUICK_START/10-kubernetes.md)
- Production: 30 min - see [50-kubernetes-deployment.md](../OPERATIONS/50-kubernetes-deployment.md)
- Both include: ConfigMap, Deployment, Service; copy-paste ready

**Q7: How do I deploy Weaver on Docker?**
- Dev local: 5 min - see [11-docker-compose.md](../QUICK_START/11-docker-compose.md) (includes mock backend)
- Production: see [51-docker-deployment.md](../OPERATIONS/51-docker-deployment.md)

**Q8: Can I run Weaver on bare metal / VMs?**
- Yes: see [12-standalone-binary.md](../QUICK_START/12-standalone-binary.md)
- Steps: Download binary → Create config → Run systemd service
- Systemd template included

**Q9: How many replicas should I run?**
- Minimum production: 3 (enables 1 failure tolerance)
- Recommended: 3-5 for redundancy + gradual rollouts
- Maximum: No hard limit; tested to 100+ replicas
- See [55-performance-tuning.md](../OPERATIONS/55-performance-tuning.md) for sizing

**Q10: How do I scale from 3 to 10 replicas?**
- Add 7 new replicas to discovery (etcd / Consul / K8s)
- Weaver auto-discovers within 10s (health check interval default)
- No config change needed; no downtime
- Monitor: `fm_gw_replicas_total` metric increases

---

## Configuration Questions

**Q11: How do I configure authentication?**
- 4 methods: Bearer Token, JWT, API Key, mTLS
- See [36-security.md](../REFERENCE/36-security.md) for each with examples
- Start with: API Key (simplest); graduate to JWT (most flexible)

**Q12: How do I configure authorization (RBAC)?**
- Define roles: admin, operator, viewer
- Assign permissions: read, write, register, admin
- See [36-security.md](../REFERENCE/36-security.md) for YAML structure
- Enforce on all endpoints (debug, metrics, service)

**Q13: How do I choose a load balancing strategy?**
- **Least-Connections**: Default; best for long-lived connections
- **Round-Robin**: Strict fairness; for stateless services
- **Consistent Hash**: Sticky sessions; minimizes cache misses
- **Weighted**: Send more traffic to faster replicas
- **Resource-Aware**: Uses CPU / memory metrics (if available)
- See [31-load-balancing-strategies.md](../REFERENCE/31-load-balancing-strategies.md) for detailed comparison

**Q14: Can I mix load balancing strategies?**
- No: one strategy per gateway instance
- Workaround: Run 2 gateway instances with different strategies; split traffic client-side
- Better: Choose one strategy that fits 80% of your use case

**Q15: How do I configure health checks?**
- 3 types: HTTP, gRPC, TCP
- See [33-health-monitoring.md](../REFERENCE/33-health-monitoring.md) for each
- Recommendation: Use same protocol as backend (e.g., HTTP health check for HTTP backend)

**Q16: What if a replica is unhealthy but my health check is passing?**
- Rare; indicates: replica serving 500s but health endpoint says "OK"
- Solutions: Make health check stricter (call real endpoint, not just /health); use custom health check
- See [33-health-monitoring.md](../REFERENCE/33-health-monitoring.md) for custom checks

---

## Reliability Questions

**Q17: How does circuit breaker work?**
- States: CLOSED (healthy) → OPEN (failing) → HALF_OPEN (testing recovery)
- CLOSED→OPEN: 5 consecutive failures
- OPEN→HALF_OPEN: after 60s (configurable)
- HALF_OPEN→CLOSED: success; HALF_OPEN→OPEN: failure
- See [34-reliability-patterns.md](../REFERENCE/34-reliability-patterns.md) for diagram

**Q18: What's the difference between circuit breaker and health checks?**
- Health check: Regular polling (every 5-10s); active diagnosis
- Circuit breaker: Passive observation of request failures; reacts faster (< 1s)
- Use both: health checks catch slow failure, circuit breaker catches fast failure

**Q19: How does retry work?**
- Exponential backoff: 100ms → 200ms → 400ms → 800ms (configurable)
- Max retries: configurable (default 3)
- Used for: transient failures (network glitch, timeout), not logic errors
- See [34-reliability-patterns.md](../REFERENCE/34-reliability-patterns.md)

**Q20: What if retry makes things worse?**
- Possible if: backend is overloaded; retry causes more load
- Solution: Use rate limiting + breaker instead; disable retry
- Retry works best for: infrastructure failures (network), not capacity issues

**Q21: How do I set request timeout?**
- Hierarchy: global_timeout > per_replica_timeout > per_connection_timeout
- If request takes > global_timeout: cancel and return error
- Recommendation: global=30s, per_replica=25s, connect=5s
- See [34-reliability-patterns.md](../REFERENCE/34-reliability-patterns.md)

**Q22: What happens to in-flight requests during gateway restart?**
- Depends on drain_timeout setting
- If drain_timeout=30s: Weaver waits 30s for in-flight to complete before shutdown
- If no drain: in-flight requests return error (client should retry)
- Recommendation: Set drain_timeout = max expected request latency + buffer

---

## Observability Questions

**Q23: How do I set up monitoring?**
- 3 tools: Prometheus (metrics), Jaeger (tracing), Grafana (dashboards)
- See [52-monitoring-setup.md](../OPERATIONS/52-monitoring-setup.md) for complete setup
- Time: 30 min; includes: Docker Compose stack, sample Prometheus config, Grafana dashboard

**Q24: What metrics should I alert on?**
- Key metrics:
  - `fm_gw_requests_total`: Should be increasing (sign of traffic)
  - `fm_gw_request_duration_p99`: Should be < 100ms (baseline dependent)
  - `fm_gw_circuit_breaker_state`: Should be 0 (CLOSED); alert if 1 (OPEN)
  - `fm_gw_rate_limit_exceeded_total`: Should be 0 (not rate-limited)
- See [39-metrics-reference.md](../REFERENCE/39-metrics-reference.md) for all metrics

**Q25: How do I debug a slow request?**
- Steps:
  1. Check Prometheus latency percentiles (p50/p99)
  2. Check Jaeger traces to see where time spent
  3. Check if replica is overloaded (CPU / memory)
  4. Check circuit breaker state (may be retrying)
- See [54-troubleshooting.md](../OPERATIONS/54-troubleshooting.md) for decision tree

**Q26: Can I see which replica handled my request?**
- Yes: `curl http://localhost:8080/debug/current-replica`
- Returns: `{ "replica": "fm-primary-1" }`
- Useful for: verifying load distribution, debugging replica-specific issues

---

## Operational Questions

**Q27: How do I update gateway configuration without downtime?**
- Process:
  1. Update ConfigMap / config file
  2. Trigger hot-reload: `curl -X POST http://localhost:8080/config/reload` (if implemented)
  3. If no hot-reload: Rolling restart of pods (K8s handles)
- Time: 30s - 2 min (depends on drain_timeout)
- See [50-kubernetes-deployment.md](../OPERATIONS/50-kubernetes-deployment.md) for rolling restart details

**Q28: How do I backup / restore Weaver configuration?**
- Weaver is stateless; configuration is external (ConfigMap, config file)
- Backup: `git commit weaver-config.yaml` (use version control)
- Restore: `kubectl apply -f weaver-config.yaml` (reapply config)
- See [50-kubernetes-deployment.md](../OPERATIONS/50-kubernetes-deployment.md)

**Q29: How do I debug a "replica not found" error?**
- Steps:
  1. Check replicas discovered: `curl http://localhost:8080/debug/replicas`
  2. Check discovery config (etcd_endpoints, consul_address, etc.)
  3. Check if replica is actually running at discovery address
  4. Check if replica is passing health checks
- See [54-troubleshooting.md](../OPERATIONS/54-troubleshooting.md) for detailed decision tree

**Q30: How do I upgrade Weaver to a new version?**
- For K8s: Update Deployment image tag + apply
- For Docker: Pull new image + restart container
- For binary: Download new version, restart systemd service
- Breaking changes: Check [57-version-matrix.md](../OPERATIONS/57-version-matrix.md) before upgrading
- See [63-upgrade-guide.md](./63-upgrade-guide.md) for detailed steps

---

**Navigation:**
- [← Previous](./60-best-practices.md)
- [Index](../INDEX.md)
- [Next →](./63-upgrade-guide.md)
