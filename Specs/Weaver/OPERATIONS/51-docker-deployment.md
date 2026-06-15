# Weaver: Docker Deployment Guide

> **Read Time:** 20 minutes  
> **Previous:** [50-kubernetes-deployment.md](./50-kubernetes-deployment.md) | **Next:** [52-monitoring-setup.md](./52-monitoring-setup.md)

---

## Production Docker Deployment

This guide covers running Weaver in production using Docker.

---

## Prerequisites

- Docker 20.10+
- Docker Compose 1.29+ (for multi-container setup)
- etcd running and accessible
- docker-compose.yaml for orchestration

---

## Dockerfile

**File: Dockerfile**

```dockerfile
FROM alpine:3.18

# Install runtime dependencies
RUN apk add --no-cache ca-certificates

# Create non-root user
RUN addgroup -S weaver && adduser -S weaver -G weaver

# Copy binary
COPY weaver /usr/local/bin/weaver
RUN chmod +x /usr/local/bin/weaver && chown weaver:weaver /usr/local/bin/weaver

# Create config directory
RUN mkdir -p /etc/weaver && chown weaver:weaver /etc/weaver

# Switch to non-root user
USER weaver

# Expose ports
EXPOSE 5051 8080 9090

# Health check
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8080/debug/replicas || exit 1

# Run Weaver
ENTRYPOINT ["/usr/local/bin/weaver"]
CMD ["-config", "/etc/weaver/config.yaml"]
```

Build:
```bash
docker build -t weaver:v1.0.0 .
docker tag weaver:v1.0.0 myregistry/weaver:v1.0.0
docker push myregistry/weaver:v1.0.0
```

---

## Single Container Deployment

**File: docker-run.sh**

```bash
#!/bin/bash

# Create config
mkdir -p /etc/weaver

cat > /etc/weaver/config.yaml << 'EOF'
gateway:
  name: "weaver-docker"

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
  interval: "5s"
  timeout: "2s"

load_balancers:
  default:
    strategy: "least_connections"

reliability:
  timeout:
    global: "30s"
  circuit_breaker:
    enabled: true
  retry:
    enabled: true
    max_attempts: 3

observability:
  metrics:
    enabled: true
    port: 9090
EOF

# Run container
docker run -d \
  --name weaver \
  --network host \
  -v /etc/weaver:/etc/weaver:ro \
  -p 5051:5051 \
  -p 8080:8080 \
  -p 9090:9090 \
  --restart unless-stopped \
  myregistry/weaver:v1.0.0
```

Run:
```bash
chmod +x docker-run.sh
./docker-run.sh

# Verify
docker ps | grep weaver
curl http://localhost:8080/debug/replicas
```

---

## Docker Compose (Production Stack)

**File: docker-compose.yaml**

```yaml
version: '3.8'

services:
  weaver:
    image: myregistry/weaver:v1.0.0
    container_name: weaver
    networks:
      - weaver
    ports:
      - "5051:5051"
      - "8080:8080"
      - "9090:9090"
    volumes:
      - ./config/weaver-config.yaml:/etc/weaver/config.yaml:ro
      - ./certs/server.crt:/etc/weaver/tls/server.crt:ro
      - ./certs/server.key:/etc/weaver/tls/server.key:ro
    environment:
      - LOG_LEVEL=INFO
    depends_on:
      - etcd
    restart: unless-stopped
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8080/debug/replicas"]
      interval: 10s
      timeout: 3s
      retries: 3
      start_period: 5s

  etcd:
    image: quay.io/coreos/etcd:v3.5.5
    container_name: etcd
    networks:
      - weaver
    environment:
      - ETCD_LISTEN_CLIENT_URLS=http://0.0.0.0:2379
      - ETCD_ADVERTISE_CLIENT_URLS=http://etcd:2379
      - ETCD_LISTEN_PEER_URLS=http://0.0.0.0:2380
      - ETCD_INITIAL_ADVERTISE_PEER_URLS=http://etcd:2380
      - ETCD_INITIAL_CLUSTER=default=http://etcd:2380
      - ETCD_INITIAL_CLUSTER_TOKEN=etcd-cluster
      - ETCD_INITIAL_CLUSTER_STATE=new
    ports:
      - "2379:2379"
    restart: unless-stopped

  prometheus:
    image: prom/prometheus:latest
    container_name: prometheus
    networks:
      - weaver
    ports:
      - "9091:9090"
    volumes:
      - ./config/prometheus.yml:/etc/prometheus/prometheus.yml:ro
      - prometheus-data:/prometheus
    command:
      - '--config.file=/etc/prometheus/prometheus.yml'
      - '--storage.tsdb.path=/prometheus'
    restart: unless-stopped

  grafana:
    image: grafana/grafana:latest
    container_name: grafana
    networks:
      - weaver
    ports:
      - "3000:3000"
    environment:
      - GF_SECURITY_ADMIN_PASSWORD=admin
    volumes:
      - grafana-data:/var/lib/grafana
    depends_on:
      - prometheus
    restart: unless-stopped

volumes:
  prometheus-data:
  grafana-data:

networks:
  weaver:
    driver: bridge
```

**File: config/prometheus.yml**

```yaml
global:
  scrape_interval: 15s
  evaluation_interval: 15s

scrape_configs:
  - job_name: 'weaver'
    static_configs:
      - targets: ['weaver:9090']
```

Run:
```bash
docker-compose up -d

# Verify
docker-compose ps
curl http://localhost:8080/debug/replicas
curl http://localhost:9091/metrics

# Access Grafana
# http://localhost:3000 (admin/admin)
```

---

## Production Considerations

### Networking

**Use custom bridge network**
```bash
docker network create weaver-net

docker run -d \
  --name weaver \
  --network weaver-net \
  -p 5051:5051 \
  -p 8080:8080 \
  -p 9090:9090 \
  myregistry/weaver:v1.0.0
```

**Use host network (if collocated with backend)**
```bash
docker run -d \
  --name weaver \
  --network host \
  myregistry/weaver:v1.0.0
```

### Storage

**Mount config as read-only volume**
```bash
docker run -d \
  --name weaver \
  -v /etc/weaver/config.yaml:/etc/weaver/config.yaml:ro \
  myregistry/weaver:v1.0.0
```

**Mount TLS certs as read-only**
```bash
docker run -d \
  --name weaver \
  -v /etc/weaver/tls:/etc/weaver/tls:ro \
  myregistry/weaver:v1.0.0
```

### Resource Limits

**Set memory and CPU limits**
```bash
docker run -d \
  --name weaver \
  --memory 1g \
  --cpus 2 \
  myregistry/weaver:v1.0.0
```

With docker-compose:
```yaml
services:
  weaver:
    resources:
      limits:
        cpus: '2'
        memory: 1G
      reservations:
        cpus: '0.5'
        memory: 256M
```

### Restart Policy

**Restart automatically on failure**
```bash
docker run -d \
  --restart unless-stopped \
  myregistry/weaver:v1.0.0
```

Options:
- `no`: Do not automatically restart
- `always`: Always restart (even if exited with 0)
- `unless-stopped`: Restart unless explicitly stopped
- `on-failure`: Restart only on non-zero exit (use for one-shot jobs)

### Logging

**Configure Docker logging driver**
```bash
docker run -d \
  --log-driver json-file \
  --log-opt max-size=10m \
  --log-opt max-file=3 \
  myregistry/weaver:v1.0.0
```

View logs:
```bash
docker logs weaver
docker logs -f weaver  # Follow
docker logs --tail 50 weaver  # Last 50 lines
```

---

## Container Registry

**Push to private registry**
```bash
# Tag image
docker tag weaver:v1.0.0 myregistry.azurecr.io/weaver:v1.0.0

# Login to registry
az acr login --name myregistry

# Push
docker push myregistry.azurecr.io/weaver:v1.0.0
```

**Pull from private registry**
```bash
docker run -d \
  --name weaver \
  myregistry.azurecr.io/weaver:v1.0.0
```

---

## Scaling Multiple Instances

**Using Docker Swarm**
```bash
# Initialize swarm
docker swarm init

# Deploy service
docker service create \
  --name weaver \
  --replicas 3 \
  -p 5051:5051 \
  myregistry/weaver:v1.0.0

# Scale up
docker service scale weaver=5

# Remove service
docker service rm weaver
```

**Using multiple containers with load balancer**
```bash
# Start 3 Weaver containers
for i in 1 2 3; do
  docker run -d \
    --name weaver-$i \
    -p 505$i:5051 \
    myregistry/weaver:v1.0.0
done

# Start nginx load balancer in front
docker run -d \
  --name nginx-lb \
  -p 5051:5051 \
  -v /etc/nginx/nginx.conf:/etc/nginx/nginx.conf:ro \
  nginx
```

**nginx.conf**
```nginx
upstream weaver {
  least_conn;
  server weaver-1:5051;
  server weaver-2:5051;
  server weaver-3:5051;
}

server {
  listen 5051;
  
  location / {
    proxy_pass grpc://weaver;
    proxy_connect_timeout 5s;
    proxy_read_timeout 30s;
    proxy_send_timeout 30s;
  }
}
```

---

## Monitoring

**Check container health**
```bash
docker ps --format "table {{.Names}}\t{{.Status}}"
```

**View resource usage**
```bash
docker stats weaver
```

**Check logs for errors**
```bash
docker logs weaver | grep -i error
```

---

## Troubleshooting

| Issue | Solution |
|-------|----------|
| Container exits immediately | Check logs: `docker logs weaver` |
| Connection refused | Check if ports are published: `docker port weaver` |
| Replicas not discovered | Check etcd connectivity: `docker exec weaver curl http://etcd:2379/v3/version` |
| High memory usage | Check if health checks are too frequent; set interval > 5s |
| Network unreachable | Ensure etcd is on same network: `docker network inspect weaver-net` |

---

**Navigation:**
- [← Previous](./50-kubernetes-deployment.md)
- [Index](../INDEX.md)
- [Next →](./52-monitoring-setup.md)
