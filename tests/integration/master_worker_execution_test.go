package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"wasmcat/internal/master"
	"wasmcat/internal/shared"
	"wasmcat/internal/worker"
)

func TestMasterGatewayDispatchesThroughWorkerEngine(t *testing.T) {
	moduleServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/wasm")
		w.Write(wasmEchoModule())
	}))
	defer moduleServer.Close()

	workerEngine := worker.NewWasmEngineWithLimits(context.Background(), worker.Limits{
		ExecutionTimeout:   0,
		ModuleFetchTimeout: 0,
		MaxModuleBytes:     1 << 20,
		MaxPayloadBytes:    1 << 20,
		MaxOutputBytes:     1 << 20,
		MaxConcurrentExecs: 2,
	})
	workerServer := &worker.WorkerServer{
		Engine: workerEngine,
		NodeID: "worker-integration",
	}

	workerHTTP := httptest.NewServer(workerServer.Handler())
	defer workerHTTP.Close()

	registry := master.NewRegistry()
	registry.RegisterWorker(shared.WorkerNode{
		ID:        "worker-integration",
		IPAddress: workerHTTP.URL,
		Latitude:  13.7563,
		Longitude: 100.5018,
		CPUFree:   75,
		RAMFreeMB: 4096,
	})

	gateway := &master.Gateway{
		Registry: registry,
		Dispatcher: &master.Dispatcher{
			Registry: registry,
			Scheduler: &master.Scheduler{
				MinCPUFree:   10,
				MinRAMFreeMB: 512,
			},
			Client: http.DefaultClient,
		},
	}

	request := shared.ExecutionRequest{
		ModuleName: "echo-integration",
		ModuleURL:  moduleServer.URL,
		Payload:    "master to worker to wasm",
		UserLat:    13.7563,
		UserLon:    100.5018,
	}
	body, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal execution request: %v", err)
	}

	httpReq := httptest.NewRequest(http.MethodPost, "/api/v1/execute", bytesReader(body))
	rec := httptest.NewRecorder()

	gateway.Handler().ServeHTTP(rec, httpReq)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d with body %s", rec.Code, rec.Body.String())
	}

	var response shared.ExecutionResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode execution response: %v", err)
	}
	if response.Result != request.Payload {
		t.Fatalf("expected echo result %q, got %q", request.Payload, response.Result)
	}
	if response.ExecutedOnNodeID != "worker-integration" {
		t.Fatalf("expected worker-integration, got %q", response.ExecutedOnNodeID)
	}
	if response.RequestID == "" {
		t.Fatal("expected generated request id in execution response")
	}
	if response.ExecutionTimeMs <= 0 {
		t.Fatalf("expected positive execution time, got %f", response.ExecutionTimeMs)
	}
}

func bytesReader(data []byte) *bytes.Reader {
	return bytes.NewReader(data)
}

func wasmEchoModule() []byte {
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
