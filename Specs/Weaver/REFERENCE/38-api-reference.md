# Weaver: API Reference

> **Read Time:** 20 minutes  
> **Previous:** [37-rate-limiting.md](./37-rate-limiting.md) | **Next:** [39-metrics-reference.md](./39-metrics-reference.md)

---

## Debug API (HTTP)

**GET /debug/replicas**
- Returns: List of all replicas with status
- Example: `curl http://localhost:8080/debug/replicas | jq`

**GET /debug/circuit-breaker**
- Returns: Circuit breaker state for each replica
- States: CLOSED, OPEN, HALF_OPEN

**GET /debug/current-replica**
- Returns: Which replica handled this request
- Useful for debugging routing

**GET /debug/config**
- Returns: Current configuration
- Shows loaded gateway config

---

## Metrics API (Prometheus)

**GET /metrics**
- Port: 9090
- Format: Prometheus text format
- Scraped by: Prometheus every 15s

**Example:**
```bash
curl http://localhost:9090/metrics | grep fm_gw_requests_total
```

---

## gRPC Endpoints

**Service Discovery:**
```bash
grpcurl -plaintext localhost:5051 list
```

**Call Service:**
```bash
grpcurl -plaintext -d '{"topic":"config"}' \
  localhost:5051 FM.Broker/Subscribe
```

---

**Navigation:**
- [← Previous](./37-rate-limiting.md)
- [Index](../INDEX.md)
- [Next →](./39-metrics-reference.md)
