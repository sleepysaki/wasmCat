package master_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"wasmcat/internal/master"
	"wasmcat/internal/shared"
)

func TestMasterHealthz(t *testing.T) {
	gateway := newReadyGateway()

	req := httptest.NewRequest(http.MethodGet, "/wasmcat/health", nil)
	rec := httptest.NewRecorder()

	gateway.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var resp shared.HealthResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode health response: %v", err)
	}
	if resp.Status != "ok" || resp.Role != "master" {
		t.Fatalf("unexpected health response: %+v", resp)
	}
}

func TestMasterReadyz(t *testing.T) {
	gateway := newReadyGateway()

	req := httptest.NewRequest(http.MethodGet, "/wasmcat/ready", nil)
	rec := httptest.NewRecorder()

	gateway.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var resp shared.HealthResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode readiness response: %v", err)
	}
	if resp.Status != "ready" || resp.Role != "master" {
		t.Fatalf("unexpected readiness response: %+v", resp)
	}
}

func TestMasterReadyzReturnsUnavailableWhenDependenciesMissing(t *testing.T) {
	gateway := &master.Gateway{}

	req := httptest.NewRequest(http.MethodGet, "/wasmcat/ready", nil)
	rec := httptest.NewRecorder()

	gateway.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503, got %d", rec.Code)
	}

	var resp shared.ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if resp.Code != "not_ready" {
		t.Fatalf("expected not_ready code, got %q", resp.Code)
	}
}

func TestMasterRejectsUnexpectedMethod(t *testing.T) {
	gateway := newReadyGateway()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/execute", nil)
	rec := httptest.NewRecorder()

	gateway.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected status 405, got %d", rec.Code)
	}
	if rec.Header().Get("Allow") != http.MethodPost {
		t.Fatalf("expected Allow POST, got %q", rec.Header().Get("Allow"))
	}

	var resp shared.ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode method error: %v", err)
	}
	if resp.Code != "method_not_allowed" {
		t.Fatalf("expected method_not_allowed code, got %q", resp.Code)
	}
}

func newReadyGateway() *master.Gateway {
	registry := master.NewRegistry()
	return &master.Gateway{
		Registry: registry,
		Dispatcher: &master.Dispatcher{
			Registry:  registry,
			Scheduler: &master.Scheduler{},
			Client:    http.DefaultClient,
		},
	}
}
