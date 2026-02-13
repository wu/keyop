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
		//goland:noinspection GoUnhandledErrorResult
		os.RemoveAll(tmpDir)
	})

	deps.SetOsProvider(core.OsProvider{})
	deps.SetLogger(logger)
	deps.SetContext(context.Background())
	messenger := core.NewMessenger(logger, deps.MustGetOsProvider())
	messenger.SetDataDir(tmpDir)

	deps.SetMessenger(messenger)

	return deps
}

func Test_tempHandler_publishes_to_heater_and_cooler(t *testing.T) {
	deps := testDeps(t)
	messenger := deps.MustGetMessenger()

	var mu sync.Mutex
	got := map[string][]core.Message{}

	// subscribe to heater and cooler channels to capture what thermostat sends
	capture := func(ch string) {
		_ = messenger.Subscribe(context.Background(), "test", ch, 0, func(m core.Message) error {

			mu.Lock()
			defer mu.Unlock()
			got[ch] = append(got[ch], m)
			return nil
		})
	}

	heaterCh := "heater-topic"
	coolerCh := "cooler-topic"
	capture(heaterCh)
	capture(coolerCh)

	cfg := core.ServiceConfig{
		Name: "thermo",
		Type: "thermostat",
		Pubs: map[string]core.ChannelInfo{
			"heater": {Name: heaterCh},
			"cooler": {Name: coolerCh},
		},
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

	// send a temp message to the temp channel
	// pick a value above max to turn cooler ON
	err = messenger.Send(core.Message{ChannelName: "temp-topic", Metric: 80})
	assert.NoError(t, err)

	// Wait for processing
	time.Sleep(1 * time.Second)

	// Assertions: thermostat should publish to both heater and cooler
	mu.Lock()
	defer mu.Unlock()

	require.Contains(t, got, heaterCh)
	assert.Len(t, got[heaterCh], 1)
	m := got[heaterCh][0]
	assert.Equal(t, "thermo", m.ServiceName)
	assert.Equal(t, "thermostat", m.ServiceType)
	assert.Equal(t, "OFF", m.State) // heater should be OFF at 80

	t.Logf("Heater message: %+v\n", m)

	data, ok := m.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected type not matched in data, got %T", m.Data)
	}
	assert.Equal(t, 80.0, data["temp"])
	assert.Equal(t, "OFF", data["heaterTargetState"])

	require.Contains(t, got, coolerCh)
	assert.Len(t, got[coolerCh], 1)
	m_cool := got[coolerCh][0]
	assert.Equal(t, "thermo", m_cool.ServiceName)
	assert.Equal(t, "thermostat", m_cool.ServiceType)
	assert.Equal(t, "ON", m_cool.State) // cooler should be ON at 80

	data_cool, ok := m_cool.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected type not matched in data, got %T", m_cool.Data)
	}

	assert.Equal(t, 80.0, data_cool["temp"])
	assert.Equal(t, "ON", data_cool["coolerTargetState"])
}

func Test_tempHandler_with_missing_pub_channels(t *testing.T) {
	deps := testDeps(t)
	messenger := deps.MustGetMessenger()

	// capture only heater channel
	var gotHeater []core.Message
	subErr := messenger.Subscribe(context.Background(), "test", "heater-topic", 0, func(m core.Message) error {
		gotHeater = append(gotHeater, m)
		return nil
	})
	assert.NoError(t, subErr)

	cfg := core.ServiceConfig{
		Name: "thermo",
		Type: "thermostat",
		Pubs: map[string]core.ChannelInfo{
			"heater": {Name: "heater-topic"},
			// cooler intentionally missing
		},
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
	_ = messenger.Send(core.Message{ChannelName: "temp-topic", Metric: 20})

	// Wait for processing
	time.Sleep(1 * time.Second)

	require.NotEmpty(t, gotHeater)
	assert.Equal(t, "ON", gotHeater[0].State)
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
			Pubs: map[string]core.ChannelInfo{
				"events": {Name: "events"},
				"heater": {Name: "heater"},
				"cooler": {Name: "cooler"},
				"errors": {Name: "errors"},
			},
			Subs: map[string]core.ChannelInfo{
				"temp": {Name: "temp"},
			},
			Config: map[string]any{"minTemp": 10.0, "maxTemp": 30.0, "mode": "auto"},
		}
		svc := NewService(deps, cfg)
		errs := svc.ValidateConfig()
		assert.Empty(t, errs)
	})

	t.Run("missing pubs", func(t *testing.T) {
		cfg := core.ServiceConfig{
			Name:   "thermo",
			Type:   "thermostat",
			Pubs:   map[string]core.ChannelInfo{},
			Subs:   map[string]core.ChannelInfo{"temp": {Name: "temp"}},
			Config: map[string]any{"minTemp": 10.0, "maxTemp": 30.0},
		}
		svc := NewService(deps, cfg)
		errs := svc.ValidateConfig()
		assert.NotEmpty(t, errs)
		assert.ErrorContains(t, errs[0], "required pubs channel 'events' is missing")
	})

	t.Run("missing subs", func(t *testing.T) {
		cfg := core.ServiceConfig{
			Name: "thermo",
			Type: "thermostat",
			Pubs: map[string]core.ChannelInfo{
				"events": {Name: "events"},
				"heater": {Name: "heater"},
				"cooler": {Name: "cooler"},
				"errors": {Name: "errors"},
			},
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
			Name: "thermo",
			Type: "thermostat",
			Pubs: map[string]core.ChannelInfo{
				"events": {Name: "events"},
				"heater": {Name: "heater"},
				"cooler": {Name: "cooler"},
				"errors": {Name: "errors"},
			},
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
			Name: "thermo",
			Type: "thermostat",
			Pubs: map[string]core.ChannelInfo{
				"events": {Name: "events"},
				"heater": {Name: "heater"},
				"cooler": {Name: "cooler"},
				"errors": {Name: "errors"},
			},
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
			Name: "thermo",
			Type: "thermostat",
			Pubs: map[string]core.ChannelInfo{
				"events": {Name: "events"},
				"heater": {Name: "heater"},
				"cooler": {Name: "cooler"},
				"errors": {Name: "errors"},
			},
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
			Name: "thermo",
			Type: "thermostat",
			Pubs: map[string]core.ChannelInfo{
				"events": {Name: "events"},
				"heater": {Name: "heater"},
				"cooler": {Name: "cooler"},
				"errors": {Name: "errors"},
			},
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
			msg := core.Message{Metric: tc.inputTemp}
			event := svc.updateState(msg, logger)
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
			ev := svc.updateState(core.Message{Metric: 60}, logger)
			assert.Equal(t, "OFF", ev.HeaterTargetState)
			assert.Equal(t, "OFF", ev.CoolerTargetState)

			// drop to 51 -> both OFF
			ev = svc.updateState(core.Message{Metric: 51}, logger)
			assert.Equal(t, "OFF", ev.HeaterTargetState)
			assert.Equal(t, "OFF", ev.CoolerTargetState)

			// drop to just below min -> heater ON, cooler OFF
			ev = svc.updateState(core.Message{Metric: 49}, logger)
			assert.Equal(t, "ON", ev.HeaterTargetState)
			assert.Equal(t, "OFF", ev.CoolerTargetState)
			svc.HeaterState = ev.HeaterTargetState // simulate heater turning ON

			// drop to just above min, hysteresis should keep heater ON
			ev = svc.updateState(core.Message{Metric: 51}, logger)
			assert.Equal(t, "ON", ev.HeaterTargetState)
			assert.Equal(t, "OFF", ev.CoolerTargetState)

			// temp goes above min + hysteresis -> heater OFF
			ev = svc.updateState(core.Message{Metric: 58}, logger)
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
			ev := svc.updateState(core.Message{Metric: 70}, logger)
			assert.Equal(t, "OFF", ev.HeaterTargetState)
			assert.Equal(t, "OFF", ev.CoolerTargetState)

			// rise near max -> both OFF
			ev = svc.updateState(core.Message{Metric: 74}, logger)
			assert.Equal(t, "OFF", ev.HeaterTargetState)
			assert.Equal(t, "OFF", ev.CoolerTargetState)

			// rise just above max -> cooler ON, heater OFF
			ev = svc.updateState(core.Message{Metric: 76}, logger)
			assert.Equal(t, "OFF", ev.HeaterTargetState)
			assert.Equal(t, "ON", ev.CoolerTargetState)
			svc.CoolerState = ev.CoolerTargetState // simulate cooler turning ON

			// drop just below max, hysteresis should keep cooler ON
			ev = svc.updateState(core.Message{Metric: 74}, logger)
			assert.Equal(t, "OFF", ev.HeaterTargetState)
			assert.Equal(t, "ON", ev.CoolerTargetState)

			// temp goes below max + hysteresis -> heater OFF
			ev = svc.updateState(core.Message{Metric: 70}, logger)
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
	ev := svc.updateState(core.Message{Metric: 60}, logger)
	assert.Equal(t, "OFF", ev.HeaterTargetState)
	assert.Equal(t, "OFF", ev.CoolerTargetState)

	// rise above target -> cooler ON
	ev = svc.updateState(core.Message{Metric: 65}, logger)
	assert.Equal(t, "OFF", ev.HeaterTargetState)
	assert.Equal(t, "ON", ev.CoolerTargetState)
	svc.CoolerState = ev.CoolerTargetState // simulate cooler turning ON

	// drop just above target -> cooler ON, heater OFF
	ev = svc.updateState(core.Message{Metric: 61}, logger)
	assert.Equal(t, "OFF", ev.HeaterTargetState)
	assert.Equal(t, "ON", ev.CoolerTargetState)

	// drop just below target, hysteresis should keep cooler ON
	ev = svc.updateState(core.Message{Metric: 59}, logger)
	assert.Equal(t, "OFF", ev.HeaterTargetState)
	assert.Equal(t, "ON", ev.CoolerTargetState)

	// drop below max + hysteresis -> heater OFF
	ev = svc.updateState(core.Message{Metric: 50}, logger)
	assert.Equal(t, "OFF", ev.HeaterTargetState)
	assert.Equal(t, "OFF", ev.CoolerTargetState)

}

func Test_updateState_heat_min_equals_max(t *testing.T) {
	deps := testDeps(t)
	svc := Service{Deps: deps, Cfg: core.ServiceConfig{Name: "thermo", Type: "thermostat"}, MinTemp: 60, MaxTemp: 60, Mode: "heat", Hysteresis: 2}
	logger := deps.MustGetLogger()

	// exact -> both OFF
	ev := svc.updateState(core.Message{Metric: 60}, logger)
	assert.Equal(t, "OFF", ev.HeaterTargetState)
	assert.Equal(t, "OFF", ev.CoolerTargetState)

	// drop below target -> heater ON
	ev = svc.updateState(core.Message{Metric: 55}, logger)
	assert.Equal(t, "ON", ev.HeaterTargetState)
	assert.Equal(t, "OFF", ev.CoolerTargetState)
	svc.HeaterState = ev.HeaterTargetState // simulate cooler turning ON

	// rise just below target -> heater stays on
	ev = svc.updateState(core.Message{Metric: 59}, logger)
	assert.Equal(t, "ON", ev.HeaterTargetState)
	assert.Equal(t, "OFF", ev.CoolerTargetState)

	// rise just above target, hysteresis should keep heater ON
	ev = svc.updateState(core.Message{Metric: 61}, logger)
	assert.Equal(t, "ON", ev.HeaterTargetState)
	assert.Equal(t, "OFF", ev.CoolerTargetState)

	// temp rises above max + hysteresis -> heater OFF
	ev = svc.updateState(core.Message{Metric: 70}, logger)
	assert.Equal(t, "OFF", ev.HeaterTargetState)
	assert.Equal(t, "OFF", ev.CoolerTargetState)
}

func Test_updateState_auto_min_equals_max(t *testing.T) {
	deps := testDeps(t)
	svc := Service{Deps: deps, Cfg: core.ServiceConfig{Name: "thermo", Type: "thermostat"}, MinTemp: 60, MaxTemp: 60, Mode: "auto", Hysteresis: 2}
	logger := deps.MustGetLogger()

	// exact -> both OFF
	ev := svc.updateState(core.Message{Metric: 60}, logger)
	assert.Equal(t, "OFF", ev.HeaterTargetState)
	assert.Equal(t, "OFF", ev.CoolerTargetState)

	// rise above target -> cooler ON
	ev = svc.updateState(core.Message{Metric: 65}, logger)
	assert.Equal(t, "OFF", ev.HeaterTargetState)
	assert.Equal(t, "ON", ev.CoolerTargetState)
	svc.CoolerState = ev.CoolerTargetState // simulate cooler turning ON

	// drop just above target -> cooler ON, heater OFF
	ev = svc.updateState(core.Message{Metric: 61}, logger)
	assert.Equal(t, "OFF", ev.HeaterTargetState)
	assert.Equal(t, "ON", ev.CoolerTargetState)

	// drop just below target, hysteresis should keep cooler ON
	ev = svc.updateState(core.Message{Metric: 59}, logger)
	assert.Equal(t, "OFF", ev.HeaterTargetState)
	assert.Equal(t, "ON", ev.CoolerTargetState)

	// drop below target -> heater ON
	ev = svc.updateState(core.Message{Metric: 55}, logger)
	assert.Equal(t, "ON", ev.HeaterTargetState)
	assert.Equal(t, "OFF", ev.CoolerTargetState)
	svc.HeaterState = ev.HeaterTargetState // simulate cooler turning ON
	svc.CoolerState = ev.CoolerTargetState // simulate cooler turning ON

	// rise just below target -> heater stays on
	ev = svc.updateState(core.Message{Metric: 59}, logger)
	assert.Equal(t, "ON", ev.HeaterTargetState)
	assert.Equal(t, "OFF", ev.CoolerTargetState)

	// rise just above target, hysteresis should keep heater ON
	ev = svc.updateState(core.Message{Metric: 61}, logger)
	assert.Equal(t, "ON", ev.HeaterTargetState)
	assert.Equal(t, "OFF", ev.CoolerTargetState)

	// temp rises above max + hysteresis -> cooler on
	ev = svc.updateState(core.Message{Metric: 70}, logger)
	assert.Equal(t, "OFF", ev.HeaterTargetState)
	assert.Equal(t, "ON", ev.CoolerTargetState)
}
