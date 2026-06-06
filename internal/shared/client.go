package shared

import (
	"crypto/tls"
	"net"
	"net/http"
	"time"
)

const (
	DefaultHTTPClientTimeout         = 30 * time.Second
	DefaultHTTPDialTimeout           = 10 * time.Second
	DefaultHTTPKeepAlive             = 30 * time.Second
	DefaultHTTPResponseHeaderTimeout = 10 * time.Second
	DefaultHTTPIdleConnTimeout       = 90 * time.Second
	DefaultHTTPTLSHandshakeTimeout   = 10 * time.Second
	DefaultHTTPMaxIdleConns          = 100
	DefaultHTTPMaxIdleConnsPerHost   = 10
)

func NewHTTPClient() *http.Client {
	return NewHTTPClientWithTLSConfig(nil)
}

func NewHTTPClientWithTLSConfig(tlsConfig *tls.Config) *http.Client {
	return &http.Client{
		Timeout:   DefaultHTTPClientTimeout,
		Transport: NewHTTPTransport(tlsConfig),
	}
}

func NewHTTPTransport(tlsConfig *tls.Config) *http.Transport {
	return &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   DefaultHTTPDialTimeout,
			KeepAlive: DefaultHTTPKeepAlive,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          DefaultHTTPMaxIdleConns,
		MaxIdleConnsPerHost:   DefaultHTTPMaxIdleConnsPerHost,
		IdleConnTimeout:       DefaultHTTPIdleConnTimeout,
		TLSHandshakeTimeout:   DefaultHTTPTLSHandshakeTimeout,
		ResponseHeaderTimeout: DefaultHTTPResponseHeaderTimeout,
		TLSClientConfig:       tlsConfig,
	}
}
