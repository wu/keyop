package webSocket

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"keyop/core"
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
}

type queueState struct {
	FileName string `json:"fileName"`
	Offset   int64  `json:"offset"`
}

func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	svc := &Service{
		Deps:  deps,
		Cfg:   cfg,
		state: make(map[string]queueState),
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
		errs = append(errs, fmt.Errorf("webSocket: port not set"))
	}
	if _, ok := svc.Cfg.Config["hostname"].(string); !ok {
		errs = append(errs, fmt.Errorf("webSocket: hostname not set"))
	}

	osProvider := svc.Deps.MustGetOsProvider()
	home, _ := osProvider.UserHomeDir()
	certPath := filepath.Join(home, ".keyop", "certs", "keyop-client.crt")
	keyPath := filepath.Join(home, ".keyop", "certs", "keyop-client.key")
	caPath := filepath.Join(home, ".keyop", "certs", "ca.crt")

	for _, p := range []string{certPath, keyPath, caPath} {
		if _, err := osProvider.Stat(p); err != nil {
			errs = append(errs, fmt.Errorf("webSocket: file not found: %s", p))
		}
	}

	return errs
}

func (svc *Service) Initialize() error {
	logger := svc.Deps.MustGetLogger()
	stateStore := svc.Deps.MustGetStateStore()

	// Load state
	if err := stateStore.Load(svc.Cfg.Name, &svc.state); err != nil {
		logger.Error("webSocket: failed to load state", "error", err)
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
	logger.Debug("webSocket: starting connection loop", "url", u.String())

	for {
		dialer := websocket.Dialer{
			TLSClientConfig: tlsConfig,
		}

		logger.Debug("webSocket: attempting to connect", "url", u.String())
		conn, _, err := dialer.Dial(u.String(), nil)
		if err != nil {
			logger.Error("webSocket: dial failed", "error", err)
			time.Sleep(5 * time.Second)
			continue
		}
		logger.Debug("webSocket: connected")

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

	logger.Info("webSocket: handling new connection")

	// Send Subscribe message with all channels we want
	var channels []string
	for _, sub := range svc.Cfg.Subs {
		channels = append(channels, sub.Name)
	}

	subscribeMsg := wsMessage{
		Type:     "subscribe",
		Channels: channels,
	}
	logger.Info("webSocket: sending subscribe message", "message", subscribeMsg)
	if err := conn.WriteJSON(subscribeMsg); err != nil {
		logger.Error("webSocket: failed to send subscribe", "error", err)
		return
	}

	// Send Resume messages for each queue we have state for
	svc.mu.Lock()
	for q, s := range svc.state {
		// Only resume if it's one of the channels we are currently interested in
		interested := false
		for _, ch := range channels {
			if ch == q {
				interested = true
				break
			}
		}
		if !interested {
			continue
		}

		resumeMsg := wsMessage{
			Type:     "resume",
			Queue:    q,
			FileName: s.FileName,
			Offset:   s.Offset,
		}
		if err := conn.WriteJSON(resumeMsg); err != nil {
			logger.Error("webSocket: failed to send resume", "error", err)
		}
	}
	svc.mu.Unlock()

	for {
		logger.Debug("webSocket: waiting for message")
		_, message, err := conn.ReadMessage()
		if err != nil {
			logger.Error("webSocket: read error", "error", err)
			return
		}
		logger.Debug("webSocket: received message", "raw", string(message))

		var msg wsMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			continue
		}

		if msg.Type == "message" {
			// At least once processing: process then ACK
			err := messenger.Send(msg.Payload)
			if err != nil {
				logger.Error("webSocket: failed to forward message", "error", err)
				// If we can't forward, do we ACK?
				// "at least once" means we should probably retry processing.
				// For now, let's not ACK if Send fails.
				continue
			}

			// Save state
			svc.mu.Lock()
			svc.state[msg.Queue] = queueState{
				FileName: msg.FileName,
				Offset:   msg.Offset,
			}
			stateStore := svc.Deps.MustGetStateStore()
			if err := stateStore.Save(svc.Cfg.Name, svc.state); err != nil {
				logger.Error("webSocket: failed to save state", "error", err)
			}
			svc.mu.Unlock()

			// Send ACK
			ack := wsMessage{Type: "ack"}
			if err := conn.WriteJSON(ack); err != nil {
				logger.Error("webSocket: failed to send ack", "error", err)
				return
			}
		}
	}
}

func (svc *Service) Check() error {
	return nil
}
