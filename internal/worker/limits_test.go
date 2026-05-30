package worker

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestExecuteRejectsOversizedPayload(t *testing.T) {
	ctx := context.Background()
	engine := NewWasmEngineWithLimits(ctx, Limits{MaxPayloadBytes: 4})

	_, err := engine.Execute(ctx, "echo", "http://example.invalid/module.wasm", "too large", "")
	if err == nil {
		t.Fatal("expected payload size error")
	}
	if !strings.Contains(err.Error(), "payload exceeds max size") {
		t.Fatalf("expected payload size error, got %v", err)
	}
}

func TestFetchAndCacheRejectsOversizedModule(t *testing.T) {
	ctx := context.Background()

	moduleServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(bytes.Repeat([]byte{0x00}, 8))
	}))
	defer moduleServer.Close()

	engine := NewWasmEngineWithLimits(ctx, Limits{MaxModuleBytes: 4})
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
		w.Write(wasmEchoModuleForLimitsTest())
	}))
	defer moduleServer.Close()

	engine := NewWasmEngineWithLimits(ctx, Limits{
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
		w.Write(wasmEchoModuleForLimitsTest())
	}))
	defer moduleServer.Close()

	engine := NewWasmEngineWithLimits(ctx, Limits{ModuleFetchTimeout: 10 * time.Millisecond})
	err := engine.FetchAndCache(ctx, "slow-fetch", moduleServer.URL, "")
	if err == nil {
		t.Fatal("expected fetch timeout error")
	}
}

func TestExecuteRejectsWhenAtCapacity(t *testing.T) {
	ctx := context.Background()
	engine := NewWasmEngineWithLimits(ctx, Limits{MaxConcurrentExecs: 1})

	// Fill the semaphore manually to simulate one execution already running.
	// This checks the backpressure path without depending on timing between goroutines.
	engine.sem <- struct{}{}
	defer func() { <-engine.sem }()

	_, err := engine.Execute(ctx, "echo", "http://example.invalid/module.wasm", "ok", "")
	if err == nil {
		t.Fatal("expected capacity error")
	}
	if !strings.Contains(err.Error(), "max execution capacity") {
		t.Fatalf("expected capacity error, got %v", err)
	}
}

func TestExecuteHonorsExecutionTimeout(t *testing.T) {
	ctx := context.Background()

	moduleServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(wasmInfiniteLoopModuleForLimitsTest())
	}))
	defer moduleServer.Close()

	engine := NewWasmEngineWithLimits(ctx, Limits{
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
	engine := NewWasmEngineWithLimits(ctx, Limits{MaxPayloadBytes: 1})
	server := &WorkerServer{Engine: engine, NodeID: "worker-test"}

	body := `{"module_name":"echo","module_url":"http://example.invalid/module.wasm","payload":"` +
		strings.Repeat("x", 5000) +
		`"}`

	req := httptest.NewRequest(http.MethodPost, "/invoke", strings.NewReader(body))
	rec := httptest.NewRecorder()

	server.handleInvoke(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rec.Code)
	}
}

func wasmEchoModuleForLimitsTest() []byte {
	return []byte{
		0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,
		0x01, 0x0c, 0x02, 0x60, 0x01, 0x7f, 0x01, 0x7f,
		0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7e,
		0x03, 0x03, 0x02, 0x00, 0x01,
		0x05, 0x03, 0x01, 0x00, 0x01,
		0x06, 0x07, 0x01, 0x7f, 0x01, 0x41, 0x80, 0x08, 0x0b,
		0x07, 0x19, 0x03,
		0x06, 0x6d, 0x65, 0x6d, 0x6f, 0x72, 0x79, 0x02, 0x00,
		0x06, 0x6d, 0x61, 0x6c, 0x6c, 0x6f, 0x63, 0x00, 0x00,
		0x03, 0x72, 0x75, 0x6e, 0x00, 0x01,
		0x0a, 0x1a, 0x02,
		0x0b, 0x00, 0x23, 0x00, 0x23, 0x00, 0x20, 0x00, 0x6a, 0x24, 0x00, 0x0b,
		0x0c, 0x00, 0x20, 0x00, 0xad, 0x42, 0x20, 0x86, 0x20, 0x01, 0xad, 0x84, 0x0b,
	}
}

func wasmInfiniteLoopModuleForLimitsTest() []byte {
	return []byte{
		0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,
		0x01, 0x0c, 0x02, 0x60, 0x01, 0x7f, 0x01, 0x7f,
		0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7e,
		0x03, 0x03, 0x02, 0x00, 0x01,
		0x05, 0x03, 0x01, 0x00, 0x01,
		0x06, 0x07, 0x01, 0x7f, 0x01, 0x41, 0x80, 0x08, 0x0b,
		0x07, 0x19, 0x03,
		0x06, 0x6d, 0x65, 0x6d, 0x6f, 0x72, 0x79, 0x02, 0x00,
		0x06, 0x6d, 0x61, 0x6c, 0x6c, 0x6f, 0x63, 0x00, 0x00,
		0x03, 0x72, 0x75, 0x6e, 0x00, 0x01,
		0x0a, 0x16, 0x02,
		0x0b, 0x00, 0x23, 0x00, 0x23, 0x00, 0x20, 0x00, 0x6a, 0x24, 0x00, 0x0b,
		0x08, 0x00, 0x03, 0x40, 0x0c, 0x00, 0x0b, 0x00, 0x0b,
	}
}
