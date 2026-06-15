# Weaver: Pod Discovery Methods Reference

> **Read Time:** 25 minutes  
> **Purpose:** All discovery methods explained  
> **Audience:** Engineers, Operators  
> **Previous:** [31-load-balancing-strategies.md](./31-load-balancing-strategies.md) | **Next:** [33-health-monitoring.md](./33-health-monitoring.md)

---

## T2 etcd Discovery

**Configuration:**
```yaml
discovery:
  type: "t2_etcd"
  poll_interval: 10s
  config:
    endpoint: "http://etcd-t2:2379"
    key_pattern: "/dashfabric/cluster/pods/fm-*"
    timeout: 5s
```

**How it works:**
- Weaver connects to etcd
- Polls for keys matching pattern every 10s
- Returns list of replicas

**When to use:** DashFabric environments (FM, CB systems)

---

## Consul Discovery

**Configuration:**
```yaml
discovery:
  type: "consul"
  poll_interval: 10s
  config:
    endpoint: "http://consul:8500"
    service_name: "my-service"
    tag_filter: "v1"
```

**How it works:**
- Weaver queries Consul catalog
- Filters by service name and tags
- Returns healthy instances

**When to use:** Consul-based infrastructure

---

## Kubernetes Discovery

**Configuration:**
```yaml
discovery:
  type: "kubernetes"
  poll_interval: 10s
  config:
    namespace: "default"
    service: "my-service"
    port_name: "grpc"
```

**How it works:**
- Weaver watches Kubernetes Endpoints
- Detects pods added/removed
- Updates replica list in real-time

**When to use:** Kubernetes deployments (recommended for cloud)

---

## DNS Discovery

**Configuration:**
```yaml
discovery:
  type: "dns"
  poll_interval: 10s
  config:
    hostname: "my-service.default.svc.cluster.local"
    port: 5051
```

**How it works:**
- Weaver resolves DNS hostname
- Returns list of IP addresses
- Updates on DNS changes

**When to use:** Simple deployments, cloud-native environments

---

## Static List Discovery

**Configuration:**
```yaml
discovery:
  type: "static"
  config:
    replicas:
      - name: "replica-1"
        address: "10.0.1.5"
        port: 5051
      - name: "replica-2"
        address: "10.0.1.6"
        port: 5051
```

**How it works:**
- Weaver uses static list from config
- No polling or service discovery
- Manual replica updates

**When to use:** Small deployments, testing, explicit control

---

## Comparison

| Method | Dynamic | When to Use |
|--------|---------|------------|
| **etcd** | ✅ Yes | DashFabric environments |
| **Consul** | ✅ Yes | Consul infrastructure |
| **Kubernetes** | ✅ Yes | Cloud deployments (recommended) |
| **DNS** | ✅ Yes | Simple cloud-native setups |
| **Static** | ❌ No | Testing, small clusters, explicit control |

---

**Navigation:**
- [← Previous](./31-load-balancing-strategies.md)
- [Index](../INDEX.md)
- [Next →](./33-health-monitoring.md)
