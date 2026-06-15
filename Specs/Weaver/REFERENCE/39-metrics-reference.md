# Weaver: Metrics Reference

> **Read Time:** 20 minutes  
> **Previous:** [38-api-reference.md](./38-api-reference.md) | **Next:** [GUIDES/](../GUIDES/)

---

## Request Metrics

**fm_gw_requests_total{replica="fm-1"}**
- Type: Counter
- Description: Total requests routed to this replica
- Labels: replica, operation

**fm_gw_request_duration_p50{replica="fm-1"}**
**fm_gw_request_duration_p99{replica="fm-1"}**
- Type: Gauge
- Description: Request latency percentiles
- Labels: replica

---

## Replica Status Metrics

**fm_gw_replica_status{replica="fm-1"}**
- Type: Gauge
- Value: 1 = HEALTHY, 0 = UNHEALTHY
- Labels: replica

**fm_gw_replicas_total**
- Type: Gauge
- Description: Total number of replicas discovered

**fm_gw_replicas_healthy**
- Type: Gauge
- Description: Number of healthy replicas

---

## Circuit Breaker Metrics

**fm_gw_circuit_breaker_state{replica="fm-1"}**
- Type: Gauge
- Values: 0=CLOSED, 1=OPEN, 2=HALF_OPEN

**fm_gw_circuit_breaker_transitions_total**
- Type: Counter
- Description: Number of state transitions

---

## Rate Limiting Metrics

**fm_gw_rate_limit_exceeded_total{dimension="global"}**
- Type: Counter
- Description: Requests rejected by rate limiter

---

## Load Balancer Metrics

**fm_gw_replica_load{replica="fm-1"}**
- Type: Gauge
- Description: Active connections per replica
- Used by: Least-connections load balancer

---

**Navigation:**
- [← Previous](./38-api-reference.md)
- [Index](../INDEX.md)
- [Next →](../GUIDES/60-best-practices.md)
