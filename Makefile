VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME)

BINARIES := hub-server hub-client hubctl
PLATFORMS := \
	linux/amd64 \
	linux/arm64 \
	linux/arm \
	darwin/amd64 \
	darwin/arm64 \
	windows/amd64 \
	windows/arm64 \
	freebsd/amd64

DIST_DIR := dist

GUI_MACOS_DIR := gui/macos/ClientHub
LIB_DYLIB := $(GUI_MACOS_DIR)/ClientHub/libclienthub.dylib

.PHONY: all build clean cross package lib-macos lib-linux lib-windows gui-macos

all: build

build:
	@mkdir -p bin
	go build -ldflags "$(LDFLAGS)" -o bin/hub-server ./cmd/server
	go build -ldflags "$(LDFLAGS)" -o bin/hub-client ./cmd/client
	go build -ldflags "$(LDFLAGS)" -o bin/hubctl     ./cmd/manager
	@echo "Built: bin/hub-server bin/hub-client bin/hubctl"

cross:
	@mkdir -p $(DIST_DIR)
	@for platform in $(PLATFORMS); do \
		os=$${platform%/*}; \
		arch=$${platform#*/}; \
		ext=""; \
		if [ "$$os" = "windows" ]; then ext=".exe"; fi; \
		dir="$(DIST_DIR)/clienthub-$$os-$$arch"; \
		mkdir -p "$$dir"; \
		echo "Building $$os/$$arch ..."; \
		GOOS=$$os GOARCH=$$arch go build -ldflags "$(LDFLAGS)" -o "$$dir/hub-server$$ext" ./cmd/server && \
		GOOS=$$os GOARCH=$$arch go build -ldflags "$(LDFLAGS)" -o "$$dir/hub-client$$ext" ./cmd/client && \
		GOOS=$$os GOARCH=$$arch go build -ldflags "$(LDFLAGS)" -o "$$dir/hubctl$$ext"     ./cmd/manager && \
		cp -r examples "$$dir/examples" && \
		echo "  -> $$dir"; \
	done
	@echo "Cross-compilation complete. Output in $(DIST_DIR)/"

package: cross
	@echo "Packaging archives ..."
	@for platform in $(PLATFORMS); do \
		os=$${platform%/*}; \
		arch=$${platform#*/}; \
		dir="clienthub-$$os-$$arch"; \
		if [ "$$os" = "windows" ]; then \
			(cd $(DIST_DIR) && zip -qr "$$dir.zip" "$$dir/"); \
			echo "  -> $(DIST_DIR)/$$dir.zip"; \
		else \
			(cd $(DIST_DIR) && tar czf "$$dir.tar.gz" "$$dir/"); \
			echo "  -> $(DIST_DIR)/$$dir.tar.gz"; \
		fi; \
	done
	@echo "Packaging complete."

# --- Shared library targets ---

lib-macos:
	@echo "Building libclienthub.dylib for macOS ..."
	CGO_ENABLED=1 go build -buildmode=c-shared \
		-o $(LIB_DYLIB) ./cmd/libclienthub
	@echo "  -> $(LIB_DYLIB)"
	@echo "  -> $(LIB_DYLIB:.dylib=.h) (auto-generated header)"

lib-linux:
	@echo "Building libclienthub.so for Linux ..."
	CGO_ENABLED=1 go build -buildmode=c-shared \
		-o build/libclienthub.so ./cmd/libclienthub
	@echo "  -> build/libclienthub.so"

lib-windows:
	@echo "Building libclienthub.dll for Windows ..."
	CGO_ENABLED=1 GOOS=windows go build -buildmode=c-shared \
		-o build/libclienthub.dll ./cmd/libclienthub
	@echo "  -> build/libclienthub.dll"

# --- GUI targets ---

gui-macos: lib-macos
	@echo "Building macOS GUI app ..."
	@if command -v xcodebuild >/dev/null 2>&1; then \
		xcodebuild -project $(GUI_MACOS_DIR)/ClientHub.xcodeproj \
			-scheme ClientHub -configuration Release build; \
	else \
		echo "  xcodebuild not found — open $(GUI_MACOS_DIR)/ClientHub.xcodeproj in Xcode to build"; \
	fi

clean:
	rm -rf bin $(DIST_DIR) build/libclienthub.*
