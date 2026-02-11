package util

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"time"
)

func GenerateTestCerts(dir string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	// Generate CA Cert
	caPriv, caCert, err := generateCACert(dir)
	if err != nil {
		return err
	}

	// Generate Server Cert signed by CA
	if err := generateSignedCert(dir, "keyop-server", caCert, caPriv); err != nil {
		return err
	}

	// Generate Client Cert signed by CA
	if err := generateSignedCert(dir, "keyop-client", caCert, caPriv); err != nil {
		return err
	}

	return nil
}

func generateCACert(dir string) (*rsa.PrivateKey, *x509.Certificate, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, err
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"KeyOp Test CA"},
			CommonName:   "KeyOp Root CA",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return nil, nil, err
	}

	certOut, err := os.Create(filepath.Join(dir, "ca.crt"))
	if err != nil {
		return nil, nil, err
	}
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes}); err != nil {
		return nil, nil, err
	}
	certOut.Close()

	caCert, err := x509.ParseCertificate(derBytes)
	if err != nil {
		return nil, nil, err
	}

	return priv, caCert, nil
}

func generateSignedCert(dir, name string, caCert *x509.Certificate, caPriv *rsa.PrivateKey) error {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject: pkix.Name{
			Organization: []string{"KeyOp Test"},
			CommonName:   name,
		},
		NotBefore: time.Now(),
		NotAfter:  time.Now().Add(time.Hour),

		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost"},
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, caCert, &priv.PublicKey, caPriv)
	if err != nil {
		return err
	}

	certOut, err := os.Create(filepath.Join(dir, name+".crt"))
	if err != nil {
		return err
	}
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes}); err != nil {
		return err
	}
	certOut.Close()

	keyOut, err := os.OpenFile(filepath.Join(dir, name+".key"), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	privBytes, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return err
	}
	if err := pem.Encode(keyOut, &pem.Block{Type: "PRIVATE KEY", Bytes: privBytes}); err != nil {
		return err
	}
	keyOut.Close()

	return nil
}

func CreateTestCerts(dir string) (string, string, string, string, error) {
	err := GenerateTestCerts(dir)
	if err != nil {
		return "", "", "", "", err
	}
	return filepath.Join(dir, "keyop-server.crt"),
		filepath.Join(dir, "keyop-server.key"),
		filepath.Join(dir, "keyop-client.crt"),
		filepath.Join(dir, "keyop-client.key"),
		nil
}
