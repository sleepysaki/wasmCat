# HA Job Reliability Plan

## Purpose

wasmCat's first HA priority is job reliability: once the master accepts an execution request, that job should survive master restart and remain diagnosable. This is more important than multi-master leadership at this stage because leader election does not help if accepted work only exists in memory.

## Reliability Contract

- `request_id` is the durable job identity.
- The master persists the job before dispatch starts.
- Duplicate requests with the same `request_id` and same execution fingerprint reuse durable state.
- Duplicate requests with the same `request_id` and different execution content return `409 request_id_conflict`.
- Successful worker responses are stored and replayed from SQLite.
- Dispatching and failed states are stored for later recovery work.
- Execution remains at-least-once. Exactly-once side effects require module-level idempotency or a future worker completion protocol.

## Implemented Slice

The current implementation adds a SQLite-backed `JobStore` and wires it into `POST /api/v1/execute` when the master starts normally.

Flow:

1. The gateway validates the execution request and ensures `request_id`.
2. It computes the same execution fingerprint used by request idempotency.
3. It creates a durable `queued` job in SQLite.
4. Before dispatch, it marks the job `dispatching` and increments the attempt count.
5. On success, it stores the `ExecutionResponse` as `succeeded`.
6. On dispatch failure, it stores `failed` with the last error.
7. A duplicate successful request is returned from durable storage, even after reopening the database.

## Current Job States

| State | Meaning |
| --- | --- |
| `queued` | Job is persisted but not yet dispatched. |
| `dispatching` | Master is trying to send the job to a worker. |
| `running` | Reserved for the worker completion protocol. |
| `succeeded` | Result is stored and can be replayed. |
| `failed` | Last synchronous dispatch attempt failed. |
| `ambiguous` | Reserved for cases where execution may have happened but completion is unknown. |

## Next Phases

1. Add a recovery loop that scans `queued` and expired `dispatching` jobs.
2. Add `GET /api/v1/jobs/{request_id}` for durable job inspection.
3. Add `POST /api/v1/jobs` for async submission.
4. Add worker completion callbacks so results can survive master crashes during active execution.
5. Add job-state metrics for queued, dispatching, succeeded, failed, and ambiguous counts.

