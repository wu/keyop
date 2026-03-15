// Package recurrence provides recurrence pattern handling for recurring tasks.
//
// Supported recurrence patterns:
//   - Daily: Every N days
//   - Weekly: On specific days of the week (Monday, Tuesday, etc.)
//   - Monthly: On a specific day of the month
//   - Yearly: On a specific month and day
//
// Example usage:
//
//	pattern := &Pattern{
//	    Type:     "weekly",
//	    Interval: 1,
//	    DaysOfWeek: []time.Weekday{time.Monday, time.Friday},
//	}
//	nextDate := pattern.Next(time.Now())
package recurrence
