package owntracks

import (
	"encoding/json"
	"fmt"
	"io"
	"keyop/core"
	"keyop/util"
	"net/http"
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

	port, portExists := svc.Cfg.Config["port"].(int)
	if portExists {
		svc.Port = port
	}

	return svc
}

func (svc Service) ValidateConfig() []error {
	logger := svc.Deps.MustGetLogger()
	var errs []error

	pubErrs := util.ValidateConfig("pubs", svc.Cfg.Pubs, []string{"owntracks"}, logger)
	if len(pubErrs) > 0 {
		errs = append(errs, pubErrs...)
	}

	// check port
	_, portExists := svc.Cfg.Config["port"].(int)
	if !portExists {
		err := fmt.Errorf("owntracks: port not set in config")
		logger.Error(err.Error())
		errs = append(errs, err)
	}

	return errs
}

func (svc Service) Initialize() error {
	logger := svc.Deps.MustGetLogger()

	mux := http.NewServeMux()
	mux.HandleFunc("/", svc.ServeHTTP)

	addr := fmt.Sprintf(":%d", svc.Port)
	logger.Info("starting owntracks http server", "addr", addr)

	go func() {
		if err := http.ListenAndServe(addr, mux); err != nil {
			logger.Error("owntracks http server failed", "error", err)
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

	var data map[string]interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		logger.Error("failed to unmarshal owntracks json", "error", err, "body", string(body))
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	logger.Error("received owntracks message", "data", data)

	msg := core.Message{
		ChannelName: "owntracks",
		Data:        data,
	}

	err = messenger.Send(msg)
	if err != nil {
		logger.Error("failed to send owntracks message", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (svc Service) Check() error {
	return nil
}
