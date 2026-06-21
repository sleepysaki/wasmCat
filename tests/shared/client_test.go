package shared_test

import (
	"crypto/tls"
	"net/http"
	"testing"
	"wasmcat/internal/shared"
)

func TestNewHTTPClientUsesBoundedTimeouts(t *testing.T) {
	client := shared.NewHTTPClient()
	if client.Timeout != shared.DefaultHTTPClientTimeout {
		t.Fatalf("expected client timeout %s, got %s", shared.DefaultHTTPClientTimeout, client.Timeout)
	}

	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport, got %T", client.Transport)
	}
	if transport.ResponseHeaderTimeout != shared.DefaultHTTPResponseHeaderTimeout {
		t.Fatalf("expected response header timeout %s, got %s", shared.DefaultHTTPResponseHeaderTimeout, transport.ResponseHeaderTimeout)
	}
	if transport.TLSHandshakeTimeout != shared.DefaultHTTPTLSHandshakeTimeout {
		t.Fatalf("expected TLS handshake timeout %s, got %s", shared.DefaultHTTPTLSHandshakeTimeout, transport.TLSHandshakeTimeout)
	}
	if transport.IdleConnTimeout != shared.DefaultHTTPIdleConnTimeout {
		t.Fatalf("expected idle conn timeout %s, got %s", shared.DefaultHTTPIdleConnTimeout, transport.IdleConnTimeout)
	}
	if transport.MaxIdleConns != shared.DefaultHTTPMaxIdleConns {
		t.Fatalf("expected max idle conns %d, got %d", shared.DefaultHTTPMaxIdleConns, transport.MaxIdleConns)
	}
	if transport.MaxIdleConnsPerHost != shared.DefaultHTTPMaxIdleConnsPerHost {
		t.Fatalf("expected max idle conns per host %d, got %d", shared.DefaultHTTPMaxIdleConnsPerHost, transport.MaxIdleConnsPerHost)
	}
}

func TestNewHTTPClientWithTLSConfigPreservesTLSConfig(t *testing.T) {
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}

	client := shared.NewHTTPClientWithTLSConfig(tlsConfig)
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport, got %T", client.Transport)
	}
	if transport.TLSClientConfig != tlsConfig {
		t.Fatal("expected transport to use provided TLS config")
	}
}
