package shared

import (
	"crypto/tls"
	"fmt"
	"io"
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
	DefaultHTTPRetryAttempts         = 3
	DefaultHTTPRetryBackoff          = 100 * time.Millisecond
)

type RetryPolicy struct {
	Attempts int
	Backoff  time.Duration
}

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

func DefaultHTTPRetryPolicy() RetryPolicy {
	return RetryPolicy{
		Attempts: DefaultHTTPRetryAttempts,
		Backoff:  DefaultHTTPRetryBackoff,
	}
}

func DoWithRetry(client *http.Client, req *http.Request) (*http.Response, error) {
	return DoWithRetryPolicy(client, req, DefaultHTTPRetryPolicy())
}

func DoWithRetryPolicy(client *http.Client, req *http.Request, policy RetryPolicy) (*http.Response, error) {
	if client == nil {
		client = http.DefaultClient
	}
	if policy.Attempts <= 0 {
		policy.Attempts = 1
	}

	var lastErr error
	for attempt := 0; attempt < policy.Attempts; attempt++ {
		attemptReq, err := requestForRetryAttempt(req, attempt)
		if err != nil {
			return nil, err
		}

		resp, err := client.Do(attemptReq)
		if err == nil && !retryableStatus(resp.StatusCode) {
			return resp, nil
		}
		if err == nil && attempt == policy.Attempts-1 {
			return resp, nil
		}
		if err != nil && !retryableRequestError(req, err) {
			return nil, err
		}
		if err == nil {
			_ = drainAndClose(resp.Body)
		}

		lastErr = err
		if err := sleepBeforeRetry(req, policy.Backoff, attempt); err != nil {
			if lastErr != nil {
				return nil, lastErr
			}
			return nil, err
		}
	}

	if lastErr != nil {
		return nil, lastErr
	}

	return nil, fmt.Errorf("http retry attempts exhausted")
}

func requestForRetryAttempt(req *http.Request, attempt int) (*http.Request, error) {
	if req == nil {
		return nil, fmt.Errorf("request is nil")
	}
	if attempt == 0 {
		return req, nil
	}

	cloned := req.Clone(req.Context())
	if req.Body == nil || req.Body == http.NoBody {
		cloned.Body = nil
		return cloned, nil
	}
	if req.GetBody == nil {
		return nil, fmt.Errorf("request body cannot be replayed")
	}

	body, err := req.GetBody()
	if err != nil {
		return nil, fmt.Errorf("recreate request body: %w", err)
	}
	cloned.Body = body
	return cloned, nil
}

func retryableStatus(statusCode int) bool {
	switch statusCode {
	case http.StatusRequestTimeout,
		http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func retryableRequestError(req *http.Request, err error) bool {
	if err == nil {
		return false
	}
	select {
	case <-req.Context().Done():
		return false
	default:
		return true
	}
}

func sleepBeforeRetry(req *http.Request, backoff time.Duration, attempt int) error {
	if backoff <= 0 {
		return nil
	}

	delay := backoff * time.Duration(attempt+1)
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-req.Context().Done():
		return req.Context().Err()
	case <-timer.C:
		return nil
	}
}

func drainAndClose(body io.ReadCloser) error {
	if body == nil {
		return nil
	}
	defer body.Close()

	_, err := io.Copy(io.Discard, io.LimitReader(body, 4096))
	return err
}
