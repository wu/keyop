package httpPost

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"keyop/core"
	"keyop/util"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

type Service struct {
	Deps       core.Dependencies
	Cfg        core.ServiceConfig
	Port       int
	Hostname   string
	Timeout    time.Duration
	httpClient *http.Client
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

func (svc *Service) ValidateConfig() []error {
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

	// check for TLS certificates
	osProvider := svc.Deps.MustGetOsProvider()
	home, err := osProvider.UserHomeDir()
	if err != nil {
		err := fmt.Errorf("httpPost: failed to get user home directory: %w", err)
		logger.Error(err.Error())
		errs = append(errs, err)
	} else {
		certPath := filepath.Join(home, ".keyop", "certs", "keyop-client.crt")
		keyPath := filepath.Join(home, ".keyop", "certs", "keyop-client.key")
		caPath := filepath.Join(home, ".keyop", "certs", "ca.crt")

		if _, err := osProvider.Stat(certPath); os.IsNotExist(err) {
			err := fmt.Errorf("httpPost: client certificate not found: %s", certPath)
			logger.Error(err.Error())
			errs = append(errs, err)
		}
		if _, err := osProvider.Stat(keyPath); os.IsNotExist(err) {
			err := fmt.Errorf("httpPost: client key not found: %s", keyPath)
			logger.Error(err.Error())
			errs = append(errs, err)
		}
		if _, err := osProvider.Stat(caPath); os.IsNotExist(err) {
			err := fmt.Errorf("httpPost: CA certificate not found: %s", caPath)
			logger.Error(err.Error())
			errs = append(errs, err)
		}
	}

	return errs
}

func (svc *Service) Initialize() error {

	messenger := svc.Deps.MustGetMessenger()

	var errs []error

	logger := svc.Deps.MustGetLogger()

	logger.Error("httpPost: initializing service", "conf", svc.Cfg)

	// Set up TLS client
	osProvider := svc.Deps.MustGetOsProvider()
	home, err := osProvider.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get user home directory: %w", err)
	}

	certPath := filepath.Join(home, ".keyop", "certs", "keyop-client.crt")
	keyPath := filepath.Join(home, ".keyop", "certs", "keyop-client.key")
	caPath := filepath.Join(home, ".keyop", "certs", "ca.crt")

	logger.Info("loading client certificate", "cert", certPath, "key", keyPath, "ca", caPath)
	certPEM, err := osProvider.ReadFile(certPath)
	if err != nil {
		return fmt.Errorf("failed to read client certificate: %w", err)
	}
	keyPEM, err := osProvider.ReadFile(keyPath)
	if err != nil {
		return fmt.Errorf("failed to read client key: %w", err)
	}

	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return fmt.Errorf("failed to create client key pair: %w", err)
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
		RootCAs:      caCertPool,
		MinVersion:   tls.VersionTLS12,
		// Ignore IP SAN by skipping default verification and providing a custom one
		// that doesn't check hostnames/IPs.
		InsecureSkipVerify: true,
		VerifyConnection: func(cs tls.ConnectionState) error {
			opts := x509.VerifyOptions{
				Roots:         caCertPool,
				Intermediates: x509.NewCertPool(),
			}
			for _, cert := range cs.PeerCertificates[1:] {
				opts.Intermediates.AddCert(cert)
			}
			_, err := cs.PeerCertificates[0].Verify(opts)
			return err
		},
	}

	svc.httpClient = &http.Client{
		Timeout: svc.Timeout,
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
	}

	for name, sub := range svc.Cfg.Subs {
		logger.Warn("httpPost: initializing subscription", "name", name, "topic", sub.Name, "maxAge", sub.MaxAge)
		err := messenger.Subscribe(svc.Deps.MustGetContext(), svc.Cfg.Name, sub.Name, sub.MaxAge, svc.messageHandler)
		if err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("httpPost: failed to initialize subscriptions: %v", errs)
	}
	return nil
}

func (svc *Service) Check() error {
	return nil
}

func (svc *Service) messageHandler(msg core.Message) error {
	logger := svc.Deps.MustGetLogger()

	// process incoming message
	logger.Info("httpPost: forwarding message", "channel", msg.ChannelName, "message", msg)

	// send message to HTTP endpoint
	url := fmt.Sprintf("https://%s:%d", svc.Hostname, svc.Port)

	jsonData, err := json.Marshal(msg)
	if err != nil {
		logger.Error("failed to marshal message to JSON", "error", err)
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), svc.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(jsonData))
	if err != nil {
		logger.Error("failed to create HTTP request", "url", url, "error", err)
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := svc.httpClient.Do(req)
	if err != nil {
		logger.Error("failed to post message to HTTP endpoint", "url", url, "error", err)
		return err
	}
	//goland:noinspection GoUnhandledErrorResult
	defer resp.Body.Close()

	logger.Debug("successfully posted message to HTTP endpoint", "url", url, "status", resp.Status)

	return nil
}
