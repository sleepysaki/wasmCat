# wasmCat Technical Documentation

## 1. System Architecture & Overview

### High-Level Purpose

wasmCat is a native master-worker WebAssembly orchestration system. The master process accepts execution requests, tracks worker nodes, chooses a target worker, resolves Azure Container Registry (ACR) module references when needed, and dispatches work over mutual TLS. The worker process registers with the master, receives execution requests, fetches and compiles WASM modules with wazero, writes request payloads into WASM linear memory, invokes the module ABI, and returns the output.

The core responsibility is to execute WASM modules on registered workers without requiring Docker, containerd, Kubernetes, or an external WASM runtime.

### Architectural Pattern

- **Master-worker architecture:** `cmd/master` runs the control plane. `cmd/worker` runs execution nodes.
- **Control plane/data plane split:** Master APIs handle registration, scheduling, and dispatch. Worker APIs handle module invocation.
- **Coordinator pattern:** `master.Dispatcher` coordinates registry lookup, ACR resolution, worker selection, conservative rescheduling, and mTLS forwarding.
- **Strategy-like scheduling:** `master.Scheduler` isolates worker selection logic. The current strategy filters workers below configured CPU/RAM thresholds, then chooses the nearest eligible worker using Haversine distance.
- **Dependency injection:** `Gateway`, `Dispatcher`, and `WorkerServer` receive dependencies as struct fields, which also makes handler tests possible.
- **In-memory registry/cache:** Worker state is held in `Registry.workers`; compiled modules are held in `WasmEngine.cache`. Execution jobs are persisted in SQLite when the master runs with a `JobStore`.
- **Event-loop background tasks:** Master cleanup and worker telemetry run on tickers controlled by cancellation contexts.
- **Native release packaging:** `scripts/build.*`, systemd templates, and GitHub release workflow distribute binaries and service files.

### Control Flow

#### Master runtime lifecycle

1. `cmd/master/main.go` checks whether the first argument is `init`.
2. If `init` is present, `runInit` parses flags and calls `bootstrap.InitMaster`, then exits.
3. Normal startup calls `logging.Configure("master")`.
4. `signal.NotifyContext` creates a root context cancelled by `SIGINT` or `SIGTERM`.
5. `config.LoadMaster` reads master environment variables.
6. The process constructs `Registry`, `Scheduler`, durable SQLite `JobStore`, `Dispatcher`, and `Gateway`.
7. If `AUTO_GENERATE_CERTS=true`, `security.GenerateCAAndCerts` writes a local CA, master cert, and worker cert.
8. A cleanup goroutine calls `Registry.Cleanup` on `CLEANUP_INTERVAL`; `WORKER_STALE_TIMEOUT` decides which workers are old enough to remove.
9. A recovery goroutine scans durable jobs on `JOB_RECOVERY_INTERVAL`, dispatches queued jobs, and marks expired active leases ambiguous.
10. `Gateway.Start` loads CA/master certificates, creates an HTTPS server requiring client certificates, and blocks until shutdown or server error.

#### Worker runtime lifecycle

1. `cmd/worker/main.go` checks whether the first argument is `init`.
2. If `init` is present, `runInit` parses flags and calls `bootstrap.InitWorker`, then exits.
3. Normal startup calls `logging.Configure("worker")`.
4. `signal.NotifyContext` creates a cancellable root context.
5. `config.LoadWorker` reads worker identity, master URL, cert path, heartbeat interval, and execution limits. When `WORKER_ADVERTISE_ADDRESS` is unset, it defaults to `<hostname>:<worker-port>` and falls back to `localhost:<worker-port>` only if the OS hostname cannot be read.
6. `worker.NewWasmEngineWithLimits` creates a wazero runtime, digest-aware compiled module cache, mutex, in-flight compile tracker, and execution semaphore.
7. `WorkerServer` is constructed with the engine, node ID, and certificate directory.
8. `StartTelemetry` runs in a goroutine, registering the worker with configured latitude/longitude and sending periodic CPU/RAM heartbeats over mTLS.
9. `WorkerServer.Start` loads CA/worker certificates, starts an HTTPS server requiring client certificates, and blocks until shutdown or server error.

#### Execution request lifecycle

1. Client sends JSON to `POST /api/v1/execute` on the master.
2. `Gateway.handleExecute` decodes `shared.ExecutionRequest`, calls `ExecutionRequest.Validate`, applies module source policy, and preserves or creates `request_id`.
3. The gateway persists a durable job when `JobStore` is configured. New IDs create `queued` jobs, matching completed IDs return stored responses, active IDs return `409 request_in_progress`, and reused IDs with different request content return `409 request_id_conflict`. Handler-only paths without a job store fall back to `ExecutionRequestTracker`.
4. `Dispatcher.Dispatch` normalizes module URL fields.
5. If the URL points at `*.azurecr.io`, the dispatcher:
   - parses the ACR reference,
   - gets a repository-scoped ACR bearer token from the master token cache,
   - fetches the OCI manifest for manifest URLs,
   - selects a WASM layer,
   - rewrites the request to the layer blob URL.
6. The dispatcher reads schedulable workers from `Registry.GetSchedulableWorkers`.
7. `Scheduler.SelectWorker` filters out workers below configured free CPU/RAM thresholds, then chooses the nearest remaining worker by latitude/longitude.
8. `Dispatcher.forwardToWorker` sends the request, including `request_id`, to `https://<worker>/invoke` over mTLS. If the selected worker has a transport error or returns `502`, `503`, or `504`, the dispatcher removes that worker from the candidate list and selects another worker. It does not reschedule worker execution errors such as `400 execution_failed`, `400 module_digest_invalid`, or `400 module_digest_mismatch`.
9. `WorkerServer.handleInvoke` limits request body size, decodes and validates the request, then calls `WasmEngine.ExecuteWithDigest`.
10. `WasmEngine.ExecuteWithDigest` enforces payload and concurrency limits, fetches/compiles the module if needed, verifies `module_digest` when supplied, instantiates a fresh module instance, writes payload bytes into WASM memory, calls exported `run(ptr, len)`, reads output bytes, and returns a string.
11. Worker returns `shared.ExecutionResponse` with `request_id` and `execution_time_ms`; master fills `ExecutedOnNodeID` if needed, stores successful responses in the durable job store or request tracker, and returns the response to the client.

## 2. Component & Module Breakdown

### `cmd/master/main.go`

- **Name & Responsibility:** Master process entrypoint. Handles `wasmcat-master init` and normal control-plane startup.
- **State & Properties:** Local references to `MasterConfig`, `Registry`, `Scheduler`, `Dispatcher`, and `Gateway`; process root context; cleanup ticker.
- **Interactions:** Calls `bootstrap.InitMaster`, `config.LoadMaster`, `security.GenerateCAAndCerts`, `master.NewRegistryWithStaleTimeout`, `Gateway.Start`.

### `cmd/worker/main.go`

- **Name & Responsibility:** Worker process entrypoint. Handles `wasmcat-worker init` and normal worker runtime startup.
- **State & Properties:** Local references to `WorkerConfig`, `WasmEngine`, `WorkerServer`; process root context.
- **Interactions:** Calls `bootstrap.InitWorker`, `config.LoadWorker`, `worker.NewWasmEngineWithLimits`, `worker.StartTelemetry`, `WorkerServer.Start`.

### `internal/bootstrap/bootstrap.go`

- **Name & Responsibility:** Creates node configuration files and directories for native installation.
- **State & Properties:** `MasterOptions`, `WorkerOptions`, and `Result` structs carry input flags and generated output locations.
- **Interactions:** Writes `master.env` and `worker.env`; optionally calls `security.GenerateCAAndCerts`; uses `security.*Path` helpers for certificate naming.

### `internal/config/config.go`

- **Name & Responsibility:** Reads process configuration from environment variables and converts string inputs into typed values, including worker location and scheduler capacity thresholds.
- **State & Properties:** `MasterConfig`, `WorkerConfig`, and `Limits` hold runtime settings.
- **Interactions:** Used by both entrypoints before creating runtime components.

### `internal/logging/logging.go`

- **Name & Responsibility:** Configures JSON structured logging and logs HTTP request metadata.
- **State & Properties:** `statusRecorder` wraps `http.ResponseWriter` and records final response status.
- **Interactions:** `cmd/master` and `cmd/worker` set default `slog` loggers. HTTP servers wrap muxes with `logging.Middleware`.

### `internal/security/mtls.go`

- **Name & Responsibility:** Generates local development certificates and creates mTLS HTTP clients.
- **State & Properties:** No long-lived package state. Certificate files are written to the configured cert directory.
- **Interactions:** Master and worker servers load certificate paths from this package. Dispatcher and telemetry clients use mTLS client configuration.

### `internal/shared/models.go`

- **Name & Responsibility:** Defines JSON models shared by master and worker.
- **State & Properties:** `WorkerNode`, `Heartbeat`, `DrainRequest`, worker state constants, `ExecutionRequest`, `ExecutionResponse`, request ID helpers, `APIResponse`, `ErrorResponse`, `HealthResponse`, and metrics response models.
- **Interactions:** All HTTP request/response handlers use these models.

### `internal/shared/http.go`

- **Name & Responsibility:** Standardizes JSON and error responses.
- **State & Properties:** No internal state.
- **Interactions:** Master and worker handlers call `WriteJSON` and `WriteError`. Error responses use safe public messages while raw errors are logged.

### `internal/shared/metrics.go`

- **Name & Responsibility:** Tracks process-local request, dispatch, execution, cache, and worker metrics.
- **State & Properties:** Mutex-protected counters, start time, request counts by status/path, dispatch success/failure counters, dispatch reschedule counters, request idempotency counters, worker execution counters, and worker module digest failure counters.
- **Interactions:** Logging middleware records request metrics; dispatcher records conservative reschedules; gateway records request idempotency outcomes; master and worker metrics endpoints expose snapshots.

### `internal/shared/client.go`

- **Name & Responsibility:** Creates outbound HTTP clients/transports with bounded timeouts and provides retry helpers for repeatable HTTP calls.
- **State & Properties:** Timeout and connection-pool constants for dial, TLS handshake, response headers, full request duration, idle connections, and idle pool size. Retry constants define 3 attempts and 100ms linear backoff.
- **Interactions:** Worker module fetches, ACR manifest/token calls, dispatcher mTLS clients, and telemetry mTLS clients use this transport policy. ACR calls, module fetches, and telemetry use `DoWithRetry`; dispatcher `/invoke` does not repeat the same worker call, but may reschedule to another worker for transport errors or `502`/`503`/`504`.

### `internal/shared/server.go`

- **Name & Responsibility:** Creates inbound HTTP servers with bounded read-header, read, write, and idle timeouts.
- **State & Properties:** Server timeout constants: 5s read-header, 30s read, 30s write, and 60s idle.
- **Interactions:** Master gateway and worker invoke server use this constructor before starting TLS listeners.

### `internal/master/gateway.go`

- **Name & Responsibility:** Master HTTPS API server and request routing.
- **State & Properties:** `Gateway.Registry`, `Gateway.Dispatcher`, `Gateway.CertDir`, `Gateway.Metrics`, optional `Gateway.Requests`, optional durable `Gateway.JobStore`, optional `Gateway.ExecuteClientIDs`, module source policy, request body limit, request cache settings, job retry/lease settings, and graceful shutdown timeout.
- **Interactions:** Updates `Registry`, validates worker certificate identity during registration/heartbeat/drain, validates execution client certificate identity when configured, checks durable or in-memory request idempotency state, calls `Dispatcher.Dispatch`, serves mTLS-protected endpoints.

### `internal/master/request_tracker.go`

- **Name & Responsibility:** Provides process-local idempotency tracking for `/api/v1/execute` requests.
- **State & Properties:** Mutex-protected map keyed by `request_id`, request fingerprints, in-flight/completed state, cached successful `ExecutionResponse`, TTL, max entry count, eviction/expiration counters, and clock function.
- **Interactions:** Handler-only and no-store gateway paths call `Begin`, `Complete`, and `Forget` to reject concurrent duplicates, return cached successes, detect request ID conflicts, and allow retries after dispatch failures. Normal master startup uses the durable job store path.

### `internal/master/job.go`

- **Name & Responsibility:** Defines durable execution job records and state names.
- **State & Properties:** `JobRecord` stores request ID, execution fingerprint, original request, status, worker ID, attempt count, max attempts, lease time, last error, optional response, and timestamps.
- **Interactions:** `Gateway.handleExecute` creates records before dispatch; `JobStore` implementations persist and transition records.

### `internal/master/job_store.go`

- **Name & Responsibility:** Defines the persistence boundary for durable execution jobs.
- **State & Properties:** `JobStore` interface and sentinel errors for missing/existing jobs.
- **Interactions:** Gateway depends on this interface rather than a concrete database. SQLite is the first implementation.

### `internal/master/sqlite_job_store.go`

- **Name & Responsibility:** Persists durable jobs in a local SQLite database.
- **State & Properties:** `database/sql.DB`, jobs table, WAL journal mode, busy timeout, JSON request/response columns, state/lease/attempt fields, and recoverable-job index.
- **Interactions:** `cmd/master` opens this store from `JOB_STORE_PATH`; `Gateway` creates jobs, marks dispatching, stores successes, stores failures, and replays completed responses.

### `internal/master/job_recovery.go`

- **Name & Responsibility:** Recovers durable jobs after master restart or after dispatch leases expire.
- **State & Properties:** `JobStore`, dispatch function, optional metrics collector, recovery interval, batch size, lease TTL, and injectable clock for tests.
- **Interactions:** `cmd/master` starts it as a goroutine. It lists recoverable jobs, dispatches `queued` jobs, requeues recoverable dispatch failures below max attempts, fails jobs at max attempts, and marks expired `dispatching`/`running` jobs as `ambiguous`.

### `internal/master/registry.go`

- **Name & Responsibility:** In-memory worker registry.
- **State & Properties:** `workers map[string]shared.WorkerNode` protected by `sync.RWMutex`; worker state is `ready` or `draining`.
- **Interactions:** Gateway registration/heartbeat/drain handlers mutate it; dispatcher reads schedulable workers; master cleanup goroutine removes stale entries.

### `internal/master/scheduler.go`

- **Name & Responsibility:** Selects a worker for execution by state, capacity, and distance.
- **State & Properties:** `MinCPUFree`, `MinRAMFreeMB`; `EarthRadius` constant for Haversine distance.
- **Interactions:** Dispatcher calls `Scheduler.SelectWorker`.

### `internal/master/dispatcher.go`

- **Name & Responsibility:** Coordinates execution dispatch from master to selected worker, including conservative alternate-worker rescheduling for transient selected-worker failures.
- **State & Properties:** `Registry`, `Scheduler`, optional injected `Client`, `CertDir`, and optional shared `Metrics` collector.
- **Interactions:** Reads registry, calls scheduler, calls ACR helpers, creates mTLS client, forwards to worker `/invoke`, and removes failed retryable candidates before selecting another worker.

### `internal/master/acr_manifest.go`

- **Name & Responsibility:** Parses ACR module references, fetches OCI manifests, selects WASM layers, and builds blob URLs.
- **State & Properties:** `acrReference`, `ociManifest`, `ociLayer` model registry references and manifest data.
- **Interactions:** `Dispatcher.Dispatch` uses this package before forwarding ACR-backed modules to workers.

### `internal/master/acr_token.go`

- **Name & Responsibility:** Provides repository-scoped ACR pull tokens using an in-memory TTL cache backed by Azure identity and ACR OAuth exchange endpoints.
- **State & Properties:** `ACRTokenProvider` stores cached tokens by `<registry>/<repository>`, expiry timestamps, a clock function, and a mint function. Response structs decode ACR refresh and access token JSON, including `expires_in`.
- **Interactions:** `Dispatcher.Dispatch` calls `GenerateACRToken` for ACR URLs.

### `internal/worker/server.go`

- **Name & Responsibility:** Worker HTTPS API server and invocation handler.
- **State & Properties:** `WorkerServer.Engine`, `WorkerServer.NodeID`, `WorkerServer.CertDir`, and `WorkerServer.Metrics`.
- **Interactions:** Calls `WasmEngine.Execute`; serves health/readiness/metrics; uses mTLS server config.

### `internal/worker/engine.go`

- **Name & Responsibility:** Fetches, compiles, caches, instantiates, and executes WASM modules.
- **State & Properties:** `wazero.Runtime`, digest-aware compiled module cache entries, cache byte counter, in-flight compile tracker, `sync.RWMutex`, `Limits`, and semaphore channel for concurrency.
- **Interactions:** Worker invoke handler calls `ExecuteWithDigest`; `FetchAndCacheWithDigest` downloads modules and compiles with wazero; memory helpers manage WASM memory.

### `internal/worker/memory.go`

- **Name & Responsibility:** Implements host-side memory operations for the project WASM ABI.
- **State & Properties:** No package state.
- **Interactions:** `WasmEngine.Execute` calls `WriteString` and `ReadString`; `WriteString` calls `Allocate`.

### `internal/worker/limits.go`

- **Name & Responsibility:** Defines worker-side resource limits and normalizes partial limit structs.
- **State & Properties:** `DefaultLimits` and `Limits`, including execution, module fetch, graceful shutdown, size, concurrency, and cache lifecycle limits.
- **Interactions:** Config loading and worker engine creation use these values.

### `internal/worker/telemetry.go`

- **Name & Responsibility:** Registers workers with the master and sends heartbeat updates containing live CPU/RAM metrics.
- **State & Properties:** Local ticker, mTLS HTTP client, configured worker coordinates, and `MetricsProvider`.
- **Interactions:** Calls master `/internal/register` and `/internal/heartbeat` endpoints.

### Packaging, CI, and release files

- **`scripts/build.sh` / `scripts/build.ps1`:** Cross-compile native master/worker binaries, copy systemd/env templates, and write `checksums.txt`.
- **`.github/workflows/ci.yml`:** Pull request and main branch quality gates.
- **`.github/workflows/release.yml`:** Tag-triggered native release publishing.
- **`packaging/systemd/*`:** Service and env templates for Linux hosts.
- **`tests/*`:** Go tests live outside production package directories and cover config, bootstrap, gateway, dispatcher, shared HTTP clients, metrics, worker server, limits, cache lifecycle, engine behavior, an in-memory master-to-worker execution path, and a real binary process smoke test.

## 3. Comprehensive API & Function Reference

### Entrypoints

#### `cmd/master/main.go`

##### `func main()`

- **Parameters:** None.
- **Return Values:** None.
- **Error Handling:** On `init` errors, writes `init failed: ...` to stderr and exits with status 1. On runtime startup errors, logs with `slog.Error` and returns from `main`.
- **Side Effects:** Reads environment variables, may write local cert files when `AUTO_GENERATE_CERTS=true`, starts HTTPS server, starts registry cleanup goroutine, handles OS signals.

##### `func runInit(args []string) error`

- **Parameters:** `args []string` are CLI flags after `wasmcat-master init`.
- **Return Values:** `error` only. Nil means config/cert directories and `master.env` were created successfully.
- **Error Handling:** Returns flag parse errors and bootstrap validation/write errors.
- **Side Effects:** Writes status lines to stdout through caller, creates directories/files through `bootstrap.InitMaster`.
- **Flags:** `--config-dir`, `--cert-dir`, `--port`, `--cleanup-interval`, `--worker-stale-timeout`, `--master-shutdown-timeout`, `--execute-client-allowlist`, `--max-execution-request-bytes`, `--execution-request-cache-ttl`, `--execution-request-cache-max-entries`, `--job-store-path`, `--job-max-attempts`, `--job-lease-ttl`, `--job-recovery-interval`, `--job-recovery-batch-size`, `--module-host-allowlist`, `--require-module-digest`, `--dev-worker-id`, `--dev-certs`, `--force`.

##### `func defaultConfigDir() string`

- **Parameters:** None.
- **Return Values:** `C:\wasmcat` on Windows, `/etc/wasmcat` elsewhere.
- **Error Handling:** None.
- **Side Effects:** None.

#### `cmd/worker/main.go`

##### `func main()`

- **Parameters:** None.
- **Return Values:** None.
- **Error Handling:** On `init` errors, writes to stderr and exits with status 1. Runtime errors are logged.
- **Side Effects:** Reads environment variables, creates wazero runtime, starts telemetry goroutine, starts HTTPS worker server, handles OS signals.

##### `func runInit(args []string) error`

- **Parameters:** `args []string` are CLI flags after `wasmcat-worker init`.
- **Return Values:** `error` only.
- **Error Handling:** Returns flag parse errors, `--max-output-bytes` overflow, and bootstrap validation/write errors.
- **Side Effects:** Creates directories and `worker.env`.
- **Flags:** `--config-dir`, `--cert-dir`, `--worker-id`, `--port`, `--master-url`, `--advertise-address`, `--heartbeat-interval`, `--execution-timeout`, `--module-fetch-timeout`, `--worker-shutdown-timeout`, `--max-module-bytes`, `--max-payload-bytes`, `--max-output-bytes`, `--max-concurrent-execs`, `--force`.

##### `func defaultConfigDir() string`

- Same behavior as master entrypoint.

### Bootstrap package

##### `func InitMaster(options MasterOptions) (Result, error)`

- **Parameters:** `MasterOptions`:
  - `ConfigDir string`: directory where `master.env` is written. Required after normalization.
  - `CertDir string`: directory for certificates. Defaults to `<ConfigDir>/certs`.
  - `Port string`: master HTTPS port. Defaults to `7270`.
  - `CleanupInterval string`: duration string for registry cleanup ticker. Defaults to `15s`.
  - `WorkerStaleTimeout string`: duration string for how long a worker may miss heartbeats before cleanup removes it. Defaults to `30s`.
  - `ShutdownTimeout string`: duration string for graceful master HTTP shutdown. Defaults to `5s`.
  - `ModuleHostAllowlist string`: comma-separated allowed module URL hosts.
  - `RequireModuleDigest bool`: requires digest-pinned module requests when true.
  - `DevWorkerID string`: worker ID used when generating local dev certs. Defaults to `worker-vn-01`.
  - `GenerateDevCerts bool`: when true, writes local CA/master/worker certs.
  - `Force bool`: allows overwriting existing env or generated dev cert files.
- **Return Values:** `Result{ConfigPath, CertDir, Warnings}`.
- **Error Handling:** Returns errors for missing dirs, invalid duration, existing files without force, certificate generation failures, and file write failures.
- **Side Effects:** Creates directories, writes `master.env`, optionally writes `ca.crt`, `ca.key`, `master.crt`, `master.key`, `worker-<id>.crt`, `worker-<id>.key`.

##### `func InitWorker(options WorkerOptions) (Result, error)`

- **Parameters:** `WorkerOptions`:
  - `ConfigDir`, `CertDir`, `WorkerID`, `Port`, `MasterURL`, `AdvertiseAddress`.
  - Duration strings: `HeartbeatInterval`, `ExecutionTimeout`, `ModuleFetchTimeout`, `ShutdownTimeout`.
  - Numeric limits: `MaxModuleBytes`, `MaxPayloadBytes`, `MaxOutputBytes`, `MaxConcurrentExecs`.
  - `Force bool`.
- **Return Values:** `Result` pointing at `worker.env` and cert dir.
- **Error Handling:** Returns errors for missing config, invalid HTTPS master URL, invalid duration strings, zero/negative limits, existing env file without force, and write failures.
- **Side Effects:** Creates directories and writes `worker.env`.

##### `func normalizeMaster(options MasterOptions) MasterOptions`

- **Parameters:** Partial master options.
- **Return Values:** Options with defaults applied.
- **Error Handling:** None.
- **Side Effects:** None.

##### `func normalizeWorker(options WorkerOptions) WorkerOptions`

- **Parameters:** Partial worker options.
- **Return Values:** Options with string defaults applied. Numeric limits must be supplied by the CLI or caller.
- **Error Handling:** None.
- **Side Effects:** None.

##### `func validateMaster(options MasterOptions) error`

- **Parameters:** Normalized master options.
- **Return Values:** Nil on valid input.
- **Error Handling:** Missing config dir, cert dir, port, dev worker ID, invalid or non-positive cleanup interval, worker stale timeout, or master shutdown timeout; CPU threshold outside `0..100`; negative RAM threshold; non-positive execution request body limit.
- **Side Effects:** None.

##### `func validateWorker(options WorkerOptions) error`

- **Parameters:** Normalized worker options.
- **Return Values:** Nil on valid input.
- **Error Handling:** Missing required strings, non-HTTPS master URL, invalid or non-positive duration strings, non-positive numeric limits.
- **Side Effects:** None.

##### `func validateURL(value string) error`

- **Parameters:** URL string.
- **Return Values:** Nil if URL has `https` scheme and host.
- **Error Handling:** URL parse errors or non-HTTPS/missing-host errors.
- **Side Effects:** None.

##### `func ensureDevCertTargetsCanBeWritten(certDir string, workerID string, force bool) error`

- **Parameters:** Certificate directory, worker ID, overwrite flag.
- **Return Values:** Nil if cert generation may proceed.
- **Error Handling:** Existing target files without `force`; filesystem stat failures.
- **Side Effects:** Reads filesystem metadata.

##### `func writeEnvFile(path string, values []envValue, force bool) error`

- **Parameters:** Output path, name/value pairs, overwrite flag.
- **Return Values:** Nil on successful write.
- **Error Handling:** Existing file without force, stat errors, write errors.
- **Side Effects:** Writes an env file with `0644` permissions.

### Config package

##### `func LoadMaster() (MasterConfig, error)`

- **Parameters:** None.
- **Return Values:** `MasterConfig` with `Port`, `CertDir`, `AutoGenerateCerts`, `WorkerIDForCert`, `CleanupInterval`, `WorkerStaleTimeout`, `ShutdownTimeout`, `MinWorkerCPUFree`, `MinWorkerRAMFreeMB`, job store settings, module source policy, and client authorization.
- **Error Handling:** Invalid or non-positive `CLEANUP_INTERVAL`, `WORKER_STALE_TIMEOUT`, `MASTER_SHUTDOWN_TIMEOUT`, `EXECUTION_REQUEST_CACHE_TTL`, `EXECUTION_REQUEST_CACHE_MAX_ENTRIES`, `JOB_MAX_ATTEMPTS`, `JOB_LEASE_TTL`, `JOB_RECOVERY_INTERVAL`, `JOB_RECOVERY_BATCH_SIZE`, or `MAX_EXECUTION_REQUEST_BYTES`; invalid `AUTO_GENERATE_CERTS` or `REQUIRE_MODULE_DIGEST`; `MIN_WORKER_CPU_FREE` outside `0..100`; negative `MIN_WORKER_RAM_FREE_MB`.
- **Side Effects:** Reads process environment.

##### `func LoadWorker() (WorkerConfig, error)`

- **Parameters:** None.
- **Return Values:** `WorkerConfig` with port, node ID, master URL, advertise address, worker coordinates, cert dir, heartbeat interval, and limits.
- **Error Handling:** Invalid duration, coordinate, or numeric limit environment variables.
- **Side Effects:** Reads process environment.

##### `func loadWorkerLimits() (Limits, error)`

- **Parameters:** None.
- **Return Values:** `Limits`.
- **Error Handling:** Invalid or non-positive `HEARTBEAT_INTERVAL`, `EXECUTION_TIMEOUT`, `MODULE_FETCH_TIMEOUT`, `WORKER_SHUTDOWN_TIMEOUT`, `MODULE_CACHE_TTL`, byte/count limits, payload/output/cache limits, or coordinates.
- **Side Effects:** Reads process environment.

##### `func stringEnv(name string, fallback string) string`

- **Parameters:** Env var name and fallback.
- **Return Values:** Env value or fallback when empty.
- **Error Handling:** None.
- **Side Effects:** Reads process environment.

##### `func durationEnv(name string, fallback time.Duration) (time.Duration, error)`

- **Parameters:** Env var name and fallback.
- **Return Values:** Parsed `time.Duration`.
- **Error Handling:** `time.ParseDuration` failures.
- **Side Effects:** Reads process environment.

##### `func boolEnv(name string, fallback bool) (bool, error)`

- **Parameters:** Env var name and fallback.
- **Return Values:** Parsed boolean.
- **Error Handling:** `strconv.ParseBool` failures.
- **Side Effects:** Reads process environment.

##### `func intEnv(name string, fallback int) (int, error)`

- **Parameters:** Env var name and fallback.
- **Return Values:** Parsed int.
- **Error Handling:** `strconv.Atoi` failures.
- **Side Effects:** Reads process environment.

##### `func int64Env(name string, fallback int64) (int64, error)`

- **Parameters:** Env var name and fallback.
- **Return Values:** Parsed int64.
- **Error Handling:** `strconv.ParseInt` failures.
- **Side Effects:** Reads process environment.

##### `func uint32Env(name string, fallback uint32) (uint32, error)`

- **Parameters:** Env var name and fallback.
- **Return Values:** Parsed uint32.
- **Error Handling:** `strconv.ParseUint` failures or overflow beyond 32 bits.
- **Side Effects:** Reads process environment.

### Security package

##### `func GenerateCAAndCerts(certDir string, workerID string) error`

- **Parameters:** `certDir` output directory; `workerID` used in worker cert filename and common name.
- **Return Values:** Nil on success.
- **Error Handling:** Missing inputs, mkdir failure, RSA generation failure, certificate creation/parse failures, file write failures.
- **Side Effects:** Writes CA private key and certificates. Uses `os.Create`, so existing files are overwritten.

##### `func CACertPath(certDir string) string`

- **Return Values:** `<certDir>/ca.crt`.

##### `func MasterCertPath(certDir string) string`

- **Return Values:** `<certDir>/master.crt`.

##### `func MasterKeyPath(certDir string) string`

- **Return Values:** `<certDir>/master.key`.

##### `func WorkerCertPath(certDir string, workerID string) string`

- **Return Values:** `<certDir>/worker-<workerID>.crt`.

##### `func WorkerKeyPath(certDir string, workerID string) string`

- **Return Values:** `<certDir>/worker-<workerID>.key`.

##### `func generateLeafCert(certPath string, keyPath string, caCert *x509.Certificate, caKey *rsa.PrivateKey, commonName string, addLocalSANs bool) error`

- **Parameters:** Output cert/key paths, CA cert/key, leaf common name, SAN toggle.
- **Return Values:** Nil on success.
- **Error Handling:** RSA generation, x509 creation, and file write failures.
- **Side Effects:** Writes certificate and key files.

##### `func writeCertificate(path string, derBytes []byte) error`

- **Parameters:** Output path and DER certificate bytes.
- **Return Values:** Nil on successful PEM write.
- **Error Handling:** File create or PEM encode failures.
- **Side Effects:** Creates or truncates certificate file.

##### `func writePrivateKey(path string, key *rsa.PrivateKey) error`

- **Parameters:** Output path and RSA private key.
- **Return Values:** Nil on successful PEM write.
- **Error Handling:** File create or PEM encode failures.
- **Side Effects:** Creates or truncates key file.

##### `func randomSerialNumber() *big.Int`

- **Return Values:** Random 128-bit serial number.
- **Error Handling:** Panics if crypto random generation fails.
- **Side Effects:** Reads from cryptographic randomness.

##### `func NewMTLSHTTPClient(certFile string, keyFile string, caFile string) (*http.Client, error)`

- **Parameters:** Client cert path, client key path, CA cert path.
- **Return Values:** HTTP client with TLS client cert, root CA pool, TLS 1.2 minimum, and shared timeout/transport defaults.
- **Error Handling:** Cert/key load failures, CA read failures, CA parse failures.
- **Side Effects:** Reads certificate files.

##### `func NewHTTPClient() *http.Client`

- **Return Values:** HTTP client with the default wasmCat timeout and connection-pool policy.
- **Side Effects:** None.

##### `func NewHTTPClientWithTLSConfig(tlsConfig *tls.Config) *http.Client`

- **Parameters:** Optional TLS client configuration.
- **Return Values:** HTTP client with the provided TLS config and default timeout policy.
- **Side Effects:** None.

##### `func NewHTTPTransport(tlsConfig *tls.Config) *http.Transport`

- **Parameters:** Optional TLS client configuration.
- **Return Values:** Transport with proxy support, dial timeout, keep-alive, TLS handshake timeout, response-header timeout, idle timeout, and idle connection limits.
- **Side Effects:** None.

##### `func DefaultHTTPRetryPolicy() RetryPolicy`

- **Return Values:** Retry policy with 3 attempts and 100ms base backoff.
- **Side Effects:** None.

##### `func DoWithRetry(client *http.Client, req *http.Request) (*http.Response, error)`

- **Parameters:** HTTP client and request.
- **Return Values:** First successful or non-retryable response, final retryable response after attempts are exhausted, or transport error.
- **Error Handling:** Retries transport errors unless the request context is done; retries `408`, `429`, `500`, `502`, `503`, and `504`.
- **Side Effects:** Sends outbound HTTP requests. Retries may send the same request multiple times if the body can be replayed.

##### `func DoWithRetryPolicy(client *http.Client, req *http.Request, policy RetryPolicy) (*http.Response, error)`

- **Parameters:** HTTP client, request, and explicit retry policy.
- **Return Values:** Same as `DoWithRetry`.
- **Error Handling:** Uses one attempt when policy attempts are zero or negative; returns an error when a retry needs a request body that cannot be recreated.
- **Side Effects:** Sends outbound HTTP requests and drains retryable response bodies before the next attempt.

##### `func NewHTTPServer(addr string, handler http.Handler, tlsConfig *tls.Config) *http.Server`

- **Parameters:** Listen address, HTTP handler, optional TLS config.
- **Return Values:** HTTP server with bounded read-header, read, write, and idle timeouts.
- **Side Effects:** None until the server is started.

### Logging package

##### `func Configure(component string) *slog.Logger`

- **Parameters:** Component name added to all log records.
- **Return Values:** Configured JSON `slog.Logger`.
- **Error Handling:** None.
- **Side Effects:** Sets global default `slog` logger and writes future logs to stdout.

##### `func Middleware(component string, next http.Handler) http.Handler`

- **Parameters:** Component label and downstream handler.
- **Return Values:** HTTP handler wrapper.
- **Error Handling:** Does not recover panics.
- **Side Effects:** Logs method, path, status, and duration after each request.

##### `func (r *statusRecorder) WriteHeader(status int)`

- **Parameters:** HTTP status code.
- **Return Values:** None.
- **Side Effects:** Records status and forwards header write.

### Shared package

##### `func (r ExecutionRequest) Validate() error`

- **Parameters:** Receiver containing optional `RequestID`, `ModuleName`, `ModuleURL`, `ModuleRegistryURL`, and optional `ModuleDigest`.
- **Return Values:** Nil when `RequestID` is valid, `ModuleName` is set, and either module URL field is present.
- **Error Handling:** Returns invalid request ID or missing field errors.
- **Side Effects:** None.

##### `func NewRequestID() (string, error)`

- **Return Values:** Generated `req_<32 hex characters>` request ID.
- **Error Handling:** Returns crypto-random read errors.
- **Side Effects:** Reads from the operating system random source.

##### `func EnsureRequestID(requestID string) (string, error)`

- **Parameters:** Optional client-provided request ID.
- **Return Values:** Trimmed client ID when provided and valid, otherwise a generated ID.
- **Error Handling:** Invalid client ID or random generation errors.
- **Side Effects:** May read from the operating system random source.

##### `func ValidateRequestID(requestID string) error`

- **Parameters:** Optional request ID.
- **Return Values:** Nil for empty IDs or valid IDs up to 128 characters.
- **Error Handling:** Rejects IDs with unsupported characters or excessive length.
- **Side Effects:** None.

##### `func WriteJSON(w http.ResponseWriter, status int, value any)`

- **Parameters:** Response writer, HTTP status, value to encode.
- **Return Values:** None.
- **Error Handling:** If JSON encoding fails after headers are written, calls `http.Error`; status may already be committed.
- **Side Effects:** Writes HTTP headers and response body.

##### `func WriteError(w http.ResponseWriter, status int, code string, err error)`

- **Parameters:** Response writer, HTTP status, machine-readable code, error.
- **Return Values:** None.
- **Error Handling:** Unknown codes use the generic public message `Request failed.`
- **Side Effects:** Logs the internal error with `slog.Warn` and writes a JSON error response with a safe public message.

##### `func RequireMethod(w http.ResponseWriter, r *http.Request, method string) bool`

- **Parameters:** Response writer, incoming request, and required HTTP method.
- **Return Values:** True when the request method matches; false when the handler should stop.
- **Error Handling:** Writes 405 `method_not_allowed` and an `Allow` header when the method is wrong.
- **Side Effects:** May write HTTP headers and JSON error response.

##### `func PublicErrorMessage(code string) string`

- **Parameters:** Machine-readable error code.
- **Return Values:** Safe client-facing message for known codes, or `Request failed.` for unknown codes.
- **Error Handling:** None.
- **Side Effects:** None.

### Master package

##### `func NewGateway(reg *Registry) *Gateway`

- **Parameters:** Registry pointer.
- **Return Values:** Gateway with registry set.
- **Error Handling:** None.
- **Side Effects:** None.

##### `func (g *Gateway) Handler() http.Handler`

- **Return Values:** HTTP handler with master routes wrapped by logging middleware.
- **Routes:** `/wasmcat/health`, `/wasmcat/ready`, `/wasmcat/metrics`, `/internal/register`, `/internal/heartbeat`, `/internal/drain`, `/api/v1/execute`.
- **Side Effects:** None until the returned handler is used.

##### `func (g *Gateway) handleHealth(w http.ResponseWriter, r *http.Request)`

- **Return Values:** JSON `HealthResponse{Status:"ok", Role:"master"}`.
- **Error Handling:** JSON write errors handled by `WriteJSON`.
- **Side Effects:** Writes HTTP response.

##### `func (g *Gateway) handleReady(w http.ResponseWriter, r *http.Request)`

- **Return Values:** 200 ready response if registry, dispatcher, and scheduler exist.
- **Error Handling:** 503 `not_ready` when dependencies are nil.
- **Side Effects:** Writes HTTP response.

##### `func (g *Gateway) handleRegister(w http.ResponseWriter, r *http.Request)`

- **Parameters:** JSON `shared.WorkerNode` in request body.
- **Return Values:** 200 `APIResponse`.
- **Error Handling:** 405 wrong method, 400 invalid JSON, 403 certificate identity mismatch.
- **Side Effects:** Mutates registry.

##### `func (g *Gateway) handleHeartbeat(w http.ResponseWriter, r *http.Request)`

- **Parameters:** JSON `shared.Heartbeat`.
- **Return Values:** 200 empty body on success.
- **Error Handling:** 400 invalid JSON; 404 unknown worker.
- **Side Effects:** Updates registry CPU/RAM/LastSeen.

##### `func (g *Gateway) Start(ctx context.Context, port string) error`

- **Parameters:** Shutdown context and listen port string.
- **Return Values:** Nil on clean shutdown.
- **Error Handling:** CA/cert read errors, TLS server errors, shutdown errors.
- **Side Effects:** Starts timeout-bounded HTTPS server requiring client certificates; on cancellation, uses `Gateway.ShutdownTimeout` or `DefaultMasterShutdownTimeout` as the graceful shutdown window.

##### `func (g *Gateway) handleExecute(w http.ResponseWriter, r *http.Request)`

- **Parameters:** JSON `shared.ExecutionRequest`.
- **Return Values:** 200 `ExecutionResponse` on success.
- **Error Handling:** 405 wrong method, 403 unauthorized execution client, 403 module policy violation, 400 invalid JSON or validation failure, 503 dispatch failure.
- **Side Effects:** Validates execution client certificate identity when configured, validates or creates `request_id`, triggers scheduling, ACR network calls, and worker network call.

##### `func (g *Gateway) validateExecuteClientIdentity(r *http.Request) error`

- **Parameters:** Incoming HTTP request.
- **Return Values:** Nil when `ExecuteClientIDs` is empty or the peer certificate CN/DNS SAN matches an allowed identity.
- **Error Handling:** Returns missing certificate or unauthorized certificate identity errors.
- **Side Effects:** None.

##### `func (g *Gateway) maxExecuteBodyBytes() int64`

- **Return Values:** Configured `MaxExecuteBodyBytes`, or the 2 MiB default when unset.
- **Side Effects:** None.

##### `func limitRequestBody(w http.ResponseWriter, r *http.Request, maxBytes int64)`

- **Parameters:** Response writer, request, and byte limit.
- **Side Effects:** Replaces `r.Body` with `http.MaxBytesReader`.

##### `func isBodyTooLargeError(err error) bool`

- **Parameters:** Decode/read error.
- **Return Values:** True when the error wraps `*http.MaxBytesError`.
- **Side Effects:** None.

##### `func NewRegistry() *Registry`

- **Return Values:** Registry with initialized worker map and default `30s` worker stale timeout.
- **Side Effects:** Allocates in-memory map.

##### `func NewRegistryWithStaleTimeout(workerStaleTimeout time.Duration) *Registry`

- **Parameters:** `workerStaleTimeout` controls how old `LastSeen` can become before cleanup removes a worker. Non-positive values fall back to `DefaultWorkerStaleTimeout`.
- **Return Values:** Registry with initialized worker map and configured stale timeout.
- **Side Effects:** Allocates in-memory map.

##### `func (r *Registry) WorkerStaleTimeout() time.Duration`

- **Return Values:** Configured worker stale timeout.
- **Side Effects:** None.

##### `func (r *Registry) RegisterWorker(worker shared.WorkerNode)`

- **Parameters:** Worker node record.
- **Return Values:** None.
- **Error Handling:** None.
- **Side Effects:** Locks registry, sets `LastSeen` if zero, stores worker by ID, logs registration.

##### `func RegisterNode(registry *Registry, worker shared.WorkerNode)`

- **Parameters:** Registry pointer and worker node.
- **Return Values:** None.
- **Side Effects:** Calls `RegisterWorker`.

##### `func (r *Registry) UpdateWorkerStatus(heartbeat shared.Heartbeat) error`

- **Parameters:** Heartbeat with node ID and resource values.
- **Return Values:** Nil when worker exists.
- **Error Handling:** Unknown worker ID.
- **Side Effects:** Mutates worker CPU, RAM, LastSeen.

##### `func (r *Registry) GetActiveWorkers() []shared.WorkerNode`

- **Return Values:** Snapshot slice of all registered workers.
- **Error Handling:** None.
- **Side Effects:** Acquires read lock.

##### `func (r *Registry) Cleanup()`

- **Return Values:** None.
- **Side Effects:** Deletes workers whose `LastSeen` is older than the registry stale timeout using the current time.

##### `func (r *Registry) CleanupAt(now time.Time)`

- **Parameters:** `now` is the reference time used to compare worker `LastSeen` values. This keeps cleanup deterministic in tests.
- **Return Values:** None.
- **Side Effects:** Deletes workers whose `LastSeen` is older than the registry stale timeout.

##### `func (s *Scheduler) SelectWorker(userLat, userLon float64, workers []shared.WorkerNode) (shared.WorkerNode, error)`

- **Parameters:** User coordinates and active worker slice.
- **Return Values:** Closest worker that meets `MinCPUFree` and `MinRAMFreeMB`.
- **Error Handling:** Empty worker list or no worker meeting capacity thresholds.
- **Side Effects:** None.

##### `func FilterWorkersByCapacity(workers []shared.WorkerNode, minCPUFree float64, minRAMFreeMB float64) []shared.WorkerNode`

- **Parameters:** Worker slice and minimum free CPU/RAM thresholds.
- **Return Values:** Workers whose `CPUFree >= minCPUFree` and `RAMFreeMB >= minRAMFreeMB`.
- **Error Handling:** None.
- **Side Effects:** None.

##### `func FindClosestWorker(userLat, userLon float64, workers []shared.WorkerNode) (shared.WorkerNode, error)`

- Same behavior as `SelectWorker`; iterates all workers and calculates Haversine distance.

##### `func calculateHaversine(lat1, lon1, lat2, lon2 float64) float64`

- **Return Values:** Distance in kilometers.
- **Error Handling:** None.
- **Side Effects:** None.

##### `func degreesToRadians(degrees float64) float64`

- **Return Values:** Radian equivalent.

##### `func (d *Dispatcher) Dispatch(ctx context.Context, req shared.ExecutionRequest) (shared.ExecutionResponse, error)`

- **Parameters:** Request context and execution request.
- **Return Values:** Worker execution response.
- **Error Handling:** ACR parse/token/manifest errors, no schedulable workers, scheduler errors, non-retryable worker forwarding errors, or the last retryable worker forwarding error after all candidates fail.
- **Side Effects:** May call Azure identity/ACR endpoints, reads registry, sends mTLS HTTP request to one or more worker candidates, logs dispatch attempts with `request_id`.

##### `func (d *Dispatcher) forwardToWorker(ctx context.Context, node shared.WorkerNode, req shared.ExecutionRequest) (shared.ExecutionResponse, error)`

- **Parameters:** Context, selected worker, execution request.
- **Return Values:** Decoded worker response with `ExecutedOnNodeID` set and `RequestID` preserved when the worker omits it.
- **Error Handling:** JSON marshal, mTLS client creation, request creation, network failure, non-2xx worker response, JSON decode failure. Network failures and worker `502`/`503`/`504` statuses are wrapped as retryable dispatch errors so `Dispatch` can choose another candidate.
- **Side Effects:** Network I/O to worker.

##### `func workerInvokeURL(address string) string`

- **Parameters:** Worker address with or without scheme.
- **Return Values:** HTTPS invoke URL ending in `/invoke`.
- **Error Handling:** None.
- **Side Effects:** None.

##### `func NewExecutionRequestTracker(ttl time.Duration) *ExecutionRequestTracker`

- **Parameters:** `ttl` controls how long completed successful responses remain cached. Non-positive values use `DefaultExecutionRequestCacheTTL`.
- **Return Values:** Request tracker with an empty map, TTL, and `time.Now` clock.
- **Error Handling:** None.
- **Side Effects:** None.

##### `func NewExecutionRequestTrackerWithLimit(ttl time.Duration, maxEntries int) *ExecutionRequestTracker`

- **Parameters:** `ttl` controls completed response age. `maxEntries` controls total request records retained in memory. Non-positive values use defaults.
- **Return Values:** Request tracker with TTL and max-entry bounds.
- **Error Handling:** None.
- **Side Effects:** None.

##### `func (t *ExecutionRequestTracker) Begin(req shared.ExecutionRequest) (ExecutionRequestStatus, error)`

- **Parameters:** Execution request with normalized `RequestID`.
- **Return Values:** Decision `ExecutionRequestStarted`, `ExecutionRequestInFlight`, `ExecutionRequestCompleted`, or `ExecutionRequestConflict`; cached response is included for completed duplicates.
- **Error Handling:** Returns fingerprinting errors if the request identity cannot be marshaled.
- **Side Effects:** Locks the tracker, removes expired completed entries, and may insert a new in-flight entry.

##### `func (t *ExecutionRequestTracker) Complete(requestID string, response shared.ExecutionResponse)`

- **Parameters:** Request ID and successful worker response.
- **Return Values:** None.
- **Error Handling:** None.
- **Side Effects:** Converts an in-flight entry into a completed cached response.

##### `func (t *ExecutionRequestTracker) Forget(requestID string)`

- **Parameters:** Request ID to remove.
- **Return Values:** None.
- **Error Handling:** None.
- **Side Effects:** Deletes request tracking state so failed dispatches can be retried.

##### `func (t *ExecutionRequestTracker) Stats() shared.RequestTrackerStats`

- **Parameters:** None.
- **Return Values:** Current idempotency tracker size, in-flight count, completed count, configured max entries, TTL seconds, eviction count, and expiration count.
- **Error Handling:** None.
- **Side Effects:** Removes expired completed entries before counting so `/wasmcat/metrics` reflects current cache state.

### ACR functions

##### `func ParseACRModuleReference(moduleRegistryURL string) (ACRReference, error)`

- Public wrapper around `parseACRModuleReference`.

##### `func parseACRModuleReference(moduleRegistryURL string) (acrReference, error)`

- **Parameters:** ACR registry, manifest, blob, tag, or referrer URL.
- **Return Values:** Registry name, repository name, optional reference, and reference kind.
- **Error Handling:** Invalid URL, non-ACR host, missing repository, missing reference.
- **Side Effects:** None.

##### `func parseACRReference(moduleRegistryURL string) (string, string, error)`

- **Return Values:** Registry name and repository name for legacy callers.
- **Error Handling:** Same as parser.

##### `func resolveACRModuleURL(ctx context.Context, moduleRegistryURL string, token string, ref acrReference) (string, error)`

- **Return Values:** Original URL for non-manifest references, or layer blob URL for manifest references.
- **Error Handling:** Manifest fetch, layer selection, blob URL build errors.
- **Side Effects:** Network I/O for manifest references.

##### `func FetchACRManifest(ctx context.Context, manifestURL string, token string) (OCIManifest, error)`

- Public wrapper around `fetchACRManifest`.

##### `func fetchACRManifest(ctx context.Context, manifestURL string, token string) (ociManifest, error)`

- **Parameters:** Context, ACR manifest URL, bearer token.
- **Return Values:** Decoded OCI manifest.
- **Error Handling:** Request creation, network errors, non-200 status, JSON decode failures, empty layer list.
- **Side Effects:** HTTP GET with `Authorization: Bearer <token>`.

##### `func SelectWASMLayer(manifest OCIManifest) (OCILayer, error)`

- Public wrapper around `selectWASMLayer`.

##### `func selectWASMLayer(manifest ociManifest) (ociLayer, error)`

- **Return Values:** Single layer if only one exists, otherwise first known WASM media type.
- **Error Handling:** Empty manifest or multiple layers without known WASM media type.
- **Side Effects:** None.

##### `func isWASMLayerMediaType(mediaType string) bool`

- **Return Values:** True for `application/wasm`, `application/vnd.module.wasm.content.layer.v1+wasm`, or `application/vnd.wasm.content.layer.v1+wasm`.

##### `func BuildACRBlobURL(originalURL string, repositoryName string, digest string) (string, error)`

- Public wrapper around `buildACRBlobURL`.

##### `func buildACRBlobURL(originalURL string, repositoryName string, digest string) (string, error)`

- **Return Values:** `/v2/<repository>/blobs/<sha256:digest>` URL on same ACR host.
- **Error Handling:** Missing repository, digest not starting with `sha256:`, invalid URL, non-ACR host.
- **Side Effects:** None.

##### `func GenerateACRToken(ctx context.Context, registryName string, repositoryName string) (string, error)`

- **Parameters:** Azure registry name without `.azurecr.io`, repository name.
- **Return Values:** Repository-scoped ACR pull access token from the process-local provider cache, or from the ACR OAuth flow on cache miss/refresh.
- **Error Handling:** Missing inputs, Azure credential creation, AAD token acquisition, JWT tenant extraction, exchange/token endpoint failures, empty minted token.
- **Side Effects:** May use Azure DefaultAzureCredential chain and network calls; mutates the in-memory token cache on successful mint.

##### `func NewACRTokenProvider() *ACRTokenProvider`

- **Return Values:** Production ACR token provider using `generateACRTokenUncached` and `time.Now`.
- **Side Effects:** Allocates an empty in-memory token map.

##### `func NewACRTokenProviderWithOptions(mint ACRTokenMintFunc, now func() time.Time) *ACRTokenProvider`

- **Parameters:** Optional token mint function and optional clock function.
- **Return Values:** Token provider configured with supplied hooks, falling back to production defaults when nil.
- **Side Effects:** Allocates an empty in-memory token map.

##### `func (p *ACRTokenProvider) Token(ctx context.Context, registryName string, repositoryName string) (string, error)`

- **Parameters:** Request context, Azure registry name without `.azurecr.io`, repository name.
- **Return Values:** Cached token if it expires more than 30 seconds in the future; otherwise a freshly minted token.
- **Error Handling:** Missing inputs, mint function errors, empty minted token.
- **Side Effects:** Reads and writes the provider cache under a mutex; may trigger Azure and ACR network I/O through the mint function.

##### `func generateACRTokenUncached(ctx context.Context, registryName string, repositoryName string) (string, time.Duration, error)`

- **Parameters:** Request context, Azure registry name without `.azurecr.io`, repository name.
- **Return Values:** Fresh repository-scoped ACR pull access token and TTL.
- **Error Handling:** Azure credential creation, AAD token acquisition, JWT tenant extraction, exchange/token endpoint failures.
- **Side Effects:** Uses Azure DefaultAzureCredential chain and ACR OAuth network calls.

##### `func exchangeAADTokenForRefreshToken(ctx context.Context, service string, tenantID string, aadToken string) (string, error)`

- **Return Values:** ACR refresh token.
- **Error Handling:** Request creation, network errors, non-200 status with body, JSON decode errors, missing `refresh_token`.
- **Side Effects:** Retryable HTTP POST to `https://<service>/oauth2/exchange`.

##### `func exchangeRefreshTokenForAccessToken(ctx context.Context, service string, repositoryName string, refreshToken string) (string, time.Duration, error)`

- **Return Values:** ACR access token scoped to `repository:<repositoryName>:pull` and token TTL. If ACR omits `expires_in`, the TTL defaults to 5 minutes.
- **Error Handling:** Request creation, network errors, non-200 status with body, JSON decode errors, missing `access_token`.
- **Side Effects:** Retryable HTTP POST to `https://<service>/oauth2/token`.

##### `func extractTenantID(jwtToken string) (string, error)`

- **Return Values:** `tid` claim from JWT payload.
- **Error Handling:** Invalid JWT format, base64 decode failure, JSON unmarshal failure, missing tenant ID.
- **Side Effects:** None.

### Worker package

##### `func (s *WorkerServer) Handler() http.Handler`

- **Return Values:** HTTP handler with `/wasmcat/health`, `/wasmcat/ready`, `/wasmcat/metrics`, and `/invoke`.
- **Side Effects:** None until served.

##### `func (s *WorkerServer) handleHealth(w http.ResponseWriter, r *http.Request)`

- **Return Values:** JSON health response with node ID and role.
- **Side Effects:** Writes HTTP response.

##### `func (s *WorkerServer) handleReady(w http.ResponseWriter, r *http.Request)`

- **Return Values:** 200 if `Engine` exists.
- **Error Handling:** 503 `not_ready` if engine is nil.
- **Side Effects:** Writes HTTP response.

##### `func (s *WorkerServer) handleInvoke(w http.ResponseWriter, r *http.Request)`

- **Parameters:** JSON execution request.
- **Return Values:** JSON execution response with `RequestID`, `Result`, `ExecutionTimeMs`, and `ExecutedOnNodeID`.
- **Error Handling:** 400 invalid body/validation failure/execution failure.
- **Side Effects:** Applies HTTP body limit, executes module through engine.

##### `func (s *WorkerServer) Start(ctx context.Context, port string) error`

- **Parameters:** Shutdown context and port.
- **Return Values:** Nil on clean shutdown.
- **Error Handling:** CA/cert load failures, TLS server errors, shutdown errors.
- **Side Effects:** Starts timeout-bounded HTTPS server requiring client certificates; on cancellation, uses `Limits.ShutdownTimeout` as the graceful shutdown window.

##### `func NewWasmEngine(ctx context.Context) *WasmEngine`

- **Parameters:** Runtime context.
- **Return Values:** Engine using `DefaultLimits`.
- **Side Effects:** Creates wazero runtime and cache.

##### `func NewWasmEngineWithLimits(ctx context.Context, limits Limits) *WasmEngine`

- **Parameters:** Runtime context and limits.
- **Return Values:** Engine with normalized limits.
- **Side Effects:** Creates wazero runtime configured with `WithCloseOnContextDone(true)`, cache map, mutex, and semaphore.

##### `func (e *WasmEngine) Limits() Limits`

- **Return Values:** Engine limits.
- **Side Effects:** None.

##### `func (e *WasmEngine) FetchAndCache(ctx context.Context, moduleName, moduleURL string, bearerToken string) error`

- **Parameters:** Module name, module URL, optional bearer token. Uses URL-aware fallback cache identity when no digest is supplied.
- **Return Values:** Nil if module is already cached or fetched and compiled.
- **Error Handling:** Missing URL on cache miss, request creation, network errors, non-200 status, read errors, module too large, digest format/mismatch errors when a digest is supplied, wazero compile errors.
- **Side Effects:** HTTP GET module bytes, optional Authorization header, compiles WASM, mutates cache.

##### `func (e *WasmEngine) FetchAndCacheWithDigest(ctx context.Context, moduleName, moduleURL string, moduleDigest string, bearerToken string) error`

- **Parameters:** Module name, URL, immutable digest when available, optional bearer token.
- **Return Values:** Nil if the digest-aware key is cached or fetched and compiled.
- **Error Handling:** Same as `FetchAndCache`, plus `moduleDigest` must be `sha256:<64 hex characters>` when provided and must match downloaded bytes.
- **Side Effects:** May coalesce with an in-flight compile, verify downloaded bytes, insert a cache entry, and evict expired/LRU entries.

##### `func (e *WasmEngine) Execute(ctx context.Context, moduleName string, moduleURL string, payload string, bearerToken string) (result string, err error)`

- **Parameters:** Module name, URL, input payload string, optional bearer token.
- **Return Values:** WASM output string.
- **Error Handling:** Payload too large, capacity exhausted, context cancellation, fetch/compile errors, missing cache entry, instantiate errors, memory write/read errors, missing `run`, `run` call failure, empty result, output too large.
- **Side Effects:** May download/compile/cache module, instantiate module, call WASM code, log execution events.

##### `func (e *WasmEngine) ExecuteWithDigest(ctx context.Context, moduleName string, moduleURL string, moduleDigest string, payload string, bearerToken string) (result string, err error)`

- **Parameters:** Module name, URL, immutable digest when available, input payload string, optional bearer token.
- **Return Values:** WASM output string.
- **Error Handling:** Same as `Execute`.
- **Side Effects:** Uses digest-aware cache lookup before instantiating and running the module.

##### `func verifyModuleDigest(moduleName string, wasmBytes []byte, moduleDigest string) error`

- **Parameters:** Module name, downloaded WASM bytes, optional `sha256:<hex>` digest.
- **Return Values:** Nil when no digest is supplied or when downloaded bytes match the digest.
- **Error Handling:** Unsupported digest scheme, missing digest value, invalid hex length/content, or SHA-256 mismatch. Digest failures are wrapped as `ModuleDigestError` so the worker API can return `module_digest_invalid` or `module_digest_mismatch` instead of a generic execution failure.
- **Side Effects:** None.

##### `func (e *WasmEngine) CacheStats() ModuleCacheStats`

- **Return Values:** Current cache entry count, raw byte count, max entries, and max bytes.
- **Side Effects:** None.

##### `func (e *WasmEngine) ClearCache(ctx context.Context)`

- **Parameters:** Context used when closing compiled modules.
- **Return Values:** None.
- **Side Effects:** Closes and removes all compiled module cache entries.

##### `func Allocate(ctx context.Context, mod api.Module, size uint32) (uint32, error)`

- **Parameters:** Wazero module instance and byte size.
- **Return Values:** Pointer returned from module-exported `malloc`.
- **Error Handling:** Missing `malloc`, call error, empty return.
- **Side Effects:** Calls WASM code.

##### `func WriteString(ctx context.Context, mod api.Module, input string) (uint32, error)`

- **Parameters:** Module instance and input string.
- **Return Values:** Pointer where bytes were written.
- **Error Handling:** Allocation error, missing memory export, failed memory write.
- **Side Effects:** Writes payload bytes into WASM linear memory.

##### `func ReadString(mod api.Module, ptr uint32, length uint32) (string, error)`

- **Parameters:** Module instance, output pointer, output length.
- **Return Values:** String copied from WASM memory.
- **Error Handling:** Missing memory export or out-of-bounds read.
- **Side Effects:** Reads WASM linear memory.

##### `func normalizeLimits(limits Limits) Limits`

- **Parameters:** Possibly partial limits.
- **Return Values:** Limits with defaults filled for non-positive values. Missing shutdown timeout defaults to execution timeout plus 5 seconds.
- **Error Handling:** None.
- **Side Effects:** None.

##### `func StartTelemetry(ctx context.Context, masterURL string, nodeID string, workerAddress string, latitude float64, longitude float64, interval time.Duration, certDir string)`

- **Parameters:** Cancellation context, master URL, worker ID, advertised address, worker coordinates, heartbeat interval, cert directory.
- **Return Values:** None.
- **Error Handling:** Logs mTLS client init failure and returns; register/heartbeat failures are logged and retried on next tick.
- **Side Effects:** Creates mTLS client; POSTs registration and heartbeat requests until context cancellation.

##### `func StartTelemetryWithMetrics(ctx context.Context, masterURL string, nodeID string, workerAddress string, latitude float64, longitude float64, interval time.Duration, certDir string, metricsProvider MetricsProvider)`

- **Parameters:** Same runtime telemetry inputs plus an injectable metrics provider.
- **Return Values:** None.
- **Error Handling:** Same as `StartTelemetry`; metric snapshot failures are logged and skip that heartbeat.
- **Side Effects:** Same network side effects as `StartTelemetry`.

##### `func (p SystemMetricsProvider) Snapshot(ctx context.Context) (NodeMetrics, error)`

- **Parameters:** Context used by gopsutil CPU and memory reads.
- **Return Values:** `NodeMetrics` with free CPU percentage and available RAM in MiB.
- **Error Handling:** CPU sampling errors, missing CPU samples, memory read errors.
- **Side Effects:** Reads host CPU and memory state.

##### `func newMTLSClient(certDir string, nodeID string) (*http.Client, error)`

- **Return Values:** Worker-authenticated HTTP client.
- **Error Handling:** Cert/key load, CA read, CA parse errors.
- **Side Effects:** Reads certificate files.

##### `func registerWorker(client *http.Client, masterURL string, nodeID string, workerAddress string, latitude float64, longitude float64)`

- **Parameters:** mTLS client, master URL, worker ID, advertised worker address, worker latitude, worker longitude.
- **Return Values:** None.
- **Error Handling:** Logs marshal, request creation, network, and rejection errors.
- **Side Effects:** HTTP POST to master `/internal/register`.

##### `func sendHeartbeat(ctx context.Context, client *http.Client, masterURL string, nodeID string, metricsProvider MetricsProvider)`

- **Parameters:** Context, mTLS client, master URL, worker ID, metrics provider.
- **Return Values:** None.
- **Error Handling:** Logs metric snapshot, marshal, request creation, network, and rejection errors.
- **Side Effects:** HTTP POST to master `/internal/heartbeat`.

## 4. Configuration, Environment, & Dependencies

### External Dependencies

- **Go toolchain:** `go 1.26.2` as declared in `go.mod`.
- **wazero `github.com/tetratelabs/wazero v1.11.0`:** Embedded WASM runtime, module compilation, instantiation, memory access, and function calls.
- **Azure SDK `github.com/Azure/azure-sdk-for-go/sdk/azidentity v1.13.1`:** Default Azure credential chain for ACR token generation.
- **Azure SDK `github.com/Azure/azure-sdk-for-go/sdk/azcore v1.20.0`:** Token request policy types.
- **gopsutil `github.com/shirou/gopsutil/v4`:** Worker host CPU and memory telemetry.
- **Go standard library:** HTTP servers/clients, TLS/x509, JSON, context, sync, time, crypto, logging.
- **Azure Container Registry API:** ACR OAuth exchange/token endpoints and OCI registry manifest/blob endpoints.
- **systemd:** Optional but documented Linux service manager for production hosts.

### Master Environment Variables

| Variable | Default | Format | Purpose |
| --- | --- | --- | --- |
| `MASTER_PORT` | `7270` | TCP port string | HTTPS listen port for master gateway. |
| `CERT_DIR` | `./certs` | Filesystem path | Directory containing `ca.crt`, `master.crt`, and `master.key`. |
| `AUTO_GENERATE_CERTS` | `true` | Go boolean string | When true, master startup writes local development certs. Set false in production. |
| `DEV_WORKER_ID` | `worker-vn-01` | string | Worker ID used for local dev cert generation. |
| `CLEANUP_INTERVAL` | `15s` | Go duration | Frequency for registry cleanup scans. |
| `WORKER_STALE_TIMEOUT` | `30s` | Go duration | Maximum time since a worker heartbeat before cleanup removes that registry entry. |
| `MASTER_SHUTDOWN_TIMEOUT` | `5s` | Go duration | Graceful shutdown window for in-flight master HTTP requests. |
| `MIN_WORKER_CPU_FREE` | `0` | float percentage | Minimum reported free CPU required before the scheduler can select a worker. |
| `MIN_WORKER_RAM_FREE_MB` | `0` | float MiB | Minimum reported free RAM required before the scheduler can select a worker. |
| `EXECUTE_CLIENT_ALLOWLIST` | empty | comma-separated certificate identities | Optional client certificate CN or DNS SAN allowlist for `/api/v1/execute`. Empty allows any trusted mTLS client. |
| `MAX_EXECUTION_REQUEST_BYTES` | `2097152` | integer bytes | Maximum JSON body size accepted by master `/api/v1/execute`. |
| `EXECUTION_REQUEST_CACHE_TTL` | `5m` | Go duration | How long successful execution responses remain cached by `request_id`. |
| `EXECUTION_REQUEST_CACHE_MAX_ENTRIES` | `4096` | integer count | Maximum in-memory request records kept by the master idempotency tracker. |
| `JOB_STORE_PATH` | `./wasmcat-jobs.db` | filesystem path | SQLite database file for durable execution job state. |
| `JOB_MAX_ATTEMPTS` | `3` | integer count | Maximum synchronous dispatch attempts recorded for one durable job. |
| `JOB_LEASE_TTL` | `30s` | Go duration | Dispatch lease duration written for durable jobs before worker forwarding. |
| `JOB_RECOVERY_INTERVAL` | `5s` | Go duration | Frequency for durable job recovery scans. |
| `JOB_RECOVERY_BATCH_SIZE` | `32` | integer count | Maximum durable jobs processed in one recovery scan. |
| `MODULE_HOST_ALLOWLIST` | empty | comma-separated hosts | Optional module URL host allowlist for `/api/v1/execute`. Empty allows any host. |
| `REQUIRE_MODULE_DIGEST` | `false` | Go boolean string | When true, execution requests must include `module_digest` or use a digest-pinned OCI manifest/blob URL. |

### Worker Environment Variables

| Variable | Default | Format | Purpose |
| --- | --- | --- | --- |
| `WORKER_ID` | `worker-vn-01` | string | Stable worker node ID and certificate filename suffix. |
| `WORKER_PORT` | `7271` | TCP port string | HTTPS listen port for worker invocation server. |
| `MASTER_URL` | `https://localhost:7270` | HTTPS URL | Master gateway URL used by telemetry. |
| `WORKER_ADVERTISE_ADDRESS` | `<hostname>:<WORKER_PORT>` | host:port or URL | Address stored in registry and used by master dispatch. Falls back to `localhost:<WORKER_PORT>` only if the OS hostname is unavailable. |
| `WORKER_LATITUDE` | `0` | float degrees, `-90` to `90` | Worker latitude stored during registration and used for distance scheduling. |
| `WORKER_LONGITUDE` | `0` | float degrees, `-180` to `180` | Worker longitude stored during registration and used for distance scheduling. |
| `CERT_DIR` | `./certs` | Filesystem path | Directory containing `ca.crt`, `worker-<id>.crt`, and `worker-<id>.key`. |
| `HEARTBEAT_INTERVAL` | `5s` | Go duration | Interval for worker registration and heartbeat loop. |
| `EXECUTION_TIMEOUT` | `5s` | Go duration | Full worker execution path timeout. |
| `MODULE_FETCH_TIMEOUT` | `10s` | Go duration | Module download/compile timeout. |
| `WORKER_SHUTDOWN_TIMEOUT` | `10s` | Go duration | Graceful shutdown window for in-flight worker HTTP requests. If unset, defaults to `EXECUTION_TIMEOUT + 5s`. |
| `MAX_MODULE_BYTES` | `10485760` | integer bytes | Maximum downloaded WASM module size. |
| `MAX_PAYLOAD_BYTES` | `1048576` | integer bytes | Maximum payload size before execution. |
| `MAX_OUTPUT_BYTES` | `1048576` | uint32 bytes | Maximum output bytes read from WASM memory. |
| `MAX_CONCURRENT_EXECS` | `4` | integer | Worker-local execution concurrency limit. |
| `MAX_CACHED_MODULES` | `128` | integer | Maximum compiled modules kept in worker cache. |
| `MAX_CACHE_BYTES` | `268435456` | integer bytes | Maximum raw WASM bytes represented by worker cache entries. |
| `MODULE_CACHE_TTL` | `30m` | Go duration | Maximum age for a compiled module cache entry. |

### Init Command Configuration

#### Master

```bash
wasmcat-master init \
  --config-dir /etc/wasmcat \
  --cert-dir /etc/wasmcat/certs \
  --port 7270 \
  --cleanup-interval 15s \
  --worker-stale-timeout 30s \
  --master-shutdown-timeout 5s \
  --module-host-allowlist modules.internal,myregistry.azurecr.io \
  --require-module-digest \
  --dev-worker-id worker-vn-01
```

`--dev-certs` generates local development certificates. `--force` overwrites existing generated files.

#### Worker

```bash
wasmcat-worker init \
  --worker-id worker-us-01 \
  --master-url https://master.example.com:7270 \
  --advertise-address worker-us-01.example.com:7271 \
  --config-dir /etc/wasmcat \
  --cert-dir /etc/wasmcat/certs
```

Worker init validates that `--master-url` is HTTPS and that numeric limits are positive.

### HTTP API Surface

All runtime endpoints are served over HTTPS with mTLS enabled.

| Component | Method | Endpoint | Expected Body | Success Response | Failure Modes |
| --- | --- | --- | --- | --- | --- |
| Master | `GET` | `/wasmcat/health` | none | `HealthResponse` | 405 wrong method, JSON encode failure only. |
| Master | `GET` | `/wasmcat/ready` | none | `HealthResponse` | 405 wrong method, 503 if registry, dispatcher, or scheduler is nil. |
| Master | `GET` | `/wasmcat/metrics` | none | `MetricsResponse` | 405 wrong method, JSON encode failure only. |
| Master | `POST` | `/internal/register` | `WorkerNode` | `APIResponse` | 405 wrong method, 413 body too large, 400 invalid JSON, 403 certificate identity mismatch. |
| Master | `POST` | `/internal/heartbeat` | `Heartbeat` | 200 empty body | 405 wrong method, 413 body too large, 400 invalid JSON, 403 certificate identity mismatch, 404 unknown worker. |
| Master | `POST` | `/internal/drain` | `DrainRequest` | `APIResponse` | 405 wrong method, 413 body too large, 400 invalid JSON, 403 certificate identity mismatch, 404 unknown worker. |
| Master | `POST` | `/api/v1/execute` | `ExecutionRequest` | `ExecutionResponse` | 405 wrong method, 413 body too large, 403 unauthorized execution client, 403 module policy violation, 409 duplicate/conflicting request ID, 400 invalid JSON/request, 503 dispatch failure. |
| Worker | `GET` | `/wasmcat/health` | none | `HealthResponse` | 405 wrong method, JSON encode failure only. |
| Worker | `GET` | `/wasmcat/ready` | none | `HealthResponse` | 405 wrong method, 503 if engine is nil. |
| Worker | `GET` | `/wasmcat/metrics` | none | `MetricsResponse` | 405 wrong method, JSON encode failure only. Includes execution, digest-failure, and module-cache counters. |
| Worker | `POST` | `/invoke` | `ExecutionRequest` | `ExecutionResponse` | 405 wrong method, 503 if engine is nil, 400 invalid JSON/request/execution failure, 400 invalid digest, 400 digest mismatch. |

Wrong methods return JSON error code `method_not_allowed` and an `Allow` header with the required method.

### WASM Module ABI

Each WASM module must export:

```text
memory
malloc(size uint32) uint32
run(ptr uint32, len uint32) uint64
```

The host writes the request payload into module memory at the pointer returned by `malloc`. The `run` function receives input pointer and length. Its `uint64` return packs output pointer and output length:

```text
output_ptr = uint32(result >> 32)
output_len = uint32(result)
```

## 5. Deployment & Operational Considerations

### Prerequisites

- Native release binaries from GitHub Releases or a local source build.
- Host OS with network access between master and workers.
- TLS certificate set:
  - Master: `ca.crt`, `master.crt`, `master.key`.
  - Worker: `ca.crt`, `worker-<WORKER_ID>.crt`, `worker-<WORKER_ID>.key`.
- Azure credentials on the master host if using ACR module references. The Azure SDK DefaultAzureCredential chain must resolve an identity authorized to pull repository content.
- systemd for the provided Linux service templates.
- No container runtime is required.

### Deployment Flow

1. Download release assets and verify `checksums.txt`.
2. Install `wasmcat-master` or `wasmcat-worker` into `/usr/local/bin`.
3. Run `wasmcat-master init` or `wasmcat-worker init`.
4. Provide production certificates in `CERT_DIR`.
5. Install the systemd service file.
6. Start service with `systemctl enable --now`.
7. Verify `/wasmcat/health` and `/wasmcat/ready` with an mTLS-capable client.

### Concurrency and Thread Safety

- `Registry` is protected by `sync.RWMutex` for concurrent registration, heartbeat, cleanup, and dispatch reads.
- `WasmEngine.cache` is protected by `sync.RWMutex`.
- Worker execution concurrency is capped by `WasmEngine.sem`, a buffered channel sized to `MaxConcurrentExecs`.
- Each execution instantiates a fresh WASM module instance, so request memory is not shared between invocations.
- The compiled module cache is process-local, digest-aware, and evicted by TTL, entry count, and raw byte budget.

### Edge Cases and Limitations

- **Worker coordinates default to zero:** Operators must set `WORKER_LATITUDE` and `WORKER_LONGITUDE`; otherwise workers appear at `0,0`.
- **Capacity thresholds are opt-in:** Defaults are zero, so every worker remains eligible unless operators set `MIN_WORKER_CPU_FREE` or `MIN_WORKER_RAM_FREE_MB`.
- **Registry cleanup is timer-based:** `CLEANUP_INTERVAL` controls scan cadence and `WORKER_STALE_TIMEOUT` controls eviction age. Set stale timeout higher than heartbeat interval to tolerate normal jitter.
- **ACR token cache is process-local:** Repeated dispatches for the same registry repository reuse a token until 30 seconds before expiry. Multiple master instances do not share token cache state.
- **HTTP clients are bounded:** Shared client defaults set total request, dial, TLS handshake, response-header, idle connection, and pool limits. Request contexts still provide operation-specific cancellation.
- **HTTP servers are bounded:** Master and worker servers set read-header, read, write, and idle timeouts through the shared server factory.
- **HTTP retries are bounded:** Safe outbound paths retry transient statuses and transport errors up to 3 attempts. Worker `/invoke` does not retry the same worker, but dispatch can reschedule to another candidate on transport failure or `502`/`503`/`504`.
- **Master request bodies are bounded:** Internal worker control messages are capped at 4 KiB. `/api/v1/execute` uses `MAX_EXECUTION_REQUEST_BYTES`.
- **Master shutdown is bounded:** Master HTTP shutdown uses `MASTER_SHUTDOWN_TIMEOUT`, so stop/restart does not wait forever on in-flight requests.
- **Worker shutdown is bounded:** Worker HTTP shutdown uses `WORKER_SHUTDOWN_TIMEOUT`; requests still remain constrained by `EXECUTION_TIMEOUT`.
- **Module cache key fallback:** Digest is preferred for cache identity. If no digest is provided, the worker falls back to module URL, then module name.
- **Cache byte accounting is approximate:** `MAX_CACHE_BYTES` uses raw WASM byte size, not exact compiled runtime memory.
- **Cold fetch coalescing is process-local:** Concurrent cold requests for the same cache key share one download/compile inside a worker process. Separate workers still compile independently.
- **Durable job reliability is partial:** normal master startup persists accepted jobs and successful responses in SQLite, so duplicate completed `request_id` calls can survive restart. Recovery loops, async job APIs, and worker completion callbacks are still future HA phases.
- **Health/readiness require mTLS:** Because TLS client auth is configured at server level, probes must present valid client certificates unless TLS routing changes.
- **Development cert generation overwrites at runtime:** `GenerateCAAndCerts` uses `os.Create`. Production should set `AUTO_GENERATE_CERTS=false`; bootstrap protects generated files unless `--force` is used.
- **Telemetry is host-level, not cgroup-level:** gopsutil reports host CPU and memory. It does not currently account for per-service cgroup quotas.
- **Some state remains process-local:** Registry and module cache are in memory. Master restart loses worker registry; worker restart loses compiled module cache. Multiple master instances still need a shared backend or leader protocol before active-active HA is safe.
- **Execution authorization allowlist is optional:** If `EXECUTE_CLIENT_ALLOWLIST` is empty, any valid client certificate trusted by the CA can call `/api/v1/execute`.
- **Module source policy is optional:** If `MODULE_HOST_ALLOWLIST` is empty and `REQUIRE_MODULE_DIGEST=false`, trusted execution clients can submit any HTTP(S) module URL.
- **WASM ABI is narrow:** Modules must match the exact `memory`, `malloc`, and packed `run` ABI. WASI modules or modules with different host imports are not supported by the current engine path.

### Operational Recommendations

- Disable `AUTO_GENERATE_CERTS` in production.
- Use short-lived or rotated mTLS certificates and restrict CA distribution.
- Use stable, unique `WORKER_ID` values because certificate filenames depend on them.
- Set worker coordinates explicitly so distance scheduling has meaningful data.
- Set capacity thresholds to prevent overloaded workers from receiving new executions.
- Ensure `WORKER_ADVERTISE_ADDRESS` is reachable from the master. Set it explicitly when the host's OS hostname is not resolvable from the master network.
- Prefer immutable module digests so workers can distinguish changed module content even when names or tags are reused.
- Keep `MAX_CONCURRENT_EXECS` aligned with CPU and memory capacity of each worker host.
- Add mTLS-capable health checks in service monitoring.
- For ACR deployments, run the master with a managed identity or service principal with repository pull permission.
