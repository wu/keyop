// Package idle report logic for generating daily idle reports from the queue file.
package idle

import (
	"fmt"
	"keyop/core"
	"sort"
	"strings"
	"time"
)

// generateIdleReport generates an idle report between the specified start and end times.
// If start is zero, it defaults to the beginning of the day (midnight) for the report day (yesterday if now is given).
// If end is zero, it defaults to the end of that same day.
// If both are zero, it defaults to the last 24 hours from 'now'.
func (svc *Service) generateIdleReport(_ core.MessengerApi, now time.Time, start, end time.Time, force bool) (string, error) {
	logger := svc.Deps.MustGetLogger()

	if svc.db == nil || *svc.db == nil {
		logger.Warn("idle: database not available; skipping report")
		return "", nil
	}

	if !force {
		if now.Hour() < 0 || now.Hour() >= 1 {
			return "", nil
		}
	}

	reportDay := localMidnight(now).AddDate(0, 0, -1)
	if !force && !svc.lastReportDay.IsZero() && svc.lastReportDay.Equal(reportDay) {
		return "", nil
	}

	// Default time range logic
	if start.IsZero() && end.IsZero() {
		// Last 24 hours
		end = now
		start = end.Add(-24 * time.Hour)
	} else if end.IsZero() {
		// From start to now
		end = now
	} else if start.IsZero() {
		// 24 hours before end
		start = end.Add(-24 * time.Hour)
	}

	logger.Info("Generating idle report", "start", start, "end", end)

	db := *svc.db
	// Query messages from SQLite
	rows, err := db.Query(`
		SELECT timestamp, hostname, status, idle_seconds, active_seconds 
		FROM idle_events 
		WHERE timestamp >= ? AND timestamp < ? 
		ORDER BY timestamp ASC`,
		start, end)
	if err != nil {
		return "", fmt.Errorf("failed to query idle events: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			logger.Warn("idle: failed to close rows", "error", err)
		}
	}()

	msgsByHost := make(map[string][]core.Message)
	for rows.Next() {
		var msg core.Message
		var idleSecs, activeSecs float64
		if err := rows.Scan(&msg.Timestamp, &msg.Hostname, &msg.Status, &idleSecs, &activeSecs); err != nil {
			logger.Error("failed to scan idle event row", "error", err)
			continue
		}
		msg.Event = "idle_status"
		msg.Data = &Event{
			Now:                   msg.Timestamp,
			Hostname:              msg.Hostname,
			Status:                msg.Status,
			IdleDurationSeconds:   idleSecs,
			ActiveDurationSeconds: activeSecs,
		}
		host := msg.Hostname
		if host == "" {
			host = "unknown"
		}
		msgsByHost[host] = append(msgsByHost[host], msg)
	}

	if len(msgsByHost) == 0 {
		logger.Warn("idle: no idle_status messages found in database for range", "start", start, "end", end)
		return "", nil
	}

	// Build per-host active periods and coverage intervals
	var allActivePeriods []ActivePeriod
	type interval struct{ Start, Stop time.Time }
	var coverageIntervals []interval
	for host, msgs := range msgsByHost {
		if len(msgs) == 0 {
			continue
		}
		first := msgs[0].Timestamp
		last := msgs[len(msgs)-1].Timestamp
		if last.After(first) {
			coverageIntervals = append(coverageIntervals, interval{Start: first, Stop: last})
		}

		inActive := false
		var activeStart time.Time
		for i, m := range msgs {
			if m.Status == "active" {
				if !inActive {
					activeStart = m.Timestamp
					inActive = true
				}
			} else {
				if inActive {
					stop := m.Timestamp
					if stop.After(activeStart) {
						allActivePeriods = append(allActivePeriods, ActivePeriod{Hostname: host, Start: activeStart, Stop: stop, DurationSeconds: stop.Sub(activeStart).Seconds()})
					}
					inActive = false
				}
			}

			// If last message and still active, extend to end of range
			if i == len(msgs)-1 && inActive {
				stop := end
				if stop.After(activeStart) {
					allActivePeriods = append(allActivePeriods, ActivePeriod{Hostname: host, Start: activeStart, Stop: stop, DurationSeconds: stop.Sub(activeStart).Seconds()})
				}
			}
		}
	}

	// Merge all active periods across hosts to compute total active time
	sort.Slice(allActivePeriods, func(i, j int) bool { return allActivePeriods[i].Start.Before(allActivePeriods[j].Start) })
	var mergedActive []ActivePeriod
	for _, p := range allActivePeriods {
		if len(mergedActive) == 0 {
			mergedActive = append(mergedActive, p)
			continue
		}
		last := &mergedActive[len(mergedActive)-1]
		if !p.Start.After(last.Stop) {
			// overlap or contiguous
			if p.Stop.After(last.Stop) {
				last.Stop = p.Stop
				last.DurationSeconds = last.Stop.Sub(last.Start).Seconds()
			}
		} else {
			mergedActive = append(mergedActive, p)
		}
	}
	activeTotalSecs := 0.0
	for _, p := range mergedActive {
		activeTotalSecs += p.DurationSeconds
	}

	// Merge coverage intervals to compute known coverage
	sort.Slice(coverageIntervals, func(i, j int) bool { return coverageIntervals[i].Start.Before(coverageIntervals[j].Start) })
	var mergedCoverage []interval
	for _, c := range coverageIntervals {
		if len(mergedCoverage) == 0 {
			mergedCoverage = append(mergedCoverage, c)
			continue
		}
		last := &mergedCoverage[len(mergedCoverage)-1]
		if !c.Start.After(last.Stop) {
			if c.Stop.After(last.Stop) {
				last.Stop = c.Stop
			}
		} else {
			mergedCoverage = append(mergedCoverage, c)
		}
	}
	knownTotalSecs := 0.0
	for _, c := range mergedCoverage {
		knownTotalSecs += c.Stop.Sub(c.Start).Seconds()
	}

	totalRangeSecs := end.Sub(start).Seconds()
	unknownTotalSecs := totalRangeSecs - knownTotalSecs
	if unknownTotalSecs < 0 {
		unknownTotalSecs = 0
	}
	idleTotalSecs := knownTotalSecs - activeTotalSecs
	if idleTotalSecs < 0 {
		idleTotalSecs = 0
	}

	formatHM := func(secs float64) string {
		m := int(secs) / 60
		h := m / 60
		mm := m % 60
		return fmt.Sprintf("%dh %dm", h, mm)
	}

	// Build hourly activity
	// We determine how many hours are in the range [start, end)
	numHours := int(end.Sub(start).Hours()) + 1
	hourly := make([]int, numHours)
	for _, p := range mergedActive {
		for h := 0; h < numHours; h++ {
			hourStart := start.Truncate(time.Hour).Add(time.Duration(h) * time.Hour)
			if hourStart.After(end) {
				continue
			}

			hourEnd := hourStart.Add(time.Hour)
			if p.Stop.Before(hourStart) || p.Start.After(hourEnd) {
				continue
			}
			s := p.Start
			if s.Before(hourStart) {
				s = hourStart
			}
			e := p.Stop
			if e.After(hourEnd) {
				e = hourEnd
			}
			overlap := e.Sub(s).Minutes()
			if overlap > 0 {
				mins := int(overlap + 0.5)
				if mins > 60 {
					mins = 60
				}
				hourly[h] += mins
			}
		}
	}

	md := fmt.Sprintf("# Idle report: %s to %s\n", start.Format("2006-01-02 15:04"), end.Format("2006-01-02 15:04"))
	md += fmt.Sprintf("* **Total active:** %s\n", formatHM(activeTotalSecs))
	md += fmt.Sprintf("* **Total idle:** %s\n", formatHM(idleTotalSecs))
	md += fmt.Sprintf("* **Total unknown:** %s\n\n", formatHM(unknownTotalSecs))

	md += "## Hourly activity\n\n"
	md += "```\n"
	for h := numHours - 1; h >= 0; h-- {
		hourTime := start.Truncate(time.Hour).Add(time.Duration(h) * time.Hour)
		label := hourTime.Format("01-02 15:00")
		bars := hourly[h]
		if bars > 60 {
			bars = 60
		}
		md += fmt.Sprintf("%s | %s %2dm\n", label, strings.Repeat("█", bars), hourly[h])
	}
	md += "```\n\n"

	md += "## Active periods\n\n"
	md += "| Hostname | Start | Stop | Duration |\n"
	md += "|---|---:|---:|---:|\n"
	// Show most recent periods first
	for i := len(allActivePeriods) - 1; i >= 0; i-- {
		p := allActivePeriods[i]
		md += fmt.Sprintf("| %s | %s | %s | %s |\n", p.Hostname, p.Start.Format("3:04pm"), p.Stop.Format("3:04pm"), formatHM(p.DurationSeconds))
	}

	// Do not emit an "idle_report" event; reports are for the Web UI only.
	// Keep returning the markdown so callers (web UI action) can render it.

	if force && start.IsZero() && end.IsZero() {
		// Only update lastReportDay if it was a standard "last 24h" report
		svc.lastReportDay = reportDay
		_ = svc.Deps.MustGetStateStore().Save(svc.Cfg.Name, ServiceState{IsIdle: svc.isIdle, LastTransition: svc.lastTransition, LastAlertHours: svc.lastAlertHours, LastReportDay: svc.lastReportDay})
	}
	logger.Info("idle: generated report from database (web-only)")
	return md, nil
}
