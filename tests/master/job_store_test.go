package master_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
	"wasmcat/internal/master"
	"wasmcat/internal/shared"
)

func TestSQLiteJobStorePersistsSucceededJobAcrossReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "jobs.db")

	store := newTestJobStore(t, path)
	req := shared.ExecutionRequest{
		RequestID:  "job-persist",
		ModuleName: "echo",
		ModuleURL:  "https://modules.internal/echo.wasm",
		Payload:    "hello",
	}
	fingerprint, err := master.ExecutionJobFingerprint(req)
	if err != nil {
		t.Fatalf("fingerprint job request: %v", err)
	}

	job := master.NewJobRecord(req, fingerprint, 3, time.Now())
	if err := store.Create(ctx, job); err != nil {
		t.Fatalf("create job: %v", err)
	}
	response := shared.ExecutionResponse{
		RequestID: req.RequestID,
		Result:    "persisted-result",
	}
	if err := store.MarkSucceeded(ctx, req.RequestID, response); err != nil {
		t.Fatalf("mark succeeded: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close first store: %v", err)
	}

	reopened := newTestJobStore(t, path)
	persisted, err := reopened.Get(ctx, req.RequestID)
	if err != nil {
		t.Fatalf("get persisted job: %v", err)
	}
	if persisted.Status != master.JobSucceeded {
		t.Fatalf("expected succeeded job, got %+v", persisted)
	}
	if persisted.Response == nil || persisted.Response.Result != "persisted-result" {
		t.Fatalf("expected persisted response, got %+v", persisted.Response)
	}
}

func TestSQLiteJobStoreRejectsDuplicateCreate(t *testing.T) {
	ctx := context.Background()
	store := newTestJobStore(t, filepath.Join(t.TempDir(), "jobs.db"))
	req := shared.ExecutionRequest{
		RequestID:  "job-duplicate",
		ModuleName: "echo",
		ModuleURL:  "https://modules.internal/echo.wasm",
		Payload:    "hello",
	}
	fingerprint, err := master.ExecutionJobFingerprint(req)
	if err != nil {
		t.Fatalf("fingerprint job request: %v", err)
	}
	job := master.NewJobRecord(req, fingerprint, 3, time.Now())

	if err := store.Create(ctx, job); err != nil {
		t.Fatalf("create first job: %v", err)
	}
	if err := store.Create(ctx, job); !errors.Is(err, master.ErrJobExists) {
		t.Fatalf("expected ErrJobExists, got %v", err)
	}
}

func TestSQLiteJobStoreListsQueuedAndExpiredJobsAsRecoverable(t *testing.T) {
	ctx := context.Background()
	store := newTestJobStore(t, filepath.Join(t.TempDir(), "jobs.db"))
	now := time.Now()

	queued := newJobRecordForTest(t, "job-queued", "queued")
	if err := store.Create(ctx, queued); err != nil {
		t.Fatalf("create queued job: %v", err)
	}

	expired := newJobRecordForTest(t, "job-expired", "expired")
	if err := store.Create(ctx, expired); err != nil {
		t.Fatalf("create expired job: %v", err)
	}
	if err := store.MarkDispatching(ctx, expired.RequestID, "worker-a", now.Add(-time.Second)); err != nil {
		t.Fatalf("mark expired dispatching: %v", err)
	}

	active := newJobRecordForTest(t, "job-active", "active")
	if err := store.Create(ctx, active); err != nil {
		t.Fatalf("create active job: %v", err)
	}
	if err := store.MarkDispatching(ctx, active.RequestID, "worker-b", now.Add(time.Minute)); err != nil {
		t.Fatalf("mark active dispatching: %v", err)
	}

	jobs, err := store.ListRecoverable(ctx, now, 10)
	if err != nil {
		t.Fatalf("list recoverable jobs: %v", err)
	}

	got := map[string]bool{}
	for _, job := range jobs {
		got[job.RequestID] = true
	}
	if !got["job-queued"] || !got["job-expired"] {
		t.Fatalf("expected queued and expired jobs to be recoverable, got %+v", got)
	}
	if got["job-active"] {
		t.Fatalf("active dispatch lease should not be recoverable, got %+v", got)
	}
}

func newTestJobStore(t *testing.T, path string) *master.SQLiteJobStore {
	t.Helper()

	store, err := master.NewSQLiteJobStore(context.Background(), path)
	if err != nil {
		t.Fatalf("open sqlite job store: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	return store
}

func newJobRecordForTest(t *testing.T, requestID string, payload string) master.JobRecord {
	t.Helper()

	req := shared.ExecutionRequest{
		RequestID:  requestID,
		ModuleName: "echo",
		ModuleURL:  "https://modules.internal/echo.wasm",
		Payload:    payload,
	}
	fingerprint, err := master.ExecutionJobFingerprint(req)
	if err != nil {
		t.Fatalf("fingerprint job request: %v", err)
	}

	return master.NewJobRecord(req, fingerprint, 3, time.Now())
}
