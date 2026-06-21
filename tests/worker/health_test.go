package worker_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"wasmcat/internal/shared"
	"wasmcat/internal/worker"
)

func TestWorkerHealthz(t *testing.T) {
	server := newReadyWorkerServer()

	req := httptest.NewRequest(http.MethodGet, "/wasmcat/health", nil)
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var resp shared.HealthResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode health response: %v", err)
	}
	if resp.Status != "ok" || resp.Role != "worker" || resp.NodeID != "worker-test" {
		t.Fatalf("unexpected health response: %+v", resp)
	}
}

func TestWorkerReadyz(t *testing.T) {
	server := newReadyWorkerServer()

	req := httptest.NewRequest(http.MethodGet, "/wasmcat/ready", nil)
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var resp shared.HealthResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode readiness response: %v", err)
	}
	if resp.Status != "ready" || resp.Role != "worker" || resp.NodeID != "worker-test" {
		t.Fatalf("unexpected readiness response: %+v", resp)
	}
}

func TestWorkerReadyzReturnsUnavailableWhenEngineMissing(t *testing.T) {
	server := &worker.WorkerServer{NodeID: "worker-test"}

	req := httptest.NewRequest(http.MethodGet, "/wasmcat/ready", nil)
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

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

func TestWorkerRejectsUnexpectedMethod(t *testing.T) {
	server := newReadyWorkerServer()

	req := httptest.NewRequest(http.MethodPost, "/wasmcat/health", nil)
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected status 405, got %d", rec.Code)
	}
	if rec.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("expected Allow GET, got %q", rec.Header().Get("Allow"))
	}

	var resp shared.ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode method error: %v", err)
	}
	if resp.Code != "method_not_allowed" {
		t.Fatalf("expected method_not_allowed code, got %q", resp.Code)
	}
}

func newReadyWorkerServer() *worker.WorkerServer {
	return &worker.WorkerServer{
		Engine: worker.NewWasmEngine(context.Background()),
		NodeID: "worker-test",
	}
}
