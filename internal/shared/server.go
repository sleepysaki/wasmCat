package shared

import (
	"crypto/tls"
	"net/http"
	"time"
)

const (
	DefaultHTTPServerReadHeaderTimeout = 5 * time.Second
	DefaultHTTPServerReadTimeout       = 30 * time.Second
	DefaultHTTPServerWriteTimeout      = 30 * time.Second
	DefaultHTTPServerIdleTimeout       = 60 * time.Second
)

func NewHTTPServer(addr string, handler http.Handler, tlsConfig *tls.Config) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		TLSConfig:         tlsConfig,
		ReadHeaderTimeout: DefaultHTTPServerReadHeaderTimeout,
		ReadTimeout:       DefaultHTTPServerReadTimeout,
		WriteTimeout:      DefaultHTTPServerWriteTimeout,
		IdleTimeout:       DefaultHTTPServerIdleTimeout,
	}
}
