# wasmcatctl

## Purpose

`wasmcatctl` is the operator CLI for wasmCat. It replaces long mTLS `curl` commands with short commands that reuse a saved master URL and client certificate configuration.

The CLI talks to the existing master HTTP API. It does not bypass the master, and it still uses mTLS through the configured CA, client certificate, and private key.

## Configuration

Create a local config file:

```powershell
go run ./cmd/wasmcatctl config init `
  --master https://localhost:7270 `
  --ca .\local\certs\ca.crt `
  --cert .\local\certs\worker-worker-vn-01.crt `
  --key .\local\certs\worker-worker-vn-01.key `
  --user-lat 21.0278 `
  --user-lon 105.8342
```

The default config path is:

```text
~/.wasmcat/config.json
```

View the active config:

```powershell
go run ./cmd/wasmcatctl config view
```

Every runtime command also accepts overrides:

```powershell
go run ./cmd/wasmcatctl --config .\my-config.json --output json health
```

## Cluster Inspection

Check master health:

```powershell
go run ./cmd/wasmcatctl health
```

Check readiness:

```powershell
go run ./cmd/wasmcatctl ready
```

List registered workers:

```powershell
go run ./cmd/wasmcatctl workers
```

Read metrics:

```powershell
go run ./cmd/wasmcatctl --output json metrics
```

## Execute A Module

Direct module URL:

```powershell
go run ./cmd/wasmcatctl execute `
  --request-id req_demo_001 `
  --module hello `
  --url https://example.com/modules/hello.wasm `
  --digest sha256:<expected_digest> `
  --payload "hello wasmCat"
```

ACR manifest URL:

```powershell
go run ./cmd/wasmcatctl execute `
  --request-id req_acr_001 `
  --module hello `
  --registry-url https://<registry>.azurecr.io/v2/<repository>/manifests/<tag-or-digest> `
  --payload "hello from ACR"
```

Payload from a file:

```powershell
go run ./cmd/wasmcatctl execute `
  --module hello `
  --url https://example.com/modules/hello.wasm `
  --payload-file .\payload.json
```

## Durable Jobs

Create an asynchronous durable job:

```powershell
go run ./cmd/wasmcatctl jobs create `
  --request-id req_job_001 `
  --module hello `
  --url https://example.com/modules/hello.wasm `
  --payload "async payload"
```

Read the job state:

```powershell
go run ./cmd/wasmcatctl jobs get req_job_001
```

## Worker Draining

Mark a worker as draining:

```powershell
go run ./cmd/wasmcatctl drain worker-vn-01
```

After this, the scheduler excludes that worker from new executions while the worker remains visible in the registry.

## Output Formats

The default output is table-oriented for humans. Use JSON when another tool needs to parse the response:

```powershell
go run ./cmd/wasmcatctl --output json workers
```

## Release Artifact

The native release scripts now build `wasmcatctl` together with the master and worker:

```text
wasmcatctl-linux-amd64
wasmcatctl-windows-amd64.exe
```
