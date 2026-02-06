package github

import (
	"encoding/json"
	"fmt"
	"io"
	"keyop/core"
	"keyop/util"
	"net/http"
	"time"
)

type Service struct {
	Deps      core.Dependencies
	Cfg       core.ServiceConfig
	lastCheck time.Time
	BaseURL   string
}

func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	return &Service{
		Deps: deps,
		Cfg:  cfg,
		// Initially check for notifications since now.
		// If we want to get all unread on first run, we could leave it as zero.
		// GitHub API's 'since' parameter helps.
		lastCheck: time.Now().Add(-1 * time.Hour),
		BaseURL:   "https://api.github.com",
	}
}

func (svc *Service) ValidateConfig() []error {
	logger := svc.Deps.MustGetLogger()
	errs := util.ValidateConfig("pubs", svc.Cfg.Pubs, []string{"alerts"}, logger)

	token, _ := svc.Cfg.Config["token"].(string)
	if token == "" {
		err := fmt.Errorf("github token is required in config")
		logger.Error(err.Error())
		errs = append(errs, err)
	}

	return errs
}

func (svc *Service) Initialize() error {
	stateStore := svc.Deps.MustGetStateStore()
	var lastCheck time.Time
	err := stateStore.Load(svc.Cfg.Name, &lastCheck)
	if err != nil {
		svc.Deps.MustGetLogger().Error("Failed to load github service state", "error", err)
	}
	if !lastCheck.IsZero() {
		svc.lastCheck = lastCheck
	}
	return nil
}

type Notification struct {
	ID      string `json:"id"`
	Subject struct {
		Title string `json:"title"`
		URL   string `json:"url"`
		Type  string `json:"type"`
	} `json:"subject"`
	Reason     string    `json:"reason"`
	UpdatedAt  time.Time `json:"updated_at"`
	LastReadAt time.Time `json:"last_read_at"`
}

func (svc *Service) Check() error {
	logger := svc.Deps.MustGetLogger()
	messenger := svc.Deps.MustGetMessenger()

	token, _ := svc.Cfg.Config["token"].(string)

	url := svc.BaseURL + "/notifications"
	// Only fetch notifications updated since our last check
	if !svc.lastCheck.IsZero() {
		url = fmt.Sprintf("%s?since=%s", url, svc.lastCheck.Format(time.RFC3339))
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("github api returned status %d: %s", resp.StatusCode, string(body))
	}

	var notifications []Notification
	if err := json.NewDecoder(resp.Body).Decode(&notifications); err != nil {
		return err
	}

	newLastCheck := svc.lastCheck
	for _, n := range notifications {
		if n.UpdatedAt.After(svc.lastCheck) {
			logger.Info("New GitHub notification found", "title", n.Subject.Title)

			err := messenger.Send(core.Message{
				ChannelName: svc.Cfg.Pubs["alerts"].Name,
				ServiceName: svc.Cfg.Name,
				ServiceType: svc.Cfg.Type,
				Text:        fmt.Sprintf("GitHub Notification: %s", n.Subject.Title),
				Data:        n,
			})
			if err != nil {
				logger.Error("Failed to send github notification alert", "error", err)
			}

			if n.UpdatedAt.After(newLastCheck) {
				newLastCheck = n.UpdatedAt
			}
		}
	}

	if newLastCheck.After(svc.lastCheck) {
		svc.lastCheck = newLastCheck
		stateStore := svc.Deps.MustGetStateStore()
		if err := stateStore.Save(svc.Cfg.Name, svc.lastCheck); err != nil {
			logger.Error("Failed to save github service state", "error", err)
		}
	}

	return nil
}
