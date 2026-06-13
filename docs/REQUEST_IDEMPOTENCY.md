# Request Idempotency

wasmCat uses `request_id` as both a trace identifier and an idempotency key for `/api/v1/execute`. Normal master startup stores accepted jobs and successful responses in the durable SQLite job store. Handler-only or embedded gateway paths without a `JobStore` fall back to the bounded in-memory tracker.

## Behavior

When an execution request reaches the master:

1. The gateway validates or creates `request_id`.
2. The gateway fingerprints the execution identity: module name, module URL/registry URL, module digest, payload, and user location.
3. If the `request_id` is new, the gateway creates a durable `queued` job and dispatches normally.
4. If the same `request_id` and same fingerprint arrive while the first request is still running, the gateway returns `409 request_in_progress`.
5. If the same `request_id` and same fingerprint arrive after success, the gateway returns the stored `ExecutionResponse` without dispatching to a worker again.
6. If the same `request_id` is reused with different request content, the gateway returns `409 request_id_conflict`.
7. If dispatch fails, the durable job records `failed` and `last_error`. It can be inspected through `GET /api/v1/jobs/{request_id}`.

## Configuration

`JOB_STORE_PATH` controls where durable job state is stored. Completed jobs can be queried with:

```text
GET /api/v1/jobs/{request_id}
```

`EXECUTION_REQUEST_CACHE_TTL` and `EXECUTION_REQUEST_CACHE_MAX_ENTRIES` still configure the fallback in-memory tracker used when a gateway has no `JobStore`.

The current durable store is local SQLite. A single master can survive restart with request history intact. Multiple active masters still need shared storage or leader election before they can safely share idempotency state.

## Operational Notes

- Clients should send stable `request_id` values when they retry a user-visible operation.
- Do not reuse a `request_id` for a different payload or module.
- Watch `master.request_cache_hits` to confirm client retries are being deduplicated.
- Watch `master.request_in_progress_conflicts` for clients retrying too aggressively before the first request completes.
- Treat any `master.request_id_conflicts` as a client bug or request identity collision.
- Use `GET /api/v1/jobs/{request_id}` to inspect `queued`, `dispatching`, `succeeded`, `failed`, and `ambiguous` durable jobs.
- This feature provides durable idempotency for one master. It is not an exactly-once execution system.
