package macosBluetoothBattery

import (
	"context"
	"fmt"
	"keyop/core"
	"runtime"
	"testing"
	"time"
)

// ── test helpers ────────────────────────────────────────────────────────────

type mockMessenger struct {
	messages []core.Message
}

func (m *mockMessenger) Send(msg core.Message) error {
	m.messages = append(m.messages, msg)
	return nil
}

func (m *mockMessenger) Subscribe(_ context.Context, _, _, _, _ string, _ time.Duration, _ func(core.Message) error) error {
	return nil
}

func (m *mockMessenger) SubscribeExtended(_ context.Context, _, _, _, _ string, _ time.Duration, _ func(core.Message, string, int64) error) error {
	return nil
}

func (m *mockMessenger) SetReaderState(_, _, _ string, _ int64) error { return nil }
func (m *mockMessenger) SeekToEnd(_, _ string) error                  { return nil }
func (m *mockMessenger) SetDataDir(_ string)                          {}
func (m *mockMessenger) SetHostname(_ string)                         {}
func (m *mockMessenger) GetStats() core.MessengerStats                { return core.MessengerStats{} }

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

func makeDeps(fakeOs *core.FakeOsProvider, messenger core.MessengerApi) core.Dependencies {
	deps := core.Dependencies{}
	deps.SetOsProvider(fakeOs)
	deps.SetLogger(&core.FakeLogger{})
	deps.SetMessenger(messenger)
	return deps
}

func makeCfg(name string) core.ServiceConfig {
	return core.ServiceConfig{
		Name: name,
		Type: "macosBluetoothBattery",
		Pubs: map[string]core.ChannelInfo{
			"metrics": {Name: "metrics_channel"},
		},
	}
}

// sampleIoregOutput reflects real ioreg output: no Product name, but a
// DeviceAddress and BatteryPercent per device.
const sampleIoregOutput = `
+-o AppleDeviceManagementHIDEventService  <class AppleDeviceManagementHIDEventService, id 0x10002fda1>
    {
      "DeviceAddress" = "c0-95-6d-05-d1-11"
      "BatteryPercent" = 78
    }
+-o AppleDeviceManagementHIDEventService  <class AppleDeviceManagementHIDEventService, id 0x10002fda2>
    {
      "DeviceAddress" = "c0-95-6d-05-d1-22"
      "BatteryPercent" = 62
    }
+-o AppleDeviceManagementHIDEventService  <class AppleDeviceManagementHIDEventService, id 0x10002fda3>
    {
      "SerialNumber" = "noBatteryHere"
    }
`

// sampleSPOutput uses a Unicode right single quotation mark (U+2019) in
// "Alex\u2019s" to match what system_profiler actually emits on macOS.
const sampleSPOutput = `{
  "SPBluetoothDataType": [{
    "device_connected": [
      { "Magic Keyboard": { "device_address": "C0:95:6D:05:D1:11" } },
      { "Alex\u2019s Magic Trackpad": { "device_address": "C0:95:6D:05:D1:22" } }
    ],
    "device_not_connected": []
  }]
}`

// ── parseIoregBatteries (pure function) ──────────────────────────────────────

func TestParseIoregBatteries_TwoDevices(t *testing.T) {
	result := parseIoregBatteries(sampleIoregOutput)
	if len(result) != 2 {
		t.Fatalf("expected 2 entries, got %d: %v", len(result), result)
	}
	if result["c0:95:6d:05:d1:11"] != 78 {
		t.Errorf("expected keyboard battery 78, got %v", result["c0:95:6d:05:d1:11"])
	}
	if result["c0:95:6d:05:d1:22"] != 62 {
		t.Errorf("expected trackpad battery 62, got %v", result["c0:95:6d:05:d1:22"])
	}
}

func TestParseIoregBatteries_Empty(t *testing.T) {
	result := parseIoregBatteries("")
	if len(result) != 0 {
		t.Errorf("expected empty map, got %v", result)
	}
}

func TestParseIoregBatteries_NoBattery(t *testing.T) {
	result := parseIoregBatteries(`
+-o AppleDeviceManagementHIDEventService  <class AppleDeviceManagementHIDEventService>
    {
      "DeviceAddress" = "aa-bb-cc-dd-ee-ff"
    }
`)
	if len(result) != 0 {
		t.Errorf("expected empty map for device with no BatteryPercent, got %v", result)
	}
}

func TestParseIoregBatteries_NoAddress(t *testing.T) {
	result := parseIoregBatteries(`
+-o AppleDeviceManagementHIDEventService  <class AppleDeviceManagementHIDEventService>
    {
      "BatteryPercent" = 50
    }
`)
	if len(result) != 0 {
		t.Errorf("expected empty map for device with no DeviceAddress, got %v", result)
	}
}

// ── parseSystemProfilerNames (pure function) ─────────────────────────────────

func TestParseSystemProfilerNames_TwoDevices(t *testing.T) {
	result := parseSystemProfilerNames([]byte(sampleSPOutput))
	if len(result) != 2 {
		t.Fatalf("expected 2 entries, got %d: %v", len(result), result)
	}
	if result["c0:95:6d:05:d1:11"] != "Magic Keyboard" {
		t.Errorf("unexpected keyboard name: %q", result["c0:95:6d:05:d1:11"])
	}
	// Unicode curly apostrophe in the JSON should be normalised to ASCII ' .
	if result["c0:95:6d:05:d1:22"] != "Alex's Magic Trackpad" {
		t.Errorf("unexpected trackpad name: %q", result["c0:95:6d:05:d1:22"])
	}
}

func TestParseSystemProfilerNames_InvalidJSON(t *testing.T) {
	result := parseSystemProfilerNames([]byte("not json"))
	if len(result) != 0 {
		t.Errorf("expected empty map for invalid JSON, got %v", result)
	}
}

func TestParseSystemProfilerNames_NotConnectedDevices(t *testing.T) {
	json := `{
	  "SPBluetoothDataType": [{
	    "device_connected": [],
	    "device_not_connected": [
	      { "Some Keyboard": { "device_address": "AA:BB:CC:DD:EE:FF" } }
	    ]
	  }]
	}`
	result := parseSystemProfilerNames([]byte(json))
	if result["aa:bb:cc:dd:ee:ff"] != "Some Keyboard" {
		t.Errorf("expected 'Some Keyboard' for not-connected device, got %q", result["aa:bb:cc:dd:ee:ff"])
	}
}

// ── normaliseDeviceName (pure function) ──────────────────────────────────────

func TestNormaliseDeviceName(t *testing.T) {
	tests := []struct{ input, want string }{
		{"Alex\u2019s Magic Trackpad", "Alex's Magic Trackpad"}, // curly right apostrophe → ASCII
		{"Alex\u2018s Magic Trackpad", "Alex's Magic Trackpad"}, // curly left apostrophe → ASCII
		{"\u201CMagic\u201D", "\"Magic\""},                      // curly double quotes → ASCII
		{"Magic Keyboard", "Magic Keyboard"},                    // no change needed
	}
	for _, tt := range tests {
		if got := normaliseDeviceName(tt.input); got != tt.want {
			t.Errorf("normaliseDeviceName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// ── normaliseMac (pure function) ─────────────────────────────────────────────

func TestNormaliseMac(t *testing.T) {
	tests := []struct{ input, want string }{
		{"c0-95-6d-05-d1-22", "c0:95:6d:05:d1:22"},
		{"C0:95:6D:05:D1:22", "c0:95:6d:05:d1:22"},
		{"c0:95:6d:05:d1:22", "c0:95:6d:05:d1:22"},
	}
	for _, tt := range tests {
		if got := normaliseMac(tt.input); got != tt.want {
			t.Errorf("normaliseMac(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// ── sanitizeName (pure function) ─────────────────────────────────────────────

func TestSanitizeName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Magic Keyboard", "magic_keyboard"},
		{"Alex's Magic Trackpad", "alex_s_magic_trackpad"},
		{"Apple Magic Mouse 2", "apple_magic_mouse_2"},
		{"  leading/trailing  ", "leading_trailing"},
		{"UPPER CASE", "upper_case"},
	}
	for _, tt := range tests {
		got := sanitizeName(tt.input)
		if got != tt.expected {
			t.Errorf("sanitizeName(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

// ── Initialize ────────────────────────────────────────────────────────────────

func TestInitialize_DefaultMetricPrefix(t *testing.T) {
	deps := core.Dependencies{}
	deps.SetLogger(&core.FakeLogger{})
	cfg := core.ServiceConfig{
		Name:   "bt_test",
		Config: map[string]interface{}{},
	}
	svc := NewService(deps, cfg).(*Service)
	if err := svc.Initialize(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if svc.metricPrefix != "bt_test.battery" {
		t.Errorf("expected default prefix 'bt_test.battery', got %q", svc.metricPrefix)
	}
}

func TestInitialize_CustomMetricPrefix(t *testing.T) {
	deps := core.Dependencies{}
	deps.SetLogger(&core.FakeLogger{})
	cfg := core.ServiceConfig{
		Name: "bt_test",
		Config: map[string]interface{}{
			"metric_prefix": "my.custom.prefix",
		},
	}
	svc := NewService(deps, cfg).(*Service)
	if err := svc.Initialize(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if svc.metricPrefix != "my.custom.prefix" {
		t.Errorf("expected prefix 'my.custom.prefix', got %q", svc.metricPrefix)
	}
}

func TestInitialize_DeviceMetrics(t *testing.T) {
	deps := core.Dependencies{}
	deps.SetLogger(&core.FakeLogger{})
	cfg := core.ServiceConfig{
		Name: "bt_test",
		Config: map[string]interface{}{
			"device_metrics": map[string]interface{}{
				"Magic Keyboard":        "home.mac.keyboard.battery",
				"Alex's Magic Trackpad": "home.mac.trackpad.battery",
			},
		},
	}
	svc := NewService(deps, cfg).(*Service)
	if err := svc.Initialize(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if svc.deviceMetrics["Magic Keyboard"] != "home.mac.keyboard.battery" {
		t.Errorf("unexpected keyboard metric: %q", svc.deviceMetrics["Magic Keyboard"])
	}
	if svc.deviceMetrics["Alex's Magic Trackpad"] != "home.mac.trackpad.battery" {
		t.Errorf("unexpected trackpad metric: %q", svc.deviceMetrics["Alex's Magic Trackpad"])
	}
}

// ── ValidateConfig ─────────────────────────────────────────────────────────

func TestValidateConfig_MissingPubs(t *testing.T) {
	deps := core.Dependencies{}
	deps.SetLogger(&core.FakeLogger{})
	cfg := core.ServiceConfig{
		Config: map[string]interface{}{},
	}
	svc := NewService(deps, cfg)
	errs := svc.ValidateConfig()
	if len(errs) != 0 {
		t.Errorf("expected no errors for empty config, got %v", errs)
	}
}

func TestValidateConfig_ValidPubs(t *testing.T) {
	deps := core.Dependencies{}
	deps.SetLogger(&core.FakeLogger{})
	cfg := core.ServiceConfig{
		Pubs: map[string]core.ChannelInfo{
			"metrics": {Name: "metrics_channel"},
		},
		Config: map[string]interface{}{},
	}
	svc := NewService(deps, cfg)
	errs := svc.ValidateConfig()
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}
}

// ── Check (darwin only) ──────────────────────────────────────────────────────

// makeFakeOs returns a FakeOsProvider whose Command function returns ioreg
// output for the first call and system_profiler output for the second.
func makeFakeOs(ioregOut, spOut []byte, ioregErr, spErr error) *core.FakeOsProvider {
	call := 0
	fakeOs := &core.FakeOsProvider{}
	fakeOs.CommandFunc = func(name string, arg ...string) core.CommandApi {
		call++
		if call == 1 {
			return &core.FakeCommand{CombinedOutputFunc: func() ([]byte, error) { return ioregOut, ioregErr }}
		}
		return &core.FakeCommand{CombinedOutputFunc: func() ([]byte, error) { return spOut, spErr }}
	}
	return fakeOs
}

func TestCheck_Darwin_TwoDevices(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin only")
	}

	fakeOs := makeFakeOs([]byte(sampleIoregOutput), []byte(sampleSPOutput), nil, nil)
	messenger := &mockMessenger{}
	deps := makeDeps(fakeOs, messenger)
	svc := NewService(deps, makeCfg("bt_svc")).(*Service)
	if err := svc.Initialize(); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	if err := svc.Check(); err != nil {
		t.Fatalf("Check failed: %v", err)
	}

	if len(messenger.messages) != 2 {
		t.Fatalf("expected 2 messages, got %d: %v", len(messenger.messages), messenger.messages)
	}

	foundKeyboard, foundTrackpad := false, false
	for _, msg := range messenger.messages {
		if msg.MetricName == "bt_svc.battery.magic_keyboard" && msg.Metric == 78 {
			foundKeyboard = true
		}
		if msg.MetricName == "bt_svc.battery.alex_s_magic_trackpad" && msg.Metric == 62 {
			foundTrackpad = true
		}
	}
	if !foundKeyboard {
		t.Errorf("keyboard metric not found: %v", messenger.messages)
	}
	if !foundTrackpad {
		t.Errorf("trackpad metric not found: %v", messenger.messages)
	}
}

func TestCheck_Darwin_DeviceMetrics(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin only")
	}

	fakeOs := makeFakeOs([]byte(sampleIoregOutput), []byte(sampleSPOutput), nil, nil)
	messenger := &mockMessenger{}
	deps := makeDeps(fakeOs, messenger)
	cfg := core.ServiceConfig{
		Name: "bt_svc",
		Type: "macosBluetoothBattery",
		Pubs: map[string]core.ChannelInfo{"metrics": {Name: "metrics_channel"}},
		Config: map[string]interface{}{
			"device_metrics": map[string]interface{}{
				"Magic Keyboard": "home.mac.keyboard.battery",
				// Trackpad not listed — should fall back to prefix+sanitized.
			},
		},
	}
	svc := NewService(deps, cfg).(*Service)
	if err := svc.Initialize(); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	if err := svc.Check(); err != nil {
		t.Fatalf("Check failed: %v", err)
	}

	foundKeyboard, foundTrackpad := false, false
	for _, msg := range messenger.messages {
		if msg.MetricName == "home.mac.keyboard.battery" && msg.Metric == 78 {
			foundKeyboard = true
		}
		if msg.MetricName == "bt_svc.battery.alex_s_magic_trackpad" && msg.Metric == 62 {
			foundTrackpad = true
		}
	}
	if !foundKeyboard {
		t.Errorf("keyboard explicit metric not found: %v", messenger.messages)
	}
	if !foundTrackpad {
		t.Errorf("trackpad fallback metric not found: %v", messenger.messages)
	}
}

func TestCheck_Darwin_NoDevices(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin only")
	}

	fakeOs := makeFakeOs([]byte(""), []byte(sampleSPOutput), nil, nil)
	messenger := &mockMessenger{}
	deps := makeDeps(fakeOs, messenger)
	svc := NewService(deps, makeCfg("bt_svc")).(*Service)
	if err := svc.Initialize(); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	if err := svc.Check(); err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if len(messenger.messages) != 0 {
		t.Errorf("expected no messages for no devices, got %d", len(messenger.messages))
	}
}

func TestCheck_Darwin_IoregError(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin only")
	}

	fakeOs := makeFakeOs(nil, nil, fmt.Errorf("ioreg failed"), nil)
	messenger := &mockMessenger{}
	deps := makeDeps(fakeOs, messenger)
	svc := NewService(deps, makeCfg("bt_svc")).(*Service)
	if err := svc.Initialize(); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	if err := svc.Check(); err == nil {
		t.Error("expected error from ioreg failure, got nil")
	}
}

func TestCheck_Darwin_SystemProfilerError(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin only")
	}

	// ioreg succeeds but system_profiler fails — we should get an error.
	fakeOs := makeFakeOs([]byte(sampleIoregOutput), nil, nil, fmt.Errorf("sp failed"))
	messenger := &mockMessenger{}
	deps := makeDeps(fakeOs, messenger)
	svc := NewService(deps, makeCfg("bt_svc")).(*Service)
	if err := svc.Initialize(); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	if err := svc.Check(); err == nil {
		t.Error("expected error from system_profiler failure, got nil")
	}
}

func TestCheck_Darwin_UnknownMacFallsBackToAddress(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin only")
	}

	// system_profiler returns no devices, so the MAC address is used as name.
	emptySP := `{"SPBluetoothDataType":[{"device_connected":[],"device_not_connected":[]}]}`
	fakeOs := makeFakeOs([]byte(sampleIoregOutput), []byte(emptySP), nil, nil)
	messenger := &mockMessenger{}
	deps := makeDeps(fakeOs, messenger)
	svc := NewService(deps, makeCfg("bt_svc")).(*Service)
	if err := svc.Initialize(); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	if err := svc.Check(); err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	// Should have sent 2 messages using the normalised MAC as the device name.
	if len(messenger.messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messenger.messages))
	}
}

func TestCheck_Darwin_SendError(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin only")
	}

	fakeOs := makeFakeOs([]byte(sampleIoregOutput), []byte(sampleSPOutput), nil, nil)
	deps := makeDeps(fakeOs, &errorMessenger{})
	svc := NewService(deps, makeCfg("bt_svc")).(*Service)
	if err := svc.Initialize(); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	// Send errors are logged but not propagated.
	if err := svc.Check(); err != nil {
		t.Errorf("Check should not propagate Send errors, got: %v", err)
	}
}

func TestCheck_UnsupportedPlatform(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("this test is for non-darwin platforms")
	}

	fakeOs := &core.FakeOsProvider{}
	messenger := &mockMessenger{}
	deps := makeDeps(fakeOs, messenger)
	svc := NewService(deps, makeCfg("bt_svc")).(*Service)
	if err := svc.Initialize(); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	if err := svc.Check(); err == nil {
		t.Error("expected error on non-darwin platform, got nil")
	}
}
