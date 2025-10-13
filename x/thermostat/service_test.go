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

func Test_updateState_thresholds(t *testing.T) {
	deps := testDeps()
	svc := Service{Deps: deps, Cfg: core.ServiceConfig{Name: "thermo", Type: "thermostat"}}
	logger := deps.MustGetLogger()

	// below min -> heater ON, cooler OFF
	ev := svc.updateState(core.Message{Value: 40}, logger)
	assert.Equal(t, "ON", ev.HeaterTargetState)
	assert.Equal(t, "OFF", ev.CoolerTargetState)
	assert.InDelta(t, 50.0, ev.MinTemp, 0.001)
	assert.InDelta(t, 75.0, ev.MaxTemp, 0.001)

	// between -> both OFF
	ev = svc.updateState(core.Message{Value: 60}, logger)
	assert.Equal(t, "OFF", ev.HeaterTargetState)
	assert.Equal(t, "OFF", ev.CoolerTargetState)

	// above max -> cooler ON, heater OFF
	ev = svc.updateState(core.Message{Value: 90}, logger)
	assert.Equal(t, "OFF", ev.HeaterTargetState)
	assert.Equal(t, "ON", ev.CoolerTargetState)
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
	}

	svc := NewService(deps, cfg)
	err := svc.Initialize()
	assert.NoError(t, err)

	// Send a cold temp to turn heater ON
	_ = messenger.Send("temp-topic", core.Message{Value: 20}, nil)

	assert.Len(t, gotHeater, 1)
	assert.Equal(t, "ON", gotHeater[0].State)
}
