.PHONY: all test test-race test-cover bench lint fmt vet clean install-tools

# Variables
GO := go
GOTEST := $(GO) test
GOBUILD := $(GO) build
GOMOD := $(GO) mod
GOFMT := gofmt
GOVET := $(GO) vet

# Build info
VERSION ?= $(shell git describe --tags --always --dirty)
COMMIT := $(shell git rev-parse --short HEAD)
BUILD_TIME := $(shell date -u '+%Y-%m-%d_%H:%M:%S')

all: fmt vet lint test

# Run tests
test:
	@echo "Running tests..."
	$(GOTEST) -v ./...

# Run tests with race detector
test-race:
	@echo "Running tests with race detector..."
	$(GOTEST) -v -race ./...

# Run tests with coverage
test-cover:
	@echo "Running tests with coverage..."
	$(GOTEST) -v -race -coverprofile=coverage.out -covermode=atomic ./...
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

# Run integration tests
test-integration:
	@echo "Running integration tests..."
	$(GOTEST) -v -tags=integration ./...

# Run benchmarks
bench:
	@echo "Running benchmarks..."
	$(GOTEST) -bench=. -benchmem -run=^$$ ./...

# Run benchmarks and compare
bench-compare:
	@echo "Running benchmarks..."
	$(GOTEST) -bench=. -benchmem -run=^$$ ./... | tee new.txt
	@echo "Compare with: benchstat old.txt new.txt"

# Lint code
lint:
	@echo "Running linters..."
	golangci-lint run ./...

# Format code
fmt:
	@echo "Formatting code..."
	$(GOFMT) -s -w .

# Check formatting
fmt-check:
	@echo "Checking code formatting..."
	@test -z "$$($(GOFMT) -s -l . | tee /dev/stderr)"

# Vet code
vet:
	@echo "Running go vet..."
	$(GOVET) ./...

# Security check
sec:
	@echo "Running security check..."
	gosec ./...

# Clean build artifacts
clean:
	@echo "Cleaning..."
	rm -f coverage.out coverage.html
	rm -f *.test
	rm -f *.prof

# Install development tools
install-tools:
	@echo "Installing development tools..."
	$(GO) install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	$(GO) install github.com/securego/gosec/v2/cmd/gosec@latest
	$(GO) install golang.org/x/perf/cmd/benchstat@latest

# Download dependencies
deps:
	@echo "Downloading dependencies..."
	$(GOMOD) download
	$(GOMOD) verify

# Tidy dependencies
tidy:
	@echo "Tidying dependencies..."
	$(GOMOD) tidy

# Run examples
example-basic:
	$(GO) run examples/basic/main.go

example-redis:
	$(GO) run examples/redis/main.go

example-prometheus:
	$(GO) run examples/prometheus/main.go

example-multitier:
	$(GO) run examples/multitier/main.go

# Generate documentation
docs:
	@echo "Generating documentation..."
	godoc -http=:6060

# Pre-commit checks
pre-commit: fmt vet lint test-race
	@echo "All pre-commit checks passed!"

# CI checks (what runs in GitHub Actions)
ci: fmt-check vet lint test-cover
	@echo "All CI checks passed!"

# Help
help:
	@echo "Available targets:"
	@echo "  all            - Run fmt, vet, lint, and test"
	@echo "  test           - Run tests"
	@echo "  test-race      - Run tests with race detector"
	@echo "  test-cover     - Run tests with coverage"
	@echo "  test-integration - Run integration tests"
	@echo "  bench          - Run benchmarks"
	@echo "  lint           - Run linters"
	@echo "  fmt            - Format code"
	@echo "  fmt-check      - Check code formatting"
	@echo "  vet            - Run go vet"
	@echo "  sec            - Run security check"
	@echo "  clean          - Clean build artifacts"
	@echo "  install-tools  - Install development tools"
	@echo "  deps           - Download dependencies"
	@echo "  tidy           - Tidy dependencies"
	@echo "  docs           - Generate documentation"
	@echo "  pre-commit     - Run all pre-commit checks"
	@echo "  ci             - Run CI checks"
	@echo "  help           - Show this help message"
