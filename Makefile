.PHONY: build test clean version help

# Version information
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "0.1.0")
GIT_COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME ?= $(shell date -u '+%Y-%m-%d %H:%M:%S' 2>/dev/null || echo "unknown")
GO_VERSION ?= $(shell go version | awk '{print $$3}')

# Build flags
LDFLAGS = -ldflags "-X github.com/LingByte/lingllm/version.Version=$(VERSION) -X github.com/LingByte/lingllm/version.GitCommit=$(GIT_COMMIT) -X github.com/LingByte/lingllm/version.BuildTime=$(BUILD_TIME) -X github.com/LingByte/lingllm/version.GoVersion=$(GO_VERSION)"

help:
	@echo "LingLLM Build Commands"
	@echo "======================"
	@echo "make build          - Build the project"
	@echo "make test           - Run all tests"
	@echo "make test-coverage  - Run tests with coverage report"
	@echo "make clean          - Clean build artifacts"
	@echo "make version        - Show version information"
	@echo "make fmt            - Format code with gofmt"
	@echo "make tools-demo     - Build tools demo"
	@echo "make batch-demo     - Build batch processing demo"

build:
	@echo "Building LingLLM..."
	@go build ./...
	@echo "✓ Build complete"

test:
	@echo "Running tests..."
	@go test ./... -v

test-coverage:
	@echo "Running tests with coverage..."
	@go test ./... -cover

clean:
	@echo "Cleaning build artifacts..."
	@go clean
	@rm -f tools-demo batch-demo
	@echo "✓ Clean complete"

version:
	@echo "LingLLM Version Information"
	@echo "============================"
	@echo "Version:    $(VERSION)"
	@echo "Git Commit: $(GIT_COMMIT)"
	@echo "Build Time: $(BUILD_TIME)"
	@echo "Go Version: $(GO_VERSION)"

fmt:
	@echo "Formatting code..."
	@gofmt -w .
	@echo "✓ Format complete"

cli:
	@echo "Building CLI tool with version info..."
	@bash build.sh

tools-demo:
	@echo "Building tools-demo..."
	@go build -o tools-demo ./examples/tools-demo
	@echo "✓ tools-demo built"

batch-demo:
	@echo "Building batch-processing-demo..."
	@go build -o batch-demo ./examples/batch-processing-demo
	@echo "✓ batch-demo built"

# Development targets
dev-build: fmt test build
	@echo "✓ Development build complete"

# Release target
release: clean test build
	@echo "✓ Release build complete"
