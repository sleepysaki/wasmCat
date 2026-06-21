package master_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"wasmcat/internal/master"
	"wasmcat/internal/shared"
)

func TestGatewayReturnsCachedResponseForCompletedRequestID(t *testing.T) {
	var workerHits int32
	workerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&workerHits, 1)
		shared.WriteJSON(w, http.StatusOK, shared.ExecutionResponse{
			RequestID: "req-cache-hit",
			Result:    "cached-result",
		})
	}))
	defer workerServer.Close()

	handler := newGatewayWithWorker(workerServer.URL).Handler()
	body := `{"request_id":"req-cache-hit","module_name":"echo","module_url":"http://module.test/echo.wasm","payload":"hello"}`

	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodPost, "/api/v1/execute", strings.NewReader(body)))
	if first.Code != http.StatusOK {
		t.Fatalf("expected first status 200, got %d with body %s", first.Code, first.Body.String())
	}

	second := httptest.NewRecorder()
	handler.ServeHTTP(second, httptest.NewRequest(http.MethodPost, "/api/v1/execute", strings.NewReader(body)))
	if second.Code != http.StatusOK {
		t.Fatalf("expected cached status 200, got %d with body %s", second.Code, second.Body.String())
	}
	if atomic.LoadInt32(&workerHits) != 1 {
		t.Fatalf("expected worker to be called once, got %d", workerHits)
	}

	var response shared.ExecutionResponse
	if err := json.NewDecoder(second.Body).Decode(&response); err != nil {
		t.Fatalf("decode cached response: %v", err)
	}
	if response.Result != "cached-result" || response.RequestID != "req-cache-hit" {
		t.Fatalf("unexpected cached response: %+v", response)
	}
}

func TestGatewayRejectsConcurrentDuplicateRequestID(t *testing.T) {
	workerStarted := make(chan struct{})
	releaseWorker := make(chan struct{})

	workerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(workerStarted)
		<-releaseWorker
		shared.WriteJSON(w, http.StatusOK, shared.ExecutionResponse{
			RequestID: "req-in-flight",
			Result:    "ok",
		})
	}))
	defer workerServer.Close()

	handler := newGatewayWithWorker(workerServer.URL).Handler()
	body := `{"request_id":"req-in-flight","module_name":"echo","module_url":"http://module.test/echo.wasm","payload":"hello"}`

	firstDone := make(chan int, 1)
	go func() {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/execute", strings.NewReader(body)))
		firstDone <- rec.Code
	}()

	<-workerStarted

	duplicate := httptest.NewRecorder()
	handler.ServeHTTP(duplicate, httptest.NewRequest(http.MethodPost, "/api/v1/execute", strings.NewReader(body)))
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("expected duplicate status 409, got %d with body %s", duplicate.Code, duplicate.Body.String())
	}

	var errResp shared.ErrorResponse
	if err := json.NewDecoder(duplicate.Body).Decode(&errResp); err != nil {
		t.Fatalf("decode duplicate error response: %v", err)
	}
	if errResp.Code != "request_in_progress" {
		t.Fatalf("expected request_in_progress, got %q", errResp.Code)
	}

	close(releaseWorker)
	if status := <-firstDone; status != http.StatusOK {
		t.Fatalf("expected first request status 200, got %d", status)
	}
}

func TestGatewayRejectsRequestIDReuseWithDifferentPayload(t *testing.T) {
	workerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		shared.WriteJSON(w, http.StatusOK, shared.ExecutionResponse{
			RequestID: "req-conflict",
			Result:    "ok",
		})
	}))
	defer workerServer.Close()

	handler := newGatewayWithWorker(workerServer.URL).Handler()
	firstBody := `{"request_id":"req-conflict","module_name":"echo","module_url":"http://module.test/echo.wasm","payload":"hello"}`
	secondBody := `{"request_id":"req-conflict","module_name":"echo","module_url":"http://module.test/echo.wasm","payload":"different"}`

	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodPost, "/api/v1/execute", strings.NewReader(firstBody)))
	if first.Code != http.StatusOK {
		t.Fatalf("expected first status 200, got %d with body %s", first.Code, first.Body.String())
	}

	second := httptest.NewRecorder()
	handler.ServeHTTP(second, httptest.NewRequest(http.MethodPost, "/api/v1/execute", strings.NewReader(secondBody)))
	if second.Code != http.StatusConflict {
		t.Fatalf("expected conflict status 409, got %d with body %s", second.Code, second.Body.String())
	}

	var errResp shared.ErrorResponse
	if err := json.NewDecoder(second.Body).Decode(&errResp); err != nil {
		t.Fatalf("decode conflict response: %v", err)
	}
	if errResp.Code != "request_id_conflict" {
		t.Fatalf("expected request_id_conflict, got %q", errResp.Code)
	}
}

func TestGatewayForgetsFailedRequestID(t *testing.T) {
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
	body := `{"request_id":"req-retry-after-failure","module_name":"echo","module_url":"http://module.test/echo.wasm","payload":"hello"}`

	failed := httptest.NewRecorder()
	handler.ServeHTTP(failed, httptest.NewRequest(http.MethodPost, "/api/v1/execute", strings.NewReader(body)))
	if failed.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected dispatch failure 503, got %d with body %s", failed.Code, failed.Body.String())
	}

	workerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		shared.WriteJSON(w, http.StatusOK, shared.ExecutionResponse{
			RequestID: "req-retry-after-failure",
			Result:    "ok-after-retry",
		})
	}))
	defer workerServer.Close()
	registry.RegisterWorker(shared.WorkerNode{
		ID:        "worker-after-failure",
		IPAddress: workerServer.URL,
	})

	retry := httptest.NewRecorder()
	handler.ServeHTTP(retry, httptest.NewRequest(http.MethodPost, "/api/v1/execute", strings.NewReader(body)))
	if retry.Code != http.StatusOK {
		t.Fatalf("expected retry status 200, got %d with body %s", retry.Code, retry.Body.String())
	}
}

func TestGatewayReplaysDurableCompletedRequestIDAfterStoreReopen(t *testing.T) {
	var workerHits int32
	workerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&workerHits, 1)
		shared.WriteJSON(w, http.StatusOK, shared.ExecutionResponse{
			RequestID: "req-durable-cache",
			Result:    "durable-result",
		})
	}))
	defer workerServer.Close()

	storePath := filepath.Join(t.TempDir(), "jobs.db")
	firstStore := newTestJobStore(t, storePath)
	firstGateway := newGatewayWithWorker(workerServer.URL)
	firstGateway.JobStore = firstStore

	body := `{"request_id":"req-durable-cache","module_name":"echo","module_url":"http://module.test/echo.wasm","payload":"hello"}`
	first := httptest.NewRecorder()
	firstGateway.Handler().ServeHTTP(first, httptest.NewRequest(http.MethodPost, "/api/v1/execute", strings.NewReader(body)))
	if first.Code != http.StatusOK {
		t.Fatalf("expected first status 200, got %d with body %s", first.Code, first.Body.String())
	}
	if err := firstStore.Close(); err != nil {
		t.Fatalf("close first store: %v", err)
	}

	secondStore := newTestJobStore(t, storePath)
	secondGateway := newGatewayWithWorker(workerServer.URL)
	secondGateway.JobStore = secondStore

	second := httptest.NewRecorder()
	secondGateway.Handler().ServeHTTP(second, httptest.NewRequest(http.MethodPost, "/api/v1/execute", strings.NewReader(body)))
	if second.Code != http.StatusOK {
		t.Fatalf("expected durable cached status 200, got %d with body %s", second.Code, second.Body.String())
	}
	if atomic.LoadInt32(&workerHits) != 1 {
		t.Fatalf("expected durable replay to avoid a second worker call, got %d calls", workerHits)
	}

	var response shared.ExecutionResponse
	if err := json.NewDecoder(second.Body).Decode(&response); err != nil {
		t.Fatalf("decode durable cached response: %v", err)
	}
	if response.Result != "durable-result" || response.RequestID != "req-durable-cache" {
		t.Fatalf("unexpected durable cached response: %+v", response)
	}
}

func TestGatewayDurableStoreRejectsRequestIDReuseWithDifferentPayload(t *testing.T) {
	workerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		shared.WriteJSON(w, http.StatusOK, shared.ExecutionResponse{
			RequestID: "req-durable-conflict",
			Result:    "ok",
		})
	}))
	defer workerServer.Close()

	gateway := newGatewayWithWorker(workerServer.URL)
	gateway.JobStore = newTestJobStore(t, filepath.Join(t.TempDir(), "jobs.db"))
	handler := gateway.Handler()

	firstBody := `{"request_id":"req-durable-conflict","module_name":"echo","module_url":"http://module.test/echo.wasm","payload":"hello"}`
	secondBody := `{"request_id":"req-durable-conflict","module_name":"echo","module_url":"http://module.test/echo.wasm","payload":"different"}`

	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodPost, "/api/v1/execute", strings.NewReader(firstBody)))
	if first.Code != http.StatusOK {
		t.Fatalf("expected first status 200, got %d with body %s", first.Code, first.Body.String())
	}

	second := httptest.NewRecorder()
	handler.ServeHTTP(second, httptest.NewRequest(http.MethodPost, "/api/v1/execute", strings.NewReader(secondBody)))
	if second.Code != http.StatusConflict {
		t.Fatalf("expected conflict status 409, got %d with body %s", second.Code, second.Body.String())
	}

	var errResp shared.ErrorResponse
	if err := json.NewDecoder(second.Body).Decode(&errResp); err != nil {
		t.Fatalf("decode conflict response: %v", err)
	}
	if errResp.Code != "request_id_conflict" {
		t.Fatalf("expected request_id_conflict, got %q", errResp.Code)
	}
}

func newGatewayWithWorker(workerURL string) *master.Gateway {
	registry := master.NewRegistry()
	registry.RegisterWorker(shared.WorkerNode{
		ID:        "worker-idempotency",
		IPAddress: workerURL,
	})

	return &master.Gateway{
		Registry: registry,
		Dispatcher: &master.Dispatcher{
			Registry:  registry,
			Scheduler: &master.Scheduler{},
			Client:    http.DefaultClient,
		},
	}
}
