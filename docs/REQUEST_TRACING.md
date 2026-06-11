# Request Tracing

wasmCat assigns a `request_id` to every execution request so operators can follow one invocation through master ingress, scheduling, worker execution, and the final response. The same field also acts as a bounded in-memory idempotency key on the master.

## Request ID Rules

Clients may provide `request_id` in `POST /api/v1/execute`:

```json
{
  "request_id": "client:deploy-42",
  "module_name": "echo",
  "module_url": "https://modules.example.com/echo.wasm",
  "payload": "hello"
}
```

If the client omits it, the master generates an ID with this shape:

```text
req_<32 lowercase hex characters>
```

Accepted client IDs are limited to 128 characters and may contain letters, digits, `_`, `-`, `.`, and `:`.

## Propagation

1. The master validates or creates `request_id` in `/api/v1/execute`.
2. The dispatcher forwards the same ID to the selected worker.
3. The worker returns the same ID in `ExecutionResponse`.
4. The master returns that ID to the client and logs it around scheduling and dispatch.
5. After a successful response, the master keeps the response for `EXECUTION_REQUEST_CACHE_TTL` so a duplicate request with the same ID and same content can receive the cached response without dispatching again.

The worker also includes `execution_time_ms` in the response. This is measured around the worker-side invoke path: request validation, cache lookup or module fetch, WASM instantiation, execution, and response construction.

## Operational Use

Use `request_id` when investigating:

- dispatch failures where the worker may have received the request;
- slow module downloads or cold compiles;
- ACR token or manifest fetch failures;
- repeated client submissions that should be deduplicated or rejected.

If a duplicate request arrives while the original is still running, the master returns `409 request_in_progress`. If the same ID is reused with different execution content, the master returns `409 request_id_conflict`.

Idempotency history is process-local and in memory. A master restart clears it, and multiple masters do not share it.
