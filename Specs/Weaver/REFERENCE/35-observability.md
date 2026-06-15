# Weaver: Observability Reference

> **Read Time:** 20 minutes  
> **Previous:** [34-reliability-patterns.md](./34-reliability-patterns.md) | **Next:** [36-security.md](./36-security.md)

---

## Metrics (Prometheus)

```yaml
observability:
  metrics:
    enabled: true
    namespace: "fm_gw"
    port: 9090
```

**Key Metrics:**
- `fm_gw_requests_total{replica="fm-1"}` — Total requests
- `fm_gw_request_duration_p99{replica="fm-1"}` — 99th percentile latency
- `fm_gw_replica_status{replica="fm-1"}` — 1=healthy, 0=unhealthy
- `fm_gw_circuit_breaker_state{replica="fm-1"}` — 0=CLOSED, 1=OPEN

---

## Tracing (OpenTelemetry + Jaeger)

```yaml
observability:
  tracing:
    enabled: true
    provider: "jaeger"
    sample_rate: 0.1
    config:
      endpoint: "http://jaeger:6831"
```

**What's traced:** Each request flow (discovery, LB, connect, send, response)
**Sampling:** 10% of requests (configurable)
**Visualization:** Jaeger UI at http://jaeger:16686

---

## Logging

```yaml
observability:
  logging:
    enabled: true
    level: "INFO"
    format: "json"
    async: true
```

**Log Levels:** DEBUG, INFO, WARN, ERROR
**Format:** Structured JSON for easy parsing
**Async:** True = non-blocking (better performance)

---

**Navigation:**
- [← Previous](./34-reliability-patterns.md)
- [Index](../INDEX.md)
- [Next →](./36-security.md)
