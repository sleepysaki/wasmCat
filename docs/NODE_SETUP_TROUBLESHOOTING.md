# Node Setup Troubleshooting

This guide records setup failures seen during a real Azure VM deployment. Use it when a master or worker service starts but the node does not become `ready`.

## First Checks

Run these before changing configuration:

```bash
sudo systemctl status wasmcat-master --no-pager
sudo systemctl status wasmcat-worker --no-pager
sudo journalctl -u wasmcat-master -n 100 --no-pager
sudo journalctl -u wasmcat-worker -n 100 --no-pager
wasmcatctl workers
```

Confirm the expected network values:

```bash
hostname -I
sudo cat /etc/wasmcat/master.env
sudo cat /etc/wasmcat/worker.env
sudo ls -la /etc/wasmcat/certs
```

## Worker Calls `localhost:7270`

Symptom:

```text
dial tcp 127.0.0.1:7270: connect: connection refused
```

Cause: `MASTER_URL=https://localhost:7270` was left in `worker.env`. On a separate worker VM, `localhost` is the worker itself, not the master.

Fix:

```bash
sudo sed -i 's|^MASTER_URL=.*|MASTER_URL=https://MASTER_PRIVATE_IP:7270|' /etc/wasmcat/worker.env
sudo sed -i 's|^WORKER_ADVERTISE_ADDRESS=.*|WORKER_ADVERTISE_ADDRESS=WORKER_PRIVATE_IP:7271|' /etc/wasmcat/worker.env
sudo systemctl restart wasmcat-worker
```

Use a private IP or private DNS name reachable inside the VNet. Use `localhost` only for same-host development.

## TLS Bad Certificate Or Unknown Authority

Symptoms:

```text
remote error: tls: bad certificate
x509: certificate signed by unknown authority
```

Cause: the master and worker are not using certificates from the same CA, or the worker certificate does not contain client authentication.

Verify the CA fingerprint on both VMs:

```bash
sudo openssl x509 -in /etc/wasmcat/certs/ca.crt -noout -fingerprint -sha256
```

The fingerprints must match. Also verify the worker certificate:

```bash
sudo openssl x509 \
  -in /etc/wasmcat/certs/worker-worker-vn-01.crt \
  -noout -subject -issuer -ext extendedKeyUsage -ext subjectAltName
```

The worker cert must be signed by the same CA trusted by the master and must include `clientAuth`. If the fingerprints differ, regenerate one complete bundle from a single CA and reinstall it on every node.

## `ca.crt` And `ca.key` Do Not Match

Symptom: a newly signed worker cert still fails mTLS.

Check whether the CA certificate and private key belong together:

```bash
openssl x509 -in ca.crt -noout -modulus | openssl md5
openssl rsa -in ca.key -noout -modulus | openssl md5
```

The hashes must match. If they differ, do not sign more certificates with that `ca.key`. Generate a new CA, then generate fresh master, worker, and operator certificates from that same CA.

## Certificate Name Does Not Match `WORKER_ID`

For:

```text
WORKER_ID=worker-vn-01
```

the worker files must be:

```text
/etc/wasmcat/certs/worker-worker-vn-01.crt
/etc/wasmcat/certs/worker-worker-vn-01.key
```

The certificate identity must also match the worker. Use common name `wasmcat-worker-worker-vn-01` or include `worker-vn-01` / `wasmcat-worker-worker-vn-01` in DNS SANs.

## Certificate SAN Does Not Match The IP

Symptom:

```text
x509: cannot validate certificate for MASTER_PRIVATE_IP because it doesn't contain any IP SANs
```

Cause: the master certificate was generated for `localhost`, but the worker connects to the master private IP.

Fix: regenerate the master certificate with the private IP in `subjectAltName`, then restart the master and copy the new CA to all clients if the CA changed.

Verify:

```bash
sudo openssl x509 -in /etc/wasmcat/certs/master.crt -noout -ext subjectAltName
```

## Auto Location Returns `429 Too Many Requests`

Symptom:

```text
auto-detect worker location: provider returned 429 Too Many Requests
```

Cause: the public geolocation provider rate-limited the VM.

Fix: switch to another provider supported by wasmCat. The worker accepts JSON with `latitude`/`longitude`, `lat`/`lon`, or `loc: "lat,lon"`.

```bash
curl -s https://ipinfo.io/json
sudo sed -i 's|^WORKER_AUTO_DETECT_LOCATION=.*|WORKER_AUTO_DETECT_LOCATION=true|' /etc/wasmcat/worker.env
sudo sed -i 's|^WORKER_LOCATION_PROVIDER_URL=.*|WORKER_LOCATION_PROVIDER_URL=https://ipinfo.io/json|' /etc/wasmcat/worker.env
sudo sed -i '/^WORKER_LATITUDE=/d' /etc/wasmcat/worker.env
sudo sed -i '/^WORKER_LONGITUDE=/d' /etc/wasmcat/worker.env
sudo systemctl restart wasmcat-worker
```

Expected log:

```text
worker location resolved ... source=auto
```

For production, prefer Azure metadata plus a region-to-coordinate map or an internal location provider. Public IP geolocation is useful for demos but is not a strong availability dependency.

## `chmod` Wildcard Says No Such File

Symptom:

```text
chmod: cannot access '/etc/wasmcat/certs/*.crt': No such file or directory
```

Cause: the cert files were not copied, were copied to another directory, or the shell could not expand the pattern.

Check:

```bash
sudo ls -la /etc/wasmcat/certs
```

Then use explicit filenames:

```bash
sudo chmod 0640 /etc/wasmcat/certs/ca.crt
sudo chmod 0640 /etc/wasmcat/certs/master.crt
sudo chmod 0600 /etc/wasmcat/certs/master.key
```

For a worker:

```bash
sudo chmod 0640 /etc/wasmcat/certs/ca.crt
sudo chmod 0640 /etc/wasmcat/certs/worker-worker-vn-01.crt
sudo chmod 0600 /etc/wasmcat/certs/worker-worker-vn-01.key
```

## `Permission Denied` Reading `/etc/wasmcat/certs`

Symptom:

```text
ls: cannot access '/etc/wasmcat/certs': Permission denied
```

Cause: the directory is owned by the `wasmcat` service user and is intentionally restricted.

Use `sudo` for inspection:

```bash
sudo ls -la /etc/wasmcat/certs
```

Keep ownership restricted:

```bash
sudo chown -R wasmcat:wasmcat /etc/wasmcat
sudo chmod 0750 /etc/wasmcat /etc/wasmcat/certs
```

## `chown: invalid user: 'wasmcat:wasmcat'`

Symptom:

```text
chown: invalid user: 'wasmcat:wasmcat'
```

Cause: the service user was not created on that VM.

Fix:

```bash
sudo useradd --system --home /var/lib/wasmcat --shell /usr/sbin/nologin wasmcat || true
sudo mkdir -p /var/lib/wasmcat
sudo chown -R wasmcat:wasmcat /etc/wasmcat /var/lib/wasmcat
```

## `scp` Permission Denied Publickey

Symptom:

```text
Permission denied (publickey).
scp: Connection closed
```

Cause: the source VM does not have an SSH private key trusted by the target VM.

Fix from the source VM:

```bash
ssh-keygen -t ed25519 -f ~/.ssh/id_ed25519 -N ""
cat ~/.ssh/id_ed25519.pub
```

Add the full public key line, including the `ssh-ed25519` prefix, to the target VM's `/home/azureuser/.ssh/authorized_keys`. Then retry:

```bash
scp -i ~/.ssh/id_ed25519 /tmp/worker-vn-01-certs.tgz azureuser@WORKER_PRIVATE_IP:/tmp/
```

## Go Version Or `go.mod` Error

Symptom:

```text
invalid go version '1.26.2': must match format 1.23
```

Cause: the VM has an older Go toolchain that cannot parse the module's `go` directive.

Fix: install the Go version required by `go.mod`, or build binaries elsewhere and copy only the compiled artifacts to the VMs. For production installs, prefer release binaries or a dedicated build machine over building on every node.

## One vCPU Worker

A one-vCPU worker can run functional demos, but do not allow high local concurrency:

```bash
sudo sed -i 's|^MAX_CONCURRENT_EXECS=.*|MAX_CONCURRENT_EXECS=1|' /etc/wasmcat/worker.env
sudo systemctl restart wasmcat-worker
```

Use larger workers for realistic throughput or module cache benchmarks.

## Expected Healthy State

A healthy worker log shows:

```text
worker location resolved ... source=auto
worker server live ... port=7271
worker registration sent ... status=200
```

The master should list the node:

```bash
wasmcatctl workers
```

Expected shape:

```text
ID            ADDRESS          STATE  CPU FREE  RAM FREE  LAST SEEN
worker-vn-01  172.16.0.5:7271  ready  100.0%    7264 MB   2s ago
```
