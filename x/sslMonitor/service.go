package sslMonitor

import (
	"crypto/tls"
	"fmt"
	"keyop/core"
	"keyop/util"
	"net"
	"time"
)

type Service struct {
	Deps           core.Dependencies
	Cfg            core.ServiceConfig
	url            string
	warningPeriod  time.Duration
	criticalPeriod time.Duration
}

func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	return &Service{
		Deps: deps,
		Cfg:  cfg,
	}
}

func (svc *Service) ValidateConfig() []error {
	logger := svc.Deps.MustGetLogger()
	errs := util.ValidateConfig("pubs", svc.Cfg.Pubs, []string{"status"}, logger)

	if url, ok := svc.Cfg.Config["url"].(string); !ok || url == "" {
		errs = append(errs, fmt.Errorf("required config parameter 'url' is missing or empty"))
	}

	return errs
}

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
		InsecureSkipVerify: insecure,
	})
	if err != nil {
		logger.Error("failed to connect to host", "url", svc.url, "error", err)
		svc.sendStatus(messenger, "CRITICAL", fmt.Sprintf("Failed to connect to %s: %v", svc.url, err))
		return err
	}
	defer conn.Close()

	certs := conn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		err := fmt.Errorf("no certificates found for %s", svc.url)
		logger.Error(err.Error())
		svc.sendStatus(messenger, "CRITICAL", err.Error())
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
		ChannelName: svc.Cfg.Pubs["status"].Name,
		ServiceName: svc.Cfg.Name,
		ServiceType: svc.Cfg.Type,
		Status:      status,
		Summary:     summary,
		Text:        summary,
	})
}
