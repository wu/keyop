package ollama

import (
	"context"
	"fmt"
	"keyop/core"
	"keyop/util"
	"strings"
	"sync"
	"time"
)

type Service struct {
	Deps          core.Dependencies
	Cfg           core.ServiceConfig
	Host          string
	Port          int
	Model         string
	BatchSize     int
	Timeout       time.Duration
	HighWaterMark int
	LowWaterMark  int
	Guidelines    string
	Messages      []Message
	Client        *Client
	Mu            sync.Mutex
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
		Deps:          deps,
		Cfg:           cfg,
		Host:          "localhost",
		Port:          11434,
		Model:         "llama3.3",
		BatchSize:     100,
		Timeout:       60 * time.Second,
		HighWaterMark: 20,
		LowWaterMark:  10,
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
	if hwm, ok := cfg.Config["highWaterMark"].(int); ok {
		svc.HighWaterMark = hwm
	}
	if lwm, ok := cfg.Config["lowWaterMark"].(int); ok {
		svc.LowWaterMark = lwm
	}
	if guidelines, ok := cfg.Config["guidelines"].(string); ok {
		svc.Guidelines = guidelines
	}

	svc.Client = NewClient(svc.Host, svc.Port, svc.Timeout)

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
	state := svc.Deps.MustGetStateStore()

	// Load history from state store
	err := state.Load(fmt.Sprintf("%s_history", svc.Cfg.Name), &svc.Messages)
	if err != nil {
		logger.Error("ollama: failed to load history", "error", err)
	}

	sub, ok := svc.Cfg.Subs["requests"]
	if !ok {
		return fmt.Errorf("ollama: requests subscription not found")
	}

	err = messenger.Subscribe(svc.Deps.MustGetContext(), svc.Cfg.Name, sub.Name, svc.Cfg.Type, svc.Cfg.Name, sub.MaxAge, svc.messageHandler)
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

	if msg.Text == "" {
		logger.Warn("ollama: empty request received")
		return nil
	}

	svc.Mu.Lock()
	svc.Messages = append(svc.Messages, Message{Role: "user", Content: msg.Text})

	pub, ok := svc.Cfg.Pubs["responses"]
	if !ok {
		svc.Mu.Unlock()
		return fmt.Errorf("ollama: responses publication not found")
	}

	// Check if history needs summarization
	if len(svc.Messages) >= svc.HighWaterMark {
		logger.Info("ollama: history high water mark reached, summarizing", "highWaterMark", svc.HighWaterMark, "lowWaterMark", svc.LowWaterMark)

		// Send notification that summarization is happening
		svc.sendBatch(messenger, pub.Name, fmt.Sprintf("Summarizing conversation history for %s...", svc.Cfg.Name))

		// Adjust summarization if guidelines are present (keep guidelines, summarize after them)
		hasGuidelines := len(svc.Messages) > 0 && svc.Messages[0].Role == "system"
		startIndex := 0
		if hasGuidelines {
			startIndex = 1
		}

		lowWaterMark := svc.LowWaterMark
		if hasGuidelines && lowWaterMark > 0 {
			lowWaterMark--
		}

		if len(svc.Messages) > startIndex+lowWaterMark {
			oldest := svc.Messages[startIndex : startIndex+lowWaterMark]
			remaining := svc.Messages[startIndex+lowWaterMark:]

			ctx, cancel := context.WithTimeout(context.Background(), svc.Timeout)
			summaryMsg, err := svc.Client.Summarize(ctx, svc.Model, oldest)
			cancel()

			if err != nil {
				logger.Error("ollama: failed to summarize history", "error", err)
			} else {
				if hasGuidelines {
					svc.Messages = append([]Message{svc.Messages[0], summaryMsg}, remaining...)
				} else {
					svc.Messages = append([]Message{summaryMsg}, remaining...)
				}
			}
		}
	}

	// Check if guidelines in history match config, and update if necessary
	if svc.Guidelines != "" {
		if len(svc.Messages) > 0 && svc.Messages[0].Role == "system" && !strings.Contains(svc.Messages[0].Content, "Summary of previous conversation:") {
			// Check if it's a guidelines message (we assume the first system message is the guidelines if it's not a summary)
			// If it doesn't match, update it
			if svc.Messages[0].Content != svc.Guidelines {
				svc.Messages[0].Content = svc.Guidelines
			}
		} else {
			// Prepend guidelines if not present at the very beginning
			svc.Messages = append([]Message{{Role: "system", Content: svc.Guidelines}}, svc.Messages...)
		}
	} else {
		// If guidelines are empty but exist in history, remove them if they are not a summary
		if len(svc.Messages) > 0 && svc.Messages[0].Role == "system" && !strings.Contains(svc.Messages[0].Content, "Summary of previous conversation:") {
			svc.Messages = svc.Messages[1:]
		}
	}

	messages := svc.Messages
	svc.Mu.Unlock()

	var batch strings.Builder
	charCount := 0

	ctx, cancel := context.WithTimeout(context.Background(), svc.Timeout)
	defer cancel()

	updatedMessages, err := svc.Client.Chat(ctx, svc.Model, messages, func(content string) error {
		batch.WriteString(content)
		charCount += len(content)

		if charCount >= svc.BatchSize {
			svc.sendBatch(messenger, pub.Name, batch.String())
			batch.Reset()
			charCount = 0
		}
		return nil
	})

	if err != nil {
		return fmt.Errorf("ollama: chat failed: %w", err)
	}

	if batch.Len() > 0 {
		svc.sendBatch(messenger, pub.Name, batch.String())
	}

	// Update history (remove assistant message added by Client.Chat before saving,
	// because we want to manage history ourselves or just use what Client.Chat returned)
	// Actually Client.Chat returns updatedMessages which INCLUDES the assistant response.
	svc.Mu.Lock()
	svc.Messages = updatedMessages
	state := svc.Deps.MustGetStateStore()
	err = state.Save(fmt.Sprintf("%s_history", svc.Cfg.Name), svc.Messages)
	if err != nil {
		logger.Error("ollama: failed to save history", "error", err)
	}
	svc.Mu.Unlock()

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
