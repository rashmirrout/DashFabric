# Weaver: Complete Configuration Reference

> **Read Time:** 45 minutes  
> **Purpose:** Comprehensive YAML configuration schema  
> **Audience:** Engineers, Operators  
> **Previous:** [25-chaos-engineering.md](../SCENARIOS/25-chaos-engineering.md) | **Next:** [31-load-balancing-strategies.md](./31-load-balancing-strategies.md)

---

## Gateway Section

```yaml
gateway:
  name: "fm-gateway"                 # Unique name for this gateway instance
  mode: "primary_aware"              # or "peer_equivalent"
  version: "1.0.0"                   # Weaver version (informational)
```

---

## Discovery Section

### etcd Discovery
```yaml
discovery:
  type: "t2_etcd"
  poll_interval: 10s
  config:
    endpoint: "http://etcd-t2:2379"
    key_pattern: "/dashfabric/cluster/pods/fm-*"
    timeout: 5s
```

### Consul Discovery
```yaml
discovery:
  type: "consul"
  poll_interval: 10s
  config:
    endpoint: "http://consul:8500"
    service_name: "fm"
    tag_filter: "v1"
```

### Kubernetes Discovery
```yaml
discovery:
  type: "kubernetes"
  poll_interval: 10s
  config:
    namespace: "default"
    service: "fm"
    port_name: "grpc"
```

---

## Health Monitoring Section

```yaml
health:
  enabled: true
  type: "http"                       # or "grpc", "tcp"
  interval: 10s
  timeout: 5s
  
  config:
    endpoint: "/api/v1/health"       # For HTTP
    expected_status: 200             # Expected HTTP status
    # OR for gRPC:
    service: "health.Health/Check"
    # OR for TCP: (no config needed)
  
  consecutive_failures: 3            # Mark UNHEALTHY after 3 failures
  
  panic_mode:
    enabled: true
    threshold_percent: 50            # Don't circuit-break if >50% down
```

---

## Listeners Section

```yaml
listeners:
  grpc:
    enabled: true
    port: 5051
    max_connections: 10000
    read_timeout: 30s
    write_timeout: 30s
    
  http:
    enabled: true
    port: 8080
    max_connections: 10000
    read_timeout: 30s
    write_timeout: 30s
    
  metrics:
    enabled: true
    port: 9090
    path: "/metrics"
```

---

## Load Balancing Section

```yaml
load_balancers:
  - name: "default"
    type: "least_connections"        # Default
    
# OR other types:
  - name: "round_robin"
    type: "round_robin"
    
  - name: "hash_based"
    type: "consistent_hash"
    config:
      hash_key: "client_id"
      virtual_nodes: 160
      
  - name: "weighted"
    type: "weighted"
    config:
      replica_weights:
        "fm-1": 2
        "fm-2": 1
```

---

## Reliability Section

```yaml
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
```

---

## Rate Limiting Section

```yaml
rate_limiting:
  enabled: true
  
  global:
    enabled: true
    requests_per_second: 100000
    
  per_client:
    enabled: true
    requests_per_second: 10000
    
  per_ip:
    enabled: true
    requests_per_second: 5000
    
  per_tenant:
    enabled: true
    requests_per_second: 50000
```

---

## Observability Section

```yaml
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
    level: "INFO"              # DEBUG, INFO, WARN, ERROR
    format: "json"
    async: true
```

---

## Authentication Section

```yaml
authentication:
  enabled: true
  method: "bearer_token"        # or "api_key", "jwt", "mtls"
  
  # For bearer token:
  config:
    header_name: "Authorization"
    scheme: "Bearer"
    
  # For JWT:
  config:
    issuer: "https://auth.example.com"
    public_key_url: "https://auth.example.com/.well-known/jwks.json"
    cache_ttl: 1h
    
  # For API Key:
  config:
    header_name: "X-API-Key"
    key_file: "/etc/weaver/keys.txt"
    
  # For mTLS:
  config:
    ca_cert_path: "/etc/weaver/ca.crt"
```

---

## Authorization Section

```yaml
authorization:
  enabled: true
  rbac:
    roles:
      admin:
        permissions: ["read", "write", "register", "admin"]
      operator:
        permissions: ["read", "write"]
      viewer:
        permissions: ["read"]
```

---

## TLS Section

```yaml
tls:
  enabled: true
  
  server:
    enabled: true
    cert_path: "/etc/weaver/certs/server.crt"
    key_path: "/etc/weaver/certs/server.key"
    min_version: "1.2"
    
  client:
    enabled: true
    ca_path: "/etc/weaver/certs/ca.crt"
    skip_verify: false
```

---

## Default Configuration Template

```yaml
gateway:
  name: "my-gateway"
  mode: "peer_equivalent"

discovery:
  type: "t2_etcd"
  poll_interval: 10s
  config:
    endpoint: "http://etcd:2379"
    key_pattern: "/my-service/*"

health:
  type: "http"
  interval: 10s
  config:
    endpoint: "/health"
  consecutive_failures: 3

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
  retry:
    enabled: true
    max_attempts: 3
  queuing:
    enabled: true

observability:
  metrics:
    enabled: true
    namespace: "my_gw"
```

---

## Configuration Best Practices

✅ **DO:**
- Set health check interval < 10s (quick failure detection)
- Set circuit breaker timeout >= 30s (allow time for recovery)
- Enable rate limiting in production
- Use TLS for client ↔ gateway communication
- Use mTLS for gateway ↔ replica communication

❌ **DON'T:**
- Set health check timeout > connection timeout (nonsensical)
- Disable circuit breaker in production (dangerous)
- Set rate limit to 0 (blocks all traffic)
- Use unencrypted communication

---

## Next Steps

- **Load Balancing Strategies** → [31-load-balancing-strategies.md](./31-load-balancing-strategies.md)
- **Discovery Methods** → [32-discovery-methods.md](./32-discovery-methods.md)
- **Health Monitoring** → [33-health-monitoring.md](./33-health-monitoring.md)

---

**Navigation:**
- [← Previous](../SCENARIOS/25-chaos-engineering.md)
- [Index](../INDEX.md)
- [Next →](./31-load-balancing-strategies.md)
