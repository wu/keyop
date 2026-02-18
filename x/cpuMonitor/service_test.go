package cpuMonitor

import (
	"bytes"
	"context"
	"fmt"
	"keyop/core"
	"os"
	"runtime"
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

func (m *mockMessenger) Subscribe(ctx context.Context, sourceName string, channelName string, serviceType string, serviceName string, maxAge time.Duration, messageHandler func(core.Message) error) error {
	return nil
}

func (m *mockMessenger) SubscribeExtended(ctx context.Context, source string, channelName string, serviceType string, serviceName string, maxAge time.Duration, messageHandler func(core.Message, string, int64) error) error {
	return nil
}

func (m *mockMessenger) SetReaderState(channelName string, readerName string, fileName string, offset int64) error {
	return nil
}

func (m *mockMessenger) SeekToEnd(channelName string, readerName string) error {
	return nil
}

func (m *mockMessenger) SetDataDir(dir string) {}

func (m *mockMessenger) GetStats() core.MessengerStats {
	return core.MessengerStats{}
}

type readWriteSeeker struct {
	*bytes.Reader
}

func (rws *readWriteSeeker) Write(p []byte) (n int, err error) {
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
	svc.Initialize()

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
	svc.Initialize()
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
