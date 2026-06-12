package master

import (
	"context"
	"errors"
	"time"
	"wasmcat/internal/shared"
)

var (
	ErrJobNotFound = errors.New("job not found")
	ErrJobExists   = errors.New("job already exists")
)

type JobStore interface {
	Create(ctx context.Context, job JobRecord) error
	Get(ctx context.Context, requestID string) (JobRecord, error)
	MarkDispatching(ctx context.Context, requestID string, workerID string, leaseUntil time.Time) error
	MarkSucceeded(ctx context.Context, requestID string, response shared.ExecutionResponse) error
	MarkFailed(ctx context.Context, requestID string, reason string) error
	ListRecoverable(ctx context.Context, now time.Time, limit int) ([]JobRecord, error)
	Close() error
}

func ExecutionJobFingerprint(req shared.ExecutionRequest) (string, error) {
	return executionRequestFingerprint(req)
}
