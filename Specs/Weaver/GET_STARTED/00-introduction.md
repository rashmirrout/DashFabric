# Weaver: Introduction

> **Read Time:** 5 minutes  
> **Audience:** Everyone  
> **Next Step:** [01-concepts.md](./01-concepts.md)

---

## What is Weaver?

**Weaver** is a universal gateway platform that routes requests to distributed backend systems with intelligence, reliability, and extensibility.

Think of it as **"one gateway to route them all"** — a single binary that intelligently routes traffic to multiple backend replicas, regardless of whether they're in FM (Fabric Management), CB (ControllerBridge), or any future system.

```
┌──────────────────────────────────┐
│      WEAVER GATEWAY              │
│  (Single Binary, Config-Driven)  │
├──────────────────────────────────┤
│ • Pod Discovery (find replicas)  │
│ • Health Monitoring (are they ok?) 
│ • Load Balancing (pick which one) 
│ • Reliability (fail-over, retry)  
│ • Observability (metrics, logs)   │
└──────────────┬───────────────────┘
               │
    ┌──────────┼──────────┐
    ↓          ↓          ↓
  FM      CB         Future
Cluster Cluster    Systems
```

---

## The Problem Weaver Solves

Today, organizations build separate gateways for each system:
- FM-Gateway for Fabric Management
- CB-Gateway for ControllerBridge
- FR-Gateway for future systems (coming soon?)

**Problem:** Each gateway duplicates ~60% of the same functionality:
- Pod discovery
- Health checking
- Load balancing
- Circuit breakers
- Retry logic
- Metrics collection
- Configuration management

This leads to:
- Code duplication and maintenance burden
- Inconsistent behavior across systems
- Slower time-to-market for new systems

---

## The Solution: Weaver

Weaver replaces all of this with:

✅ **Single Codebase, Single Binary** — Deploy the same binary to FM, CB, FR, and future systems

✅ **Configuration-Driven** — All differences are in YAML config, not code. FM, CB, and custom systems use different configs for the same binary.

✅ **Production-Ready** — Circuit breakers, retries, timeouts, health monitoring, metrics, tracing, and logging built-in from day 1

✅ **Extensible** — Pluggable discovery (etcd, Consul, Kubernetes, DNS, static), health checkers, load balancers, authentication, rate limiters, and metrics providers

✅ **Industry-Grade** — Comparable to production gateways like Envoy, Kong, and Istio used at hyperscale companies

---

## Key Characteristics

| Feature | Benefit |
|---------|---------|
| **Single Binary** | One artifact to build, test, and deploy |
| **Config-Driven** | No code changes for new systems; just YAML |
| **Pod Discovery** | Automatically finds replicas from etcd, Consul, K8s, DNS, or static lists |
| **Health Monitoring** | Continuously checks replica health (HTTP, gRPC, TCP, custom) |
| **8 Load Balancers** | Choose the right strategy: Least-Connections, Round-Robin, Consistent Hash, Weighted, Sticky, Resource-Aware, Random, or Custom |
| **Circuit Breaker** | Fail-fast when replicas are down; auto-recover when healthy |
| **Retry with Backoff** | Automatically retry transient failures with exponential backoff |
| **Request Queuing** | Handle traffic spikes with request buffering and backpressure |
| **Rate Limiting** | Global, per-client, per-IP, per-tenant rate limiting |
| **Observability** | Prometheus metrics, OpenTelemetry tracing, structured JSON logging |
| **Authentication** | Bearer token, API key, JWT, mTLS (pluggable) |
| **Authorization** | RBAC (role-based access control) |
| **Multi-Protocol** | gRPC (bidirectional streams), HTTP/REST, extensible to custom protocols |
| **Zero-Restart Config** | Update configuration without restarting the gateway |
| **Debug Endpoints** | Inspect replica state, routing decisions, and gateway health |

---

## Deployment Models

Weaver supports three deployment models:

### **Kubernetes (Recommended for Production)**
Deploy as a Kubernetes Deployment with high availability, auto-scaling, and monitoring.
→ See [10-kubernetes.md](../QUICK_START/10-kubernetes.md)

### **Docker Compose (Development)**
Deploy locally with Docker Compose for testing and integration testing.
→ See [11-docker-compose.md](../QUICK_START/11-docker-compose.md)

### **Standalone Binary (Small Deployments)**
Deploy as a single binary on bare metal or small VMs.
→ See [12-standalone-binary.md](../QUICK_START/12-standalone-binary.md)

---

## Use Cases

### **Use Case 1: FM Primary-Aware Routing**
FM has a PRIMARY replica (handles writes) and READ replicas (handle queries).

**Weaver routes:**
- Registrations/Writes → PRIMARY only
- Queries/Subscriptions → Load-balanced across all replicas

→ See [20-fm-primary-aware.md](../SCENARIOS/20-fm-primary-aware.md)

---

### **Use Case 2: CB Peer-Equivalent Routing**
CB has peer-equivalent replicas; all replicas are equal.

**Weaver routes:**
- All requests → Load-balanced across all replicas
- Exposes gRPC (bidirectional streams) and REST (observability)

→ See [21-cb-peer-equivalent.md](../SCENARIOS/21-cb-peer-equivalent.md)

---

### **Use Case 3: Custom System Integration**
Need a gateway for a new system (FR, or your custom system)?

**Weaver routes:**
- Configuration changes only (no code changes)
- Pick your discovery method, health check type, load balancing strategy
- Same binary, different config

→ See [22-custom-system.md](../SCENARIOS/22-custom-system.md)

---

## Performance Targets

Weaver is designed for production scale:

| Metric | Target |
|--------|--------|
| Routing latency (p99) | <1ms |
| Request throughput | 100,000 req/s per instance |
| Health check latency | <100ms per replica |
| Memory footprint | <50MB baseline |
| Failover detection | <5 seconds |
| Config reload time | <1 second (zero-restart) |

---

## Architecture at a Glance

Weaver has 7 core layers:

1. **Pod Discovery** — Find all available replicas
2. **Health Monitoring** — Check if each replica is healthy
3. **Load Balancing** — Select which replica to route to (8 strategies)
4. **Request Routing** — Execute the routing decision
5. **Reliability** — Circuit breaker, retry, timeout, queuing
6. **Protocol Handling** — gRPC, HTTP, REST, custom
7. **Observability** — Metrics, tracing, logging

Each layer is pluggable and can be customized via configuration.

→ See [40-hld.md](../DESIGN/40-hld.md) for complete architecture details

---

## Next Steps

**Choose your path:**

- **🚀 I want to deploy Weaver now** → [10-kubernetes.md](../QUICK_START/10-kubernetes.md) (5 min)
- **📚 I want to understand the concepts** → [01-concepts.md](./01-concepts.md) (10 min)
- **🏗️ I want to understand the architecture** → [02-architecture-overview.md](./02-architecture-overview.md) (20 min)
- **🎯 I have a specific use case in mind** → [SCENARIOS/](../SCENARIOS/) (pick your scenario)

---

**Navigation:**
- [← Previous](../INDEX.md)
- [Next →](./01-concepts.md)
