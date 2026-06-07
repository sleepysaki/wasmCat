# Request Tracing

wasmCat assigns a `request_id` to every execution request so operators can follow one invocation through master ingress, scheduling, worker execution, and the final response.

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

The worker also includes `execution_time_ms` in the response. This is measured around the worker-side invoke path: request validation, cache lookup or module fetch, WASM instantiation, execution, and response construction.

## Operational Use

Use `request_id` when investigating:

- dispatch failures where the worker may have received the request;
- slow module downloads or cold compiles;
- ACR token or manifest fetch failures;
- repeated client submissions that should be compared manually.

`request_id` is a trace/correlation field, not an idempotency guarantee. wasmCat still does not retry `/invoke`, and it does not currently store completed request IDs.
