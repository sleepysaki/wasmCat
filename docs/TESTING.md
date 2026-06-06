# Testing

wasmCat keeps tests in the separate `tests/` tree. This keeps production packages free of test files while still exercising exported APIs and realistic component boundaries.

## Execution Plan

Use this plan when validating that the orchestrator works both as separate components and as a connected control-plane-to-worker path:

1. Run `go test ./...` to validate every package and every test folder.
2. Run `go vet ./...` to catch suspicious code patterns that compile but may behave incorrectly.
3. Run `go build ./...` to confirm both command binaries and internal packages compile together.
4. Run the explicit `-coverpkg` command below to measure coverage against production packages even though tests live in `tests/`.
5. Run the native build script before release packaging to confirm generated binaries and checksums can be produced without Docker.

## Test Layers

### Unit Tests

Unit tests validate components in isolation:

- `tests/bootstrap`: config file generation, overwrite protection, local cert generation.
- `tests/config`: environment parsing and validation.
- `tests/master`: ACR parsing, gateway errors, dispatcher errors, scheduler capacity filtering.
- `tests/worker`: WASM engine execution, worker limits, worker health/readiness, JSON error handling.

Run all unit-level tests:

```powershell
$env:GOCACHE='E:\wasmCat\.gocache'
$env:GOMODCACHE='E:\wasmCat\.gomodcache'
go test ./...
```

## Integration Tests

Integration tests validate components working together. `tests/integration` starts in-memory HTTP servers for:

- module download
- worker invoke handler
- master gateway handler
- dispatcher
- scheduler
- WASM engine

The main integration path sends a request to `/api/v1/execute` and verifies that it travels through master dispatch, worker execution, module download, wazero execution, and response propagation.

Run integration tests with the rest of the suite:

```powershell
go test ./...
```

Run only integration tests:

```powershell
go test ./tests/integration
```

## Supporting Checks

Run these before a release or pull request:

```powershell
$env:GOCACHE='E:\wasmCat\.gocache'
$env:GOMODCACHE='E:\wasmCat\.gomodcache'
go test ./...
go vet ./...
go build ./...
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\build.ps1 -Version dev
```

Because tests live outside production package directories, use explicit package instrumentation for coverage:

```powershell
go test -count=1 "-coverpkg=wasmcat/internal/bootstrap,wasmcat/internal/config,wasmcat/internal/master,wasmcat/internal/worker,wasmcat/internal/shared,wasmcat/internal/security" ./tests/...
```

## Race Testing

Use the race detector where the local toolchain supports CGO:

```powershell
go test -race ./...
```

On Windows, `go test -race` requires a working C toolchain. If the linker cannot find runtime libraries such as `msvcrt` or `pthread`, fix the local GCC/MSYS installation before treating race-test failure as an application failure.
