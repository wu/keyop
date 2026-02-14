package webSocketServer

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"keyop/core"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

type Service struct {
	Deps core.Dependencies
	Cfg  core.ServiceConfig
	Port int
}

func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	svc := &Service{
		Deps: deps,
		Cfg:  cfg,
	}

	if port, ok := cfg.Config["port"].(int); ok {
		svc.Port = port
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
		CheckOrigin: func(r *http.Request) bool { return true },
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

type wsMessage struct {
	Type     string       `json:"type"`
	Queue    string       `json:"queue,omitempty"`
	Channels []string     `json:"channels,omitempty"`
	FileName string       `json:"fileName,omitempty"`
	Offset   int64        `json:"offset,omitempty"`
	Payload  core.Message `json:"payload,omitempty"`
}

func (svc *Service) handleConnection(conn *websocket.Conn) {
	defer conn.Close()
	logger := svc.Deps.MustGetLogger()
	messenger := svc.Deps.MustGetMessenger()

	logger.Debug("webSocketServer: handling new connection")

	ctx, cancel := context.WithCancel(svc.Deps.MustGetContext())
	defer cancel()

	var mu sync.Mutex
	ackChan := make(chan struct{})

	readerName := "ws_" + uuid.New().String()
	logger.Debug("webSocketServer: starting connection loop", "readerName", readerName)

	// Receiver loop for ACKs, Resume, and Subscribe
	resumeChan := make(chan wsMessage, 10)
	subscribeChan := make(chan wsMessage, 1)
	go func() {
		defer cancel() // Cancel context if reader loop exits
		for {
			logger.Debug("webSocketServer: waiting for message")
			_, message, err := conn.ReadMessage()
			logger.Debug("webSocketServer: received message", "message", string(message))
			if err != nil {
				return
			}
			var msg wsMessage
			if err := json.Unmarshal(message, &msg); err != nil {
				continue
			}
			select {
			case <-ctx.Done():
				return
			default:
				if msg.Type == "ack" {
					select {
					case ackChan <- struct{}{}:
					case <-ctx.Done():
						return
					}
				} else if msg.Type == "resume" {
					select {
					case resumeChan <- msg:
					case <-ctx.Done():
						return
					}
				} else if msg.Type == "subscribe" {
					select {
					case subscribeChan <- msg:
					case <-ctx.Done():
						return
					}
				}
			}
		}
	}()

	// Wait for Subscribe message
	var subMsg wsMessage
	select {
	case subMsg = <-subscribeChan:
	case <-ctx.Done():
		return
	case <-time.After(10 * time.Second):
		logger.Debug("webSocketServer: timeout waiting for subscribe message")
		return
	}

	requestedChannels := make(map[string]bool)
	for _, ch := range subMsg.Channels {
		if ch != "" {
			requestedChannels[ch] = true
		}
	}

	// Filter requested channels by what the server is allowed to provide (if any)
	// If svc.Cfg.Subs is empty, we might allow any channel?
	// The original implementation used svc.Cfg.Subs to decide what to push.
	var channelsToSubscribe []string
	if len(svc.Cfg.Subs) > 0 {
		for _, sub := range svc.Cfg.Subs {
			if requestedChannels[sub.Name] {
				channelsToSubscribe = append(channelsToSubscribe, sub.Name)
			}
		}
	} else {
		// If server has no subs defined, maybe it allows anything requested?
		// For safety, let's assume it MUST be in server's Subs or we don't know MaxAge etc.
		logger.Debug("webSocketServer: no subscriptions defined in server config, nothing to push")
	}

	// Process any Resume messages that arrived
	resumes := make(map[string]wsMessage)
L:
	for {
		select {
		case rm := <-resumeChan:
			resumes[rm.Queue] = rm
		default:
			break L
		}
	}

	for _, chName := range channelsToSubscribe {
		subName := chName
		sub, ok := svc.Cfg.Subs[subName]
		if !ok {
			// Find by sub.Name
			for _, s := range svc.Cfg.Subs {
				if s.Name == subName {
					sub = s
					break
				}
			}
		}

		go func(sub core.ChannelInfo) {
			// Handle resume if provided for this queue
			if rm, ok := resumes[sub.Name]; ok && rm.FileName != "" {
				err := messenger.SetReaderState(sub.Name, readerName, rm.FileName, rm.Offset)
				if err != nil {
					logger.Error("webSocketServer: failed to set reader state", "error", err)
				}
			} else {
				// start from the end when no resume is provided:
				err := messenger.SeekToEnd(sub.Name, readerName)
				if err != nil {
					logger.Error("webSocketServer: failed to seek to end", "channel", sub.Name, "error", err)
				}
			}

			err := messenger.SubscribeExtended(ctx, readerName, sub.Name, svc.Cfg.Type, svc.Cfg.Name, sub.MaxAge, func(msg core.Message, fileName string, offset int64) error {
				return svc.sendAndWaitAck(ctx, conn, &mu, sub.Name, fileName, offset, msg, ackChan)
			})
			if err != nil {
				logger.Error("webSocketServer: subscribe failed", "error", err)
			}
		}(sub)
	}

	// Keep connection alive
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			mu.Lock()
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				mu.Unlock()
				return
			}
			mu.Unlock()
		}
	}
}

func (svc *Service) sendAndWaitAck(ctx context.Context, conn *websocket.Conn, mu *sync.Mutex, queueName string, fileName string, offset int64, msg core.Message, ackChan chan struct{}) error {
	wsMsg := wsMessage{
		Type:     "message",
		Queue:    queueName,
		FileName: fileName,
		Offset:   offset,
		Payload:  msg,
	}

	mu.Lock()
	err := conn.WriteJSON(wsMsg)
	mu.Unlock()
	if err != nil {
		return err
	}

	select {
	case <-ackChan:
		return nil
	case <-time.After(60 * time.Second):
		return fmt.Errorf("ack timeout")
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (svc *Service) Check() error {
	return nil
}
