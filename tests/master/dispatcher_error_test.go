package master_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"wasmcat/internal/master"
	"wasmcat/internal/shared"
)

func TestDispatcherReturnsErrorOnWorkerNonOK(t *testing.T) {
	workerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		shared.WriteError(w, http.StatusBadGateway, "worker_failed", assertErr("boom"))
	}))
	defer workerServer.Close()

	dispatcher := newTestDispatcher(workerServer.URL)
	_, err := dispatcher.Dispatch(context.Background(), shared.ExecutionRequest{
		ModuleName: "echo",
		ModuleURL:  "http://module.test/echo.wasm",
	})
	if err == nil {
		t.Fatal("expected worker status error")
	}
	if !strings.Contains(err.Error(), "502 Bad Gateway") {
		t.Fatalf("expected worker status in error, got %v", err)
	}
}

func TestDispatcherReturnsErrorOnInvalidWorkerJSON(t *testing.T) {
	workerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`not-json`))
	}))
	defer workerServer.Close()

	dispatcher := newTestDispatcher(workerServer.URL)
	_, err := dispatcher.Dispatch(context.Background(), shared.ExecutionRequest{
		ModuleName: "echo",
		ModuleURL:  "http://module.test/echo.wasm",
	})
	if err == nil {
		t.Fatal("expected decode error")
	}
	if !strings.Contains(err.Error(), "decode worker response") {
		t.Fatalf("expected decode worker response error, got %v", err)
	}
}

func TestDispatcherSetsExecutedOnNodeID(t *testing.T) {
	workerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		shared.WriteJSON(w, http.StatusOK, shared.ExecutionResponse{Result: "ok"})
	}))
	defer workerServer.Close()

	dispatcher := newTestDispatcher(workerServer.URL)
	resp, err := dispatcher.Dispatch(context.Background(), shared.ExecutionRequest{
		RequestID:  "req-test-dispatch",
		ModuleName: "echo",
		ModuleURL:  "http://module.test/echo.wasm",
	})
	if err != nil {
		t.Fatalf("Dispatch returned error: %v", err)
	}
	if resp.ExecutedOnNodeID != "worker-test" {
		t.Fatalf("expected executed node worker-test, got %q", resp.ExecutedOnNodeID)
	}
	if resp.RequestID != "req-test-dispatch" {
		t.Fatalf("expected request id fallback, got %q", resp.RequestID)
	}
}

func TestDispatcherRetriesRetryableWorkerFailureOnAnotherWorker(t *testing.T) {
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

	dispatcher := newMultiWorkerDispatcher([]shared.WorkerNode{
		{
			ID:        "worker-unavailable",
			IPAddress: failedWorker.URL,
			Latitude:  0,
			Longitude: 0,
		},
		{
			ID:        "worker-healthy",
			IPAddress: healthyWorker.URL,
			Latitude:  10,
			Longitude: 10,
		},
	})

	resp, err := dispatcher.Dispatch(context.Background(), shared.ExecutionRequest{
		ModuleName: "echo",
		ModuleURL:  "http://module.test/echo.wasm",
		UserLat:    0,
		UserLon:    0,
	})
	if err != nil {
		t.Fatalf("Dispatch returned error: %v", err)
	}
	if resp.ExecutedOnNodeID != "worker-healthy" {
		t.Fatalf("expected retry to execute on worker-healthy, got %q", resp.ExecutedOnNodeID)
	}
	if atomic.LoadInt32(&failedWorkerHits) != 1 {
		t.Fatalf("expected one failed worker hit, got %d", failedWorkerHits)
	}
	if atomic.LoadInt32(&healthyWorkerHits) != 1 {
		t.Fatalf("expected one healthy worker hit, got %d", healthyWorkerHits)
	}
}

func TestDispatcherDoesNotRetryWorkerExecutionFailure(t *testing.T) {
	var failedWorkerHits int32
	var healthyWorkerHits int32

	failedWorker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&failedWorkerHits, 1)
		shared.WriteError(w, http.StatusBadRequest, "execution_failed", assertErr("module rejected payload"))
	}))
	defer failedWorker.Close()

	healthyWorker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&healthyWorkerHits, 1)
		shared.WriteJSON(w, http.StatusOK, shared.ExecutionResponse{Result: "unexpected"})
	}))
	defer healthyWorker.Close()

	dispatcher := newMultiWorkerDispatcher([]shared.WorkerNode{
		{
			ID:        "worker-execution-error",
			IPAddress: failedWorker.URL,
			Latitude:  0,
			Longitude: 0,
		},
		{
			ID:        "worker-healthy",
			IPAddress: healthyWorker.URL,
			Latitude:  10,
			Longitude: 10,
		},
	})

	_, err := dispatcher.Dispatch(context.Background(), shared.ExecutionRequest{
		ModuleName: "echo",
		ModuleURL:  "http://module.test/echo.wasm",
		UserLat:    0,
		UserLon:    0,
	})
	if err == nil {
		t.Fatal("expected execution failure")
	}
	if !strings.Contains(err.Error(), "400 Bad Request") {
		t.Fatalf("expected original worker error, got %v", err)
	}
	if atomic.LoadInt32(&failedWorkerHits) != 1 {
		t.Fatalf("expected one failed worker hit, got %d", failedWorkerHits)
	}
	if atomic.LoadInt32(&healthyWorkerHits) != 0 {
		t.Fatalf("expected healthy worker not to be hit, got %d", healthyWorkerHits)
	}
}

func TestGatewayReturnsJSONErrorForInvalidExecutionRequest(t *testing.T) {
	gateway := &master.Gateway{
		Registry:   master.NewRegistry(),
		Dispatcher: &master.Dispatcher{Registry: master.NewRegistry(), Scheduler: &master.Scheduler{}, Client: http.DefaultClient},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/execute", strings.NewReader(`{"payload":"missing module"}`))
	rec := httptest.NewRecorder()

	gateway.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rec.Code)
	}
	if contentType := rec.Header().Get("Content-Type"); !strings.Contains(contentType, "application/json") {
		t.Fatalf("expected JSON content type, got %q", contentType)
	}

	var errResp shared.ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&errResp); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if errResp.Code != "invalid_execution_request" {
		t.Fatalf("expected invalid_execution_request code, got %q", errResp.Code)
	}
}

func TestGatewayGeneratesRequestIDBeforeDispatch(t *testing.T) {
	var forwarded shared.ExecutionRequest
	workerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&forwarded); err != nil {
			t.Fatalf("decode forwarded request: %v", err)
		}
		shared.WriteJSON(w, http.StatusOK, shared.ExecutionResponse{
			RequestID: forwarded.RequestID,
			Result:    "ok",
		})
	}))
	defer workerServer.Close()

	registry := master.NewRegistry()
	registry.RegisterWorker(shared.WorkerNode{
		ID:        "worker-test",
		IPAddress: workerServer.URL,
	})
	gateway := &master.Gateway{
		Registry: registry,
		Dispatcher: &master.Dispatcher{
			Registry:  registry,
			Scheduler: &master.Scheduler{},
			Client:    http.DefaultClient,
		},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/execute", strings.NewReader(`{"module_name":"echo","module_url":"http://module.test/echo.wasm","payload":"hello"}`))
	rec := httptest.NewRecorder()

	gateway.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d with body %s", rec.Code, rec.Body.String())
	}
	if forwarded.RequestID == "" {
		t.Fatal("expected generated request id to be forwarded")
	}

	var response shared.ExecutionResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode execution response: %v", err)
	}
	if response.RequestID != forwarded.RequestID {
		t.Fatalf("expected response request id %q, got %q", forwarded.RequestID, response.RequestID)
	}
}

func TestGatewayPreservesClientRequestID(t *testing.T) {
	const requestID = "client-request-123"
	var forwarded shared.ExecutionRequest
	workerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&forwarded); err != nil {
			t.Fatalf("decode forwarded request: %v", err)
		}
		shared.WriteJSON(w, http.StatusOK, shared.ExecutionResponse{
			RequestID: forwarded.RequestID,
			Result:    "ok",
		})
	}))
	defer workerServer.Close()

	registry := master.NewRegistry()
	registry.RegisterWorker(shared.WorkerNode{
		ID:        "worker-test",
		IPAddress: workerServer.URL,
	})
	gateway := &master.Gateway{
		Registry: registry,
		Dispatcher: &master.Dispatcher{
			Registry:  registry,
			Scheduler: &master.Scheduler{},
			Client:    http.DefaultClient,
		},
	}

	body := `{"request_id":"` + requestID + `","module_name":"echo","module_url":"http://module.test/echo.wasm","payload":"hello"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/execute", strings.NewReader(body))
	rec := httptest.NewRecorder()

	gateway.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d with body %s", rec.Code, rec.Body.String())
	}
	if forwarded.RequestID != requestID {
		t.Fatalf("expected forwarded request id %q, got %q", requestID, forwarded.RequestID)
	}
}

func newTestDispatcher(workerURL string) *master.Dispatcher {
	registry := master.NewRegistry()
	registry.RegisterWorker(shared.WorkerNode{
		ID:        "worker-test",
		IPAddress: workerURL,
	})

	return &master.Dispatcher{
		Registry:  registry,
		Scheduler: &master.Scheduler{},
		Client:    http.DefaultClient,
	}
}

func newMultiWorkerDispatcher(workers []shared.WorkerNode) *master.Dispatcher {
	registry := master.NewRegistry()
	for _, worker := range workers {
		registry.RegisterWorker(worker)
	}

	return &master.Dispatcher{
		Registry:  registry,
		Scheduler: &master.Scheduler{},
		Client:    http.DefaultClient,
	}
}

type assertErr string

func (e assertErr) Error() string {
	return string(e)
}
