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

func TestGatewayCreateJobPersistsQueuedJob(t *testing.T) {
	ctx := context.Background()
	store := newTestJobStore(t, filepath.Join(t.TempDir(), "jobs.db"))
	handler := (&master.Gateway{JobStore: store}).Handler()
	body := `{"request_id":"job-create-queued","module_name":"echo","module_url":"http://module.test/echo.wasm","payload":"hello"}`

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/jobs", strings.NewReader(body)))

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected status 202, got %d with body %s", rec.Code, rec.Body.String())
	}

	var response shared.JobResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode job response: %v", err)
	}
	if response.RequestID != "job-create-queued" || response.Status != string(master.JobQueued) {
		t.Fatalf("unexpected create response: %+v", response)
	}
	if response.Attempt != 0 || response.MaxAttempts != master.DefaultJobMaxAttempts {
		t.Fatalf("unexpected attempt fields: %+v", response)
	}
	if response.CreatedAt == "" || response.UpdatedAt == "" {
		t.Fatalf("expected timestamps in create response: %+v", response)
	}

	job, err := store.Get(ctx, "job-create-queued")
	if err != nil {
		t.Fatalf("get stored job: %v", err)
	}
	if job.Status != master.JobQueued || job.Request.Payload != "hello" {
		t.Fatalf("unexpected stored job: %+v", job)
	}
}

func TestGatewayCreateJobReturnsExistingJobForDuplicate(t *testing.T) {
	store := newTestJobStore(t, filepath.Join(t.TempDir(), "jobs.db"))
	handler := (&master.Gateway{JobStore: store}).Handler()
	body := `{"request_id":"job-create-duplicate","module_name":"echo","module_url":"http://module.test/echo.wasm","payload":"hello"}`

	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodPost, "/api/v1/jobs", strings.NewReader(body)))
	if first.Code != http.StatusAccepted {
		t.Fatalf("expected first status 202, got %d with body %s", first.Code, first.Body.String())
	}

	second := httptest.NewRecorder()
	handler.ServeHTTP(second, httptest.NewRequest(http.MethodPost, "/api/v1/jobs", strings.NewReader(body)))
	if second.Code != http.StatusOK {
		t.Fatalf("expected duplicate status 200, got %d with body %s", second.Code, second.Body.String())
	}

	var response shared.JobResponse
	if err := json.NewDecoder(second.Body).Decode(&response); err != nil {
		t.Fatalf("decode duplicate response: %v", err)
	}
	if response.RequestID != "job-create-duplicate" || response.Status != string(master.JobQueued) {
		t.Fatalf("unexpected duplicate response: %+v", response)
	}
}

func TestGatewayCreateJobRejectsRequestIDConflict(t *testing.T) {
	store := newTestJobStore(t, filepath.Join(t.TempDir(), "jobs.db"))
	handler := (&master.Gateway{JobStore: store}).Handler()
	firstBody := `{"request_id":"job-create-conflict","module_name":"echo","module_url":"http://module.test/echo.wasm","payload":"hello"}`
	secondBody := `{"request_id":"job-create-conflict","module_name":"echo","module_url":"http://module.test/echo.wasm","payload":"different"}`

	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodPost, "/api/v1/jobs", strings.NewReader(firstBody)))
	if first.Code != http.StatusAccepted {
		t.Fatalf("expected first status 202, got %d with body %s", first.Code, first.Body.String())
	}

	second := httptest.NewRecorder()
	handler.ServeHTTP(second, httptest.NewRequest(http.MethodPost, "/api/v1/jobs", strings.NewReader(secondBody)))
	if second.Code != http.StatusConflict {
		t.Fatalf("expected conflict status 409, got %d with body %s", second.Code, second.Body.String())
	}

	var response shared.ErrorResponse
	if err := json.NewDecoder(second.Body).Decode(&response); err != nil {
		t.Fatalf("decode conflict response: %v", err)
	}
	if response.Code != "request_id_conflict" {
		t.Fatalf("expected request_id_conflict, got %q", response.Code)
	}
}

func TestGatewayCreateJobRequiresJobStore(t *testing.T) {
	handler := (&master.Gateway{}).Handler()

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/jobs", strings.NewReader(`{"module_name":"echo","module_url":"http://module.test/echo.wasm"}`)))

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

func TestGatewayCreateJobRejectsInvalidRequestAndMethod(t *testing.T) {
	store := newTestJobStore(t, filepath.Join(t.TempDir(), "jobs.db"))
	handler := (&master.Gateway{JobStore: store}).Handler()

	invalid := httptest.NewRecorder()
	handler.ServeHTTP(invalid, httptest.NewRequest(http.MethodPost, "/api/v1/jobs", strings.NewReader(`{"payload":"missing module"}`)))
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid request status 400, got %d with body %s", invalid.Code, invalid.Body.String())
	}

	wrongMethod := httptest.NewRecorder()
	handler.ServeHTTP(wrongMethod, httptest.NewRequest(http.MethodGet, "/api/v1/jobs", nil))
	if wrongMethod.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected method status 405, got %d", wrongMethod.Code)
	}
	if wrongMethod.Header().Get("Allow") != http.MethodPost {
		t.Fatalf("expected Allow POST, got %q", wrongMethod.Header().Get("Allow"))
	}
}
