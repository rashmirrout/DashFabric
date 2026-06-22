# REG_008 — Registry Cache OOM / Heap Pressure

**Severity:** CRITICAL
**Subsystem:** Registry (`pkg/fm/registry`)
**SLO Impact:** Composition latency spikes; risk of OOM-kill of the FM pod

---

## 1. Symptoms

- Alert `fm.heap.in_use_bytes > 85% of limit` AND `fm.registry.entry_count` climbing monotonically.
- Logs: `registry: cache eviction backlog growing` or `gc: GOGC trigger increased`.
- P99 `NicActor.Compose` latency > 1 s (normal is <50 ms).
- Pod `OOMKilled` events on the FM pod within last hour.

## 2. Likely Causes (ordered)

1. Acquire without Release after a deploy that changed actor lifecycle (regression).
2. Hot prefix-tag fanout — one tag holding tens of thousands of prefixes is referenced by many ENIs.
3. T1 fan-in: VnetMappingRegistry receiving a flood of mapping updates that re-create entries faster than eviction.
4. Memory limit lowered in the deployment manifest without bumping registry capacity envelope.

## 3. Diagnostics (read-only)

```bash
# Top 20 registries by entry count
fmctl registry top --by=entries --limit=20

# Heap profile (cheap; samples on demand)
fmctl debug heap --duration=10s --out=/tmp/heap.pprof
go tool pprof -top /tmp/heap.pprof | head -40

# Per-registry memory estimate
fmctl registry inspect --memory
```

```promql
# Which registry is leaking
topk(5, fm_registry_entries{registry=~".*"})

# Acquire/Release imbalance over last 30m (correlate with REG_007 too)
sum by (registry) (rate(fm_registry_acquire_total[30m])) -
sum by (registry) (rate(fm_registry_release_total[30m]))
```

## 4. Remediation

**Goal:** Get below 70% heap headroom; do NOT lose programmed-state consistency.

1. **Throttle ingress** to the offending registry first:
   `fmctl registry throttle --name=<reg> --max-rps=200`
   *Rollback:* `fmctl registry throttle --name=<reg> --reset`

2. **Force-evict cold entries** (entries with `last_used_at` > 30 min, refcount=0):
   `fmctl registry evict --name=<reg> --idle=30m`
   *Rollback:* none — re-Acquire will rehydrate from T1.

3. **If heap is still > 80% after step 2**, rotate the pod with surge:
   `kubectl rollout restart deployment/fm --replicas=+1`
   The new pod takes over via existing leader election; old pod drains and exits cleanly.
   *Rollback:* `kubectl rollout undo deployment/fm`.

4. **If a single prefix-tag is the culprit**, ask CB owner to split it (operator action, hours, not minutes). Document in incident ticket.

## 5. Rollback

- `fmctl registry evict` is safe to undo by Acquire — DO NOT manually re-insert.
- If pod rotation makes things worse (new pod also OOMs in <2 min), the issue is data-driven, not deploy-driven. Revert any recent CB config push and re-bake the failing input set offline.

## 6. Escalate When

- Heap > 90% AND eviction rate < acquire rate.
- Two pods in the deployment OOM within 10 minutes.
- Heap profile shows a non-registry leak (e.g., goroutine leak in driver pool) — switch runbooks to the relevant subsystem.

## 7. References

- `registry-pattern-design.md` §Eviction policy
- `threading-model-design.md` §11 — pod memory budgets
- `storage-architecture.md` T1 rehydration cost
- `error-handling-design.md` REG_008 entry
