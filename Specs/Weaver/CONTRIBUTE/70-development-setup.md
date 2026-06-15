# Weaver: Development Setup

> **Read Time:** 15 minutes  
> **Previous:** [../DESIGN/44-concurrency-model.md](../DESIGN/44-concurrency-model.md) | **Next:** [71-plugin-development.md](./71-plugin-development.md)

---

## Local Development Environment

Complete guide to set up Weaver development locally.

---

## Prerequisites

- Go 1.19+ (for compilation)
- Git (for version control)
- Docker + Docker Compose (for test dependencies)
- make (for build automation)
- editor/IDE (VS Code, GoLand, etc.)

---

## Repository Setup

**Clone repository:**
```bash
git clone https://github.com/dashfabric/weaver.git
cd weaver
```

**Install Go dependencies:**
```bash
go mod download
go mod tidy
```

**Verify setup:**
```bash
go version
go env GOPATH
ls -la ./cmd/weaver
```

---

## Development Docker Compose Stack

**File: docker-compose-dev.yaml**

```yaml
version: '3.8'

services:
  # etcd for service discovery
  etcd:
    image: quay.io/coreos/etcd:v3.5.5
    environment:
      - ETCD_LISTEN_CLIENT_URLS=http://0.0.0.0:2379
      - ETCD_ADVERTISE_CLIENT_URLS=http://etcd:2379
    ports:
      - "2379:2379"

  # Mock backend services
  backend-1:
    image: nicolaka/netcat-ubuntu:latest
    command: nc -l -p 5051 -e /bin/cat
    ports:
      - "5051:5051"

  backend-2:
    image: nicolaka/netcat-ubuntu:latest
    command: nc -l -p 5052 -e /bin/cat
    ports:
      - "5052:5052"

  backend-3:
    image: nicolaka/netcat-ubuntu:latest
    command: nc -l -p 5053 -e /bin/cat
    ports:
      - "5053:5053"
```

**Start dev stack:**
```bash
docker-compose -f docker-compose-dev.yaml up -d
```

---

## Build & Run

**Build Weaver binary:**
```bash
go build -o weaver ./cmd/weaver
```

**Run with config:**
```bash
./weaver -config config-dev.yaml
```

**Dev Configuration (config-dev.yaml):**
```yaml
gateway:
  name: "weaver-dev"

listeners:
  grpc:
    port: 5051

discovery:
  method: "etcd_fabric"
  config:
    endpoints:
      - "localhost:2379"
    cache_ttl: "30s"

health:
  type: "http"
  interval: "5s"
  timeout: "2s"

load_balancers:
  default:
    strategy: "round_robin"

reliability:
  timeout:
    global: "30s"
  circuit_breaker:
    enabled: true
  retry:
    enabled: true

observability:
  metrics:
    enabled: true
    port: 9090
  logging:
    level: "DEBUG"
    format: "json"
```

---

## Testing

### Unit Tests

```bash
# Run all tests
go test ./...

# Run tests in specific package
go test ./pkg/discovery

# Run with coverage
go test -cover ./...

# Run with race detection
go test -race ./...

# Run specific test
go test -run TestSelectReplica ./pkg/loadbalancer
```

### Integration Tests

```bash
# Start dev stack
docker-compose -f docker-compose-dev.yaml up -d

# Run integration tests
go test -tags=integration ./tests/integration

# Cleanup
docker-compose -f docker-compose-dev.yaml down
```

### Manual Testing

```bash
# Start Weaver
./weaver -config config-dev.yaml &

# Send test request
grpcurl -plaintext localhost:5051 list

# Check metrics
curl http://localhost:9090/metrics | head

# Check replicas discovered
curl http://localhost:8080/debug/replicas

# Kill Weaver
pkill weaver
```

---

## Project Structure

```
weaver/
├── cmd/
│   └── weaver/
│       └── main.go               # Entry point
├── pkg/
│   ├── config/
│   │   ├── config.go            # Config parsing
│   │   └── validator.go         # Config validation
│   ├── discovery/
│   │   ├── discovery.go         # Interface
│   │   ├── etcd.go              # etcd implementation
│   │   └── consul.go            # Consul implementation
│   ├── health/
│   │   ├── monitor.go           # Health monitor
│   │   └── checker.go           # Health check logic
│   ├── loadbalancer/
│   │   ├── loadbalancer.go      # Interface
│   │   ├── round_robin.go       # RR implementation
│   │   └── least_conn.go        # LC implementation
│   ├── gateway/
│   │   ├── gateway.go           # Main gateway
│   │   └── request.go           # Request handler
│   ├── reliability/
│   │   ├── circuit_breaker.go
│   │   └── retry.go
│   └── metrics/
│       └── metrics.go           # Prometheus metrics
├── tests/
│   ├── unit/
│   └── integration/
├── docs/
│   └── DEVELOPMENT.md
└── Makefile
```

---

## Makefile

**File: Makefile**

```makefile
.PHONY: build test test-coverage lint fmt clean run

build:
	go build -o weaver ./cmd/weaver

test:
	go test -race ./...

test-coverage:
	go test -cover ./... -coverprofile=coverage.out
	go tool cover -html=coverage.out

lint:
	golangci-lint run ./...

fmt:
	go fmt ./...
	goimports -w ./pkg

clean:
	rm -f weaver coverage.out

run: build
	./weaver -config config-dev.yaml

docker-build:
	docker build -t weaver:dev .

docker-run: docker-build
	docker run -p 5051:5051 -p 9090:9090 \
	  -v $(PWD)/config-dev.yaml:/etc/weaver/config.yaml:ro \
	  weaver:dev
```

**Usage:**
```bash
make build      # Compile
make test       # Run tests
make lint       # Check code quality
make clean      # Remove artifacts
make run        # Build and run locally
```

---

## Code Style

### Formatting

```bash
# Format code
go fmt ./...

# Sort imports
goimports -w ./pkg

# Check formatting
gofmt -l ./...
```

### Linting

```bash
# Install linter
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

# Run linter
golangci-lint run ./...
```

### Conventions

- **Naming:** CamelCase (public), camelCase (private)
- **Comments:** Proper English sentences
- **Functions:** < 50 lines (break into smaller)
- **Errors:** Always check and propagate
- **Logging:** Use structured JSON logs

---

## Debugging

### VS Code Configuration

**File: .vscode/launch.json**

```json
{
  "version": "0.2.0",
  "configurations": [
    {
      "name": "Weaver",
      "type": "go",
      "request": "launch",
      "mode": "debug",
      "program": "${workspaceFolder}/cmd/weaver",
      "args": ["-config", "config-dev.yaml"],
      "cwd": "${workspaceFolder}",
      "dlvToolPath": "dlv",
      "showLog": true
    }
  ]
}
```

**Run in debugger:**
- F5 to start
- Breakpoints work normally
- Inspect variables at runtime

### Print Debugging

```bash
# Add log statement
fmt.Printf("DEBUG: replica=%v, err=%v\n", replica, err)

# Or structured logging
logger.With("replica", replica).Error("failed", err)

# Run with DEBUG level
LOG_LEVEL=DEBUG ./weaver -config config-dev.yaml
```

---

## Pre-Commit Checklist

Before pushing code:

- [ ] Run `go test ./...` (all tests pass)
- [ ] Run `go test -race ./...` (no race conditions)
- [ ] Run `golangci-lint run ./...` (no lint errors)
- [ ] Run `go fmt ./...` (formatted code)
- [ ] Write unit tests for new code (>80% coverage)
- [ ] Update documentation if API changes
- [ ] Add commit message describing change

---

**Navigation:**
- [← Previous](../DESIGN/44-concurrency-model.md)
- [Index](../INDEX.md)
- [Next →](./71-plugin-development.md)
