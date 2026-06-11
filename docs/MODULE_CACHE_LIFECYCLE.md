# Module Cache Lifecycle

wasmCat workers cache compiled WASM modules to avoid downloading and compiling the same module on every execution. The cache is process-local and lives inside `worker.WasmEngine`.

## Purpose

The cache must be fast, but it must not grow forever or keep executing stale module bytes. Production workers now bound the cache by entry count, byte budget, and TTL. Cache keys are digest-aware so a reused `module_name` can still point to new immutable content safely.

## Cache Identity

The worker builds a `ModuleCacheKey` from:

- `module_name`
- `module_url`
- `module_digest`

Key priority:

1. `module_name + module_digest` when `module_digest` is present.
2. `module_name + module_url` when no digest is present.
3. `module_name` only as a final fallback.

ACR manifest resolution sets `module_digest` from the selected OCI layer digest. This means `echo:latest` can move to a new blob while the worker still distinguishes old and new compiled modules.

When `module_digest` is present, the worker verifies that downloaded bytes match the declared `sha256:<hex>` digest before compiling or caching them. A mismatch is rejected before WASM compilation starts.

## Eviction Rules

Eviction runs after a new module is compiled and inserted:

1. Remove entries older than `MODULE_CACHE_TTL`.
2. If entries exceed `MAX_CACHED_MODULES`, remove least-recently-used entries.
3. If cache bytes exceed `MAX_CACHE_BYTES`, continue removing least-recently-used entries.

The byte budget uses raw WASM byte size as the accounting unit. It is a practical approximation for compiled-module pressure and can be replaced later with runtime-specific memory accounting if wazero exposes it.

## Concurrency

Cold requests for the same cache key are coalesced. The first request downloads and compiles the module; followers wait for that compile to finish and then use the cached result. This prevents burst traffic from compiling the same module multiple times.

## Configuration

Worker environment variables:

| Variable | Default | Purpose |
| --- | --- | --- |
| `MAX_CACHED_MODULES` | `128` | Maximum compiled modules in memory. |
| `MAX_CACHE_BYTES` | `268435456` | Maximum raw WASM bytes represented by cache entries. |
| `MODULE_CACHE_TTL` | `30m` | Maximum cache entry age before refetch. |

Example:

```powershell
$env:MAX_CACHED_MODULES="64"
$env:MAX_CACHE_BYTES="134217728"
$env:MODULE_CACHE_TTL="15m"
go run ./cmd/worker
```

## Testing

Cache lifecycle behavior is covered in `tests/worker/cache_lifecycle_test.go`:

- digest-aware cache keys
- worker-side SHA-256 digest verification
- TTL refetch
- least-recently-used eviction by entry count
- eviction by byte budget
- concurrent cold-fetch coalescing
