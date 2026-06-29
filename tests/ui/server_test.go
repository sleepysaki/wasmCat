package ui_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"wasmcat/internal/ctl"
	"wasmcat/internal/shared"
	"wasmcat/internal/ui"
)

func TestDashboardServesIndex(t *testing.T) {
	server := &ui.Server{}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "wasmCat Console") {
		t.Fatalf("expected dashboard HTML, got %q", rec.Body.String())
	}
}

func TestDashboardConfigRoundTrip(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	server := &ui.Server{ConfigPath: configPath}
	cfg := ctl.Config{
		MasterURL:      "https://localhost:7270",
		CACert:         "./local/certs/ca.crt",
		ClientCert:     "./local/certs/client.crt",
		ClientKey:      "./local/certs/client.key",
		DefaultUserLat: 21.0278,
		DefaultUserLon: 105.8342,
		Output:         "table",
	}
	body, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}

	putReq := httptest.NewRequest(http.MethodPut, "/ui/api/config", bytes.NewReader(body))
	putRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(putRec, putReq)

	if putRec.Code != http.StatusOK {
		t.Fatalf("expected put status 200, got %d body %s", putRec.Code, putRec.Body.String())
	}

	getReq := httptest.NewRequest(http.MethodGet, "/ui/api/config", nil)
	getRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(getRec, getReq)

	if getRec.Code != http.StatusOK {
		t.Fatalf("expected get status 200, got %d", getRec.Code)
	}
	var response struct {
		Exists bool       `json:"exists"`
		Config ctl.Config `json:"config"`
	}
	if err := json.NewDecoder(getRec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !response.Exists || response.Config.MasterURL != "https://localhost:7270" {
		t.Fatalf("unexpected config response: %+v", response)
	}
}

func TestDashboardForwardsClusterOperations(t *testing.T) {
	master := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/wasmcat/health":
			shared.WriteJSON(w, http.StatusOK, shared.HealthResponse{Status: "ok", Role: "master"})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/workers":
			shared.WriteJSON(w, http.StatusOK, []shared.WorkerNode{{ID: "worker-vn-01", IPAddress: "localhost:7271"}})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/execute":
			var req shared.ExecutionRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode execute request: %v", err)
			}
			if req.ModuleName != "hello" || req.Payload != "payload" {
				t.Fatalf("unexpected execute request: %+v", req)
			}
			shared.WriteJSON(w, http.StatusOK, shared.ExecutionResponse{RequestID: "req_1", Result: "ok", ExecutedOnNodeID: "worker-vn-01"})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/jobs":
			shared.WriteJSON(w, http.StatusAccepted, shared.JobResponse{RequestID: "req_job_1", Status: "queued"})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/jobs/req_job_1":
			shared.WriteJSON(w, http.StatusOK, shared.JobResponse{RequestID: "req_job_1", Status: "succeeded"})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/workers/worker-vn-01/drain":
			shared.WriteJSON(w, http.StatusOK, shared.APIResponse{Status: "success", Message: "Worker marked as draining"})
		default:
			t.Fatalf("unexpected master request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer master.Close()

	client, err := ctl.NewClientWithHTTP(ctl.Config{MasterURL: master.URL}, master.Client())
	if err != nil {
		t.Fatalf("new ctl client: %v", err)
	}
	server := &ui.Server{ClientFactory: func() (*ctl.Client, error) {
		return client, nil
	}}

	assertJSONStatus(t, server, http.MethodGet, "/ui/api/health", nil, http.StatusOK)
	assertJSONStatus(t, server, http.MethodGet, "/ui/api/workers", nil, http.StatusOK)
	assertJSONStatus(t, server, http.MethodPost, "/ui/api/execute", shared.ExecutionRequest{
		ModuleName: "hello",
		ModuleURL:  "https://example.com/hello.wasm",
		Payload:    "payload",
	}, http.StatusOK)
	assertJSONStatus(t, server, http.MethodPost, "/ui/api/jobs", shared.ExecutionRequest{
		ModuleName: "hello",
		ModuleURL:  "https://example.com/hello.wasm",
		Payload:    "payload",
	}, http.StatusOK)
	assertJSONStatus(t, server, http.MethodGet, "/ui/api/jobs/req_job_1", nil, http.StatusOK)
	assertJSONStatus(t, server, http.MethodPost, "/ui/api/workers/worker-vn-01/drain", nil, http.StatusOK)
}

func assertJSONStatus(t *testing.T, server *ui.Server, method string, path string, body any, status int) {
	t.Helper()
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request: %v", err)
		}
		reader = bytes.NewReader(data)
	}

	req := httptest.NewRequest(method, path, reader)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != status {
		t.Fatalf("%s %s expected status %d, got %d body %s", method, path, status, rec.Code, rec.Body.String())
	}
}
