# wasmCat Operating Tutorial

This guide walks through a local, single-master/single-worker wasmCat cluster from a clean clone to a working WebAssembly execution request. The commands match the current Go entrypoints, bootstrap flags, mTLS certificate layout, and gateway routes.

## 1. Prerequisites & Building

Install:

- **Go 1.26+**. The module declares `go 1.26.2` in `go.mod`.
- **PowerShell 7+ or Windows PowerShell** for the Windows examples below.
- **Azure CLI** only if you execute modules from Azure Container Registry (ACR). The master uses Azure `DefaultAzureCredential`, so local ACR testing should start with `az login`.

From the repository root, build release-style native binaries:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\build.ps1 -Version dev
```

This writes:

```text
dist/wasmcat-master-windows-amd64.exe
dist/wasmcat-worker-windows-amd64.exe
dist/wasmcat-master-linux-amd64
dist/wasmcat-worker-linux-amd64
dist/checksums.txt
```

For Linux/macOS shells, use:

```bash
VERSION=dev ./scripts/build.sh
```

For ACR-backed workloads, authenticate before starting the master:

```bash
az login
az acr login --name <registry-name>
```

The Azure identity needs `AcrPull` on the registry or target repository.

## 2. Cluster Initialization: The `init` Phase

The binaries read configuration from environment variables. The `init` commands generate plain `KEY=value` files, but they do not automatically load those files into your shell.

### Master Init

Run this from the repository root:

```powershell
.\dist\wasmcat-master-windows-amd64.exe init `
  --config-dir .\run\wasmcat `
  --cert-dir .\run\wasmcat\certs `
  --port 7270 `
  --dev-certs `
  --dev-worker-id worker-vn-01 `
  --force
```

Under the hood, this calls `bootstrap.InitMaster` and:

- creates `run/wasmcat/master.env`;
- creates `run/wasmcat/certs/`;
- generates local development mTLS files:
  - `ca.crt`
  - `ca.key`
  - `master.crt`
  - `master.key`
  - `worker-worker-vn-01.crt`
  - `worker-worker-vn-01.key`
- writes `AUTO_GENERATE_CERTS=false` into `master.env` so normal startup uses the generated certs instead of overwriting them.

Important generated master values include:

```text
MASTER_PORT=7270
CERT_DIR=run/wasmcat/certs
JOB_STORE_PATH=run/wasmcat/wasmcat-jobs.db
JOB_RECOVERY_INTERVAL=5s
```

### Worker Init

Run:

```powershell
.\dist\wasmcat-worker-windows-amd64.exe init `
  --config-dir .\run\wasmcat `
  --cert-dir .\run\wasmcat\certs `
  --worker-id worker-vn-01 `
  --port 7271 `
  --master-url https://localhost:7270 `
  --advertise-address localhost:7271 `
  --latitude 10.762622 `
  --longitude 106.660172 `
  --force
```

This calls `bootstrap.InitWorker` and writes `run/wasmcat/worker.env`. The worker expects:

```text
ca.crt
worker-worker-vn-01.crt
worker-worker-vn-01.key
```

inside `CERT_DIR`. In this local setup, those files already exist because the master init command generated development certificates for `worker-vn-01`.

`--advertise-address localhost:7271` is important for same-machine development. The master stores this address in its registry and later dispatches execution requests to `https://localhost:7271/invoke`.

## 3. Booting the Nodes

Open **two terminals** from the repository root.

### Terminal 1: Start the Master

Load `master.env` into the current PowerShell process:

```powershell
Get-Content .\run\wasmcat\master.env | ForEach-Object {
  if ($_ -and -not $_.StartsWith("#")) {
    $name, $value = $_ -split "=", 2
    Set-Item -Path "Env:$name" -Value $value
  }
}
```

Start the master:

```powershell
.\dist\wasmcat-master-windows-amd64.exe
```

Expected log messages include:

```text
initializing control plane
state registry initialized
capacity-aware scheduler initialized
durable job store initialized
execution dispatcher initialized
automatic certificate generation disabled
background reaper started
job recovery loop started
master gateway live
```

The master listens on HTTPS port `7270` and requires mTLS for every route.

### Terminal 2: Start the Worker

Load `worker.env`:

```powershell
Get-Content .\run\wasmcat\worker.env | ForEach-Object {
  if ($_ -and -not $_.StartsWith("#")) {
    $name, $value = $_ -split "=", 2
    Set-Item -Path "Env:$name" -Value $value
  }
}
```

Start the worker:

```powershell
.\dist\wasmcat-worker-windows-amd64.exe
```

Expected log messages include:

```text
starting telemetry pulse
worker server live
```

On the master terminal, you should also see logs for worker registration and heartbeat updates. The worker periodically calls:

```text
POST /internal/register
POST /internal/heartbeat
```

over mTLS.

### Health Checks

Because the servers require mTLS, even health checks need a trusted client certificate:

```powershell
curl.exe `
  --cacert .\run\wasmcat\certs\ca.crt `
  --cert .\run\wasmcat\certs\master.crt `
  --key .\run\wasmcat\certs\master.key `
  https://localhost:7270/wasmcat/health
```

Expected response:

```json
{
  "status": "ok",
  "role": "master"
}
```

## 4. Executing a Workload: The Happy Path

The master execution endpoint is:

```text
POST /api/v1/execute
```

The request body must include:

- `module_name`: logical module name.
- `module_url` or `module_registry_url`: HTTP(S) or ACR source for the `.wasm` bytes.
- `payload`: string input passed into WASM memory.
- `user_lat` and `user_lon`: user coordinates used by the Haversine scheduler.
- optional `request_id`: idempotency and tracing key.
- optional `module_digest`: `sha256:<hex>` digest for module integrity.

### ACR Manifest Request Example

Create `request.json`:

```json
{
  "request_id": "demo:echo:001",
  "module_name": "echo",
  "module_registry_url": "https://myregistry.azurecr.io/v2/team/echo/manifests/latest",
  "user_lat": 10.762622,
  "user_lon": 106.660172,
  "payload": "hello from wasmCat"
}
```

Send the request with mTLS:

```powershell
curl.exe `
  --cacert .\run\wasmcat\certs\ca.crt `
  --cert .\run\wasmcat\certs\master.crt `
  --key .\run\wasmcat\certs\master.key `
  -H "Content-Type: application/json" `
  --data-binary "@request.json" `
  https://localhost:7270/api/v1/execute
```

For local development, the generated `master.crt` is acceptable as a client certificate because `EXECUTE_CLIENT_ALLOWLIST` is empty by default. In production, use a dedicated execution-client certificate and configure `EXECUTE_CLIENT_ALLOWLIST`.

### Expected Response

A successful response has this shape:

```json
{
  "request_id": "demo:echo:001",
  "result": "hello from wasmCat",
  "execution_time_ms": 1.23,
  "executed_on_node_id": "worker-vn-01"
}
```

The exact `result` depends on the WASM module. The module must export:

```text
memory
malloc(size uint32) uint32
run(ptr uint32, len uint32) uint64
```

### What Happens in the Background

1. The master validates JSON, body size, module source policy, and `request_id`.
2. The master creates or reuses a durable SQLite job record.
3. The dispatcher detects the ACR URL because the host ends with `.azurecr.io`.
4. The master uses Azure `DefaultAzureCredential` to mint a repository-scoped ACR pull token.
5. If the URL points to a manifest, the master fetches the OCI manifest and selects a WASM layer.
6. The request is rewritten to a blob URL and forwarded to the selected worker.
7. The scheduler filters workers by CPU/RAM thresholds and selects the nearest worker by Haversine distance.
8. The master calls `https://localhost:7271/invoke` over mTLS.
9. The worker downloads the WASM bytes directly into memory, verifies digest when present, compiles with `wazero`, and caches the compiled module.
10. The worker writes `payload` into WASM linear memory using `malloc`, calls `run(ptr, len)`, reads the returned pointer/length, and returns the output.
11. The worker reports completion to `POST /internal/jobs/complete`.
12. The master stores the response in SQLite and returns the JSON response to the client.

### Direct Module URL Variant

If you are not using ACR, use `module_url` instead:

```json
{
  "request_id": "demo:direct:001",
  "module_name": "echo",
  "module_url": "https://modules.example.com/echo.wasm",
  "user_lat": 10.762622,
  "user_lon": 106.660172,
  "payload": "hello direct module"
}
```

## 5. Verifying High Availability

wasmCat currently provides **single-master durable job reliability**, not true multi-master HA.

### Check Durable Job State

After an execution, query the durable job:

```powershell
curl.exe `
  --cacert .\run\wasmcat\certs\ca.crt `
  --cert .\run\wasmcat\certs\master.crt `
  --key .\run\wasmcat\certs\master.key `
  https://localhost:7270/api/v1/jobs/demo:echo:001
```

Expected response shape:

```json
{
  "request_id": "demo:echo:001",
  "status": "succeeded",
  "worker_id": "worker-vn-01",
  "attempt": 1,
  "max_attempts": 3,
  "response": {
    "request_id": "demo:echo:001",
    "result": "hello from wasmCat",
    "execution_time_ms": 1.23,
    "executed_on_node_id": "worker-vn-01"
  },
  "created_at": "2026-06-15T00:00:00Z",
  "updated_at": "2026-06-15T00:00:00Z"
}
```

### Restart the Master

1. Stop the master process with `Ctrl+C`.
2. Start it again with the same `master.env`.
3. Query the same job ID again.

Because `JOB_STORE_PATH` points to `run/wasmcat/wasmcat-jobs.db`, completed job records survive master restart.

### Worker Failure Test

1. Stop the worker process with `Ctrl+C`.
2. Submit a new execution request.
3. The master should eventually return a dispatch failure such as no schedulable workers.
4. Restart the worker.
5. Watch the master logs for registration and heartbeat messages.

For async jobs submitted through `POST /api/v1/jobs`, queued jobs are recovered by the master recovery loop every `JOB_RECOVERY_INTERVAL` (`5s` by default).

## 6. Troubleshooting

### 1. `x509: certificate signed by unknown authority`

Cause: the client or node is not using the generated CA, or `CERT_DIR` points at the wrong directory.

Fix:

```powershell
$env:CERT_DIR = ".\run\wasmcat\certs"
```

For `curl`, always include:

```powershell
--cacert .\run\wasmcat\certs\ca.crt
```

If you regenerated certificates, rerun both init commands with `--force` and restart both nodes.

### 2. `worker_identity_mismatch`

Cause: the worker claims one `WORKER_ID`, but the certificate name belongs to another worker. The master expects the worker certificate common name to match:

```text
wasmcat-worker-<WORKER_ID>
```

Fix: keep these aligned:

```text
WORKER_ID=worker-vn-01
worker-worker-vn-01.crt
worker-worker-vn-01.key
```

If needed, regenerate dev certs:

```powershell
.\dist\wasmcat-master-windows-amd64.exe init `
  --config-dir .\run\wasmcat `
  --cert-dir .\run\wasmcat\certs `
  --dev-certs `
  --dev-worker-id worker-vn-01 `
  --force
```

### 3. `no schedulable workers available` or `no active workers available`

Cause: the worker has not registered, the heartbeat stopped, the worker is draining, or the master cannot reach `WORKER_ADVERTISE_ADDRESS`.

Fix:

1. Confirm the worker is running.
2. Confirm worker config contains:

```text
MASTER_URL=https://localhost:7270
WORKER_ADVERTISE_ADDRESS=localhost:7271
```

3. Check the worker health endpoint with mTLS:

```powershell
curl.exe `
  --cacert .\run\wasmcat\certs\ca.crt `
  --cert .\run\wasmcat\certs\master.crt `
  --key .\run\wasmcat\certs\master.key `
  https://localhost:7271/wasmcat/health
```

4. Restart the worker and watch the master logs for `worker registered`.

### 4. `module_policy_violation`

Cause: the master was started with `MODULE_HOST_ALLOWLIST` or `REQUIRE_MODULE_DIGEST=true`, and the execution request does not satisfy that policy.

Fix:

- Use a module host listed in `MODULE_HOST_ALLOWLIST`; or
- include a valid `module_digest`; or
- relax policy in `master.env` for local testing.

### 5. ACR token or manifest errors

Cause: the master cannot obtain an Azure token, the identity lacks `AcrPull`, the registry URL is malformed, or the manifest has no supported WASM layer.

Fix:

```bash
az login
az acr login --name <registry-name>
```

Then verify the request uses one of these forms:

```text
https://<registry>.azurecr.io/v2/<repository>/manifests/<tag-or-digest>
https://<registry>.azurecr.io/v2/<repository>/blobs/sha256:<digest>
```

For production, prefer managed identity or service principal credentials with `AcrPull`.
