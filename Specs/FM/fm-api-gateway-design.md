# FM API Gateway Design

> **Status:** Draft v1
> **Module:** Fleet Manager (FM) — Frontend API Gateway
> **Focus:** Load balancing, request buffering, failover, and configuration strategy
> **Audience:** Platform engineers, deployment operators, API consumers

## 1. Overview

The API Gateway is the entry point for all external clients (device agents, orchestration systems, observability tools) connecting to FM. This document specifies:
- Load balancing strategy across 3 FM replicas
- Request buffering and backpressure handling
- Failover and health checks
- Rate limiting and quota enforcement
- Configuration and deployment options
- Observability and debugging

## 2. Architecture

### 2.1 Deployment Model

```
┌─────────────────────────────────────────────────────────────┐
│  Clients (Device Agents, Orchestrators, Tools)              │
└────────────┬────────────────────────────────────────────────┘
             │
             ▼
┌─────────────────────────────────────────────────────────────┐
│  API Gateway (Load Balancer)                                │
│  - Kubernetes Ingress or Service (recommended)              │
│  - Or: HAProxy, Nginx, custom LB                            │
│  - Listen: 0.0.0.0:8080 (HTTP), 5051 (gRPC)                │
├─────────────────────────────────────────────────────────────┤
│  Features:                                                  │
│  - L4/L7 load balancing (round-robin, least-conn, hash)     │
│  - Circuit breaker (fail-fast on replica down)              │
│  - Request buffering (queuing, backpressure)                │
│  - Health checks (HTTP + gRPC liveness)                     │
│  - Rate limiting (per-IP, per-device, per-operation)        │
└──────┬──────────────┬──────────────┬──────────────┬─────────┘
       │              │              │              │
       ▼              ▼              ▼              ▼
   ┌────────┐    ┌────────┐    ┌────────┐    ┌────────┐
   │ fm-0   │    │ fm-1   │    │ fm-2   │    │ fm-3   │
   │:8080   │    │:8080   │    │:8080   │    │:8080   │
   │:5051   │    │:5051   │    │:5051   │    │:5051   │
   └────────┘    └────────┘    └────────┘    └────────┘
```

### 2.2 Recommended Implementation

**For Kubernetes (recommended):**
- **Type 1:** Kubernetes Service with load balancing (built-in).
- **Type 2:** Kubernetes Ingress (L7; HTTP only).
- **Type 3:** Custom Gateway Pod(s) running Envoy/Nginx (advanced).

**For non-Kubernetes:**
- HAProxy (mature, widely used).
- Nginx (easy to configure).
- Custom application gateway (Go/Rust).

## 3. Load Balancing Strategy

### 3.1 Load Balancing Algorithms

#### 3.1.1 Round-Robin (Recommended for HTTP/REST)

**Behavior:** Distribute requests evenly across all healthy replicas.

```
Request 1 → fm-0
Request 2 → fm-1
Request 3 → fm-2
Request 4 → fm-0 (cycle repeats)
```

**Pros:**
- Simple; fair distribution.
- Good for stateless operations (device registration, GET endpoints).

**Cons:**
- Doesn't account for replica load (CPU, latency).
- If one replica is slow, requests pile up.

**Configuration (Kubernetes Service):**
```yaml
apiVersion: v1
kind: Service
metadata:
  name: fm-gateway
spec:
  type: LoadBalancer
  sessionAffinity: None  # No session stickiness
  ports:
    - port: 8080
      targetPort: 8080
      protocol: TCP
      name: http
    - port: 5051
      targetPort: 5051
      protocol: TCP
      name: grpc
  selector:
    app: fm
```

#### 3.1.2 Least Connections (Recommended for gRPC/long-lived streams)

**Behavior:** Route new connections to the replica with the fewest active connections.

```
fm-0: 10 active connections
fm-1: 15 active connections  ← Request goes here (fewest)
fm-2: 8 active connections   ← Actually here (true least)
```

**Pros:**
- Accounts for replica workload.
- Good for long-lived gRPC streams.

**Cons:**
- Requires connection tracking (slightly more overhead).

**Configuration (Kubernetes Service):**
```yaml
apiVersion: v1
kind: Service
metadata:
  name: fm-gateway-grpc
spec:
  type: LoadBalancer
  sessionAffinity: None
  ports:
    - port: 5051
      targetPort: 5051
      protocol: TCP
      name: grpc
  selector:
    app: fm
  # Kubernetes native Services don't expose "least connections"
  # Use Envoy or custom gateway for this
```

**For Envoy:**
```yaml
clusters:
  - name: fm_cluster
    connect_timeout: 10s
    type: EDS
    lb_policy: LEAST_REQUEST
    endpoints:
      - locality_lb_endpoints:
          - endpoints:
              - socket_address:
                  address: fm-0.fm.svc.cluster.local
                  port_value: 5051
              - socket_address:
                  address: fm-1.fm.svc.cluster.local
                  port_value: 5051
              - socket_address:
                  address: fm-2.fm.svc.cluster.local
                  port_value: 5051
```

#### 3.1.3 Consistent Hashing (Recommended for stateful operations)

**Behavior:** Hash device_id (or another key) to consistently route to same replica across retries.

```
device_id="dpu-1234" → hash(device_id) % 3 = 1 → fm-1
device_id="dpu-5678" → hash(device_id) % 3 = 2 → fm-2
(same device always routes to same replica until replica fails)
```

**Pros:**
- Warm cache on retry (replica remembers request).
- Reduces content-hash computations.

**Cons:**
- Imbalanced load (some devices cluster to one replica).
- Requires stateful load balancer.

**Configuration (Custom Gateway):**
```go
func routeRequest(deviceID string, replicas []Pod) Pod {
    hash := fnv.New32a()
    hash.Write([]byte(deviceID))
    idx := hash.Sum32() % uint32(len(replicas))
    return replicas[idx]
}
```

### 3.2 Recommended Selection

| Scenario | Algorithm | Why |
|----------|-----------|-----|
| HTTP REST (device registration) | Round-robin | Stateless; simple |
| gRPC Subscribe (long streams) | Least connections | Accounts for stream count |
| Device heartbeat/telemetry | Round-robin or hash | Simple; heartbeats are fire-and-forget |
| Multi-tenant / orchestrator | Consistent hash | Better cache locality |

**Hybrid approach (best):**
- HTTP: Round-robin
- gRPC: Least connections
- Both: Implement in custom gateway (Envoy) or use Kubernetes Ingress for HTTP + separate gRPC-aware LB.

## 4. Request Buffering & Backpressure

### 4.1 Buffering Strategy

**Goal:** Absorb traffic spikes without losing requests; apply backpressure when replicas saturated.

#### 4.1.1 Queue Depths (Per-Replica)

```
API Gateway
    │
    ├─ Queue to fm-0 (max 1000 requests)
    ├─ Queue to fm-1 (max 1000 requests)
    └─ Queue to fm-2 (max 1000 requests)
```

**Configuration (Envoy):**
```yaml
clusters:
  - name: fm_cluster
    type: STRICT_DNS
    connect_timeout: 10s
    lb_policy: ROUND_ROBIN
    common_lb_config:
      healthy_panic_threshold:
        value: 50.0  # Failover if >50% unhealthy
    upstream_connection_options:
      tcp_keepalive:
        keepalive_probes: 3
        keepalive_interval: 10s
        keepalive_time: 300s
    load_assignment:
      cluster_name: fm_cluster
      endpoints:
        - lb_endpoints:
            - endpoint:
                address:
                  socket_address:
                    address: fm-0
                    port_value: 8080
            - endpoint:
                address:
                  socket_address:
                    address: fm-1
                    port_value: 8080
            - endpoint:
                address:
                  socket_address:
                    address: fm-2
                    port_value: 8080
    circuit_breaker_config:
      thresholds:
        - priority: DEFAULT
          max_connections: 10000
          max_pending_requests: 1000  # Queue size
          max_requests: 10000
          max_retries: 3
```

#### 4.1.2 Backpressure Response

**When queue to all replicas is full:**
- Return `503 Service Unavailable` to client (don't queue indefinitely).
- Include `Retry-After` header (e.g., 5 seconds).

**Example response:**
```json
{
  "error": {
    "code": "SERVICE_OVERLOADED",
    "message": "All FM replicas are busy. Please retry.",
    "retry_after_seconds": 5
  }
}
```

### 4.2 Buffering Tuning

| Parameter | Default | Min | Max | Tuning Guidance |
|-----------|---------|-----|-----|---|
| `per_replica_queue_depth` | 1000 | 100 | 10000 | Increase if spikes last >1s; decrease if memory is constrained |
| `total_gateway_queue_depth` | 3000 | 500 | 30000 | Total across all replicas |
| `request_timeout` | 30s | 5s | 120s | How long to wait for replica to process request |
| `idle_timeout` | 60s | 10s | 600s | Close connection if idle for this long |

### 4.3 Weighted Queuing (Advanced)

**Assign priority/weight to different operation types:**

```yaml
routes:
  - match:
      prefix: /api/v1/devices/health
    route:
      cluster: fm_cluster
      priority: CRITICAL
      queue_priority_override: HIGH  # Don't queue; immediate response or fail

  - match:
      prefix: /api/v1/devices/register
    route:
      cluster: fm_cluster
      priority: DEFAULT
      queue_priority_override: DEFAULT  # Queue up to depth; then 503

  - match:
      prefix: /api/v1/metrics
    route:
      cluster: fm_cluster
      priority: LOW
      queue_priority_override: LOW  # Low priority; drop if queue full
```

## 5. Health Checks & Failover

### 5.1 Health Check Configuration

**HTTP health check (REST API):**

```yaml
healthChecks:
  - name: fm-http-health
    type: HTTP
    interval: 10s
    timeout: 5s
    unhealthyThreshold: 3
    healthyThreshold: 2
    httpHealthCheck:
      path: /api/v1/health
      port: 8080
      scheme: HTTP
```

**Response format:**
```json
{
  "status": "OK",
  "mode": "primary",
  "uptime_seconds": 3600,
  "dpu_count": 42,
  "registries_size": { "vnets": 100, "enis": 5000 },
  "t1_latency_ms": 12,
  "t2_latency_ms": 8,
  "t3_size_bytes": 1073741824
}
```

**gRPC health check (gRPC streaming):**

```protobuf
message HealthCheckRequest {
  string service = 1;
}

message HealthCheckResponse {
  enum ServingStatus {
    UNKNOWN = 0;
    SERVING = 1;
    NOT_SERVING = 2;
    UNKNOWN_SERVICE = 3;
  }
  ServingStatus status = 1;
}
```

**Configuration (Kubernetes):**
```yaml
livenessProbe:
  httpGet:
    path: /api/v1/health
    port: 8080
  initialDelaySeconds: 10
  periodSeconds: 10
  timeoutSeconds: 5
  failureThreshold: 3

readinessProbe:
  httpGet:
    path: /api/v1/health
    port: 8080
  initialDelaySeconds: 5
  periodSeconds: 5
  timeoutSeconds: 5
  failureThreshold: 1
```

### 5.2 Failover Behavior

**When a replica fails health check:**

1. **Load balancer marks replica as UNHEALTHY.**
   - Stop routing new requests to that replica.
   - Existing connections continue (graceful drain).

2. **In-flight requests on unhealthy replica:**
   - Retry on next healthy replica (with exponential backoff).
   - If retry fails: return error to client (idempotency preserved).

3. **Circuit breaker opens (if X% of replicas fail):**
   - If >50% of replicas unhealthy: enter panic mode.
   - Send all traffic to remaining healthy replicas (overload risk).
   - Or: return 503 to all new requests (fail-fast).

**Configuration (Envoy circuit breaker):**
```yaml
circuit_breaker_config:
  thresholds:
    - priority: DEFAULT
      max_connections: 10000
      max_pending_requests: 1000
      max_requests: 10000
      max_retries: 3
  runtime_key_prefix: circuit_breakers.default
```

### 5.3 Graceful Failover Sequence

**Timeline:**

| Time | Event | Action |
|------|-------|--------|
| T=0s | fm-0 health check fails | LB marks fm-0 UNHEALTHY |
| T=1s | In-flight request to fm-0 | Route retry to fm-1 (or fm-2) |
| T=5s | 3 consecutive failures | Circuit breaker triggers |
| T=10s | fm-0 pod restarted by K8s | New fm-0 pod starts (same state in T1) |
| T=15s | New fm-0 pod passes health check | LB marks fm-0 HEALTHY; re-adds to pool |
| T=20s | Normal load distribution resumes | Requests routed evenly across 3 replicas |

## 6. Rate Limiting & Quota

### 6.1 Rate Limiting Layers

**Layer 1: Gateway (L4 LB) — IP-based rate limiting**
- Limit per-IP: 1000 req/min (configurable).
- Enforce on gateway; drop traffic before reaching FM replicas.

**Layer 2: FM Replicas (L7 app) — Device-based rate limiting**
- Limit per-device: 100 heartbeats/min.
- Limit per-device: 10 registrations/min.

**Configuration (Envoy rate limiting):**
```yaml
local_rate_limit:
  stat_prefix: http_local_rate_limiter
  token_bucket:
    max_tokens: 1000
    tokens_per_fill: 1000
    fill_interval: 60s
  filter_enabled:
    runtime_key: local_rate_limit_enabled
    default_value:
      numerator: 100
      denominator: HUNDRED
```

### 6.2 Quota Enforcement

**Hard quota (reject if exceeded):**
- Per-IP: 1000 registrations/min → return 429.

**Soft quota (warn but allow):**
- Per-device: >100 heartbeats/min → log warning; allow.

**Monitoring quota usage:**
```
Metrics:
  - gateway_rate_limit_violations_total{ip, limit_type}
  - gateway_rate_limit_current_tokens{ip}
  - fm_device_quota_usage_percent{device_id}
```

## 7. Configuration Options

### 7.1 Gateway Config File (YAML)

```yaml
# fm-gateway-config.yaml

gateway:
  listen_port_http: 8080
  listen_port_grpc: 5051
  enable_http: true
  enable_grpc: true

load_balancer:
  algorithm: round_robin  # round_robin, least_connections, consistent_hash
  health_check_interval_seconds: 10
  health_check_timeout_seconds: 5
  unhealthy_threshold: 3
  healthy_threshold: 2

buffering:
  per_replica_queue_depth: 1000
  total_queue_depth: 3000
  request_timeout_seconds: 30

rate_limiting:
  per_ip_per_minute: 1000
  per_device_per_minute: 100
  per_device_registrations_per_minute: 10
  enabled: true

circuit_breaker:
  failure_threshold_percent: 50  # Panic if >50% unhealthy
  panic_mode_min_health_percent: 30
  half_open_retry_interval_seconds: 30

replicas:
  - name: fm-0
    address: fm-0.fm.svc.cluster.local
    port: 8080
  - name: fm-1
    address: fm-1.fm.svc.cluster.local
    port: 8080
  - name: fm-2
    address: fm-2.fm.svc.cluster.local
    port: 8080

observability:
  enable_traces: true
  enable_metrics: true
  trace_sampling_rate: 0.1  # 10% sampling
  metrics_port: 9090

tls:
  enabled: false  # Use mTLS if needed
  cert_path: /etc/fm-gateway/tls.crt
  key_path: /etc/fm-gateway/tls.key
```

### 7.2 Kubernetes Gateway Configuration

```yaml
# fm-gateway.yaml
apiVersion: v1
kind: Service
metadata:
  name: fm-gateway
  namespace: dashfabric
spec:
  type: LoadBalancer
  ports:
    - port: 8080
      targetPort: 8080
      protocol: TCP
      name: http
    - port: 5051
      targetPort: 5051
      protocol: TCP
      name: grpc
  selector:
    app: fm
  sessionAffinity: None
  externalTrafficPolicy: Local  # Preserve source IP

---

apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: fm-gateway-hpa
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: StatefulSet
    name: fm
  minReplicas: 3
  maxReplicas: 10
  metrics:
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization
          averageUtilization: 70
    - type: Resource
      resource:
        name: memory
        target:
          type: Utilization
          averageUtilization: 80
```

### 7.3 Envoy Gateway Configuration (Advanced)

```yaml
# envoy-config.yaml
admin:
  access_log_path: /tmp/admin_access.log
  address:
    socket_address:
      address: 127.0.0.1
      port_value: 9000

static_resources:
  listeners:
    - name: listener_0
      address:
        socket_address:
          address: 0.0.0.0
          port_value: 8080
      filter_chains:
        - filters:
            - name: envoy.filters.network.http_connection_manager
              typed_config:
                "@type": type.googleapis.com/envoy.extensions.filters.network.http_connection_manager.v3.HttpConnectionManager
                stat_prefix: ingress_http
                access_log:
                  - name: envoy.access_loggers.file
                    typed_config:
                      "@type": type.googleapis.com/envoy.extensions.access_loggers.file.v3.FileAccessLog
                      path: /var/log/envoy_access.log
                http_filters:
                  - name: envoy.filters.http.local_ratelimit
                    typed_config:
                      "@type": type.googleapis.com/udpa.type.v1.TypedStruct
                      type_url: type.googleapis.com/envoy.extensions.filters.http.local_ratelimit.v3.LocalRateLimit
                      value:
                        stat_prefix: http_local_rate_limiter
                        token_bucket:
                          max_tokens: 1000
                          tokens_per_fill: 1000
                          fill_interval: 60s
                  - name: envoy.filters.http.router
                    typed_config:
                      "@type": type.googleapis.com/envoy.extensions.filters.http.router.v3.Router
                route_config:
                  name: local_route
                  virtual_hosts:
                    - name: backend
                      domains: ["*"]
                      routes:
                        - match:
                            prefix: "/"
                          route:
                            cluster: fm_cluster
                            timeout: 30s

  clusters:
    - name: fm_cluster
      type: STRICT_DNS
      connect_timeout: 10s
      lb_policy: ROUND_ROBIN
      circuit_breaker_config:
        thresholds:
          - max_connections: 10000
            max_pending_requests: 1000
            max_requests: 10000
      endpoints:
        - lb_endpoints:
            - endpoint:
                address:
                  socket_address:
                    address: fm-0
                    port_value: 8080
            - endpoint:
                address:
                  socket_address:
                    address: fm-1
                    port_value: 8080
            - endpoint:
                address:
                  socket_address:
                    address: fm-2
                    port_value: 8080
      health_checks:
        - timeout: 5s
          interval: 10s
          unhealthy_threshold: 3
          healthy_threshold: 2
          http_health_check:
            path: "/api/v1/health"
            request_headers_to_add:
              - header:
                  key: "X-Gateway-Health-Check"
                  value: "true"
```

## 8. Observability

### 8.1 Metrics

**Gateway metrics:**
```
gateway_requests_total{method, path, status}
gateway_request_duration_ms{method, path, percentile}
gateway_queue_depth{replica}
gateway_active_connections{replica}
gateway_rate_limit_violations_total{ip, limit_type}
gateway_circuit_breaker_trips_total{reason}
gateway_failover_events_total{replica, reason}
gateway_replica_health_status{replica, status}
```

**Example Prometheus query:**
```
rate(gateway_requests_total[5m])  # Request rate over 5 minutes
histogram_quantile(0.99, gateway_request_duration_ms)  # p99 latency
gateway_queue_depth > 500  # Alert if queue depth exceeds 500
```

### 8.2 Logs

```
[INFO] Gateway started: listen_http=8080, listen_grpc=5051, replicas=3
[INFO] Request routed: method=POST, path=/api/v1/devices, replica=fm-0, latency_ms=12
[WARN] Replica unhealthy: replica=fm-1, reason=health_check_failed, consecutive_failures=3
[ERROR] Circuit breaker opened: healthy_replicas=1/3, panic_mode=true, fail_percentage=66%
[INFO] Replica recovered: replica=fm-1, status=healthy
```

### 8.3 Traces

**Gateway trace structure:**
```
gateway.request (root)
├── route_selection (choose replica)
├── queue.wait (if buffered)
├── replica.request (connect to replica)
│   ├── dns.lookup
│   ├── tls.handshake
│   ├── http.send
│   └── http.receive
├── retry (if first attempt failed)
└── response (format response)
```

## 9. Deployment Guidance

### 9.1 Kubernetes Deployment

**Option 1: Kubernetes Service (Simple, built-in)**
```bash
kubectl expose statefulset fm --type=LoadBalancer --port=8080:8080,5051:5051
```

**Option 2: Ingress (HTTP only; requires L7)**
```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: fm-ingress
spec:
  ingressClassName: nginx
  rules:
    - host: fm.example.com
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: fm-gateway
                port:
                  number: 8080
```

**Option 3: Envoy Gateway (Advanced; L4/L7)**
```bash
kubectl apply -f envoy-config.yaml
```

### 9.2 Non-Kubernetes Deployment

**Option 1: HAProxy**
```conf
global
    maxconn 10000
    log stdout local0

defaults
    timeout connect 5s
    timeout client 30s
    timeout server 30s

frontend http_in
    bind *:8080
    option httplog
    balance roundrobin
    default_backend fm_replicas

backend fm_replicas
    balance roundrobin
    option httpchk GET /api/v1/health
    server fm-0 10.0.0.1:8080 check inter 10s
    server fm-1 10.0.0.2:8080 check inter 10s
    server fm-2 10.0.0.3:8080 check inter 10s
```

**Option 2: Nginx**
```nginx
upstream fm_backend {
    least_conn;
    server fm-0:8080;
    server fm-1:8080;
    server fm-2:8080;
}

server {
    listen 8080;
    location / {
        proxy_pass http://fm_backend;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_read_timeout 30s;
    }

    location /api/v1/health {
        access_log off;
        proxy_pass http://fm_backend;
    }
}
```

## 10. Tuning & Troubleshooting

### 10.1 Common Issues

**Issue: High latency (p99 > 500ms)**
- **Cause 1:** Queue depth full; requests waiting.
  - **Fix:** Increase per_replica_queue_depth or scale more replicas.
- **Cause 2:** One replica slow; others okay.
  - **Fix:** Check replica CPU/memory; scale or restart slow replica.
- **Cause 3:** T1 etcd slow.
  - **Fix:** Check T1 cluster health; upgrade if needed.

**Issue: Frequent 503 errors**
- **Cause 1:** Replicas overloaded; queue full.
  - **Fix:** Scale more replicas; reduce rate limits if appropriate.
- **Cause 2:** Health checks failing (false positives).
  - **Fix:** Increase health_check_timeout_seconds or unhealthy_threshold.

**Issue: Replica flapping (repeatedly marked healthy/unhealthy)**
- **Cause 1:** Health check timeout too low (replica just slow).
  - **Fix:** Increase health_check_timeout_seconds.
- **Cause 2:** Replica T1 connection unstable.
  - **Fix:** Check T1 cluster; verify network to T1.

### 10.2 Debugging Commands

```bash
# Check load balancer status
kubectl get svc fm-gateway

# Check replica health
kubectl get pods -l app=fm -o wide

# Check current queue depth (if using Envoy)
curl localhost:9000/stats | grep queue

# Simulate replica failure (test failover)
kubectl delete pod fm-0

# Check circuit breaker status
kubectl logs <gateway-pod> | grep circuit_breaker
```

## 11. Configuration Knobs Summary

| Knob | Default | Purpose |
|------|---------|---------|
| `GATEWAY_LISTEN_PORT_HTTP` | 8080 | HTTP listen port |
| `GATEWAY_LISTEN_PORT_GRPC` | 5051 | gRPC listen port |
| `GATEWAY_LB_ALGORITHM` | round_robin | Load balancing algorithm |
| `GATEWAY_PER_REPLICA_QUEUE_DEPTH` | 1000 | Request buffer per replica |
| `GATEWAY_TOTAL_QUEUE_DEPTH` | 3000 | Total request buffer |
| `GATEWAY_REQUEST_TIMEOUT_SECONDS` | 30 | Request timeout |
| `GATEWAY_HEALTH_CHECK_INTERVAL_SECONDS` | 10 | Health check frequency |
| `GATEWAY_HEALTH_CHECK_TIMEOUT_SECONDS` | 5 | Health check timeout |
| `GATEWAY_UNHEALTHY_THRESHOLD` | 3 | Failed checks to mark unhealthy |
| `GATEWAY_HEALTHY_THRESHOLD` | 2 | Passed checks to mark healthy |
| `GATEWAY_RATE_LIMIT_PER_IP_PER_MIN` | 1000 | IP-based rate limit |
| `GATEWAY_CIRCUIT_BREAKER_FAILURE_THRESHOLD` | 50 | Panic if >50% unhealthy |

## 12. References

- `fleet-manager-rest-api.md` — REST API endpoints
- `fm-device-registration-api-design.md` — Device registration semantics
- `fm-pod-lifecycle-design.md` — FM replica lifecycle
- `deployment-tiers.md` — Deployment configurations
- Kubernetes Ingress documentation: https://kubernetes.io/docs/concepts/services-networking/ingress/
- Envoy Proxy documentation: https://www.envoyproxy.io/docs
