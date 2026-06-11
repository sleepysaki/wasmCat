# Module Source Policy

wasmCat can restrict which module URLs the master accepts before dispatching work to a worker. This protects production clusters from fetching arbitrary code from untrusted hosts.

## Settings

| Variable | Default | Purpose |
| --- | --- | --- |
| `MODULE_HOST_ALLOWLIST` | empty | Comma-separated module URL hosts allowed by `/api/v1/execute`. Empty allows any host for development. |
| `REQUIRE_MODULE_DIGEST` | `false` | Requires `module_digest` or a digest-pinned OCI URL such as `/blobs/sha256:<digest>` or `/manifests/sha256:<digest>`. |

Host matching is exact and case-insensitive. Entries may include a port when the module URL also includes that port, for example `modules.internal:8443`.

## Examples

Allow only an internal module server and one Azure Container Registry:

```text
MODULE_HOST_ALLOWLIST=modules.internal,myregistry.azurecr.io
REQUIRE_MODULE_DIGEST=true
```

Allowed request:

```json
{
  "module_name": "echo",
  "module_url": "https://modules.internal/echo.wasm",
  "module_digest": "sha256:abc123",
  "payload": "hello"
}
```

Allowed digest-pinned OCI blob request:

```json
{
  "module_name": "echo",
  "module_url": "https://myregistry.azurecr.io/v2/team/echo/blobs/sha256:abc123",
  "payload": "hello"
}
```

Rejected request when digest is required:

```json
{
  "module_name": "echo",
  "module_url": "https://modules.internal/echo.wasm",
  "payload": "hello"
}
```

## Operational Notes

- Keep `MODULE_HOST_ALLOWLIST` empty only for local development or tightly controlled test networks.
- Prefer immutable digest-pinned modules in production so reused names or tags cannot silently point to different code.
- For ACR manifest tags such as `latest`, leave `REQUIRE_MODULE_DIGEST=false` or use a digest-pinned manifest/blob URL.
