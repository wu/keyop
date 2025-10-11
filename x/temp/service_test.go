package temp

import (
	"keyop/core"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_NewTempCmd(t *testing.T) {
	deps := testDeps()
	cmd := NewCmd(deps)

	// weak assertion
	assert.NotNil(t, cmd)
}

// helper to build dependencies
func testDeps() core.Dependencies {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	deps := core.Dependencies{}
	deps.SetHostname("test-host")
	deps.SetLogger(logger)

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
	deps := testDeps()
	dir := t.TempDir()
	// Typical w1_slave content includes a line with t=VALUE
	p := writeTempFile(t, dir, "w1_slave", "aa bb cc YES\nxyz t=23125\n")

	// Point the code to our test file
	devicePath = p

	svc := Service{Deps: deps}
	got, err := svc.temp()
	assert.NoError(t, err)
	assert.Empty(t, got.Error)
	assert.InDelta(t, 23.125, got.TempC, 0.0001)
	assert.InDelta(t, 73.625, got.TempF, 0.0001)
	assert.False(t, got.Now.IsZero(), "Now should be set")

}

func Test_temp_read_error(t *testing.T) {
	deps := testDeps()

	// point to a non-existent file
	devicePath = filepath.Join(t.TempDir(), "does-not-exist")

	svc := Service{Deps: deps}
	got, err := svc.temp()
	assert.Error(t, err)
	assert.Contains(t, got.Error, "could not read from")
}

func Test_temp_empty_content(t *testing.T) {
	deps := testDeps()
	dir := t.TempDir()
	p := writeTempFile(t, dir, "w1_slave", "")
	devicePath = p

	svc := Service{Deps: deps}
	got, err := svc.temp()
	assert.Error(t, err)
	assert.Contains(t, got.Error, "no content retrieved from temp device")
}

func Test_temp_bad_integer(t *testing.T) {
	deps := testDeps()
	dir := t.TempDir()
	p := writeTempFile(t, dir, "w1_slave", "crc YES\nvalue t=abc\n")
	devicePath = p

	svc := Service{Deps: deps}
	got, err := svc.temp()
	assert.Error(t, err)
	assert.Contains(t, got.Error, "unable to convert temp string to int")
}
