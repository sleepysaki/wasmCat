# HTTP Client Timeouts

wasmCat uses bounded outbound HTTP clients for worker fetches, ACR calls, telemetry, and master-to-worker dispatch. This prevents slow or broken network peers from holding goroutines and sockets indefinitely.

## Covered Paths

- Worker module downloads in `WasmEngine`.
- Master ACR manifest fetches.
- Master ACR OAuth exchange and token requests.
- Master dispatcher calls to worker `/invoke` over mTLS.
- Worker registration and heartbeat calls to the master over mTLS.

Retry behavior is documented separately in `docs/HTTP_RETRIES.md`. Timeouts cap each outbound request path; retries decide whether a transient failure should be attempted again before the caller receives the final error or response.

## Defaults

| Setting | Default | Purpose |
| --- | --- | --- |
| Total client timeout | `30s` | Upper bound for the full request, including redirects and body read. |
| Dial timeout | `10s` | Maximum time to establish a TCP connection. |
| TLS handshake timeout | `10s` | Maximum time to complete TLS negotiation. |
| Response header timeout | `10s` | Maximum wait for response headers after the request is written. |
| Idle connection timeout | `90s` | Maximum time an idle keep-alive connection remains open. |
| Max idle connections | `100` | Process-wide idle connection pool limit per client. |
| Max idle connections per host | `10` | Per-host keep-alive limit. |

## Design Notes

Request contexts still matter. Worker execution, module fetch, ACR resolution, and dispatcher calls all pass contexts downward. The HTTP client defaults are the lower-level safety net for dial, TLS, response-header, and full-request hangs.

mTLS clients are created through the same shared transport factory, then receive the caller's certificate, root CA pool, and minimum TLS version. This keeps security behavior and network timeout behavior consistent between master and worker clients.

## Testing

`tests/shared/client_test.go` verifies the shared client factory keeps bounded timeouts and preserves caller-provided TLS configuration.
