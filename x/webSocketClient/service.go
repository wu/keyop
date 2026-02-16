package webSocketClient

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"keyop/core"
	"keyop/util"
	"net/url"
	"path/filepath"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Service struct {
	Deps     core.Dependencies
	Cfg      core.ServiceConfig
	Port     int
	Hostname string
	state    map[string]queueState
	mu       sync.Mutex

	activeSubs   map[string]context.CancelFunc
	activeSubsMu sync.Mutex
}

type queueState struct {
	FileName string `json:"fileName"`
	Offset   int64  `json:"offset"`
}

func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	svc := &Service{
		Deps:       deps,
		Cfg:        cfg,
		state:      make(map[string]queueState),
		activeSubs: make(map[string]context.CancelFunc),
	}

	if port, ok := cfg.Config["port"].(int); ok {
		svc.Port = port
	}
	if hostname, ok := cfg.Config["hostname"].(string); ok {
		svc.Hostname = hostname
	}

	return svc
}

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

func (svc *Service) Initialize() error {
	logger := svc.Deps.MustGetLogger()
	stateStore := svc.Deps.MustGetStateStore()

	// Load state
	if err := stateStore.Load(svc.Cfg.Name, &svc.state); err != nil {
		logger.Error("webSocketClient: failed to load state", "error", err)
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

	tlsConfig := &tls.Config{
		Certificates:       []tls.Certificate{cert},
		RootCAs:            caCertPool,
		InsecureSkipVerify: true,
	}

	u := url.URL{Scheme: "wss", Host: fmt.Sprintf("%s:%d", svc.Hostname, svc.Port), Path: "/ws"}
	logger.Debug("webSocketClient: starting connection loop", "url", u.String())

	for {
		dialer := websocket.Dialer{
			TLSClientConfig: tlsConfig,
		}

		logger.Debug("webSocketClient: attempting to connect", "url", u.String())
		conn, _, err := dialer.Dial(u.String(), nil)
		if err != nil {
			logger.Error("webSocketClient: dial failed", "error", err)
			time.Sleep(5 * time.Second)
			continue
		}
		logger.Debug("webSocketClient: connected")

		svc.handleConnection(conn)
		conn.Close()
		time.Sleep(1 * time.Second)
	}
}

type wsMessage struct {
	Type     string       `json:"type"`
	Queue    string       `json:"queue,omitempty"`
	Channels []string     `json:"channels,omitempty"`
	FileName string       `json:"fileName,omitempty"`
	Offset   int64        `json:"offset,omitempty"`
	Payload  core.Message `json:"payload,omitempty"`
}

func (svc *Service) handleConnection(conn *websocket.Conn) {
	logger := svc.Deps.MustGetLogger()
	messenger := svc.Deps.MustGetMessenger()

	logger.Info("webSocketClient: handling new connection")

	// Reset active local subscriptions on new connection to avoid duplicates
	svc.activeSubsMu.Lock()
	for _, cancel := range svc.activeSubs {
		cancel()
	}
	svc.activeSubs = make(map[string]context.CancelFunc)
	svc.activeSubsMu.Unlock()

	_, cancel := context.WithCancel(svc.Deps.MustGetContext())
	defer cancel()

	// Send Resume messages for each queue we have state for
	svc.mu.Lock()
	for q, s := range svc.state {
		resumeMsg := wsMessage{
			Type:     "resume",
			Queue:    q,
			FileName: s.FileName,
			Offset:   s.Offset,
		}
		if err := conn.WriteJSON(resumeMsg); err != nil {
			logger.Error("webSocketClient: failed to send resume", "error", err)
		}
	}
	svc.mu.Unlock()

	// Immediately send initial subscribe
	var initialChannels []string
	for _, sub := range svc.Cfg.Subs {
		initialChannels = append(initialChannels, sub.Name)
	}
	initialSubscribeMsg := wsMessage{
		Type:     "subscribe",
		Channels: initialChannels,
	}
	logger.Info("webSocketClient: sending initial subscribe message", "message", initialSubscribeMsg)
	if err := conn.WriteJSON(initialSubscribeMsg); err != nil {
		logger.Error("webSocketClient: failed to send initial subscribe", "error", err)
		return
	}

	hostname, err := util.GetShortHostname(svc.Deps.MustGetOsProvider())
	if err != nil {
		logger.Error("webSocketClient: failed to get hostname", "error", err)
		hostname = "unknown"
	}

	// Loop detection ID
	selfID := fmt.Sprintf("%s:%s:%s", hostname, svc.Cfg.Type, svc.Cfg.Name)

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

		if msg.Type == "message" {
			// Loop detection
			for _, r := range msg.Payload.Route {
				if r == selfID {
					logger.Debug("webSocketClient: loop detected, skipping message", "uuid", msg.Payload.Uuid)
					goto ack
				}
			}

			// this message was sent between client and server without going through the messenger,
			// so we manually append the route
			msg.Payload.Route = append(msg.Payload.Route, selfID) // Add self to route

			// At least once processing: process then ACK
			if err := messenger.Send(msg.Payload); err != nil {
				logger.Error("webSocketClient: failed to forward message", "error", err)
				// don't ack if send fails, so that the server can retry
				// should be better handling here
				continue
			}

			// Save state
			svc.mu.Lock()
			// The server sends the fileName and offset of the message it just sent.
			// When we resume, we want to start AFTER this message.
			// The messenger's SubscribeExtended provides the nextOffset, which the server sends as 'Offset'.
			svc.state[msg.Queue] = queueState{
				FileName: msg.FileName,
				Offset:   msg.Offset,
			}
			if err := svc.Deps.MustGetStateStore().Save(svc.Cfg.Name, svc.state); err != nil {
				logger.Error("webSocketClient: failed to save state", "error", err)
			}
			svc.mu.Unlock()

		ack:
			// Send ACK
			ack := wsMessage{Type: "ack"}
			if err := conn.WriteJSON(ack); err != nil {
				logger.Error("webSocketClient: failed to send ack", "error", err)
				return
			}
		}
	}
}

func (svc *Service) Check() error {
	return nil
}
