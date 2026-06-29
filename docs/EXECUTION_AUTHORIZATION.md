# Execution Client Authorization

The master always requires mTLS at the HTTPS server layer. `EXECUTE_CLIENT_ALLOWLIST` adds a second authorization check for the operator-facing `/api/v1` surface:

- `POST /api/v1/execute`
- `POST /api/v1/jobs`
- `GET /api/v1/jobs/{request_id}`
- `GET /api/v1/workers`
- `POST /api/v1/workers/{id}/drain`

These are the endpoints used by `wasmcatctl` and the `wasmcat-ui` dashboard. When the allowlist is set, the operator certificate identity (CLI/UI `client_cert`) must be listed, otherwise these calls return `403 execute_client_unauthorized`. A common symptom of a missing entry is a dashboard that loads but cannot list workers or drain a node.

Operator drain (`POST /api/v1/workers/{id}/drain`) is distinct from worker self-drain (`POST /internal/drain`). Self-drain is gated by worker certificate identity and is used by a worker to drain itself on shutdown; it is not affected by `EXECUTE_CLIENT_ALLOWLIST`.

## Configuration

Set `EXECUTE_CLIENT_ALLOWLIST` on the master to a comma-separated list of client certificate identities:

```text
EXECUTE_CLIENT_ALLOWLIST=wasmcat-client,deployer.internal
```

Each value is matched against the caller certificate common name or DNS SAN. Whitespace around commas is ignored.

If the variable is empty, any client certificate trusted by the master CA can call the execution and job APIs. That preserves local development behavior but is not recommended for production.

## Certificate Identity Examples

Accepted by this configuration:

```text
EXECUTE_CLIENT_ALLOWLIST=wasmcat-client,deployer.internal
```

- certificate common name `wasmcat-client`
- certificate DNS SAN `deployer.internal`

Rejected:

- worker certificate common name `wasmcat-worker-worker-us-01`
- any trusted certificate with no matching common name or DNS SAN
- requests without a client certificate when the allowlist is configured

## Failure Contract

Unauthorized execution clients receive:

```json
{
  "error": "Execution client is not authorized.",
  "code": "execute_client_unauthorized"
}
```

The response does not expose the rejected certificate identity. The raw internal error is logged by the master.
