package httpPostServer

import (
	"encoding/json"
	"fmt"
	"io"
	"keyop/core"
	"keyop/util"
	"net/http"
	"regexp"
)

var alphanumeric = regexp.MustCompile("^[a-zA-Z0-9]+$")

type Service struct {
	Deps      core.Dependencies
	Cfg       core.ServiceConfig
	Port      int
	targetDir string
}

func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	svc := &Service{
		Deps: deps,
		Cfg:  cfg,
	}

	port, portExists := svc.Cfg.Config["port"].(int)
	if portExists {
		svc.Port = port
	}

	targetDir, targetDirExists := svc.Cfg.Config["targetDir"].(string)
	if targetDirExists {
		svc.targetDir = targetDir
	}

	return svc
}

func (svc Service) ValidateConfig() []error {
	logger := svc.Deps.MustGetLogger()
	var errs []error

	pubErrs := util.ValidateConfig("pubs", svc.Cfg.Pubs, []string{"errors"}, logger)
	if len(pubErrs) > 0 {
		errs = append(errs, pubErrs...)
	}

	// check port
	_, portExists := svc.Cfg.Config["port"].(int)
	if !portExists {
		err := fmt.Errorf("httpPostServer: port not set in config")
		logger.Error(err.Error())
		errs = append(errs, err)
	}

	// check targetDir
	_, targetDirExists := svc.Cfg.Config["targetDir"].(string)
	if !targetDirExists {
		err := fmt.Errorf("httpPostServer: targetDir not set in config")
		logger.Error(err.Error())
		errs = append(errs, err)
	}

	return errs
}

func (svc Service) Initialize() error {
	logger := svc.Deps.MustGetLogger()
	osProvider := svc.Deps.MustGetOsProvider()

	// Create target directory if it doesn't exist
	if err := osProvider.MkdirAll(svc.targetDir, 0755); err != nil {
		logger.Error("failed to create target directory", "error", err, "targetDir", svc.targetDir)
		return err
	}
	logger.Info("target directory ready", "targetDir", svc.targetDir)

	mux := http.NewServeMux()
	mux.HandleFunc("/", svc.ServeHTTP)

	addr := fmt.Sprintf(":%d", svc.Port)
	logger.Info("starting http server", "addr", addr)

	go func() {
		if err := http.ListenAndServe(addr, mux); err != nil {
			logger.Error("http server failed", "error", err)
		}
	}()

	return nil
}

func (svc Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	logger := svc.Deps.MustGetLogger()
	messenger := svc.Deps.MustGetMessenger()

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		logger.Error("failed to read request body", "error", err)
		http.Error(w, "Error reading body", http.StatusInternalServerError)
		return
	}
	//goland:noinspection GoUnhandledErrorResult
	defer r.Body.Close()

	var msg core.Message
	if err := json.Unmarshal(body, &msg); err != nil {
		logger.Error("failed to unmarshal json", "error", err, "body", string(body))
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	logger.Debug("received json message", "message", msg)
	if msg.ChannelName == "" || !alphanumeric.MatchString(msg.ChannelName) {
		logger.Info("Missing or invalid ChannelName")
		http.Error(w, "Missing or invalid ChannelName", http.StatusBadRequest)
		return
	}

	err = messenger.Send(msg)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (svc Service) Check() error {
	return nil
}
