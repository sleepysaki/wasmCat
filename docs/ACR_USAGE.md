# Azure ACR Module Fetching

This project can fetch a private WASM module from Azure Container Registry when the execution request uses an ACR URL.

## Current Support

The current implementation supports direct OCI registry URLs that point to downloadable content, for example:

```text
https://myregistry.azurecr.io/v2/team/echo/blobs/sha256:<digest>
```

The master detects `azurecr.io`, mints a repository-scoped pull token using `DefaultAzureCredential`, forwards that token to the worker as `jit_bearer_token`, and the worker sends it as:

```text
Authorization: Bearer <token>
```

Tag or manifest resolution is not implemented yet. If you send a tag URL such as `/manifests/latest`, the worker will download the manifest JSON, not the actual `.wasm` bytes, and compilation will fail.

## Azure Identity Setup

Run the master with an Azure identity that has pull access to the ACR repository.

For local development, sign in with Azure CLI:

```bash
az login
az acr login --name myregistry
```

For production, prefer a managed identity or service principal. The identity needs at least `AcrPull` on the target registry or repository scope.

## Execution Request Example

Send the module URL through `module_url` or `module_registry_url`:

```json
{
  "module_name": "echo",
  "module_url": "https://myregistry.azurecr.io/v2/team/echo/blobs/sha256:<digest>",
  "payload": "hello wasm",
  "user_lat": 10.762622,
  "user_lon": 106.660172
}
```

Local curl example, using the development mTLS certificates:

```bash
curl --cert ./certs/master.crt \
  --key ./certs/master.key \
  --cacert ./certs/ca.crt \
  -H "Content-Type: application/json" \
  -d @request.json \
  https://localhost:7270/api/v1/execute
```

## Expected WASM ABI

The downloaded module must export:

```text
memory
malloc(size uint32) uint32
run(ptr uint32, len uint32) uint64
```

`run` returns one packed `uint64`:

```text
high 32 bits = output pointer
low 32 bits  = output length
```

## Known Limitations

- The worker performs a simple HTTP GET and compiles the response body as raw WASM.
- OCI manifest lookup is not implemented.
- Token caching is not implemented; the master mints a token per ACR execution request.
- Module download size limits and HTTP client timeouts are still needed.
- The worker cache key is `module_name`, so changing a digest while reusing the same name may keep the old compiled module until restart.
