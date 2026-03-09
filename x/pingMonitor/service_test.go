//nolint:revive
package pingMonitor

import (
	"errors"
	"keyop/core"
	"keyop/core/testutil"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCheck(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))

	t.Run("successful ping", func(t *testing.T) {
		deps := core.Dependencies{}
		deps.SetLogger(logger)
		messenger := testutil.NewFakeMessenger()
		deps.SetMessenger(messenger)

		fakeOs := &core.FakeOsProvider{
			CommandFunc: func(_ string, _ ...string) core.CommandApi {
				return &core.FakeCommand{
					CombinedOutputFunc: func() ([]byte, error) {
						return []byte("64 bytes from 8.8.8.8: icmp_seq=1 ttl=117 time=12.3 ms"), nil
					},
				}
			},
		}
		deps.SetOsProvider(fakeOs)

		cfg := core.ServiceConfig{
			Name: "net-mon",
			Type: "pingMonitor",
			Pubs: map[string]core.ChannelInfo{
				"status":  {Name: "status-topic"},
				"metrics": {Name: "metrics-topic"},
			},
			Config: map[string]interface{}{
				"host": "google.com",
			},
		}
		svc := NewService(deps, cfg)
		err := svc.Check()
		assert.NoError(t, err)

		assert.Len(t, messenger.SentMessages, 2)

		foundEvent := false
		foundMetric := false
		for _, msg := range messenger.SentMessages {
			if msg.Event == "ping_status" {
				foundEvent = true
				assert.Contains(t, msg.Text, "successful")
				assert.Contains(t, msg.Text, "12.3")
			}
			if msg.Event == "ping_metric" {
				foundMetric = true
				assert.Equal(t, 12.3, msg.Metric)
				assert.Equal(t, "net-mon.ping_time", msg.MetricName)
			}
		}
		assert.True(t, foundEvent, "Expected status message")
		assert.True(t, foundMetric, "Expected metrics message")
	})

	t.Run("successful ping with custom metric name", func(t *testing.T) {
		deps := core.Dependencies{}
		deps.SetLogger(logger)
		messenger := testutil.NewFakeMessenger()
		deps.SetMessenger(messenger)

		fakeOs := &core.FakeOsProvider{
			CommandFunc: func(_ string, _ ...string) core.CommandApi {
				return &core.FakeCommand{
					CombinedOutputFunc: func() ([]byte, error) {
						return []byte("64 bytes from 8.8.8.8: icmp_seq=1 ttl=117 time=12.3 ms"), nil
					},
				}
			},
		}
		deps.SetOsProvider(fakeOs)

		cfg := core.ServiceConfig{
			Name: "net-mon",
			Type: "pingMonitor",
			Pubs: map[string]core.ChannelInfo{
				"status":  {Name: "status-topic"},
				"metrics": {Name: "metrics-topic"},
			},
			Config: map[string]interface{}{
				"host":        "google.com",
				"metric_name": "custom.ping.latency",
			},
		}
		svc := NewService(deps, cfg)
		err := svc.Check()
		assert.NoError(t, err)

		foundMetric := false
		for _, msg := range messenger.SentMessages {
			if msg.Event == "ping_metric" {
				foundMetric = true
				assert.Equal(t, 12.3, msg.Metric)
				assert.Equal(t, "custom.ping.latency", msg.MetricName)
			}
		}
		assert.True(t, foundMetric, "Expected metrics message with custom name")
	})

	t.Run("failed ping sets status", func(t *testing.T) {
		deps := core.Dependencies{}
		deps.SetLogger(logger)
		messenger := testutil.NewFakeMessenger()
		deps.SetMessenger(messenger)

		fakeOs := &core.FakeOsProvider{
			CommandFunc: func(_ string, _ ...string) core.CommandApi {
				return &core.FakeCommand{
					CombinedOutputFunc: func() ([]byte, error) {
						return []byte("ping: cannot resolve unreachable.host: Unknown host"), errors.New("ping failed")
					},
				}
			},
		}
		deps.SetOsProvider(fakeOs)

		cfg := core.ServiceConfig{
			Name: "net-mon",
			Type: "pingMonitor",
			Pubs: map[string]core.ChannelInfo{
				"status":  {Name: "status-topic"},
				"metrics": {Name: "metrics-topic"},
			},
			Config: map[string]interface{}{
				"host": "unreachable.host",
			},
		}
		svc := NewService(deps, cfg)
		err := svc.Check()
		assert.NoError(t, err)
		assert.Len(t, messenger.SentMessages, 1)
		assert.Equal(t, "net-mon", messenger.SentMessages[0].ChannelName)
		assert.Contains(t, messenger.SentMessages[0].Text, "unreachable.host")
	})
}

func TestValidateConfig(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))

	t.Run("valid config", func(t *testing.T) {
		deps := core.Dependencies{}
		deps.SetLogger(logger)
		cfg := core.ServiceConfig{
			Name: "net-mon",
			Config: map[string]interface{}{
				"host": "google.com",
			},
		}
		svc := NewService(deps, cfg)
		errs := svc.ValidateConfig()
		assert.Empty(t, errs)
	})

	t.Run("missing host", func(t *testing.T) {
		deps := core.Dependencies{}
		deps.SetLogger(logger)
		cfg := core.ServiceConfig{
			Name:   "net-mon",
			Config: map[string]interface{}{},
		}
		svc := NewService(deps, cfg)
		errs := svc.ValidateConfig()
		found := false
		for _, e := range errs {
			if strings.Contains(e.Error(), "host is required") {
				found = true
			}
		}
		assert.True(t, found, "expected host required error")
	})

}
