package webSocketClient

import (
	"context"
	"keyop/core"
	"keyop/util"
	"keyop/x/webSocketServer"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWebSocket_ClientServer(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "ws_test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Setup certs
	certsDir := filepath.Join(tmpDir, ".keyop", "certs")
	err = util.GenerateTestCerts(certsDir)
	require.NoError(t, err)

	osProvider := core.OsProvider{}
	// Mock UserHomeDir to point to our tmpDir
	// Since core.OsProvider is a real provider, we might need a fake one to override HomeDir
	// or just set HOME environment variable if the provider uses it.
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
		Config: map[string]interface{}{
			"port": port,
		},
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

	// 3. Send message through messenger (server will pick it up and send to client)
	testMsg := core.Message{
		ChannelName: "testChannel",
		Text:        "hello websocket",
		Hostname:    "other-host",
	}

	// 4. Client should receive the message and forward it back to messenger
	// Since we are using the same messenger, we can subscribe to see it.
	// But the client forwards it to messenger.Send(msg.Payload).
	// We need to wait and check if it was received by "the other end" (which is also us).

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

	// Wait for client to connect and subscribe
	time.Sleep(2 * time.Second)

	t.Log("Sending test message...")
	// Use a DIFFERENT messenger for the initial send to avoid loop detection
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
				// We got it twice! Original + re-sent by client
				goto done
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("timed out waiting for messages, got %d", count)
		}
	}
done:

	// 6. Verify client subscribed only to requested channels
	// The server logs it, but we can't easily verify logs programmatically here
	// without redirecting them.
	// But we can check that if we send to a channel NOT in Subs, it's not received.
	nonSubscribedMsg := core.Message{
		ChannelName: "otherChannel",
		Text:        "should not be received",
		Hostname:    "other-host",
	}
	err = messenger2.Send(nonSubscribedMsg)
	require.NoError(t, err)

	// Wait a bit and ensure it's NOT received by the client (which would forward it back)
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

	// 7. Verify state was saved for resume
	// The client saves state under its service name "wsClient"
	var savedState map[string]queueState
	err = stateStore.Load("wsClient", &savedState)
	require.NoError(t, err)
	assert.Contains(t, savedState, "testChannel")
	assert.NotEmpty(t, savedState["testChannel"].FileName)
}
