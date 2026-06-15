package master_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"wasmcat/internal/master"
	"wasmcat/internal/shared"
)

func TestGatewayJobCompletionRecordsSucceededJob(t *testing.T) {
	ctx := context.Background()
	store := newTestJobStore(t, filepath.Join(t.TempDir(), "jobs.db"))
	job := newJobRecordForTest(t, "job-complete-success", "hello")
	if err := store.Create(ctx, job); err != nil {
		t.Fatalf("create job: %v", err)
	}

	handler := (&master.Gateway{JobStore: store}).Handler()
	body := `{"request_id":"job-complete-success","worker_id":"worker-a","status":"succeeded","response":{"request_id":"job-complete-success","result":"ok","executed_on_node_id":"worker-a"}}`
	req := httptest.NewRequest(http.MethodPost, "/internal/jobs/complete", strings.NewReader(body))
	req.TLS = tlsStateWithWorkerCommonName("worker-a")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d with body %s", rec.Code, rec.Body.String())
	}
	persisted, err := store.Get(ctx, job.RequestID)
	if err != nil {
		t.Fatalf("get completed job: %v", err)
	}
	if persisted.Status != master.JobSucceeded {
		t.Fatalf("expected succeeded status, got %+v", persisted)
	}
	if persisted.Response == nil || persisted.Response.Result != "ok" {
		t.Fatalf("expected persisted response, got %+v", persisted.Response)
	}
}

func TestGatewayJobCompletionRecordsFailedJob(t *testing.T) {
	ctx := context.Background()
	store := newTestJobStore(t, filepath.Join(t.TempDir(), "jobs.db"))
	job := newJobRecordForTest(t, "job-complete-failed", "hello")
	if err := store.Create(ctx, job); err != nil {
		t.Fatalf("create job: %v", err)
	}

	handler := (&master.Gateway{JobStore: store}).Handler()
	body := `{"request_id":"job-complete-failed","worker_id":"worker-a","status":"failed","error":"module trapped"}`
	req := httptest.NewRequest(http.MethodPost, "/internal/jobs/complete", strings.NewReader(body))
	req.TLS = tlsStateWithWorkerCommonName("worker-a")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d with body %s", rec.Code, rec.Body.String())
	}
	persisted, err := store.Get(ctx, job.RequestID)
	if err != nil {
		t.Fatalf("get failed job: %v", err)
	}
	if persisted.Status != master.JobFailed || persisted.LastError != "module trapped" {
		t.Fatalf("expected failed status and error, got %+v", persisted)
	}
}

func TestGatewayJobCompletionRejectsMismatchedWorkerIdentity(t *testing.T) {
	store := newTestJobStore(t, filepath.Join(t.TempDir(), "jobs.db"))
	handler := (&master.Gateway{JobStore: store}).Handler()
	body := `{"request_id":"job-complete-auth","worker_id":"worker-b","status":"failed","error":"nope"}`
	req := httptest.NewRequest(http.MethodPost, "/internal/jobs/complete", strings.NewReader(body))
	req.TLS = tlsStateWithWorkerCommonName("worker-a")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d with body %s", rec.Code, rec.Body.String())
	}
}

func TestGatewayJobCompletionRejectsInvalidAndMissingStore(t *testing.T) {
	invalidStore := newTestJobStore(t, filepath.Join(t.TempDir(), "jobs.db"))
	invalidHandler := (&master.Gateway{JobStore: invalidStore}).Handler()
	invalid := httptest.NewRecorder()
	invalidHandler.ServeHTTP(invalid, httptest.NewRequest(http.MethodPost, "/internal/jobs/complete", strings.NewReader(`{"request_id":"bad","worker_id":"worker-a","status":"succeeded"}`)))
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid status 400, got %d", invalid.Code)
	}
	var invalidResp shared.ErrorResponse
	if err := json.NewDecoder(invalid.Body).Decode(&invalidResp); err != nil {
		t.Fatalf("decode invalid response: %v", err)
	}
	if invalidResp.Code != "invalid_job_completion" {
		t.Fatalf("expected invalid_job_completion, got %q", invalidResp.Code)
	}

	missingStore := httptest.NewRecorder()
	(&master.Gateway{}).Handler().ServeHTTP(missingStore, httptest.NewRequest(http.MethodPost, "/internal/jobs/complete", strings.NewReader(`{}`)))
	if missingStore.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected missing store status 503, got %d", missingStore.Code)
	}
}

func TestGatewayJobCompletionReturnsNotFound(t *testing.T) {
	store := newTestJobStore(t, filepath.Join(t.TempDir(), "jobs.db"))
	handler := (&master.Gateway{JobStore: store}).Handler()
	body := `{"request_id":"missing-completion-job","worker_id":"worker-a","status":"failed","error":"missing"}`
	req := httptest.NewRequest(http.MethodPost, "/internal/jobs/complete", strings.NewReader(body))
	req.TLS = tlsStateWithWorkerCommonName("worker-a")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d with body %s", rec.Code, rec.Body.String())
	}
}
