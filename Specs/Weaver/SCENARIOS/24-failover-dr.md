# Weaver: Failover & Disaster Recovery Scenario

> **Read Time:** 15 minutes  
> **Scenario:** Handling replica failures and recovering gracefully  
> **Audience:** Operators, SREs, Architects  
> **Previous:** [23-multi-tenant.md](./23-multi-tenant.md) | **Next:** [25-chaos-engineering.md](./25-chaos-engineering.md)

---

## Failure Scenarios & Recovery

### **Scenario 1: Single Replica Failure**

```
T=0s:    3 replicas healthy: [fm-1 ✓, fm-2 ✓, fm-3 ✓]
         
T=5s:    fm-2 crashes (hardware failure)
         Weaver still routes to fm-1, fm-3
         
T=10s:   Health check fails for fm-2 (3 consecutive failures)
         fm-2 marked UNHEALTHY
         Circuit breaker OPENS (fail-fast)
         
T=20s:   Ops team notified (alert: "fm-2 UNHEALTHY")
         Ops restarts fm-2
         
T=30s:   fm-2 becomes responsive
         Weaver tries 1 request (HALF_OPEN state)
         Request succeeds → fm-2 marked HEALTHY
         Clients automatically resume using fm-2
         
T=35s:   Load normalized: [fm-1 ✓, fm-2 ✓, fm-3 ✓]
         No manual intervention needed; automatic recovery
```

**Impact:** ~30 seconds of reduced capacity (2 replicas instead of 3)

---

### **Scenario 2: Cascade Failure (Multiple Replicas Down)**

```
T=0s:    [fm-1 ✓, fm-2 ✓, fm-3 ✓] (3 healthy)

T=10s:   Network partition: fm-1 unreachable
         fm-1 marked UNHEALTHY
         Remaining: [fm-2 ✓, fm-3 ✓] (2 healthy)
         
T=20s:   Database connectivity issue affects fm-2, fm-3
         Both marked UNHEALTHY
         Remaining: [fm-1 ✓] (1 healthy)
         
T=25s:   Panic mode activated (>50% unhealthy)
         Circuit breaker does NOT open (prevents total outage)
         fm-1 accepts ALL traffic (overloaded)
         
T=35s:   Database fixed
         fm-2, fm-3 health checks pass → marked HEALTHY
         Load redistributes: [fm-1 ✓, fm-2 ✓, fm-3 ✓] (3 healthy)

Impact: ~15 seconds of degraded service (single replica), automatic recovery
```

---

### **Scenario 3: PRIMARY Failure (FM System)**

```
T=0s:    FM PRIMARY: fm-1 ✓ (handles all writes)
         FM READs:   fm-2 ✓, fm-3 ✓ (handle queries)

T=10s:   fm-1 (PRIMARY) crashes
         Circuit breaker opens immediately
         
         NEW BEHAVIOR:
         - Write requests: FAIL (no PRIMARY available)
         - Read requests: Still work (distributed to fm-2, fm-3)

T=20s:   Ops team notified: "PRIMARY fm-1 UNHEALTHY"
         Ops must manually promote fm-2 or fm-3 to PRIMARY
         
T=30s:   Ops completes promotion: fm-2 becomes PRIMARY
         etcd updated: fm-2 tagged as "role=primary"
         Weaver detects change (discovery poll)
         
T=40s:   Write requests now work → fm-2 (new PRIMARY)
         Service restored

Impact: Requires manual intervention; ~40 seconds of write unavailability
MITIGATION: Configure automated failover (Kubernetes leader election)
```

---

## Disaster Recovery Checklist

**Before Deployment:**
- [ ] 3+ replicas deployed (survive 2 failures)
- [ ] Circuit breaker configured (30s timeout)
- [ ] Panic mode enabled (>50% threshold)
- [ ] Health checks < 10s interval
- [ ] Monitoring/alerts configured

**During Incident:**
- [ ] Monitor dashboard: replica status, circuit breaker state
- [ ] Check logs: which replica failed? Why?
- [ ] If manual intervention needed: promote new PRIMARY or restart replica

**Post-Incident:**
- [ ] Root cause analysis: What caused failure?
- [ ] Update runbook if new scenario discovered
- [ ] Test automated recovery procedure

---

## Recovery Time Objectives (RTO)

| Scenario | RTO | Notes |
|----------|-----|-------|
| **Single replica failure** | 30s | Automatic detection + recovery |
| **Primary failure (FM)** | 40s+ | Requires manual failover |
| **Network partition** | 30s | Detected via health check timeout |
| **Cascading failure** | 15s + manual | Panic mode prevents total outage |
| **Data corruption** | 1h+ | Manual restore from backup |

---

## Backup & Recovery

**Configuration Backup:**
```bash
# Backup weaver-config.yaml
kubectl get configmap weaver-config -n weaver -o yaml > backup/weaver-config.yaml

# Restore from backup
kubectl apply -f backup/weaver-config.yaml
```

**Replica Data Backup:**
- Each system (FM, CB) manages their own data backups
- Weaver is stateless; no data to backup
- Recovery: Restore replica data; Weaver auto-discovers

---

## Next Steps

- **See Chaos Engineering Scenario** → [25-chaos-engineering.md](./25-chaos-engineering.md)
- **Troubleshooting Guide** → [54-troubleshooting.md](../OPERATIONS/54-troubleshooting.md)
- **Production Runbook** → [53-production-runbook.md](../OPERATIONS/53-production-runbook.md)

---

**Navigation:**
- [← Previous](./23-multi-tenant.md)
- [Index](../INDEX.md)
- [Next →](./25-chaos-engineering.md)
