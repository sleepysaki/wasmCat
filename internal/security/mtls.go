package security

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const certDir = "./certs"

// GenerateCAAndCerts creates a local CA and leaf certificates for the master
// and worker so the cluster can run mTLS locally without external tooling.
func GenerateCAAndCerts(workerID string) error {
	if workerID == "" {
		return fmt.Errorf("workerID is required")
	}

	if err := os.MkdirAll(certDir, 0o755); err != nil {
		return fmt.Errorf("create cert directory: %w", err)
	}

	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return fmt.Errorf("generate ca key: %w", err)
	}

	caTemplate := &x509.Certificate{
		SerialNumber:          randomSerialNumber(),
		Subject:               pkix.Name{CommonName: "wasmcat-local-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(3650 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	caCertDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		return fmt.Errorf("create ca cert: %w", err)
	}

	caCert, err := x509.ParseCertificate(caCertDER)
	if err != nil {
		return fmt.Errorf("parse ca cert: %w", err)
	}

	if err := writeCertificate(filepath.Join(certDir, "ca.crt"), caCertDER); err != nil {
		return err
	}
	if err := writePrivateKey(filepath.Join(certDir, "ca.key"), caKey); err != nil {
		return err
	}

	if err := generateLeafCert(filepath.Join(certDir, "master.crt"), filepath.Join(certDir, "master.key"), caCert, caKey, "wasmcat-master", true); err != nil {
		return err
	}

	workerName := fmt.Sprintf("wasmcat-worker-%s", workerID)
	if err := generateLeafCert(filepath.Join(certDir, fmt.Sprintf("worker-%s.crt", workerID)), filepath.Join(certDir, fmt.Sprintf("worker-%s.key", workerID)), caCert, caKey, workerName, true); err != nil {
		return err
	}

	return nil
}

func generateLeafCert(certPath string, keyPath string, caCert *x509.Certificate, caKey *rsa.PrivateKey, commonName string, addLocalSANs bool) error {
	leafKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return fmt.Errorf("generate leaf key for %s: %w", commonName, err)
	}

	template := &x509.Certificate{
		SerialNumber: randomSerialNumber(),
		Subject:      pkix.Name{CommonName: commonName},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{
			x509.ExtKeyUsageServerAuth,
			x509.ExtKeyUsageClientAuth,
		},
	}

	if addLocalSANs {
		template.DNSNames = []string{"localhost"}
		template.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, caCert, &leafKey.PublicKey, caKey)
	if err != nil {
		return fmt.Errorf("create leaf cert for %s: %w", commonName, err)
	}

	if err := writeCertificate(certPath, certDER); err != nil {
		return err
	}
	if err := writePrivateKey(keyPath, leafKey); err != nil {
		return err
	}

	return nil
}

func writeCertificate(path string, derBytes []byte) error {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create cert file %s: %w", path, err)
	}
	defer file.Close()

	return pem.Encode(file, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
}

func writePrivateKey(path string, key *rsa.PrivateKey) error {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create key file %s: %w", path, err)
	}
	defer file.Close()

	return pem.Encode(file, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
}

func randomSerialNumber() *big.Int {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, limit)
	if err != nil {
		panic(err)
	}

	return serialNumber
}

// NewMTLSHTTPClient returns an http.Client configured with a client cert and
// the CA pool used to verify the peer.
func NewMTLSHTTPClient(certFile string, keyFile string, caFile string) (*http.Client, error) {
	clientCert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("load client cert: %w", err)
	}

	caPEM, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("read ca cert: %w", err)
	}
	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("append ca certs")
	}

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			Certificates: []tls.Certificate{clientCert},
			RootCAs:      caPool,
			MinVersion:   tls.VersionTLS12,
		},
	}

	return &http.Client{Transport: transport}, nil
}
