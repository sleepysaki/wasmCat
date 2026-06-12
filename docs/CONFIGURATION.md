# Configuration

wasmCat reads runtime configuration from environment variables and keeps local-development defaults when values are not set.

Module cache behavior is documented in [MODULE_CACHE_LIFECYCLE.md](MODULE_CACHE_LIFECYCLE.md).
Execution client authorization is documented in [EXECUTION_AUTHORIZATION.md](EXECUTION_AUTHORIZATION.md).
Module source policy is documented in [MODULE_SOURCE_POLICY.md](MODULE_SOURCE_POLICY.md).
Request idempotency is documented in [REQUEST_IDEMPOTENCY.md](REQUEST_IDEMPOTENCY.md).

All duration values use Go duration syntax, such as `5s`, `30s`, or `30m`, and must be greater than zero.
Byte and count limits must be greater than zero. CPU scheduling thresholds must be between `0` and `100`; RAM thresholds must be zero or greater.

## Master

| Variable | Default | Purpose |
| --- | --- | --- |
| `MASTER_PORT` | `7270` | HTTPS port for the master gateway. |
| `CERT_DIR` | `./certs` | Directory containing `ca.crt`, `master.crt`, `master.key`, and worker certs. |
| `AUTO_GENERATE_CERTS` | `true` | Generate local development CA/master/worker certs on master startup. Disable in production. |
| `DEV_WORKER_ID` | `worker-vn-01` | Worker ID used by local dev certificate generation. |
| `CLEANUP_INTERVAL` | `15s` | How often the master scans the registry for stale workers. |
| `WORKER_STALE_TIMEOUT` | `30s` | How long a worker may go without heartbeat updates before cleanup removes it. |
| `MASTER_SHUTDOWN_TIMEOUT` | `5s` | Max graceful shutdown time for in-flight master HTTP requests. |
| `MIN_WORKER_CPU_FREE` | `0` | Minimum free CPU percentage required for a worker to receive new work. |
| `MIN_WORKER_RAM_FREE_MB` | `0` | Minimum free RAM in MiB required for a worker to receive new work. |
| `EXECUTE_CLIENT_ALLOWLIST` | empty | Comma-separated client certificate CN or DNS SAN values allowed to call `/api/v1/execute`. Empty means any trusted mTLS client can execute work. |
| `MAX_EXECUTION_REQUEST_BYTES` | `2097152` | Maximum JSON body size accepted by the master `/api/v1/execute` endpoint. |
| `EXECUTION_REQUEST_CACHE_TTL` | `5m` | How long successful `/api/v1/execute` responses are cached by `request_id` for duplicate client retries. |
| `EXECUTION_REQUEST_CACHE_MAX_ENTRIES` | `4096` | Maximum in-memory request records kept by the master idempotency tracker. Completed records are evicted before in-flight records. |
| `JOB_STORE_PATH` | `./wasmcat-jobs.db` | SQLite file used for durable execution job state and persisted successful responses. |
| `JOB_MAX_ATTEMPTS` | `3` | Maximum synchronous dispatch attempts recorded for one durable job before duplicate retries are rejected. |
| `JOB_LEASE_TTL` | `30s` | Lease duration written when the master starts dispatching a durable job. Recovery uses this later to identify expired active work. |
| `JOB_RECOVERY_INTERVAL` | `5s` | How often the master scans durable jobs for queued work and expired leases. |
| `JOB_RECOVERY_BATCH_SIZE` | `32` | Maximum durable jobs processed during one recovery scan. |
| `MODULE_HOST_ALLOWLIST` | empty | Comma-separated module URL hosts allowed by `/api/v1/execute`. Empty allows any host. |
| `REQUIRE_MODULE_DIGEST` | `false` | Require `module_digest` or a digest-pinned OCI manifest/blob URL before dispatch. |

## Worker

| Variable | Default | Purpose |
| --- | --- | --- |
| `WORKER_ID` | `worker-vn-01` | Worker node ID. |
| `WORKER_PORT` | `7271` | HTTPS port for the worker invoke server. |
| `MASTER_URL` | `https://localhost:7270` | Master URL used for registration and heartbeat. |
| `WORKER_ADVERTISE_ADDRESS` | `<hostname>:<worker-port>` | Address the master uses to call this worker. If the OS hostname cannot be read, the fallback is `localhost:<worker-port>`. |
| `WORKER_LATITUDE` | `0` | Worker latitude used by the scheduler. Must be between `-90` and `90`. |
| `WORKER_LONGITUDE` | `0` | Worker longitude used by the scheduler. Must be between `-180` and `180`. |
| `CERT_DIR` | `./certs` | Directory containing `ca.crt`, `worker-<id>.crt`, and `worker-<id>.key`. |
| `HEARTBEAT_INTERVAL` | `5s` | Worker registration and heartbeat interval. |

Worker registration and heartbeat require the mTLS client certificate identity to match `WORKER_ID`. The master accepts a certificate common name of `wasmcat-worker-<WORKER_ID>` or a DNS SAN containing either `<WORKER_ID>` or `wasmcat-worker-<WORKER_ID>`.

Set `WORKER_ADVERTISE_ADDRESS` explicitly in production when the OS hostname is not resolvable from the master, such as private IP deployments, split DNS, NAT, or custom service discovery. Use `localhost:<port>` only for same-host development.

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
