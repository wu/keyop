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
- **Build macOS notify app bundle (macOS only):**
  ```sh
  make build-notify-sender
  make deploy-notify-sender  # installs to /Applications
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
  go test ./x/reminders
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
- **Run messenger benchmark:**
  ```sh
  make bench MESSAGES=10000
  ```
- **Generate package docs (requires gomarkdoc):**
  ```sh
  make docs
  ```

## High-Level Architecture

- **Core** (`core/`): Implements the event-driven framework, including service lifecycle, message passing, persistent
  queues, payload registry, preprocessing, and dependency injection.
- **Services** (`x/`): All built-in services live under `x/` as regular Go packages. Each service implements the
  `Service` interface (`Check`, `ValidateConfig`, `Initialize`). Examples include monitors (CPU, disk, memory, ping,
  SSL), integrations (weather, tides, Kodi, Slack, GitHub, RSS), macOS-specific services (Reminders, Bluetooth
  battery, idle, notify, txtmsg, speak), and web UI tabs (notes, tasks, flashcards, movies, attachments, journal).
- **Plugins** (`plugins/`): Optional runtime extensions (e.g., Homekit, RGB matrix, helloWorld) built as Go plugins
  (`.so` files) and loaded at runtime. Each has its own `Makefile`.
- **Messenger** (`core/messenger.go`): Routes `core.Message` structs between services via named channels backed by a
  `PersistentQueue`. Supports retry with exponential back-off, a dead-letter queue (DLQ), stats, and a
  `PayloadRegistry` for typed message payloads.
- **Persistent Queue** (`core/queue.go`): Durable, file-backed message queue supporting multiple concurrent readers
  with ack/seek semantics. Backs every channel in the Messenger.
- **Payload Registry** (`core/payload.go`): Thread-safe registry mapping `DataType` strings to typed Go structs.
  Services register their payload types at init time; the Messenger decodes them automatically.
- **Preprocessing** (`core/preprocess.go`, `core/preprocess_messenger.go`): `PreprocessMessenger` wraps
  `MessengerApi` and applies configurable `sub_preprocess` / `pub_preprocess` condition rules — filtering,
  transforming, or re-routing messages without changing service code.
- **Dependencies** (`core/dependencies.go`): Struct-based dependency injection container carrying the logger,
  OS provider, messenger, state store, and context/cancel for each service.
- **State Store** (`core/state.go`): `FileStateStore` persists JSON state per service to `~/.keyop/data`.
- **Terminal UI** (`cmd/tui.go`): Optional TUI for monitoring system state.
- **Web UI** (`x/webui/`): Optional web interface for monitoring and configuration, served from embedded static
  assets. Individual services contribute tabs via the web UI extension points.
- **WebSocket transport** (`x/webSocketClient`, `x/webSocketServer`, `x/webSocketProtocol`): Bridges channels
  across hosts using a shared wire protocol defined in `x/webSocketProtocol`.
- **Self-update** (`cmd/selfupdate.go`): Built-in self-update command for downloading new releases.
- **Docker**: Multi-stage Dockerfiles for building and running minimal images, with support for plugins and web UI
  static assets.

## Key Conventions

- **Plugin Build**: Each plugin has its own `Makefile` with `build` and `clean` targets. Use
  `make build-plugins PLUGINS="plugin1 plugin2"` to build specific plugins.
- **Configuration**: YAML config files, typically under `~/.keyop/conf` or as specified in Docker images.
- **macOS Reminders**: The `x/reminders` package requires a Swift helper binary (`reminders_fetcher`), built with
  `make build-reminders-fetcher` and run only on macOS 14+.
- **macOS Notifications**: The `x/notify` package uses a native app bundle (`keyop-notify.app`). Build with
  `make build-notify-sender` and install with `make deploy-notify-sender` (macOS only).
- **Message Format**: All inter-service/plugin messages use the `core.Message` struct (see `core/messenger.go`).
  Legacy envelope format is handled automatically for backward compatibility.
- **Typed Payloads**: Services register typed payload structs via `PayloadRegistry`. Set `Message.DataType` and
  `Message.Data` to send; the registry decodes on receipt. Register payloads in `payloads.go` at package `init`.
- **Persistent State**: Services/plugins persist state using the `StateStore` interface (see `core/state.go`).
- **Testing**: Integration tests for platform-specific services (e.g., macOS Reminders) require the helper binary
  and are skipped in CI. Run a single package with `go test ./x/reminders`.
- **Logging**: Uses a `core.Logger` interface (backed by `slog`) with color output in console mode, or logs to
  `~/.keyop/logs` by default.
- **Timezone**: Defaults to America/Los_Angeles; falls back to UTC if unavailable.
- **Web UI**: Static assets are embedded in the binary and copied to `/webui-static` in Docker images.

## Service Structure

- Store main service code in "service.go"
- Store sqlite code in "sqlite.go"
- Store web server code in "webui.go""
- Extract reusable code into domain-specific files (e.g., "aurora.go" for aurora-related functionality).
- Store package-level docs in "doc.go" with a package comment.
- Store payload definitions and registration in "payloads.go"
- Store utilities that are used by multiple packages in a file with the same name as the package, e.g. "sun/sun.go"
- Store html and css in package subdirectory 'resources'

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
