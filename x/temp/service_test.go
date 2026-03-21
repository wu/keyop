package temp

import (
	"encoding/json"
	"keyop/core"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

type mockStateStore struct {
	mu   sync.Mutex
	data map[string][]byte
}

func (m *mockStateStore) Save(key string, value interface{}) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	m.data[key] = b
	return nil
}

func (m *mockStateStore) Load(key string, value interface{}) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.data[key]
	if !ok {
		return nil
	}
	return json.Unmarshal(b, value)
}

// helper to build dependencies
func testDeps(t *testing.T) core.Dependencies {
	t.Helper()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	deps := core.Dependencies{}

	tmpDir, err := os.MkdirTemp("", "temp_test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("failed to remove %s: %v", tmpDir, err)
		}
	})

	deps.SetOsProvider(core.OsProvider{})
	deps.SetLogger(logger)
	messenger := core.NewMessenger(logger, deps.MustGetOsProvider())
	messenger.SetDataDir(tmpDir)
	deps.SetMessenger(messenger)
	deps.SetStateStore(&mockStateStore{data: make(map[string][]byte)})

	return deps
}

func writeTempFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatalf("failed writing temp device file: %v", err)
	}
	return p
}

func Test_temp_success(t *testing.T) {
	deps := testDeps(t)
	dir := t.TempDir()
	// Typical w1_slave content includes a line with t=VALUE
	p := writeTempFile(t, dir, "w1_slave", "aa bb cc YES\nxyz t=23125\n")

	cfg := core.ServiceConfig{
		Name: "temp_sensor",
		Config: map[string]interface{}{
			"devicePath": p,
		},
	}
	svc := NewService(deps, cfg).(*Service)
	errs := svc.ValidateConfig()
	assert.Empty(t, errs)
	err := svc.Initialize()
	assert.NoError(t, err)

	got, err := svc.temp()
	assert.NoError(t, err)
	assert.Empty(t, got.Error)
	assert.InDelta(t, 23.125, got.TempC, 0.0001)
	assert.InDelta(t, 73.625, got.TempF, 0.0001)

}

func Test_temp_read_error(t *testing.T) {
	deps := testDeps(t)

	// point to a non-existent file
	p := filepath.Join(t.TempDir(), "does-not-exist")

	cfg := core.ServiceConfig{
		Name: "temp_sensor",
		Config: map[string]interface{}{
			"devicePath": p,
		},
	}
	svc := NewService(deps, cfg).(*Service)
	_, err := svc.temp()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "does-not-exist")
}

func Test_temp_empty_content(t *testing.T) {
	deps := testDeps(t)
	dir := t.TempDir()
	p := writeTempFile(t, dir, "w1_slave", "")

	cfg := core.ServiceConfig{
		Name: "temp_sensor",
		Config: map[string]interface{}{
			"devicePath": p,
		},
	}
	svc := NewService(deps, cfg).(*Service)
	_, err := svc.temp()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no content retrieved from temp device")
}

func Test_temp_bad_integer(t *testing.T) {
	deps := testDeps(t)
	dir := t.TempDir()
	p := writeTempFile(t, dir, "w1_slave", "crc YES\nvalue t=abc\n")

	cfg := core.ServiceConfig{
		Name: "temp_sensor",
		Config: map[string]interface{}{
			"devicePath": p,
		},
	}
	svc := NewService(deps, cfg).(*Service)
	_, err := svc.temp()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unable to convert temp string to int")
}

func Test_temp_max_temp_exceeded(t *testing.T) {
	deps := testDeps(t)
	dir := t.TempDir()
	// 100.000C = 212F
	p := writeTempFile(t, dir, "w1_slave", "aa bb cc YES\nxyz t=100000\n")

	cfg := core.ServiceConfig{
		Name: "temp_sensor",
		Config: map[string]interface{}{
			"devicePath": p,
			"maxTemp":    float64(100), // Max 100F
		},
	}
	svc := NewService(deps, cfg).(*Service)
	err := svc.Initialize()
	assert.NoError(t, err)

	got, err := svc.temp()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "temperature 212.000 exceeds max 100.000")
	assert.Equal(t, float32(100), got.TempC)
	assert.Equal(t, float32(212), got.TempF)
}
