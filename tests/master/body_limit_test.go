package master_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"wasmcat/internal/master"
	"wasmcat/internal/shared"
)

func TestGatewayExecuteRejectsOversizedBody(t *testing.T) {
	gateway := &master.Gateway{
		Registry: master.NewRegistry(),
		Dispatcher: &master.Dispatcher{
			Registry:  master.NewRegistry(),
			Scheduler: &master.Scheduler{},
			Client:    http.DefaultClient,
		},
		MaxExecuteBodyBytes: 64,
	}

	body := `{"module_name":"echo","module_url":"http://module.test/echo.wasm","payload":"` + strings.Repeat("x", 128) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/execute", strings.NewReader(body))
	rec := httptest.NewRecorder()

	gateway.Handler().ServeHTTP(rec, req)

	assertBodyTooLarge(t, rec)
}

func TestGatewayRegisterRejectsOversizedBody(t *testing.T) {
	gateway := &master.Gateway{Registry: master.NewRegistry()}

	body := `{"id":"worker-a","ip_address":"localhost:7271","latitude":1,"longitude":2,"extra":"` + strings.Repeat("x", 4096) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/internal/register", strings.NewReader(body))
	rec := httptest.NewRecorder()

	gateway.Handler().ServeHTTP(rec, req)

	assertBodyTooLarge(t, rec)
}

func assertBodyTooLarge(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected status 413, got %d with body %s", rec.Code, rec.Body.String())
	}

	var response shared.ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if response.Code != "request_body_too_large" {
		t.Fatalf("expected request_body_too_large, got %q", response.Code)
	}
	if response.Error != "Request body is too large." {
		t.Fatalf("expected safe body size message, got %q", response.Error)
	}
}
