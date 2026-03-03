package cpuMonitor

import (
	"bytes"
	"context"
	"fmt"
	"keyop/core"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

type mockMessenger struct {
	messages []core.Message
}

func (m *mockMessenger) Send(msg core.Message) error {
	m.messages = append(m.messages, msg)
	return nil
}

func (m *mockMessenger) Subscribe(_ context.Context, _ string, _ string, _ string, _ string, _ time.Duration, _ func(core.Message) error) error {
	return nil
}

func (m *mockMessenger) SubscribeExtended(_ context.Context, _ string, _ string, _ string, _ string, _ time.Duration, _ func(core.Message, string, int64) error) error {
	return nil
}

func (m *mockMessenger) SetReaderState(_ string, _ string, _ string, _ int64) error {
	return nil
}

func (m *mockMessenger) SeekToEnd(_ string, _ string) error {
	return nil
}

func (m *mockMessenger) SetDataDir(_ string) {}

func (m *mockMessenger) SetHostname(_ string) {}

func (m *mockMessenger) GetStats() core.MessengerStats {
	return core.MessengerStats{}
}

type readWriteSeeker struct {
	*bytes.Reader
}

func (rws *readWriteSeeker) Write(_ []byte) (n int, err error) {
	return 0, fmt.Errorf("read-only")
}

func TestCheck_Linux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Skipping Linux test on non-linux platform")
	}

	fakeOs := &core.FakeOsProvider{}
	fakeOs.OpenFileFunc = func(name string, flag int, perm os.FileMode) (core.FileApi, error) {
		if name == "/proc/stat" {
			content := "cpu  100 0 50 200 0 0 0 0 0 0\n"
			return &core.FakeFile{
				ReadWriteSeeker: &readWriteSeeker{Reader: bytes.NewReader([]byte(content))},
			}, nil
		}
		if name == "/proc/meminfo" {
			content := "MemTotal:       1000000 kB\nMemAvailable:    400000 kB\n"
			return &core.FakeFile{
				ReadWriteSeeker: &readWriteSeeker{Reader: bytes.NewReader([]byte(content))},
			}, nil
		}
		return nil, os.ErrNotExist
	}

	deps := core.Dependencies{}
	deps.SetOsProvider(fakeOs)
	deps.SetLogger(&core.FakeLogger{})
	messenger := &mockMessenger{}
	deps.SetMessenger(messenger)

	cfg := core.ServiceConfig{
		Name: "cpu_test",
		Pubs: map[string]core.ChannelInfo{
			"metrics": {Name: "metrics_channel"},
		},
	}

	svc := NewService(deps, cfg).(*Service)
	if err := svc.Initialize(); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// First check to initialize CPU stats
	err := svc.Check()
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}

	// Update fake /proc/stat and /proc/meminfo for second check
	fakeOs.OpenFileFunc = func(name string, flag int, perm os.FileMode) (core.FileApi, error) {
		if name == "/proc/stat" {
			// diffTotal = (200+100+300) - (100+50+200) = 600 - 350 = 250
			// diffIdle = 300 - 200 = 100
			// usage = (250 - 100) / 250 * 100 = 150 / 250 * 100 = 60%
			content := "cpu  200 0 100 300 0 0 0 0 0 0\n"
			return &core.FakeFile{
				ReadWriteSeeker: &readWriteSeeker{Reader: bytes.NewReader([]byte(content))},
			}, nil
		}
		if name == "/proc/meminfo" {
			content := "MemTotal:       1000000 kB\nMemAvailable:    400000 kB\n"
			return &core.FakeFile{
				ReadWriteSeeker: &readWriteSeeker{Reader: bytes.NewReader([]byte(content))},
			}, nil
		}
		return nil, os.ErrNotExist
	}

	err = svc.Check()
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}

	// Check for CPU metric (60.0%)
	foundCpu := false
	for _, msg := range messenger.messages {
		if msg.MetricName == "cpu_test.cpu" {
			if msg.Metric == 60.0 {
				foundCpu = true
			}
		}
	}

	if !foundCpu {
		t.Errorf("CPU metric not found or incorrect value: %v", messenger.messages)
	}
}

func TestCheck_Darwin(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Skipping Darwin test on non-darwin platform")
	}

	fakeOs := &core.FakeOsProvider{}
	fakeOs.CommandFunc = func(name string, arg ...string) core.CommandApi {
		return &core.FakeCommand{
			CombinedOutputFunc: func() ([]byte, error) {
				return []byte("CPU usage: 10.00% user, 5.00% sys, 85.00% idle\nPhysMem: 1G used (200M wired), 3G unused.\n"), nil
			},
		}
	}

	deps := core.Dependencies{}
	deps.SetOsProvider(fakeOs)
	deps.SetLogger(&core.FakeLogger{})
	messenger := &mockMessenger{}
	deps.SetMessenger(messenger)

	cfg := core.ServiceConfig{
		Name: "cpu_test",
		Pubs: map[string]core.ChannelInfo{
			"metrics": {Name: "metrics_channel"},
		},
	}

	svc := NewService(deps, cfg)
	if err := svc.Initialize(); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	err := svc.Check()
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}

	// CPU: 100 - 85 = 15%
	foundCpu := false
	for _, msg := range messenger.messages {
		if msg.MetricName == "cpu_test.cpu" && msg.Metric == 15.0 {
			foundCpu = true
		}
	}

	if !foundCpu {
		t.Errorf("CPU metric not found or incorrect value: %v", messenger.messages)
	}
}

func TestCheck_ErrorHandling(t *testing.T) {
	fakeOs := &core.FakeOsProvider{}
	fakeOs.OpenFileFunc = func(name string, flag int, perm os.FileMode) (core.FileApi, error) {
		return nil, fmt.Errorf("file error")
	}
	deps := core.Dependencies{}
	deps.SetOsProvider(fakeOs)
	deps.SetLogger(&core.FakeLogger{})
	messenger := &mockMessenger{}
	deps.SetMessenger(messenger)
	cfg := core.ServiceConfig{
		Name: "cpu_test",
		Pubs: map[string]core.ChannelInfo{
			"metrics": {Name: "metrics_channel"},
		},
	}
	svc := NewService(deps, cfg).(*Service)
	if err := svc.Initialize(); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	err := svc.Check()
	if err == nil {
		t.Error("Expected error from file failure, got nil")
	}
}

func TestCheck_UnsupportedPlatform(t *testing.T) {
	fakeOs := &core.FakeOsProvider{}
	deps := core.Dependencies{}
	deps.SetOsProvider(fakeOs)
	deps.SetLogger(&core.FakeLogger{})
	messenger := &mockMessenger{}
	deps.SetMessenger(messenger)
	cfg := core.ServiceConfig{
		Name: "cpu_test",
		Pubs: map[string]core.ChannelInfo{
			"metrics": {Name: "metrics_channel"},
		},
	}
	svc := NewService(deps, cfg).(*Service)
	// Simulate unsupported platform by calling getDarwinUsage and getLinuxCpuUsage directly
	// and by checking error handling in Check()
	origGOOS := runtime.GOOS
	if origGOOS == "linux" || origGOOS == "darwin" {
		t.Skip("Cannot test unsupported platform on supported OS")
	}
	err := svc.Check()
	if err == nil || !strings.Contains(err.Error(), "unsupported platform") {
		t.Error("Expected unsupported platform error, got", err)
	}
}

func TestInitialize_MissingConfig(t *testing.T) {
	deps := core.Dependencies{}
	deps.SetLogger(&core.FakeLogger{})
	deps.SetOsProvider(&core.FakeOsProvider{})
	cfg := core.ServiceConfig{
		Name:   "cpu_test",
		Config: map[string]interface{}{},
	}
	svc := NewService(deps, cfg).(*Service)
	err := svc.Initialize()
	if err != nil {
		t.Errorf("Unexpected error from Initialize: %v", err)
	}
	if svc.cpuMetricName != "cpu_test.cpu" {
		t.Errorf("Expected default cpuMetricName, got %s", svc.cpuMetricName)
	}
}

func TestValidateConfig_EdgeCases(t *testing.T) {
	deps := core.Dependencies{}
	deps.SetLogger(&core.FakeLogger{})
	cfg := core.ServiceConfig{
		Config: map[string]interface{}{},
	}
	svc := NewService(deps, cfg)
	errs := svc.ValidateConfig()
	if len(errs) != 0 {
		t.Errorf("Expected no errors for empty config, got %v", errs)
	}
}

// ── parseLinuxCpuStats (pure function, runs on any OS) ──────────────────────

func TestParseLinuxCpuStats_Valid(t *testing.T) {
	// total = 100+0+50+200 = 350, idle = fields[4] = 200
	content := "cpu  100 0 50 200 0 0 0 0 0 0\n"
	total, idle, err := parseLinuxCpuStats(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 350 {
		t.Errorf("expected total 350, got %d", total)
	}
	if idle != 200 {
		t.Errorf("expected idle 200, got %d", idle)
	}
}

func TestParseLinuxCpuStats_NoCpuLine(t *testing.T) {
	_, _, err := parseLinuxCpuStats("no cpu data here\n")
	if err == nil {
		t.Error("expected error for missing cpu line, got nil")
	}
}

func TestParseLinuxCpuStats_TooFewFields(t *testing.T) {
	_, _, err := parseLinuxCpuStats("cpu  100 0\n")
	if err == nil {
		t.Error("expected error for too-few fields, got nil")
	}
}

// ── getLinuxCpuUsage — uninitialized first call returns 0 ───────────────────

func TestGetLinuxCpuUsage_FirstCallReturnsZero(t *testing.T) {
	content := "cpu  100 0 50 200 0 0 0 0 0 0\n"
	fakeOs := &core.FakeOsProvider{}
	fakeOs.OpenFileFunc = func(name string, flag int, perm os.FileMode) (core.FileApi, error) {
		return &core.FakeFile{
			ReadWriteSeeker: &readWriteSeeker{Reader: bytes.NewReader([]byte(content))},
		}, nil
	}
	deps := core.Dependencies{}
	deps.SetOsProvider(fakeOs)
	deps.SetLogger(&core.FakeLogger{})
	svc := &Service{Deps: deps, initialized: false}
	usage, err := svc.getLinuxCpuUsage()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if usage != 0 {
		t.Errorf("expected 0 on first uninitialized call, got %f", usage)
	}
	if !svc.initialized {
		t.Error("expected svc.initialized to be true after first call")
	}
}

func TestGetLinuxCpuUsage_ZeroDiffTotal(t *testing.T) {
	// Two identical reads → diffTotal == 0 → usage == 0
	content := "cpu  100 0 50 200 0 0 0 0 0 0\n"
	fakeOs := &core.FakeOsProvider{}
	fakeOs.OpenFileFunc = func(name string, flag int, perm os.FileMode) (core.FileApi, error) {
		return &core.FakeFile{
			ReadWriteSeeker: &readWriteSeeker{Reader: bytes.NewReader([]byte(content))},
		}, nil
	}
	deps := core.Dependencies{}
	deps.SetOsProvider(fakeOs)
	deps.SetLogger(&core.FakeLogger{})
	svc := &Service{Deps: deps, initialized: true, lastTotal: 350, lastIdle: 200}
	usage, err := svc.getLinuxCpuUsage()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if usage != 0 {
		t.Errorf("expected 0 for zero diffTotal, got %f", usage)
	}
}

func TestGetLinuxCpuStats_ReadError(t *testing.T) {
	fakeOs := &core.FakeOsProvider{}
	fakeOs.OpenFileFunc = func(name string, flag int, perm os.FileMode) (core.FileApi, error) {
		// File opens OK but Read fails
		return &core.FakeFile{
			ReadWriteSeeker: &readWriteSeeker{Reader: bytes.NewReader([]byte{})},
		}, nil
	}
	deps := core.Dependencies{}
	deps.SetOsProvider(fakeOs)
	deps.SetLogger(&core.FakeLogger{})
	svc := &Service{Deps: deps}
	_, _, err := svc.getLinuxCpuStats()
	// Empty file → no "cpu " line found → error
	if err == nil {
		t.Error("expected error reading empty /proc/stat, got nil")
	}
}

// ── Initialize config key variants ──────────────────────────────────────────

func TestInitialize_CpuMetricNameKey(t *testing.T) {
	deps := core.Dependencies{}
	deps.SetLogger(&core.FakeLogger{})
	cfg := core.ServiceConfig{
		Name: "svc",
		Config: map[string]interface{}{
			"cpu_metric_name": "my.cpu",
		},
	}
	svc := NewService(deps, cfg).(*Service)
	if err := svc.Initialize(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if svc.cpuMetricName != "my.cpu" {
		t.Errorf("expected cpu_metric_name 'my.cpu', got %q", svc.cpuMetricName)
	}
}

func TestInitialize_MetricNameFallback(t *testing.T) {
	deps := core.Dependencies{}
	deps.SetLogger(&core.FakeLogger{})
	cfg := core.ServiceConfig{
		Name: "svc",
		Config: map[string]interface{}{
			"metric_name": "fallback.cpu",
		},
	}
	svc := NewService(deps, cfg).(*Service)
	if err := svc.Initialize(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if svc.cpuMetricName != "fallback.cpu" {
		t.Errorf("expected metric_name fallback 'fallback.cpu', got %q", svc.cpuMetricName)
	}
}

// ── getDarwinUsage — unparseable top output ──────────────────────────────────

func TestGetDarwinUsage_ParseError(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin only")
	}
	fakeOs := &core.FakeOsProvider{}
	fakeOs.CommandFunc = func(name string, arg ...string) core.CommandApi {
		return &core.FakeCommand{
			CombinedOutputFunc: func() ([]byte, error) {
				// No "CPU usage:" line with "idle" field
				return []byte("some unrelated output\n"), nil
			},
		}
	}
	deps := core.Dependencies{}
	deps.SetOsProvider(fakeOs)
	deps.SetLogger(&core.FakeLogger{})
	svc := &Service{Deps: deps}
	_, err := svc.getDarwinUsage()
	if err == nil {
		t.Error("expected parse error for unrecognised top output, got nil")
	}
}

func TestGetDarwinUsage_CommandError(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin only")
	}
	fakeOs := &core.FakeOsProvider{}
	fakeOs.CommandFunc = func(name string, arg ...string) core.CommandApi {
		return &core.FakeCommand{
			CombinedOutputFunc: func() ([]byte, error) {
				return nil, fmt.Errorf("top failed")
			},
		}
	}
	deps := core.Dependencies{}
	deps.SetOsProvider(fakeOs)
	deps.SetLogger(&core.FakeLogger{})
	svc := &Service{Deps: deps}
	_, err := svc.getDarwinUsage()
	if err == nil {
		t.Error("expected error from top command failure, got nil")
	}
}

// ── Check — messenger.Send error ────────────────────────────────────────────

type errorMessenger struct{}

func (e *errorMessenger) Send(_ core.Message) error { return fmt.Errorf("send failed") }
func (e *errorMessenger) Subscribe(_ context.Context, _, _, _, _ string, _ time.Duration, _ func(core.Message) error) error {
	return nil
}
func (e *errorMessenger) SubscribeExtended(_ context.Context, _, _, _, _ string, _ time.Duration, _ func(core.Message, string, int64) error) error {
	return nil
}
func (e *errorMessenger) SetReaderState(_, _, _ string, _ int64) error { return nil }
func (e *errorMessenger) SeekToEnd(_, _ string) error                  { return nil }
func (e *errorMessenger) SetDataDir(_ string)                          {}
func (e *errorMessenger) SetHostname(_ string)                         {}
func (e *errorMessenger) GetStats() core.MessengerStats                { return core.MessengerStats{} }

func TestCheck_SendError(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin only — Linux path would need /proc/stat mock")
	}
	fakeOs := &core.FakeOsProvider{}
	fakeOs.CommandFunc = func(name string, arg ...string) core.CommandApi {
		return &core.FakeCommand{
			CombinedOutputFunc: func() ([]byte, error) {
				return []byte("CPU usage: 10.00% user, 5.00% sys, 85.00% idle\n"), nil
			},
		}
	}
	deps := core.Dependencies{}
	deps.SetOsProvider(fakeOs)
	deps.SetLogger(&core.FakeLogger{})
	deps.SetMessenger(&errorMessenger{})
	cfg := core.ServiceConfig{
		Name: "cpu_test",
		Pubs: map[string]core.ChannelInfo{"metrics": {Name: "ch"}},
	}
	svc := NewService(deps, cfg).(*Service)
	svc.cpuMetricName = "cpu_test.cpu"
	// Check should return nil even when Send fails (it logs but doesn't propagate)
	err := svc.Check()
	if err != nil {
		t.Errorf("Check should not propagate Send error, got: %v", err)
	}
}
