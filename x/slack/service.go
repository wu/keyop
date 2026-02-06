package slack

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"keyop/core"
	"keyop/util"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Service struct {
	Deps         core.Dependencies
	Cfg          core.ServiceConfig
	lastCheck    time.Time
	BaseURL      string
	appToken     string
	botToken     string
	appID        string
	conn         *websocket.Conn
	mu           sync.Mutex
	channelCache map[string]string
	userCache    map[string]string
	cacheMu      sync.RWMutex
}

func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	return &Service{
		Deps:         deps,
		Cfg:          cfg,
		lastCheck:    time.Now(),
		BaseURL:      "https://slack.com/api",
		channelCache: make(map[string]string),
		userCache:    make(map[string]string),
	}
}

func (svc *Service) ValidateConfig() []error {
	logger := svc.Deps.MustGetLogger()
	var errs []error

	pubErrs := util.ValidateConfig("pubs", svc.Cfg.Pubs, []string{"alerts"}, logger)
	errs = append(errs, pubErrs...)

	subErrs := util.ValidateConfig("subs", svc.Cfg.Subs, []string{"alerts"}, logger)
	errs = append(errs, subErrs...)

	botToken, _ := svc.Cfg.Config["token"].(string)
	if botToken == "" {
		err := fmt.Errorf("slack token (bot token) is required in config")
		logger.Error(err.Error())
		errs = append(errs, err)
	}

	appToken, _ := svc.Cfg.Config["appToken"].(string)
	if appToken == "" {
		err := fmt.Errorf("slack appToken is required for socket mode")
		logger.Error(err.Error())
		errs = append(errs, err)
	}

	channelID, _ := svc.Cfg.Config["channelID"].(string)
	if channelID == "" {
		err := fmt.Errorf("slack channelID is required in config")
		logger.Error(err.Error())
		errs = append(errs, err)
	}

	appID, _ := svc.Cfg.Config["appID"].(string)
	if appID == "" {
		err := fmt.Errorf("slack appID is required in config (used to filter own messages)")
		logger.Error(err.Error())
		errs = append(errs, err)
	}

	return errs
}

func (svc *Service) Initialize() error {
	svc.botToken, _ = svc.Cfg.Config["token"].(string)
	svc.appToken, _ = svc.Cfg.Config["appToken"].(string)
	svc.appID, _ = svc.Cfg.Config["appID"].(string)

	messenger := svc.Deps.MustGetMessenger()
	err := messenger.Subscribe(svc.Cfg.Name, svc.Cfg.Subs["alerts"].Name, svc.Cfg.Subs["alerts"].MaxAge, svc.messageHandler)
	if err != nil {
		return err
	}

	stateStore := svc.Deps.MustGetStateStore()
	var lastCheck time.Time
	err = stateStore.Load(svc.Cfg.Name, &lastCheck)
	if err != nil {
		svc.Deps.MustGetLogger().Error("Failed to load slack service state", "error", err)
	}
	if !lastCheck.IsZero() {
		svc.lastCheck = lastCheck
	}

	return nil
}

func (svc *Service) messageHandler(msg core.Message) error {
	logger := svc.Deps.MustGetLogger()
	if msg.Text == "" {
		return nil
	}

	// Avoid forwarding messages that we ourselves published to Slack
	if msg.ServiceName == svc.Cfg.Name {
		return nil
	}

	logger.Info("Forwarding message to Slack", "text", msg.Text)

	channelID, _ := svc.Cfg.Config["channelID"].(string)

	payload := map[string]interface{}{
		"channel": channelID,
		"text":    msg.Text,
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequest("POST", svc.BaseURL+"/chat.postMessage", bytes.NewBuffer(body))
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+svc.botToken)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("slack api returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var slackResp struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&slackResp); err != nil {
		return err
	}

	if !slackResp.OK {
		return fmt.Errorf("slack api error: %s", slackResp.Error)
	}

	return nil
}

func (svc *Service) getChannelName(channelID string) string {
	if channelID == "" {
		return "unknown"
	}

	svc.cacheMu.RLock()
	name, ok := svc.channelCache[channelID]
	svc.cacheMu.RUnlock()
	if ok {
		return name
	}

	logger := svc.Deps.MustGetLogger()
	ctx := svc.Deps.MustGetContext()

	url := fmt.Sprintf("%s/conversations.info?channel=%s", svc.BaseURL, channelID)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		logger.Error("Failed to create request for conversations.info", "error", err)
		return channelID
	}
	req.Header.Set("Authorization", "Bearer "+svc.botToken)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		logger.Error("Failed to call conversations.info", "error", err)
		return channelID
	}
	defer resp.Body.Close()

	var infoResp struct {
		OK      bool   `json:"ok"`
		Error   string `json:"error"`
		Channel struct {
			Name string `json:"name"`
		} `json:"channel"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&infoResp); err != nil {
		logger.Error("Failed to decode conversations.info response", "error", err)
		return channelID
	}

	if !infoResp.OK {
		logger.Error("Slack conversations.info error", "error", infoResp.Error, "channelID", channelID)
		return channelID
	}

	svc.cacheMu.Lock()
	svc.channelCache[channelID] = infoResp.Channel.Name
	svc.cacheMu.Unlock()

	return infoResp.Channel.Name
}

func (svc *Service) getUserName(userID string) string {
	if userID == "" {
		return "unknown"
	}

	svc.cacheMu.RLock()
	name, ok := svc.userCache[userID]
	svc.cacheMu.RUnlock()
	if ok {
		return name
	}

	logger := svc.Deps.MustGetLogger()
	ctx := svc.Deps.MustGetContext()

	url := fmt.Sprintf("%s/users.info?user=%s", svc.BaseURL, userID)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		logger.Error("Failed to create request for users.info", "error", err)
		return userID
	}
	req.Header.Set("Authorization", "Bearer "+svc.botToken)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		logger.Error("Failed to call users.info", "error", err)
		return userID
	}
	defer resp.Body.Close()

	var infoResp struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
		User  struct {
			Name     string `json:"name"`
			RealName string `json:"real_name"`
		} `json:"user"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&infoResp); err != nil {
		logger.Error("Failed to decode users.info response", "error", err)
		return userID
	}

	if !infoResp.OK {
		logger.Error("Slack users.info error", "error", infoResp.Error, "userID", userID)
		return userID
	}

	userName := infoResp.User.RealName
	if userName == "" {
		userName = infoResp.User.Name
	}
	if userName == "" {
		userName = userID
	}

	svc.cacheMu.Lock()
	svc.userCache[userID] = userName
	svc.cacheMu.Unlock()

	return userName
}

type slackMessage struct {
	Type    string `json:"type"`
	User    string `json:"user"`
	Text    string `json:"text"`
	TS      string `json:"ts"`
	BotID   string `json:"bot_id"`
	AppId   string `json:"app_id"`
	Channel string `json:"channel"`
}

func (svc *Service) Check() error {
	logger := svc.Deps.MustGetLogger()
	ctx := svc.Deps.MustGetContext()
	messenger := svc.Deps.MustGetMessenger()

	// 1. Get WebSocket URL
	req, err := http.NewRequestWithContext(ctx, "POST", svc.BaseURL+"/apps.connections.open", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+svc.appToken)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("slack apps.connections.open returned status %d: %s", resp.StatusCode, string(body))
	}

	var openResp struct {
		OK    bool   `json:"ok"`
		URL   string `json:"url"`
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&openResp); err != nil {
		return err
	}
	if !openResp.OK {
		return fmt.Errorf("slack apps.connections.open error: %s", openResp.Error)
	}

	// 2. Connect to WebSocket
	dialer := websocket.DefaultDialer
	conn, _, err := dialer.DialContext(ctx, openResp.URL, nil)
	if err != nil {
		return fmt.Errorf("failed to connect to slack websocket: %w", err)
	}
	defer conn.Close()

	svc.mu.Lock()
	svc.conn = conn
	svc.mu.Unlock()

	defer func() {
		svc.mu.Lock()
		svc.conn = nil
		svc.mu.Unlock()
	}()

	logger.Info("Connected to Slack Socket Mode")

	// Start a goroutine to close the connection if the context is cancelled
	go func() {
		<-ctx.Done()
		conn.Close()
	}()

	// 3. Receive messages
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			// If context is done, this is a normal shutdown
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			return fmt.Errorf("websocket read error: %w", err)
		}

		logger.Debug("Received message from Slack Socket Mode", "message", string(message))

		var envelope struct {
			Type       string          `json:"type"`
			EnvelopeID string          `json:"envelope_id"`
			Payload    json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal(message, &envelope); err != nil {
			logger.Error("Failed to unmarshal slack envelope", "error", err)
			continue
		}

		// Acknowledge the message
		if envelope.EnvelopeID != "" {
			ack := map[string]string{"envelope_id": envelope.EnvelopeID}
			ackBytes, _ := json.Marshal(ack)
			if err := conn.WriteMessage(websocket.TextMessage, ackBytes); err != nil {
				logger.Error("Failed to send ack to slack", "error", err)
			}
		}

		if envelope.Type == "events_api" {
			var eventsPayload struct {
				Event slackMessage `json:"event"`
			}
			if err := json.Unmarshal(envelope.Payload, &eventsPayload); err != nil {
				logger.Error("Failed to unmarshal slack event payload", "error", err)
				continue
			}

			m := eventsPayload.Event
			if m.Type == "message" && m.AppId != svc.appID && m.Text != "" {
				logger.Warn("New Slack message received via Socket Mode", "user", m.User, "text", m.Text, "channel", m.Channel)

				channelName := svc.getChannelName(m.Channel)
				userName := svc.getUserName(m.User)

				err := messenger.Send(core.Message{
					ChannelName: svc.Cfg.Pubs["alerts"].Name,
					ServiceName: svc.Cfg.Name,
					ServiceType: svc.Cfg.Type,
					Text:        fmt.Sprintf("Slack [#%s] [%s]: %s", channelName, userName, m.Text),
					Data:        m,
				})
				if err != nil {
					logger.Error("Failed to send slack message to alerts channel", "error", err)
				}
			}
		} else if envelope.Type == "hello" {
			logger.Debug("Received hello from Slack")
		} else if envelope.Type == "disconnect" {
			logger.Info("Received disconnect request from Slack")
			return nil // Return nil so the kernel restarts the check loop
		}
	}
}
