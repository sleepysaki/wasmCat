package master_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"wasmcat/internal/master"
	"wasmcat/internal/shared"
)

func TestListWorkersReturnsRegistryWorkers(t *testing.T) {
	registry := master.NewRegistry()
	registry.RegisterWorker(shared.WorkerNode{
		ID:        "worker-vn-01",
		IPAddress: "localhost:7271",
		Latitude:  21.0278,
		Longitude: 105.8342,
		CPUFree:   75,
		RAMFreeMB: 2048,
	})
	gateway := &master.Gateway{Registry: registry}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/workers", nil)
	rec := httptest.NewRecorder()

	gateway.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var workers []shared.WorkerNode
	if err := json.NewDecoder(rec.Body).Decode(&workers); err != nil {
		t.Fatalf("decode workers response: %v", err)
	}
	if len(workers) != 1 {
		t.Fatalf("expected one worker, got %d", len(workers))
	}
	if workers[0].ID != "worker-vn-01" || workers[0].IPAddress != "localhost:7271" {
		t.Fatalf("unexpected worker response: %+v", workers[0])
	}
}

func TestListWorkersRequiresRegistry(t *testing.T) {
	gateway := &master.Gateway{}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/workers", nil)
	rec := httptest.NewRecorder()

	gateway.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503, got %d", rec.Code)
	}
}
