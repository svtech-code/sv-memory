# Makefile for sv-memory

BINARY_NAME=sv-memory
CMD_DIR=./cmd/sv-memory
BUILD_DIR=bin
RELEASE_DIR=dist
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64
VERSION ?= dev
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
LDFLAGS  := -X main.version=$(VERSION) -X main.commit=$(COMMIT)

.PHONY: all build test test-race lint fmt vet install clean bench release help

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
	@echo "  release      Cross-compile release artifacts for all platforms into $(RELEASE_DIR)/"
	@echo "  clean        Remove build artifacts"
	@echo "  bench        Run benchmarks"

build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	go build -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME) $(CMD_DIR)

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

release:
	@echo "Cross-compiling release binaries (version: $(VERSION))..."
	@mkdir -p $(RELEASE_DIR)/package
	@for target in $(PLATFORMS); do \
		os="$${target%/*}"; \
		arch="$${target#*/}"; \
		ext=""; \
		[ "$$os" = "windows" ] && ext=".exe"; \
		echo "  building $$os/$$arch..."; \
		GOOS="$$os" GOARCH="$$arch" CGO_ENABLED=0 go build -trimpath -ldflags="-s -w $(LDFLAGS)" -o "$(RELEASE_DIR)/package/$(BINARY_NAME)$${ext}" $(CMD_DIR); \
		if [ "$$os" = "windows" ]; then \
			(cd $(RELEASE_DIR)/package && zip -q "../$(BINARY_NAME)_$${os}_$${arch}.zip" "$(BINARY_NAME)$${ext}"); \
		else \
			tar -czf "$(RELEASE_DIR)/$(BINARY_NAME)_$${os}_$${arch}.tar.gz" -C $(RELEASE_DIR)/package "$(BINARY_NAME)$${ext}"; \
		fi; \
		rm -f "$(RELEASE_DIR)/package/$(BINARY_NAME)$${ext}"; \
	done
	@rm -rf $(RELEASE_DIR)/package
	@cd $(RELEASE_DIR) && shasum -a 256 $(BINARY_NAME)_* > checksums.txt
	@echo "Release artifacts in ./$(RELEASE_DIR):"
	@ls -la $(RELEASE_DIR)

clean:
	@echo "Cleaning build artifacts..."
	rm -rf $(BUILD_DIR)
	rm -rf $(RELEASE_DIR)
	rm -f $(BINARY_NAME)

bench:
	@echo "Running benchmarks..."
	go test -bench=. -run=^$$ ./...
