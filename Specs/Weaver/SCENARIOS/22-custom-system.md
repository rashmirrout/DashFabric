# Weaver: Custom System Scenario

> **Read Time:** 20 minutes  
> **Scenario:** Integrating a new/custom backend system (FR, custom service)  
> **Audience:** Architects, Engineers  
> **Previous:** [21-cb-peer-equivalent.md](./21-cb-peer-equivalent.md) | **Next:** [23-multi-tenant.md](./23-multi-tenant.md)

---

## WHAT: Problem Statement

**Goal:** Support a brand new backend system (not FM or CB) using the same Weaver gateway.

**Challenge:** Every system has different:
- Discovery mechanism (might use Consul instead of etcd)
- Health check protocol (might use custom TCP)
- Load balancing strategy (might need consistent hash)
- Authentication method (might use API keys instead of JWT)

**Solution:** Weaver is 100% configuration-driven. Same binary, different config.

---

## HOW: Configuration Walkthrough

### **Example: FR (Future Router) System**

```yaml
gateway:
  name: "fr-gateway"

discovery:
  type: "consul"  # ← FR uses Consul, not etcd
  config:
    endpoint: "http://consul:8500"
    service_name: "fr"
    tag_filter: "v1"

health:
  type: "tcp"  # ← FR only supports TCP health checks
  interval: 10s
  timeout: 5s

load_balancers:
  - name: "default"
    type: "consistent_hash"  # ← FR needs session affinity
    config:
      hash_key: "client_id"

listeners:
  grpc:
    enabled: true
    port: 5051
  http:
    enabled: true
    port: 8080

reliability:
  circuit_breaker:
    enabled: true
  retry:
    enabled: true
    max_attempts: 2

authentication:
  method: "api_key"  # ← FR uses API keys
  config:
    header_name: "X-API-Key"
    key_file: "/etc/weaver/fr-keys.txt"

observability:
  metrics:
    enabled: true
    namespace: "fr_gw"
```

---

## Step-by-Step Deployment

**Step 1: Discovery - Find FR Replicas**

```bash
# Query Consul for FR service instances
curl http://consul:8500/v1/catalog/service/fr

# Returns:
[
  {
    "ID": "fr-1",
    "Address": "10.2.1.5",
    "Port": 5051
  },
  {
    "ID": "fr-2",
    "Address": "10.2.1.6",
    "Port": 5051
  }
]
```

**Step 2: Health Checks - TCP Only**

```
T=0s:   TCP connect → 10.2.1.5:5051 ✓ OK (HEALTHY)
        TCP connect → 10.2.1.6:5051 ✓ OK (HEALTHY)

T=10s:  TCP connect → 10.2.1.5:5051 ✓ OK (HEALTHY)
        TCP connect → 10.2.1.6:5051 ✓ OK (HEALTHY)
```

**Step 3: Load Balancing - Consistent Hash**

```
Request 1: client_id=A → Hash("A") → fr-1 ← SELECTED
Request 2: client_id=A → Hash("A") → fr-1 ← SAME (session affinity)
Request 3: client_id=B → Hash("B") → fr-2 ← DIFFERENT CLIENT
Request 4: client_id=B → Hash("B") → fr-2 ← SAME (session affinity)
```

**Step 4: Authentication - API Key**

```bash
# Client request with API key
curl -H "X-API-Key: sk_prod_abc123" http://fr-gateway:8080/api/v1/operation

# Weaver validates: Is this key valid? (check fr-keys.txt)
# If valid: proceed
# If invalid: reject with 401 Unauthorized
```

---

## WHY: Architectural Decisions

**Why Consul instead of etcd?**
- FR team standardized on Consul for their infrastructure
- Weaver abstracts discovery; same binary works with both

**Why TCP health checks?**
- FR doesn't expose HTTP health endpoint
- TCP connection is enough to verify replica is responding

**Why Consistent Hash?**
- FR sessions must stick to same replica (session state per replica)
- Least-connections would cause unnecessary session migration

**Why API Keys instead of JWT?**
- FR clients are legacy systems that only support API keys
- Weaver supports both; config selects the right one

---

## Generic Template for New Systems

When adding a new system, ask:

| Question | Example (FR) | Your System |
|----------|------------|------------|
| **Discovery** | Consul | _____ |
| **Health Check** | TCP | _____ |
| **Load Balancing** | Consistent Hash | _____ |
| **Authentication** | API Key | _____ |
| **Protocol** | gRPC | _____ |
| **Consistency** | Session-based | _____ |

Then fill in the Weaver config accordingly.

---

## Next Steps

- **See Multi-Tenant Scenario** → [23-multi-tenant.md](./23-multi-tenant.md)
- **Configure Custom System** → [30-configuration-reference.md](../REFERENCE/30-configuration-reference.md)
- **Write Custom Plugin** → [71-plugin-development.md](../CONTRIBUTE/71-plugin-development.md)

---

**Navigation:**
- [← Previous](./21-cb-peer-equivalent.md)
- [Index](../INDEX.md)
- [Next →](./23-multi-tenant.md)
