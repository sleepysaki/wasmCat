package master_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"wasmcat/internal/master"
	"wasmcat/internal/shared"
)

func TestGatewayGetJobReturnsSucceededJob(t *testing.T) {
	ctx := context.Background()
	store := newTestJobStore(t, filepath.Join(t.TempDir(), "jobs.db"))
	job := newJobRecordForTest(t, "job-query-succeeded", "hello")
	if err := store.Create(ctx, job); err != nil {
		t.Fatalf("create job: %v", err)
	}
	if err := store.MarkDispatching(ctx, job.RequestID, "worker-a", time.Now().Add(time.Minute)); err != nil {
		t.Fatalf("mark dispatching: %v", err)
	}
	if err := store.MarkSucceeded(ctx, job.RequestID, shared.ExecutionResponse{
		RequestID:        job.RequestID,
		Result:           "ok",
		ExecutedOnNodeID: "worker-a",
	}); err != nil {
		t.Fatalf("mark succeeded: %v", err)
	}

	handler := (&master.Gateway{JobStore: store}).Handler()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/jobs/"+job.RequestID, nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d with body %s", rec.Code, rec.Body.String())
	}

	var response shared.JobResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode job response: %v", err)
	}
	if response.RequestID != job.RequestID || response.Status != string(master.JobSucceeded) {
		t.Fatalf("unexpected job response: %+v", response)
	}
	if response.Response == nil || response.Response.Result != "ok" {
		t.Fatalf("expected stored execution response, got %+v", response.Response)
	}
	if response.CreatedAt == "" || response.UpdatedAt == "" {
		t.Fatalf("expected timestamps in job response: %+v", response)
	}
}

func TestGatewayGetJobReturnsFailedAndAmbiguousState(t *testing.T) {
	ctx := context.Background()
	store := newTestJobStore(t, filepath.Join(t.TempDir(), "jobs.db"))

	failed := newJobRecordForTest(t, "job-query-failed", "failed")
	if err := store.Create(ctx, failed); err != nil {
		t.Fatalf("create failed job: %v", err)
	}
	if err := store.MarkDispatching(ctx, failed.RequestID, "worker-a", time.Now().Add(time.Minute)); err != nil {
		t.Fatalf("mark failed job dispatching: %v", err)
	}
	if err := store.MarkFailed(ctx, failed.RequestID, "worker unavailable"); err != nil {
		t.Fatalf("mark failed: %v", err)
	}

	ambiguous := newJobRecordForTest(t, "job-query-ambiguous", "ambiguous")
	if err := store.Create(ctx, ambiguous); err != nil {
		t.Fatalf("create ambiguous job: %v", err)
	}
	if err := store.MarkDispatching(ctx, ambiguous.RequestID, "worker-b", time.Now().Add(-time.Second)); err != nil {
		t.Fatalf("mark ambiguous job dispatching: %v", err)
	}
	if err := store.MarkAmbiguous(ctx, ambiguous.RequestID, "lease expired"); err != nil {
		t.Fatalf("mark ambiguous: %v", err)
	}

	handler := (&master.Gateway{JobStore: store}).Handler()
	tests := map[string]struct {
		requestID string
		status    string
		lastError string
	}{
		"failed": {
			requestID: failed.RequestID,
			status:    string(master.JobFailed),
			lastError: "worker unavailable",
		},
		"ambiguous": {
			requestID: ambiguous.RequestID,
			status:    string(master.JobAmbiguous),
			lastError: "lease expired",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/jobs/"+tt.requestID, nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("expected status 200, got %d with body %s", rec.Code, rec.Body.String())
			}

			var response shared.JobResponse
			if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
				t.Fatalf("decode job response: %v", err)
			}
			if response.Status != tt.status || response.LastError != tt.lastError {
				t.Fatalf("unexpected job response: %+v", response)
			}
			if response.Response != nil {
				t.Fatalf("expected no execution response for terminal error state, got %+v", response.Response)
			}
		})
	}
}

func TestGatewayGetJobReturnsNotFound(t *testing.T) {
	store := newTestJobStore(t, filepath.Join(t.TempDir(), "jobs.db"))
	handler := (&master.Gateway{JobStore: store}).Handler()

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/jobs/missing-job", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d with body %s", rec.Code, rec.Body.String())
	}

	var response shared.ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if response.Code != "job_not_found" {
		t.Fatalf("expected job_not_found, got %q", response.Code)
	}
}

func TestGatewayGetJobRequiresJobStore(t *testing.T) {
	handler := (&master.Gateway{}).Handler()

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/jobs/job-no-store", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503, got %d with body %s", rec.Code, rec.Body.String())
	}

	var response shared.ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if response.Code != "not_ready" {
		t.Fatalf("expected not_ready, got %q", response.Code)
	}
}

func TestGatewayGetJobRejectsBadPathAndMethod(t *testing.T) {
	store := newTestJobStore(t, filepath.Join(t.TempDir(), "jobs.db"))
	handler := (&master.Gateway{JobStore: store}).Handler()

	badPath := httptest.NewRecorder()
	handler.ServeHTTP(badPath, httptest.NewRequest(http.MethodGet, "/api/v1/jobs/bad/id", nil))
	if badPath.Code != http.StatusBadRequest {
		t.Fatalf("expected bad path status 400, got %d", badPath.Code)
	}

	wrongMethod := httptest.NewRecorder()
	handler.ServeHTTP(wrongMethod, httptest.NewRequest(http.MethodPost, "/api/v1/jobs/job-id", strings.NewReader("{}")))
	if wrongMethod.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected method status 405, got %d", wrongMethod.Code)
	}
	if wrongMethod.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("expected Allow GET, got %q", wrongMethod.Header().Get("Allow"))
	}
}
