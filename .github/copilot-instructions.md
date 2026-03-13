# Copilot Instructions for keyop

## Build, Test, and Lint Commands

- **Build main binaries (no plugins):**
  ```sh
  make build
  ```
- **Build all default plugins:**
  ```sh
  make plugins
  ```
- **Build specific plugins:**
  ```sh
  make build-plugins PLUGINS="rgbMatrix helloWorldPlugin"
  ```
- **Build main binaries and specific plugins:**
  ```sh
  make build PLUGINS="rgbMatrix helloWorldPlugin"
  ```
- **Build macOS Reminders helper (macOS only):**
  ```sh
  make build-reminders-fetcher
  ```
- **Build release artifacts (includes macOS helper):**
  ```sh
  make build-release
  ```
- **Run all tests:**
  ```sh
  make test
  # or
  go test ./...
  ```
- **Run tests for a single package:**
  ```sh
  go test ./x/macosReminders
  ```
- **Lint (requires golangci-lint):**
  ```sh
  make lint
  ```
- **Auto-fix lint and format:**
  ```sh
  make lint-fix
  ```
- **Format code:**
  ```sh
  make fmt
  ```

## High-Level Architecture

- **Core**: Implements the event-driven framework, including service lifecycle, message passing, persistent state, and
  plugin interfaces.
- **Plugins**: Extend functionality (e.g., Homekit, RGB matrix, macOS Reminders, Bluetooth battery) and are built as Go
  plugins (`.so` files) loaded at runtime.
- **Services**: Each service implements the `Service` interface (`Check`, `ValidateConfig`, `Initialize`).
- **Messenger**: Handles message routing between services and plugins.
- **State Store**: Persists service/plugin state to disk (default: `~/.keyop/data`).
- **Terminal UI**: Optional TUI for monitoring system state.
- **Web UI**: Optional web interface for monitoring and configuration, served from static assets.
- **Docker**: Multi-stage Dockerfiles for building and running minimal images, with support for plugins and web UI
  static assets.

## Key Conventions

- **Plugin Build**: Each plugin has its own `Makefile` with `build` and `clean` targets. Use
  `make build-plugins PLUGINS="plugin1 plugin2"` to build specific plugins.
- **Configuration**: YAML config files, typically under `~/.keyop/conf` or as specified in Docker images.
- **macOS Reminders**: Requires a Swift helper binary, built and run only on macOS 14+.
- **Message Format**: All inter-service/plugin messages use the `core.Message` struct (see `core/messenger.go`).
- **Persistent State**: Services/plugins persist state using the `StateStore` interface (see `core/state.go`).
- **Testing**: Integration tests for platform-specific plugins (e.g., macOS Reminders) require the helper binary and are
  skipped in CI.
- **Logging**: Uses `slog` with color output in console mode, or logs to `~/.keyop/logs` by default.
- **Timezone**: Defaults to America/Los_Angeles; falls back to UTC if unavailable.
- **Web UI**: Static assets are copied to `/webui-static` in Docker images.

## Service Structure

- Store main service code in "service.go"
- Store sqlite code in "sqlite.go"
- Store web server code in "webui.go""
- Extract reusable code into domain-specific files (e.g., "aurora.go" for aurora-related functionality).
- Store package-level docs in "doc.go" with a package comment.
- Store payload definitions in "payloads.go"

## Policy

Policy: Never run git commands (add/commit/push/branch/tag) or modify the repository without explicit, written approval
from the repo owner. If a task requires git, ask first and wait for a clear yes.

Policy: Never modify the configuration without explicit, written approval from the repo owner.

Policy: Do not look for server process ids or attempt to kill processes unless you are explicitly asked to do so by the
repo owner as part of a specific task. You can make requests to the server process. Ask me and I will provide the
local port.



---

This file summarizes build/test/lint commands, architecture, and key conventions for Copilot and other AI tools. Would
you like to adjust anything or add coverage for additional areas?
