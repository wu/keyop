package thermostat

import (
	"context"
	"keyop/core"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// helper to build dependencies similar to other service tests
func testDeps(t *testing.T) core.Dependencies {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	deps := core.Dependencies{}

	ctx, cancel := context.WithCancel(context.Background())
	deps.SetContext(ctx)
	deps.SetCancel(cancel)
	t.Cleanup(cancel)

	tmpDir, err := os.MkdirTemp("", "httpPost_test")
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
	deps.SetContext(context.Background())
	messenger := core.NewMessenger(logger, deps.MustGetOsProvider())
	messenger.SetDataDir(tmpDir)

	deps.SetMessenger(messenger)

	return deps
}

func Test_tempHandler_publishes_thermostat_event(t *testing.T) {
	deps := testDeps(t)
	messenger := deps.MustGetMessenger()

	var mu sync.Mutex
	var thermostatMsgs []core.Message

	// subscribe to the service name channel and capture thermostat_event messages
	_ = messenger.Subscribe(context.Background(), "test", "thermo", "thermostat", "test", 0, func(m core.Message) error {
		mu.Lock()
		defer mu.Unlock()
		if m.Event == "thermostat_event" {
			thermostatMsgs = append(thermostatMsgs, m)
		}
		return nil
	})

	cfg := core.ServiceConfig{
		Name: "thermo",
		Type: "thermostat",
		Subs: map[string]core.ChannelInfo{
			"temp": {Name: "temp-topic"},
		},
		Config: map[string]any{
			"minTemp": 50.0,
			"maxTemp": 75.0,
			"mode":    "auto",
		},
	}

	// Create service which subscribes to temp
	svc := NewService(deps, cfg)
	err := svc.Initialize()
	assert.NoError(t, err)
	_ = svc.(*Service).RegisterPayloads(core.GetPayloadRegistry())

	// send a temp message to the temp channel — above max to turn cooler ON
	err = messenger.Send(core.Message{ChannelName: "temp-topic", Metric: 80, Data: core.TempEvent{TempF: 80}})
	assert.NoError(t, err)

	// Wait for processing
	time.Sleep(1 * time.Second)

	mu.Lock()
	defer mu.Unlock()

	require.NotEmpty(t, thermostatMsgs)
	m := thermostatMsgs[0]
	assert.Equal(t, "thermostat_event", m.Event)
	assert.Equal(t, "OFF", m.State) // heater should be OFF at 80

	event, ok := m.Data.(*Event)
	require.True(t, ok, "expected *Event, got %T", m.Data)
	assert.Equal(t, "ON", event.CoolerTargetState)
}

func Test_tempHandler_with_missing_pub_channels(t *testing.T) {
	deps := testDeps(t)
	messenger := deps.MustGetMessenger()

	// capture thermostat_event events from service name channel
	var gotThermostat []core.Message
	var mu sync.Mutex
	subErr := messenger.Subscribe(context.Background(), "test", "thermo", "thermostat", "test", 0, func(m core.Message) error {
		if m.Event == "thermostat_event" {
			mu.Lock()
			gotThermostat = append(gotThermostat, m)
			mu.Unlock()
		}
		return nil
	})
	assert.NoError(t, subErr)

	cfg := core.ServiceConfig{
		Name: "thermo",
		Type: "thermostat",
		Subs: map[string]core.ChannelInfo{
			"temp": {Name: "temp-topic"},
		},
		Config: map[string]any{
			"minTemp": 50.0,
			"maxTemp": 75.0,
			"mode":    "auto",
		},
	}

	svc := NewService(deps, cfg)
	err := svc.Initialize()
	assert.NoError(t, err)

	// Send a cold temp to turn heater ON
	if err := messenger.Send(core.Message{ChannelName: "temp-topic", Metric: 20}); err != nil {
		assert.NoError(t, err)
	}
	// Wait for processing
	time.Sleep(1 * time.Second)

	mu.Lock()
	defer mu.Unlock()
	require.NotEmpty(t, gotThermostat)
	assert.Equal(t, "thermostat_event", gotThermostat[0].Event)
	assert.Equal(t, "ON", gotThermostat[0].State)
}

func TestValidateConfig(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	deps := core.Dependencies{}
	deps.SetLogger(logger)
	deps.SetOsProvider(core.FakeOsProvider{Host: "test-host"})
	deps.SetMessenger(core.NewMessenger(logger, deps.MustGetOsProvider()))

	t.Run("valid config", func(t *testing.T) {
		cfg := core.ServiceConfig{
			Name: "thermo",
			Type: "thermostat",
			Subs: map[string]core.ChannelInfo{
				"temp": {Name: "temp"},
			},
			Config: map[string]any{"minTemp": 10.0, "maxTemp": 30.0, "mode": "auto"},
		}
		svc := NewService(deps, cfg)
		errs := svc.ValidateConfig()
		assert.Empty(t, errs)
	})

	t.Run("missing subs", func(t *testing.T) {
		cfg := core.ServiceConfig{
			Name:   "thermo",
			Type:   "thermostat",
			Subs:   map[string]core.ChannelInfo{},
			Config: map[string]any{"minTemp": 10.0, "maxTemp": 30.0},
		}
		svc := NewService(deps, cfg)
		errs := svc.ValidateConfig()
		assert.NotEmpty(t, errs)
		assert.ErrorContains(t, errs[0], "required subs channel 'temp' is missing")
	})

	t.Run("missing minTemp", func(t *testing.T) {
		cfg := core.ServiceConfig{
			Name:   "thermo",
			Type:   "thermostat",
			Subs:   map[string]core.ChannelInfo{"temp": {Name: "temp"}},
			Config: map[string]any{"maxTemp": 30.0},
		}
		svc := NewService(deps, cfg)
		errs := svc.ValidateConfig()
		assert.NotEmpty(t, errs)
		assert.ErrorContains(t, errs[0], "minTemp not set in config")
	})

	t.Run("missing maxTemp", func(t *testing.T) {
		cfg := core.ServiceConfig{
			Name:   "thermo",
			Type:   "thermostat",
			Subs:   map[string]core.ChannelInfo{"temp": {Name: "temp"}},
			Config: map[string]any{"minTemp": 10.0},
		}
		svc := NewService(deps, cfg)
		errs := svc.ValidateConfig()
		assert.NotEmpty(t, errs)
		assert.ErrorContains(t, errs[0], "maxTemp not set in config")
	})

	t.Run("minTemp >= maxTemp", func(t *testing.T) {
		cfg := core.ServiceConfig{
			Name:   "thermo",
			Type:   "thermostat",
			Subs:   map[string]core.ChannelInfo{"temp": {Name: "temp"}},
			Config: map[string]any{"minTemp": 30.0, "maxTemp": 10.0, "mode": "auto"},
		}
		svc := NewService(deps, cfg)
		errs := svc.ValidateConfig()
		assert.NotEmpty(t, errs)
		assert.ErrorContains(t, errs[len(errs)-1], "minTemp must be less than or equal to maxTemp")
	})

	t.Run("invalid mode", func(t *testing.T) {
		cfg := core.ServiceConfig{
			Name:   "thermo",
			Type:   "thermostat",
			Subs:   map[string]core.ChannelInfo{"temp": {Name: "temp"}},
			Config: map[string]any{"minTemp": 10.0, "maxTemp": 30.0, "mode": "banana"},
		}
		svc := NewService(deps, cfg)
		errs := svc.ValidateConfig()
		assert.NotEmpty(t, errs)
		assert.Contains(t, errs[len(errs)-1].Error(), "invalid mode")
		assert.Contains(t, errs[len(errs)-1].Error(), "banana")
		assert.Contains(t, errs[len(errs)-1].Error(), "thermostat")

	})
}

func TestService_updateState(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	baseCfg := core.ServiceConfig{Name: "thermo", Type: "thermostat"}

	testCases := []struct {
		name      string
		minTemp   float64
		maxTemp   float64
		mode      string
		inputTemp float64
		heater    string
		cooler    string
	}{
		{
			name:      "below min, mode=auto",
			minTemp:   50,
			maxTemp:   75,
			mode:      "auto",
			inputTemp: 40,
			heater:    "ON",
			cooler:    "OFF",
		},
		{
			name:      "below min, mode=heat",
			minTemp:   50,
			maxTemp:   75,
			mode:      "heat",
			inputTemp: 40,
			heater:    "ON",
			cooler:    "OFF",
		},
		{
			name:      "below min, mode=cool",
			minTemp:   50,
			maxTemp:   75,
			mode:      "cool",
			inputTemp: 40,
			heater:    "OFF",
			cooler:    "OFF",
		},
		{
			name:      "above max, mode=auto",
			minTemp:   50,
			maxTemp:   75,
			mode:      "auto",
			inputTemp: 80,
			heater:    "OFF",
			cooler:    "ON",
		},
		{
			name:      "above max, mode=cool",
			minTemp:   50,
			maxTemp:   75,
			mode:      "cool",
			inputTemp: 80,
			heater:    "OFF",
			cooler:    "ON",
		},
		{
			name:      "above max, mode=heat",
			minTemp:   50,
			maxTemp:   75,
			mode:      "heat",
			inputTemp: 80,
			heater:    "OFF",
			cooler:    "OFF",
		},
		{
			name:      "between min and max, mode=auto",
			minTemp:   50,
			maxTemp:   75,
			mode:      "auto",
			inputTemp: 60,
			heater:    "OFF",
			cooler:    "OFF",
		},
		{
			name:      "between min and max, mode=off",
			minTemp:   50,
			maxTemp:   75,
			mode:      "off",
			inputTemp: 60,
			heater:    "OFF",
			cooler:    "OFF",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			svc := Service{
				Deps:    core.Dependencies{},
				Cfg:     baseCfg,
				MinTemp: tc.minTemp,
				MaxTemp: tc.maxTemp,
				Mode:    tc.mode,
			}
			event := svc.updateState(tc.inputTemp, logger)
			if event.HeaterTargetState != tc.heater {
				t.Errorf("expected heater=%s, got %s", tc.heater, event.HeaterTargetState)
			}
			if event.CoolerTargetState != tc.cooler {
				t.Errorf("expected cooler=%s, got %s", tc.cooler, event.CoolerTargetState)
			}
			if event.Temp != tc.inputTemp {
				t.Errorf("expected event.Temp=%v, got %v", tc.inputTemp, event.Temp)
			}
			if event.MinTemp != tc.minTemp {
				t.Errorf("expected event.MinTemp=%v, got %v", tc.minTemp, event.MinTemp)
			}
			if event.MaxTemp != tc.maxTemp {
				t.Errorf("expected event.MaxTemp=%v, got %v", tc.maxTemp, event.MaxTemp)
			}
			if event.Mode != tc.mode {
				t.Errorf("expected event.Mode=%v, got %v", tc.mode, event.Mode)
			}
		})
	}
}

func Test_updateState_thresholds_heat(t *testing.T) {
	for _, mode := range []string{"heat", "auto"} {
		t.Run("mode="+mode, func(t *testing.T) {
			deps := testDeps(t)
			svc := Service{Deps: deps, Cfg: core.ServiceConfig{Name: "thermo", Type: "thermostat"}, MinTemp: 50, MaxTemp: 75, Mode: mode, Hysteresis: 2}
			logger := deps.MustGetLogger()

			// between -> both OFF
			ev := svc.updateState(60, logger)
			assert.Equal(t, "OFF", ev.HeaterTargetState)
			assert.Equal(t, "OFF", ev.CoolerTargetState)

			// drop to 51 -> both OFF
			ev = svc.updateState(51, logger)
			assert.Equal(t, "OFF", ev.HeaterTargetState)
			assert.Equal(t, "OFF", ev.CoolerTargetState)

			// drop to just below min -> heater ON, cooler OFF
			ev = svc.updateState(49, logger)
			assert.Equal(t, "ON", ev.HeaterTargetState)
			assert.Equal(t, "OFF", ev.CoolerTargetState)
			svc.lastEvent = &ev // simulate heater turning ON

			// drop to just above min, hysteresis should keep heater ON
			ev = svc.updateState(51, logger)
			assert.Equal(t, "ON", ev.HeaterTargetState)
			assert.Equal(t, "OFF", ev.CoolerTargetState)

			// temp goes above min + hysteresis -> heater OFF
			ev = svc.updateState(58, logger)
			assert.Equal(t, "OFF", ev.HeaterTargetState)
			assert.Equal(t, "OFF", ev.CoolerTargetState)
		})
	}
}

func Test_updateState_thresholds_cool(t *testing.T) {

	for _, mode := range []string{"cool", "auto"} {
		t.Run("mode="+mode, func(t *testing.T) {
			deps := testDeps(t)
			svc := Service{Deps: deps, Cfg: core.ServiceConfig{Name: "thermo", Type: "thermostat"}, MinTemp: 50, MaxTemp: 75, Mode: mode, Hysteresis: 2}
			logger := deps.MustGetLogger()

			// between -> both OFF
			ev := svc.updateState(70, logger)
			assert.Equal(t, "OFF", ev.HeaterTargetState)
			assert.Equal(t, "OFF", ev.CoolerTargetState)

			// rise near max -> both OFF
			ev = svc.updateState(74, logger)
			assert.Equal(t, "OFF", ev.HeaterTargetState)
			assert.Equal(t, "OFF", ev.CoolerTargetState)

			// rise just above max -> cooler ON, heater OFF
			ev = svc.updateState(76, logger)
			assert.Equal(t, "OFF", ev.HeaterTargetState)
			assert.Equal(t, "ON", ev.CoolerTargetState)
			svc.lastEvent = &ev // simulate cooler turning ON

			// drop just below max, hysteresis should keep cooler ON
			ev = svc.updateState(74, logger)
			assert.Equal(t, "OFF", ev.HeaterTargetState)
			assert.Equal(t, "ON", ev.CoolerTargetState)

			// temp goes below max + hysteresis -> heater OFF
			ev = svc.updateState(70, logger)
			assert.Equal(t, "OFF", ev.HeaterTargetState)
			assert.Equal(t, "OFF", ev.CoolerTargetState)
		})
	}
}

func Test_updateState_cool_min_equals_max(t *testing.T) {
	deps := testDeps(t)
	svc := Service{Deps: deps, Cfg: core.ServiceConfig{Name: "thermo", Type: "thermostat"}, MinTemp: 60, MaxTemp: 60, Mode: "cool", Hysteresis: 2}
	logger := deps.MustGetLogger()

	// exact -> both OFF
	ev := svc.updateState(60, logger)
	assert.Equal(t, "OFF", ev.HeaterTargetState)
	assert.Equal(t, "OFF", ev.CoolerTargetState)

	// rise above target -> cooler ON
	ev = svc.updateState(65, logger)
	assert.Equal(t, "OFF", ev.HeaterTargetState)
	assert.Equal(t, "ON", ev.CoolerTargetState)
	svc.lastEvent = &ev // simulate cooler turning ON

	// drop just above target -> cooler ON, heater OFF
	ev = svc.updateState(61, logger)
	assert.Equal(t, "OFF", ev.HeaterTargetState)
	assert.Equal(t, "ON", ev.CoolerTargetState)

	// drop just below target, hysteresis should keep cooler ON
	ev = svc.updateState(59, logger)
	assert.Equal(t, "OFF", ev.HeaterTargetState)
	assert.Equal(t, "ON", ev.CoolerTargetState)

	// drop below max + hysteresis -> heater OFF
	ev = svc.updateState(50, logger)
	assert.Equal(t, "OFF", ev.HeaterTargetState)
	assert.Equal(t, "OFF", ev.CoolerTargetState)

}

func Test_updateState_heat_min_equals_max(t *testing.T) {
	deps := testDeps(t)
	svc := Service{Deps: deps, Cfg: core.ServiceConfig{Name: "thermo", Type: "thermostat"}, MinTemp: 60, MaxTemp: 60, Mode: "heat", Hysteresis: 2}
	logger := deps.MustGetLogger()

	// exact -> both OFF
	ev := svc.updateState(60, logger)
	assert.Equal(t, "OFF", ev.HeaterTargetState)
	assert.Equal(t, "OFF", ev.CoolerTargetState)

	// drop below target -> heater ON
	ev = svc.updateState(55, logger)
	assert.Equal(t, "ON", ev.HeaterTargetState)
	assert.Equal(t, "OFF", ev.CoolerTargetState)
	svc.lastEvent = &ev // simulate cooler turning ON

	// rise just below target -> heater stays on
	ev = svc.updateState(59, logger)
	assert.Equal(t, "ON", ev.HeaterTargetState)
	assert.Equal(t, "OFF", ev.CoolerTargetState)

	// rise just above target, hysteresis should keep heater ON
	ev = svc.updateState(61, logger)
	assert.Equal(t, "ON", ev.HeaterTargetState)
	assert.Equal(t, "OFF", ev.CoolerTargetState)

	// temp rises above max + hysteresis -> heater OFF
	ev = svc.updateState(70, logger)
	assert.Equal(t, "OFF", ev.HeaterTargetState)
	assert.Equal(t, "OFF", ev.CoolerTargetState)
}

func Test_updateState_auto_min_equals_max(t *testing.T) {
	deps := testDeps(t)
	svc := Service{Deps: deps, Cfg: core.ServiceConfig{Name: "thermo", Type: "thermostat"}, MinTemp: 60, MaxTemp: 60, Mode: "auto", Hysteresis: 2}
	logger := deps.MustGetLogger()

	// exact -> both OFF
	ev := svc.updateState(60, logger)
	assert.Equal(t, "OFF", ev.HeaterTargetState)
	assert.Equal(t, "OFF", ev.CoolerTargetState)

	// rise above target -> cooler ON
	ev = svc.updateState(65, logger)
	assert.Equal(t, "OFF", ev.HeaterTargetState)
	assert.Equal(t, "ON", ev.CoolerTargetState)
	svc.lastEvent = &ev // simulate cooler turning ON

	// drop just above target -> cooler ON, heater OFF
	ev = svc.updateState(61, logger)
	assert.Equal(t, "OFF", ev.HeaterTargetState)
	assert.Equal(t, "ON", ev.CoolerTargetState)

	// drop just below target, hysteresis should keep cooler ON
	ev = svc.updateState(59, logger)
	assert.Equal(t, "OFF", ev.HeaterTargetState)
	assert.Equal(t, "ON", ev.CoolerTargetState)

	// drop below target -> heater ON
	ev = svc.updateState(55, logger)
	assert.Equal(t, "ON", ev.HeaterTargetState)
	assert.Equal(t, "OFF", ev.CoolerTargetState)
	svc.lastEvent = &ev // simulate cooler turning ON

	// rise just below target -> heater stays on
	ev = svc.updateState(59, logger)
	assert.Equal(t, "ON", ev.HeaterTargetState)
	assert.Equal(t, "OFF", ev.CoolerTargetState)

	// rise just above target, hysteresis should keep heater ON
	ev = svc.updateState(61, logger)
	assert.Equal(t, "ON", ev.HeaterTargetState)
	assert.Equal(t, "OFF", ev.CoolerTargetState)

	// temp rises above max + hysteresis -> cooler on
	ev = svc.updateState(70, logger)
	assert.Equal(t, "OFF", ev.HeaterTargetState)
	assert.Equal(t, "ON", ev.CoolerTargetState)
}
