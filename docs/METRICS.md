# Metrics

wasmCat exposes dependency-free JSON metrics on both master and worker processes.

## Endpoint

```text
GET /wasmcat/metrics
```

The endpoint is served by the same HTTPS server as health and execution APIs. In normal runtime mode, mTLS is required before the request reaches the handler.

## Shared Fields

| Field | Purpose |
| --- | --- |
| `role` | `master` or `worker`. |
| `node_id` | Worker node ID. Omitted on master. |
| `uptime_seconds` | Seconds since the in-process metrics collector was created. |
| `requests_total` | Total requests observed before the current metrics response is written. |
| `requests_by_status` | Request counts keyed by HTTP status code. |
| `requests_by_path` | Request counts keyed by URL path. |

## Master Fields

The `master` object contains:

| Field | Purpose |
| --- | --- |
| `active_workers` | Number of workers currently in the in-memory registry. |
| `workers_by_state` | Worker counts keyed by state, such as `ready` and `draining`. |
| `oldest_heartbeat_seconds` | Age of the oldest non-zero worker heartbeat. Omitted when no heartbeat is known. |
| `dispatch_success` | Successful `/api/v1/execute` dispatches. |
| `dispatch_failure` | Failed `/api/v1/execute` dispatches. |
| `dispatch_reschedules` | Number of times the dispatcher recovered from a retryable selected-worker failure by trying another worker. |
| `dispatch_reschedule_exhausted` | Number of requests where retryable selected-worker failures consumed all available worker candidates. |
| `request_cache_hits` | Duplicate completed `request_id` requests served from the master response cache without worker dispatch. |
| `request_in_progress_conflicts` | Duplicate `request_id` requests rejected because the original request is still running. |
| `request_id_conflicts` | Requests rejected because a `request_id` was reused with different execution content. |
| `request_tracker.entries` | Current request-idempotency records held in memory. |
| `request_tracker.in_flight` | Current in-flight request IDs. |
| `request_tracker.completed` | Current completed response records available for duplicate retries. |
| `request_tracker.max_entries` | Configured idempotency tracker entry limit. |
| `request_tracker.ttl_seconds` | Configured completed-response TTL in seconds. |
| `request_tracker.evictions` | Completed records evicted because the max-entry limit was reached. |
| `request_tracker.expired` | Completed records removed because their TTL expired. |

## Worker Fields

The `worker` object contains:

| Field | Purpose |
| --- | --- |
| `execution_success` | Successful `/invoke` executions. |
| `execution_failure` | Failed `/invoke` executions. |
| `cache.entries` | Current compiled module cache entries. |
| `cache.bytes` | Raw WASM bytes represented by cache entries. |
| `cache.max_entries` | Configured cache entry limit. |
| `cache.max_bytes` | Configured cache byte limit. |

## Notes

The current format is JSON for simplicity and zero dependencies. It is suitable for smoke checks, custom probes, and early dashboards. A Prometheus text endpoint can be added later without replacing this endpoint.

High `dispatch_reschedules` means workers are becoming unavailable after registration but before execution. High `dispatch_reschedule_exhausted` means the master is running out of healthy execution capacity for at least some requests.

High `request_cache_hits` usually means clients are retrying safely. High `request_in_progress_conflicts` means clients are retrying before the original request completes. Any `request_id_conflicts` should be investigated because a caller is reusing request IDs for different work.

When `request_tracker.entries` stays near `request_tracker.max_entries` and `request_tracker.evictions` rises, increase `EXECUTION_REQUEST_CACHE_MAX_ENTRIES` or reduce client retry windows. Rising `request_tracker.expired` is normal when completed request IDs age out.
