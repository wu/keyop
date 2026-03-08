// Package notify converts incoming 'alerts' channel messages into native desktop
// notifications.  It supports optional helper binaries for macOS that leverage the
// UserNotifications framework and can display icons.
//
// Overview
// The notify service subscribes to the configured 'alerts' channel and transforms
// incoming core.Message objects into system notifications. The body of the
// notification contains the message text and a short timestamp; the title includes
// the originating service and host.
//
// Configuration
// - notify_command: optional string, path or executable name for the notification helper (default: keyop-notify)
// - notification_icon: optional string, path to an icon to attach to notifications
// - rate_limit_per_minute: optional integer controlling per-minute notification rate (default: 5)
//
// Behavior
//   - If a helper binary (e.g., /Applications/keyop-notify.app or a configured notify_command)
//     is available it is preferred; otherwise the service falls back to applescript.
//   - Rate limiting uses a rolling 60s window split into 10 buckets; on first drop a
//     "rate_limit" event is emitted.
//
// macOS Notes
//   - To have permission to post notifications via UserNotifications, helper apps must be
//     signed and installed in /Applications so macOS can present the permission prompt.
//   - The included helper builder (make build-notify-sender and make deploy-notify-sender)
//     can produce such a helper; see project docs.
package notify
