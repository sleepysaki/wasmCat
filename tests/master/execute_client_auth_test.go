package master_test

import (
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"wasmcat/internal/master"
	"wasmcat/internal/shared"
)

func TestGatewayExecuteAllowsConfiguredClientCommonName(t *testing.T) {
	gateway := newExecuteAuthGateway(t, []string{"wasmcat-client"})
	req := newExecuteRequest()
	req.TLS = tlsStateWithClientIdentity("wasmcat-client", nil)
	rec := httptest.NewRecorder()

	gateway.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d with body %s", rec.Code, rec.Body.String())
	}
}

func TestGatewayExecuteAllowsConfiguredClientDNSName(t *testing.T) {
	gateway := newExecuteAuthGateway(t, []string{"deployer.internal"})
	req := newExecuteRequest()
	req.TLS = tlsStateWithClientIdentity("custom-cn", []string{"deployer.internal"})
	rec := httptest.NewRecorder()

	gateway.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d with body %s", rec.Code, rec.Body.String())
	}
}

func TestGatewayExecuteRejectsUnlistedClientIdentity(t *testing.T) {
	gateway := newExecuteAuthGateway(t, []string{"wasmcat-client"})
	req := newExecuteRequest()
	req.TLS = tlsStateWithClientIdentity("wasmcat-worker-worker-a", nil)
	rec := httptest.NewRecorder()

	gateway.Handler().ServeHTTP(rec, req)

	assertUnauthorizedExecutionClient(t, rec)
}

func TestGatewayExecuteRejectsMissingClientCertificateWhenAllowlistConfigured(t *testing.T) {
	gateway := newExecuteAuthGateway(t, []string{"wasmcat-client"})
	req := newExecuteRequest()
	rec := httptest.NewRecorder()

	gateway.Handler().ServeHTTP(rec, req)

	assertUnauthorizedExecutionClient(t, rec)
}

func newExecuteAuthGateway(t *testing.T, allowedClientIDs []string) *master.Gateway {
	t.Helper()

	worker := newExecuteAuthWorker()
	t.Cleanup(worker.Close)

	registry := master.NewRegistry()
	registry.RegisterWorker(shared.WorkerNode{
		ID:        "worker-test",
		IPAddress: worker.URL,
	})

	return &master.Gateway{
		Registry: registry,
		Dispatcher: &master.Dispatcher{
			Registry:  registry,
			Scheduler: &master.Scheduler{},
			Client:    http.DefaultClient,
		},
		ExecuteClientIDs: allowedClientIDs,
	}
}

func newExecuteAuthWorker() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		shared.WriteJSON(w, http.StatusOK, shared.ExecutionResponse{Result: "ok"})
	}))
}

func newExecuteRequest() *http.Request {
	body := `{"module_name":"echo","module_url":"http://module.test/echo.wasm","payload":"hello"}`
	return httptest.NewRequest(http.MethodPost, "/api/v1/execute", strings.NewReader(body))
}

func tlsStateWithClientIdentity(commonName string, dnsNames []string) *tls.ConnectionState {
	return &tls.ConnectionState{PeerCertificates: []*x509.Certificate{
		{
			Subject:  pkix.Name{CommonName: commonName},
			DNSNames: dnsNames,
		},
	}}
}

func assertUnauthorizedExecutionClient(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d with body %s", rec.Code, rec.Body.String())
	}

	var response shared.ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if response.Code != "execute_client_unauthorized" {
		t.Fatalf("expected execute_client_unauthorized, got %q", response.Code)
	}
	if response.Error != "Execution client is not authorized." {
		t.Fatalf("expected safe execution client message, got %q", response.Error)
	}
	if strings.Contains(response.Error, "wasmcat-worker-worker-a") {
		t.Fatalf("public error leaked certificate identity: %q", response.Error)
	}
}
