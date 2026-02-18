package sslMonitor

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"keyop/core"
	"math/big"
	"net"
	"testing"
	"time"
)

type mockMessenger struct {
	messages []core.Message
}

func (m *mockMessenger) Send(msg core.Message) error {
	m.messages = append(m.messages, msg)
	return nil
}

func (m *mockMessenger) Subscribe(ctx context.Context, sourceName string, channelName string, serviceType string, serviceName string, maxAge time.Duration, messageHandler func(core.Message) error) error {
	return nil
}

func (m *mockMessenger) SubscribeExtended(ctx context.Context, source string, channelName string, serviceType string, serviceName string, maxAge time.Duration, messageHandler func(core.Message, string, int64) error) error {
	return nil
}

func (m *mockMessenger) SetReaderState(channelName string, readerName string, fileName string, offset int64) error {
	return nil
}

func (m *mockMessenger) SeekToEnd(channelName string, readerName string) error {
	return nil
}

func (m *mockMessenger) SetDataDir(dir string) {}

func (m *mockMessenger) GetStats() core.MessengerStats {
	return core.MessengerStats{}
}

func TestCheck(t *testing.T) {
	// 1. Setup a test TLS server
	cert, key, err := generateTestCert(time.Now().Add(10 * 24 * time.Hour)) // Expires in 10 days
	if err != nil {
		t.Fatalf("Failed to generate test cert: %v", err)
	}

	tlsCert, err := tls.X509KeyPair(cert, key)
	if err != nil {
		t.Fatalf("Failed to load key pair: %v", err)
	}

	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{tlsCert}})
	if err != nil {
		t.Fatalf("Failed to listen: %v", err)
	}
	defer ln.Close()

	addr := ln.Addr().String()

	// 2. Setup the service
	messenger := &mockMessenger{}
	deps := core.Dependencies{}
	deps.SetMessenger(messenger)
	deps.SetLogger(&core.FakeLogger{})

	cfg := core.ServiceConfig{
		Name: "ssl_test",
		Type: "sslMonitor",
		Pubs: map[string]core.ChannelInfo{
			"status": {Name: "status_channel"},
		},
		Config: map[string]interface{}{
			"url":                  addr,
			"warning_days":         30.0,
			"critical_days":        7.0,
			"insecure_skip_verify": true,
		},
	}

	svc := NewService(deps, cfg)
	if err := svc.Initialize(); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// Run the server in a goroutine
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				tlsConn := conn.(*tls.Conn)
				tlsConn.Handshake()
				tlsConn.Close()
			}()
		}
	}()

	// 3. Run Check
	err = svc.Check()
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}

	// 4. Verify results
	if len(messenger.messages) != 1 {
		t.Fatalf("Expected 1 message, got %d", len(messenger.messages))
	}

	msg := messenger.messages[0]
	if msg.Status != "WARNING" {
		t.Errorf("Expected status WARNING (expires in 10 days, warning is 30), got %s", msg.Status)
	}

	expectedText1 := fmt.Sprintf("SSL certificate for %s expires in 9 days", addr)
	expectedText2 := fmt.Sprintf("SSL certificate for %s expires in 10 days", addr)
	if msg.Text != expectedText1 && msg.Text != expectedText2 {
		t.Errorf("Unexpected message text: %s", msg.Text)
	}
}

func TestCheck_Critical(t *testing.T) {
	// 1. Setup a test TLS server
	cert, key, err := generateTestCert(time.Now().Add(5 * 24 * time.Hour)) // Expires in 5 days
	if err != nil {
		t.Fatalf("Failed to generate test cert: %v", err)
	}

	tlsCert, err := tls.X509KeyPair(cert, key)
	if err != nil {
		t.Fatalf("Failed to load key pair: %v", err)
	}

	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{tlsCert}})
	if err != nil {
		t.Fatalf("Failed to listen: %v", err)
	}
	defer ln.Close()

	addr := ln.Addr().String()

	// 2. Setup the service
	messenger := &mockMessenger{}
	deps := core.Dependencies{}
	deps.SetMessenger(messenger)
	deps.SetLogger(&core.FakeLogger{})

	cfg := core.ServiceConfig{
		Name: "ssl_test_critical",
		Type: "sslMonitor",
		Pubs: map[string]core.ChannelInfo{
			"status": {Name: "status_channel"},
		},
		Config: map[string]interface{}{
			"url":                  addr,
			"warning_days":         30.0,
			"critical_days":        7.0,
			"insecure_skip_verify": true,
		},
	}

	svc := NewService(deps, cfg)
	if err := svc.Initialize(); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// Run the server in a goroutine
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				tlsConn := conn.(*tls.Conn)
				tlsConn.Handshake()
				tlsConn.Close()
			}()
		}
	}()

	// 3. Run Check
	err = svc.Check()
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}

	// 4. Verify results
	msg := messenger.messages[0]
	if msg.Status != "CRITICAL" {
		t.Errorf("Expected status CRITICAL (expires in 5 days, critical is 7), got %s", msg.Status)
	}
}

func TestCheck_OK(t *testing.T) {
	// 1. Setup a test TLS server
	cert, key, err := generateTestCert(time.Now().Add(60 * 24 * time.Hour)) // Expires in 60 days
	if err != nil {
		t.Fatalf("Failed to generate test cert: %v", err)
	}

	tlsCert, err := tls.X509KeyPair(cert, key)
	if err != nil {
		t.Fatalf("Failed to load key pair: %v", err)
	}

	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{tlsCert}})
	if err != nil {
		t.Fatalf("Failed to listen: %v", err)
	}
	defer ln.Close()

	addr := ln.Addr().String()

	// 2. Setup the service
	messenger := &mockMessenger{}
	deps := core.Dependencies{}
	deps.SetMessenger(messenger)
	deps.SetLogger(&core.FakeLogger{})

	cfg := core.ServiceConfig{
		Name: "ssl_test_ok",
		Type: "sslMonitor",
		Pubs: map[string]core.ChannelInfo{
			"status": {Name: "status_channel"},
		},
		Config: map[string]interface{}{
			"url":                  addr,
			"warning_days":         30.0,
			"critical_days":        7.0,
			"insecure_skip_verify": true,
		},
	}

	svc := NewService(deps, cfg)
	if err := svc.Initialize(); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// Run the server in a goroutine
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				tlsConn := conn.(*tls.Conn)
				tlsConn.Handshake()
				tlsConn.Close()
			}()
		}
	}()

	// 3. Run Check
	err = svc.Check()
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}

	// 4. Verify results
	msg := messenger.messages[0]
	if msg.Status != "OK" {
		t.Errorf("Expected status OK (expires in 60 days, warning is 30), got %s", msg.Status)
	}
}

func generateTestCert(expiry time.Time) ([]byte, []byte, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, err
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Test"},
		},
		NotBefore: time.Now().Add(-time.Hour),
		NotAfter:  expiry,

		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return nil, nil, err
	}

	var certPem bytes.Buffer
	pem.Encode(&certPem, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes})

	privBytes, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, nil, err
	}
	var keyPem bytes.Buffer
	pem.Encode(&keyPem, &pem.Block{Type: "PRIVATE KEY", Bytes: privBytes})

	return certPem.Bytes(), keyPem.Bytes(), nil
}
