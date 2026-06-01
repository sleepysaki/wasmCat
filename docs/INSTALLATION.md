# Installation

wasmCat is designed to run as native binaries. The install path does not require a container runtime, a separate WASM runtime, or another orchestrator.

## Installation Model

The simplest production layout is:

```text
/usr/local/bin/wasmcat-master
/usr/local/bin/wasmcat-worker
/etc/wasmcat/master.env
/etc/wasmcat/worker.env
/etc/wasmcat/certs/
```

The binaries include the WASM runtime through wazero, so workers do not need an external container runtime or a separate WebAssembly runtime. The host only needs the operating system, network access, certificates, and the wasmCat binary for the role it runs.

The initialization design is documented in [BOOTSTRAP_PLAN.md](BOOTSTRAP_PLAN.md). The short version is: install the binary, run `init`, provide certificates, then start the service.

The release process is documented in [RELEASE_PLAN.md](RELEASE_PLAN.md). Tagged releases publish binaries, service templates, env templates, and checksums.

## Install From A Release

Set the repository and release version:

```bash
REPO="<owner>/<repo>"
VERSION="v0.1.0"
BASE_URL="https://github.com/$REPO/releases/download/$VERSION"
```

Download the Linux master, Linux worker, systemd service files, and checksums:

```bash
curl -LO "$BASE_URL/wasmcat-master-linux-amd64"
curl -LO "$BASE_URL/wasmcat-worker-linux-amd64"
curl -LO "$BASE_URL/wasmcat-master.service"
curl -LO "$BASE_URL/wasmcat-worker.service"
curl -LO "$BASE_URL/checksums.txt"
```

Verify the downloaded files:

```bash
sha256sum -c checksums.txt --ignore-missing
```

Use only the binary required for the host role. A master host needs `wasmcat-master-linux-amd64`; a worker host needs `wasmcat-worker-linux-amd64`.

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
dist/wasmcat-master-windows-amd64.exe
dist/wasmcat-worker-windows-amd64.exe
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
  --cert-dir /etc/wasmcat/certs
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
CERT_DIR=/etc/wasmcat/certs
MAX_CONCURRENT_EXECS=4
```

Copy or mount these files into `/etc/wasmcat/certs`:

```text
ca.crt
worker-worker-us-01.crt
worker-worker-us-01.key
```

The worker certificate name must match the current worker certificate path logic: `worker-<WORKER_ID>.crt` and `worker-<WORKER_ID>.key`.

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
$env:CERT_DIR="./run/wasmcat/certs"
go run ./cmd/worker
```

This mode is for development only. Production should provide certificates through a controlled secret or configuration process and set `AUTO_GENERATE_CERTS=false`.

## Upgrade

Build or download the new release, replace the binary, and restart the service:

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
