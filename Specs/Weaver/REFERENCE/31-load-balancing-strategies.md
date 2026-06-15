# Weaver: Load Balancing Strategies Reference

> **Read Time:** 30 minutes  
> **Purpose:** All 8 load balancing strategies explained  
> **Audience:** Architects, Engineers  
> **Previous:** [30-configuration-reference.md](./30-configuration-reference.md) | **Next:** [32-discovery-methods.md](./32-discovery-methods.md)

---

## 1. Least-Connections (Default)

**Algorithm:** Route to replica with fewest active connections.

**Configuration:**
```yaml
load_balancers:
  - name: "default"
    type: "least_connections"
```

**When to Use:**
- Most workloads (default choice)
- Requests have similar costs
- Want balanced load

**Example:**
```
3 replicas: fm-1 (2 connections), fm-2 (5), fm-3 (3)
New request → fm-1 (least busy)
```

**Pros:** ✅ Fair distribution ✅ Handles variable request costs
**Cons:** ❌ Doesn't respect client affinity

---

## 2. Round-Robin

**Algorithm:** Rotate through replicas (1 → 2 → 3 → 1 → ...).

**Configuration:**
```yaml
load_balancers:
  - name: "rr"
    type: "round_robin"
```

**When to Use:**
- All requests have identical cost
- Simple distribution needed
- Stateless services

**Example:**
```
Request 1 → fm-1
Request 2 → fm-2
Request 3 → fm-3
Request 4 → fm-1
```

**Pros:** ✅ Simple ✅ Predictable
**Cons:** ❌ Doesn't adapt to varying loads

---

## 3. Random

**Algorithm:** Pick random replica.

**Configuration:**
```yaml
load_balancers:
  - name: "random"
    type: "random"
```

**When to Use:**
- Testing/development
- Rarely in production
- Chaos engineering

**Pros:** ✅ Simple ✅ No coordination needed
**Cons:** ❌ Not deterministic ❌ May be unbalanced

---

## 4. Consistent Hash

**Algorithm:** Hash(client_id) → same replica always.

**Configuration:**
```yaml
load_balancers:
  - name: "hash"
    type: "consistent_hash"
    config:
      hash_key: "client_id"
      virtual_nodes: 160
```

**When to Use:**
- Need session affinity
- Client must stick to same replica
- Session state per replica

**Example:**
```
Hash("client-A") % 3 = 0 → fm-1 (always)
Hash("client-B") % 3 = 2 → fm-3 (always)
```

**Pros:** ✅ Session affinity ✅ Minimal rehashing on failure
**Cons:** ❌ May be unbalanced ❌ Doesn't adapt to load

---

## 5. Weighted

**Algorithm:** Route more traffic to higher-weight replicas.

**Configuration:**
```yaml
load_balancers:
  - name: "weighted"
    type: "weighted"
    config:
      replica_weights:
        "fm-1": 2
        "fm-2": 1
        "fm-3": 1
```

**When to Use:**
- Replicas have different capacities
- Some machines more powerful than others

**Example:**
```
3 replicas: fm-1 (weight=2), fm-2 (weight=1), fm-3 (weight=1)
Distribution: 50% → fm-1, 25% → fm-2, 25% → fm-3
```

**Pros:** ✅ Handles heterogeneous clusters
**Cons:** ❌ Requires weight configuration

---

## 6. Sticky

**Algorithm:** Remember client→replica mapping for TTL.

**Configuration:**
```yaml
load_balancers:
  - name: "sticky"
    type: "sticky"
    config:
      ttl: "5m"
```

**When to Use:**
- Need affinity but want to rebalance periodically
- Hybrid of round-robin and consistent-hash

**Example:**
```
T=0m:    Hash("client-A") → fm-1, remember for 5m
T=1m:    Same client → fm-1 (cache hit)
T=6m:    Mapping expired, rehash → maybe fm-2
```

**Pros:** ✅ Affinity ✅ Periodic rebalancing
**Cons:** ❌ TTL expiration can cause spikes

---

## 7. Resource-Aware

**Algorithm:** Route based on replica resource metrics (CPU, memory).

**Configuration:**
```yaml
load_balancers:
  - name: "resource_aware"
    type: "resource_aware"
    config:
      metric: "cpu"              # or "memory"
      threshold: 80              # Don't route if >80%
```

**When to Use:**
- Replicas report resource usage
- Want to avoid overloading specific machines
- Bursty workloads

**Example:**
```
3 replicas: fm-1 (CPU 30%), fm-2 (CPU 90%), fm-3 (CPU 20%)
New request → fm-3 (lowest CPU)
```

**Pros:** ✅ Avoids hotspots ✅ Adaptive
**Cons:** ❌ Requires metric collection ❌ More complex

---

## 8. Custom

**Algorithm:** User-defined via plugin.

**Configuration:**
```yaml
load_balancers:
  - name: "custom"
    type: "custom"
    config:
      plugin_path: "/usr/local/weaver/plugins/my_lb.so"
```

**When to Use:**
- Special requirements not met by built-ins
- Domain-specific logic

**Example:** Route by geography, time-of-day, specific rules

**Pros:** ✅ Infinite flexibility
**Cons:** ❌ Requires plugin development

---

## Comparison Table

| Strategy | Affinity | Balanced | Adaptive | When |
|----------|----------|----------|----------|------|
| Least-Connections | ❌ | ✅ | ✅ | Default |
| Round-Robin | ❌ | ✅ | ❌ | Stateless |
| Random | ❌ | ~ | ❌ | Testing |
| Consistent Hash | ✅ | ~ | ❌ | Sessions |
| Weighted | ~ | ✅ | ❌ | Heterogeneous |
| Sticky | ✅ | ✅ | ✅ | Hybrid |
| Resource-Aware | ~ | ✅ | ✅ | Bursty |
| Custom | Varies | Varies | Varies | Special |

---

## Migration: Switching Strategies

```bash
# Current (Least-Connections)
load_balancers:
  - name: "default"
    type: "least_connections"

# Update to (Consistent Hash)
load_balancers:
  - name: "default"
    type: "consistent_hash"

# Zero-restart: Edit configmap → Weaver reloads within 1s
kubectl edit configmap weaver-config

# Result: New requests use consistent hash; existing connections unaffected
```

---

## Next Steps

- **Discovery Methods** → [32-discovery-methods.md](./32-discovery-methods.md)
- **Health Monitoring** → [33-health-monitoring.md](./33-health-monitoring.md)

---

**Navigation:**
- [← Previous](./30-configuration-reference.md)
- [Index](../INDEX.md)
- [Next →](./32-discovery-methods.md)
