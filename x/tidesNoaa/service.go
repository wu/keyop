package tidesNoaa

import (
	"fmt"
	"keyop/core"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// alertedPeak records a peak that has already been announced so that
// re-runs of Check() do not send duplicate high/low tide alerts.
type alertedPeak struct {
	Type string    `json:"type"` // "high" or "low"
	Time string    `json:"time"` // noaaTimeFormat string — matches TidePeak.Time
	At   time.Time `json:"at"`   // wall-clock time when the alert was sent, used for pruning
}

// Service implements the keyop core.Service interface for tide monitoring.
type Service struct {
	Deps              core.Dependencies
	Cfg               core.ServiceConfig
	stationID         string
	dataDir           string
	apiBase           string // overridable in tests; defaults to noaaAPIBase
	metadataBase      string // overridable in tests; defaults to noaaMetadataBase
	lat               float64
	lon               float64
	alt               float64
	lowTideThreshold  float64
	extremes          TideExtremes
	alertedPeaks      []alertedPeak
	extremeTideStatus map[string]string // keyed by window label, value "warning" or "ok"
	lastBackfillDay   time.Time
	lastReportDay     time.Time // calendar day on which the last tide_report was sent
	mu                sync.RWMutex
}

func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	return &Service{
		Deps:         deps,
		Cfg:          cfg,
		apiBase:      noaaAPIBase,
		metadataBase: noaaMetadataBase,
	}
}

// ValidateConfig checks that all required configuration fields are present.
func (svc *Service) ValidateConfig() []error {
	var errs []error

	stationID, ok := svc.Cfg.Config["stationId"].(string)
	if !ok || stationID == "" {
		errs = append(errs, fmt.Errorf("tidesNoaa: required config parameter 'stationId' is missing or empty"))
	}

	// lat and lon are required when a tide report is configured.
	// They are not required for basic tide level monitoring.
	if _, hasLat := svc.Cfg.Config["lat"]; hasLat {
		if _, ok := svc.Cfg.Config["lat"].(float64); !ok {
			errs = append(errs, fmt.Errorf("tidesNoaa: 'lat' must be a float64"))
		}
	}
	if _, hasLon := svc.Cfg.Config["lon"]; hasLon {
		if _, ok := svc.Cfg.Config["lon"].(float64); !ok {
			errs = append(errs, fmt.Errorf("tidesNoaa: 'lon' must be a float64"))
		}
	}

	return errs
}

// Initialize resolves configuration values and creates the station data directory.
func (svc *Service) Initialize() error {
	svc.stationID, _ = svc.Cfg.Config["stationId"].(string)

	if dir, ok := svc.Cfg.Config["dataDir"].(string); ok && dir != "" {
		svc.dataDir = dir
	} else {
		home, err := svc.Deps.MustGetOsProvider().UserHomeDir()
		if err != nil {
			return fmt.Errorf("tidesNoaa: failed to determine home directory: %w", err)
		}
		svc.dataDir = filepath.Join(home, ".keyop", "tides")
	}

	// Observer coordinates for sunrise/sunset calculations (used by tide report).
	// If not explicitly configured, look them up from the NOAA metadata API.
	svc.lat, _ = svc.Cfg.Config["lat"].(float64)
	svc.lon, _ = svc.Cfg.Config["lon"].(float64)
	svc.alt, _ = svc.Cfg.Config["alt"].(float64)

	if svc.lat == 0 && svc.lon == 0 {
		lat, lon, err := fetchStationLocation(svc.metadataBase, svc.stationID)
		if err != nil {
			svc.Deps.MustGetLogger().Warn("tidesNoaa: could not fetch station coordinates; tide report disabled",
				"station", svc.stationID, "error", err)
		} else {
			svc.lat = lat
			svc.lon = lon
			svc.Deps.MustGetLogger().Info("tidesNoaa: using station coordinates from NOAA metadata API",
				"station", svc.stationID, "lat", lat, "lon", lon)
		}
	}

	// Low tide threshold for the daily report (default 5 ft).
	if v, ok := svc.Cfg.Config["lowTideThreshold"].(float64); ok {
		svc.lowTideThreshold = v
	} else {
		svc.lowTideThreshold = 5.0
	}

	// Per-station sub-directory keeps each station's daily files together.
	stationDir := svc.stationDir()
	if err := svc.Deps.MustGetOsProvider().MkdirAll(stationDir, 0o755); err != nil {
		return fmt.Errorf("tidesNoaa: failed to create data directory %s: %w", stationDir, err)
	}

	// Load any previously persisted extremes from the state store.
	if err := svc.Deps.MustGetStateStore().Load(svc.stateKey(), &svc.extremes); err != nil {
		svc.Deps.MustGetLogger().Warn("tidesNoaa: failed to load extremes from state store", "error", err)
	}

	// Load previously alerted peaks so we don't re-send on restart.
	if err := svc.Deps.MustGetStateStore().Load(svc.alertedPeaksKey(), &svc.alertedPeaks); err != nil {
		svc.Deps.MustGetLogger().Warn("tidesNoaa: failed to load alerted peaks from state store", "error", err)
	}

	// Load extreme tide status for alert windows
	if err := svc.Deps.MustGetStateStore().Load(svc.extremeTideStatusKey(), &svc.extremeTideStatus); err != nil {
		svc.Deps.MustGetLogger().Warn("tidesNoaa: failed to load extreme tide status from state store", "error", err)
	}

	// Load the last day a tide report was sent so we don't re-send on restart.
	if err := svc.Deps.MustGetStateStore().Load(svc.tideReportKey(), &svc.lastReportDay); err != nil {
		svc.Deps.MustGetLogger().Warn("tidesNoaa: failed to load last report day from state store", "error", err)
	}

	// Backfill extremes from existing day files so a fresh or deleted state
	// file is immediately correct rather than taking 30–365 days to converge.
	svc.backfillExtremes(time.Now())
	svc.lastBackfillDay = localMidnight(time.Now())

	return nil
}

// Check ensures tide data is up to date for today through the next fetchDays
// days, then sends a message with the current water level.
func (svc *Service) Check() error {
	logger := svc.Deps.MustGetLogger()
	now := time.Now()
	today := localMidnight(now)

	// ensureDayFiles does network and disk I/O — call it without holding svc.mu.
	if err := svc.ensureDayFiles(now); err != nil {
		return fmt.Errorf("tidesNoaa: failed to refresh tide data: %w", err)
	}

	// Re-backfill whenever the calendar date has rolled over since the last
	// backfill.  This keeps the leaderboard current for long-running services
	// without requiring a restart each day.
	svc.mu.RLock()
	needBackfill := today.After(svc.lastBackfillDay)
	svc.mu.RUnlock()
	if needBackfill {
		svc.backfillExtremes(now)
		svc.mu.Lock()
		svc.lastBackfillDay = today
		svc.mu.Unlock()
	}

	// Collect today's and yesterday's records so we always have a "current"
	// value even right at midnight.
	svc.mu.RLock()
	records, err := svc.collectRecordsAroundNow(now)
	svc.mu.RUnlock()
	if err != nil {
		return fmt.Errorf("tidesNoaa: %w", err)
	}

	current, next, err := findCurrentTide(records, now)
	if err != nil {
		return fmt.Errorf("tidesNoaa: %w", err)
	}

	logger.Debug("tidesNoaa: current tide", "station", svc.stationID, "value", current.Value, "time", current.Time)

	state := tideState(records, current)
	peak := nextPeak(records, current)

	// Scan the last 3 records (lookback=2 positions behind current) for any
	// peaks we may have missed if Check() ran late.
	recentPeakList := recentPeaks(records, current, 2)

	svc.mu.RLock()
	prevExtremes := svc.extremes
	svc.mu.RUnlock()

	messenger := svc.Deps.MustGetMessenger()

	data := map[string]interface{}{
		"stationId": svc.stationID,
		"current":   current,
		"state":     state,
	}
	if next != nil {
		data["next"] = next
	}
	if peak != nil {
		data["nextPeak"] = peak
	}

	peakText := ""
	if peak != nil {
		peakText = fmt.Sprintf(", next %s (%.2f ft) at %s", peak.Type, peak.Value, peak.Time)
	}

	if err := messenger.Send(core.Message{
		ChannelName: svc.Cfg.Name,
		ServiceName: svc.Cfg.Name,
		ServiceType: svc.Cfg.Type,
		Event:       "tide",
		Text:        fmt.Sprintf("Station %s: %.2f ft at %s, %s%s", svc.stationID, current.Value, current.Time, state, peakText),
		Summary:     fmt.Sprintf("Tide: %.2f ft %s", current.Value, state),
		Metric:      current.Value,
		MetricName:  fmt.Sprintf("tide.%s", svc.stationID),
		Data:        data,
	}); err != nil {
		return err
	}

	// Collect any peaks in the lookback window that haven't been alerted yet.
	// Mutate state under lock, then send messages after releasing.
	svc.mu.Lock()
	svc.alertedPeaks = pruneAlertedPeaks(svc.alertedPeaks, now)
	var newPeaksToSend []TidePeak
	for _, ap := range recentPeakList {
		if isPeakAlerted(svc.alertedPeaks, ap) {
			continue
		}
		svc.alertedPeaks = append(svc.alertedPeaks, alertedPeak{
			Type: ap.Type,
			Time: ap.Time,
			At:   now,
		})
		newPeaksToSend = append(newPeaksToSend, ap)
	}
	alertedPeaksCopy := svc.alertedPeaks
	svc.mu.Unlock()

	if len(newPeaksToSend) > 0 {
		if saveErr := svc.Deps.MustGetStateStore().Save(svc.alertedPeaksKey(), alertedPeaksCopy); saveErr != nil {
			logger.Warn("tidesNoaa: failed to save alerted peaks", "error", saveErr)
		}
		for _, ap := range newPeaksToSend {
			event := "high_tide_alert"
			summary := fmt.Sprintf("High tide: %.2f ft", ap.Value)
			if ap.Type == "low" {
				event = "low_tide_alert"
				summary = fmt.Sprintf("Low tide: %.2f ft", ap.Value)
			}
			if err := messenger.Send(core.Message{
				ChannelName: svc.Cfg.Name,
				ServiceName: svc.Cfg.Name,
				ServiceType: svc.Cfg.Type,
				Event:       event,
				Text:        fmt.Sprintf("Station %s: %s at %s", svc.stationID, summary, ap.Time),
				Summary:     summary,
				Metric:      ap.Value,
				MetricName:  fmt.Sprintf("tide.%s", svc.stationID),
				Data: map[string]interface{}{
					"stationId": svc.stationID,
					"peak":      ap,
				},
			}); err != nil {
				return err
			}
		}
	}

	// Check and send extreme_tide status events for each window.
	if err := svc.sendExtremeTideStatus(messenger, peak, state, prevExtremes); err != nil {
		return err
	}

	// Send the daily tide report once per day at or after 04:00 local time.
	return svc.maybeSendTideReport(messenger, now)
}

// ensureDayFiles fetches and stores any day files that are missing or stale,
// covering today through today+fetchDays.
// Must NOT be called while holding svc.mu — it performs network and disk I/O.
func (svc *Service) ensureDayFiles(now time.Time) error {
	logger := svc.Deps.MustGetLogger()
	todayMidnight := localMidnight(now)

	for i := 0; i <= fetchDays; i++ {
		day := now.AddDate(0, 0, i)
		dayMidnight := localMidnight(day)
		// dayOffset is the signed calendar-day distance from today.
		dayOffset := int(dayMidnight.Sub(todayMidnight).Hours() / 24)

		f, _ := svc.loadDayFile(day)
		if !dayFileStale(f, dayOffset, now) {
			continue
		}

		logger.Info("tidesNoaa: fetching day data", "station", svc.stationID, "date", day.Format(fileDateFormat))
		records, err := fetchDayRecords(svc.apiBase, svc.stationID, day)
		if err != nil {
			// Future days may not yet have data; log and skip rather than abort.
			if i > 0 {
				logger.Warn("tidesNoaa: could not fetch future day", "date", day.Format(fileDateFormat), "error", err)
				continue
			}
			return err
		}

		if err := svc.storeDayFile(day, records, now); err != nil {
			return err
		}

		// Update the extremes leaderboard with this day's high and low.
		// Only past days are included (today's data is still incomplete).
		if dayMidnight.Before(todayMidnight) {
			svc.mu.Lock()
			svc.extremes = updateExtremes(svc.extremes, records, day, now)
			ex := svc.extremes
			svc.mu.Unlock()
			if saveErr := svc.Deps.MustGetStateStore().Save(svc.stateKey(), ex); saveErr != nil {
				logger.Warn("tidesNoaa: failed to save extremes", "error", saveErr)
			}
		}
	}
	return nil
}

// collectRecordsAroundNow loads yesterday's, today's, and tomorrow's records,
// returning them in chronological order. Yesterday is included so
// findCurrentTide always has a "current" value right at midnight; tomorrow is
// included so nextPeak can look past end-of-day.
func (svc *Service) collectRecordsAroundNow(now time.Time) ([]TideRecord, error) {
	var combined []TideRecord

	if yf, err := svc.loadDayFile(now.AddDate(0, 0, -1)); err == nil {
		combined = append(combined, yf.Records...)
	}
	tf, err := svc.loadDayFile(now)
	if err != nil {
		return nil, fmt.Errorf("no records available for today (%s): %w", now.Format(fileDateFormat), err)
	}
	combined = append(combined, tf.Records...)
	if tmf, err := svc.loadDayFile(now.AddDate(0, 0, 1)); err == nil {
		combined = append(combined, tmf.Records...)
	}
	return combined, nil
}

// loadDayFile reads the YAML file for the given day from disk.
func (svc *Service) loadDayFile(day time.Time) (*TideDayFile, error) {
	path := svc.dayFilePath(day)
	data, err := svc.Deps.MustGetOsProvider().ReadFile(path)
	if err != nil {
		return nil, err
	}
	var f TideDayFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", path, err)
	}
	return &f, nil
}

// storeDayFile marshals records into a TideDayFile and writes it to disk.
func (svc *Service) storeDayFile(day time.Time, records []TideRecord, now time.Time) error {
	logger := svc.Deps.MustGetLogger()

	f := TideDayFile{
		StationID: svc.stationID,
		Date:      day.Format(fileDateFormat),
		FetchedAt: now,
		Records:   records,
	}

	out, err := yaml.Marshal(f)
	if err != nil {
		return fmt.Errorf("tidesNoaa: failed to marshal day file: %w", err)
	}

	path := svc.dayFilePath(day)
	file, err := svc.Deps.MustGetOsProvider().OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("tidesNoaa: failed to open %s for writing: %w", path, err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			logger.Warn("tidesNoaa: failed to close day file", "path", path, "error", closeErr)
		}
	}()

	if _, err := file.Write(out); err != nil {
		return fmt.Errorf("tidesNoaa: failed to write %s: %w", path, err)
	}

	logger.Info("tidesNoaa: day file written", "station", svc.stationID, "date", f.Date, "records", len(records))
	return nil
}

// stationDir returns the per-station subdirectory path.
func (svc *Service) stationDir() string {
	return filepath.Join(svc.dataDir, svc.stationID)
}

// dayFilePath returns the full path to the YAML file for a given day.
func (svc *Service) dayFilePath(day time.Time) string {
	return filepath.Join(svc.stationDir(), day.Format(fileDateFormat)+".yaml")
}

// stateKey returns the state store key for this service's extremes.
func (svc *Service) stateKey() string {
	return fmt.Sprintf("%s.extremes", svc.Cfg.Name)
}

// alertedPeaksKey returns the state store key for the alerted-peaks list.
func (svc *Service) alertedPeaksKey() string {
	return fmt.Sprintf("%s.alertedPeaks", svc.Cfg.Name)
}

// extremeTideStatusKey returns the state store key for the extreme tide status.
func (svc *Service) extremeTideStatusKey() string {
	return fmt.Sprintf("%s.extremeTideStatus", svc.Cfg.Name)
}

// tideReportKey returns the state store key for the last tide-report send day.
func (svc *Service) tideReportKey() string {
	return fmt.Sprintf("%s.lastReportDay", svc.Cfg.Name)
}

// maybeSendTideReport sends a tide_report event once per calendar day at or
// after 04:00 local time.  It is a no-op when lat/lon are not configured or
// when the report has already been sent today.
func (svc *Service) maybeSendTideReport(messenger core.MessengerApi, now time.Time) error {
	// Tide report requires observer coordinates for sunrise/sunset.
	svc.mu.RLock()
	lat, lon, alt := svc.lat, svc.lon, svc.alt
	lastReport := svc.lastReportDay
	svc.mu.RUnlock()

	if lat == 0 && lon == 0 {
		return nil // no coordinates configured — skip report
	}

	today := localMidnight(now)
	if !lastReport.Before(today) {
		return nil // already sent today
	}

	// On the very first run (no prior state) send immediately regardless of
	// the time of day.  On subsequent days, wait until 04:00 so the report
	// arrives after the overnight tides have settled.
	firstRun := lastReport.IsZero()
	if !firstRun && now.Hour() < 4 {
		return nil
	}

	// Gather 7 days of records starting from today.
	const reportDays = 7
	var allPeriods []LowTidePeriod

	for i := 0; i < reportDays; i++ {
		day := today.AddDate(0, 0, i)
		f, err := svc.loadDayFile(day)
		if err != nil || len(f.Records) == 0 {
			continue
		}
		// Include tomorrow's records so periods that straddle midnight are
		// captured correctly by daylightLowPeriods.
		var records []TideRecord
		records = append(records, f.Records...)
		if next, err := svc.loadDayFile(day.AddDate(0, 0, 1)); err == nil {
			records = append(records, next.Records...)
		}

		sunrise, sunset := sunriseSunset(lat, lon, alt, day)
		svc.mu.RLock()
		threshold := svc.lowTideThreshold
		svc.mu.RUnlock()

		periods := daylightLowPeriods(records, day, sunrise, sunset, threshold)
		allPeriods = append(allPeriods, periods...)
	}

	svc.mu.RLock()
	threshold := svc.lowTideThreshold
	svc.mu.RUnlock()

	text := formatTideReport(allPeriods, threshold, svc.stationID)
	summary := fmt.Sprintf("Tide report: %d daylight low-tide period(s) in next %d days", len(allPeriods), reportDays)

	if err := messenger.Send(core.Message{
		ChannelName: svc.Cfg.Name,
		ServiceName: svc.Cfg.Name,
		ServiceType: svc.Cfg.Type,
		Event:       "tide_report",
		Text:        text,
		Summary:     summary,
		Data: map[string]interface{}{
			"stationId": svc.stationID,
			"threshold": threshold,
			"periods":   allPeriods,
		},
	}); err != nil {
		return err
	}

	svc.mu.Lock()
	svc.lastReportDay = today
	svc.mu.Unlock()

	if saveErr := svc.Deps.MustGetStateStore().Save(svc.tideReportKey(), today); saveErr != nil {
		svc.Deps.MustGetLogger().Warn("tidesNoaa: failed to save last report day", "error", saveErr)
	}
	return nil
}

// isPeakAlerted returns true if a peak with the same type and time string is
// already present in the alerted list.
func isPeakAlerted(alerted []alertedPeak, p TidePeak) bool {
	for _, a := range alerted {
		if a.Type == p.Type && a.Time == p.Time {
			return true
		}
	}
	return false
}

// pruneAlertedPeaks drops entries older than 24 hours.  Tidal cycles are
// ~12.4 hours, so 24 hours is safely longer than any single cycle.
func pruneAlertedPeaks(alerted []alertedPeak, now time.Time) []alertedPeak {
	cutoff := now.Add(-24 * time.Hour)
	out := make([]alertedPeak, 0, len(alerted))
	for _, a := range alerted {
		if a.At.After(cutoff) {
			out = append(out, a)
		}
	}
	return out
}

// backfillExtremes scans all day files in the station directory and feeds
// every record through updateExtremes so that a freshly initialised (or
// state-deleted) service immediately has a correct picture of the historical
// highs and lows rather than waiting weeks to observe them again.
// It intentionally replaces svc.extremes entirely so stale state is purged.
func (svc *Service) backfillExtremes(now time.Time) {
	logger := svc.Deps.MustGetLogger()
	cutoff := now.AddDate(0, 0, -365)

	entries, err := svc.Deps.MustGetOsProvider().ReadDir(svc.stationDir())
	if err != nil {
		logger.Warn("tidesNoaa: backfill could not read station dir", "error", err)
		return
	}

	// Collect and sort filenames so records are fed in chronological order.
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if len(name) != len("2006-01-02.yaml") || filepath.Ext(name) != ".yaml" {
			continue
		}
		day, err := time.ParseInLocation(fileDateFormat, name[:len(name)-len(".yaml")], now.Location())
		if err != nil || day.Before(cutoff) || day.After(now) {
			continue
		}
		names = append(names, name)
	}
	// sort.Strings gives chronological order for YYYY-MM-DD filenames.
	sort.Strings(names)

	ex := TideExtremes{}
	for _, name := range names {
		raw, err := svc.Deps.MustGetOsProvider().ReadFile(filepath.Join(svc.stationDir(), name))
		if err != nil {
			continue
		}
		var f TideDayFile
		if err := yaml.Unmarshal(raw, &f); err != nil {
			continue
		}
		day, _ := time.ParseInLocation(fileDateFormat, name[:len(name)-len(".yaml")], now.Location())
		ex = updateExtremes(ex, f.Records, day, now)
	}

	svc.mu.Lock()
	svc.extremes = ex
	svc.mu.Unlock()

	if saveErr := svc.Deps.MustGetStateStore().Save(svc.stateKey(), ex); saveErr != nil {
		logger.Warn("tidesNoaa: failed to save backfilled extremes", "error", saveErr)
	}
	logger.Info("tidesNoaa: extremes backfilled", "station", svc.stationID, "days", len(names))
}

// sendExtremeTideStatus sends an extreme_tide event for each window whenever
// the status changes.  Status is "warning" when:
//   - the next peak is an extreme high AND the tide is currently rising, OR
//   - the next peak is an extreme low AND the tide is currently falling.
//
// Status is "ok" in all other cases (next peak is not extreme, or tide is
// moving away from the extreme direction).  Only status *changes* are sent so
// the messenger is not flooded on every Check interval.
func (svc *Service) sendExtremeTideStatus(messenger core.MessengerApi, peak *TidePeak, state string, extremes TideExtremes) error {
	logger := svc.Deps.MustGetLogger()

	type windowSpec struct {
		label  string
		window TideWindowExtremes
	}
	windows := []windowSpec{
		{"1-lunar-cycle", extremes.Window1Lunar},
		{"3-lunar-cycles", extremes.Window3Lunar},
		{"12-lunar-cycles", extremes.Window12Lunar},
	}

	svc.mu.Lock()
	if svc.extremeTideStatus == nil {
		svc.extremeTideStatus = make(map[string]string)
	}
	changed := false
	type pendingMsg struct {
		status, summary, text string
		metric                float64
		data                  map[string]interface{}
	}
	var toSend []pendingMsg

	for _, ws := range windows {
		newStatus := "ok"
		var summary, text string
		var metric float64
		data := map[string]interface{}{
			"stationId": svc.stationID,
			"window":    ws.label,
		}

		if peak != nil && peak.Time != "" {
			prevHigh := ws.window.High()
			prevLow := ws.window.Low()
			isExtremeHigh := peak.Type == "high" && prevHigh.Time != "" && peak.Value > prevHigh.Value
			isExtremeLow := peak.Type == "low" && prevLow.Time != "" && peak.Value < prevLow.Value

			if isExtremeHigh && state == "rising" {
				newStatus = "warning"
				summary = fmt.Sprintf("Extreme %s high tide rising: %.2f ft", ws.label, peak.Value)
				text = fmt.Sprintf("Station %s: rising toward extreme %s high tide %.2f ft at %s (record %.2f ft)",
					svc.stationID, ws.label, peak.Value, peak.Time, prevHigh.Value)
				metric = peak.Value
				data["peak"] = peak
				data["previous"] = prevHigh
			} else if isExtremeLow && state == "falling" {
				newStatus = "warning"
				summary = fmt.Sprintf("Extreme %s low tide falling: %.2f ft", ws.label, peak.Value)
				text = fmt.Sprintf("Station %s: falling toward extreme %s low tide %.2f ft at %s (record %.2f ft)",
					svc.stationID, ws.label, peak.Value, peak.Time, prevLow.Value)
				metric = peak.Value
				data["peak"] = peak
				data["previous"] = prevLow
			}
		}

		if newStatus == "ok" {
			summary = fmt.Sprintf("%s tide: normal", ws.label)
			text = fmt.Sprintf("Station %s: %s tide is not extreme", svc.stationID, ws.label)
		}

		if svc.extremeTideStatus[ws.label] == newStatus {
			continue // no change — skip
		}
		svc.extremeTideStatus[ws.label] = newStatus
		changed = true
		toSend = append(toSend, pendingMsg{
			status:  newStatus,
			summary: summary,
			text:    text,
			metric:  metric,
			data:    data,
		})
	}

	if changed {
		statusCopy := make(map[string]string, len(svc.extremeTideStatus))
		for k, v := range svc.extremeTideStatus {
			statusCopy[k] = v
		}
		svc.mu.Unlock()
		if saveErr := svc.Deps.MustGetStateStore().Save(svc.extremeTideStatusKey(), statusCopy); saveErr != nil {
			logger.Warn("tidesNoaa: failed to save extreme tide status", "error", saveErr)
		}
	} else {
		svc.mu.Unlock()
	}

	for _, msg := range toSend {
		if err := messenger.Send(core.Message{
			ChannelName: svc.Cfg.Name,
			ServiceName: svc.Cfg.Name,
			ServiceType: svc.Cfg.Type,
			Event:       "extreme_tide",
			Status:      msg.status,
			Text:        msg.text,
			Summary:     msg.summary,
			Metric:      msg.metric,
			MetricName:  fmt.Sprintf("tide.%s", svc.stationID),
			Data:        msg.data,
		}); err != nil {
			return err
		}
	}
	return nil
}
