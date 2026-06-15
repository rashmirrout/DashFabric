# Weaver: Plugin Development

> **Read Time:** 20 minutes  
> **Previous:** [70-development-setup.md](./70-development-setup.md) | **Next:** [72-contributing.md](./72-contributing.md)

---

## Extending Weaver

Weaver is designed to be extensible. You can add custom:
- Pod discovery methods
- Load balancing strategies  
- Health check types
- Authentication methods

---

## Plugin Architecture

Each extensibility point follows a plugin interface:

```go
type DiscoveryPlugin interface {
  Init(config Config) error
  Watch(ctx Context) <-chan []Replica
  GetReplicas() []Replica
  Close() error
}

type LoadBalancerPlugin interface {
  Select(ctx Context, replicas []*Replica) (*Replica, error)
  UpdateLoad(replica *Replica, delta int)
  Rebalance(replicas []*Replica) error
}

type HealthCheckPlugin interface {
  Check(replica *Replica) HealthCheckResult
}

type AuthPlugin interface {
  Authenticate(request Request) (User, error)
}
```

---

## Custom Discovery Plugin

### Example: Custom YAML Registry

**Scenario:** You want to load replica list from YAML file instead of etcd.

**Implementation:**

```go
// cmd/weaver-custom-discovery/main.go
package main

import (
  "io/ioutil"
  "gopkg.in/yaml.v3"
  "github.com/dashfabric/weaver/pkg/discovery"
)

type YAMLDiscovery struct {
  filePath string
  replicas []discovery.Replica
}

func (yd *YAMLDiscovery) Init(config discovery.Config) error {
  yd.filePath = config.Get("file_path")
  return yd.reload()
}

func (yd *YAMLDiscovery) reload() error {
  data, err := ioutil.ReadFile(yd.filePath)
  if err != nil {
    return err
  }
  
  var replicasData []map[string]interface{}
  yaml.Unmarshal(data, &replicasData)
  
  var replicas []discovery.Replica
  for _, r := range replicasData {
    replicas = append(replicas, discovery.Replica{
      Name: r["name"].(string),
      Address: r["address"].(string),
    })
  }
  
  yd.replicas = replicas
  return nil
}

func (yd *YAMLDiscovery) Watch(ctx Context) <-chan []discovery.Replica {
  // Periodically reload from file
  ch := make(chan []discovery.Replica)
  go func() {
    for {
      select {
      case <-ctx.Done():
        return
      case <-time.After(10 * time.Second):
        yd.reload()
        ch <- yd.replicas
      }
    }
  }()
  return ch
}

func (yd *YAMLDiscovery) GetReplicas() []discovery.Replica {
  return yd.replicas
}

func (yd *YAMLDiscovery) Close() error {
  return nil
}
```

**Config:**
```yaml
discovery:
  method: "yaml_file"
  config:
    file_path: "/etc/weaver/replicas.yaml"
```

**Replicas file (replicas.yaml):**
```yaml
- name: "replica-1"
  address: "10.0.0.1:5051"
- name: "replica-2"
  address: "10.0.0.2:5051"
- name: "replica-3"
  address: "10.0.0.3:5051"
```

---

## Custom Load Balancer

### Example: Weighted Round-Robin

**Scenario:** Send more traffic to faster replicas (weighted).

**Implementation:**

```go
// pkg/loadbalancer/weighted.go
package loadbalancer

import "github.com/dashfabric/weaver/pkg/gateway"

type WeightedRoundRobin struct {
  counter int
  weights map[string]int
}

func (wrr *WeightedRoundRobin) Select(
  ctx Context,
  replicas []*gateway.Replica,
) (*gateway.Replica, error) {
  if len(replicas) == 0 {
    return nil, ErrNoReplicas
  }
  
  // Expand replicas based on weights
  expanded := []int{}
  for i, replica := range replicas {
    weight := wrr.weights[replica.Name]
    if weight == 0 {
      weight = 1  // default weight
    }
    for j := 0; j < weight; j++ {
      expanded = append(expanded, i)
    }
  }
  
  // Select with round-robin
  selected := expanded[wrr.counter % len(expanded)]
  wrr.counter++
  
  return replicas[selected], nil
}

func (wrr *WeightedRoundRobin) UpdateLoad(
  replica *gateway.Replica,
  delta int,
) {
  // No-op for round-robin
}

func (wrr *WeightedRoundRobin) Rebalance(
  replicas []*gateway.Replica,
) error {
  return nil
}
```

**Config:**
```yaml
load_balancers:
  default:
    strategy: "weighted_round_robin"
    config:
      weights:
        replica-1: 2  # 2x weight
        replica-2: 1  # normal weight
        replica-3: 1  # normal weight
```

---

## Custom Health Check

### Example: Custom Protocol Health Check

**Scenario:** Check health via custom binary protocol.

**Implementation:**

```go
// pkg/health/custom_protocol.go
package health

import (
  "net"
  "time"
)

type CustomProtocolCheck struct {
  timeout time.Duration
}

func (cpc *CustomProtocolCheck) Check(
  replica *gateway.Replica,
) HealthCheckResult {
  conn, err := net.DialTimeout("tcp", replica.Address, cpc.timeout)
  if err != nil {
    return HealthCheckResult{
      Success: false,
      Error: err.Error(),
    }
  }
  defer conn.Close()
  
  // Send custom health check message
  msg := []byte{0x01, 0x02, 0x03}  // Custom protocol
  _, err = conn.Write(msg)
  if err != nil {
    return HealthCheckResult{Success: false, Error: err.Error()}
  }
  
  // Read response
  response := make([]byte, 10)
  conn.SetReadDeadline(time.Now().Add(cpc.timeout))
  n, err := conn.Read(response)
  if err != nil {
    return HealthCheckResult{Success: false, Error: err.Error()}
  }
  
  // Validate response (first byte = 0xFF means healthy)
  if n > 0 && response[0] == 0xFF {
    return HealthCheckResult{Success: true}
  }
  return HealthCheckResult{
    Success: false,
    Error: "invalid response",
  }
}
```

**Config:**
```yaml
health:
  type: "custom_protocol"
  interval: "5s"
  timeout: "2s"
```

---

## Custom Authentication

### Example: OAuth2 Authentication

**Scenario:** Authenticate requests using OAuth2 tokens.

**Implementation:**

```go
// pkg/auth/oauth2.go
package auth

import (
  "context"
  "fmt"
  "github.com/coreos/go-oidc"
)

type OAuth2Auth struct {
  provider *oidc.Provider
  verifier *oidc.IDTokenVerifier
}

func (oa *OAuth2Auth) Authenticate(request Request) (User, error) {
  // Extract token from Authorization header
  token := request.Header.Get("Authorization")
  if token == "" {
    return nil, ErrMissingAuth
  }
  
  // Remove "Bearer " prefix
  if len(token) > 7 && token[:7] == "Bearer " {
    token = token[7:]
  }
  
  // Verify token with OIDC provider
  idToken, err := oa.verifier.Verify(context.Background(), token)
  if err != nil {
    return nil, ErrInvalidToken
  }
  
  // Extract claims
  var claims map[string]interface{}
  idToken.Claims(&claims)
  
  return &User{
    ID: claims["sub"].(string),
    Email: claims["email"].(string),
    Role: claims["role"].(string),
  }, nil
}
```

**Config:**
```yaml
authentication:
  method: "oauth2"
  config:
    issuer: "https://auth.example.com"
    client_id: "my-app"
    client_secret: "${CLIENT_SECRET}"
```

---

## Testing Custom Plugins

### Unit Test Example

```go
// pkg/loadbalancer/weighted_test.go
package loadbalancer

import (
  "testing"
  "github.com/dashfabric/weaver/pkg/gateway"
)

func TestWeightedRoundRobin(t *testing.T) {
  wrr := &WeightedRoundRobin{
    weights: map[string]int{
      "r1": 2,
      "r2": 1,
      "r3": 1,
    },
  }
  
  replicas := []*gateway.Replica{
    {Name: "r1"}, {Name: "r2"}, {Name: "r3"},
  }
  
  // Expected distribution: r1, r1, r2, r3, r1, r1, r2, r3, ...
  expected := []string{"r1", "r1", "r2", "r3"}
  
  for i := 0; i < 10; i++ {
    selected, _ := wrr.Select(nil, replicas)
    if i < 4 && selected.Name != expected[i % 4] {
      t.Errorf("iteration %d: expected %s, got %s",
        i, expected[i % 4], selected.Name)
    }
  }
}
```

### Integration Test Example

```go
// tests/integration/custom_discovery_test.go
package integration

import (
  "testing"
  "github.com/dashfabric/weaver/pkg/discovery"
)

func TestYAMLDiscovery(t *testing.T) {
  // Create temp YAML file
  yaml := `
- name: test-replica
  address: localhost:5051
`
  
  yd := &YAMLDiscovery{}
  yd.Init(discovery.Config{"file_path": "test-replicas.yaml"})
  
  replicas := yd.GetReplicas()
  if len(replicas) != 1 {
    t.Errorf("expected 1 replica, got %d", len(replicas))
  }
  
  if replicas[0].Name != "test-replica" {
    t.Errorf("expected name=test-replica, got %s", replicas[0].Name)
  }
}
```

---

## Registering Custom Plugins

### At Startup

```go
// cmd/weaver/main.go
import (
  "github.com/dashfabric/weaver/pkg/discovery"
  myDiscovery "github.com/myorg/weaver-yaml-discovery"
)

func init() {
  // Register custom discovery
  discovery.Register("yaml_file", func() discovery.Plugin {
    return &myDiscovery.YAMLDiscovery{}
  })
}
```

### Via Config File

```yaml
gateway:
  plugins:
    - name: "yaml_discovery"
      type: "discovery"
      path: "/opt/weaver-plugins/yaml-discovery"
```

---

## Plugin Best Practices

✅ **DO:**
- Handle errors gracefully
- Support configuration via YAML
- Write unit tests (>80% coverage)
- Use goroutines for blocking I/O
- Log important events
- Respect context cancellation

❌ **DON'T:**
- Panic in plugins (breaks gateway)
- Ignore errors
- Block request processing
- Use global state (not thread-safe)
- Make assumptions about caller

---

## Publishing Plugins

**Share your plugin with community:**

1. Create repository: `github.com/yourorg/weaver-{plugin-name}`
2. Add documentation: `README.md` with examples
3. Add tests
4. Create release: `v1.0.0`
5. Submit to plugin registry

---

**Navigation:**
- [← Previous](./70-development-setup.md)
- [Index](../INDEX.md)
- [Next →](./72-contributing.md)
