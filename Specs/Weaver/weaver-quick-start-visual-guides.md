# Weaver: Quick-Start Visual Guides

> **Status:** Production Documentation  
> **Version:** 1.0 (Phase 1)  
> **Audience:** DevOps, Platform Engineers, Operators  
> **Last Updated:** 2026-06-15

---

## Overview

One-page visual guides for getting Weaver running in 5-15 minutes.

**Choose your deployment model:**

1. **[Kubernetes](#kubernetes-quick-start)** — Production deployment (recommended)
2. **[Docker Compose](#docker-compose-quick-start)** — Development & testing
3. **[Standalone Binary](#standalone-binary-quick-start)** — Small deployments

---

## Kubernetes Quick Start

### System Diagram

```
┌────────────────────────────────────────────────────────────────┐
│ KUBERNETES CLUSTER                                             │
├────────────────────────────────────────────────────────────────┤
│                                                                │
│  ┌─────────────────────────────────────────────────────────┐ │
│  │ Namespace: weaver                                       │ │
│  │                                                         │ │
│  │  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐ │ │
│  │  │  Pod 1       │  │  Pod 2       │  │  Pod 3       │ │ │
│  │  │  weaver:1.0  │  │  weaver:1.0  │  │  weaver:1.0  │ │ │
│  │  │              │  │              │  │              │ │ │
│  │  │ :5051        │  │ :5051        │  │ :5051        │ │ │
│  │  │ :8080        │  │ :8080        │  │ :8080        │ │ │
│  │  │ :9090        │  │ :9090        │  │ :9090        │ │ │
│  │  └──────────────┘  └──────────────┘  └──────────────┘ │ │
│  │         │                  │                  │        │ │
│  │         └──────────────────┼──────────────────┘        │ │
│  │                            │                           │ │
│  │  ┌───────────────────────────────────────────────────┐ │ │
│  │  │ Service: weaver (ClusterIP :5051, :8080, :9090) │ │ │
│  │  └───────────────────────────────────────────────────┘ │ │
│  │                                                         │ │
│  │  ┌───────────────────────────────────────────────────┐ │ │
│  │  │ ConfigMap: weaver-config                         │ │ │
│  │  │ (weaver-config.yaml mounted at /etc/weaver)     │ │ │
│  │  └───────────────────────────────────────────────────┘ │ │
│  │                                                         │ │
│  └─────────────────────────────────────────────────────────┘ │
│                            │                                 │
│        ┌───────────────────┼───────────────────┐            │
│        ↓                   ↓                   ↓             │
│  Backend Cluster (FM / CB / Custom)                         │
└────────────────────────────────────────────────────────────────┘
```

### 5-Minute Setup

**Step 1: Verify Prerequisites**
```bash
# Check kubectl
kubectl version --short

# Check cluster connectivity
kubectl cluster-info

# Create namespace
kubectl create namespace weaver
```

**Step 2: Create ConfigMap**
```bash
# Option A: From weaver-config.yaml
kubectl create configmap weaver-config \
  --from-file=weaver-config.yaml \
  -n weaver

# Option B: From example below
kubectl apply -f - <<EOF
apiVersion: v1
kind: ConfigMap
metadata:
  name: weaver-config
  namespace: weaver
data:
  weaver-config.yaml: |
    gateway:
      name: "fm-gateway"
    discovery:
      type: "t2_etcd"
      config:
        endpoint: "http://etcd-t2:2379"
        key_pattern: "/dashfabric/cluster/pods/fm-*"
    health:
      type: "http"
      interval: 10s
      config:
        endpoint: "/api/v1/health"
    listeners:
      grpc:
        enabled: true
        port: 5051
      http:
        enabled: true
        port: 8080
EOF
```

**Step 3: Deploy**
```bash
# Create Deployment (3 replicas)
kubectl apply -f - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: weaver
  namespace: weaver
spec:
  replicas: 3
  selector:
    matchLabels:
      app: weaver
  template:
    metadata:
      labels:
        app: weaver
    spec:
      containers:
      - name: weaver
        image: weaver:1.0.0
        ports:
        - containerPort: 5051
        - containerPort: 8080
        - containerPort: 9090
        volumeMounts:
        - name: config
          mountPath: /etc/weaver
      volumes:
      - name: config
        configMap:
          name: weaver-config
EOF

# Create Service
kubectl apply -f - <<EOF
apiVersion: v1
kind: Service
metadata:
  name: weaver
  namespace: weaver
spec:
  type: ClusterIP
  selector:
    app: weaver
  ports:
  - name: grpc
    port: 5051
    targetPort: 5051
  - name: http
    port: 8080
    targetPort: 8080
  - name: metrics
    port: 9090
    targetPort: 9090
EOF
```

**Step 4: Verify**
```bash
# Check pods
kubectl get pods -n weaver
# Expected: 3 pods in Running state

# Check service
kubectl get svc -n weaver

# Test health endpoint (port-forward)
kubectl port-forward -n weaver svc/weaver 8080:8080 &
curl http://localhost:8080/debug/replicas | jq .

# Check metrics
curl http://localhost:9090/metrics | grep fm_gw
```

**Step 5: Integration Test**
```bash
# Create test client pod
kubectl run -it test-client --image=curlimages/curl --rm -n weaver -- /bin/sh

# Inside pod:
curl http://weaver:8080/debug/replicas | jq '.replicas[] | {name, status}'
```

### Copy-Paste Configuration

**File: weaver-config.yaml**
```yaml
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
    path: "/metrics"

load_balancers:
  - name: "default"
    type: "least_connections"

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

observability:
  metrics:
    enabled: true
    namespace: "fm_gw"
    port: 9090
  tracing:
    enabled: true
    provider: "jaeger"
    sample_rate: 0.1
    config:
      endpoint: "http://jaeger:6831"
  logging:
    enabled: true
    level: "INFO"
    format: "json"
    async: true
```

### Troubleshooting

| Issue | Fix |
|-------|-----|
| Pods stuck in CrashLoopBackOff | Check logs: `kubectl logs -n weaver <pod>` |
| No replicas discovered | Verify etcd endpoint and key pattern |
| Services not responding | Check readiness probe: `kubectl describe pod -n weaver <pod>` |
| High latency | Check if all 3 pods are running; scale up if needed |

---

## Docker Compose Quick Start

### System Diagram

```
┌─────────────────────────────────────────────────────┐
│ Docker Compose Stack                                │
├─────────────────────────────────────────────────────┤
│                                                     │
│  ┌──────────────┐  ┌──────────────┐                │
│  │  weaver      │  │  etcd        │                │
│  │  :5051       │  │  :2379       │                │
│  │  :8080       │  │              │                │
│  │  :9090       │  └──────────────┘                │
│  └──────────────┘                                  │
│         │                                          │
│  ┌──────┴──────────────────────────────────┐     │
│  │                                          │     │
│  │  ┌─────────┐  ┌─────────┐ ┌─────────┐ │     │
│  │  │fm-rep-1 │  │fm-rep-2 │ │fm-rep-3 │ │     │
│  │  │:5101    │  │:5102   │ │:5103   │ │     │
│  │  └─────────┘  └─────────┘ └─────────┘ │     │
│  │  (Mock FM replicas)                    │     │
│  │                                          │     │
│  └──────────────────────────────────────────┘     │
│                                                     │
│  ┌──────────────┐  ┌──────────────┐                │
│  │prometheus    │  │jaeger        │                │
│  │:9090 (alt)   │  │:16686        │                │
│  └──────────────┘  └──────────────┘                │
│                                                     │
└─────────────────────────────────────────────────────┘
```

### 5-Minute Setup

**Step 1: Prepare Files**
```bash
# Create directory
mkdir weaver-dev && cd weaver-dev

# Create docker-compose.yaml (see below)
# Create weaver-config-local.yaml (see below)
```

**Step 2: Start Stack**
```bash
# Build and start all services
docker-compose up -d

# Verify all containers running
docker-compose ps
```

**Step 3: Verify Connectivity**
```bash
# Check Weaver is responding
curl http://localhost:8080/debug/replicas | jq .

# Check etcd is up
curl http://localhost:2379/v2/keys/dashfabric/cluster/pods/fm-1

# Check metrics
curl http://localhost:9090/metrics | grep fm_gw

# Check Jaeger UI (open browser)
# http://localhost:16686
```

**Step 4: Send Test Request**
```bash
# Test Subscribe (read)
grpcurl -plaintext -d '{"topic":"config"}' \
  localhost:5051 FM.Broker/Subscribe

# Should receive updates from mock FM replicas
```

**Step 5: Clean Up**
```bash
# Stop all containers
docker-compose down

# Remove volumes
docker-compose down -v
```

### Copy-Paste Configuration

**File: docker-compose.yaml**
```yaml
version: '3.8'

services:
  weaver:
    image: weaver:1.0.0
    container_name: weaver
    ports:
      - "5051:5051"    # gRPC
      - "8080:8080"    # HTTP
      - "9090:9090"    # Metrics
    volumes:
      - ./weaver-config-local.yaml:/etc/weaver/weaver-config.yaml:ro
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

  fm-replica-1:
    image: weaver-test-replica:1.0.0
    container_name: fm-replica-1
    environment:
      REPLICA_NAME: "fm-1"
      REPLICA_ID: "fm-1"
      HEALTH_PORT: "5050"
      ETCD_ENDPOINT: "http://etcd:2379"
    ports:
      - "5101:5050"
      - "6101:6050"
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

  prometheus:
    image: prom/prometheus:v2.40.0
    container_name: prometheus
    volumes:
      - ./prometheus.yml:/etc/prometheus/prometheus.yml:ro
      - prometheus-data:/prometheus
    ports:
      - "9091:9090"
    command:
      - "--config.file=/etc/prometheus/prometheus.yml"
      - "--storage.tsdb.path=/prometheus"
    networks:
      - weaver-net

  jaeger:
    image: jaegertracing/all-in-one:latest
    container_name: jaeger
    ports:
      - "5775:5775/udp"
      - "6831:6831/udp"
      - "16686:16686"
    networks:
      - weaver-net

volumes:
  etcd-data:
  prometheus-data:

networks:
  weaver-net:
    driver: bridge
```

**File: weaver-config-local.yaml**
```yaml
gateway:
  name: "fm-gateway-local"
  mode: "primary_aware"

discovery:
  type: "t2_etcd"
  poll_interval: 5s               # Faster in dev
  config:
    endpoint: "http://etcd:2379"
    key_pattern: "/dashfabric/cluster/pods/fm-*"

health:
  type: "http"
  interval: 5s
  timeout: 2s
  config:
    endpoint: "/api/v1/health"
  consecutive_failures: 2

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

load_balancers:
  - name: "default"
    type: "least_connections"

reliability:
  circuit_breaker:
    enabled: true
    failure_threshold: 3
    timeout: 10s
  retry:
    enabled: true
    max_attempts: 2
    backoff_strategy: "exponential"
    initial_backoff: 50ms
  queuing:
    enabled: true
    per_replica_depth: 100

observability:
  metrics:
    enabled: true
    namespace: "fm_gw"
  tracing:
    enabled: true
    provider: "jaeger"
    sample_rate: 1.0               # 100% in dev
  logging:
    enabled: true
    level: "DEBUG"
```

**File: prometheus.yml**
```yaml
global:
  scrape_interval: 15s

scrape_configs:
  - job_name: 'weaver'
    static_configs:
      - targets: ['weaver:9090']
```

---

## Standalone Binary Quick Start

### System Diagram

```
┌───────────────────────────────┐
│  Single Host                  │
├───────────────────────────────┤
│                               │
│  ./weaver --config cfg.yaml   │
│  ├─ :5051 (gRPC)              │
│  ├─ :8080 (HTTP)              │
│  └─ :9090 (Metrics)           │
│                               │
│  ← connects to →              │
│                               │
│  etcd-t2:2379 (remote)        │
│  fm-replicas (remote)         │
│                               │
└───────────────────────────────┘
```

### 5-Minute Setup

**Step 1: Build Binary**
```bash
# Clone repo
git clone https://github.com/dashfabric/weaver.git
cd weaver

# Build
make build

# Binary at: ./bin/weaver
```

**Step 2: Create Config**
```bash
cat > weaver-config.yaml <<'EOF'
gateway:
  name: "fm-gateway-standalone"
  mode: "primary_aware"

discovery:
  type: "t2_etcd"
  config:
    endpoint: "http://etcd-t2.example.com:2379"
    key_pattern: "/dashfabric/cluster/pods/fm-*"

health:
  type: "http"
  interval: 10s
  config:
    endpoint: "/api/v1/health"

listeners:
  grpc:
    enabled: true
    port: 5051
  http:
    enabled: true
    port: 8080

load_balancers:
  - name: "default"
    type: "least_connections"

reliability:
  circuit_breaker:
    enabled: true
    failure_threshold: 5
  retry:
    enabled: true
    max_attempts: 3
  queuing:
    enabled: true
    per_replica_depth: 1000

observability:
  metrics:
    enabled: true
    namespace: "fm_gw"
EOF
```

**Step 3: Run**
```bash
# Start Weaver
./bin/weaver --config weaver-config.yaml

# Expected output:
# [INFO] Weaver gateway started
# [INFO] Discovery: T2 etcd endpoint connected
# [INFO] Listeners: gRPC :5051, HTTP :8080, Metrics :9090
# [INFO] Discovered 3 replicas: [fm-1, fm-2, fm-3]
```

**Step 4: Verify**
```bash
# In another terminal:

# Check replicas
curl http://localhost:8080/debug/replicas | jq .

# Check metrics
curl http://localhost:9090/metrics | grep fm_gw_replica_status

# Test gRPC
grpcurl -plaintext localhost:5051 list
```

**Step 5: Integration Test**
```bash
# Send Subscribe request
grpcurl -plaintext -d '{"topic":"config"}' \
  localhost:5051 FM.Broker/Subscribe
```

---

## Visual Decision Tree: Which Deployment?

```
                     ┌─ KUBERNETES ─┐
                     │ • Production  │
                     │ • High        │
                     │   availability│
                     │ • Auto-scaling│
                     │ • Multi-node  │
                     └───────────────┘

Choose your        ╱           │         ╲
deployment   ───┤             │           ├───
model        ╲           │         ╱

     ┌─ DOCKER COMPOSE ─┐       ┌─ STANDALONE ─┐
     │ • Dev/test       │       │ • Small deploy│
     │ • Local testing  │       │ • Single host │
     │ • Integration    │       │ • Bare metal  │
     │   testing        │       │ • Minimal ops │
     │ • Demo           │       └───────────────┘
     └──────────────────┘
```

---

## Monitoring Quick Links

After deployment, access monitoring:

| Component | Kubernetes | Docker Compose | Standalone |
|-----------|-----------|----------------|-----------|
| **Weaver Debug** | `kubectl port-forward svc/weaver 8080:8080` | `http://localhost:8080/debug/replicas` | `http://localhost:8080/debug/replicas` |
| **Metrics (Prometheus)** | `kubectl port-forward svc/prometheus 9090:9090` | `http://localhost:9091` | Configure separately |
| **Traces (Jaeger)** | Configure ServiceMonitor | `http://localhost:16686` | Configure separately |
| **Logs** | `kubectl logs -n weaver <pod> -f` | `docker-compose logs -f weaver` | Stdout / file-based |

---

**End of Quick-Start Visual Guides**

All deployment models covered with copy-paste ready configurations.
