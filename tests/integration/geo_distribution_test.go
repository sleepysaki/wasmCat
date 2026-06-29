package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"wasmcat/internal/master"
	"wasmcat/internal/shared"
	"wasmcat/internal/worker"
)

// TestGeoAwareDispatchExecutesOnNearestWorker is the end-to-end demo check for
// the project's headline feature: a request carries the user's coordinates, the
// master selects the geographically nearest eligible worker, and that worker
// actually executes the WebAssembly module. It also proves draining reroutes
// traffic to the remaining worker regardless of distance.
func TestGeoAwareDispatchExecutesOnNearestWorker(t *testing.T) {
	moduleServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/wasm")
		w.Write(wasmEchoModule())
	}))
	defer moduleServer.Close()

	hanoi := newDemoWorker(t, "worker-hanoi")
	defer hanoi.Close()
	singapore := newDemoWorker(t, "worker-singapore")
	defer singapore.Close()

	registry := master.NewRegistry()
	registry.RegisterWorker(shared.WorkerNode{
		ID: "worker-hanoi", IPAddress: hanoi.URL,
		Latitude: 21.0278, Longitude: 105.8342, CPUFree: 80, RAMFreeMB: 4096,
	})
	registry.RegisterWorker(shared.WorkerNode{
		ID: "worker-singapore", IPAddress: singapore.URL,
		Latitude: 1.3521, Longitude: 103.8198, CPUFree: 80, RAMFreeMB: 4096,
	})

	gateway := &master.Gateway{
		Registry: registry,
		Dispatcher: &master.Dispatcher{
			Registry:  registry,
			Scheduler: &master.Scheduler{MinCPUFree: 10, MinRAMFreeMB: 512},
			Client:    http.DefaultClient,
		},
	}

	// A user in northern Vietnam must land on the Hanoi worker.
	if got := executeAt(t, gateway, moduleServer.URL, "from-hanoi", 21.0, 105.8); got.ExecutedOnNodeID != "worker-hanoi" {
		t.Fatalf("user near Hanoi: expected worker-hanoi, got %q", got.ExecutedOnNodeID)
	} else if got.Result != "from-hanoi" {
		t.Fatalf("expected echoed payload, got %q", got.Result)
	}

	// A user near Singapore must land on the Singapore worker. Same request
	// shape, different coordinates: this is "execute regardless of location".
	if got := executeAt(t, gateway, moduleServer.URL, "from-singapore", 1.3, 103.9); got.ExecutedOnNodeID != "worker-singapore" {
		t.Fatalf("user near Singapore: expected worker-singapore, got %q", got.ExecutedOnNodeID)
	}

	// Drain the Hanoi worker: a user near Hanoi must now reroute to Singapore,
	// the only remaining eligible worker.
	if err := registry.DrainWorker("worker-hanoi"); err != nil {
		t.Fatalf("drain worker-hanoi: %v", err)
	}
	if got := executeAt(t, gateway, moduleServer.URL, "after-drain", 21.0, 105.8); got.ExecutedOnNodeID != "worker-singapore" {
		t.Fatalf("after draining Hanoi: expected reroute to worker-singapore, got %q", got.ExecutedOnNodeID)
	}
}

func newDemoWorker(t *testing.T, nodeID string) *httptest.Server {
	t.Helper()

	engine := worker.NewWasmEngineWithLimits(context.Background(), worker.Limits{
		MaxModuleBytes:     1 << 20,
		MaxPayloadBytes:    1 << 20,
		MaxOutputBytes:     1 << 20,
		MaxConcurrentExecs: 2,
	})
	server := &worker.WorkerServer{Engine: engine, NodeID: nodeID}

	return httptest.NewServer(server.Handler())
}

func executeAt(t *testing.T, gateway *master.Gateway, moduleURL string, payload string, lat float64, lon float64) shared.ExecutionResponse {
	t.Helper()

	body, err := json.Marshal(shared.ExecutionRequest{
		ModuleName: "echo-demo",
		ModuleURL:  moduleURL,
		Payload:    payload,
		UserLat:    lat,
		UserLon:    lon,
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	rec := httptest.NewRecorder()
	gateway.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/execute", bytesReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("execute (%.1f,%.1f): status %d body %s", lat, lon, rec.Code, rec.Body.String())
	}

	var response shared.ExecutionResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	return response
}
