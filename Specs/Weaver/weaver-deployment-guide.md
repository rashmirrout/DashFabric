# Weaver: Deployment Guide

> **Status:** Production Ready  
> **Version:** 1.0 (Phase 1)  
> **Audience:** DevOps, SREs, Platform Engineers, Operators  
> **Last Updated:** 2026-06-15

---

## Table of Contents

1. [Deployment Models](#deployment-models)
2. [Kubernetes Deployment](#kubernetes-deployment)
3. [Docker & Compose](#docker--compose)
4. [Production Runbook](#production-runbook)
5. [Monitoring Setup](#monitoring-setup)
6. [Troubleshooting](#troubleshooting)
7. [Security Hardening](#security-hardening)
8. [Performance Tuning](#performance-tuning)

---

## Deployment Models

Weaver supports three deployment models:

1. **Kubernetes (Recommended)** — Production multi-replica deployment
2. **Docker Compose** — Development and integration testing
3. **Standalone Binary** — Small deployments, bare metal

---

## Kubernetes Deployment

### Standard Production Setup

**Architecture:**
```
Client
  ↓
Service (LoadBalancer/ClusterIP)
  ↓
┌─────────────────────────────────────┐
│ Weaver Deployment (3 replicas)      │
├─────────────────────────────────────┤
│ ├─ Pod 1: weaver:1.0.0              │
│ ├─ Pod 2: weaver:1.0.0              │
│ └─ Pod 3: weaver:1.0.0              │
└─────────────────────────────────────┘
  ↓
Backend Cluster
(FM/CB/Custom)
```

---

### Namespace

```yaml
---
apiVersion: v1
kind: Namespace
metadata:
  name: weaver
  labels:
    name: weaver
```

---

### ConfigMap

Store `weaver-config.yaml` as ConfigMap.

```yaml
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: weaver-config
  namespace: weaver
data:
  weaver-config.yaml: |
    gateway:
      name: "fm-gateway"
      mode: "primary_aware"
      
    discovery:
      type: "t2_etcd"
      poll_interval: 10s
      config:
        endpoint: "http://etcd-t2:2379"
        key_pattern: "/dashfabric/cluster/pods/fm-*"
        timeout: 5s
        
    health:
      enabled: true
      type: "http"
      interval: 10s
      timeout: 5s
      config:
        endpoint: "/api/v1/health"
        expected_status: 200
      consecutive_failures: 3
      panic_mode:
        enabled: true
        threshold_percent: 50
        
    listeners:
      grpc:
        enabled: true
        port: 5051
        max_connections: 10000
      http:
        enabled: true
        port: 8080
        max_connections: 10000
      metrics:
        enabled: true
        port: 9090
        
    reliability:
      circuit_breaker:
        enabled: true
        failure_threshold: 5
        success_threshold: 2
        timeout: 30s
      retry:
        enabled: true
        max_attempts: 3
        backoff_strategy: "exponential"
        initial_backoff: 10ms
        max_backoff: 5s
      queuing:
        enabled: true
        per_replica_depth: 1000
      timeout:
        global: 30s
        per_replica: 25s
        connect: 5s
        
    rate_limiting:
      enabled: true
      per_client:
        enabled: true
        requests_per_second: 10000
      per_ip:
        enabled: true
        requests_per_second: 5000
        
    load_balancers:
      - name: "default"
        type: "least_connections"
        
    observability:
      metrics:
        enabled: true
        namespace: "fm_gw"
        port: 9090
        path: "/metrics"
      tracing:
        enabled: true
        provider: "jaeger"
        service_name: "fm-gateway"
        sample_rate: 0.1
        config:
          endpoint: "http://jaeger-collector:6831"
      logging:
        enabled: true
        level: "INFO"
        format: "json"
        async: true
```

---

### Deployment

```yaml
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: weaver
  namespace: weaver
  labels:
    app: weaver
    version: v1
spec:
  replicas: 3
  strategy:
    type: RollingUpdate
    rollingUpdate:
      maxSurge: 1
      maxUnavailable: 1
  selector:
    matchLabels:
      app: weaver
  template:
    metadata:
      labels:
        app: weaver
        version: v1
      annotations:
        prometheus.io/scrape: "true"
        prometheus.io/port: "9090"
        prometheus.io/path: "/metrics"
    spec:
      serviceAccountName: weaver
      
      # Pod disruption budget for maintenance
      affinity:
        podAntiAffinity:
          preferredDuringSchedulingIgnoredDuringExecution:
            - weight: 100
              podAffinityTerm:
                labelSelector:
                  matchExpressions:
                    - key: app
                      operator: In
                      values:
                        - weaver
                topologyKey: kubernetes.io/hostname
      
      # Termination grace period for graceful shutdown
      terminationGracePeriodSeconds: 60
      
      containers:
        - name: weaver
          image: weaver:1.0.0
          imagePullPolicy: IfNotPresent
          
          ports:
            - name: grpc
              containerPort: 5051
              protocol: TCP
            - name: http
              containerPort: 8080
              protocol: TCP
            - name: metrics
              containerPort: 9090
              protocol: TCP
          
          # Environment variables
          env:
            - name: POD_NAME
              valueFrom:
                fieldRef:
                  fieldPath: metadata.name
            - name: POD_NAMESPACE
              valueFrom:
                fieldRef:
                  fieldPath: metadata.namespace
            - name: NODE_NAME
              valueFrom:
                fieldRef:
                  fieldPath: spec.nodeName
          
          # Resource limits and requests
          resources:
            requests:
              cpu: "500m"
              memory: "256Mi"
            limits:
              cpu: "2000m"
              memory: "1Gi"
          
          # Security context
          securityContext:
            allowPrivilegeEscalation: false
            runAsNonRoot: true
            runAsUser: 1000
            readOnlyRootFilesystem: true
            capabilities:
              drop:
                - ALL
          
          # Volume mounts
          volumeMounts:
            - name: config
              mountPath: /etc/weaver
              readOnly: true
            - name: tmp
              mountPath: /tmp
            - name: cache
              mountPath: /var/cache/weaver
          
          # Liveness probe (restart if unhealthy)
          livenessProbe:
            httpGet:
              path: /debug/replicas
              port: 8080
            initialDelaySeconds: 30
            periodSeconds: 10
            timeoutSeconds: 5
            failureThreshold: 3
          
          # Readiness probe (remove from LB if not ready)
          readinessProbe:
            httpGet:
              path: /debug/replicas
              port: 8080
            initialDelaySeconds: 10
            periodSeconds: 5
            timeoutSeconds: 3
            failureThreshold: 2
          
          # Startup probe (slow startup scenarios)
          startupProbe:
            httpGet:
              path: /debug/replicas
              port: 8080
            initialDelaySeconds: 0
            periodSeconds: 10
            timeoutSeconds: 5
            failureThreshold: 30
      
      # Volumes
      volumes:
        - name: config
          configMap:
            name: weaver-config
        - name: tmp
          emptyDir: {}
        - name: cache
          emptyDir: {}
```

---

### Service

Expose Weaver internally (for FM/CB clusters).

```yaml
---
apiVersion: v1
kind: Service
metadata:
  name: weaver
  namespace: weaver
  labels:
    app: weaver
spec:
  type: ClusterIP              # Internal only
  selector:
    app: weaver
  ports:
    - name: grpc
      port: 5051
      targetPort: grpc
      protocol: TCP
    - name: http
      port: 8080
      targetPort: http
      protocol: TCP
    - name: metrics
      port: 9090
      targetPort: metrics
      protocol: TCP
```

For external access (testing, dashboards):

```yaml
---
apiVersion: v1
kind: Service
metadata:
  name: weaver-external
  namespace: weaver
spec:
  type: LoadBalancer            # External IP assigned
  selector:
    app: weaver
  ports:
    - name: grpc
      port: 5051
      targetPort: grpc
      protocol: TCP
    - name: http
      port: 8080
      targetPort: http
      protocol: TCP
```

---

### ServiceMonitor (Prometheus)

Enable Prometheus scraping of metrics.

```yaml
---
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: weaver
  namespace: weaver
  labels:
    app: weaver
spec:
  selector:
    matchLabels:
      app: weaver
  endpoints:
    - name: metrics
      port: metrics
      interval: 30s
      path: /metrics
      scheme: http
```

---

### RBAC

Service account and role for Weaver.

```yaml
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: weaver
  namespace: weaver

---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: weaver
rules:
  # For K8s discovery (if using)
  - apiGroups: [""]
    resources: ["pods", "endpoints"]
    verbs: ["get", "list", "watch"]
  - apiGroups: [""]
    resources: ["namespaces"]
    verbs: ["get", "list"]

---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: weaver
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: weaver
subjects:
  - kind: ServiceAccount
    name: weaver
    namespace: weaver
```

---

### PodDisruptionBudget

Ensure availability during maintenance.

```yaml
---
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: weaver
  namespace: weaver
spec:
  minAvailable: 2              # Keep at least 2 pods available
  selector:
    matchLabels:
      app: weaver
```

---

### HorizontalPodAutoscaler

Auto-scale based on load.

```yaml
---
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: weaver
  namespace: weaver
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: weaver
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
  behavior:
    scaleDown:
      stabilizationWindowSeconds: 300
      policies:
        - type: Percent
          value: 50
          periodSeconds: 15
    scaleUp:
      stabilizationWindowSeconds: 0
      policies:
        - type: Percent
          value: 100
          periodSeconds: 15
        - type: Pods
          value: 2
          periodSeconds: 60
      selectPolicy: Max
```

---

## Docker & Compose

### Dockerfile

Multi-stage build for minimal image.

```dockerfile
# Build stage
FROM golang:1.21-alpine AS builder

WORKDIR /build

# Install dependencies
RUN apk add --no-cache git make ca-certificates

# Copy source
COPY . .

# Build binary
RUN CGO_ENABLED=0 GOOS=linux go build -o weaver ./cmd/weaver

# Runtime stage
FROM alpine:3.18

# Install runtime dependencies
RUN apk add --no-cache ca-certificates tzdata

# Create non-root user
RUN addgroup -g 1000 weaver && adduser -u 1000 -G weaver -s /sbin/nologin weaver

WORKDIR /app

# Copy binary from builder
COPY --from=builder /build/weaver .

# Copy config template
COPY config/weaver-config.yaml.template /etc/weaver/config.yaml.template

# Create directories
RUN mkdir -p /var/cache/weaver /tmp && \
    chown -R weaver:weaver /var/cache/weaver /tmp /app

USER weaver

EXPOSE 5051 8080 9090

HEALTHCHECK --interval=10s --timeout=5s --start-period=5s --retries=3 \
    CMD wget --quiet --tries=1 --spider http://localhost:8080/debug/replicas || exit 1

ENTRYPOINT ["./weaver"]
CMD ["--config", "/etc/weaver/weaver-config.yaml"]
```

---

### Docker Compose

Development and integration testing setup.

```yaml
---
version: '3.8'

services:
  # Weaver Gateway
  weaver:
    build: .
    container_name: weaver
    ports:
      - "5051:5051"    # gRPC
      - "8080:8080"    # HTTP
      - "9090:9090"    # Metrics
    volumes:
      - ./config/weaver-config-local.yaml:/etc/weaver/weaver-config.yaml:ro
    environment:
      LOG_LEVEL: "DEBUG"
    depends_on:
      - etcd
      - fm-replica-1
      - fm-replica-2
      - fm-replica-3
    networks:
      - weaver-net
    healthcheck:
      test: ["CMD", "wget", "--quiet", "--tries=1", "--spider", "http://localhost:8080/debug/replicas"]
      interval: 10s
      timeout: 5s
      retries: 3
      start_period: 10s

  # etcd for pod discovery
  etcd:
    image: quay.io/coreos/etcd:v3.5.0
    container_name: etcd
    environment:
      ETCD_LISTEN_CLIENT_URLS: "http://0.0.0.0:2379"
      ETCD_ADVERTISE_CLIENT_URLS: "http://etcd:2379"
    ports:
      - "2379:2379"
    volumes:
      - etcd-data:/etcd-data
    networks:
      - weaver-net

  # Mock FM Replicas
  fm-replica-1:
    image: weaver-test-replica:1.0.0
    container_name: fm-replica-1
    environment:
      REPLICA_NAME: "fm-1"
      REPLICA_ID: "fm-1"
      HEALTH_PORT: "5050"
      ETCD_ENDPOINT: "http://etcd:2379"
    ports:
      - "5101:5050"    # Health check port
      - "6101:6050"    # gRPC port
    networks:
      - weaver-net
    depends_on:
      - etcd

  fm-replica-2:
    image: weaver-test-replica:1.0.0
    container_name: fm-replica-2
    environment:
      REPLICA_NAME: "fm-2"
      REPLICA_ID: "fm-2"
      HEALTH_PORT: "5050"
      ETCD_ENDPOINT: "http://etcd:2379"
    ports:
      - "5102:5050"
      - "6102:6050"
    networks:
      - weaver-net
    depends_on:
      - etcd

  fm-replica-3:
    image: weaver-test-replica:1.0.0
    container_name: fm-replica-3
    environment:
      REPLICA_NAME: "fm-3"
      REPLICA_ID: "fm-3"
      HEALTH_PORT: "5050"
      ETCD_ENDPOINT: "http://etcd:2379"
    ports:
      - "5103:5050"
      - "6103:6050"
    networks:
      - weaver-net
    depends_on:
      - etcd

  # Prometheus for metrics collection
  prometheus:
    image: prom/prometheus:v2.40.0
    container_name: prometheus
    volumes:
      - ./config/prometheus.yml:/etc/prometheus/prometheus.yml:ro
      - prometheus-data:/prometheus
    ports:
      - "9090:9090"
    command:
      - "--config.file=/etc/prometheus/prometheus.yml"
      - "--storage.tsdb.path=/prometheus"
    networks:
      - weaver-net
    depends_on:
      - weaver

  # Jaeger for distributed tracing
  jaeger:
    image: jaegertracing/all-in-one:latest
    container_name: jaeger
    ports:
      - "5775:5775/udp"
      - "6831:6831/udp"
      - "16686:16686"   # Jaeger UI
    networks:
      - weaver-net

volumes:
  etcd-data:
  prometheus-data:

networks:
  weaver-net:
    driver: bridge
```

---

### Local Config for Docker Compose

```yaml
# config/weaver-config-local.yaml
gateway:
  name: "fm-gateway-local"
  mode: "primary_aware"

discovery:
  type: "t2_etcd"
  poll_interval: 5s           # Faster in dev
  config:
    endpoint: "http://etcd:2379"
    key_pattern: "/dashfabric/cluster/pods/fm-*"

health:
  type: "http"
  interval: 5s                # Faster in dev
  timeout: 2s
  config:
    endpoint: "/api/v1/health"
  consecutive_failures: 2      # More lenient in dev

listeners:
  grpc:
    enabled: true
    port: 5051
  http:
    enabled: true
    port: 8080
  metrics:
    enabled: true
    port: 9090

reliability:
  circuit_breaker:
    enabled: true
    failure_threshold: 3       # Lower threshold in dev
    success_threshold: 1
    timeout: 10s
  retry:
    enabled: true
    max_attempts: 2
    backoff_strategy: "exponential"
    initial_backoff: 50ms
  queuing:
    enabled: true
    per_replica_depth: 100

load_balancers:
  - name: "default"
    type: "least_connections"

observability:
  metrics:
    enabled: true
    namespace: "fm_gw"
  tracing:
    enabled: true
    provider: "jaeger"
    sample_rate: 1.0          # 100% in dev
  logging:
    enabled: true
    level: "DEBUG"
```

---

## Production Runbook

### Startup Checklist

Before deploying to production:

- [ ] All config files validated with schema
- [ ] Credentials (TLS certs, API keys) rotated
- [ ] Monitoring setup complete (Prometheus, Jaeger, alerts)
- [ ] Backup/restore procedures tested
- [ ] Capacity planning done (CPU, memory, network)
- [ ] Load testing passed (target SLA met)
- [ ] Security audit completed
- [ ] Documentation up-to-date

### Startup Procedure

```bash
# 1. Verify cluster health
kubectl get nodes -o wide

# 2. Create namespace
kubectl create namespace weaver

# 3. Apply RBAC
kubectl apply -f rbac.yaml

# 4. Apply ConfigMap
kubectl apply -f configmap.yaml

# 5. Apply Deployment (starts with replicas: 1; scale up gradually)
kubectl apply -f deployment.yaml --validate=true

# 6. Monitor rollout
kubectl rollout status deployment/weaver -n weaver --timeout=5m

# 7. Verify all pods running
kubectl get pods -n weaver

# 8. Check logs
kubectl logs -n weaver -l app=weaver --tail=50

# 9. Port forward for manual testing
kubectl port-forward -n weaver svc/weaver 5051:5051 &
kubectl port-forward -n weaver svc/weaver 8080:8080 &

# 10. Test health endpoint
curl http://localhost:8080/debug/replicas | jq .

# 11. Test metrics endpoint
curl http://localhost:9090/metrics | grep fm_gw

# 12. Scale to desired replicas
kubectl scale deployment weaver -n weaver --replicas=3

# 13. Apply HPA
kubectl apply -f hpa.yaml

# 14. Apply ServiceMonitor (if using Prometheus Operator)
kubectl apply -f servicemonitor.yaml
```

---

### Shutdown Procedure

```bash
# 1. Cordon nodes (prevent new pods)
kubectl cordon <node-name>

# 2. Drain workloads gracefully (respects terminationGracePeriod)
kubectl drain <node-name> --ignore-daemonsets --grace-period=60

# 3. Monitor pod eviction
watch kubectl get pods -n weaver

# 4. Once all pods moved, uncordon
kubectl uncordon <node-name>

# 5. For full shutdown:
kubectl delete deployment weaver -n weaver
kubectl delete service weaver -n weaver
kubectl delete configmap weaver-config -n weaver
kubectl delete namespace weaver
```

---

### Upgrading Weaver

```bash
# 1. Test new image
docker pull weaver:1.1.0
docker run --rm weaver:1.1.0 --version

# 2. Update image in Deployment (triggers rolling update)
kubectl set image deployment/weaver weaver=weaver:1.1.0 -n weaver

# 3. Monitor rollout (2 pods with new version, 1 with old; then all new)
kubectl rollout status deployment/weaver -n weaver

# 4. Verify health after upgrade
kubectl logs -n weaver -l app=weaver --since=1m

# 5. Rollback if issues (auto-restarts with old image)
kubectl rollout undo deployment/weaver -n weaver
```

---

## Monitoring Setup

### Prometheus Configuration

```yaml
# config/prometheus.yml
global:
  scrape_interval: 30s
  evaluation_interval: 30s

alerting:
  alertmanagers:
    - static_configs:
        - targets: ["alertmanager:9093"]

rule_files:
  - "weaver-rules.yml"

scrape_configs:
  - job_name: 'weaver'
    kubernetes_sd_configs:
      - role: pod
        namespaces:
          names:
            - weaver
    relabel_configs:
      - source_labels: [__meta_kubernetes_pod_annotation_prometheus_io_scrape]
        action: keep
        regex: true
      - source_labels: [__meta_kubernetes_pod_annotation_prometheus_io_path]
        action: replace
        target_label: __metrics_path__
        regex: (.+)
      - source_labels: [__address__, __meta_kubernetes_pod_annotation_prometheus_io_port]
        action: replace
        regex: ([^:]+)(?::\d+)?;(\d+)
        replacement: $1:$2
        target_label: __address__
```

---

### Alert Rules

```yaml
# config/weaver-rules.yml
groups:
  - name: weaver.rules
    interval: 30s
    rules:
      # All replicas unhealthy
      - alert: AllReplicasUnhealthy
        expr: count(fm_gw_replica_status == 1) == 0
        for: 2m
        annotations:
          summary: "Weaver: All replicas unhealthy"
          description: "No healthy FM replicas available. Check replica logs and network."

      # High error rate
      - alert: HighErrorRate
        expr: |
          (rate(fm_gw_requests_total{status="error"}[5m]) / 
           rate(fm_gw_requests_total[5m])) > 0.05
        for: 5m
        annotations:
          summary: "Weaver: High error rate (>5%)"
          description: "Error rate is {{ $value | humanizePercentage }}"

      # Latency spike
      - alert: HighLatency
        expr: histogram_quantile(0.99, fm_gw_request_latency_ms) > 100
        for: 5m
        annotations:
          summary: "Weaver: High latency (p99 > 100ms)"
          description: "P99 latency is {{ $value }}ms"

      # Circuit breaker open
      - alert: CircuitBreakerOpen
        expr: count(fm_gw_circuit_breaker_state == 1) > 0
        for: 1m
        annotations:
          summary: "Weaver: Circuit breaker open for {{ $labels.replica }}"
          description: "Replica {{ $labels.replica }} has circuit breaker OPEN. Check replica health."

      # Queue overflowing
      - alert: QueueOverflow
        expr: (fm_gw_request_queue_depth / 1000) > 0.8
        for: 2m
        annotations:
          summary: "Weaver: Queue near capacity for {{ $labels.replica }}"
          description: "Queue depth is {{ $value | humanizePercentage }}% of capacity"
```

---

### Grafana Dashboard

Example dashboard queries:

```json
{
  "panels": [
    {
      "title": "Replica Health Status",
      "targets": [
        {
          "expr": "fm_gw_replica_status{job=\"weaver\"}"
        }
      ]
    },
    {
      "title": "Request Latency (p99)",
      "targets": [
        {
          "expr": "histogram_quantile(0.99, fm_gw_request_latency_ms{job=\"weaver\"})"
        }
      ]
    },
    {
      "title": "Request Rate",
      "targets": [
        {
          "expr": "rate(fm_gw_requests_total[1m])"
        }
      ]
    },
    {
      "title": "Error Rate",
      "targets": [
        {
          "expr": "rate(fm_gw_requests_total{status=\"error\"}[1m])"
        }
      ]
    },
    {
      "title": "Circuit Breaker States",
      "targets": [
        {
          "expr": "fm_gw_circuit_breaker_state"
        }
      ]
    },
    {
      "title": "Queue Depth",
      "targets": [
        {
          "expr": "fm_gw_request_queue_depth"
        }
      ]
    }
  ]
}
```

---

## Troubleshooting

### Issue: Pods stuck in CrashLoopBackOff

**Symptoms:**
```bash
kubectl get pods -n weaver
NAME                     READY   STATUS             RESTARTS   AGE
weaver-8f7c9d8b4-abc12   0/1     CrashLoopBackOff   5          2m
```

**Diagnosis:**
```bash
# Check logs
kubectl logs -n weaver weaver-8f7c9d8b4-abc12 --tail=100

# Check events
kubectl describe pod -n weaver weaver-8f7c9d8b4-abc12
```

**Common Causes & Fixes:**

1. **Config file syntax error:**
   ```bash
   # Validate YAML
   kubectl apply -f configmap.yaml --dry-run=client
   
   # Fix config
   vi config/weaver-config.yaml
   
   # Re-apply
   kubectl apply -f configmap.yaml
   ```

2. **Resource limits too low:**
   ```yaml
   # Increase limits in deployment.yaml
   resources:
     requests:
       cpu: "1000m"
       memory: "512Mi"
   ```

3. **Image pull error:**
   ```bash
   # Check image exists
   docker pull weaver:1.0.0
   
   # Check registry credentials
   kubectl create secret docker-registry regcred \
     --docker-server=docker.io \
     --docker-username=<user> \
     --docker-password=<pass>
   ```

---

### Issue: Pods running but not healthy (readiness probe failing)

**Symptoms:**
```bash
kubectl get pods -n weaver
NAME                     READY   STATUS    RESTARTS   AGE
weaver-8f7c9d8b4-abc12   0/1     Running   0          2m
```

**Diagnosis:**
```bash
# Check readiness probe
kubectl get event -n weaver | grep weaver-8f7c9d8b4-abc12

# Test manually
kubectl port-forward -n weaver pod/weaver-8f7c9d8b4-abc12 8080:8080
curl http://localhost:8080/debug/replicas
```

**Common Causes & Fixes:**

1. **Pod discovery failing (no replicas found):**
   - Check etcd connectivity: `kubectl exec -it <pod> -c weaver -- wget http://etcd:2379/v2/keys`
   - Check key pattern: Ensure replicas registered at correct path
   - Check discovery config in ConfigMap

2. **All replicas unhealthy:**
   - Check health endpoint on replica: `curl http://<replica>:5050/api/v1/health`
   - Check firewall/network policies
   - Increase health check timeout

3. **Readiness probe timeout:**
   ```yaml
   readinessProbe:
     timeoutSeconds: 10    # Increase from 3
     initialDelaySeconds: 20  # Increase from 10
   ```

---

### Issue: Memory leak (memory increasing over time)

**Diagnosis:**
```bash
# Check memory usage
kubectl top pods -n weaver

# Check goroutine count in logs
kubectl logs -n weaver <pod-name> | grep "goroutines="
```

**Common Causes & Fixes:**

1. **Queue depth accumulating (requests not being drained):**
   - Check if replicas are healthy and responsive
   - Increase queue overflow: `overflow_behavior: "drop_oldest"`
   - Reduce queue depth: `per_replica_depth: 100`

2. **Goroutine leak in handlers:**
   - Check if gRPC streams are properly closed
   - Check handler cleanup code
   - Upgrade to latest Weaver version with fixes

3. **Metrics accumulation:**
   - Reduce sample rate: `tracing: sample_rate: 0.01`
   - Reduce logging level: `logging: level: "WARN"`

---

### Issue: High latency (p99 > 100ms)

**Diagnosis:**
```bash
# Check replica latency distribution
curl http://localhost:9090/metrics | grep "request_latency_ms"

# Check specific replica performance
curl http://localhost:8080/debug/replicas | jq '.replicas[] | {name, active_connections, avg_latency_ms}'

# Check traces
curl http://jaeger:16686/api/traces?service=fm-gateway | jq '.data[] | {trace_id, duration_ms}'
```

**Common Causes & Fixes:**

1. **Replica overloaded (too many connections):**
   - Switch to resource-aware LB: `type: "resource_aware"`
   - Scale up replicas

2. **Network latency:**
   - Check network policy: `kubectl describe networkpolicy`
   - Check pod-to-pod connectivity: `kubectl exec <pod> -- ping <other-pod>`

3. **Queuing backup:**
   - Reduce per_replica_depth: Fail fast instead of queuing
   - Increase max_replicas in HPA

---

### Issue: Rate limiting rejecting legitimate traffic

**Diagnosis:**
```bash
# Check rate limit violations
curl http://localhost:9090/metrics | grep "rate_limit_violations"

# Check which clients are being limited
kubectl logs -n weaver <pod-name> | grep "rate_limit_violation"
```

**Common Causes & Fixes:**

1. **Rate limit too aggressive:**
   ```yaml
   rate_limiting:
     per_client:
       requests_per_second: 50000   # Increase limit
   ```

2. **Shared IP behind NAT (multiple clients same IP):**
   - Use per-client limiting instead of per-IP
   - Extract client_id from header: `extractor_key: "x-client-id"`

3. **Burst traffic pattern:**
   - Increase token bucket size (allowing bursts): `burst_size: 50000`
   - Use different rate limits for different hours

---

## Security Hardening

### TLS/mTLS Configuration

```yaml
tls:
  enabled: true
  
  # Server certificate
  server:
    cert_path: "/etc/weaver/certs/server.crt"
    key_path: "/etc/weaver/certs/server.key"
    
  # Client certificate verification
  client:
    enabled: true
    ca_cert_path: "/etc/weaver/certs/ca.crt"
    required: true          # Enforce mTLS

  min_version: "1.3"        # TLS 1.3 only
  
  cipher_suites:
    - "TLS_AES_256_GCM_SHA384"
    - "TLS_CHACHA20_POLY1305_SHA256"
```

### Generating Certificates

```bash
# Generate CA key and cert
openssl genrsa -out ca.key 4096
openssl req -new -x509 -days 3650 -key ca.key -out ca.crt \
  -subj "/CN=weaver-ca/O=dashfabric"

# Generate server key and cert
openssl genrsa -out server.key 4096
openssl req -new -key server.key -out server.csr \
  -subj "/CN=weaver.weaver.svc.cluster.local/O=dashfabric"
openssl x509 -req -days 365 -in server.csr \
  -CA ca.crt -CAkey ca.key -CAcreateserial -out server.crt

# Store in K8s Secret
kubectl create secret tls weaver-certs \
  --cert=server.crt \
  --key=server.key \
  --ca=ca.crt \
  -n weaver
```

### Network Policies

Restrict traffic to/from Weaver pods.

```yaml
---
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: weaver-netpol
  namespace: weaver
spec:
  podSelector:
    matchLabels:
      app: weaver
  policyTypes:
    - Ingress
    - Egress
  
  # Allow ingress from FM/CB replicas only
  ingress:
    - from:
        - namespaceSelector:
            matchLabels:
              name: fm
      ports:
        - protocol: TCP
          port: 5051
    - from:
        - namespaceSelector:
            matchLabels:
              name: cb
      ports:
        - protocol: TCP
          port: 5051
    # Allow Prometheus scraping
    - from:
        - namespaceSelector:
            matchLabels:
              name: monitoring
      ports:
        - protocol: TCP
          port: 9090
  
  # Allow egress to:
  # - etcd (discovery)
  # - FM/CB replicas (backend)
  # - DNS
  egress:
    - to:
        - namespaceSelector:
            matchLabels:
              name: infra
      ports:
        - protocol: TCP
          port: 2379   # etcd
    - to:
        - namespaceSelector:
            matchLabels:
              name: fm
      ports:
        - protocol: TCP
          port: 5050
    - to:
        - namespaceSelector:
            matchLabels:
              name: cb
      ports:
        - protocol: TCP
          port: 5050
    - to:
        - namespaceSelector:
            matchLabels:
              name: kube-system
      ports:
        - protocol: UDP
          port: 53    # DNS
```

---

## Performance Tuning

### CPU & Memory Optimization

```yaml
# Deployment resource settings
resources:
  requests:
    cpu: "1000m"        # Initial allocation
    memory: "512Mi"
  limits:
    cpu: "2000m"        # Max usage
    memory: "1Gi"

# HPA scaling
maxReplicas: 10
metrics:
  - type: Resource
    resource:
      name: cpu
      target:
        averageUtilization: 70     # Scale at 70% CPU
  - type: Resource
    resource:
      name: memory
      target:
        averageUtilization: 80     # Scale at 80% memory
```

### Request Queue Tuning

Tune queue depth based on typical request size and latency.

```
queue_depth = (p99_latency_ms * requests_per_second / 1000) * safety_factor
            = (100 * 10000 / 1000) * 1.5
            = 1500

# Set in config
reliability:
  queuing:
    per_replica_depth: 1500
```

### Connection Pool Sizing

```yaml
listeners:
  grpc:
    max_connections: 10000   # Per replica
    keepalive_interval: 30s
  http:
    max_connections: 10000
```

---

**End of Deployment Guide**

Production-ready deployment, monitoring, troubleshooting, security, and performance tuning covered.
