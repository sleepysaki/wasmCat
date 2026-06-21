package master

import (
	"context"
	"fmt"
	"log/slog"
	"time"
	"wasmcat/internal/shared"
)

const (
	DefaultJobRecoveryInterval  = 5 * time.Second
	DefaultJobRecoveryBatchSize = 32
)

type JobDispatchFunc func(context.Context, shared.ExecutionRequest) (shared.ExecutionResponse, error)

type JobRecovery struct {
	Store     JobStore
	Dispatch  JobDispatchFunc
	Metrics   *shared.Metrics
	Interval  time.Duration
	BatchSize int
	LeaseTTL  time.Duration
	Now       func() time.Time
}

func (r *JobRecovery) Start(ctx context.Context) {
	if r == nil || r.Store == nil || r.Dispatch == nil {
		return
	}

	interval := r.interval()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	slog.Info("job recovery loop started", "interval", interval.String(), "batch_size", r.batchSize())
	r.recoverOnceAndLog(ctx)

	for {
		select {
		case <-ctx.Done():
			slog.Info("job recovery loop stopped")
			return
		case <-ticker.C:
			r.recoverOnceAndLog(ctx)
		}
	}
}

func (r *JobRecovery) RecoverOnce(ctx context.Context) error {
	if r == nil || r.Store == nil || r.Dispatch == nil {
		return nil
	}

	now := r.now()
	jobs, err := r.Store.ListRecoverable(ctx, now, r.batchSize())
	if err != nil {
		return err
	}

	for _, job := range jobs {
		if err := r.recoverJob(ctx, job, now); err != nil {
			slog.Warn("job recovery failed", "request_id", job.RequestID, "status", job.Status, "error", err)
		}
	}

	return nil
}

func (r *JobRecovery) recoverJob(ctx context.Context, job JobRecord, now time.Time) error {
	switch job.Status {
	case JobQueued:
		return r.recoverQueuedJob(ctx, job, now)
	case JobDispatching, JobRunning:
		if job.LeaseUntil.IsZero() || job.LeaseUntil.After(now) {
			return nil
		}
		return r.Store.MarkAmbiguous(ctx, job.RequestID, fmt.Sprintf("job lease expired at %s", job.LeaseUntil.Format(time.RFC3339Nano)))
	default:
		return nil
	}
}

func (r *JobRecovery) recoverQueuedJob(ctx context.Context, job JobRecord, now time.Time) error {
	leaseUntil := now.Add(r.leaseTTL())
	if err := r.Store.MarkDispatching(ctx, job.RequestID, "", leaseUntil); err != nil {
		return err
	}

	response, err := r.Dispatch(ctx, job.Request)
	if err == nil {
		r.metrics().IncDispatchSuccess()
		return r.Store.MarkSucceeded(context.Background(), job.RequestID, response)
	}

	reason := err.Error()
	nextAttempt := job.Attempt + 1
	if job.MaxAttempts > 0 && nextAttempt >= job.MaxAttempts {
		r.metrics().IncDispatchFailure()
		return r.Store.MarkFailed(context.Background(), job.RequestID, reason)
	}

	// Keep retryable recovery failures queued. This avoids burning a recovered job
	// simply because no workers have registered yet during master startup.
	return r.Store.MarkQueued(context.Background(), job.RequestID, reason)
}

func (r *JobRecovery) recoverOnceAndLog(ctx context.Context) {
	if err := r.RecoverOnce(ctx); err != nil {
		slog.Warn("job recovery scan failed", "error", err)
	}
}

func (r *JobRecovery) metrics() *shared.Metrics {
	if r.Metrics == nil {
		r.Metrics = shared.NewMetrics()
	}

	return r.Metrics
}

func (r *JobRecovery) interval() time.Duration {
	if r.Interval > 0 {
		return r.Interval
	}

	return DefaultJobRecoveryInterval
}

func (r *JobRecovery) batchSize() int {
	if r.BatchSize > 0 {
		return r.BatchSize
	}

	return DefaultJobRecoveryBatchSize
}

func (r *JobRecovery) leaseTTL() time.Duration {
	if r.LeaseTTL > 0 {
		return r.LeaseTTL
	}

	return DefaultJobLeaseTTL
}

func (r *JobRecovery) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}

	return time.Now()
}
