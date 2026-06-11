# Error Contracts

wasmCat returns stable JSON error responses without exposing raw internal errors.

## Response Shape

```json
{
  "error": "Invalid execution request.",
  "code": "invalid_execution_request"
}
```

Clients should branch on `code`. The `error` field is a safe human-readable summary and may be adjusted for clarity without changing the failure category.

## Internal Details

Handlers pass the real Go error to `shared.WriteError`. The function logs the raw error with `slog.Warn`, then returns a safe public message. This prevents leaking filesystem paths, TLS details, registry URLs, Azure token errors, worker addresses, or validation internals through API responses.

## Current Codes

| Code | Public Message |
| --- | --- |
| `method_not_allowed` | `HTTP method is not allowed for this endpoint.` |
| `execute_client_unauthorized` | `Execution client is not authorized.` |
| `request_body_too_large` | `Request body is too large.` |
| `invalid_worker_data` | `Invalid worker registration request.` |
| `invalid_heartbeat` | `Invalid worker heartbeat request.` |
| `invalid_drain_request` | `Invalid worker drain request.` |
| `worker_identity_mismatch` | `Worker certificate identity does not match the requested worker ID.` |
| `worker_not_found` | `Worker is not registered.` |
| `invalid_execution_request` | `Invalid execution request.` |
| `request_in_progress` | `Execution request is already in progress.` |
| `request_id_conflict` | `Request ID was already used for different request content.` |
| `module_policy_violation` | `Module source policy rejected the request.` |
| `module_digest_invalid` | `Module digest is invalid.` |
| `module_digest_mismatch` | `Module digest does not match downloaded content.` |
| `dispatch_failed` | `Execution could not be dispatched.` |
| `execution_failed` | `WASM execution failed.` |
| `not_ready` | `Service is not ready.` |
| `worker_failed` | `Worker request failed.` |

Unknown codes fall back to `Request failed.`

## Testing

`tests/shared/error_contract_test.go` verifies that public errors do not contain internal details. Handler tests assert endpoint-specific codes and safe messages, including worker module digest failures.
