.PHONY: build clean install test cross-platform release

# Binary names
CLIENT_BIN := ffmpeg-over-ip-client
SERVER_BIN := ffmpeg-over-ip-server

# Binary output paths
CLIENT_PATH := bin/$(CLIENT_BIN)
SERVER_PATH := bin/$(SERVER_BIN)

# Go build flags
GO_BUILD_FLAGS := -trimpath -ldflags="-s -w"

# Platforms to build for
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64

# Default target
all: build

# Build both client and server
build: $(CLIENT_PATH) $(SERVER_PATH)

# Build client binary
$(CLIENT_PATH):
	@mkdir -p bin
	go build $(GO_BUILD_FLAGS) -o $(CLIENT_PATH) ./cmd/client

# Build server binary
$(SERVER_PATH):
	@mkdir -p bin
	go build $(GO_BUILD_FLAGS) -o $(SERVER_PATH) ./cmd/server

# Clean build artifacts
clean:
	rm -rf bin
	rm -rf release

# Install binaries to GOPATH/bin
install: build
	go install $(GO_BUILD_FLAGS) ./cmd/client
	go install $(GO_BUILD_FLAGS) ./cmd/server

# Run tests
test:
	go test -v ./...

# Format code
fmt:
	go fmt ./...

# Run the server with test configuration
run-server: $(SERVER_PATH)
	./$(SERVER_PATH) --config test-config/server.jsonc

# Run the client with test configuration
run-client: $(CLIENT_PATH)
	./$(CLIENT_PATH) --config test-config/client.jsonc

# Cross-compile for all platforms
cross-platform:
	@mkdir -p release
	@for platform in $(PLATFORMS); do \
		os="$${platform%%/*}"; \
		arch="$${platform#*/}"; \
		output_dir="release/$$os-$$arch"; \
		mkdir -p $$output_dir; \
		ext=""; \
		if [ "$$os" = "windows" ]; then ext=".exe"; fi; \
		echo "Building for $$os/$$arch..."; \
		GOOS=$$os GOARCH=$$arch go build $(GO_BUILD_FLAGS) -o $$output_dir/$(CLIENT_BIN)$$ext ./cmd/client; \
		GOOS=$$os GOARCH=$$arch go build $(GO_BUILD_FLAGS) -o $$output_dir/$(SERVER_BIN)$$ext ./cmd/server; \
	done

# Package the cross-compiled binaries
release: cross-platform
	@mkdir -p release/archives
	@for platform in $(PLATFORMS); do \
		os="$${platform%%/*}"; \
		arch="$${platform#*/}"; \
		echo "Creating archive for $$os-$$arch..."; \
		tar -czf release/archives/ffmpeg-over-ip-$$os-$$arch.tar.gz -C release/$$os-$$arch .; \
	done
	@echo "Release archives created in release/archives/ directory"
