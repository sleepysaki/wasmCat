# HTTP Server Timeouts

wasmCat bounds inbound HTTP server behavior for both master and worker processes. These limits protect the servers from clients that open connections slowly, stall request bodies, or keep idle connections around too long.

## Covered Servers

- Master gateway HTTPS server.
- Worker invoke HTTPS server.

Both servers still require mTLS at the TLS configuration layer. Server timeouts are an additional resource-safety policy.

## Defaults

| Setting | Default | Purpose |
| --- | --- | --- |
| Read header timeout | `5s` | Maximum time to receive complete request headers. |
| Read timeout | `30s` | Maximum time to read the full request, including the body. |
| Write timeout | `30s` | Maximum time to write the response. |
| Idle timeout | `60s` | Maximum time to keep an idle keep-alive connection open. |

## Notes

Request-specific contexts still control downstream work such as worker execution, module fetches, ACR resolution, and dispatch. Server timeouts only bound the HTTP connection lifecycle.

Master request body size limits are separate:

- internal worker control endpoints are capped at 4 KiB;
- `/api/v1/execute` uses `MAX_EXECUTION_REQUEST_BYTES`.
