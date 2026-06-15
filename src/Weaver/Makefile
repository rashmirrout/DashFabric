.PHONY: build test test-coverage test-race lint fmt clean run help

help:
	@echo "Weaver Gateway - Available targets:"
	@echo "  make build           - Compile weaver binary"
	@echo "  make test            - Run unit tests"
	@echo "  make test-race       - Run tests with race detector"
	@echo "  make test-coverage   - Run tests with coverage report"
	@echo "  make lint            - Run linter (golangci-lint)"
	@echo "  make fmt             - Format code (gofmt, goimports)"
	@echo "  make ci              - Full CI pipeline (test + coverage + lint)"
	@echo "  make clean           - Remove build artifacts"

build:
	@echo "Building Weaver..."
	go build -o weaver ./cmd/weaver

test:
	@echo "Running tests..."
	go test -v ./...

test-race:
	@echo "Running tests with race detector..."
	go test -race -v ./...

test-coverage:
	@echo "Running tests with coverage..."
	go test -race -coverprofile=coverage.out -covermode=atomic ./...
	@echo "Coverage report generated: coverage.out"
	@coverage_percent=$$(go tool cover -func=coverage.out | grep total | awk '{print $$3}' | sed 's/%//'); \
	if [ "$${coverage_percent%.*}" -lt 100 ]; then \
		echo "ERROR: Code coverage is $${coverage_percent}%, expected 100%"; \
		exit 1; \
	fi
	@echo "✓ Code coverage: $${coverage_percent}%"

lint:
	@echo "Running linter..."
	golangci-lint run ./...

fmt:
	@echo "Formatting code..."
	go fmt ./...
	goimports -w ./pkg ./cmd ./tests

ci: test-race test-coverage lint
	@echo "✓ All CI checks passed"

clean:
	@echo "Cleaning..."
	rm -f weaver coverage.out coverage.html
	go clean

coverage-html: test-coverage
	@go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"
