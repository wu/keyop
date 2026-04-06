SHELL := /bin/bash

.PHONY: all build build-main build-plugins plugins clean clean-main clean-plugins build-reminders-fetcher clean-reminders-fetcher build-release fmt lint lint-fix test

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

# Build the Swift reminders_fetcher helper only on macOS
build-reminders-fetcher:
	@if [ "$(shell uname -s)" = "Darwin" ]; then \
		echo "Building reminders_fetcher (macOS only)"; \
		swiftc -o x/reminders/cmd/reminders_fetcher/reminders_fetcher x/reminders/cmd/reminders_fetcher/main.swift; \
	else \
		echo "Skipping reminders_fetcher build: not macOS"; \
	fi

# Build the macOS notify helper as a separate app bundle (keyop-notify.app)
build-notify-sender:
	@bash ./scripts/build-notify-sender.sh

# Deploy the keyop-notify app into /Applications and sign ad-hoc so macOS recognizes it
.PHONY: deploy-notify-sender
deploy-notify-sender:
	@if [ "$(shell uname -s)" = "Darwin" ]; then \
		set -e; \
		$(MAKE) build-notify-sender; \
		APP_SRC="x/notify/cmd/notify-sender/keyop-notify.app"; \
		APP_DST="/Applications/keyop-notify.app"; \
		if [ -d "$$APP_SRC" ]; then \
			echo "Signing $$APP_SRC (ad-hoc)"; \
			codesign -s - --force --timestamp=none "$$APP_SRC" || true; \
			echo "Installing to $$APP_DST"; \
			rm -rf "$$APP_DST"; \
			cp -R "$$APP_SRC" "/Applications/"; \
			echo "Installed $$APP_DST"; \
		else \
			echo "App bundle not found at $$APP_SRC"; \
			exit 1; \
		fi; \
	else \
		echo "deploy-notify-sender is macOS-only"; \
	fi

# Build release artifacts and package the macOS helper into $(OUTPUT_DIR)
build-release: build-main build-reminders-fetcher
	@if [ "$(shell uname -s)" = "Darwin" ]; then \
		echo "Packaging reminders_fetcher into $(OUTPUT_DIR)"; \
		mkdir -p $(OUTPUT_DIR); \
		cp x/reminders/cmd/reminders_fetcher/reminders_fetcher $(OUTPUT_DIR)/reminders_fetcher || true; \
	else \
		echo "Skipping packaging helper: not macOS"; \
	fi

clean: clean-main clean-plugins clean-reminders-fetcher

clean-main:
	rm -f $(MAIN_TARGETS)

clean-plugins:
	@set -e; \
	for p in $(PLUGINS_LIST); do \
		echo "Cleaning plugin $$p"; \
		$(MAKE) -C plugins/$$p clean; \
	done

clean-reminders-fetcher:
	@echo "Removing reminders_fetcher binary if present"; \
	rm -f x/reminders/cmd/reminders_fetcher/reminders_fetcher

# Formatting, linting and test helpers
fmt:
	@gofmt -w -s . || true
	@command -v goimports >/dev/null 2>&1 && goimports -w . || true

lint:
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "golangci-lint not available; install it to run full lint checks"; \
	fi

lint-fix: fmt
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run --fix ./...; \
	else \
		echo "golangci-lint not available; run 'make fmt' and install golangci-lint to do --fix"; \
	fi

# Run the benchmark for messenger throughput. Example:
#   make bench MESSAGES=10000
# Defaults to running with 10000 messages if MESSAGES is not set.
bench:
	@MESSAGES=$${MESSAGES:-10000}; \
	echo "Running messenger benchmark with $$MESSAGES messages"; \
	go test ./core -bench BenchmarkMessengerThroughput -benchmem -run TestNone -v -count=1 -benchtime=$${MESSAGES}x

test:
	@go test ./...

.PHONY: docs
docs:
	@command -v gomarkdoc >/dev/null 2>&1 || (echo "Installing gomarkdoc"; GO111MODULE=on go install github.com/princjef/gomarkdoc/cmd/gomarkdoc@latest)
	@mkdir -p docs
	@set -e; for pkg in $(shell go list ./...); do \
		fname=$$(echo $$pkg | sed 's|/|_|g'); \
		echo "Generating docs for $$pkg -> docs/$$fname.md"; \
		gomarkdoc $$pkg -u -o docs/$$fname.md || echo "gomarkdoc failed for $$pkg"; \
	done

# Docker convenience targets
.PHONY: docker-build docker-push docker-run

DOCKER_IMAGE ?= ghcr.io/wu/keyop
DOCKER_TAG ?= latest

docker-build:
	@./scripts/docker-build.sh $(DOCKER_IMAGE) $(DOCKER_TAG)

docker-build-debug:
	@./scripts/docker-build.sh $(DOCKER_IMAGE) $(DOCKER_TAG) Dockerfile.debug

docker-push: docker-build
	@echo "Pushing docker image $(DOCKER_IMAGE):$(DOCKER_TAG)"
	docker push $(DOCKER_IMAGE):$(DOCKER_TAG)

docker-run:
	docker run --rm -it $(DOCKER_IMAGE):$(DOCKER_TAG) $(ARGS)
