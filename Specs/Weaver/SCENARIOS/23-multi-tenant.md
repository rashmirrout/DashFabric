# Weaver: Multi-Tenant Scenario

> **Read Time:** 20 minutes  
> **Scenario:** SaaS platform with multiple tenants sharing Weaver  
> **Audience:** Architects, Engineers  
> **Previous:** [22-custom-system.md](./22-custom-system.md) | **Next:** [24-failover-dr.md](./24-failover-dr.md)

---

## WHAT: Problem Statement

**Goal:** Share one Weaver gateway across multiple customers (tenants).

**Requirements:**
- Tenant A can only access Tenant A's replicas
- Rate limiting per tenant (Tenant A quota: 10k req/s)
- Authentication: Which tenant is this request from?
- Authorization: Does this tenant have permission?

---

## HOW: Multi-Tenant Configuration

```yaml
gateway:
  name: "saas-gateway"

discovery:
  type: "kubernetes"
  config:
    namespace: "production"

authentication:
  method: "jwt"
  config:
    issuer: "https://auth.saas.com"
    public_key_url: "https://auth.saas.com/.well-known/jwks.json"

authorization:
  enabled: true
  rbac:
    roles:
      admin:
        permissions: ["read", "write", "admin"]
      user:
        permissions: ["read", "write"]
      viewer:
        permissions: ["read"]

rate_limiting:
  enabled: true
  
  # Per-tenant rate limiting
  per_tenant:
    enabled: true
    requests_per_second: 10000
    
  # Per-user rate limiting within tenant
  per_user:
    enabled: true
    requests_per_second: 1000

routing:
  # Route based on tenant claim in JWT
  tenant_selection:
    enabled: true
    jwt_claim: "tenant_id"  # JWT contains {"tenant_id": "acme"}
    replica_namespace: "{tenant_id}"  # Route to "acme" replicas
```

---

## Authentication Flow

```
CLIENT REQUEST:
  Authorization: Bearer eyJhbGc...  (JWT token)
  
WEAVER VALIDATES:
  1. Is token valid? (check signature with public key)
  2. Extract claims: {"tenant_id": "acme", "user_id": "user@acme.com", "role": "admin"}
  3. Check quota: Has tenant "acme" exceeded 10k req/s? No → proceed
  4. Check user quota: Has user@acme.com exceeded 1k req/s? No → proceed
  5. Check authorization: Does "admin" role have "write" permission? Yes → proceed
  
ROUTE REQUEST:
  Discovery: Find replicas in namespace "acme" (not other tenants)
  Route to: acme-replica-1, acme-replica-2 only
  
TENANT ISOLATION:
  Tenant "acme" never sees:
    - Other tenants' replicas
    - Other tenants' requests
    - Other tenants' metrics
```

---

## Rate Limiting Example

```
Time T: 1 second window

Tenant A: 8,000 requests → Allowed (< 10k limit)
Tenant B: 12,000 requests → 10k allowed, 2k rejected with 429

User alice@acme: 800 requests → Allowed (< 1k limit)
User bob@acme: 1,200 requests → 1k allowed, 200 rejected with 429
```

---

## Kubernetes Namespace Isolation

```
Kubernetes Cluster:
├── Namespace: acme (Tenant A replicas)
│   ├── Pod: acme-replica-1
│   ├── Pod: acme-replica-2
│   └─ NetworkPolicy: ingress only from weaver
│
├── Namespace: widgetco (Tenant B replicas)
│   ├── Pod: widgetco-replica-1
│   ├── Pod: widgetco-replica-2
│   └─ NetworkPolicy: ingress only from weaver
│
└── Namespace: weaver (Gateway, shared)
    ├── Deployment: weaver (3 replicas)
    └─ ServiceAccount: weaver-gateway
       ├── Permission: read pods in "acme"
       ├── Permission: read pods in "widgetco"
       └─ (No permission: read secrets, delete pods)
```

---

## Multi-Tenant Metrics

```
PROMETHEUS METRICS:
weaver_requests_total{tenant="acme", user="alice@acme.com"} = 1000
weaver_requests_total{tenant="acme", user="bob@acme.com"} = 500
weaver_requests_total{tenant="widgetco", user="carol@widgetco.com"} = 2000

weaver_rate_limit_exceeded{tenant="acme"} = 50 (50 requests rejected this minute)
weaver_rate_limit_exceeded{tenant="widgetco"} = 0

Visualization: Grafana shows per-tenant dashboard
  - Tenant A's quota usage: 70% (7k of 10k)
  - Tenant B's quota usage: 85% (8.5k of 10k)
```

---

## Security Considerations

**Tenant Isolation:**
✅ Network: Replicas in separate namespaces
✅ Authentication: JWT with tenant_id claim
✅ Authorization: RBAC per tenant
✅ Rate limiting: Per-tenant quotas
✅ Metrics: Tagged by tenant (no cross-tenant visibility)

**Threats Mitigated:**
- ❌ Tenant A sees Tenant B's data (prevented by namespace isolation + auth)
- ❌ Tenant A exceeds quota and affects Tenant B (prevented by per-tenant rate limiting)
- ❌ Tenant A discovers Tenant B's replicas (prevented by routing rules)

---

## Challenges & Solutions

| Challenge | Solution |
|-----------|----------|
| **One gateway, many tenants** | Routing rules extract tenant from JWT |
| **Rate limit per tenant** | Multi-dimensional rate limiting (tenant + user levels) |
| **Metrics visibility** | Tag metrics by tenant; isolate dashboards |
| **Replica sharing** | Namespace isolation; RBAC prevents cross-tenant access |
| **Key rotation** | JWT issuer rotates keys; Weaver fetches new keys automatically |

---

## Cost Efficiency

**Without Multi-Tenant Weaver:**
```
Tenant A needs gateway → Deploy separate Weaver
Tenant B needs gateway → Deploy separate Weaver
Tenant C needs gateway → Deploy separate Weaver

Total: 3 Weaver deployments × 3 replicas = 9 pods
Cost: 9 × $50/month = $450/month for gateways alone
```

**With Multi-Tenant Weaver:**
```
All tenants share one Weaver deployment: 3 replicas
Each tenant's backend replicas (separate): A=3, B=3, C=3

Total: 1 Weaver deployment (3 pods) + 9 backend replicas
Cost: 3 × $50 + 9 × $50 = $600/month (but shared gateway overhead)

Savings: More efficient as tenant count grows
```

---

## Next Steps

- **See Failover Scenario** → [24-failover-dr.md](./24-failover-dr.md)
- **Rate Limiting Reference** → [37-rate-limiting.md](../REFERENCE/37-rate-limiting.md)
- **Security Reference** → [36-security.md](../REFERENCE/36-security.md)

---

**Navigation:**
- [← Previous](./22-custom-system.md)
- [Index](../INDEX.md)
- [Next →](./24-failover-dr.md)
