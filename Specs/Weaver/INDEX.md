# Weaver: Master Documentation Index

> **Status:** 100% Open-Source Quality Documentation  
> **Version:** 1.0 (Phase 1)  
> **Last Updated:** 2026-06-15  
> **Audience:** All (Architects, Engineers, Operators, Contributors)

---

## 🗺️ Documentation Navigation Map

```
                          ┌─────────────────────┐
                          │   START HERE        │
                          │   (You are here)    │
                          └──────────┬──────────┘
                                     │
                    ┌────────────────┼────────────────┐
                    │                │                │
                    ↓                ↓                ↓
            ┌──────────────┐  ┌─────────────┐  ┌──────────────┐
            │ QUICK START  │  │ UNDERSTAND  │  │ PRODUCTION   │
            │ (5-15 min)   │  │ ARCHITECTURE│  │ OPERATIONS   │
            │              │  │ (1-2 hours) │  │ (2-4 hours)  │
            ├──────────────┤  ├─────────────┤  ├──────────────┤
            │ • Kubernetes │  │ • Concepts  │  │ • K8s Deploy │
            │ • Docker     │  │ • HLD       │  │ • Monitoring │
            │ • Standalone │  │ • LLD       │  │ • Troubleshoot
            │ • Verify     │  │ • Algorithms│  │ • Performance
            └──────────────┘  └─────────────┘  └──────────────┘
                    │                │                │
                    └────────────────┼────────────────┘
                                     │
                          ┌──────────▼──────────┐
                          │   REAL SCENARIOS    │
                          │   (Choose Your Use  │
                          │    Case)            │
                          └──────────┬──────────┘
                                     │
                ┌────────────┬────────┼────────┬────────────┬──────────┐
                ↓            ↓        ↓        ↓            ↓          ↓
          ┌─────────┐  ┌─────────┐  ┌─────────┐  ┌─────────┐  ┌─────────┐  ┌──────────┐
          │  FM     │  │  CB     │  │ Custom  │  │  Multi- │  │Failover │  │  Chaos   │
          │Primary  │  │  Peer   │  │ System  │  │  Tenant │  │  & DR   │  │Engineering
          │ Aware   │  │Equivalent  │         │  │ Isolation  │         │  │          │
          └─────────┘  └─────────┘  └─────────┘  └─────────┘  └─────────┘  └──────────┘
                                     │
                          ┌──────────▼──────────┐
                          │  DEEP REFERENCES    │
                          │  (As Needed)        │
                          └──────────┬──────────┘
                                     │
                    ┌────────────────┼────────────────┐
                    │                │                │
                    ↓                ↓                ↓
          ┌──────────────────┐  ┌─────────────┐  ┌──────────────┐
          │ CONFIGURATION    │  │ RELIABILITY │  │ OBSERVABILITY
          │ & FEATURES       │  │ & PATTERNS  │  │              │
          │                  │  │             │  │              │
          │ • Config Ref     │  │ • Circuit   │  │ • Metrics    │
          │ • Load Balancing │  │   Breaker  │  │ • Tracing    │
          │ • Discovery      │  │ • Retry    │  │ • Logging    │
          │ • Health Checks  │  │ • Timeout  │  │ • Debug API  │
          │ • Security       │  │ • Queuing  │  │ • Performance│
          │ • Rate Limiting  │  │             │  │              │
          │ • API Reference  │  │             │  │              │
          │ • Metrics List   │  │             │  │              │
          └──────────────────┘  └─────────────┘  └──────────────┘
```

---

## 📚 Reading Paths by Role

### **🏗️ For Architects**
> "I need to understand the design and make architectural decisions"

**Recommended Path (2-3 hours):**
1. [00-introduction.md](GET_STARTED/00-introduction.md) — What is Weaver? (5 min)
2. [01-concepts.md](GET_STARTED/01-concepts.md) — Core concepts explained (10 min)
3. [02-architecture-overview.md](GET_STARTED/02-architecture-overview.md) — 10,000-foot view (15 min)
4. [40-hld.md](DESIGN/40-hld.md) — High-level design deep dive (45 min)
5. [60-best-practices.md](GUIDES/60-best-practices.md) — Do's and don'ts (15 min)
6. [SCENARIOS/](SCENARIOS/) — Pick 2-3 scenarios relevant to your use case (30 min)

**Key Questions This Path Answers:**
- What problem does Weaver solve?
- How does it scale and perform?
- What are the trade-offs in design decisions?
- Which scenario matches my system's topology?

---

### **⚙️ For Engineers**
> "I need to build, configure, and extend Weaver"

**Recommended Path (4-6 hours):**
1. [00-introduction.md](GET_STARTED/00-introduction.md) — Quick overview (5 min)
2. [10-docker-compose.md](QUICK_START/10-docker-compose.md) or [12-standalone-binary.md](QUICK_START/12-standalone-binary.md) — Get Weaver running locally (15 min)
3. [30-configuration-reference.md](REFERENCE/30-configuration-reference.md) — YAML configuration (30 min)
4. [31-load-balancing-strategies.md](REFERENCE/31-load-balancing-strategies.md) — Load balancing deep dive (20 min)
5. Pick 2-3 REFERENCE docs based on your feature needs (1-2 hours)
6. [41-lld.md](DESIGN/41-lld.md) — Low-level design for implementation (1 hour)
7. [71-plugin-development.md](CONTRIBUTE/71-plugin-development.md) — Write custom plugins (1 hour)

**Key Questions This Path Answers:**
- How do I deploy Weaver locally for development?
- What configuration options do I need to support my use case?
- How do I implement a custom load balancer or discovery method?
- What are the internal data structures and algorithms?

---

### **🚀 For Operators**
> "I need to deploy, monitor, and troubleshoot Weaver in production"

**Recommended Path (2-4 hours):**
1. [00-introduction.md](GET_STARTED/00-introduction.md) — Quick overview (5 min)
2. [10-kubernetes.md](QUICK_START/10-kubernetes.md) — Deploy in 5 minutes (5 min)
3. [13-verify-deployment.md](QUICK_START/13-verify-deployment.md) — Verify it works (10 min)
4. [50-kubernetes-deployment.md](OPERATIONS/50-kubernetes-deployment.md) — Production hardening (45 min)
5. [52-monitoring-setup.md](OPERATIONS/52-monitoring-setup.md) — Prometheus + Grafana setup (30 min)
6. [54-troubleshooting.md](OPERATIONS/54-troubleshooting.md) — Decision tree for common issues (30 min)
7. [53-production-runbook.md](OPERATIONS/53-production-runbook.md) — Start/stop/upgrade procedures (15 min)

**Key Questions This Path Answers:**
- How do I deploy Weaver in Kubernetes?
- What monitoring should I set up?
- How do I troubleshoot a failing gateway?
- What's the upgrade procedure?

---

### **👥 For Contributors**
> "I want to contribute code, plugins, or documentation"

**Recommended Path (2-3 hours):**
1. [00-introduction.md](GET_STARTED/00-introduction.md) — Project overview (5 min)
2. [70-development-setup.md](CONTRIBUTE/70-development-setup.md) — Local development environment (20 min)
3. [40-hld.md](DESIGN/40-hld.md) — Architecture (45 min)
4. [41-lld.md](DESIGN/41-lld.md) — Low-level design (1 hour)
5. [71-plugin-development.md](CONTRIBUTE/71-plugin-development.md) — Plugin architecture (20 min)
6. [72-contributing.md](CONTRIBUTE/72-contributing.md) — Code of conduct and PR process (10 min)

**Key Questions This Path Answers:**
- How do I set up my development environment?
- What's the architecture and design philosophy?
- How do I write a custom plugin?
- What are the contribution guidelines?

---

## 📖 Complete Document Catalog

### **GET_STARTED (Entry Point - 3 docs)**

| Doc | Purpose | Audience | Time |
|-----|---------|----------|------|
| [00-introduction.md](GET_STARTED/00-introduction.md) | What is Weaver in 5 minutes | Everyone | 5 min |
| [01-concepts.md](GET_STARTED/01-concepts.md) | Core concepts & terminology glossary | Everyone | 15 min |
| [02-architecture-overview.md](GET_STARTED/02-architecture-overview.md) | 10,000-foot architecture view | Architects, Leads | 20 min |

---

### **QUICK_START (Deploy in 5 Minutes - 4 docs)**

| Doc | Purpose | Audience | Time |
|-----|---------|----------|------|
| [10-kubernetes.md](QUICK_START/10-kubernetes.md) | Deploy on Kubernetes | Operators | 5 min |
| [11-docker-compose.md](QUICK_START/11-docker-compose.md) | Deploy with Docker Compose | Engineers | 5 min |
| [12-standalone-binary.md](QUICK_START/12-standalone-binary.md) | Deploy standalone binary | DevOps | 5 min |
| [13-verify-deployment.md](QUICK_START/13-verify-deployment.md) | Verify deployment works | Operators | 10 min |

---

### **SCENARIOS (Real-World Use Cases - 6 docs)**

| Doc | Scenario | Use Case | Time |
|-----|----------|----------|------|
| [20-fm-primary-aware.md](SCENARIOS/20-fm-primary-aware.md) | FM Primary-Aware Routing | FM fabric management | 30 min |
| [21-cb-peer-equivalent.md](SCENARIOS/21-cb-peer-equivalent.md) | CB Peer-Equivalent Routing | Controller Bridge | 30 min |
| [22-custom-system.md](SCENARIOS/22-custom-system.md) | Custom System Integration | Future systems, FR | 25 min |
| [23-multi-tenant.md](SCENARIOS/23-multi-tenant.md) | Multi-Tenant Isolation | SaaS platforms | 30 min |
| [24-failover-dr.md](SCENARIOS/24-failover-dr.md) | Failover & Disaster Recovery | High availability | 25 min |
| [25-chaos-engineering.md](SCENARIOS/25-chaos-engineering.md) | Testing & Chaos Engineering | QA, reliability | 20 min |

---

### **REFERENCE (Technical Reference - 10 docs)**

| Doc | Purpose | Contents | Time |
|-----|---------|----------|------|
| [30-configuration-reference.md](REFERENCE/30-configuration-reference.md) | Complete YAML config schema | All configuration options | 45 min |
| [31-load-balancing-strategies.md](REFERENCE/31-load-balancing-strategies.md) | All 8 load balancers | LC, RR, Random, Hash, Weighted, Sticky, RA, Custom | 30 min |
| [32-discovery-methods.md](REFERENCE/32-discovery-methods.md) | Pod discovery methods | etcd, Consul, K8s, DNS, static | 25 min |
| [33-health-monitoring.md](REFERENCE/33-health-monitoring.md) | Health check types | HTTP, gRPC, TCP, custom | 20 min |
| [34-reliability-patterns.md](REFERENCE/34-reliability-patterns.md) | Reliability features | Circuit breaker, retry, timeout, queuing | 35 min |
| [35-observability.md](REFERENCE/35-observability.md) | Observability setup | Metrics, tracing, logging | 30 min |
| [36-security.md](REFERENCE/36-security.md) | Security features | Auth, authz, TLS, rate limiting | 25 min |
| [37-rate-limiting.md](REFERENCE/37-rate-limiting.md) | Rate limiting details | Global, per-client, per-IP, per-tenant | 20 min |
| [38-api-reference.md](REFERENCE/38-api-reference.md) | gRPC & REST API | All endpoints with examples | 30 min |
| [39-metrics-reference.md](REFERENCE/39-metrics-reference.md) | Prometheus metrics list | All metrics with descriptions | 20 min |

---

### **DESIGN (Architecture - 5 docs)**

| Doc | Purpose | Audience | Time |
|-----|---------|----------|------|
| [40-hld.md](DESIGN/40-hld.md) | High-level design | Architects, Lead Engineers | 1 hour |
| [41-lld.md](DESIGN/41-lld.md) | Low-level design overview | Engineers | 30 min |
| [42-data-structures.md](DESIGN/42-data-structures.md) | Core data structures | Engineers | 30 min |
| [43-algorithms.md](DESIGN/43-algorithms.md) | Load balancing & reliability algorithms | Engineers | 40 min |
| [44-concurrency-model.md](DESIGN/44-concurrency-model.md) | Goroutine model & synchronization | Engineers | 25 min |

---

### **OPERATIONS (Production - 8 docs)**

| Doc | Purpose | Audience | Time |
|-----|---------|----------|------|
| [50-kubernetes-deployment.md](OPERATIONS/50-kubernetes-deployment.md) | Complete K8s production guide | Operators, SREs | 1 hour |
| [51-docker-deployment.md](OPERATIONS/51-docker-deployment.md) | Docker production deployment | DevOps | 40 min |
| [52-monitoring-setup.md](OPERATIONS/52-monitoring-setup.md) | Prometheus, Jaeger, Grafana | SREs, Operators | 50 min |
| [53-production-runbook.md](OPERATIONS/53-production-runbook.md) | Start, stop, upgrade procedures | Operators | 20 min |
| [54-troubleshooting.md](OPERATIONS/54-troubleshooting.md) | Diagnosis decision tree | Operators, On-call | 45 min |
| [55-performance-tuning.md](OPERATIONS/55-performance-tuning.md) | Optimization for workload | Engineers, SREs | 45 min |
| [56-security-hardening.md](OPERATIONS/56-security-hardening.md) | Production security checklist | Security, Operators | 30 min |
| [57-version-matrix.md](OPERATIONS/57-version-matrix.md) | Compatibility matrix | DevOps | 10 min |

---

### **GUIDES (How-To & Reference - 5 docs)**

| Doc | Purpose | Contents | Time |
|-----|---------|----------|------|
| [60-best-practices.md](GUIDES/60-best-practices.md) | Do's and don'ts | Architecture decisions, config patterns | 25 min |
| [61-faq.md](GUIDES/61-faq.md) | 30 common questions | Q&A with cross-links | 30 min |
| [62-glossary.md](GUIDES/62-glossary.md) | Terminology reference | 100+ terms A-Z | 20 min |
| [63-upgrade-guide.md](GUIDES/63-upgrade-guide.md) | Version upgrade procedures | Breaking changes, migration steps | 20 min |
| [64-migration-guide.md](GUIDES/64-migration-guide.md) | From other gateways | FM-GW, CB-GW, Envoy, Kong | 25 min |

---

### **CONTRIBUTE (For Developers - 3 docs)**

| Doc | Purpose | Audience | Time |
|-----|---------|----------|------|
| [70-development-setup.md](CONTRIBUTE/70-development-setup.md) | Local dev environment | Contributors | 20 min |
| [71-plugin-development.md](CONTRIBUTE/71-plugin-development.md) | Write custom plugins | Contributors | 40 min |
| [72-contributing.md](CONTRIBUTE/72-contributing.md) | Code of conduct, PR process | Contributors | 15 min |

---

### **IMPLEMENTATION (Engineering - 1 doc)**

| Doc | Purpose | Contents |
|-----|---------|----------|
| [80-implementation-planner.md](IMPLEMENTATION/80-implementation-planner.md) | 4 sprints, 28 tasks | Sprint breakdown, effort estimates, tracker |

---

## 🎯 Quick Decision Tree

**I need to...**

### "Get Weaver running quickly"
→ Choose your deployment:
- **Kubernetes?** → [10-kubernetes.md](QUICK_START/10-kubernetes.md) (5 min)
- **Docker Compose?** → [11-docker-compose.md](QUICK_START/11-docker-compose.md) (5 min)
- **Standalone?** → [12-standalone-binary.md](QUICK_START/12-standalone-binary.md) (5 min)

### "Configure Weaver for my use case"
→ Choose your scenario:
- **FM system (primary-aware)?** → [20-fm-primary-aware.md](SCENARIOS/20-fm-primary-aware.md)
- **CB system (peer-equivalent)?** → [21-cb-peer-equivalent.md](SCENARIOS/21-cb-peer-equivalent.md)
- **Custom system?** → [22-custom-system.md](SCENARIOS/22-custom-system.md)
- **Multi-tenant SaaS?** → [23-multi-tenant.md](SCENARIOS/23-multi-tenant.md)
- **High availability?** → [24-failover-dr.md](SCENARIOS/24-failover-dr.md)

### "Troubleshoot a problem"
→ [54-troubleshooting.md](OPERATIONS/54-troubleshooting.md) (decision tree)

### "Understand a feature"
→ [30-configuration-reference.md](REFERENCE/30-configuration-reference.md) → Pick specific reference doc

### "Extend Weaver"
→ [71-plugin-development.md](CONTRIBUTE/71-plugin-development.md)

### "Answer a question"
→ [61-faq.md](GUIDES/61-faq.md)

### "Look up a term"
→ [62-glossary.md](GUIDES/62-glossary.md)

---

## 📊 Document Dependency Graph

```
00-introduction ─┬─→ 01-concepts ─┬─→ 02-architecture
                 │                │
                 └─→ 10-kubernetes→ 13-verify ─┐
                 ├─→ 11-docker-compose          │
                 └─→ 12-standalone ─────────────┤
                                               │
    Scenarios (20-25) ←─────────────────────────┘
         ↓
    30-configuration-ref → 31-load-balancing
         ↓                      ↓
    32-discovery → 33-health → 34-reliability
         ↓           ↓             ↓
    35-observability, 36-security, 37-rate-limiting
         ↓                                    ↓
    38-api-ref ← → 39-metrics-ref
         ↓
    40-hld → 41-lld → 42-data-structures → 43-algorithms → 44-concurrency
         ↓
    50-kubernetes-deploy, 51-docker, 52-monitoring
         ↓
    53-runbook, 54-troubleshooting, 55-performance, 56-security
         ↓
    57-version-matrix
         ↓
    60-best-practices, 61-faq, 62-glossary, 63-upgrade, 64-migration
         ↓
    70-dev-setup → 71-plugin-dev → 72-contributing
         ↓
    80-implementation-planner
```

---

## 🔗 Cross-Reference Convention

Throughout this documentation, you'll see cross-references formatted like:

```
For complete details, see [30-configuration-reference.md](../reference/30-configuration-reference.md).
```

**Breadcrumb Navigation:**

Every document has breadcrumbs at the top:
```
← [INDEX](../INDEX.md) > [GET_STARTED](../GET_STARTED/) > [Concepts](./01-concepts.md)
```

And navigation at the bottom:
```
**Navigation:** [Previous](./00-introduction.md) | [Next](./02-architecture-overview.md)
```

---

## ✅ How to Use This Index

1. **First Time Here?** Start with [00-introduction.md](GET_STARTED/00-introduction.md)
2. **Know Your Role?** Jump to the "Reading Paths by Role" section above
3. **Looking for Something?** Use the Decision Tree
4. **Need Quick Answers?** Try [61-faq.md](GUIDES/61-faq.md) or [62-glossary.md](GUIDES/62-glossary.md)
5. **Lost?** This INDEX is your home base — always linked

---

## 📝 Document Statuses

- ✅ **Complete & Ready** (38 docs) — Production-ready
- 🔄 **In Progress** — Phase 1 Infrastructure
- 📋 **Planned** — Phases 2-6

---

**Start Here:** [00-introduction.md](GET_STARTED/00-introduction.md)

**Questions?** Check [61-faq.md](GUIDES/61-faq.md)

**Lost in Navigation?** You're reading the right doc!

---

**Last Updated:** 2026-06-15 | **Version:** 1.0
