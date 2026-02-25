macosReminders plugin

This service reads reminders from macOS Reminders (a specified list, default `Inbox`) and publishes each reminder as a
message to the configured `task` channel.

Summary

- The plugin uses a small Swift helper (EventKit) that fetches reminders and emits one JSON object per line.
- The plugin requires the helper binary at runtime (no AppleScript fallback).
- The plugin persists seen reminders in the configured state store to avoid duplicate messages and to detect removals.

Config example (YAML)

- name: macosReminders
  type: macosReminders
  pubs:
  task:
  name: task
  description: Reminders task channel
  config:
  inbox_name: Inbox # optional; default: Inbox
  only_uncompleted: true # optional; default: true

Important: the channel is `task` (singular) — the service publishes reminder events on that channel.

Build & packaging

A small Swift helper implements the Reminders fetch using EventKit. It must be compiled on macOS.

Build the helper manually:

```bash
cd x/macosReminders/cmd/reminders_fetcher
swiftc -o reminders_fetcher main.swift
# produces x/macosReminders/cmd/reminders_fetcher/reminders_fetcher
```

Makefile targets (preferred)

- Build helper only (macOS):

```bash
make build-reminders-fetcher
```

- Build release artifacts (cross-builds for main targets) and package the macOS helper into `output/`:

```bash
make build-release
```

This copies the helper to `output/reminders_fetcher` on macOS so release bundles can include it.

How the plugin locates the helper

- The plugin looks for the helper in the following order:
  1. Sibling to the running executable (same directory as the running binary)
  2. `./reminders_fetcher` (working directory)
  3. `reminders_fetcher` on PATH

If the helper cannot be found or fails, the plugin will log an error and `Check()` will no-op.

Configuration: helper_path

If you prefer to place the compiled `reminders_fetcher` helper in a custom location, you can set the `helper_path`
config key to the absolute or relative path to the binary. When set, the plugin will prefer this path and fail if the
file does not exist.

Example:

- name: macosReminders
  type: macosReminders
  pubs:
  task:
  name: task
  config:
  helper_path: /usr/local/bin/reminders_fetcher
  inbox_name: Inbox
  only_uncompleted: true

Note: if `helper_path` is not present, the plugin will search common locations: sibling to the running binary,
`./reminders_fetcher`, and PATH.

Usage and permissions

- First run (interactive): run the helper manually once to trigger the macOS Reminders permission prompt and grant
  access:

```bash
# from the helper directory or copied output directory
./x/macosReminders/cmd/reminders_fetcher/reminders_fetcher --list Inbox --only-uncompleted
```

- After permission is granted, the helper can be run non-interactively by the Go service.
- The helper accepts:
  - `--list <LISTNAME>` (optional) — select a specific reminders list; defaults to all lists if omitted
  - `--only-uncompleted` (optional flag) — only return uncompleted reminders
  - `--timeout <seconds>` (optional) — control request/fetch timeouts (default 10)

Behavior & message format

- On first sighting of a reminder (not present in the persisted state), the plugin sends a message to the `task` channel
  with `Data.event = "created"`.
- When a previously-seen reminder disappears from the list, the plugin sends a `Data.event = "removed"` message and
  removes it from persisted state.
- Message fields include `id`, `title`, `notes`, `due_raw` (ISO8601 when available from the helper), `completed`, and
  `inbox`.

Integration tests (macOS only)

- There is a darwin-only integration test in `x/macosReminders/service_integration_test.go`.
- Requirements to run it locally:
  1. Run on macOS (darwin build tag)
  2. Build the helper first with `make build-reminders-fetcher` (or `swiftc` as above)
  3. Do NOT run in CI (the test skips when `CI=true`)

Run the integration test manually:

```bash
# build the helper first
make build-reminders-fetcher

# run the package tests (integration test will run on darwin and when helper is present)
go test ./x/macosReminders -v
```

Notes & troubleshooting

- The helper must be compiled on macOS because it links against EventKit.
- If you see an "access not granted" error from the helper, run it interactively from Terminal and grant Reminders
  permission in the macOS prompt or System Settings -> Privacy -> Reminders.
- If you prefer the plugin to use a helper from a custom absolute path, you can place the helper next to the running
  binary or add it to PATH. If you want a config flag for the exact path, I can add that option.

Platform requirement

Note: the Swift helper now calls the macOS 14+ Reminders API (`requestFullAccessToRemindersWithCompletion`) directly.
The helper therefore requires macOS 14 or later to compile and run. If you need to support older macOS versions, we can
reintroduce a fallback path, but the current implementation drops backward compatibility to keep the helper code simpler
and more reliable.

History

- Former AppleScript fallback was removed for reliability and performance; the Swift EventKit helper is the supported
  mechanism.
