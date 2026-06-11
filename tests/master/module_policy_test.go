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

func TestGatewayRejectsDisallowedModuleHost(t *testing.T) {
	gateway := &master.Gateway{
		Registry:     master.NewRegistry(),
		Dispatcher:   &master.Dispatcher{Registry: master.NewRegistry(), Scheduler: &master.Scheduler{}, Client: http.DefaultClient},
		ModulePolicy: master.ModulePolicy{AllowedHosts: []string{"modules.internal"}},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/execute", strings.NewReader(`{"module_name":"echo","module_url":"https://evil.example/echo.wasm"}`))
	rec := httptest.NewRecorder()

	gateway.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d with body %s", rec.Code, rec.Body.String())
	}

	var response shared.ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if response.Code != "module_policy_violation" {
		t.Fatalf("expected module_policy_violation code, got %q", response.Code)
	}
}

func TestGatewayRejectsMissingModuleDigestWhenRequired(t *testing.T) {
	gateway := &master.Gateway{
		Registry:     master.NewRegistry(),
		Dispatcher:   &master.Dispatcher{Registry: master.NewRegistry(), Scheduler: &master.Scheduler{}, Client: http.DefaultClient},
		ModulePolicy: master.ModulePolicy{RequireDigest: true},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/execute", strings.NewReader(`{"module_name":"echo","module_url":"https://modules.internal/echo.wasm"}`))
	rec := httptest.NewRecorder()

	gateway.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d with body %s", rec.Code, rec.Body.String())
	}

	var response shared.ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if response.Code != "module_policy_violation" {
		t.Fatalf("expected module_policy_violation code, got %q", response.Code)
	}
}

func TestGatewayAllowsDigestPinnedModuleWhenPolicyRequiresDigest(t *testing.T) {
	workerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		shared.WriteJSON(w, http.StatusOK, shared.ExecutionResponse{Result: "ok"})
	}))
	defer workerServer.Close()

	registry := master.NewRegistry()
	registry.RegisterWorker(shared.WorkerNode{ID: "worker-test", IPAddress: workerServer.URL})
	gateway := &master.Gateway{
		Registry: registry,
		Dispatcher: &master.Dispatcher{
			Registry:  registry,
			Scheduler: &master.Scheduler{},
			Client:    http.DefaultClient,
		},
		ModulePolicy: master.ModulePolicy{
			AllowedHosts:  []string{"modules.internal"},
			RequireDigest: true,
		},
	}

	body := `{"module_name":"echo","module_url":"https://modules.internal/echo.wasm","module_digest":"sha256:abc","payload":"hello"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/execute", strings.NewReader(body))
	rec := httptest.NewRecorder()

	gateway.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d with body %s", rec.Code, rec.Body.String())
	}
}
