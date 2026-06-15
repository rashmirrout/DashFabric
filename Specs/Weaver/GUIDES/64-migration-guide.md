# Weaver: Migration Guide

> **Read Time:** 20 minutes  
> **Previous:** [63-upgrade-guide.md](./63-upgrade-guide.md) | **Next:** [../OPERATIONS/50-kubernetes-deployment.md](../OPERATIONS/50-kubernetes-deployment.md)

---

## Migration Overview

Migrating from another gateway to Weaver? This guide shows how to map your current configuration.

**Migration Approach:**
1. Run Weaver in parallel (blue-green)
2. Map configuration from old system
3. Route subset of traffic to Weaver
4. Monitor and validate
5. Cut over completely
6. Decommission old system

---

## Migration from FM-Gateway

### Architecture Mapping

| FM-Gateway | Weaver |
|-----------|--------|
| FM Fabric Manager topology | Weaver with FM scenarios config |
| Pod registry (etcd) | Pod Discovery: `etcd_fabric` |
| Health checks (HTTP) | Health monitoring: `http` |
| Load balancer (least-conn) | Load balancing: `least_connections` |
| Failover logic | Circuit breaker + retry |
| Metrics (Prometheus) | Observability: `prometheus` |

### Configuration Mapping

**FM-Gateway config:**
```yaml
fm_gateway:
  port: 5051
  discovery:
    type: etcd
    endpoints: ["etcd:2379"]
  health_check:
    interval: 10s
    type: http
  load_balancer: least_connections
  failover:
    timeout: 5s
    retry_count: 3
```

**Weaver equivalent:**
```yaml
gateway:
  name: "fm-primary-aware"
  listeners:
    grpc:
      port: 5051

discovery:
  method: "etcd_fabric"
  config:
    endpoints:
      - "etcd:2379"

health:
  type: "http"
  interval: "10s"
  timeout: "2s"

load_balancers:
  default:
    strategy: "least_connections"

reliability:
  timeout:
    global: "5s"
  circuit_breaker:
    failure_threshold: 5
    success_threshold: 2
    timeout: "60s"
  retry:
    max_attempts: 3
    backoff:
      base: "100ms"
      multiplier: 2
      max: "5s"

observability:
  metrics:
    enabled: true
    namespace: "fm_gw"
    port: 9090
```

### Migration Steps

1. **Deploy Weaver alongside FM-Gateway**
   ```bash
   kubectl apply -f weaver-deployment.yaml
   # FM-Gateway still running; traffic unaffected
   ```

2. **Map configuration (see above)**
   - Copy etcd discovery endpoints
   - Copy health check settings
   - Copy load balancing strategy

3. **Test with 10% traffic**
   ```bash
   # Route 10% of clients to Weaver, 90% to FM-Gateway
   # Use Istio VirtualService or nginx upstream weights
   ```

4. **Monitor metrics**
   - Check: `fm_gw_requests_total`, `fm_gw_request_errors_total`
   - Compare with FM-Gateway metrics
   - Should see similar latency profile

5. **Increase to 50%, then 100%**
   ```bash
   # After 1 hour at 10% with 0 errors: increase to 50%
   # After 1 hour at 50% with 0 errors: increase to 100%
   ```

6. **Decommission FM-Gateway**
   ```bash
   kubectl delete deployment fm-gateway
   ```

---

## Migration from CB-Gateway

### Architecture Mapping

| CB-Gateway | Weaver |
|-----------|--------|
| Peer-equivalent replicas | Weaver with CB scenarios config |
| Pod registry (Consul) | Pod Discovery: `consul` |
| Health checks (TCP) | Health monitoring: `tcp` |
| Load balancer (round-robin) | Load balancing: `round_robin` |
| Replication protocol (gRPC) | Weaver gRPC gateway |
| Metrics (Prometheus) | Observability: `prometheus` |

### Configuration Mapping

**CB-Gateway config:**
```yaml
cb_gateway:
  port: 5051
  discovery:
    type: consul
    datacenter: "dc1"
  health_check:
    interval: 5s
    type: tcp
    port: 5051
  load_balancer: round_robin
  replication:
    quorum: 2
```

**Weaver equivalent:**
```yaml
gateway:
  name: "cb-peer-equivalent"
  listeners:
    grpc:
      port: 5051

discovery:
  method: "consul"
  config:
    datacenter: "dc1"

health:
  type: "tcp"
  port: 5051
  interval: "5s"
  timeout: "2s"

load_balancers:
  default:
    strategy: "round_robin"

reliability:
  timeout:
    global: "10s"
  circuit_breaker:
    failure_threshold: 5
    success_threshold: 2
    timeout: "60s"
  retry:
    max_attempts: 2
    backoff:
      base: "50ms"
      multiplier: 2
      max: "3s"

observability:
  metrics:
    enabled: true
    namespace: "fm_gw"
    port: 9090
```

### Migration Steps

Same as FM-Gateway (see above), but:

1. Verify Consul connectivity
   ```bash
   curl http://consul:8500/v1/catalog/services
   ```

2. Test TCP health checks
   ```bash
   nc -zv replica-host 5051
   ```

3. Validate round-robin load distribution
   ```bash
   for i in {1..20}; do 
     curl -s http://localhost:8080/debug/current-replica | jq '.replica'
   done | sort | uniq -c
   # Should see ~equal distribution
   ```

---

## Migration from Envoy

### Key Differences

| Envoy | Weaver |
|-------|--------|
| Low-level proxy; requires control plane | High-level gateway; config-driven |
| Extensive filter chain customization | Simplified: discovery → LB → backend |
| Must run with service mesh (Istio) | Standalone; no control plane required |
| Complex Envoy configuration (xDS) | Simple YAML configuration |

### When Envoy is Better
- You need: L7 HTTP filtering / request rewriting / custom logic
- You have: existing Istio / Consul service mesh infrastructure
- You need: advanced observability features

### When Weaver is Better
- You need: Simple universal gateway for DashFabric
- You want: Configuration over code
- You need: Low operational overhead

### Configuration Mapping (If Migrating)

**Envoy Listener:**
```yaml
listeners:
- name: listener_0
  address:
    socket_address:
      address: 0.0.0.0
      port_number: 5051
  filter_chains:
  - filters:
    - name: envoy.filters.network.http_connection_manager
      typed_config:
        stat_prefix: requests_all
        route_config:
          virtual_hosts:
          - name: backend
            domains: ["*"]
            routes:
            - match:
                prefix: "/"
              route:
                cluster: backend_cluster
        http_filters:
        - name: envoy.filters.http.router
```

**Weaver equivalent:**
```yaml
gateway:
  name: "envoy-equivalent"
  listeners:
    grpc:
      port: 5051

discovery:
  method: "kubernetes"  # Assuming K8s
  config:
    namespace: "default"
    service_port: 5051

load_balancers:
  default:
    strategy: "least_connections"

reliability:
  timeout:
    global: "30s"
  circuit_breaker:
    failure_threshold: 5
    success_threshold: 2
    timeout: "60s"
```

### Migration Approach

**Not recommended for full Envoy feature replacement.** Instead:

1. Keep Envoy for L7 filtering / rewriting
2. Use Weaver for replica discovery / load balancing inside VPC
3. Stack: Client → Envoy (external) → Weaver (internal) → Replicas

Or:

1. Migrate only simple Envoy deployments (no custom filters)
2. For complex deployments, keep Envoy

---

## Migration from Kong

### Architecture Mapping

| Kong | Weaver |
|-------|--------|
| API gateway (REST/HTTP) | Universal gateway (any protocol) |
| Plugin ecosystem | Configurable strategies |
| Database (PostgreSQL/Cassandra) | Stateless; external config |
| Declarative config via API | YAML configuration |
| Metrics (Prometheus plugin) | Built-in Prometheus support |

### Key Differences

**Kong is better for:**
- REST APIs with request/response transformation
- API versioning and management
- Rich plugin ecosystem
- Portal / API marketplace

**Weaver is better for:**
- gRPC and multiplexing protocols
- Low-latency gateway
- Simple configuration-driven setup
- No database dependency

### Configuration Mapping

**Kong Service:**
```yaml
services:
- name: backend
  url: "http://backend:5051"
  healthchecks:
    active:
      http_path: "/health"
      interval: 10
  load_balancer: "round-robin"
```

**Weaver equivalent:**
```yaml
discovery:
  method: "static"  # Specify backend hosts directly
  config:
    endpoints:
      - "backend-1:5051"
      - "backend-2:5051"
      - "backend-3:5051"

health:
  type: "http"
  endpoint: "/health"
  interval: "10s"

load_balancers:
  default:
    strategy: "round_robin"
```

### Migration Strategy

**Not recommended as full replacement.** Instead:

1. Kong remains gateway for REST APIs
2. Use Weaver for internal gRPC services
3. Stack: Clients → Kong (external) → Weaver (internal gRPC) → Services

Or:

1. Migrate only APIs without complex Kong plugins
2. Reimplement plugin logic in client applications
3. Run Weaver in parallel

---

## Migration from Istio

### Architecture Mapping

| Istio | Weaver |
|---------|--------|
| Service mesh (distributed) | Gateway (centralized) |
| Data plane (Envoy sidecars) | Single binary |
| Control plane (istiod) | Configuration file |
| VirtualService for routing | Load balancer config |
| DestinationRule for LB | Strategy selection |

### Key Differences

**Istio is better for:**
- Distributed service mesh (sidecar injection)
- Mutual TLS between services
- Complex traffic policies
- Multi-cluster communication

**Weaver is better for:**
- Single gateway (not distributed mesh)
- Simple load balancing
- Low operational overhead
- Configuration-driven (no control plane)

### Migration Strategy

**Not recommended as full Istio replacement.** Instead:

1. Keep Istio for inter-service communication (mTLS, policies)
2. Replace Istio Ingress with Weaver for external gateway
3. Architecture: Clients → Weaver (external) → K8s services → Istio mesh

Or:

1. Remove Istio; use Weaver for all ingress + internal routing
2. Add mTLS at application layer if needed
3. Simpler but less flexible than Istio

### Configuration Mapping

**Istio VirtualService:**
```yaml
apiVersion: networking.istio.io/v1beta1
kind: VirtualService
metadata:
  name: backend
spec:
  hosts:
  - backend
  http:
  - match:
    - uri:
        prefix: /
    route:
    - destination:
        host: backend
        subset: v1
      weight: 90
    - destination:
        host: backend
        subset: v2
      weight: 10
```

**Weaver equivalent:**
```yaml
load_balancers:
  default:
    strategy: "weighted"
    config:
      weights:
        - replica: "backend-v1-1"
          weight: 90
        - replica: "backend-v1-2"
          weight: 90
        - replica: "backend-v2-1"
          weight: 10
```

---

## General Migration Checklist

- [ ] Document current gateway configuration
- [ ] Map current config to Weaver YAML (use mapping tables above)
- [ ] Deploy Weaver in staging environment
- [ ] Test with production-like load (using tools like Apache JMeter, k6)
- [ ] Validate all replicas discovered and healthy
- [ ] Run 13-verify-deployment.md checklist
- [ ] Route subset of production traffic (10%) to Weaver
- [ ] Monitor for 24 hours; check error rates, latency
- [ ] Increase traffic to 50%; monitor another 24 hours
- [ ] Increase traffic to 100%; monitor 24 hours
- [ ] Decommission old gateway
- [ ] Document lessons learned

---

**Navigation:**
- [← Previous](./63-upgrade-guide.md)
- [Index](../INDEX.md)
- [Next →](../OPERATIONS/50-kubernetes-deployment.md)
