# Weaver: Complete Glossary

> **Read Time:** 30 minutes  
> **Purpose:** Comprehensive terminology reference for all Weaver concepts  
> **Audience:** Everyone  
> **Previous:** [INDEX.md](../INDEX.md) | **Next:** [61-faq.md](./61-faq.md)

---

## A

### **Active Connection**
A currently open connection between Weaver and a replica. Used by least-connections load balancer to determine which replica has the least number of active connections. Example: gRPC Subscribe stream is 1 active connection.

### **Affinity**
Property of routing where the same client always routes to the same replica. Achieved using consistent hash or sticky load balancer. Benefits: maintain session state, reduce cache misses. Trade-off: less balanced load if some clients are heavier than others.

### **API Key**
Static authentication credential (string token). Example: `X-API-Key: abc123def456`. Used for service-to-service authentication. Simpler than JWT but no expiration built-in.

### **Authorization**
Verification of what actions an authenticated client can perform. Example: "User is authenticated as admin, so they can write to any topic". See also: Authentication, RBAC.

### **Autoscaling**
Automatic increase/decrease of replica count based on load. Example: Kubernetes HPA (Horizontal Pod Autoscaler) scales CB replicas from 4 to 10 when CPU > 80%.

---

## B

### **Bearer Token**
Authentication method using `Authorization: Bearer <token>` header. Token can be opaque (server-generated) or JWT (client-verifiable). See also: Authentication, JWT.

### **Backoff**
Wait time between retry attempts. Strategies: constant (wait same time), linear (10ms, 20ms, 30ms), exponential (10ms, 20ms, 40ms). See also: Exponential Backoff, Retry.

### **Bidirectional Stream**
gRPC feature where client and server can send messages independently. Example: FM Subscribe streams updates from server while client can Publish new events. Enables real-time communication. See also: Unary RPC.

### **Buffer**
Temporary storage for data in transit. Example: Request queue buffers incoming requests when replicas are slow. Size: per_replica_depth (e.g., 1000 requests).

### **Bulkhead**
Pattern of isolating resources (connections, threads, queues) to prevent one failure from affecting others. Example: Per-replica request queue prevents one slow replica from exhausting shared resources.

---

## C

### **CB (ControllerBridge)**
DashFabric system for distributed event brokering. Topology: Peer-equivalent (all replicas equal). Routing: Symmetric (any request to any replica). Consistency: Eventual. See also: FM, Peer-Equivalent.

### **Chaos Engineering**
Intentional introduction of failures to test system resilience. Example: Kill one replica and verify Weaver detects failure and routes around it within 30 seconds.

### **Circuit Breaker**
State machine protecting against cascading failures. States: CLOSED (normal) → OPEN (fail-fast) → HALF_OPEN (testing recovery) → CLOSED (recovered). Similar to electrical circuit breaker that trips on overload.

### **Cloud Native**
Patterns for building applications for cloud environments. Includes: containerization, microservices, scalability, resilience. Weaver follows cloud-native principles.

### **Cluster**
Group of replicas running the same service. Example: FM cluster has 3-4 replicas. Each replica registers in etcd with key like `/dashfabric/cluster/pods/fm-1`.

### **Consistent Hash**
Load balancing algorithm where Hash(client_id) → same replica always. Benefit: session affinity. Trade-off: less balanced load. Uses virtual nodes to handle replica failures.

### **Connection Pool**
Cache of open connections to replicas. Benefit: reuse connections (avoids connection overhead). Drawback: connection counts limit (e.g., 65k TCP connections per host).

---

## D

### **DIP (Device IP)**
Internal IP address of a device/replica in network. Weaver routes to replica's DIP (not VIP).

### **Discovery**
Process of finding available replicas. Methods: etcd, Consul, Kubernetes API, DNS, static config. Weaver polls every 10s for changes.

### **DNS**
Domain Name System discovery. Example: Weaver queries DNS for `fm.service.consul` → returns list of IP:port for FM replicas.

---

## E

### **Endpoint**
Network address where Weaver listens or connects to. Example: `127.0.0.1:5051` (gRPC), `127.0.0.1:8080` (HTTP), `127.0.0.1:9090` (Metrics).

### **etcd**
Key-value store used for pod discovery in DashFabric. FM and CB replicas register themselves in etcd with keys like `/dashfabric/cluster/pods/fm-1`. Weaver polls etcd every 10s.

### **Eventually Consistent**
Consistency model where replicas converge to the same state over time (not immediately). Example: CB publishes event to cb-1, but cb-2 doesn't see it for 50ms (until replication). See also: Strong Consistency.

### **Exponential Backoff**
Retry strategy with exponentially increasing wait times. Example: 10ms → 20ms → 40ms → 80ms → 160ms. Benefits: avoids hammering failing service. See also: Retry, Backoff.

---

## F

### **Failover**
Automatic switch to backup when primary fails. Example: FM PRIMARY replica (fm-1) fails → writes are rejected until PRIMARY is restored or failover to backup happens. Not automatic in Weaver (manual operation).

### **FM (Fabric Management)**
DashFabric system for managing device inventory and state. Topology: Primary-aware (1 PRIMARY + N READ). Routing: Asymmetric (writes to PRIMARY only, reads to any). Consistency: Strong. See also: CB, Primary-Aware.

### **Framework**
Software library providing reusable components. Weaver is not a framework; it's a gateway (single binary).

---

## G

### **Gateway**
Proxy server sitting between clients and backend replicas. Responsibilities: discovery, health checks, load balancing, routing, reliability, observability.

### **Global Limit**
Rate limit applied to all traffic through gateway. Example: `global: 100k req/s`. If exceeded, requests rejected. See also: Rate Limiting.

### **gRPC**
RPC framework by Google. Features: HTTP/2, binary encoding, bidirectional streaming, lower latency than HTTP. Used by FM ↔ Weaver and CB ↔ Weaver. See also: HTTP, REST.

---

## H

### **HALF_OPEN State (Circuit Breaker)**
Temporary state where circuit breaker allows 1 test request to see if replica has recovered. If succeeds → CLOSED. If fails → OPEN.

### **Health Check**
Periodic ping to verify replica is alive and healthy. Types: HTTP, gRPC, TCP. Interval: 10s. Consecutive failures to mark UNHEALTHY: 3.

### **High Availability (HA)**
System design ensuring continued operation if components fail. Weaver achieves HA by: 3+ replicas, health checks, failover to healthy replicas.

### **Horizontal Scaling**
Adding more replicas to increase throughput. Example: 4 CB replicas handle 100k req/s; add 4 more → 200k req/s. See also: Vertical Scaling.

### **HTTP**
HyperText Transfer Protocol. Text protocol, request-response only. Less efficient than gRPC but human-readable. Weaver uses HTTP for REST API (observability, debugging).

---

## I

### **Idempotent**
Request that produces same result no matter how many times executed. Example: Weaver retry is safe if backend operation is idempotent. Non-idempotent: delete operation (deleting twice has different result).

### **Ingress**
Entry point into system. Example: Weaver is ingress for FM/CB replicas. Kubernetes ingress: traffic router in K8s cluster.

---

## J

### **Jaeger**
Distributed tracing system. Weaver sends traces to Jaeger (OpenTelemetry protocol). Used to visualize request flow, debug latency.

### **JWT (JSON Web Token)**
Cryptographically signed token containing claims (e.g., user_id, tenant_id, roles). Can be verified without calling auth server. Used for authentication.

---

## K

### **Kubernetes**
Container orchestration platform. Weaver deployed as Deployment (multiple replicas) + Service (load balancer) + ConfigMap (configuration) on K8s.

---

## L

### **Latency**
Time delay. Example: p99 latency = 99th percentile latency (99% of requests faster than this). Weaver targets: routing latency <1ms, end-to-end <5ms.

### **Least Connections**
Load balancer strategy: route request to replica with fewest active connections. Benefits: balanced load. Assumption: all requests have similar work.

### **Load Balancer**
Component selecting which replica receives request. 8 strategies: Least-Connections, Round-Robin, Random, Consistent Hash, Weighted, Sticky, Resource-Aware, Custom.

---

## M

### **Metrics**
Numerical measurements of system behavior. Example: `fm_gw_requests_total=1000`, `fm_gw_latency_p99=2.5ms`. Collected by Prometheus, visualized in Grafana.

### **mTLS (Mutual TLS)**
Encryption where both client and server authenticate each other via certificates. More secure than one-way TLS. Weaver supports mTLS for client authentication.

---

## N

### **Network Policy**
Kubernetes object restricting network traffic. Example: "pods in namespace X can only reach pods in namespace Y". Used for security isolation.

---

## O

### **Observability**
Ability to understand system behavior. Three pillars: Metrics (Prometheus), Traces (Jaeger), Logs (structured JSON). Weaver provides all three.

### **OpenTelemetry**
Standard for collecting observability data (metrics, traces, logs). Weaver uses OTel for tracing.

---

## P

### **Panic Mode**
Safety feature: if >50% of replicas are unhealthy, circuit breaker does NOT open. Allows degraded service instead of total failure.

### **Peer-Equivalent**
Topology where all replicas are equal (no PRIMARY). Example: CB (all replicas can handle Publish, Subscribe, Query). Opposite: Primary-Aware (FM has PRIMARY for writes).

### **Per-Replica**
Per-instance limit. Example: `per_replica_depth=1000` means 1000 queued requests per replica (not total). Related: Per-Client, Per-IP, Global.

### **Plugin**
Extensible component. Weaver plugins: Discovery, Health Checker, Load Balancer, Authenticator, Rate Limiter, Metrics Provider.

### **Pod**
Kubernetes smallest deployable unit. Contains 1+ containers. Example: Weaver pod runs weaver container. FM pod runs FM container.

### **Pod Discovery**
Finding available replicas. See: Discovery.

### **Primary**
Replica designated for writes. Example: FM PRIMARY (fm-1) handles all registrations. READ replicas (fm-2, fm-3, fm-4) handle queries.

### **Primary-Aware**
Topology with one PRIMARY replica for writes and READ replicas for reads. Example: FM. Opposite: Peer-Equivalent (CB).

### **Prometheus**
Metrics database and monitoring system. Weaver exposes metrics on port 9090; Prometheus scrapes every 15s.

---

## Q

### **Query**
Read request to retrieve data. Example: FM Query returns list of devices. Opposite: Publish (write request).

### **Queue**
Buffer for pending requests. When replica busy, requests wait in queue. Size limit: per_replica_depth (e.g., 1000).

---

## R

### **RBAC (Role-Based Access Control)**
Authorization method based on user roles. Roles: admin (all permissions), operator (write permissions), viewer (read-only). User assigned to role; role has permissions.

### **Read**
Query operation. Example: FM Query, CB Subscribe. Opposite: Write, Publish.

### **Replica**
Single instance of backend service. Example: fm-1, fm-2, fm-3 are 3 replicas of FM. Status: HEALTHY (responding) or UNHEALTHY (not responding).

### **Request Timeout**
Maximum time to wait for response. Types: global (entire request), per-replica (per attempt), connect (connection establishment). Example: global=30s, per-replica=25s, connect=5s.

### **REST (Representational State Transfer)**
HTTP-based API style using standard methods (GET, POST, PUT, DELETE). Example: `GET /api/v1/topics` returns list of topics. Less efficient than gRPC but widely used.

### **Retry**
Automatic re-attempt of failed request. Strategy: exponential backoff (wait 10ms, 20ms, 40ms...). Max attempts: 3. Benefits: handle transient failures. Risk: unsafe for non-idempotent operations.

### **Round Robin**
Load balancer strategy: rotate through replicas (1 → 2 → 3 → 1 → ...). Benefits: simple, fair. Assumes equal request costs.

---

## S

### **Sampling (Tracing)**
Trace only subset of requests to reduce overhead. Example: sample_rate=0.1 traces 10% of requests.

### **Service**
Backend system (FM, CB, etc.). Weaver routes to replicas of a service.

### **Sticky**
Load balancer strategy remembering client→replica mapping for TTL. Benefits: affinity without consistent hash. Drawback: less effective if TTL expires.

### **Stream**
Persistent connection sending/receiving messages over time. Example: gRPC Subscribe stream receives events for hours. Opposite: unary (request-response).

### **Strong Consistency**
Consistency model where writes are immediately visible to all readers. Example: FM PRIMARY ensures all subsequent reads see the write. Opposite: Eventual Consistency.

---

## T

### **Tenant**
Customer or organization in multi-tenant system. Example: CB supports multi-tenant, each with own events/subscriptions. Rate limiting per tenant: each tenant gets quota.

### **Timeout**
Maximum wait time for operation. See: Request Timeout.

### **TLS (Transport Layer Security)**
Encryption protocol for network communication. Version 1.2+. Weaver supports client→Weaver TLS and Weaver→Replica TLS.

### **Topology**
Arrangement of replicas. Types: Primary-Aware (FM), Peer-Equivalent (CB).

### **Trace**
End-to-end record of request flow. Example: Client request → Weaver discovery (1ms) → LB selection (0.2ms) → connect (5ms) → send (0.5ms) → response (1.2ms) = 8ms total. Visualized in Jaeger.

### **Transient**
Temporary, likely to resolve on retry. Example: network timeout is transient; call retry. Opposite: permanent (replica down for hours).

---

## U

### **Unary RPC**
Simple request-response (not streaming). Example: FM Query is unary (send query, get response). Opposite: Bidirectional Stream.

---

## V

### **VIP (Virtual IP)**
Logical IP address for a service. Example: FM VIP used for NAT. Weaver routes to replica DIP (not VIP).

---

## W

### **Weaver**
Universal gateway platform. Single binary, config-driven, supports FM, CB, and future systems.

### **Weighted Load Balancer**
Load balancer routing more traffic to higher-weight replicas. Example: replica "fm-1" weight=2, "fm-2" weight=1 → twice as much traffic to fm-1.

### **Write**
Modify operation. Example: FM Register, CB Publish. Opposite: Read, Query.

---

## Z

### **Zero-Restart**
Configuration update without restarting gateway. Weaver detects config changes and reloads within 1 second. Benefit: no downtime.

---

## Cross-Reference by Category

### **Topology**
- Primary-Aware
- Peer-Equivalent
- Cluster
- Replica

### **Load Balancing**
- Least Connections
- Round Robin
- Consistent Hash
- Weighted
- Sticky
- Affinity

### **Reliability**
- Circuit Breaker
- Retry
- Timeout
- Request Queue
- Health Check
- Failover
- Panic Mode

### **Observability**
- Metrics
- Prometheus
- Traces
- Jaeger
- OpenTelemetry
- Logs
- Sampling

### **Security**
- Authentication
- Authorization
- JWT
- Bearer Token
- API Key
- mTLS
- TLS
- RBAC

### **Operations**
- Deployment
- Kubernetes
- Scaling
- Horizontal Scaling
- Vertical Scaling
- Autoscaling

### **Protocols**
- gRPC
- HTTP
- REST
- Bidirectional Stream
- Unary RPC

### **Systems**
- FM (Fabric Management)
- CB (ControllerBridge)
- DashFabric
- etcd
- Consul

---

## Acronyms Quick Reference

| Acronym | Expansion |
|---------|-----------|
| **ABAC** | Attribute-Based Access Control |
| **API** | Application Programming Interface |
| **CA** | Certificate Authority |
| **CB** | ControllerBridge |
| **DIP** | Device IP |
| **DRY** | Don't Repeat Yourself |
| **FM** | Fabric Management |
| **gRPC** | gRPC Remote Procedure Call |
| **HA** | High Availability |
| **HTTP** | HyperText Transfer Protocol |
| **JWT** | JSON Web Token |
| **K8s** | Kubernetes |
| **LB** | Load Balancer |
| **mTLS** | Mutual Transport Layer Security |
| **OTel** | OpenTelemetry |
| **p50/p99** | 50th/99th percentile |
| **RBAC** | Role-Based Access Control |
| **REST** | Representational State Transfer |
| **RFC** | Request For Comments |
| **RPC** | Remote Procedure Call |
| **SLA** | Service Level Agreement |
| **TLS** | Transport Layer Security |
| **TTL** | Time To Live |
| **VIP** | Virtual IP |

---

## How to Use This Glossary

1. **Search for term** — Use browser Find (Ctrl+F / Cmd+F)
2. **Read definition** — Clear, concise explanation
3. **See "See also"** — Links to related concepts
4. **Cross-reference** — Use "Cross-Reference by Category" to find similar terms

---

**Navigation:**
- [← Previous](../INDEX.md)
- [Index](../INDEX.md)
- [Next →](./61-faq.md)
