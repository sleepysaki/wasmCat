package worker_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"wasmcat/internal/shared"
	"wasmcat/internal/worker"
)

func TestWorkerMetricsReportsExecutionAndCacheState(t *testing.T) {
	moduleServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/wasm")
		w.Write(wasmEchoModule())
	}))
	defer moduleServer.Close()

	engine := worker.NewWasmEngineWithLimits(context.Background(), worker.Limits{
		MaxPayloadBytes:    1024,
		MaxModuleBytes:     1024,
		MaxOutputBytes:     1024,
		MaxConcurrentExecs: 2,
	})
	server := &worker.WorkerServer{Engine: engine, NodeID: "worker-metrics"}
	handler := server.Handler()

	body := `{"module_name":"echo","module_url":"` + moduleServer.URL + `","payload":"hello metrics"}`
	execRec := httptest.NewRecorder()
	handler.ServeHTTP(execRec, httptest.NewRequest(http.MethodPost, "/invoke", strings.NewReader(body)))
	if execRec.Code != http.StatusOK {
		t.Fatalf("expected invoke status 200, got %d with body %s", execRec.Code, execRec.Body.String())
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/wasmcat/metrics", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected metrics status 200, got %d with body %s", rec.Code, rec.Body.String())
	}

	var response shared.MetricsResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode metrics response: %v", err)
	}
	if response.Role != "worker" || response.NodeID != "worker-metrics" {
		t.Fatalf("unexpected metrics identity: %+v", response)
	}
	if response.Worker == nil {
		t.Fatal("expected worker metrics")
	}
	if response.Worker.ExecutionSuccess != 1 {
		t.Fatalf("expected one execution success, got %+v", response.Worker)
	}
	if response.Worker.Cache.Entries != 1 {
		t.Fatalf("expected one cached module, got %+v", response.Worker.Cache)
	}
	if response.RequestsByPath["/invoke"] != 1 {
		t.Fatalf("expected invoke request count, got %+v", response.RequestsByPath)
	}
}

func TestWorkerMetricsCountsExecutionFailure(t *testing.T) {
	engine := worker.NewWasmEngineWithLimits(context.Background(), worker.Limits{MaxPayloadBytes: 4})
	server := &worker.WorkerServer{Engine: engine, NodeID: "worker-metrics"}
	handler := server.Handler()

	body := `{"module_name":"echo","module_url":"http://module.test/echo.wasm","payload":"too large"}`
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/invoke", strings.NewReader(body)))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/wasmcat/metrics", nil))

	var response shared.MetricsResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode metrics response: %v", err)
	}
	if response.Worker == nil || response.Worker.ExecutionFailure != 1 {
		t.Fatalf("expected one execution failure, got %+v", response.Worker)
	}
}

func TestWorkerMetricsCountsModuleDigestFailures(t *testing.T) {
	moduleServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/wasm")
		w.Write(wasmEchoModule())
	}))
	defer moduleServer.Close()

	engine := worker.NewWasmEngine(context.Background())
	server := &worker.WorkerServer{Engine: engine, NodeID: "worker-metrics"}
	handler := server.Handler()

	requests := []string{
		`{"module_name":"echo","module_url":"` + moduleServer.URL + `","module_digest":"md5:abc","payload":"hello"}`,
		`{"module_name":"echo","module_url":"` + moduleServer.URL + `","module_digest":"sha256:0000000000000000000000000000000000000000000000000000000000000000","payload":"hello"}`,
	}

	for _, body := range requests {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/invoke", strings.NewReader(body)))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected digest failure status 400, got %d with body %s", rec.Code, rec.Body.String())
		}
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/wasmcat/metrics", nil))

	var response shared.MetricsResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode metrics response: %v", err)
	}
	if response.Worker == nil {
		t.Fatal("expected worker metrics")
	}
	if response.Worker.ExecutionFailure != 2 {
		t.Fatalf("expected two execution failures, got %+v", response.Worker)
	}
	if response.Worker.ModuleDigestInvalid != 1 {
		t.Fatalf("expected one invalid digest failure, got %+v", response.Worker)
	}
	if response.Worker.ModuleDigestMismatch != 1 {
		t.Fatalf("expected one digest mismatch failure, got %+v", response.Worker)
	}
}
