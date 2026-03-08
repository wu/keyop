// Package speak implements a service that converts incoming messages into spoken audio.
// The service will speak the 'Summary' field if it exists, or the 'Text' field if the
// 'Summary' field is empty.
//
// Currently, it only works on macOS, as it relies on the 'say' command to speak text.
// The service validates that it runs on Darwin (macOS) and will return a configuration error
// on other platforms.
//
// When 'say' exits with success, a "speech" event (payload type service.speech.v1) will be emitted
// with the spoken text in the Message text.  If there is an error returned from the say command,
// an error event will be emitted with the error details.
//
// Rate limiting
//   - The service supports a per-minute rate limit controlled by the configuration key
//     `rate_limit_per_minute` (integer). If not specified, the default is 5 events per minute.
//   - The limiter uses a rolling 60 second window divided into 10 buckets (6s each). Events are
//     counted into the current bucket; when the total across all buckets exceeds the configured
//     limit, further incoming messages are dropped until the window advances.
//   - When the rate limit is first exceeded, the service emits a "rate_limit" event with a short
//     summary indicating that alerts were skipped. Subsequent dropped events do not re-emit the
//     summary until an allowed event resets the warning state.
//
// # MACOS SPECIFIC NOTES
//
// To use the higher quality siri voices on macOS, it uses the default "system voice"
// setting.  While it is possible to specify a voice to the 'say' command, the choices are
// limited and don't include the highest quality voices.
//
//   - First, in "Apple Intelligence and Siri", select your preferred Siri voice.
//   - Second, in System Preferences > Accessibility => "Read and Speak", set the system voice.
//
// Unfortunately, the exact steps to configure this vary by macOS version.  These instructions are
// for Tahoe.  Try to search in Preferences for 'voice', look for something like "Voice (spoken content)".
// In the system voice drop-down, choose the siri voice option, mine was near the top and was
// named "Siri (Voice 2)".
package speak
