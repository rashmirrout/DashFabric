package testutil

// Replica fixtures for testing

// HealthyReplicas returns 3 healthy replica addresses
func HealthyReplicas() []string {
	return []string{
		"replica-1:5051",
		"replica-2:5051",
		"replica-3:5051",
	}
}

// LargeReplicaList returns 100 replica addresses
func LargeReplicaList() []string {
	replicas := make([]string, 100)
	for i := 0; i < 100; i++ {
		replicas[i] = "replica-" + string(rune(i+1)) + ":5051"
	}
	return replicas
}

// SingleReplica returns a single replica address
func SingleReplica() []string {
	return []string{"replica-1:5051"}
}

// EmptyReplicaList returns empty replica list
func EmptyReplicaList() []string {
	return []string{}
}

// Config fixtures for testing

// MinimalConfig returns minimal test configuration
func MinimalConfig() string {
	return `
gateway:
  name: test-gateway

listeners:
  grpc:
    port: 5051

discovery:
  method: static
  config:
    replicas:
      - replica-1:5051
      - replica-2:5051
      - replica-3:5051

health:
  type: http
  interval: 5s
  timeout: 2s

load_balancers:
  default:
    strategy: round_robin
`
}

// FullConfig returns comprehensive test configuration
func FullConfig() string {
	return `
gateway:
  name: test-gateway

listeners:
  grpc:
    port: 5051

discovery:
  method: etcd
  config:
    endpoints:
      - localhost:2379
    cache_ttl: 30s

health:
  type: http
  interval: 5s
  timeout: 2s
  path: /health

load_balancers:
  default:
    strategy: least_connections

reliability:
  timeout:
    global: 30s
    per_replica: 10s
    connect: 5s
  circuit_breaker:
    enabled: true
    failure_threshold: 5
    success_threshold: 2
    timeout: 30s
  retry:
    enabled: true
    max_attempts: 3
    initial_backoff: 100ms
    max_backoff: 10s

observability:
  metrics:
    enabled: true
    port: 9090
  logging:
    level: DEBUG
    format: json
`
}

// Test message fixtures

// SampleRequest returns a sample request payload
func SampleRequest() []byte {
	return []byte(`{"method":"GetStatus","params":{}}`)
}

// SampleResponse returns a sample response payload
func SampleResponse() []byte {
	return []byte(`{"status":"ok","data":{}}`)
}

// SampleHeaders returns sample request headers
func SampleHeaders() map[string]string {
	return map[string]string{
		"Content-Type":  "application/json",
		"Authorization": "Bearer token123",
		"X-Request-ID":  "req-001",
	}
}
