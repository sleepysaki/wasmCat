# wasmCat
A container orchestrator exclusively used for WASM (WebAssembly) codes.

## Installation

wasmCat is installed as native master and worker binaries. It does not require a container runtime to run the orchestrator.

See [docs/INSTALLATION.md](docs/INSTALLATION.md) for release downloads, native builds, `init`, service, and upgrade instructions.

Use [docs/PRODUCTION_VM_DEPLOYMENT.md](docs/PRODUCTION_VM_DEPLOYMENT.md) for a zero-to-hero Ubuntu VM deployment runbook, including systemd setup, ACR module publishing, and public HTTPS exposure.

Use [docs/NODE_SETUP_TROUBLESHOOTING.md](docs/NODE_SETUP_TROUBLESHOOTING.md) when a VM node starts but does not register, including mTLS, SSH copy, private IP, and auto-location failures.

Use [docs/WASMCATCTL.md](docs/WASMCATCTL.md) for `wasmcatctl` commands that replace long mTLS `curl` calls during operation.
