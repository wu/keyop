// Package logicalday provides utilities for calculating logical days,
// which are useful for users who work past midnight.
//
// A logical day is a calendar day that ends at a configurable time (e.g., 4am)
// rather than at midnight. This is useful for shift workers and night owls
// who want to group their work by sleep cycles rather than calendar dates.
//
// For example, with a logical day end-of-day setting of 4am:
//   - A task scheduled at 2am Saturday is considered part of Friday's logical day
//   - A task scheduled at 5am Saturday is considered part of Saturday's logical day
//   - A task with no specific time (scheduled_time=false) uses its date as-is
package logicalday
