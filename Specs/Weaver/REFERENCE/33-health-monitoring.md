# Weaver: Health Monitoring Reference

> **Read Time:** 20 minutes  
> **Previous:** [32-discovery-methods.md](./32-discovery-methods.md) | **Next:** [34-reliability-patterns.md](./34-reliability-patterns.md)

---

## HTTP Health Checks

```yaml
health:
  type: "http"
  interval: 10s
  timeout: 5s
  config:
    endpoint: "/api/v1/health"
    expected_status: 200
  consecutive_failures: 3
```

**How:** GET request to endpoint, check status
**When:** Standard HTTP/REST services

---

## gRPC Health Checks

```yaml
health:
  type: "grpc"
  interval: 10s
  timeout: 5s
  config:
    service: "health.Health/Check"
  consecutive_failures: 3
```

**How:** Call gRPC Health.Check() service
**When:** gRPC services (FM, CB systems)

---

## TCP Health Checks

```yaml
health:
  type: "tcp"
  interval: 10s
  timeout: 5s
  consecutive_failures: 3
```

**How:** Connect to port, if successful = healthy
**When:** Services without explicit health endpoint

---

## Panic Mode

```yaml
health:
  panic_mode:
    enabled: true
    threshold_percent: 50
```

**What:** If >50% of replicas down, don't circuit-break
**Why:** Prevent total outage; allow degraded service

---

**Navigation:**
- [← Previous](./32-discovery-methods.md)
- [Index](../INDEX.md)
- [Next →](./34-reliability-patterns.md)
