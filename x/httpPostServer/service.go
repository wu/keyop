package httpPostServer

import (
	"encoding/json"
	"fmt"
	"io"
	"keyop/core"
	"net/http"
	"os"
	"regexp"
	"time"
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
	defer r.Body.Close()

	var msg map[string]interface{}
	if err := json.Unmarshal(body, &msg); err != nil {
		logger.Error("failed to unmarshal json", "error", err, "body", string(body))
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	logger.Debug("received json message", "message", msg)
	serviceName, serviceNameExists := msg["ServiceName"].(string)
	if !serviceNameExists || !alphanumeric.MatchString(serviceName) {
		logger.Info("Missing or invalid ServiceName")
		http.Error(w, "Missing or invalid ServiceName", http.StatusBadRequest)
		return
	}

	today := time.Now().Format("20060102")
	filename := fmt.Sprintf("%s/%s_%s.json", svc.targetDir, serviceName, today)
	osProvider := svc.Deps.MustGetOsProvider()
	f, err := osProvider.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		logger.Error("failed to open file for appending", "error", err, "filename", filename)
		http.Error(w, "failed to open file for appending", http.StatusInternalServerError)
		return
	} else {
		if _, err := f.Write(body); err != nil {
			logger.Error("failed to write json to file", "error", err, "filename", filename)
			http.Error(w, "failed to write json to file", http.StatusInternalServerError)
			return
		} else {
			if _, err := f.WriteString("\n"); err != nil {
				logger.Error("failed to write newline to file", "error", err, "filename", filename)
				http.Error(w, "failed to write newline to file", http.StatusInternalServerError)
				return
			}
			logger.Info("logged json to file", "filename", filename)
		}
		f.Close()
	}

	w.WriteHeader(http.StatusOK)
}

func (svc Service) Check() error {
	return nil
}
