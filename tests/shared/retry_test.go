package shared_test

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"
	"wasmcat/internal/shared"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestDoWithRetryRetriesTransientStatus(t *testing.T) {
	t.Parallel()

	attempts := 0
	client := &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			attempts++
			if attempts == 1 {
				return retryTestResponse(http.StatusServiceUnavailable, "try again"), nil
			}
			return retryTestResponse(http.StatusOK, "ok"), nil
		}),
	}
	req, err := http.NewRequest(http.MethodGet, "https://example.test/module.wasm", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}

	resp, err := shared.DoWithRetryPolicy(client, req, shared.RetryPolicy{Attempts: 2, Backoff: time.Millisecond})
	if err != nil {
		t.Fatalf("retry request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected final 200, got %d", resp.StatusCode)
	}
	if attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
}

func TestDoWithRetryDoesNotRetryPermanentStatus(t *testing.T) {
	t.Parallel()

	attempts := 0
	client := &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			attempts++
			return retryTestResponse(http.StatusBadRequest, "bad request"), nil
		}),
	}
	req, err := http.NewRequest(http.MethodGet, "https://example.test/module.wasm", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}

	resp, err := shared.DoWithRetryPolicy(client, req, shared.RetryPolicy{Attempts: 3, Backoff: time.Millisecond})
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected final 400, got %d", resp.StatusCode)
	}
	if attempts != 1 {
		t.Fatalf("expected 1 attempt, got %d", attempts)
	}
}

func TestDoWithRetryReplaysRequestBody(t *testing.T) {
	t.Parallel()

	attempts := 0
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			attempts++
			body, err := io.ReadAll(req.Body)
			if err != nil {
				t.Fatalf("read body: %v", err)
			}
			if string(body) != `{"node_id":"worker-1"}` {
				t.Fatalf("unexpected request body on attempt %d: %q", attempts, string(body))
			}
			if attempts == 1 {
				return retryTestResponse(http.StatusTooManyRequests, "limited"), nil
			}
			return retryTestResponse(http.StatusOK, "ok"), nil
		}),
	}
	req, err := http.NewRequest(http.MethodPost, "https://example.test/internal/heartbeat", bytes.NewBufferString(`{"node_id":"worker-1"}`))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}

	resp, err := shared.DoWithRetryPolicy(client, req, shared.RetryPolicy{Attempts: 2, Backoff: time.Millisecond})
	if err != nil {
		t.Fatalf("retry request failed: %v", err)
	}
	defer resp.Body.Close()

	if attempts != 2 {
		t.Fatalf("expected body to be sent twice, got %d attempts", attempts)
	}
}

func TestDoWithRetryReturnsLastTransientStatusWhenExhausted(t *testing.T) {
	t.Parallel()

	attempts := 0
	client := &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			attempts++
			return retryTestResponse(http.StatusGatewayTimeout, "timeout"), nil
		}),
	}
	req, err := http.NewRequest(http.MethodGet, "https://example.test/module.wasm", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}

	resp, err := shared.DoWithRetryPolicy(client, req, shared.RetryPolicy{Attempts: 3, Backoff: time.Millisecond})
	if err != nil {
		t.Fatalf("expected final response without transport error, got %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusGatewayTimeout {
		t.Fatalf("expected final 504, got %d", resp.StatusCode)
	}
	if attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}
}

func TestDoWithRetryRetriesTransportError(t *testing.T) {
	t.Parallel()

	attempts := 0
	client := &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			attempts++
			if attempts == 1 {
				return nil, errors.New("temporary network failure")
			}
			return retryTestResponse(http.StatusOK, "ok"), nil
		}),
	}
	req, err := http.NewRequest(http.MethodGet, "https://example.test/module.wasm", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}

	resp, err := shared.DoWithRetryPolicy(client, req, shared.RetryPolicy{Attempts: 2, Backoff: time.Millisecond})
	if err != nil {
		t.Fatalf("retry request failed: %v", err)
	}
	defer resp.Body.Close()

	if attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
}

func retryTestResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Body:       io.NopCloser(bytes.NewBufferString(body)),
		Header:     make(http.Header),
	}
}
