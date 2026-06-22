# ADP_005 — T1 (fm-data-store) Unreachable

**Severity:** CRITICAL
**Subsystem:** Storage adapter (`pkg/fm/storage`)
**SLO Impact:** All composition stalls. New CB writes invisible. Programmed state is NOT changing — read existing T3 mirrors continues to work for emergency reads.

---

## 1. Symptoms

- Alert `fm.adapter.t1.error_rate > 50%` for > 60 s.
- Logs: `adapter[t1]: connect: <error>` or `adapter[t1]: rpc: deadline exceeded`.
- Composition queue depth growing; `fm.compose.queue_depth` rising.
- Per-actor state stuck at WAITING_REFS or COMPOSING.

## 2. Likely Causes (ordered)

1. T1 cluster genuinely unreachable (network partition, T1 pods down).
2. Adapter-side TLS cert rotation failure — adapter has stale cert, T1 rejects (correlate with cert-expiry alerts in same window).
3. T1 leader election in progress (transient, expect <30 s).
4. DNS/service-mesh failure between FM and T1.

## 3. Diagnostics (read-only)

```bash
# Direct probe from inside the FM pod
fmctl probe t1 --timeout=5s

# DNS / endpoint
nslookup fm-data-store.svc.cluster.local
kubectl get endpoints fm-data-store -n <ns>

# Cert in use
fmctl adapter cert --target=t1
openssl x509 -in /var/run/secrets/fm/t1.crt -noout -dates -subject -issuer

# T1 cluster health (if you have access)
kubectl get pods -l app=fm-data-store -n <ns>
```

```promql
fm_adapter_request_errors_total{target="t1"} / fm_adapter_request_total{target="t1"}
fm_adapter_connection_state{target="t1"}
```

## 4. Remediation

**Goal:** Don't make things worse. FM is *designed* to wait quietly on T1. Don't force false progress.

1. **Check infra page first.** If T1 is on the platform incident list, just track it and ensure FM hasn't accidentally degraded further.

2. **If certs are the cause** (Cause #2):
   `fmctl adapter cert-rotate --target=t1 --force`
   This pulls a fresh SVID via SPIRE. *Rollback:* none — old cert is already rejected.

3. **If leader-election is suspected** (Cause #3): wait 60 s. If still failing, it's not leader-election.

4. **If genuinely partitioned** (Cause #1):
   - FM will continue running with cached registry data. Existing programmed state is preserved.
   - Do NOT restart the FM pod to "try to reconnect" — restart loses warm registries; on reconnect FM will rehydrate anyway.
   - Coordinate with the T1 owner. Do not failover FM to a different T1 cluster without explicit cross-cluster runbook (separate doc, not in this set).

5. **Watermark sanity** after T1 returns: confirm no ADP_008 (watermark regression) — that's a different runbook and worse.

## 5. Rollback

- Cert rotation is one-way; if the new cert is itself bad, page security-on-call.
- Do NOT bypass adapter validation (`--insecure-skip-verify` is rejected by CI lint; if you see this flag, someone smuggled it in — page security).

## 6. Escalate When

- T1 unreachable > 5 min AND no infra incident open → page T1 owner.
- ADP_005 returns to OK then immediately re-fires with the same error — flaky network or split-cluster T1; page network on-call.
- Multiple adapter targets unreachable simultaneously (ADP_005 + ADP_006) — likely cluster-wide event; switch to a cluster-failover runbook (separate doc).

## 7. References

- `adapter-protocol-design.md` §Connection state machine
- `security-design.md` §4 — mTLS rotation
- `storage-architecture.md` T1 fabric
- `error-handling-design.md` ADP_005 entry
