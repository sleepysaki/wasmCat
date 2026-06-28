# Native Release Automation Plan

## Purpose

wasmCat is intended to be installed as native binaries with no container runtime requirement. The release process must match that design: users should be able to download a small set of files, verify checksums, install the binary, run `init`, and start the service.

Before this release plan, users could build artifacts locally with `scripts/build.sh` or `scripts/build.ps1`, but that still required cloning the repository and having a Go toolchain installed. Release automation removes that overhead for normal installation.

## Release Trigger

Releases are created from Git tags that look like semantic versions:

```text
v0.1.0
v1.2.3
```

Pushing a tag starts `.github/workflows/release.yml`.

## Release Checks

The release workflow repeats the important quality gates before publishing files:

```text
gofmt
go mod tidy drift check
go vet ./...
go test ./...
native release build
```

This prevents a release from publishing artifacts that were not built from a clean, tested tree.

## Release Artifacts

Each release publishes:

```text
wasmcat-master-linux-amd64
wasmcat-worker-linux-amd64
wasmcatctl-linux-amd64
wasmcat-ui-linux-amd64
wasmcat-master-windows-amd64.exe
wasmcat-worker-windows-amd64.exe
wasmcatctl-windows-amd64.exe
wasmcat-ui-windows-amd64.exe
wasmcat-master.service
wasmcat-worker.service
wasmcat-master.env
wasmcat-worker.env
checksums.txt
```

The service files are included so Linux users do not need to clone the repository just to install systemd units.

## Operator Flow

The intended production install flow is:

```text
download release assets
verify checksums
install master or worker binary
install wasmcatctl for operator commands
install wasmcat-ui for the browser dashboard
run init
provide certificates
install service file
start service
```

This keeps installation simple while preserving explicit control over certificates and host identity.

## Publishing A Release

Create and push a version tag:

```bash
git tag v0.1.0
git push origin v0.1.0
```

The workflow creates the GitHub Release if it does not exist. If the release already exists, the workflow uploads the newly built assets with `--clobber`, which makes reruns predictable.

## Why This Fits wasmCat

This approach keeps wasmCat lightweight:

- no Docker requirement
- no package manager requirement
- no external WASM runtime requirement
- no custom installer service
- no hidden bootstrap process

The release artifact is just the binaries, service templates, and checksums.
