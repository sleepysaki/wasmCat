# Repository Guidelines

## Project Structure & Module Organization

This repository is a Go module named `wasmcat`, a WASM-focused container orchestrator. Entrypoints live under `cmd/`: `cmd/master` starts the control plane gateway, registry, scheduler, and dispatcher; `cmd/worker` starts a worker node and telemetry loop. Core packages live under `internal/`: `internal/master` contains scheduling, dispatch, gateway, registry, and ACR token logic; `internal/worker` contains the WASM execution engine, server, memory helpers, and telemetry; `internal/security` handles mTLS certificate setup; `internal/shared` contains shared models. `modules/`, `test_modules/`, and `til/` are workspace directories for WASM modules, fixtures, or experiments.

## Build, Test, and Development Commands

- `go mod tidy`: clean and synchronize module dependencies.
- `go build ./...`: compile all packages and catch type or import errors.
- `go test ./...`: run all package tests.
- `go run ./cmd/master`: start the master gateway on port `7270`.
- `go run ./cmd/worker`: start a worker on port `7271` and connect to the local master.

Run commands from the repository root. Start the master before the worker for local integration checks.

## Coding Style & Naming Conventions

Use standard Go formatting: run `gofmt` on changed `.go` files before committing. Keep package names short, lowercase, and aligned with their directory names. Exported identifiers should use PascalCase and include useful doc comments when they form package APIs. Prefer clear, direct names for orchestration concepts, such as `Registry`, `Dispatcher`, `WorkerServer`, and `WasmEngine`.

## Testing Guidelines

Place tests next to the package they cover using Go's `*_test.go` convention. Use table-driven tests for scheduler, registry, dispatcher, and security behavior where inputs and expected outputs are clear. Keep WASM fixtures in `test_modules/` when tests need real module files. Always run `go test ./...` before opening a PR.

## Commit & Pull Request Guidelines

Recent commits use short, imperative summaries such as `implement mtls`, `Fix port`, and `update dispatcher to use execute api correctly`. Keep commit subjects concise and focused on one change. Pull requests should include a short description, test results, linked issues if applicable, and notes for behavior that affects ports, mTLS certificates, Azure Container Registry access, or worker/master communication.

## Security & Configuration Tips

Do not commit generated certificates, access tokens, Azure credentials, or local module artifacts. Keep GitHub Actions and Azure DevOps secrets in the platform secret stores. When changing mTLS or ACR code, document any required environment variables or credential setup in the PR.
