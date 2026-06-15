# Weaver: Version Compatibility Matrix

> **Read Time:** 10 minutes  
> **Previous:** [56-security-hardening.md](./56-security-hardening.md) | **Next:** [../DESIGN/40-hld.md](../DESIGN/40-hld.md)

---

## Current Versions

**Latest Release:** v1.0.0 (2026-06-15)

---

## Weaver Compatibility Matrix

| Weaver | Go | Kubernetes | Docker | etcd | Consul | gRPC | Status |
|--------|----|----|--------|------|--------|------|--------|
| v1.0.0 | 1.19+ | 1.20+ | 20.10+ | 3.4+ | 1.11+ | 1.45+ | Latest |
| v0.9.0 | 1.18+ | 1.19+ | 20.00+ | 3.3+ | 1.10+ | 1.40+ | Supported |
| v0.8.0 | 1.17+ | 1.18+ | 19.03+ | 3.2+ | 1.9+ | 1.35+ | End-of-life |

---

## Support Policy

| Version | Release Date | End of Support |
|---------|--------------|----------------|
| v1.0.0 | 2026-06-15 | 2028-06-15 (24 months) |
| v0.9.0 | 2025-12-01 | 2026-12-01 (12 months) |
| v0.8.0 | 2025-06-01 | 2025-12-01 (6 months) |

**Policy:** Each version is supported for the duration above; security updates provided.

---

## Upgrade Path

```
v0.8.0 → v0.9.0 → v1.0.0

Recommendation:
  If on v0.8.0: Upgrade to v0.9.0, then v1.0.0
  If on v0.9.0: Upgrade to v1.0.0
  If on v1.0.0: Stay current
```

---

## Breaking Changes by Version

### v0.8.0 → v0.9.0

**Config Migration Required:** YES

| Field | Old | New | Migration |
|-------|-----|-----|-----------|
| discovery.type | "etcd_v3" | "etcd_fabric" | Update config |
| health.path | "url" field | "endpoint" field | Restructure object |
| load_balancer | "name" field | strategy inline | Flatten structure |

**Upgrade Steps:**
1. Review changelog
2. Update config using migration helper: `weaver --migrate-config v0.8.0 v0.9.0 --from config-old.yaml --to config-new.yaml`
3. Test new config in staging
4. Deploy new version
5. Monitor for issues

### v0.9.0 → v1.0.0

**Config Migration Required:** NO

Fully backward compatible. Existing v0.9.0 configs work as-is.

**New Features (opt-in):**
- `resource_aware` load balancing strategy (optional)
- `panic_threshold` reliability setting (optional)

---

## Feature Matrix

| Feature | v0.8.0 | v0.9.0 | v1.0.0 |
|---------|--------|--------|--------|
| Pod discovery (etcd) | ✅ | ✅ | ✅ |
| Pod discovery (Consul) | ✅ | ✅ | ✅ |
| Pod discovery (Kubernetes) | ❌ | ✅ | ✅ |
| Health checks (HTTP) | ✅ | ✅ | ✅ |
| Health checks (gRPC) | ✅ | ✅ | ✅ |
| Health checks (TCP) | ❌ | ✅ | ✅ |
| Load balancer (round-robin) | ✅ | ✅ | ✅ |
| Load balancer (least-conn) | ✅ | ✅ | ✅ |
| Load balancer (consistent-hash) | ✅ | ✅ | ✅ |
| Load balancer (resource-aware) | ❌ | ❌ | ✅ |
| Circuit breaker | ✅ | ✅ | ✅ |
| Retry with backoff | ✅ | ✅ | ✅ |
| Rate limiting (global) | ✅ | ✅ | ✅ |
| Rate limiting (per-tenant) | ❌ | ✅ | ✅ |
| Authentication (bearer) | ✅ | ✅ | ✅ |
| Authentication (JWT) | ✅ | ✅ | ✅ |
| Authentication (mTLS) | ❌ | ✅ | ✅ |
| TLS encryption | ✅ | ✅ | ✅ |
| Prometheus metrics | ✅ | ✅ | ✅ |
| Jaeger tracing | ❌ | ✅ | ✅ |

---

## Known Issues

### v1.0.0

**Issue 1: WebSocket not supported**
- Status: Won't fix in v1.x
- Workaround: Use separate WebSocket gateway
- Planned for: v2.0

**Issue 2: Large payloads (>100MB) may timeout**
- Status: Known limitation
- Workaround: Increase `reliability.timeout.global` to 120s
- Planned fix: Streaming support in v1.1

**Issue 3: Memory leak under extreme load (>1M concurrent connections)**
- Status: Under investigation
- Workaround: Restart Weaver every 7 days under extreme load
- Planned fix: v1.0.1

### v0.9.0

**Issue 1: Consul discovery doesn't work with Consul Enterprise**
- Status: Fixed in v1.0.0
- Workaround: Use Kubernetes discovery instead

**Issue 2: Circuit breaker can flap under network jitter**
- Status: Fixed in v1.0.0
- Workaround: Increase `failure_threshold` to 10

---

## Deprecation Notices

### Deprecated in v1.0.0 (Will be removed in v2.0)

- ❌ `discovery.type: "etcd_v2"` → Use `etcd_fabric`
- ❌ `health.url` field → Use `health.endpoint`
- ❌ `load_balancer.name` field → Use `load_balancers.strategy`

---

## Platform Support

### Operating Systems

| OS | Supported Versions |
|----|-------------------|
| Linux | 2.6.32+ (glibc 2.12+) |
| macOS | 10.14+ |
| Windows | Server 2016+ |
| Docker | 20.10+ |
| Kubernetes | 1.20+ |

### Architecture

| Arch | v1.0.0 |
|------|--------|
| x86_64 | ✅ |
| arm64 | ✅ |
| arm/v7 | ⚠️ (tested, not officially supported) |

---

## Dependency Versions

### Runtime Dependencies

| Dependency | v1.0.0 | Notes |
|------------|--------|-------|
| Go stdlib | 1.19+ | Built with Go 1.21 |
| gRPC | 1.45+ | Protocol buffer v3 |
| OpenTelemetry | 0.35+ | Optional; for tracing |
| Prometheus client | 1.12+ | Optional; for metrics |

### System Dependencies

| Dependency | v1.0.0 | Notes |
|------------|--------|-------|
| glibc | 2.12+ | Linux only |
| kernel | 2.6.32+ | Linux only |
| DNS resolver | libresolv | Linux only |
| CA certificates | system CA store | For TLS validation |

---

## Building from Source

### Supported Build Environments

```
Go 1.19+
OS: Linux, macOS, Windows, Docker
Architecture: x86_64, arm64

Unsupported but may work:
  Go 1.18 (untested)
  arm/v7 (untested)
```

### Build Flags

```bash
# Build Weaver binary
go build -o weaver ./cmd/weaver

# Build with version info
go build -ldflags "-X main.Version=v1.0.0" -o weaver ./cmd/weaver

# Cross-compile for arm64
GOARCH=arm64 go build -o weaver-arm64 ./cmd/weaver

# Cross-compile for Windows
GOOS=windows GOARCH=amd64 go build -o weaver.exe ./cmd/weaver
```

---

## Release Schedule

**Current Plan:**

| Version | Expected | Type |
|---------|----------|------|
| v1.0.1 | 2026-09-15 | Patch (bug fixes) |
| v1.1.0 | 2027-03-15 | Minor (new features) |
| v2.0.0 | 2027-12-01 | Major (breaking changes) |

**Releases:** Quarterly (Jan, Apr, Jul, Oct)

---

## Version History

### v1.0.0 (2026-06-15)

**New Features:**
- ✅ Kubernetes pod discovery
- ✅ TCP health checks
- ✅ Resource-aware load balancing
- ✅ Jaeger distributed tracing
- ✅ Per-tenant rate limiting
- ✅ mTLS authentication

**Breaking Changes:**
- None (backward compatible with v0.9.0)

**Security:**
- Fixed 2 critical auth vulnerabilities
- Updated dependencies for CVE fixes

### v0.9.0 (2025-12-01)

**New Features:**
- ✅ Consul pod discovery
- ✅ JWT authentication
- ✅ gRPC health checks
- ✅ Prometheus metrics

**Breaking Changes:**
- Config schema reorganized
- Migration helper provided

### v0.8.0 (2025-06-01)

**Initial Release:**
- ✅ Basic gateway functionality
- ✅ etcd discovery
- ✅ HTTP health checks
- ✅ Load balancing (round-robin, least-conn, consistent-hash)
- ✅ Circuit breaker + retry
- ✅ Rate limiting

---

**Navigation:**
- [← Previous](./56-security-hardening.md)
- [Index](../INDEX.md)
- [Next →](../DESIGN/40-hld.md)
