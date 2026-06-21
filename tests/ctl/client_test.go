package ctl_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"wasmcat/internal/ctl"
	"wasmcat/internal/shared"
)

func TestClientWorkers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/workers" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		shared.WriteJSON(w, http.StatusOK, []shared.WorkerNode{{ID: "worker-vn-01", IPAddress: "localhost:7271"}})
	}))
	defer server.Close()

	client, err := ctl.NewClientWithHTTP(ctl.Config{MasterURL: server.URL}, server.Client())
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	workers, err := client.Workers(context.Background())
	if err != nil {
		t.Fatalf("workers: %v", err)
	}
	if len(workers) != 1 || workers[0].ID != "worker-vn-01" {
		t.Fatalf("unexpected workers: %+v", workers)
	}
}

func TestClientExecuteSendsExecutionRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/execute" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var req shared.ExecutionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.ModuleName != "hello" || req.Payload != "payload" {
			t.Fatalf("unexpected execution request: %+v", req)
		}
		shared.WriteJSON(w, http.StatusOK, shared.ExecutionResponse{RequestID: "req_1", Result: "ok", ExecutedOnNodeID: "worker-vn-01"})
	}))
	defer server.Close()

	client, err := ctl.NewClientWithHTTP(ctl.Config{MasterURL: server.URL}, server.Client())
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	resp, err := client.Execute(context.Background(), shared.ExecutionRequest{
		ModuleName: "hello",
		ModuleURL:  "https://example.com/hello.wasm",
		Payload:    "payload",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if resp.Result != "ok" {
		t.Fatalf("expected ok result, got %q", resp.Result)
	}
}

func TestClientReturnsAPIErrorCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		shared.WriteError(w, http.StatusServiceUnavailable, "dispatch_failed", assertErr("no workers"))
	}))
	defer server.Close()

	client, err := ctl.NewClientWithHTTP(ctl.Config{MasterURL: server.URL}, server.Client())
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	_, err = client.Health(context.Background())
	if err == nil {
		t.Fatalf("expected error")
	}
	if got := err.Error(); got != "dispatch_failed: Execution could not be dispatched." {
		t.Fatalf("unexpected error: %q", got)
	}
}

type assertErr string

func (e assertErr) Error() string {
	return string(e)
}
