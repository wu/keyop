package httpPost

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"keyop/core"
	"keyop/util"
	"net/http"
	"time"
)

type Service struct {
	Deps     core.Dependencies
	Cfg      core.ServiceConfig
	Port     int
	Hostname string
	Timeout  time.Duration
}

func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	svc := &Service{
		Deps:    deps,
		Cfg:     cfg,
		Timeout: 30 * time.Second,
	}

	port, portExists := svc.Cfg.Config["port"].(int)
	if portExists {
		svc.Port = port
	}

	hostname, hostnameDirExists := svc.Cfg.Config["hostname"].(string)
	if hostnameDirExists {
		svc.Hostname = hostname
	}

	timeoutStr, timeoutExists := svc.Cfg.Config["timeout"].(string)
	if timeoutExists {
		timeout, err := time.ParseDuration(timeoutStr)
		if err == nil {
			svc.Timeout = timeout
		}
	} else {
		// default timeout
		svc.Timeout = 30 * time.Second
	}

	return svc
}

func (svc Service) ValidateConfig() []error {
	logger := svc.Deps.MustGetLogger()

	var errs []error

	subErrs := util.ValidateConfig("subs", svc.Cfg.Subs, []string{"temp"}, logger)
	errs = append(errs, subErrs...)

	// check port
	_, portExists := svc.Cfg.Config["port"].(int)
	if !portExists {
		err := fmt.Errorf("httpPostServer: port not set in config")
		logger.Error(err.Error())
		errs = append(errs, err)
	}

	// check hostname
	_, hostnameExists := svc.Cfg.Config["hostname"].(string)
	if !hostnameExists {
		err := fmt.Errorf("httpPostServer: hostname not set in config")
		logger.Error(err.Error())
		errs = append(errs, err)
	}

	return errs
}

func (svc Service) Initialize() error {

	messenger := svc.Deps.MustGetMessenger()

	// TODO: iterate through subscriptions and set up handlers
	return messenger.Subscribe(svc.Cfg.Name, svc.Cfg.Subs["heartbeat"].Name, svc.messageHandler)

	return nil
}

func (svc Service) Check() error {
	return nil
}

func (svc Service) messageHandler(msg core.Message) error {
	logger := svc.Deps.MustGetLogger()

	// process incoming message
	logger.Warn("httpPost: received temp message", "message", msg)

	// send message to HTTP endpoint
	url := fmt.Sprintf("http://%s:%d", svc.Hostname, svc.Port)

	jsonData, err := json.Marshal(msg)
	if err != nil {
		logger.Error("failed to marshal message to JSON", "error", err)
		return err
	}

	// TODO: strategy for retry on failure in messenger, return error here

	client := &http.Client{
		Timeout: svc.Timeout,
	}

	ctx, cancel := context.WithTimeout(context.Background(), svc.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(jsonData))
	if err != nil {
		logger.Error("failed to create HTTP request", "url", url, "error", err)
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		logger.Error("failed to post message to HTTP endpoint", "url", url, "error", err)
		return err
	}
	defer resp.Body.Close()

	logger.Info("successfully posted message to HTTP endpoint", "url", url, "status", resp.Status)

	return nil
}
