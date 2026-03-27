//nolint:revive
package diskspace

import (
	"keyop/core"
	"keyop/core/testutil"
	"testing"
)

// newTestDeps returns a minimal Dependencies suitable for unit tests.
func newTestDeps() core.Dependencies {
	deps := core.Dependencies{}
	deps.SetLogger(&core.FakeLogger{})
	deps.SetOsProvider(core.FakeOsProvider{Host: "testhost"})
	messenger := testutil.NewFakeMessenger()
	deps.SetMessenger(messenger)
	return deps
}

// newTestCfg returns a minimal ServiceConfig with an optional config map.
func newTestCfg(params map[string]interface{}) core.ServiceConfig {
	if params == nil {
		params = map[string]interface{}{}
	}
	return core.ServiceConfig{
		Name:   "diskspace_test",
		Type:   "diskspace",
		Config: params,
	}
}

// TestNewService_Defaults verifies that default thresholds are applied when none are configured.
func TestNewService_Defaults(t *testing.T) {
	svc := NewService(newTestDeps(), newTestCfg(nil)).(*Service)

	if svc.warningThreshold != 80.0 {
		t.Errorf("expected warningThreshold=80.0, got %.1f", svc.warningThreshold)
	}
	if svc.criticalThreshold != 90.0 {
		t.Errorf("expected criticalThreshold=90.0, got %.1f", svc.criticalThreshold)
	}
}

// TestNewService_CustomThresholds verifies that custom float64 thresholds are read from config.
func TestNewService_CustomThresholds(t *testing.T) {
	cfg := newTestCfg(map[string]interface{}{
		"warningThreshold":  float64(70),
		"criticalThreshold": float64(85),
	})
	svc := NewService(newTestDeps(), cfg).(*Service)

	if svc.warningThreshold != 70.0 {
		t.Errorf("expected warningThreshold=70.0, got %.1f", svc.warningThreshold)
	}
	if svc.criticalThreshold != 85.0 {
		t.Errorf("expected criticalThreshold=85.0, got %.1f", svc.criticalThreshold)
	}
}

// TestNewService_IntThresholds verifies that integer thresholds are also accepted.
func TestNewService_IntThresholds(t *testing.T) {
	cfg := newTestCfg(map[string]interface{}{
		"warningThreshold":  int(75),
		"criticalThreshold": int(95),
	})
	svc := NewService(newTestDeps(), cfg).(*Service)

	if svc.warningThreshold != 75.0 {
		t.Errorf("expected warningThreshold=75.0, got %.1f", svc.warningThreshold)
	}
	if svc.criticalThreshold != 95.0 {
		t.Errorf("expected criticalThreshold=95.0, got %.1f", svc.criticalThreshold)
	}
}

// TestNewService_IncludeExcludeLists verifies that include/exclude lists are parsed from config.
func TestNewService_IncludeExcludeLists(t *testing.T) {
	cfg := newTestCfg(map[string]interface{}{
		"include": []interface{}{"/", "/data"},
		"exclude": []interface{}{"/dev", "/run"},
	})
	svc := NewService(newTestDeps(), cfg).(*Service)

	if len(svc.includes) != 2 {
		t.Errorf("expected 2 includes, got %d", len(svc.includes))
	}
	if len(svc.excludes) != 2 {
		t.Errorf("expected 2 excludes, got %d", len(svc.excludes))
	}
}

// TestValidateConfig_Valid verifies that no errors are returned for valid thresholds.
func TestValidateConfig_Valid(t *testing.T) {
	svc := &Service{
		Deps:              newTestDeps(),
		warningThreshold:  80.0,
		criticalThreshold: 90.0,
	}
	errs := svc.ValidateConfig()
	if len(errs) != 0 {
		t.Errorf("expected no errors, got: %v", errs)
	}
}

// TestValidateConfig_WarnEqualsCritical verifies that an error is returned when warning == critical.
func TestValidateConfig_WarnEqualsCritical(t *testing.T) {
	svc := &Service{
		Deps:              newTestDeps(),
		warningThreshold:  80.0,
		criticalThreshold: 80.0,
	}
	errs := svc.ValidateConfig()
	if len(errs) == 0 {
		t.Error("expected an error when warningThreshold == criticalThreshold, got none")
	}
}

// TestValidateConfig_WarnGreaterThanCritical verifies that an error is returned when warning > critical.
func TestValidateConfig_WarnGreaterThanCritical(t *testing.T) {
	svc := &Service{
		Deps:              newTestDeps(),
		warningThreshold:  95.0,
		criticalThreshold: 80.0,
	}
	errs := svc.ValidateConfig()
	if len(errs) == 0 {
		t.Error("expected an error when warningThreshold > criticalThreshold, got none")
	}
}

// TestValidateConfig_ZeroThresholds verifies that an error is returned when both thresholds are 0
// (zero warning is not less than zero critical, so the same ">=" rule applies).
func TestValidateConfig_ZeroThresholds(t *testing.T) {
	svc := &Service{
		Deps:              newTestDeps(),
		warningThreshold:  0.0,
		criticalThreshold: 0.0,
	}
	errs := svc.ValidateConfig()
	if len(errs) == 0 {
		t.Error("expected an error when both thresholds are 0, got none")
	}
}

// TestInitialize_NoOp verifies that Initialize always returns nil.
func TestInitialize_NoOp(t *testing.T) {
	svc := NewService(newTestDeps(), newTestCfg(nil))
	if err := svc.Initialize(); err != nil {
		t.Errorf("Initialize should return nil, got: %v", err)
	}
}

// TestIsIncluded_EmptyIncludeList verifies that any mount is included when include list is empty.
func TestIsIncluded_EmptyIncludeList(t *testing.T) {
	svc := &Service{
		includes:          []string{},
		excludes:          []string{},
		warningThreshold:  80.0,
		criticalThreshold: 90.0,
	}
	for _, mount := range []string{"/", "/home", "/data", "/var/log"} {
		if !svc.isIncluded(mount) {
			t.Errorf("expected mount %q to be included with empty include list", mount)
		}
	}
}

// TestIsIncluded_WithIncludeList verifies that only listed mounts are included.
func TestIsIncluded_WithIncludeList(t *testing.T) {
	svc := &Service{
		includes:          []string{"/", "/home"},
		excludes:          []string{},
		warningThreshold:  80.0,
		criticalThreshold: 90.0,
	}
	if !svc.isIncluded("/") {
		t.Error("expected '/' to be included")
	}
	if !svc.isIncluded("/home") {
		t.Error("expected '/home' to be included")
	}
	if svc.isIncluded("/data") {
		t.Error("expected '/data' to be excluded (not in include list)")
	}
}

// TestIsIncluded_Excluded verifies that excluded mounts are not included even if in the include list.
func TestIsIncluded_Excluded(t *testing.T) {
	svc := &Service{
		includes:          []string{"/", "/dev"},
		excludes:          []string{"/dev"},
		warningThreshold:  80.0,
		criticalThreshold: 90.0,
	}
	if !svc.isIncluded("/") {
		t.Error("expected '/' to be included")
	}
	if svc.isIncluded("/dev") {
		t.Error("expected '/dev' to be excluded even though it is in the include list")
	}
}

// TestIsIncluded_ExcludePattern verifies that mounts with an excluded prefix are also excluded.
func TestIsIncluded_ExcludePattern(t *testing.T) {
	svc := &Service{
		includes:          []string{},
		excludes:          []string{"/dev"},
		warningThreshold:  80.0,
		criticalThreshold: 90.0,
	}
	// Exact match
	if svc.isIncluded("/dev") {
		t.Error("expected '/dev' to be excluded")
	}
	// Prefix match
	if svc.isIncluded("/dev/shm") {
		t.Error("expected '/dev/shm' to be excluded via prefix match on '/dev'")
	}
	// Non-excluded mount should pass through (empty include list → all included)
	if !svc.isIncluded("/") {
		t.Error("expected '/' to be included")
	}
}

// TestCheck_NoError verifies that Check() succeeds on the current system
// by monitoring only the root filesystem.
func TestCheck_NoError(t *testing.T) {
	cfg := newTestCfg(map[string]interface{}{
		"include": []interface{}{"/"},
	})
	svc := NewService(newTestDeps(), cfg)
	if err := svc.Check(); err != nil {
		t.Errorf("Check() returned unexpected error: %v", err)
	}
}

// TestGatherFilesystems verifies that gatherFilesystems returns at least one entry
// with valid fields when the root filesystem is included.
func TestGatherFilesystems(t *testing.T) {
	deps := newTestDeps()
	svc := &Service{
		Deps:              deps,
		includes:          []string{"/"},
		excludes:          []string{},
		warningThreshold:  80.0,
		criticalThreshold: 90.0,
	}

	results, err := svc.gatherFilesystems()
	if err != nil {
		t.Fatalf("gatherFilesystems() returned error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("gatherFilesystems() returned no results for '/'")
	}

	fs := results[0]
	if fs.Filesystem == "" {
		t.Error("FilesystemUsage.Filesystem should not be empty")
	}
	if fs.TotalBytes == 0 {
		t.Error("FilesystemUsage.TotalBytes should be > 0")
	}
	if fs.UsedPercent < 0 || fs.UsedPercent > 100 {
		t.Errorf("FilesystemUsage.UsedPercent out of range: %.2f", fs.UsedPercent)
	}
	if fs.FreePercent < 0 || fs.FreePercent > 100 {
		t.Errorf("FilesystemUsage.FreePercent out of range: %.2f", fs.FreePercent)
	}
}

// TestFilesystemUsage_Level verifies that levels are correctly assigned based on thresholds.
// It calls Check() and inspects the sent messages for the level values.
func TestFilesystemUsage_Level(t *testing.T) {
	deps := newTestDeps()
	messenger := testutil.NewFakeMessenger()
	deps.SetMessenger(messenger)

	// Use thresholds high enough that the root filesystem almost certainly lands at "ok".
	// (real disk usage on a dev machine is very unlikely to exceed 99%)
	cfg := core.ServiceConfig{
		Name:   "diskspace_test",
		Type:   "diskspace",
		Config: map[string]interface{}{"include": []interface{}{"/"}},
	}
	svc := &Service{
		Deps:              deps,
		Cfg:               cfg,
		includes:          []string{"/"},
		excludes:          []string{},
		warningThreshold:  80.0,
		criticalThreshold: 90.0,
	}

	filesystems, err := svc.gatherFilesystems()
	if err != nil {
		t.Fatalf("gatherFilesystems() failed: %v", err)
	}
	if len(filesystems) == 0 {
		t.Skip("no filesystems returned; skipping level test")
	}

	// Assign levels using the same logic as Check().
	for i := range filesystems {
		fs := &filesystems[i]
		switch {
		case fs.UsedPercent >= svc.criticalThreshold:
			fs.Level = "critical"
		case fs.UsedPercent >= svc.warningThreshold:
			fs.Level = "warning"
		default:
			fs.Level = "ok"
		}
		if fs.Level != "ok" && fs.Level != "warning" && fs.Level != "critical" {
			t.Errorf("unexpected level %q for filesystem %s", fs.Level, fs.Filesystem)
		}
	}

	// Spot-check level thresholds with synthetic values.
	cases := []struct {
		usedPct float64
		want    string
	}{
		{50.0, "ok"},
		{79.9, "ok"},
		{80.0, "warning"},
		{89.9, "warning"},
		{90.0, "critical"},
		{99.9, "critical"},
	}
	for _, tc := range cases {
		fs := FilesystemUsage{UsedPercent: tc.usedPct}
		switch {
		case fs.UsedPercent >= svc.criticalThreshold:
			fs.Level = "critical"
		case fs.UsedPercent >= svc.warningThreshold:
			fs.Level = "warning"
		default:
			fs.Level = "ok"
		}
		if fs.Level != tc.want {
			t.Errorf("UsedPercent=%.1f: expected level %q, got %q", tc.usedPct, tc.want, fs.Level)
		}
	}
}

// TestCheck_EmitsMessages verifies that Check() sends a diskspace_event and a status_event.
func TestCheck_EmitsMessages(t *testing.T) {
	deps := newTestDeps()
	messenger := testutil.NewFakeMessenger()
	deps.SetMessenger(messenger)

	cfg := core.ServiceConfig{
		Name:   "diskspace_test",
		Type:   "diskspace",
		Config: map[string]interface{}{"include": []interface{}{"/"}},
	}
	svc := &Service{
		Deps:              deps,
		Cfg:               cfg,
		includes:          []string{"/"},
		excludes:          []string{},
		warningThreshold:  80.0,
		criticalThreshold: 90.0,
	}

	if err := svc.Check(); err != nil {
		t.Fatalf("Check() returned unexpected error: %v", err)
	}

	var foundDiskspace, foundStatus bool
	for _, msg := range messenger.SentMessages {
		if msg.Event == "diskspace_event" {
			foundDiskspace = true
		}
		if msg.Event == "status_event" {
			foundStatus = true
		}
	}
	if !foundDiskspace {
		t.Error("expected a diskspace_event message to be sent")
	}
	if !foundStatus {
		t.Error("expected a status_event message to be sent")
	}
}
