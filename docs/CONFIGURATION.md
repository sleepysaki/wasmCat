# Configuration

wasmCat reads runtime configuration from environment variables and keeps local-development defaults when values are not set.

## Master

| Variable | Default | Purpose |
| --- | --- | --- |
| `MASTER_PORT` | `7270` | HTTPS port for the master gateway. |
| `DEV_WORKER_ID` | `worker-vn-01` | Worker ID used by local dev certificate generation. |
| `CLEANUP_INTERVAL` | `15s` | How often the master removes stale workers from the registry. |

## Worker

| Variable | Default | Purpose |
| --- | --- | --- |
| `WORKER_ID` | `worker-vn-01` | Worker node ID. |
| `WORKER_PORT` | `7271` | HTTPS port for the worker invoke server. |
| `MASTER_URL` | `https://localhost:7270` | Master URL used for registration and heartbeat. |
| `WORKER_ADVERTISE_ADDRESS` | `localhost:<worker-port>` | Address the master uses to call this worker. |
| `HEARTBEAT_INTERVAL` | `5s` | Worker registration and heartbeat interval. |

## Worker Limits

| Variable | Default | Purpose |
| --- | --- | --- |
| `EXECUTION_TIMEOUT` | `5s` | Max time for a worker execution path. |
| `MODULE_FETCH_TIMEOUT` | `10s` | Max time to fetch and compile a module. |
| `MAX_MODULE_BYTES` | `10485760` | Max downloaded module size. |
| `MAX_PAYLOAD_BYTES` | `1048576` | Max request payload size. |
| `MAX_OUTPUT_BYTES` | `1048576` | Max WASM output size. |
| `MAX_CONCURRENT_EXECS` | `4` | Max simultaneous executions per worker. |

Example:

```powershell
$env:WORKER_ID="worker-us-01"
$env:MASTER_URL="https://master.internal:7270"
$env:MAX_CONCURRENT_EXECS="8"
go run ./cmd/worker
```
