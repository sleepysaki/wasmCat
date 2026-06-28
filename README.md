# wasmCat
A container orchestrator exclusively used for WASM (WebAssembly) codes.

## Installation

wasmCat is installed as native master and worker binaries. It does not require a container runtime to run the orchestrator.

See [docs/INSTALLATION.md](docs/INSTALLATION.md) for release downloads, native builds, `init`, service, and upgrade instructions.

For first-time installs and in-place updates, use `scripts/install-or-update.sh`. It backs up the current binary, installs the new one, restarts only the relevant systemd service, rolls back on a failed restart, and never touches `/etc/wasmcat` (env, certs, and the SQLite job database are preserved):

```bash
sudo sh scripts/install-or-update.sh --role master --source ./dist   # or --role worker / --role ctl,ui
```

Use [docs/PRODUCTION_VM_DEPLOYMENT.md](docs/PRODUCTION_VM_DEPLOYMENT.md) for a zero-to-hero Ubuntu VM deployment runbook, including systemd setup, ACR module publishing, and public HTTPS exposure.

Use [docs/WASM_MODULE_PIPELINE.md](docs/WASM_MODULE_PIPELINE.md) for the GitHub Actions flow that builds WABT `.wat` modules and publishes `.wasm` OCI artifacts to Azure Container Registry.

Use [docs/NODE_SETUP_TROUBLESHOOTING.md](docs/NODE_SETUP_TROUBLESHOOTING.md) when a VM node starts but does not register, including mTLS, SSH copy, private IP, and auto-location failures.

Use [docs/WASMCATCTL.md](docs/WASMCATCTL.md) for `wasmcatctl` commands that replace long mTLS `curl` calls during operation.

Use [docs/WEB_DASHBOARD.md](docs/WEB_DASHBOARD.md) for the browser-based `wasmcat-ui` operator console.

Use [docs/DEMO.md](docs/DEMO.md) to demonstrate the system end to end. `sh scripts/demo.sh` builds the binaries and verifies every core capability (setup, registration, geo-aware distribution and execution, durable jobs, drain, mTLS) with a requirement-by-requirement result map.
