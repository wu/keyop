// Package sslMonitor checks TLS/SSL certificate health for configured endpoints and emits expiration warnings.
package sslMonitor

import (
	"crypto/tls"
	"fmt"
	"keyop/core"
	"net"
	"time"
)

// Service inspects remote certificates, records expiry information, and publishes alerts when renewal is required.
type Service struct {
	Deps           core.Dependencies
	Cfg            core.ServiceConfig
	url            string
	warningPeriod  time.Duration
	criticalPeriod time.Duration
}

// NewService creates a new service using the provided dependencies and configuration.
func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	return &Service{
		Deps: deps,
		Cfg:  cfg,
	}
}

// ValidateConfig validates the service configuration and returns any validation errors.
func (svc *Service) ValidateConfig() []error {
	var errs []error

	if url, ok := svc.Cfg.Config["url"].(string); !ok || url == "" {
		errs = append(errs, fmt.Errorf("required config parameter 'url' is missing or empty"))
	}

	return errs
}

// Initialize performs one-time startup required by the service (resource loading or connectivity checks).
func (svc *Service) Initialize() error {
	svc.url, _ = svc.Cfg.Config["url"].(string)

	warningDays, _ := svc.Cfg.Config["warning_days"].(float64)
	if warningDays == 0 {
		warningDays = 30 // default 30 days
	}
	svc.warningPeriod = time.Duration(warningDays) * 24 * time.Hour

	criticalDays, _ := svc.Cfg.Config["critical_days"].(float64)
	if criticalDays == 0 {
		criticalDays = 7 // default 7 days
	}
	svc.criticalPeriod = time.Duration(criticalDays) * 24 * time.Hour

	return nil
}

// Check performs the service's periodic work: collect data, evaluate state, and publish messages/metrics.
func (svc *Service) Check() error {
	logger := svc.Deps.MustGetLogger()
	messenger := svc.Deps.MustGetMessenger()

	// Parse host and port from URL. URL should be like "example.com:443" or just "example.com"
	host := svc.url
	if _, _, err := net.SplitHostPort(host); err != nil {
		host = net.JoinHostPort(host, "443")
	}

	dialer := &net.Dialer{
		Timeout: 10 * time.Second,
	}

	insecure, _ := svc.Cfg.Config["insecure_skip_verify"].(bool)

	conn, err := tls.DialWithDialer(dialer, "tcp", host, &tls.Config{
		InsecureSkipVerify: insecure, //nolint:gosec // operator-configured TLS verification
	})
	if err != nil {
		logger.Error("failed to connect to host", "url", svc.url, "error", err)
		if sendErr := svc.sendStatus(messenger, "CRITICAL", fmt.Sprintf("Failed to connect to %s: %v", svc.url, err)); sendErr != nil {
			logger.Error("sslMonitor: failed to send status", "error", sendErr)
		}
		return err
	}
	defer func() {
		if err := conn.Close(); err != nil {
			logger.Error("sslMonitor: failed to close connection", "error", err)
		}
	}()

	certs := conn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		err := fmt.Errorf("no certificates found for %s", svc.url)
		logger.Error(err.Error())
		if sendErr := svc.sendStatus(messenger, "CRITICAL", err.Error()); sendErr != nil {
			logger.Error("sslMonitor: failed to send status", "error", sendErr)
		}
		return err
	}

	cert := certs[0]
	now := time.Now()
	expiresIn := cert.NotAfter.Sub(now)

	status := "OK"
	if expiresIn < svc.criticalPeriod {
		status = "CRITICAL"
	} else if expiresIn < svc.warningPeriod {
		status = "WARNING"
	}

	daysRemaining := int(expiresIn.Hours() / 24)
	summary := fmt.Sprintf("SSL certificate for %s expires in %d days", svc.url, daysRemaining)
	if expiresIn < 0 {
		summary = fmt.Sprintf("SSL certificate for %s EXPIRED %d days ago", svc.url, -daysRemaining)
	}

	logger.Info("SSL Certificate check complete", "url", svc.url, "status", status, "expiresIn", expiresIn)

	return svc.sendStatus(messenger, status, summary)
}

func (svc *Service) sendStatus(messenger core.MessengerApi, status string, summary string) error {
	return messenger.Send(core.Message{
		ChannelName: svc.Cfg.Name,
		ServiceName: svc.Cfg.Name,
		Event:       "status",
		Status:      status,
		Summary:     summary,
		Text:        summary,
	})
}
