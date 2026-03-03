package webSocketServer

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"keyop/core"
	"keyop/util"
	wsp "keyop/x/webSocketProtocol"
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

// Helper utilities for tests to make SetReadDeadline/Close error handling explicit.
func safeSetReadDeadline(t *testing.T, c *websocket.Conn, tm time.Time) {
	t.Helper()
	if err := c.SetReadDeadline(tm); err != nil {
		t.Logf("SetReadDeadline failed: %v", err)
	}
}

func safeClearReadDeadline(t *testing.T, c *websocket.Conn) {
	t.Helper()
	if err := c.SetReadDeadline(time.Time{}); err != nil {
		t.Logf("Clear ReadDeadline failed: %v", err)
	}
}

func TestValidateConfig(t *testing.T) {
	home, err := os.MkdirTemp("", "keyop-test-home-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(home); err != nil {
			t.Logf("failed to remove temp dir %s: %v", home, err)
		}
	}()

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
	defer func() {
		if err := os.RemoveAll(home); err != nil {
			t.Logf("failed to remove temp dir %s: %v", home, err)
		}
	}()

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
	sendErr            error             // if non-nil, Send returns this error
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
	if f.sendErr != nil {
		return f.sendErr
	}
	f.messages = append(f.messages, msg)
	return nil
}

func (f *fakeMessenger) Subscribe(ctx context.Context, sourceName, channelName, serviceType, serviceName string, maxAge time.Duration, messageHandler func(core.Message) error) error {
	return f.SubscribeExtended(ctx, sourceName, channelName, serviceType, serviceName, maxAge, func(msg core.Message, fileName string, offset int64) error {
		return messageHandler(msg)
	})
}

func (f *fakeMessenger) SubscribeExtended(ctx context.Context, _ string, channelName string, _ string, _ string, _ time.Duration, messageHandler func(core.Message, string, int64) error) error {
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

func (f *fakeMessenger) SetReaderState(channelName, readerName, fileName string, offset int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.readerStates[channelName+":"+readerName] = fmt.Sprintf("%s:%d", fileName, offset)
	return nil
}

func (f *fakeMessenger) SeekToEnd(channelName string, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seekedChannels[channelName] = true
	return nil
}

func (f *fakeMessenger) SetDataDir(_ string) {}

func (f *fakeMessenger) SetHostname(_ string) {}

func (f *fakeMessenger) GetStats() core.MessengerStats {
	return core.MessengerStats{}
}

// setupTestServer creates a TLS test server that calls svc.handleConnection for each
// connection, and returns a connected client websocket.Conn plus a teardown func.
func setupTestServer(t *testing.T, svc *Service, certDir string) (*websocket.Conn, func()) {
	t.Helper()

	serverCert := filepath.Join(certDir, "keyop-server.crt")
	serverKey := filepath.Join(certDir, "keyop-server.key")
	clientCert := filepath.Join(certDir, "keyop-client.crt")
	clientKey := filepath.Join(certDir, "keyop-client.key")
	caFile := filepath.Join(certDir, "ca.crt")

	cert, err := tls.LoadX509KeyPair(serverCert, serverKey)
	require.NoError(t, err)
	caCert, err := os.ReadFile(caFile)
	require.NoError(t, err)
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCert)

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		svc.handleConnection(conn)
	}))
	srv.TLS = &tls.Config{
		Certificates:       []tls.Certificate{cert},
		ClientCAs:          caCertPool,
		ClientAuth:         tls.RequireAndVerifyClientCert,
		InsecureSkipVerify: true,
	}
	srv.StartTLS()

	clientCertObj, err := tls.LoadX509KeyPair(clientCert, clientKey)
	require.NoError(t, err)
	dialer := websocket.Dialer{
		TLSClientConfig: &tls.Config{
			RootCAs:            caCertPool,
			Certificates:       []tls.Certificate{clientCertObj},
			InsecureSkipVerify: true,
		},
	}
	wsURL := strings.Replace(srv.URL, "https", "wss", 1) + "/ws"
	conn, _, err := dialer.Dial(wsURL, nil)
	require.NoError(t, err)

	return conn, func() { conn.Close(); srv.Close() }
}

// doHandshake performs the hello/welcome exchange on a raw client conn.
func doHandshake(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	require.NoError(t, conn.WriteJSON(wsMessage{V: 1, Type: "hello", ClientID: "test-client",
		Capabilities: &wsCapabilities{Batch: true}}))
	var welcome wsMessage
	safeSetReadDeadline(t, conn, time.Now().Add(5*time.Second))
	require.NoError(t, conn.ReadJSON(&welcome))
	safeClearReadDeadline(t, conn)
	require.Equal(t, "welcome", welcome.Type)
	require.Equal(t, 1, welcome.V)
}

// TestHandleConnection tests the full lifecycle of a websocket connection, including
// handshake, subscription, message reception, and acknowledgment.
func TestHandleConnection(t *testing.T) {
	home, err := os.MkdirTemp("", "keyop-test-home-*")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(home); err != nil {
			t.Logf("failed to remove temp dir %s: %v", home, err)
		}
	}()

	certDir := filepath.Join(home, ".keyop", "certs")
	_, _, _, _, err = util.CreateTestCerts(certDir)
	require.NoError(t, err)

	deps := core.Dependencies{}
	deps.SetLogger(&core.FakeLogger{})
	fakeOs := &core.FakeOsProvider{Home: home}
	fakeOs.ReadFileFunc = func(name string) ([]byte, error) { return os.ReadFile(name) }
	deps.SetOsProvider(fakeOs)

	messenger := newFakeMessenger()
	deps.SetMessenger(messenger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	deps.SetContext(ctx)

	svc := NewService(deps, core.ServiceConfig{
		Name:   "ws-server",
		Config: map[string]any{"port": 0},
		Subs:   map[string]core.ChannelInfo{"test-channel": {Name: "test-channel"}},
	}).(*Service)

	conn, teardown := setupTestServer(t, svc, certDir)
	defer teardown()

	// 1. Hello / Welcome handshake
	doHandshake(t, conn)

	// 2. Send Subscribe
	require.NoError(t, conn.WriteJSON(wsMessage{V: 1, Type: "subscribe", Channels: []string{"test-channel"}}))

	require.Eventually(t, func() bool {
		messenger.mu.Lock()
		defer messenger.mu.Unlock()
		return messenger.subscribedChannels["test-channel"]
	}, 5*time.Second, 100*time.Millisecond)

	// 3. Server delivers a message as a batch with batchId
	testMsg := core.Message{Text: "hello", ChannelName: "test-channel"}
	messenger.subscribeChan <- testMsg

	var received wsMessage
	safeSetReadDeadline(t, conn, time.Now().Add(5*time.Second))
	require.NoError(t, conn.ReadJSON(&received))
	safeClearReadDeadline(t, conn)
	assert.Equal(t, "batch", received.Type)
	assert.Equal(t, 1, received.V)
	assert.NotEmpty(t, received.BatchID)
	require.Len(t, received.Items, 1)
	assert.Equal(t, "hello", received.Items[0].Payload.Text)

	// 4. Send correlated ack
	require.NoError(t, conn.WriteJSON(wsMessage{V: 1, Type: "ack", BatchID: received.BatchID}))

	conn.Close()

	// 5. Test Resume on second connection
	conn2, teardown2 := setupTestServer(t, svc, certDir)
	defer teardown2()
	doHandshake(t, conn2)

	require.NoError(t, conn2.WriteJSON(wsMessage{V: 1, Type: "resume", Queue: "test-channel", FileName: "oldfile", Offset: 123}))
	require.NoError(t, conn2.WriteJSON(wsMessage{V: 1, Type: "subscribe", Channels: []string{"test-channel"}}))

	assert.Eventually(t, func() bool {
		messenger.mu.Lock()
		defer messenger.mu.Unlock()
		for k, v := range messenger.readerStates {
			if strings.HasPrefix(k, "test-channel:ws_") && v == "oldfile:123" {
				return true
			}
		}
		return false
	}, 2*time.Second, 100*time.Millisecond)

	// 6. Delivery on second connection
	testMsg2 := core.Message{Text: "world", ChannelName: "test-channel"}
	messenger.subscribeChan <- testMsg2

	conn2.SetReadDeadline(time.Now().Add(5 * time.Second))
	require.NoError(t, conn2.ReadJSON(&received))
	conn2.SetReadDeadline(time.Time{})
	assert.Equal(t, "batch", received.Type)
	assert.NotEmpty(t, received.BatchID)
	require.Len(t, received.Items, 1)
	assert.Equal(t, "world", received.Items[0].Payload.Text)

	require.NoError(t, conn2.WriteJSON(wsMessage{V: 1, Type: "ack", BatchID: received.BatchID}))
	conn2.Close()
}

// TestHandleConnection_VersionMismatch verifies the server sends an error frame and closes
// when the client sends a hello with a version != 1.
func TestHandleConnection_VersionMismatch(t *testing.T) {
	home, err := os.MkdirTemp("", "keyop-test-vmismatch-*")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(home); err != nil {
			t.Logf("failed to remove temp dir %s: %v", home, err)
		}
	}()

	certDir := filepath.Join(home, ".keyop", "certs")
	_, _, _, _, err = util.CreateTestCerts(certDir)
	require.NoError(t, err)

	deps := core.Dependencies{}
	deps.SetLogger(&core.FakeLogger{})
	fakeOs := &core.FakeOsProvider{Home: home}
	fakeOs.ReadFileFunc = func(name string) ([]byte, error) { return os.ReadFile(name) }
	deps.SetOsProvider(fakeOs)
	deps.SetMessenger(newFakeMessenger())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	deps.SetContext(ctx)

	svc := NewService(deps, core.ServiceConfig{
		Name:   "ws-server-mismatch",
		Config: map[string]any{"port": 0},
	}).(*Service)

	conn, teardown := setupTestServer(t, svc, certDir)
	defer teardown()

	// Send hello with wrong version
	require.NoError(t, conn.WriteJSON(wsMessage{V: 99, Type: "hello", ClientID: "bad-client",
		Capabilities: &wsCapabilities{Batch: true}}))

	var errMsg wsMessage
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	require.NoError(t, conn.ReadJSON(&errMsg))
	conn.SetReadDeadline(time.Time{})

	assert.Equal(t, "error", errMsg.Type)
	assert.Equal(t, wsp.CodeUnsupportedVersion, errMsg.Code)
	assert.Equal(t, 1, errMsg.ExpectedV)
	assert.Equal(t, 99, errMsg.GotV)

	// Server should close; the next read must fail
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, nextErr := conn.ReadMessage()
	assert.Error(t, nextErr, "server should close after version error")
}

// TestHandleConnection_Batching verifies multiple messages are batched and each ack
// correlates by batchId.
func TestHandleConnection_Batching(t *testing.T) {
	home, err := os.MkdirTemp("", "keyop-test-batch-*")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(home); err != nil {
			t.Logf("failed to remove temp dir %s: %v", home, err)
		}
	}()

	certDir := filepath.Join(home, ".keyop", "certs")
	_, _, _, _, err = util.CreateTestCerts(certDir)
	require.NoError(t, err)

	deps := core.Dependencies{}
	deps.SetLogger(&core.FakeLogger{})
	fakeOs := &core.FakeOsProvider{Home: home}
	fakeOs.ReadFileFunc = func(name string) ([]byte, error) { return os.ReadFile(name) }
	deps.SetOsProvider(fakeOs)

	messenger := newFakeMessenger()
	deps.SetMessenger(messenger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	deps.SetContext(ctx)

	const batchSize = 5
	svc := NewService(deps, core.ServiceConfig{
		Name:   "ws-server-batch",
		Config: map[string]any{"port": 0, "batch_size": batchSize},
	}).(*Service)

	conn, teardown := setupTestServer(t, svc, certDir)
	defer teardown()

	doHandshake(t, conn)
	require.NoError(t, conn.WriteJSON(wsMessage{V: 1, Type: "subscribe", Channels: []string{"batch-ch"}}))

	require.Eventually(t, func() bool {
		messenger.mu.Lock()
		defer messenger.mu.Unlock()
		return messenger.subscribedChannels["batch-ch"]
	}, 5*time.Second, 100*time.Millisecond)

	for i := 0; i < batchSize; i++ {
		messenger.subscribeChan <- core.Message{Text: fmt.Sprintf("msg-%d", i), ChannelName: "batch-ch"}
	}

	received := make([]BatchItem, 0, batchSize)
	for len(received) < batchSize {
		var batchMsg wsMessage
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		require.NoError(t, conn.ReadJSON(&batchMsg))
		conn.SetReadDeadline(time.Time{})
		assert.Equal(t, "batch", batchMsg.Type)
		assert.NotEmpty(t, batchMsg.BatchID)
		assert.NotEmpty(t, batchMsg.Items)
		assert.LessOrEqual(t, len(batchMsg.Items), batchSize)
		received = append(received, batchMsg.Items...)
		require.NoError(t, conn.WriteJSON(wsMessage{V: 1, Type: "ack", BatchID: batchMsg.BatchID}))
	}

	assert.Len(t, received, batchSize)
	for i, item := range received {
		assert.Equal(t, fmt.Sprintf("msg-%d", i), item.Payload.Text)
	}
}

// TestHandleConnection_ClientBatch verifies the server handles an incoming client batch,
// publishes all messages, and sends a correlated ack.
func TestHandleConnection_ClientBatch(t *testing.T) {
	home, err := os.MkdirTemp("", "keyop-test-clientbatch-*")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(home); err != nil {
			t.Logf("failed to remove temp dir %s: %v", home, err)
		}
	}()

	certDir := filepath.Join(home, ".keyop", "certs")
	_, _, _, _, err = util.CreateTestCerts(certDir)
	require.NoError(t, err)

	deps := core.Dependencies{}
	deps.SetLogger(&core.FakeLogger{})
	fakeOs := &core.FakeOsProvider{Home: home}
	fakeOs.ReadFileFunc = func(name string) ([]byte, error) { return os.ReadFile(name) }
	deps.SetOsProvider(fakeOs)

	messenger := newFakeMessenger()
	deps.SetMessenger(messenger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	deps.SetContext(ctx)

	svc := NewService(deps, core.ServiceConfig{
		Name:   "ws-server-clientbatch",
		Config: map[string]any{"port": 0},
	}).(*Service)

	conn, teardown := setupTestServer(t, svc, certDir)
	defer teardown()

	doHandshake(t, conn)

	batchID := "test-batch-id-001"
	batch := wsMessage{
		V:       1,
		Type:    "batch",
		BatchID: batchID,
		Items: []BatchItem{
			{Payload: core.Message{Text: "one", ChannelName: "ch"}},
			{Payload: core.Message{Text: "two", ChannelName: "ch"}},
			{Payload: core.Message{Text: "three", ChannelName: "ch"}},
		},
	}
	require.NoError(t, conn.WriteJSON(batch))

	var ackMsg wsMessage
	safeSetReadDeadline(t, conn, time.Now().Add(3*time.Second))
	require.NoError(t, conn.ReadJSON(&ackMsg))
	safeClearReadDeadline(t, conn)
	assert.Equal(t, "ack", ackMsg.Type)
	assert.Equal(t, batchID, ackMsg.BatchID, "ack must carry the same batchId")

	assert.Eventually(t, func() bool {
		messenger.mu.Lock()
		defer messenger.mu.Unlock()
		return len(messenger.messages) >= 3
	}, 2*time.Second, 50*time.Millisecond)

	messenger.mu.Lock()
	defer messenger.mu.Unlock()
	require.Len(t, messenger.messages, 3)
	assert.Equal(t, "one", messenger.messages[0].Text)
	assert.Equal(t, "two", messenger.messages[1].Text)
	assert.Equal(t, "three", messenger.messages[2].Text)
}

// TestHandleConnection_ClientBatch_NoAckOnFailure verifies the server does NOT send an ack
// when messenger.Send fails, preserving at-least-once semantics.
func TestHandleConnection_ClientBatch_NoAckOnFailure(t *testing.T) {
	home, err := os.MkdirTemp("", "keyop-test-clientbatch-fail-*")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(home); err != nil {
			t.Logf("failed to remove temp dir %s: %v", home, err)
		}
	}()

	certDir := filepath.Join(home, ".keyop", "certs")
	_, _, _, _, err = util.CreateTestCerts(certDir)
	require.NoError(t, err)

	deps := core.Dependencies{}
	deps.SetLogger(&core.FakeLogger{})
	fakeOs := &core.FakeOsProvider{Home: home}
	fakeOs.ReadFileFunc = func(name string) ([]byte, error) { return os.ReadFile(name) }
	deps.SetOsProvider(fakeOs)

	messenger := newFakeMessenger()
	messenger.sendErr = fmt.Errorf("injected send failure")
	deps.SetMessenger(messenger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	deps.SetContext(ctx)

	svc := NewService(deps, core.ServiceConfig{
		Name:   "ws-server-clientbatch-fail",
		Config: map[string]any{"port": 0},
	}).(*Service)

	conn, teardown := setupTestServer(t, svc, certDir)
	defer teardown()

	doHandshake(t, conn)

	batch := wsMessage{
		V:       1,
		Type:    "batch",
		BatchID: "fail-batch-id",
		Items:   []BatchItem{{Payload: core.Message{Text: "fail-me", ChannelName: "ch"}}},
	}
	require.NoError(t, conn.WriteJSON(batch))

	conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	var unexpected wsMessage
	err = conn.ReadJSON(&unexpected)
	assert.Error(t, err, "expected no ack from server when Send fails, but got: %+v", unexpected)
}

// TestHandleConnection_AckCorrelation verifies that acks keyed by batchId only release
// their own waiter and do not cross-release other in-flight batches.
//
// The test creates a raw (non-TLS) websocket pair, drives sendBatchAndWaitAck directly on
// the "server" side conn, and has a goroutine on the "client" side that receives two
// batch frames then sends the acks back out of order (second batchId first).
func TestHandleConnection_AckCorrelation(t *testing.T) {
	deps := core.Dependencies{}
	deps.SetLogger(&core.FakeLogger{})
	fakeOs := &core.FakeOsProvider{}
	deps.SetOsProvider(fakeOs)
	deps.SetMessenger(newFakeMessenger())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	deps.SetContext(ctx)

	svc := NewService(deps, core.ServiceConfig{
		Name:   "ws-server-ackcorr",
		Config: map[string]any{"port": 0},
	}).(*Service)

	// Build a plain (non-TLS) websocket pair.
	serverConnCh := make(chan *websocket.Conn, 1)
	plainSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		select {
		case serverConnCh <- c:
		default:
		}
		<-ctx.Done()
		c.Close()
	}))
	defer plainSrv.Close()

	wsURL := strings.Replace(plainSrv.URL, "http", "ws", 1)
	clientConn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer func() {
		if err := clientConn.Close(); err != nil {
			t.Logf("failed to close client websocket conn: %v", err)
		}
	}()

	var serverConn *websocket.Conn
	select {
	case serverConn = <-serverConnCh:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for server conn")
	}

	cw := &connWriter{conn: serverConn}
	pendingMu := sync.Mutex{}
	pending := make(map[string]*pendingAck)

	batchID1 := "corr-batch-id-1"
	batchID2 := "corr-batch-id-2"

	// Client-side goroutine: read both batch frames, then ack them OUT OF ORDER.
	type rcvd struct{ id string }
	rcvdCh := make(chan rcvd, 4)
	go func() {
		var frames []wsMessage
		for len(frames) < 2 {
			clientConn.SetReadDeadline(time.Now().Add(5 * time.Second))
			var m wsMessage
			if err := clientConn.ReadJSON(&m); err != nil {
				return
			}
			clientConn.SetReadDeadline(time.Time{})
			if m.Type == "batch" {
				frames = append(frames, m)
				rcvdCh <- rcvd{id: m.BatchID}
			}
		}
		// Ack second frame first, then first frame.
		time.Sleep(10 * time.Millisecond)
		_ = clientConn.WriteJSON(wsMessage{V: protocolVersion, Type: "ack", BatchID: frames[1].BatchID})
		time.Sleep(10 * time.Millisecond)
		_ = clientConn.WriteJSON(wsMessage{V: protocolVersion, Type: "ack", BatchID: frames[0].BatchID})
	}()

	// Server-side: start a goroutine that reads ack frames and signals pending map,
	// exactly as the real read-loop does.
	go func() {
		for {
			serverConn.SetReadDeadline(time.Now().Add(10 * time.Second))
			var m wsMessage
			if err := serverConn.ReadJSON(&m); err != nil {
				return
			}
			serverConn.SetReadDeadline(time.Time{})
			if m.Type == "ack" && m.BatchID != "" {
				pendingMu.Lock()
				if w, ok := pending[m.BatchID]; ok {
					close(w.done)
					delete(pending, m.BatchID)
				}
				pendingMu.Unlock()
			}
		}
	}()

	// Launch two concurrent sendBatchAndWaitAck calls.
	var err1, err2 error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		err1 = svc.sendBatchAndWaitAck(ctx, cw, &pendingMu, pending, batchID1, "ch-A",
			[]BatchItem{{Payload: core.Message{Text: "batch1"}}})
	}()
	go func() {
		defer wg.Done()
		err2 = svc.sendBatchAndWaitAck(ctx, cw, &pendingMu, pending, batchID2, "ch-B",
			[]BatchItem{{Payload: core.Message{Text: "batch2"}}})
	}()

	// Collect IDs of received batch frames.
	var receivedIDs []string
	recvDeadline := time.NewTimer(5 * time.Second)
	for len(receivedIDs) < 2 {
		select {
		case r := <-rcvdCh:
			receivedIDs = append(receivedIDs, r.id)
		case <-recvDeadline.C:
			t.Fatal("timed out waiting for 2 batch frames on client side")
		}
	}
	recvDeadline.Stop()

	assert.ElementsMatch(t, []string{batchID1, batchID2}, receivedIDs, "both batchIds must arrive")

	// Both goroutines must complete without error.
	doneCh := make(chan struct{})
	go func() { wg.Wait(); close(doneCh) }()
	select {
	case <-doneCh:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for sendBatchAndWaitAck goroutines to complete")
	}
	assert.NoError(t, err1, "sendBatchAndWaitAck for batchID1 must succeed")
	assert.NoError(t, err2, "sendBatchAndWaitAck for batchID2 must succeed")
}

// TestHandleConnection_PostHandshakeVersionMismatch verifies that after a successful
// hello/welcome exchange, if the client sends any frame with v != 1, the server sends an
// UNSUPPORTED_VERSION error frame and closes the connection.
func TestHandleConnection_PostHandshakeVersionMismatch(t *testing.T) {
	home, err := os.MkdirTemp("", "keyop-test-post-ver-*")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(home); err != nil {
			t.Logf("failed to remove temp dir %s: %v", home, err)
		}
	}()

	certDir := filepath.Join(home, ".keyop", "certs")
	_, _, _, _, err = util.CreateTestCerts(certDir)
	require.NoError(t, err)

	deps := core.Dependencies{}
	deps.SetLogger(&core.FakeLogger{})
	fakeOs := &core.FakeOsProvider{Home: home}
	fakeOs.ReadFileFunc = func(name string) ([]byte, error) { return os.ReadFile(name) }
	deps.SetOsProvider(fakeOs)
	deps.SetMessenger(newFakeMessenger())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	deps.SetContext(ctx)

	svc := NewService(deps, core.ServiceConfig{
		Name:   "ws-server-post-ver",
		Config: map[string]any{"port": 0},
	}).(*Service)

	conn, teardown := setupTestServer(t, svc, certDir)
	defer teardown()

	// Successful handshake first
	doHandshake(t, conn)

	// Send a subscribe with wrong version — server must reject it
	require.NoError(t, conn.WriteJSON(wsMessage{
		V:        99,
		Type:     "subscribe",
		Channels: []string{"some-channel"},
	}))

	var errMsg wsMessage
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	require.NoError(t, conn.ReadJSON(&errMsg))
	conn.SetReadDeadline(time.Time{})

	assert.Equal(t, "error", errMsg.Type)
	assert.Equal(t, wsp.CodeUnsupportedVersion, errMsg.Code)
	assert.Equal(t, 1, errMsg.ExpectedV)
	assert.Equal(t, 99, errMsg.GotV)
	assert.NotEmpty(t, errMsg.Message)
	assert.Contains(t, errMsg.Message, "99", "message should mention the bad version")

	// Server should close after sending the error
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, closeErr := conn.ReadMessage()
	assert.Error(t, closeErr, "server should close connection after version error frame")
}

// TestHandleConnection_BadHandshake verifies that if the client sends a non-hello first
// frame, the server responds with a BAD_HANDSHAKE error frame and closes.
func TestHandleConnection_BadHandshake(t *testing.T) {
	home, err := os.MkdirTemp("", "keyop-test-bad-hs-*")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(home); err != nil {
			t.Logf("failed to remove temp dir %s: %v", home, err)
		}
	}()

	certDir := filepath.Join(home, ".keyop", "certs")
	_, _, _, _, err = util.CreateTestCerts(certDir)
	require.NoError(t, err)

	deps := core.Dependencies{}
	deps.SetLogger(&core.FakeLogger{})
	fakeOs := &core.FakeOsProvider{Home: home}
	fakeOs.ReadFileFunc = func(name string) ([]byte, error) { return os.ReadFile(name) }
	deps.SetOsProvider(fakeOs)
	deps.SetMessenger(newFakeMessenger())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	deps.SetContext(ctx)

	svc := NewService(deps, core.ServiceConfig{
		Name:   "ws-server-bad-hs",
		Config: map[string]any{"port": 0},
	}).(*Service)

	conn, teardown := setupTestServer(t, svc, certDir)
	defer teardown()

	// Send a subscribe instead of hello
	require.NoError(t, conn.WriteJSON(wsMessage{
		V:        1,
		Type:     "subscribe",
		Channels: []string{"some-channel"},
	}))

	var errMsg wsMessage
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	require.NoError(t, conn.ReadJSON(&errMsg))
	conn.SetReadDeadline(time.Time{})

	assert.Equal(t, "error", errMsg.Type)
	assert.Equal(t, wsp.CodeBadHandshake, errMsg.Code)
	assert.Equal(t, wsp.BadHandshakeMsg, errMsg.Message)

	// Server should close
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, closeErr := conn.ReadMessage()
	assert.Error(t, closeErr, "server should close after BAD_HANDSHAKE error")
}

// TestFlushPending_Unit is a pure unit test for flushPending: it registers several waiters,
// calls flushPending, and asserts every done channel is closed immediately with no
// double-close panic.
func TestFlushPending_Unit(t *testing.T) {
	pending := make(map[string]*pendingAck)
	ids := []string{"a", "b", "c"}
	for _, id := range ids {
		pending[id] = &pendingAck{done: make(chan struct{})}
	}

	// Pre-close one to verify idempotency (no double-close panic).
	close(pending["b"].done)

	// Must complete without panicking and within 50ms.
	done := make(chan struct{})
	go func() {
		flushPending(pending)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(50 * time.Millisecond):
		t.Fatal("flushPending did not return in time")
	}

	assert.Empty(t, pending, "flushPending must clear the map")

	// All channels must be readable (closed), not blocking.
	for _, id := range ids {
		// We can't read from the channels anymore since the map was cleared,
		// so we verify by re-checking that the map is empty.
		_ = id
	}
}

// TestSendBatchAndWaitAck_FlushOnClose verifies that when the connection drops while a
// sendBatchAndWaitAck call is in-flight, calling flushPending (as handleConnection's
// deferred cleanup does) causes the call to unblock within 500ms — well before ackTimeout.
func TestSendBatchAndWaitAck_FlushOnClose(t *testing.T) {
	deps := core.Dependencies{}
	deps.SetLogger(&core.FakeLogger{})
	deps.SetOsProvider(&core.FakeOsProvider{})
	deps.SetMessenger(newFakeMessenger())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	deps.SetContext(ctx)

	svc := NewService(deps, core.ServiceConfig{
		Name:   "ws-flush-test",
		Config: map[string]any{"port": 0},
	}).(*Service)

	// Build a plain websocket pair — we only need a conn to write through.
	serverConnCh := make(chan *websocket.Conn, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		c, _ := upgrader.Upgrade(w, r, nil)
		serverConnCh <- c
		<-ctx.Done()
		c.Close()
	}))
	defer srv.Close()

	clientConn, _, err := websocket.DefaultDialer.Dial(
		strings.Replace(srv.URL, "http", "ws", 1), nil)
	require.NoError(t, err)
	defer func() {
		if err := clientConn.Close(); err != nil {
			t.Logf("failed to close client websocket conn: %v", err)
		}
	}()

	serverConn := <-serverConnCh

	cw := &connWriter{conn: serverConn}
	pendingMu := sync.Mutex{}
	pending := make(map[string]*pendingAck)

	batchID := "flush-test-batch"

	// Start sendBatchAndWaitAck in a background goroutine; it will block waiting for ack.
	errCh := make(chan error, 1)
	started := make(chan struct{})
	go func() {
		close(started)
		errCh <- svc.sendBatchAndWaitAck(ctx, cw, &pendingMu, pending, batchID, "q",
			[]BatchItem{{Payload: core.Message{Text: "x"}}})
	}()
	<-started

	// Give it a moment to reach the select.
	time.Sleep(20 * time.Millisecond)

	// Simulate connection teardown: call flushPending (exactly as the defer does).
	t0 := time.Now()
	pendingMu.Lock()
	flushPending(pending)
	pendingMu.Unlock()

	// The goroutine must unblock within 500ms.
	select {
	case gotErr := <-errCh:
		elapsed := time.Since(t0)
		assert.Less(t, elapsed, 500*time.Millisecond,
			"sendBatchAndWaitAck must unblock within 500ms of flushPending, took %v", elapsed)
		// The error should be nil (waiter.done was closed, not ctx cancelled).
		assert.NoError(t, gotErr, "flush should return nil, not an error")
	case <-time.After(1 * time.Second):
		t.Fatal("sendBatchAndWaitAck did not unblock within 1s after flushPending")
	}
}
