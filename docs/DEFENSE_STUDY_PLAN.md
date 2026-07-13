# Defense Study Plan — Zero to Ready by Monday

Written for: defense on **Monday, July 13**, starting from **Friday, July 10**
with the assumption that you are re-learning this repository from scratch.
This is the *on-ramp*. Once each section makes sense, move to
`docs/THESIS_DEFENSE_PREP.md` (slide-by-slide speaking script + 37 drill
questions) for rehearsal, and keep `docs/THESIS_DRAFT.md` nearby as the
written source of truth.

Read this document top to bottom once, slowly, on Friday. Do not skip to the
schedule at the bottom until you've read Parts 1–4 at least once.

---

## Part 0 — The one-paragraph pitch (memorize this verbatim first)

> "wasmCat is a system I built that lets you run small pieces of code —
> compiled to WebAssembly — on whichever server is physically closest to the
> user who requested it, without using Docker or Kubernetes. You send a
> request to a central 'master' server, it picks the nearest healthy 'worker'
> server out of however many are registered, sends the code there securely,
> the worker runs it in a lightweight sandboxed runtime, and sends the result
> back. It's a research-scale prototype of 'serverless computing for the
> edge,' built entirely in Go."

If you remember nothing else and freeze, say this. Every slide expands one
clause of this sentence.

---

## Part 1 — Concepts you need before any of the code makes sense

Read each of these as if you'd never heard the term. They are the vocabulary
the examiners will use.

### 1.1 What is "the edge" / edge computing?

Normally, when you use an app, your request travels to one big data center
(say, in Singapore or the US) no matter where you physically are. That trip
takes time — physics: light/electricity in cable has a speed limit, so a
user in Hanoi talking to a server in the US pays real, unavoidable delay
(latency) on every request.

**Edge computing** means: instead of one big central server, you have many
*smaller* servers spread out geographically (Hanoi, Singapore, Seattle,
wherever), and you run the user's request on whichever one is *closest to
them*, so the round trip is shorter. wasmCat's whole job is deciding *which*
edge server should run a given request.

### 1.2 What is "serverless"?

This does **not** mean "no servers exist." It means: as a developer, you
don't say "run my code on machine X." You just say "run this function," and
the platform decides which machine, starts it, runs it, and hands you the
result — you never manage a server yourself. wasmCat is a serverless
platform: you submit a function (a `.wasm` module) and a request; wasmCat
picks the machine.

### 1.3 What is WebAssembly (WASM), and why not just use Docker containers?

**WebAssembly** is a compact, portable binary format for code. You can
compile C, Rust, Go, etc. into a `.wasm` file, and that one file will run
identically on any machine that has a WASM *runtime* — no matter the OS or
CPU architecture. It runs inside a **sandbox**: by default a WASM module can
only touch its own private chunk of memory ("linear memory") and cannot
touch the host filesystem, network, or other processes unless you explicitly
give it that ability.

**Containers (Docker/Kubernetes)** solve a similar-sounding problem
(package code + run it anywhere) but much more heavily: a container image
must be *pulled* over the network, *unpacked* layer by layer, and the OS has
to set up isolated namespaces and cgroups before your code even starts. For
a tiny function that should run in milliseconds, all of that setup is
disproportionate overhead. WASM startup is closer to "load a small file and
jump into it" than "boot a mini virtual filesystem."

**wazero** is the specific WASM runtime wasmCat uses. Its defining feature
for this project: it's a **pure Go library** — meaning it's not a separate
program you install and talk to over a socket, it's linked directly into
the wasmCat worker binary. There is nothing else to install on a machine to
run WASM modules besides the wasmCat binary itself.

Why this matters for the thesis: **"no Docker, no Kubernetes, no separate
runtime process"** is a hard constraint of this project, stated explicitly
in the project's engineering rules. If asked "why not containers," the
honest answer is *overhead + operational complexity for small edge
functions*, not "containers are bad."

### 1.4 Master–worker architecture

This is the most important structural idea. There are exactly two kinds of
running processes in wasmCat:

- **The master**: one process, the "brain." It never runs user code. It
  knows who's registered as a worker, decides which worker should handle a
  given request, and forwards the request there.
- **Workers**: many processes, the "hands." Each one registers itself with
  the master, regularly reports "I'm alive, here's my free CPU/RAM," and
  when the master sends it a job, it actually runs the WASM module and
  returns the result.

This is called a **control plane / data plane split**: the master is the
control plane (decisions), workers are the data plane (execution). Keeping
these separate is a deliberate design choice — the master's job (routing
decisions) and the worker's job (running arbitrary, possibly slow user code)
have very different risk profiles, and you don't want a slow/misbehaving
user function to ever affect the component making routing decisions for
everyone else.

### 1.5 mTLS — why the system cares so much about certificates

**TLS** is the encryption that makes `https://` secure — normally only the
*server* proves its identity with a certificate, and the *client* (e.g. your
browser) doesn't need one.

**mTLS (mutual TLS)** flips this so *both sides* must present a certificate
and prove who they are before any connection is allowed. wasmCat uses mTLS
for **every** internal connection: master↔worker, worker↔master. Concretely
this means: every machine in the cluster (master and every worker) has its
own private key and certificate, signed by a shared internal Certificate
Authority (CA) that you generate once with `scripts/gen-certs.sh`. If a
connection arrives without a valid, CA-signed client certificate, it's
rejected before any application code even sees the request.

On top of plain mTLS, wasmCat adds **certificate-bound identity**: when a
worker says "I am `worker-vn-01`," the master checks that the certificate
it just presented was actually issued *for* `worker-vn-01` (its Common Name
or Subject Alternative Name), not just that it's *some* valid certificate.
This stops one worker from lying about which worker it is.

**Why this matters so much for a thesis defense**: mTLS is the answer to
almost every "how is this secure" question. Learn it well.

### 1.6 SQLite and "durable" jobs

A normal in-memory variable disappears if the program restarts or crashes.
**Durable** means: written to disk, in a form that survives a crash/restart.
wasmCat uses **SQLite** (a lightweight, file-based database — no separate
database server process to install) to record job requests so that if the
master process restarts mid-job, it can figure out later what happened
instead of just losing track of the request.

**Idempotency** is a related idea: if the same request (same `request_id`)
somehow gets sent twice — e.g. because a client's network hiccupped and it
retried — the system should not run the function twice. wasmCat handles
this by remembering the result of a `request_id` the first time and simply
replaying that stored result if the same ID comes in again.

### 1.7 ACR (Azure Container Registry) and JIT provisioning

Normally, `.wasm` modules could just be fetched from any URL. wasmCat also
supports fetching them from **Azure Container Registry**, a cloud storage
service Azure offers (usually for Docker images, but wasmCat pushes `.wasm`
files there as generic artifacts using a tool called **ORAS**). "JIT" =
Just-In-Time: the worker doesn't pre-download every possible module; it
fetches and compiles a module *the first time* it's actually needed for a
request, then caches the compiled result.

### 1.8 Haversine distance / geo-routing

**Haversine formula** is standard trigonometry to compute the "great-circle"
distance between two GPS coordinates on a sphere (i.e., real-world straight-
line distance between two lat/lon points, accounting for the Earth's
curvature). wasmCat's scheduler uses it to answer: "of the workers that have
enough free capacity, which one is geographically nearest to this request's
coordinates?" It is a **proxy** for network latency, not a direct
measurement of it — closer usually (but not always) means faster, and this
project is explicit that this is a simplification, not a claim of measuring
real network performance.

---

## Part 2 — The story: what happens when someone uses wasmCat

Walk through this narrative until you can retell it without notes. This
*is* the system — everything else is detail on top of this shape.

1. **Setup, once**: An operator starts one master process and one-or-more
   worker processes (each is just running a binary, e.g. as a systemd
   service on a VM). Each worker knows its own GPS coordinates (configured,
   or auto-detected from its public IP). Each worker registers itself with
   the master over mTLS, saying "I exist, here's my address, my location, my
   free CPU/RAM." The master keeps a live table of all currently-known
   workers in memory. Every few seconds, each worker sends a small
   heartbeat ("still alive, here's my current free CPU/RAM") so the master's
   table stays fresh; if a worker stops heartbeating, the master eventually
   forgets it.

2. **A client wants to run a function**: They send an HTTP request to the
   master (`POST /api/v1/execute`) containing: a unique `request_id`, which
   module to run (`module_name` + a URL or ACR reference to the `.wasm`
   file), the input data (`payload`), and their own GPS coordinates
   (`user_lat`/`user_lon`).

3. **The master decides who runs it**: It checks the request is valid, then
   asks the **scheduler**: "given these coordinates, which registered
   worker should handle this?" The scheduler first throws out any worker
   that's draining (being taken offline) or doesn't have enough free
   CPU/RAM, then picks the geographically nearest one of what's left, using
   Haversine distance.

4. **The master forwards the job**: Over an mTLS connection, it sends the
   full request to that chosen worker's `/invoke` endpoint. (If the module
   reference points at Azure Container Registry instead of a plain URL, the
   master first fetches a short-lived, scoped access token and resolves the
   real download link before forwarding — this is the "JIT provisioning"
   step.)

5. **The worker runs the code**: It checks if it already has this exact
   module (by content, not just by name — see below) compiled and cached in
   memory. If not, it downloads the `.wasm` bytes, optionally verifies they
   match an expected cryptographic hash, and compiles them with the wazero
   runtime — this compile step is the expensive part, roughly 2 seconds in
   this project's measurements. It then creates a *fresh, isolated instance*
   of that compiled module (fast — single-digit milliseconds), feeds it the
   input payload, runs it, and reads back the output.

6. **The result comes back**: The worker returns the output, execution time,
   and its own worker ID to the master, which returns it to the original
   client as JSON. The client can see exactly which worker actually ran their
   code.

7. **If something durable is needed instead** (the request should survive a
   master crash, or the client doesn't want to wait synchronously): the
   client instead calls `POST /api/v1/jobs`. The master immediately writes
   the job to SQLite and replies "accepted, come check later" — a background
   loop then picks it up and runs the same dispatch process as step 3–6, and
   the client polls `GET /api/v1/jobs/{request_id}` for the result.

That's the entire system. Everything in the code is in service of making
one of these seven steps correct, fast, or safe.

---

## Part 3 — A guided tour of "where is that in the code"

You don't need to read all the Go code. You need to know *what lives where*
so that if an examiner asks "show me," you can navigate there calmly.

| Concept from Part 2 | Lives in | One-line description |
|---|---|---|
| HTTP endpoints (`/api/v1/execute`, `/api/v1/jobs`, health, etc.) | `internal/master/gateway.go` | Validates and routes incoming HTTP requests |
| The live table of workers | `internal/master/registry.go` | An in-memory map protected by a lock, plus a cleanup loop for dead workers |
| "Which worker should run this" | `internal/master/scheduler.go` | Capacity filter + Haversine nearest-match |
| "Send the job to that worker, retry sanely if it fails" | `internal/master/dispatcher.go` | Talks to the chosen worker over mTLS, decides what's safe to retry |
| Azure Container Registry token/manifest handling | `internal/master/acr_token.go`, `acr_manifest.go` | Gets a scoped download token, resolves the module's real blob URL |
| Durable job records in SQLite | `internal/master/job.go`, `job_store.go`, `sqlite_job_store.go`, `job_recovery.go` | Persist jobs, recover them after a restart, handle the "ambiguous" case |
| Worker's own HTTP server (`/invoke`, health) | `internal/worker/server.go` | Receives dispatched jobs from the master |
| Registering + heartbeating to the master | `internal/worker/telemetry.go` | Sends "I'm alive" messages |
| Actually running the WASM module | `internal/worker/engine.go` | Download, cache, compile, instantiate, execute — the core execution engine |
| Certificates / mTLS setup | `internal/security/mtls.go` | Generates and loads certs, builds mTLS HTTP clients/servers |
| The shared data shapes (`ExecutionRequest`, `ExecutionResponse`, `WorkerNode`) | `internal/shared/` | Struct definitions both master and worker import |
| Env-var configuration parsing | `internal/config/` | Reads things like `MIN_WORKER_CPU_FREE` from environment |
| `wasmcat-master`, `wasmcat-worker`, `wasmcatctl` startup | `cmd/master/main.go`, `cmd/worker/main.go`, `cmd/wasmcatctl/main.go` | The actual entrypoints that wire everything together |

If an examiner says "show me the scheduler," you now know: open
`internal/master/scheduler.go`, point at `SelectWorker`, and narrate: filter
by capacity, then find closest by Haversine.

---

## Part 4 — Facts and numbers worth memorizing cold

These come up in almost every defense regardless of what the examiner
focuses on. Know them without looking.

- **Language**: Go. **WASM runtime**: wazero (pure Go, embedded — no
  external runtime process). **Database**: SQLite via a pure-Go driver
  (`modernc.org/sqlite`) — chosen specifically to avoid CGO.
- **Two binaries that matter most**: `wasmcat-master`, `wasmcat-worker`
  (plus an operator CLI, `wasmcatctl`, and a web dashboard).
- **Two execution modes ("ABIs")**: the custom minimal `malloc`/`memory`/
  `run` ABI, and standard WASI (`wasip1`, stdin/stdout) for ordinary
  Rust/Go/C toolchain output. Auto-detected by which function the module
  exports.
- **Security**: mTLS on every internal connection; worker identity is bound
  to its certificate (claimed ID must match cert CN/SAN); module bytes are
  SHA-256 verified before compiling; ACR tokens are repository-scoped,
  pull-only, short-lived.
- **Scheduling**: two stages — filter out `draining` workers and anything
  below configured free CPU/RAM thresholds, then pick nearest by Haversine
  distance (Earth radius 6371 km in the formula).
- **Reliability**: job states are `queued → dispatching/running →
  succeeded/failed`, with a special `ambiguous` state when a lease expires
  and the system can't prove whether the worker actually ran it — and it
  deliberately does **not** auto-retry an ambiguous job.
- **Measured results** (two-region Azure deployment):
  - Cold execution (first run, includes download + compile): **~2.1s**
    direct URL, **~2.4s** via ACR.
  - Warm execution (module already compiled and cached): **~7.5ms** direct
    URL, **~2.9ms** via ACR.
  - Geo-routing verified end-to-end: a request from Singapore coordinates
    was routed to `worker-vn-01`; the same request from Seattle coordinates
    was routed to `worker-west-us-01`.
- **Stated limitations** (say these proactively, don't wait to be caught):
  single master = no control-plane HA (SQLite durability is local, not
  replicated); in-memory worker registry rebuilt from heartbeats after a
  restart; Haversine is a distance proxy, not measured network latency;
  isolation is host-level CPU/RAM, not per-request cgroups/fuel metering;
  workload model is stateless functions only.

---

## Part 5 — Three-day schedule (today is Friday, July 10; defense is Monday, July 13)

### Friday, July 10 — Understand (aim: 3–4 focused hours)

1. Read this document (Parts 0–4) once, slowly. (45 min)
2. Read `docs/THESIS_DEFENSE_PREP.md` Part 1 (the slide-by-slide script)
   while having `slide/slide.pdf` open next to it, slide by slide. Don't try
   to memorize yet — just connect each slide's visual to the story in Part 2
   above. (45–60 min)
3. Open the files listed in the Part 3 table and skim each for ~2 minutes —
   not to understand every line, just to recognize the shape and see the
   function names mentioned in this guide actually exist where claimed. (45
   min)
4. Re-read Part 4 (facts/numbers) twice. Close the document and try to
   write all of it from memory on paper; check what you missed. (30 min)
5. Stop for the day once you can do the Part 0 pitch + Part 2 story from
   memory, out loud, without notes.

### Saturday, July 11 — Rehearse (aim: full run-through + drilling)

1. Morning: read `docs/THESIS_DEFENSE_PREP.md` Part 1 out loud, slide by
   slide, at talking pace, with `slide/slide.pdf` on screen. Time yourself —
   target ~15 minutes total. Do this twice.
2. Afternoon: go through all 37 questions in
   `docs/THESIS_DEFENSE_PREP.md` Part 2. For each, cover the answer, say
   your own version out loud first, *then* check it against the written
   answer. Flag any you got clearly wrong or blanked on.
3. Evening: re-drill only the flagged questions from step 2, plus the 5
   questions in `docs/THESIS_DRAFT.md` Appendix E.
4. If possible, do one full run (slides + surprise questions) in front of
   a friend/family member/mirror/phone recording, even if they don't
   understand the content — the goal is fluency under mild social pressure,
   not technical review.

### Sunday, July 12 — Consolidate (aim: light, not exhausting)

1. One more full run-through of the 15-minute script, out loud, without
   reading it — just glancing at slide titles as prompts.
2. Skim (don't re-derive) Part 4's facts/numbers list once more.
3. Re-read the limitations slide (17) and practice saying it *confidently* —
   this is the part people under-rehearse and then sound defensive about
   live. It should sound like "I know exactly where the edges of this
   system are," not an apology.
4. Prepare logistics: laptop charged, `slide.pdf` accessible offline,
   backup copy on a USB drive or phone, know the exam room/time, clothes
   ready. Sleep on time — do not cram past a reasonable hour.

### Monday, July 13 — Defense day

1. Morning: one calm read-through of Part 4 only (facts/numbers), and the
   one-paragraph pitch from Part 0. Nothing new — this is a warm-up, not
   study.
2. If a question stumps you live: it is completely acceptable to say "That
   wasn't something I measured / that's outside what I tested — here's what
   I *do* know about it," and connect it back to a limitation you already
   have an honest answer for. This reads as rigor, not weakness — examiners
   respond far worse to confident guessing than to a precise "I don't know,
   but here's the related thing I verified."
3. Breathe, present the pitch from Part 0 in your own words as the opening
   line, then follow the slide script.

---

## Part 6 — If you truly only have 30 minutes before you walk in

Read, in this order:
1. Part 0 (the pitch) — memorize it.
2. Part 2 (the seven-step story) — be able to retell it.
3. Part 4 (facts/numbers) — especially the limitations list.
4. `docs/THESIS_DEFENSE_PREP.md` slide titles only (the outline), so you
   recognize the shape of your own deck as it appears on screen.
