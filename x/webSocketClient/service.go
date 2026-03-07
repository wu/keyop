// Package webSocketClient provides a reusable websocket client implementation for streaming integrations.
package webSocketClient

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"keyop/core"
	"keyop/util"
	wsp "keyop/x/webSocketProtocol"
	"net/url"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

const (
	defaultBatchSize = 20
)

// Aliases so internal code keeps reading naturally.
const (
	protocolVersion = wsp.ProtocolVersion
	pingInterval    = wsp.PingInterval
	pongTimeout     = wsp.PongTimeout
	welcomeTimeout  = wsp.WelcomeTimeout
	ackTimeout      = wsp.AckTimeout
)

// Service manages websocket connections, message dispatch, and reconnection logic for streaming upstreams.
type Service struct {
	Deps              core.Dependencies
	Cfg               core.ServiceConfig
	Port              int
	Hostname          string
	RouteLoopSkipHost string
	BatchSize         int
	state             map[string]queueState
	mu                sync.Mutex

	activeSubs   map[string]context.CancelFunc
	activeSubsMu sync.Mutex
}

type queueState struct {
	FileName string `json:"fileName"`
	Offset   int64  `json:"offset"`
}

// NewService creates a new service using the provided dependencies and configuration.
func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	svc := &Service{
		Deps:       deps,
		Cfg:        cfg,
		BatchSize:  defaultBatchSize,
		state:      make(map[string]queueState),
		activeSubs: make(map[string]context.CancelFunc),
	}

	if port, ok := cfg.Config["port"].(int); ok {
		svc.Port = port
	}
	if hostname, ok := cfg.Config["hostname"].(string); ok {
		svc.Hostname = hostname
	}
	if skipHost, ok := cfg.Config["route_loop_skip_host"].(string); ok {
		svc.RouteLoopSkipHost = skipHost
	}
	if bs, ok := cfg.Config["batch_size"].(int); ok && bs > 0 {
		svc.BatchSize = bs
	}

	return svc
}

// ValidateConfig validates the service configuration and returns any validation errors.
func (svc *Service) ValidateConfig() []error {
	var errs []error

	if _, ok := svc.Cfg.Config["port"].(int); !ok {
		errs = append(errs, fmt.Errorf("webSocketClient: port not set"))
	}
	if _, ok := svc.Cfg.Config["hostname"].(string); !ok {
		errs = append(errs, fmt.Errorf("webSocketClient: hostname not set"))
	}

	osProvider := svc.Deps.MustGetOsProvider()
	home, _ := osProvider.UserHomeDir()
	certPath := filepath.Join(home, ".keyop", "certs", "keyop-client.crt")
	keyPath := filepath.Join(home, ".keyop", "certs", "keyop-client.key")
	caPath := filepath.Join(home, ".keyop", "certs", "ca.crt")

	for _, p := range []string{certPath, keyPath, caPath} {
		if _, err := osProvider.Stat(p); err != nil {
			errs = append(errs, fmt.Errorf("webSocketClient: file not found: %s", p))
		}
	}

	return errs
}

// Initialize performs one-time startup required by the service (resource loading or connectivity checks).
func (svc *Service) Initialize() error {
	logger := svc.Deps.MustGetLogger()
	stateStore := svc.Deps.MustGetStateStore()

	if err := stateStore.Load(svc.Cfg.Name, &svc.state); err != nil {
		logger.Error("webSocketClient: failed to load state", "error", err)
	}
	if svc.state == nil {
		svc.state = make(map[string]queueState)
	}

	go svc.connectLoop()

	return nil
}

func (svc *Service) connectLoop() {
	logger := svc.Deps.MustGetLogger()
	osProvider := svc.Deps.MustGetOsProvider()
	home, _ := osProvider.UserHomeDir()

	certPath := filepath.Join(home, ".keyop", "certs", "keyop-client.crt")
	keyPath := filepath.Join(home, ".keyop", "certs", "keyop-client.key")
	caPath := filepath.Join(home, ".keyop", "certs", "ca.crt")

	certPEM, _ := osProvider.ReadFile(certPath)
	keyPEM, _ := osProvider.ReadFile(keyPath)
	caCertPEM, _ := osProvider.ReadFile(caPath)
	cert, _ := tls.X509KeyPair(certPEM, keyPEM)
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCertPEM)

	// Optional SPKI pin: if "server_cert_spki_sha256" is set in config (hex string),
	// the leaf cert's SubjectPublicKeyInfo hash must match.
	spkiPin, _ := svc.Cfg.Config["server_cert_spki_sha256"].(string)

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      caCertPool,
		// InsecureSkipVerify bypasses the default hostname/SAN checks so that
		// self-signed / private-PKI certs work.  We perform our own chain
		// verification in VerifyPeerCertificate below.
		InsecureSkipVerify: true, //nolint:gosec
		VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			if len(rawCerts) == 0 {
				return fmt.Errorf("webSocketClient: server presented no certificate")
			}

			// Parse leaf cert.
			leaf, err := x509.ParseCertificate(rawCerts[0])
			if err != nil {
				return fmt.Errorf("webSocketClient: failed to parse server leaf cert: %w", err)
			}

			// Build intermediates pool from any extra certs in the chain.
			intermediates := x509.NewCertPool()
			for _, raw := range rawCerts[1:] {
				c, err := x509.ParseCertificate(raw)
				if err != nil {
					return fmt.Errorf("webSocketClient: failed to parse intermediate cert: %w", err)
				}
				intermediates.AddCert(c)
			}

			// Verify the chain against our CA pool.
			opts := x509.VerifyOptions{
				Roots:         caCertPool,
				Intermediates: intermediates,
				KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
				CurrentTime:   time.Now(),
			}
			if _, err := leaf.Verify(opts); err != nil {
				return fmt.Errorf("webSocketClient: server cert verify failed: %w", err)
			}

			// Optional SPKI pin check.
			if spkiPin != "" {
				sum := sha256.Sum256(leaf.RawSubjectPublicKeyInfo)
				got := hex.EncodeToString(sum[:])
				if got != spkiPin {
					return fmt.Errorf("webSocketClient: server cert SPKI pin mismatch (got %s)", got)
				}
			}

			return nil
		},
	}

	u := url.URL{Scheme: "wss", Host: fmt.Sprintf("%s:%d", svc.Hostname, svc.Port), Path: "/ws"}
	logger.Debug("webSocketClient: starting connection loop", "url", u.String())

	for {
		dialer := websocket.Dialer{TLSClientConfig: tlsConfig}

		logger.Debug("webSocketClient: attempting to connect", "url", u.String())
		conn, _, err := dialer.Dial(u.String(), nil)
		if err != nil {
			logger.Error("webSocketClient: dial failed", "error", err)
			time.Sleep(5 * time.Second)
			continue
		}
		logger.Debug("webSocketClient: connected")

		svc.handleConnection(conn)
		if err := conn.Close(); err != nil {
			logger.Debug("webSocketClient: conn close failed", "error", err)
		}
		time.Sleep(1 * time.Second)
	}
}

// BatchItem mirrors the server's BatchItem for wire compatibility.
// doneCh is local-only (not serialised) — closed by the batch-sender after ack.
type BatchItem struct {
	Queue    string       `json:"queue,omitempty"`
	FileName string       `json:"fileName,omitempty"`
	Offset   int64        `json:"offset,omitempty"`
	Payload  core.Message `json:"payload,omitempty"`
	doneCh   chan error   // not on wire
}

// wsMessage is the unified protocol v=1 wire format (shared with server package).
type wsMessage struct {
	V            int             `json:"v"`
	Type         string          `json:"type"`
	ClientID     string          `json:"clientId,omitempty"`
	ServerID     string          `json:"serverId,omitempty"`
	Capabilities *wsCapabilities `json:"capabilities,omitempty"`
	Heartbeat    *wsHeartbeat    `json:"heartbeat,omitempty"`
	// error frame
	Code      string `json:"code,omitempty"`
	Message   string `json:"message,omitempty"`
	ExpectedV int    `json:"expectedV,omitempty"`
	GotV      int    `json:"gotV,omitempty"`
	// resume
	Queue    string `json:"queue,omitempty"`
	FileName string `json:"fileName,omitempty"`
	Offset   int64  `json:"offset,omitempty"`
	// subscribe
	Channels []string `json:"channels,omitempty"`
	// batch / ack
	BatchID string      `json:"batchId,omitempty"`
	Items   []BatchItem `json:"items,omitempty"`
	// legacy
	Payload core.Message `json:"payload,omitempty"`
}

type wsCapabilities struct {
	Batch bool `json:"batch"`
}

type wsHeartbeat struct {
	PingIntervalMs int `json:"pingIntervalMs"`
	PongTimeoutMs  int `json:"pongTimeoutMs"`
}

// connWriter serialises all websocket writes through a single mutex.
type connWriter struct {
	mu   sync.Mutex
	conn *websocket.Conn
}

func (w *connWriter) writeJSON(v interface{}) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.conn.WriteJSON(v)
}

func (w *connWriter) writePing() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.conn.WriteMessage(websocket.PingMessage, nil)
}

func (w *connWriter) close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.conn.Close(); err != nil {
		fmt.Printf("webSocketClient: failed to close conn: %v\n", err)
	}
}

// flushPending closes all outstanding pending-ack done channels so that batch-sender
// goroutines unblock immediately when the connection is torn down or on reconnect.
// Must be called with pendingMu held.
func flushPending(pending map[string]*pendingAck) {
	for id, w := range pending {
		select {
		case <-w.done: // already closed
		default:
			close(w.done)
		}
		delete(pending, id)
	}
}

// pendingAck is a waiter for a single in-flight client→server batch.
type pendingAck struct {
	done chan struct{}
}

func (svc *Service) handleConnection(conn *websocket.Conn) {
	logger := svc.Deps.MustGetLogger()
	messenger := svc.Deps.MustGetMessenger()

	logger.Info("webSocketClient: handling new connection")

	// Reset active local subscriptions on new connection
	svc.activeSubsMu.Lock()
	for _, cancel := range svc.activeSubs {
		cancel()
	}
	svc.activeSubs = make(map[string]context.CancelFunc)
	svc.activeSubsMu.Unlock()

	ctx, cancel := context.WithCancel(svc.Deps.MustGetContext())
	defer cancel()

	cw := &connWriter{conn: conn}

	clientID := uuid.New().String()

	// ── Handshake: send hello, wait for welcome ───────────────────────────────
	if err := cw.writeJSON(wsMessage{
		V:            protocolVersion,
		Type:         "hello",
		ClientID:     clientID,
		Capabilities: &wsCapabilities{Batch: true},
	}); err != nil {
		logger.Error("webSocketClient: failed to send hello", "error", err)
		return
	}
	logger.Debug("webSocketClient: sent hello", "clientId", clientID)

	if err := conn.SetReadDeadline(time.Now().Add(welcomeTimeout)); err != nil {
		logger.Error("webSocketClient: failed to set read deadline for welcome", "error", err)
		return
	}
	_, raw, err := conn.ReadMessage()
	if err != nil {
		logger.Error("webSocketClient: failed to read welcome", "error", err)
		return
	}
	if err := conn.SetReadDeadline(time.Time{}); err != nil {
		logger.Error("webSocketClient: failed to clear read deadline", "error", err)
		// continue; not fatal
	}

	var welcome wsMessage
	if err := json.Unmarshal(raw, &welcome); err != nil {
		logger.Error("webSocketClient: failed to parse welcome", "error", err)
		return
	}
	if welcome.Type == "error" {
		logger.Error("webSocketClient: server rejected handshake",
			"code", welcome.Code, "message", welcome.Message,
			"expectedV", welcome.ExpectedV, "gotV", welcome.GotV)
		return
	}
	if welcome.Type != "welcome" || welcome.V != protocolVersion {
		logger.Error("webSocketClient: unexpected welcome", "type", welcome.Type, "v", welcome.V)
		return
	}
	logger.Debug("webSocketClient: received welcome", "serverId", welcome.ServerID)
	logger.Debug("webSocketClient: handshake complete", "serverId", welcome.ServerID)

	// ── Ping / Pong deadlines ─────────────────────────────────────────────────
	if err := conn.SetReadDeadline(time.Now().Add(pongTimeout)); err != nil {
		logger.Error("webSocketClient: failed to set read deadline for pong", "error", err)
		return
	}
	conn.SetPongHandler(func(string) error {
		if err := conn.SetReadDeadline(time.Now().Add(pongTimeout)); err != nil {
			logger.Error("webSocketClient: failed to extend read deadline on pong", "error", err)
			return err
		}
		return nil
	})

	go func() {
		ticker := time.NewTicker(pingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := cw.writePing(); err != nil {
					logger.Error("webSocketClient: ping failed", "error", err)
					cancel()
					return
				}
			}
		}
	}()

	// ── Pending-ack registry (client→server batches) ───────────────────────────
	pendingMu := sync.Mutex{}
	pending := make(map[string]*pendingAck)

	// ── (Send) Resume messages ──────────────────────────────────────────────────
	svc.mu.Lock()
	for q, s := range svc.state {
		resumeMsg := wsMessage{
			V:        protocolVersion,
			Type:     "resume",
			Queue:    q,
			FileName: s.FileName,
			Offset:   s.Offset,
		}
		if err := cw.writeJSON(resumeMsg); err != nil {
			logger.Error("webSocketClient: failed to send resume", "error", err)
		}
	}
	svc.mu.Unlock()

	// ── Send initial subscribe ────────────────────────────────────────────────
	var initialChannels []string
	for _, sub := range svc.Cfg.Subs {
		remoteName := sub.Remote
		if remoteName == "" {
			remoteName = sub.Name
		}
		initialChannels = append(initialChannels, remoteName)
	}
	// Log the subscribe message so BAD_HANDSHAKE can be correlated with server response.
	subMsg := wsMessage{
		V:        protocolVersion,
		Type:     "subscribe",
		Channels: initialChannels,
	}
	logger.Info("webSocketClient: sending initial subscribe message", "message", subMsg)
	if err := cw.writeJSON(subMsg); err != nil {
		logger.Error("webSocketClient: failed to send initial subscribe", "error", err)
		return
	}

	hostname, err := util.GetShortHostname(svc.Deps.MustGetOsProvider())
	if err != nil {
		logger.Error("webSocketClient: failed to get hostname", "error", err)
		hostname = "unknown"
	}
	selfID := fmt.Sprintf("%s:%s:%s", hostname, svc.Cfg.Type, svc.Cfg.Name)

	// Build a remote→local channel name map for inbound sub messages.
	// When a sub has a Remote name the server delivers on that name; we remap
	// it back to the local Name before forwarding into the local messenger.
	remoteToLocal := make(map[string]string)
	for _, sub := range svc.Cfg.Subs {
		if sub.Remote != "" {
			remoteToLocal[sub.Remote] = sub.Name
		}
	}

	// ── Start outbound publishing goroutines ──────────────────────────────────
	for _, pub := range svc.Cfg.Pubs {
		pubCtx, pubCancel := context.WithCancel(ctx)
		svc.activeSubsMu.Lock()
		svc.activeSubs["pub_"+pub.Name] = pubCancel
		svc.activeSubsMu.Unlock()

		batchChan := make(chan BatchItem, svc.BatchSize*2)

		// Batch-sender: collects items, assigns batchId, waits for correlated ack.
		go func(pCtx context.Context) {
			for {
				var item BatchItem
				select {
				case <-pCtx.Done():
					return
				case item = <-batchChan:
				}

				batch := []BatchItem{item}
				for len(batch) < svc.BatchSize {
					select {
					case next := <-batchChan:
						batch = append(batch, next)
					default:
						goto send
					}
				}
			send:
				batchID := uuid.New().String()
				waiter := &pendingAck{done: make(chan struct{})}
				pendingMu.Lock()
				pending[batchID] = waiter
				pendingMu.Unlock()

				// Strip doneCh before serialisation (doneCh is not on wire)
				wireItems := make([]BatchItem, len(batch))
				for i, bi := range batch {
					wireItems[i] = BatchItem{
						Queue:    bi.Queue,
						FileName: bi.FileName,
						Offset:   bi.Offset,
						Payload:  bi.Payload,
					}
				}

				if err := cw.writeJSON(wsMessage{
					V:       protocolVersion,
					Type:    "batch",
					BatchID: batchID,
					Items:   wireItems,
				}); err != nil {
					pendingMu.Lock()
					delete(pending, batchID)
					pendingMu.Unlock()
					logger.Error("webSocketClient: failed to send batch", "error", err)
					for _, bi := range batch {
						if bi.doneCh != nil {
							bi.doneCh <- err
						}
					}
					return
				}

				// Wait for correlated ack
				var ackErr error
				select {
				case <-waiter.done:
					// success
				case <-pCtx.Done():
					ackErr = pCtx.Err()
					pendingMu.Lock()
					delete(pending, batchID)
					pendingMu.Unlock()
				case <-time.After(ackTimeout):
					ackErr = fmt.Errorf("webSocketClient: ack timeout for batchId %s", batchID)
					pendingMu.Lock()
					delete(pending, batchID)
					pendingMu.Unlock()
					// Close connection to force reconnect/resume (at-least-once)
					cw.close()
				}

				for _, bi := range batch {
					if bi.doneCh != nil {
						if ackErr != nil {
							bi.doneCh <- ackErr
						} else {
							close(bi.doneCh)
						}
					}
				}
				if ackErr != nil {
					logger.Error("webSocketClient: did not receive server ack for batch", "error", ackErr)
					return
				}
			}
		}(pubCtx)

		go func(p core.ChannelInfo, pCtx context.Context) {
			err := messenger.Subscribe(pCtx, "client_pub_"+p.Name, p.Name, svc.Cfg.Type, svc.Cfg.Name, p.MaxAge, func(m core.Message) error {
				if svc.RouteLoopSkipHost != "" && m.Hostname == svc.RouteLoopSkipHost {
					logger.Debug("webSocketClient: skipping message from route_loop_skip_host", "uuid", m.Uuid, "hostname", m.Hostname)
					return nil
				}
				// Rewrite channel name to the configured remote name before forwarding.
				if p.Remote != "" {
					m.ChannelName = p.Remote
				}
				done := make(chan error, 1)
				select {
				case batchChan <- BatchItem{Payload: m, doneCh: done}:
				case <-pCtx.Done():
					return pCtx.Err()
				}
				select {
				case err := <-done:
					return err
				case <-pCtx.Done():
					return pCtx.Err()
				}
			})
			if err != nil && err != context.Canceled {
				logger.Error("webSocketClient: publication subscribe failed", "channel", p.Name, "error", err)
			}
		}(pub, pubCtx)
	}

	// ── Inbound read loop ─────────────────────────────────────────────────────
	// Flush all pending outbound-batch waiters when this connection closes, so that
	// batch-sender goroutines unblock and can retry on the next reconnect.
	defer func() {
		pendingMu.Lock()
		flushPending(pending)
		pendingMu.Unlock()
	}()

	for {
		logger.Debug("webSocketClient: waiting for message")
		_, message, err := conn.ReadMessage()
		if err != nil {
			logger.Error("webSocketClient: read error", "error", err)
			return
		}
		logger.Debug("webSocketClient: received message", "raw", string(message))

		var msg wsMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			continue
		}

		// Enforce protocol version on every post-handshake frame.
		if msg.V != protocolVersion {
			logger.Error("webSocketClient: version mismatch on frame", "type", msg.Type, "v", msg.V)
			if err := cw.writeJSON(wsMessage{
				V:         protocolVersion,
				Type:      "error",
				Code:      wsp.CodeUnsupportedVersion,
				ExpectedV: protocolVersion,
				GotV:      msg.V,
				Message:   wsp.UnsupportedVersionMsg(protocolVersion, msg.V),
			}); err != nil {
				logger.Error("webSocketClient: failed to write JSON", "error", err)
			}
			cw.close()
			return
		}

		switch msg.Type {
		case "ack":
			// Server acking a client→server batch — signal the matching waiter.
			if msg.BatchID != "" {
				pendingMu.Lock()
				if w, ok := pending[msg.BatchID]; ok {
					// Guard against a race where flushPending already closed this
					// channel (e.g. context cancelled concurrently with an ack).
					select {
					case <-w.done: // already closed — skip
					default:
						close(w.done)
					}
					delete(pending, msg.BatchID)
				}
				pendingMu.Unlock()
			}

		case "batch":
			// Server→client batch delivery.
			var lastQueue, lastFileName string
			var lastOffset int64
			failed := false
			for _, item := range msg.Items {
				loopDetected := false
				for _, r := range item.Payload.Route {
					if r == selfID {
						logger.Debug("webSocketClient: loop detected in batch item, skipping", "uuid", item.Payload.Uuid)
						loopDetected = true
						break
					}
				}
				if loopDetected {
					continue
				}
				item.Payload.Route = append(item.Payload.Route, selfID)
				// Remap remote channel name to local channel name if configured.
				if localName, ok := remoteToLocal[item.Payload.ChannelName]; ok {
					item.Payload.ChannelName = localName
				}
				logger.Info("webSocketClient: received", "uuid", item.Payload.Uuid, "channel", item.Payload.ChannelName, "hostname", item.Payload.Hostname)
				if err := messenger.Send(item.Payload); err != nil {
					logger.Error("webSocketClient: failed to forward batched message", "error", err)
					failed = true
					break
				}
				lastQueue = item.Queue
				lastFileName = item.FileName
				lastOffset = item.Offset
			}

			if failed {
				continue
			}

			if lastQueue != "" {
				svc.mu.Lock()
				svc.state[lastQueue] = queueState{FileName: lastFileName, Offset: lastOffset}
				if err := svc.Deps.MustGetStateStore().Save(svc.Cfg.Name, svc.state); err != nil {
					logger.Error("webSocketClient: failed to save state", "error", err)
				}
				svc.mu.Unlock()
			}

			// Send correlated ack
			if msg.BatchID != "" {
				if err := cw.writeJSON(wsMessage{
					V:       protocolVersion,
					Type:    "ack",
					BatchID: msg.BatchID,
					Queue:   msg.Queue,
				}); err != nil {
					logger.Error("webSocketClient: failed to send batch ack", "error", err)
					return
				}
			}
		}
	}
}

// Check performs the service's periodic work: collect data, evaluate state, and publish messages/metrics.
func (svc *Service) Check() error {
	return nil
}
