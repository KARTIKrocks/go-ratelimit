GOLANGCI_LINT_VERSION := v2.14.0
GOIMPORTS_VERSION := v0.50.0
GOVULNCHECK_VERSION := v1.8.0

# Nested modules. Each has its own go.mod, so the root module's ./... does not
# reach them — they need to be built and tested explicitly. They resolve the
# root module through `replace` directives and go.work.
SUBMODULES := redisstore metrics
EXAMPLE_MODULES := examples/basic examples/multitier examples/redis examples/prometheus

.PHONY: all setup deps tidy tidy-modules tidy-check test test-v test-race test-modules test-integration vet vet-modules lint lint-modules lint-fix fix vuln vuln-modules print-govulncheck-version print-golangci-lint-version build build-examples bench bench-compare fmt fmt-check cover clean ci pre-commit help

all: fmt vet vet-modules lint lint-modules test test-modules build build-examples

## Install development tools (skips if already present)
setup:
	@command -v golangci-lint >/dev/null 2>&1 || { \
		echo "Installing golangci-lint $(GOLANGCI_LINT_VERSION)..."; \
		go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION); \
	}
	@command -v goimports >/dev/null 2>&1 || { \
		echo "Installing goimports $(GOIMPORTS_VERSION)..."; \
		go install golang.org/x/tools/cmd/goimports@$(GOIMPORTS_VERSION); \
	}
	@command -v govulncheck >/dev/null 2>&1 || { \
		echo "Installing govulncheck $(GOVULNCHECK_VERSION)..."; \
		go install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION); \
	}

## Download module dependencies
deps:
	go mod download
	go mod verify

## Tidy go.mod/go.sum for the root module
tidy:
	go mod tidy

## Tidy go.mod/go.sum for every nested module
tidy-modules:
	@for m in $(SUBMODULES) $(EXAMPLE_MODULES); do \
		echo "==> tidy $$m"; \
		(cd $$m && go mod tidy) || exit 1; \
	done

## Fail if any go.mod/go.sum is not tidy, without leaving the change behind.
tidy-check:
	@files="go.mod go.sum $(addsuffix /go.mod,$(SUBMODULES) $(EXAMPLE_MODULES)) $(addsuffix /go.sum,$(SUBMODULES) $(EXAMPLE_MODULES))"; \
	if [ -n "$$(git status --porcelain -- $$files)" ]; then \
		echo "go.mod/go.sum already modified; commit or stash before running tidy-check"; \
		exit 1; \
	fi; \
	$(MAKE) --no-print-directory tidy tidy-modules; \
	if ! git diff --quiet -- $$files; then \
		echo "go.mod/go.sum are not tidy — run 'make tidy tidy-modules' and commit:"; \
		git diff --stat -- $$files; \
		git checkout -- $$files; \
		exit 1; \
	fi; \
	echo "all modules tidy"

## Run all tests with race detector
test test-race:
	go test -race -count=1 ./...

## Run tests with verbose output
test-v:
	go test -race -v -count=1 ./...

## Run tests for every nested module
test-modules:
	@for m in $(SUBMODULES); do \
		echo "==> test $$m"; \
		(cd $$m && go test -race -count=1 ./...) || exit 1; \
	done

## Run integration tests (needs Redis, see REDIS_URL)
test-integration:
	go test -v -tags=integration ./...
	cd redisstore && go test -v -tags=integration ./...

## Format code
fmt: setup
	gofmt -s -w .
	goimports -w .

## Check formatting
fmt-check:
	@test -z "$$(gofmt -s -l . | tee /dev/stderr)"

## Run go vet
vet:
	go vet ./...

## Run go vet for every nested module
vet-modules:
	@for m in $(SUBMODULES) $(EXAMPLE_MODULES); do \
		echo "==> vet $$m"; \
		(cd $$m && go vet ./...) || exit 1; \
	done

## Run golangci-lint
lint: setup
	golangci-lint run ./...

## Run golangci-lint for every nested module
lint-modules: setup
	@for m in $(SUBMODULES); do \
		echo "==> lint $$m"; \
		(cd $$m && golangci-lint run ./...) || exit 1; \
	done

## Run golangci-lint with auto-fix
lint-fix: setup
	golangci-lint run --fix ./...

## Apply `go fix` modernizers, then formatting and lint auto-fixes
fix:
	go fix ./...
	@for m in $(SUBMODULES) $(EXAMPLE_MODULES); do \
		echo "==> fix $$m"; \
		(cd $$m && go fix ./...) || exit 1; \
	done
	$(MAKE) --no-print-directory fmt lint-fix

## Scan the root module for known vulnerabilities reachable from this code.
## Needs network access; also scans the installed Go standard library.
vuln: setup
	govulncheck ./...

## Print the pinned scanner version (CI reads this to avoid drift).
print-govulncheck-version:
	@echo $(GOVULNCHECK_VERSION)

## Print the pinned linter version (CI reads this to avoid drift).
print-golangci-lint-version:
	@echo $(GOLANGCI_LINT_VERSION)

## Scan every nested module
vuln-modules: setup
	@for m in $(SUBMODULES); do \
		echo "==> vuln $$m"; \
		(cd $$m && govulncheck ./...) || exit 1; \
	done

## Build all packages
build:
	go build ./...

## Build example modules
build-examples:
	@for m in $(EXAMPLE_MODULES); do \
		echo "==> build $$m"; \
		(cd $$m && go build ./...) || exit 1; \
	done

## Run benchmarks
bench:
	go test -bench=. -benchmem -run='^$$' ./...

## Run benchmarks and save to new.txt for benchstat
bench-compare:
	go test -bench=. -benchmem -run='^$$' ./... | tee new.txt
	@echo "Compare with: benchstat old.txt new.txt"

## Run tests with coverage report
cover:
	go test -race -coverprofile=coverage.out -covermode=atomic ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

## Remove build artifacts
clean:
	rm -f coverage.out coverage.html *.test *.prof

## Quick checks before committing
pre-commit: fmt vet lint test

## Everything the CI merge gate checks, in one target.
ci: fmt-check vet vet-modules lint lint-modules test test-modules build build-examples vuln vuln-modules

## Show available targets
help:
	@awk '/^## /{d=substr($$0,4);next} /^[a-zA-Z_-]+( [a-zA-Z_-]+)*:/{split($$0,a,":");printf "  %-24s %s\n",a[1],d;d=""} !/^## /{d=""}' $(MAKEFILE_LIST)
