package master_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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
		ModuleName: "echo",
		ModuleURL:  "http://module.test/echo.wasm",
	})
	if err != nil {
		t.Fatalf("Dispatch returned error: %v", err)
	}
	if resp.ExecutedOnNodeID != "worker-test" {
		t.Fatalf("expected executed node worker-test, got %q", resp.ExecutedOnNodeID)
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

type assertErr string

func (e assertErr) Error() string {
	return string(e)
}
