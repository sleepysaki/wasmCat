package master_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"wasmcat/internal/master"
	"wasmcat/internal/shared"
)

func TestMasterMetricsReportsRequestsAndWorkers(t *testing.T) {
	registry := master.NewRegistry()
	registry.RegisterWorker(shared.WorkerNode{
		ID:        "worker-metrics",
		IPAddress: "localhost:7271",
		LastSeen:  time.Now().Add(-2 * time.Second),
	})
	gateway := &master.Gateway{
		Registry: registry,
		Dispatcher: &master.Dispatcher{
			Registry:  registry,
			Scheduler: &master.Scheduler{},
			Client:    http.DefaultClient,
		},
	}
	handler := gateway.Handler()

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/wasmcat/health", nil))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/wasmcat/metrics", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d with body %s", rec.Code, rec.Body.String())
	}

	var response shared.MetricsResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode metrics response: %v", err)
	}
	if response.Role != "master" {
		t.Fatalf("expected master role, got %q", response.Role)
	}
	if response.RequestsTotal != 1 {
		t.Fatalf("expected one counted request before metrics snapshot, got %d", response.RequestsTotal)
	}
	if response.RequestsByPath["/wasmcat/health"] != 1 {
		t.Fatalf("expected health path count, got %+v", response.RequestsByPath)
	}
	if response.Master == nil || response.Master.ActiveWorkers != 1 {
		t.Fatalf("expected one active worker, got %+v", response.Master)
	}
	if response.Master.OldestHeartbeatS == nil {
		t.Fatal("expected oldest heartbeat age to be reported")
	}
}

func TestMasterMetricsCountsDispatchFailure(t *testing.T) {
	registry := master.NewRegistry()
	gateway := &master.Gateway{
		Registry: registry,
		Dispatcher: &master.Dispatcher{
			Registry:  registry,
			Scheduler: &master.Scheduler{},
			Client:    http.DefaultClient,
		},
	}
	handler := gateway.Handler()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/execute", strings.NewReader(`{"module_name":"echo","module_url":"http://module.test/echo.wasm"}`))
	handler.ServeHTTP(httptest.NewRecorder(), req)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/wasmcat/metrics", nil))

	var response shared.MetricsResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode metrics response: %v", err)
	}
	if response.Master == nil || response.Master.DispatchFailure != 1 {
		t.Fatalf("expected one dispatch failure, got %+v", response.Master)
	}
}
