package temp

import (
	"keyop/core"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

// helper to build dependencies
func testDeps(t *testing.T) core.Dependencies {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	deps := core.Dependencies{}

	tmpDir, err := os.MkdirTemp("", "httpPost_test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		os.RemoveAll(tmpDir)
	})

	deps.SetOsProvider(core.OsProvider{})
	deps.SetLogger(logger)
	messenger := core.NewMessenger(logger, deps.MustGetOsProvider())
	messenger.SetDataDir(tmpDir)

	deps.SetMessenger(messenger)

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
		Pubs: map[string]core.ChannelInfo{
			"events":  {Name: "temp"},
			"metrics": {Name: "metrics"},
		},
		Config: map[string]interface{}{
			"devicePath": p,
		},
	}
	svc := NewService(deps, cfg).(*Service)
	errs := svc.ValidateConfig()
	assert.Empty(t, errs)
	err := svc.Initialize()
	assert.NoError(t, err)

	// Validate config with correct pubs
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
		Pubs: map[string]core.ChannelInfo{
			"events":  {Name: "events"},
			"metrics": {Name: "metrics"},
		},
		Config: map[string]interface{}{
			"devicePath": p,
		},
	}
	svc := NewService(deps, cfg).(*Service)
	got, err := svc.temp()
	assert.Error(t, err)
	assert.Contains(t, got.Error, "could not read from")
}

func Test_temp_empty_content(t *testing.T) {
	deps := testDeps(t)
	dir := t.TempDir()
	p := writeTempFile(t, dir, "w1_slave", "")

	cfg := core.ServiceConfig{
		Pubs: map[string]core.ChannelInfo{
			"events":  {Name: "events"},
			"metrics": {Name: "metrics"},
		},
		Config: map[string]interface{}{
			"devicePath": p,
		},
	}
	svc := NewService(deps, cfg).(*Service)
	got, err := svc.temp()
	assert.Error(t, err)
	assert.Contains(t, got.Error, "no content retrieved from temp device")
}

func Test_temp_bad_integer(t *testing.T) {
	deps := testDeps(t)
	dir := t.TempDir()
	p := writeTempFile(t, dir, "w1_slave", "crc YES\nvalue t=abc\n")

	cfg := core.ServiceConfig{
		Pubs: map[string]core.ChannelInfo{
			"events":  {Name: "events"},
			"metrics": {Name: "metrics"},
		},
		Config: map[string]interface{}{
			"devicePath": p,
		},
	}
	svc := NewService(deps, cfg).(*Service)
	got, err := svc.temp()
	assert.Error(t, err)
	assert.Contains(t, got.Error, "unable to convert temp string to int")
}
