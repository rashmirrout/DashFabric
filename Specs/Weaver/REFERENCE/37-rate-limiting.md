# Weaver: Rate Limiting Reference

> **Read Time:** 15 minutes  
> **Previous:** [36-security.md](./36-security.md) | **Next:** [38-api-reference.md](./38-api-reference.md)

---

## Rate Limiting Dimensions

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

## Algorithm: Token Bucket

**How it works:**
1. Bucket holds N tokens
2. Each request consumes 1 token
3. Tokens refill at rate R tokens/second
4. If bucket empty, request rejected (429)

**Example:**
```
Global: 100k tokens/sec
Tenant A uses 8k tokens → Allowed
Tenant B uses 12k tokens → 10k allowed, 2k rejected
```

---

## When to Use Each

| Dimension | Use When |
|-----------|----------|
| **Global** | Protect gateway capacity |
| **Per-Client** | Fair share per application |
| **Per-IP** | DDoS protection |
| **Per-Tenant** | Multi-tenant isolation |

---

**Navigation:**
- [← Previous](./36-security.md)
- [Index](../INDEX.md)
- [Next →](./38-api-reference.md)
