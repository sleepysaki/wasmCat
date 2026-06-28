# UNIVERSITY OF SCIENCE AND TECHNOLOGY OF HANOI

# DEPARTMENT OF INFORMATION AND COMMUNICATION TECHNOLOGY

# BACHELOR THESIS

By

`<Student's name>`

Title:

**wasmCat (EdgeNet): A Native Decentralized WebAssembly Orchestrator for Edge Computing**

External Supervisor: `<External supervisor name>`

Internal Supervisor: `<Internal supervisor name>`

Hanoi, June 2026

---

## Supervisor Certification

To whom it may concern,

I, `<supervisor's name>`, certify that the thesis report of Mr/Ms. `<student's name>` is qualified to be presented to the Bachelor Thesis Jury 2025-2026.

Hanoi, `<date>`

Supervisor's signature

---

## Table of Contents

ACKNOWLEDGEMENTS

LIST OF ABBREVIATIONS

LIST OF TABLES

LIST OF FIGURES

ABSTRACT

I/ INTRODUCTION

II/ OBJECTIVES

III/ MATERIALS AND METHODS

IV/ RESULTS AND DISCUSSION

V/ CONCLUSION & PERSPECTIVE

REFERENCES

APPENDICES

---

## ACKNOWLEDGEMENTS

I would like to express my gratitude to the Department of Information and Communication Technology at the University of Science and Technology of Hanoi for providing the academic environment in which this project was designed and implemented. I am especially grateful to my supervisors for their guidance in refining the research problem, evaluating the engineering direction, and keeping the work aligned with both distributed systems practice and thesis expectations.

I would also like to thank the lecturers and teaching assistants whose courses in operating systems, networking, cloud computing, cybersecurity, and software engineering shaped the technical foundation of this work. Their instruction directly influenced the design decisions in wasmCat, including the separation of control plane and data plane, the use of mutual TLS for internal communication, and the emphasis on reproducible testing.

Finally, I thank my classmates, friends, and family for their encouragement during the design, implementation, debugging, and documentation phases of the project. Building a working WebAssembly orchestration system required repeated iteration across networking, runtime execution, authentication, registry integration, and reliability concerns. Their support helped sustain the work through those iterations.

---

## LIST OF ABBREVIATIONS

| Abbreviation | Meaning |
| --- | --- |
| ABI | Application Binary Interface |
| ACR | Azure Container Registry |
| API | Application Programming Interface |
| CI | Continuous Integration |
| CPU | Central Processing Unit |
| DNS | Domain Name System |
| HA | High Availability |
| HTTP | Hypertext Transfer Protocol |
| JIT | Just-In-Time |
| JSON | JavaScript Object Notation |
| mTLS | Mutual Transport Layer Security |
| OCI | Open Container Initiative |
| RAM | Random Access Memory |
| SAN | Subject Alternative Name |
| SDK | Software Development Kit |
| TLS | Transport Layer Security |
| TTL | Time To Live |
| USTH | University of Science and Technology of Hanoi |
| VM | Virtual Machine |
| WAL | Write-Ahead Logging |
| WASI | WebAssembly System Interface |
| WASM | WebAssembly |

---

## LIST OF TABLES

Table 1: Main wasmCat source code modules

Table 2: Major configuration parameters

Table 3: Implemented test layers

Table 4: Current limitations and future work

---

## LIST OF FIGURES

No figures are included in this text-only thesis draft. Architecture and execution flow are described in prose so the document can be reviewed without diagrams or demonstrations.

---

## ABSTRACT

Edge computing requires lightweight execution platforms that can place computation near users while maintaining isolation, portability, and operational simplicity. Traditional container orchestrators provide strong ecosystem support but introduce substantial runtime dependencies and deployment overhead for small edge nodes. This thesis presents wasmCat, also called EdgeNet, a native decentralized master-worker WebAssembly orchestration system written in Go. The system accepts execution requests at a master control plane, selects workers using capacity-aware spatial routing, provisions WebAssembly modules just in time from direct URLs or Azure Container Registry, and executes them on workers using the wazero runtime.

wasmCat separates orchestration from execution through a control plane and data plane design. Internal communication is protected by mutual TLS, worker state is maintained in a thread-safe registry, execution jobs are persisted in SQLite, and compiled modules are cached on workers using digest-aware keys with size, count, and TTL eviction. The implementation includes request idempotency, module source policy checks, ACR token caching, worker draining, health/readiness endpoints, structured metrics, and automated tests across unit, integration, and smoke layers.

The thesis analyzes the architecture, implementation methods, operational behavior, reliability properties, and current limitations of the system. The result is a working academic prototype with several production-oriented mechanisms, while still requiring distributed master replication, stronger benchmarking, and hardened certificate operations before full production deployment.

Key words: WebAssembly, edge computing, orchestration, mutual TLS, Azure Container Registry, Go.

---

# I/ INTRODUCTION

## 1.1 Global Context

Modern cloud systems increasingly need to execute code close to the point where data is produced or consumed. This requirement appears in Internet of Things gateways, smart city infrastructure, industrial monitoring, low-latency web services, and geographically distributed applications. In these environments, sending every request to a central cloud region can add latency, increase bandwidth cost, and reduce resilience when connectivity is unstable. Edge computing addresses this problem by moving computation closer to users, devices, or local networks.

The main challenge is that edge nodes are usually more constrained and less homogeneous than centralized cloud servers. A small edge deployment may consist of low-power machines, virtual machines in multiple regions, or local servers with different operating systems and resource capacities. A production platform for this context must handle scheduling, isolation, secure communication, module distribution, node health, and operational simplicity. These concerns are familiar from container orchestration, but the standard tools can be heavy for a student-built edge system or for small deployments where the goal is to run short-lived functions instead of full application containers.

WebAssembly provides a promising execution model for this problem. A WebAssembly module is compact, portable, sandboxed, and designed for deterministic execution. Originally associated with browsers, WebAssembly is now used on servers through runtimes that execute modules outside the browser. When a module is compiled once and instantiated repeatedly, it can provide fast startup behavior compared with starting a full process or container. This makes WebAssembly suitable for edge workloads that process small payloads, such as validation, transformation, inference pre-processing, filtering, or event handling.

wasmCat, also referred to as EdgeNet in this thesis, explores how far a native WebAssembly orchestrator can go without depending on Docker, containerd, Kubernetes, or an external process supervisor as its core execution substrate. The system is implemented in Go and uses a master-worker architecture. The master accepts client requests, tracks workers, resolves module references, and dispatches work. The workers run HTTPS servers, register with the master, report telemetry, fetch modules, cache compiled WebAssembly, execute the requested module, and return the result.

The project is intentionally narrow compared with Kubernetes. It does not attempt to manage arbitrary containers, pods, volumes, service meshes, or declarative cluster objects. Instead, it focuses on one core responsibility: securely execute WebAssembly modules on suitable edge workers with low operational overhead. This focus makes the implementation suitable for academic analysis because each major system concern is visible in the codebase: scheduling, registry state, mTLS, module provisioning, runtime execution, cache lifecycle, durable job tracking, and test coverage.

## 1.2 Problem Statement

The research problem of this thesis is the design and implementation of a lightweight orchestration system for WebAssembly workloads in an edge-like environment. The system should be easy to install as native binaries, avoid unnecessary dependency on a container runtime, support secure master-worker communication, and decide where to execute work based on both resource availability and physical proximity.

The specific problem can be divided into six technical questions. First, how can a master node maintain enough live worker state to schedule work without introducing a complex distributed database at the prototype stage? Second, how can workers execute untrusted or semi-trusted code while keeping the host process separate from module memory? Third, how can module fetching be integrated with a real registry such as Azure Container Registry while still supporting direct module URLs for development and testing? Fourth, how can the system prevent stale compiled modules from being reused when a remote module changes? Fifth, how can request execution become reliable enough that duplicate submissions, master restarts, or worker failures do not immediately lose all state? Sixth, how can the project remain easy to install without depending on Docker for packaging the orchestrator itself?

The implemented system answers these questions through a set of scoped engineering choices. Worker membership is tracked in an in-memory registry protected by `sync.RWMutex`, while execution job state is persisted in SQLite. WebAssembly execution is handled by wazero, a pure Go runtime, so workers do not need a C runtime binding or a separate native engine installation. Internal APIs use HTTPS with mutual TLS. Module provisioning supports both direct module URLs and ACR OCI references. Worker cache keys include module name, URL, and digest, and the cache enforces TTL, maximum entries, and maximum represented bytes. Request IDs and durable jobs provide idempotency and recovery foundations.

## 1.3 Related Technical Background

Container orchestration platforms such as Kubernetes are the dominant model for operating distributed workloads. They provide declarative scheduling, service discovery, self-healing, and rolling updates. However, their strength is also their cost: they assume a fairly large control plane, a container runtime, container images, and a complex operational model. For a project focused on small WebAssembly functions at the edge, this is more machinery than the core research question requires.

Function-as-a-service platforms provide a different model: users submit code that runs in response to requests or events. These systems are closer to wasmCat's execution style, but many production FaaS platforms are built on containers or proprietary cloud services. wasmCat instead exposes the orchestration mechanics directly in a small Go codebase. This makes it possible to study the internal path from request validation to worker selection, module fetching, runtime instantiation, memory transfer, and response generation.

WebAssembly's main contribution to this design is its sandboxed execution model. A module cannot directly access host memory; it interacts with the host through exported and imported functions and through linear memory. wasmCat defines a simple ABI in which modules export `malloc` and `run`. The host calls `malloc` to reserve module memory for the input payload, writes the payload bytes into WebAssembly linear memory, then calls `run(ptr, len)`. The module returns a packed pointer and length describing the output region. This ABI is simple enough for a thesis prototype, but strict enough that modules must follow the expected signature.

Mutual TLS is used because internal node communication is not treated as implicitly trusted. In a distributed edge system, a worker registration endpoint is sensitive: a malicious process should not be able to claim a worker ID and receive workloads. In wasmCat, the master requires client certificates, verifies the certificate chain against the configured CA, and checks that a worker certificate identity matches the worker ID it claims. This provides both encryption and node authentication at the transport layer.

Azure Container Registry is integrated because it represents a realistic module distribution backend. Instead of assuming modules are stored as local files, the master can parse an ACR reference, mint a repository-scoped pull token using Azure identity, fetch an OCI manifest, select a WebAssembly layer, and forward a blob URL and bearer token to the worker. This keeps the worker focused on downloading bytes and executing modules, while the master owns registry-specific resolution.

## 1.4 Scope of the Thesis

The thesis scope is an implementation and architectural evaluation of wasmCat as a native WebAssembly orchestration prototype. The work includes master and worker binaries, bootstrap commands, mTLS certificate handling, worker telemetry, capacity-aware spatial scheduling, direct and ACR-based module fetching, wazero execution, module cache lifecycle controls, durable SQLite-backed jobs, request idempotency, structured errors, metrics, installation documentation, and automated tests.

The scope does not include a full production cluster manager. In particular, the project does not yet implement multi-master consensus, replicated durable state, leader election, automatic certificate rotation, distributed service discovery, a public dashboard, autoscaling, multi-tenant authorization, or full benchmark methodology across multiple physical edge regions. These are discussed as limitations and future work.

# II/ OBJECTIVES

## 2.1 Scientific Objective

The scientific objective of this thesis is to design and implement a native WebAssembly orchestration system that demonstrates secure, lightweight, and location-aware execution of modules across edge workers. The strategy is to build a small but complete master-worker platform in Go, then evaluate its architecture, execution lifecycle, reliability mechanisms, and limitations from the perspective of distributed systems and edge computing.

## 2.2 Engineering Objectives

The first engineering objective is to build a control plane that accepts execution requests through an HTTP API, validates input, tracks request identity, stores durable job state, and dispatches work to a suitable worker. In the codebase this responsibility is concentrated in `cmd/master/main.go` and `internal/master/gateway.go`, with support from the registry, scheduler, dispatcher, job store, module policy, request tracker, and ACR resolver packages.

The second objective is to build a data plane that executes WebAssembly modules safely and efficiently. The worker must expose an invocation endpoint, enforce execution limits, fetch modules on demand, verify module digests when supplied, cache compiled modules, instantiate a fresh module for each request, bridge strings through WebAssembly linear memory, and return a structured response. This is implemented in `cmd/worker/main.go`, `internal/worker/server.go`, `internal/worker/engine.go`, and `internal/worker/memory.go`.

The third objective is to secure internal communication. The master and workers must use mTLS so that worker registration, heartbeat, dispatch, drain, and completion callback traffic cannot be trivially spoofed. The security layer must also bind worker certificate identity to worker ID during internal control operations.

The fourth objective is to support practical module distribution. The system must execute modules from direct URLs and from Azure Container Registry references. For ACR, the master must understand OCI manifest URLs and blob URLs, issue repository-scoped pull tokens, select WebAssembly layers, and propagate digest information to workers.

The fifth objective is to improve reliability beyond a pure synchronous prototype. The project must support request IDs, duplicate request behavior, durable job records, job leases, recovery scanning, worker completion callbacks, and conservative rescheduling when selected workers fail before execution.

The sixth objective is to make the system installable without Docker. The release model should produce native binaries and systemd service templates. Operators should be able to initialize the master and workers using `init` commands and environment files, then run the processes directly on Linux or Windows.

## 2.3 Evaluation Objectives

The evaluation objective is not to prove production readiness through large-scale benchmarks, because the repository does not yet contain controlled multi-node measurement data. Instead, this thesis evaluates implementation completeness, architectural correctness, operational reproducibility, and test coverage. The strongest claims are tied to code-level evidence: route definitions, TLS configuration, scheduler logic, cache eviction rules, SQLite state transitions, and automated tests. Performance claims are framed as mechanism-based analysis and future benchmark targets unless directly measured.

# III/ MATERIALS AND METHODS

## 3.1 Development Materials

The project is implemented as a Go module named `wasmcat`. The `go.mod` file specifies Go 1.26.2 and depends on a small set of major libraries. The WebAssembly runtime is `github.com/tetratelabs/wazero`, chosen because it is implemented in Go and can be embedded directly inside the worker process. Azure authentication uses `github.com/Azure/azure-sdk-for-go/sdk/azidentity` and Azure core packages. Host telemetry uses `github.com/shirou/gopsutil/v4`. Durable local job storage uses `modernc.org/sqlite`, a Go-compatible SQLite driver that avoids requiring CGO in the common build path.

The repository is organized into entrypoints, internal packages, documentation, packaging, scripts, tests, and module fixtures. The master entrypoint is `cmd/master/main.go`; the worker entrypoint is `cmd/worker/main.go`. The `internal/master` package contains the control-plane implementation. The `internal/worker` package contains the runtime execution implementation. Shared HTTP models, retry helpers, metrics, server defaults, and address helpers live in `internal/shared`. Bootstrap and configuration packages are separated into `internal/bootstrap` and `internal/config`. Security helpers live in `internal/security`.

Table 1 summarizes the major source modules.

| Module | Responsibility |
| --- | --- |
| `cmd/master/main.go` | Master startup, dependency construction, signal handling, init command |
| `cmd/worker/main.go` | Worker startup, engine construction, telemetry startup, init command |
| `internal/master/gateway.go` | Master API routes, request validation, job handling, mTLS server |
| `internal/master/dispatcher.go` | Worker selection coordination, ACR resolution, mTLS forwarding |
| `internal/master/registry.go` | Thread-safe in-memory worker state |
| `internal/master/scheduler.go` | Capacity filtering and Haversine worker selection |
| `internal/master/sqlite_job_store.go` | Durable job persistence using SQLite |
| `internal/master/job_recovery.go` | Recovery loop for queued and lease-expired jobs |
| `internal/worker/server.go` | Worker HTTPS API, invocation handler, completion callbacks |
| `internal/worker/engine.go` | wazero runtime, module fetching, digest verification, cache lifecycle |
| `internal/worker/memory.go` | Host-to-WASM string memory bridge |
| `internal/security/mtls.go` | Certificate generation and mTLS HTTP clients |

## 3.2 Architecture Design Method

The system uses a master-worker architecture with a clear separation between control plane and data plane. The control plane is responsible for cluster knowledge and decisions. It stores worker metadata, validates requests, checks module policy, resolves ACR references, chooses a worker, records job state, and forwards the execution request. The data plane is responsible for actual execution. Workers fetch module bytes, compile or reuse compiled modules, instantiate modules, copy payloads into WebAssembly memory, call the module entrypoint, and return outputs.

The master is intentionally centralized in the current implementation. This is a pragmatic research decision. A complete distributed control plane would require consensus, state replication, leader election, and conflict handling, which would increase implementation complexity significantly. Instead, wasmCat uses an in-memory registry for live worker state and SQLite for durable execution jobs. This allows the thesis to evaluate the core orchestration path while acknowledging that high availability is partial rather than complete.

The control plane/data plane boundary is implemented through HTTP APIs. The master exposes public execution endpoints and internal worker endpoints. The worker exposes an internal invocation endpoint and local health/readiness endpoints. All production server startup paths configure TLS with `ClientAuth: tls.RequireAndVerifyClientCert`, which means a peer must present a trusted certificate before the handler receives the request.

## 3.3 Master Runtime Method

On normal startup, `cmd/master/main.go` configures structured logging, creates a root context cancelled by `SIGINT` or `SIGTERM`, loads environment configuration using `config.LoadMaster`, and constructs the runtime dependencies. The worker registry is created with a stale timeout. The scheduler is configured with minimum free CPU and RAM thresholds. The SQLite job store is opened at `JOB_STORE_PATH`. The dispatcher receives the registry, scheduler, certificate directory, and shared metrics collector. The gateway receives the registry, dispatcher, job store, metrics collector, execution-client allowlist, module policy, request body limit, request cache settings, job lease settings, and shutdown timeout.

Two background loops are started by the master. The first loop periodically calls `Registry.Cleanup`, removing workers whose `LastSeen` value is older than `WORKER_STALE_TIMEOUT`. The second loop is `JobRecovery.Start`, which scans the durable job store at `JOB_RECOVERY_INTERVAL`. It dispatches queued jobs, requeues recoverable failures below the maximum attempt count, and marks expired active leases as ambiguous when the master cannot prove the final execution outcome.

The gateway's route table is defined in `Gateway.Handler`. The main endpoints are `/wasmcat/health`, `/wasmcat/ready`, `/wasmcat/metrics`, `/api/v1/execute`, `/api/v1/jobs`, `/api/v1/jobs/{request_id}`, `/internal/register`, `/internal/heartbeat`, `/internal/drain`, and `/internal/jobs/complete`. The health endpoint confirms process liveness, while readiness confirms that dependencies such as registry, dispatcher, and scheduler are initialized. The internal endpoints are used by workers and protected by mTLS identity validation.

## 3.4 Worker Runtime Method

On normal startup, `cmd/worker/main.go` configures logging, creates a cancellable root context, loads worker configuration, constructs a `WasmEngine`, constructs a `WorkerServer`, starts telemetry in a goroutine, and starts the worker HTTPS server. The telemetry loop registers the worker with the master using worker ID, advertised address, latitude, and longitude. It then sends regular heartbeats containing CPU and RAM availability.

The worker server exposes `/wasmcat/health`, `/wasmcat/ready`, `/wasmcat/metrics`, and `/invoke`. Readiness requires that the execution engine is initialized. The `/invoke` handler enforces a request body limit, decodes `ExecutionRequest`, validates the request, ensures a request ID, and executes the module through `WasmEngine.ExecuteWithDigest`.

The worker intentionally executes with a background context instead of directly tying execution to the inbound HTTP request context. This design is important for job reliability. If the master disconnects or restarts after forwarding work, the worker can still complete bounded execution and report the final state through `/internal/jobs/complete`. The execution remains bounded by the worker's own timeout, so it is not allowed to run forever.

## 3.5 Scheduling Method

The scheduler combines resource filtering with spatial routing. Each worker registers with a latitude and longitude and sends heartbeats containing free CPU percentage and free RAM in MiB. `Scheduler.SelectWorker` first calls `FilterWorkersByCapacity`, which excludes draining workers and workers below configured CPU or RAM thresholds. It then calls `FindClosestWorker`, which calculates the Haversine distance between the user coordinates and each eligible worker.

The Haversine formula is suitable because it estimates great-circle distance on the Earth's surface from latitude and longitude. This is more relevant to edge computing than pure round-robin selection because user-perceived latency is often correlated with geographic distance, especially when workers are distributed across regions. The current scheduler is simple: it does not include network latency probing, queue depth, historical execution duration, or weighted scoring. However, its implementation is deterministic, testable, and directly connected to the edge computing objective.

## 3.6 ACR Module Provisioning Method

wasmCat supports two module source modes. The first is direct module URL execution, where the request contains a `module_url` pointing to downloadable `.wasm` bytes. The second is ACR-backed execution, where the request contains a URL under an `*.azurecr.io` hostname. The dispatcher detects ACR URLs using the parsed hostname suffix.

For ACR references, the master parses the registry name, repository name, reference, and reference kind. If the request points to a manifest, the master mints a repository-scoped pull token, fetches the OCI manifest, selects a WebAssembly layer, builds the blob URL, and forwards that blob URL to the worker. If the request already points to a blob, the dispatcher can forward the original URL and digest reference. The digest is then used by the worker to validate the downloaded module and to build the module cache key.

The token provider caches repository-scoped ACR access tokens by `<registry>/<repository>`. Cached tokens are reused until they approach expiry, with a refresh skew to avoid giving workers nearly expired credentials. This reduces repeated Azure OAuth exchange overhead for consecutive executions from the same repository.

## 3.7 WebAssembly Execution Method

The worker uses wazero as an embedded WebAssembly runtime. `NewWasmEngineWithLimits` creates a wazero runtime with close-on-context behavior, initializes a compiled module cache, initializes an in-flight compile map, creates an HTTP client, normalizes execution limits, and creates a semaphore channel sized by `MaxConcurrentExecs`.

Execution begins in `ExecuteWithDigest`. The worker first rejects oversized payloads. It then tries to acquire the semaphore. If all execution slots are busy, the function returns a capacity error instead of queueing unbounded work in memory. After acquiring a slot, the worker creates an execution context with `ExecutionTimeout`. This timeout covers module fetching, compilation, instantiation, memory write, `run` invocation, memory read, and cleanup.

The module is fetched and compiled only if the digest-aware cache key is missing or expired. On a cache miss, the worker creates a GET request, attaches a bearer token when supplied, downloads up to `MaxModuleBytes + 1`, rejects oversized modules, verifies a `sha256:` digest when present, and compiles the module through wazero. The compiled module is stored in the cache and later instantiated per request. This distinction is important: the compiled module is reusable, but each execution receives a fresh module instance and therefore fresh linear memory.

The ABI expects the module to export `malloc`, `memory`, and `run`. The host calls `malloc(size)` to obtain a pointer in module memory. It writes the input bytes at that pointer using `mod.Memory().Write`. It then calls `run(ptr, len)`. The result is expected to be a `uint64` in which the high 32 bits represent the output pointer and the low 32 bits represent the output length. The host checks the output length against `MaxOutputBytes`, reads the output bytes from linear memory, and returns them as a Go string.

## 3.8 Module Cache Lifecycle Method

Caching compiled modules is essential for performance because compilation is usually more expensive than instantiation. However, an unbounded cache is unsafe for production-like operation. The implemented cache lifecycle uses a key composed of module name, module URL, and digest when available. This prevents the most serious stale-code case: if a module name remains constant but the registry resolves to a different digest, the digest-aware key separates the old compiled module from the new one.

The cache enforces three lifecycle controls. `MaxCachedModules` limits the number of entries. `MaxCacheBytes` limits the total raw module bytes represented by cached entries. `ModuleCacheTTL` limits the age of an entry from creation time. Eviction removes expired entries first and then removes the least recently used entries until count and byte limits are satisfied. The engine also tracks in-flight compilation by cache key so concurrent requests for the same cold module do not all download and compile the same bytes independently.

## 3.9 Reliability Method

The reliability design has two layers: request idempotency and durable jobs. A request may include a `request_id`; if it does not, the master generates one. The request ID is validated to prevent unsupported characters and excessive length. For durable operation, the master computes a fingerprint of the execution request and stores a `JobRecord` in SQLite. If a client repeats the same request ID with the same fingerprint, the master can return the existing job or stored response. If the same request ID is reused with different request content, the master returns a conflict.

SQLite stores jobs with status, worker ID, attempt count, maximum attempts, lease time, last error, original request JSON, optional response JSON, and timestamps. WAL mode and a busy timeout are enabled. The recovery loop lists recoverable jobs: queued jobs and active jobs whose leases have expired. Queued jobs are dispatched. Failed jobs may be retried until `JOB_MAX_ATTEMPTS`. Jobs whose final outcome cannot be proven are marked ambiguous rather than incorrectly reported as succeeded or failed.

The worker completion callback is the second reliability layer. After executing a job, the worker posts a success or failure completion to the master. This allows the master to record a result even if the original synchronous HTTP connection becomes unreliable. The callback uses the worker's mTLS identity and is validated by the master before updating the job store.

## 3.10 Security Method

The primary security mechanism is mutual TLS. Both master and worker servers load a CA certificate and require verified client certificates. The master uses the CA, master certificate, and master key to serve HTTPS. Workers use worker-specific certificates and keys. Outbound internal clients are created with the corresponding certificate, private key, and CA bundle.

The master additionally validates worker peer identity for internal worker operations. The certificate common name must match `wasmcat-worker-<workerID>`, or a DNS SAN must match either `<workerID>` or `wasmcat-worker-<workerID>`. This prevents a worker with one certificate from claiming another worker's ID during registration, heartbeat, drain, or completion callback operations.

Execution client authorization is optional but supported. If `EXECUTE_CLIENT_ALLOWLIST` is configured, the client certificate common name or DNS SAN must match an allowed identity before the client can call execution endpoints. This gives operators a path to restrict workload submission to approved client certificates.

Module source policy is another security-related mechanism. The master can enforce an allowed host list and can require module digests. The host allowlist reduces the risk of arbitrary remote module downloads. The digest requirement reduces content drift risk by forcing requests to identify expected module bytes.

## 3.11 Testing Method

Tests are stored in a separate `tests/` tree, grouped by area. This follows the project's contributor guidance and keeps test files out of implementation packages. Unit tests cover individual packages such as config loading, request ID validation, scheduler behavior, registry behavior, module policy, ACR manifest parsing, ACR token caching, worker limits, module cache lifecycle, metrics, and error contracts. Integration tests cover master-worker execution behavior. Smoke tests exercise process-level behavior.

Table 3 summarizes the implemented test layers.

| Test layer | Location | Purpose |
| --- | --- | --- |
| Unit tests | `tests/master`, `tests/worker`, `tests/shared`, `tests/config`, `tests/bootstrap` | Verify isolated functions and state transitions |
| Integration tests | `tests/integration` | Verify master-worker execution behavior across components |
| Smoke tests | `tests/smoke` | Verify binaries and process behavior at a higher level |
| Supporting checks | CI and release workflows | Run formatting, vetting, tests, and builds |

The standard local test command is `go test ./...`. The release workflow also runs `gofmt -l .`, `go mod tidy` with a clean diff check, `go vet ./...`, `go test ./...`, and release builds. This creates a reproducible baseline for future changes.

# IV/ RESULTS AND DISCUSSION

## 4.1 Implemented System Overview

The implemented system is a working native WebAssembly orchestrator with two binaries: `wasmcat-master` and `wasmcat-worker`. The master starts a control-plane HTTPS API on port `7270` by default. The worker starts an execution HTTPS API on port `7271` by default. Both processes are configured through environment variables or generated `.env` files created by `init` commands. The release model builds Linux and Windows native binaries and includes systemd service templates for Linux installation.

The most important result is that the system implements a complete request path. A client can submit an execution request to the master. The master validates the request, checks module policy, creates or reuses a request ID, records durable job state, resolves ACR metadata when necessary, selects a worker using capacity and distance, forwards the request over mTLS, receives a worker response, stores the result, and returns JSON to the client. The worker can fetch a module, compile it, cache it, instantiate it, write input into WebAssembly memory, call the module, read output memory, report completion, and respond.

This result is stronger than a minimal proof of concept because several production-oriented mechanisms are present: timeouts, retry policies, request body limits, structured error contracts, metrics endpoints, graceful shutdown, durable jobs, worker draining, cache eviction, digest verification, and release automation. However, it is not yet a complete production platform because master HA, certificate lifecycle automation, controlled benchmarks, and multi-tenant policy are incomplete.

## 4.2 Execution Lifecycle Result

The critical execution lifecycle begins at `POST /api/v1/execute`. The gateway requires POST, optionally verifies the execution client certificate identity, limits the request body, decodes JSON, validates required fields, checks module policy, and ensures a valid request ID. Because the current master startup always opens a SQLite job store, the durable path is used in normal operation.

The master computes an execution fingerprint and attempts to create a new job record. If creation succeeds, the job enters a dispatching path. If a record already exists with the same fingerprint, the gateway decides based on existing status. A completed job returns the stored response. An active job returns `409 request_in_progress`. A failed job may be eligible for another attempt if it has not reached the configured maximum. A record with the same request ID but different fingerprint returns `409 request_id_conflict`.

The dispatcher normalizes module URL fields and then handles ACR-specific logic. For ACR URLs, it parses the reference, requests a repository-scoped token, fetches a manifest when needed, selects the WebAssembly layer, rewrites the module URL to the blob URL, and sets the module digest. It then retrieves schedulable workers from the registry. Schedulable workers exclude stale workers and draining workers. The scheduler filters by CPU and RAM, calculates Haversine distances, and returns the closest eligible worker.

The dispatcher sends the request to `https://<worker>/invoke`. If the selected worker has a transport error or returns `502`, `503`, or `504`, the dispatcher removes that candidate and tries another eligible worker. This behavior is conservative. It avoids replaying requests that may have already executed and returned an application-level failure, while still handling failures that likely occurred before execution.

The worker handles `/invoke`, validates the request, and calls the engine. The engine checks payload size and concurrency capacity, creates a timeout, fetches or reuses the compiled module, instantiates the module, writes the input string into WebAssembly memory, calls `run`, reads the output, and returns an `ExecutionResponse`. The worker also reports the completion to the master. The master stores success or failure in SQLite and returns the response to the client.

## 4.3 Scheduling and Edge Awareness

The implemented scheduler demonstrates the core edge computing concept of proximity-aware placement. Unlike round-robin scheduling, which distributes load without considering user location, wasmCat includes the user's latitude and longitude in the execution request. Workers also register latitude and longitude. The scheduler calculates the physical distance from the user to each eligible worker and selects the closest one.

This is a defensible academic design because the project is about edge execution rather than general cloud batch scheduling. In an edge setting, the nearest worker is often a reasonable first approximation for lower network latency. The code also avoids selecting workers that report insufficient CPU or RAM availability. This means the scheduler is not purely geographic; it is capacity-aware.

The limitation is that geographic distance is not the same as network latency. Real networks are shaped by routing policy, peering, congestion, wireless conditions, and cloud provider topology. A worker geographically close to a user may still be slower than a farther worker with a better route. Therefore, the current scheduler should be understood as a first scheduling strategy, not the final policy. Future work should combine Haversine distance with measured round-trip time, queue depth, historical execution latency, module cache locality, and worker error rate.

## 4.4 WebAssembly Runtime Behavior

The worker runtime design is suitable for short-lived module execution. The most important performance mechanism is compiled module caching. The worker does not compile the module for every request when the cache key is already present. Instead, it reuses a compiled module and instantiates a new module instance for each request. This gives each execution isolated linear memory while avoiding repeated compilation cost.

The engine also avoids disk I/O in the hot module path. Modules are downloaded into memory, optionally verified by digest, compiled by wazero, and cached as compiled modules. This fits the edge use case because small modules can be transferred and compiled quickly, and repeated invocations benefit from cache hits. The system should not claim a specific cold-start latency until measured under controlled conditions, but the implementation supports low overhead by avoiding container startup, external runtime processes, and disk-based staging.

The ABI is simple and transparent. The host and module exchange strings through linear memory using pointer and length values. This simplicity is useful for an academic prototype because it makes memory safety boundaries explicit. However, the ABI is strict. A module that does not export `malloc`, `memory`, or `run` will fail. A module that returns an invalid pointer or length will cause a read error. A module that returns output larger than the configured limit will be rejected. Future versions should define a more formal ABI specification, include tooling to validate modules before deployment, and possibly support WASI-style interfaces for richer workloads.

## 4.5 Module Cache Lifecycle Result

The module cache lifecycle feature addresses a practical production concern. The original simple design of caching compiled modules forever by module name would be efficient but unsafe. If a remote module changed while keeping the same logical name, the worker could continue executing an old compiled module. The current implementation improves this by using digest-aware cache keys and eviction policies.

The cache key includes module name, module URL, and digest. When an ACR manifest resolves to a layer digest, that digest becomes part of the worker cache identity. If the registry content changes and a new digest is resolved, the worker sees a different cache key and compiles the new bytes. This directly addresses stale module execution risk for digest-pinned or manifest-resolved modules.

The cache also prevents unbounded memory growth. `MaxCachedModules` controls entry count, `MaxCacheBytes` controls represented module byte size, and `ModuleCacheTTL` controls entry age. The engine evicts expired entries and then removes least recently used entries until limits are satisfied. This is not a complete memory accounting system because compiled module memory may differ from raw byte size, but it is a reasonable operational approximation for a Go-level cache.

## 4.6 ACR Integration Result

The ACR implementation provides realistic private module distribution. The dispatcher identifies Azure Container Registry URLs, parses repository and reference information, obtains an ACR pull token through Azure identity and OAuth exchange endpoints, resolves manifests to WebAssembly layers, and sends workers the blob URL and bearer token needed for download. This separates registry-specific authentication from worker execution.

The implementation handles three reference kinds: repository-style references, manifest references, and blob references. Manifest references are the most important because they allow the master to inspect OCI metadata and select a layer. If the manifest has exactly one layer, that layer is selected. If it has multiple layers, the selector searches for known WebAssembly media types such as `application/wasm` or `application/vnd.module.wasm.content.layer.v1+wasm`.

The strongest part of this design is that digest information flows from registry metadata to worker execution. This supports both integrity checking and correct caching. The main limitation is operational dependency on Azure identity configuration. The master host must have a valid Azure identity, such as Azure CLI login, managed identity, environment credentials, or another credential source supported by `DefaultAzureCredential`. Token caching is process-local, so a restarted master must mint tokens again.

## 4.7 Security Result

The implemented mTLS design provides a strong baseline for internal node communication. Because both master and worker servers require verified client certificates, internal endpoints are not simple unauthenticated HTTP APIs. The master verifies worker identity against the claimed worker ID during registration, heartbeat, drain, and completion callback operations. This reduces the risk of a process impersonating a worker by submitting JSON with another worker's ID.

The optional execution-client allowlist is important because `/api/v1/execute` is a sensitive endpoint. When configured, only certificates whose common name or DNS SAN matches an allowed value can submit execution requests. This is not a complete user authorization system, but it is a practical certificate-based control for thesis and early deployment scenarios.

The security limitations are also clear. Development certificate generation is useful for local testing, but production needs proper certificate issuance, rotation, revocation, and storage. Private keys must not be committed to the repository. The system does not yet implement per-module authorization, tenant isolation, signed module policy, or a central audit log. The worker executes WebAssembly in a sandbox, but the host still decides what module URL is fetched. Therefore, module source policy and digest requirements should be enabled in production-like tests.

## 4.8 Reliability and HA Discussion

wasmCat currently implements job reliability foundations, not full high availability. The most important improvement is the SQLite-backed job store. Jobs survive master process restarts because they are stored on disk. Request IDs prevent accidental duplicate execution semantics from being hidden. Fingerprints prevent request ID reuse for different module content. Job leases allow the master to identify active jobs whose outcome became uncertain. Worker completion callbacks allow the master to record results even when the original request path is interrupted.

The recovery loop provides a clear state machine. Queued jobs can be dispatched. Failed jobs can be retried until the maximum attempt count. Expired active jobs can be marked ambiguous when the master cannot safely infer whether execution completed. This is academically important because ambiguous is more honest than falsely marking a job failed. In distributed systems, a coordinator may lose contact with a worker after dispatching work. If the work may have executed, automatic replay can violate at-most-once semantics. wasmCat therefore distinguishes recoverable pre-execution failures from uncertain post-dispatch outcomes.

However, HA is not complete. The master remains a single active control-plane process. SQLite is local to that process and not replicated. If the master host is lost, the job database is lost unless the file is backed up or stored on durable shared storage. There is no leader election, no follower master, and no consensus protocol. The worker registry is in memory, so live worker membership must be rebuilt through registration and heartbeat after restart. These limitations should be openly defended: the project prioritizes job reliability first, while full control-plane HA is future work.

## 4.9 Observability and Operations Result

The system includes practical observability mechanisms. Both master and worker expose health, readiness, and metrics endpoints under `/wasmcat/...`. The metrics model includes request counters, dispatch success and failure counters, reschedule counters, request idempotency counters, worker execution success and failure counters, module digest failure counters, active worker counts, worker state counts, oldest heartbeat age, and worker cache statistics.

Structured logging uses Go's `log/slog`. HTTP middleware records request metadata and status codes. The dispatcher logs selected workers, attempts, dispatch completion, and failure conditions. The worker logs execution start, completion, failure, and module cache events. These logs are important for operating a distributed system because execution spans multiple processes.

Installation is also documented. The project avoids requiring Docker for the orchestrator itself. Native release automation builds platform-specific binaries and systemd service templates. Bootstrap commands create configuration files and certificate directories. This approach supports the project's design goal: an edge orchestrator should be easy to install on a host without depending on another container orchestrator.

## 4.10 Testing Result

The repository contains tests across the major subsystems. Master tests cover registry behavior, scheduling capacity filters, dispatch errors, worker identity validation, health endpoints, metrics, request idempotency, job creation and querying, durable job storage, job recovery, module policy, ACR manifest parsing, ACR token caching, body limits, draining, and execution client authorization. Worker tests cover engine behavior, module cache lifecycle, execution limits, health endpoints, metrics, server errors, and completion callbacks. Shared tests cover address generation, HTTP client behavior, retry behavior, error contracts, request ID rules, and server defaults.

This coverage is valuable because the system has many failure paths. For example, ACR token behavior must be tested without contacting Azure; the implementation supports injectable minting and clock functions for that purpose. Job recovery must be tested with deterministic states and lease times. Cache lifecycle must be tested for count, byte, TTL, and digest key behavior. Worker execution limits must be tested separately from HTTP server behavior.

The current test suite is appropriate for a thesis implementation, but production maturity would require additional categories. Race tests should be run regularly with `go test -race ./...`. Multi-process integration tests should run master and worker binaries with generated certificates. Fault injection should kill worker and master processes during execution. ACR integration should be tested against a real registry in a controlled environment. Performance benchmarks should measure cold compile latency, warm cache latency, module fetch latency, scheduling overhead, and maximum concurrent throughput.

## 4.11 Production Readiness Assessment

The current project is near usable as an experimental or thesis demonstration platform. It can be built, initialized, started, and tested locally. It has a real master-worker execution path, mTLS, ACR integration, scheduling, module execution, caching, reliability primitives, and documentation. It is therefore suitable for a final-year thesis implementation and for controlled real-environment testing on a small number of virtual machines.

It is not yet ready for unsupervised production use. The main missing feature is full high availability for the control plane. A production orchestrator should tolerate master host failure without losing control-plane availability. It should replicate durable job state or store it in an external highly available database. It should support certificate rotation and revocation. It should include stronger module signing and admission controls. It should include performance benchmarks with clear methodology. It should include stronger worker resource isolation, because current CPU/RAM telemetry reflects host-level availability and does not enforce per-execution CPU or memory quotas at the operating-system level.

Table 4 summarizes current limitations and future work.

| Area | Current state | Required improvement |
| --- | --- | --- |
| Master HA | Single active master with local SQLite | Replicated job store, leader election, or external HA database |
| Worker isolation | WebAssembly sandbox plus Go-level limits | Stronger CPU and memory enforcement per execution |
| Certificate operations | Dev generation and static files | Production CA workflow, rotation, revocation, secret storage |
| Scheduling | Capacity plus Haversine | Add RTT, cache locality, queue depth, and historical latency |
| ACR integration | Token cache and manifest resolution | Real registry integration tests and operational credential guide |
| Benchmarks | Mechanism-based analysis | Controlled cold/warm latency and throughput measurements |
| Multi-tenancy | Optional client cert allowlist | Tenant policy, per-module authorization, audit logging |

## 4.12 Academic Discussion

The most defensible academic contribution of wasmCat is not that it replaces Kubernetes. It does not. The contribution is a clear, compact implementation of a WebAssembly-oriented orchestration path where the important distributed systems concerns are visible and analyzable. The codebase demonstrates how a control plane can receive requests, validate identities, maintain worker state, schedule based on spatial information, resolve registry artifacts, and drive sandboxed execution on remote workers.

The design also shows the tradeoff between simplicity and availability. An in-memory registry is fast and easy to reason about, but it is not durable. SQLite job state improves reliability without introducing a full database cluster, but it does not solve multi-master failover. Haversine routing aligns with edge computing, but it does not measure actual latency. mTLS provides strong machine authentication, but it does not by itself solve application-level authorization. WebAssembly sandboxing isolates module memory, but it does not automatically provide complete resource governance.

These tradeoffs are suitable for thesis defense because they show engineering judgment. A final-year project should not hide limitations. Instead, it should identify which limitations are acceptable for the prototype and which ones must be solved before production. wasmCat's current implementation is credible because its design choices are explicit, its behavior is testable, and its future work follows logically from the implemented architecture.

# V/ CONCLUSION & PERSPECTIVE

## 5.1 Conclusion

This thesis presented wasmCat, or EdgeNet, a native decentralized WebAssembly orchestration system written in Go. The project addresses the problem of running portable, sandboxed WebAssembly modules on edge workers without relying on Docker, containerd, Kubernetes, or a separate external WebAssembly runtime. The system is structured as a master-worker platform with a control plane for request handling and scheduling, and a data plane for module fetching and execution.

The implementation achieved the main objectives. The master exposes secure APIs, tracks workers, performs capacity-aware Haversine scheduling, resolves ACR module references, enforces module policies, records durable jobs in SQLite, and handles request idempotency. The worker registers with the master, reports telemetry, serves an mTLS-protected invocation endpoint, fetches modules, verifies digests, caches compiled modules with lifecycle controls, runs modules using wazero, bridges strings through WebAssembly linear memory, and reports completion callbacks. The repository also includes installation documentation, release automation, systemd templates, metrics, structured errors, and a broad test suite.

The system is appropriate for thesis demonstration and controlled real-environment testing. It shows how a small Go codebase can implement core orchestration behavior for WebAssembly modules. It also makes important distributed systems tradeoffs visible: centralized control plane state, conservative retry behavior, durable job leases, worker heartbeats, and certificate-based trust.

## 5.2 Perspective and Future Work

The next development direction should prioritize control-plane availability. The current job reliability features are useful, but the master remains a single active node. A production-oriented version should introduce either an external highly available database for job state or a replicated master design with leader election. Worker registry state should be reconstructible from heartbeats, but job state must survive host failure.

The second direction is rigorous performance evaluation. The project should define a benchmark suite that measures direct URL cold starts, ACR manifest resolution overhead, ACR blob download overhead, warm cache execution, scheduler overhead, worker saturation behavior, and failure recovery time. Results should distinguish compilation time, instantiation time, network fetch time, and module execution time.

The third direction is security hardening. Production deployment should include certificate rotation, revocation, secret storage guidance, signed module verification, strict digest requirements, module host allowlists, and per-client authorization policies. The current mTLS foundation is strong, but production security requires lifecycle management and auditability.

The fourth direction is richer scheduling. The scheduler should combine geographic distance with measured latency, cache locality, worker queue depth, historical success rate, and module-specific constraints. This would turn the current deterministic policy into a more adaptive edge scheduler.

The fifth direction is a more formal module ABI. The current `malloc` and `run` ABI is simple, but modules must match it exactly. A production platform should provide a module SDK, validation tooling, and versioned ABI contracts. It may also consider WASI support for workloads that need standardized system interfaces.

Overall, wasmCat demonstrates that a native WebAssembly orchestrator can be built with a compact architecture while still incorporating security, scheduling, registry integration, caching, reliability, testing, and deployment concerns. Its current state is strong for academic use and promising as a foundation for further production-grade research.

---

# REFERENCES

1. The Go Authors. The Go Programming Language Documentation. Available from the official Go documentation.
2. Tetrate Labs. wazero: A zero-dependency WebAssembly runtime for Go developers. Available from the official wazero project documentation.
3. WebAssembly Community Group. WebAssembly Core Specification. Available from the official WebAssembly specification.
4. Microsoft. Azure Container Registry Documentation. Available from Microsoft Azure documentation.
5. Open Container Initiative. OCI Image Specification. Available from the official OCI specification repository.
6. SQLite Consortium. SQLite Documentation. Available from the official SQLite documentation.
7. Fielding, R. T. Architectural Styles and the Design of Network-based Software Architectures. Doctoral dissertation, University of California, Irvine, 2000.
8. Tanenbaum, A. S., and Van Steen, M. Distributed Systems: Principles and Paradigms. Pearson.
9. Coulouris, G., Dollimore, J., Kindberg, T., and Blair, G. Distributed Systems: Concepts and Design. Pearson.
10. Burns, B., Beda, J., and Hightower, K. Kubernetes: Up and Running. O'Reilly Media.

---

# APPENDICES

## Appendix A: Public API Summary

The master exposes `POST /api/v1/execute` for synchronous execution. The request body contains `request_id`, `module_name`, `module_url` or `module_registry_url`, optional `module_digest`, `user_lat`, `user_lon`, and `payload`. The response contains `request_id`, `result`, `execution_time_ms`, and `executed_on_node_id`.

The master exposes `POST /api/v1/jobs` for asynchronous durable job creation. It accepts the same request shape as synchronous execution but returns a job record instead of waiting for execution. The master exposes `GET /api/v1/jobs/{request_id}` for job status retrieval.

Internal master endpoints include `POST /internal/register`, `POST /internal/heartbeat`, `POST /internal/drain`, and `POST /internal/jobs/complete`. These endpoints are intended for worker use and are protected by mTLS.

The worker exposes `POST /invoke` for execution requests forwarded by the master. The worker also exposes `/wasmcat/health`, `/wasmcat/ready`, and `/wasmcat/metrics`.

## Appendix B: Major Configuration Parameters

Table 2 lists major configuration parameters.

| Variable | Default | Meaning |
| --- | --- | --- |
| `MASTER_PORT` | `7270` | HTTPS port used by the master |
| `WORKER_PORT` | `7271` | HTTPS port used by the worker |
| `CERT_DIR` | `./certs` | Directory containing CA, certificate, and key files |
| `WORKER_STALE_TIMEOUT` | `30s` | Maximum heartbeat age before registry cleanup removes a worker |
| `MIN_WORKER_CPU_FREE` | `0` | Minimum free CPU percentage required for scheduling |
| `MIN_WORKER_RAM_FREE_MB` | `0` | Minimum free RAM required for scheduling |
| `JOB_STORE_PATH` | `./wasmcat-jobs.db` | SQLite file for durable job state |
| `JOB_MAX_ATTEMPTS` | `3` | Maximum durable attempts per request |
| `JOB_LEASE_TTL` | `30s` | Lease duration for active dispatch state |
| `MAX_MODULE_BYTES` | `10 MiB` | Maximum downloaded module size |
| `MAX_PAYLOAD_BYTES` | `1 MiB` | Maximum execution payload size |
| `MAX_OUTPUT_BYTES` | `1 MiB` | Maximum WASM output size |
| `MAX_CONCURRENT_EXECS` | `4` | Worker execution semaphore size |
| `MAX_CACHED_MODULES` | `128` | Maximum worker compiled module cache entries |
| `MAX_CACHE_BYTES` | `256 MiB` | Maximum represented raw module bytes in cache |
| `MODULE_CACHE_TTL` | `30m` | Maximum age of compiled module cache entries |

## Appendix C: Operating Commands

The native release build can be produced with:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\build.ps1 -Version 0.1.0
```

The local quality gate is:

```powershell
go mod tidy
gofmt -w .
go vet ./...
go test ./...
go build ./...
```

The master can be initialized with:

```bash
wasmcat-master init \
  --config-dir /etc/wasmcat \
  --cert-dir /etc/wasmcat/certs \
  --port 7270 \
  --job-store-path /etc/wasmcat/wasmcat-jobs.db
```

The worker can be initialized with:

```bash
wasmcat-worker init \
  --worker-id worker-us-01 \
  --master-url https://master.example.com:7270 \
  --advertise-address worker-us-01.example.com:7271 \
  --config-dir /etc/wasmcat \
  --cert-dir /etc/wasmcat/certs
```

## Appendix D: Example Execution Request

```json
{
  "request_id": "req_thesis_demo_001",
  "module_name": "hello",
  "module_url": "https://example.com/modules/hello.wasm",
  "module_digest": "sha256:<expected digest>",
  "user_lat": 21.0278,
  "user_lon": 105.8342,
  "payload": "{\"message\":\"hello wasmCat\"}"
}
```

## Appendix E: Defense-Oriented Questions

Question 1: Why does wasmCat use WebAssembly instead of containers?

Answer: The project targets small edge workloads where portability, low startup overhead, and simple installation are more important than full container ecosystem compatibility. WebAssembly modules are compact and sandboxed. With wazero, the runtime is embedded directly in Go, so workers do not need Docker, containerd, or a separate native runtime installation.

Question 2: Why is the master not fully highly available?

Answer: The thesis prioritizes job reliability before full control-plane HA. SQLite durable jobs, request IDs, leases, and worker callbacks reduce execution loss across process restarts. Full HA requires replicated state and leader election, which are substantial distributed systems features and are identified as future work.

Question 3: How does the system avoid executing stale remote modules?

Answer: ACR manifest resolution produces a layer digest, and the worker cache key includes the digest. If the remote module changes and the digest changes, the worker compiles and caches a new entry. The worker can also verify a supplied `sha256:` digest against downloaded bytes before compilation.

Question 4: Why is mTLS used instead of only JWT?

Answer: mTLS authenticates machines at the transport layer and protects every internal request before it reaches handler logic. This is useful for worker registration, heartbeat, dispatch, and completion callbacks. JWT could be added for user-level authorization, but mTLS is a strong foundation for node identity.

Question 5: What is the biggest current limitation of the execution ABI?

Answer: The ABI requires modules to export `malloc`, `memory`, and `run(ptr, len)`. This is simple and efficient, but not flexible. Modules that do not follow the exact contract fail. Future work should include SDK tooling, validation, and versioned ABI definitions.
