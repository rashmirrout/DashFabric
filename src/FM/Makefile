.PHONY: help build clean test test-unit test-integration test-chaos test-coverage test-race lint fmt vet ci docker-build

help:
	@echo "Fabric Manager (FM) Makefile"
	@echo ""
	@echo "Build Targets:"
	@echo "  make build              - Compile Go code"
	@echo "  make clean              - Remove build artifacts"
	@echo ""
	@echo "Testing Targets:"
	@echo "  make test               - Run all tests (unit + integration + chaos)"
	@echo "  make test-unit          - Unit tests only"
	@echo "  make test-integration   - Integration tests only"
	@echo "  make test-chaos         - Chaos tests only"
	@echo "  make test-coverage      - Show coverage (fails if < 100%)"
	@echo "  make test-race          - Run with race detector"
	@echo ""
	@echo "Code Quality:"
	@echo "  make lint               - Run golangci-lint"
	@echo "  make fmt                - Format code"
	@echo "  make vet                - Run go vet"
	@echo ""
	@echo "CI/CD:"
	@echo "  make ci                 - Run all checks (build + lint + test + coverage)"
	@echo ""
	@echo "Docker:"
	@echo "  make docker-build       - Build Docker image"

# Build
build:
	@echo "Building FM..."
	go build -race -v ./...

clean:
	@echo "Cleaning..."
	rm -f coverage.out coverage.html
	go clean

# Testing
test: test-unit test-integration test-chaos
	@echo "✓ All tests passed"

test-unit:
	@echo "Running unit tests..."
	go test -race -v -short -coverprofile=coverage.out ./pkg/...

test-integration:
	@echo "Running integration tests..."
	go test -race -v -run Integration ./tests/integration/...

test-chaos:
	@echo "Running chaos tests..."
	go test -race -v -run Chaos ./tests/chaos/...

test-coverage:
	@echo "Running tests with coverage..."
	go test -race -v -coverprofile=coverage.out -covermode=atomic ./...
	go tool cover -html=coverage.out -o coverage.html
	@COVERAGE=$$(go tool cover -func=coverage.out | grep total | awk '{print $$3}' | sed 's/%//'); \
	echo "Coverage: $$COVERAGE%"; \
	if (( $$(echo "$$COVERAGE < 100" | bc -l) )); then \
		echo "ERROR: Coverage $$COVERAGE% < 100%"; \
		exit 1; \
	fi; \
	echo "✓ Coverage check passed"

test-race:
	@echo "Running tests with -race detector..."
	go test -race -v ./...

# Code Quality
lint:
	@echo "Running golangci-lint..."
	golangci-lint run ./...

fmt:
	@echo "Formatting code..."
	gofmt -s -w .
	goimports -w .

vet:
	@echo "Running go vet..."
	go vet ./...

# CI/CD
ci: build lint test-race test-coverage
	@echo "✓ All CI checks passed"

# Docker
docker-build:
	@echo "Building Docker image..."
	docker build -t fm:latest .

# Dependencies
deps:
	@echo "Downloading dependencies..."
	go mod download
	go mod tidy

# Install linters
install-linters:
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	go install github.com/cosmtrek/air@latest
