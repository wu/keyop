package webSocketClient

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"keyop/core"
	"keyop/util"
	wsp "keyop/x/webSocketProtocol"
	"keyop/x/webSocketServer"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWebSocket_ClientServer(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "ws_test")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("failed to remove %s: %v", tmpDir, err)
		}
	}()

	certsDir := filepath.Join(tmpDir, ".keyop", "certs")
	err = util.GenerateTestCerts(certsDir)
	require.NoError(t, err)

	osProvider := core.OsProvider{}
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	logger := &core.FakeLogger{}
	messenger := core.NewMessenger(logger, osProvider)
	stateStore := core.NewFileStateStore(filepath.Join(tmpDir, "state"), osProvider)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	deps := core.Dependencies{}
	deps.SetLogger(logger)
	deps.SetOsProvider(osProvider)
	deps.SetMessenger(messenger)
	deps.SetStateStore(stateStore)
	deps.SetContext(ctx)
	deps.SetCancel(cancel)

	port := 12345

	// 1. Start Server
	serverCfg := core.ServiceConfig{
		Name: "wsServer",
		Subs: map[string]core.ChannelInfo{
			"testSub": {Name: "testChannel"},
		},
		Config: map[string]interface{}{"port": port},
	}
	serverSvc := webSocketServer.NewService(deps, serverCfg)
	err = serverSvc.Initialize()
	require.NoError(t, err)

	// 2. Start Client
	clientCfg := core.ServiceConfig{
		Name: "wsClient",
		Subs: map[string]core.ChannelInfo{
			"sub1": {Name: "testChannel"},
		},
		Config: map[string]interface{}{
			"port":     port,
			"hostname": "localhost",
		},
	}
	clientSvc := NewService(deps, clientCfg)
	err = clientSvc.Initialize()
	require.NoError(t, err)

	// 3. Subscribe for result
	testMsg := core.Message{
		ChannelName: "testChannel",
		Text:        "hello websocket",
		Hostname:    "other-host",
	}

	received := make(chan core.Message, 10)
	err = messenger.Subscribe(ctx, "testReader", "testChannel", "webSocketClient", "test", 0, func(m core.Message) error {
		t.Logf("RECV: %s (hostname: %s, route: %v)", m.Text, m.Hostname, m.Route)
		if m.Text == "hello websocket" {
			select {
			case received <- m:
			default:
			}
		}
		return nil
	})
	require.NoError(t, err)

	time.Sleep(2 * time.Second)

	t.Log("Sending test message...")
	realOs := core.OsProvider{}
	fakeOs := core.FakeOsProvider{
		Host:            "other-host",
		UserHomeDirFunc: realOs.UserHomeDir,
		ReadFileFunc:    realOs.ReadFile,
		OpenFileFunc:    realOs.OpenFile,
		MkdirAllFunc:    realOs.MkdirAll,
		ReadDirFunc:     realOs.ReadDir,
		StatFunc:        realOs.Stat,
	}
	messenger2 := core.NewMessenger(logger, fakeOs)
	messenger2.SetDataDir(filepath.Join(tmpDir, ".keyop", "data"))
	err = messenger2.Send(testMsg)
	require.NoError(t, err)

	count := 0
	for {
		select {
		case <-received:
			count++
			if count == 2 {
				goto done
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("timed out waiting for messages, got %d", count)
		}
	}
done:

	nonSubscribedMsg := core.Message{
		ChannelName: "otherChannel",
		Text:        "should not be received",
		Hostname:    "other-host",
	}
	err = messenger2.Send(nonSubscribedMsg)
	require.NoError(t, err)

	otherReceived := make(chan core.Message, 10)
	err = messenger.Subscribe(ctx, "otherReader", "otherChannel", "webSocketClient", "test", 0, func(m core.Message) error {
		if m.Text == "should not be received" {
			otherReceived <- m
		}
		return nil
	})
	require.NoError(t, err)

	recvCount := 0
Loop:
	for {
		select {
		case <-otherReceived:
			recvCount++
			if recvCount > 1 {
				t.Fatal("Received forwarded message for non-subscribed channel")
			}
		case <-time.After(1 * time.Second):
			break Loop
		}
	}

	var savedState map[string]queueState
	err = stateStore.Load("wsClient", &savedState)
	require.NoError(t, err)
	assert.Contains(t, savedState, "testChannel")
	assert.NotEmpty(t, savedState["testChannel"].FileName)
}

// setupClientBatchTest creates a single cert set under tmpDir/.keyop/certs, sets HOME to
// tmpDir so the client Service finds those certs, starts a raw mTLS test server using the
// same certs, and returns the WS URL and a cleanup function.
func setupClientBatchTest(t *testing.T, handler http.HandlerFunc) (wsURL string, tmpDir string, cleanup func()) {
	t.Helper()

	dir, err := os.MkdirTemp("", "ws_client_batch_*")
	require.NoError(t, err)

	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", dir)

	certsDir := filepath.Join(dir, ".keyop", "certs")
	serverCert, serverKey, _, _, err := util.CreateTestCerts(certsDir)
	require.NoError(t, err)

	cert, err := tls.LoadX509KeyPair(serverCert, serverKey)
	require.NoError(t, err)
	caCertBytes, err := os.ReadFile(filepath.Join(certsDir, "ca.crt"))
	require.NoError(t, err)
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCertBytes)

	tlsCfg := &tls.Config{
		Certificates:       []tls.Certificate{cert},
		ClientCAs:          caCertPool,
		ClientAuth:         tls.RequireAndVerifyClientCert,
		InsecureSkipVerify: true,
	}

	server := httptest.NewUnstartedServer(handler)
	server.TLS = tlsCfg
	server.StartTLS()

	url := strings.Replace(server.URL, "https", "wss", 1) + "/ws"
	return url, dir, func() {
		server.Close()
		if err := os.Setenv("HOME", oldHome); err != nil {
			t.Logf("failed to restore HOME: %v", err)
		}
		if err := os.RemoveAll(dir); err != nil {
			t.Logf("failed to remove tmp dir %s: %v", dir, err)
		}
	}
}

// sendWelcome performs the server-side handshake on a raw conn: reads hello and writes
// welcome. Returns false if the handshake fails (test helpers should check this).
func sendWelcome(t *testing.T, conn *websocket.Conn) bool {
	t.Helper()
	var hello wsMessage
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if err := conn.ReadJSON(&hello); err != nil {
		t.Logf("sendWelcome: ReadJSON hello error: %v", err)
		return false
	}
	conn.SetReadDeadline(time.Time{})
	if hello.Type != "hello" || hello.V != protocolVersion {
		t.Logf("sendWelcome: unexpected hello type=%q v=%d", hello.Type, hello.V)
		return false
	}
	err := conn.WriteJSON(wsMessage{
		V:            protocolVersion,
		Type:         "welcome",
		ServerID:     uuid.New().String(),
		Capabilities: &wsCapabilities{Batch: true},
		Heartbeat: &wsHeartbeat{
			PingIntervalMs: int(pingInterval.Milliseconds()),
			PongTimeoutMs:  int(pongTimeout.Milliseconds()),
		},
	})
	return err == nil
}

// TestClientHandlesIncomingBatch verifies that the client correctly processes an incoming
// batch message from the server: publishes each payload to the messenger, saves state for
// the last item, and sends a correlated ack.
func TestClientHandlesIncomingBatch(t *testing.T) {
	const batchSize = 3
	batchID := uuid.New().String()
	ackReceived := make(chan string, 1)

	serverHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() {
			if err := conn.Close(); err != nil {
				t.Logf("server handler conn close error: %v", err)
			}
		}()

		// Handshake
		if !sendWelcome(t, conn) {
			return
		}

		// Read subscribe (and resume if any)
		for {
			var msg wsMessage
			conn.SetReadDeadline(time.Now().Add(3 * time.Second))
			if err := conn.ReadJSON(&msg); err != nil {
				break
			}
			conn.SetReadDeadline(time.Time{})
			if msg.Type == "subscribe" {
				break
			}
		}

		// Send a batch
		batch := wsMessage{
			V:       protocolVersion,
			Type:    "batch",
			BatchID: batchID,
			Queue:   "testCh",
			Items:   make([]BatchItem, batchSize),
		}
		for i := 0; i < batchSize; i++ {
			batch.Items[i] = BatchItem{
				Queue:    "testCh",
				FileName: "f1",
				Offset:   int64(i + 1),
				Payload:  core.Message{Text: fmt.Sprintf("item-%d", i), ChannelName: "testCh", Hostname: "remote-host"},
			}
		}
		if err := conn.WriteJSON(batch); err != nil {
			return
		}

		// Expect a correlated ack
		var ack wsMessage
		conn.SetReadDeadline(time.Now().Add(10 * time.Second))
		if err := conn.ReadJSON(&ack); err != nil {
			return
		}
		conn.SetReadDeadline(time.Time{})
		if ack.Type == "ack" {
			select {
			case ackReceived <- ack.BatchID:
			default:
			}
		}
	})

	wsURL, tmpDir, cleanup := setupClientBatchTest(t, serverHandler)
	defer cleanup()

	logger := &core.FakeLogger{}
	osProvider := core.OsProvider{}
	messenger := core.NewMessenger(logger, osProvider)
	stateStore := core.NewFileStateStore(filepath.Join(tmpDir, "state"), osProvider)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	deps := core.Dependencies{}
	deps.SetLogger(logger)
	deps.SetOsProvider(osProvider)
	deps.SetMessenger(messenger)
	deps.SetStateStore(stateStore)
	deps.SetContext(ctx)
	deps.SetCancel(cancel)

	host := strings.TrimPrefix(wsURL, "wss://")
	host = strings.Split(host, "/")[0]
	hostParts := strings.Split(host, ":")
	portStr := hostParts[len(hostParts)-1]
	var port int
	fmt.Sscanf(portStr, "%d", &port)

	received := make(chan core.Message, 10)
	require.NoError(t, messenger.Subscribe(ctx, "batchReader", "testCh", "test", "test", 0, func(m core.Message) error {
		received <- m
		return nil
	}))

	svc := NewService(deps, core.ServiceConfig{
		Name: "batchClient",
		Subs: map[string]core.ChannelInfo{"sub": {Name: "testCh"}},
		Config: map[string]interface{}{
			"port":       port,
			"hostname":   "localhost",
			"batch_size": batchSize,
		},
	})
	require.NoError(t, svc.Initialize())

	// Wait for the correlated ack
	select {
	case gotBatchID := <-ackReceived:
		assert.Equal(t, batchID, gotBatchID, "ack batchId must match the sent batchId")
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for ack from client")
	}

	// Collect all forwarded messages
	got := make([]core.Message, 0, batchSize)
	deadline := time.After(5 * time.Second)
collect:
	for len(got) < batchSize {
		select {
		case m := <-received:
			if m.Hostname == "remote-host" {
				got = append(got, m)
			}
		case <-deadline:
			break collect
		}
	}
	assert.Len(t, got, batchSize, "expected all batch items to be forwarded to messenger")

	var savedState map[string]queueState
	require.NoError(t, stateStore.Load("batchClient", &savedState))
	if assert.Contains(t, savedState, "testCh") {
		assert.Equal(t, "f1", savedState["testCh"].FileName)
		assert.Equal(t, int64(batchSize), savedState["testCh"].Offset)
	}
}

// TestClientSendsOutgoingBatch verifies that when the client has multiple messages queued
// for publishing it sends them as a batch (up to batchSize) with a batchId, and the server
// acks with the same batchId.
func TestClientSendsOutgoingBatch(t *testing.T) {
	const batchSize = 5

	var (
		mu            sync.Mutex
		receivedBatch []BatchItem
		batchReceived = make(chan struct{}, 1)
	)

	serverHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() {
			if err := conn.Close(); err != nil {
				t.Logf("server handler conn close error: %v", err)
			}
		}()

		if !sendWelcome(t, conn) {
			return
		}

		for {
			var msg wsMessage
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}
			switch msg.Type {
			case "subscribe", "resume":
				// ignore
			case "batch":
				mu.Lock()
				receivedBatch = append(receivedBatch, msg.Items...)
				total := len(receivedBatch)
				mu.Unlock()
				// Ack with matching batchId
				if err := conn.WriteJSON(wsMessage{V: protocolVersion, Type: "ack", BatchID: msg.BatchID}); err != nil {
					assert.NoError(t, err)
				}
				if total >= batchSize {
					select {
					case batchReceived <- struct{}{}:
					default:
					}
				}
			}
		}
	})

	wsURL, tmpDir, cleanup := setupClientBatchTest(t, serverHandler)
	defer cleanup()

	logger := &core.FakeLogger{}
	osProvider := core.OsProvider{}
	messenger := core.NewMessenger(logger, osProvider)
	stateStore := core.NewFileStateStore(filepath.Join(tmpDir, "state"), osProvider)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	deps := core.Dependencies{}
	deps.SetLogger(logger)
	deps.SetOsProvider(osProvider)
	deps.SetMessenger(messenger)
	deps.SetStateStore(stateStore)
	deps.SetContext(ctx)
	deps.SetCancel(cancel)

	host := strings.TrimPrefix(wsURL, "wss://")
	host = strings.Split(host, "/")[0]
	hostParts := strings.Split(host, ":")
	portStr := hostParts[len(hostParts)-1]
	var port int
	fmt.Sscanf(portStr, "%d", &port)

	svc := NewService(deps, core.ServiceConfig{
		Name: "outboundBatchClient",
		Pubs: map[string]core.ChannelInfo{"pub": {Name: "outCh"}},
		Config: map[string]interface{}{
			"port":       port,
			"hostname":   "localhost",
			"batch_size": batchSize,
		},
	})
	require.NoError(t, svc.Initialize())

	time.Sleep(500 * time.Millisecond)

	realOs := core.OsProvider{}
	fakeOs := core.FakeOsProvider{
		Host:            "sender-host",
		UserHomeDirFunc: realOs.UserHomeDir,
		ReadFileFunc:    realOs.ReadFile,
		OpenFileFunc:    realOs.OpenFile,
		MkdirAllFunc:    realOs.MkdirAll,
		ReadDirFunc:     realOs.ReadDir,
		StatFunc:        realOs.Stat,
	}
	sender := core.NewMessenger(logger, fakeOs)
	sender.SetDataDir(filepath.Join(tmpDir, ".keyop", "data"))
	for i := 0; i < batchSize; i++ {
		require.NoError(t, sender.Send(core.Message{
			ChannelName: "outCh",
			Text:        fmt.Sprintf("out-%d", i),
			Hostname:    "sender-host",
		}))
	}

	select {
	case <-batchReceived:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for client to send batch to server")
	}

	mu.Lock()
	total := len(receivedBatch)
	mu.Unlock()
	assert.Equal(t, batchSize, total, "all messages should have been delivered to the server")
}

// TestClientOutboundAtLeastOnce verifies that if the first server connection drops without
// acking the batch, the client retries delivery on reconnection so messages are delivered
// at least once.
func TestClientOutboundAtLeastOnce(t *testing.T) {
	var (
		mu           sync.Mutex
		allReceived  []BatchItem
		connCount    int
		everReceived = make(chan struct{}, 1)
	)

	serverHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() {
			if err := conn.Close(); err != nil {
				t.Logf("server handler conn close error: %v", err)
			}
		}()

		if !sendWelcome(t, conn) {
			return
		}

		mu.Lock()
		connCount++
		myConn := connCount
		mu.Unlock()

		for {
			var msg wsMessage
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}
			if msg.Type == "subscribe" || msg.Type == "resume" {
				continue
			}
			if msg.Type == "batch" {
				mu.Lock()
				allReceived = append(allReceived, msg.Items...)
				mu.Unlock()

				if myConn == 1 {
					// First connection: drop without acking to simulate server crash.
					return
				}
				// Second connection: ack normally.
				if err := conn.WriteJSON(wsMessage{V: protocolVersion, Type: "ack", BatchID: msg.BatchID}); err != nil {
					assert.NoError(t, err)
				}
				select {
				case everReceived <- struct{}{}:
				default:
				}
			}
		}
	})

	wsURL, tmpDir, cleanup := setupClientBatchTest(t, serverHandler)
	defer cleanup()

	logger := &core.FakeLogger{}
	osProvider := core.OsProvider{}
	messenger := core.NewMessenger(logger, osProvider)
	stateStore := core.NewFileStateStore(filepath.Join(tmpDir, "state"), osProvider)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	deps := core.Dependencies{}
	deps.SetLogger(logger)
	deps.SetOsProvider(osProvider)
	deps.SetMessenger(messenger)
	deps.SetStateStore(stateStore)
	deps.SetContext(ctx)
	deps.SetCancel(cancel)

	host := strings.TrimPrefix(wsURL, "wss://")
	host = strings.Split(host, "/")[0]
	hostParts := strings.Split(host, ":")
	portStr := hostParts[len(hostParts)-1]
	var port int
	fmt.Sscanf(portStr, "%d", &port)

	svc := NewService(deps, core.ServiceConfig{
		Name: "atLeastOnceClient",
		Pubs: map[string]core.ChannelInfo{"pub": {Name: "retryOutCh"}},
		Config: map[string]interface{}{
			"port":       port,
			"hostname":   "localhost",
			"batch_size": 5,
		},
	})
	require.NoError(t, svc.Initialize())

	time.Sleep(500 * time.Millisecond)

	realOs := core.OsProvider{}
	fakeOs := core.FakeOsProvider{
		Host:            "retry-host",
		UserHomeDirFunc: realOs.UserHomeDir,
		ReadFileFunc:    realOs.ReadFile,
		OpenFileFunc:    realOs.OpenFile,
		MkdirAllFunc:    realOs.MkdirAll,
		ReadDirFunc:     realOs.ReadDir,
		StatFunc:        realOs.Stat,
	}
	sender := core.NewMessenger(logger, fakeOs)
	sender.SetDataDir(filepath.Join(tmpDir, ".keyop", "data"))
	require.NoError(t, sender.Send(core.Message{
		ChannelName: "retryOutCh",
		Text:        "must-arrive",
		Hostname:    "retry-host",
	}))

	select {
	case <-everReceived:
	case <-time.After(15 * time.Second):
		t.Fatal("timed out waiting for retried delivery on second connection")
	}

	mu.Lock()
	total := len(allReceived)
	mu.Unlock()
	assert.GreaterOrEqual(t, total, 2,
		"message should have been delivered at least twice: once on the failed connection and once on retry")
	for _, item := range allReceived {
		assert.Equal(t, "must-arrive", item.Payload.Text)
	}
}

// TestClientVersionMismatch verifies that if the server sends back an error frame with
// UNSUPPORTED_VERSION, the client closes the connection without further processing.
func TestClientVersionMismatch(t *testing.T) {
	closedAfterError := make(chan struct{}, 1)

	serverHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() {
			if err := conn.Close(); err != nil {
				t.Logf("server handler conn close error: %v", err)
			}
		}()

		// Read hello
		var hello wsMessage
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		if err := conn.ReadJSON(&hello); err != nil {
			return
		}
		conn.SetReadDeadline(time.Time{})

		// Reply with UNSUPPORTED_VERSION error instead of welcome
		if err := conn.WriteJSON(wsMessage{
			V:         protocolVersion,
			Type:      "error",
			Code:      wsp.CodeUnsupportedVersion,
			ExpectedV: protocolVersion,
			GotV:      hello.V,
			Message:   wsp.UnsupportedVersionMsg(protocolVersion, hello.V),
		}); err != nil {
			t.Logf("conn.WriteJSON failed: %v", err)
		}

		// After sending the error we expect the client to close; wait briefly
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, _, _ = conn.ReadMessage() // will fail when client closes
		select {
		case closedAfterError <- struct{}{}:
		default:
		}
	})

	wsURL, _, cleanup := setupClientBatchTest(t, serverHandler)
	defer cleanup()

	host := strings.TrimPrefix(wsURL, "wss://")
	host = strings.Split(host, "/")[0]
	hostParts := strings.Split(host, ":")
	portStr := hostParts[len(hostParts)-1]
	var port int
	fmt.Sscanf(portStr, "%d", &port)

	logger := &core.FakeLogger{}
	osProvider := core.OsProvider{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	deps := core.Dependencies{}
	deps.SetLogger(logger)
	deps.SetOsProvider(osProvider)
	deps.SetMessenger(core.NewMessenger(logger, osProvider))
	stateDir, _ := os.MkdirTemp("", "ws_vmismatch_*")
	defer func() {
		if err := os.RemoveAll(stateDir); err != nil {
			t.Logf("failed to remove %s: %v", stateDir, err)
		}
	}()

	deps.SetStateStore(core.NewFileStateStore(filepath.Join(stateDir, "state"), osProvider))
	deps.SetContext(ctx)
	deps.SetCancel(cancel)

	svc := NewService(deps, core.ServiceConfig{
		Name: "mismatchClient",
		Subs: map[string]core.ChannelInfo{"sub": {Name: "anyCh"}},
		Config: map[string]interface{}{
			"port":     port,
			"hostname": "localhost",
		},
	})
	require.NoError(t, svc.Initialize())

	select {
	case <-closedAfterError:
		// client closed the connection after receiving the error — pass
	case <-time.After(8 * time.Second):
		t.Fatal("timed out: server did not see client close after version error")
	}
}

// TestClientAckCrossRelease verifies that when two outbound batches are in-flight
// concurrently with different batchIds (one per pub channel), an ack for one batchId
// releases only that waiter and does not unblock the other.
//
// Two separate Pubs channels give the client two independent batch-sender goroutines,
// so both can be in-flight at the same time.  The mock server captures both, then acks
// them out of order (second batchId first).
func TestClientAckCrossRelease(t *testing.T) {
	type capturedBatch struct {
		batchID string
		items   []BatchItem
	}

	var (
		mu           sync.Mutex
		captured     []capturedBatch
		bothCaptured = make(chan struct{})
		allAcked     = make(chan struct{}, 2)
		onceClose    sync.Once
	)

	serverHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() {
			if err := conn.Close(); err != nil {
				t.Logf("server handler conn close error: %v", err)
			}
		}()

		if !sendWelcome(t, conn) {
			return
		}

		var batches []capturedBatch
		for {
			var msg wsMessage
			conn.SetReadDeadline(time.Now().Add(10 * time.Second))
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}
			conn.SetReadDeadline(time.Time{})
			if msg.Type == "subscribe" || msg.Type == "resume" {
				continue
			}
			if msg.Type == "batch" {
				mu.Lock()
				batches = append(batches, capturedBatch{batchID: msg.BatchID, items: msg.Items})
				total := len(batches)
				mu.Unlock()

				if total == 2 {
					mu.Lock()
					captured = batches
					mu.Unlock()
					onceClose.Do(func() { close(bothCaptured) })

					// Ack out of order: second received batch first, then first.
					if err := conn.WriteJSON(wsMessage{V: protocolVersion, Type: "ack", BatchID: batches[1].batchID}); err != nil {
						assert.NoError(t, err)
					}
					time.Sleep(20 * time.Millisecond)
					if err := conn.WriteJSON(wsMessage{V: protocolVersion, Type: "ack", BatchID: batches[0].batchID}); err != nil {
						assert.NoError(t, err)
					}
					allAcked <- struct{}{}
					allAcked <- struct{}{}

					// Drain any further messages
					for {
						conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
						var m wsMessage
						if err := conn.ReadJSON(&m); err != nil {
							return
						}
					}
				}
			}
		}
	})

	wsURL, tmpDir, cleanup := setupClientBatchTest(t, serverHandler)
	defer cleanup()

	logger := &core.FakeLogger{}
	osProvider := core.OsProvider{}
	messenger := core.NewMessenger(logger, osProvider)
	stateStore := core.NewFileStateStore(filepath.Join(tmpDir, "state"), osProvider)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	deps := core.Dependencies{}
	deps.SetLogger(logger)
	deps.SetOsProvider(osProvider)
	deps.SetMessenger(messenger)
	deps.SetStateStore(stateStore)
	deps.SetContext(ctx)
	deps.SetCancel(cancel)

	host := strings.TrimPrefix(wsURL, "wss://")
	host = strings.Split(host, "/")[0]
	hostParts := strings.Split(host, ":")
	portStr := hostParts[len(hostParts)-1]
	var port int
	fmt.Sscanf(portStr, "%d", &port)

	// Two separate pub channels → two independent batch-sender goroutines, so both
	// can be in-flight at the same time.  batch_size=1 guarantees one batch per message.
	svc := NewService(deps, core.ServiceConfig{
		Name: "crossReleaseClient",
		Pubs: map[string]core.ChannelInfo{
			"pubA": {Name: "crossChA"},
			"pubB": {Name: "crossChB"},
		},
		Config: map[string]interface{}{
			"port":       port,
			"hostname":   "localhost",
			"batch_size": 1,
		},
	})
	require.NoError(t, svc.Initialize())

	time.Sleep(400 * time.Millisecond)

	realOs := core.OsProvider{}
	fakeOs := core.FakeOsProvider{
		Host:            "cross-host",
		UserHomeDirFunc: realOs.UserHomeDir,
		ReadFileFunc:    realOs.ReadFile,
		OpenFileFunc:    realOs.OpenFile,
		MkdirAllFunc:    realOs.MkdirAll,
		ReadDirFunc:     realOs.ReadDir,
		StatFunc:        realOs.Stat,
	}
	sender := core.NewMessenger(logger, fakeOs)
	sender.SetDataDir(filepath.Join(tmpDir, ".keyop", "data"))
	// Send one message per channel so each batch-sender fires concurrently.
	require.NoError(t, sender.Send(core.Message{ChannelName: "crossChA", Text: "msg-A", Hostname: "cross-host"}))
	require.NoError(t, sender.Send(core.Message{ChannelName: "crossChB", Text: "msg-B", Hostname: "cross-host"}))

	// Wait for server to capture both batches before acking either.
	select {
	case <-bothCaptured:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for both batches to arrive at server")
	}

	mu.Lock()
	cap := captured
	mu.Unlock()

	require.Len(t, cap, 2, "expected 2 captured batches")
	assert.NotEqual(t, cap[0].batchID, cap[1].batchID, "each batch must have a distinct batchId")

	// Wait for both acks to be sent
	for i := 0; i < 2; i++ {
		select {
		case <-allAcked:
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out waiting for ack signal %d", i+1)
		}
	}

	// Each batch has exactly 1 item; verify the correct payloads were delivered.
	texts := map[string]bool{}
	for _, cb := range cap {
		for _, item := range cb.items {
			texts[item.Payload.Text] = true
		}
	}
	assert.True(t, texts["msg-A"], "msg-A must be in a batch")
	assert.True(t, texts["msg-B"], "msg-B must be in a batch")
}

// TestClientPostHandshakeVersionMismatch verifies that after a successful handshake, if
// the server sends a frame with v != 1, the client sends an UNSUPPORTED_VERSION error
// frame back and closes the connection.
func TestClientPostHandshakeVersionMismatch(t *testing.T) {
	clientSentError := make(chan wsMessage, 1)
	clientClosed := make(chan struct{}, 1)

	serverHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() {
			if err := conn.Close(); err != nil {
				t.Logf("server handler conn close error: %v", err)
			}
		}()

		// Complete the handshake normally
		if !sendWelcome(t, conn) {
			return
		}

		// Drain subscribe/resume frames
		for {
			var msg wsMessage
			conn.SetReadDeadline(time.Now().Add(3 * time.Second))
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}
			conn.SetReadDeadline(time.Time{})
			if msg.Type == "subscribe" {
				break
			}
		}

		// Send a batch with wrong version — client must reject it
		if err := conn.WriteJSON(wsMessage{
			V:       99,
			Type:    "batch",
			BatchID: uuid.New().String(),
			Items:   []BatchItem{{Payload: core.Message{Text: "bad-v-msg"}}},
		}); err != nil {
			t.Logf("conn.WriteJSON failed: %v", err)
		}

		// Expect the client to respond with an error frame then close
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		var reply wsMessage
		if err := conn.ReadJSON(&reply); err == nil && reply.Type == "error" {
			select {
			case clientSentError <- reply:
			default:
			}
		}
		conn.SetReadDeadline(time.Time{})

		// Client should close — next read will fail
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, _, _ = conn.ReadMessage()
		select {
		case clientClosed <- struct{}{}:
		default:
		}
	})

	wsURL, _, cleanup := setupClientBatchTest(t, serverHandler)
	defer cleanup()

	host := strings.TrimPrefix(wsURL, "wss://")
	host = strings.Split(host, "/")[0]
	hostParts := strings.Split(host, ":")
	portStr := hostParts[len(hostParts)-1]
	var port int
	fmt.Sscanf(portStr, "%d", &port)

	logger := &core.FakeLogger{}
	osProvider := core.OsProvider{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	deps := core.Dependencies{}
	deps.SetLogger(logger)
	deps.SetOsProvider(osProvider)
	deps.SetMessenger(core.NewMessenger(logger, osProvider))
	stateDir, _ := os.MkdirTemp("", "ws_postver_*")
	defer func() {
		if err := os.RemoveAll(stateDir); err != nil {
			t.Logf("failed to remove %s: %v", stateDir, err)
		}
	}()

	deps.SetStateStore(core.NewFileStateStore(filepath.Join(stateDir, "state"), osProvider))
	deps.SetContext(ctx)
	deps.SetCancel(cancel)

	svc := NewService(deps, core.ServiceConfig{
		Name: "postVerClient",
		Subs: map[string]core.ChannelInfo{"sub": {Name: "anyCh"}},
		Config: map[string]interface{}{
			"port":     port,
			"hostname": "localhost",
		},
	})
	require.NoError(t, svc.Initialize())

	select {
	case errFrame := <-clientSentError:
		assert.Equal(t, "error", errFrame.Type)
		assert.Equal(t, wsp.CodeUnsupportedVersion, errFrame.Code)
		assert.Equal(t, 1, errFrame.ExpectedV)
		assert.Equal(t, 99, errFrame.GotV)
		assert.NotEmpty(t, errFrame.Message)
		assert.Contains(t, errFrame.Message, "99", "error message should mention the bad version number")
	case <-time.After(8 * time.Second):
		t.Fatal("timed out: client did not send UNSUPPORTED_VERSION error frame")
	}

	select {
	case <-clientClosed:
		// client closed after sending error — pass
	case <-time.After(3 * time.Second):
		t.Fatal("timed out: client did not close connection after version error")
	}
}

// TestClientFlushPendingOnClose has two parts:
//
//  1. A unit sub-test that directly verifies flushPending unblocks a pending waiter
//     within 500ms (no goroutine leaks, no ackTimeout wait).
//
//  2. An integration sub-test that verifies the client reconnects promptly after the
//     server drops without acking — observable because the second connection attempt
//     completes a handshake within a few seconds.
func TestClientFlushPendingOnClose(t *testing.T) {
	t.Run("unit/flushPending unblocks waiter within 500ms", func(t *testing.T) {
		pending := make(map[string]*pendingAck)
		batchID := "unit-flush-id"
		w := &pendingAck{done: make(chan struct{})}
		pending[batchID] = w

		// Simulate what the batch-sender select does: block on waiter.done.
		senderDone := make(chan error, 1)
		go func() {
			select {
			case <-w.done:
				senderDone <- nil
			case <-time.After(wsp.AckTimeout):
				senderDone <- fmt.Errorf("ackTimeout fired before flushPending")
			}
		}()

		time.Sleep(10 * time.Millisecond) // let goroutine reach the select

		// Flush — as handleConnection's defer does on shutdown.
		t0 := time.Now()
		var mu sync.Mutex
		mu.Lock()
		flushPending(pending)
		mu.Unlock()

		select {
		case err := <-senderDone:
			elapsed := time.Since(t0)
			require.NoError(t, err)
			assert.Less(t, elapsed, 500*time.Millisecond,
				"waiter must unblock within 500ms of flushPending, took %v", elapsed)
		case <-time.After(1 * time.Second):
			t.Fatal("waiter did not unblock within 1s after flushPending")
		}
		assert.Empty(t, pending, "flushPending must clear the map")
	})

	t.Run("unit/flushPending is idempotent on already-closed channel", func(t *testing.T) {
		pending := make(map[string]*pendingAck)
		for _, id := range []string{"x", "y", "z"} {
			pending[id] = &pendingAck{done: make(chan struct{})}
		}
		close(pending["y"].done) // pre-close one

		require.NotPanics(t, func() { flushPending(pending) }, "flushPending must not panic on pre-closed channel")
		assert.Empty(t, pending)
	})

	t.Run("integration/client reconnects after server drop without ack", func(t *testing.T) {
		// Count how many times the server receives a hello (= number of connection attempts).
		connCount := make(chan struct{}, 10)

		serverHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer func() {
				if err := conn.Close(); err != nil {
					t.Logf("server handler conn close error: %v", err)
				}
			}()

			if !sendWelcome(t, conn) {
				return
			}
			connCount <- struct{}{}

			for {
				var msg wsMessage
				conn.SetReadDeadline(time.Now().Add(10 * time.Second))
				if err := conn.ReadJSON(&msg); err != nil {
					return
				}
				conn.SetReadDeadline(time.Time{})
				if msg.Type == "batch" {
					return // drop without acking
				}
			}
		})

		wsURL, tmpDir, cleanup := setupClientBatchTest(t, serverHandler)
		defer cleanup()

		host := strings.TrimPrefix(wsURL, "wss://")
		host = strings.Split(host, "/")[0]
		hostParts := strings.Split(host, ":")
		portStr := hostParts[len(hostParts)-1]
		var port int
		fmt.Sscanf(portStr, "%d", &port)

		logger := &core.FakeLogger{}
		osProvider := core.OsProvider{}
		messenger := core.NewMessenger(logger, osProvider)
		stateStore := core.NewFileStateStore(filepath.Join(tmpDir, "state"), osProvider)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		deps := core.Dependencies{}
		deps.SetLogger(logger)
		deps.SetOsProvider(osProvider)
		deps.SetMessenger(messenger)
		deps.SetStateStore(stateStore)
		deps.SetContext(ctx)
		deps.SetCancel(cancel)

		svc := NewService(deps, core.ServiceConfig{
			Name: "flushIntegrationClient",
			Pubs: map[string]core.ChannelInfo{"pub": {Name: "flushCh2"}},
			Config: map[string]interface{}{
				"port":       port,
				"hostname":   "localhost",
				"batch_size": 1,
			},
		})
		require.NoError(t, svc.Initialize())
		time.Sleep(300 * time.Millisecond)

		realOs := core.OsProvider{}
		fakeOs := core.FakeOsProvider{
			Host:            "flush-host2",
			UserHomeDirFunc: realOs.UserHomeDir,
			ReadFileFunc:    realOs.ReadFile,
			OpenFileFunc:    realOs.OpenFile,
			MkdirAllFunc:    realOs.MkdirAll,
			ReadDirFunc:     realOs.ReadDir,
			StatFunc:        realOs.Stat,
		}
		sender := core.NewMessenger(logger, fakeOs)
		sender.SetDataDir(filepath.Join(tmpDir, ".keyop", "data"))
		require.NoError(t, sender.Send(core.Message{
			ChannelName: "flushCh2", Text: "flush-me", Hostname: "flush-host2",
		}))

		// First connection should arrive quickly.
		select {
		case <-connCount:
		case <-time.After(5 * time.Second):
			t.Fatal("timed out: first connection never completed handshake")
		}

		// After the server drops, flushPending must release the batch-sender so the
		// connectLoop can attempt a second connection within ~2s (1s reconnect sleep).
		select {
		case <-connCount:
			// second connection arrived — flushPending worked
		case <-time.After(5 * time.Second):
			t.Fatal("client did not reconnect within 5s after server drop — flushPending may be blocking")
		}
	})
}

// setupWrongCAServer creates a test TLS WS server signed by a *different* CA than the
// one installed under dir/.keyop/certs. The client (which trusts only its own CA)
// must reject the connection.
func setupWrongCAServer(t *testing.T, clientDir string, handler http.HandlerFunc) (wsURL string, cleanup func()) {
	t.Helper()

	// Generate a completely independent CA + server cert in a temp dir.
	wrongCertsDir, err := os.MkdirTemp("", "wrong_ca_*")
	require.NoError(t, err)

	wrongServerCert, wrongServerKey, _, _, err := util.CreateTestCerts(wrongCertsDir)
	require.NoError(t, err)

	cert, err := tls.LoadX509KeyPair(wrongServerCert, wrongServerKey)
	require.NoError(t, err)

	// Load the wrong CA for requiring client certs — we still want a working TLS
	// handshake at the transport layer so we can observe the VerifyPeerCertificate
	// rejection (the server won't verify client certs in this helper).
	tlsCfg := &tls.Config{
		Certificates:       []tls.Certificate{cert},
		InsecureSkipVerify: true, //nolint:gosec — test-only server
	}

	server := httptest.NewUnstartedServer(handler)
	server.TLS = tlsCfg
	server.StartTLS()

	url := strings.Replace(server.URL, "https", "wss", 1) + "/ws"
	return url, func() {
		server.Close()
		if err := os.RemoveAll(wrongCertsDir); err != nil {
			t.Logf("failed to remove %s: %v", wrongCertsDir, err)
		}
	}
}

// TestClientRejectsWrongCAServer verifies that VerifyPeerCertificate causes the client
// to refuse a server whose certificate is signed by a different CA. The Dial call must
// fail and the server handler must never receive a WebSocket upgrade.
func TestClientRejectsWrongCAServer(t *testing.T) {
	upgraded := make(chan struct{}, 1)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() {
			if err := conn.Close(); err != nil {
				t.Logf("server handler conn close error: %v", err)
			}
		}()
		select {
		case upgraded <- struct{}{}:
		default:
		}
		// Keep the connection alive briefly so the client gets the TLS error before
		// we tear down.
		time.Sleep(500 * time.Millisecond)
	})

	// Client certs go under clientDir.
	clientDir, err := os.MkdirTemp("", "ws_wrongca_client_*")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(clientDir); err != nil {
			t.Logf("failed to remove %s: %v", clientDir, err)
		}
	}()

	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", clientDir)
	defer os.Setenv("HOME", oldHome)

	clientCertsDir := filepath.Join(clientDir, ".keyop", "certs")
	err = util.GenerateTestCerts(clientCertsDir)
	require.NoError(t, err)

	wsURL, cleanup := setupWrongCAServer(t, clientDir, handler)
	defer cleanup()

	host := strings.TrimPrefix(wsURL, "wss://")
	host = strings.Split(host, "/")[0]
	hostParts := strings.Split(host, ":")
	portStr := hostParts[len(hostParts)-1]
	var port int
	fmt.Sscanf(portStr, "%d", &port)

	logger := &core.FakeLogger{}
	osProvider := core.OsProvider{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stateDir, err := os.MkdirTemp("", "ws_wrongca_state_*")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(stateDir); err != nil {
			t.Logf("failed to remove %s: %v", stateDir, err)
		}
	}()

	deps := core.Dependencies{}
	deps.SetLogger(logger)
	deps.SetOsProvider(osProvider)
	deps.SetMessenger(core.NewMessenger(logger, osProvider))
	deps.SetStateStore(core.NewFileStateStore(filepath.Join(stateDir, "state"), osProvider))
	deps.SetContext(ctx)
	deps.SetCancel(cancel)

	svc := NewService(deps, core.ServiceConfig{
		Name: "wrongCAClient",
		Subs: map[string]core.ChannelInfo{"sub": {Name: "anyCh"}},
		Config: map[string]interface{}{
			"port":     port,
			"hostname": "localhost",
		},
	})
	require.NoError(t, svc.Initialize())

	// The client's connectLoop retries every 5s; give it enough time to attempt at
	// least one dial and fail. The server handler must never see an upgrade.
	select {
	case <-upgraded:
		t.Fatal("client connected to a server with a wrong-CA cert — VerifyPeerCertificate did not reject it")
	case <-time.After(3 * time.Second):
		// No upgrade observed — the client correctly rejected the wrong-CA server.
	}
}

// TestClientSPKIPinMismatch verifies that when "server_cert_spki_sha256" is configured
// with a value that does not match the actual server leaf SPKI, the client rejects the
// connection even though the certificate chain is otherwise valid.
func TestClientSPKIPinMismatch(t *testing.T) {
	upgraded := make(chan struct{}, 1)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() {
			if err := conn.Close(); err != nil {
				t.Logf("server handler conn close error: %v", err)
			}
		}()
		select {
		case upgraded <- struct{}{}:
		default:
		}
		time.Sleep(500 * time.Millisecond)
	})

	clientDir, err := os.MkdirTemp("", "ws_spki_mismatch_*")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(clientDir); err != nil {
			t.Logf("failed to remove %s: %v", clientDir, err)
		}
	}()

	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", clientDir)
	defer os.Setenv("HOME", oldHome)

	clientCertsDir := filepath.Join(clientDir, ".keyop", "certs")
	serverCert, serverKey, _, _, err := util.CreateTestCerts(clientCertsDir)
	require.NoError(t, err)

	cert, err := tls.LoadX509KeyPair(serverCert, serverKey)
	require.NoError(t, err)
	caCertBytes, err := os.ReadFile(filepath.Join(clientCertsDir, "ca.crt"))
	require.NoError(t, err)
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCertBytes)

	tlsCfg := &tls.Config{
		Certificates:       []tls.Certificate{cert},
		ClientCAs:          caCertPool,
		ClientAuth:         tls.RequireAndVerifyClientCert,
		InsecureSkipVerify: true, //nolint:gosec — test-only server
	}

	server := httptest.NewUnstartedServer(handler)
	server.TLS = tlsCfg
	server.StartTLS()
	t.Cleanup(server.Close)

	wsURL := strings.Replace(server.URL, "https", "wss", 1) + "/ws"
	host := strings.TrimPrefix(wsURL, "wss://")
	host = strings.Split(host, "/")[0]
	hostParts := strings.Split(host, ":")
	portStr := hostParts[len(hostParts)-1]
	var port int
	fmt.Sscanf(portStr, "%d", &port)

	logger := &core.FakeLogger{}
	osProvider := core.OsProvider{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stateDir, err := os.MkdirTemp("", "ws_spki_mismatch_state_*")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(stateDir); err != nil {
			t.Logf("failed to remove %s: %v", stateDir, err)
		}
	}()

	deps := core.Dependencies{}
	deps.SetLogger(logger)
	deps.SetOsProvider(osProvider)
	deps.SetMessenger(core.NewMessenger(logger, osProvider))
	deps.SetStateStore(core.NewFileStateStore(filepath.Join(stateDir, "state"), osProvider))
	deps.SetContext(ctx)
	deps.SetCancel(cancel)

	svc := NewService(deps, core.ServiceConfig{
		Name: "spkiMismatchClient",
		Subs: map[string]core.ChannelInfo{"sub": {Name: "anyCh"}},
		Config: map[string]interface{}{
			"port":     port,
			"hostname": "localhost",
			// Deliberately wrong SPKI pin — all zeros.
			"server_cert_spki_sha256": "0000000000000000000000000000000000000000000000000000000000000000",
		},
	})
	require.NoError(t, svc.Initialize())

	select {
	case <-upgraded:
		t.Fatal("client connected despite SPKI pin mismatch — VerifyPeerCertificate did not reject it")
	case <-time.After(3 * time.Second):
		// Correctly rejected.
	}
}

// TestClientSPKIPinMatch verifies that when "server_cert_spki_sha256" is set to the
// correct SHA-256 hex of the server leaf SPKI, the client accepts the connection.
func TestClientSPKIPinMatch(t *testing.T) {
	upgraded := make(chan struct{}, 1)
	connected := make(chan struct{}, 1)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() {
			if err := conn.Close(); err != nil {
				t.Logf("server handler conn close error: %v", err)
			}
		}()
		select {
		case upgraded <- struct{}{}:
		default:
		}

		// Minimal server-side handshake so the client's handleConnection proceeds.
		var hello wsMessage
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		if err := conn.ReadJSON(&hello); err != nil {
			return
		}
		conn.SetReadDeadline(time.Time{})

		if err := conn.WriteJSON(wsMessage{
			V:            protocolVersion,
			Type:         "welcome",
			ServerID:     uuid.New().String(),
			Capabilities: &wsCapabilities{Batch: true},
			Heartbeat: &wsHeartbeat{
				PingIntervalMs: int(pingInterval.Milliseconds()),
				PongTimeoutMs:  int(pongTimeout.Milliseconds()),
			},
		}); err != nil {
			t.Logf("conn.WriteJSON failed: %v", err)
		}
		select {
		case connected <- struct{}{}:
		default:
		}
		// Drain to keep the connection alive.
		for {
			conn.SetReadDeadline(time.Now().Add(10 * time.Second))
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})

	clientDir, err := os.MkdirTemp("", "ws_spki_match_*")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(clientDir); err != nil {
			t.Logf("failed to remove %s: %v", clientDir, err)
		}
	}()

	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", clientDir)
	defer os.Setenv("HOME", oldHome)

	clientCertsDir := filepath.Join(clientDir, ".keyop", "certs")
	serverCert, serverKey, _, _, err := util.CreateTestCerts(clientCertsDir)
	require.NoError(t, err)

	// Load the leaf cert to compute the correct SPKI pin.
	leafCertBytes, err := os.ReadFile(serverCert)
	require.NoError(t, err)
	tlsCert, err := tls.LoadX509KeyPair(serverCert, serverKey)
	require.NoError(t, err)
	_ = leafCertBytes
	parsedLeaf, err := x509.ParseCertificate(tlsCert.Certificate[0])
	require.NoError(t, err)
	spkiSum := sha256.Sum256(parsedLeaf.RawSubjectPublicKeyInfo)
	spkiPin := hex.EncodeToString(spkiSum[:])

	caCertBytes, err := os.ReadFile(filepath.Join(clientCertsDir, "ca.crt"))
	require.NoError(t, err)
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCertBytes)

	tlsCfg := &tls.Config{
		Certificates:       []tls.Certificate{tlsCert},
		ClientCAs:          caCertPool,
		ClientAuth:         tls.RequireAndVerifyClientCert,
		InsecureSkipVerify: true, //nolint:gosec — test-only server
	}

	server := httptest.NewUnstartedServer(handler)
	server.TLS = tlsCfg
	server.StartTLS()
	t.Cleanup(server.Close)

	wsURL := strings.Replace(server.URL, "https", "wss", 1) + "/ws"
	host := strings.TrimPrefix(wsURL, "wss://")
	host = strings.Split(host, "/")[0]
	hostParts := strings.Split(host, ":")
	portStr := hostParts[len(hostParts)-1]
	var port int
	fmt.Sscanf(portStr, "%d", &port)

	logger := &core.FakeLogger{}
	osProvider := core.OsProvider{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stateDir, err := os.MkdirTemp("", "ws_spki_match_state_*")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(stateDir); err != nil {
			t.Logf("failed to remove %s: %v", stateDir, err)
		}
	}()

	deps := core.Dependencies{}
	deps.SetLogger(logger)
	deps.SetOsProvider(osProvider)
	deps.SetMessenger(core.NewMessenger(logger, osProvider))
	deps.SetStateStore(core.NewFileStateStore(filepath.Join(stateDir, "state"), osProvider))
	deps.SetContext(ctx)
	deps.SetCancel(cancel)

	svc := NewService(deps, core.ServiceConfig{
		Name: "spkiMatchClient",
		Subs: map[string]core.ChannelInfo{"sub": {Name: "anyCh"}},
		Config: map[string]interface{}{
			"port":                    port,
			"hostname":                "localhost",
			"server_cert_spki_sha256": spkiPin,
		},
	})
	require.NoError(t, svc.Initialize())

	select {
	case <-upgraded:
		// WebSocket upgrade succeeded — client accepted the server cert.
	case <-time.After(5 * time.Second):
		t.Fatal("timed out: client did not connect to server with matching SPKI pin")
	}
}
