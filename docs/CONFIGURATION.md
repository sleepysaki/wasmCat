# Configuration

wasmCat reads runtime configuration from environment variables and keeps local-development defaults when values are not set.

Module cache behavior is documented in [MODULE_CACHE_LIFECYCLE.md](MODULE_CACHE_LIFECYCLE.md).
Execution client authorization is documented in [EXECUTION_AUTHORIZATION.md](EXECUTION_AUTHORIZATION.md).

## Master

| Variable | Default | Purpose |
| --- | --- | --- |
| `MASTER_PORT` | `7270` | HTTPS port for the master gateway. |
| `CERT_DIR` | `./certs` | Directory containing `ca.crt`, `master.crt`, `master.key`, and worker certs. |
| `AUTO_GENERATE_CERTS` | `true` | Generate local development CA/master/worker certs on master startup. Disable in production. |
| `DEV_WORKER_ID` | `worker-vn-01` | Worker ID used by local dev certificate generation. |
| `CLEANUP_INTERVAL` | `15s` | How often the master scans the registry for stale workers. |
| `WORKER_STALE_TIMEOUT` | `30s` | How long a worker may go without heartbeat updates before cleanup removes it. |
| `MIN_WORKER_CPU_FREE` | `0` | Minimum free CPU percentage required for a worker to receive new work. |
| `MIN_WORKER_RAM_FREE_MB` | `0` | Minimum free RAM in MiB required for a worker to receive new work. |
| `EXECUTE_CLIENT_ALLOWLIST` | empty | Comma-separated client certificate CN or DNS SAN values allowed to call `/api/v1/execute`. Empty means any trusted mTLS client can execute work. |
| `MAX_EXECUTION_REQUEST_BYTES` | `2097152` | Maximum JSON body size accepted by the master `/api/v1/execute` endpoint. |

## Worker

| Variable | Default | Purpose |
| --- | --- | --- |
| `WORKER_ID` | `worker-vn-01` | Worker node ID. |
| `WORKER_PORT` | `7271` | HTTPS port for the worker invoke server. |
| `MASTER_URL` | `https://localhost:7270` | Master URL used for registration and heartbeat. |
| `WORKER_ADVERTISE_ADDRESS` | `localhost:<worker-port>` | Address the master uses to call this worker. |
| `WORKER_LATITUDE` | `0` | Worker latitude used by the scheduler. Must be between `-90` and `90`. |
| `WORKER_LONGITUDE` | `0` | Worker longitude used by the scheduler. Must be between `-180` and `180`. |
| `CERT_DIR` | `./certs` | Directory containing `ca.crt`, `worker-<id>.crt`, and `worker-<id>.key`. |
| `HEARTBEAT_INTERVAL` | `5s` | Worker registration and heartbeat interval. |

Worker registration and heartbeat require the mTLS client certificate identity to match `WORKER_ID`. The master accepts a certificate common name of `wasmcat-worker-<WORKER_ID>` or a DNS SAN containing either `<WORKER_ID>` or `wasmcat-worker-<WORKER_ID>`.

## Worker Limits

| Variable | Default | Purpose |
| --- | --- | --- |
| `EXECUTION_TIMEOUT` | `5s` | Max time for a worker execution path. |
| `MODULE_FETCH_TIMEOUT` | `10s` | Max time to fetch and compile a module. |
| `WORKER_SHUTDOWN_TIMEOUT` | `10s` | Max graceful shutdown time for in-flight worker HTTP requests. Defaults to `EXECUTION_TIMEOUT + 5s` when unset. |
| `MAX_MODULE_BYTES` | `10485760` | Max downloaded module size. |
| `MAX_PAYLOAD_BYTES` | `1048576` | Max request payload size. |
| `MAX_OUTPUT_BYTES` | `1048576` | Max WASM output size. |
| `MAX_CONCURRENT_EXECS` | `4` | Max simultaneous executions per worker. |
| `MAX_CACHED_MODULES` | `128` | Max compiled WASM modules kept in the worker cache. |
| `MAX_CACHE_BYTES` | `268435456` | Max raw WASM bytes represented by cached compiled modules. |
| `MODULE_CACHE_TTL` | `30m` | Max age for a compiled module cache entry before refetch. |

Example:

```powershell
$env:WORKER_ID="worker-us-01"
$env:MASTER_URL="https://master.internal:7270"
$env:WORKER_LATITUDE="40.7128"
$env:WORKER_LONGITUDE="-74.0060"
$env:CERT_DIR="C:\wasmcat\certs"
$env:MAX_CONCURRENT_EXECS="8"
go run ./cmd/worker
```

For production, mount certificates as secrets and disable local certificate generation:

```powershell
$env:AUTO_GENERATE_CERTS="false"
$env:CERT_DIR="C:\wasmcat\certs"
go run ./cmd/master
```
