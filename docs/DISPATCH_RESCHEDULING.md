# Dispatch Rescheduling

wasmCat uses conservative dispatch rescheduling to improve availability without blindly replaying user code.

## Purpose

The master normally selects the closest schedulable worker and forwards the execution request to that worker's `/invoke` endpoint. In production, a selected worker can become unreachable between its last heartbeat and the dispatch attempt. Rescheduling lets the master try another eligible worker when the first selected worker clearly fails in a retryable way.

## Retryable Failures

The dispatcher may select another worker when the selected worker fails before a usable execution response:

- Network or transport error from the master HTTP client.
- Worker response status `502 Bad Gateway`.
- Worker response status `503 Service Unavailable`.
- Worker response status `504 Gateway Timeout`.

These failures usually mean the selected worker, its listener, or a gateway in front of it was unavailable.

## Non-Retryable Failures

The dispatcher does not reschedule on failures that may represent completed or partially completed WASM execution:

- Worker `400` errors, including `execution_failed`.
- Invalid successful worker JSON response.
- Request marshal or request construction errors.
- Scheduler errors such as no worker meeting capacity requirements.
- Context cancellation or deadline expiry.

This boundary avoids duplicating user code execution when the master cannot prove the original worker did not run the module.

## Control Flow

1. The dispatcher loads ready workers from the registry.
2. The scheduler selects the closest candidate by capacity and location.
3. The dispatcher forwards the request to `/invoke`.
4. On success, the response is returned immediately.
5. On retryable failure, the failed worker is removed from the in-memory candidate list and the scheduler selects again.
6. On non-retryable failure or exhausted candidates, the original error is returned.

Rescheduling is per request and process-local. It does not persist job state, mark a worker unhealthy, or provide exactly-once execution.

## Operational Notes

- Keep worker heartbeat and stale-timeout settings tight enough that dead workers leave the registry quickly.
- Use `request_id` for tracing rescheduled attempts across logs.
- Treat this feature as availability hardening, not durable job retry. Durable execution would require persisted request state and idempotency controls.
