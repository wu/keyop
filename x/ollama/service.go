package ollama

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"keyop/core"
	"keyop/util"
	"net/http"
	"strings"
	"sync"
	"time"
)

type Service struct {
	Deps      core.Dependencies
	Cfg       core.ServiceConfig
	Host      string
	Port      int
	Model     string
	BatchSize int
	Timeout   time.Duration
	Context   []int
	Mu        sync.Mutex
}

type OllamaRequest struct {
	Model   string `json:"model"`
	Prompt  string `json:"prompt"`
	Stream  bool   `json:"stream"`
	Context []int  `json:"context,omitempty"`
}

type OllamaResponse struct {
	Model     string    `json:"model"`
	CreatedAt time.Time `json:"created_at"`
	Response  string    `json:"response"`
	Done      bool      `json:"done"`
	Context   []int     `json:"context,omitempty"`
}

func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	svc := &Service{
		Deps:      deps,
		Cfg:       cfg,
		Host:      "localhost",
		Port:      11434,
		Model:     "llama3.3",
		BatchSize: 100,
		Timeout:   60 * time.Second,
	}

	if host, ok := cfg.Config["host"].(string); ok {
		svc.Host = host
	}
	if port, ok := cfg.Config["port"].(int); ok {
		svc.Port = port
	}
	if model, ok := cfg.Config["model"].(string); ok {
		svc.Model = model
	}
	if batchSize, ok := cfg.Config["batchSize"].(int); ok {
		svc.BatchSize = batchSize
	}
	if timeoutStr, ok := cfg.Config["timeout"].(string); ok {
		if timeout, err := time.ParseDuration(timeoutStr); err == nil {
			svc.Timeout = timeout
		}
	}

	return svc
}

func (svc *Service) ValidateConfig() []error {
	logger := svc.Deps.MustGetLogger()
	var errs []error

	if _, ok := svc.Cfg.Config["host"].(string); !ok {
		errs = append(errs, fmt.Errorf("ollama: host not set in config"))
	}
	if _, ok := svc.Cfg.Config["port"].(int); !ok {
		errs = append(errs, fmt.Errorf("ollama: port not set in config"))
	}

	pubErrs := util.ValidateConfig("pubs", svc.Cfg.Pubs, []string{"responses"}, logger)
	if len(pubErrs) > 0 {
		errs = append(errs, pubErrs...)
	}

	subErrs := util.ValidateConfig("subs", svc.Cfg.Subs, []string{"requests"}, logger)
	if len(subErrs) > 0 {
		errs = append(errs, subErrs...)
	}

	return errs
}

func (svc *Service) Initialize() error {
	messenger := svc.Deps.MustGetMessenger()
	logger := svc.Deps.MustGetLogger()

	sub, ok := svc.Cfg.Subs["requests"]
	if !ok {
		return fmt.Errorf("ollama: requests subscription not found")
	}

	err := messenger.Subscribe(svc.Cfg.Name, sub.Name, sub.MaxAge, svc.messageHandler)
	if err != nil {
		return fmt.Errorf("ollama: failed to subscribe to %s: %w", sub.Name, err)
	}

	logger.Info("ollama: initialized", "host", svc.Host, "port", svc.Port, "batchSize", svc.BatchSize)
	return nil
}

func (svc *Service) Check() error {
	return nil
}

func (svc *Service) messageHandler(msg core.Message) error {
	logger := svc.Deps.MustGetLogger()
	messenger := svc.Deps.MustGetMessenger()

	var ollamaReq OllamaRequest
	ollamaReq.Model = svc.Model

	svc.Mu.Lock()
	ollamaReq.Context = svc.Context
	svc.Mu.Unlock()

	if msg.Text != "" {
		ollamaReq.Prompt = msg.Text
		logger.Warn("ollama: received message with text", "text", msg.Text)
	}

	if ollamaReq.Prompt == "" {
		logger.Warn("ollama: empty request received")
		return nil
	}
	ollamaReq.Stream = true

	url := fmt.Sprintf("http://%s:%d/api/generate", svc.Host, svc.Port)
	reqBody, err := json.Marshal(ollamaReq)
	if err != nil {
		return fmt.Errorf("ollama: failed to marshal request: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), svc.Timeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(reqBody))
	if err != nil {
		return fmt.Errorf("ollama: failed to create http request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("ollama: failed to call ollama api: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ollama: api returned status %d", resp.StatusCode)
	}

	reader := bufio.NewReader(resp.Body)
	var fullResponse strings.Builder
	var batch strings.Builder
	charCount := 0

	pub, ok := svc.Cfg.Pubs["responses"]
	if !ok {
		return fmt.Errorf("ollama: responses publication not found")
	}

	var finalContext []int

	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("ollama: error reading stream: %w", err)
		}

		var ollamaResp OllamaResponse
		if err := json.Unmarshal(line, &ollamaResp); err != nil {
			logger.Error("ollama: failed to unmarshal stream response", "error", err, "line", string(line))
			continue
		}

		content := ollamaResp.Response

		batch.WriteString(content)
		fullResponse.WriteString(content)
		charCount += len(content)

		if charCount >= svc.BatchSize {
			svc.sendBatch(messenger, pub.Name, batch.String())
			batch.Reset()
			charCount = 0
		}

		if ollamaResp.Done {
			finalContext = ollamaResp.Context
			break
		}
	}

	if batch.Len() > 0 {
		svc.sendBatch(messenger, pub.Name, batch.String())
	}

	// Update context
	svc.Mu.Lock()
	defer svc.Mu.Unlock()
	if len(finalContext) > 0 {
		svc.Context = finalContext
	}

	return nil
}

func (svc *Service) sendBatch(messenger core.MessengerApi, channelName string, content string) {
	respMsg := core.Message{
		ChannelName: channelName,
		Text:        content,
		ServiceType: "ollama",
		ServiceName: svc.Cfg.Name,
	}
	_ = messenger.Send(respMsg)
}
