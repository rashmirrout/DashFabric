# Weaver Gateway Production Deployment Guide

## Overview

This guide provides instructions for deploying the Weaver Gateway in production environments, including Kubernetes and Docker deployments, monitoring, troubleshooting, and operations.

## Prerequisites

- Kubernetes 1.20+ or Docker 20.10+
- kubectl CLI configured with cluster access
- Docker CLI (for building images)
- helm (optional, for advanced deployments)
- Prometheus and Jaeger (optional, for monitoring)

## Kubernetes Deployment

### 1. Quick Start

Deploy Weaver Gateway to a Kubernetes cluster:

```bash
kubectl apply -f deploy/kubernetes/weaver-deployment.yaml
```

Verify deployment:

```bash
kubectl get pods -n weaver
kubectl get svc -n weaver
kubectl get hpa -n weaver
```

### 2. Configuration

Edit the ConfigMap to customize behavior:

```bash
kubectl edit configmap weaver-config -n weaver
```

Key configuration sections:
- **load_balancer**: Strategy (round_robin, least_connections, weighted, random)
- **discovery**: Service discovery method (kubernetes, etcd, consul, dns_srv)
- **health**: Health check configuration
- **rate_limiting**: Per-client and per-IP limits
- **circuit_breaker**: Failure thresholds and timeouts

### 3. Scaling

Horizontal scaling is automatic via HPA. To adjust:

```bash
kubectl edit hpa weaver-gateway-hpa -n weaver
```

Manual scaling:

```bash
kubectl scale statefulset weaver-gateway --replicas=5 -n weaver
```

### 4. Rolling Updates

Update the image in the StatefulSet:

```bash
kubectl set image statefulset/weaver-gateway \
  weaver-gateway=weaver-gateway:v1.1.0 -n weaver
```

Monitor rollout:

```bash
kubectl rollout status statefulset/weaver-gateway -n weaver
```

### 5. Accessing Logs

View logs from all replicas:

```bash
kubectl logs -n weaver -l app=weaver-gateway --tail=100 -f
```

View specific replica:

```bash
kubectl logs weaver-gateway-0 -n weaver
```

## Docker Deployment

### 1. Building the Image

```bash
cd deploy/docker
docker build -t weaver-gateway:latest .
docker tag weaver-gateway:latest weaver-gateway:v1.0.0
docker push your-registry/weaver-gateway:v1.0.0
```

### 2. Local Development

Start development environment:

```bash
docker-compose -f deploy/docker/docker-compose.yaml up
```

Access services:
- Weaver Gateway: localhost:5000
- Prometheus: localhost:9091
- Jaeger: localhost:16686

### 3. Production Docker Deployment

Deploy with production settings:

```bash
docker run -d \
  --name weaver-gateway \
  -p 5000:5000 \
  -p 9090:9090 \
  -v /etc/weaver/config.yaml:/etc/weaver/config.yaml:ro \
  -e WEAVER_LOG_LEVEL=info \
  --restart=always \
  weaver-gateway:latest
```

## Monitoring & Observability

### 1. Prometheus Metrics

Metrics are exposed on port 9090 by default:

```bash
curl http://localhost:9090/metrics
```

Key metrics:
- `requests_total`: Total requests by status, replica
- `requests_duration_seconds`: Request latency (p50, p95, p99)
- `errors_total`: Errors by type and replica
- `replicas_healthy`: Number of healthy replicas
- `circuit_breaker_state`: Circuit breaker state per replica
- `rate_limit_exceeded_total`: Rate limit violations

### 2. Jaeger Tracing

Enable tracing in config:

```yaml
observability:
  tracing:
    enabled: true
    jaeger_endpoint: "http://jaeger:6831"
```

Access Jaeger UI: http://localhost:16686

### 3. Logging

Logs are structured JSON by default. Example log event:

```json
{
  "timestamp": "2026-06-15T18:58:22Z",
  "event_type": "request_completed",
  "trace_id": "trace-123",
  "request_id": "req-456",
  "level": "info",
  "message": "Request completed: status=200, latency=45ms",
  "context": {
    "status": 200,
    "replica": "replica-1"
  }
}
```

## Operations

### 1. Health Checks

Check replica health:

```bash
kubectl exec -it weaver-gateway-0 -n weaver -- \
  curl -s http://localhost:9090/health
```

### 2. Circuit Breaker Management

Monitor circuit breaker state:

```bash
kubectl exec -it weaver-gateway-0 -n weaver -- \
  curl -s http://localhost:9090/metrics | grep circuit_breaker
```

### 3. Rate Limit Monitoring

Check rate limit violations:

```bash
kubectl exec -it weaver-gateway-0 -n weaver -- \
  curl -s http://localhost:9090/metrics | grep rate_limit_exceeded
```

## Troubleshooting

### 1. Pod Fails to Start

Check pod status and events:

```bash
kubectl describe pod weaver-gateway-0 -n weaver
kubectl logs weaver-gateway-0 -n weaver --previous
```

### 2. High Error Rate

1. Check replica health:

```bash
kubectl logs weaver-gateway-0 -n weaver | grep "status_changed"
```

2. Verify backend availability:

```bash
kubectl get endpoints -n weaver
```

3. Check circuit breaker state:

```bash
kubectl exec -it weaver-gateway-0 -n weaver -- \
  curl -s http://localhost:9090/metrics | grep circuit_breaker
```

### 3. Memory Leaks

Monitor memory usage:

```bash
kubectl top pod -n weaver
kubectl top pod weaver-gateway-0 -n weaver --containers
```

Check for goroutine leaks in logs.

### 4. Connection Issues

Check active connections:

```bash
kubectl exec -it weaver-gateway-0 -n weaver -- \
  netstat -an | grep ESTABLISHED | wc -l
```

### 5. Configuration Issues

Validate configuration:

```bash
kubectl get configmap weaver-config -n weaver -o yaml
kubectl describe configmap weaver-config -n weaver
```

## Performance Tuning

### 1. Connection Pool

Adjust max connections in config:

```yaml
gateway:
  max_connections: 2000
```

### 2. Timeouts

Tune timeout values:

```yaml
retry:
  max_attempts: 5
  initial_backoff: 50ms
  max_backoff: 5s
```

### 3. Rate Limiting

Increase rate limits for high-traffic scenarios:

```yaml
rate_limiting:
  per_client: 5000
  per_ip: 10000
```

### 4. Resource Limits

Adjust resource requests/limits for capacity:

```yaml
resources:
  requests:
    cpu: 200m
    memory: 512Mi
  limits:
    cpu: 1000m
    memory: 1Gi
```

## Disaster Recovery

### 1. Backup Configuration

Backup ConfigMap:

```bash
kubectl get configmap weaver-config -n weaver -o yaml > backup-config.yaml
```

### 2. Restore Configuration

Restore from backup:

```bash
kubectl apply -f backup-config.yaml
```

### 3. Rollback

Rollback to previous version:

```bash
kubectl rollout undo statefulset/weaver-gateway -n weaver
```

## Security

### 1. Enable mTLS

Configure client certificate authentication:

```yaml
auth:
  type: "mtls"
  client_ca: "/etc/weaver/client-ca.crt"
```

### 2. Enable RBAC

Define role-based access:

```yaml
rbac:
  roles:
    admin:
      - read
      - write
      - delete
    user:
      - read
```

### 3. Rate Limiting for Security

Prevent DDoS attacks:

```yaml
rate_limiting:
  per_ip: 1000
  global: 100000
```

## Maintenance

### 1. Regular Updates

Check for updates:

```bash
docker pull weaver-gateway:latest
```

### 2. Log Rotation

Configure log rotation for container logs.

### 3. Metrics Retention

Adjust Prometheus retention:

```bash
--storage.tsdb.retention.time=30d
```

## Support

For issues or questions:
1. Check logs: `kubectl logs weaver-gateway-0 -n weaver`
2. Check metrics: `http://localhost:9090/metrics`
3. Check Jaeger traces: `http://localhost:16686`
4. Report issues with reproduction steps and logs
