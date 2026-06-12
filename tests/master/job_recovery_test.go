package master_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"
	"wasmcat/internal/master"
	"wasmcat/internal/shared"
)

func TestJobRecoveryDispatchesQueuedJobAndStoresResult(t *testing.T) {
	ctx := context.Background()
	store := newTestJobStore(t, filepath.Join(t.TempDir(), "jobs.db"))
	job := newJobRecordForTest(t, "job-recover-success", "hello")
	if err := store.Create(ctx, job); err != nil {
		t.Fatalf("create queued job: %v", err)
	}

	recovery := &master.JobRecovery{
		Store: store,
		Dispatch: func(ctx context.Context, req shared.ExecutionRequest) (shared.ExecutionResponse, error) {
			return shared.ExecutionResponse{
				RequestID: req.RequestID,
				Result:    "recovered-" + req.Payload,
			}, nil
		},
		LeaseTTL: time.Second,
		Now:      func() time.Time { return time.Unix(100, 0) },
	}

	if err := recovery.RecoverOnce(ctx); err != nil {
		t.Fatalf("recover once: %v", err)
	}

	recovered, err := store.Get(ctx, job.RequestID)
	if err != nil {
		t.Fatalf("get recovered job: %v", err)
	}
	if recovered.Status != master.JobSucceeded {
		t.Fatalf("expected succeeded job, got %+v", recovered)
	}
	if recovered.Attempt != 1 {
		t.Fatalf("expected one recovery attempt, got %+v", recovered)
	}
	if recovered.Response == nil || recovered.Response.Result != "recovered-hello" {
		t.Fatalf("expected recovered response, got %+v", recovered.Response)
	}
}

func TestJobRecoveryRequeuesQueuedJobWhenDispatchFailsBelowMaxAttempts(t *testing.T) {
	ctx := context.Background()
	store := newTestJobStore(t, filepath.Join(t.TempDir(), "jobs.db"))
	job := newJobRecordForTest(t, "job-recover-requeue", "hello")
	job.MaxAttempts = 3
	if err := store.Create(ctx, job); err != nil {
		t.Fatalf("create queued job: %v", err)
	}

	recovery := &master.JobRecovery{
		Store: store,
		Dispatch: func(ctx context.Context, req shared.ExecutionRequest) (shared.ExecutionResponse, error) {
			return shared.ExecutionResponse{}, fmt.Errorf("no workers yet")
		},
		LeaseTTL: time.Second,
		Now:      func() time.Time { return time.Unix(100, 0) },
	}

	if err := recovery.RecoverOnce(ctx); err != nil {
		t.Fatalf("recover once: %v", err)
	}

	requeued, err := store.Get(ctx, job.RequestID)
	if err != nil {
		t.Fatalf("get requeued job: %v", err)
	}
	if requeued.Status != master.JobQueued {
		t.Fatalf("expected job to be queued again, got %+v", requeued)
	}
	if requeued.Attempt != 1 {
		t.Fatalf("expected one failed recovery attempt, got %+v", requeued)
	}
	if requeued.LastError != "no workers yet" {
		t.Fatalf("expected last dispatch error to be stored, got %+v", requeued)
	}
}

func TestJobRecoveryFailsQueuedJobAtMaxAttempts(t *testing.T) {
	ctx := context.Background()
	store := newTestJobStore(t, filepath.Join(t.TempDir(), "jobs.db"))
	job := newJobRecordForTest(t, "job-recover-failed", "hello")
	job.MaxAttempts = 1
	if err := store.Create(ctx, job); err != nil {
		t.Fatalf("create queued job: %v", err)
	}

	recovery := &master.JobRecovery{
		Store: store,
		Dispatch: func(ctx context.Context, req shared.ExecutionRequest) (shared.ExecutionResponse, error) {
			return shared.ExecutionResponse{}, fmt.Errorf("still unavailable")
		},
		LeaseTTL: time.Second,
		Now:      func() time.Time { return time.Unix(100, 0) },
	}

	if err := recovery.RecoverOnce(ctx); err != nil {
		t.Fatalf("recover once: %v", err)
	}

	failed, err := store.Get(ctx, job.RequestID)
	if err != nil {
		t.Fatalf("get failed job: %v", err)
	}
	if failed.Status != master.JobFailed {
		t.Fatalf("expected failed job, got %+v", failed)
	}
	if failed.LastError != "still unavailable" {
		t.Fatalf("expected failure reason to be stored, got %+v", failed)
	}
}

func TestJobRecoveryMarksExpiredDispatchingLeaseAmbiguous(t *testing.T) {
	ctx := context.Background()
	store := newTestJobStore(t, filepath.Join(t.TempDir(), "jobs.db"))
	now := time.Unix(100, 0)
	job := newJobRecordForTest(t, "job-recover-ambiguous", "hello")
	if err := store.Create(ctx, job); err != nil {
		t.Fatalf("create queued job: %v", err)
	}
	if err := store.MarkDispatching(ctx, job.RequestID, "worker-a", now.Add(-time.Second)); err != nil {
		t.Fatalf("mark dispatching: %v", err)
	}

	recovery := &master.JobRecovery{
		Store: store,
		Dispatch: func(ctx context.Context, req shared.ExecutionRequest) (shared.ExecutionResponse, error) {
			t.Fatal("expired dispatching jobs must not be blindly redispatched")
			return shared.ExecutionResponse{}, nil
		},
		Now: func() time.Time { return now },
	}

	if err := recovery.RecoverOnce(ctx); err != nil {
		t.Fatalf("recover once: %v", err)
	}

	ambiguous, err := store.Get(ctx, job.RequestID)
	if err != nil {
		t.Fatalf("get ambiguous job: %v", err)
	}
	if ambiguous.Status != master.JobAmbiguous {
		t.Fatalf("expected ambiguous job, got %+v", ambiguous)
	}
	if ambiguous.LastError == "" {
		t.Fatalf("expected ambiguity reason, got %+v", ambiguous)
	}
}
