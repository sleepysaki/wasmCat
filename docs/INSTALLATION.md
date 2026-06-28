# Installation

wasmCat is designed to run as native binaries. The install path does not require a container runtime, a separate WASM runtime, or another orchestrator.

## Installation Model

The simplest production layout is:

```text
/usr/local/bin/wasmcat-master
/usr/local/bin/wasmcat-worker
/usr/local/bin/wasmcatctl
/usr/local/bin/wasmcat-ui
/etc/wasmcat/master.env
/etc/wasmcat/worker.env
/etc/wasmcat/certs/
```

The binaries include the WASM runtime through wazero, so workers do not need an external container runtime or a separate WebAssembly runtime. The host only needs the operating system, network access, certificates, and the wasmCat binary for the role it runs.

The initialization design is documented in [BOOTSTRAP_PLAN.md](BOOTSTRAP_PLAN.md). The short version is: install the binary, run `init`, provide certificates, then start the service.

The release process is documented in [RELEASE_PLAN.md](RELEASE_PLAN.md). Tagged releases publish binaries, service templates, env templates, and checksums.

Setup failure patterns from real VM deployments are recorded in [NODE_SETUP_TROUBLESHOOTING.md](NODE_SETUP_TROUBLESHOOTING.md). Check it when a worker service is running but does not appear in `wasmcatctl workers`.

## Install From A Release

Set the repository and release version:

```bash
REPO="<owner>/<repo>"
VERSION="v0.1.0"
BASE_URL="https://github.com/$REPO/releases/download/$VERSION"
```

Download the Linux master, Linux worker, operator CLI, browser dashboard, systemd service files, and checksums:

```bash
curl -LO "$BASE_URL/wasmcat-master-linux-amd64"
curl -LO "$BASE_URL/wasmcat-worker-linux-amd64"
curl -LO "$BASE_URL/wasmcatctl-linux-amd64"
curl -LO "$BASE_URL/wasmcat-ui-linux-amd64"
curl -LO "$BASE_URL/wasmcat-master.service"
curl -LO "$BASE_URL/wasmcat-worker.service"
curl -LO "$BASE_URL/checksums.txt"
```

Verify the downloaded files:

```bash
sha256sum -c checksums.txt --ignore-missing
```

Use only the binary required for the host role. A master host needs `wasmcat-master-linux-amd64`; a worker host needs `wasmcat-worker-linux-amd64`. Operator machines can install `wasmcatctl-linux-amd64` to avoid long mTLS `curl` commands and `wasmcat-ui-linux-amd64` for a browser dashboard.

## Build From Source

Build from source when developing wasmCat or testing changes that are not published in a release.

From the repository root on Linux or macOS:

```bash
sh scripts/build.sh
```

From Windows PowerShell:

```powershell
.\scripts\build.ps1
```

If local execution policy blocks unsigned scripts, run the script with a process-scoped bypass:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\build.ps1
```

Both scripts write release artifacts to `dist/`:

```text
dist/wasmcat-master-linux-amd64
dist/wasmcat-worker-linux-amd64
dist/wasmcatctl-linux-amd64
dist/wasmcat-ui-linux-amd64
dist/wasmcat-master-windows-amd64.exe
dist/wasmcat-worker-windows-amd64.exe
dist/wasmcatctl-windows-amd64.exe
dist/wasmcat-ui-windows-amd64.exe
dist/wasmcat-master.service
dist/wasmcat-worker.service
dist/wasmcat-master.env
dist/wasmcat-worker.env
dist/checksums.txt
```

Set a version when building a release:

```bash
VERSION=0.1.0 sh scripts/build.sh
```

```powershell
.\scripts\build.ps1 -Version 0.1.0
```

## Operator Dashboard

`wasmcat-ui` is an optional browser console for operators. It uses the same config file as `wasmcatctl`; the browser talks to the local UI backend, and the backend calls the master over mTLS.

Install the dashboard binary on an operator machine:

```bash
sudo install -m 0755 wasmcat-ui-linux-amd64 /usr/local/bin/wasmcat-ui
```

Create the shared CLI/UI config:

```bash
wasmcatctl config init \
  --master https://master.example.com:7270 \
  --ca /etc/wasmcat/certs/ca.crt \
  --cert /etc/wasmcat/certs/worker-worker-vn-01.crt \
  --key /etc/wasmcat/certs/worker-worker-vn-01.key
```

Start the dashboard:

```bash
wasmcat-ui --listen :7280
```

Open:

```text
http://localhost:7280
```

Keep the dashboard on a trusted operator machine or behind an authenticated network boundary. It can read the configured client private key path in order to call the master.

## Linux Master Install

Create the service user:

```bash
sudo useradd --system --home /nonexistent --shell /usr/sbin/nologin wasmcat
```

Install the master binary:

```bash
sudo install -m 0755 wasmcat-master-linux-amd64 /usr/local/bin/wasmcat-master
```

Initialize the master config:

```bash
sudo wasmcat-master init \
  --config-dir /etc/wasmcat \
  --cert-dir /etc/wasmcat/certs \
  --worker-stale-timeout 30s \
  --master-shutdown-timeout 5s \
  --min-worker-cpu-free 10 \
  --min-worker-ram-free-mb 256
```

This creates:

```text
/etc/wasmcat/master.env
/etc/wasmcat/certs/
```

The generated production config keeps local certificate generation disabled:

```text
AUTO_GENERATE_CERTS=false
CERT_DIR=/etc/wasmcat/certs
MASTER_PORT=7270
CLEANUP_INTERVAL=15s
WORKER_STALE_TIMEOUT=30s
MASTER_SHUTDOWN_TIMEOUT=5s
MIN_WORKER_CPU_FREE=10
MIN_WORKER_RAM_FREE_MB=256
EXECUTE_CLIENT_ALLOWLIST=
MAX_EXECUTION_REQUEST_BYTES=2097152
EXECUTION_REQUEST_CACHE_TTL=5m
EXECUTION_REQUEST_CACHE_MAX_ENTRIES=4096
JOB_STORE_PATH=/etc/wasmcat/wasmcat-jobs.db
JOB_MAX_ATTEMPTS=3
JOB_LEASE_TTL=30s
JOB_RECOVERY_INTERVAL=5s
JOB_RECOVERY_BATCH_SIZE=32
MODULE_HOST_ALLOWLIST=
REQUIRE_MODULE_DIGEST=false
```

`JOB_STORE_PATH` is the local SQLite database for durable execution job state. Keep it on persistent disk, not a temporary filesystem, if you want completed `request_id` results and accepted job records to survive master restart.
The recovery loop scans this database for queued jobs and expired active leases. Queued jobs are retried; expired active leases are marked `ambiguous` because a worker may already have executed them.

For production, set `EXECUTE_CLIENT_ALLOWLIST` to the client certificate common names or DNS SANs allowed to submit execution requests, enqueue async jobs, and inspect durable job state. Example:

```text
EXECUTE_CLIENT_ALLOWLIST=wasmcat-client,deployer.internal
```

Set `MODULE_HOST_ALLOWLIST` and `REQUIRE_MODULE_DIGEST` to restrict executable module sources. Example:

```text
MODULE_HOST_ALLOWLIST=modules.internal,myregistry.azurecr.io
REQUIRE_MODULE_DIGEST=true
```

Mount or copy these files into `/etc/wasmcat/certs`:

```text
ca.crt
master.crt
master.key
```

Make the certificates readable by the service user:

```bash
sudo chown -R wasmcat:wasmcat /etc/wasmcat/certs
sudo chmod 0750 /etc/wasmcat/certs
sudo chmod 0640 /etc/wasmcat/certs/*.crt /etc/wasmcat/certs/*.key
```

Install the service file:

```bash
sudo install -m 0644 wasmcat-master.service /etc/systemd/system/wasmcat-master.service
```

Start the service:

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now wasmcat-master
sudo systemctl status wasmcat-master
```

Check the master:

```bash
curl -k https://localhost:7270/wasmcat/health
curl -k https://localhost:7270/wasmcat/ready
```

## Linux Worker Install

Create the service user if it does not already exist:

```bash
sudo useradd --system --home /nonexistent --shell /usr/sbin/nologin wasmcat || true
```

Install the worker binary:

```bash
sudo install -m 0755 wasmcat-worker-linux-amd64 /usr/local/bin/wasmcat-worker
```

Initialize the worker config:

```bash
sudo wasmcat-worker init \
  --worker-id worker-us-01 \
  --master-url https://master.example.com:7270 \
  --advertise-address worker-us-01.example.com:7271 \
  --worker-shutdown-timeout 10s \
  --max-cached-modules 128 \
  --max-cache-bytes 268435456 \
  --module-cache-ttl 30m \
  --config-dir /etc/wasmcat \
  --cert-dir /etc/wasmcat/certs
```

This creates:

```text
/etc/wasmcat/worker.env
/etc/wasmcat/certs/
```

The generated config includes the worker identity, master URL, advertised address, heartbeat interval, and execution limits:

```text
WORKER_ID=worker-us-01
MASTER_URL=https://master.example.com:7270
WORKER_ADVERTISE_ADDRESS=worker-us-01.example.com:7271
WORKER_AUTO_DETECT_LOCATION=true
WORKER_LOCATION_PROVIDER_URL=https://ipapi.co/json/
CERT_DIR=/etc/wasmcat/certs
WORKER_SHUTDOWN_TIMEOUT=10s
MAX_CONCURRENT_EXECS=4
MAX_CACHED_MODULES=128
MAX_CACHE_BYTES=268435456
MODULE_CACHE_TTL=30m
```

`WORKER_ADVERTISE_ADDRESS` must be reachable from the master. The worker runtime and `wasmcat-worker init` default this value to `<hostname>:<worker-port>` when it is not set, which is safer than localhost for multi-host installs. Set it explicitly when the master should use a private IP, DNS name, or load-balancer address. Use `localhost:<port>` only for same-host development.

Worker coordinates are detected automatically at startup when `WORKER_LATITUDE` and `WORKER_LONGITUDE` are omitted. Use `--latitude` and `--longitude` only when you need to pin the worker to a known site or when your cloud provider's public-IP geolocation is inaccurate.

Copy or mount these files into `/etc/wasmcat/certs`:

```text
ca.crt
worker-worker-us-01.crt
worker-worker-us-01.key
```

The worker certificate name must match the current worker certificate path logic: `worker-<WORKER_ID>.crt` and `worker-<WORKER_ID>.key`.

The worker certificate identity must also match the claimed worker ID. For `WORKER_ID=worker-us-01`, use either common name `wasmcat-worker-worker-us-01` or a DNS SAN containing `worker-us-01` or `wasmcat-worker-worker-us-01`.

Make the certificates readable by the service user:

```bash
sudo chown -R wasmcat:wasmcat /etc/wasmcat/certs
sudo chmod 0750 /etc/wasmcat/certs
sudo chmod 0640 /etc/wasmcat/certs/*.crt /etc/wasmcat/certs/*.key
```

Install the service file:

```bash
sudo install -m 0644 wasmcat-worker.service /etc/systemd/system/wasmcat-worker.service
```

Start the worker:

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now wasmcat-worker
sudo systemctl status wasmcat-worker
```

Check the worker:

```bash
curl -k https://localhost:7271/wasmcat/health
curl -k https://localhost:7271/wasmcat/ready
```

## Local Development Without Services

For a quick local run, generate local development certificates and config:

```powershell
go run ./cmd/master init --config-dir ./run/wasmcat --cert-dir ./run/wasmcat/certs --dev-certs --force
```

Start the master:

```powershell
$env:AUTO_GENERATE_CERTS="false"
$env:CERT_DIR="./run/wasmcat/certs"
go run ./cmd/master
```

Then start a worker in another terminal:

```powershell
$env:WORKER_ID="worker-vn-01"
$env:MASTER_URL="https://localhost:7270"
$env:WORKER_ADVERTISE_ADDRESS="localhost:7271"
$env:WORKER_LATITUDE="13.7563"
$env:WORKER_LONGITUDE="100.5018"
$env:CERT_DIR="./run/wasmcat/certs"
go run ./cmd/worker
```

This mode is for development only. Production should provide certificates through a controlled secret or configuration process and set `AUTO_GENERATE_CERTS=false`.

## Install or Update With the Script

`scripts/install-or-update.sh` automates first-time installs and in-place updates so you do not have to repeat the manual `install`/`restart` steps after every code change. It is safe to re-run.

What it does for each selected component:

- creates the `wasmcat` service user and `/etc/wasmcat` + `/var/lib/wasmcat` (master/worker roles), so installs never fail with `chown: invalid user`,
- backs up the current binary as `<binary>.bak` before replacing it,
- installs the new binary into `/usr/local/bin`,
- shows the installed version and the new version,
- optionally copies a certificate bundle (`--certs-from`) with explicit per-file permissions,
- restarts only the relevant systemd service (`wasmcat-master` or `wasmcat-worker`),
- automatically rolls that component back to `.bak` if the service fails to come back active.

It never overwrites `/etc/wasmcat/*.env` or the SQLite job database, so configuration and durable jobs are preserved across updates. (`--certs-from` is the one exception: it intentionally writes the certs you point it at.)

Build (or download) the binaries first, then run the script for the host role. The role can be positional or a `--role` flag:

```bash
# Build artifacts into ./dist on a build machine, then copy the repo or dist/ to the VM.
sh scripts/build.sh

# Master VM (positional role):
sudo sh scripts/install-or-update.sh master --source ./dist

# Worker VM:
sudo sh scripts/install-or-update.sh worker --source ./dist

# Master VM, also installing the cert bundle produced by gen-certs.sh:
sudo sh scripts/install-or-update.sh master --source ./dist --certs-from /tmp/master-certs

# Operator machine (no systemd services involved):
sh scripts/install-or-update.sh ctl,ui --source ./dist --bin-dir "$HOME/.local/bin"
```

Install directly from a tagged GitHub release instead of a local `dist/` (checksums are verified when published):

```bash
sudo sh scripts/install-or-update.sh --role master --version v0.1.0 --repo <owner>/<repo>
```

First-time install on a fresh VM can also drop the systemd unit file from the artifacts:

```bash
sudo sh scripts/install-or-update.sh --role master --source ./dist --install-service
# then provide /etc/wasmcat config + certs (see below) and:
sudo systemctl enable --now wasmcat-master
```

Roll a bad update back to the previous binary and restart the service:

```bash
sudo sh scripts/install-or-update.sh --role master --rollback
```

Preview every action without changing anything:

```bash
sudo sh scripts/install-or-update.sh --role master --source ./dist --dry-run
```

Run `sh scripts/install-or-update.sh --help` for the full option list.

## Generate mTLS Certificates

`scripts/gen-certs.sh` generates a complete cluster bundle from a single CA, which avoids the most common mTLS setup failures (CA mismatch between nodes, worker certificates missing `clientAuth`, master certificates missing the private IP in their SAN, and worker certificate filenames not matching `WORKER_ID`).

Run it on an operator machine (not on the nodes). The CA private key stays in the PKI directory (`./wasmcat-pki` by default) and is never copied to a node:

```bash
sh scripts/gen-certs.sh master --ip MASTER_PRIVATE_IP --dns master.internal
sh scripts/gen-certs.sh worker --id worker-vn-01 --ip WORKER_PRIVATE_IP
sh scripts/gen-certs.sh operator --id wasmcat-operator
```

This writes per-node bundles under `./wasmcat-certs/`:

```text
wasmcat-certs/master/                 ca.crt master.crt master.key
wasmcat-certs/worker-worker-vn-01/    ca.crt worker-worker-vn-01.crt worker-worker-vn-01.key
wasmcat-certs/operator/               ca.crt operator.crt operator.key
```

Copy each bundle to its node and install it with the cert step of the install script (which sets ownership and per-file permissions):

```bash
scp -r wasmcat-certs/master azureuser@MASTER_PRIVATE_IP:/tmp/master-certs
# on the master VM:
sudo sh scripts/install-or-update.sh master --source ./dist --certs-from /tmp/master-certs
```

Keep `wasmcat-pki/` (the CA) safe and offline. To add a worker later, run `gen-certs.sh worker --id NEW_ID --ip NEW_IP` against the same PKI directory; it reuses the existing CA so every node still trusts one authority. Verify the shared CA fingerprint at any time with `sh scripts/gen-certs.sh fingerprint`.

If the master sets `EXECUTE_CLIENT_ALLOWLIST`, add the operator identity (the operator certificate common name, e.g. `wasmcat-operator`) to it. See [EXECUTION_AUTHORIZATION.md](EXECUTION_AUTHORIZATION.md).

## Manual Upgrade

The script is preferred, but the manual path still works. Build or download the new release, replace the binary, and restart the service:

```bash
sudo install -m 0755 wasmcat-master-linux-amd64 /usr/local/bin/wasmcat-master
sudo systemctl restart wasmcat-master
```

For workers:

```bash
sudo install -m 0755 wasmcat-worker-linux-amd64 /usr/local/bin/wasmcat-worker
sudo systemctl restart wasmcat-worker
```

Keep `/etc/wasmcat/*.env` and `/etc/wasmcat/certs` outside the release artifact so upgrades do not overwrite host configuration or secrets.

## Check Installed Versions

Each binary reports its build version, which the update script uses to show before/after versions:

```bash
wasmcat-master version
wasmcat-worker version
wasmcatctl version
wasmcat-ui --version
```

A plain `go build`/`go run` reports `dev`; release builds report the tag passed through `VERSION=...`.
