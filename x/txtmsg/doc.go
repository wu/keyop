// Package txtmsg converts configured text alerts into macOS Messages app text messages.
//
// Overview
// The messages package subscribes to a configured channel (usually 'alerts') and sends
// text messages to a configured macOS Messages buddy using osascript. On successful sends
// the service emits a 'txtmsg' event; on failure it emits a 'txtmsg_error' event.
//
// Configuration
// The service supports the following config keys:
//   - address: string (required)
//     The Messages buddy identifier to send texts to.
//
// Payloads
// The package registers a small typed payload for text events:
//   - text / service.txtmsg.v1: contains Now timestamp and the text summary.
//
// Events
//   - txtmsg: Emitted after a successful delivery; includes the text in the Text field
//     and a service.txtmsg.v1 payload in Data.
//   - txtmsg_error: Emitted when sending fails; includes error information in Text and Status.
//
// Rate limiting
//   - The service supports a per-minute rate limit controlled by the configuration key
//     `rate_limit_per_minute` (integer). If not specified, the default is 5 events per minute.
//   - The limiter uses a rolling 60-second window divided into 10 buckets (6s each). Events are
//     counted into the current bucket; when the total across all buckets exceeds the configured
//     limit, further incoming messages are dropped until the window advances.
//   - When the rate limit is first exceeded, the service emits a "txtmsg_rate_limit" event with a short
//     summary indicating that alerts were skipped. Subsequent dropped events do not re-emit the
//     summary until an allowed event resets the warning state.
package txtmsg
