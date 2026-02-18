package webSocketServer

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"keyop/core"
	"keyop/util"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateConfig(t *testing.T) {
	home, err := os.MkdirTemp("", "keyop-test-home-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(home)

	certDir := filepath.Join(home, ".keyop", "certs")
	if err := os.MkdirAll(certDir, 0755); err != nil {
		t.Fatalf("failed to create cert dir: %v", err)
	}

	// Create dummy files for validation
	files := []string{
		filepath.Join(certDir, "keyop-server.crt"),
		filepath.Join(certDir, "keyop-server.key"),
		filepath.Join(certDir, "ca.crt"),
	}
	for _, f := range files {
		if err := os.WriteFile(f, []byte("dummy"), 0644); err != nil {
			t.Fatalf("failed to create dummy file %s: %v", f, err)
		}
	}

	tests := []struct {
		name    string
		config  map[string]any
		wantErr bool
	}{
		{
			name: "valid config",
			config: map[string]any{
				"port": 8080,
			},
			wantErr: false,
		},
		{
			name:    "missing port",
			config:  map[string]any{},
			wantErr: true,
		},
		{
			name: "wrong port type",
			config: map[string]any{
				"port": "8080",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps := core.Dependencies{}
			deps.SetLogger(&core.FakeLogger{})
			fakeOs := &core.FakeOsProvider{Home: home}
			fakeOs.StatFunc = func(name string) (os.FileInfo, error) {
				return os.Stat(name)
			}
			deps.SetOsProvider(fakeOs)

			svc := NewService(deps, core.ServiceConfig{
				Config: tt.config,
			})

			errs := svc.ValidateConfig()
			if (len(errs) > 0) != tt.wantErr {
				t.Errorf("ValidateConfig() errors = %v, wantErr %v", errs, tt.wantErr)
			}
		})
	}
}

func TestInitialize(t *testing.T) {
	home, err := os.MkdirTemp("", "keyop-test-home-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(home)

	certDir := filepath.Join(home, ".keyop", "certs")
	_, _, _, _, err = util.CreateTestCerts(certDir)
	if err != nil {
		t.Fatalf("failed to create test certs: %v", err)
	}

	deps := core.Dependencies{}
	deps.SetLogger(&core.FakeLogger{})
	fakeOs := &core.FakeOsProvider{Home: home}
	fakeOs.ReadFileFunc = func(name string) ([]byte, error) {
		return os.ReadFile(name)
	}
	deps.SetOsProvider(fakeOs)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	deps.SetContext(ctx)

	svc := NewService(deps, core.ServiceConfig{
		Config: map[string]any{
			"port": 0, // 0 for random port
		},
	})

	err = svc.Initialize()
	if err != nil {
		t.Fatalf("Initialize() failed: %v", err)
	}

	// Since it starts a goroutine for ListenAndServeTLS, we just check if it returns nil
	// and doesn't panic.
}

type fakeMessenger struct {
	mu                 sync.Mutex
	messages           []core.Message
	subscribeChan      chan core.Message
	subscribedChannels map[string]bool
	seekedChannels     map[string]bool
	readerStates       map[string]string // channel:reader -> fileName:offset
}

func newFakeMessenger() *fakeMessenger {
	return &fakeMessenger{
		subscribeChan:      make(chan core.Message, 10),
		subscribedChannels: make(map[string]bool),
		seekedChannels:     make(map[string]bool),
		readerStates:       make(map[string]string),
	}
}

func (f *fakeMessenger) Send(msg core.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.messages = append(f.messages, msg)
	return nil
}

func (f *fakeMessenger) Subscribe(ctx context.Context, sourceName string, channelName string, serviceType string, serviceName string, maxAge time.Duration, messageHandler func(core.Message) error) error {
	return f.SubscribeExtended(ctx, sourceName, channelName, serviceType, serviceName, maxAge, func(msg core.Message, fileName string, offset int64) error {
		return messageHandler(msg)
	})
}

func (f *fakeMessenger) SubscribeExtended(ctx context.Context, source string, channelName string, serviceType string, serviceName string, maxAge time.Duration, messageHandler func(core.Message, string, int64) error) error {
	f.mu.Lock()
	f.subscribedChannels[channelName] = true
	f.mu.Unlock()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case msg := <-f.subscribeChan:
				if msg.ChannelName == channelName {
					_ = messageHandler(msg, "testfile", 0)
				}
			}
		}
	}()
	return nil
}

func (f *fakeMessenger) SetReaderState(channelName string, readerName string, fileName string, offset int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.readerStates[channelName+":"+readerName] = fmt.Sprintf("%s:%d", fileName, offset)
	return nil
}

func (f *fakeMessenger) SeekToEnd(channelName string, readerName string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seekedChannels[channelName] = true
	return nil
}

func (f *fakeMessenger) SetDataDir(dir string) {}

func (f *fakeMessenger) GetStats() core.MessengerStats {
	return core.MessengerStats{}
}

func TestHandleConnection(t *testing.T) {
	home, err := os.MkdirTemp("", "keyop-test-home-*")
	require.NoError(t, err)
	defer os.RemoveAll(home)

	certDir := filepath.Join(home, ".keyop", "certs")
	serverCert, serverKey, clientCert, clientKey, err := util.CreateTestCerts(certDir)
	require.NoError(t, err)

	deps := core.Dependencies{}
	logger := &core.FakeLogger{}
	deps.SetLogger(logger)

	fakeOs := &core.FakeOsProvider{Home: home}
	fakeOs.ReadFileFunc = func(name string) ([]byte, error) {
		return os.ReadFile(name)
	}
	deps.SetOsProvider(fakeOs)

	messenger := newFakeMessenger()
	deps.SetMessenger(messenger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	deps.SetContext(ctx)

	svc := NewService(deps, core.ServiceConfig{
		Name: "ws-server",
		Config: map[string]any{
			"port": 0,
		},
		Subs: map[string]core.ChannelInfo{
			"test-channel": {Name: "test-channel"},
		},
	}).(*Service)

	// Setup TLS server for testing handleConnection
	cert, err := tls.LoadX509KeyPair(serverCert, serverKey)
	require.NoError(t, err)

	caCert, err := os.ReadFile(filepath.Join(certDir, "ca.crt"))
	require.NoError(t, err)
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCert)

	tlsConfig := &tls.Config{
		Certificates:       []tls.Certificate{cert},
		ClientCAs:          caCertPool,
		ClientAuth:         tls.RequireAndVerifyClientCert,
		InsecureSkipVerify: true,
	}

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		// Use a WaitGroup to ensure handleConnection finishes before server close if possible,
		// though handleConnection might run for long.
		svc.handleConnection(conn)
	}))
	server.TLS = tlsConfig
	server.StartTLS()
	defer server.Close()

	// Setup client
	clientCertObj, err := tls.LoadX509KeyPair(clientCert, clientKey)
	require.NoError(t, err)

	dialer := websocket.Dialer{
		TLSClientConfig: &tls.Config{
			RootCAs:            caCertPool,
			Certificates:       []tls.Certificate{clientCertObj},
			InsecureSkipVerify: true,
		},
	}

	wsURL := strings.Replace(server.URL, "https", "wss", 1) + "/ws"
	conn, _, err := dialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer conn.Close()

	// 1. Send Subscribe
	subMsg := wsMessage{
		Type:     "subscribe",
		Channels: []string{"test-channel"},
	}
	err = conn.WriteJSON(subMsg)
	require.NoError(t, err)

	// Send ACK for any potential message (though none sent yet)
	// We need to make sure the server has processed the subscribe
	// Since we are using a fake messenger that records subscriptions, we can wait for it.

	// Wait for subscription to happen in background
	require.Eventually(t, func() bool {
		messenger.mu.Lock()
		defer messenger.mu.Unlock()
		return messenger.subscribedChannels["test-channel"]
	}, 5*time.Second, 100*time.Millisecond)

	// 2. Send a message from messenger and wait for it on WS
	testMsg := core.Message{Text: "hello", ChannelName: "test-channel"}
	messenger.subscribeChan <- testMsg

	var received wsMessage
	err = conn.ReadJSON(&received)
	require.NoError(t, err)
	assert.Equal(t, "message", received.Type)
	assert.Equal(t, "hello", received.Payload.Text)

	// 3. Send ACK
	ackMsg := wsMessage{
		Type: "ack",
	}
	err = conn.WriteJSON(ackMsg)
	require.NoError(t, err)

	conn.Close() // Close first connection after test

	// 4. Test Resume
	conn2, _, err := dialer.Dial(wsURL, nil)
	require.NoError(t, err)

	resumeMsg := wsMessage{
		Type:     "resume",
		Queue:    "test-channel",
		FileName: "oldfile",
		Offset:   123,
	}
	err = conn2.WriteJSON(resumeMsg)
	require.NoError(t, err)

	subMsg2 := wsMessage{
		Type:     "subscribe",
		Channels: []string{"test-channel"},
	}
	err = conn2.WriteJSON(subMsg2)
	require.NoError(t, err)

	assert.Eventually(t, func() bool {
		messenger.mu.Lock()
		defer messenger.mu.Unlock()
		found := false
		for k, v := range messenger.readerStates {
			if strings.HasPrefix(k, "test-channel:ws_") && v == "oldfile:123" {
				found = true
				break
			}
		}
		return found
	}, 2*time.Second, 100*time.Millisecond)

	// 5. Test Ping
	// We skip the long ping wait in test.

	testMsg2 := core.Message{Text: "world", ChannelName: "test-channel"}
	messenger.subscribeChan <- testMsg2

	err = conn2.ReadJSON(&received)
	require.NoError(t, err)
	assert.Equal(t, "world", received.Payload.Text)

	// 6. Close first connection
	// Already closed above

	// 7. Close second connection
	conn2.Close()
}
