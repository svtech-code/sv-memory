# Makefile for sv-memory

BINARY_NAME=sv-memory
CMD_DIR=./cmd/sv-memory
BUILD_DIR=bin

.PHONY: all build test test-race lint fmt vet install clean bench help

all: build

help:
	@echo "Available targets:"
	@echo "  build        Build the sv-memory binary"
	@echo "  test         Run all unit tests"
	@echo "  test-race    Run all unit tests with data race detection"
	@echo "  lint         Run golangci-lint on the codebase"
	@echo "  fmt          Format the codebase using gofmt"
	@echo "  vet          Run go vet on the codebase"
	@echo "  install      Install the binary to GOPATH/bin"
	@echo "  clean        Remove build artifacts"
	@echo "  bench        Run benchmarks"

build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	go build -o $(BUILD_DIR)/$(BINARY_NAME) $(CMD_DIR)

test:
	@echo "Running unit tests..."
	go test -v -cover ./...

test-race:
	@echo "Running unit tests with race detector..."
	go test -v -cover -race ./...

lint:
	@echo "Running golangci-lint..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run; \
	else \
		echo "golangci-lint not found. Please install it or use '.golangci.yml' integration."; \
		exit 1; \
	fi

fmt:
	@echo "Formatting Go files..."
	go fmt ./...

vet:
	@echo "Running go vet..."
	go vet ./...

install: build
	@echo "Installing $(BINARY_NAME) to GOPATH/bin..."
	go install $(CMD_DIR)

clean:
	@echo "Cleaning build artifacts..."
	rm -rf $(BUILD_DIR)
	rm -f $(BINARY_NAME)

bench:
	@echo "Running benchmarks..."
	go test -bench=. -run=^$$ ./...
