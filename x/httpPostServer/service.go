package httpPostServer

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"keyop/core"
	"keyop/util"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
)

var alphanumeric = regexp.MustCompile("^[a-zA-Z0-9]+$")

type Service struct {
	Deps      core.Dependencies
	Cfg       core.ServiceConfig
	Port      int
	targetDir string
	hostname  string
}

func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	logger := deps.MustGetLogger()

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

	hostname, err := util.GetShortHostname(svc.Deps.MustGetOsProvider())
	if err != nil {
		logger.Error("httpPostServer: failed to get hostname", "error", err)
	}
	svc.hostname = hostname

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

	// check for TLS certificates
	osProvider := svc.Deps.MustGetOsProvider()
	home, err := osProvider.UserHomeDir()
	if err != nil {
		err := fmt.Errorf("httpPostServer: failed to get user home directory: %w", err)
		logger.Error(err.Error())
		errs = append(errs, err)
	} else {
		certPath := filepath.Join(home, ".keyop", "certs", "keyop-server.crt")
		keyPath := filepath.Join(home, ".keyop", "certs", "keyop-server.key")
		caPath := filepath.Join(home, ".keyop", "certs", "ca.crt")

		if _, err := osProvider.Stat(certPath); os.IsNotExist(err) {
			err := fmt.Errorf("httpPostServer: server certificate not found: %s", certPath)
			logger.Error(err.Error())
			errs = append(errs, err)
		}
		if _, err := osProvider.Stat(keyPath); os.IsNotExist(err) {
			err := fmt.Errorf("httpPostServer: server key not found: %s", keyPath)
			logger.Error(err.Error())
			errs = append(errs, err)
		}
		if _, err := osProvider.Stat(caPath); os.IsNotExist(err) {
			err := fmt.Errorf("httpPostServer: CA certificate not found: %s", caPath)
			logger.Error(err.Error())
			errs = append(errs, err)
		}
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

	// Load TLS certificates
	home, err := osProvider.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get user home directory: %w", err)
	}
	certPath := filepath.Join(home, ".keyop", "certs", "keyop-server.crt")
	keyPath := filepath.Join(home, ".keyop", "certs", "keyop-server.key")
	caPath := filepath.Join(home, ".keyop", "certs", "ca.crt")

	logger.Info("loading server certificate", "cert", certPath, "key", keyPath, "ca", caPath)
	certPEM, err := osProvider.ReadFile(certPath)
	if err != nil {
		return fmt.Errorf("failed to read server certificate: %w", err)
	}
	keyPEM, err := osProvider.ReadFile(keyPath)
	if err != nil {
		return fmt.Errorf("failed to read server key: %w", err)
	}

	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return fmt.Errorf("failed to create server key pair: %w", err)
	}

	caCertPEM, err := osProvider.ReadFile(caPath)
	if err != nil {
		return fmt.Errorf("failed to read CA certificate: %w", err)
	}
	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caCertPEM) {
		return fmt.Errorf("failed to append CA certificate to pool")
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientCAs:    caCertPool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
		MinVersion:   tls.VersionTLS12,
	}

	server := &http.Server{
		Addr:      addr,
		Handler:   mux,
		TLSConfig: tlsConfig,
	}

	logger.Info("starting https server with mutual authentication", "addr", addr)

	go func() {
		if err := server.ListenAndServeTLS("", ""); err != nil {
			logger.Error("https server failed", "error", err)
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

	// this message was sent between client and server without going through the messenger,
	// so we manually append the route
	msg.Route = append(msg.Route, fmt.Sprintf("%s:%s:%s", svc.hostname, svc.Cfg.Type, svc.Cfg.Name)) // Add self to route

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
