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

func TestWorkerServerReturnsJSONErrorForInvalidRequest(t *testing.T) {
	server := &worker.WorkerServer{
		Engine: worker.NewWasmEngineWithLimits(context.Background(), worker.Limits{MaxPayloadBytes: 128}),
		NodeID: "worker-test",
	}

	req := httptest.NewRequest(http.MethodPost, "/invoke", strings.NewReader(`{"payload":"missing module"}`))
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

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
	if !strings.Contains(errResp.Error, "module_name is required") {
		t.Fatalf("expected validation error, got %q", errResp.Error)
	}
}
