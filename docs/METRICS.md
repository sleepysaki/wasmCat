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
| `oldest_heartbeat_seconds` | Age of the oldest non-zero worker heartbeat. Omitted when no heartbeat is known. |
| `dispatch_success` | Successful `/api/v1/execute` dispatches. |
| `dispatch_failure` | Failed `/api/v1/execute` dispatches. |

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
