package thermostat

import (
	"encoding/json"
	"keyop/core"
	"log/slog"
	"os"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

// helper to build dependencies similar to other service tests
func testDeps() core.Dependencies {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	deps := core.Dependencies{}
	deps.SetOsProvider(core.FakeOsProvider{Host: "test-host"})
	deps.SetLogger(logger)
	deps.SetMessenger(core.NewMessenger(logger, deps.MustGetOsProvider()))
	return deps
}

func Test_tempHandler_publishes_to_heater_and_cooler(t *testing.T) {
	deps := testDeps()
	messenger := deps.MustGetMessenger()

	// capture publishes
	type captured struct {
		msg  core.Message
		data Event
	}
	var mu sync.Mutex
	got := map[string][]captured{}

	// subscribe to heater and cooler channels to capture what thermostat sends
	capture := func(ch string) {
		_ = messenger.Subscribe("test", ch, func(m core.Message) error {
			var ev Event
			_ = json.Unmarshal([]byte(m.Data), &ev)
			mu.Lock()
			defer mu.Unlock()
			got[ch] = append(got[ch], captured{msg: m, data: ev})
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
	err = messenger.Send("temp-topic", core.Message{Value: 80}, nil)
	assert.NoError(t, err)

	// Assertions: thermostat should publish to both heater and cooler
	mu.Lock()
	defer mu.Unlock()

	if assert.Contains(t, got, heaterCh) {
		assert.Len(t, got[heaterCh], 1)
		m := got[heaterCh][0]
		assert.Equal(t, "thermo", m.msg.ServiceName)
		assert.Equal(t, "thermostat", m.msg.ServiceType)
		assert.Equal(t, "OFF", m.msg.State) // heater should be OFF at 80
		assert.Equal(t, 80.0, m.data.Temp)
		assert.Equal(t, "OFF", m.data.HeaterTargetState)
	}

	if assert.Contains(t, got, coolerCh) {
		assert.Len(t, got[coolerCh], 1)
		m := got[coolerCh][0]
		assert.Equal(t, "thermo", m.msg.ServiceName)
		assert.Equal(t, "thermostat", m.msg.ServiceType)
		assert.Equal(t, "ON", m.msg.State) // cooler should be ON at 80
		assert.Equal(t, 80.0, m.data.Temp)
		assert.Equal(t, "ON", m.data.CoolerTargetState)
	}
}

func Test_tempHandler_with_missing_pub_channels(t *testing.T) {
	deps := testDeps()
	messenger := deps.MustGetMessenger()

	// capture only heater channel
	var gotHeater []core.Message
	_ = messenger.Subscribe("test", "heater-topic", func(m core.Message) error {
		gotHeater = append(gotHeater, m)
		return nil
	})

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
	_ = messenger.Send("temp-topic", core.Message{Value: 20}, nil)

	assert.Len(t, gotHeater, 1)
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
			},
			Subs:   map[string]core.ChannelInfo{"temp": {Name: "temp"}},
			Config: map[string]any{"minTemp": 30.0, "maxTemp": 10.0, "mode": "auto"},
		}
		svc := NewService(deps, cfg)
		errs := svc.ValidateConfig()
		assert.NotEmpty(t, errs)
		assert.ErrorContains(t, errs[len(errs)-1], "minTemp must be less than maxTemp")
	})

	t.Run("invalid mode", func(t *testing.T) {
		cfg := core.ServiceConfig{
			Name: "thermo",
			Type: "thermostat",
			Pubs: map[string]core.ChannelInfo{
				"events": {Name: "events"},
				"heater": {Name: "heater"},
				"cooler": {Name: "cooler"},
			},
			Subs:   map[string]core.ChannelInfo{"temp": {Name: "temp"}},
			Config: map[string]any{"minTemp": 10.0, "maxTemp": 30.0, "mode": "banana"},
		}
		svc := NewService(deps, cfg)
		errs := svc.ValidateConfig()
		assert.NotEmpty(t, errs)

		assert.Contains(t, errs[0].Error(), "invalid mode")
		assert.Contains(t, errs[0].Error(), "banana")
		assert.Contains(t, errs[0].Error(), "thermostat")

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
			msg := core.Message{Value: tc.inputTemp}
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

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || (len(s) > len(substr) && (contains(s[1:], substr) || contains(s[:len(s)-1], substr)))) || (len(substr) > 0 && len(s) > 0 && (s[0] == substr[0] && contains(s[1:], substr[1:])))
}
