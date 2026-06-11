package master_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
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

func TestMasterMetricsReportsDispatchReschedules(t *testing.T) {
	var failedWorkerHits int32
	var healthyWorkerHits int32

	failedWorker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&failedWorkerHits, 1)
		shared.WriteError(w, http.StatusServiceUnavailable, "worker_unavailable", assertErr("temporary outage"))
	}))
	defer failedWorker.Close()

	healthyWorker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&healthyWorkerHits, 1)
		shared.WriteJSON(w, http.StatusOK, shared.ExecutionResponse{Result: "ok"})
	}))
	defer healthyWorker.Close()

	registry := master.NewRegistry()
	registry.RegisterWorker(shared.WorkerNode{
		ID:        "worker-unavailable",
		IPAddress: failedWorker.URL,
		Latitude:  0,
		Longitude: 0,
	})
	registry.RegisterWorker(shared.WorkerNode{
		ID:        "worker-healthy",
		IPAddress: healthyWorker.URL,
		Latitude:  10,
		Longitude: 10,
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

	req := httptest.NewRequest(http.MethodPost, "/api/v1/execute", strings.NewReader(`{"module_name":"echo","module_url":"http://module.test/echo.wasm","user_lat":0,"user_lon":0}`))
	executeRec := httptest.NewRecorder()
	handler.ServeHTTP(executeRec, req)
	if executeRec.Code != http.StatusOK {
		t.Fatalf("expected execute status 200, got %d with body %s", executeRec.Code, executeRec.Body.String())
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/wasmcat/metrics", nil))

	var response shared.MetricsResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode metrics response: %v", err)
	}
	if response.Master == nil || response.Master.DispatchSuccess != 1 {
		t.Fatalf("expected one dispatch success, got %+v", response.Master)
	}
	if response.Master.DispatchReschedules != 1 {
		t.Fatalf("expected one dispatch reschedule, got %+v", response.Master)
	}
	if response.Master.DispatchRescheduleExhausted != 0 {
		t.Fatalf("expected no exhausted reschedule, got %+v", response.Master)
	}
	if atomic.LoadInt32(&failedWorkerHits) != 1 || atomic.LoadInt32(&healthyWorkerHits) != 1 {
		t.Fatalf("expected both workers to be hit once, failed=%d healthy=%d", failedWorkerHits, healthyWorkerHits)
	}
}
