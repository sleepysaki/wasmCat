package master

import (
	"time"
	"wasmcat/internal/shared"
)

type JobStatus string

const (
	// JobQueued means the master persisted the job but has not started dispatching it yet.
	JobQueued JobStatus = "queued"
	// JobDispatching means the master is actively trying to send the job to a worker.
	// If the master dies here, later recovery can inspect the expired lease.
	JobDispatching JobStatus = "dispatching"
	// JobRunning is reserved for the worker-completion protocol, where workers report final results.
	JobRunning JobStatus = "running"
	// JobSucceeded means a worker response has been durably stored and can be replayed to duplicate callers.
	JobSucceeded JobStatus = "succeeded"
	// JobFailed means the current synchronous dispatch attempt failed with a known error.
	// The same request_id and fingerprint may be retried until max attempts is reached.
	JobFailed JobStatus = "failed"
	// JobAmbiguous means the master cannot prove whether the worker executed the job.
	// Recovery must not blindly redispatch ambiguous jobs.
	JobAmbiguous JobStatus = "ambiguous"
)

const (
	DefaultJobMaxAttempts = 3
	DefaultJobLeaseTTL    = 30 * time.Second
)

type JobRecord struct {
	RequestID   string
	Fingerprint string
	Request     shared.ExecutionRequest
	Status      JobStatus
	WorkerID    string
	Attempt     int
	MaxAttempts int
	LeaseUntil  time.Time
	LastError   string
	Response    *shared.ExecutionResponse
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func NewJobRecord(req shared.ExecutionRequest, fingerprint string, maxAttempts int, now time.Time) JobRecord {
	if maxAttempts <= 0 {
		maxAttempts = DefaultJobMaxAttempts
	}

	return JobRecord{
		RequestID:   req.RequestID,
		Fingerprint: fingerprint,
		Request:     req,
		Status:      JobQueued,
		MaxAttempts: maxAttempts,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}
