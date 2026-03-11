package txtmsg

import (
	"bufio"
	"bytes"
	"fmt"
	"keyop/core"
	"path/filepath"
	"strings"
	"time"
)

// ServiceState holds persistent runtime state for the txtmsg service (last report day).
type ServiceState struct {
	LastReportDay time.Time `json:"last_report_day"`
}

// maybeSendTxtmsgReport reads the configured queue file for the previous day and sends a report.
func (svc *Service) maybeSendTxtmsgReport(messenger core.MessengerApi, now time.Time, force bool) error {
	logger := svc.Deps.MustGetLogger()
	if !force {
		if now.Hour() < 0 || now.Hour() >= 1 {
			return nil
		}
	}

	reportDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -1)
	if !svc.lastReportDay.IsZero() && svc.lastReportDay.Equal(reportDay) {
		return nil
	}

	template := svc.queueFileTemplate
	if template == "" {
		logger.Info("txtmsg: report_queue_file not configured; skipping report")
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

	data, err := svc.Deps.MustGetOsProvider().ReadFile(queuePath)
	if err != nil {
		logger.Warn("txtmsg: no queue file for previous day", "path", queuePath, "error", err)
		return nil
	}

	scanner := bufio.NewScanner(bytes.NewReader(data))
	processed := 0
	sent := 0
	notSent := 0
	issues := make(map[string]int)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		msg, parseErr := core.UnmarshalMessage([]byte(line))
		if parseErr != nil {
			continue
		}
		// consider only txtmsg unified events
		if msg.Event != "txtmsg" {
			continue
		}
		processed++
		// try to interpret Data.sent
		sentFlag := true
		if msg.Data != nil {
			switch d := msg.Data.(type) {
			case map[string]interface{}:
				if v, ok := d["sent"]; ok {
					if b, ok2 := v.(bool); ok2 {
						sentFlag = b
					}
				}
				if !sentFlag {
					if v, ok := d["details"]; ok {
						issues[fmt.Sprintf("%v", v)]++
					} else {
						issues["unspecified"]++
					}
				}
			}
		}
		if sentFlag {
			sent++
		} else {
			notSent++
		}
	}

	if err := scanner.Err(); err != nil {
		logger.Warn("txtmsg: error scanning queue file", "path", queuePath, "error", err)
	}

	// build markdown
	md := fmt.Sprintf("# txtmsg report for %s\n", reportDay.Format("2006-01-02"))
	md += fmt.Sprintf("- Processed: %d\n", processed)
	md += fmt.Sprintf("- Sent: %d\n", sent)
	md += fmt.Sprintf("- Not sent: %d\n\n", notSent)
	md += "## Issues breakdown\n\n"
	if len(issues) == 0 {
		md += "No issues recorded.\n"
	} else {
		for k, v := range issues {
			md += fmt.Sprintf("- %s: %d\n", k, v)
		}
	}

	err = messenger.Send(core.Message{
		ChannelName: svc.Cfg.Name,
		ServiceName: svc.Cfg.Name,
		ServiceType: svc.Cfg.Type,
		Event:       "txtmsg_report",
		Summary:     "text message report available",
		Text:        "txtmsg report for " + reportDay.Format("2006-01-02"),
		Body:        md,
		Data: map[string]interface{}{
			"date":             reportDay.Format("2006-01-02"),
			"processed":        processed,
			"sent":             sent,
			"not_sent":         notSent,
			"issues_breakdown": issues,
		},
	})
	if err != nil {
		return err
	}

	svc.lastReportDay = reportDay
	_ = svc.Deps.MustGetStateStore().Save(svc.Cfg.Name, ServiceState{LastReportDay: svc.lastReportDay})
	logger.Info("txtmsg: sent nightly report", "date", reportDay.Format("2006-01-02"))
	return nil
}
