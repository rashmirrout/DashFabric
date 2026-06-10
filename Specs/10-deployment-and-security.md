# 10 — Deployment & Security

> This document specifies the production Kubernetes topology, networking,
> identity and authorization, secret management, multi-tenancy, and the
> packaging/release pipeline.

---

## 1. Deployment Topology

### 1.1 Layout per Region

```
┌───────────────────────────────────────────────────────────────────────┐
│                       Region: westus2                                 │
│  ┌──────────────────────────────────────────────────────────────┐     │
│  │  Kubernetes Cluster: dashfabric-westus2-prod                 │     │
│  │  3 control-plane nodes (HA managed control plane)            │     │
│  │  N worker node pools across 3 AZs                            │     │
│  │  ┌─────────────────────────────────────────────────────┐     │     │
│  │  │  Namespace: dashfabric-system                        │     │     │
│  │  │  • PM Deployment             (3 replicas)            │     │     │
│  │  │  • NBG Deployment            (3 replicas, HPA)       │     │     │
│  │  │  • ShardSet StatefulSets     (×N, 3 pods each)       │     │     │
│  │  │  • Admin Gateway             (2 replicas)            │     │     │
│  │  │  • Webhook validators        (2 replicas)            │     │     │
│  │  ├─────────────────────────────────────────────────────┤     │     │
│  │  │  Namespace: dashfabric-data                          │     │     │
│  │  │  • etcd StatefulSet          (5 replicas)            │     │     │
│  │  │  • SPIRE Server              (3 replicas)            │     │     │
│  │  ├─────────────────────────────────────────────────────┤     │     │
│  │  │  Namespace: dashfabric-obs                           │     │     │
│  │  │  • Prometheus / VictoriaMetrics                       │     │     │
│  │  │  • Loki                                              │     │     │
│  │  │  • Tempo                                             │     │     │
│  │  │  • Grafana                                           │     │     │
│  │  │  • OTel Collector (gateway)                          │     │     │
│  │  └─────────────────────────────────────────────────────┘     │     │
│  └──────────────────────────────────────────────────────────────┘     │
│                                                                       │
│  Object storage: S3-compatible (for bulk route/mapping blobs)         │
│  Vault: HashiCorp Vault (or cloud-native KMS) for secrets             │
└───────────────────────────────────────────────────────────────────────┘
```

### 1.2 ShardSet Manifest (Sketch)

```yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: shard-0007
  namespace: dashfabric-system
spec:
  serviceName: shard-0007         # headless
  replicas: 3
  updateStrategy:
    type: OnDelete                # manual ordering for ISSU
  podManagementPolicy: Parallel   # all 3 spin up together on creation
  selector:
    matchLabels: { app: dashfabric, role: shard, shard: "0007" }
  template:
    metadata:
      labels: { app: dashfabric, role: shard, shard: "0007" }
      annotations:
        prometheus.io/scrape: "true"
        prometheus.io/port: "8081"
    spec:
      affinity:
        podAntiAffinity:
          requiredDuringSchedulingIgnoredDuringExecution:
            - labelSelector:
                matchLabels: { shard: "0007" }
              topologyKey: topology.kubernetes.io/zone
      topologySpreadConstraints:
        - maxSkew: 1
          topologyKey: topology.kubernetes.io/zone
          whenUnsatisfiable: DoNotSchedule
          labelSelector: { matchLabels: { shard: "0007" } }
      serviceAccountName: dashfabric-shard
      securityContext:
        runAsNonRoot: true
        runAsUser: 65532
        fsGroup: 65532
        seccompProfile: { type: RuntimeDefault }
      containers:
        - name: shard
          image: registry.example.com/dashfabric/shard:v1.3.0
          args:
            - --shard-id=0007
            - --region=westus2
            - --etcd-endpoints=etcd.dashfabric-data.svc:2379
          ports:
            - { name: grpc, containerPort: 8443 }
            - { name: metrics, containerPort: 8081 }
            - { name: admin, containerPort: 8083 }
          env:
            - name: POD_NAME
              valueFrom: { fieldRef: { fieldPath: metadata.name } }
            - name: POD_NAMESPACE
              valueFrom: { fieldRef: { fieldPath: metadata.namespace } }
            - name: GOMEMLIMIT
              value: "11000MiB"
          resources:
            requests: { cpu: 4, memory: 8Gi }
            limits:   { cpu: 8, memory: 12Gi }
          volumeMounts:
            - { name: wal,   mountPath: /var/lib/dashfabric }
            - { name: certs, mountPath: /etc/dashfabric/certs, readOnly: true }
          readinessProbe:
            httpGet: { path: /readyz, port: 8080 }
            periodSeconds: 5
          livenessProbe:
            httpGet: { path: /healthz, port: 8080 }
            periodSeconds: 10
          lifecycle:
            preStop:
              exec: { command: ["/dashfabric", "drain", "--timeout=5s"] }
      volumes:
        - name: certs
          csi:
            driver: secrets-store.csi.k8s.io
            readOnly: true
            volumeAttributes:
              secretProviderClass: dashfabric-shard-certs
  volumeClaimTemplates:
    - metadata: { name: wal }
      spec:
        accessModes: [ReadWriteOnce]
        storageClassName: nvme-ssd
        resources: { requests: { storage: 50Gi } }
```

### 1.3 Headless Service (per ShardSet)
```yaml
apiVersion: v1
kind: Service
metadata:
  name: shard-0007
spec:
  clusterIP: None
  selector: { shard: "0007" }
  ports:
    - { name: grpc, port: 8443 }
```

Pods resolve as `shard-0007-0.shard-0007.dashfabric-system.svc.cluster.local`.

### 1.4 External Exposure
- NBG exposed via a **regional LoadBalancer** (anycast in DCs with anycast).
- ShardSets reachable to devices via per-shard `LoadBalancer` services or
  via an `Ingress` with hostname prefix (e.g. `s0007.shards.westus2.dashfabric.example`).

---

## 2. Networking

### 2.1 Cluster Networking
- CNI: Cilium with eBPF (kube-proxy replacement) for performance and visibility.
- Pod-to-pod and pod-to-service: standard.
- Egress to etcd: via headless Service (DNS round-robin).
- Egress to DPU fleet: dedicated VLANs reachable over the underlay; mTLS over
  TCP/8443.

### 2.2 Network Policies
- **Default deny** ingress/egress in `dashfabric-system`.
- Allow lists per workload:
  - NBG → etcd, SPIRE, observability.
  - Shard → etcd, SPIRE, DPU fleet (egress to defined CIDRs).
  - PM → etcd, K8s API.
  - Admin Gateway → all shards' admin port.

### 2.3 Bandwidth Considerations
- During a bulk mapping push to a DPU, transient bandwidth from shard to
  device can spike to **hundreds of MB**. Reserve at least **10 Gb/s** between
  shard nodes and the DPU fabric (typical DC underlay handles this trivially).

---

## 3. Identity & Authentication

### 3.1 Identities
| Subject | Identity type | Issued by |
|---|---|---|
| Device (DPU) | SPIFFE SVID over mTLS | SPIRE Server (TPM attestation) |
| DashFabric pod | SPIFFE SVID | SPIRE Server (K8s SAT attestation) |
| Operator (human) | OIDC token (corp SSO) → K8s RBAC | Corp IdP + dex |
| Service-to-service (intra-cluster) | mTLS via Cilium service mesh or sidecar | SPIRE |

### 3.2 Cert Rotation
- DPU SVIDs: 1 h TTL.
- Pod SVIDs: 24 h TTL.
- Trust bundle (root CA) rotation: blue/green with overlap window of 30 days.

### 3.3 Authorization
- **Devices** can only `Register/Unregister/Heartbeat` for their own
  `host_id` (SAN match enforced).
- **Pods** access etcd via per-role RBAC (NBG read-only on most paths, shard
  read on `/config`, write on `/state`, PM writes on `/shardmap`).
- **Operators** authorize via K8s RBAC roles consumed by the Admin Gateway:
  - `dashfabric:viewer` (read-only dashboards, `dfctl ... list/get`).
  - `dashfabric:operator` (device actions, pause/resume).
  - `dashfabric:sre-senior` (shard split/merge, drain).
  - `dashfabric:admin` (region freeze, schema migrations).

---

## 4. Secret Management

| Secret | Source | Path on disk |
|---|---|---|
| Pod TLS keypairs | SPIRE Workload API | `/etc/dashfabric/certs/` (CSI mount) |
| etcd client certs | SPIRE-issued, scoped via etcd RBAC roles | same as above |
| Vault tokens (if used) | Vault Agent injector | `/var/run/secrets/vaultproject.io/` |
| Operator KMS keys | Cloud KMS via CSI | not on disk; calls signed |

**No plaintext secrets in K8s Secrets, ConfigMaps, env vars, or image layers.**

---

## 5. Multi-Tenancy

### 5.1 Tenancy Levels
- **Device-level tenancy** is enforced by upstream control plane (which
  tenant owns which ENI). DashFabric simply records and propagates the
  `tenant_id` label.
- **DashFabric service tenancy** (who can operate DashFabric) is enforced
  by K8s RBAC.

### 5.2 Per-Tenant Quotas
Defined in `/policy/v1/tenants/<TenantID>`:

```yaml
TenantQuota:
  max_enis: 50000
  max_event_rate_per_sec: 10000
  max_drift_alerts_per_hour: 1000
```

Enforced at the dispatcher (event rate limiter) and at the PM (admission
when assigning hosts).

### 5.3 Per-Tenant Visibility
- Logs and metrics are labelled with `tenant_id`.
- Dashboards filter by tenant.
- A read-only "tenant ops" view in `dfctl` shows only that tenant's hosts.

### 5.4 Isolation Guarantees
- Mailbox QoS prevents one tenant's storm from starving another's
  (per-tenant token buckets at the dispatcher).
- No tenant can see another tenant's intent or state via any DashFabric
  API.

---

## 6. Image and Release Pipeline

### 6.1 Image Build
- Multi-stage Dockerfile, scratch base, static binary.
- SBOM generated via Syft; published alongside image.
- Image signed via Cosign (Sigstore); admission controller rejects unsigned
  images.
- Vulnerability scan via Grype on every PR + nightly on `main`.

### 6.2 Versioning
- SemVer: `MAJOR.MINOR.PATCH`.
- Backward-compatibility window: N-2 minor releases.
- `dashfabric --version` reports: `version, gitSha, buildTime, schemaVersion`.

### 6.3 Release Channels
| Channel | Purpose |
|---|---|
| `dev` | Per-PR builds; staging clusters only |
| `rc` | Release candidates; canary regions |
| `stable` | Production-wide |
| `lts` | Long-term support tags (annual) |

### 6.4 Promotion
- Automated promotion `rc → stable` after 7 days canary with healthy SLOs.
- Manual approval gate by release manager.

### 6.5 Rollback
- StatefulSet OnDelete strategy → operator deletes pod; previous image
  rehydrates (the prior tag remains pullable).
- For PVC schema-incompatible cases: `dfctl wal repair` rebuilds from etcd
  on the older binary.

---

## 7. Disaster Recovery

| Loss | RPO | RTO | Procedure |
|---|---|---|---|
| Single pod | 0 | < 5 s | HA failover (auto) |
| Single ShardSet (all 3 pods) | 0 | ≤ 30 s | PM provisions fresh ShardSet |
| etcd cluster | 0 for steady state; minutes of fresh-intent gap | etcd quorum recovery time | etcd snapshot restore from S3 |
| Entire K8s cluster | 0 for devices (they're independent); region offline for new intent | ≤ 1 h | Restore K8s + DashFabric from IaC; etcd restore from snapshot |
| Region (physical) | Loss of all programming continuity in region | DC rebuild time | Devices reregister when DC returns |

Etcd snapshots: every 30 minutes to S3, retained 30 days.

---

## 8. Compliance & Audit

- All operator actions audit-logged (Loki + 90 day retention + WORM in
  cold storage).
- Certificate issuance audited via SPIRE event log.
- PII / customer data exposure points are documented; tenant addresses are
  considered sensitive and redacted in non-production exports.
- SOC2 controls map per `compliance/SOC2-control-map.md` (TBD).

---

## 9. Capacity Planning

### 9.1 Per-Region Baseline (10k devices)
| Component | Replicas | Sum CPU | Sum Mem | Sum Disk |
|---|---|---|---|---|
| NBG | 3 | 3 | 3 Gi | — |
| Shards (5 × 3) | 15 | 60 | 120 Gi | 750 Gi |
| PM | 3 | 1 | 1 Gi | — |
| etcd | 5 | 50 | 160 Gi | 5 Ti |
| SPIRE | 3 | 3 | 6 Gi | 30 Gi |
| Observability | varies | 30 | 100 Gi | 5 Ti (logs/metrics retention) |

### 9.2 Scaling Triggers
- Devices > 80 % of shard cap × N → add shards.
- NBG CPU > 60 % p95 for 10 m → HPA adds NBG replicas.
- etcd write latency > 50 ms p99 → consider sharding etcd by region sub-keys.

---

## 10. Threat Model (Sketch)

| Threat | Vector | Mitigation |
|---|---|---|
| Compromised DPU pushes false data | Mutual auth | TPM attestation + per-DPU SVID; data plane is upstream-driven, not DPU-asserted |
| Compromised pod alters intent | Internal | etcd RBAC: shards have **read-only** access to `/config/*` |
| Replay of registration | Network | bootstrap_nonce + Bloom dedup |
| MITM on gNMI | Network | mTLS with cert pinning |
| Operator mistake (mass delete) | Human | Audit + RBAC tiers + destructive command confirmations |
| Stale Primary writes after lease loss | Internal race | Fence tokens (when supported); Lease check before each RPC; failed RPCs do not corrupt because Apply is idempotent |
| Upstream poisons intent | Supply chain | Schema validation, anomaly-detection alerts on sudden churn |
| Disk side-channel attacks | Physical | Encrypted PVCs (cloud-default KMS or LUKS) |
| Log exfiltration | Internal | Loki tenant scopes; PII redaction pipeline |

A formal threat model document (`security/threat-model.md`) is a follow-up
deliverable in `12-roadmap-and-open-questions.md`.

---

## 11. Open Questions

| ID | Question | Default |
|---|---|---|
| OQ-1001 | Self-host etcd or use a managed service? | **Self-host** for control + isolation; managed K8s control plane is fine. |
| OQ-1002 | Use Cilium service mesh for intra-cluster mTLS, or plain SPIRE+gRPC? | **SPIRE+gRPC** for v1 (simpler); evaluate mesh in v2. |
| OQ-1003 | Should the device side run a sidecar agent or build into DASH container? | **Sidecar for v1**, upstream PR to merge into DASH container in v2. |
| OQ-1004 | Backup strategy for object storage (route/mapping blobs)? | **Object-storage native versioning + cross-region replication.** |
| OQ-1005 | Should we publish a Helm chart or a Kustomize overlay? | **Both**; Helm for installation, Kustomize for env overrides. |
