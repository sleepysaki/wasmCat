# wasmCat Production VM Deployment Runbook

This guide deploys wasmCat as native Go binaries on Ubuntu 24.04 VMs. wasmCat does not require Docker, containerd, Kubernetes, or any sidecar runtime. The master runs the control plane, SQLite job state, ACR manifest resolution, and geo-aware scheduling. Workers fetch `.wasm` bytes directly into memory and execute them with the embedded wazero runtime.

If a node does not register during setup, use [NODE_SETUP_TROUBLESHOOTING.md](NODE_SETUP_TROUBLESHOOTING.md). It records real VM setup failures, including `localhost` master URLs, CA fingerprint mismatches, certificate SAN issues, SSH copy failures, and geolocation rate limits.

## 1. VM Specifications & Installation Guide

### Recommended VM Sizes

| Role | Minimum | Recommended | Notes |
| --- | --- | --- | --- |
| Master | 1 vCPU, 1 GiB RAM, 10 GiB disk | 2 vCPU, 2-4 GiB RAM, 20 GiB SSD | Mostly handles HTTPS routing, registry state, ACR token/manifest calls, and SQLite job state. |
| Worker | 2 vCPU, 2 GiB RAM, 20 GiB disk | 4 vCPU, 4-8 GiB RAM, 30 GiB SSD | Compiles and caches WASM modules in memory. Increase RAM for larger modules or higher cache limits. |

Use Ubuntu 24.04 LTS. Open only the ports that are required:

- Master: TCP `7270` from trusted workers and operator clients.
- Worker: TCP `7271` from the master only.
- Public HTTPS: TCP `443` only if you add a public API proxy.

### Build or Install Binaries

On a build machine with Go installed:

```bash
git clone https://github.com/<your-org>/wasmCat.git
cd wasmCat
go mod download
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o dist/wasmcat-master ./cmd/master
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o dist/wasmcat-worker ./cmd/worker
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o dist/wasmcatctl ./cmd/wasmcatctl
```

Copy binaries to the target VMs:

```bash
scp dist/wasmcat-master ubuntu@MASTER_PUBLIC_IP:/tmp/
scp dist/wasmcat-worker ubuntu@WORKER_PUBLIC_IP:/tmp/
scp dist/wasmcatctl ubuntu@MASTER_PUBLIC_IP:/tmp/
```

On each VM, install the binary and create the service user:

```bash
sudo useradd --system --home /var/lib/wasmcat --shell /usr/sbin/nologin wasmcat || true
sudo mkdir -p /etc/wasmcat /var/lib/wasmcat
sudo chown wasmcat:wasmcat /etc/wasmcat /var/lib/wasmcat
```

On the master VM:

```bash
sudo install -m 0755 /tmp/wasmcat-master /usr/local/bin/wasmcat-master
sudo install -m 0755 /tmp/wasmcatctl /usr/local/bin/wasmcatctl
```

On each worker VM:

```bash
sudo install -m 0755 /tmp/wasmcat-worker /usr/local/bin/wasmcat-worker
```

### Bootstrap Master

For a first real-environment test, generate development mTLS certificates on the master and copy the worker certificate to the worker VM. For production, replace these certificates with certificates from your own CA and keep `AUTO_GENERATE_CERTS=false`.

```bash
sudo wasmcat-master init \
  --config-dir /etc/wasmcat \
  --cert-dir /etc/wasmcat/certs \
  --port 7270 \
  --dev-certs \
  --dev-worker-id worker-vn-01 \
  --job-store-path /etc/wasmcat/wasmcat-jobs.db \
  --min-worker-cpu-free 10 \
  --min-worker-ram-free-mb 256 \
  --module-host-allowlist myregistry.azurecr.io \
  --force
```

Secure generated files:

```bash
sudo chown -R wasmcat:wasmcat /etc/wasmcat
sudo chmod 0750 /etc/wasmcat /etc/wasmcat/certs
sudo chmod 0640 /etc/wasmcat/master.env /etc/wasmcat/certs/*.crt
sudo chmod 0600 /etc/wasmcat/certs/*.key
```

Copy the worker materials to the worker VM:

```bash
sudo tar -C /etc/wasmcat/certs -czf /tmp/worker-vn-01-certs.tgz ca.crt worker-worker-vn-01.crt worker-worker-vn-01.key
scp /tmp/worker-vn-01-certs.tgz ubuntu@WORKER_PUBLIC_IP:/tmp/
```

### Bootstrap Worker

On the worker VM:

```bash
sudo mkdir -p /etc/wasmcat/certs
sudo tar -C /etc/wasmcat/certs -xzf /tmp/worker-vn-01-certs.tgz
sudo wasmcat-worker init \
  --config-dir /etc/wasmcat \
  --cert-dir /etc/wasmcat/certs \
  --worker-id worker-vn-01 \
  --port 7271 \
  --master-url https://MASTER_PRIVATE_OR_PUBLIC_IP:7270 \
  --advertise-address WORKER_PRIVATE_OR_PUBLIC_IP:7271 \
  --max-concurrent-execs 4 \
  --max-cached-modules 128 \
  --max-cache-bytes 268435456 \
  --force
sudo chown -R wasmcat:wasmcat /etc/wasmcat
sudo chmod 0750 /etc/wasmcat /etc/wasmcat/certs
sudo chmod 0640 /etc/wasmcat/worker.env /etc/wasmcat/certs/*.crt
sudo chmod 0600 /etc/wasmcat/certs/*.key
```

By default, the worker auto-detects the VM location during startup through `WORKER_LOCATION_PROVIDER_URL` and registers those coordinates with the master. The master stores the coordinates in its worker registry and uses them for Haversine routing. Pin coordinates only when you know the physical site or your cloud provider's public-IP geolocation is inaccurate:

```bash
sudo wasmcat-worker init \
  --config-dir /etc/wasmcat \
  --cert-dir /etc/wasmcat/certs \
  --worker-id worker-vn-01 \
  --master-url https://MASTER_PRIVATE_OR_PUBLIC_IP:7270 \
  --advertise-address WORKER_PRIVATE_OR_PUBLIC_IP:7271 \
  --latitude 21.0278 \
  --longitude 105.8342 \
  --force
```

### systemd Services

Create the master service on the master VM:

```bash
sudo tee /etc/systemd/system/wasmcat-master.service >/dev/null <<'EOF'
[Unit]
Description=wasmCat master control plane
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=wasmcat
Group=wasmcat
EnvironmentFile=/etc/wasmcat/master.env
ExecStart=/usr/local/bin/wasmcat-master
Restart=on-failure
RestartSec=5s
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/etc/wasmcat

[Install]
WantedBy=multi-user.target
EOF
sudo systemctl daemon-reload
sudo systemctl enable --now wasmcat-master
sudo systemctl status wasmcat-master --no-pager
```

Create the worker service on the worker VM:

```bash
sudo tee /etc/systemd/system/wasmcat-worker.service >/dev/null <<'EOF'
[Unit]
Description=wasmCat worker node
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=wasmcat
Group=wasmcat
EnvironmentFile=/etc/wasmcat/worker.env
ExecStart=/usr/local/bin/wasmcat-worker
Restart=on-failure
RestartSec=5s
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/etc/wasmcat

[Install]
WantedBy=multi-user.target
EOF
sudo systemctl daemon-reload
sudo systemctl enable --now wasmcat-worker
sudo systemctl status wasmcat-worker --no-pager
```

Verify logs:

```bash
journalctl -u wasmcat-master -f
journalctl -u wasmcat-worker -f
```

## 2. Code-to-Node Pipeline

### WASM ABI Required by wasmCat

The current worker expects each module to export:

```text
memory
malloc(size uint32) uint32
run(ptr uint32, len uint32) uint64
```

The `run` return value packs the output pointer and length:

```text
high 32 bits = output pointer
low 32 bits  = output length
```

### Example Addition Module

The current ABI is easiest to target with TinyGo because standard Go `GOOS=wasip1 GOARCH=wasm` produces a WASI module with runtime imports that wasmCat does not yet instantiate. Use this file as `add.go`:

```go
package main

import "unsafe"

var next uint32 = 1024

//export malloc
func malloc(size uint32) uint32 {
	ptr := next
	next += size
	return ptr
}

//export run
func run(ptr uint32, size uint32) uint64 {
	input := unsafe.Slice((*byte)(unsafe.Pointer(uintptr(ptr))), size)
	a, b := parsePair(input)
	output := itoa(a + b)
	outPtr := malloc(uint32(len(output)))
	out := unsafe.Slice((*byte)(unsafe.Pointer(uintptr(outPtr))), len(output))
	copy(out, output)
	return (uint64(outPtr) << 32) | uint64(len(output))
}

func parsePair(input []byte) (int, int) {
	value := 0
	left := 0
	seenComma := false
	for _, ch := range input {
		if ch == ',' {
			left = value
			value = 0
			seenComma = true
			continue
		}
		if ch >= '0' && ch <= '9' {
			value = value*10 + int(ch-'0')
		}
	}
	if !seenComma {
		return value, 0
	}
	return left, value
}

func itoa(value int) []byte {
	if value == 0 {
		return []byte("0")
	}
	var buf [20]byte
	i := len(buf)
	for value > 0 {
		i--
		buf[i] = byte('0' + value%10)
		value /= 10
	}
	return buf[i:]
}

func main() {}
```

Build it:

```bash
curl -LO https://github.com/tinygo-org/tinygo/releases/download/v0.33.0/tinygo_0.33.0_amd64.deb
sudo dpkg -i tinygo_0.33.0_amd64.deb
tinygo build -target=wasm -no-debug -o add.wasm add.go
sha256sum add.wasm
```

If you intentionally test the standard Go WASI compiler path, this command produces a WASI module, but that module is not compatible with the current wasmCat ABI-only worker without adding WASI host imports:

```bash
GOOS=wasip1 GOARCH=wasm go build -o add-wasi.wasm add.go
```

### Push the Module to Azure Container Registry

Install ORAS and Azure CLI:

```bash
sudo apt-get update
sudo apt-get install -y curl ca-certificates gpg
curl -sL https://aka.ms/InstallAzureCLIDeb | sudo bash
curl -LO https://github.com/oras-project/oras/releases/download/v1.2.0/oras_1.2.0_linux_amd64.tar.gz
tar -xzf oras_1.2.0_linux_amd64.tar.gz oras
sudo install -m 0755 oras /usr/local/bin/oras
```

Create or use an existing ACR:

```bash
az login
az group create --name wasmcat-rg --location southeastasia
az acr create --resource-group wasmcat-rg --name MYREGISTRY --sku Basic
az acr login --name MYREGISTRY
```

Push the artifact:

```bash
oras push MYREGISTRY.azurecr.io/wasmcat/add:v1 \
  --artifact-type application/wasm \
  add.wasm:application/wasm
```

Give the master VM identity pull access. For Azure VM managed identity:

```bash
az vm identity assign --resource-group wasmcat-rg --name MASTER_VM_NAME
PRINCIPAL_ID=$(az vm show --resource-group wasmcat-rg --name MASTER_VM_NAME --query identity.principalId -o tsv)
ACR_ID=$(az acr show --name MYREGISTRY --resource-group wasmcat-rg --query id -o tsv)
az role assignment create --assignee "$PRINCIPAL_ID" --role AcrPull --scope "$ACR_ID"
```

### Execute the Module

Create an operator config on the master VM or your admin machine:

```bash
wasmcatctl config init \
  --master https://MASTER_PUBLIC_OR_PRIVATE_IP:7270 \
  --ca /etc/wasmcat/certs/ca.crt \
  --cert /etc/wasmcat/certs/master.crt \
  --key /etc/wasmcat/certs/master.key
```

Preferred CLI execution:

```bash
wasmcatctl execute \
  --request-id req_add_001 \
  --module add \
  --registry-url https://MYREGISTRY.azurecr.io/v2/wasmcat/add/manifests/v1 \
  --payload "2,3"
```

Equivalent raw `curl` request:

```bash
curl --fail-with-body \
  --cacert /etc/wasmcat/certs/ca.crt \
  --cert /etc/wasmcat/certs/master.crt \
  --key /etc/wasmcat/certs/master.key \
  -H "Content-Type: application/json" \
  -d '{
    "request_id": "req_add_001",
    "module_name": "add",
    "module_registry_url": "https://MYREGISTRY.azurecr.io/v2/wasmcat/add/manifests/v1",
    "payload": "2,3",
    "user_lat": 21.0278,
    "user_lon": 105.8342
  }' \
  https://MASTER_PUBLIC_OR_PRIVATE_IP:7270/api/v1/execute
```

Expected response:

```json
{
  "request_id": "req_add_001",
  "result": "5",
  "execution_time_ms": 1.2,
  "executed_on_node_id": "worker-vn-01"
}
```

## 3. Exposing the Application Safely

wasmCat does not currently include a built-in reverse proxy for public, browser-style end-user traffic. The master gateway itself is strict mTLS. That is correct for internal node traffic, but public users should not be handed cluster client certificates casually.

Use two separate surfaces:

1. **Internal control plane:** Keep `:7270` mTLS for workers and trusted operators.
2. **Public API edge:** Put Nginx, Caddy, Azure Application Gateway, or Azure Front Door in front of a narrow API service. The edge terminates normal public HTTPS and forwards only approved requests to wasmCat using a dedicated mTLS client certificate.

### Azure NSG Rules

Recommended rules:

| VM | Port | Source | Purpose |
| --- | --- | --- | --- |
| Master | `7270` | Worker subnet, admin IPs, proxy private IP | wasmCat mTLS API. |
| Worker | `7271` | Master private IP only | Worker invoke API. |
| Proxy | `443` | Internet | Public HTTPS entrypoint. |
| Any | `22` | Admin IPs only | SSH administration. |

### Nginx Public HTTPS Proxy

This pattern terminates public HTTPS on `443`, then Nginx calls the mTLS master using a dedicated client certificate. Keep this proxy scoped to end-user execution routes only; do not expose `/internal/*`.

```nginx
server {
    listen 443 ssl http2;
    server_name api.example.com;

    ssl_certificate /etc/letsencrypt/live/api.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/api.example.com/privkey.pem;

    client_max_body_size 2m;

    location = /api/v1/execute {
        proxy_pass https://MASTER_PRIVATE_IP:7270/api/v1/execute;
        proxy_http_version 1.1;
        proxy_set_header Host MASTER_PRIVATE_IP;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto https;

        proxy_ssl_server_name off;
        proxy_ssl_trusted_certificate /etc/wasmcat/proxy/ca.crt;
        proxy_ssl_certificate /etc/wasmcat/proxy/proxy-client.crt;
        proxy_ssl_certificate_key /etc/wasmcat/proxy/proxy-client.key;
        proxy_ssl_verify on;
    }

    location /internal/ {
        return 403;
    }
}
```

Install and reload:

```bash
sudo apt-get update
sudo apt-get install -y nginx certbot python3-certbot-nginx
sudo certbot --nginx -d api.example.com
sudo nginx -t
sudo systemctl reload nginx
```

### Caddy Alternative

Caddy is simpler for automatic public TLS, but its upstream mTLS settings still need explicit certificates:

```caddy
api.example.com {
    request_body {
        max_size 2MB
    }

    handle /api/v1/execute {
        reverse_proxy https://MASTER_PRIVATE_IP:7270 {
            transport http {
                tls
                tls_server_name MASTER_PRIVATE_IP
                tls_trusted_ca_certs /etc/wasmcat/proxy/ca.crt
                tls_client_auth /etc/wasmcat/proxy/proxy-client.crt /etc/wasmcat/proxy/proxy-client.key
            }
        }
    }

    handle /internal/* {
        respond 403
    }
}
```

### Operational Checks

Run these after deployment:

```bash
systemctl is-active wasmcat-master
systemctl is-active wasmcat-worker
journalctl -u wasmcat-master --since "10 min ago" --no-pager
journalctl -u wasmcat-worker --since "10 min ago" --no-pager
wasmcatctl workers
wasmcatctl ready
```

If workers do not appear, check the worker certificate name, `WORKER_ID`, `MASTER_URL`, firewall access to `7270`, and whether `WORKER_ADVERTISE_ADDRESS` is reachable from the master.

For exact symptoms and fixes, see [NODE_SETUP_TROUBLESHOOTING.md](NODE_SETUP_TROUBLESHOOTING.md).
