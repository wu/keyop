package kodi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"keyop/core"
	"keyop/util"
	"net/http"
)

type Service struct {
	Deps     core.Dependencies
	Cfg      core.ServiceConfig
	Host     string
	Port     int
	Username string
	Password string
}

type KodiState struct {
	CurrentTitle string `json:"current_title"`
}

func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	svc := &Service{
		Deps: deps,
		Cfg:  cfg,
	}

	if host, ok := svc.Cfg.Config["host"].(string); ok {
		svc.Host = host
	}

	if port, ok := svc.Cfg.Config["port"].(int); ok {
		svc.Port = port
	}

	username, ok := svc.Cfg.Config["username"].(string)
	if !ok {
		username = "kodi"
	}

	password, ok := svc.Cfg.Config["password"].(string)
	if !ok {
		password = "kodi"
	}

	svc.Username = username
	svc.Password = password

	return svc
}

func (svc *Service) ValidateConfig() []error {
	logger := svc.Deps.MustGetLogger()
	errs := util.ValidateConfig("pubs", svc.Cfg.Pubs, []string{"events"}, logger)

	if svc.Host == "" {
		err := fmt.Errorf("config field 'host' is required")
		logger.Error(err.Error())
		errs = append(errs, err)
	}

	// check port
	_, portExists := svc.Cfg.Config["port"].(int)
	if !portExists {
		err := fmt.Errorf("kodi: port not set in config or not an int")
		logger.Error(err.Error())
		errs = append(errs, err)
	}

	return errs
}

func (svc *Service) Initialize() error {
	return nil
}

type jsonRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
	ID      int         `json:"id"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result"`
	Error   *jsonRPCError   `json:"error"`
	ID      int             `json:"id"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type activePlayer struct {
	PlayerID int    `json:"playerid"`
	Type     string `json:"type"`
}

type itemDetails struct {
	Item struct {
		Title string `json:"title"`
		Label string `json:"label"`
		Type  string `json:"type"`
	} `json:"item"`
}

type playerProperties struct {
	Time struct {
		Hours        int `json:"hours"`
		Minutes      int `json:"minutes"`
		Seconds      int `json:"seconds"`
		Milliseconds int `json:"milliseconds"`
	} `json:"time"`
	TotalTime struct {
		Hours        int `json:"hours"`
		Minutes      int `json:"minutes"`
		Seconds      int `json:"seconds"`
		Milliseconds int `json:"milliseconds"`
	} `json:"totaltime"`
	Speed int `json:"speed"`
}

func (svc *Service) Check() error {
	logger := svc.Deps.MustGetLogger()
	messenger := svc.Deps.MustGetMessenger()
	stateStore := svc.Deps.MustGetStateStore()

	url := fmt.Sprintf("http://%s:%d/jsonrpc", svc.Host, svc.Port)

	// 1. Get current title
	var currentTitle string
	title, err := svc.getPlayingTitle(url, 1)
	if err != nil {
		logger.Error("Failed to get playing title", "error", err)
		return err
	}
	currentTitle = title

	// 2. Load previous state
	var prevState KodiState
	err = stateStore.Load(svc.Cfg.Name, &prevState)
	if err != nil {
		logger.Warn("Failed to load state", "error", err)
	}

	// 3. Get player properties if playing
	var playbackTime string
	if currentTitle != "" {
		var props playerProperties
		params := map[string]interface{}{
			"playerid":   1,
			"properties": []string{"time", "totaltime", "speed"},
		}
		err = svc.callKodi(url, "Player.GetProperties", params, &props)
		if err == nil {
			playbackTime = fmt.Sprintf("%02d:%02d:%02d", props.Time.Hours, props.Time.Minutes, props.Time.Seconds)
		} else {
			logger.Error("Failed to get player properties", "error", err)
		}
	}

	// 4. Compare and send events
	if currentTitle != prevState.CurrentTitle {
		if currentTitle != "" {
			// Movie started or changed
			logger.Info("Movie started", "title", currentTitle)
			err = messenger.Send(core.Message{
				ChannelName: svc.Cfg.Pubs["events"].Name,
				ServiceName: svc.Cfg.Name,
				ServiceType: svc.Cfg.Type,
				Text:        fmt.Sprintf("Movie started: %s", currentTitle),
				Data:        map[string]string{"title": currentTitle, "status": "playing", "time": playbackTime},
			})
		} else {
			// Movie stopped
			logger.Info("Movie stopped", "previous_title", prevState.CurrentTitle)
			err = messenger.Send(core.Message{
				ChannelName: svc.Cfg.Pubs["events"].Name,
				ServiceName: svc.Cfg.Name,
				ServiceType: svc.Cfg.Type,
				Text:        fmt.Sprintf("Movie stopped: %s", prevState.CurrentTitle),
				Data:        map[string]string{"title": prevState.CurrentTitle, "status": "stopped"},
			})
		}

		if err != nil {
			logger.Error("Failed to send event", "error", err)
		}

		// 5. Save new state
		prevState.CurrentTitle = currentTitle
		err = stateStore.Save(svc.Cfg.Name, prevState)
		if err != nil {
			logger.Error("Failed to save state", "error", err)
		}
	} else if currentTitle != "" {
		// Movie still playing, send update with time
		logger.Debug("Movie still playing", "title", currentTitle, "time", playbackTime)
		err = messenger.Send(core.Message{
			ChannelName: svc.Cfg.Pubs["events"].Name,
			ServiceName: svc.Cfg.Name,
			ServiceType: svc.Cfg.Type,
			Text:        fmt.Sprintf("Movie playing: %s (%s)", currentTitle, playbackTime),
			Data:        map[string]string{"title": currentTitle, "status": "playing", "time": playbackTime},
		})
		if err != nil {
			logger.Error("Failed to send playing update", "error", err)
		}
	}

	return nil
}

func (svc *Service) callKodi(url string, method string, params interface{}, result interface{}) error {
	logger := svc.Deps.MustGetLogger()

	reqBody := jsonRPCRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
		ID:      1,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	logger.Debug("calling kodi", "url", url, "request", string(body))
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(svc.Username, svc.Password)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("kodi returned status %d", resp.StatusCode)
	}

	respData, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	var rpcResp jsonRPCResponse
	if err := json.Unmarshal(respData, &rpcResp); err != nil {
		return err
	}

	if rpcResp.Error != nil {
		return fmt.Errorf("kodi rpc error: %s (code: %d)", rpcResp.Error.Message, rpcResp.Error.Code)
	}

	return json.Unmarshal(rpcResp.Result, result)
}

func (svc *Service) getPlayingTitle(url string, playerID int) (string, error) {

	var details itemDetails
	params := map[string]interface{}{
		"playerid":   playerID,
		"properties": []string{"title"},
	}
	err := svc.callKodi(url, "Player.GetItem", params, &details)
	if err != nil {
		return "", err
	}

	title := details.Item.Title
	if title == "" {
		title = details.Item.Label
	}
	return title, nil
}
