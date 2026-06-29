# wasmCat Project Context

## 1. Project Overview & Thesis Context

wasmCat is a native master-worker WebAssembly serverless orchestrator written in Go. Its purpose is to run WebAssembly modules on distributed edge workers without Docker, containerd, Kubernetes, or an external WebAssembly runtime service.

The system is designed around a clear control-plane/data-plane split:

- The **master** accepts execution requests, tracks workers, applies module source policy, resolves Azure Container Registry (ACR) module references, selects a worker, and dispatches execution over mutual TLS.
- The **worker** registers with the master, reports telemetry, fetches `.wasm` modules just in time, compiles and caches them with wazero, executes the module ABI, and returns the result.
- The **operator layer** includes `wasmcatctl`, a web dashboard, production VM deployment documentation, and a WABT-to-ACR module publishing workflow.

This is a final-year engineering thesis project. That matters: implementation quality, architectural justification, testing evidence, operational reproducibility, and honest discussion of limitations are as important as feature count. When working on this repository, treat every feature as both a software change and a thesis artifact that may need documentation, diagrams, experiments, or defense-ready explanation.

## 2. Technology Stack & Constraints

### Language and Runtime

- **Language:** Go `1.26.2`, as declared in `go.mod`.
- **WASM runtime:** `github.com/tetratelabs/wazero v1.11.0`.
- **Runtime model:** Embedded Go-native WebAssembly execution. No external Wasm runtime process is required.
- **CGO policy:** Avoid CGO dependencies. The project is intended to build and run as native Go binaries with minimal host assumptions.

### Storage

- **Durable job state:** `modernc.org/sqlite v1.52.0`.
- **Database driver:** pure-Go SQLite driver, chosen to preserve the no-CGO constraint.
- **SQLite behavior:** `internal/master/sqlite_job_store.go` opens a local SQLite database, enables WAL mode, sets a busy timeout, persists job request/response JSON, and indexes recoverable jobs.

### Cloud and Registry Integration

- **Registry:** Azure Container Registry (ACR).
- **Azure identity:** `github.com/Azure/azure-sdk-for-go/sdk/azidentity v1.13.1` and `azcore v1.20.0`.
- **ACR model:** wasmCat accepts ACR OCI manifest URLs such as `https://<registry>.azurecr.io/v2/<repo>/manifests/<tag>`. The master resolves the manifest to a WASM blob URL and forwards a repository-scoped pull token to the worker.
- **Module publishing:** `.github/workflows/publish-wasm-modules.yml` builds WABT `.wat` modules into `.wasm` and pushes them to ACR with ORAS as `application/wasm` OCI artifacts.

### Telemetry, HTTP, and Security

- **Telemetry:** `github.com/shirou/gopsutil/v4 v4.26.5` for CPU and RAM metrics.
- **HTTP:** Go standard `net/http`.
- **TLS/mTLS:** Go standard `crypto/tls` and `crypto/x509`.
- **Logging:** structured logging through `log/slog`, wrapped by `internal/logging`.

### Hard Constraints

- Keep wasmCat as a set of native Go binaries: `wasmcat-master`, `wasmcat-worker`, and `wasmcatctl`.
- Do not introduce Docker, Kubernetes, containerd, sidecars, or another orchestrator as runtime dependencies.
- Do not add external C-based dependencies or CGO-only packages.
- Prefer pure-Go libraries and explicit OS/service-manager integration such as systemd.
- Tests live in the separate `tests/` tree, not next to production files.

## 3. Architecture Breakdown

### Entrypoints

- `cmd/master/main.go`: starts the master control plane or runs `wasmcat-master init`.
- `cmd/worker/main.go`: starts a worker node or runs `wasmcat-worker init`.
- `cmd/wasmcatctl/main.go`: operator CLI for health, readiness, metrics, worker listing, synchronous execution, durable jobs, and worker draining.

### Control Plane: Master

The master is implemented mainly under `internal/master`.

#### Gateway

`internal/master/gateway.go` exposes the master HTTPS API:

- `GET /wasmcat/health`
- `GET /wasmcat/ready`
- `GET /wasmcat/metrics`
- `POST /internal/register`
- `POST /internal/heartbeat`
- `POST /internal/drain`
- `POST /internal/jobs/complete`
- `POST /api/v1/execute`
- `POST /api/v1/jobs`
- `GET /api/v1/jobs/{request_id}`
- `GET /api/v1/workers`

The gateway validates methods, limits request body sizes, decodes shared JSON models, applies module source policy, validates optional execution-client identities, validates worker certificate identity, and writes safe public error responses.

#### Registry

`internal/master/registry.go` stores live worker state in memory:

- `workers map[string]shared.WorkerNode`
- protected by `sync.RWMutex`
- supports `ready` and `draining` worker states
- updates registration and heartbeat state
- removes stale workers through a background cleanup loop

The registry is intentionally in-memory because worker liveness is real-time cluster state. It is not a durable node inventory.

#### Scheduler

`internal/master/scheduler.go` implements capacity-aware geo-routing:

1. Only ready workers are eligible.
2. Workers below `MIN_WORKER_CPU_FREE` are filtered out.
3. Workers below `MIN_WORKER_RAM_FREE_MB` are filtered out.
4. Remaining workers are scored by Haversine distance from `user_lat` and `user_lon`.
5. The nearest eligible worker is selected.

This is a defensible edge-computing routing baseline, but it is not equivalent to measured network latency. Future scheduling should incorporate RTT, queue depth, cache locality, and historical execution time.

#### Dispatcher

`internal/master/dispatcher.go` coordinates execution dispatch:

- normalizes `module_url` and `module_registry_url`
- detects ACR URLs
- obtains repository-scoped ACR tokens
- resolves ACR manifests to blob URLs
- gets schedulable workers from the registry
- asks the scheduler to select a worker
- forwards execution to the worker's `/invoke` endpoint over mTLS
- conservatively reschedules on transport errors and selected retryable worker HTTP statuses

Dispatcher rescheduling is intentionally conservative. It does not blindly repeat errors that may mean user code executed or failed deterministically.

#### ACR Resolution

`internal/master/acr_manifest.go` and `internal/master/acr_token.go` implement ACR module provisioning:

- parse `*.azurecr.io` registry, repository, manifest, and blob references
- use Azure `DefaultAzureCredential`
- exchange Azure identity tokens for ACR refresh/access tokens
- cache repository-scoped ACR pull tokens in memory
- fetch OCI manifests
- select a WASM layer by media type or single-layer fallback
- build `/v2/<repository>/blobs/sha256:<digest>` URLs

ACR token caching is process-local; multiple masters would not share token cache state.

#### Durable Job Store and Recovery

`internal/master/job.go`, `job_store.go`, `sqlite_job_store.go`, and `job_recovery.go` implement durable request state:

- Job states: `queued`, `dispatching`, `running`, `succeeded`, `failed`, `ambiguous`.
- Jobs store the original `ExecutionRequest`, fingerprint, attempts, max attempts, worker ID, lease deadline, last error, optional response, and timestamps.
- SQLite stores request/response JSON and state transition fields.
- WAL mode and busy timeout are enabled.
- The recovery loop scans recoverable jobs every `JOB_RECOVERY_INTERVAL`.
- Queued jobs are dispatched in the background.
- Expired `dispatching` or `running` leases become `ambiguous` instead of being blindly re-executed.

This improves reliability across master process restarts, but it is not full multi-master HA. SQLite is local durable state, not replicated cluster consensus.

### Data Plane: Worker

The worker is implemented mainly under `internal/worker`.

#### Worker HTTPS API

`internal/worker/server.go` exposes:

- `GET /wasmcat/health`
- `GET /wasmcat/ready`
- `GET /wasmcat/metrics`
- `POST /invoke`

The worker server enforces mTLS, limits request body size, decodes execution requests, calls the engine, records metrics, and posts durable completion callbacks to the master when a `request_id` is present.

#### Telemetry Loop

`internal/worker/telemetry.go` registers the worker with the master and sends periodic heartbeats:

- registration includes worker ID, advertised address, coordinates, CPU free, RAM free, and state
- heartbeat includes node ID, CPU free, and RAM free
- mTLS is used for register, heartbeat, drain, and completion communication

Worker location is resolved before telemetry starts. `internal/config.ResolveWorkerLocation` uses explicit `WORKER_LATITUDE`/`WORKER_LONGITUDE`, or auto-detects through `WORKER_LOCATION_PROVIDER_URL` when `WORKER_AUTO_DETECT_LOCATION=true`.

#### WasmEngine

`internal/worker/engine.go` owns the embedded wazero runtime and module cache:

- `runtime wazero.Runtime`
- digest-aware module cache map
- `sync.RWMutex` for cache safety
- in-flight compile coalescing to avoid duplicate cold compiles
- semaphore channel for `MAX_CONCURRENT_EXECS`
- bounded HTTP client for module fetches

Execution path:

1. Enforce payload size.
2. Acquire execution semaphore.
3. Apply execution timeout.
4. Fetch and cache module if needed.
5. Verify `module_digest` when supplied.
6. Compile and cache the module.
7. Instantiate a fresh module instance per request.
8. Write payload into Wasm linear memory.
9. Call `run(ptr, len)`.
10. Read output pointer/length from the packed return value.
11. Enforce output size.
12. Return output as a string.

#### WASM ABI

The worker supports two execution modes, selected by the request `abi` field (`""` auto-detect, `"wasmcat"`, or `"wasi"`).

The custom wasmCat ABI expects:

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

The host writes input bytes into module memory at the pointer returned by `malloc`, calls `run`, then reads output bytes from module memory. This keeps execution lightweight and deterministic.

The WASI mode runs standard `wasip1` command modules (Rust, Go, TinyGo, C, etc.): the worker instantiates `wasi_snapshot_preview1` (via wazero), delivers the payload on stdin, and reads the result from stdout (exit code 0 = success). When `abi` is empty the worker auto-detects: a module exporting `run` uses the wasmCat ABI; a module exporting `_start` uses WASI. Both modes share the same digest-aware compiled-module cache and per-request instance isolation.

#### Limits and Cache Lifecycle

`internal/worker/limits.go` defines defaults:

- `ExecutionTimeout`: `5s`
- `ModuleFetchTimeout`: `10s`
- `ShutdownTimeout`: `10s`
- `MaxModuleBytes`: `10 MiB`
- `MaxPayloadBytes`: `1 MiB`
- `MaxOutputBytes`: `1 MiB`
- `MaxConcurrentExecs`: `4`
- `MaxCachedModules`: `128`
- `MaxCacheBytes`: `256 MiB`
- `ModuleCacheTTL`: `30m`

The compiled module cache is digest-aware. If the same module name points at new remote bytes with a new digest, the cache key changes and the worker does not reuse stale compiled code.

### Security

Security is centered on strict mTLS:

- Master gateway requires client certificates.
- Worker API requires client certificates.
- Dispatcher uses master certificates when calling workers.
- Worker telemetry and completion callbacks use worker certificates when calling the master.
- Gateway validates worker certificate identity against the requested worker ID for registration, heartbeat, drain, and completion.
- Optional `EXECUTE_CLIENT_ALLOWLIST` restricts which trusted client certificate identities may call public execution/job APIs.

Certificate naming matters. For `WORKER_ID=worker-vn-01`, the worker cert files are expected as:

```text
worker-worker-vn-01.crt
worker-worker-vn-01.key
```

The trusted worker identity should be represented by common name `wasmcat-worker-worker-vn-01` or DNS SAN values containing `worker-vn-01` or `wasmcat-worker-worker-vn-01`.

## 4. Core Execution Workflows

### Synchronous Path: `POST /api/v1/execute`

The synchronous path blocks until the master has dispatched work to a worker and received a result.

1. Client sends an `ExecutionRequest` to `POST /api/v1/execute`.
2. Gateway validates request method, body size, JSON structure, request ID, module fields, optional client certificate allowlist, and module source policy.
3. Gateway ensures or creates a `request_id`.
4. If the durable `JobStore` is configured, the gateway creates or checks a durable job record for idempotency.
5. Matching completed request IDs return the stored response.
6. Same request ID with different fingerprint returns `409 request_id_conflict`.
7. Active duplicate request IDs return `409 request_in_progress`.
8. Dispatcher resolves ACR references if needed.
9. Dispatcher selects an eligible worker through the registry and scheduler.
10. Dispatcher forwards to `POST /invoke` on the selected worker over mTLS.
11. Worker executes the module through `WasmEngine`.
12. Worker sends a completion callback to `POST /internal/jobs/complete` when a request ID is present.
13. Worker returns `ExecutionResponse`.
14. Master stores the successful response and returns JSON to the client.

This path is best for short serverless-style functions where the caller expects an immediate result.

### Asynchronous Durable Path: `POST /api/v1/jobs`

The asynchronous path persists the request and returns immediately.

1. Client sends an `ExecutionRequest` to `POST /api/v1/jobs`.
2. Gateway validates method, body size, JSON, module fields, module policy, request ID, and optional execution-client identity.
3. Gateway computes a fingerprint over the meaningful request identity.
4. Gateway creates a `queued` `JobRecord` in SQLite.
5. New jobs return `202 Accepted`.
6. Duplicate same request ID and same fingerprint return the existing job.
7. Duplicate same request ID but different fingerprint returns `409 request_id_conflict`.
8. `JobRecovery` scans queued/recoverable jobs in the background.
9. Recovery marks the job `dispatching` with a lease.
10. Recovery dispatches through the same `Dispatcher` used by synchronous execution.
11. Worker executes the request and reports completion to `/internal/jobs/complete`.
12. Gateway marks the job `succeeded` with stored `ExecutionResponse`, or `failed` with an error.
13. If a dispatch/running lease expires, recovery marks the job `ambiguous`.
14. Clients inspect status with `GET /api/v1/jobs/{request_id}`.

This path is more reliable under restarts or unstable clients because the request state survives master process restarts. It is still local-node durability, not replicated HA.

## 5. Current Development State & Next Steps

### Recently Added or Current Features

- Native master/worker bootstrap commands through `wasmcat-master init` and `wasmcat-worker init`.
- `wasmcatctl` operator CLI replacing long mTLS `curl` commands.
- Worker auto-location for cloud VM routing, with configurable geolocation provider.
- Master/worker health, readiness, and metrics endpoints under `/wasmcat/*`.
- Strict mTLS and worker certificate identity binding.
- Capacity-aware Haversine scheduler.
- ACR manifest resolution and repository-scoped token caching.
- Module source allowlist and optional digest requirements.
- Digest-aware worker compiled-module cache with max entries, max represented bytes, and TTL eviction.
- Bounded worker execution limits: payload, module size, output size, fetch timeout, execution timeout, concurrent execution count.
- Durable SQLite job store for synchronous idempotency and async jobs.
- Durable async job API: `POST /api/v1/jobs` and `GET /api/v1/jobs/{request_id}`.
- Worker job completion callback to `POST /internal/jobs/complete`.
- Durable job recovery loop with conservative ambiguity handling.
- Worker draining endpoint and state.
- `version` subcommand on all four binaries (`wasmcat-master version`, `wasmcat-worker version`, `wasmcatctl version`, `wasmcat-ui --version`); the build-time `-X main.version` ldflag now applies to every binary.
- `scripts/install-or-update.sh` for first-time install and idempotent in-place updates: accepts a positional role (`install-or-update.sh master|worker|ctl|ui|all`) or `--role`, creates the `wasmcat` service user and dirs, backs up the prior binary as `.bak`, shows installed-vs-new versions, optionally installs a cert bundle with explicit per-file permissions (`--certs-from`), restarts only the matching systemd service, auto-rolls-back on a failed restart, supports `--rollback`/`--dry-run`/`--install-service` and downloading a tagged release, and never overwrites `/etc/wasmcat/*.env` or the SQLite DB.
- `scripts/gen-certs.sh` generates a consistent mTLS bundle from one CA (master/worker/operator) with correct SANs, `serverAuth`/`clientAuth`, and `worker-<WORKER_ID>` filenames; the CA private key stays in a separate PKI directory, never on a node. Prevents the common CA-mismatch / missing-clientAuth / missing-IP-SAN / wrong-filename failures.
- `scripts/build.sh` validates the local Go toolchain against `go.mod` and fails early with a clear message instead of the confusing `invalid go version` error.
- Worker auto-location accepts a comma-separated `WORKER_LOCATION_PROVIDER_URL` and falls back across providers (default `https://ipapi.co/json/,https://ipinfo.io/json`); `wasmcat-worker init` warns when `MASTER_URL`/`WORKER_ADVERTISE_ADDRESS` use a loopback host.
- Optional WASI execution mode beside the custom ABI: requests carry an `abi` field (`""` auto-detect, `wasmcat`, `wasi`); `wasmcatctl execute/jobs create --abi` and the dashboard ABI selector expose it. Standard `wasip1` modules run via stdin/stdout using wazero's `wasi_snapshot_preview1`.
- Separate `tests/` tree covering unit, integration, and smoke tests.
- Production VM deployment docs, node setup troubleshooting, and ACR usage docs. Module publishing to ACR is documented as manual ORAS steps in `docs/PRODUCTION_VM_DEPLOYMENT.md`.
- `scripts/demo.sh` capability demo runner plus `docs/DEMO.md`.
- `.gitattributes` enforces LF for shell/Go files so scripts stay runnable on Linux and gofmt is consistent across platforms.

### Not Yet Built (Despite Earlier Notes)

A WABT-to-ACR GitHub Actions publishing pipeline is **not** implemented. The following were referenced in earlier drafts but do not exist in the tree: `.github/workflows/publish-wasm-modules.yml`, `docs/WASM_MODULE_PIPELINE.md`, and `examples/wasm-modules/`. Building them is a candidate next step (see below). There is also an inert `.github/workflow/sync-to-ado.yml` in a misnamed directory (`workflow`, singular) that GitHub Actions never runs.

### Immediate Engineering Next Steps

1. Validate the WABT-to-ACR GitHub workflow against a real ACR instance.
2. Run the full real-environment ACR execution path: GitHub workflow -> ACR artifact -> master manifest resolution -> worker JIT fetch -> wazero result.
3. Add measured latency tables for direct URL cold/warm cache and ACR cold/warm cache.
4. Add a small module SDK/template so developers can write production logic without hand-writing WAT.
5. WASI support is implemented as an optional execution mode beside the custom ABI (request `abi=wasi`, or auto-detected from `_start`). Next: broaden compatibility for modules needing host imports beyond `wasi_snapshot_preview1`, and add WASI filesystem/env policy if required.
6. Improve production HA beyond local SQLite: external replicated database, leader election, or single-master failover strategy.
7. Extend scheduling with measured RTT, queue depth, cache locality, and historical execution time.
8. Strengthen resource isolation with fuel metering, memory caps, cgroups, or process-level sandboxing.
9. Continue thesis writing with precise limitation analysis; avoid overstating enterprise production readiness.

### Known Production Limitations

- Single active master is a control-plane availability risk.
- SQLite durability is local to one master VM and is not replicated.
- Worker registry is in-memory and reconstructed from registration/heartbeat traffic.
- Haversine distance is a geographic approximation, not real network latency.
- Host-level CPU/RAM telemetry is not the same as per-invocation cgroup isolation.
- Execution supports the custom `memory`/`malloc`/`run` ABI and standard `wasip1` WASI modules (stdin/stdout); modules needing host imports beyond `wasi_snapshot_preview1` still do not run unchanged.
- ACR token cache is process-local.
- Public IP geolocation providers can rate-limit or report inaccurate coordinates; production should prefer cloud metadata or internal region mapping.

## 6. AI Instructions (Rules for Claude)

When assisting with this project, follow these rules.

### Engineering Rules

- Always write idiomatic, high-performance Go.
- Never suggest adding CGO dependencies unless the user explicitly decides to abandon the no-CGO constraint.
- Never suggest Docker, Kubernetes, containerd, or a sidecar as a required runtime dependency.
- Keep the system installable as native binaries.
- Prefer pure-Go dependencies and standard-library primitives.
- Always consider concurrent map writes. Use `sync.RWMutex`, `sync.Mutex`, channels, or another explicit synchronization strategy.
- Preserve the existing package boundaries:
  - `cmd/` for binaries
  - `internal/master` for control-plane logic
  - `internal/worker` for data-plane execution
  - `internal/shared` for shared contracts
  - `internal/security` for mTLS helpers
  - `internal/config` for env parsing
  - `internal/bootstrap` for init/config generation
  - `internal/ctl` for `wasmcatctl`
- Keep tests in the separate `tests/` tree.
- Run or recommend `go test ./...`, `go vet ./...`, and `go build ./...` for meaningful code changes.
- For docs-only changes, state that tests were not required.
- When adding features, also update relevant docs under `docs/`.

### Architecture Rules

- Do not describe the durable job store as full HA. It is local durable recovery.
- Treat ambiguous jobs carefully. Do not blindly redispatch when the system cannot prove whether a worker executed a request.
- Preserve mTLS identity checks for worker registration, heartbeat, drain, and completion.
- Keep module source policy and digest-aware cache behavior in mind when changing ACR/module-fetch code.
- When adding execution compatibility, prefer an explicit new mode such as optional WASI support rather than weakening the current ABI path.

### Thesis Writing Rules

- Write in a formal academic tone, but avoid robotic list-like prose.
- Use numbered square-bracket citations such as `[1]`.
- Follow USTH ICT Bachelor Thesis formatting expectations when drafting thesis content:
  - Roman numeral chapter naming where required.
  - Clear section numbering.
  - Academic explanation before implementation detail.
  - Limitations stated objectively.
- Do not overclaim production readiness. Present wasmCat as a strong thesis prototype with a working end-to-end orchestrator, durable local job recovery, geo-aware scheduling, mTLS security, and ACR provisioning, while acknowledging remaining enterprise gaps.
- When discussing diagrams, prefer Mermaid diagrams that map directly to actual code modules and routes.

### Communication Rules

- Be precise and pragmatic.
- If the user asks for implementation, apply the change rather than only proposing it.
- If the user asks for a plan, include purpose, implementation steps, tests, and documentation updates.
- If deployment troubleshooting appears, check:
  - `MASTER_URL`
  - `WORKER_ADVERTISE_ADDRESS`
  - CA fingerprints
  - certificate SANs
  - worker cert filename versus `WORKER_ID`
  - `WORKER_AUTO_DETECT_LOCATION`
  - Azure NSG/firewall ports `7270` and `7271`
  - master managed identity `AcrPull`
  - GitHub/CI identity `AcrPush`
