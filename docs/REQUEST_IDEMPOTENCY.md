# Request Idempotency

wasmCat uses `request_id` as both a trace identifier and a bounded in-memory idempotency key for `/api/v1/execute`.

## Behavior

When an execution request reaches the master:

1. The gateway validates or creates `request_id`.
2. The gateway fingerprints the execution identity: module name, module URL/registry URL, module digest, payload, and user location.
3. If the `request_id` is new, the gateway marks it in-flight and dispatches normally.
4. If the same `request_id` and same fingerprint arrive while the first request is still running, the gateway returns `409 request_in_progress`.
5. If the same `request_id` and same fingerprint arrive after success, the gateway returns the cached `ExecutionResponse` without dispatching to a worker again.
6. If the same `request_id` is reused with different request content, the gateway returns `409 request_id_conflict`.
7. If dispatch fails, the gateway forgets the `request_id` so clients can retry the same request after fixing capacity or worker availability.

## Configuration

`EXECUTION_REQUEST_CACHE_TTL` controls how long successful responses remain available for duplicate request IDs. The default is `5m`.

`EXECUTION_REQUEST_CACHE_MAX_ENTRIES` controls the maximum number of in-memory request records kept by the master. The default is `4096`. When the tracker exceeds this limit, it evicts the least recently used completed records first. In-flight records are not evicted because doing so could allow duplicate execution while the original request is still running.

This cache is process-local and in memory. A master restart clears it, and multiple master instances do not share request history.

## Operational Notes

- Clients should send stable `request_id` values when they retry a user-visible operation.
- Do not reuse a `request_id` for a different payload or module.
- Watch `master.request_cache_hits` to confirm client retries are being deduplicated.
- Watch `master.request_in_progress_conflicts` for clients retrying too aggressively before the first request completes.
- Treat any `master.request_id_conflicts` as a client bug or request identity collision.
- Watch `master.request_tracker.entries`, `master.request_tracker.evictions`, and `master.request_tracker.expired` to tune cache size and TTL.
- This feature prevents accidental duplicate dispatch through one master process. It is not a durable exactly-once execution system.
- Durable idempotency across restarts or multiple masters would require external storage and stricter request ownership.
