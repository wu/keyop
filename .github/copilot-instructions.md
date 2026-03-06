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
- **Camel Case**: Treat acronyms as normal words (e.g., `HtmlClient`, not `HTMLClient`) and use "Id" instead of "ID" for
  identifiers.

## additional notes

Please don't add or stage any files in git or make any commits without discussing it with me first.

Never modify the configuration without discussing it with me first.



---

This file summarizes build/test/lint commands, architecture, and key conventions for Copilot and other AI tools. Would
you like to adjust anything or add coverage for additional areas?
