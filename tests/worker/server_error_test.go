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
	if errResp.Error != "Invalid execution request." {
		t.Fatalf("expected safe validation error, got %q", errResp.Error)
	}
	if strings.Contains(errResp.Error, "module_name") {
		t.Fatalf("public error leaked internal validation detail: %q", errResp.Error)
	}
}

func TestWorkerInvokeReturnsUnavailableWhenEngineMissing(t *testing.T) {
	server := &worker.WorkerServer{NodeID: "worker-test"}

	req := httptest.NewRequest(http.MethodPost, "/invoke", strings.NewReader(`{"module_name":"echo","module_url":"http://module.test/echo.wasm","payload":"hello"}`))
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503, got %d", rec.Code)
	}

	var errResp shared.ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&errResp); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if errResp.Code != "not_ready" {
		t.Fatalf("expected not_ready code, got %q", errResp.Code)
	}
}

func TestWorkerInvokeReturnsModuleDigestErrorCodes(t *testing.T) {
	moduleServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/wasm")
		w.Write(wasmEchoModule())
	}))
	defer moduleServer.Close()

	tests := map[string]struct {
		digest      string
		wantCode    string
		wantMessage string
	}{
		"invalid digest format": {
			digest:      "md5:abc",
			wantCode:    "module_digest_invalid",
			wantMessage: "Module digest is invalid.",
		},
		"digest mismatch": {
			digest:      "sha256:0000000000000000000000000000000000000000000000000000000000000000",
			wantCode:    "module_digest_mismatch",
			wantMessage: "Module digest does not match downloaded content.",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			engine := worker.NewWasmEngine(context.Background())
			server := &worker.WorkerServer{Engine: engine, NodeID: "worker-test"}
			body := `{"module_name":"echo","module_url":"` + moduleServer.URL + `","module_digest":"` + tt.digest + `","payload":"hello"}`
			req := httptest.NewRequest(http.MethodPost, "/invoke", strings.NewReader(body))
			rec := httptest.NewRecorder()

			server.Handler().ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected status 400, got %d", rec.Code)
			}

			var errResp shared.ErrorResponse
			if err := json.NewDecoder(rec.Body).Decode(&errResp); err != nil {
				t.Fatalf("decode error response: %v", err)
			}
			if errResp.Code != tt.wantCode {
				t.Fatalf("expected code %q, got %q", tt.wantCode, errResp.Code)
			}
			if errResp.Error != tt.wantMessage {
				t.Fatalf("expected message %q, got %q", tt.wantMessage, errResp.Error)
			}
		})
	}
}
