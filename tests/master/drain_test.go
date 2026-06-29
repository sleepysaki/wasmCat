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

func TestGatewayDrainMarksWorkerUnschedulable(t *testing.T) {
	registry := master.NewRegistry()
	registry.RegisterWorker(shared.WorkerNode{
		ID:        "worker-a",
		IPAddress: "localhost:7271",
		State:     shared.WorkerStateReady,
	})
	gateway := &master.Gateway{Registry: registry}

	req := httptest.NewRequest(http.MethodPost, "/internal/drain", strings.NewReader(`{"node_id":"worker-a"}`))
	req.TLS = tlsStateWithWorkerCommonName("worker-a")
	rec := httptest.NewRecorder()

	gateway.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d with body %s", rec.Code, rec.Body.String())
	}
	if len(registry.GetSchedulableWorkers()) != 0 {
		t.Fatalf("expected drained worker to be unschedulable")
	}
	workers := registry.GetActiveWorkers()
	if len(workers) != 1 || workers[0].State != shared.WorkerStateDraining {
		t.Fatalf("expected worker state draining, got %+v", workers)
	}
}

func TestGatewayDrainRejectsMismatchedCertificateIdentity(t *testing.T) {
	registry := master.NewRegistry()
	registry.RegisterWorker(shared.WorkerNode{
		ID:        "worker-b",
		IPAddress: "localhost:7271",
	})
	gateway := &master.Gateway{Registry: registry}

	req := httptest.NewRequest(http.MethodPost, "/internal/drain", strings.NewReader(`{"node_id":"worker-b"}`))
	req.TLS = tlsStateWithWorkerCommonName("worker-a")
	rec := httptest.NewRecorder()

	gateway.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d with body %s", rec.Code, rec.Body.String())
	}
}

func TestGatewayOperatorDrainViaAPIMarksWorkerDraining(t *testing.T) {
	registry := master.NewRegistry()
	registry.RegisterWorker(shared.WorkerNode{
		ID:        "worker-a",
		IPAddress: "localhost:7271",
		State:     shared.WorkerStateReady,
	})
	gateway := &master.Gateway{Registry: registry}

	// No execute-client allowlist: any trusted mTLS client may drain.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workers/worker-a/drain", nil)
	rec := httptest.NewRecorder()

	gateway.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d with body %s", rec.Code, rec.Body.String())
	}
	workers := registry.GetActiveWorkers()
	if len(workers) != 1 || workers[0].State != shared.WorkerStateDraining {
		t.Fatalf("expected worker state draining, got %+v", workers)
	}
}

func TestGatewayOperatorDrainAllowsListedOperatorIdentity(t *testing.T) {
	registry := master.NewRegistry()
	registry.RegisterWorker(shared.WorkerNode{ID: "worker-a", IPAddress: "localhost:7271"})
	gateway := &master.Gateway{Registry: registry, ExecuteClientIDs: []string{"ops-team"}}

	// An operator certificate (not the worker's own certificate) is allowed to
	// drain when its identity is in the execute-client allowlist. This is the
	// path the CLI and UI use, and it must not require worker-identity certs.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workers/worker-a/drain", nil)
	req.TLS = tlsStateWithClientIdentity("ops-team", nil)
	rec := httptest.NewRecorder()

	gateway.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d with body %s", rec.Code, rec.Body.String())
	}
	if len(registry.GetSchedulableWorkers()) != 0 {
		t.Fatalf("expected drained worker to be unschedulable")
	}
}

func TestGatewayOperatorDrainRejectsUnlistedClient(t *testing.T) {
	registry := master.NewRegistry()
	registry.RegisterWorker(shared.WorkerNode{ID: "worker-a", IPAddress: "localhost:7271"})
	gateway := &master.Gateway{Registry: registry, ExecuteClientIDs: []string{"ops-team"}}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/workers/worker-a/drain", nil)
	req.TLS = tlsStateWithClientIdentity("intruder", nil)
	rec := httptest.NewRecorder()

	gateway.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d with body %s", rec.Code, rec.Body.String())
	}
}

func TestGatewayOperatorDrainUnknownWorkerReturns404(t *testing.T) {
	registry := master.NewRegistry()
	gateway := &master.Gateway{Registry: registry}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/workers/missing/drain", nil)
	rec := httptest.NewRecorder()

	gateway.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d with body %s", rec.Code, rec.Body.String())
	}
}

func TestMasterMetricsReportsWorkersByState(t *testing.T) {
	registry := master.NewRegistry()
	registry.RegisterWorker(shared.WorkerNode{ID: "ready-worker", IPAddress: "localhost:7271"})
	registry.RegisterWorker(shared.WorkerNode{ID: "draining-worker", IPAddress: "localhost:7272", State: shared.WorkerStateDraining})

	gateway := &master.Gateway{
		Registry: registry,
		Dispatcher: &master.Dispatcher{
			Registry:  registry,
			Scheduler: &master.Scheduler{},
			Client:    http.DefaultClient,
		},
	}

	rec := httptest.NewRecorder()
	gateway.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/wasmcat/metrics", nil))

	var response shared.MetricsResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode metrics response: %v", err)
	}
	if response.Master == nil {
		t.Fatal("expected master metrics")
	}
	if response.Master.WorkersByState[shared.WorkerStateReady] != 1 {
		t.Fatalf("expected one ready worker, got %+v", response.Master.WorkersByState)
	}
	if response.Master.WorkersByState[shared.WorkerStateDraining] != 1 {
		t.Fatalf("expected one draining worker, got %+v", response.Master.WorkersByState)
	}
}
