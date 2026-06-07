# HTTP Retry Policy

wasmCat uses bounded retries for outbound HTTP calls that are safe to repeat. The goal is to absorb short network interruptions without hiding persistent failures or duplicating WASM execution.

## Covered Paths

- Master ACR OAuth exchange requests.
- Master ACR repository token requests.
- Master ACR manifest fetches.
- Worker module downloads.
- Worker registration, heartbeat, and drain telemetry calls.

Master-to-worker `/invoke` dispatch is intentionally not retried. A failed response from that path is ambiguous: the worker may already have executed the module before the connection failed. Retrying could run the same request twice.

## Defaults

| Setting | Default | Purpose |
| --- | --- | --- |
| Attempts | `3` | One initial request plus up to two retries. |
| Backoff | `100ms`, then `200ms` | Short linear delay between attempts. |
| Retry statuses | `408`, `429`, `500`, `502`, `503`, `504` | Common transient timeout, rate-limit, and upstream failure responses. |

The retry helper still obeys the request context and the shared HTTP client timeout. If the caller's context is canceled or reaches its deadline, retries stop immediately.

## Request Body Handling

Retries only replay a request body when Go can recreate it through `Request.GetBody`. Requests built from `bytes.Buffer`, `bytes.Reader`, or `strings.Reader` support this automatically through `http.NewRequest`.

If a request body cannot be replayed, the helper returns an error instead of sending a malformed retry.

## Operational Notes

- Retry failures are still surfaced to the caller after attempts are exhausted.
- Final retryable HTTP responses are returned to the caller so existing status-specific error handling still runs.
- The policy is intentionally small. Longer retry loops belong in higher-level controllers or external supervisors, not inside a single request path.
