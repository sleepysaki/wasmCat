#!/usr/bin/env sh
# gen-certs.sh generates a consistent mTLS bundle for a wasmCat cluster from a
# single certificate authority. It exists because the most common deployment
# failures are CA/cert mismatches: master and worker trusting different CAs,
# worker certs missing clientAuth, master certs missing the private IP in their
# SAN, or worker cert filenames not matching WORKER_ID.
#
# The CA private key stays in a dedicated PKI working directory (default
# ./wasmcat-pki) and is never written into a node's cert directory. Keep that
# directory off the nodes (ideally offline); only the per-node bundles below are
# copied to the master, workers, and operator machines.
#
# Examples:
#   ./gen-certs.sh ca
#   ./gen-certs.sh master --ip 10.0.0.4 --dns master.internal
#   ./gen-certs.sh worker --id worker-vn-01 --ip 10.0.0.5
#   ./gen-certs.sh operator --id wasmcat-operator
#   ./gen-certs.sh fingerprint
#
# Output bundles are written under <out>/ (default ./wasmcat-certs):
#   master/   ca.crt master.crt master.key
#   worker-<id>/  ca.crt worker-<id>.crt worker-<id>.key
#   operator/ ca.crt operator.crt operator.key
#
# Requires openssl (Ubuntu 24.04 ships OpenSSL 3.x). No Docker, no Go.

set -eu

PKI_DIR="./wasmcat-pki"
OUT_DIR="./wasmcat-certs"
ACTION=""
ID=""
IP=""
DNS=""
DAYS_CA=3650
DAYS_LEAF=365

log() { printf '%s\n' "$*"; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

usage() {
  cat <<'EOF'
Usage: gen-certs.sh ACTION [options]

Actions:
  ca                         Create the certificate authority (idempotent).
  master --ip IP [--dns N]   Create master.crt/master.key (CN wasmcat-master).
  worker --id ID --ip IP     Create worker-<ID>.crt/.key (CN wasmcat-worker-<ID>).
         [--dns N]
  operator [--id NAME]       Create operator.crt/.key (clientAuth only).
  fingerprint                Print the CA SHA-256 fingerprint.

Options:
  --pki-dir DIR   CA working directory holding ca.crt and ca.key (default ./wasmcat-pki).
  --out DIR       Output directory for per-node bundles (default ./wasmcat-certs).
  --ip IP         IP address(es) to add to the certificate SAN (comma-separated
                  for several, e.g. a master on both private and public IPs).
  --dns NAME      DNS name(s) to add to the certificate SAN (comma-separated).
  --id VALUE      Worker ID (worker) or operator identity (operator).
  -h, --help      Show this help.

The CA private key (ca.key) lives only in --pki-dir. Never copy it to a node.
EOF
}

[ $# -ge 1 ] || { usage; die "an action is required"; }
ACTION="$1"
shift

while [ $# -gt 0 ]; do
  case "$1" in
    --pki-dir) PKI_DIR="${2:-}"; shift 2 ;;
    --pki-dir=*) PKI_DIR="${1#*=}"; shift ;;
    --out) OUT_DIR="${2:-}"; shift 2 ;;
    --out=*) OUT_DIR="${1#*=}"; shift ;;
    --id) ID="${2:-}"; shift 2 ;;
    --id=*) ID="${1#*=}"; shift ;;
    --ip) IP="${2:-}"; shift 2 ;;
    --ip=*) IP="${1#*=}"; shift ;;
    --dns) DNS="${2:-}"; shift 2 ;;
    --dns=*) DNS="${1#*=}"; shift ;;
    -h|--help) usage; exit 0 ;;
    *) die "unknown argument: $1 (use --help)" ;;
  esac
done

command -v openssl >/dev/null 2>&1 || die "openssl is required"

CA_CRT="$PKI_DIR/ca.crt"
CA_KEY="$PKI_DIR/ca.key"

ensure_ca() {
  mkdir -p "$PKI_DIR"
  if [ -f "$CA_CRT" ] && [ -f "$CA_KEY" ]; then
    return 0
  fi
  log "creating certificate authority in $PKI_DIR"
  openssl req -x509 -newkey rsa:2048 -nodes \
    -keyout "$CA_KEY" -out "$CA_CRT" -days "$DAYS_CA" \
    -subj "/CN=wasmcat-local-ca" \
    -addext "basicConstraints=critical,CA:TRUE" \
    -addext "keyUsage=critical,keyCertSign,cRLSign" >/dev/null 2>&1
  chmod 0600 "$CA_KEY"
  log "CA created: $CA_CRT"
}

# csv_to_san PREFIX LIST -> "PREFIX:a,PREFIX:b" from a comma-separated LIST.
# Lets --ip and --dns each carry several values, e.g. a master reachable on both
# a private and a public address.
csv_to_san() {
  prefix="$1"
  out=""
  oldifs="$IFS"
  IFS=','
  for item in $2; do
    item="$(printf '%s' "$item" | tr -d '[:space:]')"
    [ -n "$item" ] || continue
    if [ -z "$out" ]; then
      out="$prefix:$item"
    else
      out="$out,$prefix:$item"
    fi
  done
  IFS="$oldifs"
  printf '%s' "$out"
}

# sign_leaf CN OUT_BASE SAN_LIST
# OUT_BASE is the path prefix (without extension) for the generated .crt/.key.
sign_leaf() {
  cn="$1"
  out_base="$2"
  san="$3"

  key="$out_base.key"
  crt="$out_base.crt"
  csr="$out_base.csr"
  ext="$out_base.ext"

  openssl req -newkey rsa:2048 -nodes -keyout "$key" -subj "/CN=$cn" -out "$csr" >/dev/null 2>&1

  {
    echo "basicConstraints=CA:FALSE"
    echo "keyUsage=digitalSignature,keyEncipherment"
    echo "extendedKeyUsage=serverAuth,clientAuth"
    if [ -n "$san" ]; then
      echo "subjectAltName=$san"
    fi
  } > "$ext"

  openssl x509 -req -in "$csr" -CA "$CA_CRT" -CAkey "$CA_KEY" -CAcreateserial \
    -days "$DAYS_LEAF" -sha256 -extfile "$ext" -out "$crt" >/dev/null 2>&1

  chmod 0600 "$key"
  rm -f "$csr" "$ext"
}

# sign_operator CN OUT_BASE  (clientAuth only; operators never serve TLS)
sign_operator() {
  cn="$1"
  out_base="$2"

  key="$out_base.key"
  crt="$out_base.crt"
  csr="$out_base.csr"
  ext="$out_base.ext"

  openssl req -newkey rsa:2048 -nodes -keyout "$key" -subj "/CN=$cn" -out "$csr" >/dev/null 2>&1
  {
    echo "basicConstraints=CA:FALSE"
    echo "keyUsage=digitalSignature"
    echo "extendedKeyUsage=clientAuth"
    echo "subjectAltName=DNS:$cn"
  } > "$ext"
  openssl x509 -req -in "$csr" -CA "$CA_CRT" -CAkey "$CA_KEY" -CAcreateserial \
    -days "$DAYS_LEAF" -sha256 -extfile "$ext" -out "$crt" >/dev/null 2>&1
  chmod 0600 "$key"
  rm -f "$csr" "$ext"
}

print_fingerprint() {
  [ -f "$CA_CRT" ] || die "no CA at $CA_CRT; run 'gen-certs.sh ca' first"
  log "CA fingerprint (must match on every node):"
  openssl x509 -in "$CA_CRT" -noout -fingerprint -sha256
}

case "$ACTION" in
  ca)
    ensure_ca
    print_fingerprint
    ;;

  master)
    [ -n "$IP" ] || die "master needs --ip (the address workers use to reach the master)"
    ensure_ca
    bundle="$OUT_DIR/master"
    mkdir -p "$bundle"
    san="DNS:localhost,IP:127.0.0.1"
    [ -n "$IP" ] && san="$san,$(csv_to_san IP "$IP")"
    [ -n "$DNS" ] && san="$san,$(csv_to_san DNS "$DNS")"
    sign_leaf "wasmcat-master" "$bundle/master" "$san"
    cp "$CA_CRT" "$bundle/ca.crt"
    log "master bundle ready: $bundle (ca.crt master.crt master.key)"
    print_fingerprint
    ;;

  worker)
    [ -n "$ID" ] || die "worker needs --id (must match WORKER_ID)"
    [ -n "$IP" ] || die "worker needs --ip (the address the master uses to reach the worker)"
    ensure_ca
    bundle="$OUT_DIR/worker-$ID"
    mkdir -p "$bundle"
    # CN and SANs both carry the worker identity so the master's identity check
    # accepts the cert, and the IP SAN lets the master verify the TLS server.
    san="DNS:localhost,DNS:$ID,DNS:wasmcat-worker-$ID,IP:127.0.0.1"
    [ -n "$IP" ] && san="$san,$(csv_to_san IP "$IP")"
    [ -n "$DNS" ] && san="$san,$(csv_to_san DNS "$DNS")"
    sign_leaf "wasmcat-worker-$ID" "$bundle/worker-$ID" "$san"
    cp "$CA_CRT" "$bundle/ca.crt"
    log "worker bundle ready: $bundle (ca.crt worker-$ID.crt worker-$ID.key)"
    log "install the worker cert files as worker-$ID.crt and worker-$ID.key (filename must match WORKER_ID)"
    print_fingerprint
    ;;

  operator)
    [ -n "$ID" ] || ID="wasmcat-operator"
    ensure_ca
    bundle="$OUT_DIR/operator"
    mkdir -p "$bundle"
    sign_operator "$ID" "$bundle/operator"
    cp "$CA_CRT" "$bundle/ca.crt"
    log "operator bundle ready: $bundle (ca.crt operator.crt operator.key)"
    log "if the master sets EXECUTE_CLIENT_ALLOWLIST, add '$ID' to it"
    print_fingerprint
    ;;

  fingerprint)
    print_fingerprint
    ;;

  -h|--help|help)
    usage
    ;;

  *)
    usage
    die "unknown action: $ACTION"
    ;;
esac
