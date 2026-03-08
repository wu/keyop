// Package notify implements a service that converts messages arriving on the configured 'alerts'
// channel into pop-up system notifications. The service will alert with the content of the 'Text' field
// if it exists.  It will include the timestamp in the notification text and the service/host in the
// notification title to provide context.
//
// Currently, this service only works on macOS.
//
// Configuration
// - notify_command: optional string, path, or name of the helper executable to run (default: keyop-notify)
// - rate_limit_per_minute: optional integer controlling per-minute notification rate (default: 5)
//
// Rate limiting
//   - The service supports a per-minute rate limit controlled by the configuration key
//     `rate_limit_per_minute` (integer). If not specified, the default is 5 events per minute.
//   - The limiter uses a rolling 60-second window divided into 10 buckets (6s each). Events are
//     counted into the current bucket; when the total across all buckets exceeds the configured
//     limit, further incoming messages are dropped until the window advances.
//   - When the rate limit is first exceeded, the service emits a "rate_limit" event with a short
//     summary indicating that alerts were skipped. Subsequent dropped events do not re-emit the
//     summary until an allowed event resets the warning state.
//
// # MACOS SPECIFIC NOTES
//
// The service uses applescript to display notifications on macOS.  There is a limitation to applescript,
// though, in that it doesn't support attaching an icon to the notification.  To work around this, the service
// supports executing an external helper command (default name: `keyop-notify`) which can use the native
// UserNotifications framework to display notifications with an attached icon.
//
// The helper command can be compiled from the included Go source:
//
//	 # build the helper command from the included Go source (or provide your own that accepts the same args)
//	 make build-notify-sender
//
//	# sign and copy into /Applications to allow execution from the service without additional permissions
//	make deploy-notify-sender
//
// Once signed and installed, the helper can be exercised directly from the command line to test.
//
//	open /Applications/keyop-notify.app --args --title "Test Title" --body "Test Body"
//
// Note that if you make some changes to the helper (icon) and rebuild it, you may stop seeing notifications.
// If that happens, you will need to go into Preferences => Notifications, find "keyop-notify" in the list of apps,
// right click, and choose "reset notifications" to remove it.  Then run the "open" command
// above to send a notification, and it will re-register the helper with the system and allow it to display notifications.
//
// If the helper command is not present or fails to execute, the service will fall back to using applescript
// directly, but the notifications will use the 'Script Editor' icon.
package notify
