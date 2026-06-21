package shared_test

import (
	"crypto/tls"
	"net/http"
	"testing"
	"wasmcat/internal/shared"
)

func TestNewHTTPServerUsesBoundedTimeouts(t *testing.T) {
	t.Parallel()

	server := shared.NewHTTPServer(":7270", http.NewServeMux(), nil)

	if server.ReadHeaderTimeout != shared.DefaultHTTPServerReadHeaderTimeout {
		t.Fatalf("expected read header timeout %s, got %s", shared.DefaultHTTPServerReadHeaderTimeout, server.ReadHeaderTimeout)
	}
	if server.ReadTimeout != shared.DefaultHTTPServerReadTimeout {
		t.Fatalf("expected read timeout %s, got %s", shared.DefaultHTTPServerReadTimeout, server.ReadTimeout)
	}
	if server.WriteTimeout != shared.DefaultHTTPServerWriteTimeout {
		t.Fatalf("expected write timeout %s, got %s", shared.DefaultHTTPServerWriteTimeout, server.WriteTimeout)
	}
	if server.IdleTimeout != shared.DefaultHTTPServerIdleTimeout {
		t.Fatalf("expected idle timeout %s, got %s", shared.DefaultHTTPServerIdleTimeout, server.IdleTimeout)
	}
}

func TestNewHTTPServerPreservesAddressHandlerAndTLSConfig(t *testing.T) {
	t.Parallel()

	handler := http.NewServeMux()
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}

	server := shared.NewHTTPServer(":9443", handler, tlsConfig)

	if server.Addr != ":9443" {
		t.Fatalf("expected addr :9443, got %q", server.Addr)
	}
	if server.Handler != handler {
		t.Fatal("expected server to use provided handler")
	}
	if server.TLSConfig != tlsConfig {
		t.Fatal("expected server to use provided tls config")
	}
}
