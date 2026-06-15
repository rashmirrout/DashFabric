# Weaver: Security Reference

> **Read Time:** 20 minutes  
> **Previous:** [35-observability.md](./35-observability.md) | **Next:** [37-rate-limiting.md](./37-rate-limiting.md)

---

## Authentication Methods

**Bearer Token:**
```yaml
authentication:
  method: "bearer_token"
  config:
    header_name: "Authorization"
    scheme: "Bearer"
```

**JWT:**
```yaml
authentication:
  method: "jwt"
  config:
    issuer: "https://auth.example.com"
    public_key_url: "https://auth.example.com/.well-known/jwks.json"
```

**API Key:**
```yaml
authentication:
  method: "api_key"
  config:
    header_name: "X-API-Key"
    key_file: "/etc/weaver/keys.txt"
```

**mTLS:**
```yaml
authentication:
  method: "mtls"
  config:
    ca_cert_path: "/etc/weaver/ca.crt"
```

---

## Authorization (RBAC)

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
```

---

## TLS Encryption

```yaml
tls:
  enabled: true
  server:
    cert_path: "/etc/weaver/certs/server.crt"
    key_path: "/etc/weaver/certs/server.key"
    min_version: "1.2"
```

---

**Navigation:**
- [← Previous](./35-observability.md)
- [Index](../INDEX.md)
- [Next →](./37-rate-limiting.md)
