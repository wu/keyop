// Package reminders integrates with the macOS Reminders app to fetch and publish task creation events.
//
// The service polls a specified Reminders list (default: "Inbox") using a small Swift helper built on
// EventKit. When a new reminder appears, the service publishes a [tasks.TaskCreateEvent] to the "tasks"
// channel so the tasks service can insert it automatically. When a previously-seen reminder disappears,
// a [ReminderRemovedEvent] is published to the "reminders" channel.
//
// # Architecture
//
// The service uses a Swift helper binary (reminders_fetcher) that fetches reminders via EventKit and
// emits one JSON object per line. The Go service diffs the current list against persisted state to
// detect additions and removals. State is persisted via the configured state store to survive restarts
// without re-emitting events.
//
// # Event flow
//
//   - New reminder detected → [tasks.TaskCreateEvent] sent to channel "tasks"
//   - Reminder removed → [ReminderRemovedEvent] sent to channel "reminders"
//
// The tasks service subscribes to the "tasks" channel and inserts the task into its database on receipt.
// If the reminder has a specific time (not just a date), HasScheduledTime is set to true so the task
// displays a time alongside the date.
//
// # Typed payloads
//
// The service implements [core.PayloadProvider] (Name + RegisterPayloads). Registered types:
//
//   - "service.tasks.create.v1"    → [tasks.TaskCreateEvent] (published on new reminder)
//   - "service.reminders.removed.v1" → [ReminderRemovedEvent] (published on removal)
//
// # Configuration (YAML)
//
//	name: reminders
//	type: reminders
//	freq: 1m
//	config:
//	  inbox_name: Inbox           # optional; default: Inbox
//	  only_uncompleted: true      # optional; default: true
//	  user_id: 1                  # optional; assigned to created tasks
//	  helper_path: /path/to/reminders_fetcher  # optional; falls back to PATH / sibling binary
//
// # Building the Swift helper
//
// The helper must be compiled on macOS (links against EventKit, requires macOS 14+):
//
//	make build-reminders-fetcher
//	# or manually:
//	swiftc -o x/reminders/cmd/reminders_fetcher/reminders_fetcher \
//	        x/reminders/cmd/reminders_fetcher/main.swift
//
// # Permissions
//
// On first run, execute the helper interactively from Terminal to trigger the macOS Reminders
// permission prompt, then grant access in System Settings → Privacy → Reminders:
//
//	./x/reminders/cmd/reminders_fetcher/reminders_fetcher --list Inbox --only-uncompleted
//
// # Integration tests
//
// A darwin-only integration test lives in service_integration_test.go. It requires the helper binary
// and is skipped in CI (CI=true). Run locally with:
//
//	make build-reminders-fetcher
//	go test ./x/reminders/... -v
package reminders
