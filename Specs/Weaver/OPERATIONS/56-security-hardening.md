# Weaver: Security Hardening

> **Read Time:** 20 minutes  
> **Previous:** [55-performance-tuning.md](./55-performance-tuning.md) | **Next:** [57-version-matrix.md](./57-version-matrix.md)

---

## Pre-Production Security Checklist

Before deploying to production, verify all items below.

---

## Network Security

### ☐ TLS Encryption Enabled

```yaml
tls:
  enabled: true
  server:
    cert_path: "/etc/weaver/tls/server.crt"
    key_path: "/etc/weaver/tls/server.key"
    min_version: "1.2"  # or "1.3" if available
```

**Generate self-signed cert (for testing):**
```bash
openssl req -x509 -newkey rsa:4096 -keyout server.key -out server.crt -days 365 -nodes
```

**Generate signed cert (production):**
- Use your certificate authority or Let's Encrypt
- Ensure cert is not self-signed in production

### ☐ mTLS Enabled (Client Authentication)

```yaml
tls:
  server:
    client_auth: true
    client_ca_path: "/etc/weaver/tls/client-ca.crt"
```

Requires clients to present valid certificate.

### ☐ Network Policies Applied

```yaml
# Kubernetes NetworkPolicy: deny all ingress by default
ingress:
  - from:
    - podSelector:
        matchLabels:
          allowed: "true"
    ports:
    - protocol: TCP
      port: 5051

# Egress: allow to etcd, Jaeger, backend replicas
egress:
  - to:
    - podSelector:
        matchLabels:
          app: etcd
    ports:
    - protocol: TCP
      port: 2379
```

### ☐ Firewall Rules

- Only open ports: 5051 (gRPC), 8080 (debug), 9090 (metrics)
- Restrict source IPs if possible
- Block all other traffic

### ☐ Private Network

Weaver should not be accessible from internet:
- Run in private VPC / subnet
- Use NAT gateway for outbound traffic
- Use VPN / bastion for admin access

---

## Authentication & Authorization

### ☐ Authentication Enabled

Choose one:

**Option 1: Bearer Token (Simple)**
```yaml
authentication:
  method: "bearer_token"
  config:
    header_name: "Authorization"
    scheme: "Bearer"
    tokens:
      - "secret-token-1"
      - "secret-token-2"
```

**Option 2: API Key**
```yaml
authentication:
  method: "api_key"
  config:
    header_name: "X-API-Key"
    keys:
      - "prod-api-key-1"
      - "prod-api-key-2"
```

**Option 3: JWT (Recommended for production)**
```yaml
authentication:
  method: "jwt"
  config:
    issuer: "https://auth.example.com"
    public_key_url: "https://auth.example.com/.well-known/jwks.json"
```

**Option 4: mTLS (Strongest)**
```yaml
authentication:
  method: "mtls"
  config:
    ca_cert_path: "/etc/weaver/tls/client-ca.crt"
```

### ☐ Authorization Enabled

```yaml
authorization:
  enabled: true
  rbac:
    roles:
      admin:
        permissions: ["read", "write", "register", "admin"]
      operator:
        permissions: ["read", "write"]
      viewer:
        permissions: ["read"]
    
    users:
      admin-user:
        role: "admin"
      ops-team:
        role: "operator"
      monitoring:
        role: "viewer"
```

### ☐ Audit Logging Enabled

```yaml
observability:
  audit_log:
    enabled: true
    log_all_requests: true
    path: "/var/log/weaver/audit.log"
    format: "json"
```

---

## Data Protection

### ☐ Secrets Not in Config

Never store secrets in weaver-config.yaml:

❌ **WRONG:**
```yaml
authentication:
  config:
    api_key: "hardcoded-secret"
```

✅ **RIGHT:**
```yaml
authentication:
  config:
    api_key_file: "/etc/weaver/secrets/api-key"
```

Or use environment variables:
```yaml
authentication:
  config:
    api_key: "${API_KEY}"  # Read from env var
```

### ☐ Secrets Manager Configured

```bash
# Kubernetes Secrets
kubectl create secret generic weaver-secrets \
  --from-literal=api-key=secret123 \
  --from-file=tls-cert=./server.crt \
  --from-file=tls-key=./server.key \
  -n weaver

# Docker secrets
docker secret create api-key api-key.txt
```

### ☐ Sensitive Data Masked in Logs

```yaml
observability:
  logging:
    mask_sensitive_headers:
      - "Authorization"
      - "X-API-Key"
      - "Cookie"
```

### ☐ Encryption at Rest (if needed)

For etcd backend:
```bash
# Enable encryption
ETCD_ENCRYPTION_ENABLED=true
```

---

## Container Security

### ☐ Run as Non-Root User

```yaml
# Kubernetes
securityContext:
  runAsNonRoot: true
  runAsUser: 1000
  fsGroup: 1000

# Docker
docker run --user 1000:1000 weaver
```

### ☐ Read-Only Root Filesystem

```yaml
# Kubernetes
securityContext:
  readOnlyRootFilesystem: true

# Docker
docker run --read-only weaver
```

### ☐ Drop Unnecessary Capabilities

```yaml
# Kubernetes
securityContext:
  capabilities:
    drop:
      - ALL
    add:
      - NET_BIND_SERVICE  # Only if needed
```

### ☐ Resource Limits Set

```yaml
# Kubernetes
resources:
  limits:
    cpu: "2"
    memory: "1Gi"
  requests:
    cpu: "500m"
    memory: "256Mi"

# Docker
docker run --memory 1g --cpus 2 weaver
```

### ☐ No Privileged Mode

```yaml
# Kubernetes
securityContext:
  privileged: false

# Docker
docker run  # NO --privileged flag
```

---

## Image Security

### ☐ Use Minimal Base Image

```dockerfile
FROM alpine:3.18  # Minimal attack surface
# NOT: FROM ubuntu:22.04
```

### ☐ Regular Image Scanning

```bash
# Scan for vulnerabilities
trivy image myregistry/weaver:v1.0.0

# Check CVEs
docker scout cves myregistry/weaver:v1.0.0
```

### ☐ Image Signing (Optional)

```bash
# Sign image
cosign sign myregistry/weaver:v1.0.0

# Verify signature
cosign verify myregistry/weaver:v1.0.0
```

### ☐ Image Registry Security

- Use private registry (not Docker Hub public)
- Enable authentication to registry
- Enable image scanning on push
- Enforce signed images in policy

---

## Kubernetes-Specific

### ☐ RBAC Configured

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: weaver
rules:
  # Minimal permissions only
  - apiGroups: [""]
    resources: ["pods"]
    verbs: ["get", "list"]  # NOT "create", "delete", "patch"
```

### ☐ Pod Security Policy

```yaml
apiVersion: policy/v1beta1
kind: PodSecurityPolicy
metadata:
  name: weaver-psp
spec:
  privileged: false
  runAsUser:
    rule: 'MustRunAsNonRoot'
  fsGroup:
    rule: 'MustRunAs'
    ranges:
      - min: 1000
        max: 1000
  readOnlyRootFilesystem: true
  capabilities:
    drop:
      - ALL
```

### ☐ Network Policies Enforced

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: weaver-deny-all
spec:
  podSelector:
    matchLabels:
      app: weaver
  policyTypes:
  - Ingress
  ingress:
  - from:
    - namespaceSelector:
        matchLabels:
          name: trusted
    ports:
    - protocol: TCP
      port: 5051
```

### ☐ RBAC for ServiceAccount

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: weaver
rules:
  - apiGroups: [""]
    resources: ["configmaps"]
    verbs: ["get", "watch"]  # Can read config, not modify
```

---

## Monitoring & Auditing

### ☐ Audit Logging Enabled

```yaml
observability:
  audit_log:
    enabled: true
    events_to_log:
      - "authentication_failure"
      - "authorization_failure"
      - "config_change"
      - "secret_access"
```

### ☐ Security Alerts Configured

```yaml
# AlertManager
- alert: AuthenticationFailure
  expr: increase(weaver_auth_failures_total[5m]) > 10
  for: 5m
  annotations:
    severity: warning

- alert: UnauthorizedAccess
  expr: increase(weaver_authz_denied_total[5m]) > 5
  for: 5m
  annotations:
    severity: warning
```

### ☐ Security Events Monitored

- Authentication failures (failed logins)
- Authorization denials (permission denied)
- Privilege escalations
- Configuration changes
- Secret access

---

## Deployment Verification

### ☐ Run Security Validation

```bash
# Check TLS
openssl s_client -connect localhost:5051

# Check authentication required
curl -v http://localhost:5051/  # Should fail
curl -H "Authorization: Bearer token" http://localhost:5051/  # Should succeed

# Check authorization
curl -H "Authorization: Bearer viewer-token" http://localhost:8080/admin  # Should fail
curl -H "Authorization: Bearer admin-token" http://localhost:8080/admin  # Should succeed
```

### ☐ Run Pod Security Standards Check

```bash
# Kubernetes
kubectl label namespace weaver pod-security.kubernetes.io/enforce=restricted
```

### ☐ Certificate Validation

```bash
# Check cert expiration
openssl x509 -in server.crt -noout -dates

# Check cert details
openssl x509 -in server.crt -noout -text
```

---

## Incident Response

### If Compromised

1. **Immediate:**
   - Revoke all tokens/keys
   - Restart Weaver with new secrets
   - Check logs for suspicious activity

2. **Investigation:**
   - Review audit logs
   - Check for lateral movement
   - Identify compromised systems

3. **Remediation:**
   - Patch vulnerability
   - Rotate all credentials
   - Deploy patched version
   - Monitor for re-compromise

---

## Security Best Practices Summary

| Practice | Level | Effort |
|----------|-------|--------|
| TLS encryption | ✅ Required | Low |
| Authentication | ✅ Required | Low |
| Authorization | ✅ Required | Medium |
| mTLS | 🟡 Recommended | Medium |
| Audit logging | 🟡 Recommended | Medium |
| Secrets manager | 🟡 Recommended | Medium |
| Network policies | 🟡 Recommended | Low |
| Image scanning | 🟡 Recommended | Low |
| RBAC (K8s) | 🟡 Recommended | Low |

---

**Navigation:**
- [← Previous](./55-performance-tuning.md)
- [Index](../INDEX.md)
- [Next →](./57-version-matrix.md)
