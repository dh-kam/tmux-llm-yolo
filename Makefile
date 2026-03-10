GO := go
APP := tmux-llm-yolo
MODULE := github.com/dh-kam/tmux-llm-yolo
OUTPUT_DIR := build
DIST_DIR := dist
VERSION_FILE := internal/buildinfo/version.go
VERSION ?= $(shell sed -n 's/^[[:space:]]*Version[[:space:]]*=[[:space:]]*"\(.*\)"/\1/p' $(VERSION_FILE))
GIT_COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || printf 'dev')
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

GOFLAGS_debug := -gcflags "all=-N -l"
GOFLAGS_release := -trimpath
LDFLAGS_common := -X '$(MODULE)/internal/buildinfo.Version=$(VERSION)' -X '$(MODULE)/internal/buildinfo.GitCommit=$(GIT_COMMIT)' -X '$(MODULE)/internal/buildinfo.BuildDate=$(BUILD_DATE)'
LDFLAGS_debug := $(LDFLAGS_common)
LDFLAGS_release := -s -w $(LDFLAGS_common)

rwildcard = $(wildcard $(1)$(strip $(2))) $(foreach d,$(wildcard $(1)*/),$(call rwildcard,$(d),$(strip $(2))))
SRC_FILES := $(call rwildcard,./,*.go)

.ONESHELL:
.DEFAULT_GOAL := help

all: linux

test:
	$(GO) test ./...

fmt:
	$(GO) fmt ./...

print-version:
	@printf '%s\n' '$(VERSION)'

linux: linux-amd64-debug linux-amd64-release linux-arm64-debug linux-arm64-release
linux-amd64: linux-amd64-debug linux-amd64-release
linux-arm64: linux-arm64-debug linux-arm64-release
linux-debug: linux-amd64-debug linux-arm64-debug
linux-release: linux-amd64-release linux-arm64-release
release-artifacts: $(DIST_DIR)/$(APP)_$(VERSION)_linux_amd64_debug.tar.gz $(DIST_DIR)/$(APP)_$(VERSION)_linux_amd64_release.tar.gz $(DIST_DIR)/$(APP)_$(VERSION)_linux_arm64_debug.tar.gz $(DIST_DIR)/$(APP)_$(VERSION)_linux_arm64_release.tar.gz

linux-amd64-debug: $(OUTPUT_DIR)/linux-amd64/debug/$(APP)
linux-amd64-release: $(OUTPUT_DIR)/linux-amd64/release/$(APP)
linux-arm64-debug: $(OUTPUT_DIR)/linux-arm64/debug/$(APP)
linux-arm64-release: $(OUTPUT_DIR)/linux-arm64/release/$(APP)

$(OUTPUT_DIR)/linux-amd64/debug/$(APP): $(SRC_FILES)
	mkdir -p $(@D)
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 $(GO) build $(GOFLAGS_debug) -ldflags "$(LDFLAGS_debug)" -o $@ .

$(OUTPUT_DIR)/linux-amd64/release/$(APP): $(SRC_FILES)
	mkdir -p $(@D)
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 $(GO) build $(GOFLAGS_release) -ldflags "$(LDFLAGS_release)" -o $@ .

$(OUTPUT_DIR)/linux-arm64/debug/$(APP): $(SRC_FILES)
	mkdir -p $(@D)
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 $(GO) build $(GOFLAGS_debug) -ldflags "$(LDFLAGS_debug)" -o $@ .

$(OUTPUT_DIR)/linux-arm64/release/$(APP): $(SRC_FILES)
	mkdir -p $(@D)
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 $(GO) build $(GOFLAGS_release) -ldflags "$(LDFLAGS_release)" -o $@ .

$(DIST_DIR)/$(APP)_$(VERSION)_linux_amd64_debug.tar.gz: $(OUTPUT_DIR)/linux-amd64/debug/$(APP)
	mkdir -p $(DIST_DIR)
	tar -C $(OUTPUT_DIR)/linux-amd64/debug -czf $@ $(APP)

$(DIST_DIR)/$(APP)_$(VERSION)_linux_amd64_release.tar.gz: $(OUTPUT_DIR)/linux-amd64/release/$(APP)
	mkdir -p $(DIST_DIR)
	tar -C $(OUTPUT_DIR)/linux-amd64/release -czf $@ $(APP)

$(DIST_DIR)/$(APP)_$(VERSION)_linux_arm64_debug.tar.gz: $(OUTPUT_DIR)/linux-arm64/debug/$(APP)
	mkdir -p $(DIST_DIR)
	tar -C $(OUTPUT_DIR)/linux-arm64/debug -czf $@ $(APP)

$(DIST_DIR)/$(APP)_$(VERSION)_linux_arm64_release.tar.gz: $(OUTPUT_DIR)/linux-arm64/release/$(APP)
	mkdir -p $(DIST_DIR)
	tar -C $(OUTPUT_DIR)/linux-arm64/release -czf $@ $(APP)

clean:
	rm -rf $(OUTPUT_DIR) $(DIST_DIR)

help:
	@echo "Usage:"
	@echo "  make test"
	@echo "  make linux"
	@echo "  make linux-amd64-debug"
	@echo "  make linux-amd64-release"
	@echo "  make linux-arm64-debug"
	@echo "  make linux-arm64-release"
	@echo "  make release-artifacts"
	@echo "  make print-version"
	@echo "Variables:"
	@echo "  VERSION=$(VERSION)"
	@echo "  GIT_COMMIT=$(GIT_COMMIT)"
	@echo "  BUILD_DATE=$(BUILD_DATE)"

.PHONY: all clean fmt help linux linux-amd64 linux-amd64-debug linux-amd64-release linux-arm64 linux-arm64-debug linux-arm64-release linux-debug linux-release print-version release-artifacts test
