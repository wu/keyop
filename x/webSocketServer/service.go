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

	activeSubs := make(map[string]context.CancelFunc)
	activeSubsMu := sync.Mutex{}

	// Receiver loop for ACKs, Resume, and Subscribe
	msgChan := make(chan wsMessage, 20)
	go func() {
		defer cancel() // Cancel context if reader loop exits
		for {
			logger.Debug("webSocketServer: waiting for message")
			_, message, err := conn.ReadMessage()
			if err != nil {
				return
			}
			logger.Debug("webSocketServer: received message", "message", string(message))
			var msg wsMessage
			if err := json.Unmarshal(message, &msg); err != nil {
				continue
			}

			if msg.Type == "ack" {
				select {
				case ackChan <- struct{}{}:
				case <-ctx.Done():
					return
				}
			} else {
				select {
				case msgChan <- msg:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	resumesMu := sync.Mutex{}
	resumes := make(map[string]wsMessage)
	lastPing := time.Now()

	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-msgChan:
			if msg.Type == "resume" {
				resumesMu.Lock()
				resumes[msg.Queue] = msg
				resumesMu.Unlock()
				logger.Debug("webSocketServer: received resume", "queue", msg.Queue, "file", msg.FileName, "offset", msg.Offset)
			} else if msg.Type == "subscribe" {
				// Process subscribe
				subMsg := msg
				requestedChannels := make(map[string]bool)
				for _, ch := range subMsg.Channels {
					if ch != "" {
						requestedChannels[ch] = true
					}
				}

				var channelsToSubscribe []core.ChannelInfo
				if len(svc.Cfg.Subs) > 0 {
					for _, sub := range svc.Cfg.Subs {
						if requestedChannels[sub.Name] {
							channelsToSubscribe = append(channelsToSubscribe, sub)
						}
					}
				}

				activeSubsMu.Lock()
				// Stop channels no longer requested
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

				// Start new channels
				for _, sub := range channelsToSubscribe {
					if _, active := activeSubs[sub.Name]; !active {
						subCtx, subCancel := context.WithCancel(ctx)
						activeSubs[sub.Name] = subCancel

						go func(s core.ChannelInfo, sCtx context.Context, sCancel context.CancelFunc) {
							// Handle resume if provided for this queue
							resumesMu.Lock()
							rm, ok := resumes[s.Name]
							if ok {
								delete(resumes, s.Name) // Use it once
							}
							resumesMu.Unlock()

							if ok && rm.FileName != "" {
								logger.Debug("webSocketServer: resuming subscription", "channel", s.Name, "file", rm.FileName, "offset", rm.Offset)
								// Check if the reader already has THIS state or LATER.
								// Since we use ephemeral reader name for each connection, it should be empty initially.
								err := messenger.SetReaderState(s.Name, readerName, rm.FileName, rm.Offset)
								if err != nil {
									logger.Error("webSocketServer: failed to set reader state", "error", err)
								}
								// IMPORTANT: When resuming, SubscribeExtended will start reading FROM the given offset.
								// If the offset was the position of a message that was ALREADY processed but NOT acknowledged,
								// it will be sent again. This is "at least once" delivery.
							} else {
								// start from the end when no resume is provided:
								logger.Debug("webSocketServer: seeking to end for new subscription", "channel", s.Name)
								err := messenger.SeekToEnd(s.Name, readerName)
								if err != nil {
									logger.Error("webSocketServer: failed to seek to end", "channel", s.Name, "error", err)
								}
							}

							err := messenger.SubscribeExtended(sCtx, readerName, s.Name, svc.Cfg.Type, svc.Cfg.Name, s.MaxAge, func(msg core.Message, fileName string, offset int64) error {
								logger.Debug("webSocketServer: sending message", "channel", s.Name, "uuid", msg.Uuid)
								return svc.sendAndWaitAck(sCtx, conn, &mu, s.Name, fileName, offset, msg, ackChan)
							})
							if err != nil && err != context.Canceled {
								logger.Error("webSocketServer: subscribe failed", "channel", s.Name, "error", err)
							}
						}(sub, subCtx, subCancel)
					}
				}
				activeSubsMu.Unlock()
			}

		case <-time.After(1 * time.Second):
			// Ping connection every 30 seconds
			if time.Since(lastPing) > 30*time.Second {
				mu.Lock()
				if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					mu.Unlock()
					return
				}
				mu.Unlock()
				lastPing = time.Now()
			}
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
