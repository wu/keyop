# Logical Day Concept

## Overview

A **logical day** is a calendar day that ends at a configurable time rather than at midnight. This concept is useful for
people who work irregular hours or sleep after midnight.

## Motivation

Many people don't organize their work by calendar days. Instead, they organize by sleep cycles:

- Night shift workers
- Early morning workers (e.g., bakers, farmers)
- Night owls who work until early morning

For example, a baker might work a shift from 10pm to 6am. For them:

- A task scheduled at 2am should be considered part of "today" (their workday)
- Not part of tomorrow (the calendar day)

Without the logical day concept, tasks would be grouped by calendar date, which doesn't align with how these users
naturally think about their work.

## Configuration

The logical day end-of-day time is configured globally in the keyop configuration:

```yaml
logicalDayEndOfDay: "04:00"  # Tasks before 4am belong to yesterday's logical day
```

Format: `HH:MM` or `HH:MM:SS` in 24-hour time
Default: `04:00` (4am)
Timezone: User's local timezone

## How It Works

### Two Types of Scheduled Tasks

Tasks in the system have a `scheduled_time` flag that determines how their logical day is calculated:

#### 1. Tasks with No Specific Time (`scheduled_time = false`)

These are all-day tasks scheduled for a particular date. For example:

- "Do laundry" scheduled for Saturday, March 14

For these tasks, the logical day is simply the scheduled date itself. The time of day doesn't matter.

**Example:**

```
scheduled_date: 2026-03-14
scheduled_time: false
end-of-day:     04:00

→ Logical day = 2026-03-14 (Saturday)
```

#### 2. Tasks with Specific Times (`scheduled_time = true`)

These are time-specific tasks. For example:

- "Bake bread" scheduled for 2am Saturday, March 14

For these tasks, the logical day is calculated by comparing the scheduled time against the end-of-day setting:

- **If the scheduled time is BEFORE end-of-day**: The task belongs to the PREVIOUS calendar day's logical day
- **If the scheduled time is AT or AFTER end-of-day**: The task belongs to that calendar day's logical day

**Examples with 4am end-of-day:**

```
scheduled_date:   2026-03-14 (Saturday)
scheduled_time:   02:00 (2am)
end-of-day:       04:00 (4am)

2am < 4am → belongs to previous day
→ Logical day = 2026-03-13 (Friday)
```

```
scheduled_date:   2026-03-14 (Saturday)
scheduled_time:   05:00 (5am)
end-of-day:       04:00 (4am)

5am >= 4am → belongs to this day
→ Logical day = 2026-03-14 (Saturday)
```

```
scheduled_date:   2026-03-13 (Friday)
scheduled_time:   23:00 (11pm)
end-of-day:       04:00 (4am)

11pm >= 4am → belongs to this day
→ Logical day = 2026-03-13 (Friday)
```

## Implementation Details

### Database Schema

Tasks table includes:

- `scheduled_date` (DATE) - The date the task is scheduled for
- `scheduled_time` (BOOLEAN) - Whether this task has a specific time
- If `scheduled_time = true`, there should be a time component available for comparison

### Library API

The `logicalday` package provides a `Calculator` for logical day operations:

```go
import "keyop/x/logicalday"

// Create a calculator with 4am end-of-day in the user's timezone
loc, _ := time.LoadLocation("America/Los_Angeles")
calc := logicalday.NewCalculator("04:00:00", loc)

// Get the logical day for a scheduled time
logicalDay := calc.GetLogicalDay(scheduledTime, hasSpecificTime)

// Check if a logical day is today
isTodayTask := calc.IsToday(logicalDay)

// Get the boundaries of the current logical day
start := calc.LogicalTodayStart()  // When does the current logical day start?
end := calc.LogicalTodayEnd()      // When does the current logical day end?
```

### Filtering Tasks for "Today"

When displaying tasks for "today", the system:

1. Determines what "today" means in the user's local timezone
2. For each task, calculates its logical day using `calc.GetLogicalDay(time, hasSpecificTime)`
3. Only displays tasks where `calc.IsToday(logicalDay)` is true

This ensures that tasks scheduled before the end-of-day cutoff are grouped with the previous day's work, not the next
day.

## Examples

### Example 1: Baker's Schedule (4am end-of-day)

Baker starts work at 10pm Friday and finishes at 6am Saturday.

| Task         | Scheduled  | Time         | Logical Day | Why          |
|--------------|------------|--------------|-------------|--------------|
| Mix dough    | 2026-03-13 | 23:00 (11pm) | Fri Mar 13  | 11pm >= 4am  |
| Shape loaves | 2026-03-14 | 02:00 (2am)  | Fri Mar 13  | 2am < 4am    |
| Bake         | 2026-03-14 | 05:00 (5am)  | Sat Mar 14  | 5am >= 4am   |
| Check oven   | 2026-03-14 | No time      | Sat Mar 14  | All-day task |

Result: Baker sees "Friday's work" from 11pm Friday through 4am Saturday, then "Saturday's work" starts at 5am Saturday.

### Example 2: Regular 9-5 Worker (Midnight end-of-day, default)

Regular worker with standard schedule.

| Task             | Scheduled  | Time        | Logical Day | Why          |
|------------------|------------|-------------|-------------|--------------|
| Morning standup  | 2026-03-13 | 09:00 (9am) | Fri Mar 13  | 9am >= 00:00 |
| Project deadline | 2026-03-14 | No time     | Sat Mar 14  | All-day task |
| EOD meeting      | 2026-03-14 | 17:00 (5pm) | Sat Mar 14  | 5pm >= 00:00 |

Result: Standard calendar day behavior.

## Timezone Handling

The logical day calculations are always performed in the user's local timezone:

1. End-of-day time is interpreted in the local timezone
2. "Today" is the current date in the local timezone
3. When comparing scheduled times, they're converted to the local timezone first

This ensures that workers in different timezones see consistent logical days based on their local time.

## Backward Compatibility

If no `logicalDayEndOfDay` configuration is provided, the system defaults to `04:00` (4am), giving shift workers
reasonable default behavior. Regular workers can leave this as-is, and their tasks will behave nearly identically to
calendar-day behavior (with the minor quirk that tasks scheduled between midnight and 4am wrap to the previous day).

For strict calendar-day behavior, users can set `logicalDayEndOfDay: "00:00"`.
