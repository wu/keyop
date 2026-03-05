package webSocketServer

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"keyop/core"
	wsp "keyop/x/webSocketProtocol"
	"net/http"
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
	protocolVersion  = wsp.ProtocolVersion
	pingInterval     = wsp.PingInterval
	pongTimeout      = wsp.PongTimeout
	handshakeTimeout = wsp.HandshakeTimeout
	ackTimeout       = wsp.AckTimeout
)

type Service struct {
	Deps      core.Dependencies
	Cfg       core.ServiceConfig
	Port      int
	BatchSize int
}

func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	svc := &Service{
		Deps:      deps,
		Cfg:       cfg,
		BatchSize: defaultBatchSize,
	}

	if port, ok := cfg.Config["port"].(int); ok {
		svc.Port = port
	}
	if bs, ok := cfg.Config["batch_size"].(int); ok && bs > 0 {
		svc.BatchSize = bs
	}

	return svc
}

func (svc *Service) ValidateConfig() []error {
	logger := svc.Deps.MustGetLogger()
	var errs []error

	if _, ok := svc.Cfg.Config["port"].(int); !ok {
		err := fmt.Errorf("webSocketServer: port not set in config")
		logger.Error(err.Error())
		errs = append(errs, err)
	}

	osProvider := svc.Deps.MustGetOsProvider()
	home, err := osProvider.UserHomeDir()
	if err != nil {
		errs = append(errs, fmt.Errorf("webSocketServer: failed to get home dir: %w", err))
	} else {
		certPath := filepath.Join(home, ".keyop", "certs", "keyop-server.crt")
		keyPath := filepath.Join(home, ".keyop", "certs", "keyop-server.key")
		caPath := filepath.Join(home, ".keyop", "certs", "ca.crt")

		for _, p := range []string{certPath, keyPath, caPath} {
			if _, err := osProvider.Stat(p); err != nil {
				errs = append(errs, fmt.Errorf("webSocketServer: file not found: %s", p))
			}
		}
	}

	return errs
}

func (svc *Service) Initialize() error {
	logger := svc.Deps.MustGetLogger()
	osProvider := svc.Deps.MustGetOsProvider()

	home, err := osProvider.UserHomeDir()
	if err != nil {
		return err
	}

	certPath := filepath.Join(home, ".keyop", "certs", "keyop-server.crt")
	keyPath := filepath.Join(home, ".keyop", "certs", "keyop-server.key")
	caPath := filepath.Join(home, ".keyop", "certs", "ca.crt")

	certPEM, _ := osProvider.ReadFile(certPath)
	keyPEM, _ := osProvider.ReadFile(keyPath)
	caCertPEM, _ := osProvider.ReadFile(caPath)

	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return err
	}

	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCertPEM)

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientCAs:    caCertPool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
		MinVersion:   tls.VersionTLS12,
	}

	upgrader := websocket.Upgrader{
		CheckOrigin: func(_ *http.Request) bool { return true },
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			logger.Error("webSocketServer: upgrade failed", "error", err)
			return
		}
		go svc.handleConnection(conn)
	})

	server := &http.Server{
		Addr:      fmt.Sprintf(":%d", svc.Port),
		Handler:   mux,
		TLSConfig: tlsConfig,
	}

	go func() {
		if err := server.ListenAndServeTLS("", ""); err != nil {
			logger.Error("webSocketServer: server failed", "error", err)
		}
	}()

	return nil
}

// BatchItem holds a single message along with its queue position metadata.
type BatchItem struct {
	Queue    string       `json:"queue,omitempty"`
	FileName string       `json:"fileName,omitempty"`
	Offset   int64        `json:"offset,omitempty"`
	Payload  core.Message `json:"payload,omitempty"`
}

// wsCapabilities describes optional protocol features.
type wsCapabilities struct {
	Batch bool `json:"batch"`
}

// wsHeartbeat carries ping/pong timing parameters in the welcome message.
type wsHeartbeat struct {
	PingIntervalMs int `json:"pingIntervalMs"`
	PongTimeoutMs  int `json:"pongTimeoutMs"`
}

// wsMessage is the unified protocol v=1 wire format.
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
	// resume / batch items
	Queue    string `json:"queue,omitempty"`
	FileName string `json:"fileName,omitempty"`
	Offset   int64  `json:"offset,omitempty"`
	// subscribe
	Channels []string `json:"channels,omitempty"`
	// batch / ack
	BatchID string      `json:"batchId,omitempty"`
	Items   []BatchItem `json:"items,omitempty"`
	// legacy single-payload (kept for fallback only)
	Payload core.Message `json:"payload,omitempty"`
}

// pendingAck is a waiter for a single in-flight server→client batch.
type pendingAck struct {
	done chan struct{}
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
		fmt.Printf("webSocketServer: failed to close conn: %v\n", err)
	}
}

// flushPending closes all in-flight pending-ack done channels so that any goroutine
// waiting in sendBatchAndWaitAck unblocks immediately when the connection is torn down.
// It must be called with pendingMu held.
//
// Deleting map keys during a range loop is intentional and safe in Go (spec §For range).
// The select guards against a double-close in case the ACK read-loop already closed the
// channel for this entry before the deferred flush ran.
func flushPending(pending map[string]*pendingAck) {
	for id, w := range pending {
		select {
		case <-w.done: // already closed — skip
		default:
			close(w.done)
		}
		delete(pending, id)
	}
}

func (svc *Service) handleConnection(conn *websocket.Conn) {
	// get logger early so we can report Close errors from defer
	logger := svc.Deps.MustGetLogger()
	defer func() {
		if err := conn.Close(); err != nil {
			logger.Error("webSocketServer: failed to close connection", "error", err)
		}
	}()
	messenger := svc.Deps.MustGetMessenger()

	logger.Debug("webSocketServer: handling new connection")

	ctx, cancel := context.WithCancel(svc.Deps.MustGetContext())
	defer cancel()

	cw := &connWriter{conn: conn}

	// ── Handshake ─────────────────────────────────────────────────────────────
	if err := conn.SetReadDeadline(time.Now().Add(handshakeTimeout)); err != nil {
		logger.Error("webSocketServer: failed to set read deadline for handshake", "error", err)
	}
	_, raw, err := conn.ReadMessage()
	if err != nil {
		logger.Error("webSocketServer: handshake read failed", "error", err)
		return
	}
	if err := conn.SetReadDeadline(time.Time{}); err != nil {
		logger.Error("webSocketServer: failed to clear read deadline", "error", err)
	}

	var hello wsMessage
	if err := json.Unmarshal(raw, &hello); err != nil || hello.Type != "hello" {
		logger.Error("webSocketServer: expected hello", "raw", string(raw))
		if err := cw.writeJSON(wsMessage{
			V:       protocolVersion,
			Type:    "error",
			Code:    wsp.CodeBadHandshake,
			Message: wsp.BadHandshakeMsg,
		}); err != nil {
			logger.Error("webSocketServer: failed to write JSON", "error", err)
		}
		return
	}
	if hello.V != protocolVersion {
		if err := cw.writeJSON(wsMessage{
			V:         protocolVersion,
			Type:      "error",
			Code:      wsp.CodeUnsupportedVersion,
			ExpectedV: protocolVersion,
			GotV:      hello.V,
			Message:   wsp.UnsupportedVersionMsg(protocolVersion, hello.V),
		}); err != nil {
			logger.Error("webSocketServer: failed to write JSON", "error", err)
		}
		return
	}

	serverID := uuid.New().String()
	if err := cw.writeJSON(wsMessage{
		V:            protocolVersion,
		Type:         "welcome",
		ServerID:     serverID,
		Capabilities: &wsCapabilities{Batch: true},
		Heartbeat: &wsHeartbeat{
			PingIntervalMs: int(pingInterval.Milliseconds()),
			PongTimeoutMs:  int(pongTimeout.Milliseconds()),
		},
	}); err != nil {
		logger.Error("webSocketServer: failed to send welcome", "error", err)
		return
	}

	// ── Ping / Pong deadlines ──────────────────────────────────────────────────
	if err := conn.SetReadDeadline(time.Now().Add(pongTimeout)); err != nil {
		logger.Error("webSocketServer: failed to set read deadline for pong", "error", err)
	}
	conn.SetPongHandler(func(string) error {
		if err := conn.SetReadDeadline(time.Now().Add(pongTimeout)); err != nil {
			logger.Error("webSocketServer: failed to extend read deadline on pong", "error", err)
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
					logger.Error("webSocketServer: ping failed", "error", err)
					cancel()
					return
				}
			}
		}
	}()

	// ── Pending-ack registry (server→client batches) ───────────────────────────
	pendingMu := sync.Mutex{}
	pending := make(map[string]*pendingAck)

	readerName := "ws_" + uuid.New().String()
	logger.Debug("webSocketServer: starting connection loop", "readerName", readerName, "clientId", hello.ClientID)

	activeSubs := make(map[string]context.CancelFunc)
	activeSubsMu := sync.Mutex{}
	resumesMu := sync.Mutex{}
	resumes := make(map[string]wsMessage)

	clientBatchChan := make(chan wsMessage, 20)

	// Client→server batch processor
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case msg := <-clientBatchChan:
				failed := false
				for _, item := range msg.Items {
					if err := messenger.Send(item.Payload); err != nil {
						logger.Error("webSocketServer: failed to forward batched message from client", "error", err)
						failed = true
						break
					}
				}
				if !failed && msg.BatchID != "" {
					if err := cw.writeJSON(wsMessage{V: protocolVersion, Type: "ack", BatchID: msg.BatchID, Queue: msg.Queue}); err != nil {
						logger.Error("webSocketServer: failed to write JSON", "error", err)
					}
				}
			}
		}
	}()

	// Read loop
	msgChan := make(chan wsMessage, 20)
	go func() {
		defer cancel()
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				return
			}
			logger.Debug("webSocketServer: received message", "message", string(message))
			var msg wsMessage
			if err := json.Unmarshal(message, &msg); err != nil {
				continue
			}
			// Enforce protocol version on every post-handshake frame.
			if msg.V != protocolVersion {
				logger.Error("webSocketServer: version mismatch on frame", "type", msg.Type, "v", msg.V)
				if err := cw.writeJSON(wsMessage{
					V:         protocolVersion,
					Type:      "error",
					Code:      wsp.CodeUnsupportedVersion,
					ExpectedV: protocolVersion,
					GotV:      msg.V,
					Message:   wsp.UnsupportedVersionMsg(protocolVersion, msg.V),
				}); err != nil {
					logger.Error("webSocketServer: failed to write JSON", "error", err)
				}
				cw.close()
				return
			}
			switch msg.Type {
			case "ack":
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
				select {
				case clientBatchChan <- msg:
				case <-ctx.Done():
					return
				}
			default:
				select {
				case msgChan <- msg:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	// Flush all pending ack waiters when the connection closes so that batch-sender
	// goroutines don't leak.
	defer func() {
		pendingMu.Lock()
		flushPending(pending)
		pendingMu.Unlock()
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-msgChan:
			switch msg.Type {
			case "resume":
				resumesMu.Lock()
				resumes[msg.Queue] = msg
				resumesMu.Unlock()
				logger.Debug("webSocketServer: received resume", "queue", msg.Queue, "file", msg.FileName, "offset", msg.Offset)

			case "subscribe":
				var channelsToSubscribe []core.ChannelInfo
				for _, chName := range msg.Channels {
					if chName != "" {
						channelsToSubscribe = append(channelsToSubscribe, core.ChannelInfo{Name: chName})
					}
				}

				activeSubsMu.Lock()
				for chName, cancelSub := range activeSubs {
					found := false
					for _, sub := range channelsToSubscribe {
						if sub.Name == chName {
							found = true
							break
						}
					}
					if !found {
						cancelSub()
						delete(activeSubs, chName)
					}
				}

				for _, sub := range channelsToSubscribe {
					if _, active := activeSubs[sub.Name]; !active {
						subCtx, subCancel := context.WithCancel(ctx)
						activeSubs[sub.Name] = subCancel

						go func(s core.ChannelInfo, sCtx context.Context) {
							resumesMu.Lock()
							rm, ok := resumes[s.Name]
							if ok {
								delete(resumes, s.Name)
							}
							resumesMu.Unlock()

							if ok && rm.FileName != "" {
								logger.Debug("webSocketServer: resuming subscription", "channel", s.Name, "file", rm.FileName, "offset", rm.Offset)
								if err := messenger.SetReaderState(s.Name, readerName, rm.FileName, rm.Offset); err != nil {
									logger.Error("webSocketServer: failed to set reader state", "error", err)
								}
							} else {
								logger.Debug("webSocketServer: seeking to end for new subscription", "channel", s.Name)
								if err := messenger.SeekToEnd(s.Name, readerName); err != nil {
									logger.Error("webSocketServer: failed to seek to end", "channel", s.Name, "error", err)
								}
							}

							batchChan := make(chan BatchItem, svc.BatchSize*2)

							go func() {
								for {
									var item BatchItem
									select {
									case <-sCtx.Done():
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
									if err := svc.sendBatchAndWaitAck(sCtx, cw, &pendingMu, pending, batchID, s.Name, batch); err != nil {
										if err != context.Canceled {
											logger.Error("webSocketServer: batch send failed", "channel", s.Name, "error", err)
										}
										return
									}
								}
							}()

							err := messenger.SubscribeExtended(sCtx, readerName, s.Name, svc.Cfg.Type, svc.Cfg.Name, s.MaxAge, func(msg core.Message, fileName string, offset int64) error {
								logger.Debug("webSocketServer: queuing message for batch", "channel", s.Name, "uuid", msg.Uuid)
								select {
								case batchChan <- BatchItem{
									Queue:    s.Name,
									FileName: fileName,
									Offset:   offset,
									Payload:  msg,
								}:
									return nil
								case <-sCtx.Done():
									return sCtx.Err()
								}
							})
							if err != nil && err != context.Canceled {
								logger.Error("webSocketServer: subscribe failed", "channel", s.Name, "error", err)
							}
						}(sub, subCtx)
					}
				}
				activeSubsMu.Unlock()
			}
		}
	}
}

// sendBatchAndWaitAck sends a batch to the client and waits for the correlated ack.
// On timeout it closes the connection to force reconnect/resume (at-least-once semantics).
func (svc *Service) sendBatchAndWaitAck(
	ctx context.Context,
	cw *connWriter,
	pendingMu *sync.Mutex,
	pending map[string]*pendingAck,
	batchID string,
	queue string,
	batch []BatchItem,
) error {
	waiter := &pendingAck{done: make(chan struct{})}
	pendingMu.Lock()
	pending[batchID] = waiter
	pendingMu.Unlock()

	if err := cw.writeJSON(wsMessage{
		V:       protocolVersion,
		Type:    "batch",
		BatchID: batchID,
		Queue:   queue,
		Items:   batch,
	}); err != nil {
		pendingMu.Lock()
		delete(pending, batchID)
		pendingMu.Unlock()
		return err
	}

	select {
	case <-waiter.done:
		return nil
	case <-time.After(ackTimeout):
		pendingMu.Lock()
		delete(pending, batchID)
		pendingMu.Unlock()
		cw.close()
		return fmt.Errorf("ack timeout for batchId %s (%d messages)", batchID, len(batch))
	case <-ctx.Done():
		pendingMu.Lock()
		delete(pending, batchID)
		pendingMu.Unlock()
		return ctx.Err()
	}
}

func (svc *Service) Check() error {
	return nil
}
