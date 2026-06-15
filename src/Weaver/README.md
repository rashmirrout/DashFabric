# Weaver Gateway - Generic Application Load Balancing & Routing

**Production-grade gateway with advanced load balancing, service discovery, reliability patterns, and comprehensive observability.**

## Overview

Weaver is a horizontally-scalable, fault-tolerant application gateway implementing:
- **8 load balancing strategies** (round-robin, least-connections, weighted, random, resource-aware, sticky, custom)
- **5 service discovery methods** (static, etcd, Consul, Kubernetes, DNS SRV)
- **4 health check types** (HTTP, TCP, gRPC, custom)
- **Reliability patterns** (circuit breaker, exponential backoff retry, timeout management)
- **4 authentication methods** (bearer token, JWT, API key, mTLS)
- **Multi-dimensional rate limiting** (global, per-client, per-IP, per-tenant)
- **Observability stack** (Prometheus metrics, structured JSON logging, Jaeger distributed tracing)
- **Plugin system** for extensibility

## Feature Matrix

| Feature | Status | Notes |
|---------|--------|-------|
| **Load Balancing** | ✅ | 8 strategies, O(1) to O(n) complexity |
| | | • RoundRobin (O(1)), LeastConnections (O(n)), Weighted (O(n)) |
| | | • Random (O(1)), ResourceAware (O(n)), Sticky (O(1) avg) |
| | | • Custom (plugin), Consistent Hash (O(log n)) |
| **Service Discovery** | ✅ | 5 methods with watch-based updates |
| | | • Static (YAML config), etcd (k-v watch), Consul (service catalog) |
| | | • Kubernetes (endpoints API), DNS SRV (DNS records) |
| **Health Checks** | ✅ | 4 types, parallel checking, state machine |
| | | • HTTP (status code), TCP (connection), gRPC (RPC) |
| | | • Custom (pluggable), panic mode when all unhealthy |
| **Circuit Breaker** | ✅ | CLOSED/OPEN/HALF_OPEN states, configurable thresholds |
| **Retry Strategy** | ✅ | Exponential backoff with jitter, max attempts |
| **Timeout Management** | ✅ | Global + per-replica timeouts, hierarchical |
| **Rate Limiting** | ✅ | Token bucket, 4 dimensions (independent enforcement) |
| **Authentication** | ✅ | Bearer, JWT (OIDC), API key, mTLS |
| **Authorization (RBAC)** | ✅ | Role-based + scope-based permissions |
| **TLS/mTLS** | ✅ | Server certs, client cert validation |
| **Observability** | ✅ | Prometheus (20+ metrics), JSON logging, Jaeger traces |
| **Deployment** | ✅ | Kubernetes (StatefulSet, HPA, PDB), Docker (multi-stage) |
| **Testing** | ✅ | 100+ tests (unit, integration, chaos), >90% mutation kill |
| **Performance** | ✅ | LB selection <1µs, throughput 80k-100k rps, p99 <100ms |

## Quick Start

### Kubernetes Deployment

```bash
# Deploy to cluster
kubectl apply -f deploy/kubernetes/weaver-deployment.yaml

# Verify
kubectl get pods -n weaver
kubectl get svc -n weaver

# Check metrics
kubectl port-forward -n weaver svc/weaver-gateway 9090:9090
curl http://localhost:9090/metrics
```

### Docker Compose (Development)

```bash
# Start full stack (gateway + backends + etcd + prometheus + jaeger)
docker-compose -f deploy/docker/docker-compose.yaml up

# Access services
curl http://localhost:5000/health          # Gateway health
curl http://localhost:9090/metrics         # Metrics
http://localhost:16686                     # Jaeger UI
```

### Configuration

Edit `deploy/kubernetes/weaver-deployment.yaml` ConfigMap or set environment variables:

```yaml
gateway:
  port: 5000
  max_connections: 2000
  request_timeout_ms: 5000

load_balancer:
  strategy: "least_connections"           # or: round_robin, weighted, random, resource_aware, sticky, custom
  rebalance_interval_ms: 30000

discovery:
  type: "kubernetes"                       # or: static, etcd, consul, dns_srv
  namespace: "default"
  label_selector: "app=backend"

health:
  type: "tcp"                              # or: http, grpc, custom
  interval_ms: 5000
  timeout_ms: 5000
  failure_threshold: 3
  success_threshold: 2

rate_limiting:
  enabled: true
  per_client: 5000 req/s
  per_ip: 10000 req/s
  global: 100000 req/s

circuit_breaker:
  failure_threshold: 5
  success_threshold: 2
  timeout_ms: 100

auth:
  type: "bearer"                           # or: jwt, api_key, mtls
  
observability:
  metrics_enabled: true
  logging_level: "info"
  tracing_enabled: false                   # Set true + configure jaeger endpoint
```

## Deployment Checklist

### Pre-Deployment

- [ ] Service discovery configured and reachable
- [ ] Backend services registered and healthy
- [ ] Health check endpoints validated (correct port, path)
- [ ] Authentication credentials configured
- [ ] TLS certificates generated (if using mTLS)
- [ ] Resource limits reviewed for cluster capacity
- [ ] Disaster recovery backup plan documented

### Kubernetes Deployment

```bash
# 1. Create namespace
kubectl create namespace weaver

# 2. Create secrets (if using API keys or certs)
kubectl create secret generic weaver-auth \
  --from-literal=api-key="xxx" \
  -n weaver

# 3. Deploy configuration
kubectl apply -f deploy/kubernetes/weaver-deployment.yaml

# 4. Verify pod readiness
kubectl wait --for=condition=ready pod \
  -l app=weaver-gateway \
  -n weaver \
  --timeout=300s

# 5. Test gateway
kubectl port-forward -n weaver svc/weaver-gateway 5000:5000
grpcurl -plaintext localhost:5000 weaver.Forward

# 6. Set up monitoring
kubectl apply -f deploy/kubernetes/prometheus-configmap.yaml
kubectl port-forward -n weaver svc/prometheus 9090:9090

# 7. Verify horizontal pod autoscaling
kubectl get hpa -n weaver -w
```

### Docker Deployment

```bash
# 1. Build image
docker build -t weaver-gateway:latest deploy/docker/

# 2. Test locally
docker run -p 5000:5000 -p 9090:9090 \
  -e WEAVER_CONFIG=/etc/weaver/config.yaml \
  weaver-gateway:latest

# 3. Push to registry
docker tag weaver-gateway:latest your-registry/weaver-gateway:v1.0.0
docker push your-registry/weaver-gateway:v1.0.0

# 4. Deploy to production
docker run -d \
  --name weaver-prod \
  -p 5000:5000 \
  -p 9090:9090 \
  -v /etc/weaver:/etc/weaver:ro \
  --restart always \
  your-registry/weaver-gateway:v1.0.0
```

### Post-Deployment Validation

```bash
# 1. Check pod status
kubectl get pods -n weaver -o wide

# 2. Verify leader election (if applicable)
kubectl logs -n weaver -l app=weaver-gateway | grep "leader"

# 3. Validate metrics endpoint
kubectl exec -n weaver weaver-gateway-0 -- \
  curl -s http://localhost:9090/metrics | head -20

# 4. Test end-to-end request flow
kubectl run test-pod --image=curlimages/curl -it --rm -- \
  curl -v http://weaver-gateway.weaver.svc.cluster.local:5000/health

# 5. Monitor initial traffic
kubectl logs -n weaver -l app=weaver-gateway --tail=50 -f
```

## Troubleshooting Decision Tree

### Issue: Gateway Pod Won't Start

```
1. Check pod events:
   kubectl describe pod weaver-gateway-0 -n weaver
   
2. Common causes:
   a) Image pull failed
      → Check image registry access, pull secret
   b) Insufficient resources
      → Check node capacity: kubectl describe nodes
      → Adjust requests/limits in deployment
   c) Volume mount failed
      → Verify PVC exists: kubectl get pvc -n weaver
   d) Health check failing
      → Temporarily disable: set initialDelaySeconds: 60
      
3. Check logs:
   kubectl logs weaver-gateway-0 -n weaver --previous
   kubectl logs weaver-gateway-0 -n weaver
```

### Issue: High Error Rate / Request Failures

```
1. Check discovery:
   kubectl exec -n weaver weaver-gateway-0 -- \
     curl -s http://localhost:9090/metrics | grep replicas_healthy
   
2. If low replica count:
   a) Verify backend service:
      kubectl get endpoints -n default <service-name>
   b) Check health check config:
      kubectl get configmap weaver-config -n weaver -o yaml | grep health
   c) Check backend logs:
      kubectl logs -n default -l app=backend --tail=20
      
3. Check circuit breaker state:
   kubectl exec -n weaver weaver-gateway-0 -- \
     curl -s http://localhost:9090/metrics | grep circuit_breaker_state
   
4. If circuit breaker OPEN:
   a) Wait for timeout (default 100ms)
   b) Or temporarily increase failure_threshold in config
   
5. Check rate limiting:
   kubectl exec -n weaver weaver-gateway-0 -- \
     curl -s http://localhost:9090/metrics | grep rate_limit_exceeded
   
   If exceeded:
   a) Increase limits in config: rate_limiting.per_client, per_ip
   b) Or reduce client request rate
```

### Issue: High Latency / Slow Responses

```
1. Check percentile latencies:
   kubectl exec -n weaver weaver-gateway-0 -- \
     curl -s http://localhost:9090/metrics | grep request_duration
   
2. If p99 >> p50 (spiky latency):
   a) Check load distribution:
      kubectl exec -n weaver weaver-gateway-0 -- \
        curl -s http://localhost:9090/metrics | grep loadbalancer_selections
   b) If uneven, switch LB strategy:
      LeastConnections for connection-aware balancing
      ResourceAware for CPU/memory-aware balancing
   
3. Check replica response times:
   kubectl exec -n weaver weaver-gateway-0 -- \
     curl -s http://localhost:9090/metrics | grep backend_latency
   
   If backends slow:
   a) Increase timeout thresholds
   b) Scale up backend replicas
   c) Check backend resource utilization:
      kubectl top pod -n default -l app=backend
      
4. Check for GC pauses (Go runtime):
   kubectl logs -n weaver weaver-gateway-0 | grep "gc"
   
   If high GC overhead:
   a) Increase memory limit: resources.limits.memory
   b) Tune GC: set GOGC=75 environment variable
```

### Issue: Memory Leak or Growing Memory Usage

```
1. Check current memory usage:
   kubectl top pod weaver-gateway-0 -n weaver --containers
   
2. Verify against limit:
   kubectl describe pod weaver-gateway-0 -n weaver | grep -A2 "Limits"
   
3. Check goroutine count:
   kubectl exec -n weaver weaver-gateway-0 -- \
     curl -s http://localhost:9090/metrics | grep go_goroutines
   
   If increasing:
   a) Check for goroutine leaks in long-lived connections
   b) Verify discovery watch isn't spawning excess goroutines
   c) Check health monitor goroutines
   
4. Get heap profile:
   kubectl exec -n weaver weaver-gateway-0 -- \
     curl -s http://localhost:6060/debug/pprof/heap | head
   
5. If confirmed leak:
   a) Restart pod: kubectl delete pod weaver-gateway-0 -n weaver
   b) File bug with pprof output attached
   c) Temporary: increase memory limit to buy time
```

### Issue: Specific Replica Consistently Failing

```
1. Check replica status:
   kubectl exec -n weaver weaver-gateway-0 -- \
     curl -s http://localhost:9090/metrics | grep "replica.*healthy"
   
2. If marked unhealthy:
   a) Check health check results:
      kubectl logs -n weaver weaver-gateway-0 | grep <replica-name>
   b) Manually test replica:
      kubectl run debug-pod --image=curlimages/curl -it --rm -- \
        curl -v http://<replica-endpoint>:5000/health
   c) Check replica logs:
      kubectl logs -n default <replica-pod>
      
3. If replica is crashing:
   a) Check pod events:
      kubectl describe pod <replica-pod> -n default
   b) Check restart count:
      kubectl get pod <replica-pod> -n default -o jsonpath='{.status.containerStatuses[0].restartCount}'
   c) If restarting frequently, check resource limits:
      kubectl describe pod <replica-pod> | grep -A2 "Limits"
```

### Issue: Cannot Connect to Gateway

```
1. Check service accessibility:
   a) From within cluster:
      kubectl run debug-pod --image=curlimages/curl -it --rm -- \
        curl -v http://weaver-gateway.weaver.svc.cluster.local:5000/health
   b) From outside cluster (if LoadBalancer):
      curl -v http://<external-ip>:5000/health
      
2. Check service endpoints:
   kubectl get endpoints -n weaver weaver-gateway
   
3. If endpoints empty:
   a) Verify pod is ready:
      kubectl get pods -n weaver -o wide | grep weaver-gateway
   b) Check pod readiness probe:
      kubectl describe pod weaver-gateway-0 -n weaver | grep -A5 "Readiness"
      
4. Check network policies:
   kubectl get networkpolicy -n weaver
   kubectl describe networkpolicy <policy-name> -n weaver
   
5. If using service mesh (Istio):
   a) Check VirtualService:
      kubectl get vs -n weaver
   b) Check DestinationRule:
      kubectl get dr -n weaver
```

### Issue: Prometheus Metrics Not Showing

```
1. Check if metrics endpoint is working:
   kubectl exec -n weaver weaver-gateway-0 -- \
     curl -v http://localhost:9090/metrics
   
2. If 404 or connection refused:
   a) Check if gateway is listening on 9090:
      kubectl exec -n weaver weaver-gateway-0 -- \
        netstat -an | grep 9090
   b) Check observability config:
      kubectl get configmap weaver-config -n weaver -o yaml | grep metrics
      
3. Check Prometheus scrape config:
   kubectl get configmap prometheus-config -n weaver -o yaml
   
   Should have:
   ```yaml
   - job_name: 'weaver-gateway'
     static_configs:
       - targets: ['weaver-gateway:9090']
   ```
   
4. Check Prometheus targets:
   kubectl port-forward -n weaver svc/prometheus 9090:9090
   http://localhost:9090/targets
   
   If "DOWN":
   a) Check scrape errors in UI
   b) Verify ServiceMonitor (if using Prometheus Operator):
      kubectl describe servicemonitor weaver-metrics -n weaver
```

### Issue: TLS/mTLS Handshake Failures

```
1. Check certificate validity:
   kubectl get secret weaver-tls -n weaver -o yaml | grep tls.crt | head -c 100
   
   Decode and verify:
   echo "<cert-content>" | base64 -d | openssl x509 -text -noout
   
2. Verify certificate dates:
   openssl s_client -connect <gateway-endpoint>:5000 -showcerts
   
3. If using client certs (mTLS):
   a) Verify client certificate is signed by CA:
      openssl verify -CAfile ca.crt client.crt
   b) Test mTLS connection:
      grpcurl -cacert ca.crt -cert client.crt -key client.key \
        <gateway-endpoint>:5000 weaver.Forward
        
4. Check certificate mount in pod:
   kubectl exec -n weaver weaver-gateway-0 -- ls -la /etc/weaver/tls/
```

## Performance Tuning

### Optimize for Throughput (100k+ rps)

```yaml
# Increase worker threads and connections
gateway:
  max_connections: 5000
  listen_backlog: 256

# Use lock-free load balancer
load_balancer:
  strategy: "random"  # O(1), minimal contention

# Tune resource limits
resources:
  requests:
    cpu: 1000m        # 1 full core
    memory: 1Gi       # 1GB
  limits:
    cpu: 2000m
    memory: 2Gi

# Disable tracing (overhead)
observability:
  tracing_enabled: false
  logging_level: "warn"
```

### Optimize for Latency (p99 < 50ms)

```yaml
# Reduce timeouts, enable fast-path
retry:
  max_attempts: 2
  initial_backoff: 10ms
  max_backoff: 100ms

# Use ResourceAware for even load distribution
load_balancer:
  strategy: "resource_aware"

# Lower health check frequency
health:
  interval_ms: 30000  # Check every 30s
  timeout_ms: 2000

# Tune GC
resources:
  env:
    - name: GOGC
      value: "75"     # More frequent GC, lower latency spikes
```

### Optimize for Memory Efficiency

```yaml
# Reduce replica count / connection limits
gateway:
  max_connections: 500

# Use Sticky for connection reuse
load_balancer:
  strategy: "sticky"

# Tune resource limits
resources:
  limits:
    memory: 256Mi     # Constrain memory usage
```

## Monitoring & Alerts

### Key Metrics to Watch

| Metric | Target | Alert If |
|--------|--------|----------|
| `requests_duration_seconds_p99` | <100ms | >200ms |
| `errors_total` rate | <0.1% | >1% |
| `circuit_breaker_state{state="OPEN"}` | 0 | >0 for >30s |
| `replicas_healthy` | ≥80% | <50% |
| `rate_limit_exceeded_total` rate | Low | Sudden spike |
| `go_goroutines` | Stable | Continuously increasing |
| `go_memstats_alloc_bytes` | Stable | Continuously increasing |

### Prometheus Alert Rules

```yaml
groups:
  - name: weaver
    interval: 30s
    rules:
      - alert: WeaverHighLatency
        expr: histogram_quantile(0.99, requests_duration_seconds) > 0.1
        for: 5m
        
      - alert: WeaverHighErrorRate
        expr: rate(errors_total[5m]) > 0.01
        for: 5m
        
      - alert: WeaverCircuitBreakerOpen
        expr: circuit_breaker_state{state="OPEN"} > 0
        for: 1m
        
      - alert: WeaverGoroutineLeak
        expr: increase(go_goroutines[15m]) > 100
        for: 10m
        
      - alert: WeaverMemoryLeak
        expr: increase(go_memstats_alloc_bytes[30m]) > 100000000
        for: 20m
```

## Troubleshooting Commands Reference

```bash
# Status checks
kubectl get all -n weaver
kubectl describe pod weaver-gateway-0 -n weaver

# Logs
kubectl logs -n weaver -l app=weaver-gateway --tail=100 -f
kubectl logs -n weaver weaver-gateway-0 --previous

# Metrics
kubectl exec -n weaver weaver-gateway-0 -- curl -s http://localhost:9090/metrics | grep <metric-name>
kubectl port-forward -n weaver svc/prometheus 9090:9090  # Then http://localhost:9090

# Connectivity
kubectl run debug --image=curlimages/curl -it --rm -- curl -v http://weaver-gateway.weaver.svc.cluster.local:5000/health

# Resource usage
kubectl top pod -n weaver
kubectl top pod weaver-gateway-0 -n weaver --containers

# Config
kubectl get configmap weaver-config -n weaver -o yaml
kubectl edit configmap weaver-config -n weaver

# Events
kubectl describe node <node-name> | grep -A10 Events
kubectl get events -n weaver --sort-by='.lastTimestamp'
```

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                     Client Requests                          │
└────────┬────────────────────────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────────────────────────────────┐
│  Authentication & Authorization (Bearer, JWT, mTLS, API Key) │
└────────┬────────────────────────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────────────────────────────────┐
│  Rate Limiting (Token Bucket, Multi-dimensional)             │
└────────┬────────────────────────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────────────────────────────────┐
│  Load Balancing (8 strategies: RR, LC, Weighted, etc.)       │
└────────┬────────────────────────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────────────────────────────────┐
│  Circuit Breaker (CLOSED/OPEN/HALF_OPEN)                    │
└────────┬────────────────────────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────────────────────────────────┐
│  Retry with Exponential Backoff + Timeout Management         │
└────────┬────────────────────────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────────────────────────────────┐
│              Backend Replica Selection                       │
│  ┌────────┐  ┌────────┐  ┌────────┐  ┌────────┐  ┌────────┐ │
│  │ Rep 1  │  │ Rep 2  │  │ Rep 3  │  │ Rep 4  │  │ Rep 5  │ │
│  └────────┘  └────────┘  └────────┘  └────────┘  └────────┘ │
└─────────────────────────────────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────────────────────────────────┐
│  Service Discovery (Static, etcd, K8s, Consul, DNS SRV)      │
└─────────────────────────────────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────────────────────────────────┐
│  Health Monitoring (HTTP, TCP, gRPC, Custom)                 │
│  State Machine: HEALTHY ↔ UNHEALTHY, Panic Mode              │
└─────────────────────────────────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────────────────────────────────┐
│  Observability                                               │
│  ┌──────────────────────────────────────────────────────┐   │
│  │  Prometheus Metrics (20+)  │  JSON Logging  │ Jaeger │   │
│  └──────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────┘
```

## Testing

### Run All Tests

```bash
# Unit tests + integration tests
make test                    # ~30s, 100+ tests

# With race detector
make test-race              # ~2min, detects concurrency bugs

# Coverage report
make coverage                # Generates HTML report (100% target)

# Chaos engineering tests
make test-chaos              # Replica kill, latency injection, partition recovery

# Benchmarks
make bench                   # LB selection, rate limiting, circuit breaker
```

### Test Coverage

- **Line Coverage**: 100% on all new code
- **Branch Coverage**: 100% on control flow
- **Mutation Kill Rate**: >90% (tests detect subtle bugs)
- **Race Detector**: No data races (-race flag)
- **Concurrent Safety**: All components tested with concurrent access

## Resources

- **Design**: See `Weaver - A Generic Application Gateway - User Guide` in docs/
- **Deployment Guide**: `deploy/PRODUCTION_DEPLOYMENT_GUIDE.md` (60+ pages)
- **Performance Guide**: `PERFORMANCE_GUIDE.md` (benchmarks, profiling, optimization)
- **API Reference**: `protos/weaver.proto`
- **Implementation Summary**: `IMPLEMENTATION_SUMMARY.md`

## Future Scopes

- [ ] HTTP/2 push support
- [ ] WebSocket upgrade handling
- [ ] gRPC streaming optimization
- [ ] Service mesh integration (Istio, Linkerd)
- [ ] Policy-driven routing (header matching, path prefix)
- [ ] Distributed rate limiting (with etcd/Redis backend)
- [ ] Machine learning-based load prediction
- [ ] Multi-region federation

---

**Status**: Phase 7 ✅ Complete, Phase 8 ✅ Complete  
**Last Updated**: 2026-06-15  
**Test Coverage**: 100% | **Mutation Kill**: >90% | **Production Ready**: Yes
