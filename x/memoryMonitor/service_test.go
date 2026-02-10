package memoryMonitor

import (
	"bytes"
	"fmt"
	"keyop/core"
	"os"
	"runtime"
	"testing"
	"time"
)

type readWriteSeeker struct {
	*bytes.Reader
}

func (rws *readWriteSeeker) Write(p []byte) (n int, err error) {
	return 0, fmt.Errorf("read-only")
}

type mockMessenger struct {
	messages []core.Message
}

func (m *mockMessenger) Send(msg core.Message) error {
	m.messages = append(m.messages, msg)
	return nil
}

func (m *mockMessenger) Subscribe(sourceName string, channelName string, maxAge time.Duration, messageHandler func(core.Message) error) error {
	return nil
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
	messenger := &mockMessenger{}
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

	if len(messenger.messages) != 1 {
		t.Fatalf("Expected 1 message, got %d", len(messenger.messages))
	}

	msg := messenger.messages[0]
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
	messenger := &mockMessenger{}
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

	if len(messenger.messages) != 1 {
		t.Fatalf("Expected 1 message, got %d", len(messenger.messages))
	}

	msg := messenger.messages[0]
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
