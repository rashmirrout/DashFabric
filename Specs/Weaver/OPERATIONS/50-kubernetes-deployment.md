# Weaver: Kubernetes Deployment Guide

> **Read Time:** 30 minutes  
> **Previous:** [../GUIDES/64-migration-guide.md](../GUIDES/64-migration-guide.md) | **Next:** [51-docker-deployment.md](./51-docker-deployment.md)

---

## Complete Kubernetes Deployment

This guide provides production-ready Kubernetes manifests and configurations.

---

## Prerequisites

- Kubernetes 1.20+ cluster
- kubectl configured
- etcd (DashFabric T2) running and accessible
- Prometheus (optional, for metrics collection)

---

## Step 1: Create Namespace

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: weaver
  labels:
    name: weaver
```

Apply:
```bash
kubectl apply -f namespace.yaml
```

---

## Step 2: Create ConfigMap

**File: weaver-configmap.yaml**

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: weaver-config
  namespace: weaver
data:
  config.yaml: |
    gateway:
      name: "weaver-prod"
      description: "Production Weaver gateway"
    
    listeners:
      grpc:
        port: 5051
        max_connections: 10000
    
    discovery:
      method: "etcd_fabric"
      config:
        endpoints:
          - "etcd-0.etcd-headless.default.svc.cluster.local:2379"
          - "etcd-1.etcd-headless.default.svc.cluster.local:2379"
          - "etcd-2.etcd-headless.default.svc.cluster.local:2379"
        dial_timeout: "5s"
        cache_ttl: "30s"
    
    health:
      type: "http"
      interval: "5s"
      timeout: "2s"
      unhealthy_threshold: 3
      healthy_threshold: 2
    
    load_balancers:
      default:
        strategy: "least_connections"
    
    reliability:
      timeout:
        global: "30s"
        per_replica: "25s"
        connection: "5s"
      circuit_breaker:
        enabled: true
        failure_threshold: 5
        success_threshold: 2
        timeout: "60s"
      retry:
        enabled: true
        max_attempts: 3
        backoff:
          base: "100ms"
          multiplier: 2
          max: "5s"
    
    observability:
      metrics:
        enabled: true
        namespace: "fm_gw"
        port: 9090
      tracing:
        enabled: true
        provider: "jaeger"
        sample_rate: 0.1
        config:
          endpoint: "http://jaeger-agent.observability.svc.cluster.local:6831"
      logging:
        enabled: true
        level: "INFO"
        format: "json"
        async: true
    
    authentication:
      method: "api_key"
      config:
        header_name: "X-API-Key"
        keys:
          - "prod-key-12345"
    
    tls:
      enabled: true
      server:
        cert_path: "/etc/weaver/tls/server.crt"
        key_path: "/etc/weaver/tls/server.key"
        min_version: "1.2"
```

Apply:
```bash
kubectl apply -f weaver-configmap.yaml
```

---

## Step 3: Create TLS Secret

```bash
# Generate self-signed cert (for testing)
openssl req -x509 -newkey rsa:4096 -keyout key.pem -out cert.pem -days 365 -nodes

# Create secret
kubectl create secret tls weaver-tls \
  --cert=cert.pem \
  --key=key.pem \
  -n weaver

# Or from existing cert:
kubectl create secret tls weaver-tls \
  --cert=/path/to/server.crt \
  --key=/path/to/server.key \
  -n weaver
```

Verify:
```bash
kubectl get secret weaver-tls -n weaver
```

---

## Step 4: Create Deployment

**File: weaver-deployment.yaml**

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: weaver
  namespace: weaver
  labels:
    app: weaver
    version: v1
spec:
  replicas: 3
  strategy:
    type: RollingUpdate
    rollingUpdate:
      maxSurge: 1
      maxUnavailable: 0  # Zero downtime
  selector:
    matchLabels:
      app: weaver
  template:
    metadata:
      labels:
        app: weaver
      annotations:
        prometheus.io/scrape: "true"
        prometheus.io/port: "9090"
        prometheus.io/path: "/metrics"
    spec:
      serviceAccountName: weaver
      securityContext:
        runAsNonRoot: true
        runAsUser: 1000
        fsGroup: 1000
      
      containers:
      - name: weaver
        image: registry/weaver:v1.0.0  # Update to your image
        imagePullPolicy: IfNotPresent
        
        ports:
        - name: grpc
          containerPort: 5051
          protocol: TCP
        - name: debug
          containerPort: 8080
          protocol: TCP
        - name: metrics
          containerPort: 9090
          protocol: TCP
        
        env:
        - name: POD_NAME
          valueFrom:
            fieldRef:
              fieldPath: metadata.name
        - name: POD_NAMESPACE
          valueFrom:
            fieldRef:
              fieldPath: metadata.namespace
        - name: POD_IP
          valueFrom:
            fieldRef:
              fieldPath: status.podIP
        
        resources:
          requests:
            cpu: 500m
            memory: 256Mi
          limits:
            cpu: 2000m
            memory: 1Gi
        
        livenessProbe:
          httpGet:
            path: /debug/replicas
            port: debug
          initialDelaySeconds: 30
          periodSeconds: 10
          timeoutSeconds: 5
          failureThreshold: 3
        
        readinessProbe:
          httpGet:
            path: /debug/replicas
            port: debug
          initialDelaySeconds: 10
          periodSeconds: 5
          timeoutSeconds: 3
          failureThreshold: 2
        
        volumeMounts:
        - name: config
          mountPath: /etc/weaver
          readOnly: true
        - name: tls
          mountPath: /etc/weaver/tls
          readOnly: true
        
        securityContext:
          allowPrivilegeEscalation: false
          readOnlyRootFilesystem: true
          capabilities:
            drop:
            - ALL
      
      volumes:
      - name: config
        configMap:
          name: weaver-config
      - name: tls
        secret:
          secretName: weaver-tls
      
      affinity:
        podAntiAffinity:
          preferredDuringSchedulingIgnoredDuringExecution:
          - weight: 100
            podAffinityTerm:
              labelSelector:
                matchExpressions:
                - key: app
                  operator: In
                  values:
                  - weaver
              topologyKey: kubernetes.io/hostname
      
      terminationGracePeriodSeconds: 30
```

Apply:
```bash
kubectl apply -f weaver-deployment.yaml

# Verify rollout
kubectl rollout status deployment/weaver -n weaver
```

---

## Step 5: Create Service

**File: weaver-service.yaml**

```yaml
apiVersion: v1
kind: Service
metadata:
  name: weaver
  namespace: weaver
  labels:
    app: weaver
spec:
  type: LoadBalancer  # Or NodePort for internal cluster
  selector:
    app: weaver
  ports:
  - name: grpc
    port: 5051
    targetPort: 5051
    protocol: TCP
  - name: debug
    port: 8080
    targetPort: 8080
    protocol: TCP
  - name: metrics
    port: 9090
    targetPort: 9090
    protocol: TCP
  sessionAffinity: None

---
apiVersion: v1
kind: Service
metadata:
  name: weaver-headless
  namespace: weaver
  labels:
    app: weaver
spec:
  clusterIP: None  # Headless service for direct pod access
  selector:
    app: weaver
  ports:
  - name: grpc
    port: 5051
    targetPort: 5051
    protocol: TCP
```

Apply:
```bash
kubectl apply -f weaver-service.yaml

# Get LoadBalancer IP
kubectl get service weaver -n weaver
```

---

## Step 6: Create RBAC (ServiceAccount, Role, RoleBinding)

**File: weaver-rbac.yaml**

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: weaver
  namespace: weaver

---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: weaver
rules:
# No special permissions needed; Weaver uses etcd for discovery
# If using Kubernetes discovery, would need:
# - apiGroups: [""]
#   resources: ["pods", "endpoints"]
#   verbs: ["get", "list", "watch"]

---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: weaver
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: weaver
subjects:
- kind: ServiceAccount
  name: weaver
  namespace: weaver
```

Apply:
```bash
kubectl apply -f weaver-rbac.yaml
```

---

## Step 7: Create PodDisruptionBudget

**File: weaver-pdb.yaml**

```yaml
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: weaver-pdb
  namespace: weaver
spec:
  minAvailable: 1  # At least 1 pod always available
  selector:
    matchLabels:
      app: weaver
```

Apply:
```bash
kubectl apply -f weaver-pdb.yaml
```

---

## Step 8: Create NetworkPolicy (Optional)

**File: weaver-networkpolicy.yaml**

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: weaver-network-policy
  namespace: weaver
spec:
  podSelector:
    matchLabels:
      app: weaver
  policyTypes:
  - Ingress
  - Egress
  
  ingress:
  - from:
    - namespaceSelector: {}  # Allow from any namespace
    ports:
    - protocol: TCP
      port: 5051  # gRPC
    - protocol: TCP
      port: 8080  # Debug
    - protocol: TCP
      port: 9090  # Metrics
  
  egress:
  - to:
    - namespaceSelector: {}  # Allow to any namespace
    ports:
    - protocol: TCP
      port: 2379  # etcd
    - protocol: TCP
      port: 6831  # Jaeger
    - protocol: TCP
      port: 9090  # Prometheus (if pushing)
```

Apply:
```bash
kubectl apply -f weaver-networkpolicy.yaml
```

---

## Step 9: Create HorizontalPodAutoscaler

**File: weaver-hpa.yaml**

```yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: weaver-hpa
  namespace: weaver
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: weaver
  minReplicas: 3
  maxReplicas: 10
  metrics:
  - type: Resource
    resource:
      name: cpu
      target:
        type: Utilization
        averageUtilization: 70
  - type: Resource
    resource:
      name: memory
      target:
        type: Utilization
        averageUtilization: 80
  behavior:
    scaleDown:
      stabilizationWindowSeconds: 300
      policies:
      - type: Percent
        value: 50
        periodSeconds: 60
    scaleUp:
      stabilizationWindowSeconds: 0
      policies:
      - type: Percent
        value: 100
        periodSeconds: 15
      - type: Pods
        value: 2
        periodSeconds: 15
      selectPolicy: Max
```

Apply:
```bash
kubectl apply -f weaver-hpa.yaml

# Monitor scaling
kubectl get hpa weaver -n weaver --watch
```

---

## Verification

**1. Check pods are running**
```bash
kubectl get pods -n weaver
# Should see 3 pods in Running status
```

**2. Check replicas discovered**
```bash
kubectl port-forward service/weaver 8080:8080 -n weaver
curl http://localhost:8080/debug/replicas
```

**3. Check metrics**
```bash
kubectl port-forward service/weaver 9090:9090 -n weaver
curl http://localhost:9090/metrics | grep fm_gw_requests_total
```

**4. Check logs**
```bash
kubectl logs -l app=weaver -n weaver --tail=50
```

**5. Run complete verification checklist**
See [../QUICK_START/13-verify-deployment.md](../QUICK_START/13-verify-deployment.md)

---

## Configuration Updates

**Update ConfigMap**
```bash
# Edit config
kubectl edit configmap weaver-config -n weaver

# Trigger rolling restart to apply
kubectl rollout restart deployment/weaver -n weaver

# Monitor rollout
kubectl rollout status deployment/weaver -n weaver
```

---

## Scaling

**Scale up to 5 replicas**
```bash
kubectl scale deployment weaver --replicas=5 -n weaver
```

**Scale down to 2 replicas**
```bash
kubectl scale deployment weaver --replicas=2 -n weaver
# Note: Respects PodDisruptionBudget (min 1)
```

---

## Troubleshooting

| Issue | Solution |
|-------|----------|
| Pods stuck in CrashLoopBackOff | Check logs: `kubectl logs <pod> -n weaver` |
| Replicas not discovered | Check etcd connectivity: `kubectl exec <pod> -- nc -zv etcd:2379` |
| High memory usage | Check if health checks have tight intervals; reduce to 10s+ |
| Connection refused | Check if ports are exposed; verify Service exists |

---

**Navigation:**
- [← Previous](../GUIDES/64-migration-guide.md)
- [Index](../INDEX.md)
- [Next →](./51-docker-deployment.md)
