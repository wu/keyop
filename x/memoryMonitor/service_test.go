//nolint:revive
package memoryMonitor

import (
	"bytes"
	"context"
	"fmt"
	"keyop/core"
	"keyop/core/testutil"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

type readWriteSeeker struct {
	*bytes.Reader
}

func (rws *readWriteSeeker) Write(p []byte) (n int, err error) {
	return 0, fmt.Errorf("read-only")
}

func TestCheck_Darwin(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Skipping Darwin test on non-darwin platform")
	}
	fakeOs := &core.FakeOsProvider{}
	fakeOs.CommandFunc = func(name string, arg ...string) core.CommandApi {
		if name == "vm_stat" {
			return &core.FakeCommand{
				CombinedOutputFunc: func() ([]byte, error) {
					return []byte(`Mach Virtual Memory Statistics: (page size of 16384 bytes)
Pages free:                               87332.
Pages active:                            592381.
Pages inactive:                          582392.
Pages speculative:                        12345.
File-backed pages:                         1000.
`), nil
				},
			}
		}
		if name == "sysctl" && len(arg) > 1 && arg[1] == "hw.memsize" {
			return &core.FakeCommand{
				OutputFunc: func() ([]byte, error) {
					// (87332 + 1000) pages * 16384 bytes/page = 1,447,215,104 bytes "free"
					// We want to test a case where we have some utilization.
					// Let's say total memory is 14472151040 (10x "free" bytes), so utilization is 90%.
					return []byte("14472151040\n"), nil
				},
			}
		}
		return nil
	}

	deps := core.Dependencies{}
	deps.SetOsProvider(fakeOs)
	deps.SetLogger(&core.FakeLogger{})
	messenger := testutil.NewFakeMessenger()
	deps.SetMessenger(messenger)

	cfg := core.ServiceConfig{
		Name: "memory_test",
		Pubs: map[string]core.ChannelInfo{
			"metrics": {Name: "metrics_channel"},
		},
	}

	svc := NewService(deps, cfg)
	err := svc.Initialize()
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	err = svc.Check()
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}

	if len(messenger.SentMessages) != 1 {
		t.Fatalf("Expected 1 message, got %d", len(messenger.SentMessages))
	}

	msg := messenger.SentMessages[0]
	if msg.MetricName != "memory_test.utilized_percent" {
		t.Errorf("Expected metric name memory_test.utilized_percent, got %s", msg.MetricName)
	}

	// 90% utilized (10% free)
	if msg.Metric < 89.9 || msg.Metric > 90.1 {
		t.Errorf("Expected metric value approximately 90.0, got %f", msg.Metric)
	}
}

func TestParseVmStat_Error(t *testing.T) {
	_, err := parseVmStat("some random output")
	if err == nil {
		t.Error("Expected error when 'Pages free' is missing, got nil")
	}
}

func TestValidateConfig(t *testing.T) {
	deps := core.Dependencies{}
	deps.SetLogger(&core.FakeLogger{})

	t.Run("valid config", func(t *testing.T) {
		cfg := core.ServiceConfig{
			Pubs: map[string]core.ChannelInfo{
				"metrics": {Name: "metrics_channel"},
			},
			Config: map[string]interface{}{
				"metric_name": "custom.memory",
			},
		}
		svc := NewService(deps, cfg)
		errs := svc.ValidateConfig()
		if len(errs) > 0 {
			t.Errorf("Expected no errors, got %v", errs)
		}
	})

	t.Run("invalid metric_name type", func(t *testing.T) {
		cfg := core.ServiceConfig{
			Pubs: map[string]core.ChannelInfo{
				"metrics": {Name: "metrics_channel"},
			},
			Config: map[string]interface{}{
				"metric_name": 123,
			},
		}
		svc := NewService(deps, cfg)
		errs := svc.ValidateConfig()
		if len(errs) == 0 {
			t.Error("Expected error for invalid metric_name type, got none")
		}
	})
}

func TestCheck_Linux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Skipping Linux test on non-linux platform")
	}

	fakeOs := &core.FakeOsProvider{}
	fakeOs.OpenFileFunc = func(name string, flag int, perm os.FileMode) (core.FileApi, error) {
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
	messenger := testutil.NewFakeMessenger()
	deps.SetMessenger(messenger)

	cfg := core.ServiceConfig{
		Name: "memory_test",
		Pubs: map[string]core.ChannelInfo{
			"metrics": {Name: "metrics_channel"},
		},
	}

	svc := NewService(deps, cfg)
	err := svc.Initialize()
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	err = svc.Check()
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}

	if len(messenger.SentMessages) != 1 {
		t.Fatalf("Expected 1 message, got %d", len(messenger.SentMessages))
	}

	msg := messenger.SentMessages[0]
	// (1000000 - 400000) / 1000000 * 100 = 60.0%
	if msg.Metric != 60.0 {
		t.Errorf("Expected metric value 60.0, got %f", msg.Metric)
	}
}

func TestInitialize_CustomMetricName(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Skipping Darwin-only initialization test")
	}
	fakeOs := &core.FakeOsProvider{}
	fakeOs.CommandFunc = func(name string, arg ...string) core.CommandApi {
		if name == "sysctl" {
			return &core.FakeCommand{
				OutputFunc: func() ([]byte, error) {
					return []byte("16000000000\n"), nil
				},
			}
		}
		return nil
	}
	deps := core.Dependencies{}
	deps.SetOsProvider(fakeOs)
	deps.SetLogger(&core.FakeLogger{})

	cfg := core.ServiceConfig{
		Name: "test_service",
		Config: map[string]interface{}{
			"metric_name": "custom.metric",
		},
	}
	svc := NewService(deps, cfg).(*Service)
	err := svc.Initialize()
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	if svc.MetricName != "custom.metric" {
		t.Errorf("Expected MetricName to be custom.metric, got %s", svc.MetricName)
	}
	if svc.TotalMemoryBytes != 16000000000 {
		t.Errorf("Expected TotalMemoryBytes to be 16000000000, got %d", svc.TotalMemoryBytes)
	}
}

func TestCheck_ErrorHandling(t *testing.T) {
	switch runtime.GOOS {
	case "darwin":
		// On Darwin, Initialize() calls sysctl for total memory; mock it properly,
		// then fail only on the vm_stat call used during Check().
		fakeOs := &core.FakeOsProvider{}
		fakeOs.CommandFunc = func(name string, arg ...string) core.CommandApi {
			if name == "sysctl" {
				return &core.FakeCommand{
					OutputFunc: func() ([]byte, error) {
						return []byte("16000000000\n"), nil
					},
				}
			}
			// vm_stat called during Check() — return an error
			return &core.FakeCommand{
				CombinedOutputFunc: func() ([]byte, error) {
					return nil, fmt.Errorf("vm_stat error")
				},
			}
		}
		deps := core.Dependencies{}
		deps.SetOsProvider(fakeOs)
		deps.SetLogger(&core.FakeLogger{})
		messenger := testutil.NewFakeMessenger()
		deps.SetMessenger(messenger)
		cfg := core.ServiceConfig{
			Name: "memory_test",
			Pubs: map[string]core.ChannelInfo{
				"metrics": {Name: "metrics_channel"},
			},
		}
		svc := NewService(deps, cfg)
		err := svc.Initialize()
		if err != nil {
			t.Fatalf("Unexpected error from Initialize: %v", err)
		}
		err = svc.Check()
		if err == nil {
			t.Error("Expected error from command failure, got nil")
		}
	case "linux":
		fakeOs := &core.FakeOsProvider{}
		fakeOs.OpenFileFunc = func(name string, flag int, perm os.FileMode) (core.FileApi, error) {
			return nil, fmt.Errorf("file error")
		}
		deps := core.Dependencies{}
		deps.SetOsProvider(fakeOs)
		deps.SetLogger(&core.FakeLogger{})
		messenger := testutil.NewFakeMessenger()
		deps.SetMessenger(messenger)
		cfg := core.ServiceConfig{
			Name: "memory_test",
			Pubs: map[string]core.ChannelInfo{
				"metrics": {Name: "metrics_channel"},
			},
		}
		svc := NewService(deps, cfg)
		err := svc.Initialize()
		if err != nil {
			t.Fatalf("Unexpected error from Initialize: %v", err)
		}
		err = svc.Check()
		if err == nil {
			t.Error("Expected error from file failure, got nil")
		}
	default:
		t.Skip("Unsupported platform for TestCheck_ErrorHandling")
	}
}

func TestCheck_UnsupportedPlatform(t *testing.T) {
	fakeOs := &core.FakeOsProvider{}
	deps := core.Dependencies{}
	deps.SetOsProvider(fakeOs)
	deps.SetLogger(&core.FakeLogger{})
	messenger := testutil.NewFakeMessenger()
	deps.SetMessenger(messenger)
	cfg := core.ServiceConfig{
		Name: "memory_test",
		Pubs: map[string]core.ChannelInfo{
			"metrics": {Name: "metrics_channel"},
		},
	}
	svc := NewService(deps, cfg)
	origGOOS := runtime.GOOS
	if origGOOS == "linux" || origGOOS == "darwin" {
		t.Skip("Cannot test unsupported platform on supported OS")
	}
	err := svc.Initialize()
	if err != nil {
		t.Errorf("Unexpected error from Initialize: %v", err)
	}
	err = svc.Check()
	if err == nil || !strings.Contains(err.Error(), "unsupported platform") {
		t.Error("Expected unsupported platform error, got", err)
	}
}

func TestInitialize_MissingConfig(t *testing.T) {
	fakeOs := &core.FakeOsProvider{}
	if runtime.GOOS == "darwin" {
		// Initialize calls sysctl on Darwin to get total memory
		fakeOs.CommandFunc = func(name string, arg ...string) core.CommandApi {
			return &core.FakeCommand{
				OutputFunc: func() ([]byte, error) {
					return []byte("16000000000\n"), nil
				},
			}
		}
	}
	deps := core.Dependencies{}
	deps.SetOsProvider(fakeOs)
	deps.SetLogger(&core.FakeLogger{})
	cfg := core.ServiceConfig{
		Name:   "memory_test",
		Config: map[string]interface{}{},
	}
	svc := NewService(deps, cfg).(*Service)
	err := svc.Initialize()
	if err != nil {
		t.Fatalf("Unexpected error from Initialize: %v", err)
	}
	// When no metric_name is configured, default should be "<service_name>.utilized_percent"
	if svc.MetricName != "memory_test.utilized_percent" {
		t.Errorf("Expected default MetricName 'memory_test.utilized_percent', got %q", svc.MetricName)
	}
}

func TestValidateConfig_EdgeCases(t *testing.T) {
	deps := core.Dependencies{}
	deps.SetLogger(&core.FakeLogger{})
	cfg := core.ServiceConfig{
		Config: map[string]interface{}{
			"metric_name": 42, // invalid type — should error
		},
	}
	svc := NewService(deps, cfg)
	errs := svc.ValidateConfig()
	if len(errs) == 0 {
		t.Error("Expected error for invalid metric_name type, got none")
	}
}

// ── parseMeminfo (pure function, runs on any OS) ─────────────────────────────

func TestParseMeminfo_Valid(t *testing.T) {
	content := "MemTotal:       1000000 kB\nMemAvailable:    400000 kB\n"
	usage, err := parseMeminfo(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// (1000000-400000)/1000000*100 = 60.0
	if usage != 60.0 {
		t.Errorf("expected 60.0, got %f", usage)
	}
}

func TestParseMeminfo_MissingMemTotal(t *testing.T) {
	content := "MemAvailable:    400000 kB\n"
	_, err := parseMeminfo(content)
	if err == nil {
		t.Error("expected error for missing MemTotal, got nil")
	}
}

func TestParseMeminfo_SkipsShortLines(t *testing.T) {
	// A line with only one field should be skipped without panic
	content := "MemTotal:       1000000 kB\nMemAvailable:    500000 kB\nSomeKey\n"
	usage, err := parseMeminfo(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if usage != 50.0 {
		t.Errorf("expected 50.0, got %f", usage)
	}
}

// ── getLinuxMemUsage — read error path ───────────────────────────────────────

func TestGetLinuxMemUsage_ReadError(t *testing.T) {
	fakeOs := &core.FakeOsProvider{}
	fakeOs.OpenFileFunc = func(name string, flag int, perm os.FileMode) (core.FileApi, error) {
		// File opens fine but is empty → parseMeminfo will return an error
		return &core.FakeFile{
			ReadWriteSeeker: &readWriteSeeker{Reader: bytes.NewReader([]byte{})},
		}, nil
	}
	deps := core.Dependencies{}
	deps.SetOsProvider(fakeOs)
	deps.SetLogger(&core.FakeLogger{})
	svc := &Service{Deps: deps}
	_, err := svc.getLinuxMemUsage()
	if err == nil {
		t.Error("expected error for empty /proc/meminfo, got nil")
	}
}

// ── parseVmStat error branches ───────────────────────────────────────────────

func TestParseVmStat_MissingPageSize(t *testing.T) {
	_, err := parseVmStat("no page size line here\n")
	if err == nil {
		t.Error("expected error for missing page size, got nil")
	}
}

func TestParseVmStat_InvalidPageSize(t *testing.T) {
	// page size present but not a valid integer
	output := "Mach Virtual Memory Statistics: (page size of BADNUM bytes)\n" +
		"Pages free:    100.\nFile-backed pages:    50.\n"
	_, err := parseVmStat(output)
	if err == nil {
		t.Error("expected error for invalid page size, got nil")
	}
}

func TestParseVmStat_InvalidFreePages(t *testing.T) {
	output := "Mach Virtual Memory Statistics: (page size of 4096 bytes)\n" +
		"Pages free:    BADNUM.\nFile-backed pages:    50.\n"
	_, err := parseVmStat(output)
	if err == nil {
		t.Error("expected error for invalid free pages, got nil")
	}
}

func TestParseVmStat_InvalidFileBackedPages(t *testing.T) {
	output := "Mach Virtual Memory Statistics: (page size of 4096 bytes)\n" +
		"Pages free:    100.\nFile-backed pages:    BADNUM.\n"
	_, err := parseVmStat(output)
	if err == nil {
		t.Error("expected error for invalid file-backed pages, got nil")
	}
}

func TestParseVmStat_MissingFileBacked(t *testing.T) {
	output := "Mach Virtual Memory Statistics: (page size of 4096 bytes)\n" +
		"Pages free:    100.\n"
	_, err := parseVmStat(output)
	if err == nil {
		t.Error("expected error for missing File-backed pages, got nil")
	}
}

func TestParseVmStat_Valid(t *testing.T) {
	output := "Mach Virtual Memory Statistics: (page size of 4096 bytes)\n" +
		"Pages free:                               100.\n" +
		"File-backed pages:                         50.\n"
	freeBytes, err := parseVmStat(output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// (100+50)*4096 = 614400
	if freeBytes != 614400 {
		t.Errorf("expected 614400, got %d", freeBytes)
	}
}

// ── getDarwinMemUsage — vm_stat parse error ───────────────────────────────────

func TestGetDarwinMemUsage_ParseError(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin only")
	}
	fakeOs := &core.FakeOsProvider{}
	fakeOs.CommandFunc = func(name string, arg ...string) core.CommandApi {
		return &core.FakeCommand{
			CombinedOutputFunc: func() ([]byte, error) {
				return []byte("unparseable output\n"), nil
			},
		}
	}
	deps := core.Dependencies{}
	deps.SetOsProvider(fakeOs)
	deps.SetLogger(&core.FakeLogger{})
	svc := &Service{Deps: deps, TotalMemoryBytes: 16000000000}
	_, err := svc.getDarwinMemUsage()
	if err == nil {
		t.Error("expected parse error from bad vm_stat output, got nil")
	}
}

// ── Initialize — sysctl returns non-integer (Darwin) ─────────────────────────

func TestInitialize_SysctlParseError(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin only")
	}
	fakeOs := &core.FakeOsProvider{}
	fakeOs.CommandFunc = func(name string, arg ...string) core.CommandApi {
		return &core.FakeCommand{
			OutputFunc: func() ([]byte, error) {
				return []byte("not-a-number\n"), nil
			},
		}
	}
	deps := core.Dependencies{}
	deps.SetOsProvider(fakeOs)
	deps.SetLogger(&core.FakeLogger{})
	cfg := core.ServiceConfig{Name: "mem", Config: map[string]interface{}{}}
	svc := NewService(deps, cfg)
	err := svc.Initialize()
	if err == nil {
		t.Error("expected error when sysctl returns non-integer, got nil")
	}
}

// ── Check — messenger.Send error ─────────────────────────────────────────────

type errorMessenger struct {
	payloadRegistry core.PayloadRegistry
}

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

func (e *errorMessenger) GetPayloadRegistry() core.PayloadRegistry { return e.payloadRegistry }

func (e *errorMessenger) SetPayloadRegistry(r core.PayloadRegistry) { e.payloadRegistry = r }

func TestCheck_SendError(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin only — Linux would need /proc/meminfo mock")
	}
	fakeOs := &core.FakeOsProvider{}
	fakeOs.CommandFunc = func(name string, arg ...string) core.CommandApi {
		return &core.FakeCommand{
			CombinedOutputFunc: func() ([]byte, error) {
				return []byte(`Mach Virtual Memory Statistics: (page size of 4096 bytes)
Pages free:                               100.
File-backed pages:                         50.
`), nil
			},
		}
	}
	deps := core.Dependencies{}
	deps.SetOsProvider(fakeOs)
	deps.SetLogger(&core.FakeLogger{})
	deps.SetMessenger(&errorMessenger{})
	cfg := core.ServiceConfig{
		Name: "mem_test",
		Pubs: map[string]core.ChannelInfo{"metrics": {Name: "ch"}},
	}
	svc := NewService(deps, cfg).(*Service)
	svc.MetricName = "mem_test.utilized_percent"
	svc.TotalMemoryBytes = 16000000000
	err := svc.Check()
	if err == nil {
		t.Error("expected Check to propagate Send error, got nil")
	}
}
