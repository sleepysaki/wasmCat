package master

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"wasmcat/internal/shared"

	_ "modernc.org/sqlite"
)

type SQLiteJobStore struct {
	db  *sql.DB
	now func() time.Time
}

func NewSQLiteJobStore(ctx context.Context, path string) (*SQLiteJobStore, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("job store path is required")
	}
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create job store directory: %w", err)
		}
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open job store: %w", err)
	}

	store := &SQLiteJobStore{db: db, now: time.Now}
	if err := store.init(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}

	return store, nil
}

func (s *SQLiteJobStore) init(ctx context.Context) error {
	statements := []string{
		`PRAGMA journal_mode=WAL;`,
		`PRAGMA busy_timeout=5000;`,
		`CREATE TABLE IF NOT EXISTS jobs (
			request_id TEXT PRIMARY KEY,
			fingerprint TEXT NOT NULL,
			request_json TEXT NOT NULL,
			status TEXT NOT NULL,
			worker_id TEXT NOT NULL DEFAULT '',
			attempt INTEGER NOT NULL DEFAULT 0,
			max_attempts INTEGER NOT NULL DEFAULT 3,
			lease_until INTEGER NOT NULL DEFAULT 0,
			last_error TEXT NOT NULL DEFAULT '',
			response_json TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_jobs_recoverable ON jobs(status, lease_until, updated_at);`,
	}

	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("initialize job store: %w", err)
		}
	}

	return nil
}

func (s *SQLiteJobStore) Create(ctx context.Context, job JobRecord) error {
	if job.CreatedAt.IsZero() {
		job.CreatedAt = s.now()
	}
	if job.UpdatedAt.IsZero() {
		job.UpdatedAt = job.CreatedAt
	}
	if job.Status == "" {
		job.Status = JobQueued
	}
	if job.MaxAttempts <= 0 {
		job.MaxAttempts = DefaultJobMaxAttempts
	}

	requestJSON, err := json.Marshal(job.Request)
	if err != nil {
		return fmt.Errorf("marshal job request: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO jobs (
			request_id, fingerprint, request_json, status, worker_id, attempt,
			max_attempts, lease_until, last_error, response_json, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		job.RequestID,
		job.Fingerprint,
		string(requestJSON),
		string(job.Status),
		job.WorkerID,
		job.Attempt,
		job.MaxAttempts,
		unixMilli(job.LeaseUntil),
		job.LastError,
		"",
		unixMilli(job.CreatedAt),
		unixMilli(job.UpdatedAt),
	)
	if err != nil {
		if isSQLiteConstraintError(err) {
			return ErrJobExists
		}
		return fmt.Errorf("create job: %w", err)
	}

	return nil
}

func (s *SQLiteJobStore) Get(ctx context.Context, requestID string) (JobRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT request_id, fingerprint, request_json, status, worker_id, attempt,
			max_attempts, lease_until, last_error, response_json, created_at, updated_at
		FROM jobs
		WHERE request_id = ?`,
		requestID,
	)

	job, err := scanJob(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return JobRecord{}, ErrJobNotFound
		}
		return JobRecord{}, err
	}

	return job, nil
}

func (s *SQLiteJobStore) MarkQueued(ctx context.Context, requestID string, reason string) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE jobs
		SET status = ?, worker_id = '', lease_until = 0, last_error = ?, updated_at = ?
		WHERE request_id = ?`,
		string(JobQueued),
		reason,
		unixMilli(s.now()),
		requestID,
	)
	return requireUpdated(result, err, "mark job queued")
}

func (s *SQLiteJobStore) MarkDispatching(ctx context.Context, requestID string, workerID string, leaseUntil time.Time) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE jobs
		SET status = ?, worker_id = ?, attempt = attempt + 1, lease_until = ?, last_error = '', updated_at = ?
		WHERE request_id = ?`,
		string(JobDispatching),
		workerID,
		unixMilli(leaseUntil),
		unixMilli(s.now()),
		requestID,
	)
	return requireUpdated(result, err, "mark job dispatching")
}

func (s *SQLiteJobStore) MarkSucceeded(ctx context.Context, requestID string, response shared.ExecutionResponse) error {
	responseJSON, err := json.Marshal(response)
	if err != nil {
		return fmt.Errorf("marshal job response: %w", err)
	}

	result, err := s.db.ExecContext(ctx, `
		UPDATE jobs
		SET status = ?, lease_until = 0, last_error = '', response_json = ?, updated_at = ?
		WHERE request_id = ?`,
		string(JobSucceeded),
		string(responseJSON),
		unixMilli(s.now()),
		requestID,
	)
	return requireUpdated(result, err, "mark job succeeded")
}

func (s *SQLiteJobStore) MarkFailed(ctx context.Context, requestID string, reason string) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE jobs
		SET status = ?, lease_until = 0, last_error = ?, updated_at = ?
		WHERE request_id = ?`,
		string(JobFailed),
		reason,
		unixMilli(s.now()),
		requestID,
	)
	return requireUpdated(result, err, "mark job failed")
}

func (s *SQLiteJobStore) MarkAmbiguous(ctx context.Context, requestID string, reason string) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE jobs
		SET status = ?, lease_until = 0, last_error = ?, updated_at = ?
		WHERE request_id = ?`,
		string(JobAmbiguous),
		reason,
		unixMilli(s.now()),
		requestID,
	)
	return requireUpdated(result, err, "mark job ambiguous")
}

func (s *SQLiteJobStore) ListRecoverable(ctx context.Context, now time.Time, limit int) ([]JobRecord, error) {
	if limit <= 0 {
		limit = 32
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT request_id, fingerprint, request_json, status, worker_id, attempt,
			max_attempts, lease_until, last_error, response_json, created_at, updated_at
		FROM jobs
		WHERE status = ?
			OR (status IN (?, ?) AND lease_until > 0 AND lease_until <= ?)
		ORDER BY updated_at ASC
		LIMIT ?`,
		string(JobQueued),
		string(JobDispatching),
		string(JobRunning),
		unixMilli(now),
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list recoverable jobs: %w", err)
	}
	defer rows.Close()

	var jobs []JobRecord
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recoverable jobs: %w", err)
	}

	return jobs, nil
}

func (s *SQLiteJobStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}

	return s.db.Close()
}

type jobScanner interface {
	Scan(dest ...any) error
}

func scanJob(scanner jobScanner) (JobRecord, error) {
	var job JobRecord
	var requestJSON string
	var responseJSON string
	var status string
	var leaseUntil int64
	var createdAt int64
	var updatedAt int64

	if err := scanner.Scan(
		&job.RequestID,
		&job.Fingerprint,
		&requestJSON,
		&status,
		&job.WorkerID,
		&job.Attempt,
		&job.MaxAttempts,
		&leaseUntil,
		&job.LastError,
		&responseJSON,
		&createdAt,
		&updatedAt,
	); err != nil {
		return JobRecord{}, err
	}

	if err := json.Unmarshal([]byte(requestJSON), &job.Request); err != nil {
		return JobRecord{}, fmt.Errorf("unmarshal job request: %w", err)
	}
	if responseJSON != "" {
		var response shared.ExecutionResponse
		if err := json.Unmarshal([]byte(responseJSON), &response); err != nil {
			return JobRecord{}, fmt.Errorf("unmarshal job response: %w", err)
		}
		job.Response = &response
	}

	job.Status = JobStatus(status)
	job.LeaseUntil = timeFromUnixMilli(leaseUntil)
	job.CreatedAt = timeFromUnixMilli(createdAt)
	job.UpdatedAt = timeFromUnixMilli(updatedAt)

	return job, nil
}

func requireUpdated(result sql.Result, err error, operation string) error {
	if err != nil {
		return fmt.Errorf("%s: %w", operation, err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("%s rows affected: %w", operation, err)
	}
	if rows == 0 {
		return ErrJobNotFound
	}

	return nil
}

func unixMilli(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}

	return t.UnixMilli()
}

func timeFromUnixMilli(value int64) time.Time {
	if value == 0 {
		return time.Time{}
	}

	return time.UnixMilli(value)
}

func isSQLiteConstraintError(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "constraint")
}
