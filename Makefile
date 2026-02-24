SHELL := /bin/bash

.PHONY: all build build-main build-plugins plugins clean clean-main clean-plugins

OUTPUT_DIR := output

GIT_BRANCH := $(shell git rev-parse --abbrev-ref HEAD 2>/dev/null)
GIT_HASH := $(shell git rev-parse --short HEAD 2>/dev/null)
GIT_VERSION := $(shell git describe --tags --always --dirty 2>/dev/null)
BUILD_TIME := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -X keyop/cmd.Branch=$(GIT_BRANCH) -X keyop/cmd.Commit=$(GIT_HASH) -X keyop/cmd.Version=$(GIT_VERSION) -X keyop/cmd.BuildTime=$(BUILD_TIME)

MAIN_TARGETS := \
	$(OUTPUT_DIR)/keyop-darwin-arm64 \
	$(OUTPUT_DIR)/keyop-darwin-amd64 \
	$(OUTPUT_DIR)/keyop-linux-amd64 \
	$(OUTPUT_DIR)/keyop-linux-arm64 \
	$(OUTPUT_DIR)/keyop-linux-arm

DEFAULT_PLUGINS := helloWorldPlugin homekitPlugin rgbMatrix
PLUGINS ?=
PLUGINS_LIST := $(if $(strip $(PLUGINS)),$(PLUGINS),$(DEFAULT_PLUGINS))

all: build

ifeq ($(strip $(PLUGINS)),)
build: build-main
else
build: build-main build-plugins
endif

build-main: $(MAIN_TARGETS)

$(OUTPUT_DIR):
	mkdir -p $(OUTPUT_DIR)

$(OUTPUT_DIR)/keyop-darwin-arm64: | $(OUTPUT_DIR)
	@echo "Building keyop for macos arm"
	GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $@

$(OUTPUT_DIR)/keyop-darwin-amd64: | $(OUTPUT_DIR)
	@echo "Building keyop for macos intel"
	GOOS=darwin GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $@

$(OUTPUT_DIR)/keyop-linux-amd64: | $(OUTPUT_DIR)
	@echo "Building keyop for linux amd64"
	GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $@

$(OUTPUT_DIR)/keyop-linux-arm64: | $(OUTPUT_DIR)
	@echo "Building keyop for linux arm64"
	GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $@

$(OUTPUT_DIR)/keyop-linux-arm: | $(OUTPUT_DIR)
	@echo "Building keyop for linux arm"
	GOARM=6 GOOS=linux GOARCH=arm go build -ldflags "$(LDFLAGS)" -o $@

build-plugins:
	@set -e; \
	for p in $(PLUGINS_LIST); do \
		echo "Building plugin $$p"; \
		$(MAKE) -C plugins/$$p build; \
	done

plugins: PLUGINS=$(DEFAULT_PLUGINS)
plugins: build-plugins

clean: clean-main clean-plugins

clean-main:
	rm -f $(MAIN_TARGETS)

clean-plugins:
	@set -e; \
	for p in $(PLUGINS_LIST); do \
		echo "Cleaning plugin $$p"; \
		$(MAKE) -C plugins/$$p clean; \
	done

