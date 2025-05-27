.PHONY: build test test-verbose test-cover clean run-sitemap run-crawl docker-build docker-test help version

# Version information
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

# Build flags
LDFLAGS = -s -w -X main.version=$(VERSION)

# Show version information
version:
	@echo "Version: $(VERSION)"
	@echo "Commit:  $(COMMIT)"
	@echo "Date:    $(BUILD_DATE)"

# Build the binary
build:
	go build -ldflags "$(LDFLAGS)" -o link-checker ./cmd/link-checker

# Build with full version info (for releases)
build-release:
	go build -ldflags "$(LDFLAGS) -X main.commit=$(COMMIT) -X main.buildDate=$(BUILD_DATE)" -o link-checker ./cmd/link-checker

## Linting ##
.PHONY: modverify
modverify: ## Runs 'go mod verify'
	@go mod verify

.PHONY: vet
vet: ## Runs 'go vet'
	@go vet ./...

.PHONY: gofumpt
gofumpt: vet ## Check linting with 'gofumpt'
	@go run mvdan.cc/gofumpt -l -d .

.PHONY: golangci-lint
golangci-lint: ## Lint using 'golangci-lint'
	@go run github.com/golangci/golangci-lint/cmd/golangci-lint \
	run --timeout=300s --out-format checkstyle ./... 2>&1 | tee checkstyle-report.xml

.PHONY: lint
lint: modverify vet gofumpt golangci-lint ## Run all linters

.PHONY: check-vuln
check-vuln: ## Check for vulnerabilities using 'govulncheck'
	@echo "Checking for vulnerabilities..."
	go run golang.org/x/vuln/cmd/govulncheck ./...

# Run all tests
test:
	go test ./...

# Run tests with verbose output
test-verbose:
	go test ./... -v

# Run tests with coverage
test-cover:
	go test ./... -cover

# Clean build artifacts
clean:
	rm -f link-checker

# Test with Josh's sitemap (example)
run-sitemap:
	env INPUT_SITEMAP-URL=https://joshbeard.me/sitemap.xml \
		INPUT_TIMEOUT=30 \
		INPUT_MAX-CONCURRENT=5 \
		INPUT_VERBOSE=true \
		./link-checker

# Test with crawling (example)
run-crawl:
	env INPUT_BASE-URL=https://joshbeard.me \
		INPUT_MAX-DEPTH=1 \
		INPUT_TIMEOUT=30 \
		INPUT_MAX-CONCURRENT=3 \
		INPUT_VERBOSE=true \
		./link-checker

# Build Docker image
docker-build:
	docker build -t link-checker .

# Test with Docker using sitemap
docker-test:
	docker run --rm \
		-e INPUT_SITEMAP-URL=https://joshbeard.me/sitemap.xml \
		-e INPUT_TIMEOUT=30 \
		-e INPUT_MAX-CONCURRENT=5 \
		-e INPUT_VERBOSE=true \
		link-checker

# Show help
help:
	@echo "Available targets:"
	@echo "  lint          - Run all linters"
	@echo "  build         - Build the binary with version info"
	@echo "  build-release - Build the binary with full version info (commit, date)"
	@echo "  test          - Run all tests"
	@echo "  test-verbose  - Run tests with verbose output"
	@echo "  test-cover    - Run tests with coverage"
	@echo "  clean         - Clean build artifacts"
	@echo "  run-sitemap   - Test with Josh's sitemap (requires build)"
	@echo "  run-crawl     - Test with crawling (requires build)"
	@echo "  docker-build  - Build Docker image"
	@echo "  docker-test   - Test with Docker"
	@echo "  help          - Show this help"
	@echo ""
	@echo "Version: $(VERSION)"