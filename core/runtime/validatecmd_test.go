package runtime

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/wu/keyop/core"
	"github.com/wu/keyop/core/testutil"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// validateTestService is a minimal core.Service whose validation outcome is
// controlled per registered type.
type validateTestService struct {
	errs []error
}

func (s *validateTestService) Check() error            { return nil }
func (s *validateTestService) Initialize() error       { return nil }
func (s *validateTestService) ValidateConfig() []error { return s.errs }

func init() {
	core.RegisterService("validatecmd-ok", func(_ core.Dependencies, _ core.ServiceConfig, _ context.Context) interface{} {
		return &validateTestService{}
	})
	core.RegisterService("validatecmd-bad", func(_ core.Dependencies, _ core.ServiceConfig, _ context.Context) interface{} {
		return &validateTestService{errs: []error{assert.AnError}}
	})
	core.RegisterService("validatecmd-panics", func(_ core.Dependencies, _ core.ServiceConfig, _ context.Context) interface{} {
		panic("constructor exploded")
	})
	core.RegisterService("validatecmd-not-a-service", func(_ core.Dependencies, _ core.ServiceConfig, _ context.Context) interface{} {
		return struct{}{}
	})
}

func newValidateTestDeps(t *testing.T) (core.Dependencies, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	deps := core.Dependencies{}
	deps.SetOsProvider(testutil.FakeOsProvider{Host: "test-host"})
	deps.SetLogger(logger)
	deps.SetContext(ctx)
	deps.SetStateStore(&testutil.NoOpStateStore{})
	return deps, &buf
}

func writeConf(t *testing.T, dir, name, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
}

func runValidateCmd(t *testing.T, deps core.Dependencies, args ...string) error {
	t.Helper()
	// The command points KEYOP_CONF_DIR at its dir argument via os.Setenv;
	// t.Setenv registers the restore so that doesn't leak into other tests.
	t.Setenv("KEYOP_CONF_DIR", os.Getenv("KEYOP_CONF_DIR"))
	cmd := NewValidateCmd(deps)
	cmd.SetArgs(args)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	return cmd.Execute()
}

func Test_validateCmd_valid_config_passes(t *testing.T) {
	dir := t.TempDir()
	writeConf(t, dir, "ok.yaml", "service: validatecmd-ok\nfreq: 5s\n")

	deps, buf := newValidateTestDeps(t)
	err := runValidateCmd(t, deps, dir)
	assert.NoError(t, err)
	assert.Contains(t, buf.String(), "configuration valid")
}

func Test_validateCmd_uses_env_dir_when_no_arg(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("KEYOP_CONF_DIR", dir)
	writeConf(t, dir, "ok.yaml", "service: validatecmd-ok\nfreq: 5s\n")

	deps, _ := newValidateTestDeps(t)
	assert.NoError(t, runValidateCmd(t, deps))
}

func Test_validateCmd_validation_errors_fail(t *testing.T) {
	dir := t.TempDir()
	writeConf(t, dir, "bad.yaml", "service: validatecmd-bad\nfreq: 5s\n")

	deps, buf := newValidateTestDeps(t)
	err := runValidateCmd(t, deps, dir)
	assert.ErrorContains(t, err, "service configuration errors detected")
	assert.Contains(t, buf.String(), "service config validation error")
}

func Test_validateCmd_unknown_type_fails(t *testing.T) {
	dir := t.TempDir()
	writeConf(t, dir, "mystery.yaml", "service: validatecmd-unregistered\nfreq: 5s\n")

	deps, buf := newValidateTestDeps(t)
	err := runValidateCmd(t, deps, dir)
	assert.ErrorContains(t, err, "service configuration errors detected")
	assert.Contains(t, buf.String(), "service type not registered")
}

func Test_validateCmd_unknown_type_skipped_with_flag(t *testing.T) {
	dir := t.TempDir()
	writeConf(t, dir, "ok.yaml", "service: validatecmd-ok\nfreq: 5s\n")
	writeConf(t, dir, "mystery.yaml", "service: validatecmd-unregistered\nfreq: 5s\n")

	deps, buf := newValidateTestDeps(t)
	err := runValidateCmd(t, deps, dir, "--ignore-unknown")
	assert.NoError(t, err)
	assert.Contains(t, buf.String(), "service type not registered; skipping")
}

func Test_validateCmd_constructor_panic_reported_as_error(t *testing.T) {
	dir := t.TempDir()
	writeConf(t, dir, "boom.yaml", "service: validatecmd-panics\nfreq: 5s\n")

	deps, buf := newValidateTestDeps(t)
	err := runValidateCmd(t, deps, dir)
	assert.ErrorContains(t, err, "service configuration errors detected")
	assert.Contains(t, buf.String(), "constructor panicked")
}

func Test_validateCmd_non_service_constructor_fails(t *testing.T) {
	dir := t.TempDir()
	writeConf(t, dir, "notsvc.yaml", "service: validatecmd-not-a-service\nfreq: 5s\n")

	deps, buf := newValidateTestDeps(t)
	err := runValidateCmd(t, deps, dir)
	assert.ErrorContains(t, err, "service configuration errors detected")
	assert.Contains(t, buf.String(), "does not implement core.Service")
}

func Test_validateCmd_missing_dir_fails(t *testing.T) {
	deps, _ := newValidateTestDeps(t)
	err := runValidateCmd(t, deps, filepath.Join(t.TempDir(), "does-not-exist"))
	assert.ErrorContains(t, err, "config directory does not exist")
}
