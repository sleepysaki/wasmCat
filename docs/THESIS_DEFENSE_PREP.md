# Thesis Defense Preparation — wasmCat

This document supports `slide/slide.tex` (19 slides, ~15 minutes) with a spoken
presentation script and a practice question bank. It complements, and does not
duplicate, `docs/THESIS_DRAFT.md` Appendix E — read that appendix too; the two
are meant to be studied together.

Pacing target: 15 minutes talk + 10-15 minutes Q&A. Numbers below assume ~45-60s
per slide; cut the italic *(if short on time, cut this)* lines first if you are
running long in rehearsal.

---

## Part 1 — Presentation Script

### 1. Title (15s)
"Good morning/afternoon. My thesis is *wasmCat*, a geo-aware WebAssembly
serverless orchestrator, supervised by Eng. Trần Tuấn Anh and Assoc. Prof. Trần
Giang Sơn."

### 2. Outline (15s)
"I'll cover the problem context, the system design method, the execution
lifecycle, measured results, limitations, and conclusion."

### 3. Context (45s)
"Two trends motivate this work. Edge computing puts compute close to the user
because for latency-sensitive workloads, *where* code runs matters as much as
how much compute it gets. Serverless lets a developer submit a function, not a
server — the platform picks the node, runs it, and returns a result; the
developer never manually chooses a machine. wasmCat combines both: it's a
native Go master-worker orchestrator that runs WebAssembly functions on
geo-distributed edge workers, with no container runtime anywhere in the path."

### 4. Why WebAssembly, not Containers? (45s)
"Containers carry real overhead for small, short-lived edge functions: you pull
an image, unpack layers, set up namespaces and cgroups, and the control plane
is heavy — and none of that scheduling logic is geography-aware. WebAssembly
with wazero gives a single `.wasm` artifact that runs on any host, sandboxed to
linear memory only, compiled once and instantiated per request in
microseconds, inside a pure-Go runtime that's *embedded in the worker process*
— no separate runtime daemon. The gap I identified: edge functions need fast
sandboxed startup, minimal node dependencies, *and* location-aware placement.
No existing lightweight tool combined all three."

### 5. Contributions (45s)
"Six contributions: a Go-native master-worker orchestrator with a clean
control-plane/data-plane split; capacity-aware geo-routing using the Haversine
formula; an embedded wazero engine with two execution ABIs, a digest-aware
module cache, and strict resource limits; a security model built on mTLS with
certificate-bound worker identity and module digest verification; a
reliability layer with durable SQLite jobs, idempotency, and crash recovery;
and operator tooling — a CLI, a web dashboard, and just-in-time module
provisioning from Azure Container Registry."

### 6. System Architecture (30s)
"*(point at diagram)* The master exposes a gateway, holds an in-memory worker
registry, runs the geo-scheduler and dispatcher, resolves ACR references, and
persists jobs in SQLite. Every worker registers, sends heartbeats, and
receives dispatched work over mutual TLS. This is a deliberate control-plane /
data-plane split: the master never executes user code."

### 7. Data Model: Input → Output (45s)
"Every execution is a request/response pair. The request carries a
`request_id` for idempotency, the module reference, a JSON or raw payload, and
the caller's coordinates for routing. The response carries the result,
measured execution time, and — importantly — *which worker* actually ran it,
so the client can observe the routing decision. This is `POST
/api/v1/execute`: JSON in, JSON out, same request ID round-trips."

### 8. Worker Model & Registry (45s)
"The registry is a `map[string]WorkerNode` behind a `sync.RWMutex` — many
concurrent reads for scheduling, rare writes for register/heartbeat/state
change, so the mutex choice matters for throughput. Static metadata like
coordinates is set once at registration; heartbeats every 5 seconds only carry
CPU and RAM so they stay cheap. A background loop evicts workers that stop
heartbeating, so the registry self-heals without an operator manually
removing dead nodes."

### 9. Geo-Aware, Capacity-Aware Scheduling (45s)
"Scheduling is two stages. First, filter: drop draining workers, then drop
anything below the configured free CPU or RAM threshold. Second, score the
remaining eligible workers by great-circle distance using Haversine, and pick
the nearest. *(honest framing, say deliberately)* Distance is a *proxy* for
latency, not a measurement of it — I say this explicitly because incorporating
real RTT, queue depth, and cache locality is future work, not something I'm
claiming to have solved."

### 10. Two Execution Modes — Byte-level I/O (45s)
"Two ABIs, auto-detected from the module's exports. The custom wasmCat ABI is
minimal: the module exports `memory`, `malloc`, and `run`; the host writes the
payload into linear memory, calls `run(ptr, len)`, and gets back one packed
`uint64` — high 32 bits are the output pointer, low 32 bits the output length.
It's simple and fast but requires the module to follow that exact contract.
The second mode is standard WASI `wasip1`: the module exports `_start`,
receives the payload on stdin, and the result comes back on stdout — any
Rust, Go, TinyGo, or C toolchain that targets `wasip1` runs unmodified. Both
modes share the same digest-aware compiled-module cache and get a fresh,
isolated instance per request."

### 11. Security Model (45s)
"Four points. mTLS is enforced everywhere — a handler never runs before the
client certificate is verified. Certificate-bound identity means a worker
claiming ID `worker-vn-01` must present a certificate whose CN or DNS SAN
actually matches that ID, which stops one compromised or misconfigured worker
from impersonating another. Module integrity: SHA-256 is verified before
compilation, and the digest is part of the cache key, so a changed remote
module can never silently reuse stale compiled code. And ACR credentials are
repository-scoped, pull-only, and short-lived. One more boundary worth
stating: the browser dashboard never talks to the master directly — a local
backend process holds the mTLS client certificate, so private keys never
reach the browser."

### 12. Synchronous Execution: Input → Process → Output (45s)
"*(walk the concrete example)* Client sends a request with `request_id`,
module name, JSON payload, and coordinates. The master validates it, checks
idempotency, resolves ACR if needed, asks the scheduler for the nearest
eligible worker, and forwards it over mTLS to that worker's `/invoke`
endpoint. The worker fetches/compiles if it's a cache miss, runs it, and
returns output. In this concrete run, a JSON-formatting module on
`worker-vn-01` took under 3 milliseconds warm, and the response tells the
client exactly which node executed it."

### 13. Durable Job Process & State Machine (45s)
"For the asynchronous path, `POST /api/v1/jobs` returns `202` immediately and
persists the job in SQLite. A background recovery loop leases and dispatches
queued jobs. Idempotency here means: same `request_id` with the same
fingerprint replays the stored result; same ID with a *different* fingerprint
is rejected with `409` rather than silently executed twice. *(this is the
line examiners probe — say it precisely)* If a job's lease expires while it's
`dispatching` or `running` and the worker never confirmed completion, the
recovery loop marks it `ambiguous` — it does **not** blindly retry, because
retrying could double-execute a side-effecting function. That's a deliberate
correctness-over-convenience choice."

### 14. JIT Module Provisioning from Azure ACR (30s)
"When the master sees an `*.azurecr.io` reference, it uses
`DefaultAzureCredential` to get a repository-scoped `pull` token, fetches the
OCI manifest, picks the `application/wasm` layer, and forwards the resolved
blob URL plus a short-lived bearer token and digest to the worker. The worker
downloads and compiles it — and critically, the worker itself never holds any
Azure credential, only that one per-request scoped token."

### 15. Result 1 — Geo-Routing Works End to End (45s)
"*(read table)* Same request body, two different caller coordinates: from
Singapore coordinates the scheduler picked `worker-vn-01` in Southeast Asia;
from Seattle coordinates it picked `worker-west-us-01` in the US — same code,
routing decided purely by location and capacity. I also verified draining:
once a worker is marked draining it's excluded from selection and traffic
shifts to the remaining ready worker. And I verified the mTLS boundary
directly: a plain `curl` without a client certificate is rejected at the
transport layer, before any application logic runs."

### 16. Result 2 — Cache Dominates Latency (45s)
"*(read table)* Cold execution — first request for a module version — costs
about 2.1 seconds for a direct HTTP URL and 2.4 seconds through ACR, dominated
by download and wazero compilation. Warm execution, reusing the compiled
cache, drops to 7.5 milliseconds direct and 2.9 milliseconds through ACR —
because on a warm path it's just a fresh module instantiation, not a
recompile. That cost is paid once per module version per worker, and there's
no image pull or namespace setup on *any* request, cold or warm."

### 17. Limitations (30s)
"I want to state these directly rather than have them surface as questions.
Single master — SQLite gives local durable recovery, not high availability.
The registry is in-memory and rebuilt from heartbeats after a restart.
Haversine is not measured RTT — no congestion or cache-locality awareness yet.
Isolation is host-level CPU/RAM, not per-invocation cgroups or fuel metering.
And the workload model is stateless functions — no sockets or filesystem
access beyond what `wasip1` exposes."

### 18. Conclusion & Future Work (30s)
"wasmCat demonstrates a compact native Go system doing capacity- and
location-aware scheduling, JIT ACR provisioning, mTLS security, and durable
idempotent jobs, validated on a real two-region Azure deployment. Future work:
an HA control plane with leader election and an external job store; smarter
scheduling using measured RTT, queue depth, and cache locality; stronger
isolation via fuel metering and admission control; and a wider execution
model — an HTTP-handler ABI, WASI-HTTP, and eventually the component model."

### 19. Thank You
"Thank you — happy to take questions."

---

## Part 2 — Practice Question Bank

Organized by topic so you can drill weak areas. Answers are deliberately
precise about what the code *actually* does (verified against
`internal/master` and `internal/worker`), not aspirational — examiners probe
exactly where a slide is vaguer than the implementation.

### A. Motivation & Positioning

**Q1. Why not just use Kubernetes with a topology-aware scheduler, or
Cloudflare Workers / Fastly Compute?**
wasmCat targets the specific gap of a *self-hostable, dependency-light*
orchestrator: Kubernetes' topology-aware scheduling requires the whole K8s
control plane (etcd, kubelet, CNI) which is heavy for a handful of edge VMs,
and commercial edge-function platforms are closed, hosted services you cannot
run on your own infrastructure or inspect for a thesis. wasmCat is two Go
binaries plus SQLite, installable on a bare VM, with the routing decision made
explicitly and inspectably in ~50 lines of scheduler code.

**Q2. Isn't Haversine distance a weak proxy for latency? Why not just measure
RTT directly?**
It is a proxy, stated as such on the limitations slide. Geographic distance
correlates with RTT but ignores routing path, congestion, and peering. I chose
it as a baseline because it requires zero extra infrastructure (no active
probing between every worker pair) and is deterministic and testable. Real RTT
measurement, e.g. periodic pings from workers to representative regions, is
explicitly future work (§5.2 / slide 17).

**Q3. What's the actual novel contribution versus assembling existing pieces
(wazero, mTLS, SQLite)?**
The contribution is the *system*, not any single library: the specific
combination of a capacity-filtered geo-scheduler, a digest-aware compiled
module cache shared across two ABIs, certificate-bound worker identity tied
into idempotent durable job recovery, and JIT credential-scoped ACR
provisioning — engineered and measured end-to-end on a real two-region
deployment. Each piece individually exists elsewhere; the integration and the
correctness properties (e.g. `ambiguous` state, digest-keyed cache) are the
engineering contribution.

### B. Architecture & Control/Data Plane Split

**Q4. Why is the registry in-memory instead of persisted like jobs are?**
Worker liveness is real-time cluster state — a worker that stopped
heartbeating 30 seconds ago is *not* schedulable regardless of what a
database said 5 minutes ago. Persisting it would only add write overhead
without improving correctness, since a restarted master must re-verify
liveness via heartbeats anyway. Jobs are different: they represent durable
intent ("this request must eventually run or be reported as ambiguous") that
must survive a master restart, so they belong in SQLite.

**Q5. What happens if the master process crashes mid-dispatch?**
For the synchronous `/api/v1/execute` path with no job store configured,
the in-flight request is simply lost — the client sees a connection error.
For the durable `/api/v1/jobs` path, the job was already persisted as
`queued`/`dispatching` before the crash; on restart, `JobRecovery` re-scans
recoverable jobs. If it was still `queued`, it gets redispatched. If it was
`dispatching`/`running` with a lease that has now expired and no completion
was recorded, it's marked `ambiguous` rather than blindly retried, because the
worker may have already executed it (`internal/master/job_recovery.go`).

**Q6. Why `sync.RWMutex` for the registry and not a `sync.Map` or channel-based
actor pattern?**
The registry's access pattern is read-heavy (every dispatch calls
`GetSchedulableWorkers`) and write-light (register/heartbeat/state-change,
roughly every 5s per worker). `RWMutex` lets concurrent reads proceed without
blocking each other, which is the dominant case; `sync.Map` optimizes for
disjoint-key access patterns that don't fit here, and a channel-based actor
would serialize reads unnecessarily.

### C. Scheduling

**Q7. What exactly does "capacity-aware" filter on, and what are the
thresholds?**
`FilterWorkersByCapacity` drops any worker in the `draining` state, then drops
workers whose most recent heartbeat reports `CPUFree` below
`MIN_WORKER_CPU_FREE` or `RAMFreeMB` below `MIN_WORKER_RAM_FREE_MB` (both
configurable via environment). Only the surviving set is passed to the
Haversine distance scoring in `FindClosestWorker`
(`internal/master/scheduler.go`).

**Q8. What happens if no worker meets the capacity thresholds?**
`SelectWorker` returns an explicit error — "no active workers meet capacity
requirements" — which propagates back to the dispatcher and then the client as
a failed dispatch. There's no silent fallback to an overloaded worker.

**Q9. Is the scheduling decision made once per request, or can it change
mid-flight?**
It can be *re-made* within the same request if the first choice fails
transiently. The dispatcher re-runs `Scheduler.SelectWorker` after any
retryable failure, but only against a shrinking local candidate list (the
failed worker removed) — it doesn't mutate the shared registry, since
heartbeat cleanup owns registry membership (`internal/master/dispatcher.go`).

### D. Dispatch, Retries, and Failure Semantics

**Q10. When does the dispatcher retry against another worker, and when does
it give up immediately?**
Retry only happens for errors classified `retryable`: a transport-level
failure (connection refused, timeout — the request never reached the worker
or never got a response) or specific worker HTTP statuses `502`/`503`/`504`
(gateway-style, meaning the worker path was temporarily unavailable). Any
other worker response — including execution errors returned with a normal
status — is *not* retried, because the module may already have run
(`isRetryableDispatchError`, `isRetryableWorkerStatus` in
`internal/master/dispatcher.go`). This is the same non-retry-on-ambiguity
principle as the job recovery loop, applied at the synchronous dispatch layer.

**Q11. Why not just retry every failure up to N times — isn't that simpler
and more resilient?**
Because a side-effecting module (e.g. one that calls out to a payment API)
could be executed twice if the worker actually ran it but the response was
lost after a 200. Blind retry trades a rare "your request wasn't served"
failure for a much worse "your request may have run twice" failure. The
thesis explicitly favors the former.

### E. Execution ABI & wazero

**Q12. How does auto-detection decide between the wasmcat ABI and WASI?**
`resolveModuleABI` inspects `compiled.ExportedFunctions()`: if the module
exports `run`, it's treated as wasmcat ABI; else if it exports `_start`, it's
treated as WASI; if neither, execution fails with an explicit error asking the
caller to set `abi` explicitly (`internal/worker/engine.go`). A caller can
also force the mode via the `abi` field to skip detection.

**Q13. Why keep the custom ABI at all instead of standardizing on WASI?**
The custom ABI is a deliberately minimal contract (`malloc`+`memory`+`run`,
one packed `uint64` return) that's cheaper to implement for tiny modules
written directly in WAT or a minimal SDK, and avoids the overhead of setting
up stdin/stdout streams for trivial request/response functions. WASI is kept
alongside it — not as a replacement — specifically so standard toolchain
output (Rust, TinyGo, Go, C targeting `wasip1`) runs unmodified. Slide 10's
"auto-detect" framing and the CLAUDE.md rule ("prefer an explicit new mode
rather than weakening the current ABI path") both reflect this: add
capability, don't dilute the existing contract.

**Q14. Is a new wazero instance created per request? Isn't that slow?**
Compilation (the expensive step) happens once per module version and is
cached; each request only gets a fresh **instantiation** of the already
compiled module (`e.runtime.InstantiateModule`), which wazero is designed to
make cheap — this is exactly what the cold/warm latency table demonstrates
(2+ seconds cold including compile, single-digit milliseconds warm,
instantiate-only). Fresh instantiation per request also gives isolation: no
state leaks between concurrent requests for the same module.

**Q15. What stops the module cache from growing without bound?**
Three independent bounds enforced in `evictLocked`
(`internal/worker/engine.go`): TTL (`ModuleCacheTTL`, default 30 min, entries
older than that are dropped regardless of use), max entry count
(`MaxCachedModules`, default 128), and max total bytes (`MaxCacheBytes`,
default 256 MiB). Entry-count and byte-count eviction both remove the least
recently used entry first (tracked via a monotonic `usedOrder` clock updated
on every cache hit) — so it's LRU within the size/count caps, with TTL as a
hard ceiling independent of usage.

**Q16. What happens if two concurrent requests both need to cold-compile the
same uncached module?**
`beginCompile`/`finishCompile` implement leader/follower coalescing: the first
goroutine to reach the cache miss becomes the "leader" and performs the
download+compile; any concurrent goroutine for the *same cache key* instead
waits on a channel and then rechecks the cache, rather than each triggering
its own redundant download and compile (`internal/worker/engine.go`,
`FetchAndCacheWithDigest`). This avoids duplicate cold-start cost under
concurrent first requests for a new module version.

**Q17. What is the cache key, precisely — could two different modules
collide?**
`ModuleCacheKey{ModuleName, ModuleURL, Digest}`, stringified with digest
taking priority, then URL, then bare name (`ModuleCacheKey.String()`). If a
`module_digest` is supplied, the cache key is digest-scoped, so a URL/name
reused for genuinely different bytes gets a distinct cache entry — collisions
would require a SHA-256 collision, not just a name reuse.

**Q18. How is a request's payload actually moved into WASM linear memory for
the custom ABI?**
The host calls the module's exported `malloc(size)` to get a pointer, writes
the payload bytes into the module's linear memory at that pointer
(`WriteString`), then calls `run(ptr, len)`. `run` returns one packed `uint64`
— the high 32 bits are the output pointer, the low 32 bits the output length
— and the host reads that byte range back out of linear memory
(`runWasmcatModule`). Go memory and Wasm linear memory are separate address
spaces, so this explicit copy is required in both directions.

### F. Security

**Q19. Concretely, how is "certificate-bound identity" enforced — walk through
one call.**
For `POST /internal/register` (or heartbeat/drain/completion) with a claimed
`WORKER_ID=worker-vn-01`, the gateway inspects the verified client
certificate's Common Name and DNS SANs from the mTLS handshake and requires
that they actually match `worker-vn-01` (per CLAUDE.md's naming convention,
CN `wasmcat-worker-worker-vn-01` or a SAN containing `worker-vn-01`). If the
claimed ID and the certificate identity disagree, the request is rejected
before it reaches registry-mutation logic — a worker cannot register or
heartbeat under another worker's identity even if it somehow obtained a
generically-signed client certificate.

**Q20. Why does module digest verification happen on the worker, not just
trusted from the master?**
Defense in depth: the master resolves the digest (from ACR manifest or the
caller-supplied `module_digest`) and forwards it, but the *worker* is the
component that downloads and compiles untrusted bytes, so the worker
re-verifies SHA-256 against the actual downloaded bytes before compiling
(`verifyModuleDigest` in `internal/worker/engine.go`). This protects against a
compromised or misconfigured module host serving different bytes than the
master resolved, a MITM on the worker's fetch path, or a bug in the
resolution step — the worker never compiles bytes it can't verify when a
digest was supplied.

**Q21. What's the blast radius if a worker's private key is stolen?**
The stolen key only authenticates as that one worker's identity (bound to its
CN/SAN), so it could register/heartbeat/complete jobs as that specific worker
and receive `/invoke` calls scoped to it — it cannot impersonate the master,
other workers, or pull unscoped ACR credentials, since ACR tokens are
repository-scoped, pull-only, short-lived, and minted server-side by the
master's managed identity, not held long-term by workers. This is a stated
scope limitation (host-level compromise isn't fully modeled) but the identity
binding contains the damage to that one node's traffic.

**Q22. Why does the dashboard have a local backend instead of calling the
master directly from the browser?**
Because mTLS requires presenting a client private key during the TLS
handshake, and a private key must never be shipped to or held by a browser
process. The local backend holds the operator's mTLS client certificate and
proxies dashboard requests to the master, so the key stays on the operator's
machine (slide 11, "Boundary preserved").

### G. Reliability, Idempotency, Job Store

**Q23. Precisely, what is hashed into a job's "fingerprint," and why does it
matter?**
`executionRequestFingerprint` hashes the *meaningful request identity* fields
of `ExecutionRequest` — module reference, digest, and payload, not
incidental/transport fields (`internal/master/request_tracker.go`). It matters
because idempotency is keyed on `request_id` alone, but a client could reuse a
`request_id` for a genuinely different request by mistake; comparing
fingerprints lets the gateway tell "same request replayed" (return the stored
result) apart from "different request, same ID" (return `409
request_id_conflict`) rather than silently executing the wrong body under an
old ID.

**Q24. Walk through exactly what makes a job `ambiguous`, and why not just
call it `failed`?**
A job in `dispatching` or `running` becomes `ambiguous` only when its lease
(`LeaseUntil`) has expired *and* no completion callback was ever recorded
(`recoverJob` in `job_recovery.go`). `failed` would imply the system knows the
execution did not succeed — but a lease can expire because the worker crashed
after executing but before its completion callback reached the master, in
which case the module *did* run. Calling that `failed` and letting a client or
retry logic re-run it risks double execution; `ambiguous` is an honest "we
cannot prove what happened" state that requires operator or client judgment,
which is the correctness-over-convenience principle stated on slide 13.

**Q25. What is a "lease" here, concretely, and who sets its duration?**
When `JobRecovery` picks up a `queued` job, it calls `MarkDispatching` with
`leaseUntil = now + LeaseTTL` (`DefaultJobLeaseTTL` if unset). That's the
deadline by which the job must reach a terminal state (`succeeded`/`failed`)
or record a completion callback; if that deadline passes with the job still
`dispatching`/`running`, the next recovery scan marks it `ambiguous`. It's
effectively a distributed timeout that bounds how long an unconfirmed
in-flight job can block resolution.

**Q26. Why SQLite instead of Postgres/etcd/Redis for job durability, given the
thesis flags this as *not* HA?**
SQLite matches the actual requirement: local, single-node durability across a
master process restart on one VM — not multi-node consensus. It needs zero
extra infrastructure to deploy (no separate DB service, no CGO via
`modernc.org/sqlite`, consistent with the project's no-CGO constraint), which
fits a thesis-scale, VM-installable orchestrator. Postgres/etcd would be the
right choice specifically when building the HA control plane named as future
work — introducing that complexity now would be solving a problem
(multi-master consensus) the current single-master design doesn't have.

**Q27. What are `MaxAttempts` and what happens when they're exhausted?**
Each job record tracks an `Attempt` counter and a configured `MaxAttempts`.
On a *retryable* recovery dispatch failure (e.g., no workers registered yet),
`recoverQueuedJob` increments the attempt count; if the next attempt would
reach `MaxAttempts`, the job is marked `failed` with the last error instead of
being requeued indefinitely — otherwise it's kept `queued` for another
recovery pass (`internal/master/job_recovery.go`).

### H. ACR / Module Provisioning

**Q28. Why is the ACR token cached, and what's the actual risk of that cache
being process-local?**
Requesting a fresh ACR refresh/access token on every dispatch would add
Azure AD round-trip latency to every ACR-sourced execution; caching
repository-scoped tokens in memory avoids that on the warm path. The stated
risk (slide/limitations, CLAUDE.md) is that this cache is per-process: running
multiple master replicas would not share cached tokens, so each would
independently mint and cache its own — this is a minor efficiency gap, not a
correctness or security issue, since tokens are still scoped and short-lived
regardless of which master minted them.

**Q29. How does the master pick which OCI layer is the actual `.wasm`
module?**
After fetching the manifest, it selects a layer by `application/wasm` media
type; if no layer declares that media type, it falls back to the single-layer
case (`internal/master/acr_manifest.go`). This assumes one WASM artifact per
manifest, which matches how the ORAS publishing workflow pushes modules.

### I. Results & Measurement Methodology

**Q30. How were the cold/warm latency numbers measured — is this a single
run, and is it statistically meaningful?**
*(be honest here — this is a common examiner target)* The numbers reported
(slide 16) are point measurements of `execution_time_ms` from real requests
against the two-region Azure deployment, one cold request per module/source
combination (necessarily — "cold" is only true once per cache lifetime) and a
warm request immediately after. This demonstrates the *mechanism* (cache
absence vs. presence dominates latency) rather than a statistically
rigorous distribution; repeated-trial statistics (mean/variance across many
warm requests, tail latency) is an evaluation improvement I'd flag as future
work if pressed.

**Q31. The geo-routing result table only shows two coordinate pairs — how do
you know this generalizes?**
The two points were chosen to be unambiguous test cases (clearly closer to
one deployed worker than the other) to demonstrate correct end-to-end
behavior of the *mechanism* — Haversine scoring plus capacity filtering plus
mTLS-authenticated dispatch — not to claim broad statistical coverage. The
scheduler code itself (`FindClosestWorker`) is a straightforward nearest-
neighbor scan validated by targeted unit tests in `tests/`, not by the demo
figures alone.

**Q32. Only two workers were deployed for evaluation — does the design
actually scale to many more?**
The registry, filtering, and Haversine scan are all linear in the number of
registered workers with no architectural ceiling at two — `RWMutex`-protected
map iteration and a nearest-neighbor scan both scale to dozens–hundreds of
workers without algorithmic change. What was *not* evaluated at larger scale
is registry lock contention under high heartbeat frequency and dispatcher
throughput under concurrent load, which I'd name as a measurement gap rather
than a design limitation.

### J. Testing & Engineering Process

**Q33. What is actually covered by the `tests/` tree — unit, integration, or
both?**
Both, kept out of the production packages per project convention. Unit-level
coverage targets pure logic — Haversine/filtering in the scheduler, fingerprint
computation, digest verification, cache eviction ordering. Integration-style
tests exercise the gateway/dispatcher/worker flow together (e.g., idempotent
replay, `409` conflict behavior, retry-vs-no-retry classification) against
real HTTP servers rather than mocks, consistent with the project's testing
philosophy. See `docs/TESTING.md` for the current breakdown if asked for
exact package names.

**Q34. Why Go instead of Rust, given wazero is a Go library and other Wasm
runtimes (Wasmtime, wasmer) are Rust-native?**
Go gives goroutines and channels as a natural fit for the concurrent
register/heartbeat/dispatch/recovery loops without an async runtime
dependency, a mature standard-library `net/http` + `crypto/tls` stack for
mTLS, straightforward static binary deployment, and — specifically — wazero
is a *pure-Go* Wasm runtime, meaning no CGO bridge to a Rust/C runtime is
needed at all, which directly satisfies the project's no-CGO, native-binary
constraint. Wasmtime/wasmer would have pulled in a CGO boundary.

### K. Scope, Honesty, and Future Work

**Q35. If you had one more semester, what's the single highest-value next
feature?**
Replacing Haversine with measured RTT-based scheduling, because it directly
strengthens the thesis's core claim (location-aware routing) with real
network-quality data instead of a geographic proxy — the architecture already
has the hook point (`Scheduler.SelectWorker`), so it's an extension of an
existing interface, not a redesign.

**Q36. Is wasmCat production-ready?**
No, and the thesis states that directly (slide 17, `docs/THESIS_DRAFT.md`
§4.11). It's a working end-to-end prototype validated on a real two-region
deployment with genuine security (mTLS, cert-bound identity, digest
verification) and genuine local durability — but single-master, in-memory
registry, host-level isolation, and stateless-only workloads are real gaps
against an enterprise bar, and I'd rather state that precisely than have it
surface as a credibility problem under questioning.

**Q37. What would break first if this were deployed at real production
traffic?**
Most likely the single master's registry `RWMutex` and SQLite write path
under high heartbeat frequency and job-write volume — both are correct but
single-node bottlenecks by design; that's precisely the HA gap named in
limitations, not a bug.

---

## Delivery Notes

- Know the exact file/function names above cold — examiners reward precision
  ("it's in `dispatcher.go`, the `isRetryableDispatchError` check") far more
  than a correct-but-vague description.
- If asked something not covered here or in `docs/THESIS_DRAFT.md` Appendix E,
  it's fine to say "that's not something I measured / that's future work" —
  do not improvise a claim about behavior you haven't verified in the code.
- The limitations slide (17) is a strength, not a weakness, if you present it
  confidently: it shows you know exactly where the system's boundaries are.
