package kodi

import (
	"context"
	"encoding/json"
	"keyop/core"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockKodiHandler struct {
	mu           sync.RWMutex
	activePlayer *activePlayer
	playingItem  *itemDetails
	playerProps  *playerProperties
	lastUsername string
	lastPassword string
}

func (m *mockKodiHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	username, password, ok := r.BasicAuth()
	m.mu.Lock()
	if ok {
		m.lastUsername = username
		m.lastPassword = password
	}
	m.mu.Unlock()

	var req jsonRPCRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	var result interface{}
	switch req.Method {
	case "Player.GetActivePlayers":
		if m.activePlayer != nil {
			result = []activePlayer{*m.activePlayer}
		} else {
			result = []activePlayer{}
		}
	case "Player.GetItem":
		result = m.playingItem
	case "Player.GetProperties":
		result = m.playerProps
	default:
		http.Error(w, "Method not found", http.StatusNotFound)
		return
	}

	resp := jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
	}
	resJSON, _ := json.Marshal(result)
	resp.Result = resJSON

	json.NewEncoder(w).Encode(resp)
}

func TestService_Check(t *testing.T) {
	mockKodi := &mockKodiHandler{}
	server := httptest.NewServer(mockKodi)
	defer server.Close()

	u, err := url.Parse(server.URL)
	require.NoError(t, err)
	host := u.Hostname()
	port, _ := strconv.Atoi(u.Port())

	tmpDir, err := os.MkdirTemp("", "kodi_test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	deps := core.Dependencies{}
	logger := &core.FakeLogger{}
	deps.SetLogger(logger)
	osProvider := core.OsProvider{}
	deps.SetOsProvider(osProvider)
	stateStore := core.NewFileStateStore(tmpDir, osProvider)
	deps.SetStateStore(stateStore)

	messenger := &fakeMessenger{}
	deps.SetMessenger(messenger)

	cfg := core.ServiceConfig{
		Name: "kodi-test",
		Type: "kodi",
		Pubs: map[string]core.ChannelInfo{
			"events": {Name: "kodi-events"},
		},
		Config: map[string]interface{}{
			"host": host,
			"port": port,
		},
	}

	svc := NewService(deps, cfg)

	t.Run("Initially nothing playing", func(t *testing.T) {
		err := svc.Check()
		assert.NoError(t, err)
		assert.Empty(t, messenger.messages)

		mockKodi.mu.RLock()
		assert.Equal(t, "kodi", mockKodi.lastUsername)
		assert.Equal(t, "kodi", mockKodi.lastPassword)
		mockKodi.mu.RUnlock()
	})

	t.Run("Custom credentials", func(t *testing.T) {
		cfg2 := cfg
		cfg2.Config = map[string]interface{}{
			"host":     host,
			"port":     port,
			"username": "admin",
			"password": "password123",
		}
		svc2 := NewService(deps, cfg2)
		err := svc2.Check()
		assert.NoError(t, err)

		mockKodi.mu.RLock()
		assert.Equal(t, "admin", mockKodi.lastUsername)
		assert.Equal(t, "password123", mockKodi.lastPassword)
		mockKodi.mu.RUnlock()
	})

	t.Run("Movie starts playing", func(t *testing.T) {
		messenger.messages = nil
		mockKodi.mu.Lock()
		mockKodi.activePlayer = &activePlayer{PlayerID: 1, Type: "video"}
		mockKodi.playingItem = &itemDetails{}
		mockKodi.playingItem.Item.Title = "The Matrix"
		mockKodi.playerProps = &playerProperties{}
		mockKodi.playerProps.Time.Minutes = 1
		mockKodi.playerProps.Time.Seconds = 30
		mockKodi.mu.Unlock()

		err := svc.Check()
		assert.NoError(t, err)

		require.Len(t, messenger.messages, 1)
		assert.Equal(t, "kodi-events", messenger.messages[0].ChannelName)
		assert.Contains(t, messenger.messages[0].Text, "Movie started: The Matrix")
		data := messenger.messages[0].Data.(map[string]string)
		assert.Equal(t, "The Matrix", data["title"])
		assert.Equal(t, "playing", data["status"])
		assert.Equal(t, "00:01:30", data["time"])

		messenger.messages = nil
	})

	t.Run("Same movie still playing", func(t *testing.T) {
		messenger.messages = nil
		mockKodi.mu.Lock()
		mockKodi.playerProps.Time.Minutes = 2
		mockKodi.playerProps.Time.Seconds = 0
		mockKodi.mu.Unlock()

		err := svc.Check()
		assert.NoError(t, err)

		require.Len(t, messenger.messages, 1)
		assert.Contains(t, messenger.messages[0].Text, "Movie playing: The Matrix (00:02:00)")
		data := messenger.messages[0].Data.(map[string]string)
		assert.Equal(t, "00:02:00", data["time"])
	})

	t.Run("Movie changes", func(t *testing.T) {
		messenger.messages = nil
		mockKodi.mu.Lock()
		mockKodi.playingItem.Item.Title = "Inception"
		mockKodi.playerProps.Time.Minutes = 0
		mockKodi.playerProps.Time.Seconds = 10
		mockKodi.mu.Unlock()

		err := svc.Check()
		assert.NoError(t, err)

		require.Len(t, messenger.messages, 1)
		assert.Contains(t, messenger.messages[0].Text, "Movie started: Inception")
		data := messenger.messages[0].Data.(map[string]string)
		assert.Equal(t, "00:00:10", data["time"])
	})

	t.Run("Movie stops playing", func(t *testing.T) {
		messenger.messages = nil
		mockKodi.mu.Lock()
		mockKodi.activePlayer = nil
		mockKodi.playingItem = nil
		mockKodi.mu.Unlock()

		err := svc.Check()
		assert.NoError(t, err)

		require.Len(t, messenger.messages, 1)
		assert.Equal(t, "kodi-events", messenger.messages[0].ChannelName)
		assert.Contains(t, messenger.messages[0].Text, "Movie stopped: Inception")
		data := messenger.messages[0].Data.(map[string]string)
		assert.Equal(t, "Inception", data["title"])
		assert.Equal(t, "stopped", data["status"])
	})

	t.Run("Invalid config - missing port", func(t *testing.T) {
		cfg3 := cfg
		cfg3.Config = map[string]interface{}{
			"host": host,
		}
		svc3 := NewService(deps, cfg3)
		errs := svc3.ValidateConfig()
		assert.NotEmpty(t, errs)
	})

	t.Run("Invalid config - port as float64", func(t *testing.T) {
		cfg4 := cfg
		cfg4.Config = map[string]interface{}{
			"host": host,
			"port": 8080.0,
		}
		svc4 := NewService(deps, cfg4)
		errs := svc4.ValidateConfig()
		assert.NotEmpty(t, errs)
	})

	t.Run("Valid config", func(t *testing.T) {
		errs := svc.(*Service).ValidateConfig()
		assert.Empty(t, errs)
	})
}

type fakeMessenger struct {
	messages []core.Message
}

func (f *fakeMessenger) Send(msg core.Message) error {
	f.messages = append(f.messages, msg)
	return nil
}

func (f *fakeMessenger) Subscribe(ctx context.Context, sourceName string, channelName string, serviceType string, serviceName string, maxAge time.Duration, messageHandler func(core.Message) error) error {
	return nil
}

func (f *fakeMessenger) SubscribeExtended(ctx context.Context, source string, channelName string, serviceType string, serviceName string, maxAge time.Duration, messageHandler func(core.Message, string, int64) error) error {
	return nil
}

func (f *fakeMessenger) SetReaderState(channelName string, readerName string, fileName string, offset int64) error {
	return nil
}

func (f *fakeMessenger) SeekToEnd(channelName string, readerName string) error {
	return nil
}

func (f *fakeMessenger) SetDataDir(dir string) {}
