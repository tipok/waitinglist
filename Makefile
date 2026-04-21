# Variables
BINARY_NAME=waitinglist
MAIN_PATH=cmd/server/main.go
BUILD_DIR=bin
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64

temp = $(subst /, ,$@)
os = $(word 1, $(temp))
arch = $(word 2, $(temp))

# Default target
.PHONY: all
all: build

# Build the binary
.PHONY: build
build:
	@mkdir -p $(BUILD_DIR)
	go build -o $(BUILD_DIR)/$(BINARY_NAME) $(MAIN_PATH)

release: $(PLATFORMS)

.PHONY: release $(PLATFORMS)
$(PLATFORMS):
	@mkdir -p $(BUILD_DIR)
	GOOS=$(os) GOARCH=$(arch) go build -a -gcflags=all="-l -B" -ldflags="-w -s" -o '$(BUILD_DIR)/$(BINARY_NAME)-$(os)-$(arch)' $(MAIN_PATH)

# Run tests
.PHONY: test
test:
	go test ./...

# Determine container runtime: prefer 'container' if available, fall back to 'docker'
CONTAINER_RUNTIME := $(shell container system status >/dev/null 2>&1 && echo container || echo docker)

# Lint using golangci-lint in container
.PHONY: lint
lint:
	$(CONTAINER_RUNTIME) run --rm -v $(PWD):/app -w /app golangci/golangci-lint:latest golangci-lint run

# Format code
.PHONY: format
format:
	goimports -w .

# Check code formatting (CI-friendly, doesn't modify files)
.PHONY: format-check
format-check:
	@echo "Checking code formatting..."
	@unformatted=$$(goimports -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "The following files are not formatted:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi
	@echo "All files are properly formatted."

# Clean build artifacts
.PHONY: clean
clean:
	rm -rf $(BUILD_DIR)

# Install dependencies
.PHONY: deps
deps:
	go mod tidy
	go mod download

# Docker image builds
IMAGE_NAME ?= $(BINARY_NAME)

.PHONY: docker-build\:amd64
docker-build\:amd64:
	$(CONTAINER_RUNTIME) build --build-arg TARGETARCH=amd64 -t $(IMAGE_NAME):latest-amd64 .

.PHONY: docker-build\:arm64
docker-build\:arm64:
	$(CONTAINER_RUNTIME) build --build-arg TARGETARCH=arm64 -t $(IMAGE_NAME):latest-arm64 .

.PHONY: docker-build
docker-build:
	$(MAKE) 'docker-build:amd64'
	$(MAKE) 'docker-build:arm64'

# Run the binary
.PHONY: run
run: build
	./$(BUILD_DIR)/$(BINARY_NAME)
