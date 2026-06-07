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
	"time"
	"wasmcat/internal/master"
	"wasmcat/internal/shared"
)

func TestGatewayRegisterAcceptsMatchingWorkerCertificateIdentity(t *testing.T) {
	gateway := &master.Gateway{Registry: master.NewRegistry()}
	requestBody := `{"id":"worker-a","ip_address":"localhost:7271","latitude":1,"longitude":2}`
	req := httptest.NewRequest(http.MethodPost, "/internal/register", strings.NewReader(requestBody))
	req.TLS = tlsStateWithWorkerCommonName("worker-a")
	rec := httptest.NewRecorder()

	gateway.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d with body %s", rec.Code, rec.Body.String())
	}
	workers := gateway.Registry.GetActiveWorkers()
	if len(workers) != 1 || workers[0].ID != "worker-a" {
		t.Fatalf("expected worker-a to be registered, got %+v", workers)
	}
}

func TestGatewayRegisterRejectsMismatchedWorkerCertificateIdentity(t *testing.T) {
	gateway := &master.Gateway{Registry: master.NewRegistry()}
	requestBody := `{"id":"worker-b","ip_address":"localhost:7271","latitude":1,"longitude":2}`
	req := httptest.NewRequest(http.MethodPost, "/internal/register", strings.NewReader(requestBody))
	req.TLS = tlsStateWithWorkerCommonName("worker-a")
	rec := httptest.NewRecorder()

	gateway.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d with body %s", rec.Code, rec.Body.String())
	}

	var response shared.ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if response.Code != "worker_identity_mismatch" {
		t.Fatalf("expected worker_identity_mismatch, got %q", response.Code)
	}
	if len(gateway.Registry.GetActiveWorkers()) != 0 {
		t.Fatal("mismatched worker should not be registered")
	}
}

func TestGatewayHeartbeatRejectsMismatchedWorkerCertificateIdentity(t *testing.T) {
	registry := master.NewRegistry()
	registry.RegisterWorker(shared.WorkerNode{
		ID:        "worker-b",
		IPAddress: "localhost:7271",
		LastSeen:  time.Now(),
	})
	gateway := &master.Gateway{Registry: registry}

	req := httptest.NewRequest(http.MethodPost, "/internal/heartbeat", strings.NewReader(`{"node_id":"worker-b","cpu_free":90,"ram_free_mb":1024}`))
	req.TLS = tlsStateWithWorkerCommonName("worker-a")
	rec := httptest.NewRecorder()

	gateway.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d with body %s", rec.Code, rec.Body.String())
	}
}

func TestGatewayHeartbeatAcceptsMatchingWorkerDNSName(t *testing.T) {
	registry := master.NewRegistry()
	registry.RegisterWorker(shared.WorkerNode{
		ID:        "worker-a",
		IPAddress: "localhost:7271",
		LastSeen:  time.Now(),
	})
	gateway := &master.Gateway{Registry: registry}

	req := httptest.NewRequest(http.MethodPost, "/internal/heartbeat", strings.NewReader(`{"node_id":"worker-a","cpu_free":90,"ram_free_mb":1024}`))
	req.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{
		{
			Subject:  pkix.Name{CommonName: "custom-common-name"},
			DNSNames: []string{"worker-a"},
		},
	}}
	rec := httptest.NewRecorder()

	gateway.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d with body %s", rec.Code, rec.Body.String())
	}
}

func tlsStateWithWorkerCommonName(workerID string) *tls.ConnectionState {
	return &tls.ConnectionState{PeerCertificates: []*x509.Certificate{
		{Subject: pkix.Name{CommonName: "wasmcat-worker-" + workerID}},
	}}
}
