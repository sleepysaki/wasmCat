# Bootstrap Initialization Plan

## Purpose

wasmCat should be easy to install without depending on a container runtime or a separate orchestrator. Native binaries already make the runtime lightweight, but manual setup still creates operational overhead: users must create config directories, write environment files, manage certificate paths, and keep worker IDs aligned with certificate names.

The bootstrap step reduces that setup to one explicit initialization command per node role. It keeps installation simple while preserving production controls around certificates and service configuration.

## Design Goals

- Keep wasmCat native-first: no Docker, containerd, Kubernetes, or external WASM runtime is required.
- Generate only small text configuration files that the existing runtime already understands.
- Avoid hidden global state by writing predictable files under a configurable directory.
- Protect existing configuration by refusing to overwrite files unless `--force` is provided.
- Support local development with generated mTLS certificates, but keep production certificate handling explicit.
- Keep master and worker startup behavior unchanged after initialization.

## Command Shape

The current project uses separate master and worker binaries, so bootstrap commands are attached to those binaries:

```bash
wasmcat-master init
wasmcat-worker init
```

This avoids introducing a new umbrella CLI before the project needs one. A future `wasmcat` CLI can wrap the same bootstrap package without changing the underlying behavior.

## Master Initialization

The master init command creates:

```text
/etc/wasmcat/master.env
/etc/wasmcat/certs/
```

Recommended production command:

```bash
sudo wasmcat-master init \
  --config-dir /etc/wasmcat \
  --cert-dir /etc/wasmcat/certs
```

The generated `master.env` disables automatic certificate generation:

```text
AUTO_GENERATE_CERTS=false
```

That keeps production startup deterministic. The generated file also includes scheduler capacity thresholds:

```text
MIN_WORKER_CPU_FREE=0
MIN_WORKER_RAM_FREE_MB=0
```

Operators can raise these values to stop the master from assigning new work to overloaded workers. Operators must provide `ca.crt`, `master.crt`, and `master.key` before starting the service.

For local development, the command can also generate a local CA, master cert, and one worker cert:

```bash
wasmcat-master init \
  --config-dir ./run/wasmcat \
  --cert-dir ./run/wasmcat/certs \
  --dev-certs \
  --dev-worker-id worker-vn-01
```

## Worker Initialization

The worker init command creates:

```text
/etc/wasmcat/worker.env
/etc/wasmcat/certs/
```

Recommended production command:

```bash
sudo wasmcat-worker init \
  --worker-id worker-us-01 \
  --master-url https://master.example.com:7270 \
  --advertise-address worker-us-01.example.com:7271 \
  --latitude 40.7128 \
  --longitude -74.0060 \
  --config-dir /etc/wasmcat \
  --cert-dir /etc/wasmcat/certs
```

The worker command validates that `MASTER_URL` is an HTTPS URL and writes the same location and limit settings used by the worker runtime.

Operators must provide:

```text
ca.crt
worker-<WORKER_ID>.crt
worker-<WORKER_ID>.key
```

## Safety Rules

- Existing `master.env` or `worker.env` files are not overwritten unless `--force` is passed.
- Development certificate generation refuses to overwrite existing certificate files unless `--force` is passed.
- Production initialization creates the certificate directory but does not fabricate production credentials.
- The init command exits after writing files. It does not start the long-running service.

## Operational Flow

The intended host setup is:

```text
install binary
run init command
provide or generate certificates
install systemd unit
start service
```

This keeps the platform easy to install while leaving service managers, certificates, and host identity visible to the operator.
