# Weaver: Docker Compose Quick Start (5 Minutes)

> **Read Time:** 5 minutes  
> **Deployment Time:** 5 minutes  
> **Audience:** Engineers, Development  
> **Previous:** [10-kubernetes.md](./10-kubernetes.md) | **Next:** [12-standalone-binary.md](./12-standalone-binary.md)

---

## System Diagram

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
│  │:9091         │  │:16686        │                │
│  └──────────────┘  └──────────────┘                │
│                                                     │
└─────────────────────────────────────────────────────┘
```

---

## 5-Minute Setup

### **Step 1: Prepare Files** (1 min)

```bash
# Create directory
mkdir weaver-dev && cd weaver-dev

# Create these files (see sections below):
# - docker-compose.yaml
# - weaver-config-local.yaml
# - prometheus.yml
```

---

### **Step 2: Create docker-compose.yaml** (1 min)

```bash
cat > docker-compose.yaml <<'EOF'
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
EOF
```

---

### **Step 3: Create weaver-config-local.yaml** (1 min)

```bash
cat > weaver-config-local.yaml <<'EOF'
gateway:
  name: "fm-gateway-local"
  mode: "primary_aware"

discovery:
  type: "t2_etcd"
  poll_interval: 5s
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
    sample_rate: 1.0
  logging:
    enabled: true
    level: "DEBUG"
EOF
```

---

### **Step 4: Create prometheus.yml** (1 min)

```bash
cat > prometheus.yml <<'EOF'
global:
  scrape_interval: 15s

scrape_configs:
  - job_name: 'weaver'
    static_configs:
      - targets: ['weaver:9090']
EOF
```

---

### **Step 5: Start Stack** (1 min)

```bash
# Start all services
docker-compose up -d

# Verify all containers running
docker-compose ps
```

**Expected Output:**
```
NAME              STATUS      PORTS
weaver            Up 30s      5051/tcp, 8080/tcp, 9090/tcp
etcd              Up 35s      2379/tcp
fm-replica-1      Up 30s      5050/tcp
fm-replica-2      Up 30s      5050/tcp
fm-replica-3      Up 30s      5050/tcp
prometheus        Up 25s      9090/tcp (as 9091)
jaeger            Up 20s      6831/udp, 16686/tcp
```

---

## Verify Connectivity

```bash
# Check Weaver is responding
curl http://localhost:8080/debug/replicas | jq .

# Expected: List of replicas with status
```

**Expected Output:**
```json
{
  "replicas": [
    {
      "name": "fm-1",
      "address": "10.0.1.5",
      "port": 5051,
      "status": "HEALTHY"
    },
    {
      "name": "fm-2",
      "address": "10.0.1.6",
      "port": 5051,
      "status": "HEALTHY"
    },
    {
      "name": "fm-3",
      "address": "10.0.1.7",
      "port": 5051,
      "status": "HEALTHY"
    }
  ]
}
```

---

## Access Monitoring UIs

```bash
# Weaver debug API
curl http://localhost:8080/debug/replicas | jq .

# Metrics (Prometheus)
curl http://localhost:9091/metrics | grep fm_gw

# Jaeger UI (open browser)
open http://localhost:16686
```

---

## Common Tasks

### **View Logs**

```bash
# All logs
docker-compose logs -f

# Weaver only
docker-compose logs -f weaver

# Specific service
docker-compose logs -f etcd
```

### **Test with gRPC**

```bash
# Install grpcurl if not present
brew install grpcurl  # macOS
# or apt install grpcurl  # Linux

# Test Subscribe (read)
grpcurl -plaintext -d '{"topic":"config"}' \
  localhost:5051 FM.Broker/Subscribe

# Should receive updates from mock FM replicas
```

### **Stop Stack**

```bash
# Stop all containers
docker-compose down

# Remove volumes
docker-compose down -v
```

---

## Troubleshooting

| Issue | Fix |
|-------|-----|
| **Port 5051 already in use** | `docker-compose down && docker-compose up -d` |
| **etcd connection refused** | Wait for etcd to start: `docker-compose logs etcd` |
| **No replicas discovered** | Check etcd: `docker exec etcd etcdctl get /dashfabric/cluster/pods/fm-1` |
| **Weaver container exits** | Check logs: `docker-compose logs weaver` |

---

## Next Steps

- **Verify Deployment** → [13-verify-deployment.md](./13-verify-deployment.md)
- **See Real Scenarios** → [20-fm-primary-aware.md](../SCENARIOS/20-fm-primary-aware.md)
- **Production Deployment** → [50-kubernetes-deployment.md](../OPERATIONS/50-kubernetes-deployment.md)

---

**Navigation:**
- [← Previous](./10-kubernetes.md)
- [Index](../INDEX.md)
- [Next →](./12-standalone-binary.md)
