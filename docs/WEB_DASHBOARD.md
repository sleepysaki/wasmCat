# wasmCat Web Dashboard

## Purpose

`wasmcat-ui` is a local operator dashboard for wasmCat. It provides the same operational actions as `wasmcatctl` without requiring long mTLS `curl` commands.

The browser does not connect directly to the master. Instead:

```text
Browser -> wasmcat-ui -> wasmcat-master
```

The UI backend reads the same config file used by `wasmcatctl`, creates the mTLS client, and calls the master APIs.

## Start The Dashboard

Create or verify the CLI/UI config first:

```powershell
go run ./cmd/wasmcatctl config init `
  --master https://localhost:7270 `
  --ca .\local\certs\ca.crt `
  --cert .\local\certs\worker-worker-vn-01.crt `
  --key .\local\certs\worker-worker-vn-01.key `
  --user-lat 21.0278 `
  --user-lon 105.8342
```

Start the dashboard:

```powershell
go run ./cmd/wasmcat-ui --listen :7280
```

Open:

```text
http://localhost:7280
```

Use a custom config path:

```powershell
go run ./cmd/wasmcat-ui --listen :7280 --config .\my-wasmcat-config.json
```

## Supported Operations

The dashboard can perform the current `wasmcatctl` action set:

- Check master health.
- Check master readiness.
- View master metrics.
- List workers.
- Drain a worker.
- Execute a module synchronously.
- Create a durable job.
- Query a durable job by `request_id`.
- View and save UI/CLI mTLS configuration.

## Pages

### Dashboard

Shows master health, readiness, active worker count, dispatch success count, and the latest refresh status. The tiles load independently: if one endpoint is unavailable or gated (for example worker listing under an execute-client allowlist), that tile shows `error` and the others still render. The connection badge reads `Connected`, `Degraded` (some calls failed), or `Disconnected` (all calls failed).

Use the **Auto-refresh** toggle in the top bar to poll the live views (dashboard, workers, metrics) every 5 seconds. It does not poll the Execute, Jobs, or Settings forms so it never clears input.

### Workers

Lists registered workers with address, state, CPU/RAM availability, location, last heartbeat age, and a drain action. A worker already in the `draining` state shows a disabled button instead of a drain action.

Drain calls the operator endpoint `POST /api/v1/workers/{id}/drain`. If the master has `EXECUTE_CLIENT_ALLOWLIST` set, the UI `client_cert` identity must be in that allowlist or drain (and worker listing) return `403 execute_client_unauthorized`. See [EXECUTION_AUTHORIZATION.md](EXECUTION_AUTHORIZATION.md).

### Execute

Runs `POST /api/v1/execute` through the UI backend. It supports direct module URLs and Azure ACR registry URLs, and an ABI selector (auto-detect / `wasmcat` / `wasi`) so you can run both custom-ABI modules and standard `wasip1` WASI modules.

### Jobs

Queries `GET /api/v1/jobs/{request_id}`. The master does not currently expose a full job listing endpoint, so the dashboard requires a known request ID.

### Metrics

Shows a compact metric summary and the full JSON metrics response.

### Settings

Reads and writes the same JSON config format used by `wasmcatctl`:

```json
{
  "master_url": "https://localhost:7270",
  "ca_cert": "./local/certs/ca.crt",
  "client_cert": "./local/certs/worker-worker-vn-01.crt",
  "client_key": "./local/certs/worker-worker-vn-01.key",
  "default_user_lat": 21.0278,
  "default_user_lon": 105.8342,
  "output": "table"
}
```

## Security Notes

Keep `wasmcat-ui` on a trusted operator machine or behind a trusted network boundary. The dashboard backend can read the configured client private key path and use it to call the master. Do not expose `wasmcat-ui` directly to untrusted networks without adding authentication in front of it.

The master still enforces mTLS. The UI is only a convenience layer; it does not weaken master-worker authentication.

## Release Artifact

Release builds include:

```text
wasmcat-ui-linux-amd64
wasmcat-ui-windows-amd64.exe
```
