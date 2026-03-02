package tidesNoaa

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"keyop/core"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

type mockMessenger struct {
	messages []core.Message
	mu       sync.Mutex
}

func (m *mockMessenger) Send(msg core.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, msg)
	return nil
}

func (m *mockMessenger) Subscribe(_ context.Context, _ string, _ string, _ string, _ string, _ time.Duration, _ func(core.Message) error) error {
	return nil
}

func (m *mockMessenger) SubscribeExtended(_ context.Context, _ string, _ string, _ string, _ string, _ time.Duration, _ func(core.Message, string, int64) error) error {
	return nil
}

func (m *mockMessenger) SetReaderState(_ string, _ string, _ string, _ int64) error { return nil }
func (m *mockMessenger) SeekToEnd(_ string, _ string) error                         { return nil }
func (m *mockMessenger) SetDataDir(_ string)                                        {}
func (m *mockMessenger) SetHostname(_ string)                                       {}
func (m *mockMessenger) GetStats() core.MessengerStats                              { return core.MessengerStats{} }

// mockOsProvider intercepts file I/O into an in-memory map while delegating
// MkdirAll to the real OS so directory creation works.
type mockOsProvider struct {
	core.OsProvider
	dir   string
	files map[string][]byte
	mu    sync.Mutex
}

func newMockOsProvider(t *testing.T) *mockOsProvider {
	t.Helper()
	return &mockOsProvider{
		dir:   t.TempDir(),
		files: make(map[string][]byte),
	}
}

func (m *mockOsProvider) UserHomeDir() (string, error) { return m.dir, nil }

func (m *mockOsProvider) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

func (m *mockOsProvider) ReadFile(name string) ([]byte, error) {
	m.mu.Lock()
	data, ok := m.files[name]
	m.mu.Unlock()
	if ok {
		return data, nil
	}
	return os.ReadFile(name)
}

func (m *mockOsProvider) OpenFile(name string, _ int, _ os.FileMode) (core.FileApi, error) {
	return &writeCapture{name: name, provider: m}, nil
}

func (m *mockOsProvider) ReadDir(dirname string) ([]os.DirEntry, error) {
	// Real on-disk entries (may be empty for a temp dir).
	real, _ := os.ReadDir(dirname)
	seen := make(map[string]bool, len(real))
	result := make([]os.DirEntry, 0, len(real))
	for _, e := range real {
		seen[e.Name()] = true
		result = append(result, e)
	}
	// Also expose any in-memory files written via OpenFile/writeCapture.
	m.mu.Lock()
	defer m.mu.Unlock()
	for path := range m.files {
		if filepath.Dir(path) == dirname {
			name := filepath.Base(path)
			if !seen[name] {
				seen[name] = true
				result = append(result, &fakeDirEntry{name: name})
			}
		}
	}
	return result, nil
}

type fakeDirEntry struct{ name string }

func (e *fakeDirEntry) Name() string               { return e.name }
func (e *fakeDirEntry) IsDir() bool                { return false }
func (e *fakeDirEntry) Type() os.FileMode          { return 0 }
func (e *fakeDirEntry) Info() (os.FileInfo, error) { return nil, nil }

type writeCapture struct {
	name     string
	provider *mockOsProvider
	buf      bytes.Buffer
}

func (w *writeCapture) Write(p []byte) (int, error)        { return w.buf.Write(p) }
func (w *writeCapture) WriteString(s string) (int, error)  { return w.buf.WriteString(s) }
func (w *writeCapture) Read(p []byte) (int, error)         { return w.buf.Read(p) }
func (w *writeCapture) Seek(_ int64, _ int) (int64, error) { return 0, nil }
func (w *writeCapture) Close() error {
	w.provider.mu.Lock()
	defer w.provider.mu.Unlock()
	w.provider.files[w.name] = w.buf.Bytes()
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func makeDeps(t *testing.T, messenger core.MessengerApi, osProvider core.OsProviderApi) core.Dependencies {
	t.Helper()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	deps := core.Dependencies{}
	deps.SetLogger(logger)
	deps.SetMessenger(messenger)
	deps.SetOsProvider(osProvider)
	deps.SetStateStore(&core.NoOpStateStore{})
	deps.SetContext(context.Background())
	return deps
}

func makeCfg(stationID string, extraConfig map[string]interface{}) core.ServiceConfig {
	cfg := core.ServiceConfig{
		Name: "tide-test",
		Type: "tidesNoaa",
		Pubs: map[string]core.ChannelInfo{},
		Config: map[string]interface{}{
			"stationId": stationID,
		},
	}
	for k, v := range extraConfig {
		cfg.Config[k] = v
	}
	return cfg
}

// buildRecords builds count 6-minute interval records starting from
// base.
func buildRecords(base time.Time, count int) []TideRecord {
	records := make([]TideRecord, count)
	for i := 0; i < count; i++ {
		records[i] = TideRecord{
			Time:  base.Add(time.Duration(i) * 6 * time.Minute).Format(noaaTimeFormat),
			Value: float64(i) * 0.1,
		}
	}
	return records
}

// mockNoaaServer returns a test server that serves a per-day records response.
// The records map is keyed by date string (YYYYMMDD).
func mockNoaaServer(t *testing.T, recordsByDate map[string][]TideRecord, errMsg string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if errMsg != "" {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"error": map[string]string{"message": errMsg},
			})
			return
		}
		date := r.URL.Query().Get("begin_date")
		records, ok := recordsByDate[date]
		if !ok || len(records) == 0 {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"error": map[string]string{"message": "No data for " + date},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"predictions": records})
	}))
}

// seedDayFile writes a TideDayFile into the mock OS provider's in-memory store.
func seedDayFile(t *testing.T, svc *Service, day time.Time, records []TideRecord, fetchedAt time.Time) {
	t.Helper()
	f := TideDayFile{
		StationID: svc.stationID,
		Date:      day.Format(fileDateFormat),
		FetchedAt: fetchedAt,
		Records:   records,
	}
	data, err := yaml.Marshal(f)
	require.NoError(t, err)

	path := svc.dayFilePath(day)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))

	osP := svc.Deps.MustGetOsProvider().(*mockOsProvider)
	osP.mu.Lock()
	osP.files[path] = data
	osP.mu.Unlock()
}

// ---------------------------------------------------------------------------
// tides.go – unit tests
// ---------------------------------------------------------------------------

func TestLocalMidnight(t *testing.T) {
	// localMidnight should return midnight in the local zone, not UTC.
	loc := time.FixedZone("UTC-8", -8*3600)
	ts := time.Date(2026, 3, 1, 14, 30, 0, 0, loc)
	got := localMidnight(ts)
	assert.Equal(t, 2026, got.Year())
	assert.Equal(t, time.March, got.Month())
	assert.Equal(t, 1, got.Day())
	assert.Equal(t, 0, got.Hour())
	assert.Equal(t, 0, got.Minute())
	assert.Equal(t, loc, got.Location())
}

func TestDayFileStale(t *testing.T) {
	now := time.Now()

	t.Run("nil file is stale", func(t *testing.T) {
		assert.True(t, dayFileStale(nil, 0, now))
	})

	t.Run("empty records is stale", func(t *testing.T) {
		assert.True(t, dayFileStale(&TideDayFile{}, 0, now))
	})

	t.Run("past day (offset -1) is never stale", func(t *testing.T) {
		f := &TideDayFile{FetchedAt: now.Add(-25 * time.Hour), Records: buildRecords(now.AddDate(0, 0, -1), 10)}
		assert.False(t, dayFileStale(f, -1, now))
	})

	t.Run("past day (offset -30) is never stale", func(t *testing.T) {
		f := &TideDayFile{FetchedAt: now.Add(-800 * time.Hour), Records: buildRecords(now.AddDate(0, 0, -30), 10)}
		assert.False(t, dayFileStale(f, -30, now))
	})

	t.Run("today (offset 0) fetched recently is fresh", func(t *testing.T) {
		f := &TideDayFile{FetchedAt: now.Add(-30 * time.Minute), Records: buildRecords(now, 10)}
		assert.False(t, dayFileStale(f, 0, now))
	})

	t.Run("today (offset 0) fetched over 1 hour ago is stale", func(t *testing.T) {
		f := &TideDayFile{FetchedAt: now.Add(-90 * time.Minute), Records: buildRecords(now, 10)}
		assert.True(t, dayFileStale(f, 0, now))
	})

	t.Run("tomorrow (offset 1) fetched recently is fresh", func(t *testing.T) {
		f := &TideDayFile{FetchedAt: now.Add(-30 * time.Minute), Records: buildRecords(now.AddDate(0, 0, 1), 10)}
		assert.False(t, dayFileStale(f, 1, now))
	})

	t.Run("tomorrow (offset 1) fetched over 1 hour ago is stale", func(t *testing.T) {
		f := &TideDayFile{FetchedAt: now.Add(-90 * time.Minute), Records: buildRecords(now.AddDate(0, 0, 1), 10)}
		assert.True(t, dayFileStale(f, 1, now))
	})

	t.Run("day+2 remains fresh even if fetched long ago", func(t *testing.T) {
		f := &TideDayFile{FetchedAt: now.Add(-48 * time.Hour), Records: buildRecords(now.AddDate(0, 0, 2), 10)}
		assert.False(t, dayFileStale(f, 2, now))
	})

	t.Run("day+3 remains fresh even if fetched long ago", func(t *testing.T) {
		f := &TideDayFile{FetchedAt: now.Add(-72 * time.Hour), Records: buildRecords(now.AddDate(0, 0, 3), 10)}
		assert.False(t, dayFileStale(f, 3, now))
	})
}

func TestFetchDayRecords(t *testing.T) {
	today := time.Now()
	records := buildRecords(today.Truncate(24*time.Hour), 240) // full day of 6-min records

	t.Run("returns records from API", func(t *testing.T) {
		server := mockNoaaServer(t, map[string][]TideRecord{
			today.Format(noaaDateFormat): records,
		}, "")
		defer server.Close()

		got, err := fetchDayRecords(server.URL, "9414290", today)
		require.NoError(t, err)
		require.Len(t, got, len(records))
		assert.Equal(t, records[0].Time, got[0].Time)
		assert.InDelta(t, records[1].Value, got[1].Value, 0.001)
	})

	t.Run("propagates API-level error", func(t *testing.T) {
		server := mockNoaaServer(t, nil, "Station ID not found")
		defer server.Close()

		_, err := fetchDayRecords(server.URL, "INVALID", today)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Station ID not found")
	})

	t.Run("errors on non-200 status", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		defer server.Close()

		_, err := fetchDayRecords(server.URL, "9414290", today)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "503")
	})

	t.Run("errors when data array is empty", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"predictions": []TideRecord{}})
		}))
		defer server.Close()

		_, err := fetchDayRecords(server.URL, "9414290", today)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no records")
	})
}

func TestFindCurrentTide(t *testing.T) {
	now := time.Now().Truncate(time.Minute)

	t.Run("returns most-recent past record and next", func(t *testing.T) {
		records := []TideRecord{
			{Time: now.Add(-12 * time.Minute).Format(noaaTimeFormat), Value: 3.1},
			{Time: now.Add(-6 * time.Minute).Format(noaaTimeFormat), Value: 3.4},
			{Time: now.Add(6 * time.Minute).Format(noaaTimeFormat), Value: 3.7},
		}
		curr, next, err := findCurrentTide(records, now)
		require.NoError(t, err)
		assert.InDelta(t, 3.4, curr.Value, 0.001)
		require.NotNil(t, next)
		assert.InDelta(t, 3.7, next.Value, 0.001)
	})

	t.Run("all records in future uses first", func(t *testing.T) {
		records := []TideRecord{
			{Time: now.Add(6 * time.Minute).Format(noaaTimeFormat), Value: 1.0},
			{Time: now.Add(12 * time.Minute).Format(noaaTimeFormat), Value: 1.1},
		}
		curr, next, err := findCurrentTide(records, now)
		require.NoError(t, err)
		assert.InDelta(t, 1.0, curr.Value, 0.001)
		require.NotNil(t, next)
		assert.InDelta(t, 1.1, next.Value, 0.001)
	})

	t.Run("single past record has nil next", func(t *testing.T) {
		records := []TideRecord{
			{Time: now.Add(-6 * time.Minute).Format(noaaTimeFormat), Value: 2.5},
		}
		curr, next, err := findCurrentTide(records, now)
		require.NoError(t, err)
		assert.InDelta(t, 2.5, curr.Value, 0.001)
		assert.Nil(t, next)
	})

	t.Run("empty records returns error", func(t *testing.T) {
		_, _, err := findCurrentTide(nil, now)
		assert.Error(t, err)
	})
}

func TestTideState(t *testing.T) {
	now := time.Now().Truncate(time.Minute)

	rising := []TideRecord{
		{Time: now.Add(-12 * time.Minute).Format(noaaTimeFormat), Value: 2.0},
		{Time: now.Add(-6 * time.Minute).Format(noaaTimeFormat), Value: 2.5},
		{Time: now.Format(noaaTimeFormat), Value: 3.0},
	}
	falling := []TideRecord{
		{Time: now.Add(-12 * time.Minute).Format(noaaTimeFormat), Value: 5.0},
		{Time: now.Add(-6 * time.Minute).Format(noaaTimeFormat), Value: 4.5},
		{Time: now.Format(noaaTimeFormat), Value: 4.0},
	}

	t.Run("rising", func(t *testing.T) {
		curr := &rising[2]
		assert.Equal(t, "rising", tideState(rising, curr))
	})

	t.Run("falling", func(t *testing.T) {
		curr := &falling[2]
		assert.Equal(t, "falling", tideState(falling, curr))
	})

	t.Run("first record returns empty", func(t *testing.T) {
		curr := &rising[0]
		assert.Equal(t, "", tideState(rising, curr))
	})

	t.Run("nil current returns empty", func(t *testing.T) {
		assert.Equal(t, "", tideState(rising, nil))
	})
}

func TestNextPeak(t *testing.T) {
	now := time.Now().Truncate(time.Minute)

	makePeak := func(base time.Time, values []float64) []TideRecord {
		r := make([]TideRecord, len(values))
		for i, v := range values {
			r[i] = TideRecord{
				Time:  base.Add(time.Duration(i) * 6 * time.Minute).Format(noaaTimeFormat),
				Value: v,
			}
		}
		return r
	}

	t.Run("finds next high peak", func(t *testing.T) {
		records := makePeak(now.Add(-6*time.Minute), []float64{3.0, 3.5, 4.0, 4.5, 5.0, 4.5, 4.0})
		curr := &records[1] // currently rising at 3.5
		peak := nextPeak(records, curr)
		require.NotNil(t, peak)
		assert.Equal(t, "high", peak.Type)
		assert.InDelta(t, 5.0, peak.Value, 0.001)
	})

	t.Run("finds next low peak", func(t *testing.T) {
		records := makePeak(now.Add(-6*time.Minute), []float64{4.0, 3.5, 3.0, 2.5, 2.0, 2.5, 3.0})
		curr := &records[1] // currently falling at 3.5
		peak := nextPeak(records, curr)
		require.NotNil(t, peak)
		assert.Equal(t, "low", peak.Type)
		assert.InDelta(t, 2.0, peak.Value, 0.001)
	})

	t.Run("returns nil when no peak in window", func(t *testing.T) {
		records := makePeak(now, []float64{1.0, 1.5, 2.0, 2.5, 3.0})
		curr := &records[0]
		assert.Nil(t, nextPeak(records, curr))
	})

	t.Run("returns nil for nil current", func(t *testing.T) {
		records := makePeak(now, []float64{1.0, 2.0, 1.0})
		assert.Nil(t, nextPeak(records, nil))
	})
}

func TestRecentPeaks(t *testing.T) {
	now := time.Now().Truncate(time.Minute)
	build := func(values []float64) []TideRecord {
		r := make([]TideRecord, len(values))
		for i, v := range values {
			r[i] = TideRecord{
				Time:  now.Add(time.Duration(i) * 6 * time.Minute).Format(noaaTimeFormat),
				Value: v,
			}
		}
		return r
	}

	t.Run("detects high peak at current record (lookback=0)", func(t *testing.T) {
		records := build([]float64{3.0, 5.0, 3.0})
		peaks := recentPeaks(records, &records[1], 0)
		require.Len(t, peaks, 1)
		assert.Equal(t, "high", peaks[0].Type)
		assert.InDelta(t, 5.0, peaks[0].Value, 0.001)
	})

	t.Run("detects low peak at current record (lookback=0)", func(t *testing.T) {
		records := build([]float64{5.0, 1.0, 5.0})
		peaks := recentPeaks(records, &records[1], 0)
		require.Len(t, peaks, 1)
		assert.Equal(t, "low", peaks[0].Type)
		assert.InDelta(t, 1.0, peaks[0].Value, 0.001)
	})

	t.Run("finds peak one step behind current (lookback=1)", func(t *testing.T) {
		// index 1 is the peak; index 2 is current (Check ran late).
		records := build([]float64{3.0, 5.0, 4.0, 3.5})
		peaks := recentPeaks(records, &records[2], 1)
		require.Len(t, peaks, 1)
		assert.Equal(t, "high", peaks[0].Type)
		assert.InDelta(t, 5.0, peaks[0].Value, 0.001)
	})

	t.Run("finds peak two steps behind current (lookback=2)", func(t *testing.T) {
		// index 1 is the peak; current is index 3 (two steps late).
		records := build([]float64{3.0, 5.0, 4.0, 3.5, 3.0})
		peaks := recentPeaks(records, &records[3], 2)
		require.Len(t, peaks, 1)
		assert.Equal(t, "high", peaks[0].Type)
		assert.InDelta(t, 5.0, peaks[0].Value, 0.001)
	})

	t.Run("returns nil for non-peak window", func(t *testing.T) {
		records := build([]float64{3.0, 4.0, 5.0, 6.0})
		assert.Nil(t, recentPeaks(records, &records[2], 2))
	})

	t.Run("returns nil for first record", func(t *testing.T) {
		records := build([]float64{5.0, 3.0, 1.0})
		assert.Nil(t, recentPeaks(records, &records[0], 2))
	})

	t.Run("returns nil for nil current", func(t *testing.T) {
		records := build([]float64{3.0, 5.0, 3.0})
		assert.Nil(t, recentPeaks(records, nil, 2))
	})

	// --- plateau cases ---

	t.Run("plateau high peak detected (lookback=0)", func(t *testing.T) {
		// 3.0, 5.0, 5.0, 3.0 — plateau at indices 1-2; current is index 2
		records := build([]float64{3.0, 5.0, 5.0, 3.0})
		peaks := recentPeaks(records, &records[2], 1)
		require.Len(t, peaks, 1, "one peak for the plateau")
		assert.Equal(t, "high", peaks[0].Type)
		assert.InDelta(t, 5.0, peaks[0].Value, 0.001)
		// First record of the plateau should be reported.
		assert.Equal(t, records[1].Time, peaks[0].Time)
	})

	t.Run("plateau low peak detected (lookback=1)", func(t *testing.T) {
		// 5.0, 1.0, 1.0, 5.0 — plateau at indices 1-2; current is index 3
		records := build([]float64{5.0, 1.0, 1.0, 5.0})
		peaks := recentPeaks(records, &records[3], 2)
		require.Len(t, peaks, 1)
		assert.Equal(t, "low", peaks[0].Type)
		assert.InDelta(t, 1.0, peaks[0].Value, 0.001)
		assert.Equal(t, records[1].Time, peaks[0].Time)
	})

	t.Run("no duplicate peaks for single plateau in scan window", func(t *testing.T) {
		// Plateau spans indices 2-4; lookback=4 covers the whole thing.
		records := build([]float64{3.0, 4.0, 5.0, 5.0, 5.0, 4.0, 3.0})
		peaks := recentPeaks(records, &records[5], 4)
		assert.Len(t, peaks, 1, "plateau must produce exactly one peak")
	})
}

func TestNextPeakPlateau(t *testing.T) {
	now := time.Now().Truncate(time.Minute)
	build := func(values []float64) []TideRecord {
		r := make([]TideRecord, len(values))
		for i, v := range values {
			r[i] = TideRecord{
				Time:  now.Add(time.Duration(i) * 6 * time.Minute).Format(noaaTimeFormat),
				Value: v,
			}
		}
		return r
	}

	t.Run("detects high plateau as next peak", func(t *testing.T) {
		// rising to 5.0 plateau then falling — current at index 0
		records := build([]float64{3.0, 4.0, 5.0, 5.0, 4.0, 3.0})
		peak := nextPeak(records, &records[0])
		require.NotNil(t, peak)
		assert.Equal(t, "high", peak.Type)
		assert.InDelta(t, 5.0, peak.Value, 0.001)
		// First record of the plateau.
		assert.Equal(t, records[2].Time, peak.Time)
	})

	t.Run("detects low plateau as next peak", func(t *testing.T) {
		// falling to 1.0 plateau then rising — current at index 0
		records := build([]float64{4.0, 3.0, 1.0, 1.0, 3.0, 4.0})
		peak := nextPeak(records, &records[0])
		require.NotNil(t, peak)
		assert.Equal(t, "low", peak.Type)
		assert.InDelta(t, 1.0, peak.Value, 0.001)
		assert.Equal(t, records[2].Time, peak.Time)
	})

	t.Run("monotone rising with plateau — no reversal", func(t *testing.T) {
		// 1.0, 2.0, 3.0, 3.0, 4.0 — plateau then continues up, no peak
		records := build([]float64{1.0, 2.0, 3.0, 3.0, 4.0})
		assert.Nil(t, nextPeak(records, &records[0]))
	})
}

func TestDailyHighLow(t *testing.T) {
	day := time.Date(2026, 3, 1, 0, 0, 0, 0, time.Local)
	records := []TideRecord{
		{Time: "2026-03-01 06:00", Value: 3.0},
		{Time: "2026-03-01 12:00", Value: 9.5},
		{Time: "2026-03-01 18:00", Value: 1.2},
	}
	high, low := dailyHighLow(records, day)
	require.NotNil(t, high)
	require.NotNil(t, low)
	assert.InDelta(t, 9.5, high.Value, 0.001)
	assert.InDelta(t, 1.2, low.Value, 0.001)
	// RecordedAt should be noon of the day for both.
	assert.Equal(t, 12, high.RecordedAt.Hour())
	assert.Equal(t, 12, low.RecordedAt.Hour())

	t.Run("empty records returns nil", func(t *testing.T) {
		h, l := dailyHighLow(nil, day)
		assert.Nil(t, h)
		assert.Nil(t, l)
	})
}

func TestUpdateExtremes(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.Local)

	// dayRecords builds a one-record slice for a day daysAgo in the past.
	dayRecs := func(high, low float64, daysAgo int) ([]TideRecord, time.Time) {
		day := now.AddDate(0, 0, -daysAgo)
		return []TideRecord{
			{Time: day.Add(6 * time.Hour).Format(noaaTimeFormat), Value: high},
			{Time: day.Add(18 * time.Hour).Format(noaaTimeFormat), Value: low},
		}, day
	}

	t.Run("fresh state accumulates high and low per window", func(t *testing.T) {
		ex := TideExtremes{}

		// 12-lunar-cycle only (~354 days; use 200 days ago).
		r, d := dayRecs(15.0, 0.1, 200)
		ex = updateExtremes(ex, r, d, now)

		// 3-lunar-cycle only (~88.5 days; use 60 days ago).
		r, d = dayRecs(12.0, 0.5, 60)
		ex = updateExtremes(ex, r, d, now)

		// 1-lunar-cycle (~29.5 days; use 10 days ago).
		r, d = dayRecs(10.0, 1.0, 10)
		ex = updateExtremes(ex, r, d, now)

		assert.InDelta(t, 10.0, ex.Window1Lunar.High().Value, 0.001)
		assert.InDelta(t, 1.0, ex.Window1Lunar.Low().Value, 0.001)
		assert.InDelta(t, 12.0, ex.Window3Lunar.High().Value, 0.001)
		assert.InDelta(t, 0.5, ex.Window3Lunar.Low().Value, 0.001)
		assert.InDelta(t, 15.0, ex.Window12Lunar.High().Value, 0.001)
		assert.InDelta(t, 0.1, ex.Window12Lunar.Low().Value, 0.001)
	})

	t.Run("record older than 12 lunar cycles is ignored", func(t *testing.T) {
		ex := TideExtremes{}
		r, d := dayRecs(99.0, 0.0, 400)
		ex = updateExtremes(ex, r, d, now)
		assert.Equal(t, TideExtremeEntry{}, ex.Window12Lunar.High())
	})

	t.Run("leaderboard retains next-best after champion ages out", func(t *testing.T) {
		ex := TideExtremes{}
		// Seed two entries within the 1-lunar-cycle window (~29.5 days).
		r, d := dayRecs(11.0, 2.0, 25) // will age out after ~5 days
		ex = updateExtremes(ex, r, d, now)
		r, d = dayRecs(10.0, 3.0, 15) // stays in window
		ex = updateExtremes(ex, r, d, now)
		assert.InDelta(t, 11.0, ex.Window1Lunar.High().Value, 0.001)

		// Advance time 6 days — the day-25 entry (now 31 days old, > 29.53) expires.
		future := now.AddDate(0, 0, 6)
		r2, d2 := dayRecs(5.0, 4.0, 0)
		d2 = future
		ex = updateExtremes(ex, r2, d2, future)
		// 11.0 is gone; 10.0 is now the high.
		assert.InDelta(t, 10.0, ex.Window1Lunar.High().Value, 0.001)
	})

	t.Run("expired extreme is evicted on next update", func(t *testing.T) {
		ex := TideExtremes{}
		// 27 days ago — inside the 1-lunar-cycle window (28 days) at time of seeding.
		r, d := dayRecs(10.0, 1.0, 27)
		ex = updateExtremes(ex, r, d, now)
		assert.InDelta(t, 10.0, ex.Window1Lunar.High().Value, 0.001)

		// Advance 2 days — entry is now 29 days old, outside the 28-day window.
		future := now.AddDate(0, 0, 2)
		r2, d2 := dayRecs(5.0, 2.0, 0)
		d2 = future
		ex = updateExtremes(ex, r2, d2, future)
		assert.InDelta(t, 5.0, ex.Window1Lunar.High().Value, 0.001)
	})

	t.Run("single day seeds all applicable windows", func(t *testing.T) {
		ex := TideExtremes{}
		r, d := dayRecs(5.5, 1.5, 5)
		ex = updateExtremes(ex, r, d, now)
		assert.InDelta(t, 5.5, ex.Window1Lunar.High().Value, 0.001)
		assert.InDelta(t, 1.5, ex.Window1Lunar.Low().Value, 0.001)
		assert.InDelta(t, 5.5, ex.Window3Lunar.High().Value, 0.001)
		assert.InDelta(t, 5.5, ex.Window12Lunar.High().Value, 0.001)
	})

	t.Run("leaderboard is trimmed to maxExtremeEntries", func(t *testing.T) {
		ex := TideExtremes{}
		for i := 0; i < maxExtremeEntries+5; i++ {
			day := now.AddDate(0, 0, -i)
			r := []TideRecord{
				{Time: day.Format(noaaTimeFormat), Value: float64(i)},
			}
			ex = updateExtremes(ex, r, day, now)
		}
		assert.LessOrEqual(t, len(ex.Window12Lunar.Highs), maxExtremeEntries)
		assert.LessOrEqual(t, len(ex.Window12Lunar.Lows), maxExtremeEntries)
	})
}

func TestYAMLRoundTrip(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	original := TideDayFile{
		StationID: "9414290",
		Date:      "2026-03-01",
		FetchedAt: now,
		Records: []TideRecord{
			{Time: "2026-03-01 06:00", Value: 3.21},
			{Time: "2026-03-01 06:06", Value: 3.35},
		},
	}

	data, err := yaml.Marshal(original)
	require.NoError(t, err)

	var decoded TideDayFile
	require.NoError(t, yaml.Unmarshal(data, &decoded))

	assert.Equal(t, original.StationID, decoded.StationID)
	assert.Equal(t, original.Date, decoded.Date)
	require.Len(t, decoded.Records, 2)
	assert.InDelta(t, original.Records[1].Value, decoded.Records[1].Value, 0.001)
}

// ---------------------------------------------------------------------------
// service.go – unit tests
// ---------------------------------------------------------------------------

func TestBackfillExtremes(t *testing.T) {
	now := time.Now()
	osP := newMockOsProvider(t)
	deps := makeDeps(t, &mockMessenger{}, osP)
	svc := NewService(deps, makeCfg("9414290", nil)).(*Service)
	require.NoError(t, svc.Initialize())

	// Write a day file from 10 days ago with a clear high (9.0) and low (1.0).
	past := now.AddDate(0, 0, -10)
	pastRecords := []TideRecord{
		{Time: past.Add(-1 * time.Hour).Format(noaaTimeFormat), Value: 9.0},
		{Time: past.Format(noaaTimeFormat), Value: 5.0},
		{Time: past.Add(1 * time.Hour).Format(noaaTimeFormat), Value: 1.0},
	}
	seedDayFile(t, svc, past, pastRecords, past)

	// Backfill — simulates a fresh restart after the state file was deleted.
	svc.mu.Lock()
	svc.extremes = TideExtremes{}
	svc.mu.Unlock()
	svc.backfillExtremes(now)

	svc.mu.RLock()
	ex := svc.extremes
	svc.mu.RUnlock()

	assert.InDelta(t, 9.0, ex.Window1Lunar.High().Value, 0.001, "1-lunar-cycle high should be 9.0")
	assert.InDelta(t, 1.0, ex.Window1Lunar.Low().Value, 0.001, "1-lunar-cycle low should be 1.0")
	assert.InDelta(t, 9.0, ex.Window3Lunar.High().Value, 0.001)
	assert.InDelta(t, 9.0, ex.Window12Lunar.High().Value, 0.001)
}

func TestValidateConfig(t *testing.T) {
	t.Run("missing stationId", func(t *testing.T) {
		osP := newMockOsProvider(t)
		deps := makeDeps(t, &mockMessenger{}, osP)
		cfg := core.ServiceConfig{
			Name:   "tide-test",
			Type:   "tidesNoaa",
			Config: map[string]interface{}{},
		}
		svc := NewService(deps, cfg).(*Service)
		errs := svc.ValidateConfig()
		require.Len(t, errs, 1)
		assert.Contains(t, errs[0].Error(), "stationId")
	})

	t.Run("valid config", func(t *testing.T) {
		osP := newMockOsProvider(t)
		deps := makeDeps(t, &mockMessenger{}, osP)
		svc := NewService(deps, makeCfg("9414290", nil)).(*Service)
		errs := svc.ValidateConfig()
		assert.Empty(t, errs)
	})
}

func TestInitialize_DefaultDataDir(t *testing.T) {
	osP := newMockOsProvider(t)
	deps := makeDeps(t, &mockMessenger{}, osP)
	svc := NewService(deps, makeCfg("9414290", nil)).(*Service)

	require.NoError(t, svc.Initialize())
	assert.Equal(t, filepath.Join(osP.dir, ".keyop", "tides"), svc.dataDir)
	assert.Equal(t, "9414290", svc.stationID)
	// Station sub-directory should have been created.
	_, err := os.Stat(svc.stationDir())
	assert.NoError(t, err)
}

func TestInitialize_CustomDataDir(t *testing.T) {
	osP := newMockOsProvider(t)
	customDir := filepath.Join(t.TempDir(), "custom-tides")
	deps := makeDeps(t, &mockMessenger{}, osP)
	svc := NewService(deps, makeCfg("9414290", map[string]interface{}{
		"dataDir": customDir,
	})).(*Service)

	require.NoError(t, svc.Initialize())
	assert.Equal(t, customDir, svc.dataDir)
}

func TestDayFilePath(t *testing.T) {
	osP := newMockOsProvider(t)
	deps := makeDeps(t, &mockMessenger{}, osP)
	svc := NewService(deps, makeCfg("9414290", nil)).(*Service)
	require.NoError(t, svc.Initialize())

	day := time.Date(2026, 3, 1, 0, 0, 0, 0, time.Local)
	path := svc.dayFilePath(day)
	assert.Equal(t, filepath.Join(svc.stationDir(), "2026-03-01.yaml"), path)
}

func TestCheck_SendsMessageFromCache(t *testing.T) {
	now := time.Now()
	// Build a day's worth of 6-minute records; first record is 4 hours ago.
	records := buildRecords(now.Add(-4*time.Hour), 120)

	// Provide fresh records for all future days so no real network calls are made.
	recordsByDate := map[string][]TideRecord{}
	for i := 0; i <= fetchDays; i++ {
		day := now.AddDate(0, 0, i)
		recordsByDate[day.Format(noaaDateFormat)] = buildRecords(day.Truncate(24*time.Hour), 240)
	}
	server := mockNoaaServer(t, recordsByDate, "")
	defer server.Close()

	osP := newMockOsProvider(t)
	messenger := &mockMessenger{}
	deps := makeDeps(t, messenger, osP)

	svc := NewService(deps, makeCfg("9414290", nil)).(*Service)
	require.NoError(t, svc.Initialize())
	svc.apiBase = server.URL

	// Seed today's file as fresh — it should not be re-fetched.
	seedDayFile(t, svc, now, records, now)

	err := svc.Check()
	require.NoError(t, err)

	messenger.mu.Lock()
	msgs := messenger.messages
	messenger.mu.Unlock()

	require.GreaterOrEqual(t, len(msgs), 1)
	// First message must always be the tide event.
	msg := msgs[0]
	assert.Equal(t, "tide-test", msg.ChannelName)
	assert.Equal(t, "tide-test", msg.ServiceName)
	assert.Equal(t, "tidesNoaa", msg.ServiceType)
	assert.Equal(t, "tide", msg.Event)
	assert.Contains(t, msg.Text, "9414290")
	assert.NotEmpty(t, msg.Summary)
	assert.Equal(t, "tide.9414290", msg.MetricName)

	data, ok := msg.Data.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "9414290", data["stationId"])
	assert.NotNil(t, data["current"])
	assert.Contains(t, []string{"rising", "falling", ""}, data["state"])
	_ = data["nextPeak"]
}

func TestCheck_SendsExtremeTideWarning(t *testing.T) {
	now := time.Now()

	// Records: rising toward a peak of 20.0 (next peak after current).
	// current is at now-6m (rising), peak at now+6m.
	peakValue := 20.0
	records := []TideRecord{
		{Time: now.Add(-18 * time.Minute).Format(noaaTimeFormat), Value: 17.0},
		{Time: now.Add(-12 * time.Minute).Format(noaaTimeFormat), Value: 18.0},
		{Time: now.Add(-6 * time.Minute).Format(noaaTimeFormat), Value: 19.0},     // current (rising)
		{Time: now.Add(6 * time.Minute).Format(noaaTimeFormat), Value: peakValue}, // next peak
		{Time: now.Add(12 * time.Minute).Format(noaaTimeFormat), Value: 19.5},
		{Time: now.Add(18 * time.Minute).Format(noaaTimeFormat), Value: 19.0},
	}

	recordsByDate := map[string][]TideRecord{}
	for i := 0; i <= fetchDays; i++ {
		recordsByDate[now.AddDate(0, 0, i).Format(noaaDateFormat)] = buildRecords(now.AddDate(0, 0, i).Truncate(24*time.Hour), 10)
	}
	server := mockNoaaServer(t, recordsByDate, "")
	defer server.Close()

	osP := newMockOsProvider(t)
	messenger := &mockMessenger{}
	deps := makeDeps(t, messenger, osP)

	svc := NewService(deps, makeCfg("9414290", nil)).(*Service)
	require.NoError(t, svc.Initialize())
	svc.apiBase = server.URL
	seedDayFile(t, svc, now, records, now)

	// Pre-load extremes with a historical high of 15.0 so the upcoming
	// 20.0 peak would be a new record in all three windows.
	prevTime := now.AddDate(0, 0, -10)
	prevEntry := TideExtremeEntry{Value: 15.0, Time: prevTime.Format(noaaTimeFormat), RecordedAt: prevTime}
	lowEntry := TideExtremeEntry{Value: 0.5, Time: prevTime.Format(noaaTimeFormat), RecordedAt: prevTime}
	prevWindow := TideWindowExtremes{
		Highs: []TideExtremeEntry{prevEntry},
		Lows:  []TideExtremeEntry{lowEntry},
	}
	svc.mu.Lock()
	svc.extremes = TideExtremes{Window1Lunar: prevWindow, Window3Lunar: prevWindow, Window12Lunar: prevWindow}
	svc.lastBackfillDay = time.Now().Truncate(24 * time.Hour)
	svc.mu.Unlock()

	require.NoError(t, svc.Check())

	messenger.mu.Lock()
	msgs := messenger.messages
	messenger.mu.Unlock()

	events := map[string][]core.Message{}
	for _, m := range msgs {
		events[m.Event] = append(events[m.Event], m)
	}

	require.Len(t, events["tide"], 1, "expected exactly one tide message")

	warnings := filterByEventAndStatus(msgs, "extreme_tide", "warning")
	require.NotEmpty(t, warnings, "expected at least one extreme_tide warning")
	for _, w := range warnings {
		assert.Contains(t, w.Text, "9414290")
		assert.InDelta(t, peakValue, w.Metric, 0.001)
		d, ok := w.Data.(map[string]interface{})
		require.True(t, ok)
		assert.NotEmpty(t, d["window"])
		assert.NotNil(t, d["peak"])
		assert.NotNil(t, d["previous"])
	}
}

func filterByEventAndStatus(msgs []core.Message, event, status string) []core.Message {
	var out []core.Message
	for _, m := range msgs {
		if m.Event == event && m.Status == status {
			out = append(out, m)
		}
	}
	return out
}

func TestSendExtremeTideStatus(t *testing.T) {
	prevTime := time.Now().AddDate(0, 0, -10)
	prevEntry := func(v float64) TideExtremeEntry {
		return TideExtremeEntry{Value: v, Time: prevTime.Format(noaaTimeFormat), RecordedAt: prevTime}
	}
	windowWithHighLow := func(h, l float64) TideWindowExtremes {
		return TideWindowExtremes{
			Highs: []TideExtremeEntry{prevEntry(h)},
			Lows:  []TideExtremeEntry{prevEntry(l)},
		}
	}
	extremes := TideExtremes{
		Window1Lunar:  windowWithHighLow(10.0, 1.0),
		Window3Lunar:  windowWithHighLow(12.0, 0.5),
		Window12Lunar: windowWithHighLow(14.0, 0.2),
	}

	makeService := func(t *testing.T, messenger *mockMessenger) *Service {
		t.Helper()
		osP := newMockOsProvider(t)
		deps := makeDeps(t, messenger, osP)
		svc := NewService(deps, makeCfg("9414290", nil)).(*Service)
		require.NoError(t, svc.Initialize())
		svc.extremeTideStatus = make(map[string]string)
		return svc
	}

	t.Run("warning when rising toward extreme high", func(t *testing.T) {
		messenger := &mockMessenger{}
		svc := makeService(t, messenger)
		peak := &TidePeak{Type: "high", Value: 11.0, Time: "2026-03-01 14:00"} // beats 1-lunar-cycle record of 10.0
		require.NoError(t, svc.sendExtremeTideStatus(messenger, peak, "rising", extremes))
		messenger.mu.Lock()
		msgs := filterByEvent(messenger.messages, "extreme_tide")
		messenger.mu.Unlock()
		require.NotEmpty(t, msgs)
		warnings := 0
		for _, m := range msgs {
			if m.Status == "warning" {
				warnings++
				assert.Contains(t, m.Summary, "rising")
			}
		}
		assert.Greater(t, warnings, 0, "expected at least one warning")
	})

	t.Run("ok when rising but next peak is not extreme", func(t *testing.T) {
		messenger := &mockMessenger{}
		svc := makeService(t, messenger)
		peak := &TidePeak{Type: "high", Value: 8.0, Time: "2026-03-01 14:00"} // below all records
		require.NoError(t, svc.sendExtremeTideStatus(messenger, peak, "rising", extremes))
		messenger.mu.Lock()
		msgs := filterByEvent(messenger.messages, "extreme_tide")
		messenger.mu.Unlock()
		for _, m := range msgs {
			assert.Equal(t, "ok", m.Status)
		}
	})

	t.Run("warning when falling toward extreme low", func(t *testing.T) {
		messenger := &mockMessenger{}
		svc := makeService(t, messenger)
		peak := &TidePeak{Type: "low", Value: 0.1, Time: "2026-03-01 18:00"} // beats all low records
		require.NoError(t, svc.sendExtremeTideStatus(messenger, peak, "falling", extremes))
		messenger.mu.Lock()
		msgs := filterByEvent(messenger.messages, "extreme_tide")
		messenger.mu.Unlock()
		warnings := 0
		for _, m := range msgs {
			if m.Status == "warning" {
				warnings++
				assert.Contains(t, m.Summary, "falling")
			}
		}
		assert.Greater(t, warnings, 0)
	})

	t.Run("no message sent when status has not changed", func(t *testing.T) {
		messenger := &mockMessenger{}
		svc := makeService(t, messenger)
		// Pre-seed status as ok for all windows.
		svc.extremeTideStatus = map[string]string{
			"1-lunar-cycle": "ok", "3-lunar-cycles": "ok", "12-lunar-cycles": "ok",
		}
		peak := &TidePeak{Type: "high", Value: 8.0, Time: "2026-03-01 14:00"}
		require.NoError(t, svc.sendExtremeTideStatus(messenger, peak, "rising", extremes))
		messenger.mu.Lock()
		msgs := filterByEvent(messenger.messages, "extreme_tide")
		messenger.mu.Unlock()
		assert.Empty(t, msgs, "no message should be sent when status unchanged")
	})

	t.Run("status transitions from warning to ok when peak is no longer extreme", func(t *testing.T) {
		messenger := &mockMessenger{}
		svc := makeService(t, messenger)
		// Start in warning state.
		svc.extremeTideStatus = map[string]string{
			"1-lunar-cycle": "warning", "3-lunar-cycles": "warning", "12-lunar-cycles": "warning",
		}
		peak := &TidePeak{Type: "high", Value: 8.0, Time: "2026-03-01 14:00"} // not extreme
		require.NoError(t, svc.sendExtremeTideStatus(messenger, peak, "rising", extremes))
		messenger.mu.Lock()
		msgs := filterByEvent(messenger.messages, "extreme_tide")
		messenger.mu.Unlock()
		require.NotEmpty(t, msgs)
		for _, m := range msgs {
			assert.Equal(t, "ok", m.Status)
		}
	})
}

func TestCheck_SendsHighLowTideAlert(t *testing.T) {
	base := time.Now().Truncate(time.Minute)

	for _, tc := range []struct {
		name                    string
		pre, peak, post1, post2 float64
		peakType                string
		wantEvent               string
	}{
		{"high tide", 8.0, 10.2, 8.0, 7.5, "high", "high_tide_alert"},
		{"low tide", 3.0, 0.8, 3.0, 3.5, "low", "low_tide_alert"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			records := []TideRecord{
				{Time: base.Add(-12 * time.Minute).Format(noaaTimeFormat), Value: tc.pre},
				{Time: base.Add(-6 * time.Minute).Format(noaaTimeFormat), Value: tc.peak},  // peak = current
				{Time: base.Add(6 * time.Minute).Format(noaaTimeFormat), Value: tc.post1},  // future
				{Time: base.Add(12 * time.Minute).Format(noaaTimeFormat), Value: tc.post2}, // future
			}
			runHighLowAlertTest(t, base, records, tc.wantEvent, tc.peak, tc.peakType)
		})
	}

	t.Run("peak one step behind current (Check ran late)", func(t *testing.T) {
		// Peak at base-12m; current at base-6m (one step past the peak).
		records := []TideRecord{
			{Time: base.Add(-18 * time.Minute).Format(noaaTimeFormat), Value: 8.0},
			{Time: base.Add(-12 * time.Minute).Format(noaaTimeFormat), Value: 10.5}, // peak
			{Time: base.Add(-6 * time.Minute).Format(noaaTimeFormat), Value: 9.0},   // current
			{Time: base.Add(6 * time.Minute).Format(noaaTimeFormat), Value: 8.0},
		}
		runHighLowAlertTest(t, base, records, "high_tide_alert", 10.5, "high")
	})
}

// runHighLowAlertTest is shared scaffolding for high/low tide alert subtests.
func runHighLowAlertTest(t *testing.T, base time.Time, records []TideRecord, wantEvent string, wantMetric float64, peakType string) {
	t.Helper()
	recordsByDate := map[string][]TideRecord{}
	for i := 0; i <= fetchDays; i++ {
		recordsByDate[base.AddDate(0, 0, i).Format(noaaDateFormat)] = buildRecords(base.AddDate(0, 0, i).Truncate(24*time.Hour), 10)
	}
	server := mockNoaaServer(t, recordsByDate, "")
	defer server.Close()

	osP := newMockOsProvider(t)
	messenger := &mockMessenger{}
	deps := makeDeps(t, messenger, osP)
	svc := NewService(deps, makeCfg("9414290", nil)).(*Service)
	require.NoError(t, svc.Initialize())
	svc.apiBase = server.URL
	svc.lastBackfillDay = base.Truncate(24 * time.Hour)
	seedDayFile(t, svc, base, records, base)

	require.NoError(t, svc.Check())

	messenger.mu.Lock()
	msgs := messenger.messages
	messenger.mu.Unlock()

	evts := map[string][]core.Message{}
	for _, m := range msgs {
		evts[m.Event] = append(evts[m.Event], m)
	}

	require.Len(t, evts[wantEvent], 1, "expected exactly one %s", wantEvent)
	alert := evts[wantEvent][0]
	assert.InDelta(t, wantMetric, alert.Metric, 0.001)
	if peakType == "high" {
		assert.Contains(t, alert.Summary, "High tide")
	} else {
		assert.Contains(t, alert.Summary, "Low tide")
	}
	assert.Contains(t, alert.Summary, fmt.Sprintf("%.2f", wantMetric))
}

func TestCheck_NoDoublePeakAlert(t *testing.T) {
	base := time.Now().Truncate(time.Minute)

	records := []TideRecord{
		{Time: base.Add(-12 * time.Minute).Format(noaaTimeFormat), Value: 8.0},
		{Time: base.Add(-6 * time.Minute).Format(noaaTimeFormat), Value: 10.2}, // peak = current
		{Time: base.Add(6 * time.Minute).Format(noaaTimeFormat), Value: 8.0},
		{Time: base.Add(12 * time.Minute).Format(noaaTimeFormat), Value: 7.5},
	}
	recordsByDate := map[string][]TideRecord{}
	for i := 0; i <= fetchDays; i++ {
		recordsByDate[base.AddDate(0, 0, i).Format(noaaDateFormat)] = buildRecords(base.AddDate(0, 0, i).Truncate(24*time.Hour), 10)
	}
	server := mockNoaaServer(t, recordsByDate, "")
	defer server.Close()

	osP := newMockOsProvider(t)
	messenger := &mockMessenger{}
	deps := makeDeps(t, messenger, osP)
	svc := NewService(deps, makeCfg("9414290", nil)).(*Service)
	require.NoError(t, svc.Initialize())
	svc.apiBase = server.URL
	svc.lastBackfillDay = base.Truncate(24 * time.Hour)
	seedDayFile(t, svc, base, records, base)

	// First Check — alert should fire once.
	require.NoError(t, svc.Check())
	messenger.mu.Lock()
	firstCount := len(filterByEvent(messenger.messages, "high_tide_alert"))
	messenger.mu.Unlock()
	assert.Equal(t, 1, firstCount, "first Check should send exactly one high_tide_alert")

	// Second Check — same records, peak already alerted — must not re-fire.
	require.NoError(t, svc.Check())
	messenger.mu.Lock()
	secondCount := len(filterByEvent(messenger.messages, "high_tide_alert"))
	messenger.mu.Unlock()
	assert.Equal(t, 1, secondCount, "second Check must not duplicate the high_tide_alert")
}

func filterByEvent(msgs []core.Message, event string) []core.Message {
	var out []core.Message
	for _, m := range msgs {
		if m.Event == event {
			out = append(out, m)
		}
	}
	return out
}

func TestCheck_FetchesMissingDayFiles(t *testing.T) {
	now := time.Now()
	todayKey := now.Format(noaaDateFormat)
	records := buildRecords(now.Add(-1*time.Hour), 60)

	recordsByDate := map[string][]TideRecord{}
	// Provide records for today and all future days.
	for i := 0; i <= fetchDays; i++ {
		day := now.AddDate(0, 0, i)
		recordsByDate[day.Format(noaaDateFormat)] = buildRecords(day.Truncate(24*time.Hour), 240)
	}
	recordsByDate[todayKey] = records

	server := mockNoaaServer(t, recordsByDate, "")
	defer server.Close()

	osP := newMockOsProvider(t)
	messenger := &mockMessenger{}
	deps := makeDeps(t, messenger, osP)

	svc := NewService(deps, makeCfg("9414290", nil)).(*Service)
	require.NoError(t, svc.Initialize())
	svc.apiBase = server.URL

	err := svc.Check()
	require.NoError(t, err)

	messenger.mu.Lock()
	count := len(messenger.messages)
	messenger.mu.Unlock()
	assert.GreaterOrEqual(t, count, 1)
	assert.Equal(t, "tide", messenger.messages[0].Event)
}

func TestCheck_RefreshesStaleFile(t *testing.T) {
	now := time.Now()
	// Seed today with a file fetched 2 hours ago (stale).
	staleRecords := buildRecords(now.Add(-3*time.Hour), 30)
	freshRecords := buildRecords(now.Add(-1*time.Hour), 60)

	recordsByDate := map[string][]TideRecord{}
	for i := 0; i <= fetchDays; i++ {
		recordsByDate[now.AddDate(0, 0, i).Format(noaaDateFormat)] = buildRecords(now.AddDate(0, 0, i).Truncate(24*time.Hour), 240)
	}
	recordsByDate[now.Format(noaaDateFormat)] = freshRecords

	server := mockNoaaServer(t, recordsByDate, "")
	defer server.Close()

	osP := newMockOsProvider(t)
	messenger := &mockMessenger{}
	deps := makeDeps(t, messenger, osP)

	svc := NewService(deps, makeCfg("9414290", nil)).(*Service)
	require.NoError(t, svc.Initialize())
	svc.apiBase = server.URL

	// Seed stale file (FetchedAt = 2 hours ago).
	seedDayFile(t, svc, now, staleRecords, now.Add(-2*time.Hour))

	err := svc.Check()
	require.NoError(t, err)

	messenger.mu.Lock()
	msgs := messenger.messages
	messenger.mu.Unlock()
	require.GreaterOrEqual(t, len(msgs), 1)
	assert.Equal(t, "tide", msgs[0].Event)
}

func TestCheck_PropagatesTodayFetchError(t *testing.T) {
	server := mockNoaaServer(t, nil, "No data was found.")
	defer server.Close()

	osP := newMockOsProvider(t)
	messenger := &mockMessenger{}
	deps := makeDeps(t, messenger, osP)

	svc := NewService(deps, makeCfg("9414290", nil)).(*Service)
	require.NoError(t, svc.Initialize())
	svc.apiBase = server.URL

	err := svc.Check()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "No data was found.")

	messenger.mu.Lock()
	count := len(messenger.messages)
	messenger.mu.Unlock()
	assert.Equal(t, 0, count)
}

func TestCheck_UsesYesterdayRecordsNearMidnight(t *testing.T) {
	// Simulate "now" at 00:02 — today's first record is at 00:06, so we need
	// yesterday's last record to be the "current" one.
	today := time.Date(2026, 3, 1, 0, 2, 0, 0, time.Local)
	yesterday := today.AddDate(0, 0, -1)

	// Yesterday: records up to 23:54.
	yRecords := buildRecords(time.Date(2026, 2, 28, 0, 0, 0, 0, time.Local), 240)
	// Today: first record at 00:06.
	tRecords := buildRecords(time.Date(2026, 3, 1, 0, 6, 0, 0, time.Local), 10)

	osP := newMockOsProvider(t)
	messenger := &mockMessenger{}
	deps := makeDeps(t, messenger, osP)

	svc := NewService(deps, makeCfg("9414290", nil)).(*Service)
	require.NoError(t, svc.Initialize())

	// Seed both files as fresh (past day is never stale).
	seedDayFile(t, svc, yesterday, yRecords, today.Add(-1*time.Hour))
	seedDayFile(t, svc, today, tRecords, today)

	// Manually call collectRecordsAroundNow and findCurrentTide to verify.
	records, err := svc.collectRecordsAroundNow(today)
	require.NoError(t, err)
	assert.True(t, len(records) > 0)

	curr, _, err := findCurrentTide(records, today)
	require.NoError(t, err)
	// current must come from yesterday's records since today's first record is 00:06.
	currTime, _ := time.ParseInLocation(noaaTimeFormat, curr.Time, time.Local)
	assert.True(t, currTime.Before(today), "expected current record to be before midnight")
}
