package worker_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"wasmcat/internal/shared"
	"wasmcat/internal/worker"
)

func TestWorkerServerReportsSuccessfulJobCompletion(t *testing.T) {
	completionCh := make(chan shared.JobCompletionRequest, 1)
	master := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/jobs/complete" {
			t.Fatalf("unexpected callback path %q", r.URL.Path)
		}
		var completion shared.JobCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&completion); err != nil {
			t.Fatalf("decode completion callback: %v", err)
		}
		completionCh <- completion
		shared.WriteJSON(w, http.StatusOK, shared.APIResponse{Status: "success"})
	}))
	defer master.Close()

	moduleServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(wasmEchoModule())
	}))
	defer moduleServer.Close()

	server := &worker.WorkerServer{
		Engine:            worker.NewWasmEngine(context.Background()),
		NodeID:            "worker-callback",
		MasterURL:         master.URL,
		CompletionClient:  master.Client(),
		CompletionTimeout: time.Second,
	}
	body := `{"request_id":"worker-callback-success","module_name":"echo","module_url":"` + moduleServer.URL + `","payload":"hello"}`
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/invoke", strings.NewReader(body)))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected invoke status 200, got %d with body %s", rec.Code, rec.Body.String())
	}

	select {
	case completion := <-completionCh:
		if completion.RequestID != "worker-callback-success" || completion.WorkerID != "worker-callback" {
			t.Fatalf("unexpected completion identity: %+v", completion)
		}
		if completion.Status != shared.JobCompletionSucceeded {
			t.Fatalf("expected succeeded completion, got %+v", completion)
		}
		if completion.Response == nil || completion.Response.Result != "hello" {
			t.Fatalf("expected successful response, got %+v", completion.Response)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for completion callback")
	}
}

func TestWorkerServerReportsFailedJobCompletion(t *testing.T) {
	completionCh := make(chan shared.JobCompletionRequest, 1)
	master := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var completion shared.JobCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&completion); err != nil {
			t.Fatalf("decode completion callback: %v", err)
		}
		completionCh <- completion
		shared.WriteJSON(w, http.StatusOK, shared.APIResponse{Status: "success"})
	}))
	defer master.Close()

	moduleServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(wasmEchoModule())
	}))
	defer moduleServer.Close()

	server := &worker.WorkerServer{
		Engine:            worker.NewWasmEngine(context.Background()),
		NodeID:            "worker-callback",
		MasterURL:         master.URL,
		CompletionClient:  master.Client(),
		CompletionTimeout: time.Second,
	}
	body := `{"request_id":"worker-callback-failed","module_name":"echo","module_url":"` + moduleServer.URL + `","module_digest":"sha256:0000000000000000000000000000000000000000000000000000000000000000","payload":"hello"}`
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/invoke", strings.NewReader(body)))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected invoke status 400, got %d with body %s", rec.Code, rec.Body.String())
	}

	select {
	case completion := <-completionCh:
		if completion.RequestID != "worker-callback-failed" || completion.WorkerID != "worker-callback" {
			t.Fatalf("unexpected completion identity: %+v", completion)
		}
		if completion.Status != shared.JobCompletionFailed {
			t.Fatalf("expected failed completion, got %+v", completion)
		}
		if completion.Error == "" {
			t.Fatalf("expected completion error, got %+v", completion)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for completion callback")
	}
}
