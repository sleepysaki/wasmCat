package worker_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"wasmcat/internal/worker"
)

func TestExecuteRejectsOversizedPayload(t *testing.T) {
	ctx := context.Background()
	engine := worker.NewWasmEngineWithLimits(ctx, worker.Limits{MaxPayloadBytes: 4})

	_, err := engine.Execute(ctx, "echo", "http://example.invalid/module.wasm", "too large", "")
	if err == nil {
		t.Fatal("expected payload size error")
	}
	if !strings.Contains(err.Error(), "payload exceeds max size") {
		t.Fatalf("expected payload size error, got %v", err)
	}
}

func TestWorkerLimitsDefaultShutdownTimeoutFollowsExecutionTimeout(t *testing.T) {
	ctx := context.Background()
	engine := worker.NewWasmEngineWithLimits(ctx, worker.Limits{ExecutionTimeout: 20 * time.Second})

	if engine.Limits().ShutdownTimeout != 25*time.Second {
		t.Fatalf("expected shutdown timeout 25s, got %s", engine.Limits().ShutdownTimeout)
	}
}

func TestFetchAndCacheRejectsOversizedModule(t *testing.T) {
	ctx := context.Background()

	moduleServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(bytes.Repeat([]byte{0x00}, 8))
	}))
	defer moduleServer.Close()

	engine := worker.NewWasmEngineWithLimits(ctx, worker.Limits{MaxModuleBytes: 4})
	err := engine.FetchAndCache(ctx, "large-module", moduleServer.URL, "")
	if err == nil {
		t.Fatal("expected module size error")
	}
	if !strings.Contains(err.Error(), "exceeds max size") {
		t.Fatalf("expected module size error, got %v", err)
	}
}

func TestExecuteRejectsOversizedOutput(t *testing.T) {
	ctx := context.Background()

	moduleServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(wasmEchoModule())
	}))
	defer moduleServer.Close()

	engine := worker.NewWasmEngineWithLimits(ctx, worker.Limits{
		MaxPayloadBytes: 64,
		MaxOutputBytes:  4,
	})

	_, err := engine.Execute(ctx, "echo", moduleServer.URL, "hello", "")
	if err == nil {
		t.Fatal("expected output size error")
	}
	if !strings.Contains(err.Error(), "output exceeds max size") {
		t.Fatalf("expected output size error, got %v", err)
	}
}

func TestFetchAndCacheHonorsFetchTimeout(t *testing.T) {
	ctx := context.Background()

	moduleServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.Write(wasmEchoModule())
	}))
	defer moduleServer.Close()

	engine := worker.NewWasmEngineWithLimits(ctx, worker.Limits{ModuleFetchTimeout: 10 * time.Millisecond})
	err := engine.FetchAndCache(ctx, "slow-fetch", moduleServer.URL, "")
	if err == nil {
		t.Fatal("expected fetch timeout error")
	}
}

func TestExecuteRejectsWhenAtCapacity(t *testing.T) {
	ctx := context.Background()

	requestStarted := make(chan struct{})
	releaseRequest := make(chan struct{})
	moduleServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(requestStarted)
		<-releaseRequest
		w.Write(wasmEchoModule())
	}))
	defer moduleServer.Close()

	engine := worker.NewWasmEngineWithLimits(ctx, worker.Limits{MaxConcurrentExecs: 1})
	done := make(chan error, 1)
	go func() {
		_, err := engine.Execute(ctx, "blocked", moduleServer.URL, "ok", "")
		done <- err
	}()

	<-requestStarted
	_, err := engine.Execute(ctx, "second", moduleServer.URL, "ok", "")
	if err == nil {
		t.Fatal("expected capacity error")
	}
	if !strings.Contains(err.Error(), "max execution capacity") {
		t.Fatalf("expected capacity error, got %v", err)
	}

	close(releaseRequest)
	if err := <-done; err != nil {
		t.Fatalf("first execution should finish after release, got %v", err)
	}
}

func TestExecuteHonorsExecutionTimeout(t *testing.T) {
	ctx := context.Background()

	moduleServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(wasmInfiniteLoopModule())
	}))
	defer moduleServer.Close()

	engine := worker.NewWasmEngineWithLimits(ctx, worker.Limits{
		ExecutionTimeout: 20 * time.Millisecond,
		MaxPayloadBytes:  64,
	})

	_, err := engine.Execute(ctx, "loop", moduleServer.URL, "hello", "")
	if err == nil {
		t.Fatal("expected execution timeout error")
	}
}

func TestWorkerServerRejectsOversizedRequestBody(t *testing.T) {
	ctx := context.Background()
	engine := worker.NewWasmEngineWithLimits(ctx, worker.Limits{MaxPayloadBytes: 1})
	server := &worker.WorkerServer{Engine: engine, NodeID: "worker-test"}

	body := `{"module_name":"echo","module_url":"http://example.invalid/module.wasm","payload":"` +
		strings.Repeat("x", 5000) +
		`"}`

	req := httptest.NewRequest(http.MethodPost, "/invoke", strings.NewReader(body))
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rec.Code)
	}
}
