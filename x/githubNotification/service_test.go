package githubNotification

import (
	"encoding/json"
	"keyop/core"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testDeps(t *testing.T) core.Dependencies {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	deps := core.Dependencies{}

	tmpDir, err := os.MkdirTemp("", "github_test")
	require.NoError(t, err)
	t.Cleanup(func() {
		os.RemoveAll(tmpDir)
	})

	osProvider := core.OsProvider{}
	deps.SetOsProvider(osProvider)
	deps.SetLogger(logger)
	deps.SetStateStore(core.NewFileStateStore(tmpDir, osProvider))
	messenger := core.NewMessenger(logger, deps.MustGetOsProvider())
	messenger.SetDataDir(tmpDir)

	deps.SetMessenger(messenger)

	return deps
}

func TestService_ValidateConfig(t *testing.T) {
	deps := testDeps(t)

	tests := []struct {
		name        string
		pubs        map[string]core.ChannelInfo
		config      map[string]interface{}
		expectError bool
	}{
		{
			name: "valid config",
			pubs: map[string]core.ChannelInfo{
				"alerts": {Name: "alerts-channel"},
			},
			config: map[string]interface{}{
				"token": "fake-token",
			},
			expectError: false,
		},
		{
			name: "missing alerts pub",
			pubs: map[string]core.ChannelInfo{},
			config: map[string]interface{}{
				"token": "fake-token",
			},
			expectError: true,
		},
		{
			name: "missing token",
			pubs: map[string]core.ChannelInfo{
				"alerts": {Name: "alerts-channel"},
			},
			config:      map[string]interface{}{},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := core.ServiceConfig{
				Pubs:   tt.pubs,
				Config: tt.config,
			}
			svc := NewService(deps, cfg)
			errs := svc.ValidateConfig()
			if tt.expectError {
				assert.NotEmpty(t, errs)
			} else {
				assert.Empty(t, errs)
			}
		})
	}
}

func TestService_Check(t *testing.T) {
	deps := testDeps(t)
	messenger := deps.MustGetMessenger()

	var capturedMessages []core.Message
	err := messenger.Subscribe("test", "alerts-channel", 0, func(msg core.Message) error {
		capturedMessages = append(capturedMessages, msg)
		return nil
	})
	require.NoError(t, err)

	// Mock GitHub API
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer fake-token", r.Header.Get("Authorization"))

		notifications := []Notification{
			{
				ID: "1",
				Subject: struct {
					Title string `json:"title"`
					URL   string `json:"url"`
					Type  string `json:"type"`
				}{
					Title: "New PR comment",
				},
				UpdatedAt: time.Now(),
			},
		}
		json.NewEncoder(w).Encode(notifications)
	}))
	defer ts.Close()

	cfg := core.ServiceConfig{
		Name: "githubNotification-test",
		Type: "githubNotification",
		Pubs: map[string]core.ChannelInfo{
			"alerts": {Name: "alerts-channel"},
		},
		Config: map[string]interface{}{
			"token": "fake-token",
		},
	}

	svc := NewService(deps, cfg).(*Service)
	svc.BaseURL = ts.URL
	svc.lastCheck = time.Now().Add(-1 * time.Hour)

	err = svc.Check()
	assert.NoError(t, err)

	// Wait a bit for messenger to process
	time.Sleep(100 * time.Millisecond)

	assert.Len(t, capturedMessages, 1)
	assert.Equal(t, "GitHub Notification: New PR comment", capturedMessages[0].Text)
}

func TestService_Persistence(t *testing.T) {
	deps := testDeps(t)
	stateStore := deps.MustGetStateStore()

	cfg := core.ServiceConfig{
		Name: "githubNotification-persistence-test",
		Type: "githubNotification",
		Pubs: map[string]core.ChannelInfo{
			"alerts": {Name: "alerts-channel"},
		},
		Config: map[string]interface{}{
			"token": "fake-token",
		},
	}

	// 1. Save state manually and check if Initialize loads it
	lastTime := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	err := stateStore.Save(cfg.Name, lastTime)
	require.NoError(t, err)

	svc := NewService(deps, cfg).(*Service)
	err = svc.Initialize()
	assert.NoError(t, err)
	assert.True(t, svc.lastCheck.Equal(lastTime), "lastCheck should be loaded from state store")

	// 2. Mock GitHub API to return a newer notification and check if state is updated
	newTime := time.Date(2025, 2, 1, 12, 0, 0, 0, time.UTC)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		notifications := []Notification{
			{
				ID: "2",
				Subject: struct {
					Title string `json:"title"`
					URL   string `json:"url"`
					Type  string `json:"type"`
				}{Title: "Newer Notification"},
				UpdatedAt: newTime,
			},
		}
		json.NewEncoder(w).Encode(notifications)
	}))
	defer ts.Close()

	svc.BaseURL = ts.URL
	err = svc.Check()
	assert.NoError(t, err)
	assert.True(t, svc.lastCheck.Equal(newTime), "lastCheck should be updated in memory")

	// 3. Verify it was saved to the store
	var savedTime time.Time
	err = stateStore.Load(cfg.Name, &savedTime)
	assert.NoError(t, err)
	assert.True(t, savedTime.Equal(newTime), "lastCheck should be saved to state store")
}
