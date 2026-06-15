# Weaver: Kubernetes Quick Start (5 Minutes)

> **Read Time:** 5 minutes  
> **Deployment Time:** 5 minutes  
> **Audience:** DevOps, Operators  
> **Previous:** [02-architecture-overview.md](../GET_STARTED/02-architecture-overview.md) | **Next:** [11-docker-compose.md](./11-docker-compose.md)

---

## System Diagram

```
┌────────────────────────────────────────────────────────────────┐
│ KUBERNETES CLUSTER                                             │
├────────────────────────────────────────────────────────────────┤
│                                                                │
│  ┌─────────────────────────────────────────────────────────┐ │
│  │ Namespace: weaver                                       │ │
│  │                                                         │ │
│  │  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐ │ │
│  │  │  Pod 1       │  │  Pod 2       │  │  Pod 3       │ │ │
│  │  │  weaver:1.0  │  │  weaver:1.0  │  │  weaver:1.0  │ │ │
│  │  │              │  │              │  │              │ │ │
│  │  │ :5051        │  │ :5051        │  │ :5051        │ │ │
│  │  │ :8080        │  │ :8080        │  │ :8080        │ │ │
│  │  │ :9090        │  │ :9090        │  │ :9090        │ │ │
│  │  └──────────────┘  └──────────────┘  └──────────────┘ │ │
│  │         │                  │                  │        │ │
│  │         └──────────────────┼──────────────────┘        │ │
│  │                            │                           │ │
│  │  ┌───────────────────────────────────────────────────┐ │ │
│  │  │ Service: weaver (ClusterIP :5051, :8080, :9090) │ │ │
│  │  └───────────────────────────────────────────────────┘ │ │
│  │                                                         │ │
│  │  ┌───────────────────────────────────────────────────┐ │ │
│  │  │ ConfigMap: weaver-config                         │ │ │
│  │  │ (weaver-config.yaml mounted at /etc/weaver)     │ │ │
│  │  └───────────────────────────────────────────────────┘ │ │
│  │                                                         │ │
│  └─────────────────────────────────────────────────────────┘ │
│                            │                                 │
│        ┌───────────────────┼───────────────────┐            │
│        ↓                   ↓                   ↓             │
│  Backend Cluster (FM / CB / Custom)                         │
└────────────────────────────────────────────────────────────────┘
```

---

## 5-Minute Setup

### **Step 1: Verify Prerequisites** (1 min)

```bash
# Check kubectl
kubectl version --short

# Check cluster connectivity
kubectl cluster-info

# Create namespace
kubectl create namespace weaver
```

**Expected Output:**
```
Client Version: v1.29.0
Server Version: v1.29.0
Kubernetes control plane is running at https://...
```

---

### **Step 2: Create ConfigMap** (1 min)

Create the Weaver configuration:

```bash
kubectl apply -f - <<'EOF'
apiVersion: v1
kind: ConfigMap
metadata:
  name: weaver-config
  namespace: weaver
data:
  weaver-config.yaml: |
    gateway:
      name: "fm-gateway"
    
    discovery:
      type: "t2_etcd"
      config:
        endpoint: "http://etcd-t2:2379"
        key_pattern: "/dashfabric/cluster/pods/fm-*"
    
    health:
      type: "http"
      interval: 10s
      config:
        endpoint: "/api/v1/health"
    
    listeners:
      grpc:
        enabled: true
        port: 5051
      http:
        enabled: true
        port: 8080
      metrics:
        enabled: true
        port: 9090
    
    load_balancers:
      - name: "default"
        type: "least_connections"
    
    reliability:
      circuit_breaker:
        enabled: true
        failure_threshold: 5
      retry:
        enabled: true
        max_attempts: 3
        backoff_strategy: "exponential"
        initial_backoff: 10ms
      queuing:
        enabled: true
        per_replica_depth: 1000
EOF
```

---

### **Step 3: Deploy Weaver** (2 min)

Deploy the Weaver Deployment and Service:

```bash
kubectl apply -f - <<'EOF'
apiVersion: apps/v1
kind: Deployment
metadata:
  name: weaver
  namespace: weaver
spec:
  replicas: 3
  selector:
    matchLabels:
      app: weaver
  template:
    metadata:
      labels:
        app: weaver
    spec:
      containers:
      - name: weaver
        image: weaver:1.0.0
        ports:
        - containerPort: 5051
          name: grpc
        - containerPort: 8080
          name: http
        - containerPort: 9090
          name: metrics
        volumeMounts:
        - name: config
          mountPath: /etc/weaver
        resources:
          requests:
            cpu: 100m
            memory: 128Mi
          limits:
            cpu: 500m
            memory: 512Mi
        livenessProbe:
          httpGet:
            path: /debug/replicas
            port: 8080
          initialDelaySeconds: 10
          periodSeconds: 10
        readinessProbe:
          httpGet:
            path: /debug/replicas
            port: 8080
          initialDelaySeconds: 5
          periodSeconds: 5
      volumes:
      - name: config
        configMap:
          name: weaver-config
EOF
```

```bash
kubectl apply -f - <<'EOF'
apiVersion: v1
kind: Service
metadata:
  name: weaver
  namespace: weaver
spec:
  type: ClusterIP
  selector:
    app: weaver
  ports:
  - name: grpc
    port: 5051
    targetPort: 5051
  - name: http
    port: 8080
    targetPort: 8080
  - name: metrics
    port: 9090
    targetPort: 9090
EOF
```

---

### **Step 4: Verify Deployment** (1 min)

```bash
# Check pods are running
kubectl get pods -n weaver

# Check service
kubectl get svc -n weaver

# Check logs
kubectl logs -n weaver -l app=weaver --tail=10
```

**Expected Output:**
```
NAME                      READY   STATUS    RESTARTS   AGE
weaver-7d8f4c9b7d-abc12   1/1     Running   0          30s
weaver-7d8f4c9b7d-def45   1/1     Running   0          30s
weaver-7d8f4c9b7d-ghi78   1/1     Running   0          30s

NAME     TYPE        CLUSTER-IP     EXTERNAL-IP   PORT(S)
weaver   ClusterIP   10.96.123.45   <none>        5051/TCP,8080/TCP,9090/TCP

[INFO] Weaver gateway started on port 5051 (gRPC)
[INFO] Weaver HTTP listener started on port 8080
[INFO] Discovery: T2 etcd endpoint connected
```

---

## Troubleshooting

| Issue | Diagnosis | Fix |
|-------|-----------|-----|
| **Pods stuck in CrashLoopBackOff** | Check container logs | `kubectl logs -n weaver <pod>` |
| **No replicas discovered** | Check etcd connectivity | Verify etcd endpoint and key pattern in config |
| **Services not responding** | Check readiness probe | `kubectl describe pod -n weaver <pod>` |
| **ImagePullBackOff** | Check image availability | Ensure `weaver:1.0.0` image exists in your registry |

---

## Common Tasks

### **Update Configuration (Zero-Restart)**

```bash
kubectl edit configmap weaver-config -n weaver

# Weaver will auto-reload within 1 second
# No restart needed!
```

### **Scale Weaver**

```bash
# Scale to 5 replicas
kubectl scale deployment weaver --replicas=5 -n weaver

# Check status
kubectl get pods -n weaver
```

### **View Metrics**

```bash
# Port-forward to access metrics
kubectl port-forward -n weaver svc/weaver 9090:9090 &

# Query metrics
curl http://localhost:9090/metrics | grep fm_gw_replica_status
```

### **Port-Forward for Local Testing**

```bash
# Forward gRPC
kubectl port-forward -n weaver svc/weaver 5051:5051 &

# Forward HTTP
kubectl port-forward -n weaver svc/weaver 8080:8080 &

# Now test locally
grpcurl -plaintext localhost:5051 list
curl http://localhost:8080/debug/replicas | jq .
```

---

## Next Steps

- **Verify Deployment** → [13-verify-deployment.md](./13-verify-deployment.md)
- **See Real Scenarios** → [20-fm-primary-aware.md](../SCENARIOS/20-fm-primary-aware.md)
- **Production Hardening** → [50-kubernetes-deployment.md](../OPERATIONS/50-kubernetes-deployment.md)
- **Configure Weaver** → [30-configuration-reference.md](../REFERENCE/30-configuration-reference.md)

---

**Navigation:**
- [← Previous](../GET_STARTED/02-architecture-overview.md)
- [Index](../INDEX.md)
- [Next →](./11-docker-compose.md)
