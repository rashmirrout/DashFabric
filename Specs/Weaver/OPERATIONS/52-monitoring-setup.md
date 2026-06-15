# Weaver: Monitoring Setup Guide

> **Read Time:** 25 minutes  
> **Previous:** [51-docker-deployment.md](./51-docker-deployment.md) | **Next:** [53-production-runbook.md](./53-production-runbook.md)

---

## Complete Monitoring Stack

This guide sets up Prometheus, Jaeger, and Grafana to monitor Weaver.

---

## Architecture

```
Weaver (metrics, traces)
  ↓
Prometheus (scrapes metrics every 15s)
  ↓
Grafana (visualizes metrics, queries Prometheus)

Weaver (traces sampled at 10%)
  ↓
Jaeger Agent (port 6831)
  ↓
Jaeger Collector (aggregates traces)
  ↓
Jaeger Query (http://jaeger:16686 UI)
```

---

## Prerequisites

- Docker and Docker Compose (recommended)
- Or: Kubernetes cluster
- 4GB available memory (minimum)

---

## Docker Compose Stack

**File: docker-compose-monitoring.yaml**

```yaml
version: '3.8'

services:
  # Prometheus - Time-series database for metrics
  prometheus:
    image: prom/prometheus:latest
    container_name: prometheus
    networks:
      - monitoring
    ports:
      - "9090:9090"
    volumes:
      - ./config/prometheus.yml:/etc/prometheus/prometheus.yml:ro
      - ./config/prometheus-alerts.yml:/etc/prometheus/prometheus-alerts.yml:ro
      - prometheus-data:/prometheus
    command:
      - '--config.file=/etc/prometheus/prometheus.yml'
      - '--storage.tsdb.path=/prometheus'
      - '--storage.tsdb.retention.time=15d'
    restart: unless-stopped

  # Grafana - Visualization and dashboarding
  grafana:
    image: grafana/grafana:latest
    container_name: grafana
    networks:
      - monitoring
    ports:
      - "3000:3000"
    environment:
      - GF_SECURITY_ADMIN_PASSWORD=admin
      - GF_USERS_ALLOW_SIGN_UP=false
    volumes:
      - ./config/grafana-datasources.yml:/etc/grafana/provisioning/datasources/datasources.yml:ro
      - ./config/grafana-dashboards.yml:/etc/grafana/provisioning/dashboards/dashboards.yml:ro
      - ./dashboards:/etc/grafana/provisioning/dashboards:ro
      - grafana-data:/var/lib/grafana
    depends_on:
      - prometheus
    restart: unless-stopped

  # Jaeger - Distributed tracing
  jaeger:
    image: jaegertracing/all-in-one:latest
    container_name: jaeger
    networks:
      - monitoring
    ports:
      - "16686:16686"  # UI
      - "6831:6831/udp"  # Agent
    environment:
      - COLLECTOR_ZIPKIN_HTTP_PORT=9411
    restart: unless-stopped

  # AlertManager - Routes alerts from Prometheus
  alertmanager:
    image: prom/alertmanager:latest
    container_name: alertmanager
    networks:
      - monitoring
    ports:
      - "9093:9093"
    volumes:
      - ./config/alertmanager.yml:/etc/alertmanager/alertmanager.yml:ro
      - alertmanager-data:/alertmanager
    command:
      - '--config.file=/etc/alertmanager/alertmanager.yml'
      - '--storage.path=/alertmanager'
    restart: unless-stopped

volumes:
  prometheus-data:
  grafana-data:
  alertmanager-data:

networks:
  monitoring:
    driver: bridge
```

---

## Configuration Files

### Prometheus Config

**File: config/prometheus.yml**

```yaml
global:
  scrape_interval: 15s
  evaluation_interval: 15s
  external_labels:
    cluster: 'production'

# Alert rules
rule_files:
  - 'prometheus-alerts.yml'

# AlertManager routing
alerting:
  alertmanagers:
    - static_configs:
        - targets:
            - alertmanager:9093

scrape_configs:
  # Weaver gateway metrics
  - job_name: 'weaver'
    static_configs:
      - targets: ['weaver:9090']
    scrape_interval: 15s
    metrics_path: '/metrics'

  # Prometheus self-monitoring
  - job_name: 'prometheus'
    static_configs:
      - targets: ['prometheus:9090']
```

### Alert Rules

**File: config/prometheus-alerts.yml**

```yaml
groups:
  - name: weaver_alerts
    interval: 30s
    rules:
      # Gateway down
      - alert: WeaverGatewayDown
        expr: up{job="weaver"} == 0
        for: 2m
        labels:
          severity: critical
        annotations:
          summary: "Weaver gateway is down"
          description: "Weaver has been down for > 2 minutes"

      # High error rate
      - alert: WeaverHighErrorRate
        expr: |
          (
            increase(fm_gw_request_errors_total[5m])
            /
            increase(fm_gw_requests_total[5m])
          ) > 0.05
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Weaver error rate is high (>5%)"

      # High latency
      - alert: WeaverHighLatency
        expr: fm_gw_request_duration_p99 > 500
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Weaver p99 latency is high (>500ms)"

      # Circuit breaker open
      - alert: WeaverCircuitBreakerOpen
        expr: fm_gw_circuit_breaker_state == 1
        for: 2m
        labels:
          severity: warning
        annotations:
          summary: "Weaver circuit breaker is OPEN"
          description: "Circuit breaker has been OPEN for > 2 minutes"

      # Rate limited
      - alert: WeaverRateLimited
        expr: increase(fm_gw_rate_limit_exceeded_total[5m]) > 100
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Weaver is rate-limiting requests"

      # No replicas healthy
      - alert: WeaverNoHealthyReplicas
        expr: fm_gw_replicas_healthy == 0
        for: 1m
        labels:
          severity: critical
        annotations:
          summary: "Weaver has no healthy replicas"
```

### Grafana Datasources

**File: config/grafana-datasources.yml**

```yaml
apiVersion: 1

datasources:
  - name: Prometheus
    type: prometheus
    access: proxy
    url: http://prometheus:9090
    isDefault: true
```

### Grafana Dashboards

**File: config/grafana-dashboards.yml**

```yaml
apiVersion: 1

providers:
  - name: 'Weaver Dashboards'
    orgId: 1
    folder: 'Weaver'
    type: file
    disableDeletion: false
    options:
      path: /etc/grafana/provisioning/dashboards
```

### Grafana Dashboard JSON

**File: dashboards/weaver-overview.json**

```json
{
  "dashboard": {
    "title": "Weaver Gateway Overview",
    "tags": ["weaver"],
    "timezone": "browser",
    "panels": [
      {
        "title": "Request Rate",
        "targets": [
          {
            "expr": "increase(fm_gw_requests_total[5m])"
          }
        ],
        "type": "graph"
      },
      {
        "title": "Error Rate",
        "targets": [
          {
            "expr": "increase(fm_gw_request_errors_total[5m]) / increase(fm_gw_requests_total[5m])"
          }
        ],
        "type": "graph"
      },
      {
        "title": "P99 Latency (ms)",
        "targets": [
          {
            "expr": "fm_gw_request_duration_p99"
          }
        ],
        "type": "graph"
      },
      {
        "title": "Circuit Breaker State",
        "targets": [
          {
            "expr": "fm_gw_circuit_breaker_state"
          }
        ],
        "type": "graph"
      },
      {
        "title": "Healthy Replicas",
        "targets": [
          {
            "expr": "fm_gw_replicas_healthy / fm_gw_replicas_total"
          }
        ],
        "type": "gauge"
      },
      {
        "title": "Active Connections",
        "targets": [
          {
            "expr": "sum(fm_gw_replica_load)"
          }
        ],
        "type": "graph"
      }
    ]
  }
}
```

### AlertManager Config

**File: config/alertmanager.yml**

```yaml
global:
  resolve_timeout: 5m

route:
  receiver: default
  group_by: ['alertname', 'cluster']
  group_wait: 10s
  group_interval: 10s
  repeat_interval: 12h
  routes:
    - match:
        severity: critical
      receiver: critical
      continue: true
    - match:
        severity: warning
      receiver: warnings

receivers:
  - name: default
    # Webhook to your notification system
    webhook_configs:
      - url: http://localhost:5001/alert

  - name: critical
    # PagerDuty for critical alerts
    pagerduty_configs:
      - routing_key: secret_key_here

  - name: warnings
    # Slack for warnings
    slack_configs:
      - api_url: https://hooks.slack.com/services/YOUR/WEBHOOK/URL
        channel: '#weaver-alerts'
```

---

## Deployment

### Docker Compose

```bash
# Create directories
mkdir -p config dashboards

# Copy config files (from above)
# Copy prometheus.yml, alerts, datasources, etc.

# Start stack
docker-compose -f docker-compose-monitoring.yaml up -d

# Verify
docker-compose ps
```

### Kubernetes

**File: monitoring-deployment.yaml**

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: prometheus-config
  namespace: monitoring
data:
  prometheus.yml: |
    global:
      scrape_interval: 15s
    scrape_configs:
      - job_name: 'weaver'
        kubernetes_sd_configs:
          - role: pod
        relabel_configs:
          - source_labels: [__meta_kubernetes_pod_label_app]
            action: keep
            regex: weaver

---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: prometheus
  namespace: monitoring
spec:
  replicas: 1
  selector:
    matchLabels:
      app: prometheus
  template:
    metadata:
      labels:
        app: prometheus
    spec:
      containers:
      - name: prometheus
        image: prom/prometheus:latest
        ports:
        - containerPort: 9090
        volumeMounts:
        - name: config
          mountPath: /etc/prometheus
      volumes:
      - name: config
        configMap:
          name: prometheus-config

---
apiVersion: v1
kind: Service
metadata:
  name: prometheus
  namespace: monitoring
spec:
  selector:
    app: prometheus
  ports:
  - port: 9090
  type: LoadBalancer
```

Deploy:
```bash
kubectl create namespace monitoring
kubectl apply -f monitoring-deployment.yaml
```

---

## Accessing UI

### Prometheus
- URL: http://localhost:9090
- Query metrics: `fm_gw_requests_total`

### Grafana
- URL: http://localhost:3000
- Default login: admin / admin
- Create dashboard: `+` → Dashboard

### Jaeger
- URL: http://localhost:16686
- View traces by service: Select "weaver" from dropdown

### AlertManager
- URL: http://localhost:9093

---

## Querying Metrics

### PromQL Examples

**Request rate per second**
```promql
rate(fm_gw_requests_total[5m])
```

**Error rate percentage**
```promql
(
  increase(fm_gw_request_errors_total[5m])
  /
  increase(fm_gw_requests_total[5m])
) * 100
```

**P99 latency**
```promql
fm_gw_request_duration_p99
```

**Healthy replicas**
```promql
count(fm_gw_replica_status == 1)
```

**Circuit breaker flapping**
```promql
increase(fm_gw_circuit_breaker_transitions_total[5m])
```

---

## Retention Policies

**Prometheus**: 15 days (set in prometheus.yml with `retention.time=15d`)
**Grafana**: No limit (data source dependent)
**Jaeger**: 72 hours (configurable)

---

## Backup

**Backup Prometheus data**
```bash
tar -czf prometheus-backup-$(date +%Y%m%d).tar.gz /var/lib/docker/volumes/prometheus-data/_data
```

**Backup Grafana dashboards**
```bash
# Export all dashboards to JSON
docker exec grafana grafana-cli admin export-dashboard
```

---

**Navigation:**
- [← Previous](./51-docker-deployment.md)
- [Index](../INDEX.md)
- [Next →](./53-production-runbook.md)
