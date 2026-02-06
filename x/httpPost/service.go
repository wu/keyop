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

	// check port
	_, portExists := svc.Cfg.Config["port"].(int)
	if !portExists {
		err := fmt.Errorf("httpPost: port not set in config")
		logger.Error(err.Error())
		errs = append(errs, err)
	}

	// check hostname
	_, hostnameExists := svc.Cfg.Config["hostname"].(string)
	if !hostnameExists {
		err := fmt.Errorf("httpPost: hostname not set in config")
		logger.Error(err.Error())
		errs = append(errs, err)
	}

	pubErrs := util.ValidateConfig("pubs", svc.Cfg.Pubs, []string{"errors"}, logger)
	if len(pubErrs) > 0 {
		errs = append(errs, pubErrs...)
	}

	// validate subscriptions
	if svc.Cfg.Subs == nil {
		err := fmt.Errorf("httpPost: no subscriptions defined in config")
		logger.Error(err.Error())
		errs = append(errs, err)
		return errs
	}

	return errs
}

func (svc Service) Initialize() error {

	messenger := svc.Deps.MustGetMessenger()

	var errs []error

	logger := svc.Deps.MustGetLogger()

	logger.Error("httpPost: initializing service", "conf", svc.Cfg)

	for name, sub := range svc.Cfg.Subs {
		logger.Error("httpPost: initializing subscription", "name", name, "topic", sub.Name, "maxAge", sub.MaxAge)
		err := messenger.Subscribe(svc.Cfg.Name, sub.Name, sub.MaxAge, svc.messageHandler)
		if err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("httpPost: failed to initialize subscriptions: %v", errs)
	}
	return nil
}

func (svc Service) Check() error {
	return nil
}

func (svc Service) messageHandler(msg core.Message) error {
	logger := svc.Deps.MustGetLogger()

	// process incoming message
	logger.Info("httpPost: forwarding message", "channel", msg.ChannelName, "message", msg)

	// send message to HTTP endpoint
	url := fmt.Sprintf("http://%s:%d", svc.Hostname, svc.Port)

	jsonData, err := json.Marshal(msg)
	if err != nil {
		logger.Error("failed to marshal message to JSON", "error", err)
		return err
	}

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
	//goland:noinspection GoUnhandledErrorResult
	defer resp.Body.Close()

	logger.Debug("successfully posted message to HTTP endpoint", "url", url, "status", resp.Status)

	return nil
}
