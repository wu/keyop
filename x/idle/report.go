// Package idle report logic for generating daily idle reports from the queue file.
package idle

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"keyop/core"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// maybeSendIdleReport sends a daily report for the previous day between 00:00 and 01:00 local time.
// If force is true the report will be generated regardless of the current hour (used for reportOnStartup).
func (svc *Service) maybeSendIdleReport(messenger core.MessengerApi, now time.Time, force bool) error {
	logger := svc.Deps.MustGetLogger()

	logger.Warn("Checking if idle report should be sent", "force", force, "hour", now.Hour())
	if !force {
		if now.Hour() < 0 || now.Hour() >= 1 {
			logger.Warn("Not sending idle report: outside of 00:00-01:00 local time")
			return nil
		}
	}

	reportDay := localMidnight(now).AddDate(0, 0, -1)
	if !svc.lastReportDay.IsZero() && svc.lastReportDay.Equal(reportDay) {
		logger.Warn("Not sending idle report: already sent report for this day", "reportDay", reportDay.Format(fileDateFormat))
		return nil
	}

	// Determine queue file path by replacing yyyymmdd token in the template
	template := svc.queueFileTemplate
	if template == "" {
		logger.Info("idle: report_queue_file not configured; skipping report")
		return nil
	}
	queuePath := strings.ReplaceAll(template, "yyyymmdd", reportDay.Format("20060102"))
	if strings.HasPrefix(queuePath, "~") {
		if home, herr := svc.Deps.MustGetOsProvider().UserHomeDir(); herr == nil {
			if strings.HasPrefix(queuePath, "~/") {
				queuePath = filepath.Join(home, queuePath[2:])
			} else {
				queuePath = filepath.Join(home, queuePath[1:])
			}
		}
	}
	logger.Warn("Idle report: determined queue file path", "path", queuePath)

	data, err := svc.Deps.MustGetOsProvider().ReadFile(queuePath)
	if err != nil {
		logger.Warn("idle: no queue file for previous day", "path", queuePath, "error", err)
		return nil
	}

	// Parse one JSON envelope/message per line, collect idle_status messages per host
	msgsByHost := make(map[string][]core.Message)
	dayStart := reportDay
	dayEnd := dayStart.Add(24 * time.Hour)
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var env core.Envelope
		var msg core.Message
		if err := json.Unmarshal([]byte(line), &env); err == nil && (env.Version != "" || env.ID != "") {
			msg = env.ToMessage()
		} else {
			// Try legacy Message
			if err := json.Unmarshal([]byte(line), &msg); err != nil {
				logger.Warn("idle: failed to unmarshal queue line", "path", queuePath, "error", err)
				continue
			}
		}

		if msg.Event != "idle_status" {
			continue
		}
		if msg.Timestamp.IsZero() {
			continue
		}
		if msg.Timestamp.Before(dayStart) || !msg.Timestamp.Before(dayEnd) {
			continue
		}
		host := msg.Hostname
		if host == "" {
			host = svc.hostname
		}
		msgsByHost[host] = append(msgsByHost[host], msg)
	}
	if err := scanner.Err(); err != nil {
		logger.Warn("idle: error scanning queue file", "path", queuePath, "error", err)
	}

	if len(msgsByHost) == 0 {
		logger.Warn("idle: no idle_status messages found in queue", "path", queuePath)
		return nil
	}

	// Sort messages for each host
	for h := range msgsByHost {
		sort.Slice(msgsByHost[h], func(i, j int) bool { return msgsByHost[h][i].Timestamp.Before(msgsByHost[h][j].Timestamp) })
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
		if first.Before(dayStart) {
			first = dayStart
		}
		last := msgs[len(msgs)-1].Timestamp
		if last.After(dayEnd) {
			last = dayEnd
		}
		if last.After(first) {
			coverageIntervals = append(coverageIntervals, interval{Start: first, Stop: last})
		}

		inActive := false
		var activeStart time.Time
		for i, m := range msgs {
			if m.Status == "active" {
				if !inActive {
					activeStart = m.Timestamp
					if activeStart.Before(dayStart) {
						activeStart = dayStart
					}
					inActive = true
				}
			} else {
				if inActive {
					stop := m.Timestamp
					if stop.After(dayEnd) {
						stop = dayEnd
					}
					if stop.After(activeStart) {
						allActivePeriods = append(allActivePeriods, ActivePeriod{Hostname: host, Start: activeStart, Stop: stop, DurationSeconds: stop.Sub(activeStart).Seconds()})
					}
					inActive = false
				}
			}

			// If last message and still active, extend to day end
			if i == len(msgs)-1 && inActive {
				stop := dayEnd
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

	totalDaySecs := 24 * 60 * 60
	unknownTotalSecs := float64(totalDaySecs) - knownTotalSecs
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

	// Build hourly activity: minutes active per hour (0-23)
	hourly := make([]int, 24)
	for _, p := range mergedActive {
		for h := 0; h < 24; h++ {
			hourStart := dayStart.Add(time.Duration(h) * time.Hour)
			hourEnd := hourStart.Add(time.Hour)
			if p.Stop.Before(hourStart) || p.Start.After(hourEnd) {
				continue
			}
			start := p.Start
			if start.Before(hourStart) {
				start = hourStart
			}
			end := p.Stop
			if end.After(hourEnd) {
				end = hourEnd
			}
			overlap := end.Sub(start).Minutes()
			if overlap < 0 {
				overlap = 0
			}
			// round to nearest minute
			mins := int(overlap + 0.5)
			if mins > 60 {
				mins = 60
			}
			hourly[h] += mins
		}
	}

	md := fmt.Sprintf("# Idle report for %s\n", reportDay.Format(fileDateFormat))
	md += fmt.Sprintf("**Total active:** %s\n", formatHM(activeTotalSecs))
	md += fmt.Sprintf("**Total idle:** %s\n", formatHM(idleTotalSecs))
	md += fmt.Sprintf("**Total unknown:** %s\n\n", formatHM(unknownTotalSecs))

	// Hourly bar chart (one bar per hour; bar length = minutes active)
	md += "## Hourly activity\n\n"
	md += "```\n"
	for h := 0; h < 24; h++ {
		label := fmt.Sprintf("%02d:00", h)
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
	for _, p := range allActivePeriods {
		md += fmt.Sprintf("| %s | %s | %s | %s |\n", p.Hostname, p.Start.Format("3:04pm"), p.Stop.Format("3:04pm"), formatHM(p.DurationSeconds))
	}

	err = messenger.Send(core.Message{
		ChannelName: svc.Cfg.Name,
		ServiceName: svc.Cfg.Name,
		ServiceType: svc.Cfg.Type,
		Event:       "idle_report",
		Summary:     "idle report for " + reportDay.Format("2006-01-02"),
		Text:        md,
		Data: map[string]interface{}{
			"date":               reportDay.Format(fileDateFormat),
			"active_seconds":     activeTotalSecs,
			"idle_seconds":       idleTotalSecs,
			"unknown_seconds":    unknownTotalSecs,
			"active_periods":     allActivePeriods,
			"hourly_active_mins": hourly,
		},
	})
	if err != nil {
		return err
	}

	svc.lastReportDay = reportDay
	_ = svc.Deps.MustGetStateStore().Save(svc.Cfg.Name, ServiceState{IsIdle: svc.isIdle, LastTransition: svc.lastTransition, LastAlertHours: svc.lastAlertHours, LastReportDay: svc.lastReportDay})
	logger.Info("idle: sent nightly idle report", "date", reportDay.Format(fileDateFormat))
	return nil
}
