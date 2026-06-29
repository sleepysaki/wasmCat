# wasmCat Demo Guide

This guide shows how to demonstrate that wasmCat works end to end. There are two
levels: a fast, deterministic automated demo (no network or cloud needed) and a
live multi-node cluster demo.

## 1. Automated Capability Demo (recommended)

One command builds the native binaries and drives the real master gateway,
dispatcher, geo-aware scheduler, wazero engine, durable SQLite job store, and
mTLS stack, then prints a result map per core requirement:

```bash
sh scripts/demo.sh
```

Show the full real dispatch/execution logs (useful for a live walkthrough):

```bash
VERBOSE=1 sh scripts/demo.sh
```

Each line maps a functional requirement to the tests that prove it:

| Capability | What it demonstrates |
| --- | --- |
| Setup | `wasmcat-master/worker init`, dev cert generation, `version`, loopback warnings |
| Registration & registry | workers register, heartbeat, are listed, and go stale |
| Geo-aware distribution + execution | a request's coordinates pick the nearest worker, which runs the module |
| Real binaries over mTLS | compiled `wasmcat-master` + `wasmcat-worker` execute WASM over mutual TLS |
| Capacity-aware scheduling | nearest selection, draining exclusion, CPU/RAM thresholds |
| Sync execute + idempotency | `/api/v1/execute`, cached/duplicate/conflicting `request_id` |
| Durable async jobs | `/api/v1/jobs` create, query, recovery, completion callback |
| mTLS identity & authorization | worker identity binding and execute-client allowlist |
| ACR provisioning | manifest parse, layer select, blob URL, scoped token cache |
| Worker limits & cache | payload/module/output limits, digest verification, digest-aware cache |
| Auto-location fallback | multi-provider location detection |
| Health/readiness/metrics | operational endpoints |

### Headline checks to narrate

The geo-routing proof (different user coordinates route to and execute on the
nearest worker, and draining reroutes traffic):

```bash
go test ./tests/integration -run TestGeoAwareDispatchExecutesOnNearestWorker -v
```

The real-binary proof (the actual compiled binaries talk over mTLS and execute a
WebAssembly module end to end):

```bash
go test ./tests/smoke -run TestMasterWorkerBinariesExecuteWASMOverMTLS -v
```

The full suite:

```bash
go test ./...
```

## 2. Live Cluster Demo

For a running cluster (one master and two geo-separated workers, exercised with
`wasmcatctl` and the `wasmcat-ui` dashboard), follow the runbook in
[PRODUCTION_VM_DEPLOYMENT.md](PRODUCTION_VM_DEPLOYMENT.md). The short path:

1. Generate one consistent certificate bundle with `scripts/gen-certs.sh`
   (`ca`, `master --ip ...`, `worker --id ... --ip ...`, `operator`). See
   [INSTALLATION.md](INSTALLATION.md#generate-mtls-certificates).
2. Install binaries and certs with
   `scripts/install-or-update.sh master|worker --certs-from ...`.
3. Start the services, then drive the cluster with `wasmcatctl health`,
   `wasmcatctl workers`, `wasmcatctl execute ...`, `wasmcatctl jobs ...`, and
   `wasmcatctl drain ...`, or use the `wasmcat-ui` dashboard
   ([WEB_DASHBOARD.md](WEB_DASHBOARD.md)).

A live execute call needs a real module that targets the wasmCat ABI (`memory`,
`malloc`, `run`). Build one with TinyGo and publish it to ACR using the steps in
[PRODUCTION_VM_DEPLOYMENT.md](PRODUCTION_VM_DEPLOYMENT.md) (Code-to-Node
Pipeline), then execute it with
`wasmcatctl execute --module ... --registry-url ... --payload ...`.
