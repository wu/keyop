package thermostat

import (
	"keyop/core"
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewDefaultService(t *testing.T) {
	// Set test values for the command-line flags
	cmdMinTemp = 55.5
	cmdMaxTemp = 77.7

	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	deps := core.Dependencies{}
	deps.SetLogger(logger)
	deps.SetOsProvider(core.FakeOsProvider{Host: "test-host"})
	deps.SetMessenger(core.NewMessenger(logger, deps.MustGetOsProvider()))

	svc := NewDefaultService(deps)
	assert.NotNil(t, svc)

	cfg := svc.(*Service).Cfg
	assert.Equal(t, "thermostat", cfg.Name)
	assert.Equal(t, "thermostat", cfg.Type)
	assert.Contains(t, cfg.Pubs, "events")
	assert.Contains(t, cfg.Pubs, "heater")
	assert.Contains(t, cfg.Pubs, "cooler")
	assert.Contains(t, cfg.Subs, "temp")
	assert.Equal(t, 55.5, cfg.Config["minTemp"])
	assert.Equal(t, 77.7, cfg.Config["maxTemp"])
}
